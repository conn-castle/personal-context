package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
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

func TestSearchWritersCoverRecordAndChatDomains(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	projectID := "project/search"
	record := repository.Record{
		ID:             "20260514-aabbccdd",
		Date:           "2026-05-14",
		DayOrder:       "a0",
		ProjectID:      projectID,
		SourceDeviceID: "device/search",
		CreatedAt:      now,
		UpdatedAt:      now,
		HTMLContent:    strPtr("html"),
	}
	chat := repository.ChatSearchResult{
		Session: repository.ChatSession{
			ID:             "20260514-beadfeed",
			ProjectID:      &projectID,
			SourceDeviceID: "device/search",
			LastActivityAt: now,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		Item:    repository.ChatItem{Ordinal: 2},
		Snippet: strings.Repeat("snippet ", 20),
	}
	results := []repository.DomainSearchResult{
		{Domain: "records", Record: &record},
		{Domain: "chats", Chat: &chat},
	}

	tableOut := &bytes.Buffer{}
	if err := searchTable(tableOut, results); err != nil {
		t.Fatalf("searchTable() error = %v", err)
	}
	if out := tableOut.String(); !strings.Contains(out, "records") || !strings.Contains(out, "chats") {
		t.Fatalf("expected both domains in table output, got %q", out)
	}

	idsOut := &bytes.Buffer{}
	if err := searchIDs(idsOut, results); err != nil {
		t.Fatalf("searchIDs() error = %v", err)
	}
	if out := idsOut.String(); !strings.Contains(out, record.ID) || !strings.Contains(out, chat.Session.ID) {
		t.Fatalf("expected both ids in ids output, got %q", out)
	}

	jsonOut := &bytes.Buffer{}
	if err := searchJSON(jsonOut, results, map[string]repository.ChildCounts{record.ID: {Figures: 1, DataFiles: 2}}); err != nil {
		t.Fatalf("searchJSON() error = %v", err)
	}
	if out := jsonOut.String(); !strings.Contains(out, `"domain": "records"`) || !strings.Contains(out, `"domain": "chats"`) || !strings.Contains(out, `"figure_count": 1`) {
		t.Fatalf("expected both domains in json output, got %q", out)
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
	addRecordWithContent(
		t,
		"<html>json search test</html>",
		"json notes",
		`{"project_id":"proj-search"}`,
		map[string][]byte{"plot.png": []byte("figure")},
		map[string][]byte{"metrics.json": []byte(`{"ok":true}`)},
	)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "json", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search json: %v", err)
	}

	var items []searchResultJSON
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("parse json: %v\noutput: %s", err, stdout.String())
	}
	if len(items) != 1 {
		t.Fatal("expected at least one result")
	}
	got := items[0]
	if got.ProjectID != "proj-search" {
		t.Fatalf("expected project_id=proj-search, got %v", got.ProjectID)
	}
	if got.ID == "" || got.Date == "" || got.DayOrder == "" || got.SourceDeviceID == "" || got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Fatalf("expected identity/provenance/timestamps to be populated, got %+v", got)
	}
	if got.DeletedAt != nil || got.SourceRef != nil || got.GitRemoteURL != nil || got.GitHash != nil {
		t.Fatalf("expected nullable fields to be null by default, got %+v", got)
	}
	if !got.HasHTML || !got.HasNotes || got.FigureCount != 1 || got.DataFileCount != 1 {
		t.Fatalf("expected enriched fields from record and children, got %+v", got)
	}
}

func TestSearchJSONFlagAlias(t *testing.T) {
	setupEnv(t)
	addRecordWithContent(
		t,
		"<html>json alias search test</html>",
		"json alias notes",
		`{"project_id":"proj-search-json-flag"}`,
		nil,
		nil,
	)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "alias", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search --json: %v", err)
	}

	var items []searchResultJSON
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("parse json: %v\noutput: %s", err, stdout.String())
	}
	if len(items) != 1 {
		t.Fatalf("expected one result, got %d", len(items))
	}
	if items[0].Domain != "records" || items[0].ProjectID != "proj-search-json-flag" {
		t.Fatalf("expected records result from json alias, got %+v", items[0])
	}
}

func TestSearchJSONFlagRejectsConflictingFormat(t *testing.T) {
	setupEnv(t)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "query", "--json", "--format", "table"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected conflicting --json and --format table to fail")
	}
	if !strings.Contains(err.Error(), "--json cannot be combined with --format table") {
		t.Fatalf("expected conflict error, got %v", err)
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

	var items []searchResultJSON
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty array, got %+v", items)
	}
}

func TestSearchJSONWithDeletedAt(t *testing.T) {
	setupEnv(t)
	id := addRecord(t)

	// Soft-delete the record
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "Test", "--format", "json", "--deleted"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search json: %v", err)
	}

	var items []searchResultJSON
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("parse json: %v\noutput: %s", err, stdout.String())
	}

	found := false
	for _, r := range items {
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
	delCmd.SetArgs([]string{"records", "delete", id})
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

func TestSearchValidationAndEmptyPageBranches(t *testing.T) {
	setupEnv(t)
	id := addRecordWithContent(t, "<html>branch needle</html>", "", `{"project_id":"search-branch"}`, nil, nil)

	cases := [][]string{
		{"search", "--offset", "-1", "needle"},
		{"search", "--domain", "notes", "needle"},
	}
	for _, args := range cases {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected %v to fail", args)
		}
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "--offset", "99", "needle"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search high offset: %v", err)
	}
	if !strings.Contains(stdout.String(), "No matching records found.") {
		t.Fatalf("expected no results for high offset, got %q", stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "--domain", "records", "--format", "ids", "branch needle"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search records ids: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != id {
		t.Fatalf("search ids = %q, want %q", stdout.String(), id)
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

func TestSearchDefaultLimitTruncatesIDsToStderr(t *testing.T) {
	setupEnv(t)
	for i := range 51 {
		addRecordWithContent(t, "<html>default limit needle</html>", "", "", nil, nil, "--date", "2026-05-08")
		_ = i
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"search", "needle", "--format", "ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search default limit: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != defaultSearchLimit {
		t.Fatalf("expected %d ids, got %d", defaultSearchLimit, len(lines))
	}
	if !strings.Contains(stderr.String(), "Showing first 50 results (use --limit 0 to see all") {
		t.Fatalf("expected truncation message on stderr, got %q", stderr.String())
	}
}

func TestSearchLimitZeroIsUnlimited(t *testing.T) {
	setupEnv(t)
	for range 51 {
		addRecordWithContent(t, "<html>unlimited needle</html>", "", "", nil, nil, "--date", "2026-05-08")
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"search", "unlimited", "--format", "ids", "--limit", "0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search unlimited: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 51 {
		t.Fatalf("expected 51 ids, got %d", len(lines))
	}
	if stderr.String() != "" {
		t.Fatalf("expected no stderr truncation message, got %q", stderr.String())
	}
}

func TestSearchTableTruncationFooter(t *testing.T) {
	setupEnv(t)
	addRecordWithContent(t, "<html>table footer one</html>", "", "", nil, nil)
	addRecordWithContent(t, "<html>table footer two</html>", "", "", nil, nil)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "footer", "--limit", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("search table footer: %v", err)
	}

	if !strings.Contains(stdout.String(), "Showing first 1 results (use --limit 0 to see all") {
		t.Fatalf("expected truncation footer on stdout, got %q", stdout.String())
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
