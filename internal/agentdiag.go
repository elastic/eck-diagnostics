// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/cli-runtime/pkg/resource"

	"github.com/elastic/eck-diagnostics/internal/archive"
	"github.com/elastic/eck-diagnostics/internal/extraction"
	internalfilters "github.com/elastic/eck-diagnostics/internal/filters"
)

// based on eck-operator code the agent container name is constant "agent"
const agentContainerName = "agent"

// agentDiagnosticJob represents a diagnostic collection job for a single agent pod.
type agentDiagnosticJob struct {
	nsn        types.NamespacedName
	outputFile string
	execErr    error // error from running elastic-agent diagnostics
}

func runAgentDiagnostics(ctx context.Context, k *Kubectl, ns string, zipFile *archive.ZipFile, verbose bool, diagTimeout time.Duration, concurrency int, filters internalfilters.Filters) {
	resources, err := k.getResourcesMatching("pod", ns, "common.k8s.elastic.co/type=agent")
	if err != nil {
		zipFile.AddError(err)
		return // unrecoverable let's return
	}

	// Collect all agent pods that need diagnostics
	var jobs []*agentDiagnosticJob
	if err := resources.Visit(func(info *resource.Info, _ error) error {
		resourceName := info.Name
		labels, err := meta.NewAccessor().Labels(info.Object)
		if err != nil {
			zipFile.AddError(fmt.Errorf("while accessing labels for %s/%s: %w", ns, resourceName, err))
			return nil
		}

		v, found := labels["agent.k8s.elastic.co/version"]
		if !found || v == "" {
			logger.Printf("Skipping %s/%s as it has no version", ns, resourceName)
			return nil
		}
		ver, err := version.ParseSemantic(v)
		if err != nil {
			zipFile.AddError(fmt.Errorf("failed to parse agent version %q: %w", v, err))
			return nil
		}

		if ver.LessThan(version.MustParseSemantic("7.16.0")) {
			logger.Printf("Skipping %s/%s as it is below min version of 7.16.0", ns, resourceName)
			return nil
		}

		if !filters.Matches(labels) {
			return nil
		}

		jobs = append(jobs, &agentDiagnosticJob{
			nsn:        types.NamespacedName{Namespace: ns, Name: resourceName},
			outputFile: fmt.Sprintf("eck-agent-diag-%s.zip", resourceName),
		})
		return nil
	}); err != nil {
		zipFile.AddError(err)
		return
	}

	if len(jobs) == 0 {
		return
	}

	// Run elastic-agent diagnostics concurrently (this part can be slow and prone to errors)
	runAgentDiagnosticsExec(ctx, k, jobs, diagTimeout, concurrency)

	// Sequentially copy results into zip and clean up
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			logger.Printf("Aborting Elastic Agent diagnostics: %v", ctx.Err())
			return
		default:
		}

		if job.execErr == nil {
			copyAgentDiagnostics(ctx, k, job, zipFile, verbose)
		} else {
			zipFile.AddError(fmt.Errorf("while collecting agent diagnostics for %s: %w", job.nsn, job.execErr))
		}

		cleanupAgentDiagnostics(ctx, k, job)
	}
}

// runAgentDiagnosticsExec runs elastic-agent diagnostics concurrently across all jobs.
func runAgentDiagnosticsExec(ctx context.Context, k *Kubectl, jobs []*agentDiagnosticJob, diagTimeout time.Duration, concurrency int) {
	var wg sync.WaitGroup

	// Spawn worker goroutines
	numWorkers := min(concurrency, len(jobs))
	workCh := make(chan *agentDiagnosticJob, numWorkers)
	for range numWorkers {
		wg.Go(func() {
			for job := range workCh {
				logger.Printf("Collecting agent diagnostics for %s", job.nsn)
				diagCtx, diagCancel := context.WithTimeout(ctx, diagTimeout)
				job.execErr = k.Exec(diagCtx, job.nsn, agentContainerName, "elastic-agent", "diagnostics", "collect", "-f", job.outputFile)
				diagCancel()
			}
		})
	}

	// Send work to workers
sendLoop:
	for _, job := range jobs {
		select {
		case workCh <- job:
		case <-ctx.Done():
			break sendLoop
		}
	}
	close(workCh)
	wg.Wait()
}

// copyAgentDiagnostics copies the diagnostic archive from the agent container into the zip file.
func copyAgentDiagnostics(ctx context.Context, k *Kubectl, job *agentDiagnosticJob, zipFile *archive.ZipFile, verbose bool) {
	logger.Printf("Extracting agent diagnostics for %s", job.nsn)

	reader, err := k.Copy(ctx, job.nsn, agentContainerName, job.outputFile, zipFile.AddError)
	if err != nil {
		zipFile.AddError(err)
		return
	}

	defer func() {
		// Drain and close the reader. k.Copy returns a pipe connected to a goroutine streaming
		// from the remote tar command. If we exit early (e.g., UntarIntoZip fails), the pipe
		// buffer fills and the goroutine blocks forever. Draining and closing unblocks it.
		_, _ = io.Copy(io.Discard, reader)
		_ = reader.Close()
	}()

	source := extraction.RemoteSource{
		Namespace:    job.nsn.Namespace,
		PodName:      job.nsn.Name,
		Typ:          "agent",
		ResourceName: job.nsn.Name,
		PodOutputDir: "/",
	}
	if err := extraction.UntarIntoZip(reader, source, zipFile, verbose); err != nil {
		zipFile.AddError(fmt.Errorf("while copying diagnostic data from Pod %s into diagnostic archive: %w", job.nsn, err))
	}
}

// cleanupAgentDiagnostics removes the diagnostic archive from the agent container.
// Errors are only logged if exec succeeded (i.e., a file was likely created).
func cleanupAgentDiagnostics(ctx context.Context, k *Kubectl, job *agentDiagnosticJob) {
	if err := k.Exec(ctx, job.nsn, agentContainerName, "rm", job.outputFile); err != nil && job.execErr == nil {
		logger.Printf("while cleaning up agent container %s: %v", job.nsn, err)
	}
}
