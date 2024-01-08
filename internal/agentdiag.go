// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"errors"
	"fmt"
	"time"

	"github.com/elastic/eck-diagnostics/internal/archive"
	"github.com/elastic/eck-diagnostics/internal/extraction"
	internal_filters "github.com/elastic/eck-diagnostics/internal/filters"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/cli-runtime/pkg/resource"
)

// based on eck-operator code the agent container name is constant "agent"
const agentContainerName = "agent"

func runAgentDiagnostics(k *Kubectl, ns string, zipFile *archive.ZipFile, verbose bool, stopCh chan struct{}, filters internal_filters.Filters) {
	outputFile := time.Now().Format("eck-agent-diag-2006-01-02T15-04-05Z.zip")
	resources, err := k.getResourcesMatching("pod", ns, "common.k8s.elastic.co/type=agent")
	if err != nil {
		zipFile.AddError(err)
		return // unrecoverable let's return
	}
	if err := resources.Visit(func(info *resource.Info, _ error) error {
		select {
		case <-stopCh:
			return errors.New("aborting Elastic Agent diagnostic")
		default:
			// continue processing agents
		}

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
			zipFile.AddError(err)
			return nil
		}

		if ver.LessThan(version.MustParseSemantic("7.16.0")) {
			logger.Printf("Skipping %s/%s as it is below min version of 7.16.0", ns, resourceName)
			return nil
		}

		if !filters.Matches(labels) {
			return nil
		}

		nsn := types.NamespacedName{Namespace: ns, Name: resourceName}

		needsCleanup := diagnosticForAgentPod(nsn, k, outputFile, zipFile, verbose)

		// no matter what happened: try to clean up the diagnostic archive in the agent container
		if err := k.Exec(nsn, agentContainerName, "rm", outputFile); err != nil {
			// but only report any errors during cleaning up if there is a likelihood that we created an archive to clean up
			// in the first place
			if needsCleanup {
				zipFile.AddError(fmt.Errorf("while cleaning up agent container %s: %w", nsn, err))
			}
		}
		return nil
	}); err != nil {
		zipFile.AddError(err)
	}
}

// diagnosticForAgentPod runs the diagnostic sub command in the agent container identified by nsn. Returns a boolean indicating
// whether the diagnostic command has run and there is a diagnostic archive in the container to clean up after to avoid filling up
// the containers file system.
func diagnosticForAgentPod(nsn types.NamespacedName, k *Kubectl, outputFile string, zipFile *archive.ZipFile, verbose bool) bool {
	logger.Printf("Extracting agent diagnostics for %s", nsn)
	if err := k.Exec(nsn, agentContainerName, "elastic-agent", "diagnostics", "collect", "-f", outputFile); err != nil {
		zipFile.AddError(fmt.Errorf("while extracting agent diagnostics: %w", err))
		return false
	}

	reader, err := k.Copy(nsn, agentContainerName, outputFile, zipFile.AddError)
	if err != nil {
		zipFile.AddError(err)
		return true
	}

	source := extraction.RemoteSource{
		Namespace:    nsn.Namespace,
		PodName:      nsn.Name, // no separate diagnostic Pod in this case
		Typ:          "agent",
		ResourceName: nsn.Name,
		PodOutputDir: "/",
	}
	if err := extraction.UntarIntoZip(reader, source, zipFile, verbose); err != nil {
		zipFile.AddError(fmt.Errorf("while copying diagnostic data from Pod %s into diagnostic archive: %w", nsn, err))
	}
	return true
}
