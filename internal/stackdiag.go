// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/elastic/eck-diagnostics/internal/archive"
	"github.com/elastic/eck-diagnostics/internal/extraction"
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

	// names used to identify different stack diagnostic job types (need to match the names of the corresponding CRDs)
	elasticsearchJob = "elasticsearch"
	kibanaJob        = "kibana"
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
	sync.RWMutex
	extraction.RemoteSource
	d     bool
	timer *time.Timer
	done  chan struct{}
}

func (d *diagJob) StartTimer(dur time.Duration) <-chan time.Time {
	d.Lock()
	defer d.Unlock()
	d.timer = time.NewTimer(dur)
	return d.timer.C
}

func (d *diagJob) Done() bool {
	d.RLock()
	defer d.RUnlock()
	return d.d
}

func (d *diagJob) MarkDone() {
	d.Lock()
	defer d.Unlock()
	d.d = true
	if d.timer != nil {
		// We are OK with not draining the timer channel here. We do not want to reuse it, and we don't want to block
		// under any circumstance. The only point here is to avoid the timer from firing once the job is complete.
		d.timer.Stop()
	}
	d.done <- struct{}{}
}

// diagJobState captures the state of running a set of jobs to extract diagnostics from Elastic Stack applications.
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
func newDiagJobState(clientSet *kubernetes.Clientset, config *rest.Config, ns string, verbose bool, image string, stopCh chan struct{}) *diagJobState {
	ctx, cancelFunc := context.WithCancel(context.Background())
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
		<-stopCh
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

	diagnosticType, shortType := diagnosticTypeForApplication(typ)

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

	var job = diagJob{
		RemoteSource: extraction.RemoteSource{
			Namespace:    ds.ns,
			PodName:      podName,
			Typ:          typ,
			ResourceName: resourceName,
			PodOutputDir: podOutputDir,
		},
		done: make(chan struct{}, 1),
	}
	// start a dedicated timer for each job and terminate the job when the timer expires.
	go func(j *diagJob) {
		timerChan := j.StartTimer(jobTimeout)
		select {
		case <-timerChan:
			logger.Printf("Diagnostic job for %s %s/%s timed out, terminating", j.Typ, j.Namespace, j.ResourceName)
			if err = ds.terminateJob(context.Background(), j); err != nil {
				logger.Printf("while terminating job %s", err.Error())
			}
		case <-j.done:
			// we use separate done signal here to avoid building up lots of go routines that are only terminated by
			// the overall termination of the program if a job does not exceed its timeout.
		}
	}(&job)
	ds.jobs[podName] = &job
	return nil
}

// diagnosticTypeForApplication returns the diagnosticType as expected by the stack diagnostics tool and a short type
// matching the shorthand used by ECK in service names for the given application type.
func diagnosticTypeForApplication(typ string) (string, string) {
	switch typ {
	case elasticsearchJob:
		return "api", "es"
	case kibanaJob:
		return "kibana-api", "kb"
	}
	panic("programming error: unknown type")
}

// extractFromRemote runs the equivalent of "kubectl cp" to extract the stack diagnostics from a remote Pod.
func (ds *diagJobState) extractFromRemote(pod *corev1.Pod, file *archive.ZipFile) {
	job, found := ds.jobs[pod.Name]
	if !found {
		file.AddError(fmt.Errorf("no job for Pod %s/%s", pod.Namespace, pod.Name))
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
			file.AddError(err)
			return
		}
	}()
	err := extraction.UntarIntoZip(reader, job.RemoteSource, file, ds.verbose)
	if err != nil {
		file.AddError(err)
		return
	}
	err = ds.completeJob(job)
	if err != nil {
		file.AddError(err)
		return
	}
}

// extractJobResults runs an informer to be notified of Pod status changes and extract diagnostic data from any Pod
// that has reached running state.
func (ds *diagJobState) extractJobResults(file *archive.ZipFile) {
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

			if job.Done() {
				return
			}

			switch pod.Status.Phase {
			case corev1.PodPending:
				if err := ds.detectImageErrors(pod); err != nil {
					file.AddError(err)
					file.AddError(ds.terminateJob(ds.context, job))
				}
			case corev1.PodUnknown:
				logger.Printf("Unexpected diagnostic Pod %s/%s in unknown phase", pod.Namespace, pod.Name)
			case corev1.PodRunning:
				ds.extractFromRemote(pod, file)
			case corev1.PodSucceeded:
				file.AddError(fmt.Errorf("unexpected: Pod %s/%s succeeded", pod.Namespace, pod.Name))
				file.AddError(ds.completeJob(job))
			case corev1.PodFailed:
				file.AddError(fmt.Errorf("unexpected: Pod %s/%s failed", pod.Namespace, pod.Name))
				file.AddError(ds.completeJob(job))
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
				if !j.Done() {
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
		file.AddError(fmt.Errorf("extracting Elastic stack diagnostic for namespace %s: %w", ds.ns, err))
	}
	// make sure any open jobs are aborted at this point, under normal circumstances this should be a NOOP
	// when interrupted jobs might still be running and should be stopped now.
	file.AddError(ds.abortAllJobs())
}

// abortAllJobs terminates all open jobs.
func (ds *diagJobState) abortAllJobs() error {
	var errs []error
	for _, j := range ds.jobs {
		if !j.Done() {
			logger.Printf("Aborting diagnostic extraction for %s %s/%s", j.Typ, ds.ns, j.ResourceName)
			// use a new context for this cleanup as the main context might have been cancelled already
			errs = append(errs, ds.terminateJob(context.Background(), j))
		}
	}
	return utilerrors.NewAggregate(errs)
}

// completeJob to be called after successful completion, terminates the job.
func (ds *diagJobState) completeJob(job *diagJob) error {
	logger.Printf("%s diagnostics extracted for %s/%s\n", strings.Title(job.Typ), ds.ns, job.ResourceName)
	return ds.terminateJob(ds.context, job)
}

// terminateJob marks job as done and deletes diagnostic Pod.
func (ds *diagJobState) terminateJob(ctx context.Context, job *diagJob) error {
	job.MarkDone()
	return ds.clientSet.CoreV1().Pods(ds.ns).Delete(ctx, job.PodName, metav1.DeleteOptions{GracePeriodSeconds: pointer.Int64Ptr(0)})
}

// detectImageErrors tries to detect Image pull errors on the diagnostic container. Callers should then terminate the job
// as there is little chance of the image being made available during the execution time of the tool.
func (ds *diagJobState) detectImageErrors(pod *corev1.Pod) error {
	for _, status := range pod.Status.InitContainerStatuses {
		if status.State.Waiting != nil && strings.Contains(status.State.Waiting.Reason, "Image") {
			return fmt.Errorf("failed running stack diagnostics: %s:%s", status.State.Waiting.Reason, status.State.Waiting.Message)
		}
	}
	return nil
}

// runStackDiagnostics extracts diagnostic data from all clusters in the given namespace ns using the official
// Elasticsearch support diagnostics.
func runStackDiagnostics(k *Kubectl, ns string, zipFile *archive.ZipFile, verbose bool, image string, stopCh chan struct{}) {
	config, err := k.factory.ToRESTConfig()
	if err != nil {
		zipFile.AddError(err)
		return // not recoverable let's stop here
	}
	clientSet, err := k.factory.KubernetesClientSet()
	if err != nil {
		zipFile.AddError(err)
		return // not recoverable
	}
	state := newDiagJobState(clientSet, config, ns, verbose, image, stopCh)

	if err := scheduleJobs(k, ns, zipFile.AddError, state, elasticsearchJob); err != nil {
		zipFile.AddError(err)
		return
	}
	if err := scheduleJobs(k, ns, zipFile.AddError, state, kibanaJob); err != nil {
		zipFile.AddError(err)
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
