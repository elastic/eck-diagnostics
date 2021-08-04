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

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // auth on gke
)

var (
	logger = log.New(os.Stdout, "", log.LstdFlags)
)

type DumpParams struct {
	Kubeconfig          string
	OperatorNamespaces  []string
	ResourcesNamespaces []string
	OutputDir           string
	Verbose             bool
}

func (dp DumpParams) AllNamespaces() []string {
	nss := make([]string, 0, len(dp.ResourcesNamespaces)+len(dp.OperatorNamespaces))
	nss = append(nss, dp.ResourcesNamespaces...)
	nss = append(nss, dp.OperatorNamespaces...)
	return nss
}

func RunDump(params DumpParams) error {
	logger.Printf("ECK diagnostics with %+v", params)
	kubectl, err := NewKubectl(params.Kubeconfig)
	if err != nil {
		return err
	}

	if err := kubectl.CheckNamespaces(context.Background(), params.AllNamespaces()); err != nil {
		return err
	}

	zipFileName := diagnosticFilename(params.OutputDir)
	zipFile, err := NewZipFile(zipFileName)
	if err != nil {
		return err
	}

	if err := zipFile.add(map[string]func(io.Writer) error{
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
	}); err != nil {
		return err
	}

	for _, ns := range params.OperatorNamespaces {
		logger.Printf("Extracting Kubernetes diagnostics from %s\n", ns)
		if err := zipFile.add(getResources(kubectl, ns, []string{
			"statefulsets",
			"pods",
			"services",
			"configmaps",
			"events",
			"networkpolicies",
			"controllerrevisions",
		})); err != nil {
			return err
		}

		if err := zipFile.add(map[string]func(io.Writer) error{
			filepath.Join(ns, "secrets.json"): func(writer io.Writer) error {
				return kubectl.GetMeta("secrets", ns, writer)
			},
		}); err != nil {
			return err
		}

		if err := kubectl.Logs(ns, "", zipFile.Create); err != nil {
			return err
		}
	}

	for _, ns := range params.ResourcesNamespaces {
		logger.Printf("Extracting Kubernetes diagnostics from %s\n", ns)
		if err := zipFile.add(getResources(kubectl, ns, []string{
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
			"enterprisesearch",
			"beat",
			"agent",
			"elasticmapsserver",
		})); err != nil {
			return err
		}

		if err := getLogs(kubectl, zipFile, ns,
			"common.k8s.elastic.co/type=elasticsearch",
			"common.k8s.elastic.co/type=kibana",
			"common.k8s.elastic.co/type=apm-server",
			"common.k8s.elastic.co/type=enterprise-search",
			"common.k8s.elastic.co/type=beat",
			"common.k8s.elastic.co/type=agent",
			"common.k8s.elastic.co/type=maps",
		); err != nil {
			return err
		}

		if err := runElasticsearchDiagnostics(kubectl, ns, zipFile, params.Verbose); err != nil {
			return err
		}
	}
	defer func() {
		logger.Printf("ECK diagnostics written to %s\n", zipFileName)
	}()
	return zipFile.Close()
}

func getLogs(k *Kubectl, zipFile *ZipFile, ns string, selector ...string) error {
	for _, s := range selector {
		if err := k.Logs(ns, s, zipFile.Create); err != nil {
			return err
		}
	}
	return nil
}

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

type ZipFile struct {
	*zip.Writer
	underlying io.Closer
}

func NewZipFile(path string) (*ZipFile, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := zip.NewWriter(f)
	return &ZipFile{
		Writer:     w,
		underlying: f,
	}, nil
}

func (z ZipFile) Close() error {
	// TODO aggregate error?
	defer z.underlying.Close()
	return z.Writer.Close()
}

func (z ZipFile) add(fns map[string]func(io.Writer) error) error {
	for k, f := range fns {
		fw, err := z.Create(k)
		if err != nil {
			return err
		}
		if err := f(fw); err != nil {
			return err
		}
	}
	return nil
}

func diagnosticFilename(dir string) string {
	file := fmt.Sprintf("eck-diagnostic-%s.zip", time.Now().Format("2006-01-02T15-04-05"))
	if dir != "" {
		file = filepath.Join(dir, file)
	}
	return file
}
