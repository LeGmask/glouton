// Copyright 2015-2023 Bleemeo
//
// bleemeo.com an infrastructure monitoring solution in the Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package crashreport

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"glouton/types"
	"io"
	"strings"
	"time"
)

type inSituZipWriter struct {
	baseFolder      string
	zipWriter       *zip.Writer
	currentFileName string
}

// newInSituZipWriter returns an ArchiveWriter able to write directly into the given zip archive.
func newInSituZipWriter(baseFolder string, zipWriter *zip.Writer) types.ArchiveWriter {
	return &inSituZipWriter{
		// Zip entries must not start with a slash, and slashes will automatically be added before filenames.
		baseFolder: strings.Trim(baseFolder, "/"),
		zipWriter:  zipWriter,
	}
}

func (zw *inSituZipWriter) Create(filename string) (io.Writer, error) {
	fullFilename := zw.baseFolder + "/" + filename
	zw.currentFileName = fullFilename

	return zw.zipWriter.Create(fullFilename)
}

func (zw *inSituZipWriter) CurrentFileName() string {
	return zw.currentFileName
}

// Copied from api/tar_archive.go

type tarArchive struct {
	w                  *tar.Writer
	currentFileContent *bytes.Buffer
	currentFileHeader  tar.Header
}

func newTarWriter(w io.Writer) *tarArchive {
	return &tarArchive{
		w: tar.NewWriter(w),
	}
}

func (a *tarArchive) CurrentFileName() string {
	return a.currentFileHeader.Name
}

func (a *tarArchive) flushPending() error {
	if a.currentFileHeader.Name == "" {
		return nil
	}

	a.currentFileHeader.Size = int64(a.currentFileContent.Len())

	if err := a.w.WriteHeader(&a.currentFileHeader); err != nil {
		return err
	}

	_, err := a.w.Write(a.currentFileContent.Bytes())

	return err
}

func (a *tarArchive) Create(filename string) (io.Writer, error) {
	if err := a.flushPending(); err != nil {
		return nil, err
	}

	a.currentFileHeader = tar.Header{
		Name:    filename,
		ModTime: time.Now(),
		Mode:    0o644,
	}

	if a.currentFileContent == nil {
		a.currentFileContent = &bytes.Buffer{}
	}

	a.currentFileContent.Reset()

	return a.currentFileContent, nil
}

func (a *tarArchive) Close() error {
	if err := a.flushPending(); err != nil {
		return err
	}

	return a.w.Close()
}
