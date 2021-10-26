// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package extraction

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/elastic/eck-diagnostics/internal/log"

	"github.com/elastic/eck-diagnostics/internal/archive"
)

var logger = log.Logger

type RemoteSource struct {
	Namespace    string // de-normalized for convenience
	PodName      string
	Typ          string
	ResourceName string
	PodOutputDir string
}

// sourceDirPrefix the directory prefix the stack support-diagnostics tool uses in the archive it creates.
func (j *RemoteSource) sourceDirPrefix() string {
	prefix := "api-diagnostics"
	if j.Typ == "kibana" {
		prefix = fmt.Sprintf("%s-%s", j.Typ, prefix)
	}
	return prefix
}

// outputDirPrefix the directory hierarchy we want to use in the archive created by this tool. It should be the Namespace
// of the resource we are creating diagnostics for followed by the type (elasticserach or kibana currently) and the name
// of the resource.
func (j *RemoteSource) outputDirPrefix() string {
	return archive.Path(j.Namespace, j.Typ, j.ResourceName)
}

// UntarIntoZip extracts the files transferred via tar from the Pod into the given ZipFile.
func UntarIntoZip(reader *io.PipeReader, job RemoteSource, file *archive.ZipFile, verbose bool) error {
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			break
		}
		remoteFilename := header.Name
		// remove the path prefix on the Pod
		relOutputDir := fmt.Sprintf("%s/", strings.TrimPrefix(job.PodOutputDir, "/"))
		relativeFilename := strings.TrimPrefix(remoteFilename, relOutputDir)
		// stack diagnostics create output in a directory called api-diagnostics-{{.Timestamp}}
		if !strings.HasPrefix(relativeFilename, job.sourceDirPrefix()) {
			if verbose {
				logger.Printf("Ignoring file %s in tar from %s diagnostics\n", header.Name, job.ResourceName)
			}
			continue
		}
		manifest := archive.StackDiagnosticManifest{DiagType: job.Typ}

		switch {
		case strings.HasSuffix(relativeFilename, "tar.gz"):
			manifest.DiagPath = job.outputDirPrefix()
			if err := RepackageTarGzip(tarReader, job.outputDirPrefix(), file); err != nil {
				return err
			}
		case strings.HasSuffix(relativeFilename, ".zip"):
			manifest.DiagPath = job.outputDirPrefix()
			if err := RepackageZip(tarReader, job.outputDirPrefix(), file); err != nil {
				return err
			}
		default:
			path := archive.Path(job.Namespace, job.Typ, job.ResourceName, relativeFilename)
			manifest.DiagPath = path
			out, err := file.Create(path)
			if err != nil {
				return err
			}
			// accept decompression bomb for CLI as we control the src
			if _, err := io.Copy(out, tarReader); err != nil { //nolint:gosec
				return err
			}
		}
		file.AddManifestEntry(manifest)
	}
	return nil
}

// RepackageTarGzip repackages the *.tar.gz archives produced by the support diagnostics tool into the given ZipFile.
func RepackageTarGzip(in io.Reader, outputDirPrefix string, zipFile *archive.ZipFile) error {
	gzReader, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	topLevelDir := ""
	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			break
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if topLevelDir == "" {
				topLevelDir = header.Name
			}
			continue
		case tar.TypeReg:
			out, err := zipFile.Create(toOutputPath(header.Name, topLevelDir, outputDirPrefix))
			if err != nil {
				return err
			}
			// accept decompression bomb for CLI tool and we control the src
			_, err = io.Copy(out, tarReader) //nolint:gosec
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// RepackageZip repackages the *.zip file produced by the support diagnostics tool into the zip file produced by this tool
func RepackageZip(in io.Reader, outputDirPrefix string, zipFile *archive.ZipFile) error {
	// it seems the only way to repack a zip archive is to completely read it into memory first
	b := new(bytes.Buffer)
	if _, err := b.ReadFrom(in); err != nil {
		return err
	}

	zipReader, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		return err
	}
	// api-diagnostics creates a common top folder we don't need when repackaging
	topLevelDir := ""
	for _, f := range zipReader.File {
		// skip all the directory entries
		if f.UncompressedSize64 == 0 {
			continue
		}
		// extract the tld first time round
		if topLevelDir == "" {
			topLevelDir = archive.RootDir(f.Name)
		}
		out, err := zipFile.Create(toOutputPath(f.Name, topLevelDir, outputDirPrefix))
		if err != nil {
			return err
		}
		if err := copyFromZip(f, out); err != nil {
			return err
		}
	}
	return nil
}

// copyFromZip writes the contents of file f from a zip file into out.
func copyFromZip(f *zip.File, out io.Writer) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	if _, err := io.Copy(out, rc); err != nil { //nolint:gosec
		return err
	}
	return nil
}

// toOutputPath removes the path prefix topLevelDir from original and re-bases it in outputDirPrefix.
func toOutputPath(original, topLevelDir, outputDirPrefix string) string {
	// topLevelDir should always be a Unix-style path like /api-diagnostics-20210907-133527 so a simple trim should suffice
	// and avoids filepath.* functions that would insert platform specific path elements that are incompatible with
	// the ZIP format.
	return archive.Path(outputDirPrefix, strings.TrimPrefix(original, topLevelDir))
}
