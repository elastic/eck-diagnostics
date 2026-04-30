// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest/fake"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"
)

func TestGetMeta(t *testing.T) {
	tests := []struct {
		name           string
		objects        []runtime.Object
		keepSecretData bool
		wantDataField  bool
	}{
		{
			name: "single secret data redacted when keepSecretData is false",
			objects: []runtime.Object{
				&corev1.Secret{
					TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: "default", Annotations: map[string]string{corev1.LastAppliedConfigAnnotation: "sensitive-config-data"}},
					Data:       map[string][]byte{"password": []byte("supersecret")},
					Type:       corev1.SecretTypeOpaque,
				},
			},
			keepSecretData: false,
			wantDataField:  false,
		},
		{
			name: "single secret data preserved when keepSecretData is true",
			objects: []runtime.Object{
				&corev1.Secret{
					TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: "default", Annotations: map[string]string{corev1.LastAppliedConfigAnnotation: "sensitive-config-data"}},
					Data:       map[string][]byte{"password": []byte("supersecret")},
					Type:       corev1.SecretTypeOpaque,
				},
			},
			keepSecretData: true,
			wantDataField:  true,
		},
		{
			name: "multiple secrets redacted when keepSecretData is false",
			objects: []runtime.Object{
				&corev1.Secret{
					TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
					ObjectMeta: metav1.ObjectMeta{Name: "secret-1", Namespace: "default"},
					Data:       map[string][]byte{"key1": []byte("value1")},
					Type:       corev1.SecretTypeOpaque,
				},
				&corev1.Secret{
					TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
					ObjectMeta: metav1.ObjectMeta{Name: "secret-2", Namespace: "default"},
					Data:       map[string][]byte{"key2": []byte("value2")},
					Type:       corev1.SecretTypeOpaque,
				},
			},
			keepSecretData: false,
			wantDataField:  false,
		},
		{
			name: "multiple secrets preserved when keepSecretData is true",
			objects: []runtime.Object{
				&corev1.Secret{
					TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
					ObjectMeta: metav1.ObjectMeta{Name: "secret-1", Namespace: "default"},
					Data:       map[string][]byte{"key1": []byte("value1")},
					Type:       corev1.SecretTypeOpaque,
				},
				&corev1.Secret{
					TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
					ObjectMeta: metav1.ObjectMeta{Name: "secret-2", Namespace: "default"},
					Data:       map[string][]byte{"key2": []byte("value2")},
					Type:       corev1.SecretTypeOpaque,
				},
			},
			keepSecretData: true,
			wantDataField:  true,
		},
		{
			name: "pod spec is removed",
			objects: []runtime.Object{
				&corev1.Pod{
					TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
					ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "main", Image: "nginx:latest"},
						},
					},
				},
			},
			keepSecretData: false,
			wantDataField:  false,
		},
		{
			name: "mixed secrets and pods",
			objects: []runtime.Object{
				&corev1.Secret{
					TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
					ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
					Data:       map[string][]byte{"token": []byte("secret-token")},
					Type:       corev1.SecretTypeOpaque,
				},
				&corev1.Pod{
					TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
					ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "default"},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "app", Image: "myapp:v1"},
						},
					},
				},
			},
			keepSecretData: false,
			wantDataField:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := &corev1.List{}
			for _, obj := range tt.objects {
				raw, err := runtime.Encode(scheme.Codecs.LegacyCodec(corev1.SchemeGroupVersion), obj)
				require.NoError(t, err, "failed to encode object")
				list.Items = append(list.Items, runtime.RawExtension{Raw: raw})
			}

			tf := cmdtesting.NewTestFactory().WithNamespace("default")
			defer tf.Cleanup()

			codec := scheme.Codecs.LegacyCodec(scheme.Scheme.PrioritizedVersionsAllGroups()...)
			tf.UnstructuredClient = &fake.RESTClient{
				NegotiatedSerializer: resource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer,
				Resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     cmdtesting.DefaultHeader(),
					Body:       cmdtesting.ObjBody(codec, list),
				},
			}

			kubectl := &Kubectl{
				factory: tf,
			}

			var buf bytes.Buffer
			err := kubectl.GetMeta("secrets", "default", tt.keepSecretData, &buf)
			require.NoError(t, err, "GetMeta() should not return an error")

			var result struct {
				Items []map[string]interface{} `json:"Items"`
			}
			require.NoError(t, json.Unmarshal(buf.Bytes(), &result), "failed to unmarshal result")
			require.Len(t, result.Items, len(tt.objects))

			for i, item := range result.Items {
				_, hasData := item["data"]
				assert.Equalf(t, tt.wantDataField, hasData, "item[%d]: data field presence mismatch", i)

				_, hasSpec := item["spec"]
				assert.Falsef(t, hasSpec, "item[%d]: spec field should always be removed", i)

				metadata, ok := item["metadata"].(map[string]interface{})
				require.Truef(t, ok, "item[%d]: metadata should be present and be a map", i)

				annotations, _ := metadata["annotations"].(map[string]interface{})
				if annotations != nil {
					_, hasLastApplied := annotations[corev1.LastAppliedConfigAnnotation]
					assert.Falsef(t, hasLastApplied, "item[%d]: last-applied-configuration annotation should be removed", i)
				}
			}
		})
	}
}
