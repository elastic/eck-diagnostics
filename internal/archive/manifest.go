// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package archive

import (
	"time"
)

type StackDiagnosticManifest struct {
	DiagType string `json:"diagType"`
	DiagPath string `json:"diagPath"`
}

type DiagnosticManifest struct {
	DiagType            string                    `json:"diagType"`
	DiagVersion         string                    `json:"diagVersion"`
	CollectionDate      time.Time                 `json:"collectionDate"`
	IncludedDiagnostics []StackDiagnosticManifest `json:"includedDiagnostics"`
}

func NewDiagnosticManifest(diagVersion string) DiagnosticManifest {
	return DiagnosticManifest{
		DiagType:       "eck-diagnostics",
		DiagVersion:    diagVersion,
		CollectionDate: time.Now(),
	}
}
