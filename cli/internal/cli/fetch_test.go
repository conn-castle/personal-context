package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		name    string
		recordID string
		opts    fetchOptions
	}{
		{"record+project", "abc", fetchOptions{Project: "org/proj"}},
		{"record+recent", "abc", fetchOptions{Recent: "3d"}},
		{"project+recent", "", fetchOptions{Project: "org/proj", Recent: "3d"}},
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
	records           map[string]repository.Record           // keyed by record ID
	recordsByProject  map[string][]repository.Record         // keyed by project ID
	recordsByDateFrom map[string][]repository.Record         // keyed by "*" (any date from)
	dataFiles        map[string][]repository.RecordDataFile // keyed by record ID
	s3Data           map[string]string                     // keyed by S3 key
	s3Error          error
	getRecordErr      error
	listRecordsErr    error
	listDataFilesErr error
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
	return nil, nil
}

func (m *fetchMockRepo) ListRecordDataFilesByRecordID(_ context.Context, recordID string) ([]repository.RecordDataFile, error) {
	if m.cfg.listDataFilesErr != nil {
		return nil, m.cfg.listDataFilesErr
	}
	return m.cfg.dataFiles[recordID], nil
}
