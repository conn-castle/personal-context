package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if err := os.WriteFile(filepath.Join(dir, "record.html"), []byte(opts.HTMLContent), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}

	if opts.Notes != "" {
		if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(opts.Notes), 0o644); err != nil {
			t.Fatalf("write notes.md: %v", err)
		}
	}

	if opts.MetadataJSON == "" {
		opts.MetadataJSON = `{"project_id":"test/default-project","source_device_id":"test-device"}`
	} else {
		var meta map[string]any
		if err := json.Unmarshal([]byte(opts.MetadataJSON), &meta); err == nil {
			if _, ok := meta["project_id"]; !ok {
				meta["project_id"] = "test/default-project"
			}
			if _, ok := meta["source_device_id"]; !ok {
				meta["source_device_id"] = "test-device"
			}
			raw, err := json.Marshal(meta)
			if err == nil {
				opts.MetadataJSON = string(raw)
			}
		}
	}
	if opts.MetadataJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(opts.MetadataJSON), 0o644); err != nil {
			t.Fatalf("write metadata.json: %v", err)
		}
		var meta struct {
			ProjectID      string `json:"project_id"`
			SourceDeviceID string `json:"source_device_id"`
		}
		if err := json.Unmarshal([]byte(opts.MetadataJSON), &meta); err == nil && meta.ProjectID != "" && meta.SourceDeviceID != "" {
			ensureRegisteredProjectAndDevice(t, meta.ProjectID, meta.SourceDeviceID)
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
	setupEnv(t)
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
		t.Fatal("expected record ID on stdout, got empty string")
	}
}

func TestAddCommandWithDateFlag(t *testing.T) {
	setupEnv(t)
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
		t.Fatal("expected record ID on stdout")
	}
}

func TestAddCommandWithProjectFlag(t *testing.T) {
	setupEnv(t)
	inputDir := makeInputFolder(t, inputFolderOpts{MetadataJSON: `{"project_id":"my-proj","source_device_id":"test-device"}`})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"add", "--project", "my-proj", inputDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("add --project failed: %v", err)
	}

	id := strings.TrimSpace(stdout.String())
	if id == "" {
		t.Fatal("expected record ID on stdout")
	}
}

func TestAddCommandWithFigures(t *testing.T) {
	setupEnv(t)
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
		t.Fatal("expected record ID on stdout")
	}
}

func TestAddCommandWithDataFiles(t *testing.T) {
	homeDir := setupEnv(t)
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
		t.Fatal("expected record ID on stdout")
	}

	// Verify data file was persisted: show --format json should list it.
	showOut := &bytes.Buffer{}
	showCmd := NewRootCommand(RootCommandOptions{Stdout: showOut, Stderr: &bytes.Buffer{}})
	showCmd.SetArgs([]string{"show", "--format", "json", id})
	if err := showCmd.Execute(); err != nil {
		t.Fatalf("show after add: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(showOut.Bytes(), &parsed); err != nil {
		t.Fatalf("parse show json: %v", err)
	}
	dataFiles, ok := parsed["data_files"].([]interface{})
	if !ok || len(dataFiles) != 1 {
		t.Fatalf("expected 1 data file in show output, got %v", parsed["data_files"])
	}
	df := dataFiles[0].(map[string]interface{})
	if df["filename"] != "results.csv" {
		t.Fatalf("expected filename=results.csv, got %v", df["filename"])
	}
	if df["hash"] == nil || df["hash"] == "" {
		t.Fatal("expected non-empty hash for data file")
	}

	// Verify data file exists on disk.
	dataPath := filepath.Join(homeDir, "personal-context", "data", id, "results.csv")
	if _, err := os.Stat(dataPath); err != nil {
		t.Fatalf("expected data file on disk at %s: %v", dataPath, err)
	}
}

func TestAddCommandInvalidDate(t *testing.T) {
	setupEnv(t)
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

func TestAddCommandMissingRecordHTMLStoresNull(t *testing.T) {
	setupEnv(t)
	// Create an empty directory with no record.html.
	emptyDir := t.TempDir()
	writeDefaultProvenanceMetadata(t, emptyDir)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"add", emptyDir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("add without record.html: %v", err)
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatal("expected record ID on stdout")
	}
}

func TestRunAddStoresNullHTMLAndSourceRef(t *testing.T) {
	setupEnv(t)
	inputDir := t.TempDir()
	writeDefaultProvenanceMetadata(t, inputDir)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runAdd(context.Background(), stdout, stderr, inputDir, "2026-05-07", "", "", "opaque-source", position{kind: "last"}); err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}
	recordID := strings.TrimSpace(stdout.String())
	stack, err := openLocalStack(os.Getenv("PC_HOME"))
	if err != nil {
		t.Fatalf("open stack: %v", err)
	}
	defer func() { _ = stack.Close() }()
	record, err := stack.Repo.GetRecordByID(context.Background(), recordID)
	if err != nil {
		t.Fatalf("GetRecordByID() error = %v", err)
	}
	if record.HTMLContent != nil {
		t.Fatalf("HTMLContent = %q, want nil", *record.HTMLContent)
	}
	if record.SourceRef == nil || *record.SourceRef != "opaque-source" {
		t.Fatalf("SourceRef = %v", record.SourceRef)
	}
}

func TestRunAddRejectsArchivedProjectBeforeCreatingRecord(t *testing.T) {
	setupEnv(t)
	inputDir := makeInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"archived-project","source_device_id":"test-device"}`,
	})

	homeDir, err := resolveHomeDir()
	if err != nil {
		t.Fatalf("resolve home: %v", err)
	}
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open stack: %v", err)
	}
	if _, err := stack.Repo.ArchiveProject(context.Background(), "archived-project"); err != nil {
		t.Fatalf("archive project: %v", err)
	}
	if err := stack.Close(); err != nil {
		t.Fatalf("close stack: %v", err)
	}

	err = runAdd(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, inputDir, "2026-05-07", "", "", "", position{kind: "last"})
	if err == nil {
		t.Fatal("expected archived project error")
	}
	if !strings.Contains(err.Error(), "archived") {
		t.Fatalf("runAdd() error = %v, want archived project", err)
	}
}

func TestAddCommandMutuallyExclusiveFlags(t *testing.T) {
	setupEnv(t)
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
	setupEnv(t)

	// Add the first record (default position = last).
	inputDir1 := makeInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>Record 1</body></html>",
	})
	stdout1 := &bytes.Buffer{}
	cmd1 := NewRootCommand(RootCommandOptions{Stdout: stdout1, Stderr: &bytes.Buffer{}})
	cmd1.SetArgs([]string{"add", "--date", "2025-07-01", inputDir1})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("add record 1 failed: %v", err)
	}
	id1 := strings.TrimSpace(stdout1.String())
	if id1 == "" {
		t.Fatal("expected record 1 ID")
	}

	// Add the second record with --first on the same date.
	inputDir2 := makeInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>Record 2</body></html>",
	})
	stdout2 := &bytes.Buffer{}
	cmd2 := NewRootCommand(RootCommandOptions{Stdout: stdout2, Stderr: &bytes.Buffer{}})
	cmd2.SetArgs([]string{"add", "--first", "--date", "2025-07-01", inputDir2})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("add record 2 --first failed: %v", err)
	}
	id2 := strings.TrimSpace(stdout2.String())
	if id2 == "" {
		t.Fatal("expected record 2 ID")
	}
}

func TestAddCommandNoArgs(t *testing.T) {
	setupEnv(t)

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
	pos, err := resolvePositionFlags(false, false, "record-abc", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos.kind != "after" {
		t.Fatalf("expected kind 'after', got %q", pos.kind)
	}
	if pos.referenceID != "record-abc" {
		t.Fatalf("expected referenceID 'record-abc', got %q", pos.referenceID)
	}
}

func TestResolvePositionFlagsBefore(t *testing.T) {
	pos, err := resolvePositionFlags(false, false, "", "record-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos.kind != "before" {
		t.Fatalf("expected kind 'before', got %q", pos.kind)
	}
	if pos.referenceID != "record-xyz" {
		t.Fatalf("expected referenceID 'record-xyz', got %q", pos.referenceID)
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

func TestResolvePositionFlagsLastExplicit(t *testing.T) {
	pos, err := resolvePositionFlags(false, true, "", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if pos.kind != "last" {
		t.Fatalf("expected last, got %q", pos.kind)
	}
}

// --- computeDayOrder coverage: first, after, before on non-empty list ---

func TestComputeDayOrderFirst(t *testing.T) {
	setupEnv(t)
	// Add two records on same date so the list is non-empty
	id1 := addRecord(t, "--date", "2025-05-01")
	id2 := addRecord(t, "--date", "2025-05-01")

	// Now add a third with --first
	id3 := addRecord(t, "--date", "2025-05-01", "--first")

	order1 := getDayOrder(t, id1)
	order2 := getDayOrder(t, id2)
	order3 := getDayOrder(t, id3)

	// --first record should sort before both existing records
	if order3 >= order1 {
		t.Fatalf("--first record day_order %q should be < first existing %q", order3, order1)
	}
	if order1 >= order2 {
		t.Fatalf("original order violated: id1 %q should be < id2 %q", order1, order2)
	}
}

func TestComputeDayOrderAfter(t *testing.T) {
	setupEnv(t)
	id1 := addRecord(t, "--date", "2025-05-02")
	id2 := addRecord(t, "--date", "2025-05-02")

	// Add a third after id1 (should be between id1 and id2)
	id3 := addRecord(t, "--date", "2025-05-02", "--after", id1)

	order1 := getDayOrder(t, id1)
	order2 := getDayOrder(t, id2)
	order3 := getDayOrder(t, id3)

	if order3 <= order1 {
		t.Fatalf("--after id1: new record day_order %q should be > id1 %q", order3, order1)
	}
	if order3 >= order2 {
		t.Fatalf("--after id1: new record day_order %q should be < id2 %q", order3, order2)
	}
}

func TestComputeDayOrderAfterLast(t *testing.T) {
	setupEnv(t)
	id1 := addRecord(t, "--date", "2025-05-03")
	id2 := addRecord(t, "--date", "2025-05-03")

	// Add after the last record (should use GenerateAtEnd)
	id3 := addRecord(t, "--date", "2025-05-03", "--after", id2)

	order1 := getDayOrder(t, id1)
	order2 := getDayOrder(t, id2)
	order3 := getDayOrder(t, id3)

	if order1 >= order2 {
		t.Fatalf("original order violated: id1 %q should be < id2 %q", order1, order2)
	}
	if order3 <= order2 {
		t.Fatalf("--after last: new record day_order %q should be > id2 %q", order3, order2)
	}
}

func TestComputeDayOrderBefore(t *testing.T) {
	setupEnv(t)
	id1 := addRecord(t, "--date", "2025-05-04")
	id2 := addRecord(t, "--date", "2025-05-04")

	// Add before id2 (should be between id1 and id2)
	id3 := addRecord(t, "--date", "2025-05-04", "--before", id2)

	order1 := getDayOrder(t, id1)
	order2 := getDayOrder(t, id2)
	order3 := getDayOrder(t, id3)

	if order3 <= order1 {
		t.Fatalf("--before id2: new record day_order %q should be > id1 %q", order3, order1)
	}
	if order3 >= order2 {
		t.Fatalf("--before id2: new record day_order %q should be < id2 %q", order3, order2)
	}
}

func TestComputeDayOrderBeforeFirst(t *testing.T) {
	setupEnv(t)
	id1 := addRecord(t, "--date", "2025-05-05")
	id2 := addRecord(t, "--date", "2025-05-05")

	// Add before the first record (should use GenerateAtStart)
	id3 := addRecord(t, "--date", "2025-05-05", "--before", id1)

	order1 := getDayOrder(t, id1)
	order2 := getDayOrder(t, id2)
	order3 := getDayOrder(t, id3)

	if order3 >= order1 {
		t.Fatalf("--before first: new record day_order %q should be < id1 %q", order3, order1)
	}
	if order1 >= order2 {
		t.Fatalf("original order violated: id1 %q should be < id2 %q", order1, order2)
	}
}

func TestComputeDayOrderAfterNotFound(t *testing.T) {
	setupEnv(t)
	addRecord(t, "--date", "2025-05-06")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "record.html"), []byte("<html>X</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDefaultProvenanceMetadata(t, dir)
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", "--date", "2025-05-06", "--after", "nonexistent-id", dir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --after nonexistent")
	}
	if !strings.Contains(err.Error(), "reference record") {
		t.Fatalf("expected reference record error, got: %v", err)
	}
}

func TestComputeDayOrderBeforeNotFound(t *testing.T) {
	setupEnv(t)
	addRecord(t, "--date", "2025-05-07")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "record.html"), []byte("<html>X</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDefaultProvenanceMetadata(t, dir)
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"add", "--date", "2025-05-07", "--before", "nonexistent-id", dir})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --before nonexistent")
	}
	if !strings.Contains(err.Error(), "reference record") {
		t.Fatalf("expected reference record error, got: %v", err)
	}
}
