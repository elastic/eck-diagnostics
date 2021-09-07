// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package archive

import (
	"archive/zip"
	"io"
	"log"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/errors"
)

const archivePathSeparator = '/'

// Path joins elem to form a (ZIP) archive path.
func Path(elem ...string) string {
	// ZIP files use / as separator on all platforms
	return strings.Join(elem, string(archivePathSeparator))
}

// RootDir returns the top level directory in a ZIP archive path.
func RootDir(name string) string {
	if len(name) == 0 {
		return name
	}
	i := 1
	for i < len(name) && name[i] != archivePathSeparator {
		i++
	}
	return name[0:i]
}

// ZipFile wraps a zip.Writer to add a few convenience functions and implement resource closing.
type ZipFile struct {
	*zip.Writer
	underlying io.Closer
	errs       []error
	log        *log.Logger
}

// NewZipFile creates a new zip file named fileName.
func NewZipFile(fileName string, log *log.Logger) (*ZipFile, error) {
	f, err := os.Create(fileName)
	if err != nil {
		return nil, err
	}
	w := zip.NewWriter(f)
	return &ZipFile{
		Writer:     w,
		underlying: f,
		log:        log,
	}, nil
}

// Close closes the zip.Writer and the underlying file.
func (z *ZipFile) Close() error {
	errs := []error{z.writeErrorsToFile(), z.Writer.Close(), z.underlying.Close()}
	return errors.NewAggregate(errs)
}

// Add takes a map of file names and functions to evaluate with the intent to add the result of the evaluation to the
// zip file at the name used as key in the map.
func (z *ZipFile) Add(fns map[string]func(io.Writer) error) {
	for k, f := range fns {
		fw, err := z.Create(k)
		if err != nil {
			z.errs = append(z.errs, err)
			return
		}
		z.errs = append(z.errs, f(fw))
	}
}

// AddError records an error to be persistent in the ZipFile.
func (z *ZipFile) AddError(err error) {
	if err == nil {
		return
	}
	// log errors immediately to give user early feedback
	log.Print(err.Error())
	z.errs = append(z.errs, err)
}

// writeErrorsToFile writes the accumulated errors to a file inside the ZipFile.
func (z *ZipFile) writeErrorsToFile() error {
	aggregate := errors.NewAggregate(z.errs)
	if aggregate == nil {
		return nil
	}
	out, err := z.Create("eck-diagnostic-errors.txt")
	if err != nil {
		return err
	}
	errorString := aggregate.Error()
	// errors have been logged already just include in zip archive to inform support
	_, err = out.Write([]byte(errorString))
	return err
}
