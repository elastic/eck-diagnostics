// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import "testing"

func Test_extractTypeName(t *testing.T) {
	tests := []struct {
		name      string
		selectors string
		want      string
		want1     string
		wantErr   bool
	}{
		{
			name:      "elasticsearch type/name should extract properly",
			selectors: "common.k8s.elastic.co/type=elasticsearch,elasticsearch.k8s.elastic.co/cluster-name=myname",
			want:      "elasticsearch",
			want1:     "myname",
			wantErr:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := extractTypeName(tt.selectors)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractTypeName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractTypeName() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("extractTypeName() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}
