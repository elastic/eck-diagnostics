// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ghodss/yaml"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
)

// ManagedNamespaces describes which namespaces an ECK operator instance manages.
// Exactly one of Static or Selector is set when All is false.
type ManagedNamespaces struct {
	All      bool                  `json:"all"`
	Static   []string              `json:"static,omitempty"`
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// OperatorInfo contains metadata about a single ECK operator instance.
type OperatorInfo struct {
	Namespace         string            `json:"namespace"`
	Version           string            `json:"version"`
	ManagedNamespaces ManagedNamespaces `json:"managed_namespaces"`
	InstallMethod     string            `json:"install_method"`
	parsedVersion     *version.Version
}

// detectOperatorInfo gathers metadata about the ECK operator running in the given namespace.
func detectOperatorInfo(c *kubernetes.Clientset, namespace, userSpecifiedVersion string) OperatorInfo {
	info := OperatorInfo{
		Namespace:         namespace,
		Version:           "unknown",
		InstallMethod:     "unknown",
		ManagedNamespaces: ManagedNamespaces{All: true},
		parsedVersion:     fallbackMaxVersion,
	}

	if userSpecifiedVersion != "" {
		if v, err := version.ParseSemantic(userSpecifiedVersion); err == nil {
			info.parsedVersion = v
			info.Version = v.String()
		}
	}

	sset, err := findOperatorStatefulSet(c, namespace)
	if err != nil {
		logger.Printf("Error finding operator StatefulSet in namespace %s: %v", namespace, err)
		return info
	}

	var podTemplate corev1.PodTemplateSpec
	if sset != nil {
		podTemplate = podTemplateFromStatefulSet(sset, &info, userSpecifiedVersion)
	} else {
		pt, err := podTemplateFromDeployment(c, namespace, &info, userSpecifiedVersion)
		if err != nil {
			logger.Println(err.Error())
			return info
		}
		podTemplate = pt
	}

	info.ManagedNamespaces = detectManagedNamespaces(c, namespace, podTemplate)
	return info
}

// versionFromStatefulSet extracts the ECK version from a StatefulSet, preferring the standard
// app.kubernetes.io/version label (available since ECK 1.3) and falling back to the container image tag.
func versionFromStatefulSet(sset *appsv1.StatefulSet) *version.Version {
	if v, err := version.ParseSemantic(sset.Labels["app.kubernetes.io/version"]); err == nil {
		return v
	}
	return extractVersionFromContainers(sset.Spec.Template.Spec.Containers)
}

func podTemplateFromStatefulSet(sset *appsv1.StatefulSet, info *OperatorInfo, userSpecifiedVersion string) corev1.PodTemplateSpec {
	info.InstallMethod = detectInstallMethod(sset.Labels)
	if userSpecifiedVersion == "" {
		v := versionFromStatefulSet(sset)
		info.parsedVersion = v
		if v != fallbackMaxVersion {
			info.Version = v.String()
		}
	}
	return sset.Spec.Template
}

func podTemplateFromDeployment(c *kubernetes.Clientset, namespace string, info *OperatorInfo, userSpecifiedVersion string) (corev1.PodTemplateSpec, error) {
	deployment, err := c.AppsV1().Deployments(namespace).Get(context.Background(), "elastic-operator", metav1.GetOptions{})
	if err != nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf("operator statefulset not found, checking for OLM deployment but failed: %w", err)
	}
	info.InstallMethod = detectInstallMethod(deployment.Labels)
	if userSpecifiedVersion == "" {
		v, verr := extractVersionFromOLMMetadata(deployment.Labels)
		if verr != nil {
			logger.Println("ECK operator not found in OLM metadata checking container image as last resort")
			v = extractVersionFromContainers(deployment.Spec.Template.Spec.Containers)
		}
		info.parsedVersion = v
		if v != fallbackMaxVersion {
			info.Version = v.String()
		}
	}
	return deployment.Spec.Template, nil
}

// detectInstallMethod infers the installation method from operator labels.
// Works for both StatefulSet (helm/yaml) and Deployment (olm) label sets.
func detectInstallMethod(lbls map[string]string) string {
	if chart, ok := lbls["helm.sh/chart"]; ok && strings.Contains(chart, "eck-operator") {
		return "helm"
	}
	if _, ok := lbls["control-plane"]; ok {
		return "yaml"
	}
	if _, ok := lbls["olm.owner"]; ok {
		return "olm"
	}
	return "unknown"
}

// detectManagedNamespaces reads the operator's managed namespaces from args, env vars, or config file,
// following Viper's precedence: CLI arg > env var > config file.
func detectManagedNamespaces(c kubernetes.Interface, namespace string, podTemplate corev1.PodTemplateSpec) ManagedNamespaces {
	for _, container := range podTemplate.Spec.Containers {
		if container.Name != "manager" {
			continue
		}

		if val := extractFlagValue(container.Args, "--namespaces"); val != "" {
			return ManagedNamespaces{All: false, Static: splitCSV(val)}
		}

		for _, env := range container.Env {
			if env.Name != "NAMESPACES" {
				continue
			}
			if env.Value != "" {
				return ManagedNamespaces{All: false, Static: splitCSV(env.Value)}
			}
			if val := resolveEnvValueFromFieldRef(podTemplate.ObjectMeta, env.ValueFrom); val != "" {
				return ManagedNamespaces{All: false, Static: splitCSV(val)}
			}
		}

		configPath := extractFlagValue(container.Args, "--config")
		if configPath == "" {
			break
		}
		cmName, dataKey := findConfigMapForPath(podTemplate.Spec, container, configPath)
		if cmName == "" || dataKey == "" {
			logger.Printf("WARNING: operator in namespace %q uses --config=%s but the config source could not "+
				"be traced to a ConfigMap (may be mounted from a Secret); managed namespaces reported as all",
				namespace, configPath)
			break
		}
		cm, err := c.CoreV1().ConfigMaps(namespace).Get(context.Background(), cmName, metav1.GetOptions{})
		if err != nil {
			logger.Printf("WARNING: operator in namespace %q: could not read ConfigMap %q: %v; managed "+
				"namespaces reported as all", namespace, cmName, err)
			break
		}
		ns, err := parseManagedNamespacesFromConfigMap(cm.Data, dataKey)
		if err != nil {
			logger.Printf("WARNING: operator in namespace %q: could not parse ConfigMap %q key %q: %v; managed "+
				"namespaces reported as all", namespace, cmName, dataKey, err)
			break
		}
		if ns != nil {
			return *ns
		}
		break
	}
	return ManagedNamespaces{All: true}
}

// extractFlagValue finds the value of --flag value or --flag=value in a list of args.
func extractFlagValue(args []string, flag string) string {
	for i, arg := range args {
		if v, ok := strings.CutPrefix(arg, flag+"="); ok {
			return v
		}
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// findConfigMapForPath traces a mounted config file path back to the ConfigMap name and data key
// that provides it. When the volume uses an items mapping, the key is resolved via the item whose
// path matches; otherwise the filename relative to the mount point is used as the key.
func findConfigMapForPath(podSpec corev1.PodSpec, container corev1.Container, configPath string) (cmName, dataKey string) {
	var mountName, mountPath, relPath string
	for _, vm := range container.VolumeMounts {
		// SubPath mounts a single file: mountPath IS the file path, so require an exact match.
		if vm.SubPath != "" && configPath == vm.MountPath {
			mountName = vm.Name
			mountPath = vm.MountPath
			relPath = vm.SubPath
			break
		}
		if vm.SubPath == "" && (configPath == vm.MountPath || strings.HasPrefix(configPath, strings.TrimSuffix(vm.MountPath, "/")+"/")) {
			mountName = vm.Name
			mountPath = vm.MountPath
			break
		}
	}
	if mountName == "" {
		return "", ""
	}
	if relPath == "" {
		relPath = strings.TrimPrefix(configPath, strings.TrimSuffix(mountPath, "/")+"/")
	}
	for _, vol := range podSpec.Volumes {
		if vol.Name != mountName || vol.ConfigMap == nil {
			continue
		}
		if len(vol.ConfigMap.Items) == 0 {
			return vol.ConfigMap.Name, relPath
		}
		for _, item := range vol.ConfigMap.Items {
			if item.Path == relPath {
				return vol.ConfigMap.Name, item.Key
			}
		}
		return "", ""
	}
	return "", ""
}

type eckConfig struct {
	Namespaces        any                   `json:"namespaces"`
	NamespaceSelector *metav1.LabelSelector `json:"namespace-selector"`
}

// resolveEnvValueFromFieldRef resolves a ValueFrom annotation field ref against the pod template
// metadata. OLM injects olm.targetNamespaces into the Deployment's pod template annotations, so
// the value is available without querying running pods.
func resolveEnvValueFromFieldRef(podMeta metav1.ObjectMeta, src *corev1.EnvVarSource) string {
	if src == nil || src.FieldRef == nil {
		return ""
	}
	annotationKey := annotationKeyFromFieldPath(src.FieldRef.FieldPath)
	if annotationKey == "" {
		return ""
	}
	return podMeta.Annotations[annotationKey]
}

// annotationKeyFromFieldPath extracts the annotation key from a fieldPath of the form
// metadata.annotations['<key>'], returning "" for any other form.
func annotationKeyFromFieldPath(fieldPath string) string {
	trimmed, ok := strings.CutPrefix(fieldPath, "metadata.annotations['")
	if !ok {
		return ""
	}
	key, ok := strings.CutSuffix(trimmed, "']")
	if !ok {
		return ""
	}
	return key
}

// parseManagedNamespacesFromConfigMap parses the operator config from the given ConfigMap data key.
// Returns (nil, nil) when the config is valid but contains no namespace restriction.
func parseManagedNamespacesFromConfigMap(data map[string]string, key string) (*ManagedNamespaces, error) {
	content, ok := data[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found in ConfigMap data", key)
	}
	var cfg eckConfig
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if cfg.NamespaceSelector != nil &&
		(len(cfg.NamespaceSelector.MatchLabels) > 0 || len(cfg.NamespaceSelector.MatchExpressions) > 0) {
		return &ManagedNamespaces{All: false, Selector: cfg.NamespaceSelector}, nil
	}
	nss, err := parseNamespacesValue(cfg.Namespaces)
	if err != nil {
		return nil, err
	}
	if len(nss) > 0 {
		return &ManagedNamespaces{All: false, Static: nss}, nil
	}
	return nil, nil
}

// parseNamespacesValue handles both []any (YAML sequence) and string (comma-separated).
func parseNamespacesValue(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	switch val := v.(type) {
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				result = append(result, strings.TrimSpace(s))
			}
		}
		return result, nil
	case string:
		return splitCSV(val), nil
	default:
		return nil, fmt.Errorf("namespaces field has unexpected type %T", v)
	}
}

// splitCSV splits a comma-separated list, trimming whitespace from each entry.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// warnMissingNamespaces logs a warning for each managed namespace not covered by collectedNamespaces.
func warnMissingNamespaces(c *kubernetes.Clientset, infos []OperatorInfo, collectedNamespaces []string) {
	collectedNamespacesSet := sets.New(collectedNamespaces...)

	for _, info := range infos {
		mn := info.ManagedNamespaces
		switch {
		case mn.All:
			logger.Printf("WARNING: Operator in namespace %q manages all namespaces; the diagnostic only collects resources from %v. Specify additional workload namespaces via --resources-namespaces to broaden coverage.", info.Namespace, collectedNamespacesSet.UnsortedList())
		case mn.Selector != nil:
			managed, err := namespacesMatchingSelector(c, mn.Selector)
			if err != nil {
				logger.Printf("WARNING: Could not evaluate namespace selector for operator in %q: %v", info.Namespace, err)
				continue
			}
			if missing := missingNamespaces(managed, collectedNamespacesSet); len(missing) > 0 {
				logger.Printf("WARNING: Operator in namespace %q manages namespaces %v (via selector) which are not included in this diagnostic. Use --resources-namespaces to include them.", info.Namespace, missing)
			}
		case len(mn.Static) > 0:
			if missing := missingNamespaces(mn.Static, collectedNamespacesSet); len(missing) > 0 {
				logger.Printf("WARNING: Operator in namespace %q manages namespaces %v which are not included in this diagnostic. Use --resources-namespaces to include them.", info.Namespace, missing)
			}
		}
	}
}

// namespacesMatchingSelector lists cluster namespaces whose labels match the given selector.
func namespacesMatchingSelector(c *kubernetes.Clientset, selector *metav1.LabelSelector) ([]string, error) {
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}
	nsList, err := c.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{
		LabelSelector: sel.String(),
	})
	if err != nil {
		return nil, err
	}
	result := make([]string, len(nsList.Items))
	for i, ns := range nsList.Items {
		result[i] = ns.Name
	}
	return result, nil
}

// missingNamespaces returns entries from managed that are absent from collected.
func missingNamespaces(managed []string, collected sets.Set[string]) []string {
	var missing []string
	for _, ns := range managed {
		if _, ok := collected[ns]; !ok {
			missing = append(missing, ns)
		}
	}
	return missing
}

// writeOperatorsJSON serialises a slice of OperatorInfo as a JSON array to w.
func writeOperatorsJSON(w io.Writer, infos []OperatorInfo) error {
	return json.NewEncoder(w).Encode(infos)
}
