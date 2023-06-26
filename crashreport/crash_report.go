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
	"context"
	"errors"
	"fmt"
	"glouton/logger"
	"glouton/types"
	"glouton/utils/archivewriter"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/utils/strings/slices"
)

const (
	crashReportWorkDir = "crash_report"

	stderrFileName    = "stderr.log"
	oldStderrFileName = crashReportWorkDir + string(os.PathSeparator) + stderrFileName

	panicDiagnosticArchive = "crash_diagnostic.tar"

	crashReportArchiveFormat  = "crashreport_20060102-150405.zip"
	crashReportArchivePattern = "crashreport_[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9].zip"

	writeInProgressFlag = "crash_report_in_progress"
)

var errFailedToDiagnostic = errors.New("failed to generate a diagnostic")

type diagnosticFunc = func(context.Context, types.ArchiveWriter) error

//nolint:gochecknoglobals
var (
	lock sync.Mutex

	disabled   bool
	dir        string
	diagnostic diagnosticFunc
)

// SetOptions defines multiple things related to crash reporting:
// - enabled: whether crash reports should be created or not
// - stateDir: the directory where crash reports should be created
// - maxDirCount: the maximum number of crash reports we should keep in dir
// - diagnosticFn: a callback to generate diagnostics (might be nil if no diagnostic should be created).
func SetOptions(enabled bool, stateDir string, diagnosticFn diagnosticFunc) {
	lock.Lock()
	defer lock.Unlock()

	disabled = !enabled
	dir = stateDir
	diagnostic = diagnosticFn
}

func logMultiErrs(errs prometheus.MultiError) {
	for _, err := range errs {
		logger.V(1).Println(err)
	}
}

func isWriteInProgress(stateDir string) bool {
	_, err := os.Stat(filepath.Join(stateDir, writeInProgressFlag))

	return err == nil
}

func createWorkDirIfNotExist(stateDir string) error {
	workDirPath := filepath.Join(stateDir, crashReportWorkDir)

	err := os.Mkdir(workDirPath, 0o740)
	if err != nil && !os.IsExist(err) {
		return err
	}

	return nil
}

// SetupStderrRedirection creates a file that will receive stderr output.
// If such a file already exists, it is moved to a '.old' version and
// a new and empty file takes it place.
func SetupStderrRedirection() {
	lock.Lock()
	stateDir := dir
	lock.Unlock()

	if isWriteInProgress(stateDir) {
		// If the flag has not been deleted the last run, it may be because the crash reporting process crashed.
		// So to try not to crash again, we skip the crash reporting this time.
		// We will try to report the next time, so we delete the flag.
		logMultiErrs(markAsDone(stateDir))
	}

	if err := createWorkDirIfNotExist(stateDir); err != nil {
		logger.V(1).Println("Failed to create crash report work dir:", err)

		return
	}

	stderrFilePath := filepath.Join(stateDir, stderrFileName)
	oldStderrFilePath := filepath.Join(stateDir, oldStderrFileName)

	if _, err := os.Stat(stderrFilePath); err == nil {
		err = os.Rename(stderrFilePath, oldStderrFilePath)
		if err != nil {
			logger.V(1).Println("Failed to handle old stderr log file:", err)
		}
	}

	newStderrFile, err := os.Create(stderrFilePath)
	if err != nil {
		logger.V(1).Println("Failed to create a new stderr log file:", err)

		return
	}

	defer newStderrFile.Close()

	err = redirectOSSpecificStderrToFile(newStderrFile.Fd())
	if err != nil {
		logger.V(1).Println("Failed to redirect stderr to log file:", err)
	}
}

// PurgeCrashReports deletes oldest crash reports present in 'stateDir'
// and only keeps the 'maxReportCount' most recent ones.
// Crash reports specified as 'preserve' won't be deleted.
func PurgeCrashReports(maxReportCount int, preserve ...string) {
	lock.Lock()
	stateDir := dir
	lock.Unlock()

	existingCrashReports, err := filepath.Glob(filepath.Join(stateDir, crashReportArchivePattern))
	if err != nil {
		// filepath.Glob's documentation states that "Glob ignores file system errors [...]".
		// The only possible returned error is ErrBadPattern, and the above pattern is known to be valid.
		return
	}

	crashReportCount := len(existingCrashReports)

	// Prevent preserved reports from being purged
	for _, report := range preserve {
		// A slices package will be included in the standard library from Go 1.21.
		// For now, using the one from the k8s API.
		if idx := slices.Index(existingCrashReports, report); idx >= 0 {
			// Remove report from the list of crash reports
			existingCrashReports = append(existingCrashReports[:idx], existingCrashReports[idx+1:]...)
		}
	}

	if crashReportCount <= maxReportCount {
		return
	}

	// Remove the oldest excess crash reports
	sort.Strings(existingCrashReports)

	for i, report := range existingCrashReports {
		if i == crashReportCount-maxReportCount {
			break
		}

		err = os.RemoveAll(report)
		if err != nil {
			logger.V(1).Printf("Failed to remove old crash report %q: %v", report, err)
		}
	}
}

// BundleCrashReportFiles creates a crash report archive with the current datetime in its name,
// then moves the previous stderr log file to it, and creates a flag file to prevent any upload,
// as the crash report is not complete (the flag will be removed once the diagnostic is created).
// It returns the path to the created crash report (or an empty string if not created).
func BundleCrashReportFiles(ctx context.Context, maxReportCount int) (reportPath string) {
	lock.Lock()
	stateDir := dir
	enabled := !disabled
	diagnosticFn := diagnostic
	lock.Unlock()

	if isWriteInProgress(stateDir) {
		return ""
	}

	defer func() { logMultiErrs(markAsDone(stateDir)) }()

	if !enabled || maxReportCount <= 0 {
		// Crash reports are apparently disabled in config.
		return ""
	}

	var foundStderrLog, foundPanicDiagnostic bool

	lastStderrFilePath := filepath.Join(stateDir, oldStderrFileName)

	lastStderrFile, err := os.Open(lastStderrFilePath)
	if err != nil {
		return "" // No previous crash
	}

	defer lastStderrFile.Close()

	// Only the first 4KiB of the file are needed
	// to know whether it contains a stacktrace or not.
	lastStderrFileContent := make([]byte, 4096)

	_, err = lastStderrFile.Read(lastStderrFileContent)
	if err == nil || errors.Is(err, io.EOF) {
		if bytes.Contains(lastStderrFileContent, []byte("panic")) {
			foundStderrLog = true
		}
	}

	if _, err = os.Stat(filepath.Join(stateDir, panicDiagnosticArchive)); err == nil {
		foundPanicDiagnostic = true
	}

	if foundStderrLog || foundPanicDiagnostic {
		return makeBundle(ctx, stateDir, diagnosticFn)
	}

	return "" // No crash report created
}

// makeBundle creates an archive with the current datetime in its name,
// containing the old stderr log file, a freshly created diagnostic
// and the diagnostic generated at the moment of the last crash, if one.
// It returns the name of the created archive if everything went well, otherwise an empty string.
func makeBundle(ctx context.Context, stateDir string, diagnosticFn diagnosticFunc) string {
	// Create a file to flag that the crash report is not complete because we haven't generated a diagnostic yet.
	_, err := os.Create(filepath.Join(stateDir, writeInProgressFlag))
	if err != nil {
		logger.V(1).Println("Failed to create flag to mark crash report writing as in progress")
	}

	crashReportPath := filepath.Join(stateDir, time.Now().Format(crashReportArchiveFormat))

	crashReportArchive, err := os.Create(crashReportPath)
	if err != nil {
		logger.V(1).Printf("Can't create crash report archive %q: %v", crashReportPath, err)

		return ""
	}

	defer crashReportArchive.Close()

	zipWriter := zip.NewWriter(crashReportArchive)

	defer zipWriter.Close()

	stderrFile, err := os.Open(filepath.Join(stateDir, oldStderrFileName))
	if err == nil { // Open stderr log file
		info, _ := stderrFile.Stat()

		writer, err := zipWriter.CreateHeader(&zip.FileHeader{
			Name:     stderrFileName,
			Modified: info.ModTime(),
			Method:   zip.Deflate,
		})
		if err == nil { // Create stderr.log entry in zip
			_, err = io.Copy(writer, stderrFile)
			if err != nil { // Copy stderr log file content to zip
				logger.V(1).Println("Failed to copy stderr log file to zip:", err)
			}
		} else {
			logger.V(1).Println("Failed create stderr log file in zip:", err)
		}

		stderrFile.Close()
	}

	// If a diagnostic has been generated when crashing, copy it into the archive
	crashDiagnosticFile, err := os.Open(filepath.Join(stateDir, panicDiagnosticArchive))
	if err == nil {
		defer func() {
			crashDiagnosticFile.Close()

			err := os.Remove(crashDiagnosticFile.Name())
			if err != nil {
				logger.V(1).Println("Failed to delete crash diagnostic archive:", err)
			}
		}()

		crashDiagnosticReader := tar.NewReader(crashDiagnosticFile)

		for {
			entry, err := crashDiagnosticReader.Next()
			if err != nil {
				break
			}

			crashDiagnosticZipWriter, err := zipWriter.CreateHeader(&zip.FileHeader{
				Name:     "crash_diagnostic/" + entry.Name,
				Modified: entry.ModTime,
				Method:   zip.Deflate,
			})
			if err != nil {
				logger.V(1).Println("Failed to create a crash diagnostic entry to crash report archive:", err)

				continue
			}

			_, err = io.Copy(crashDiagnosticZipWriter, crashDiagnosticReader) //nolint:gosec
			if err != nil {
				logger.V(1).Printf("Failed to copy crash diagnostic file %q to crash report archive: %v", entry.Name, err)
			}
		}
	} else {
		logger.V(1).Println("Failed to copy crash diagnostic to crash report archive:", err)
	}

	if diagnosticFn != nil {
		subDirZipWriter := archivewriter.NewSubDirZipWriter("diagnostic", zipWriter)

		diagnosticCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		err = generateDiagnostic(diagnosticCtx, subDirZipWriter, diagnosticFn)
		if err != nil {
			logger.V(1).Println("Failed to generate a diagnostic into the crash report archive:", err)
		}
	} else {
		logger.V(1).Println("Can't generate a diagnostic.")
	}

	return crashReportPath
}

// markAsDone removes the crash report working directory and
// deletes the file implying that crash report writing is not done yet,
// which means that crash reports are now ready for upload.
// It returns all encountered errors while trying to go until the end.
func markAsDone(stateDir string) (errs prometheus.MultiError) {
	err := os.RemoveAll(filepath.Join(stateDir, crashReportWorkDir))
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to remove the crash report work dir: %w", err))
	}

	err = os.Remove(filepath.Join(stateDir, writeInProgressFlag))
	if err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("failed to delete write-in-progress flag: %w", err))
	}

	return
}

// generateDiagnostic writes a diagnostic to the given writer.
// The given context must have defined a timeout if the generation
// of the diagnostic should be limited in time.
// When calling generateDiagnostic, the diagnostic callback must not be nil.
func generateDiagnostic(ctx context.Context, writer types.ArchiveWriter, diagnosticFn diagnosticFunc) error {
	done := make(chan error, 1) // Buffered channel to avoid the goroutine being blocked on send

	go func() {
		defer func() {
			if recovErr := recover(); recovErr != nil {
				fmt.Fprintln(os.Stderr, "Diagnostic generation crashed:", recovErr)
				done <- errFailedToDiagnostic
			}
		}()

		done <- diagnosticFn(ctx, writer)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
