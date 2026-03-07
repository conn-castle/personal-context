package e2e_test

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

var binaryPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "pc-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	binaryPath = filepath.Join(tmpDir, "pc")
	cmd := exec.Command("go", "build", "-o", binaryPath, "github.com/conn-castle/personal-context/cli/cmd/pc")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build pc binary: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	exitCode := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(exitCode)
}

// runResult captures the output of a pc command execution.
type runResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// runPC executes the pc binary with the given args and PC_HOME set to homeDir.
func runPC(t *testing.T, homeDir string, args ...string) runResult {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "PC_HOME="+homeDir)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run pc: %v", err)
		}
	}

	return runResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// runPCSuccess runs pc and fails the test if exit code is non-zero.
func runPCSuccess(t *testing.T, homeDir string, args ...string) string {
	t.Helper()
	result := runPC(t, homeDir, args...)
	if result.ExitCode != 0 {
		t.Fatalf("pc %v failed (exit %d):\nstdout: %s\nstderr: %s",
			args, result.ExitCode, result.Stdout, result.Stderr)
	}
	return result.Stdout
}

// runPCFailure runs pc and fails the test if exit code is zero.
func runPCFailure(t *testing.T, homeDir string, args ...string) string {
	t.Helper()
	result := runPC(t, homeDir, args...)
	if result.ExitCode == 0 {
		t.Fatalf("pc %v unexpectedly succeeded:\nstdout: %s", args, result.Stdout)
	}
	return result.Stderr
}

// openTestDB opens the SQLite database for the given home directory.
func openTestDB(t *testing.T, homeDir string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// queryRowCount returns the count of rows in a table.
func queryRowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

// createInputFolder creates a temp folder suitable for `pc add` input.
func createInputFolder(t *testing.T, opts inputFolderOpts) string {
	t.Helper()
	dir := t.TempDir()

	if opts.HTMLContent == "" {
		opts.HTMLContent = "<html><body><h1>Test Slide</h1></body></html>"
	}

	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte(opts.HTMLContent), 0o644); err != nil {
		t.Fatalf("write slide.html: %v", err)
	}

	if opts.Notes != "" {
		if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(opts.Notes), 0o644); err != nil {
			t.Fatalf("write notes.md: %v", err)
		}
	}

	if opts.MetadataJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(opts.MetadataJSON), 0o644); err != nil {
			t.Fatalf("write metadata.json: %v", err)
		}
	}

	if len(opts.Figures) > 0 {
		figDir := filepath.Join(dir, "figures")
		if err := os.MkdirAll(figDir, 0o755); err != nil {
			t.Fatalf("create figures dir: %v", err)
		}
		for name, content := range opts.Figures {
			if err := os.WriteFile(filepath.Join(figDir, name), content, 0o644); err != nil {
				t.Fatalf("write figure %s: %v", name, err)
			}
		}
	}

	if len(opts.DataFiles) > 0 {
		dataDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			t.Fatalf("create data dir: %v", err)
		}
		for name, content := range opts.DataFiles {
			if err := os.WriteFile(filepath.Join(dataDir, name), content, 0o644); err != nil {
				t.Fatalf("write data file %s: %v", name, err)
			}
		}
	}

	return dir
}

// inputFolderOpts configures a test input folder.
type inputFolderOpts struct {
	HTMLContent  string
	Notes        string
	MetadataJSON string
	Figures      map[string][]byte
	DataFiles    map[string][]byte
}
