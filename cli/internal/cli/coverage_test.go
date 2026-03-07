package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
