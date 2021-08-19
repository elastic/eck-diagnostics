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
			if got := max(tt.args.versions); !reflect.DeepEqual(got, tt.want) {
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