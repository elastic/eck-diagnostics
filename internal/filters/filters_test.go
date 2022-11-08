// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package filters

import "testing"

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		filters []string
		want    string
		wantErr bool
	}{
		{
			name:    "empty/no filter is valid",
			filters: []string{},
			want:    "",
			wantErr: false,
		},
		{
			name:    "valid name and agent type is valid",
			filters: []string{"name=myagent", "type=agent"},
			want:    "common.k8s.elastic.co/type=agent,agent.k8s.elastic.co/name=myagent",
			wantErr: false,
		},
		{
			name:    "valid name and apm type is valid",
			filters: []string{"name=myapm", "type=apm"},
			want:    "common.k8s.elastic.co/type=apm,apm.k8s.elastic.co/name=myapm",
			wantErr: false,
		},
		{
			name:    "valid name and beat type is valid",
			filters: []string{"name=mybeat", "type=beat"},
			want:    "common.k8s.elastic.co/type=beat,beat.k8s.elastic.co/name=mybeat",
			wantErr: false,
		},
		{
			name:    "valid name and elasticsearch type is valid",
			filters: []string{"name=mycluster", "type=elasticsearch"},
			want:    "common.k8s.elastic.co/type=elasticsearch,elasticsearch.k8s.elastic.co/cluster-name=mycluster",
			wantErr: false,
		},
		{
			name:    "valid name and enterprisesearch type is valid",
			filters: []string{"name=mycluster", "type=enterprisesearch"},
			want:    "common.k8s.elastic.co/type=enterprisesearch,enterprisesearch.k8s.elastic.co/name=mycluster",
			wantErr: false,
		},
		{
			name:    "valid name and kibana type is valid",
			filters: []string{"name=mykb", "type=kibana"},
			want:    "common.k8s.elastic.co/type=kibana,kibana.k8s.elastic.co/name=mykb",
			wantErr: false,
		},
		{
			name:    "valid name and maps type is valid",
			filters: []string{"name=mymaps", "type=maps"},
			want:    "common.k8s.elastic.co/type=maps,maps.k8s.elastic.co/name=mymaps",
			wantErr: false,
		},
		{
			name:    "missing type is invalid",
			filters: []string{"name=mycluster"},
			want:    "",
			wantErr: true,
		},
		{
			name:    "missing name is invalid",
			filters: []string{"type=elasticsearch"},
			want:    "",
			wantErr: true,
		},
		{
			name:    "multiple types is invalid",
			filters: []string{"type=elasticsearch", "type=kibana"},
			want:    "",
			wantErr: true,
		},
		{
			name:    "multiple names is invalid",
			filters: []string{"name=myname1", "name=myname2"},
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid type is invalid",
			filters: []string{"type=invalid", "name=mytest"},
			want:    "",
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
			if got.LabelSelector != tt.want {
				t.Errorf("New() = %v, want %v", got, tt.want)
			}
		})
	}
}
