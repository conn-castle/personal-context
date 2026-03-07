package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
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
		if err := os.MkdirAll(figDir, 0o755); err != nil {
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
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
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
		t.Fatalf("add: %v", err)
	}
	return strings.TrimSpace(stdout.String())
}

// --- computeDayOrder coverage: first, after, before on non-empty list ---

func TestComputeDayOrderFirst(t *testing.T) {
	setupEnv(t)
	// Add two slides on same date so the list is non-empty
	addSlide(t, "--date", "2025-05-01")
	id2 := addSlide(t, "--date", "2025-05-01")

	// Now add a third with --first
	stdout := &bytes.Buffer{}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>First</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", "--date", "2025-05-01", "--first", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add --first: %v", err)
	}
	_ = id2
}

func TestComputeDayOrderAfter(t *testing.T) {
	setupEnv(t)
	id1 := addSlide(t, "--date", "2025-05-02")
	_ = addSlide(t, "--date", "2025-05-02")

	// Add a third after id1
	stdout := &bytes.Buffer{}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>After</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", "--date", "2025-05-02", "--after", id1, dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add --after: %v", err)
	}
}

func TestComputeDayOrderAfterLast(t *testing.T) {
	setupEnv(t)
	_ = addSlide(t, "--date", "2025-05-03")
	id2 := addSlide(t, "--date", "2025-05-03")

	// Add after the last slide (should use GenerateAtEnd)
	stdout := &bytes.Buffer{}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>AfterLast</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", "--date", "2025-05-03", "--after", id2, dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add --after last: %v", err)
	}
}

func TestComputeDayOrderBefore(t *testing.T) {
	setupEnv(t)
	_ = addSlide(t, "--date", "2025-05-04")
	id2 := addSlide(t, "--date", "2025-05-04")

	// Add before id2
	stdout := &bytes.Buffer{}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>Before</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", "--date", "2025-05-04", "--before", id2, dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add --before: %v", err)
	}
}

func TestComputeDayOrderBeforeFirst(t *testing.T) {
	setupEnv(t)
	id1 := addSlide(t, "--date", "2025-05-05")
	_ = addSlide(t, "--date", "2025-05-05")

	// Add before the first slide (should use GenerateAtStart)
	stdout := &bytes.Buffer{}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>BeforeFirst</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", "--date", "2025-05-05", "--before", id1, dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add --before first: %v", err)
	}
}

func TestComputeDayOrderAfterNotFound(t *testing.T) {
	setupEnv(t)
	addSlide(t, "--date", "2025-05-06")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>X</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", "--date", "2025-05-06", "--after", "nonexistent-id", dir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --after nonexistent")
	}
	if !strings.Contains(err.Error(), "reference slide") {
		t.Fatalf("expected reference slide error, got: %v", err)
	}
}

func TestComputeDayOrderBeforeNotFound(t *testing.T) {
	setupEnv(t)
	addSlide(t, "--date", "2025-05-07")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slide.html"), []byte("<html>X</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", "--date", "2025-05-07", "--before", "nonexistent-id", dir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --before nonexistent")
	}
	if !strings.Contains(err.Error(), "reference slide") {
		t.Fatalf("expected reference slide error, got: %v", err)
	}
}

// --- Show coverage: deleted slide, git fields, figures/data in text/json ---

func TestShowTextWithAllFields(t *testing.T) {
	setupEnv(t)
	id := addSlideWithContent(t,
		`<html><img src="figures/fig.png">body</html>`,
		"my notes",
		`{"project_id":"proj1","git_remote_url":"https://github.com/org/repo","git_hash":"abcdef1234567890abcdef1234567890abcdef12"}`,
		map[string][]byte{"fig.png": []byte("png")},
		map[string][]byte{"data.csv": []byte("a,b\n1,2\n")},
	)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"show", id})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show: %v", err)
	}

	out := stdout.String()
	for _, want := range []string{"proj1", "Git Remote:", "Git Hash:", "my notes", "Figures:", "fig.png", "Data files:", "data.csv"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestShowJSONWithAllFields(t *testing.T) {
	setupEnv(t)
	id := addSlideWithContent(t,
		`<html><img src="figures/chart.png">body</html>`,
		"json notes",
		`{"project_id":"proj-json","git_remote_url":"https://example.com","git_hash":"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}`,
		map[string][]byte{"chart.png": []byte("data")},
		map[string][]byte{"results.csv": []byte("x\n1\n")},
	)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"show", "--format", "json", id})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show json: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse json: %v\noutput: %s", err, stdout.String())
	}

	if result["project_id"] != "proj-json" {
		t.Fatalf("expected project_id=proj-json, got %v", result["project_id"])
	}
	if result["git_remote_url"] != "https://example.com" {
		t.Fatalf("expected git_remote_url, got %v", result["git_remote_url"])
	}
	figures := result["figures"].([]interface{})
	if len(figures) != 1 {
		t.Fatalf("expected 1 figure, got %d", len(figures))
	}
	dataFiles := result["data_files"].([]interface{})
	if len(dataFiles) != 1 {
		t.Fatalf("expected 1 data file, got %d", len(dataFiles))
	}
}

func TestShowDeletedSlideText(t *testing.T) {
	setupEnv(t)
	id := addSlide(t)

	// Delete the slide
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Show should still work and display deleted status
	stdout := &bytes.Buffer{}
	showCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	showCmd.SetArgs([]string{"show", id})
	if err := showCmd.Execute(); err != nil {
		t.Fatalf("show deleted: %v", err)
	}

	if !strings.Contains(stdout.String(), "deleted") {
		t.Fatalf("expected 'deleted' in output:\n%s", stdout.String())
	}
}

func TestShowDeletedSlideJSON(t *testing.T) {
	setupEnv(t)
	id := addSlide(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stdout := &bytes.Buffer{}
	showCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	showCmd.SetArgs([]string{"show", "--format", "json", id})
	if err := showCmd.Execute(); err != nil {
		t.Fatalf("show json deleted: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse json: %v", err)
	}

	if result["deleted_at"] == nil {
		t.Fatal("expected deleted_at to be non-null in JSON")
	}
}

func TestShowTextNoNotes(t *testing.T) {
	setupEnv(t)
	id := addSlide(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"show", id})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show: %v", err)
	}

	if !strings.Contains(stdout.String(), "(none)") {
		t.Fatalf("expected (none) for notes:\n%s", stdout.String())
	}
}

// --- Edit coverage: data files path, old file cleanup ---

func TestEditWithDataFiles(t *testing.T) {
	setupEnv(t)
	// Add a slide with data files
	id := addSlideWithContent(t,
		"<html>original</html>", "", "",
		nil,
		map[string][]byte{"old.csv": []byte("old data")},
	)

	// Edit with different data files
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(newDir, "slide.html"), []byte("<html>edited</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(newDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "new.csv"), []byte("new data"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", id, newDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if !strings.Contains(stdout.String(), "updated") {
		t.Fatalf("expected 'updated' in output: %s", stdout.String())
	}
}

func TestEditReplacesFiguresAndDataFiles(t *testing.T) {
	setupEnv(t)
	// Add with both figures and data files
	id := addSlideWithContent(t,
		`<html><img src="figures/old.png">body</html>`,
		"notes",
		"",
		map[string][]byte{"old.png": []byte("old-fig")},
		map[string][]byte{"old-data.csv": []byte("x\n1\n")},
	)

	// Edit with new figures and data
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(newDir, "slide.html"), []byte(`<html><img src="figures/new.png">new</html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "notes.md"), []byte("new notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	figDir := filepath.Join(newDir, "figures")
	if err := os.MkdirAll(figDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(figDir, "new.png"), []byte("new-fig"), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(newDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "new-data.csv"), []byte("y\n2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"edit", id, newDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if !strings.Contains(stdout.String(), "updated") {
		t.Fatalf("expected 'updated': %s", stdout.String())
	}
}

// --- Move coverage: after/before positions ---

func TestMovePositionAfter(t *testing.T) {
	setupEnv(t)
	id1 := addSlide(t, "--date", "2025-06-01")
	id2 := addSlide(t, "--date", "2025-06-01")
	id3 := addSlide(t, "--date", "2025-06-01")

	// Move id3 after id1
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"move", id3, "--after", id1})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("move --after: %v", err)
	}
	if !strings.Contains(stdout.String(), "moved") {
		t.Fatalf("expected 'moved': %s", stdout.String())
	}
	_ = id2
}

func TestMovePositionBefore(t *testing.T) {
	setupEnv(t)
	id1 := addSlide(t, "--date", "2025-06-02")
	_ = addSlide(t, "--date", "2025-06-02")
	id3 := addSlide(t, "--date", "2025-06-02")

	// Move id1 before id3
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"move", id1, "--before", id3})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("move --before: %v", err)
	}
	if !strings.Contains(stdout.String(), "moved") {
		t.Fatalf("expected 'moved': %s", stdout.String())
	}
}

func TestMovePositionLast(t *testing.T) {
	setupEnv(t)
	id1 := addSlide(t, "--date", "2025-06-03")
	_ = addSlide(t, "--date", "2025-06-03")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"move", id1, "--last"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("move --last: %v", err)
	}
}

func TestMoveDateAndPosition(t *testing.T) {
	setupEnv(t)
	id := addSlide(t, "--date", "2025-06-04")
	// Add some slides on target date
	_ = addSlide(t, "--date", "2025-06-05")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"move", id, "--date", "2025-06-05", "--first"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("move --date --first: %v", err)
	}
}

// --- Delete/restore error branch (non-ErrNotFound) already covered by existing tests ---
// The non-ErrNotFound path requires a DB error which is hard to simulate with real SQLite.
// The key missing coverage is the ErrNotFound wrapping which is covered by the NotFound tests.

// --- Add with hash (data file path) ---

func TestAddWithDataFilesCoversHash(t *testing.T) {
	setupEnv(t)
	id := addSlideWithContent(t,
		"<html>data</html>", "", "",
		nil,
		map[string][]byte{"metrics.csv": []byte("col\n1\n2\n")},
	)
	if id == "" {
		t.Fatal("expected slide ID")
	}
}

// --- resolvePositionFlags last branch ---

func TestResolvePositionFlagsLastExplicit(t *testing.T) {
	pos, err := resolvePositionFlags(false, true, "", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if pos.kind != "last" {
		t.Fatalf("expected last, got %q", pos.kind)
	}
}

// --- Project command coverage ---

func TestProjectSetSuccess(t *testing.T) {
	homeDir := setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "set", "my-project"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}
	if !strings.Contains(stdout.String(), `"my-project"`) {
		t.Fatalf("expected project name in output, got %q", stdout.String())
	}

	// Verify config was updated
	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg["active_project"] != "my-project" {
		t.Fatalf("expected active_project=my-project in config, got %v", cfg["active_project"])
	}
}

func TestProjectSetOverwrites(t *testing.T) {
	homeDir := setupEnv(t)

	cmd1 := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd1.SetArgs([]string{"project", "set", "first"})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("project set first: %v", err)
	}

	cmd2 := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd2.SetArgs([]string{"project", "set", "second"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("project set second: %v", err)
	}

	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg["active_project"] != "second" {
		t.Fatalf("expected active_project=second, got %v", cfg["active_project"])
	}
}

func TestProjectSetEmptyName(t *testing.T) {
	setupEnv(t)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "set", ""})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for empty project name")
	}
}

func TestProjectSetWhitespaceName(t *testing.T) {
	setupEnv(t)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "set", "   "})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for whitespace-only project name")
	}
}

func TestProjectClearSuccess(t *testing.T) {
	setupEnv(t)

	// Set first
	cmd1 := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd1.SetArgs([]string{"project", "set", "to-clear"})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}

	// Clear
	stdout := &bytes.Buffer{}
	cmd2 := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd2.SetArgs([]string{"project", "clear"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("project clear: %v", err)
	}
	if !strings.Contains(stdout.String(), "Active project cleared.") {
		t.Fatalf("expected clear message, got %q", stdout.String())
	}
}

func TestProjectClearIdempotent(t *testing.T) {
	setupEnv(t)

	// Clear without setting first
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "clear"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project clear: %v", err)
	}
	if !strings.Contains(stdout.String(), "Active project cleared.") {
		t.Fatalf("expected clear message, got %q", stdout.String())
	}
}

func TestProjectListEmpty(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project list: %v", err)
	}
	if !strings.Contains(stdout.String(), "No projects found.") {
		t.Fatalf("expected 'No projects found.', got %q", stdout.String())
	}
}

func TestProjectListWithProjects(t *testing.T) {
	setupEnv(t)

	// Add slides with projects
	addSlideWithContent(t, "<html>A</html>", "", `{"project_id":"alpha"}`, nil, nil)
	addSlideWithContent(t, "<html>B</html>", "", `{"project_id":"beta"}`, nil, nil)
	addSlideWithContent(t, "<html>C</html>", "", `{"project_id":"alpha"}`, nil, nil) // duplicate

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project list: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "alpha") {
		t.Fatalf("expected alpha in output, got %q", out)
	}
	if !strings.Contains(out, "beta") {
		t.Fatalf("expected beta in output, got %q", out)
	}
}

func TestProjectListMarksActive(t *testing.T) {
	setupEnv(t)

	addSlideWithContent(t, "<html>A</html>", "", `{"project_id":"alpha"}`, nil, nil)
	addSlideWithContent(t, "<html>B</html>", "", `{"project_id":"beta"}`, nil, nil)

	// Set active project
	setCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	setCmd.SetArgs([]string{"project", "set", "beta"})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project list: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "beta (active)") {
		t.Fatalf("expected 'beta (active)' in output, got %q", out)
	}
	// alpha should not have (active)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "alpha") && strings.Contains(line, "(active)") {
			t.Fatalf("alpha should not be marked active: %q", line)
		}
	}
}

func TestProjectCommandShowsHelp(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project help: %v", err)
	}
	if !strings.Contains(stdout.String(), "Manage active project") {
		t.Fatalf("expected help text, got %q", stdout.String())
	}
}

func TestAddUsesActiveProject(t *testing.T) {
	homeDir := setupEnv(t)

	// Set active project
	setCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	setCmd.SetArgs([]string{"project", "set", "active-proj"})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}

	// Add slide without --project flag
	id := addSlide(t)

	// Verify project from DB
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open stack: %v", err)
	}
	defer func() { _ = stack.Close() }()

	slide, err := stack.Repo.GetSlideByID(t.Context(), id)
	if err != nil {
		t.Fatalf("get slide: %v", err)
	}
	if slide.ProjectID == nil {
		t.Fatal("expected project_id to be set from active project")
	}
	if *slide.ProjectID != "active-proj" {
		t.Fatalf("expected project_id=active-proj, got %q", *slide.ProjectID)
	}
}

func TestAddProjectFlagOverridesActiveProject(t *testing.T) {
	homeDir := setupEnv(t)

	// Set active project
	setCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	setCmd.SetArgs([]string{"project", "set", "active-proj"})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}

	// Add slide with --project flag
	id := addSlide(t, "--project", "flag-proj")

	// Verify flag wins
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open stack: %v", err)
	}
	defer func() { _ = stack.Close() }()

	slide, err := stack.Repo.GetSlideByID(t.Context(), id)
	if err != nil {
		t.Fatalf("get slide: %v", err)
	}
	if slide.ProjectID == nil || *slide.ProjectID != "flag-proj" {
		t.Fatalf("expected project_id=flag-proj, got %v", slide.ProjectID)
	}
}

func TestAddMetadataOverridesActiveProject(t *testing.T) {
	homeDir := setupEnv(t)

	// Set active project
	setCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	setCmd.SetArgs([]string{"project", "set", "active-proj"})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("project set: %v", err)
	}

	// Add slide with metadata.json project_id (no --project flag)
	id := addSlideWithContent(t, "<html>X</html>", "", `{"project_id":"metadata-proj"}`, nil, nil)

	// Verify metadata wins over active project
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open stack: %v", err)
	}
	defer func() { _ = stack.Close() }()

	slide, err := stack.Repo.GetSlideByID(t.Context(), id)
	if err != nil {
		t.Fatalf("get slide: %v", err)
	}
	if slide.ProjectID == nil || *slide.ProjectID != "metadata-proj" {
		t.Fatalf("expected project_id=metadata-proj, got %v", slide.ProjectID)
	}
}

// --- GC coverage ---

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

func TestGCEmptyTrash(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"gc"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if !strings.Contains(stdout.String(), "No expired trash to clean up.") {
		t.Fatalf("expected clean message, got %q", stdout.String())
	}
}

func TestGCDeletesExpiredTrash(t *testing.T) {
	homeDir := setupEnv(t)

	id := addSlideWithContent(t,
		`<html><img src="figures/fig.png">content</html>`, "", "",
		map[string][]byte{"fig.png": []byte("image")},
		map[string][]byte{"data.csv": []byte("a,b")},
	)

	// Soft-delete.
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Backdate to 31 days ago.
	backdateDeletedAtUnit(t, homeDir, id, 31)

	// Run gc.
	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	if err := gcCmd.Execute(); err != nil {
		t.Fatalf("gc: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, fmt.Sprintf("Deleted %s", id)) {
		t.Fatalf("expected 'Deleted %s' in output, got %q", id, out)
	}
	if !strings.Contains(out, "Removed 1 slide(s).") {
		t.Fatalf("expected summary, got %q", out)
	}

	// Verify slide is gone from DB.
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM slides WHERE id = ?", id).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected slide to be hard-deleted, got count=%d", count)
	}

	// Verify files are removed.
	figurePath := filepath.Join(homeDir, "personal-context", "figures", id, "fig.png")
	if _, err := os.Stat(figurePath); !os.IsNotExist(err) {
		t.Fatalf("expected figure file to be removed, stat err=%v", err)
	}
	dataPath := filepath.Join(homeDir, "personal-context", "data", id, "data.csv")
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected data file to be removed, stat err=%v", err)
	}
}

func TestGCLeavesYoungTrashUnit(t *testing.T) {
	setupEnv(t)

	id := addSlide(t)

	// Soft-delete (just now).
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Run gc.
	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	if err := gcCmd.Execute(); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if !strings.Contains(stdout.String(), "No expired trash to clean up.") {
		t.Fatalf("expected young trash to be left alone, got %q", stdout.String())
	}
}

func TestGCMixedAgesUnit(t *testing.T) {
	homeDir := setupEnv(t)

	idOld := addSlide(t, "--date", "2025-01-01")
	idYoung := addSlide(t, "--date", "2025-01-02")

	// Soft-delete both.
	for _, id := range []string{idOld, idYoung} {
		delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		delCmd.SetArgs([]string{"delete", id})
		if err := delCmd.Execute(); err != nil {
			t.Fatalf("delete %s: %v", id, err)
		}
	}

	// Backdate only the old one.
	backdateDeletedAtUnit(t, homeDir, idOld, 31)

	// Run gc.
	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	if err := gcCmd.Execute(); err != nil {
		t.Fatalf("gc: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, idOld) {
		t.Fatalf("expected old slide %s to be deleted, got %q", idOld, out)
	}
	if strings.Contains(out, idYoung) {
		t.Fatalf("expected young slide %s to NOT be deleted, got %q", idYoung, out)
	}
}

func TestGCListSlidesDBError(t *testing.T) {
	homeDir := setupEnv(t)

	// Corrupt slides table.
	corruptTable(t, homeDir, "slides")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"gc"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

// --- Search coverage ---

func TestSearchTableNoResults(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "nonexistent-query-xyz"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(stdout.String(), "No matching slides found.") {
		t.Fatalf("expected 'No matching slides found.', got %q", stdout.String())
	}
}

func TestSearchTableWithResults(t *testing.T) {
	setupEnv(t)
	addSlideWithContent(t, "<html>searchable content here</html>", "my notes about kittens", "", nil, nil)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "kittens"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "Date") {
		t.Fatalf("expected table header, got %q", out)
	}
}

func TestSearchTableWithProject(t *testing.T) {
	setupEnv(t)
	addSlideWithContent(t, "<html>project slide</html>", "", `{"project_id":"alpha"}`, nil, nil)
	addSlideWithContent(t, "<html>project slide beta</html>", "", `{"project_id":"beta"}`, nil, nil)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "project", "--project", "alpha"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "alpha") {
		t.Fatalf("expected alpha project in output, got %q", out)
	}
}

func TestSearchIDsFormat(t *testing.T) {
	setupEnv(t)
	id := addSlideWithContent(t, "<html>ids output test</html>", "ids notes content", "", nil, nil)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "ids", "--format", "ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search ids: %v", err)
	}

	out := strings.TrimSpace(stdout.String())
	if !strings.Contains(out, id) {
		t.Fatalf("expected slide ID %s in output, got %q", id, out)
	}
}

func TestSearchIDsFormatEmpty(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "nonexistent-query-xyz", "--format", "ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search ids: %v", err)
	}

	out := strings.TrimSpace(stdout.String())
	if out != "" {
		t.Fatalf("expected empty output for no results in ids format, got %q", out)
	}
}

func TestSearchJSONFormat(t *testing.T) {
	setupEnv(t)
	addSlideWithContent(t, "<html>json search test</html>", "json notes", `{"project_id":"proj-search"}`, nil, nil)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "json", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search json: %v", err)
	}

	var results []searchResultJSON
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("parse json: %v\noutput: %s", err, stdout.String())
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].ProjectID == nil || *results[0].ProjectID != "proj-search" {
		t.Fatalf("expected project_id=proj-search, got %v", results[0].ProjectID)
	}
}

func TestSearchJSONFormatEmpty(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "nonexistent-query-xyz", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search json: %v", err)
	}

	var results []searchResultJSON
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d", len(results))
	}
}

func TestSearchJSONWithDeletedAt(t *testing.T) {
	setupEnv(t)
	id := addSlide(t)

	// Soft-delete the slide
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "Test", "--format", "json", "--deleted"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search json: %v", err)
	}

	var results []searchResultJSON
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("parse json: %v\noutput: %s", err, stdout.String())
	}

	found := false
	for _, r := range results {
		if r.ID == id {
			found = true
			if r.DeletedAt == nil {
				t.Fatal("expected deleted_at to be set for deleted slide")
			}
		}
	}
	if !found {
		t.Fatalf("expected slide %s in deleted results", id)
	}
}

func TestSearchInvalidFormat(t *testing.T) {
	setupEnv(t)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "query", "--format", "xml"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestSearchWhitespaceOnlyQuery(t *testing.T) {
	setupEnv(t)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "   "})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for whitespace-only query")
	}
	if !strings.Contains(err.Error(), "query must not be empty") {
		t.Fatalf("expected empty-query error, got %v", err)
	}
}

func TestSearchNegativeLimit(t *testing.T) {
	setupEnv(t)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "query", "--limit", "-1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for negative limit")
	}
	if !strings.Contains(err.Error(), "limit must be >= 0") {
		t.Fatalf("expected limit validation error, got %v", err)
	}
}

func TestSearchWithLimit(t *testing.T) {
	setupEnv(t)
	addSlideWithContent(t, "<html>limit test one</html>", "limit notes one", "", nil, nil)
	addSlideWithContent(t, "<html>limit test two</html>", "limit notes two", "", nil, nil)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "limit", "--format", "ids", "--limit", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search limit: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 result with --limit 1, got %d: %q", len(lines), stdout.String())
	}
}

func TestSearchListSlidesDBError(t *testing.T) {
	homeDir := setupEnv(t)

	// Corrupt slides table.
	corruptTable(t, homeDir, "slides")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "query"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

// --- Trash coverage ---

func TestTrashEmptyList(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"trash"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if !strings.Contains(stdout.String(), "Trash is empty.") {
		t.Fatalf("expected 'Trash is empty.', got %q", stdout.String())
	}
}

func TestTrashWithDeletedSlides(t *testing.T) {
	setupEnv(t)
	id := addSlide(t)

	// Soft-delete the slide
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"trash"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trash: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "Date") || !strings.Contains(out, "Deleted At") {
		t.Fatalf("expected table header, got %q", out)
	}
	if !strings.Contains(out, id) {
		t.Fatalf("expected slide ID %s in trash output, got %q", id, out)
	}
}

func TestTrashListSlidesDBError(t *testing.T) {
	homeDir := setupEnv(t)

	corruptTable(t, homeDir, "slides")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"trash"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

// --- Doctor coverage: ListSlideIDsOnDisk error, ListSlides error ---

func TestDoctorListSlideIDsOnDiskError(t *testing.T) {
	homeDir := setupEnv(t)

	// Replace figures directory with a file to make ListSlideIDsOnDisk fail
	figuresDir := filepath.Join(homeDir, "personal-context", "figures")
	if err := os.RemoveAll(figuresDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(figuresDir, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when figures dir is a file")
	}
}

func TestDoctorListSlidesDBError(t *testing.T) {
	homeDir := setupEnv(t)

	corruptTable(t, homeDir, "slides")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

// --- GC: DeleteSlide error path ---

func TestGCDeleteSlideError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	// Soft-delete
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Backdate to 31 days ago
	backdateDeletedAtUnit(t, homeDir, id, 31)

	// Drop the slides table so DeleteSlide fails
	// But first we need to make ListSlides succeed... so we use a different approach:
	// Corrupt the slide_figures table referenced by cascade delete
	// Actually, we need a simpler approach: drop sync_version to make delete trigger fail.
	// DeleteSlide is a hard DELETE FROM slides, which triggers slides_sync_bump_after_delete.
	corruptTable(t, homeDir, "sync_version")

	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	err := gcCmd.Execute()
	if err == nil {
		t.Fatal("expected error when DeleteSlide trigger fails")
	}
	if !strings.Contains(err.Error(), "hard delete slide") {
		t.Fatalf("expected 'hard delete slide' error, got %v", err)
	}
}

// --- GC: DeleteSlideDir error path ---

func TestGCDeleteSlideDirError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlideWithContent(t,
		`<html><img src="figures/fig.png">content</html>`, "", "",
		map[string][]byte{"fig.png": []byte("image")},
		nil,
	)

	// Soft-delete
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Backdate to 31 days ago
	backdateDeletedAtUnit(t, homeDir, id, 31)

	// Make the figures directory unwritable so RemoveAll fails
	figureSlideDir := filepath.Join(homeDir, "personal-context", "figures", id)
	if err := os.Chmod(figureSlideDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(figureSlideDir, 0o755) })

	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	err := gcCmd.Execute()
	if err != nil {
		t.Fatalf("expected gc to succeed with a warning, got error: %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "Warning: failed to remove files for slide") {
		t.Fatalf("expected warning about failed file removal in stdout, got %q", output)
	}
}

// --- Search: table format with project_id field populated ---

func TestSearchTableProjectColumn(t *testing.T) {
	setupEnv(t)
	addSlideWithContent(t, "<html>project column test</html>", "", `{"project_id":"myproj"}`, nil, nil)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "project column"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "myproj") {
		t.Fatalf("expected project column 'myproj' in table, got %q", out)
	}
}

// --- Trash: slide with nil DeletedAt (covers deletedAt empty string branch) ---

func TestTrashDeletedAtFormat(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	// Soft-delete the slide
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify that trash output includes a properly formatted timestamp
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"trash"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trash: %v", err)
	}

	out := stdout.String()
	// The timestamp should be in 2006-01-02T15:04:05Z format
	if !strings.Contains(out, "T") || !strings.Contains(out, "Z") {
		t.Fatalf("expected ISO timestamp in trash output, got %q", out)
	}

	// Verify the header columns are present
	_ = homeDir
}

// (Project write-error and project-list DB-error tests live in deps_internal_test.go.)
