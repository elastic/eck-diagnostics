// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

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
	cmd.Flags().StringVar(&diagParams.DiagnosticImage, "diagnostic-image", internal.DiagnosticImage, "diagnostic image to be used")
	cmd.Flags().StringSliceVarP(&diagParams.OperatorNamespaces, "operator-namespaces", "o", []string{"elastic-system"}, "Comma-separated list of namespace(s) in which operator(s) are running")
	cmd.Flags().StringSliceVarP(&diagParams.ResourcesNamespaces, "resources-namespaces", "r", []string{"default"}, "Comma-separated list of namespace(s) in which resources are managed")
	cmd.Flags().StringVar(&diagParams.ECKVersion, "eck-version", "", "ECK version in use, will try to autodetect if not specified")
	cmd.Flags().StringVar(&diagParams.OutputDir, "output-directory", "", "Path where to output diagnostic results")
	cmd.Flags().StringVar(&diagParams.Kubeconfig, "kubeconfig", "", "optional path to kube config, defaults to $HOME/.kube/config")
	cmd.Flags().BoolVar(&diagParams.Verbose, "verbose", false, "Verbose mode")
	if err := cmd.Execute(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}
