// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package filters

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		filters []string
		want    Filters
		wantErr bool
	}{
		{
			name:    "empty/no filter is valid",
			filters: []string{},
			want:    Filters{byType: map[string][]Filter{}},
			wantErr: false,
		},
		{
			name:    "valid name and agent type is valid",
			filters: []string{"agent=myagent"},
			want: Filters{byType: map[string][]Filter{
				"agent": {{
					Type: "agent",
					Name: "myagent",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "agent")).
						Add(mustParseRequirement("agent.k8s.elastic.co/name", "myagent")),
				}},
			}},
			wantErr: false,
		},
		{
			name:    "valid name and apm type is valid",
			filters: []string{"apm=myapm"},
			want: Filters{byType: map[string][]Filter{
				"apm": {{
					Type: "apm",
					Name: "myapm",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "apm")).
						Add(mustParseRequirement("apm.k8s.elastic.co/name", "myapm")),
				}},
			}},
			wantErr: false,
		},
		{
			name:    "valid name and beat type is valid",
			filters: []string{"beat=mybeat"},
			want: Filters{byType: map[string][]Filter{
				"beat": {{
					Type: "beat",
					Name: "mybeat",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "beat")).
						Add(mustParseRequirement("beat.k8s.elastic.co/name", "mybeat")),
				}},
			}},
			wantErr: false,
		},
		{
			name:    "valid name and elasticsearch type is valid",
			filters: []string{"elasticsearch=mycluster"},
			want: Filters{byType: map[string][]Filter{
				"elasticsearch": {{
					Type: "elasticsearch",
					Name: "mycluster",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "elasticsearch")).
						Add(mustParseRequirement("elasticsearch.k8s.elastic.co/cluster-name", "mycluster")),
				}},
			}},
			wantErr: false,
		},
		{
			name:    "valid name and enterprisesearch type is valid",
			filters: []string{"enterprisesearch=mycluster"},
			want: Filters{byType: map[string][]Filter{
				"enterprisesearch": {{
					Type: "enterprisesearch",
					Name: "mycluster",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "enterprisesearch")).
						Add(mustParseRequirement("enterprisesearch.k8s.elastic.co/name", "mycluster")),
				}},
			}},
			wantErr: false,
		},
		{
			name:    "valid name and kibana type is valid",
			filters: []string{"kibana=mykb"},
			want: Filters{byType: map[string][]Filter{
				"kibana": {{
					Type: "kibana",
					Name: "mykb",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "kibana")).
						Add(mustParseRequirement("kibana.k8s.elastic.co/name", "mykb")),
				}},
			}},
			wantErr: false,
		},
		{
			name:    "valid name and maps type is valid",
			filters: []string{"maps=mymaps"},
			want: Filters{byType: map[string][]Filter{
				"maps": {{
					Type: "maps",
					Name: "mymaps",
					Selector: labels.NewSelector().
						Add(mustParseRequirement("common.k8s.elastic.co/type", "maps")).
						Add(mustParseRequirement("maps.k8s.elastic.co/name", "mymaps")),
				}},
			}},
			wantErr: false,
		},
		{
			name:    "multiple valid filters return correctly",
			filters: []string{"elasticsearch=mycluster", "kibana=my-kb", "agent=my-agent"},
			want: Filters{byType: map[string][]Filter{
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
			}},
			wantErr: false,
		},
		{
			name:    "invalid type is invalid",
			filters: []string{"type=invalid"},
			want:    Filters{byType: map[string][]Filter{}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(tt.filters)
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

func TestFilters_Matches(t *testing.T) {
	set := labels.Set{
		"common.k8s.elastic.co/type":                "elasticsearch",
		"elasticsearch.k8s.elastic.co/cluster-name": "my-cluster",
	}
	selector := labels.SelectorFromSet(set)
	for k, v := range set {
		req, _ := labels.NewRequirement(k, selection.Equals, []string{v})
		selector.Add(*req)
	}
	defaultFilters := Filters{
		byType: map[string][]Filter{
			"elasticsearch": {{
				Type:     "elasticsearch",
				Name:     "my-cluster",
				Selector: selector,
			}},
		},
	}
	tests := []struct {
		name      string
		labels    map[string]string
		filterMap map[string][]Filter
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
			filterMap: defaultFilters.byType,
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
			filterMap: defaultFilters.byType,
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
			filterMap: nil,
			want:      true,
		},
		{
			name: "with selector matches",
			labels: map[string]string{
				"control-plane": "elastic-operator",
			},
			filterMap: defaultFilters.byType,
			selectors: []labels.Selector{
				labels.Set{
					"control-plane": "elastic-operator",
				}.AsSelector(),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Filters{
				byType:    tt.filterMap,
				selectors: tt.selectors,
			}
			if got := f.Matches(tt.labels); got != tt.want {
				t.Errorf("Filters.Matches() = %v, want %v", got, tt.want)
			}
		})
		t.Run(tt.name, func(t *testing.T) {
			pairs := []string{}
			for key, value := range tt.labels {
				pairs = append(pairs, fmt.Sprintf("%s=%s", key, value))
			}
			labelsString := strings.Join(pairs, ",") // KEY_1=VAL_1,KEY_2=VAL_2

			f := Filters{
				byType: tt.filterMap,
			}
			if got := f.MatchesAgainstString(labelsString); got != tt.want {
				t.Errorf("Filters.MatchesAgainstString() = %v, want %v", got, tt.want)
			}
		})
	}
}
