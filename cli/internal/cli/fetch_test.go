package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	pcs3 "github.com/conn-castle/personal-context/cli/internal/s3client"
)

// --- Argument validation tests ---

func TestRunFetchNoModeSelector(t *testing.T) {
	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "", fetchOptions{})
	if err == nil {
		t.Fatal("expected error when no mode selector")
	}
	if !strings.Contains(err.Error(), "specify a slide ID") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchMultipleModeSelectors(t *testing.T) {
	tests := []struct {
		name    string
		slideID string
		opts    fetchOptions
	}{
		{"slide+project", "abc", fetchOptions{Project: "org/proj"}},
		{"slide+recent", "abc", fetchOptions{Recent: "3d"}},
		{"project+recent", "", fetchOptions{Project: "org/proj", Recent: "3d"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, tt.slideID, tt.opts)
			if err == nil {
				t.Fatal("expected error for multiple mode selectors")
			}
			if !strings.Contains(err.Error(), "mutually exclusive") {
				t.Fatalf("unexpected error = %v", err)
			}
		})
	}
}

// --- Cloud not configured ---

func TestRunFetchCloudNotConfigured(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	t.Setenv(pcHomeEnvVar, homeDir)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string) (*cloudStack, error) {
		return nil, errCloudNotConfigured
	}

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "slide-id", fetchOptions{})
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
	openCloudStackFn = func(context.Context, string) (*cloudStack, error) {
		return nil, errors.New("connection failed")
	}

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "slide-id", fetchOptions{})
	if err == nil {
		t.Fatal("expected error on cloud open failure")
	}
	if !strings.Contains(err.Error(), "open cloud") {
		t.Fatalf("unexpected error = %v", err)
	}
}

// --- Slide mode ---

func TestRunFetchSlideSuccess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)
	outputDir := t.TempDir()

	mockCloudStackForFetch(t, &fetchMockConfig{
		slides: map[string]repository.Slide{
			"slide-1": {ID: "slide-1", Date: "2025-03-01"},
		},
		dataFiles: map[string][]repository.SlideDataFile{
			"slide-1": {
				{ID: 1, SlideID: "slide-1", Filename: "report.csv", S3Key: "data/slide-1/report.csv"},
			},
		},
		s3Data: map[string]string{
			"data/slide-1/report.csv": "col1,col2\n1,2\n",
		},
	})

	stdout := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, &bytes.Buffer{}, "slide-1", fetchOptions{Output: outputDir})
	if err != nil {
		t.Fatalf("runFetch() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Downloaded 1 file(s)") {
		t.Fatalf("expected download message, got %q", stdout.String())
	}

	// Verify file was written.
	content, err := os.ReadFile(filepath.Join(outputDir, "slide-1", "report.csv"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "col1,col2\n1,2\n" {
		t.Fatalf("unexpected file content = %q", string(content))
	}
}

func TestRunFetchSlideNotFound(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "nonexistent", fetchOptions{Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for nonexistent slide")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchSlideNoDataFiles(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		slides: map[string]repository.Slide{
			"slide-1": {ID: "slide-1", Date: "2025-03-01"},
		},
	})

	stdout := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, &bytes.Buffer{}, "slide-1", fetchOptions{Output: t.TempDir()})
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
		slidesByProject: map[string][]repository.Slide{
			proj: {
				{ID: "s1", Date: "2025-01-01", ProjectID: &proj},
				{ID: "s2", Date: "2025-01-02", ProjectID: &proj},
			},
		},
		dataFiles: map[string][]repository.SlideDataFile{
			"s1": {{ID: 1, SlideID: "s1", Filename: "a.txt", S3Key: "data/s1/a.txt"}},
			"s2": {{ID: 2, SlideID: "s2", Filename: "b.txt", S3Key: "data/s2/b.txt"}},
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

	for _, f := range []struct{ slide, file, content string }{
		{"s1", "a.txt", "alpha"},
		{"s2", "b.txt", "beta"},
	} {
		data, err := os.ReadFile(filepath.Join(outputDir, f.slide, f.file))
		if err != nil {
			t.Fatalf("ReadFile %s/%s error = %v", f.slide, f.file, err)
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
		slidesByDateFrom: map[string][]repository.Slide{
			"*": {{ID: "r1", Date: "2025-03-01"}},
		},
		dataFiles: map[string][]repository.SlideDataFile{
			"r1": {{ID: 1, SlideID: "r1", Filename: "data.bin", S3Key: "data/r1/data.bin"}},
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
		slides: map[string]repository.Slide{
			"slide-1": {ID: "slide-1", Date: "2025-03-01"},
		},
		dataFiles: map[string][]repository.SlideDataFile{
			"slide-1": {{ID: 1, SlideID: "slide-1", Filename: "missing.csv", S3Key: "data/slide-1/missing.csv"}},
		},
		s3Error: errors.New("access denied"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "slide-1", fetchOptions{Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on S3 download failure")
	}
	if !strings.Contains(err.Error(), "download") {
		t.Fatalf("unexpected error = %v", err)
	}
}

// --- Default output path ---

func TestRunFetchDefaultOutputPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		slides: map[string]repository.Slide{
			"slide-1": {ID: "slide-1", Date: "2025-03-01"},
		},
		dataFiles: map[string][]repository.SlideDataFile{
			"slide-1": {{ID: 1, SlideID: "slide-1", Filename: "f.txt", S3Key: "data/slide-1/f.txt"}},
		},
		s3Data: map[string]string{
			"data/slide-1/f.txt": "content",
		},
	})

	stdout := &bytes.Buffer{}
	err := runFetch(context.Background(), stdout, &bytes.Buffer{}, "slide-1", fetchOptions{})
	if err != nil {
		t.Fatalf("runFetch() error = %v", err)
	}

	expectedDir := filepath.Join(homeDir, "personal-context", "data")
	if !strings.Contains(stdout.String(), expectedDir) {
		t.Fatalf("expected default path in output, got %q", stdout.String())
	}

	// Verify file exists at default path.
	_, err = os.Stat(filepath.Join(expectedDir, "slide-1", "f.txt"))
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

// --- ListSlides error paths ---

func TestRunFetchProjectListSlidesError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		listSlidesErr: errors.New("db error"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "", fetchOptions{Project: "org/proj", Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on list slides failure")
	}
	if !strings.Contains(err.Error(), "list project slides") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchRecentListSlidesError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		listSlidesErr: errors.New("db error"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "", fetchOptions{Recent: "1d", Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on list slides failure")
	}
	if !strings.Contains(err.Error(), "list recent slides") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchSlideGetSlideError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		getSlideErr: errors.New("db error"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "slide-1", fetchOptions{Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on get slide failure")
	}
	if !strings.Contains(err.Error(), "get slide") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunFetchSlideListDataFilesError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockCloudStackForFetch(t, &fetchMockConfig{
		slides: map[string]repository.Slide{
			"slide-1": {ID: "slide-1", Date: "2025-03-01"},
		},
		listDataFilesErr: errors.New("db error"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "slide-1", fetchOptions{Output: t.TempDir()})
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
		slidesByProject: map[string][]repository.Slide{
			proj: {{ID: "s1", Date: "2025-01-01", ProjectID: &proj}},
		},
		listDataFilesErr: errors.New("data files error"),
	})

	err := runFetch(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "", fetchOptions{Project: proj, Output: t.TempDir()})
	if err == nil {
		t.Fatal("expected error on collect data files failure")
	}
	if !strings.Contains(err.Error(), "list data files for slide") {
		t.Fatalf("unexpected error = %v", err)
	}
}

// --- Test helpers ---

// fetchMockConfig configures the mock cloud stack for fetch tests.
type fetchMockConfig struct {
	slides           map[string]repository.Slide           // keyed by slide ID
	slidesByProject  map[string][]repository.Slide         // keyed by project ID
	slidesByDateFrom map[string][]repository.Slide         // keyed by "*" (any date from)
	dataFiles        map[string][]repository.SlideDataFile // keyed by slide ID
	s3Data           map[string]string                     // keyed by S3 key
	s3Error          error
	getSlideErr      error
	listSlidesErr    error
	listDataFilesErr error
}

// mockCloudStackForFetch sets up openCloudStackFn and downloadS3FileFn for fetch tests.
func mockCloudStackForFetch(t *testing.T, cfg *fetchMockConfig) {
	t.Helper()

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })

	repo := &fetchMockRepo{cfg: cfg}

	openCloudStackFn = func(_ context.Context, _ string) (*cloudStack, error) {
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

// fetchMockRepo is a mock repository for fetch tests.
type fetchMockRepo struct {
	mockRepo
	cfg *fetchMockConfig
}

func (m *fetchMockRepo) GetSlideByID(_ context.Context, id string) (repository.Slide, error) {
	if m.cfg.getSlideErr != nil {
		return repository.Slide{}, m.cfg.getSlideErr
	}
	slide, ok := m.cfg.slides[id]
	if !ok {
		return repository.Slide{}, repository.ErrNotFound
	}
	return slide, nil
}

func (m *fetchMockRepo) ListSlides(_ context.Context, filter repository.ListSlidesFilter) ([]repository.Slide, error) {
	if m.cfg.listSlidesErr != nil {
		return nil, m.cfg.listSlidesErr
	}
	if filter.ProjectID != nil {
		return m.cfg.slidesByProject[*filter.ProjectID], nil
	}
	if filter.DateFrom != nil {
		return m.cfg.slidesByDateFrom["*"], nil
	}
	return nil, nil
}

func (m *fetchMockRepo) ListSlideDataFilesBySlideID(_ context.Context, slideID string) ([]repository.SlideDataFile, error) {
	if m.cfg.listDataFilesErr != nil {
		return nil, m.cfg.listDataFilesErr
	}
	return m.cfg.dataFiles[slideID], nil
}
