// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"context"
	"regexp"
	"strings"

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

// detectECKVersion tries to detect the ECK version by inspecting the ECK operator stateful set.
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

	// try to parse the Docker image tag for older versions of ECK
	for _, container := range statefulSet.Spec.Template.Spec.Containers {
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
