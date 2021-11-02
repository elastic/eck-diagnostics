// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package log

import (
	"bytes"
	"io"
	"log"
	"os"
)

var (
	// Buffer containing all log statements made through the logger, so that they can be included in the diagnostic archive.
	Buffer bytes.Buffer

	// Logger to be used by this program. Available in its own package to avoid import cycles.
	Logger = log.New(io.MultiWriter(os.Stdout, &Buffer), "", log.LstdFlags)
)
