package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/listpage"
	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestListCommandFiltersAndPaginatesRecords(t *testing.T) {
	setupEnv(t)
	olderID := addRecordWithContent(t, "<html>older</html>", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-01")
	addRecordWithContent(t, "<html>other</html>", "", `{"project_id":"beta/project"}`, nil, nil, "--date", "2026-01-02")
	newerID := addRecordWithContent(t, "<html>newer</html>", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-03")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "list", "--project", "alpha/project", "--limit", "1", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list first page: %v", err)
	}

	var firstPage listpage.Response[recordListItem]
	if err := json.Unmarshal(stdout.Bytes(), &firstPage); err != nil {
		t.Fatalf("parse first page json: %v\n%s", err, stdout.String())
	}
	if len(firstPage.Items) != 1 || firstPage.Items[0].ID != newerID {
		t.Fatalf("expected newest alpha record %s, got %+v", newerID, firstPage.Items)
	}
	if firstPage.Total != 2 {
		t.Fatalf("expected total=2, got %d", firstPage.Total)
	}
	if firstPage.NextCursor == nil || *firstPage.NextCursor == "" {
		t.Fatalf("expected next cursor, got %+v", firstPage.NextCursor)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "list", "--project", "alpha/project", "--limit", "1", "--cursor", *firstPage.NextCursor, "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list second page: %v", err)
	}

	var secondPage listpage.Response[recordListItem]
	if err := json.Unmarshal(stdout.Bytes(), &secondPage); err != nil {
		t.Fatalf("parse second page json: %v\n%s", err, stdout.String())
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID != olderID {
		t.Fatalf("expected older alpha record %s, got %+v", olderID, secondPage.Items)
	}
	if secondPage.Total != 2 {
		t.Fatalf("expected cursor page total=2, got %d", secondPage.Total)
	}
	if secondPage.NextCursor != nil {
		t.Fatalf("expected no next cursor on second page, got %q", *secondPage.NextCursor)
	}
}

func TestListCommandRejectsInvalidOptions(t *testing.T) {
	setupEnv(t)

	for _, args := range [][]string{
		{"records", "list", "--limit", "0"},
		{"records", "list", "--limit", "501"},
		{"records", "list", "--format", "xml"},
		{"records", "list", "--cursor", "not-base64"},
		{"records", "list", "--from", "2026-99-99"},
		{"records", "list", "--from", "2026-01-02", "--to", "2026-01-01"},
		{"records", "list", "--all", "--cursor", listpage.EncodeCursor(listpage.Cursor{Date: "2026-01-01", DayOrder: "a0", ID: "20260101-aaaabbbb"})},
	} {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected %v to fail", args)
		}
	}
}

func TestListCommandTableIDsDeletedAndEmptyOutput(t *testing.T) {
	setupEnv(t)
	firstID := addRecordWithContent(t, "<html>first</html>", "notes", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-01")
	addRecordWithContent(t, "", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-02")
	thirdID := addRecordWithContent(t, "<html>third</html>", "", `{"project_id":"alpha/project"}`, nil, map[string][]byte{"data.csv": []byte("data")}, "--date", "2026-01-03")
	noHTMLID := addRecordWithoutHTML(t, "alpha/project", "2026-01-04")
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", firstID})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "list", "--limit", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list table: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, noHTMLID) || !strings.Contains(out, "ID") {
		t.Fatalf("expected table with active record %s, got %q", noHTMLID, out)
	}
	if !strings.Contains(stdout.String(), "Next cursor:") {
		t.Fatalf("expected table next cursor, got %q", stdout.String())
	}

	stdout.Reset()
	stderr := &bytes.Buffer{}
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"records", "list", "--format", "ids", "--limit", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list ids: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != noHTMLID || !strings.Contains(stderr.String(), "Next cursor:") {
		t.Fatalf("unexpected ids output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "list", "--has-data", "--all", "--format", "ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list has-data ids: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != thirdID {
		t.Fatalf("expected only data-bearing record %s, got %q", thirdID, stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "list", "--has-html", "--all", "--format", "ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list has-html ids: %v", err)
	}
	if strings.Contains(stdout.String(), noHTMLID) {
		t.Fatalf("expected record without HTML %s to be excluded, got %q", noHTMLID, stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "list", "--deleted", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list deleted json: %v", err)
	}
	var deletedPage listpage.Response[recordListItem]
	if err := json.Unmarshal(stdout.Bytes(), &deletedPage); err != nil {
		t.Fatalf("parse deleted json: %v\n%s", err, stdout.String())
	}
	if len(deletedPage.Items) != 1 || deletedPage.Items[0].ID != firstID || deletedPage.Items[0].DeletedAt == nil {
		t.Fatalf("expected deleted record with deleted_at, got %+v", deletedPage.Items)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "list", "--project", "missing/project"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list empty table: %v", err)
	}
	if !strings.Contains(stdout.String(), "No matching records found.") {
		t.Fatalf("expected empty table message, got %q", stdout.String())
	}
}

func TestListCommandContentFiltersJSONTotals(t *testing.T) {
	setupEnv(t)
	dataOldID := addRecordWithContent(t, "<html>data old</html>", "", `{"project_id":"alpha/project"}`, nil, map[string][]byte{"old.csv": []byte("old")}, "--date", "2026-01-01")
	addRecordWithoutHTML(t, "alpha/project", "2026-01-02")
	addRecordWithContent(t, "<html>html only</html>", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-03")
	dataNewID := addRecordWithContent(t, "<html>data new</html>", "", `{"project_id":"alpha/project"}`, nil, map[string][]byte{"new.csv": []byte("new")}, "--date", "2026-01-04")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "list", "--has-data", "--limit", "1", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list has-data json: %v", err)
	}
	var dataPage listpage.Response[recordListItem]
	if err := json.Unmarshal(stdout.Bytes(), &dataPage); err != nil {
		t.Fatalf("parse has-data json: %v\n%s", err, stdout.String())
	}
	if dataPage.Total != 2 || len(dataPage.Items) != 1 || dataPage.Items[0].ID != dataNewID || dataPage.NextCursor == nil {
		t.Fatalf("unexpected has-data page: %+v", dataPage)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "list", "--has-data", "--limit", "1", "--cursor", *dataPage.NextCursor, "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list has-data cursor json: %v", err)
	}
	var secondDataPage listpage.Response[recordListItem]
	if err := json.Unmarshal(stdout.Bytes(), &secondDataPage); err != nil {
		t.Fatalf("parse has-data cursor json: %v\n%s", err, stdout.String())
	}
	if secondDataPage.Total != 2 || len(secondDataPage.Items) != 1 || secondDataPage.Items[0].ID != dataOldID || secondDataPage.NextCursor != nil {
		t.Fatalf("unexpected has-data cursor page: %+v", secondDataPage)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "list", "--has-html", "--all", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list has-html json: %v", err)
	}
	var htmlPage listpage.Response[recordListItem]
	if err := json.Unmarshal(stdout.Bytes(), &htmlPage); err != nil {
		t.Fatalf("parse has-html json: %v\n%s", err, stdout.String())
	}
	if htmlPage.Total != 3 || len(htmlPage.Items) != 3 {
		t.Fatalf("unexpected has-html page: %+v", htmlPage)
	}
}

func TestStatsCommandJSONCountsAndSizes(t *testing.T) {
	setupEnv(t)
	addRecordWithContent(
		t,
		`<html><img src="figures/plot.png"></html>`,
		"notes",
		`{"project_id":"alpha/project"}`,
		map[string][]byte{"plot.png": []byte("figure-bytes")},
		map[string][]byte{"data.csv": []byte("abcde")},
		"--date",
		"2026-01-03",
	)
	deletedID := addRecordWithContent(t, "<html>deleted</html>", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-04")
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", deletedID})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "stats", "--project", "alpha/project", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stats: %v", err)
	}

	var stats recordStatsJSON
	if err := json.Unmarshal(stdout.Bytes(), &stats); err != nil {
		t.Fatalf("parse stats json: %v\n%s", err, stdout.String())
	}
	if stats.ActiveRecordCount != 1 || stats.DeletedRecordCount != 1 || stats.SelectedRecordCount != 1 {
		t.Fatalf("unexpected record counts: %+v", stats)
	}
	if stats.HTMLRecordCount != 1 || stats.NotesRecordCount != 1 || stats.FigureCount != 1 || stats.DataFileCount != 1 {
		t.Fatalf("unexpected content counts: %+v", stats)
	}
	if stats.RecordedDataFileBytes != 5 {
		t.Fatalf("expected recorded data-file bytes 5, got %d", stats.RecordedDataFileBytes)
	}
	if stats.LocalAttachmentBytes != int64(len("figure-bytes")+len("abcde")) {
		t.Fatalf("unexpected local attachment bytes: %d", stats.LocalAttachmentBytes)
	}
	if stats.StoreFileBytes <= 0 || stats.LocalTotalBytes != stats.StoreFileBytes+stats.LocalAttachmentBytes {
		t.Fatalf("unexpected store/total bytes: %+v", stats)
	}
}

func TestStatsCommandTextDeletedAndInvalidFormat(t *testing.T) {
	setupEnv(t)
	activeID := addRecordWithContent(t, "<html>active</html>", "", `{"project_id":"alpha/project"}`, nil, nil)
	deletedID := addRecordWithContent(t, "<html>deleted</html>", "", `{"project_id":"alpha/project"}`, nil, nil)
	if activeID == deletedID {
		t.Fatal("expected distinct test records")
	}
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", deletedID})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "stats", "--deleted"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stats text deleted: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"Selected records: 1 (deleted)", "Active records: 1", "Deleted records: 1", "Local total bytes:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in stats output, got %q", want, out)
		}
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "stats", "--format", "yaml"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid stats format to fail")
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "stats", "--from", "2026-99-99"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid stats date to fail")
	}
}

func TestStatsCommandTextEmptyStore(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "stats"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stats empty: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "Oldest record date: (none)") || !strings.Contains(out, "Newest record date: (none)") {
		t.Fatalf("expected empty date stats, got %q", out)
	}
}

func TestFilesListCommandReportsLocalInventory(t *testing.T) {
	setupEnv(t)
	recordID := addRecordWithContent(
		t,
		`<html><img src="figures/plot.png"></html>`,
		"",
		`{"project_id":"alpha/project"}`,
		map[string][]byte{"plot.png": []byte("figure-bytes")},
		map[string][]byte{"data.csv": []byte("abcde")},
	)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "files", "list", "--record", recordID, "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("files list: %v", err)
	}

	var items []fileInventoryItem
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatalf("parse files json: %v\n%s", err, stdout.String())
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 files, got %d: %+v", len(items), items)
	}
	kinds := map[string]fileInventoryItem{}
	for _, item := range items {
		kinds[item.Kind] = item
		if item.RecordID != recordID || item.Status != "present" || item.LocalPath == "" {
			t.Fatalf("unexpected file inventory item: %+v", item)
		}
	}
	if kinds["figure"].LocalSize == nil || *kinds["figure"].LocalSize != int64(len("figure-bytes")) {
		t.Fatalf("unexpected figure item: %+v", kinds["figure"])
	}
	if kinds["data"].RecordedSize == nil || *kinds["data"].RecordedSize != 5 {
		t.Fatalf("unexpected data item: %+v", kinds["data"])
	}
}

func TestFilesListCommandTableMissingEmptyAndInvalid(t *testing.T) {
	homeDir := setupEnv(t)
	recordID := addRecordWithContent(
		t,
		`<html><img src="figures/plot.png"></html>`,
		"",
		`{"project_id":"alpha/project"}`,
		map[string][]byte{"plot.png": []byte("figure-bytes")},
		map[string][]byte{"data.csv": []byte("abcde")},
	)
	if err := os.Remove(filepath.Join(basePath(homeDir), "data", recordID, "data.csv")); err != nil {
		t.Fatalf("remove local data file: %v", err)
	}
	if err := os.Remove(filepath.Join(basePath(homeDir), "figures", recordID, "plot.png")); err != nil {
		t.Fatalf("remove local figure file: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "files", "list", "--record", recordID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("files list table: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "missing") || !strings.Contains(out, "plot.png") {
		t.Fatalf("expected table inventory with missing data file, got %q", out)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "files", "list", "--project", "missing/project"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("files list empty: %v", err)
	}
	if !strings.Contains(stdout.String(), "No matching files found.") {
		t.Fatalf("expected no files message, got %q", stdout.String())
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "files"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("files help command: %v", err)
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "files", "list", "--format", "xml"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid files format to fail")
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "files", "list", "--from", "2026-99-99"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid files date to fail")
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "files", "list", "--record", "20260101-deadbeef"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected missing record filter to fail")
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "stats", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stats with missing local data file: %v", err)
	}
	var stats recordStatsJSON
	if err := json.Unmarshal(stdout.Bytes(), &stats); err != nil {
		t.Fatalf("parse missing-file stats json: %v\n%s", err, stdout.String())
	}
	if stats.MissingAttachmentCount != 2 {
		t.Fatalf("expected two missing attachments, got %+v", stats)
	}
}

func TestRecordDiscoveryHelperErrorPaths(t *testing.T) {
	if _, err := listpage.DecodeCursor("not!valid!base64"); err == nil {
		t.Fatal("expected invalid base64 cursor to fail")
	}
	if _, err := listpage.DecodeCursor(base64ForTest(t, "{")); err == nil {
		t.Fatal("expected invalid json cursor to fail")
	}
	if _, err := listpage.DecodeCursor(base64ForTest(t, `{"date":"2026-01-01"}`)); err == nil {
		t.Fatal("expected incomplete cursor to fail")
	}

	records := []repository.Record{
		repositoryRecord("2026-01-02", "m", "20260102-ccccdddd"),
		repositoryRecord("2026-01-03", "b", "20260103-aaaabbbb"),
		repositoryRecord("2026-01-03", "a", "20260103-eeeeffff"),
		repositoryRecord("2026-01-03", "a", "20260103-aaaabbbb"),
	}
	sortRecordsForDiscovery(records)
	gotOrder := []string{records[0].ID, records[1].ID, records[2].ID, records[3].ID}
	wantOrder := []string{"20260103-aaaabbbb", "20260103-eeeeffff", "20260103-aaaabbbb", "20260102-ccccdddd"}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("sort order = %v, want %v", gotOrder, wantOrder)
		}
	}
	sortRecordsForDiscovery([]repository.Record{
		repositoryRecord("2026-01-01", "a", "20260101-aaaabbbb"),
		repositoryRecord("2026-01-02", "a", "20260102-aaaabbbb"),
	})
	sortRecordsForDiscovery([]repository.Record{
		repositoryRecord("2026-01-01", "z", "20260101-aaaabbbb"),
		repositoryRecord("2026-01-01", "a", "20260101-aaaabbbb"),
	})
	sortRecordsForDiscovery([]repository.Record{
		repositoryRecord("2026-01-01", "a", "20260101-eeeeffff"),
		repositoryRecord("2026-01-01", "a", "20260101-aaaabbbb"),
		repositoryRecord("2026-01-01", "a", "20260101-aaaabbbb"),
	})

	cursor := listpage.Cursor{Date: "2026-01-02", DayOrder: "m", ID: "20260102-ccccdddd"}
	older := repositoryRecord("2026-01-01", "a", "20260101-aaaabbbb")
	if !listpage.IsAfterCursor(older.Date, older.DayOrder, older.ID, cursor) {
		t.Fatal("expected older date to be after cursor in newest-first order")
	}
	laterOrder := repositoryRecord("2026-01-02", "z", "20260102-aaaabbbb")
	if !listpage.IsAfterCursor(laterOrder.Date, laterOrder.DayOrder, laterOrder.ID, cursor) {
		t.Fatal("expected later day_order to be after cursor")
	}
	laterID := repositoryRecord("2026-01-02", "m", "20260102-eeeeffff")
	if !listpage.IsAfterCursor(laterID.Date, laterID.DayOrder, laterID.ID, cursor) {
		t.Fatal("expected later id to be after cursor")
	}

	dir := t.TempDir()
	if _, _, err := statLocalAttachment(func(string, string) (string, error) { return dir, nil }, "id", "file"); err == nil {
		t.Fatal("expected statLocalAttachment to reject directories")
	}
	if _, _, err := statLocalAttachment(func(string, string) (string, error) { return "", errors.New("resolve failed") }, "id", "file"); err == nil {
		t.Fatal("expected statLocalAttachment to surface resolver failures")
	}
	if _, _, _, err := resolveLocalAttachment(func(string, string) (string, error) { return dir, nil }, "id", "file"); err == nil {
		t.Fatal("expected resolveLocalAttachment to reject directories")
	}
	if _, _, _, err := resolveLocalAttachment(func(string, string) (string, error) { return "", errors.New("resolve failed") }, "id", "file"); err == nil {
		t.Fatal("expected resolveLocalAttachment to surface resolver failures")
	}
	if got := applyRecordCursor([]repository.Record{repositoryRecord("2026-01-03", "a", "20260103-aaaabbbb")}, &cursor); got != nil {
		t.Fatalf("expected cursor past end to return nil, got %+v", got)
	}
	if err := writeRecordListTable(errorWriter{}, []recordListItem{{ID: "id"}}, nil); err == nil {
		t.Fatal("expected table flush writer error")
	}
}

func TestRecordDiscoveryRepositoryErrorPaths(t *testing.T) {
	ctx := context.Background()
	record := repository.Record{ID: "20260101-aaaabbbb", Date: "2026-01-01", DayOrder: "a0"}
	figureErr := errors.New("figure lookup failed")
	dataErr := errors.New("data lookup failed")
	item := buildRecordListItem(record, repository.ChildCounts{Figures: 2, DataFiles: 3})
	if item.FigureCount != 2 || item.DataFileCount != 3 {
		t.Fatalf("counts = (%d, %d), want (2, 3)", item.FigureCount, item.DataFileCount)
	}

	fsClient, err := filesystem.NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("filesystem.NewClient: %v", err)
	}
	stack := &localStack{FS: fsClient, Repo: &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
			return nil, figureErr
		},
	}}
	if _, err := buildRecordStats(ctx, t.TempDir(), stack, []repository.Record{record}); err == nil || !strings.Contains(err.Error(), "figure lookup failed") {
		t.Fatalf("buildRecordStats figure error = %v", err)
	}

	stack.Repo = &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
			return nil, nil
		},
		listDataFilesFn: func(context.Context, string) ([]repository.RecordDataFile, error) {
			return nil, dataErr
		},
	}
	if _, err := buildFileInventoryItems(ctx, stack, record); err == nil || !strings.Contains(err.Error(), "data lookup failed") {
		t.Fatalf("buildFileInventoryItems data error = %v", err)
	}

	stack.Repo = &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
			return nil, figureErr
		},
	}
	if _, err := buildFileInventoryItems(ctx, stack, record); err == nil || !strings.Contains(err.Error(), "figure lookup failed") {
		t.Fatalf("buildFileInventoryItems figure error = %v", err)
	}

	stack.Repo = &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
			return []repository.RecordFigure{{RecordID: record.ID, Filename: "../bad", S3Key: "figures/bad"}}, nil
		},
		listDataFilesFn: func(context.Context, string) ([]repository.RecordDataFile, error) {
			return nil, nil
		},
	}
	if _, err := buildRecordStats(ctx, t.TempDir(), stack, []repository.Record{record}); err == nil {
		t.Fatal("expected buildRecordStats to fail on invalid figure filename")
	}

	stack.Repo = &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
			return nil, nil
		},
		listDataFilesFn: func(context.Context, string) ([]repository.RecordDataFile, error) {
			return []repository.RecordDataFile{{RecordID: record.ID, Filename: "../bad", S3Key: "data/bad"}}, nil
		},
	}
	if _, err := buildRecordStats(ctx, t.TempDir(), stack, []repository.Record{record}); err == nil {
		t.Fatal("expected buildRecordStats to fail on invalid data filename")
	}
	if _, err := buildFileInventoryItems(ctx, stack, record); err == nil {
		t.Fatal("expected buildFileInventoryItems to fail on invalid data filename")
	}
}

func TestRecordDiscoveryHomeDirErrorPaths(t *testing.T) {
	origResolveHomeDirFn := resolveHomeDirFn
	resolveHomeDirFn = func() (string, error) {
		return "", errors.New("home unavailable")
	}
	t.Cleanup(func() { resolveHomeDirFn = origResolveHomeDirFn })

	if err := runList(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, recordListOptions{Limit: 1, Format: "json"}); err == nil || !strings.Contains(err.Error(), "home unavailable") {
		t.Fatalf("runList home error = %v", err)
	}
	if err := runStats(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, recordStatsOptions{Format: "json"}); err == nil || !strings.Contains(err.Error(), "home unavailable") {
		t.Fatalf("runStats home error = %v", err)
	}
	if err := runFilesList(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, filesListOptions{Format: "json"}); err == nil || !strings.Contains(err.Error(), "home unavailable") {
		t.Fatalf("runFilesList home error = %v", err)
	}
}

func TestRecordDiscoveryOpenStackErrorPaths(t *testing.T) {
	origResolveHomeDirFn := resolveHomeDirFn
	resolveHomeDirFn = func() (string, error) {
		return t.TempDir(), nil
	}
	t.Cleanup(func() { resolveHomeDirFn = origResolveHomeDirFn })

	if err := runList(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, recordListOptions{Limit: 1, Format: "json"}); err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("runList open stack error = %v", err)
	}
	if err := runStats(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, recordStatsOptions{Format: "json"}); err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("runStats open stack error = %v", err)
	}
	if err := runFilesList(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, filesListOptions{Format: "json"}); err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("runFilesList open stack error = %v", err)
	}
}

func TestRecordDiscoveryRunRepositoryErrorPaths(t *testing.T) {
	homeDir := setupEnv(t)
	withResolvedHomeDir(t, homeDir)

	origNewSQLiteRepoFn := newSQLiteRepoFn
	t.Cleanup(func() { newSQLiteRepoFn = origNewSQLiteRepoFn })
	listErr := errors.New("list failed")
	childErr := errors.New("children failed")
	record := repositoryRecord("2026-01-01", "a0", "20260101-aaaabbbb")

	newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
		return &mockRepo{
			listRecordsFn: func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error) {
				return nil, listErr
			},
		}, nil
	}
	if err := runList(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, recordListOptions{Limit: 1, Format: "json"}); err == nil || !strings.Contains(err.Error(), "list failed") {
		t.Fatalf("runList list error = %v", err)
	}
	if err := runFilesList(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, filesListOptions{Format: "json"}); err == nil || !strings.Contains(err.Error(), "list failed") {
		t.Fatalf("runFilesList list error = %v", err)
	}
	if err := runStats(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, recordStatsOptions{Format: "json"}); err == nil || !strings.Contains(err.Error(), "list failed") {
		t.Fatalf("runStats active list error = %v", err)
	}

	activeCalls := 0
	newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
		return &mockRepo{
			listRecordsFn: func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error) {
				activeCalls++
				if activeCalls == 1 {
					return nil, nil
				}
				return nil, listErr
			},
		}, nil
	}
	if err := runStats(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, recordStatsOptions{Format: "json"}); err == nil || !strings.Contains(err.Error(), "list failed") {
		t.Fatalf("runStats deleted list error = %v", err)
	}

	newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
		return &mockRepo{
			listRecordsFn: func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error) {
				return []repository.Record{record}, nil
			},
			listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
				return nil, childErr
			},
		}, nil
	}
	if err := runList(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, recordListOptions{Limit: 1, Format: "json"}); err == nil || !strings.Contains(err.Error(), "children failed") {
		t.Fatalf("runList child error = %v", err)
	}
	if err := runFilesList(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, filesListOptions{Format: "json"}); err == nil || !strings.Contains(err.Error(), "children failed") {
		t.Fatalf("runFilesList child error = %v", err)
	}
	if err := runStats(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, recordStatsOptions{Format: "json"}); err == nil || !strings.Contains(err.Error(), "children failed") {
		t.Fatalf("runStats child error = %v", err)
	}
}

func TestExportCommandFiltersRecords(t *testing.T) {
	setupEnv(t)
	includedID := addRecordWithContent(t, "<html>included</html>", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-02")
	excludedByProject := addRecordWithContent(t, "<html>wrong project</html>", "", `{"project_id":"beta/project"}`, nil, nil, "--date", "2026-01-02")
	excludedByDate := addRecordWithContent(t, "<html>wrong date</html>", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-05")

	exportDir := t.TempDir()
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"export", "--path", exportDir, "--project", "alpha/project", "--from", "2026-01-01", "--to", "2026-01-03"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(stdout.String(), "Exported 1 records") {
		t.Fatalf("expected filtered export count, got %q", stdout.String())
	}

	for _, id := range []string{includedID} {
		if _, err := os.Stat(filepath.Join(exportDir, "records", id)); err != nil {
			t.Fatalf("expected exported record %s: %v", id, err)
		}
	}
	for _, id := range []string{excludedByProject, excludedByDate} {
		if _, err := os.Stat(filepath.Join(exportDir, "records", id)); !os.IsNotExist(err) {
			t.Fatalf("expected record %s to be filtered out, stat err = %v", id, err)
		}
	}
}

func base64ForTest(t *testing.T, value string) string {
	t.Helper()
	return encodeBase64ForTest([]byte(value))
}

func encodeBase64ForTest(data []byte) string {
	return strings.TrimRight(base64.StdEncoding.EncodeToString(data), "\n")
}

func repositoryRecord(date string, dayOrder string, id string) repository.Record {
	return repository.Record{Date: date, DayOrder: dayOrder, ID: id}
}

func addRecordWithoutHTML(t *testing.T, projectID string, date string) string {
	t.Helper()
	ensureRegisteredProjectAndDevice(t, projectID, "test-device")
	dir := t.TempDir()
	metadata := `{"project_id":"` + projectID + `","source_device_id":"test-device"}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("notes-only"), 0o644); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "add", "--date", date, dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add record without HTML: %v", err)
	}
	return strings.TrimSpace(stdout.String())
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestRecordMatchesFilterCoversAllBranches(t *testing.T) {
	now := time.Now().UTC()
	active := repository.Record{ID: "a1", Date: "2026-02-15", ProjectID: "alpha/project", UpdatedAt: now}
	deleted := active
	when := now
	deleted.DeletedAt = &when

	project := "alpha/project"
	other := "beta/project"
	from := "2026-02-01"
	earlierFrom := "2026-03-01"
	to := "2026-02-28"
	earlierTo := "2026-02-01"

	cases := []struct {
		name   string
		record repository.Record
		filter repository.ListRecordsFilter
		want   bool
	}{
		{name: "matches with no filter", record: active, filter: repository.ListRecordsFilter{}, want: true},
		{name: "project mismatch", record: active, filter: repository.ListRecordsFilter{ProjectID: &other}, want: false},
		{name: "project match", record: active, filter: repository.ListRecordsFilter{ProjectID: &project}, want: true},
		{name: "from window match", record: active, filter: repository.ListRecordsFilter{DateFrom: &from}, want: true},
		{name: "from window before", record: active, filter: repository.ListRecordsFilter{DateFrom: &earlierFrom}, want: false},
		{name: "to window match", record: active, filter: repository.ListRecordsFilter{DateTo: &to}, want: true},
		{name: "to window after", record: active, filter: repository.ListRecordsFilter{DateTo: &earlierTo}, want: false},
		{name: "only-deleted excludes active", record: active, filter: repository.ListRecordsFilter{IncludeDeleted: true, OnlyDeleted: true}, want: false},
		{name: "only-deleted matches deleted", record: deleted, filter: repository.ListRecordsFilter{IncludeDeleted: true, OnlyDeleted: true}, want: true},
		{name: "default excludes deleted", record: deleted, filter: repository.ListRecordsFilter{}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := recordMatchesFilter(tc.record, tc.filter); got != tc.want {
				t.Fatalf("recordMatchesFilter(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestFilesListCommandRecordFilterMismatch(t *testing.T) {
	setupEnv(t)
	recordID := addRecordWithContent(t, "<html>x</html>", "", `{"project_id":"alpha/project"}`, nil, nil)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "files", "list", "--record", recordID, "--project", "beta/project"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "does not match the requested filters") {
		t.Fatalf("expected filter mismatch error, got %v", err)
	}
}
