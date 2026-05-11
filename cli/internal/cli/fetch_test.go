package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	pcs3 "github.com/conn-castle/personal-context/cli/internal/s3client"
)

// --- Argument validation tests ---

func TestRunFetchNoModeSelector(t *testing.T) {
	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "", fetchOptions{})
	if err == nil {
		t.Fatal("expected error when no mode selector")
	}
	if !strings.Contains(err.Error(), "specify a record ID") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchMultipleModeSelectors(t *testing.T) {
	tests := []struct {
		name     string
		recordID string
		opts     fetchOptions
	}{
		{"record+project", "abc", fetchOptions{Project: "org/proj"}},
		{"record+recent", "abc", fetchOptions{Recent: "3d"}},
		{"record+all", "abc", fetchOptions{All: true}},
		{"project+recent", "", fetchOptions{Project: "org/proj", Recent: "3d"}},
		{"all+project", "", fetchOptions{All: true, Project: "org/proj"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, tt.recordID, tt.opts)
			if err == nil {
				t.Fatal("expected error for multiple mode selectors")
			}
			if !strings.Contains(err.Error(), "mutually exclusive") {
				t.Fatalf("unexpected error = %v", err)
			}
		})
	}
}

func TestRunFetchAllRejectsOutputOverride(t *testing.T) {
	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "", fetchOptions{
		All:    true,
		Output: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for --all with --output")
	}
	if !strings.Contains(err.Error(), "cannot be combined with --output") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestNewFetchCommandParsesFlagsAndRunsFetch(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)
	outputDir := t.TempDir()

	proj := "org/proj"
	mockCloudStackForFetch(t, &fetchMockConfig{
		recordsByProject: map[string][]repository.Record{
			proj: {{ID: "s1", Date: "2025-01-01", ProjectID: proj}},
		},
		dataFiles: map[string][]repository.RecordDataFile{
			"s1": {{ID: 1, RecordID: "s1", Filename: "a.txt", S3Key: "data/s1/a.txt"}},
		},
		s3Data: map[string]string{"data/s1/a.txt": "alpha"},
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := newFetchCommand(stdout, stderr)
	cmd.SetArgs([]string{"--project", proj, "--output", outputDir})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Downloaded 1 file(s)") {
		t.Fatalf("stdout = %q, want download summary", stdout.String())
	}
}

func TestNewFetchCommandPassesRecordIDArgument(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)
	outputDir := t.TempDir()

	mockCloudStackForFetch(t, &fetchMockConfig{
		records: map[string]repository.Record{
			"record-arg": {ID: "record-arg", Date: "2025-03-01"},
		},
		dataFiles: map[string][]repository.RecordDataFile{
			"record-arg": {{ID: 1, RecordID: "record-arg", Filename: "a.txt", S3Key: "data/record-arg/a.txt"}},
		},
		s3Data: map[string]string{"data/record-arg/a.txt": "alpha"},
	})

	stdout := &bytes.Buffer{}
	cmd := newFetchCommand(stdout, &bytes.Buffer{})
	cmd.SetArgs([]string{"record-arg", "--output", outputDir})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Downloaded 1 file(s)") {
		t.Fatalf("stdout = %q, want download summary", stdout.String())
	}
}

// --- Cloud not configured ---

func TestRunFetchCloudNotConfigured(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	t.Setenv(pcHomeEnvVar, homeDir)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errCloudNotConfigured
	}

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "record-id", fetchOptions{})
	if err == nil {
		t.Fatal("expected error when cloud is not configured")
	}
	if !strings.Contains(err.Error(), "cloud is not configured") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchCloudOpenError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	t.Setenv(pcHomeEnvVar, homeDir)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errors.New("connection failed")
	}

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "record-id", fetchOptions{})
	if err == nil {
		t.Fatal("expected error on cloud open failure")
	}
	if !strings.Contains(err.Error(), "open cloud") {
		t.Fatalf("unexpected error = %v", err)
	}
}

// --- Record mode ---

func TestRunFetchRecordSuccess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)
	outputDir := t.TempDir()

	mockCloudStackForFetch(t, &fetchMockConfig{
		records: map[string]repository.Record{
			"record-1": {ID: "record-1", Date: "2025-03-01"},
		},
		dataFiles: map[string][]repository.RecordDataFile{
			"record-1": {
				{ID: 1, RecordID: "record-1", Filename: "report.csv", S3Key: "data/record-1/report.csv"},
			},
		},
		s3Data: map[string]string{
			"data/record-1/report.csv": "col1,col2\n1,2\n",
		},
	})

	stdout := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, &bytes.Buffer{}, "record-1", fetchOptions{Output: outputDir})
	if err != nil {
		t.Fatalf("runFetch() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Downloaded 1 file(s)") {
		t.Fatalf("expected download message, got %q", stdout.String())
	}

	// Verify file was written.
	content, err := os.ReadFile(filepath.Join(outputDir, "record-1", "report.csv"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "col1,col2\n1,2\n" {
		t.Fatalf("unexpected file content = %q", string(content))
	}
}

func TestRunFetchRecordNotFound(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "nonexistent", fetchOptions{Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for nonexistent record")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchRecordNoDataFiles(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		records: map[string]repository.Record{
			"record-1": {ID: "record-1", Date: "2025-03-01"},
		},
	})

	stdout := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, &bytes.Buffer{}, "record-1", fetchOptions{Output: t.TempDir()})
	if err != nil {
		t.Fatalf("runFetch() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "No data files") {
		t.Fatalf("expected 'no data files' message, got %q", stdout.String())
	}
}

// --- Project mode ---

func TestRunFetchProjectSuccess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)
	outputDir := t.TempDir()

	proj := "org/proj"
	mockCloudStackForFetch(t, &fetchMockConfig{
		recordsByProject: map[string][]repository.Record{
			proj: {
				{ID: "s1", Date: "2025-01-01", ProjectID: proj},
				{ID: "s2", Date: "2025-01-02", ProjectID: proj},
			},
		},
		dataFiles: map[string][]repository.RecordDataFile{
			"s1": {{ID: 1, RecordID: "s1", Filename: "a.txt", S3Key: "data/s1/a.txt"}},
			"s2": {{ID: 2, RecordID: "s2", Filename: "b.txt", S3Key: "data/s2/b.txt"}},
		},
		s3Data: map[string]string{
			"data/s1/a.txt": "alpha",
			"data/s2/b.txt": "beta",
		},
	})

	stdout := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, &bytes.Buffer{}, "", fetchOptions{Project: proj, Output: outputDir})
	if err != nil {
		t.Fatalf("runFetch() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Downloaded 2 file(s)") {
		t.Fatalf("expected 2 files downloaded, got %q", stdout.String())
	}

	for _, f := range []struct{ record, file, content string }{
		{"s1", "a.txt", "alpha"},
		{"s2", "b.txt", "beta"},
	} {
		data, err := os.ReadFile(filepath.Join(outputDir, f.record, f.file))
		if err != nil {
			t.Fatalf("ReadFile %s/%s error = %v", f.record, f.file, err)
		}
		if string(data) != f.content {
			t.Fatalf("expected %q, got %q", f.content, string(data))
		}
	}
}

// --- Recent mode ---

func TestRunFetchRecentSuccess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)
	outputDir := t.TempDir()

	mockCloudStackForFetch(t, &fetchMockConfig{
		recordsByDateFrom: map[string][]repository.Record{
			"*": {{ID: "r1", Date: "2025-03-01"}},
		},
		dataFiles: map[string][]repository.RecordDataFile{
			"r1": {{ID: 1, RecordID: "r1", Filename: "data.bin", S3Key: "data/r1/data.bin"}},
		},
		s3Data: map[string]string{
			"data/r1/data.bin": "binary",
		},
	})

	stdout := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, &bytes.Buffer{}, "", fetchOptions{Recent: "1m", Output: outputDir})
	if err != nil {
		t.Fatalf("runFetch() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Downloaded 1 file(s)") {
		t.Fatalf("expected download message, got %q", stdout.String())
	}
}

func TestRunFetchRecentInvalidWindow(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "", fetchOptions{Recent: "abc", Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for invalid recent window")
	}
}

// --- S3 download error ---

func TestRunFetchS3DownloadError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		records: map[string]repository.Record{
			"record-1": {ID: "record-1", Date: "2025-03-01"},
		},
		dataFiles: map[string][]repository.RecordDataFile{
			"record-1": {{ID: 1, RecordID: "record-1", Filename: "missing.csv", S3Key: "data/record-1/missing.csv"}},
		},
		s3Error: errors.New("access denied"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "record-1", fetchOptions{Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on S3 download failure")
	}
	if !strings.Contains(err.Error(), "download") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestDownloadS3FileFnWritesAtomically(t *testing.T) {
	client := newTestS3Client(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/test-bucket/data/record/report.csv" {
			t.Fatalf("path = %s, want /test-bucket/data/record/report.csv", r.URL.Path)
		}
		_, _ = w.Write([]byte("a,b\n1,2\n"))
	}))

	destPath := filepath.Join(t.TempDir(), "nested", "report.csv")
	if err := downloadS3FileFn(context.Background(), client, "data/record/report.csv", destPath); err != nil {
		t.Fatalf("downloadS3FileFn() error = %v", err)
	}
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "a,b\n1,2\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestDownloadS3FileFnReportsLocalWriteErrors(t *testing.T) {
	client := newTestS3Client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("content"))
	}))

	blockerPath := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blockerPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := downloadS3FileFn(context.Background(), client, "data/record/report.csv", filepath.Join(blockerPath, "report.csv"))
	if err == nil {
		t.Fatal("expected directory creation error")
	}
	if !strings.Contains(err.Error(), "create directory") {
		t.Fatalf("error = %v, want create directory", err)
	}
}

func TestDownloadS3FileFnReportsS3Errors(t *testing.T) {
	client := newTestS3Client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))

	err := downloadS3FileFn(context.Background(), client, "data/missing.csv", filepath.Join(t.TempDir(), "missing.csv"))
	if err == nil {
		t.Fatal("expected download error")
	}
	if !strings.Contains(err.Error(), "download data/missing.csv") {
		t.Fatalf("error = %v, want S3 download context", err)
	}
}

// --- Default output path ---

func TestRunFetchDefaultOutputPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		records: map[string]repository.Record{
			"record-1": {ID: "record-1", Date: "2025-03-01"},
		},
		dataFiles: map[string][]repository.RecordDataFile{
			"record-1": {{ID: 1, RecordID: "record-1", Filename: "f.txt", S3Key: "data/record-1/f.txt"}},
		},
		s3Data: map[string]string{
			"data/record-1/f.txt": "content",
		},
	})

	stdout := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, &bytes.Buffer{}, "record-1", fetchOptions{})
	if err != nil {
		t.Fatalf("runFetch() error = %v", err)
	}

	expectedDir := filepath.Join(homeDir, "personal-context", "data")
	if !strings.Contains(stdout.String(), expectedDir) {
		t.Fatalf("expected default path in output, got %q", stdout.String())
	}

	// Verify file exists at default path.
	_, err = os.Stat(filepath.Join(expectedDir, "record-1", "f.txt"))
	if err != nil {
		t.Fatalf("expected file at default path: %v", err)
	}
}

// --- All mode ---

func TestRunFetchAllDownloadsMissingAndSkipsVerifiedFiles(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	presentContent := "already local"
	downloadContent := "download me"
	presentHash := hashFetchTestData(presentContent)
	downloadHash := hashFetchTestData(downloadContent)

	presentPath := filepath.Join(homeDir, "personal-context", "data", "r1", "present.txt")
	if err := os.MkdirAll(filepath.Dir(presentPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(presentPath, []byte(presentContent), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &fetchMockConfig{
		recordsAll: []repository.Record{
			{ID: "r1", Date: "2025-01-01"},
			{ID: "r2", Date: "2025-01-02"},
		},
		dataFiles: map[string][]repository.RecordDataFile{
			"r1": {{ID: 1, RecordID: "r1", Filename: "present.txt", S3Key: "data/r1/present.txt", Size: int64(len(presentContent)), Hash: presentHash}},
			"r2": {{ID: 2, RecordID: "r2", Filename: "missing.txt", S3Key: "data/r2/missing.txt", Size: int64(len(downloadContent)), Hash: downloadHash}},
		},
		s3Data: map[string]string{
			"data/r1/present.txt": "should not download",
			"data/r2/missing.txt": downloadContent,
		},
	}
	mockCloudStackForFetch(t, cfg)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runFetch(context.Background(), stdout, stderr, "", fetchOptions{All: true}); err != nil {
		t.Fatalf("runFetch() error = %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{
		"Records scanned: 2",
		"Files already present: 1",
		"Files downloaded: 1",
		"Bytes downloaded: 11",
		"Missing/failed files: 0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want %q", out, want)
		}
	}
	if len(cfg.downloadedKeys) != 1 || cfg.downloadedKeys[0] != "data/r2/missing.txt" {
		t.Fatalf("downloaded keys = %v, want only missing file", cfg.downloadedKeys)
	}
	got, err := os.ReadFile(filepath.Join(homeDir, "personal-context", "data", "r2", "missing.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != downloadContent {
		t.Fatalf("downloaded content = %q, want %q", got, downloadContent)
	}
}

func TestRunFetchAllRedownloadsCorruptLocalFile(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	cloudContent := "correct bytes"
	canonicalPath := filepath.Join(homeDir, "personal-context", "data", "r1", "data.txt")
	if err := os.MkdirAll(filepath.Dir(canonicalPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(canonicalPath, []byte("wrong"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &fetchMockConfig{
		recordsAll: []repository.Record{{ID: "r1", Date: "2025-01-01"}},
		dataFiles: map[string][]repository.RecordDataFile{
			"r1": {{ID: 1, RecordID: "r1", Filename: "data.txt", S3Key: "data/r1/data.txt", Size: int64(len(cloudContent)), Hash: hashFetchTestData(cloudContent)}},
		},
		s3Data: map[string]string{"data/r1/data.txt": cloudContent},
	}
	mockCloudStackForFetch(t, cfg)

	stdout := &bytes.Buffer{}
	if err := runFetch(context.Background(), stdout, &bytes.Buffer{}, "", fetchOptions{All: true}); err != nil {
		t.Fatalf("runFetch() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Files downloaded: 1") {
		t.Fatalf("stdout = %q, want one download", stdout.String())
	}
	got, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != cloudContent {
		t.Fatalf("content after fetch = %q, want %q", got, cloudContent)
	}
}

func TestRunFetchAllReportsDownloadFailuresAfterSummary(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	cfg := &fetchMockConfig{
		recordsAll: []repository.Record{{ID: "r1", Date: "2025-01-01"}},
		dataFiles: map[string][]repository.RecordDataFile{
			"r1": {{ID: 1, RecordID: "r1", Filename: "missing.txt", S3Key: "data/r1/missing.txt", Size: 7, Hash: hashFetchTestData("missing")}},
		},
		s3Error: errors.New("not found"),
	}
	mockCloudStackForFetch(t, cfg)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, stderr, "", fetchOptions{All: true})
	if err == nil {
		t.Fatal("expected fetch --all failure")
	}
	if !strings.Contains(err.Error(), "fetch --all failed") {
		t.Fatalf("unexpected error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Missing/failed files: 1") {
		t.Fatalf("stdout = %q, want failed count", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Failed: r1/missing.txt") {
		t.Fatalf("stderr = %q, want failed file detail", stderr.String())
	}
}

func TestRunFetchAllReportsVerificationFailures(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	cfg := &fetchMockConfig{
		recordsAll: []repository.Record{{ID: "r1", Date: "2025-01-01"}},
		dataFiles: map[string][]repository.RecordDataFile{
			"r1": {{ID: 1, RecordID: "r1", Filename: "data.txt", S3Key: "data/r1/data.txt", Size: 5, Hash: hashFetchTestData("right")}},
		},
		s3Data: map[string]string{"data/r1/data.txt": "wrong"},
	}
	mockCloudStackForFetch(t, cfg)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, stderr, "", fetchOptions{All: true})
	if err == nil {
		t.Fatal("expected verification failure")
	}
	if !strings.Contains(err.Error(), "verify downloaded file") {
		t.Fatalf("unexpected error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Missing/failed files: 1") {
		t.Fatalf("stdout = %q, want failed count", stdout.String())
	}
	if !strings.Contains(stderr.String(), "hash mismatch") {
		t.Fatalf("stderr = %q, want hash mismatch detail", stderr.String())
	}
}

func TestNewFetchCommandParsesAllFlag(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		recordsAll: []repository.Record{{ID: "r1", Date: "2025-01-01"}},
	})

	stdout := &bytes.Buffer{}
	cmd := newFetchCommand(stdout, &bytes.Buffer{})
	cmd.SetArgs([]string{"--all"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Records scanned: 1") {
		t.Fatalf("stdout = %q, want all-mode summary", stdout.String())
	}
}

func TestRunFetchAllListRecordsError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		listRecordsErr: errors.New("db unavailable"),
	})

	stdout := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, &bytes.Buffer{}, "", fetchOptions{All: true})
	if err == nil {
		t.Fatal("expected list records error")
	}
	if !strings.Contains(err.Error(), "list non-deleted records") {
		t.Fatalf("unexpected error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Records scanned: 0") {
		t.Fatalf("stdout = %q, want zero-record summary", stdout.String())
	}
}

func TestRunFetchAllListDataFilesError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	downloadContent := "good bytes"
	cfg := &fetchMockConfig{
		recordsAll: []repository.Record{
			{ID: "r1", Date: "2025-01-01"},
			{ID: "r2", Date: "2025-01-02"},
		},
		listDataFilesErrByRecord: map[string]error{
			"r1": errors.New("data query failed"),
		},
		dataFiles: map[string][]repository.RecordDataFile{
			"r2": {{ID: 1, RecordID: "r2", Filename: "ok.txt", S3Key: "data/r2/ok.txt", Size: int64(len(downloadContent)), Hash: hashFetchTestData(downloadContent)}},
		},
		s3Data: map[string]string{"data/r2/ok.txt": downloadContent},
	}
	mockCloudStackForFetch(t, cfg)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, stderr, "", fetchOptions{All: true})
	if err == nil {
		t.Fatal("expected aggregated fetch --all failure")
	}
	if !strings.Contains(err.Error(), "fetch --all failed") {
		t.Fatalf("unexpected error = %v", err)
	}
	if !strings.Contains(stderr.String(), "Failed: record r1: list data files: data query failed") {
		t.Fatalf("stderr = %q, want per-record listing failure detail", stderr.String())
	}
	for _, want := range []string{
		"Records scanned: 2",
		"Files downloaded: 1",
		"Missing/failed files: 1",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q (loop should continue past failed record)", stdout.String(), want)
		}
	}
	if len(cfg.downloadedKeys) != 1 || cfg.downloadedKeys[0] != "data/r2/ok.txt" {
		t.Fatalf("downloaded keys = %v, want r2 file only", cfg.downloadedKeys)
	}
}

func TestRunFetchAllReportsResolvePathFailures(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	cfg := &fetchMockConfig{
		recordsAll: []repository.Record{{ID: "r1", Date: "2025-01-01"}},
		dataFiles: map[string][]repository.RecordDataFile{
			"r1": {{ID: 1, RecordID: "r1", Filename: "../bad.txt", S3Key: "data/r1/bad.txt", Size: 3, Hash: hashFetchTestData("bad")}},
		},
		s3Data: map[string]string{"data/r1/bad.txt": "bad"},
	}
	mockCloudStackForFetch(t, cfg)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, stderr, "", fetchOptions{All: true})
	if err == nil {
		t.Fatal("expected resolve path failure")
	}
	if !strings.Contains(err.Error(), "fetch --all failed") {
		t.Fatalf("unexpected error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Missing/failed files: 1") {
		t.Fatalf("stdout = %q, want failed count", stdout.String())
	}
	if !strings.Contains(stderr.String(), "must not include path separators") {
		t.Fatalf("stderr = %q, want path validation detail", stderr.String())
	}
	if len(cfg.downloadedKeys) != 0 {
		t.Fatalf("downloaded keys = %v, want no downloads", cfg.downloadedKeys)
	}
}

func TestRunFetchAllReportsInvalidMetadataFailures(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	cfg := &fetchMockConfig{
		recordsAll: []repository.Record{{ID: "r1", Date: "2025-01-01"}},
		dataFiles: map[string][]repository.RecordDataFile{
			"r1": {{ID: 1, RecordID: "r1", Filename: "data.txt", S3Key: "data/r1/data.txt", Size: 3}},
		},
		s3Data: map[string]string{"data/r1/data.txt": "bad"},
	}
	mockCloudStackForFetch(t, cfg)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, stderr, "", fetchOptions{All: true})
	if err == nil {
		t.Fatal("expected invalid metadata failure")
	}
	if !strings.Contains(err.Error(), "fetch --all failed") {
		t.Fatalf("unexpected error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Missing/failed files: 1") {
		t.Fatalf("stdout = %q, want failed count", stdout.String())
	}
	if !strings.Contains(stderr.String(), "hash is required") {
		t.Fatalf("stderr = %q, want metadata detail", stderr.String())
	}
	if len(cfg.downloadedKeys) != 0 {
		t.Fatalf("downloaded keys = %v, want no downloads", cfg.downloadedKeys)
	}
}

func TestRunFetchAllReportsLocalDirectoryFailures(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	canonicalPath := filepath.Join(homeDir, "personal-context", "data", "r1", "data.txt")
	if err := os.MkdirAll(canonicalPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	cfg := &fetchMockConfig{
		recordsAll: []repository.Record{{ID: "r1", Date: "2025-01-01"}},
		dataFiles: map[string][]repository.RecordDataFile{
			"r1": {{ID: 1, RecordID: "r1", Filename: "data.txt", S3Key: "data/r1/data.txt", Size: 4, Hash: hashFetchTestData("data")}},
		},
		s3Data: map[string]string{"data/r1/data.txt": "data"},
	}
	mockCloudStackForFetch(t, cfg)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, stderr, "", fetchOptions{All: true})
	if err == nil {
		t.Fatal("expected local directory failure")
	}
	if !strings.Contains(err.Error(), "fetch --all failed") {
		t.Fatalf("unexpected error = %v", err)
	}
	if !strings.Contains(stderr.String(), "local file path is a directory") {
		t.Fatalf("stderr = %q, want directory detail", stderr.String())
	}
	if len(cfg.downloadedKeys) != 0 {
		t.Fatalf("downloaded keys = %v, want no downloads", cfg.downloadedKeys)
	}
}

func TestValidateFetchAllDataFile(t *testing.T) {
	valid := repository.RecordDataFile{
		RecordID: "r1",
		Filename: "data.txt",
		S3Key:    "data/r1/data.txt",
		Size:     1,
		Hash:     hashFetchTestData("x"),
	}
	tests := []struct {
		name string
		file repository.RecordDataFile
		want string
	}{
		{"valid", valid, ""},
		{"record id", repository.RecordDataFile{Filename: valid.Filename, S3Key: valid.S3Key, Size: valid.Size, Hash: valid.Hash}, "record_id is required"},
		{"filename", repository.RecordDataFile{RecordID: valid.RecordID, S3Key: valid.S3Key, Size: valid.Size, Hash: valid.Hash}, "filename is required"},
		{"s3 key", repository.RecordDataFile{RecordID: valid.RecordID, Filename: valid.Filename, Size: valid.Size, Hash: valid.Hash}, "s3_key is required"},
		{"size", repository.RecordDataFile{RecordID: valid.RecordID, Filename: valid.Filename, S3Key: valid.S3Key, Size: -1, Hash: valid.Hash}, "size must be non-negative"},
		{"hash", repository.RecordDataFile{RecordID: valid.RecordID, Filename: valid.Filename, S3Key: valid.S3Key, Size: valid.Size}, "hash is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFetchAllDataFile(tt.file)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("validateFetchAllDataFile() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateFetchAllDataFile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLocalDataFileMatchesHashMismatchAndDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	matches, err := localDataFileMatches(repository.RecordDataFile{
		Size: int64(len("abc")),
		Hash: hashFetchTestData("def"),
	}, path)
	if err != nil {
		t.Fatalf("localDataFileMatches() error = %v", err)
	}
	if matches {
		t.Fatal("expected same-size hash mismatch to return false")
	}

	_, err = localDataFileMatches(repository.RecordDataFile{}, dir)
	if err == nil || !strings.Contains(err.Error(), "local file path is a directory") {
		t.Fatalf("localDataFileMatches() error = %v, want directory error", err)
	}
}

func TestVerifyDataFileFailureBranches(t *testing.T) {
	dir := t.TempDir()

	if err := verifyDataFile(repository.RecordDataFile{}, filepath.Join(dir, "missing.txt")); err == nil || !strings.Contains(err.Error(), "stat downloaded file") {
		t.Fatalf("missing verify error = %v, want stat error", err)
	}

	if err := verifyDataFile(repository.RecordDataFile{}, dir); err == nil || !strings.Contains(err.Error(), "downloaded path is a directory") {
		t.Fatalf("directory verify error = %v, want directory error", err)
	}

	path := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := verifyDataFile(repository.RecordDataFile{Size: 4, Hash: hashFetchTestData("abc")}, path); err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("size verify error = %v, want size mismatch", err)
	}
}

func TestSummarizeFetchAllFailures(t *testing.T) {
	// Under the preview cap: every failure is inlined, no truncation suffix.
	short := []string{"r1/a: oops", "r1/b: oops"}
	got := summarizeFetchAllFailures(short)
	want := "2 file(s) missing or failed: r1/a: oops; r1/b: oops"
	if got != want {
		t.Fatalf("summarizeFetchAllFailures(short) = %q, want %q", got, want)
	}

	// Over the preview cap: only the first fetchAllFailuresErrorPreview are inlined,
	// suffix names the remainder, full list expected on stderr.
	long := make([]string, fetchAllFailuresErrorPreview+3)
	for i := range long {
		long[i] = fmt.Sprintf("r%d: oops", i)
	}
	got = summarizeFetchAllFailures(long)
	if !strings.HasPrefix(got, fmt.Sprintf("%d file(s) missing or failed (first %d:", len(long), fetchAllFailuresErrorPreview)) {
		t.Fatalf("summarizeFetchAllFailures(long) prefix wrong: %q", got)
	}
	if !strings.Contains(got, fmt.Sprintf("… and %d more — see stderr", len(long)-fetchAllFailuresErrorPreview)) {
		t.Fatalf("summarizeFetchAllFailures(long) missing remainder suffix: %q", got)
	}
	if strings.Contains(got, "r"+strconv.Itoa(fetchAllFailuresErrorPreview)+":") {
		t.Fatalf("summarizeFetchAllFailures(long) leaked an over-cap entry: %q", got)
	}
}

// --- parseRecentWindow tests ---

func TestParseRecentWindow(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"3d", true},
		{"2w", true},
		{"1m", true},
		{"1y", true},
		{"10d", true},
		{"", false},
		{"d", false},
		{"0d", false},
		{"-1d", false},
		{"3x", false},
		{"abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseRecentWindow(tt.input)
			if tt.valid {
				if err != nil {
					t.Fatalf("parseRecentWindow(%q) error = %v", tt.input, err)
				}
				// Verify it's a valid date.
				if _, parseErr := time.Parse("2006-01-02", result); parseErr != nil {
					t.Fatalf("result %q is not a valid date: %v", result, parseErr)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error for %q, got result %q", tt.input, result)
				}
			}
		})
	}
}

// --- ListRecords error paths ---

func TestRunFetchProjectListRecordsError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		listRecordsErr: errors.New("db error"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "", fetchOptions{Project: "org/proj", Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on list records failure")
	}
	if !strings.Contains(err.Error(), "list project records") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchRecentListRecordsError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		listRecordsErr: errors.New("db error"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "", fetchOptions{Recent: "1d", Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on list records failure")
	}
	if !strings.Contains(err.Error(), "list recent records") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchRecordGetRecordError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		getRecordErr: errors.New("db error"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "record-1", fetchOptions{Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on get record failure")
	}
	if !strings.Contains(err.Error(), "get record") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchRecordListDataFilesError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		records: map[string]repository.Record{
			"record-1": {ID: "record-1", Date: "2025-03-01"},
		},
		listDataFilesErr: errors.New("db error"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "record-1", fetchOptions{Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on list data files failure")
	}
	if !strings.Contains(err.Error(), "list data files") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchProjectCollectDataFilesError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	proj := "org/proj"
	mockCloudStackForFetch(t, &fetchMockConfig{
		recordsByProject: map[string][]repository.Record{
			proj: {{ID: "s1", Date: "2025-01-01", ProjectID: proj}},
		},
		listDataFilesErr: errors.New("data files error"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "", fetchOptions{Project: proj, Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on collect data files failure")
	}
	if !strings.Contains(err.Error(), "list data files for record") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchPathTraversalSanitized(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	outputDir := t.TempDir()
	mockCloudStackForFetch(t, &fetchMockConfig{
		records: map[string]repository.Record{
			"s1": {ID: "s1", Date: "2025-01-01"},
		},
		dataFiles: map[string][]repository.RecordDataFile{
			"s1": {{RecordID: "../../etc", Filename: "../passwd", S3Key: "k"}},
		},
		s3Data: map[string]string{"k": "data"},
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "s1", fetchOptions{Output: outputDir})
	if err != nil {
		t.Fatalf("unexpected error = %v", err)
	}

	// Verify the file was written safely inside outputDir (traversal stripped).
	safePath := filepath.Join(outputDir, "etc", "passwd")
	if _, statErr := os.Stat(safePath); statErr != nil {
		t.Fatalf("expected file at sanitized path %s, got error: %v", safePath, statErr)
	}

	// Verify no file was written outside outputDir.
	unsafePath := filepath.Join(outputDir, "..", "..", "etc", "passwd")
	if _, statErr := os.Stat(unsafePath); statErr == nil {
		t.Fatal("traversal was NOT sanitized — file written outside output directory")
	}
}

func TestRunFetchPathTraversalEmptyComponentRejected(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	// filepath.Base("") returns ".", which is rejected.
	mockCloudStackForFetch(t, &fetchMockConfig{
		records: map[string]repository.Record{
			"s1": {ID: "s1", Date: "2025-01-01"},
		},
		dataFiles: map[string][]repository.RecordDataFile{
			"s1": {{RecordID: "s1", Filename: "", S3Key: "k"}},
		},
		s3Data: map[string]string{"k": "data"},
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "s1", fetchOptions{Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for empty filename")
	}
	if !strings.Contains(err.Error(), "invalid path component") {
		t.Fatalf("unexpected error = %v", err)
	}
}

// --- Test helpers ---

// fetchMockConfig configures the mock cloud stack for fetch tests.
type fetchMockConfig struct {
	records           map[string]repository.Record // keyed by record ID
	recordsAll        []repository.Record
	recordsByProject  map[string][]repository.Record         // keyed by project ID
	recordsByDateFrom map[string][]repository.Record         // keyed by "*" (any date from)
	dataFiles         map[string][]repository.RecordDataFile // keyed by record ID
	s3Data            map[string]string                      // keyed by S3 key
	s3Error           error
	getRecordErr      error
	listRecordsErr    error
	listDataFilesErr  error
	listDataFilesErrByRecord map[string]error
	downloadedKeys    []string
}

// mockCloudStackForFetch sets up openCloudStackFn and downloadS3FileFn for fetch tests.
func mockCloudStackForFetch(t *testing.T, cfg *fetchMockConfig) {
	t.Helper()

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })

	repo := &fetchMockRepo{cfg: cfg}

	openCloudStackFn = func(_ context.Context, _, _ string) (*cloudStack, error) {
		return &cloudStack{
			Repo: repo,
		}, nil
	}

	origDownload := downloadS3FileFn
	t.Cleanup(func() { downloadS3FileFn = origDownload })

	downloadS3FileFn = func(_ context.Context, _ *pcs3.Client, key string, destPath string) error {
		if cfg.s3Error != nil {
			return cfg.s3Error
		}
		content, ok := cfg.s3Data[key]
		if !ok {
			return errors.New("not found: " + key)
		}
		cfg.downloadedKeys = append(cfg.downloadedKeys, key)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(destPath, []byte(content), 0o644)
	}
}

func newTestS3Client(t *testing.T, handler http.Handler) *pcs3.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := aws.Config{
		Region:      "us-east-1",
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("test", "test", "")),
		HTTPClient:  server.Client(),
	}
	s3Client := awss3.NewFromConfig(cfg, func(o *awss3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})
	client, err := pcs3.New(s3Client, "test-bucket", "")
	if err != nil {
		t.Fatalf("s3client.New() error = %v", err)
	}
	return client
}

// fetchMockRepo is a mock repository for fetch tests.
type fetchMockRepo struct {
	mockRepo
	cfg *fetchMockConfig
}

func (m *fetchMockRepo) GetRecordByID(_ context.Context, id string) (repository.Record, error) {
	if m.cfg.getRecordErr != nil {
		return repository.Record{}, m.cfg.getRecordErr
	}
	record, ok := m.cfg.records[id]
	if !ok {
		return repository.Record{}, repository.ErrNotFound
	}
	return record, nil
}

func (m *fetchMockRepo) ListRecords(_ context.Context, filter repository.ListRecordsFilter) ([]repository.Record, error) {
	if m.cfg.listRecordsErr != nil {
		return nil, m.cfg.listRecordsErr
	}
	if filter.ProjectID != nil {
		return m.cfg.recordsByProject[*filter.ProjectID], nil
	}
	if filter.DateFrom != nil {
		return m.cfg.recordsByDateFrom["*"], nil
	}
	return m.cfg.recordsAll, nil
}

func (m *fetchMockRepo) ListRecordDataFilesByRecordID(_ context.Context, recordID string) ([]repository.RecordDataFile, error) {
	if err, ok := m.cfg.listDataFilesErrByRecord[recordID]; ok {
		return nil, err
	}
	if m.cfg.listDataFilesErr != nil {
		return nil, m.cfg.listDataFilesErr
	}
	return m.cfg.dataFiles[recordID], nil
}

func hashFetchTestData(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
