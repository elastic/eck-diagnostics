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
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
)

var maxVersion = version.MustParseSemantic("999.9999.9999") // if we cannot detect version assume a max version to turn on all features

func detectECKVersion(c *kubernetes.Clientset, namespace, userSpecifiedVersion string) *version.Version {
	if userSpecifiedVersion != "" {
		parsed, err := version.ParseSemantic(userSpecifiedVersion)
		if err != nil {
			logger.Println(err.Error())
			return maxVersion
		}
		return parsed
	}
	statefulSet, err := c.AppsV1().StatefulSets(namespace).Get(context.Background(), "elastic-operator", metav1.GetOptions{})
	if err != nil {
		logger.Println(err.Error())
		return maxVersion
	}
	for _, container := range statefulSet.Spec.Template.Spec.Containers {
		// likely but not certain that this is the operator container
		if strings.Contains(container.Image, "eck-operator") {
			parsed, err := extractVersionFromDockerImage(container.Image)
			if err != nil {
				logger.Printf(err.Error())
				return maxVersion
			}
			return parsed
		}
	}
	return maxVersion
}

func extractVersionFromDockerImage(image string) (*version.Version, error) {
	regex := regexp.MustCompile(":([^@]+)")
	matches := regex.FindAllStringSubmatch(image, -1)
	if len(matches) == 1 && len(matches[0]) == 2 {
		return version.ParseSemantic(matches[0][1])
	}
	return maxVersion, nil
}

func max(versions []*version.Version) *version.Version {
	if len(versions) == 0 {
		return maxVersion
	}
	res := versions[0]
	for _, v := range versions[1:] {
		if res.LessThan(v) {
			res = v
		}
	}
	return res
}
