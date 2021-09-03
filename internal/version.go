// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package internal

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	statefulSet, err := c.AppsV1().StatefulSets(namespace).Get(context.Background(), "elastic-operator", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return extractVersionFromDeployment(c, namespace)
	}
	if err != nil {
		logger.Println(err.Error())
		return fallbackMaxVersion
	}

	// since version 1.3 ECK uses standard labels
	versionLabel := statefulSet.Labels["app.kubernetes.io/version"]
	parsed, err := version.ParseSemantic(versionLabel)
	if err == nil {
		return parsed
	}

	return extractVersionFromContainers(statefulSet.Spec.Template.Spec.Containers)
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
	return extractVersionFromContainers(deployment.Spec.Template.Spec.Containers)
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
