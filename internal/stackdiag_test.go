// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"bytes"
	_ "embed"
	"reflect"
	"testing"
	"text/template"

	"github.com/ghodss/yaml"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"
)

func Test_jobTemplate_imagePullSecrets(t *testing.T) {
	tests := []struct {
		name             string
		imagePullSecrets []string
		want             []corev1.LocalObjectReference
	}{
		{
			name:             "secrets set when flag provided",
			imagePullSecrets: []string{"my-registry-secret", "another-secret"},
			want:             []corev1.LocalObjectReference{{Name: "my-registry-secret"}, {Name: "another-secret"}},
		},
		{
			name:             "secrets absent when flag not provided",
			imagePullSecrets: nil,
			want:             nil,
		},
		{
			name:             "secrets absent when empty slice provided",
			imagePullSecrets: []string{},
			want:             nil,
		},
	}

	baseData := map[string]interface{}{
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
	}

	tpl, err := template.New("job").Parse(jobTemplate)
	if err != nil {
		t.Fatalf("parsing template: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make(map[string]interface{}, len(baseData)+1)
			for k, v := range baseData {
				data[k] = v
			}
			data["ImagePullSecrets"] = tt.imagePullSecrets

			buf := new(bytes.Buffer)
			if err := tpl.Execute(buf, data); err != nil {
				t.Fatalf("executing template: %v", err)
			}
			var pod corev1.Pod
			if err := yaml.Unmarshal(buf.Bytes(), &pod); err != nil {
				t.Fatalf("unmarshaling pod: %v\nrendered:\n%s", err, buf.String())
			}
			if !reflect.DeepEqual(pod.Spec.ImagePullSecrets, tt.want) {
				t.Errorf("ImagePullSecrets = %v, want %v", pod.Spec.ImagePullSecrets, tt.want)
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
