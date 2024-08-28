// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/version"
)

func Test_min(t *testing.T) {
	type args struct {
		versions []*version.Version
	}
	tests := []struct {
		name string
		args args
		want *version.Version
	}{
		{
			name: "no versions",
			args: args{
				versions: nil,
			},
			want: fallbackMaxVersion,
		},
		{
			name: "single version",
			args: args{
				versions: []*version.Version{
					version.MustParseSemantic("1.0.0"),
				},
			},
			want: version.MustParseSemantic("1.0.0"),
		},
		{
			name: "multiple versions",
			args: args{
				versions: []*version.Version{
					version.MustParseSemantic("1.3.0"),
					version.MustParseSemantic("1.2.0"),
					version.MustParseSemantic("1.5.0"),
				},
			},
			want: version.MustParseSemantic("1.5.0"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maxVersion(tt.args.versions); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("max() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_extractVersionFromDockerImage(t *testing.T) {
	type args struct {
		image string
	}
	tests := []struct {
		name    string
		args    args
		want    *version.Version
		wantErr bool
	}{
		{
			name: "no version",
			args: args{
				image: "myimage",
			},
			want:    fallbackMaxVersion,
			wantErr: false,
		},
		{
			name: "image with version and sha",
			args: args{
				image: "myimage:1.2.0@2343af3434",
			},
			want:    version.MustParseSemantic("1.2.0"),
			wantErr: false,
		},
		{
			name: "image with version",
			args: args{
				image: "myimage:1.2.0",
			},
			want:    version.MustParseSemantic("1.2.0"),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractVersionFromDockerImage(tt.args.image)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractVersionFromDockerImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractVersionFromDockerImage() got = %v, want %v", got, tt.want)
			}
		})
	}
}
