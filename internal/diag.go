// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/elastic/eck-diagnostics/internal/archive"
	"k8s.io/apimachinery/pkg/util/version"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // auth on gke
)

var (
	logBuffer bytes.Buffer
	logger  = log.New(io.MultiWriter(os.Stdout, &logBuffer), "", log.LstdFlags)
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
	zipFile, err := archive.NewZipFile(zipFileName, logger)
	if err != nil {
		return err
	}

	zipFile.Add(map[string]func(io.Writer) error{
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

		zipFile.Add(getResources(kubectl, ns, []string{
			"statefulsets",
			"pods",
			"services",
			"configmaps",
			"events",
			"networkpolicies",
			"controllerrevisions",
		}))

		zipFile.Add(map[string]func(io.Writer) error{
			archive.Path(ns, "secrets.json"): func(writer io.Writer) error {
				return kubectl.GetMeta("secrets", ns, writer)
			},
		})

		if err := kubectl.Logs(ns, "", zipFile.Create); err != nil {
			zipFile.AddError(err)
		}
	}

	maxOperatorVersion := max(operatorVersions)
	logVersion(maxOperatorVersion)

	for _, ns := range params.ResourcesNamespaces {
		logger.Printf("Extracting Kubernetes diagnostics from %s\n", ns)
		zipFile.Add(getResources(kubectl, ns, []string{
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
			zipFile.Add(getResources(kubectl, ns, []string{
				"enterprisesearch",
				"beat",
			}))
		}

		if maxOperatorVersion.AtLeast(version.MustParseSemantic("1.4.0")) {
			zipFile.Add(getResources(kubectl, ns, []string{
				"agent",
			}))
		}

		if maxOperatorVersion.AtLeast(version.MustParseSemantic("1.6.0")) {
			zipFile.Add(getResources(kubectl, ns, []string{
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

		runStackDiagnostics(kubectl, ns, zipFile, params.Verbose, params.DiagnosticImage)
	}

	addDiagnosticLogToArchive(zipFile, &logBuffer)

	if err := zipFile.Close(); err != nil {
		// log the errors here and don't return them to the invoking command as we don't want usage help to be
		// printed in this case
		logger.Println(err.Error())
	}
	logger.Printf("ECK diagnostics written to %s\n", zipFileName)
	return nil
}

// addDiagnosticLogToArchive adds the passed bytes.Buffer reference as eck-diagnostics.log to the given archive.
// The underlying assumption being that the number of log lines produced by this tool is small enough to allow to be
// kept in memory.
func addDiagnosticLogToArchive(zipFile *archive.ZipFile, logContents *	bytes.Buffer)  {
	writer, err := zipFile.Create("eck-diagnostics.log")
	if err != nil {
		zipFile.AddError(err)
		return
	}
	_, err = writer.Write(logContents.Bytes())
	zipFile.AddError(err)
}

// getLogs extracts logs from all Pods that match the given selectors in the namespace ns and adds them to zipFile.
func getLogs(k *Kubectl, zipFile *archive.ZipFile, ns string, selector ...string) {
	for _, s := range selector {
		if err := k.Logs(ns, s, zipFile.Create); err != nil {
			zipFile.AddError(err)
		}
	}
}

// getResources produces a map of filenames to functions that will when invoked retrieve the resources identified by rs
// and add write them to a writer passed to said functions.
func getResources(k *Kubectl, ns string, rs []string) map[string]func(io.Writer) error {
	m := map[string]func(io.Writer) error{}
	for _, r := range rs {
		resource := r
		m[archive.Path(ns, resource+".json")] = func(w io.Writer) error {
			return k.Get(resource, ns, w)
		}
	}
	return m
}

// diagnosticFilename calculates a file name to be used for the diagnostic archive based on the current time.
func diagnosticFilename(dir string) string {
	file := fmt.Sprintf("eck-diagnostic-%s.zip", time.Now().Format("2006-01-02T15-04-05"))
	if dir != "" {
		file = filepath.Join(dir, file)
	}
	return file
}
