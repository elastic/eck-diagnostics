// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
)

// fallbackMaxVersion if we cannot detect version assume a max version to turn on all features
var fallbackMaxVersion = version.MustParseSemantic("999.9999.9999")

// logVersion log the given version or unknown if we only have the fallback.
func logVersion(v *version.Version) {
	s := v.String()
	if v == fallbackMaxVersion {
		s = "unknown"
	}
	logger.Printf("ECK version is %s\n", s)
}

// detectECKVersion tries to detect the ECK version by inspecting the ECK operator stateful set or deployment.
func detectECKVersion(c *kubernetes.Clientset, namespace, userSpecifiedVersion string) *version.Version {
	if userSpecifiedVersion != "" {
		parsed, err := version.ParseSemantic(userSpecifiedVersion)
		if err != nil {
			logger.Println(err.Error())
			return fallbackMaxVersion
		}
		return parsed
	}

	statefulSet, err := findOperatorStatefulSet(c, namespace)
	if err != nil {
		logger.Println(err.Error())
		return fallbackMaxVersion
	}

	// we were not able to find a StatefulSet we might be dealing with an ECK operator deployed via OLM
	if statefulSet == nil {
		return extractVersionFromDeployment(c, namespace)
	}

	// since version 1.3 ECK uses standard labels
	versionLabel := statefulSet.Labels["app.kubernetes.io/version"]
	parsed, err := version.ParseSemantic(versionLabel)
	if err == nil {
		return parsed
	}

	return extractVersionFromContainers(statefulSet.Spec.Template.Spec.Containers)
}

func findOperatorStatefulSet(c *kubernetes.Clientset, namespace string) (*appsv1.StatefulSet, error) {
	// we use the control-plane label since ECK version 1.0
	ssets, err := c.AppsV1().StatefulSets(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "control-plane=elastic-operator"})
	if err != nil {
		return nil, err
	}

	// there is the possibility that users have deployed multiple ECK operators into the same namespace which we are
	// ignoring here by assuming exactly one
	if len(ssets.Items) == 1 {
		return &ssets.Items[0], nil
	}

	// when deployed via Helm we don't have the control-plan label
	ssets, err = c.AppsV1().StatefulSets(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app.kubernetes.io/managed-by=Helm"})
	if err != nil {
		return nil, err
	}

	for _, set := range ssets.Items {
		// unfortunately the chart name also encodes the version which is what we are trying to find out
		// that's why  we are doing a substring match here
		if chart, ok := set.Labels["helm.sh/chart"]; ok && strings.Contains(chart, "eck-operator") {
			return &set, nil
		}
	}
	return nil, nil
}

// extractVersionFromContainers tries to find the operator container in the list of containers to extract version information.
func extractVersionFromContainers(containers []corev1.Container) *version.Version {
	// try to parse the Docker image tag for older versions of ECK
	for _, container := range containers {
		// likely but not certain that this is the operator container
		if strings.Contains(container.Image, "eck-operator") {
			parsed, err := extractVersionFromDockerImage(container.Image)
			if err != nil {
				logger.Printf(err.Error())
				return fallbackMaxVersion
			}
			return parsed
		}
	}
	return fallbackMaxVersion
}

// extractVersionFromDeployment tries to extract version information from a deployment as it is typically used in ECK installations via OLM.
func extractVersionFromDeployment(c *kubernetes.Clientset, namespace string) *version.Version {
	deployment, err := c.AppsV1().Deployments(namespace).Get(context.Background(), "elastic-operator", metav1.GetOptions{})
	if err != nil {
		logger.Println(fmt.Errorf("operator statefulset not found, checking for OLM deployment but failed: %w", err).Error())
		return fallbackMaxVersion
	}
	v, err := extractVersionFromOLMMetadata(deployment.Labels)
	if err != nil {
		logger.Println("ECK operator not found in OLM metadata checking container image as last resort")
		return extractVersionFromContainers(deployment.Spec.Template.Spec.Containers)
	}
	return v
}

func extractVersionFromOLMMetadata(labels map[string]string) (*version.Version, error) {
	if val, ok := labels["olm.owner"]; ok {
		if _, v, found := strings.Cut(val, "."); found {
			return version.ParseSemantic(v)
		}
	}
	return nil, errors.New("no OLM metadata found")
}

// extractVersionFromDockerImage parses the version tag out of the given Docker image identifier.
func extractVersionFromDockerImage(image string) (*version.Version, error) {
	regex := regexp.MustCompile(":([^@]+)")
	matches := regex.FindAllStringSubmatch(image, -1)
	if len(matches) == 1 && len(matches[0]) == 2 {
		return version.ParseSemantic(matches[0][1])
	}
	return fallbackMaxVersion, nil
}

// max returns the maximum of the given versions.
func max(versions []*version.Version) *version.Version {
	if len(versions) == 0 {
		return fallbackMaxVersion
	}
	res := versions[0]
	for _, v := range versions[1:] {
		if res.LessThan(v) {
			res = v
		}
	}
	return res
}
