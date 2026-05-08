package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSearchTableNoResults(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "nonexistent-query-xyz"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(stdout.String(), "No matching records found.") {
		t.Fatalf("expected 'No matching records found.', got %q", stdout.String())
	}
}

func TestSearchTableWithResults(t *testing.T) {
	setupEnv(t)
	addRecordWithContent(t, "<html>searchable content here</html>", "my notes about kittens", "", nil, nil)

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
	addRecordWithContent(t, "<html>project record</html>", "", `{"project_id":"alpha"}`, nil, nil)
	addRecordWithContent(t, "<html>project record beta</html>", "", `{"project_id":"beta"}`, nil, nil)

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
	if strings.Contains(out, "beta") {
		t.Fatalf("expected beta project to be excluded by --project filter, got %q", out)
	}
}

func TestSearchIDsFormat(t *testing.T) {
	setupEnv(t)
	id := addRecordWithContent(t, "<html>ids output test</html>", "ids notes content", "", nil, nil)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "ids", "--format", "ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search ids: %v", err)
	}

	out := strings.TrimSpace(stdout.String())
	if !strings.Contains(out, id) {
		t.Fatalf("expected record ID %s in output, got %q", id, out)
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
	addRecordWithContent(t, "<html>json search test</html>", "json notes", `{"project_id":"proj-search"}`, nil, nil)

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
	if results[0].ProjectID != "proj-search" {
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
	id := addRecord(t)

	// Soft-delete the record
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
				t.Fatal("expected deleted_at to be set for deleted record")
			}
		}
	}
	if !found {
		t.Fatalf("expected record %s in deleted results", id)
	}
}

func TestSearchExcludesDeletedByDefault(t *testing.T) {
	setupEnv(t)
	id := addRecordWithContent(t, "<html>deletable record for exclusion test</html>", "", "", nil, nil)

	// Soft-delete the record.
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Search without --deleted should exclude the soft-deleted record.
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "deletable", "--format", "ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search: %v", err)
	}

	if strings.Contains(stdout.String(), id) {
		t.Fatalf("expected deleted record %s to be excluded from default search, got %q", id, stdout.String())
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
	addRecordWithContent(t, "<html>limit test one</html>", "limit notes one", "", nil, nil)
	addRecordWithContent(t, "<html>limit test two</html>", "limit notes two", "", nil, nil)

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

func TestSearchListRecordsDBError(t *testing.T) {
	homeDir := setupEnv(t)

	// Corrupt records table.
	corruptTable(t, homeDir, "records")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "query"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when records table missing")
	}
}

func TestSearchTableProjectColumn(t *testing.T) {
	setupEnv(t)
	addRecordWithContent(t, "<html>project column test</html>", "", `{"project_id":"myproj"}`, nil, nil)

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
