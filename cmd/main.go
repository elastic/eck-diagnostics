// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/elastic/eck-diagnostics/internal"
	internal_filters "github.com/elastic/eck-diagnostics/internal/filters"
	"github.com/spf13/cobra"
)

const (
	operatorNamespaces  = "operator-namespaces"
	resourcesNamespaces = "resources-namespaces"
)

var (
	filters    []string
	diagParams = internal.Params{}
)

func main() {
	cmd := &cobra.Command{
		Use:     "eck-diagnostics",
		Short:   "ECK support diagnostics tool",
		Long:    "Dump ECK and Kubernetes data for support and troubleshooting purposes.",
		PreRunE: preRunOperations,
		Version: internal.Version(),
		RunE: func(_ *cobra.Command, args []string) error {
			return internal.Run(diagParams)
		},
	}
	cmd.Flags().StringVar(&diagParams.DiagnosticImage, "diagnostic-image", internal.DiagnosticImage, "Diagnostic image to be used for stack diagnostics, see run-stack-diagnostics")
	cmd.Flags().BoolVar(&diagParams.RunStackDiagnostics, "run-stack-diagnostics", true, "Run diagnostics on deployed Elasticsearch clusters and Kibana instances, requires deploying diagnostic Pods into the cluster")
	cmd.Flags().BoolVar(&diagParams.RunAgentDiagnostics, "run-agent-diagnostics", false, "Run diagnostics on deployed Elastic Agents. Warning: credentials will not be redacted and appear as plain text in the archive")
	cmd.Flags().StringSliceVarP(&diagParams.OperatorNamespaces, operatorNamespaces, "o", []string{"elastic-system"}, "Comma-separated list of namespace(s) in which operator(s) are running")
	cmd.Flags().StringSliceVarP(&diagParams.ResourcesNamespaces, resourcesNamespaces, "r", nil, "Comma-separated list of namespace(s) in which resources are managed")
	cmd.Flags().StringSliceVarP(&filters, "filters", "f", nil, fmt.Sprintf(`Comma-separated list of filters in format "type=name". ex: elasticsearch=my-cluster (Supported types %v)`, internal_filters.ValidTypes))
	cmd.Flags().StringVar(&diagParams.ECKVersion, "eck-version", "", "ECK version in use, will try to autodetect if not specified")
	cmd.Flags().StringVar(&diagParams.OutputDir, "output-directory", "", "Path where to output diagnostic results")
	cmd.Flags().StringVarP(&diagParams.OutputName, "output-name", "n", fmt.Sprintf("eck-diagnostics-%s.zip", time.Now().Format("2006-01-02T15-04-05")), "Name of the output diagnostics file")
	cmd.Flags().StringVar(&diagParams.Kubeconfig, "kubeconfig", "", "optional path to kube config, defaults to $HOME/.kube/config")
	cmd.Flags().BoolVar(&diagParams.Verbose, "verbose", false, "Verbose mode")
	cmd.Flags().DurationVar(&diagParams.StackDiagnosticsTimeout, "stack-diagnostics-timeout", 5*time.Minute, "Maximum time to wait for Elaticsearch and Kibana diagnostics to complete")

	if err := cmd.MarkFlagRequired(resourcesNamespaces); err != nil {
		exitWithError(err)
	}

	if err := cmd.Execute(); err != nil {
		// cobra logs the error already no need to redo that
		exitWithError(nil)
	}
}

func preRunOperations(cmd *cobra.Command, args []string) error {
	if err := validation(cmd, args); err != nil {
		return err
	}
	return parseFilters(cmd, args)
}

func validation(_ *cobra.Command, _ []string) error {
	if diagParams.OutputName == "" {
		return fmt.Errorf("output-name cannot be empty")
	}
	if filepath.Ext(diagParams.OutputName) != ".zip" {
		return fmt.Errorf("output-name extension must end in '.zip'")
	}

	type validations struct {
		namespaces []string
		name       string
	}
	for _, v := range []validations{
		{
			namespaces: diagParams.OperatorNamespaces,
			name:       operatorNamespaces,
		}, {
			namespaces: diagParams.ResourcesNamespaces,
			name:       resourcesNamespaces,
		},
	} {
		if len(v.namespaces) == 0 {
			return fmt.Errorf("%s is a required parameter", v.name)
		}
		for _, ns := range v.namespaces {
			if ns == "" {
				return fmt.Errorf("%s cannot be an empty string", v.name)
			}
		}
	}
	return nil
}

func parseFilters(_ *cobra.Command, _ []string) error {
	filters, err := internal_filters.New(filters)
	if err != nil {
		return err
	}
	diagParams.Filters = filters
	return nil
}

func exitWithError(err error) {
	if err != nil {
		log.Printf("Error: %v", err)
	}
	os.Exit(1)
}
