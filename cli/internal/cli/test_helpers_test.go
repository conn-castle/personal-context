package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// setupEnv is a shared helper for coverage tests — runs pc setup and returns homeDir.
func setupEnv(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return homeDir
}

// addSlide is a helper that adds a minimal slide and returns the slide ID.
func addSlide(t *testing.T, extraArgs ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>Test</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	args := append([]string{"add"}, extraArgs...)
	args = append(args, dir)
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	return strings.TrimSpace(stdout.String())
}

// addSlideWithContent adds a slide with specific HTML, notes, and optional metadata/figures.
func addSlideWithContent(t *testing.T, html, notes, metadata string, figures map[string][]byte, dataFiles map[string][]byte, extraArgs ...string) string {
	t.Helper()
	dir := t.TempDir()
	if html == "" {
		html = "<html>Test</html>"
	}
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	if notes != "" {
		if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(notes), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if metadata != "" {
		if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if len(figures) > 0 {
		figDir := filepath.Join(dir, "figures")
		if err := os.MkdirAll(figDir, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, data := range figures {
			if err := os.WriteFile(filepath.Join(figDir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(dataFiles) > 0 {
		dataDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, data := range dataFiles {
			if err := os.WriteFile(filepath.Join(dataDir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	stdout := &bytes.Buffer{}
	args := append([]string{"add"}, extraArgs...)
	args = append(args, dir)
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add with content: %v", err)
	}
	return strings.TrimSpace(stdout.String())
}

// getDayOrder returns the day_order field for a slide by running show --format json.
func getDayOrder(t *testing.T, slideID string) string {
	t.Helper()
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"show", "--format", "json", slideID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show %s: %v", slideID, err)
	}
	var s slideJSON
	if err := json.Unmarshal(stdout.Bytes(), &s); err != nil {
		t.Fatalf("parse show json for %s: %v", slideID, err)
	}
	return s.DayOrder
}

// backdateDeletedAtUnit updates deleted_at in the DB for a slide to simulate aging.
func backdateDeletedAtUnit(t *testing.T, homeDir string, slideID string, daysAgo int) {
	t.Helper()
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	past := time.Now().UTC().Add(-time.Duration(daysAgo) * 24 * time.Hour)
	ts := past.Format("2006-01-02T15:04:05.000Z")
	_, err = db.Exec(`UPDATE slides SET deleted_at = ? WHERE id = ?`, ts, slideID)
	if err != nil {
		t.Fatalf("backdate deleted_at for %s: %v", slideID, err)
	}
}
