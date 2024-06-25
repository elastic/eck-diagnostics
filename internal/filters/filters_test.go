// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package filters

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilerrs "k8s.io/apimachinery/pkg/util/errors"
)

func TestNewTypeFilter(t *testing.T) {
	tests := []struct {
		name    string
		filters []string
		want    TypeFilters
		wantErr bool
	}{
		{
			name:    "empty/no filter is valid",
			filters: []string{},
			want:    make(TypeFilters),
			wantErr: false,
		},
		{
			name:    "valid name and agent type is valid",
			filters: []string{"agent=myagent"},
			want: TypeFilters(map[string][]TypeFilter{
				"agent": {{
					Type: "agent",
					Name: "myagent",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "agent")).
						Add(mustParseRequirement("agent.k8s.elastic.co/name", "myagent")),
				}},
			}),
			wantErr: false,
		},
		{
			name:    "valid name and apm type is valid",
			filters: []string{"apm=myapm"},
			want: TypeFilters(map[string][]TypeFilter{
				"apm": {{
					Type: "apm",
					Name: "myapm",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "apm")).
						Add(mustParseRequirement("apm.k8s.elastic.co/name", "myapm")),
				}},
			}),
			wantErr: false,
		},
		{
			name:    "valid name and beat type is valid",
			filters: []string{"beat=mybeat"},
			want: TypeFilters(map[string][]TypeFilter{
				"beat": {{
					Type: "beat",
					Name: "mybeat",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "beat")).
						Add(mustParseRequirement("beat.k8s.elastic.co/name", "mybeat")),
				}},
			}),
			wantErr: false,
		},
		{
			name:    "valid name and elasticsearch type is valid",
			filters: []string{"elasticsearch=mycluster"},
			want: TypeFilters(map[string][]TypeFilter{
				"elasticsearch": {{
					Type: "elasticsearch",
					Name: "mycluster",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "elasticsearch")).
						Add(mustParseRequirement("elasticsearch.k8s.elastic.co/cluster-name", "mycluster")),
				}},
			}),
			wantErr: false,
		},
		{
			name:    "valid name and enterprisesearch type is valid",
			filters: []string{"enterprisesearch=mycluster"},
			want: TypeFilters(map[string][]TypeFilter{
				"enterprisesearch": {{
					Type: "enterprisesearch",
					Name: "mycluster",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "enterprisesearch")).
						Add(mustParseRequirement("enterprisesearch.k8s.elastic.co/name", "mycluster")),
				}},
			}),
			wantErr: false,
		},
		{
			name:    "valid name and kibana type is valid",
			filters: []string{"kibana=mykb"},
			want: TypeFilters(map[string][]TypeFilter{
				"kibana": {{
					Type: "kibana",
					Name: "mykb",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "kibana")).
						Add(mustParseRequirement("kibana.k8s.elastic.co/name", "mykb")),
				}},
			}),
			wantErr: false,
		},
		{
			name:    "valid name and maps type is valid",
			filters: []string{"maps=mymaps"},
			want: TypeFilters(map[string][]TypeFilter{
				"maps": {{
					Type: "maps",
					Name: "mymaps",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "maps")).
						Add(mustParseRequirement("maps.k8s.elastic.co/name", "mymaps")),
				}},
			}),
			wantErr: false,
		},
		{
			name:    "multiple valid filters return correctly",
			filters: []string{"elasticsearch=mycluster", "kibana=my-kb", "agent=my-agent"},
			want: TypeFilters(map[string][]TypeFilter{
				"agent": {{
					Type: "agent",
					Name: "my-agent",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "agent")).
						Add(mustParseRequirement("agent.k8s.elastic.co/name", "my-agent")),
				}},
				"elasticsearch": {{
					Type: "elasticsearch",
					Name: "mycluster",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "elasticsearch")).
						Add(mustParseRequirement("elasticsearch.k8s.elastic.co/cluster-name", "mycluster")),
				}},
				"kibana": {{
					Type: "kibana",
					Name: "my-kb",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "kibana")).
						Add(mustParseRequirement("kibana.k8s.elastic.co/name", "my-kb")),
				}},
			}),
			wantErr: false,
		},
		{
			name:    "invalid type is invalid",
			filters: []string{"type=invalid"},
			want:    Empty,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewTypeFilter(tt.filters)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("New() = diff: %s", cmp.Diff(got, tt.want))
			}
		})
	}
}

func mustParseRequirement(k, v string) labels.Requirement {
	req, err := labels.NewRequirement(k, selection.Equals, []string{v})
	if err != nil {
		panic(fmt.Sprintf("failed creating label requirement from key: %s, value: %s", k, v))
	}
	return *req
}

func TestTypeFilters_Matches(t *testing.T) {
	set := labels.Set{
		"common.k8s.elastic.co/type":                "elasticsearch",
		"elasticsearch.k8s.elastic.co/cluster-name": "my-cluster",
	}
	selector := labels.SelectorFromSet(set)
	for k, v := range set {
		req, _ := labels.NewRequirement(k, selection.Equals, []string{v})
		selector.Add(*req)
	}
	defaultTypeFilters := TypeFilters(
		map[string][]TypeFilter{
			"elasticsearch": {{
				Type:     "elasticsearch",
				Name:     "my-cluster",
				Selector: selector,
			}},
		},
	)
	tests := []struct {
		name      string
		labels    map[string]string
		filterMap Filters
		selectors []labels.Selector
		want      bool
	}{
		{
			name: "default elasticsearch labels should match elasticsearch filter selector",
			labels: map[string]string{
				"common.k8s.elastic.co/type":                              "elasticsearch",
				"controller-revision-hash":                                "my-cluster-es-default-64ffc4847c",
				"elasticsearch.k8s.elastic.co/cluster-name":               "my-cluster",
				"elasticsearch.k8s.elastic.co/http-scheme":                "https",
				"elasticsearch.k8s.elastic.co/node-data":                  "true",
				"elasticsearch.k8s.elastic.co/node-data_cold":             "true",
				"elasticsearch.k8s.elastic.co/node-data_content":          "true",
				"elasticsearch.k8s.elastic.co/node-data_frozen":           "true",
				"elasticsearch.k8s.elastic.co/node-data_hot":              "true",
				"elasticsearch.k8s.elastic.co/node-data_warm":             "true",
				"elasticsearch.k8s.elastic.co/node-ingest":                "true",
				"elasticsearch.k8s.elastic.co/node-master":                "true",
				"elasticsearch.k8s.elastic.co/node-ml":                    "true",
				"elasticsearch.k8s.elastic.co/node-remote_cluster_client": "true",
				"elasticsearch.k8s.elastic.co/node-transform":             "true",
				"elasticsearch.k8s.elastic.co/node-voting_only":           "false",
				"elasticsearch.k8s.elastic.co/statefulset-name":           "my-cluster-es-default",
				"elasticsearch.k8s.elastic.co/version":                    "8.2.3",
				"statefulset.kubernetes.io/pod-name":                      "my-cluster-es-default-0",
			},
			filterMap: defaultTypeFilters,
			want:      true,
		},
		{
			name: "agent pod labels should not match elasticsearch filter selector",
			labels: map[string]string{
				"agent.k8s.elastic.co/name":    "fleet-server",
				"agent.k8s.elastic.co/version": "8.2.3",
				"common.k8s.elastic.co/type":   "agent",
				"pod-template-hash":            "7cbfdc4d78",
			},
			filterMap: defaultTypeFilters,
			want:      false,
		},
		{
			name: "empty filters matches",
			labels: map[string]string{
				"agent.k8s.elastic.co/name":    "fleet-server",
				"agent.k8s.elastic.co/version": "8.2.3",
				"common.k8s.elastic.co/type":   "agent",
				"pod-template-hash":            "7cbfdc4d78",
			},
			filterMap: Empty,
			want:      true,
		},
		{
			name: "from selector matches",
			labels: map[string]string{
				"control-plane": "elastic-operator",
			},
			filterMap: LabelFilter([]labels.Selector{labels.Set{"control-plane": "elastic-operator"}.AsSelector()}),
			want:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.filterMap
			if got := f.Matches(tt.labels); got != tt.want {
				t.Errorf("TypeFilters.Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

const none = "_"

var (
	nothing = TypeFilter{
		Type:     none,
		Name:     none,
		Selector: labels.Nothing(),
	}
	typeFixture          = "elasticsearch"
	nameFixture          = "es"
	nothingFilterFixture = TypeFilters(map[string][]TypeFilter{none: {nothing}})
	esFilterFixture      = TypeFilters(map[string][]TypeFilter{"elasticsearch": {TypeFilter{
		Type:     typeFixture,
		Name:     nameFixture,
		Selector: labels.NewSelector().Add(mustParseRequirement("common.k8s.elastic.co/type", "elasticsearch")),
	}}})
)

func TestAnd(t *testing.T) {
	type args struct {
		fs []Filters
	}
	tests := []struct {
		name         string
		args         args
		labels       map[string]string
		wantMatches  bool
		wantContains bool
		wantEmpty    bool
	}{
		{
			name:         "empty",
			args:         args{},
			wantMatches:  true,
			wantContains: true,
			wantEmpty:    true,
		},
		{
			name: "nothing",
			args: args{
				fs: []Filters{nothingFilterFixture},
			},
			wantMatches:  false,
			wantContains: false,
			wantEmpty:    false,
		},
		{
			name: "nothing and something is still nothing",
			args: args{fs: []Filters{
				nothingFilterFixture,
				esFilterFixture,
			}},
			labels: map[string]string{
				"common.k8s.elastic.co/type": "elasticsearch",
			},
			wantMatches:  false,
			wantContains: false,
			wantEmpty:    false,
		},
		{
			name: "refine filters by and'ing: match case",
			args: args{fs: []Filters{
				LabelFilter([]labels.Selector{labels.NewSelector().Add(mustParseRequirement("my-label", "value"))}),
				esFilterFixture,
			}},
			labels: map[string]string{
				"common.k8s.elastic.co/type": "elasticsearch",
				"my-label":                   "value",
			},
			wantMatches:  true,
			wantContains: false,
			wantEmpty:    false,
		},
		{
			name: "refine filters by and'ing: reject case",
			args: args{fs: []Filters{
				LabelFilter([]labels.Selector{labels.NewSelector().Add(mustParseRequirement("my-label", "value"))}),
				esFilterFixture,
			}},
			labels: map[string]string{
				"common.k8s.elastic.co/type": "elasticsearch",
				"my-label":                   "other",
			},
			wantMatches:  false,
			wantContains: false,
			wantEmpty:    false,
		},
		{
			name: "contains is also and'ed ",
			args: args{fs: []Filters{
				TypeFilters(map[string][]TypeFilter{typeFixture: {{
					Name:     nameFixture,
					Type:     typeFixture,
					Selector: labels.Nothing(),
				}}}),
				esFilterFixture,
			}},
			labels:       map[string]string{"common.k8s.elastic.co/type": "elasticsearch"},
			wantMatches:  false,
			wantContains: true,
			wantEmpty:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := And(tt.args.fs...)
			require.Equal(t, tt.wantContains, f.Contains(nameFixture, typeFixture), "contains")
			require.Equal(t, tt.wantMatches, f.Matches(tt.labels), "matches")
			require.Equal(t, tt.wantEmpty, f.Empty(), "empty")
		})
	}
}

func TestNewLabelFilter(t *testing.T) {
	type args struct {
		source []string
	}
	tests := []struct {
		name    string
		args    args
		want    Filters
		wantErr int
	}{
		{
			name:    "empty",
			args:    args{},
			want:    Empty,
			wantErr: 0,
		},
		{
			name: "Single selector OK",
			args: args{
				source: []string{"label=value-a"},
			},
			want:    LabelFilter{labels.Set{"label": "value-a"}.AsSelector()},
			wantErr: 0,
		},
		{
			name: "Multiple selectors: OK ",
			args: args{
				source: []string{"label=value-a", "label=value-b"},
			},
			want: LabelFilter{
				labels.Set{"label": "value-a"}.AsSelector(),
				labels.Set{"label": "value-b"}.AsSelector(),
			},
			wantErr: 0,
		},
		{
			name: "Multiple selectors: one error",
			args: args{
				source: []string{"valid=selector", "invalid("},
			},
			want:    Empty,
			wantErr: 1,
		},
		{
			name: "Multiple selectors: multiple errors",
			args: args{
				source: []string{"invalid=(selector", "invalid("},
			},
			want:    Empty,
			wantErr: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewLabelFilter(tt.args.source)
			if (err != nil) != (tt.wantErr > 0) {
				t.Errorf("NewLabelFilter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				var agg utilerrs.Aggregate
				if errors.As(err, &agg) {
					require.Equal(t, tt.wantErr, len(agg.Errors()), "number of expected errors")
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewLabelFilter() = diff: %s", cmp.Diff(got, tt.want))
			}
		})
	}
}

func TestOr(t *testing.T) {
	type args struct {
		fs []Filters
	}
	tests := []struct {
		name         string
		args         args
		labels       map[string]string
		wantMatches  bool
		wantContains bool
		wantEmpty    bool
	}{
		{
			name:         "empty",
			args:         args{},
			wantMatches:  true,
			wantContains: true,
			wantEmpty:    true,
		},
		{
			name: "nothing",
			args: args{
				fs: []Filters{nothingFilterFixture},
			},
			labels:       map[string]string{},
			wantMatches:  false,
			wantContains: false,
			wantEmpty:    false,
		},
		{
			name: "nothing and empty",
			args: args{
				fs: []Filters{nothingFilterFixture, Empty},
			},
			wantMatches:  true, // empty matches everything
			wantContains: true, // empty contains everything
			wantEmpty:    true,
		},
		{
			name: "match something rhs",
			args: args{
				fs: []Filters{
					nothingFilterFixture,
					esFilterFixture,
				},
			},
			labels: map[string]string{
				"common.k8s.elastic.co/type": "elasticsearch",
			},
			wantMatches:  true,
			wantContains: true,
			wantEmpty:    false,
		},
		{
			name: "match something lhs",
			args: args{
				fs: []Filters{
					esFilterFixture,
					nothingFilterFixture,
				},
			},
			labels: map[string]string{
				"common.k8s.elastic.co/type": "elasticsearch",
			},
			wantMatches:  true,
			wantContains: true,
			wantEmpty:    false,
		},
		{
			name: "match something both sides",
			args: args{
				fs: []Filters{
					esFilterFixture,
					LabelFilter{labels.Set{"key": "value"}.AsSelector()},
				},
			},
			labels: map[string]string{
				"key": "value",
			},
			wantMatches:  true,
			wantContains: true,
			wantEmpty:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Or(tt.args.fs...)
			require.Equal(t, tt.wantContains, f.Contains(nameFixture, typeFixture), "contains")
			require.Equal(t, tt.wantMatches, f.Matches(tt.labels), "matches")
			require.Equal(t, tt.wantEmpty, f.Empty(), "empty")
		})
	}
}
