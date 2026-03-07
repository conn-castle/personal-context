package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/personal-context/cli/internal/repository"

	_ "modernc.org/sqlite"
)

// Tests for error paths that are hard to reach through happy-path integration tests.

// --- resolveHomeDir error for each command ---

func withBrokenHomeDir(t *testing.T, fn func()) {
	t.Helper()
	old := resolveHomeDirFn
	t.Cleanup(func() { resolveHomeDirFn = old })
	resolveHomeDirFn = func() (string, error) {
		return "", errors.New("home dir unavailable")
	}
	fn()
}

func TestAddHomeDirError(t *testing.T) {
	withBrokenHomeDir(t, func() {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"add", "/tmp/fake"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestShowHomeDirError(t *testing.T) {
	withBrokenHomeDir(t, func() {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"show", "some-id"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestEditHomeDirError(t *testing.T) {
	withBrokenHomeDir(t, func() {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"edit", "some-id", "/tmp/fake"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestDeleteHomeDirError(t *testing.T) {
	withBrokenHomeDir(t, func() {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"delete", "some-id"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestRestoreHomeDirError(t *testing.T) {
	withBrokenHomeDir(t, func() {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"restore", "some-id"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestMoveHomeDirError(t *testing.T) {
	withBrokenHomeDir(t, func() {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"move", "some-id", "--date", "2025-01-01"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSetupHomeDirError(t *testing.T) {
	withBrokenHomeDir(t, func() {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"setup"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestDefaultResolveHomeDirUserHomeFallback(t *testing.T) {
	t.Setenv("PC_HOME", "")

	home, err := defaultResolveHomeDir()
	if err != nil {
		t.Fatalf("defaultResolveHomeDir() error = %v", err)
	}
	if home == "" {
		t.Fatal("expected non-empty home")
	}
}

// --- Commands without setup (openLocalStack error on config read) ---

func TestAddWithoutSetup(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>x</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error without setup")
	}
}

func TestShowWithoutSetup(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"show", "some-id"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error without setup")
	}
}

func TestEditWithoutSetup(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>x</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", "some-id", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error without setup")
	}
}

func TestDeleteWithoutSetup(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"delete", "some-id"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error without setup")
	}
}

func TestRestoreWithoutSetup(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"restore", "some-id"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error without setup")
	}
}

func TestMoveWithoutSetup(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"move", "some-id", "--date", "2025-01-01"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error without setup")
	}
}

// --- computeDayOrder: unknown position kind ---

func TestComputeDayOrderUnknownKind(t *testing.T) {
	homeDir := setupEnv(t)

	// Add a slide on the target date so the list is non-empty and the switch is reached
	addSlide(t, "--date", "2025-01-01")

	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stack.Close() }()

	_, err = computeDayOrder(context.Background(), stack.Repo, "2025-01-01", "", position{kind: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown position kind")
	}
}

// --- computeDayOrder: list error ---

type errorListRepo struct{ mockRepo }

func (r *errorListRepo) ListSlides(_ context.Context, _ repository.ListSlidesFilter) ([]repository.Slide, error) {
	return nil, errors.New("list error")
}

func TestComputeDayOrderListError(t *testing.T) {
	_, err := computeDayOrder(context.Background(), &errorListRepo{}, "2025-01-01", "", position{kind: "last"})
	if err == nil {
		t.Fatal("expected error from ListSlides")
	}
}

// --- runAdd: invalid input dir ---

func TestAddInvalidInputDir(t *testing.T) {
	setupEnv(t)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", "/nonexistent/path/to/nowhere"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for nonexistent input dir")
	}
}

// --- DB corruption tests: drop tables to trigger non-ErrNotFound error paths ---

func corruptTable(t *testing.T, homeDir, table string) {
	t.Helper()
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec("DROP TABLE " + table); err != nil {
		t.Fatalf("drop %s: %v", table, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
}

func openErrorPathsDB(t *testing.T, homeDir string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func hasRegularFiles(root string) (bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if entry.IsDir() {
			found, err := hasRegularFiles(path)
			if err != nil {
				return false, err
			}
			if found {
				return true, nil
			}
			continue
		}
		return true, nil
	}
	return false, nil
}

func TestDeleteDBError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	corruptTable(t, homeDir, "slides")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"delete", id})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

func TestRestoreDBError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	corruptTable(t, homeDir, "slides")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"restore", id})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

func TestShowFiguresError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	corruptTable(t, homeDir, "slide_figures")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"show", id})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slide_figures table missing")
	}
}

func TestShowDataFilesError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	corruptTable(t, homeDir, "slide_data_files")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"show", id})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slide_data_files table missing")
	}
}

func TestShowSlidesError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	corruptTable(t, homeDir, "slides")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"show", id})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

func TestEditGetSlideError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	corruptTable(t, homeDir, "slides")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>x</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", id, dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

func TestEditInputParseError(t *testing.T) {
	setupEnv(t)
	id := addSlide(t)

	emptyDir := t.TempDir()

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", id, emptyDir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing slide.html in edit")
	}
}

func TestMoveGetSlideError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	corruptTable(t, homeDir, "slides")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"move", id, "--date", "2025-01-01"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

func TestMoveAfterNonexistentRef(t *testing.T) {
	setupEnv(t)
	addSlide(t, "--date", "2025-07-01")

	id2 := addSlide(t, "--date", "2025-07-01")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"move", id2, "--after", "nonexistent-ref"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for --after nonexistent ref")
	}
}

// --- Edit: old figures list error ---

func TestEditOldFiguresListError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlideWithContent(t,
		`<html><img src="figures/fig.png">body</html>`, "", "",
		map[string][]byte{"fig.png": []byte("data")}, nil,
	)

	// Drop slide_figures to trigger error when listing old figures
	corruptTable(t, homeDir, "slide_figures")

	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(newDir, "slide.html"), []byte("<html>new</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", id, newDir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slide_figures table missing")
	}
}

// --- Edit: old data files list error ---

func TestEditOldDataFilesListError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlideWithContent(t,
		"<html>body</html>", "", "",
		nil, map[string][]byte{"d.csv": []byte("x")},
	)

	// Drop slide_data_files to trigger error when listing old data files
	corruptTable(t, homeDir, "slide_data_files")

	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(newDir, "slide.html"), []byte("<html>new</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", id, newDir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slide_data_files table missing")
	}
}

// --- Add: create slide DB error ---

func TestAddCreateSlideError(t *testing.T) {
	homeDir := setupEnv(t)

	corruptTable(t, homeDir, "slides")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>x</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

// --- Move: mutually exclusive position flags ---

func TestMoveMutuallyExclusiveFlags(t *testing.T) {
	setupEnv(t)
	id := addSlide(t)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"move", id, "--first", "--last"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for mutually exclusive flags")
	}
}

// --- Add: figure copy error (unwritable directory) ---

func TestAddFigureCopyError(t *testing.T) {
	homeDir := setupEnv(t)

	// Make the figures directory unwritable
	figuresDir := filepath.Join(homeDir, "personal-context", "figures")
	if err := os.MkdirAll(figuresDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(figuresDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(figuresDir, 0o755) })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte(`<html><img src="figures/fig.png">x</html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	figDir := filepath.Join(dir, "figures")
	if err := os.MkdirAll(figDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(figDir, "fig.png"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when figures dir is unwritable")
	}
}

// --- Add: data file copy error (unwritable directory) ---

func TestAddDataFileCopyError(t *testing.T) {
	homeDir := setupEnv(t)

	// Make the data directory unwritable
	dataDir := filepath.Join(homeDir, "personal-context", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dataDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dataDir, 0o755) })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>x</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	dDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dDir, "file.csv"), []byte("a,b"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when data dir is unwritable")
	}
}

// --- Edit: figure copy error (unwritable directory) ---

func TestEditFigureCopyError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	// Make the figures directory unwritable
	figuresDir := filepath.Join(homeDir, "personal-context", "figures")
	if err := os.MkdirAll(figuresDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(figuresDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(figuresDir, 0o755) })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte(`<html><img src="figures/new.png">x</html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	figDir := filepath.Join(dir, "figures")
	if err := os.MkdirAll(figDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(figDir, "new.png"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", id, dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when figures dir is unwritable")
	}
}

// --- Edit: data file copy error (unwritable directory) ---

func TestEditDataFileCopyError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	// Make the data directory unwritable
	dataDir := filepath.Join(homeDir, "personal-context", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dataDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dataDir, 0o755) })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>x</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	dDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dDir, "file.csv"), []byte("a,b"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", id, dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when data dir is unwritable")
	}
}

// --- Setup: ensureConfig write error ---

func TestSetupEnsureConfigWriteError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Create .pc dir so setup gets past MkdirAll, but make it unwritable
	// AFTER the DB is created (DB needs to write). So we need to run setup
	// partially. Instead, create the DB file manually, then make .pc read-only.
	pcDir := filepath.Join(homeDir, "personal-context", ".pc")
	if err := os.MkdirAll(pcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Run setup once to create DB and templates
	cmd1 := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd1.SetArgs([]string{"setup"})
	if err := cmd1.Execute(); err != nil {
		t.Fatal(err)
	}

	// Remove config.json and make .pc dir read-only to block config write
	configPath := filepath.Join(pcDir, "config.json")
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(pcDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(pcDir, 0o755) })

	// Re-run setup — should fail on ensureConfig write
	cmd2 := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd2.SetArgs([]string{"setup"})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("expected error when .pc dir is read-only")
	}
}

// --- Move: position resolvePositionFlags error in RunE ---

func TestMoveMultiplePositionFlags(t *testing.T) {
	setupEnv(t)
	id := addSlide(t)

	// --first and --after together should fail
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"move", id, "--first", "--after", "some-id"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for multiple position flags")
	}
}

// --- Add: CreateSlideFigure error (drop slide_figures, keep slides) ---

func TestAddCreateSlideFigureError(t *testing.T) {
	homeDir := setupEnv(t)

	corruptTable(t, homeDir, "slide_figures")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte(`<html><img src="figures/fig.png">x</html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	figDir := filepath.Join(dir, "figures")
	if err := os.MkdirAll(figDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(figDir, "fig.png"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slide_figures table missing")
	}

	db := openErrorPathsDB(t, homeDir)
	var slideCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM slides").Scan(&slideCount); err != nil {
		t.Fatalf("count slides: %v", err)
	}
	if slideCount != 0 {
		t.Fatalf("expected no slide rows after failed add, got %d", slideCount)
	}

	figuresRoot := filepath.Join(homeDir, "personal-context", "figures")
	hasFiles, err := hasRegularFiles(figuresRoot)
	if err != nil {
		t.Fatalf("read figures root: %v", err)
	}
	if hasFiles {
		t.Fatal("expected no copied figure files after failed add")
	}
}

// --- Add: CreateSlideDataFile error (drop slide_data_files, keep slides) ---

func TestAddCreateSlideDataFileError(t *testing.T) {
	homeDir := setupEnv(t)

	corruptTable(t, homeDir, "slide_data_files")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>x</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	dDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dDir, "file.csv"), []byte("a,b"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slide_data_files table missing")
	}

	db := openErrorPathsDB(t, homeDir)
	var slideCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM slides").Scan(&slideCount); err != nil {
		t.Fatalf("count slides: %v", err)
	}
	if slideCount != 0 {
		t.Fatalf("expected no slide rows after failed add, got %d", slideCount)
	}

	dataRoot := filepath.Join(homeDir, "personal-context", "data")
	hasFiles, err := hasRegularFiles(dataRoot)
	if err != nil {
		t.Fatalf("read data root: %v", err)
	}
	if hasFiles {
		t.Fatal("expected no copied data files after failed add")
	}
}

// --- Setup: ensureConfig write error (directory named config.json blocks write) ---

func TestSetupEnsureConfigDirectoryBlock(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Run setup once
	cmd1 := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd1.SetArgs([]string{"setup"})
	if err := cmd1.Execute(); err != nil {
		t.Fatal(err)
	}

	// Replace config.json with a directory to block ensureConfig write
	pcDir := filepath.Join(homeDir, "personal-context", ".pc")
	configPath := filepath.Join(pcDir, "config.json")
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Re-run setup — should fail on ensureConfig write
	cmd2 := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd2.SetArgs([]string{"setup"})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("expected error when config.json is a directory")
	}
}

// --- Edit: UpdateSlide error (rename slides to something that SELECT works on but UPDATE triggers fail) ---
// Note: renaming the table makes SELECT fail too, so we use ALTER TABLE to drop a required column.
// This is not cleanly testable without mocking; we cover via different approaches.

// --- Edit: CreateSlideFigure error after successful UpdateSlide ---
// We'd need slide_figures to not exist for INSERT but exist for SELECT (ListSlideFiguresBySlideID).
// Since both operations hit the same table, this requires mocking. Covered by edit figure copy error instead.

// --- Move: runMove resolveHomeDir and update error paths ---

func TestMoveUpdateSlideError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t, "--date", "2025-08-01")

	// Drop the sync_version table to make UpdateSlide's trigger fail
	// (the slides_sync_bump_after_update trigger references sync_version)
	corruptTable(t, homeDir, "sync_version")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"move", id, "--date", "2025-09-01"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when sync_version table missing (trigger fails)")
	}
}

// --- Edit: UpdateSlide error via trigger failure ---
// Slide has no old figures/data, so delete loops are skipped. UpdateSlide trigger fails.

func TestEditUpdateSlideError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlideWithContent(t,
		`<html><img src="figures/old.png"></html>`, "", "",
		map[string][]byte{"old.png": []byte("old-figure")},
		map[string][]byte{"old.csv": []byte("old,data")},
	)

	// Drop sync_version to make the slides_sync_bump_after_update trigger fail
	corruptTable(t, homeDir, "sync_version")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>new content</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", id, dir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when sync_version table missing")
	}

	db := openErrorPathsDB(t, homeDir)
	var figureCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM slide_figures WHERE slide_id = ?", id).Scan(&figureCount); err != nil {
		t.Fatalf("count slide_figures: %v", err)
	}
	if figureCount != 1 {
		t.Fatalf("expected figure rows to remain unchanged, got %d", figureCount)
	}

	var dataCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM slide_data_files WHERE slide_id = ?", id).Scan(&dataCount); err != nil {
		t.Fatalf("count slide_data_files: %v", err)
	}
	if dataCount != 1 {
		t.Fatalf("expected data file rows to remain unchanged, got %d", dataCount)
	}

	figurePath := filepath.Join(homeDir, "personal-context", "figures", id, "old.png")
	if _, err := os.Stat(figurePath); err != nil {
		t.Fatalf("expected old figure file to remain after failed edit: %v", err)
	}

	dataPath := filepath.Join(homeDir, "personal-context", "data", id, "old.csv")
	if _, err := os.Stat(dataPath); err != nil {
		t.Fatalf("expected old data file to remain after failed edit: %v", err)
	}
}

// --- Edit: DeleteSlideFigure error via trigger failure ---
// Slide has old figures. Drop sync_version. Edit with no new figures → figure delete trigger fails.

func TestEditDeleteSlideFigureError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlideWithContent(t,
		`<html><img src="figures/old.png">body</html>`, "", "",
		map[string][]byte{"old.png": []byte("data")}, nil,
	)

	// Drop sync_version to make the figures_sync_bump_after_delete trigger fail
	corruptTable(t, homeDir, "sync_version")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>no figures</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", id, dir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when figure delete trigger fails")
	}
}

// --- Edit: DeleteSlideDataFile error via trigger failure ---
// Slide has old data files. Drop sync_version. Edit with no new data → data delete trigger fails.

func TestEditDeleteSlideDataFileError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlideWithContent(t,
		"<html>body</html>", "", "",
		nil, map[string][]byte{"old.csv": []byte("x")},
	)

	// Drop sync_version to make the data_files_sync_bump_after_delete trigger fail
	corruptTable(t, homeDir, "sync_version")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>no data</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", id, dir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when data file delete trigger fails")
	}
}

// --- Edit: CreateSlideFigure error ---
// Slide has no old figures/data. Drop sync_version AND slide_figures.
// Edit with new figures → UpdateSlide needs sync trigger. But UpdateSlide trigger fires first.
// Actually, we need UpdateSlide to succeed but CreateSlideFigure to fail.
// Approach: add slide, drop slide_figures (not sync_version). Edit with new figures.
// ListSlideFiguresBySlideID fails before we get to create... so use different approach:
// Add slide without figures. Edit with new figures. Drop slide_figures table before edit.
// GetSlideByID: OK. ParseInputFolder: OK. CopyFigure: OK (filesystem).
// ListSlideFiguresBySlideID: FAILS (table dropped) → covers line 89, not 127.
// So to reach CreateSlideFigure, we need ListSlideFiguresBySlideID to succeed...
// This is only possible with mocking. Skip for now.

// --- Add: CreateSlide error (corrupt slides to make INSERT fail) ---
// computeDayOrder uses ListSlides which SELECT from slides. If slides exists but INSERT fails...
// Drop the sync_version table: INSERT into slides fires the sync_bump_after_insert trigger.

func TestAddCreateSlideTriggError(t *testing.T) {
	homeDir := setupEnv(t)

	// Drop sync_version to make the slides_sync_bump_after_insert trigger fail
	corruptTable(t, homeDir, "sync_version")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>x</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slide insert trigger fails")
	}
}

// --- Delete: non-ErrNotFound error (trigger failure) ---

func TestDeleteTriggerError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	// Drop sync_version to make the slides_sync_bump_after_update trigger fail
	// (SoftDeleteSlide does an UPDATE SET deleted_at)
	corruptTable(t, homeDir, "sync_version")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"delete", id})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when delete trigger fails")
	}
}

// --- Restore: non-ErrNotFound error (trigger failure) ---

func TestRestoreTriggerError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	// Delete first (so there's something to restore)
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatal(err)
	}

	// Now drop sync_version to make the restore's UPDATE trigger fail
	corruptTable(t, homeDir, "sync_version")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"restore", id})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when restore trigger fails")
	}
}
