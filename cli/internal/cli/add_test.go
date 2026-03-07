package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestEnv creates a clean PC_HOME, runs `pc setup`, and returns the home dir.
func setupTestEnv(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return homeDir
}

// inputFolderOpts configures a test input folder for pc add.
type inputFolderOpts struct {
	HTMLContent  string
	Notes        string
	MetadataJSON string
	Figures      map[string][]byte
	DataFiles    map[string][]byte
}

// makeInputFolder creates a temp folder suitable for `pc add` input.
func makeInputFolder(t *testing.T, opts inputFolderOpts) string {
	t.Helper()
	dir := t.TempDir()

	if opts.HTMLContent == "" {
		opts.HTMLContent = "<html><body>Test</body></html>"
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

// ---------------------------------------------------------------------------
// Add command tests
// ---------------------------------------------------------------------------

func TestAddCommandSuccess(t *testing.T) {
	setupTestEnv(t)
	inputDir := makeInputFolder(t, inputFolderOpts{})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"add", inputDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	id := strings.TrimSpace(stdout.String())
	if id == "" {
		t.Fatal("expected slide ID on stdout, got empty string")
	}
}

func TestAddCommandWithDateFlag(t *testing.T) {
	setupTestEnv(t)
	inputDir := makeInputFolder(t, inputFolderOpts{})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"add", "--date", "2025-06-15", inputDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("add --date failed: %v", err)
	}

	id := strings.TrimSpace(stdout.String())
	if id == "" {
		t.Fatal("expected slide ID on stdout")
	}
}

func TestAddCommandWithProjectFlag(t *testing.T) {
	setupTestEnv(t)
	inputDir := makeInputFolder(t, inputFolderOpts{})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"add", "--project", "my-proj", inputDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("add --project failed: %v", err)
	}

	id := strings.TrimSpace(stdout.String())
	if id == "" {
		t.Fatal("expected slide ID on stdout")
	}
}

func TestAddCommandWithFigures(t *testing.T) {
	setupTestEnv(t)
	inputDir := makeInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><body><img src="figures/chart.png"></body></html>`,
		Figures: map[string][]byte{
			"chart.png": {0x89, 0x50, 0x4E, 0x47}, // PNG magic bytes
		},
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"add", inputDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("add with figures failed: %v", err)
	}

	id := strings.TrimSpace(stdout.String())
	if id == "" {
		t.Fatal("expected slide ID on stdout")
	}
}

func TestAddCommandWithDataFiles(t *testing.T) {
	setupTestEnv(t)
	inputDir := makeInputFolder(t, inputFolderOpts{
		DataFiles: map[string][]byte{
			"results.csv": []byte("col1,col2\n1,2\n"),
		},
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"add", inputDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("add with data files failed: %v", err)
	}

	id := strings.TrimSpace(stdout.String())
	if id == "" {
		t.Fatal("expected slide ID on stdout")
	}
}

func TestAddCommandInvalidDate(t *testing.T) {
	setupTestEnv(t)
	inputDir := makeInputFolder(t, inputFolderOpts{})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"add", "--date", "bad", inputDir})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid date")
	}
	if !strings.Contains(err.Error(), "invalid date") {
		t.Fatalf("expected error to contain 'invalid date', got %q", err.Error())
	}
}

func TestAddCommandMissingSlideHTML(t *testing.T) {
	setupTestEnv(t)
	// Create an empty directory with no slide.html.
	emptyDir := t.TempDir()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"add", emptyDir})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing slide.html")
	}
	if !strings.Contains(err.Error(), "slide.html") {
		t.Fatalf("expected error to contain 'slide.html', got %q", err.Error())
	}
}

func TestAddCommandMutuallyExclusiveFlags(t *testing.T) {
	setupTestEnv(t)
	inputDir := makeInputFolder(t, inputFolderOpts{})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"add", "--first", "--last", inputDir})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags --first and --last")
	}
	if !strings.Contains(err.Error(), "only one position flag") {
		t.Fatalf("expected mutual exclusion error, got %q", err.Error())
	}
}

func TestAddCommandPositionFirst(t *testing.T) {
	setupTestEnv(t)

	// Add the first slide (default position = last).
	inputDir1 := makeInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>Slide 1</body></html>",
	})
	stdout1 := &bytes.Buffer{}
	cmd1 := NewRootCommand(RootCommandOptions{Stdout: stdout1, Stderr: &bytes.Buffer{}})
	cmd1.SetArgs([]string{"add", "--date", "2025-07-01", inputDir1})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("add slide 1 failed: %v", err)
	}
	id1 := strings.TrimSpace(stdout1.String())
	if id1 == "" {
		t.Fatal("expected slide 1 ID")
	}

	// Add the second slide with --first on the same date.
	inputDir2 := makeInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>Slide 2</body></html>",
	})
	stdout2 := &bytes.Buffer{}
	cmd2 := NewRootCommand(RootCommandOptions{Stdout: stdout2, Stderr: &bytes.Buffer{}})
	cmd2.SetArgs([]string{"add", "--first", "--date", "2025-07-01", inputDir2})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("add slide 2 --first failed: %v", err)
	}
	id2 := strings.TrimSpace(stdout2.String())
	if id2 == "" {
		t.Fatal("expected slide 2 ID")
	}
}

func TestAddCommandNoArgs(t *testing.T) {
	setupTestEnv(t)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"add"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

// ---------------------------------------------------------------------------
// Show command tests
// ---------------------------------------------------------------------------

// addTestSlide is a helper that adds a minimal slide and returns its ID.
func addTestSlide(t *testing.T) string {
	t.Helper()
	inputDir := makeInputFolder(t, inputFolderOpts{})
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", inputDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add test slide: %v", err)
	}
	id := strings.TrimSpace(stdout.String())
	if id == "" {
		t.Fatal("add returned empty ID")
	}
	return id
}

func TestShowCommandTextFormat(t *testing.T) {
	setupTestEnv(t)
	id := addTestSlide(t)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"show", id})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("show failed: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, id) {
		t.Fatalf("expected output to contain slide ID %q, got %q", id, out)
	}
	if !strings.Contains(out, "Date:") {
		t.Fatalf("expected output to contain 'Date:', got %q", out)
	}
}

func TestShowCommandJSONFormat(t *testing.T) {
	setupTestEnv(t)
	id := addTestSlide(t)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"show", "--format", "json", id})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("show --format json failed: %v", err)
	}

	var parsed slideJSON
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got parse error: %v\nraw output: %s", err, stdout.String())
	}
	if parsed.ID != id {
		t.Fatalf("expected JSON id=%q, got %q", id, parsed.ID)
	}
}

func TestShowCommandNotFound(t *testing.T) {
	setupTestEnv(t)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"show", "nonexistent-id-12345"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent slide")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %q", err.Error())
	}
}

func TestShowCommandInvalidFormat(t *testing.T) {
	setupTestEnv(t)
	id := addTestSlide(t)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"show", "--format", "xml", id})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Fatalf("expected 'unknown format' error, got %q", err.Error())
	}
}

func TestShowCommandNoArgs(t *testing.T) {
	setupTestEnv(t)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"show"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

// ---------------------------------------------------------------------------
// Direct function tests for coverage
// ---------------------------------------------------------------------------

func TestResolvePositionFlagsDefault(t *testing.T) {
	pos, err := resolvePositionFlags(false, false, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos.kind != "last" {
		t.Fatalf("expected kind 'last', got %q", pos.kind)
	}
	if pos.referenceID != "" {
		t.Fatalf("expected empty referenceID, got %q", pos.referenceID)
	}
}

func TestResolvePositionFlagsFirst(t *testing.T) {
	pos, err := resolvePositionFlags(true, false, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos.kind != "first" {
		t.Fatalf("expected kind 'first', got %q", pos.kind)
	}
}

func TestResolvePositionFlagsAfter(t *testing.T) {
	pos, err := resolvePositionFlags(false, false, "slide-abc", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos.kind != "after" {
		t.Fatalf("expected kind 'after', got %q", pos.kind)
	}
	if pos.referenceID != "slide-abc" {
		t.Fatalf("expected referenceID 'slide-abc', got %q", pos.referenceID)
	}
}

func TestResolvePositionFlagsBefore(t *testing.T) {
	pos, err := resolvePositionFlags(false, false, "", "slide-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos.kind != "before" {
		t.Fatalf("expected kind 'before', got %q", pos.kind)
	}
	if pos.referenceID != "slide-xyz" {
		t.Fatalf("expected referenceID 'slide-xyz', got %q", pos.referenceID)
	}
}

func TestResolvePositionFlagsMutualExclusion(t *testing.T) {
	tests := []struct {
		name   string
		first  bool
		last   bool
		after  string
		before string
	}{
		{"first+last", true, true, "", ""},
		{"first+after", true, false, "id1", ""},
		{"first+before", true, false, "", "id2"},
		{"last+after", false, true, "id1", ""},
		{"after+before", false, false, "id1", "id2"},
		{"first+last+after", true, true, "id1", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolvePositionFlags(tc.first, tc.last, tc.after, tc.before)
			if err == nil {
				t.Fatal("expected mutual exclusion error")
			}
			if !strings.Contains(err.Error(), "only one position flag") {
				t.Fatalf("expected 'only one position flag' error, got %q", err.Error())
			}
		})
	}
}

func TestTruncateShortString(t *testing.T) {
	result := truncate("hello", 10)
	if result != "hello" {
		t.Fatalf("expected 'hello', got %q", result)
	}
}

func TestTruncateLongString(t *testing.T) {
	result := truncate("a very long string", 10)
	expected := "a very ..."
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestTruncateMultibyteRunes(t *testing.T) {
	// 4 CJK characters = 4 runes but 12 bytes; truncate at 3 runes should not split
	result := truncate("日本語文", 3)
	if result != "日本語" {
		t.Fatalf("expected %q, got %q", "日本語", result)
	}
}

func TestTruncateExactBoundary(t *testing.T) {
	result := truncate("hello", 5)
	if result != "hello" {
		t.Fatalf("expected %q, got %q", "hello", result)
	}
}

func TestTruncateMaxLenZero(t *testing.T) {
	result := truncate("hello", 0)
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestTruncateMaxLenThree(t *testing.T) {
	// maxLen=3 with string longer than 3: returns first 3 runes (no "...")
	result := truncate("abcdef", 3)
	if result != "abc" {
		t.Fatalf("expected %q, got %q", "abc", result)
	}
}

func TestTruncateEmptyString(t *testing.T) {
	result := truncate("", 10)
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}
