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
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"glouton/types"
	"glouton/utils/archivewriter"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func setupTestDir(t *testing.T) (testDir string, delTestDir func()) {
	t.Helper()

	testDir, err := os.MkdirTemp("", "testworkdir_")
	if err != nil {
		t.Skip("Could not create test directory:", err)
	}

	delTestDir = func() {
		err := os.RemoveAll(testDir)
		if err != nil {
			t.Logf("Failed to remove test dir %q", testDir)
		}
	}

	if tmpInfo, err := os.Stat(testDir); err != nil {
		delTestDir()
		t.Skip("Failed to", err)
	} else if tmpInfo.Mode().Perm()&0o200 == 0 {
		delTestDir()
		t.Skipf("Missing write permission for temp dir %q", testDir)
	}

	return testDir, delTestDir
}

func TestCrashReportArchivePattern(t *testing.T) {
	t.Parallel()

	if _, err := filepath.Match(crashReportArchivePattern, ""); err != nil {
		t.Fatal("`crashReportArchivePattern` is invalid:", err)
	}
}

func TestWorkDirCreation(t *testing.T) {
	t.Parallel()

	wrapper := func(t *testing.T, testDir string) {
		t.Helper()

		err := createWorkDirIfNotExist(testDir)
		if err != nil {
			t.Fatal("Failed to create work dir:", err)
		}

		workDirPath := filepath.Join(testDir, crashReportWorkDir)

		info, err := os.Stat(workDirPath)
		if err != nil {
			t.Fatal("Failed to", err)
		}

		if !info.IsDir() {
			t.Fatalf("Work dir %q is not a directory ...", workDirPath)
		}

		perm := info.Mode().Perm()
		if perm != 448 {
			t.Fatalf("Did not create work dir with expected permissions:\nwant: -rwx------\n got: %s", perm)
		}
	}

	t.Run("Work dir not existing", func(t *testing.T) {
		testDir, delTmpDir := setupTestDir(t)
		defer delTmpDir()

		wrapper(t, testDir)
	})

	t.Run("Work dir already existing", func(t *testing.T) {
		testDir, delTmpDir := setupTestDir(t)
		defer delTmpDir()

		err := os.Mkdir(filepath.Join(testDir, crashReportWorkDir), 0o700)
		if err != nil && !os.IsExist(err) {
			t.Fatal("Failed to pre-create crash report work dir:", err)
		}

		wrapper(t, testDir)
	})
}

func TestIsWriteInProgress(t *testing.T) {
	t.Parallel()

	t.Run("In progress", func(t *testing.T) {
		testDir, delTmpDir := setupTestDir(t)
		defer delTmpDir()

		f, err := os.Create(filepath.Join(testDir, writeInProgressFlag))
		if err != nil {
			t.Fatal("Failed to create write-in-progress flag file:", err)
		}

		f.Close()

		if !IsWriteInProgress(testDir) {
			t.Fatal("Write is in progress but was not considered as such.")
		}
	})

	t.Run("Not in progress", func(t *testing.T) {
		testDir, delTmpDir := setupTestDir(t)
		defer delTmpDir()

		if IsWriteInProgress(testDir) {
			t.Fatal("Write is not in progress but was considered to be in progress.")
		}
	})
}

func TestStderrRedirection(t *testing.T) {
	t.Parallel()

	testDir, delTmpDir := setupTestDir(t)
	defer delTmpDir()

	setupStderrRedirection(testDir)

	stderrFilePath := filepath.Join(testDir, stderrFileName)

	info, err := os.Stat(stderrFilePath)
	if err != nil {
		t.Fatal("Failed to", err)
	}

	if info.Size() != 0 {
		t.Fatal("Stderr log file should be empty until someone writes to stderr")
	}

	const logContent = "This is a message written on stderr."

	_, err = fmt.Fprint(os.Stderr, logContent)
	if err != nil {
		t.Fatal("Failed to write to stderr:", err)
	}

	stderrContent, err := os.ReadFile(stderrFilePath)
	if err != nil {
		t.Fatal("Failed to", err)
	}

	strStderr := string(stderrContent)
	if strStderr != logContent {
		t.Fatalf("Unexpected content from stderr:\nwant: %q\n got: %q", logContent, strStderr)
	}
}

func TestPurgeCrashReports(t *testing.T) {
	t.Parallel()

	t.Run("Keep the last reports", func(t *testing.T) {
		testDir, delTmpDir := setupTestDir(t)
		defer delTmpDir()

		const keep = 2
		mostRecentReports := make([]string, keep)

		for i := 0; i < 5; i++ {
			reportTime := time.Now().Add(time.Duration(-i) * time.Hour).Add(time.Duration(i*7) * time.Second)
			crashReportPath := filepath.Join(testDir, reportTime.Format(crashReportArchiveFormat))

			if i < keep {
				mostRecentReports[i] = crashReportPath // Save it for later check
			}

			f, err := os.Create(crashReportPath)
			if err != nil {
				t.Fatal("Failed to create fake crash report:", err)
			}

			f.Close()
		}

		purgeCrashReports(keep, []string{}, testDir)

		allReports, err := filepath.Glob(filepath.Join(testDir, crashReportArchivePattern))
		if err != nil {
			t.Fatal("Invalid pattern for `crashReportArchivePattern`:", err)
		}

		sort.Strings(mostRecentReports)

		if diff := cmp.Diff(allReports, mostRecentReports); diff != "" {
			t.Log(diff)
			t.Fatal("Unexpected crash report purge result.")
		}
	})

	t.Run("Keep preserved reports", func(t *testing.T) {
		testDir, delTmpDir := setupTestDir(t)
		defer delTmpDir()

		var reportsToKeep []string

		for i := 0; i < 10; i++ {
			reportTime := time.Now().Add(time.Duration(-i) * time.Hour).Add(time.Duration(i*7) * time.Second)
			crashReportPath := filepath.Join(testDir, reportTime.Format(crashReportArchiveFormat))

			if i%3 == 0 {
				reportsToKeep = append(reportsToKeep, crashReportPath) // Save it for later check
			}

			f, err := os.Create(crashReportPath)
			if err != nil {
				t.Fatal("Failed to create fake crash report:", err)
			}

			f.Close()
		}

		// The max report count doesn't matter when some reports should be preserved.
		purgeCrashReports(0, reportsToKeep, testDir)

		allReports, err := filepath.Glob(filepath.Join(testDir, crashReportArchivePattern))
		if err != nil {
			t.Fatal("Invalid pattern for `crashReportArchivePattern`:", err)
		}

		sort.Strings(reportsToKeep)

		if diff := cmp.Diff(allReports, reportsToKeep); diff != "" {
			t.Log(diff)
			t.Fatal("Unexpected crash report purge result.")
		}
	})
}

func TestMarkAsDone(t *testing.T) {
	t.Parallel()

	testDir, delTmpDir := setupTestDir(t)
	defer delTmpDir()

	err := createWorkDirIfNotExist(testDir)
	if err != nil {
		t.Fatal("Failed to create work dir:", err)
	}

	flagFilePath := filepath.Join(testDir, writeInProgressFlag)

	f, err := os.Create(flagFilePath)
	if err != nil {
		t.Fatal("Failed to create write-in-progress flag:", err)
	}

	f.Close()

	errs := markAsDone(testDir)
	if errs != nil {
		t.Fatal("Some errors logs have been written by markAsDone():", errs.Error())
	}

	_, err = os.Stat(filepath.Join(testDir, crashReportWorkDir))
	if !os.IsNotExist(err) {
		t.Fatal("markAsDone() did not delete crash report work dir.")
	}

	_, err = os.Stat(flagFilePath)
	if !os.IsNotExist(err) {
		t.Fatal("markAsDone() did not delete the write-in-progress flag.")
	}
}

func TestGenerateDiagnostic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name               string
		ctxTimeout         time.Duration
		diagnosticDuration time.Duration
		diagnosticError    error
		shouldPanic        bool
		expectedError      error
	}{
		{
			name:               "Errorless behavior",
			ctxTimeout:         time.Second,
			diagnosticDuration: time.Millisecond,
			diagnosticError:    nil,
			expectedError:      nil,
		},
		{
			name:               "Context timeout",
			ctxTimeout:         time.Millisecond,
			diagnosticDuration: 10 * time.Millisecond,
			expectedError:      context.DeadlineExceeded,
		},
		{
			name:               "Panic",
			ctxTimeout:         time.Second,
			diagnosticDuration: time.Millisecond,
			shouldPanic:        true,
			expectedError:      errFailedToDiagnostic,
		},
	}

	for _, testCase := range cases {
		tc := testCase

		t.Run(tc.name, func(t *testing.T) {
			diagnosticFn := func(context.Context, types.ArchiveWriter) error {
				time.Sleep(tc.diagnosticDuration) // Simulates processing

				if tc.shouldPanic {
					panic("Panicking")
				}

				return tc.diagnosticError
			}

			ctx, cancel := context.WithTimeout(context.Background(), tc.ctxTimeout)
			defer cancel()

			err := generateDiagnostic(ctx, nil, diagnosticFn)
			if !errors.Is(err, tc.expectedError) {
				t.Fatalf("Unexpected error: want '%v', got '%v'", tc.expectedError, err)
			}
		})
	}
}

func TestBundleCrashReportFiles(t *testing.T) { //nolint:maintidx
	t.Parallel()

	// fileCmp allows comparing a filename and
	// a pattern while knowing which is which.
	type fileCmp struct {
		filenameOrPattern string
		// toBeTested is set to true for filenames
		// that should be tested against the pattern.
		toBeTested bool
	}

	cases := []struct {
		name string
		// setup
		previousStderr            bool
		previousStderrContent     string
		previousDiagnostic        bool
		previousDiagnosticContent map[string]string
		diagnosticContent         map[string]string
		writeAlreadyInProgress    bool
		// params
		reportingEnabled bool
		// expected result
		wantReportPath      bool
		wantArchiveFiles    map[string]string
		wantFilesInStateDir []fileCmp // Those files can be patterns to match
	}{
		{
			name:               "Reporting disabled",
			reportingEnabled:   false,
			previousStderr:     false,
			previousDiagnostic: false,
			wantReportPath:     false,
		},
		{
			name:               "No files from previous runs",
			reportingEnabled:   true,
			previousStderr:     false,
			previousDiagnostic: false,
			wantReportPath:     false,
		},
		{
			name:                  "A panic-free stderr file",
			reportingEnabled:      true,
			previousStderr:        true,
			previousStderrContent: "Some logs",
			previousDiagnostic:    false,
			wantReportPath:        false,
		},
		{
			name:                  "Stderr file reporting a panic",
			reportingEnabled:      true,
			previousStderr:        true,
			previousStderrContent: "panic: something went wrong",
			previousDiagnostic:    false,
			diagnosticContent:     map[string]string{"file.json": `{"some": "data"}`},
			wantReportPath:        true,
			wantArchiveFiles: map[string]string{
				"stderr.log":           "panic: something went wrong",
				"diagnostic/file.json": `{"some": "data"}`,
			},
			wantFilesInStateDir: []fileCmp{{filenameOrPattern: "crashreport_*.zip"}},
		},
		{
			name:                      "With previous crash diagnostic",
			previousStderr:            true,
			previousStderrContent:     "panic: here we go again",
			previousDiagnostic:        true,
			previousDiagnosticContent: map[string]string{"file.txt": "Some crash diagnostic content"},
			diagnosticContent:         map[string]string{"file.log": "Some logs"},
			reportingEnabled:          true,
			wantReportPath:            true,
			wantArchiveFiles: map[string]string{
				"stderr.log":                "panic: here we go again",
				"diagnostic/file.log":       "Some logs",
				"crash_diagnostic/file.txt": "Some crash diagnostic content",
			},
			wantFilesInStateDir: []fileCmp{{filenameOrPattern: "crashreport_*.zip"}},
		},
		{
			name:                   "Had a crash but write is in progress",
			previousStderr:         true,
			previousStderrContent:  "panic: that seems serious",
			writeAlreadyInProgress: true,
			wantReportPath:         false,
			wantFilesInStateDir:    []fileCmp{}, // The flag should be deleted before the function returns
		},
	}

	for _, testCase := range cases {
		tc := testCase

		t.Run(tc.name, func(t *testing.T) {
			stateDir, delTmpDir := setupTestDir(t)
			defer delTmpDir()

			if tc.previousStderr || tc.previousDiagnostic {
				err := os.Mkdir(filepath.Join(stateDir, crashReportWorkDir), 0o700)
				if err != nil {
					t.Fatal("Failed to create crash report work dir:", err)
				}
			}

			if tc.previousStderr {
				stderrFile, err := os.Create(filepath.Join(stateDir, oldStderrFileName))
				if err != nil {
					t.Fatal("Failed to create stderr file:", err)
				}

				fmt.Fprint(stderrFile, tc.previousStderrContent)
				stderrFile.Close()
			}

			if tc.previousDiagnostic {
				diagnosticFile, err := os.Create(filepath.Join(stateDir, panicDiagnosticArchive))
				if err != nil {
					t.Fatal("Failed to create diagnostic archive file:", err)
				}

				tarWriter := archivewriter.NewTarWriter(diagnosticFile)

				for file, content := range tc.previousDiagnosticContent {
					w, err := tarWriter.Create(file)
					if err != nil {
						t.Fatal("Failed to write to previous diagnostic archive:", err)
					}

					fmt.Fprint(w, content)
				}

				if err = tarWriter.Close(); err != nil {
					t.Fatal("Failed to close diagnostic archive:", err)
				}

				diagnosticFile.Close()
			}

			if tc.writeAlreadyInProgress {
				f, err := os.Create(filepath.Join(stateDir, writeInProgressFlag))
				if err != nil {
					t.Fatal("Failed to create write-in-progress file:", err)
				}

				f.Close()
			}

			diagnosticFn := func(ctx context.Context, writer types.ArchiveWriter) error {
				for file, content := range tc.diagnosticContent {
					w, err := writer.Create(file)
					if err != nil {
						return err
					}

					fmt.Fprint(w, content)
				}

				return nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			reportPath := bundleCrashReportFiles(ctx, 2, stateDir, tc.reportingEnabled, diagnosticFn)
			if (reportPath != "") != tc.wantReportPath {
				t.Fatalf("Expected a report path: %t, got one: %t.", tc.wantReportPath, reportPath != "")
			}

			// Check that we have the expected files in state dir
			filesInStateDir, err := os.ReadDir(stateDir)
			if err != nil {
				t.Fatal("Failed to list files in state dir:", err)
			}

			stateDirFiles := make([]fileCmp, len(filesInStateDir))

			for i, file := range filesInStateDir {
				stateDirFiles[i] = fileCmp{filenameOrPattern: file.Name(), toBeTested: true}
			}

			filepathComparer := cmp.Comparer(func(x, y fileCmp) bool {
				// As x and y are given in an unknown order, we need to know
				// which one is the filename to test and which one is the pattern to match.
				var pattern, value string
				if x.toBeTested {
					pattern, value = y.filenameOrPattern, x.filenameOrPattern
				} else {
					pattern, value = x.filenameOrPattern, y.filenameOrPattern
				}

				match, err := path.Match(pattern, value)
				if err != nil {
					t.Fatalf("Bad pattern %q", pattern)
				}

				return match
			})

			if tc.wantFilesInStateDir == nil {
				// Nil and empty slices are given as different by cmp.Diff,
				// so we make the expected files slice non-nil.
				tc.wantFilesInStateDir = []fileCmp{}
			}

			if diff := cmp.Diff(tc.wantFilesInStateDir, stateDirFiles, filepathComparer); diff != "" {
				t.Fatal("Unexpected state dir content:\n", diff)
			}

			if !tc.wantReportPath {
				return // Nothing more to test
			}

			// Check that we have the expected report archive content
			reportArchive, err := zip.OpenReader(reportPath)
			if err != nil {
				t.Fatal("Failed to open crash report archive:", err)
			}

			defer reportArchive.Close()

			if tc.previousStderr {
				reportStderr, err := reportArchive.Open(stderrFileName)
				if err != nil {
					t.Fatal("Failed to open stderr file from report archive:", err)
				}

				stderrStat, err := reportStderr.Stat()
				if err != nil {
					t.Fatal("Failed to stat stderr file from report archive:", err)
				}

				stderrContent := make([]byte, stderrStat.Size())

				_, err = reportStderr.Read(stderrContent)
				if err != nil && !errors.Is(err, io.EOF) {
					t.Fatal("Failed to read content of stderr file from report archive:", err)
				}

				reportStderr.Close()

				if diff := cmp.Diff(tc.previousStderrContent, string(stderrContent)); diff != "" {
					t.Fatal("Unexpected stderr file content:\n", diff)
				}
			}

			if tc.wantArchiveFiles != nil {
				archiveFiles := make(map[string]string, len(reportArchive.File))

				for _, reportFile := range reportArchive.File {
					fileReader, err := reportFile.Open()
					if err != nil {
						t.Fatalf("Failed to open %q from report archive: %v", reportFile.Name, err)
					}

					fileContent := make([]byte, reportFile.UncompressedSize64)

					_, err = fileReader.Read(fileContent)
					if err != nil && !errors.Is(err, io.EOF) {
						t.Fatalf("Failed to read content of %q from report archive: %v", reportFile.Name, err)
					}

					fileReader.Close()

					archiveFiles[reportFile.Name] = string(fileContent)
				}

				if diff := cmp.Diff(tc.wantArchiveFiles, archiveFiles); diff != "" {
					t.Fatal("Unexpected report archive content:\n", diff)
				}
			}
		})
	}
}
