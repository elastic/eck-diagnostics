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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/cli-runtime/pkg/resource"
)

func runAgentDiagnostics(k *Kubectl, ns string, zipFile *archive.ZipFile, verbose bool, stopCh chan struct{}) {
	outputFile := time.Now().Format("eck-agent-diag-2006-01-02T15-04-05Z.zip")
	resources, err := k.getResourcesMatching("pod", ns, " common.k8s.elastic.co/type=agent")
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
			zipFile.AddError(fmt.Errorf("while accessing labels for %s/%s: %w", resourceName, ns, err))
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

		nsn := types.NamespacedName{Namespace: ns, Name: resourceName}

		logger.Printf("Extracting agent diagnostics for %s", nsn)
		if err = k.Exec(nsn, "elastic-agent", "diagnostics", "collect", "-f", outputFile); err != nil {
			zipFile.AddError(fmt.Errorf("while extracting agent diagnostics: %w", err))
			return nil
		}

		reader, err := k.Copy(nsn, "agent", outputFile, zipFile.AddError)
		if err != nil {
			zipFile.AddError(err)
			return nil
		}

		source := extraction.RemoteSource{
			Namespace:    ns,
			PodName:      resourceName, // no separate diagnostic Pod in this case
			Typ:          "agent",
			ResourceName: resourceName,
			PodOutputDir: "/",
		}
		if err := extraction.UntarIntoZip(reader, source, zipFile, verbose); err != nil {
			zipFile.AddError(fmt.Errorf("while copying diagnostic data from Pod %s into diagnostic archive: %w", nsn, err))
		}
		if err := k.Exec(nsn, "rm", outputFile); err != nil {
			zipFile.AddError(fmt.Errorf("while cleaning up agent container %s: %w", nsn, err))
			return err
		}
		return nil
	}); err != nil {
		zipFile.AddError(err)
	}
}
