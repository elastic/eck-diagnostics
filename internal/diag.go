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

package internal

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/version"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // auth on gke
)

var (
	logger = log.New(os.Stdout, "", log.LstdFlags)
)

// Params is a collection of parameters controlling the extraction of diagnostic data.
// See the main command for explanation of individual parameters.
type Params struct {
	DiagnosticImage     string
	ECKVersion          string
	Kubeconfig          string
	OperatorNamespaces  []string
	ResourcesNamespaces []string
	OutputDir           string
	Verbose             bool
}

// AllNamespaces returns a slice containing all namespaces from which we want to extract diagnostic data.
func (dp Params) AllNamespaces() []string {
	nss := make([]string, 0, len(dp.ResourcesNamespaces)+len(dp.OperatorNamespaces))
	nss = append(nss, dp.ResourcesNamespaces...)
	nss = append(nss, dp.OperatorNamespaces...)
	return nss
}

// Run extracts diagnostic information based on the given params.
// It produces a zip file with the contents as a side effect.
func Run(params Params) error {
	logger.Printf("ECK diagnostics with parameters: %+v", params)
	kubectl, err := NewKubectl(params.Kubeconfig)
	if err != nil {
		return err
	}

	if err := kubectl.CheckNamespaces(context.Background(), params.AllNamespaces()); err != nil {
		return err
	}

	clientSet, err := kubectl.factory.KubernetesClientSet()
	if err != nil {
		return err
	}

	zipFileName := diagnosticFilename(params.OutputDir)
	zipFile, err := NewZipFile(zipFileName)
	if err != nil {
		return err
	}

	zipFile.add(map[string]func(io.Writer) error{
		"version.json": func(writer io.Writer) error {
			return kubectl.Version(writer)
		},
		"nodes.json": func(writer io.Writer) error {
			return kubectl.Get("nodes", "", writer)
		},
		"podsecuritypolicies.json": func(writer io.Writer) error {
			return kubectl.Get("podsecuritypolicies", "", writer)
		},
		"clusterroles.txt": func(writer io.Writer) error {
			return kubectl.Describe("clusterroles", "elastic", "", writer)
		},
	})

	operatorVersions := make([]*version.Version, 0, len(params.OperatorNamespaces))

	for _, ns := range params.OperatorNamespaces {
		logger.Printf("Extracting Kubernetes diagnostics from %s\n", ns)

		operatorVersions = append(operatorVersions, detectECKVersion(clientSet, ns, params.ECKVersion))

		zipFile.add(getResources(kubectl, ns, []string{
			"statefulsets",
			"pods",
			"services",
			"configmaps",
			"events",
			"networkpolicies",
			"controllerrevisions",
		}))

		zipFile.add(map[string]func(io.Writer) error{
			filepath.Join(ns, "secrets.json"): func(writer io.Writer) error {
				return kubectl.GetMeta("secrets", ns, writer)
			},
		})

		if err := kubectl.Logs(ns, "", zipFile.Create); err != nil {
			zipFile.addError(err)
		}
	}

	maxOperatorVersion := max(operatorVersions)
	logVersion(maxOperatorVersion)

	for _, ns := range params.ResourcesNamespaces {
		logger.Printf("Extracting Kubernetes diagnostics from %s\n", ns)
		zipFile.add(getResources(kubectl, ns, []string{
			"statefulsets",
			"replicasets",
			"deployments",
			"daemonsets",
			"pods",
			"persistentvolumes",
			"persistentvolumeclaims",
			"services",
			"endpoints",
			"configmaps",
			"events",
			"networkpolicies",
			"controllerrevisions",
			"kibana",
			"elasticsearch",
			"apmserver",
		}))

		if maxOperatorVersion.AtLeast(version.MustParseSemantic("1.2.0")) {
			zipFile.add(getResources(kubectl, ns, []string{
				"enterprisesearch",
				"beat",
			}))
		}

		if maxOperatorVersion.AtLeast(version.MustParseSemantic("1.4.0")) {
			zipFile.add(getResources(kubectl, ns, []string{
				"agent",
			}))
		}

		if maxOperatorVersion.AtLeast(version.MustParseSemantic("1.6.0")) {
			zipFile.add(getResources(kubectl, ns, []string{
				"elasticmapsserver",
			}))
		}

		getLogs(kubectl, zipFile, ns,
			"common.k8s.elastic.co/type=elasticsearch",
			"common.k8s.elastic.co/type=kibana",
			"common.k8s.elastic.co/type=apm-server",
			// the below where introduced in later version but label selector will just return no result:
			"common.k8s.elastic.co/type=enterprise-search", // 1.2.0
			"common.k8s.elastic.co/type=beat",              // 1.2.0
			"common.k8s.elastic.co/type=agent",             // 1.4.0
			"common.k8s.elastic.co/type=maps",              // 1.6.0
		)

		runElasticsearchDiagnostics(kubectl, ns, zipFile, params.Verbose, params.DiagnosticImage)
	}

	if err := zipFile.Close(); err != nil {
		// log the errors here and don't return them to the invoking command as we don't want usage help to be
		// printed in this case
		logger.Println(err.Error())
	}
	logger.Printf("ECK diagnostics written to %s\n", zipFileName)
	return nil
}

// getLogs extracts logs from all Pods that match the given selectors in the namespace ns and adds them to zipFile.
func getLogs(k *Kubectl, zipFile *ZipFile, ns string, selector ...string) {
	for _, s := range selector {
		if err := k.Logs(ns, s, zipFile.Create); err != nil {
			zipFile.addError(err)
		}
	}
}

// getResources produces a map of filenames to functions that will when invoked retrieve the resources identified by rs
// and add write them to a writer passed to said functions.
func getResources(k *Kubectl, ns string, rs []string) map[string]func(io.Writer) error {
	m := map[string]func(io.Writer) error{}
	for _, r := range rs {
		resource := r
		m[filepath.Join(ns, resource+".json")] = func(w io.Writer) error {
			return k.Get(resource, ns, w)
		}
	}
	return m
}

// ZipFile wraps a zip.Writer to add a few convenience functions and implement resource closing.
type ZipFile struct {
	*zip.Writer
	underlying io.Closer
	errs       []error
}

// NewZipFile creates a new zip file named fileName.
func NewZipFile(fileName string) (*ZipFile, error) {
	f, err := os.Create(fileName)
	if err != nil {
		return nil, err
	}
	w := zip.NewWriter(f)
	return &ZipFile{
		Writer:     w,
		underlying: f,
	}, nil
}

// Close closes the zip.Writer and the underlying file.
func (z *ZipFile) Close() error {
	errs := []error{z.writeErrorsToFile(), z.Writer.Close(), z.underlying.Close()}
	return errors.NewAggregate(errs)
}

// add takes a map of file names and functions to evaluate with the intent to add the result of the evaluation to the
// zip file at the name used as key in the map.
func (z *ZipFile) add(fns map[string]func(io.Writer) error) {
	for k, f := range fns {
		fw, err := z.Create(k)
		if err != nil {
			z.errs = append(z.errs, err)
			return
		}
		z.errs = append(z.errs, f(fw))
	}
}

// addError records an error to be persistent in the ZipFile.
func (z *ZipFile) addError(err error) {
	if err == nil {
		return
	}
	// log errors immediately to give user early feedback
	logger.Printf(err.Error())
	z.errs = append(z.errs, err)
}

// writeErrorsToFile writes the accumulated errors to a file inside the ZipFile.
func (z *ZipFile) writeErrorsToFile() error {
	aggregate := errors.NewAggregate(z.errs)
	if aggregate == nil {
		return nil
	}
	out, err := z.Create("eck-diagnostic-errors.txt")
	if err != nil {
		return err
	}
	errorString := aggregate.Error()
	// errors have been logged already just include in zip archive to inform support
	_, err = out.Write([]byte(errorString))
	return err
}

// diagnosticFilename calculates a file name to be used for the diagnostic archive based on the current time.
func diagnosticFilename(dir string) string {
	file := fmt.Sprintf("eck-diagnostic-%s.zip", time.Now().Format("2006-01-02T15-04-05"))
	if dir != "" {
		file = filepath.Join(dir, file)
	}
	return file
}
