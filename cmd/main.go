// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package main

import (
	"log"
	"os"

	"github.com/elastic/eck-diagnostics/internal"
	"github.com/spf13/cobra"
)

var (
	diagParams = internal.Params{}
)

func main() {
	cmd := &cobra.Command{
		Use:   "eck-diagnostics",
		Short: "ECK support diagnostics tool",
		Long:  "Dump ECK and Kubernetes data for support and troubleshooting purposes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return internal.Run(diagParams)
		},
	}
	cmd.Flags().StringVar(&diagParams.DiagnosticImage, "diagnostic-image", internal.DiagnosticImage, "Diagnostic image to be used for stack diagnostics, see run-stack-diagnostics")
	cmd.Flags().BoolVar(&diagParams.RunStackDiagnostics, "run-stack-diagnostics", true, "Run diagnostics on deployed Elasticsearch clusters and Kibana instances, requires deploying diagnostic Pods into the cluster")
	cmd.Flags().BoolVar(&diagParams.RunAgentDiagnostics, "run-agent-diagnostics", false, "Run diagnostics on deployed Elastic Agents. Warning: credentials will not be redacted and appear as plain text in the archive")
	cmd.Flags().StringSliceVarP(&diagParams.OperatorNamespaces, "operator-namespaces", "o", []string{"elastic-system"}, "Comma-separated list of namespace(s) in which operator(s) are running")
	cmd.Flags().StringSliceVarP(&diagParams.ResourcesNamespaces, "resources-namespaces", "r", nil, "Comma-separated list of namespace(s) in which resources are managed")
	cmd.Flags().StringVar(&diagParams.ECKVersion, "eck-version", "", "ECK version in use, will try to autodetect if not specified")
	cmd.Flags().StringVar(&diagParams.OutputDir, "output-directory", "", "Path where to output diagnostic results")
	cmd.Flags().StringVar(&diagParams.Kubeconfig, "kubeconfig", "", "optional path to kube config, defaults to $HOME/.kube/config")
	cmd.Flags().BoolVar(&diagParams.Verbose, "verbose", false, "Verbose mode")

	if err := cmd.MarkFlagRequired("resources-namespaces"); err != nil {
		exitWithError(err)
	}

	if err := cmd.Execute(); err != nil {
		// cobra logs the error already no need to redo that
		exitWithError(nil)
	}
}

func exitWithError(err error) {
	if err != nil {
		log.Printf("Error: %v", err)
	}
	os.Exit(1)
}
