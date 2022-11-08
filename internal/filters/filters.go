// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package filters

import (
	"fmt"
	"strings"

	"k8s.io/utils/strings/slices"
)

var (
	// ValidTypes are the valid types of Elastic resources that are supported by the filtering system.
	ValidTypes              = []string{"agent", "apm", "beat", "elasticsearch", "enterprisesearch", "kibana", "maps"}
	elasticTypeKey          = "common.k8s.elastic.co/type"
	elasticsearchNameFormat = "%s.k8s.elastic.co/cluster-name"
	elasticNameFormat       = "%s.k8s.elastic.co/name"
)

// Filter is a type + name filter that translates into a labelSelector that is applied when querying for Kubernetes resources.
// Both type and name are required at this time.
//
// examples supported:
// name=mycluster,type=elasticsearch
// name=mykb,type=kibana
type Filter struct {
	Type          string
	Name          string
	LabelSelector string
}

// New returns a new filter, given a slice of key=value pairs,
// runs validation on the given slice, and returns an error
// if the given key=value pairs are invalid.
//
// source example:
// []string{"name=mycluster", "type=elasticsearch"}
func New(source []string) (Filter, error) {
	return parse(source)
}

func parse(source []string) (Filter, error) {
	filter := Filter{}
	if len(source) == 0 {
		return filter, nil
	}
	var typ, name string
	for _, fltr := range source {
		filterSlice := strings.Split(fltr, "=")
		if len(filterSlice) != 2 {
			return filter, fmt.Errorf("invalid filter: %s", fltr)
		}
		k, v := filterSlice[0], filterSlice[1]
		switch k {
		case "type":
			{
				if typ != "" {
					return filter, fmt.Errorf("only a single type filter is supported")
				}
				typ = v
			}
		case "name":
			{
				if name != "" {
					return filter, fmt.Errorf("only a single name filter is supported")
				}
				name = v
			}
		default:
			return filter, fmt.Errorf("invalid filter key: %s. Only 'type', and 'name' are supported", k)
		}
	}
	if typ == "" {
		return filter, fmt.Errorf("invalid filter: missing 'type'")
	}
	if err := validateType(typ); err != nil {
		return filter, err
	}
	if name == "" {
		return filter, fmt.Errorf("invalid filter: missing 'name'")
	}
	filter.Type, filter.Name = typ, name
	filter.LabelSelector = convertFilterToLabelSelector(filter.Type, filter.Name)
	return filter, nil
}

func validateType(typ string) error {
	if !slices.Contains(ValidTypes, typ) {
		return fmt.Errorf("invalid filter type: %s, supported types: %v", typ, ValidTypes)
	}
	return nil
}

// convertFilterToLabelSelector will convert a given Elastic custom resource type,
// and name into a valid Kubernetes labelSelector.
func convertFilterToLabelSelector(typ, name string) string {
	format := "common.k8s.elastic.co/type=%s,%s.k8s.elastic.co/%s=%s"
	nameAttr := name
	if typ == "elasticsearch" {
		nameAttr = "cluster-name"
	}
	return fmt.Sprintf(format, typ, typ, nameAttr, name)
}
