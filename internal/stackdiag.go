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
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
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
	jobTimeout = 5 * time.Minute
	// jobPollingInterval is used to configure the informer used to be notified of Pod status changes.
	jobPollingInterval = 10 * time.Second
)

// diagJob represents a pod whose job it is to extract diagnostic data from an Elasticsearch cluster.
type diagJob struct {
	podName      string
	namespace    string // de-normalized for convenience
	typ          string
	resourceName string
	done         bool
}

// sourceDirPrefix the directory prefix the stack support-diagnostics tool uses in the archive it creates.
func (j diagJob) sourceDirPrefix() string {
	prefix := "api-diagnostics"
	if j.typ == "kibana" {
		prefix = fmt.Sprintf("%s-%s", j.typ, prefix)
	}
	return prefix
}

// outputDirPrefix the directory hierarchy we want to use in the archive created by this tool. It should be the namespace
// of the resource we are creating diagnostics for followed by the type (elasticserach or kibana currently) and the name
// of the resource.
func (j diagJob) outputDirPrefix() string {
	return filepath.Join(j.namespace, j.typ, j.resourceName)
}

// diagJobState captures the state of running a set of job to extract diagnostics from Elastic Stack applications.
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
func newDiagJobState(clientSet *kubernetes.Clientset, config *rest.Config, ns string, verbose bool, image string, stop chan struct{}) *diagJobState {
	ctx, cancelFunc := context.WithTimeout(context.Background(), jobTimeout)
	factory := informers.NewSharedInformerFactoryWithOptions(
		clientSet,
		jobPollingInterval,
		informers.WithNamespace(ns),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = "app.kubernetes.io/name=eck-diagnostics"
		}))
	state := &diagJobState{
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
	go func() {
		<-stop
		cancelFunc()
	}()
	return state
}

// scheduleJob creates a Pod to extract diagnostic data from an Elasticsearch cluster or Kibana called resourceName.
func (ds *diagJobState) scheduleJob(typ, esName, resourceName string, tls bool) error {
	podName := fmt.Sprintf("%s-%s-diag", resourceName, typ)
	tpl, err := template.New("job").Parse(jobTemplate)
	if err != nil {
		return err
	}

	diagnosticType := "api"
	shortType := "es"

	if typ == "kibana" {
		shortType = "kb"
		diagnosticType = "kibana-api"
	}

	buffer := new(bytes.Buffer)
	err = tpl.Execute(buffer, map[string]interface{}{
		"PodName":           podName,
		"DiagnosticImage":   ds.diagnosticImage,
		"Namespace":         ds.ns,
		"ESName":            esName,
		"SVCName":           fmt.Sprintf("%s-%s-http", resourceName, shortType),
		"Type":              diagnosticType,
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
		podName:      podName,
		namespace:    ds.ns,
		resourceName: resourceName,
		typ:          typ,
	}
	return nil
}

// extractFromRemote runs the equivalent of "kubectl cp" to extract the stack diagnostics from a remote Pod.
func (ds *diagJobState) extractFromRemote(pod *corev1.Pod, file *ZipFile) {
	job, found := ds.jobs[pod.Name]
	if !found {
		file.addError(fmt.Errorf("no job for Pod %s/%s", pod.Namespace, pod.Name))
		return
	}
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
		defer func() {
			// TODO: this routine never terminates in my experiments and this code never runs
			// we are effectively leaking go routines for every diagnostic we run
			outStream.Close()
		}()
		err := options.Run()
		if err != nil {
			file.addError(err)
			return
		}
	}()
	err := ds.untarIntoZip(reader, job, file)
	if err != nil {
		file.addError(err)
		return
	}
	err = ds.completeJob(job)
	if err != nil {
		file.addError(err)
		return
	}
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
				ds.extractFromRemote(pod, file)
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
	err := ds.context.Err()

	// we cancel the context when we are done but want to log any other errors e.g. deadline exceeded
	if err != nil && !errors.Is(err, context.Canceled) {
		file.addError(fmt.Errorf("extracting Elastic stack diagnostic for namespace %s: %w", ds.ns, err))
	}
	// make sure any open jobs are aborted at this point, under normal circumstances this should be a NOOP
	// when interrupted jobs might still be running and should be stopped now.
	file.addError(ds.abortAllJobs())
}

// untarIntoZip extracts the files transferred via tar from the Pod into the given ZipFile.
func (ds *diagJobState) untarIntoZip(reader *io.PipeReader, job *diagJob, file *ZipFile) error {
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
		if !strings.HasPrefix(relativeFilename, job.sourceDirPrefix()) {
			if ds.verbose {
				logger.Printf("Ignoring file %s in tar from %s diagnostics\n", header.Name, job.resourceName)
			}
			continue
		}
		switch {
		case strings.HasSuffix(relativeFilename, "tar.gz"):
			if err := ds.repackageTarGzip(tarReader, job.outputDirPrefix(), file); err != nil {
				return err
			}
		case strings.HasSuffix(relativeFilename, ".zip"):
			if err := ds.repackageZip(tarReader, job.outputDirPrefix(), file); err != nil {
				return err
			}
		default:
			out, err := file.Create(filepath.Join(ds.ns, job.typ, job.resourceName, relativeFilename))
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

// abortAllJobs terminates all open jobs.
func (ds *diagJobState) abortAllJobs() error {
	var errs []error
	for _, j := range ds.jobs {
		if !j.done {
			logger.Printf("Aborting diagnostic extraction for %s %s/%s", j.typ, ds.ns, j.resourceName)
			// use a new context for this cleanup as the main context might have been cancelled already
			errs = append(errs, ds.terminateJob(context.Background(), j))
		}
	}
	return utilerrors.NewAggregate(errs)
}

// completeJob to be called after successful completion, terminates the job.
func (ds *diagJobState) completeJob(job *diagJob) error {
	logger.Printf("%s diagnostics extracted for %s/%s\n", strings.Title(job.typ), ds.ns, job.resourceName)
	return ds.terminateJob(ds.context, job)
}

// terminateJob marks job as done and deletes diagnostic Pod.
func (ds *diagJobState) terminateJob(ctx context.Context, job *diagJob) error {
	job.done = true
	return ds.clientSet.CoreV1().Pods(ds.ns).Delete(ctx, job.podName, metav1.DeleteOptions{GracePeriodSeconds: pointer.Int64Ptr(0)})
}

// repackageTarGzip repackages the *.tar.gz archives produced by the support diagnostics tool into the given ZipFile.
func (ds *diagJobState) repackageTarGzip(in io.Reader, outputDirPrefix string, zipFile *ZipFile) error {
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
			newPath, err := toOutputPath(header.Name, topLevelDir, outputDirPrefix)
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

// repackageZip repackages the *.zip file produced by the support diagnostics tool into the zip file produced by this tool
func (ds *diagJobState) repackageZip(in io.Reader, outputDirPrefix string, zipFile *ZipFile) error {
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
		newPath, err := toOutputPath(f.Name, topLevelDir, outputDirPrefix)
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

// copyFromZip writes the contents of file f from a zip file into out.
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

// rootDir returns the top level directory in a path
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

// toOutputPath removes the path prefix topLevelDir from original and re-bases it in outputDirPrefix.
func toOutputPath(original, topLevelDir, outputDirPrefix string) (string, error) {
	rel, err := filepath.Rel(topLevelDir, original)
	if err != nil {
		return "", err
	}
	return filepath.Join(outputDirPrefix, rel), nil
}

// runStackDiagnostics extracts diagnostic data from all clusters in the given namespace ns using the official
// Elasticsearch support diagnostics.
func runStackDiagnostics(k *Kubectl, ns string, zipFile *ZipFile, verbose bool, image string, stop chan struct{}) {
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
	state := newDiagJobState(clientSet, config, ns, verbose, image, stop)

	if err := scheduleJobs(k, ns, zipFile.addError, state, "elasticsearch"); err != nil {
		zipFile.addError(err)
		return
	}
	if err := scheduleJobs(k, ns, zipFile.addError, state, "kibana"); err != nil {
		zipFile.addError(err)
		return
	}
	// don't start extracting if there is nothing to do
	if len(state.jobs) == 0 {
		return
	}
	state.extractJobResults(zipFile)
}

// scheduleJobs lists all resources of type typ and schedules a diagnostic job for each of them
func scheduleJobs(k *Kubectl, ns string, recordErr func(error), state *diagJobState, typ string) error {
	resources, err := k.getResources(typ, ns)
	if err != nil {
		return err // not recoverable
	}
	return resources.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			// record error but continue trying for other resources
			recordErr(err)
		}

		resourceName := info.Name
		es, err := runtime.DefaultUnstructuredConverter.ToUnstructured(info.Object)
		if err != nil {
			recordErr(err)
			return nil
		}
		disabled, found, err := unstructured.NestedBool(es, "spec", "http", "tls", "selfSignedCertificate", "disabled")
		if err != nil {
			recordErr(err)
			return nil
		}
		tls := !(found && disabled)

		esName := resourceName
		if typ != "elasticsearch" {
			val, found, err := unstructured.NestedString(es, "spec", "elasticsearchRef", "name")
			if err != nil {
				recordErr(err)
				return nil
			}
			if !found || val == "" {
				logger.Printf("Skipping %s/%s as it it not using elasticsearchRef", ns, resourceName)
				return nil
			}
			esName = val
		}

		recordErr(state.scheduleJob(typ, esName, resourceName, tls))
		return nil
	})
}
