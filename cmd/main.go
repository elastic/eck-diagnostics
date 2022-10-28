// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/elastic/eck-diagnostics/internal"
	"github.com/spf13/cobra"
)

var (
	filters                 []string
	diagParams              = internal.Params{}
	elasticTypeKey          = "common.k8s.elastic.co/type"
	elasticsearchNameFormat = "%s.k8s.elastic.co/cluster-name"
	elasticNameFormat       = "%s.k8s.elastic.co/name"
)

func main() {
	cmd := &cobra.Command{
		Use:     "eck-diagnostics",
		Short:   "ECK support diagnostics tool",
		Long:    "Dump ECK and Kubernetes data for support and troubleshooting purposes.",
		PreRunE: formatFilters,
		RunE: func(cmd *cobra.Command, args []string) error {
			return internal.Run(diagParams)
		},
	}
	cmd.Flags().StringVar(&diagParams.DiagnosticImage, "diagnostic-image", internal.DiagnosticImage, "Diagnostic image to be used for stack diagnostics, see run-stack-diagnostics")
	cmd.Flags().BoolVar(&diagParams.RunStackDiagnostics, "run-stack-diagnostics", true, "Run diagnostics on deployed Elasticsearch clusters and Kibana instances, requires deploying diagnostic Pods into the cluster")
	cmd.Flags().BoolVar(&diagParams.RunAgentDiagnostics, "run-agent-diagnostics", false, "Run diagnostics on deployed Elastic Agents. Warning: credentials will not be redacted and appear as plain text in the archive")
	cmd.Flags().StringSliceVarP(&diagParams.OperatorNamespaces, "operator-namespaces", "o", []string{"elastic-system"}, "Comma-separated list of namespace(s) in which operator(s) are running")
	cmd.Flags().StringSliceVarP(&diagParams.ResourcesNamespaces, "resources-namespaces", "r", nil, "Comma-separated list of namespace(s) in which resources are managed")
	cmd.Flags().StringSliceVarP(&filters, "filters", "f", nil, `Comma-separated list of filters in format "type=type, name=name" (Supported types 'elasticsearch')`)
	cmd.Flags().StringVar(&diagParams.ECKVersion, "eck-version", "", "ECK version in use, will try to autodetect if not specified")
	cmd.Flags().StringVar(&diagParams.OutputDir, "output-directory", "", "Path where to output diagnostic results")
	cmd.Flags().StringVar(&diagParams.Kubeconfig, "kubeconfig", "", "optional path to kube config, defaults to $HOME/.kube/config")
	cmd.Flags().BoolVar(&diagParams.Verbose, "verbose", false, "Verbose mode")
	cmd.Flags().DurationVar(&diagParams.StackDiagnosticsTimeout, "stack-diagnostics-timeout", 5*time.Minute, "Maximum time to wait for Elaticsearch and Kibana diagnostics to complete")

	if err := cmd.MarkFlagRequired("resources-namespaces"); err != nil {
		exitWithError(err)
	}

	if err := cmd.Execute(); err != nil {
		// cobra logs the error already no need to redo that
		exitWithError(nil)
	}
}

func formatFilters(_ *cobra.Command, _ []string) error {
	var typ, name string
	if len(filters) == 0 {
		return nil
	}
	for _, filter := range filters {
		filterSlice := strings.Split(filter, "=")
		if len(filterSlice) != 2 {
			return fmt.Errorf("Invalid filter: %s", filter)
		}
		k, v := filterSlice[0], filterSlice[1]
		switch k {
		case "type":
			{
				if typ != "" {
					return fmt.Errorf("Only a single type filter is supported.")
				}
				typ = v
			}
		case "name":
			{
				if name != "" {
					return fmt.Errorf("Only a single name filter is supported.")
				}
				name = v
			}
		default:
			return fmt.Errorf("Invalid filter key: %s. Only 'type', and 'name' are supported.", k)
		}
	}
	if typ == "" {
		return fmt.Errorf("Invalid Filter: missing 'type'")
	}
	if name == "" {
		return fmt.Errorf("Invalid Filter: missing 'name'")
	}
	diagParams.LabelSelector = expandfilter(typ, name)
	return nil
}

func expandfilter(typ, name string) string {
	var elasticfilter string
	elasticfilter += elasticTypeKey + "=" + strings.ToLower(typ) + ","
	switch typ {
	case "elasticsearch":
		elasticfilter += fmt.Sprintf(elasticsearchNameFormat, strings.ToLower(typ)) + "=" + name
	default:
		elasticfilter += fmt.Sprintf(elasticNameFormat, strings.ToLower(typ)) + "=" + name
	}

	return elasticfilter
}

func exitWithError(err error) {
	if err != nil {
		log.Printf("Error: %v", err)
	}
	os.Exit(1)
}
