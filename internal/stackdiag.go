// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/ghodss/yaml"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/utils/pointer"
)

const (
	DiagnosticImage = "docker.elastic.co/eck-dev/support-diagnostics:8.1.4"

	podOutputDir         = "/diagnostic-output"
	podMainContainerName = "offer-output"
)

var (
	//go:embed job.tpl.yml
	jobTemplate string
	// jobTimeout governs how long at most diagnostic data extraction may take.
	jobTimeout = 10 * time.Minute
	// jobPollingInterval is used to configure the informer used to be notified of Pod status changes.
	jobPollingInterval = 10 * time.Second
)

// diagJob represents a pod whose job it is to extract diagnostic data from an Elasticsearch cluster.
type diagJob struct {
	podName string
	esName  string
	done    bool
}

// diagJobState captures the state of running a set of job to extract diagnostics from Elasticsearch.
type diagJobState struct {
	ns              string
	clientSet       *kubernetes.Clientset
	config          *rest.Config
	informer        cache.SharedInformer
	jobs            map[string]*diagJob
	context         context.Context
	cancelFunc      context.CancelFunc
	verbose         bool
	diagnosticImage string
}

// newDiagJobState creates a new state struct to run diagnostic Pods.
func newDiagJobState(clientSet *kubernetes.Clientset, config *rest.Config, ns string, verbose bool, image string) *diagJobState {
	ctx, cancelFunc := context.WithTimeout(context.Background(), jobTimeout)
	factory := informers.NewSharedInformerFactoryWithOptions(
		clientSet,
		jobPollingInterval,
		informers.WithNamespace(ns),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = "app.kubernetes.io/name=eck-diagnostics"
		}))
	return &diagJobState{
		jobs:            map[string]*diagJob{},
		ns:              ns,
		clientSet:       clientSet,
		config:          config,
		informer:        factory.Core().V1().Pods().Informer(),
		cancelFunc:      cancelFunc,
		context:         ctx,
		verbose:         verbose,
		diagnosticImage: image,
	}
}

// scheduleJob creates a Pod to extract diagnostic data from an Elasticsearch cluster esName.
func (ds *diagJobState) scheduleJob(esName string, tls bool) error {
	podName := fmt.Sprintf("%s-diag", esName)
	tpl, err := template.New("job").Parse(jobTemplate)
	if err != nil {
		return err
	}

	buffer := new(bytes.Buffer)
	err = tpl.Execute(buffer, map[string]interface{}{
		"PodName":           podName,
		"DiagnosticImage":   ds.diagnosticImage,
		"ESNamespace":       ds.ns,
		"ESName":            esName,
		"TLS":               tls,
		"OutputDir":         podOutputDir,
		"MainContainerName": podMainContainerName,
	})
	if err != nil {
		return err
	}

	var pod corev1.Pod
	err = yaml.Unmarshal(buffer.Bytes(), &pod)
	if err != nil {
		return err
	}

	err = ds.clientSet.CoreV1().Pods(ds.ns).Delete(context.Background(), podName, metav1.DeleteOptions{GracePeriodSeconds: pointer.Int64Ptr(0)})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	_, err = ds.clientSet.CoreV1().Pods(ds.ns).Create(context.Background(), &pod, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	ds.jobs[podName] = &diagJob{
		podName: podName,
		esName:  esName,
	}
	return nil
}

// extractJobResults runs an informer to be notified of Pod status changes and extract diagnostic data from any Pod
// that has reached running state.
func (ds *diagJobState) extractJobResults(file *ZipFile) {
	ds.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if pod, ok := obj.(*corev1.Pod); ok && ds.verbose {
				logger.Printf("Diagnostic pod %s/%s added\n", pod.Namespace, pod.Name)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pod, ok := newObj.(*corev1.Pod)
			if !ok {
				logger.Printf("Unexpected %v, expected type Pod\n", newObj)
				return
			}
			job, found := ds.jobs[pod.Name]
			if !found {
				logger.Printf("Unexpected no record for Pod %s/%s\n", pod.Namespace, pod.Name)
				return
			}

			if job.done {
				return
			}

			switch pod.Status.Phase {
			case corev1.PodPending:
				// ignore
			case corev1.PodUnknown:
				logger.Printf("Unexpected diagnostic Pod %s/%s in unknown phase", pod.Namespace, pod.Name)
			case corev1.PodRunning:
				// extract logs
				execErrOut := io.Discard
				if ds.verbose {
					execErrOut = os.Stderr
				}
				reader, outStream := io.Pipe()
				options := &exec.ExecOptions{
					StreamOptions: exec.StreamOptions{
						IOStreams: genericclioptions.IOStreams{
							In:     nil,
							Out:    outStream,
							ErrOut: execErrOut,
						},

						Namespace:     pod.Namespace,
						PodName:       pod.Name,
						ContainerName: podMainContainerName,
					},
					Config:    ds.config,
					PodClient: ds.clientSet.CoreV1(),
					Command:   []string{"tar", "cf", "-", podOutputDir},
					Executor:  &exec.DefaultRemoteExecutor{},
				}
				go func() {
					defer outStream.Close()
					err := options.Run()
					if err != nil {
						file.addError(err)
						return
					}
				}()
				err := ds.untarIntoZip(reader, job.esName, file)
				if err != nil {
					file.addError(err)
					return
				}
				err = ds.completeJob(job)
				if err != nil {
					file.addError(err)
					return
				}
			case corev1.PodSucceeded:
				file.addError(fmt.Errorf("unexpected: Pod %s/%s succeeded", pod.Namespace, pod.Name))
				file.addError(ds.completeJob(job))
			case corev1.PodFailed:
				file.addError(fmt.Errorf("unexpected: Pod %s/%s failed", pod.Namespace, pod.Name))
				file.addError(ds.completeJob(job))
			}
		},
		DeleteFunc: func(obj interface{}) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				logger.Printf("Unexpected %v, expected type Pod", obj)
				return
			}

			if ds.verbose {
				logger.Printf("%s/%s deleted", pod.Namespace, pod.Name)
			}

			done := true
			for _, j := range ds.jobs {
				if !j.done {
					done = false
				}
			}
			if done {
				ds.cancelFunc()
			}
		},
	})
	ds.informer.Run(ds.context.Done())
}

// untarIntoZip extracts the files transferred via tar from the Pod into the given ZipFile.
func (ds *diagJobState) untarIntoZip(reader *io.PipeReader, esName string, file *ZipFile) error {
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			break
		}
		remoteFilename := header.Name
		// remove the path prefix on the Pod
		relOutputDir := fmt.Sprintf("%s/", strings.TrimPrefix(podOutputDir, "/"))
		relativeFilename := strings.TrimPrefix(remoteFilename, relOutputDir)
		// stack diagnostics create output in a directory called api-diagnostics-{{.Timestamp}}
		if !strings.HasPrefix(relativeFilename, "api-diagnostics") {
			if ds.verbose {
				logger.Printf("Ignoring file %s in tar from %s diagnostics\n", header.Name, esName)
			}
			continue
		}
		switch {
		case strings.HasSuffix(relativeFilename, "tar.gz"):
			if err := ds.repackageTarGzip(tarReader, esName, file); err != nil {
				return err
			}
		case strings.HasSuffix(relativeFilename, ".zip"):
			if err := ds.repackageZip(tarReader, esName, file); err != nil {
				return err
			}
		default:
			out, err := file.Create(filepath.Join(ds.ns, "elasticsearch", esName, relativeFilename))
			if err != nil {
				return err
			}
			// accept decompression bomb for CLI as we control the src
			if _, err := io.Copy(out, tarReader); err != nil { //nolint:gosec
				return err
			}
		}
	}
	return nil
}

// completeJob marks the given job as done and deletes the corresponding Pod.
func (ds *diagJobState) completeJob(job *diagJob) error {
	logger.Printf("Elasticsearch diagnostics extracted for cluster %s/%s\n", ds.ns, job.esName)
	job.done = true
	return ds.clientSet.CoreV1().Pods(ds.ns).Delete(ds.context, job.podName, metav1.DeleteOptions{GracePeriodSeconds: pointer.Int64Ptr(0)})
}

// repackageTarGzip repackages the *.tar.gz archives produced by the Elasticsearch diagnostic tool into the given ZipFile.
func (ds *diagJobState) repackageTarGzip(in io.Reader, esName string, zipFile *ZipFile) error {
	gzReader, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	topLevelDir := ""
	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			break
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if topLevelDir == "" {
				topLevelDir = header.Name
			}
			continue
		case tar.TypeReg:
			newPath, err := ds.asECKDiagPath(header.Name, topLevelDir, esName)
			if err != nil {
				return err
			}
			out, err := zipFile.Create(newPath)
			if err != nil {
				return err
			}
			// accept decompression bomb for CLI tool and we control the src
			_, err = io.Copy(out, tarReader) //nolint:gosec
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (ds *diagJobState) repackageZip(in io.Reader, esName string, zipFile *ZipFile) error {
	// it seems the only way to repack a zip archive is to completely read it into memory first
	b := new(bytes.Buffer)
	if _, err := b.ReadFrom(in); err != nil {
		return err
	}

	zipReader, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		return err
	}
	// api-diagnostics creates a common top folder we don't need when repackaging
	topLevelDir := ""
	for _, f := range zipReader.File {
		// skip all the directory entries
		if f.UncompressedSize64 == 0 {
			continue
		}
		// extract the tld first time round
		if topLevelDir == "" {
			topLevelDir = rootDir(f.Name)
		}
		newPath, err := ds.asECKDiagPath(f.Name, topLevelDir, esName)
		if err != nil {
			return err
		}
		out, err := zipFile.Create(newPath)
		if err != nil {
			return err
		}
		if err := copyFromZip(f, out); err != nil {
			return err
		}
	}
	return nil
}

func copyFromZip(f *zip.File, out io.Writer) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	if _, err := io.Copy(out, rc); err != nil { //nolint:gosec
		return err
	}
	return nil
}

func rootDir(name string) string {
	if len(name) == 0 {
		return name
	}
	i := 1
	for i < len(name) && name[i] != os.PathSeparator {
		i++
	}
	return name[0:i]
}

func (ds *diagJobState) asECKDiagPath(original, topLevelDir, esName string) (string, error) {
	rel, err := filepath.Rel(topLevelDir, original)
	if err != nil {
		return "", err
	}
	return filepath.Join(ds.ns, "elasticsearch", esName, rel), nil
}

// runElasticsearchDiagnostics extracts diagnostic data from all clusters in the given namespace ns using the official
// Elasticsearch support diagnostics.
func runElasticsearchDiagnostics(k *Kubectl, ns string, zipFile *ZipFile, verbose bool, image string) {
	config, err := k.factory.ToRESTConfig()
	if err != nil {
		zipFile.addError(err)
		return // not recoverable let's stop here
	}
	clientSet, err := k.factory.KubernetesClientSet()
	if err != nil {
		zipFile.addError(err)
		return // not recoverable
	}
	state := newDiagJobState(clientSet, config, ns, verbose, image)

	resources, err := k.getResources("elasticsearch", ns)
	if err != nil {
		zipFile.addError(err)
		return // not recoverable
	}
	if err := resources.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			// record error but continue trying for other resources
			zipFile.addError(err)
		}

		esName := info.Name
		es, err := runtime.DefaultUnstructuredConverter.ToUnstructured(info.Object)
		if err != nil {
			zipFile.addError(err)
			return nil
		}
		disabled, found, err := unstructured.NestedBool(es, "spec", "http", "tls", "selfSignedCertificate", "disabled")
		if err != nil {
			zipFile.addError(err)
			return nil
		}
		tls := !(found && disabled)

		zipFile.addError(state.scheduleJob(esName, tls))
		return nil
	}); err != nil {
		zipFile.addError(err)
	}
	// don't start extracting if there is nothing to do
	if len(state.jobs) == 0 {
		return
	}
	state.extractJobResults(zipFile)
}
