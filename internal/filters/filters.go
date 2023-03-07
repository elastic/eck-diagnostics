// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package filters

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/strings/slices"
)

var (
	// ValidTypes are the valid types of Elastic resources that are supported by the filtering system.
	ValidTypes = []string{"agent", "apm", "beat", "elasticsearch", "enterprisesearch", "kibana", "maps"}
	Empty      = Filters{}
)

// Filters contains a Filter map for each Elastic type given in the filter "source".
type Filters struct {
	byType   map[string][]Filter
	selector labels.Selector
}

func (f Filters) WithSelector(selector labels.Selector) Filters {
	f.selector = selector
	return f
}

// Empty returns if there are no defined filters.
func (f Filters) Empty() bool {
	return len(f.byType) == 0
}

// Matches will determine if the given labels matches any of the
// Filter's label selectors.
func (f Filters) Matches(lbls map[string]string) bool {
	// empty set of filters always matches.
	if f.Empty() {
		return true
	}
	for _, fs := range f.byType {
		for _, filter := range fs {
			if filter.Selector.Matches(labels.Set(lbls)) {
				return true
			}
		}
	}
	if f.selector != nil {
		return f.selector.Matches(labels.Set(lbls))
	}
	return false
}

// Contains will check if any of the filters of named type 'typ'
// contain a filter for an object named 'name'.
func (f Filters) Contains(name, typ string) bool {
	var (
		ok          bool
		typeFilters []Filter
	)
	if typeFilters, ok = f.byType[typ]; !ok {
		return false
	}
	for _, filter := range typeFilters {
		if filter.Name == name {
			return true
		}
	}
	return false
}

// Filter contains a type + name (type = elasticsearch, name = my-cluster)
// and a label selector to easily determine if any queried resource's labels match
// a given filter.
type Filter struct {
	Type     string
	Name     string
	Selector labels.Selector
}

// New returns a new set of filters, given a slice of key=value pairs,
// parses and validates the given filters, and returns an error
// if the given key=value pairs are invalid.
//
// source example:
// []string{"elasticsearch=my-cluster", "kibana=my-kb"}
func New(source []string) (Filters, error) {
	return parse(source)
}

// parse will validate the given source filters, and for each
// filter type, will create a Filter with a label selector,
// returning the set of Filters, and any errors encountered.
func parse(source []string) (Filters, error) {
	filters := Filters{
		byType: map[string][]Filter{},
	}
	if len(source) == 0 {
		return filters, nil
	}
	var typ, name string
	for _, fltr := range source {
		filterSlice := strings.Split(fltr, "=")
		if len(filterSlice) != 2 {
			return filters, fmt.Errorf("invalid filter: %s", fltr)
		}
		typ, name = filterSlice[0], filterSlice[1]
		if err := validateType(typ); err != nil {
			return filters, err
		}
		if _, ok := filters.byType[typ]; ok {
			return filters, fmt.Errorf("invalid filter: %s: multiple filters for the same type (%s) are not supported", fltr, typ)
		}
		selector := processSelector(typ, name)
		filters.byType[typ] = append(filters.byType[typ], Filter{
			Name:     name,
			Type:     typ,
			Selector: selector,
		})
	}
	return filters, nil
}

func validateType(typ string) error {
	if !slices.Contains(ValidTypes, typ) {
		return fmt.Errorf("invalid filter type: %s, supported types: %v", typ, ValidTypes)
	}
	return nil
}

// processSelector will create a label set for a given type+name pair,
// create a selector from that label set, and add label requirements
// to the selector for each key/value in the label set, returning the selector
// and any errors in processing.
//
// Having a labels.Selector on the Filter allows an easy match operation:
//
//	Selector.Match(LabelSet)
//
// against any runtime object's labels.
func processSelector(typ, name string) labels.Selector {
	nameAttr := "name"
	if typ == "elasticsearch" {
		nameAttr = "cluster-name"
	}
	set := labels.Set{
		"common.k8s.elastic.co/type":                       typ,
		fmt.Sprintf("%s.k8s.elastic.co/%s", typ, nameAttr): name,
	}
	return labels.SelectorFromValidatedSet(set)
}
