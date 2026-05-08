package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// --- Show tests (from coverage_test.go) ---

func TestShowTextWithAllFields(t *testing.T) {
	setupEnv(t)
	id := addRecordWithContent(t,
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
	id := addRecordWithContent(t,
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
	figures, ok := result["figures"].([]interface{})
	if !ok {
		t.Fatalf("expected figures to be a JSON array, got %T", result["figures"])
	}
	if len(figures) != 1 {
		t.Fatalf("expected 1 figure, got %d", len(figures))
	}
	dataFiles, ok := result["data_files"].([]interface{})
	if !ok {
		t.Fatalf("expected data_files to be a JSON array, got %T", result["data_files"])
	}
	if len(dataFiles) != 1 {
		t.Fatalf("expected 1 data file, got %d", len(dataFiles))
	}
}

func TestShowDeletedRecordText(t *testing.T) {
	setupEnv(t)
	id := addRecord(t)

	// Delete the record
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

func TestShowDeletedRecordJSON(t *testing.T) {
	setupEnv(t)
	id := addRecord(t)

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
	id := addRecord(t)

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

// --- Show command tests (from add_test.go) ---

func TestShowCommandTextFormat(t *testing.T) {
	setupEnv(t)
	id := addRecord(t)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"show", id})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("show failed: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, id) {
		t.Fatalf("expected output to contain record ID %q, got %q", id, out)
	}
	if !strings.Contains(out, "Date:") {
		t.Fatalf("expected output to contain 'Date:', got %q", out)
	}
}

func TestShowCommandJSONFormat(t *testing.T) {
	setupEnv(t)
	id := addRecord(t)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"show", "--format", "json", id})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("show --format json failed: %v", err)
	}

	var parsed recordJSON
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got parse error: %v\nraw output: %s", err, stdout.String())
	}
	if parsed.ID != id {
		t.Fatalf("expected JSON id=%q, got %q", id, parsed.ID)
	}
}

func TestShowCommandNotFound(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"show", "nonexistent-id-12345"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent record")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %q", err.Error())
	}
}

func TestShowCommandInvalidFormat(t *testing.T) {
	setupEnv(t)
	id := addRecord(t)

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
	setupEnv(t)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"show"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

// --- Truncate tests (from add_test.go) ---

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
