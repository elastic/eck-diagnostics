// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/elastic/eck-diagnostics/internal/archive"
	"github.com/elastic/eck-diagnostics/internal/filters"
	"github.com/elastic/eck-diagnostics/internal/log"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/version"

	_ "k8s.io/client-go/plugin/pkg/client/auth/azure" // auth on azure (deprecated)
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"   // auth on gke (deprecated)
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"  // auth flow for OpenID Connect (deprecated)
)

var logger = log.Logger

// Params is a collection of parameters controlling the extraction of diagnostic data.
// See the main command for explanation of individual parameters.
type Params struct {
	DiagnosticImage         string
	ECKVersion              string
	Kubeconfig              string
	OperatorNamespaces      []string
	ResourcesNamespaces     []string
	OutputDir               string
	OutputName              string
	RunStackDiagnostics     bool
	RunAgentDiagnostics     bool
	Verbose                 bool
	StackDiagnosticsTimeout time.Duration
	Filters                 filters.Filters
	ESUsername              string
	ESPassword              string
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
	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		s := <-sigCh
		logger.Printf("Aborting: %v received", s)
		close(stopCh)
	}()

	kubectl, err := NewKubectl(params.Kubeconfig, params.Verbose)
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

	zipFileName := filepath.Join(params.OutputDir, params.OutputName)
	zipFile, err := archive.NewZipFile(zipFileName, about().Version, logger)
	if err != nil {
		return err
	}

	// Filters is intentionally empty in many of these, as Elastic labels
	// are not applied to these resources.
	zipFile.Add(map[string]func(io.Writer) error{
		"version.json": func(writer io.Writer) error {
			return kubectl.Version(writer)
		},
		"nodes.json": func(writer io.Writer) error {
			return kubectl.GetByLabel("nodes", "", filters.Empty, writer)
		},
		"podsecuritypolicies.json": func(writer io.Writer) error {
			return kubectl.GetByLabel("podsecuritypolicies", "", filters.Empty, writer)
		},
		"storageclasses.json": func(writer io.Writer) error {
			return kubectl.GetByLabel("storageclasses", "", filters.Empty, writer)
		},
		"clusterroles.txt": func(writer io.Writer) error {
			return kubectl.Describe("clusterroles", "elastic", "", writer)
		},
		"clusterrolebindings.txt": func(writer io.Writer) error {
			return kubectl.Describe("clusterrolebindings", "elastic", "", writer)
		},
	})

	var operatorLabels []labels.Set
	operatorVersions := make([]*version.Version, 0, len(params.OperatorNamespaces))
	for _, ns := range params.OperatorNamespaces {
		// Find the label in use by operator in this namespace and add this to
		// the set of filters to ensure we always retrieve the objects
		// associated with the ECK operator.
		operatorLabel, err := getOperatorLabel(kubectl, ns)
		if err != nil || operatorLabel == nil {
			logger.Printf("Could not find label corresponding to ECK Operator in namespace %s: potentially not including operator data in diagnostics", ns)
		} else {
			operatorLabels = append(operatorLabels, operatorLabel)
		}

		operatorVersions = append(operatorVersions, detectECKVersion(clientSet, ns, params.ECKVersion))
	}

	maxOperatorVersion := max(operatorVersions)
	logVersion(maxOperatorVersion)

	allNamespaces := sets.New(params.ResourcesNamespaces...)
	allNamespaces.Insert(params.OperatorNamespaces...)

	logsLabels := []string{
		"common.k8s.elastic.co/type=elasticsearch",
		"common.k8s.elastic.co/type=kibana",
		"common.k8s.elastic.co/type=apm-server",
		// the below were introduced in later version but label selector will just return no result:
		"common.k8s.elastic.co/type=enterprise-search", // 1.2.0
		"common.k8s.elastic.co/type=beat",              // 1.2.0
		"common.k8s.elastic.co/type=agent",             // 1.4.0
		"common.k8s.elastic.co/type=maps",              // 1.6.0
		"common.k8s.elastic.co/type=logstash",          // 2.8.0
	}

	selectors := make([]labels.Selector, len(operatorLabels))
	for i, label := range operatorLabels {
		selectors[i] = label.AsSelector()
		logsLabels = append(logsLabels, label.AsSelector().String())
	}
	namespaceFilters := params.Filters.WithSelectors(selectors)

LOOP:
	for ns := range allNamespaces {
		select {
		case <-stopCh:
			break LOOP
		default:
		}

		logger.Printf("Extracting Kubernetes diagnostics from %s\n", ns)
		zipFile.Add(getResources(kubectl.GetByLabel, ns, namespaceFilters, []string{
			"statefulsets",
			"replicasets",
			"deployments",
			"daemonsets",
			"pods",
			"persistentvolumeclaims",
			"services",
			"endpoints",
			"configmaps",
			"controllerrevisions",
		}))

		zipFile.Add(getResources(kubectl.GetByName, ns, namespaceFilters, []string{
			"kibana",
			"elasticsearch",
			"apmserver",
		}))

		// Filters is intentionally empty here, as Elastic labels
		// are not applied to these resources.
		zipFile.Add(getResources(kubectl.GetByLabel, ns, filters.Empty, []string{
			"persistentvolumes",
			"events",
			"networkpolicies",
			"serviceaccount",
		}))

		if maxOperatorVersion.AtLeast(version.MustParseSemantic("1.2.0")) {
			zipFile.Add(getResources(kubectl.GetByName, ns, namespaceFilters, []string{
				"enterprisesearch",
				"beat",
			}))
		}

		if maxOperatorVersion.AtLeast(version.MustParseSemantic("1.4.0")) {
			zipFile.Add(getResources(kubectl.GetByName, ns, namespaceFilters, []string{
				"agent",
			}))
		}

		if maxOperatorVersion.AtLeast(version.MustParseSemantic("1.6.0")) {
			zipFile.Add(getResources(kubectl.GetByName, ns, namespaceFilters, []string{
				"elasticmapsserver",
			}))
		}
		if maxOperatorVersion.AtLeast(logstashMinVersion) {
			zipFile.Add(getResources(kubectl.GetByName, ns, namespaceFilters, []string{
				"logstash",
			}))
		}

		zipFile.Add(map[string]func(io.Writer) error{
			archive.Path(ns, "secrets.json"): func(writer io.Writer) error {
				return kubectl.GetMeta("secrets", ns, writer)
			},
		})

		getLogs(kubectl, zipFile, ns, namespaceFilters, logsLabels...)

		if params.RunStackDiagnostics {
			runStackDiagnostics(
				kubectl,
				ns,
				zipFile,
				params.Verbose,
				params.DiagnosticImage,
				params.StackDiagnosticsTimeout,
				stopCh,
				namespaceFilters,
				maxOperatorVersion,
				params.ESUsername,
				params.ESPassword,
			)
		}

		if params.RunAgentDiagnostics {
			runAgentDiagnostics(kubectl, ns, zipFile, params.Verbose, stopCh, namespaceFilters)
		}
	}

	addDiagnosticLogToArchive(zipFile, &log.Buffer)

	if err := zipFile.Close(); err != nil {
		// log the errors here and don't return them to the invoking command as we don't want usage help to be
		// printed in this case
		logger.Println(err.Error())
	}
	logger.Printf("ECK diagnostics written to %s\n", zipFileName)
	return nil
}

// getOperatorLabel will attempt to find the labels associated with the ECK Operator, returning any errors.
//
// 1) if using yaml manifests or OLM it will always be "control-plane=elastic-operator"
// 2) if using Helm, find label with key: helm.sh/chart, and value containing "eck-operator-*"
func getOperatorLabel(kc *Kubectl, ns string) (labels.Set, error) {
	// try the most common case first pure YAML and OLM installations will have this label
	pods, err := kc.Clientset.CoreV1().Pods(ns).List(context.Background(), v1.ListOptions{LabelSelector: "control-plane=elastic-operator"})
	if err == nil && len(pods.Items) > 0 {
		return labels.Set{"control-plane": "elastic-operator"}, nil
	}
	// for Helm use the service account to be independent of Deployment or StatefulSet (even though all Helm installs should use a StatefulSet)
	saList, err := kc.Clientset.CoreV1().ServiceAccounts(ns).List(context.Background(), v1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("while retrieving list of serviceaccounts in ns '%s': %w", ns, err)
	}
	for _, sa := range saList.Items {
		for k, v := range sa.Labels {
			if k == "helm.sh/chart" && strings.Contains(v, "eck-operator") {
				// The helm.sh/chart=eck-operator-* label isn't propagated down to sub-resources
				// so use the app.kubernetes.io/name label, which is propagated.
				return labels.Set{"app.kubernetes.io/name": sa.Labels["app.kubernetes.io/name"]}, nil
			}
		}
	}
	return nil, fmt.Errorf("unable to find any resources belonging to the eck operator")
}

// addDiagnosticLogToArchive adds the passed bytes.Buffer reference as eck-diagnostics.log to the given archive.
// The underlying assumption being that the number of log lines produced by this tool is small enough to allow them to
// be kept in memory.
func addDiagnosticLogToArchive(zipFile *archive.ZipFile, logContents *bytes.Buffer) {
	writer, err := zipFile.Create("eck-diagnostics.log")
	if err != nil {
		zipFile.AddError(err)
		return
	}
	_, err = writer.Write(logContents.Bytes())
	zipFile.AddError(err)
}

// getLogs extracts logs from all Pods that match the given selectors in the namespace ns and adds them to zipFile.
func getLogs(k *Kubectl, zipFile *archive.ZipFile, ns string, filters filters.Filters, selector ...string) {
	for _, s := range selector {
		if err := k.Logs(ns, s, filters, zipFile.Create); err != nil {
			zipFile.AddError(err)
		}
	}
}

// getResources produces a map of filenames to functions that will when invoked retrieve the resources identified by rs
// and add write them to a writer passed to said functions.
func getResources(f func(string, string, filters.Filters, io.Writer) error, ns string, filters filters.Filters, rs []string) map[string]func(io.Writer) error {
	m := map[string]func(io.Writer) error{}
	for _, r := range rs {
		resource := r
		m[archive.Path(ns, resource+".json")] = func(w io.Writer) error {
			return f(resource, ns, filters, w)
		}
	}
	return m
}
