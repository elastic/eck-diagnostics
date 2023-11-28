// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	_ "embed"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/version"
)

func Test_supportedStackDiagTypesFor(t *testing.T) {
	type args struct {
		eckVersion *version.Version
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "before 2.8",
			args: args{eckVersion: version.MustParseSemantic("2.7.99")},
			want: []string{"elasticsearch", "kibana"},
		},
		{
			name: "after 2.8",
			args: args{eckVersion: version.MustParseSemantic("2.8.0")},
			want: []string{"elasticsearch", "kibana", "logstash"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := supportedStackDiagTypesFor(tt.args.eckVersion); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("supportedStackDiagTypesFor() = %v, want %v", got, tt.want)
			}
		})
	}
}
