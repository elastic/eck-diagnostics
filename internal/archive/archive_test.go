// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package archive

import "testing"

func Test_rootDir(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "/",
			args: args{
				name: "/",
			},
			want: "/",
		},
		{
			name: "single dir",
			args: args{
				"/dir",
			},
			want: "/dir",
		},
		{
			name: "single dir with trailing slash",
			args: args{
				"/dir/",
			},
			want: "/dir",
		},
		{
			name: "not a dir",
			args: args{
				"version.txt",
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RootDir(tt.args.name); got != tt.want {
				t.Errorf("RootDir() = %v, want %v", got, tt.want)
			}
		})
	}
}
