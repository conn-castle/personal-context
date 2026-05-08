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

	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestListCommandFiltersAndPaginatesRecords(t *testing.T) {
	setupEnv(t)
	olderID := addSlideWithContent(t, "<html>older</html>", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-01")
	addSlideWithContent(t, "<html>other</html>", "", `{"project_id":"beta/project"}`, nil, nil, "--date", "2026-01-02")
	newerID := addSlideWithContent(t, "<html>newer</html>", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-03")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"list", "--project", "alpha/project", "--limit", "1", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list first page: %v", err)
	}

	var firstPage recordListJSON
	if err := json.Unmarshal(stdout.Bytes(), &firstPage); err != nil {
		t.Fatalf("parse first page json: %v\n%s", err, stdout.String())
	}
	if len(firstPage.Items) != 1 || firstPage.Items[0].ID != newerID {
		t.Fatalf("expected newest alpha record %s, got %+v", newerID, firstPage.Items)
	}
	if firstPage.NextCursor == nil || *firstPage.NextCursor == "" {
		t.Fatalf("expected next cursor, got %+v", firstPage.NextCursor)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"list", "--project", "alpha/project", "--limit", "1", "--cursor", *firstPage.NextCursor, "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list second page: %v", err)
	}

	var secondPage recordListJSON
	if err := json.Unmarshal(stdout.Bytes(), &secondPage); err != nil {
		t.Fatalf("parse second page json: %v\n%s", err, stdout.String())
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID != olderID {
		t.Fatalf("expected older alpha record %s, got %+v", olderID, secondPage.Items)
	}
	if secondPage.NextCursor != nil {
		t.Fatalf("expected no next cursor on second page, got %q", *secondPage.NextCursor)
	}
}

func TestListCommandRejectsInvalidOptions(t *testing.T) {
	setupEnv(t)

	for _, args := range [][]string{
		{"list", "--limit", "0"},
		{"list", "--limit", "501"},
		{"list", "--format", "xml"},
		{"list", "--cursor", "not-base64"},
		{"list", "--from", "2026-99-99"},
		{"list", "--from", "2026-01-02", "--to", "2026-01-01"},
		{"list", "--all", "--cursor", encodeCLICursor(cliCursorPayload{Date: "2026-01-01", DayOrder: "a0", ID: "20260101-aaaabbbb"})},
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
	firstID := addSlideWithContent(t, "<html>first</html>", "notes", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-01")
	addSlideWithContent(t, "", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-02")
	thirdID := addSlideWithContent(t, "<html>third</html>", "", `{"project_id":"alpha/project"}`, nil, map[string][]byte{"data.csv": []byte("data")}, "--date", "2026-01-03")
	noHTMLID := addRecordWithoutHTML(t, "alpha/project", "2026-01-04")
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", firstID})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"list", "--limit", "1"})
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
	cmd.SetArgs([]string{"list", "--format", "ids", "--limit", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list ids: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != noHTMLID || !strings.Contains(stderr.String(), "Next cursor:") {
		t.Fatalf("unexpected ids output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"list", "--has-data", "--all", "--format", "ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list has-data ids: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != thirdID {
		t.Fatalf("expected only data-bearing record %s, got %q", thirdID, stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"list", "--has-html", "--all", "--format", "ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list has-html ids: %v", err)
	}
	if strings.Contains(stdout.String(), noHTMLID) {
		t.Fatalf("expected record without HTML %s to be excluded, got %q", noHTMLID, stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"list", "--deleted", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list deleted json: %v", err)
	}
	var deletedPage recordListJSON
	if err := json.Unmarshal(stdout.Bytes(), &deletedPage); err != nil {
		t.Fatalf("parse deleted json: %v\n%s", err, stdout.String())
	}
	if len(deletedPage.Items) != 1 || deletedPage.Items[0].ID != firstID || deletedPage.Items[0].DeletedAt == nil {
		t.Fatalf("expected deleted record with deleted_at, got %+v", deletedPage.Items)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"list", "--project", "missing/project"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list empty table: %v", err)
	}
	if !strings.Contains(stdout.String(), "No matching records found.") {
		t.Fatalf("expected empty table message, got %q", stdout.String())
	}
}

func TestStatsCommandJSONCountsAndSizes(t *testing.T) {
	setupEnv(t)
	addSlideWithContent(
		t,
		`<html><img src="figures/plot.png"></html>`,
		"notes",
		`{"project_id":"alpha/project"}`,
		map[string][]byte{"plot.png": []byte("figure-bytes")},
		map[string][]byte{"data.csv": []byte("abcde")},
		"--date",
		"2026-01-03",
	)
	deletedID := addSlideWithContent(t, "<html>deleted</html>", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-04")
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", deletedID})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"stats", "--project", "alpha/project", "--format", "json"})
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
	activeID := addSlideWithContent(t, "<html>active</html>", "", `{"project_id":"alpha/project"}`, nil, nil)
	deletedID := addSlideWithContent(t, "<html>deleted</html>", "", `{"project_id":"alpha/project"}`, nil, nil)
	if activeID == deletedID {
		t.Fatal("expected distinct test records")
	}
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", deletedID})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"stats", "--deleted"})
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
	cmd.SetArgs([]string{"stats", "--format", "yaml"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid stats format to fail")
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"stats", "--from", "2026-99-99"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid stats date to fail")
	}
}

func TestStatsCommandTextEmptyStore(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"stats"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stats empty: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "Oldest record date: (none)") || !strings.Contains(out, "Newest record date: (none)") {
		t.Fatalf("expected empty date stats, got %q", out)
	}
}

func TestFilesListCommandReportsLocalInventory(t *testing.T) {
	setupEnv(t)
	recordID := addSlideWithContent(
		t,
		`<html><img src="figures/plot.png"></html>`,
		"",
		`{"project_id":"alpha/project"}`,
		map[string][]byte{"plot.png": []byte("figure-bytes")},
		map[string][]byte{"data.csv": []byte("abcde")},
	)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"files", "list", "--record", recordID, "--format", "json"})
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
	recordID := addSlideWithContent(
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
	cmd.SetArgs([]string{"files", "list", "--record", recordID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("files list table: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "missing") || !strings.Contains(out, "plot.png") {
		t.Fatalf("expected table inventory with missing data file, got %q", out)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"files", "list", "--project", "missing/project"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("files list empty: %v", err)
	}
	if !strings.Contains(stdout.String(), "No matching files found.") {
		t.Fatalf("expected no files message, got %q", stdout.String())
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"files"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("files help command: %v", err)
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"files", "list", "--format", "xml"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid files format to fail")
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"files", "list", "--from", "2026-99-99"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid files date to fail")
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"files", "list", "--record", "20260101-deadbeef"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected missing record filter to fail")
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"stats", "--format", "json"})
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
	if _, err := decodeCLICursor("not-base64"); err == nil {
		t.Fatal("expected invalid base64 cursor to fail")
	}
	if _, err := decodeCLICursor(base64ForTest(t, "{")); err == nil {
		t.Fatal("expected invalid json cursor to fail")
	}
	if _, err := decodeCLICursor(base64ForTest(t, `{"date":"2026-01-01"}`)); err == nil {
		t.Fatal("expected incomplete cursor to fail")
	}

	slides := []repository.Slide{
		repositorySlide("2026-01-02", "m", "20260102-ccccdddd"),
		repositorySlide("2026-01-03", "b", "20260103-aaaabbbb"),
		repositorySlide("2026-01-03", "a", "20260103-eeeeffff"),
		repositorySlide("2026-01-03", "a", "20260103-aaaabbbb"),
	}
	sortRecordsForDiscovery(slides)
	gotOrder := []string{slides[0].ID, slides[1].ID, slides[2].ID, slides[3].ID}
	wantOrder := []string{"20260103-aaaabbbb", "20260103-eeeeffff", "20260103-aaaabbbb", "20260102-ccccdddd"}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("sort order = %v, want %v", gotOrder, wantOrder)
		}
	}
	sortRecordsForDiscovery([]repository.Slide{
		repositorySlide("2026-01-01", "a", "20260101-aaaabbbb"),
		repositorySlide("2026-01-02", "a", "20260102-aaaabbbb"),
	})
	sortRecordsForDiscovery([]repository.Slide{
		repositorySlide("2026-01-01", "z", "20260101-aaaabbbb"),
		repositorySlide("2026-01-01", "a", "20260101-aaaabbbb"),
	})
	sortRecordsForDiscovery([]repository.Slide{
		repositorySlide("2026-01-01", "a", "20260101-eeeeffff"),
		repositorySlide("2026-01-01", "a", "20260101-aaaabbbb"),
		repositorySlide("2026-01-01", "a", "20260101-aaaabbbb"),
	})

	cursor := &cliCursorPayload{Date: "2026-01-02", DayOrder: "m", ID: "20260102-ccccdddd"}
	if !recordIsAfterCursor(repositorySlide("2026-01-01", "a", "20260101-aaaabbbb"), cursor) {
		t.Fatal("expected older date to be after cursor in newest-first order")
	}
	if !recordIsAfterCursor(repositorySlide("2026-01-02", "z", "20260102-aaaabbbb"), cursor) {
		t.Fatal("expected later day_order to be after cursor")
	}
	if !recordIsAfterCursor(repositorySlide("2026-01-02", "m", "20260102-eeeeffff"), cursor) {
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
	if got := applyRecordCursor([]repository.Slide{repositorySlide("2026-01-03", "a", "20260103-aaaabbbb")}, cursor); got != nil {
		t.Fatalf("expected cursor past end to return nil, got %+v", got)
	}
	if err := writeRecordListTable(errorWriter{}, []recordListItem{{ID: "id"}}, nil); err == nil {
		t.Fatal("expected table flush writer error")
	}
}

func TestRecordDiscoveryRepositoryErrorPaths(t *testing.T) {
	ctx := context.Background()
	slide := repository.Slide{ID: "20260101-aaaabbbb", Date: "2026-01-01", DayOrder: "a0"}
	figureErr := errors.New("figure lookup failed")
	dataErr := errors.New("data lookup failed")

	if _, err := buildRecordListItem(ctx, &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
			return nil, figureErr
		},
	}, slide); err == nil || !strings.Contains(err.Error(), "figure lookup failed") {
		t.Fatalf("buildRecordListItem figure error = %v", err)
	}
	if _, err := buildRecordListItem(ctx, &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
			return nil, nil
		},
		listDataFilesFn: func(context.Context, string) ([]repository.SlideDataFile, error) {
			return nil, dataErr
		},
	}, slide); err == nil || !strings.Contains(err.Error(), "data lookup failed") {
		t.Fatalf("buildRecordListItem data error = %v", err)
	}

	fsClient, err := filesystem.NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("filesystem.NewClient: %v", err)
	}
	stack := &localStack{FS: fsClient, Repo: &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
			return nil, figureErr
		},
	}}
	if _, err := buildRecordStats(ctx, t.TempDir(), stack, []repository.Slide{slide}); err == nil || !strings.Contains(err.Error(), "figure lookup failed") {
		t.Fatalf("buildRecordStats figure error = %v", err)
	}

	stack.Repo = &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
			return nil, nil
		},
		listDataFilesFn: func(context.Context, string) ([]repository.SlideDataFile, error) {
			return nil, dataErr
		},
	}
	if _, err := buildFileInventoryItems(ctx, stack, slide); err == nil || !strings.Contains(err.Error(), "data lookup failed") {
		t.Fatalf("buildFileInventoryItems data error = %v", err)
	}

	stack.Repo = &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
			return nil, figureErr
		},
	}
	if _, err := buildFileInventoryItems(ctx, stack, slide); err == nil || !strings.Contains(err.Error(), "figure lookup failed") {
		t.Fatalf("buildFileInventoryItems figure error = %v", err)
	}

	stack.Repo = &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
			return []repository.SlideFigure{{SlideID: slide.ID, Filename: "../bad", S3Key: "figures/bad"}}, nil
		},
		listDataFilesFn: func(context.Context, string) ([]repository.SlideDataFile, error) {
			return nil, nil
		},
	}
	if _, err := buildRecordStats(ctx, t.TempDir(), stack, []repository.Slide{slide}); err == nil {
		t.Fatal("expected buildRecordStats to fail on invalid figure filename")
	}

	stack.Repo = &mockRepo{
		listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
			return nil, nil
		},
		listDataFilesFn: func(context.Context, string) ([]repository.SlideDataFile, error) {
			return []repository.SlideDataFile{{SlideID: slide.ID, Filename: "../bad", S3Key: "data/bad"}}, nil
		},
	}
	if _, err := buildRecordStats(ctx, t.TempDir(), stack, []repository.Slide{slide}); err == nil {
		t.Fatal("expected buildRecordStats to fail on invalid data filename")
	}
	if _, err := buildFileInventoryItems(ctx, stack, slide); err == nil {
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
	slide := repositorySlide("2026-01-01", "a0", "20260101-aaaabbbb")

	newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
		return &mockRepo{
			listSlidesFn: func(context.Context, repository.ListSlidesFilter) ([]repository.Slide, error) {
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
			listSlidesFn: func(context.Context, repository.ListSlidesFilter) ([]repository.Slide, error) {
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
			listSlidesFn: func(context.Context, repository.ListSlidesFilter) ([]repository.Slide, error) {
				return []repository.Slide{slide}, nil
			},
			listFiguresFn: func(context.Context, string) ([]repository.SlideFigure, error) {
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
	includedID := addSlideWithContent(t, "<html>included</html>", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-02")
	excludedByProject := addSlideWithContent(t, "<html>wrong project</html>", "", `{"project_id":"beta/project"}`, nil, nil, "--date", "2026-01-02")
	excludedByDate := addSlideWithContent(t, "<html>wrong date</html>", "", `{"project_id":"alpha/project"}`, nil, nil, "--date", "2026-01-05")

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
		if _, err := os.Stat(filepath.Join(exportDir, "slides", id)); err != nil {
			t.Fatalf("expected exported record %s: %v", id, err)
		}
	}
	for _, id := range []string{excludedByProject, excludedByDate} {
		if _, err := os.Stat(filepath.Join(exportDir, "slides", id)); !os.IsNotExist(err) {
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

func repositorySlide(date string, dayOrder string, id string) repository.Slide {
	return repository.Slide{Date: date, DayOrder: dayOrder, ID: id}
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
	cmd.SetArgs([]string{"add", "--date", date, dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add record without HTML: %v", err)
	}
	return strings.TrimSpace(stdout.String())
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
