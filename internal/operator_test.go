// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func Test_detectInstallMethod(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name:   "helm install",
			labels: map[string]string{"helm.sh/chart": "eck-operator-2.14.0", "app.kubernetes.io/managed-by": "Helm"},
			want:   "helm",
		},
		{
			name:   "yaml install",
			labels: map[string]string{"control-plane": "elastic-operator"},
			want:   "yaml",
		},
		{
			name:   "olm install",
			labels: map[string]string{"olm.owner": "elastic-operator.2.14.0", "olm.owner.kind": "ClusterServiceVersion"},
			want:   "olm",
		},
		{
			name:   "unknown",
			labels: map[string]string{"some-other-label": "value"},
			want:   "unknown",
		},
		{
			name:   "empty labels",
			labels: map[string]string{},
			want:   "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectInstallMethod(tt.labels); got != tt.want {
				t.Errorf("detectInstallMethod() = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_extractFlagValue(t *testing.T) {
	tests := []struct {
		name string
		args []string
		flag string
		want string
	}{
		{
			name: "--flag=value form",
			args: []string{"--config=/conf/eck.yaml", "--other=x"},
			flag: "--config",
			want: "/conf/eck.yaml",
		},
		{
			name: "--flag value form",
			args: []string{"--config", "/conf/eck.yaml", "--other", "x"},
			flag: "--config",
			want: "/conf/eck.yaml",
		},
		{
			name: "--namespaces with csv",
			args: []string{"--namespaces=ns1,ns2,ns3"},
			flag: "--namespaces",
			want: "ns1,ns2,ns3",
		},
		{
			name: "flag not present",
			args: []string{"--other=value"},
			flag: "--namespaces",
			want: "",
		},
		{
			name: "flag at end with no value",
			args: []string{"--config"},
			flag: "--config",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractFlagValue(tt.args, tt.flag); got != tt.want {
				t.Errorf("extractFlagValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_splitCSV(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "single", input: "ns1", want: []string{"ns1"}},
		{name: "multiple", input: "ns1,ns2,ns3", want: []string{"ns1", "ns2", "ns3"}},
		{name: "with spaces", input: " ns1 , ns2 ", want: []string{"ns1", "ns2"}},
		{name: "empty string", input: "", want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitCSV(tt.input)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitCSV() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_findConfigMapForPath(t *testing.T) {
	baseVolumes := []corev1.Volume{
		{
			Name: "config-vol",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "elastic-operator"},
				},
			},
		},
		{
			Name: "secret-vol",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: "some-secret"},
			},
		},
	}
	baseContainer := corev1.Container{
		VolumeMounts: []corev1.VolumeMount{
			{Name: "config-vol", MountPath: "/conf"},
			{Name: "secret-vol", MountPath: "/certs"},
		},
	}

	tests := []struct {
		name        string
		podSpec     corev1.PodSpec
		container   corev1.Container
		configPath  string
		wantCMName  string
		wantDataKey string
	}{
		{
			name:        "standard mount, key from filename",
			podSpec:     corev1.PodSpec{Volumes: baseVolumes},
			container:   baseContainer,
			configPath:  "/conf/eck.yaml",
			wantCMName:  "elastic-operator",
			wantDataKey: "eck.yaml",
		},
		{
			name:        "custom config filename",
			podSpec:     corev1.PodSpec{Volumes: baseVolumes},
			container:   baseContainer,
			configPath:  "/conf/operator.yaml",
			wantCMName:  "elastic-operator",
			wantDataKey: "operator.yaml",
		},
		{
			name: "items mapping remaps key",
			podSpec: corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name: "config-vol",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "elastic-operator"},
							Items:                []corev1.KeyToPath{{Key: "my-config", Path: "eck.yaml"}},
						},
					},
				}},
			},
			container:   baseContainer,
			configPath:  "/conf/eck.yaml",
			wantCMName:  "elastic-operator",
			wantDataKey: "my-config",
		},
		{
			name: "items mapping with no matching path returns empty",
			podSpec: corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name: "config-vol",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "elastic-operator"},
							Items:                []corev1.KeyToPath{{Key: "my-config", Path: "other.yaml"}},
						},
					},
				}},
			},
			container:   baseContainer,
			configPath:  "/conf/eck.yaml",
			wantCMName:  "",
			wantDataKey: "",
		},
		{
			name:        "no matching mount",
			podSpec:     corev1.PodSpec{Volumes: baseVolumes},
			container:   baseContainer,
			configPath:  "/other/file.yaml",
			wantCMName:  "",
			wantDataKey: "",
		},
		{
			name:        "secret volume returns empty",
			podSpec:     corev1.PodSpec{Volumes: baseVolumes},
			container:   baseContainer,
			configPath:  "/certs/tls.crt",
			wantCMName:  "",
			wantDataKey: "",
		},
		{
			name: "subPath single-file mount uses subPath as key",
			podSpec: corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name: "config-vol",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "elastic-operator"},
						},
					},
				}},
			},
			container: corev1.Container{
				VolumeMounts: []corev1.VolumeMount{
					{Name: "config-vol", MountPath: "/conf/operator.yaml", SubPath: "operator.yaml"},
				},
			},
			configPath:  "/conf/operator.yaml",
			wantCMName:  "elastic-operator",
			wantDataKey: "operator.yaml",
		},
		{
			name: "subPath mount does not match different configPath",
			podSpec: corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name: "config-vol",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "elastic-operator"},
						},
					},
				}},
			},
			container: corev1.Container{
				VolumeMounts: []corev1.VolumeMount{
					{Name: "config-vol", MountPath: "/conf/operator.yaml", SubPath: "operator.yaml"},
				},
			},
			configPath:  "/conf/eck.yaml",
			wantCMName:  "",
			wantDataKey: "",
		},
		{
			name:        "mount at /conf does not match configPath /config/eck.yaml",
			podSpec:     corev1.PodSpec{Volumes: baseVolumes},
			container:   baseContainer,
			configPath:  "/config/eck.yaml",
			wantCMName:  "",
			wantDataKey: "",
		},
		{
			name: "subPath with items remapping resolves to mapped key",
			podSpec: corev1.PodSpec{
				Volumes: []corev1.Volume{{
					Name: "config-vol",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "elastic-operator"},
							Items:                []corev1.KeyToPath{{Key: "my-config", Path: "operator.yaml"}},
						},
					},
				}},
			},
			container: corev1.Container{
				VolumeMounts: []corev1.VolumeMount{
					{Name: "config-vol", MountPath: "/conf/operator.yaml", SubPath: "operator.yaml"},
				},
			},
			configPath:  "/conf/operator.yaml",
			wantCMName:  "elastic-operator",
			wantDataKey: "my-config",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCM, gotKey := findConfigMapForPath(tt.podSpec, tt.container, tt.configPath)
			if gotCM != tt.wantCMName || gotKey != tt.wantDataKey {
				t.Errorf("findConfigMapForPath() = (%q, %q), want (%q, %q)", gotCM, gotKey, tt.wantCMName, tt.wantDataKey)
			}
		})
	}
}

func Test_parseManagedNamespacesFromConfigMap(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]string
		dataKey string
		want    *ManagedNamespaces
		wantErr bool
	}{
		{
			name:    "static namespaces as yaml sequence",
			data:    map[string]string{"eck.yaml": "namespaces: [ns1,ns2]\n"},
			dataKey: "eck.yaml",
			want:    &ManagedNamespaces{All: false, Static: []string{"ns1", "ns2"}},
		},
		{
			name:    "static namespaces as yaml list",
			data:    map[string]string{"eck.yaml": "namespaces:\n- ns1\n- ns2\n"},
			dataKey: "eck.yaml",
			want:    &ManagedNamespaces{All: false, Static: []string{"ns1", "ns2"}},
		},
		{
			name:    "namespace-selector",
			data:    map[string]string{"eck.yaml": "namespace-selector:\n  matchLabels:\n    eck-managed: \"true\"\n"},
			dataKey: "eck.yaml",
			want: &ManagedNamespaces{
				All:      false,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"eck-managed": "true"}},
			},
		},
		{
			name:    "custom data key (e.g. operator.yaml)",
			data:    map[string]string{"operator.yaml": "namespaces: [ns1,ns2]\n"},
			dataKey: "operator.yaml",
			want:    &ManagedNamespaces{All: false, Static: []string{"ns1", "ns2"}},
		},
		{
			name:    "key not present returns error",
			data:    map[string]string{"other.yaml": "namespaces: [ns1]\n"},
			dataKey: "eck.yaml",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid yaml returns error",
			data:    map[string]string{"eck.yaml": "namespaces: [\n"},
			dataKey: "eck.yaml",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "namespaces with unexpected type returns error",
			data:    map[string]string{"eck.yaml": "namespaces: 123\n"},
			dataKey: "eck.yaml",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "no namespace restriction returns nil without error",
			data:    map[string]string{"eck.yaml": "operator-namespace: elastic-system\n"},
			dataKey: "eck.yaml",
			want:    nil,
		},
		{
			name:    "empty namespace list returns nil without error",
			data:    map[string]string{"eck.yaml": "namespaces: []\n"},
			dataKey: "eck.yaml",
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseManagedNamespacesFromConfigMap(tt.data, tt.dataKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseManagedNamespacesFromConfigMap() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseManagedNamespacesFromConfigMap() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func Test_annotationKeyFromFieldPath(t *testing.T) {
	tests := []struct {
		fieldPath string
		want      string
	}{
		{fieldPath: "metadata.annotations['olm.targetNamespaces']", want: "olm.targetNamespaces"},
		{fieldPath: "metadata.annotations['olm.operatorNamespace']", want: "olm.operatorNamespace"},
		{fieldPath: "metadata.name", want: ""},
		{fieldPath: "metadata.namespace", want: ""},
		{fieldPath: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.fieldPath, func(t *testing.T) {
			if got := annotationKeyFromFieldPath(tt.fieldPath); got != tt.want {
				t.Errorf("annotationKeyFromFieldPath(%q) = %q, want %q", tt.fieldPath, got, tt.want)
			}
		})
	}
}

func Test_resolveEnvValueFromFieldRef(t *testing.T) {
	podMeta := metav1.ObjectMeta{
		Annotations: map[string]string{"olm.targetNamespaces": "ns1,ns2"},
	}
	src := &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{
			FieldPath: "metadata.annotations['olm.targetNamespaces']",
		},
	}
	if got := resolveEnvValueFromFieldRef(podMeta, src); got != "ns1,ns2" {
		t.Errorf("resolveEnvValueFromFieldRef() = %q, want %q", got, "ns1,ns2")
	}
}

func Test_detectManagedNamespaces(t *testing.T) {
	const operatorImage = "docker.elastic.co/eck/eck-operator:2.14.0"
	tests := []struct {
		name        string
		podTemplate corev1.PodTemplateSpec
		want        ManagedNamespaces
	}{
		{
			name: "arg wins over env var",
			podTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "manager",
						Image: operatorImage,
						Args:  []string{"--namespaces=ns-from-arg"},
						Env:   []corev1.EnvVar{{Name: "NAMESPACES", Value: "ns-from-env"}},
					}},
				},
			},
			want: ManagedNamespaces{All: false, Static: []string{"ns-from-arg"}},
		},
		{
			name: "env wins over config file",
			podTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "manager",
						Image: operatorImage,
						Args:  []string{"--config=/conf/eck.yaml"},
						Env:   []corev1.EnvVar{{Name: "NAMESPACES", Value: "ns-from-env"}},
					}},
				},
			},
			want: ManagedNamespaces{All: false, Static: []string{"ns-from-env"}},
		},
		{
			name: "ValueFrom reads annotation from pod template metadata",
			podTemplate: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"olm.targetNamespaces": "ns1,ns2"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "manager",
						Image: operatorImage,
						Env: []corev1.EnvVar{{
							Name: "NAMESPACES",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "metadata.annotations['olm.targetNamespaces']",
								},
							},
						}},
					}},
				},
			},
			want: ManagedNamespaces{All: false, Static: []string{"ns1", "ns2"}},
		},
		{
			name: "ValueFrom with no annotation in pod template returns all",
			podTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "manager",
						Image: operatorImage,
						Env: []corev1.EnvVar{{
							Name: "NAMESPACES",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "metadata.annotations['olm.targetNamespaces']",
								},
							},
						}},
					}},
				},
			},
			want: ManagedNamespaces{All: true},
		},
		{
			name: "no arg no env no volume returns all",
			podTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "manager",
						Image: operatorImage,
						Args:  []string{"--config=/conf/eck.yaml"},
					}},
				},
			},
			want: ManagedNamespaces{All: true},
		},
		{
			name: "no manager container returns all",
			podTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "sidecar",
						Image: "some-other-image:latest",
						Args:  []string{"--namespaces=ns1"},
					}},
				},
			},
			want: ManagedNamespaces{All: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectManagedNamespaces(nil, "elastic-system", tt.podTemplate)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("detectManagedNamespaces() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func Test_detectOperatorInfo(t *testing.T) {
	const ns = "elastic-system"

	tests := []struct {
		name                 string
		objects              []appsv1.StatefulSet
		deployments          []appsv1.Deployment
		userSpecifiedVersion string
		wantVersion          string
		wantInstallMethod    string
		wantManagedNS        ManagedNamespaces
	}{
		{
			name: "StatefulSet path: version from label, yaml install, all namespaces",
			objects: []appsv1.StatefulSet{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastic-operator",
					Namespace: ns,
					Labels: map[string]string{
						"control-plane":             "elastic-operator",
						"app.kubernetes.io/version": "2.14.0",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "manager", Image: "docker.elastic.co/eck/eck-operator:2.14.0"}},
						},
					},
				},
			}},
			wantVersion:       "2.14.0",
			wantInstallMethod: "yaml",
			wantManagedNS:     ManagedNamespaces{All: true},
		},
		{
			name: "StatefulSet path: user-specified version overrides label",
			objects: []appsv1.StatefulSet{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastic-operator",
					Namespace: ns,
					Labels: map[string]string{
						"control-plane":             "elastic-operator",
						"app.kubernetes.io/version": "2.14.0",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "manager"}},
						},
					},
				},
			}},
			userSpecifiedVersion: "2.13.0",
			wantVersion:          "2.13.0",
			wantInstallMethod:    "yaml",
			wantManagedNS:        ManagedNamespaces{All: true},
		},
		{
			name: "StatefulSet path: helm install detected from labels",
			objects: []appsv1.StatefulSet{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastic-operator",
					Namespace: ns,
					Labels: map[string]string{
						"helm.sh/chart":                "eck-operator-2.14.0",
						"app.kubernetes.io/managed-by": "Helm",
						"app.kubernetes.io/version":    "2.14.0",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "manager"}},
						},
					},
				},
			}},
			wantVersion:       "2.14.0",
			wantInstallMethod: "helm",
			wantManagedNS:     ManagedNamespaces{All: true},
		},
		{
			name: "StatefulSet path: managed namespaces from --namespaces flag",
			objects: []appsv1.StatefulSet{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastic-operator",
					Namespace: ns,
					Labels: map[string]string{
						"control-plane":             "elastic-operator",
						"app.kubernetes.io/version": "2.14.0",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "manager",
								Image: "docker.elastic.co/eck/eck-operator:2.14.0",
								Args:  []string{"--namespaces=ns1,ns2"},
							}},
						},
					},
				},
			}},
			wantVersion:       "2.14.0",
			wantInstallMethod: "yaml",
			wantManagedNS:     ManagedNamespaces{All: false, Static: []string{"ns1", "ns2"}},
		},
		{
			name: "OLM Deployment path: version from OLM label",
			deployments: []appsv1.Deployment{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastic-operator",
					Namespace: ns,
					Labels: map[string]string{
						"olm.owner": "elastic-operator.2.14.0",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "manager"}},
						},
					},
				},
			}},
			wantVersion:       "2.14.0",
			wantInstallMethod: "olm",
			wantManagedNS:     ManagedNamespaces{All: true},
		},
		{
			name:              "no operator found returns defaults",
			wantVersion:       "unknown",
			wantInstallMethod: "unknown",
			wantManagedNS:     ManagedNamespaces{All: true},
		},
		{
			name: "StatefulSet path: version falls back to container image tag",
			objects: []appsv1.StatefulSet{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastic-operator",
					Namespace: ns,
					Labels:    map[string]string{"control-plane": "elastic-operator"},
				},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "manager",
								Image: "docker.elastic.co/eck/eck-operator:2.13.0",
							}},
						},
					},
				},
			}},
			wantVersion:       "2.13.0",
			wantInstallMethod: "yaml",
			wantManagedNS:     ManagedNamespaces{All: true},
		},
		{
			name: "OLM Deployment path: version falls back to container image tag",
			deployments: []appsv1.Deployment{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastic-operator",
					Namespace: ns,
					Labels:    map[string]string{"olm.owner": "elastic-operator"},
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "manager",
								Image: "docker.elastic.co/eck/eck-operator:2.13.0",
							}},
						},
					},
				},
			}},
			wantVersion:       "2.13.0",
			wantInstallMethod: "olm",
			wantManagedNS:     ManagedNamespaces{All: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []runtime.Object
			for i := range tt.objects {
				objs = append(objs, &tt.objects[i])
			}
			for i := range tt.deployments {
				objs = append(objs, &tt.deployments[i])
			}
			c := fake.NewSimpleClientset(objs...)
			got := detectOperatorInfo(c, ns, tt.userSpecifiedVersion)
			if got.Version != tt.wantVersion {
				t.Errorf("Version = %q, want %q", got.Version, tt.wantVersion)
			}
			if got.InstallMethod != tt.wantInstallMethod {
				t.Errorf("InstallMethod = %q, want %q", got.InstallMethod, tt.wantInstallMethod)
			}
			if !reflect.DeepEqual(got.ManagedNamespaces, tt.wantManagedNS) {
				t.Errorf("ManagedNamespaces = %+v, want %+v", got.ManagedNamespaces, tt.wantManagedNS)
			}
		})
	}
}
