package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/repository"

	_ "modernc.org/sqlite"
)

// mockRepo is a minimal repository mock for unit-testing command logic.
type mockRepo struct {
	getTemplateByNameFn func(ctx context.Context, name string) (repository.Template, error)
	createTemplateFn    func(ctx context.Context, input repository.CreateTemplateInput) (repository.Template, error)
}

func (m *mockRepo) GetTemplateByName(ctx context.Context, name string) (repository.Template, error) {
	if m.getTemplateByNameFn != nil {
		return m.getTemplateByNameFn(ctx, name)
	}
	return repository.Template{}, repository.ErrNotFound
}

func (m *mockRepo) CreateTemplate(ctx context.Context, input repository.CreateTemplateInput) (repository.Template, error) {
	if m.createTemplateFn != nil {
		return m.createTemplateFn(ctx, input)
	}
	return repository.Template{}, nil
}

// Stubs for remaining Repository interface methods.
func (m *mockRepo) CreateSlide(context.Context, repository.CreateSlideInput) (repository.Slide, error) {
	return repository.Slide{}, nil
}
func (m *mockRepo) GetSlideByID(context.Context, string) (repository.Slide, error) {
	return repository.Slide{}, nil
}
func (m *mockRepo) UpdateSlide(context.Context, repository.UpdateSlideInput) (repository.Slide, error) {
	return repository.Slide{}, nil
}
func (m *mockRepo) ListSlides(context.Context, repository.ListSlidesFilter) ([]repository.Slide, error) {
	return nil, nil
}
func (m *mockRepo) SoftDeleteSlide(context.Context, string) error { return nil }
func (m *mockRepo) RestoreSlide(context.Context, string) error    { return nil }
func (m *mockRepo) DeleteSlide(context.Context, string) error     { return nil }
func (m *mockRepo) CreateSlideFigure(context.Context, repository.CreateSlideFigureInput) (repository.SlideFigure, error) {
	return repository.SlideFigure{}, nil
}
func (m *mockRepo) GetSlideFigureByID(context.Context, int64) (repository.SlideFigure, error) {
	return repository.SlideFigure{}, nil
}
func (m *mockRepo) UpdateSlideFigure(context.Context, repository.UpdateSlideFigureInput) (repository.SlideFigure, error) {
	return repository.SlideFigure{}, nil
}
func (m *mockRepo) ListSlideFiguresBySlideID(context.Context, string) ([]repository.SlideFigure, error) {
	return nil, nil
}
func (m *mockRepo) DeleteSlideFigure(context.Context, int64) error { return nil }
func (m *mockRepo) CreateSlideDataFile(context.Context, repository.CreateSlideDataFileInput) (repository.SlideDataFile, error) {
	return repository.SlideDataFile{}, nil
}
func (m *mockRepo) GetSlideDataFileByID(context.Context, int64) (repository.SlideDataFile, error) {
	return repository.SlideDataFile{}, nil
}
func (m *mockRepo) UpdateSlideDataFile(context.Context, repository.UpdateSlideDataFileInput) (repository.SlideDataFile, error) {
	return repository.SlideDataFile{}, nil
}
func (m *mockRepo) ListSlideDataFilesBySlideID(context.Context, string) ([]repository.SlideDataFile, error) {
	return nil, nil
}
func (m *mockRepo) DeleteSlideDataFile(context.Context, int64) error { return nil }
func (m *mockRepo) UpdateTemplate(context.Context, repository.UpdateTemplateInput) (repository.Template, error) {
	return repository.Template{}, nil
}
func (m *mockRepo) ListTemplates(context.Context) ([]repository.Template, error) { return nil, nil }
func (m *mockRepo) DeleteTemplate(context.Context, string) error                 { return nil }
func (m *mockRepo) GetSyncVersion(context.Context) (repository.SyncVersion, error) {
	return repository.SyncVersion{}, nil
}

func TestSeedTemplatesAllNew(t *testing.T) {
	created := make(map[string]bool)
	repo := &mockRepo{
		getTemplateByNameFn: func(_ context.Context, _ string) (repository.Template, error) {
			return repository.Template{}, repository.ErrNotFound
		},
		createTemplateFn: func(_ context.Context, input repository.CreateTemplateInput) (repository.Template, error) {
			created[input.Name] = true
			return repository.Template{Name: input.Name}, nil
		},
	}

	if err := seedTemplates(context.Background(), repo); err != nil {
		t.Fatalf("seedTemplates() error = %v", err)
	}

	for _, tmpl := range builtinTemplates {
		if !created[tmpl.Name] {
			t.Fatalf("template %q was not created", tmpl.Name)
		}
	}
}

func TestSeedTemplatesSkipsExisting(t *testing.T) {
	repo := &mockRepo{
		getTemplateByNameFn: func(_ context.Context, _ string) (repository.Template, error) {
			return repository.Template{}, nil // already exists
		},
		createTemplateFn: func(_ context.Context, _ repository.CreateTemplateInput) (repository.Template, error) {
			t.Fatal("CreateTemplate should not be called for existing templates")
			return repository.Template{}, nil
		},
	}

	if err := seedTemplates(context.Background(), repo); err != nil {
		t.Fatalf("seedTemplates() error = %v", err)
	}
}

func TestSeedTemplatesFailsOnCheckError(t *testing.T) {
	repo := &mockRepo{
		getTemplateByNameFn: func(_ context.Context, _ string) (repository.Template, error) {
			return repository.Template{}, errors.New("db connection lost")
		},
	}

	err := seedTemplates(context.Background(), repo)
	if err == nil {
		t.Fatal("expected error when GetTemplateByName fails with non-ErrNotFound")
	}
}

func TestSeedTemplatesFailsOnCreateError(t *testing.T) {
	repo := &mockRepo{
		getTemplateByNameFn: func(_ context.Context, _ string) (repository.Template, error) {
			return repository.Template{}, repository.ErrNotFound
		},
		createTemplateFn: func(_ context.Context, _ repository.CreateTemplateInput) (repository.Template, error) {
			return repository.Template{}, errors.New("insert failed")
		},
	}

	err := seedTemplates(context.Background(), repo)
	if err == nil {
		t.Fatal("expected error when CreateTemplate fails")
	}
}

func TestEnsureConfigSkipsExistingValidConfig(t *testing.T) {
	homeDir := t.TempDir()
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	// Write a valid config first
	if err := store.Write(config.Config{}); err != nil {
		t.Fatalf("Write error = %v", err)
	}

	// ensureConfig should skip
	if err := ensureConfig(store); err != nil {
		t.Fatalf("ensureConfig() error = %v", err)
	}
}

func TestEnsureConfigCreatesWhenMissing(t *testing.T) {
	homeDir := t.TempDir()
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	// No config file exists
	if err := ensureConfig(store); err != nil {
		t.Fatalf("ensureConfig() error = %v", err)
	}

	// Verify it was created
	cfg, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	mode, err := cfg.Mode()
	if err != nil {
		t.Fatalf("Mode() error = %v", err)
	}
	if mode != config.ModeLocalOnly {
		t.Fatalf("expected local-only, got %q", mode)
	}
}

func TestEnsureConfigFailsOnInvalidExistingConfig(t *testing.T) {
	homeDir := t.TempDir()
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}

	configPath := store.Path()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	if err := ensureConfig(store); err == nil {
		t.Fatal("expected ensureConfig to fail for invalid existing config")
	}
}

func TestRunSetupWithBlockedDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Create a file where the .pc dir should be to block MkdirAll
	blocker := filepath.Join(homeDir, "personal-context", ".pc")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	if err := os.WriteFile(blocker, []byte("block"), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runSetup(context.Background(), stdout, stderr)
	if err == nil {
		t.Fatal("expected error when directory creation blocked")
	}
}

func TestOpenLocalStackFailsOnBadDBPath(t *testing.T) {
	homeDir := t.TempDir()
	// Write a valid config so config read passes
	store, _ := config.NewStore(homeDir)
	_ = store.Write(config.Config{})

	// Block the DB path by creating a directory named "pc.db" so Open fails
	dbFile := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	if err := os.MkdirAll(dbFile, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	_, err := openLocalStack(homeDir)
	if err == nil {
		t.Fatal("expected error when DB path is a directory")
	}
}

func TestRunSetupDatabaseOpenFails(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Create .pc dir to pass MkdirAll, but block pc.db
	pcDir := filepath.Join(homeDir, "personal-context", ".pc")
	if err := os.MkdirAll(pcDir, 0o700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	// Create a directory named "pc.db" to make sqlite.Open fail
	if err := os.MkdirAll(filepath.Join(pcDir, "pc.db"), 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when database open fails")
	}
}

func TestRunSetupSeedTemplatesError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Do a clean setup first
	stdout := &bytes.Buffer{}
	err := runSetup(context.Background(), stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("initial setup failed: %v", err)
	}

	// Corrupt the templates table to make seed fail
	db := openTestDBInternal(t, homeDir)
	if _, err := db.Exec("DROP TABLE templates"); err != nil {
		t.Fatalf("drop templates: %v", err)
	}
	db.Close()

	// Re-run setup — should fail on seed
	stdout.Reset()
	err = runSetup(context.Background(), stdout, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when templates table is missing")
	}
}

func TestRunSetupConfigStoreWriteCoversPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Do a full setup — this covers the config creation path
	stdout := &bytes.Buffer{}
	if err := runSetup(context.Background(), stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runSetup() error = %v", err)
	}

	// Verify config was written
	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config.json not created: %v", err)
	}

	// Run again — config exists, should skip write
	stdout.Reset()
	if err := runSetup(context.Background(), stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("re-runSetup() error = %v", err)
	}
}

func TestOpenLocalStackEmptyHomeDir(t *testing.T) {
	_, err := openLocalStack("")
	if err == nil {
		t.Fatal("expected error for empty homeDir")
	}
}

// openTestDBInternal opens a raw SQLite connection for test verification.
func openTestDBInternal(t *testing.T, homeDir string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	return db
}
