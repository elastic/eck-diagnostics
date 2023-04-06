// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/elastic/eck-diagnostics/internal/archive"
	internal_filters "github.com/elastic/eck-diagnostics/internal/filters"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/kubectl/pkg/cmd/get"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/describe"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/util/openapi"
)

// Kubectl provides utilities based on the kubectl API.
type Kubectl struct {
	defaultNamespace string
	config           *rest.Config
	*kubernetes.Clientset
	factory       cmdutil.Factory
	openAPISchema openapi.Resources
	out           io.Writer
	errOut        io.Writer
	verbose       bool
}

// NewKubectl creates a new instance of Kubectl.
func NewKubectl(kubeConfig string, verbose bool) (*Kubectl, error) {
	confFlags := genericclioptions.NewConfigFlags(true)
	if kubeConfig != "" {
		confFlags.KubeConfig = &kubeConfig
	}
	matchVersionFlags := cmdutil.NewMatchVersionFlags(confFlags)
	factory := cmdutil.NewFactory(matchVersionFlags)

	openAPISchema, err := factory.OpenAPISchema()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve OpenAPI schema: %w", err)
	}

	config, err := factory.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	clientSet, err := factory.KubernetesClientSet()
	if err != nil {
		return nil, err
	}

	return &Kubectl{
		Clientset:        clientSet,
		defaultNamespace: "default",
		config:           config,
		factory:          factory,
		openAPISchema:    openAPISchema,
		out:              os.Stdout,
		errOut:           os.Stderr,
		verbose:          verbose,
	}, nil
}

// CheckNamespaces verifies that all namespaces in nss exist.
func (c Kubectl) CheckNamespaces(ctx context.Context, nss []string) error {
	clientSet, err := c.factory.KubernetesClientSet()
	if err != nil {
		return err
	}
	for _, ns := range nss {
		if _, err := clientSet.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func (c Kubectl) Copy(nsn types.NamespacedName, container string, path string, recordErr func(error)) (*io.PipeReader, error) {
	execErrOut := io.Discard
	if c.verbose {
		execErrOut = c.errOut
	}
	reader, outStream := io.Pipe()
	options := &exec.ExecOptions{
		StreamOptions: exec.StreamOptions{
			IOStreams: genericclioptions.IOStreams{
				In:     nil,
				Out:    outStream,
				ErrOut: execErrOut,
			},

			Namespace:     nsn.Namespace,
			PodName:       nsn.Name,
			ContainerName: container,
		},
		Config:    c.config,
		PodClient: c.CoreV1(),
		Command:   []string{"tar", "cf", "-", path},
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
			recordErr(err)
			return
		}
	}()
	return reader, nil
}

func (c Kubectl) Exec(nsn types.NamespacedName, cmd ...string) error {
	execErrOut := io.Discard
	if c.verbose {
		execErrOut = c.errOut
	}
	options := &exec.ExecOptions{
		StreamOptions: exec.StreamOptions{
			IOStreams: genericclioptions.IOStreams{
				In:     nil,
				Out:    nil,
				ErrOut: execErrOut,
			},

			Namespace: nsn.Namespace,
			PodName:   nsn.Name,
		},
		Config:    c.config,
		PodClient: c.CoreV1(),
		Command:   cmd,
		Executor:  &exec.DefaultRemoteExecutor{},
	}
	return options.Run()
}

// GetByLabel retrieves the K8s objects of type resource in namespace and marshals them into the writer w.
// If filters is not empty, this will only return resources within the cluster that its labels match
// at least one of the filter's label selectors.
func (c Kubectl) GetByLabel(resource, namespace string, filters internal_filters.Filters, w io.Writer) error {
	return c.getFiltered(resource, namespace, w,
		func(object metav1.Object) bool {
			return filters.Matches(object.GetLabels())
		},
		filters.Empty())
}

// GetByName retrieves the K8s objects of type resource in namespace and marshals them into the writer w.
// If filters is not empty, this will only return resources within the cluster that its name matches
// at least one of the filter's type+name pair.
func (c Kubectl) GetByName(resource, namespace string, filters internal_filters.Filters, w io.Writer) error {
	return c.getFiltered(resource, namespace, w,
		func(object metav1.Object) bool {
			return filters.Contains(object.GetName(), resource)
		},
		filters.Empty())
}

func (c Kubectl) getFiltered(resource, namespace string, w io.Writer, filter func(object metav1.Object) bool, skipFilter bool) error {
	r, err := c.getResources(resource, namespace)
	if err != nil {
		return err
	}

	printer, err := printers.NewTypeSetter(scheme.Scheme).WrapToPrinter(&printers.JSONPrinter{}, nil)
	if err != nil {
		return err
	}

	obj, err := r.Object()
	if err != nil {
		return err
	}

	// If there are no filters, simply return the unfiltered resources.
	if skipFilter {
		return printer.PrintObj(obj, w)
	}

	// Otherwise, convert the returned resource to a List, and filter for any matching objects
	// using the provided filter func.
	var list *corev1.List
	list, ok := obj.(*corev1.List)
	if !ok {
		return fmt.Errorf("while converting returned object (%T) to list", obj)
	}

	filtered := corev1.List{}
	for _, item := range list.Items {
		obj, err := meta.Accessor(item.Object)
		if err != nil {
			return fmt.Errorf("while accessing metadata for %s: %w", item.Object.GetObjectKind().GroupVersionKind().String(), err)
		}

		if filter(obj) {
			filtered.Items = append(filtered.Items, item)
		}
	}

	return printer.PrintObj(&filtered, w)
}

// GetInHumanReadable retrieves the K8s objects of type resource in namespace and marshals them into the writer w.
// If filters is not empty, this will only return resources within the cluster that its labels match
// at least one of the filter's label selectors.
func (c Kubectl) GetInHumanReadable(resource, namespace string, filters internal_filters.Filters, w io.Writer) error {
	execErrOut := io.Discard
	if c.verbose {
		execErrOut = c.errOut
	}

	pipeR, pipeW := io.Pipe()
	defer pipeR.Close()

	// GetOptions.Run() will output to the pipe writer.
	options := get.NewGetOptions("eck-diagnostics", genericclioptions.IOStreams{In: nil, Out: pipeW, ErrOut: execErrOut})
	cmd := get.NewCmdGet(options.CmdParent, c.factory, options.IOStreams)

	// Suppresses output to stderr
	options.IgnoreNotFound = true
	// Needs to show labels for filtering
	ShowLabels := true
	options.PrintFlags.HumanReadableFlags.ShowLabels = &ShowLabels

	if err := options.Complete(c.factory, cmd, []string{resource}); err != nil {
		return err
	}

	// Needs to set ns options here since the above Complete() overrides them using values from factory.
	options.Namespace = namespace
	options.ExplicitNamespace = true

	if err := options.Validate(); err != nil {
		return err
	}

	go func() {
		defer pipeW.Close()
		// Simulates "kubectl --ignore-not-found --show-labels --namespace {{namespace}} get {{resource}}"
		_ = options.Run(c.factory, cmd, []string{resource})
	}()

	// Bridges pipe reader to the writer w with or without filtering.
	if filters.Empty() {
		_, err := io.Copy(w, pipeR)
		return err
	}

	scanner, index := bufio.NewScanner(pipeR), 0
	for scanner.Scan() {
		line := scanner.Text() // either blank, header, or resource line
		switch {
		case line == "":
			fmt.Fprintln(w, line)
		case strings.HasPrefix(line, "NAME "):
			index = strings.LastIndex(line, "LABELS")
			fmt.Fprintln(w, line)
		case index > 0 && filters.MatchesAgainstString(line[index:]):
			fmt.Fprintln(w, line)
		}
	}
	return scanner.Err()
}

// getResources retrieves the K8s objects of type resource and returns a resource.Result.
func (c Kubectl) getResources(resource string, namespace string) (*resource.Result, error) {
	r := c.factory.NewBuilder().
		Unstructured().
		NamespaceParam(namespace).DefaultNamespace().AllNamespaces(false).
		ResourceTypeOrNameArgs(true, resource).
		ContinueOnError().
		Latest().
		Flatten().
		Do()

	r.IgnoreErrors(apierrors.IsNotFound)
	if err := r.Err(); err != nil {
		return nil, err
	}
	return r, nil
}

// getResourcesMatching retrieves the K8s objects of type resource matching label selector and returns a resource.Result.
func (c Kubectl) getResourcesMatching(resource string, namespace string, selector string) (*resource.Result, error) {
	r := c.factory.NewBuilder().
		Unstructured().
		NamespaceParam(namespace).DefaultNamespace().AllNamespaces(false).
		ResourceTypeOrNameArgs(true, resource).
		LabelSelector(selector).
		ContinueOnError().
		Latest().
		Flatten().
		Do()

	r.IgnoreErrors(apierrors.IsNotFound)
	if err := r.Err(); err != nil {
		return nil, err
	}
	return r, nil
}

// GetMeta retrieves the metadata for the K8s objects of type resource and marshals them into writer w.
// It tries to elide sensitive data like secret contents or kubectl last-applied configuration annotations.
func (c Kubectl) GetMeta(resource, namespace string, w io.Writer) error {
	r := c.factory.NewBuilder().
		Unstructured().
		NamespaceParam(namespace).DefaultNamespace().AllNamespaces(false).
		ResourceTypeOrNameArgs(true, resource).
		ContinueOnError().
		Latest().
		Flatten().
		Do()

	r.IgnoreErrors(apierrors.IsNotFound)
	if err := r.Err(); err != nil {
		return err
	}

	type MetaList struct {
		Items []interface{}
	}

	var metas MetaList
	metaAccess := meta.NewAccessor()
	infos, err := r.Infos()
	if err != nil {
		return err
	}
	for i := range infos {
		obj := infos[i].Object
		annotations, err := metaAccess.Annotations(obj)
		if err != nil {
			return err
		}
		// last-applied-configuration can contain sensitive data let's remove it
		delete(annotations, corev1.LastAppliedConfigAnnotation)
		if err := metaAccess.SetAnnotations(obj, annotations); err != nil {
			return err
		}
		unstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return err
		}
		// remove the actual secret data
		delete(unstructured, "data")
		// or spec for other objects
		delete(unstructured, "spec")
		metas.Items = append(metas.Items, unstructured)
	}
	bytes, err := json.MarshalIndent(metas, "", "    ")
	if err != nil {
		return err
	}
	_, err = w.Write(bytes)
	return err
}

// Describe mimics "kubectl describe" and writes the result to writer w.
func (c Kubectl) Describe(resource, prefix, namespace string, w io.Writer) error {
	r := c.factory.NewBuilder().
		Unstructured().
		NamespaceParam(namespace).DefaultNamespace().AllNamespaces(false).
		ResourceTypeOrNameArgs(true, resource).
		ContinueOnError().
		Latest().
		Flatten().
		Do()
	if err := r.Err(); err != nil {
		return err
	}

	infos, err := r.Infos()
	if err != nil {
		return err
	}
	for _, info := range infos {
		if !strings.HasPrefix(info.Name, prefix) {
			continue
		}
		mapping := info.ResourceMapping()
		desc, err := describe.Describer(c.factory, mapping)
		if err != nil {
			return err
		}
		s, err := desc.Describe(info.Namespace, info.Name, describe.DescriberSettings{ShowEvents: true})
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "%s\n", s)
	}
	return nil
}

// Logs mimics "kubectl logs -l selector" and writes the result to writers produced by out when given a filename.
func (c Kubectl) Logs(namespace string, selector string, filters internal_filters.Filters, out func(string) (io.Writer, error)) error {
	builder := c.factory.NewBuilder().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		NamespaceParam(namespace).
		SingleResourceType().ResourceTypes("pods")

	if selector != "" {
		builder.LabelSelector(selector)
	} else {
		builder.SelectAllParam(true)
	}
	infos, err := builder.Do().Infos()
	if err != nil {
		return err
	}

	for i := range infos {
		obj := infos[i].Object
		switch t := obj.(type) {
		case *corev1.PodList:
			for _, p := range t.Items {
				// ignore pod when filters are being applied, and the pod doesn't match the filters.
				if !filters.Matches(p.Labels) {
					continue
				}
				if err := c.requestLogs(p, out); err != nil {
					return err
				}
			}
		case *corev1.Pod:
			// ignore pod when filters are being applied, and the pod doesn't match the filters.
			if !filters.Matches(t.Labels) {
				continue
			}
			if err := c.requestLogs(*t, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// requestLogs requests the logs for pod and writes the result to writers produced by out when given a filename.
func (c Kubectl) requestLogs(pod corev1.Pod, out func(string) (io.Writer, error)) error {
	// if Pod not in running state let's not extract logs, trying to get logs from previous container does not seem to work
	// reliably and might lead to extra misleading noise in the diagnostic data
	if pod.Status.Phase != corev1.PodRunning {
		return nil
	}

	logFn := polymorphichelpers.LogsForObjectFn
	reqs, err := logFn(c.factory, &pod, &corev1.PodLogOptions{}, 20*time.Second, true)
	if err != nil {
		return err
	}
	writer, err := out(archive.Path(pod.Namespace, "pod", pod.Name, "logs.txt"))
	if err != nil {
		return err
	}
	for _, r := range reqs {
		if err := streamLogs(types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, r, writer); err != nil {
			return err
		}
	}
	return nil
}

// streamLogs when given a ResponseWrapper streams the result to out adding textual start and end markers.
func streamLogs(nsn types.NamespacedName, request rest.ResponseWrapper, out io.Writer) error {
	stream, err := request.Stream(context.Background())
	if err != nil {
		return err
	}
	defer stream.Close()
	_, _ = out.Write([]byte(fmt.Sprintf("==== START logs for %s ====\n", nsn.String())))
	defer func() {
		_, _ = out.Write([]byte(fmt.Sprintf("==== END logs for %s ====\n", nsn.String())))
	}()
	r := bufio.NewReader(stream)
	for {
		bytes, err := r.ReadBytes('\n')
		if _, err := out.Write(bytes); err != nil {
			return err
		}

		if err != nil {
			if err != io.EOF {
				return err
			}
			return nil
		}
	}
}

// versionInfo exists to marshal both eck-diagnostics version information and K8s server version information.
type versionInfo struct {
	DiagnosticsVersion DiagnosticsVersion
	ServerVersion      *version.Info
}

// Version is inspired by "kubectl version" but includes version information about this tool in addition to K8s
// server version information.
func (c Kubectl) Version(out io.Writer) error {
	v := versionInfo{
		DiagnosticsVersion: about(),
	}
	client, err := c.factory.ToDiscoveryClient()
	if err != nil {
		return err
	}
	// mirroring kubectl behaviour to fetch fresh data from server
	client.Invalidate()
	serverVersion, err := client.ServerVersion()
	if err != nil {
		return err
	}
	v.ServerVersion = serverVersion
	bytes, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = out.Write(bytes)
	return err
}
