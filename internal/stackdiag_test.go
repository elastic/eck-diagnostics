// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"bytes"
	"maps"
	"reflect"
	"strings"
	"testing"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/version"
)

func Test_jobTemplate_port(t *testing.T) {
	tests := []struct {
		name        string
		port        int32
		wantContain string
		wantAbsent  string
	}{
		{
			name:        "port flag injected when port is non-zero",
			port:        443,
			wantContain: "--port 443",
		},
		{
			name:       "port flag absent when port is zero",
			port:       0,
			wantAbsent: "--port ",
		},
	}

	baseData := map[string]any{
		"PodName":           "test-pod",
		"DiagnosticImage":   "docker.elastic.co/eck-dev/support-diagnostics:latest",
		"Namespace":         "default",
		"ESSecretName":      "",
		"ESSecretKey":       "",
		"SVCName":           "my-kibana-kb-http",
		"Type":              "kibana-api",
		"TLS":               false,
		"OutputDir":         "/diagnostic-output",
		"MainContainerName": "stack-diagnostics",
		"ImagePullSecrets":  nil,
	}

	tpl, err := template.New("job").Parse(jobTemplate)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make(map[string]any, len(baseData)+1)
			maps.Copy(data, baseData)
			data["Port"] = tt.port

			buf := new(bytes.Buffer)
			if err := tpl.Execute(buf, data); err != nil {
				t.Fatalf("executing template: %v", err)
			}
			rendered := buf.String()
			if tt.wantContain != "" && !strings.Contains(rendered, tt.wantContain) {
				t.Errorf("expected rendered template to contain %q\nrendered:\n%s", tt.wantContain, rendered)
			}
			if tt.wantAbsent != "" && strings.Contains(rendered, tt.wantAbsent) {
				t.Errorf("expected rendered template NOT to contain %q\nrendered:\n%s", tt.wantAbsent, rendered)
			}
		})
	}
}

func baseTemplateData() map[string]any {
	return map[string]any{
		"PodName":           "test-pod",
		"DiagnosticImage":   "docker.elastic.co/eck-dev/support-diagnostics:latest",
		"Namespace":         "default",
		"ESSecretName":      "es-secret",
		"ESSecretKey":       "elastic",
		"SVCName":           "elasticsearch-es-http",
		"Type":              "api",
		"TLS":               true,
		"OutputDir":         "/diagnostic-output",
		"MainContainerName": "stack-diagnostics",
		"ImagePullSecrets":  nil,
	}
}

func findContainer(pod *corev1.Pod, name string) *corev1.Container {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == name {
			return &pod.Spec.Containers[i]
		}
	}
	return nil
}

func Test_renderDiagnosticPod(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(data map[string]any)
		patch    string
		wantErr  bool
		validate func(t *testing.T, pod *corev1.Pod)
	}{
		{
			name:  "image pull secrets set when flag provided",
			setup: func(d map[string]any) { d["ImagePullSecrets"] = []string{"my-registry-secret", "another-secret"} },
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				want := []corev1.LocalObjectReference{{Name: "my-registry-secret"}, {Name: "another-secret"}}
				if !reflect.DeepEqual(pod.Spec.ImagePullSecrets, want) {
					t.Errorf("ImagePullSecrets = %v, want %v", pod.Spec.ImagePullSecrets, want)
				}
			},
		},
		{
			name: "image pull secrets absent when flag not provided",
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				if pod.Spec.ImagePullSecrets != nil {
					t.Errorf("expected nil ImagePullSecrets, got %v", pod.Spec.ImagePullSecrets)
				}
			},
		},
		{
			name:  "image pull secrets absent when empty slice provided",
			setup: func(d map[string]any) { d["ImagePullSecrets"] = []string{} },
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				if pod.Spec.ImagePullSecrets != nil {
					t.Errorf("expected nil ImagePullSecrets, got %v", pod.Spec.ImagePullSecrets)
				}
			},
		},
		{
			name:  "nil patch returns unmodified pod",
			patch: "",
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				if pod.Labels["app.kubernetes.io/name"] != "eck-diagnostics" {
					t.Errorf("expected label app.kubernetes.io/name=eck-diagnostics, got %v", pod.Labels)
				}
				c := findContainer(pod, podMainContainerName)
				if c == nil {
					t.Fatal("diagnostic container not found")
				}
				want := resource.MustParse("20Mi")
				got := c.Resources.Requests[corev1.ResourceMemory]
				if want.Cmp(got) != 0 {
					t.Errorf("memory request = %v, want %v", got.String(), want.String())
				}
			},
		},
		{
			name: "add metadata label preserves existing labels",
			patch: `
metadata:
  labels:
    network-policy: allow-es
`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				if pod.Labels["network-policy"] != "allow-es" {
					t.Errorf("expected label network-policy=allow-es, got %v", pod.Labels)
				}
				if pod.Labels["app.kubernetes.io/name"] != "eck-diagnostics" {
					t.Errorf("original label app.kubernetes.io/name lost, labels: %v", pod.Labels)
				}
			},
		},
		{
			name: "set serviceAccountName preserves automountServiceAccountToken",
			patch: `
spec:
  serviceAccountName: diag-sa
`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				if pod.Spec.ServiceAccountName != "diag-sa" {
					t.Errorf("serviceAccountName = %q, want %q", pod.Spec.ServiceAccountName, "diag-sa")
				}
				if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
					t.Errorf("automountServiceAccountToken should be false, got %v", pod.Spec.AutomountServiceAccountToken)
				}
			},
		},
		{
			name: "override container resources preserves limits",
			patch: `
spec:
  containers:
  - name: stack-diagnostics
    resources:
      requests:
        memory: 64Mi
        cpu: 200m
`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				c := findContainer(pod, podMainContainerName)
				if c == nil {
					t.Fatal("diagnostic container not found")
				}
				wantMem := resource.MustParse("64Mi")
				gotMem := c.Resources.Requests[corev1.ResourceMemory]
				if wantMem.Cmp(gotMem) != 0 {
					t.Errorf("memory request = %v, want 64Mi", gotMem.String())
				}
				wantCPU := resource.MustParse("200m")
				gotCPU := c.Resources.Requests[corev1.ResourceCPU]
				if wantCPU.Cmp(gotCPU) != 0 {
					t.Errorf("cpu request = %v, want 200m", gotCPU.String())
				}
				wantMemLimit := resource.MustParse("2Gi")
				gotMemLimit := c.Resources.Limits[corev1.ResourceMemory]
				if wantMemLimit.Cmp(gotMemLimit) != 0 {
					t.Errorf("memory limit = %v, want 2Gi (should be preserved)", gotMemLimit.String())
				}
			},
		},
		{
			name: "add env var is merged by name preserving existing vars",
			patch: `
spec:
  containers:
  - name: stack-diagnostics
    env:
    - name: MY_VAR
      value: "my-value"
`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				c := findContainer(pod, podMainContainerName)
				if c == nil {
					t.Fatal("diagnostic container not found")
				}
				envMap := make(map[string]string, len(c.Env))
				for _, e := range c.Env {
					envMap[e.Name] = e.Value
				}
				if envMap["MY_VAR"] != "my-value" {
					t.Errorf("MY_VAR not found or wrong value in env: %v", envMap)
				}
				if _, ok := envMap["ES_PW"]; !ok {
					t.Errorf("ES_PW was lost after patch, env: %v", envMap)
				}
			},
		},
		{
			name: "patch can set imagePullSecrets",
			patch: `
spec:
  imagePullSecrets:
  - name: my-registry-secret
  - name: another-secret
`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				want := []corev1.LocalObjectReference{{Name: "my-registry-secret"}, {Name: "another-secret"}}
				if !reflect.DeepEqual(pod.Spec.ImagePullSecrets, want) {
					t.Errorf("ImagePullSecrets = %v, want %v", pod.Spec.ImagePullSecrets, want)
				}
			},
		},
		{
			// imagePullSecrets merges by name; $patch:replace makes the patch fully override the flag value.
			name:  "patch imagePullSecrets takes precedence over flag when using $patch replace",
			setup: func(d map[string]any) { d["ImagePullSecrets"] = []string{"flag-secret"} },
			patch: `
spec:
  imagePullSecrets:
  - $patch: replace
  - name: patch-secret
`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				want := []corev1.LocalObjectReference{{Name: "patch-secret"}}
				if !reflect.DeepEqual(pod.Spec.ImagePullSecrets, want) {
					t.Errorf("ImagePullSecrets = %v, want %v", pod.Spec.ImagePullSecrets, want)
				}
			},
		},
		{
			// $patch:delete matched by merge key removes only that item, leaving the rest.
			name:  "$patch delete removes matched item from list",
			setup: func(d map[string]any) { d["ImagePullSecrets"] = []string{"keep-secret", "remove-secret"} },
			patch: `
spec:
  imagePullSecrets:
  - $patch: delete
    name: remove-secret
`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				want := []corev1.LocalObjectReference{{Name: "keep-secret"}}
				if !reflect.DeepEqual(pod.Spec.ImagePullSecrets, want) {
					t.Errorf("ImagePullSecrets = %v, want %v", pod.Spec.ImagePullSecrets, want)
				}
			},
		},
		{
			// $retainKeys lists the only keys to keep on an object; all others are removed from the merged result.
			name: "$retainKeys strips unlisted fields from object",
			patch: `
spec:
  containers:
  - name: stack-diagnostics
    securityContext:
      $retainKeys:
      - runAsNonRoot
      runAsNonRoot: true
`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				c := findContainer(pod, podMainContainerName)
				if c == nil {
					t.Fatal("diagnostic container not found")
				}
				sc := c.SecurityContext
				if sc == nil {
					t.Fatal("securityContext is nil")
				}
				if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
					t.Errorf("runAsNonRoot should be true")
				}
				if sc.AllowPrivilegeEscalation != nil {
					t.Errorf("allowPrivilegeEscalation should be nil after $retainKeys, got %v", sc.AllowPrivilegeEscalation)
				}
				if sc.Capabilities != nil {
					t.Errorf("capabilities should be nil after $retainKeys, got %v", sc.Capabilities)
				}
				if sc.ReadOnlyRootFilesystem != nil {
					t.Errorf("readOnlyRootFilesystem should be nil after $retainKeys, got %v", sc.ReadOnlyRootFilesystem)
				}
			},
		},
		{
			// $setElementOrder reorders an already-merged list without changing its contents.
			name:  "$setElementOrder reorders list without changing contents",
			setup: func(d map[string]any) { d["ImagePullSecrets"] = []string{"secret-a", "secret-b"} },
			patch: `
spec:
  $setElementOrder/imagePullSecrets:
  - name: secret-b
  - name: secret-a
`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				want := []corev1.LocalObjectReference{{Name: "secret-b"}, {Name: "secret-a"}}
				if !reflect.DeepEqual(pod.Spec.ImagePullSecrets, want) {
					t.Errorf("ImagePullSecrets = %v, want %v", pod.Spec.ImagePullSecrets, want)
				}
			},
		},
		{
			// $deleteFromPrimitiveList removes a specific value from a []string field (no merge key).
			name: "$deleteFromPrimitiveList removes value from primitive list",
			patch: `
spec:
  containers:
  - name: stack-diagnostics
    $deleteFromPrimitiveList/command:
    - "-c"
`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				c := findContainer(pod, podMainContainerName)
				if c == nil {
					t.Fatal("diagnostic container not found")
				}
				want := []string{"sh"}
				if !reflect.DeepEqual(c.Command, want) {
					t.Errorf("command = %v, want %v", c.Command, want)
				}
			},
		},
		{
			name:  "JSON patch adds metadata label",
			patch: `{"metadata":{"labels":{"network-policy":"allow-es"}}}`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				if pod.Labels["network-policy"] != "allow-es" {
					t.Errorf("expected label network-policy=allow-es, got %v", pod.Labels)
				}
				if pod.Labels["app.kubernetes.io/name"] != "eck-diagnostics" {
					t.Errorf("original label app.kubernetes.io/name lost, labels: %v", pod.Labels)
				}
			},
		},
		{
			name: "JSON patch overrides container resources",
			patch: `{
  "spec": {
    "containers": [
      {
        "name": "stack-diagnostics",
        "resources": {"requests": {"memory": "64Mi", "cpu": "200m"}}
      }
    ]
  }
}`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				c := findContainer(pod, podMainContainerName)
				if c == nil {
					t.Fatal("diagnostic container not found")
				}
				wantMem := resource.MustParse("64Mi")
				if wantMem.Cmp(c.Resources.Requests[corev1.ResourceMemory]) != 0 {
					t.Errorf("memory request = %v, want 64Mi", c.Resources.Requests[corev1.ResourceMemory])
				}
				wantMemLimit := resource.MustParse("2Gi")
				if wantMemLimit.Cmp(c.Resources.Limits[corev1.ResourceMemory]) != 0 {
					t.Errorf("memory limit = %v, want 2Gi (should be preserved)", c.Resources.Limits[corev1.ResourceMemory])
				}
			},
		},
		{
			name:    "invalid YAML patch returns error mentioning patch",
			patch:   "invalid: yaml: {[",
			wantErr: true,
		},
		{
			name: "patch that replaces containers list removes diagnostic container",
			patch: `
spec:
  containers:
  - $patch: replace
  - name: other
    image: busybox
    command: ["sh"]
    args: ["-c", "true"]
`,
			wantErr: true,
		},
		{
			name: "patch that clears container command triggers sanity check",
			patch: `
spec:
  containers:
  - name: stack-diagnostics
    command: []
    args: []
`,
			wantErr: true,
		},
		{
			name: "example patch applies cleanly",
			patch: `
metadata:
  labels:
    network-policy: allow-es
spec:
  serviceAccountName: diagnostics-sa
  containers:
  - name: stack-diagnostics
    securityContext:
      capabilities:
        drop: ["ALL"]
      runAsNonRoot: true
      readOnlyRootFilesystem: true
    resources:
      requests:
        memory: 64Mi
        cpu: 200m
`,
			validate: func(t *testing.T, pod *corev1.Pod) {
				t.Helper()
				if pod.Labels["network-policy"] != "allow-es" {
					t.Errorf("network-policy label missing: %v", pod.Labels)
				}
				if pod.Spec.ServiceAccountName != "diagnostics-sa" {
					t.Errorf("serviceAccountName = %q, want diagnostics-sa", pod.Spec.ServiceAccountName)
				}
				c := findContainer(pod, podMainContainerName)
				if c == nil {
					t.Fatal("diagnostic container not found")
				}
				wantMem := resource.MustParse("64Mi")
				if wantMem.Cmp(c.Resources.Requests[corev1.ResourceMemory]) != 0 {
					t.Errorf("memory request = %v, want 64Mi", c.Resources.Requests[corev1.ResourceMemory])
				}
				if c.SecurityContext == nil || c.SecurityContext.RunAsNonRoot == nil || !*c.SecurityContext.RunAsNonRoot {
					t.Errorf("runAsNonRoot should be true after patch")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := baseTemplateData()
			if tt.setup != nil {
				tt.setup(data)
			}

			pod, err := renderDiagnosticPod(data, []byte(tt.patch))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.validate != nil {
				tt.validate(t, pod)
			}
		})
	}
}

func Test_supportedStackDiagTypesFor(t *testing.T) {
	type args struct {
		eckVersion *version.Version
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "before 2.8",
			args: args{eckVersion: version.MustParseSemantic("2.7.99")},
			want: []string{"elasticsearch", "kibana"},
		},
		{
			name: "after 2.8",
			args: args{eckVersion: version.MustParseSemantic("2.8.0")},
			want: []string{"elasticsearch", "kibana", "logstash"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := supportedStackDiagTypesFor(tt.args.eckVersion); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("supportedStackDiagTypesFor() = %v, want %v", got, tt.want)
			}
		})
	}
}
