// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package internal

var (
	buildVersion  string
	buildDate     string
	buildHash     string
	snapshotBuild string
)

// DiagnosticsVersion captures version and build information about this tool.
type DiagnosticsVersion struct {
	Version   string
	Hash      string
	BuildDate string
}

func Version() string {
	v := buildVersion
	if snapshotBuild == "true" {
		v += "-SNAPSHOT"
	}
	return v
}

func about() DiagnosticsVersion {
	return DiagnosticsVersion{
		Version:   Version(),
		Hash:      buildHash,
		BuildDate: buildDate,
	}
}
