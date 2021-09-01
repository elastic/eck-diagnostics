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
	cmd.Flags().StringArrayVarP(&diagParams.OperatorNamespaces, "operator-namespaces", "o", []string{"elastic-system"}, "Namespace(s) in which operator(s) are running")
	cmd.Flags().StringArrayVarP(&diagParams.ResourcesNamespaces, "resources-namespaces", "r", []string{"default"}, "Namespace(s) in which resources are managed")
	cmd.Flags().StringVar(&diagParams.ECKVersion, "eck-version", "", "ECK version in use, will try to autodetect if not specified")
	cmd.Flags().StringVar(&diagParams.OutputDir, "output-directory", "", "Path where to output diagnostic results")
	cmd.Flags().StringVar(&diagParams.Kubeconfig, "kubeconfig", "", "optional path to kube config, defaults to $HOME/.kube/config")
	cmd.Flags().BoolVar(&diagParams.Verbose, "verbose", false, "Verbose mode")
	if err := cmd.Execute(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}
