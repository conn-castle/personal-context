package e2e_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEditReplacesContent(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add initial record
	addDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>Original</body></html>",
		Notes:       "original notes",
	})
	stdout := runPCSuccess(t, homeDir, "add", addDir)
	recordID := strings.TrimSpace(stdout)

	// Capture immutable fields before edit
	db := openTestDB(t, homeDir)
	var dateBefore, dayOrderBefore string
	if err := db.QueryRow("SELECT date, day_order FROM records WHERE id = ?", recordID).Scan(&dateBefore, &dayOrderBefore); err != nil {
		t.Fatalf("query before edit: %v", err)
	}

	// Edit with new content
	editDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>Replaced</body></html>",
		Notes:       "replaced notes",
	})
	runPCSuccess(t, homeDir, "edit", recordID, editDir)

	// Verify HTML content changed
	var htmlContent string
	if err := db.QueryRow("SELECT html_content FROM records WHERE id = ?", recordID).Scan(&htmlContent); err != nil {
		t.Fatalf("query html_content: %v", err)
	}
	if htmlContent != "<html><body>Replaced</body></html>" {
		t.Fatalf("expected replaced HTML, got %q", htmlContent)
	}

	// Verify notes changed
	var notes sql.NullString
	if err := db.QueryRow("SELECT notes FROM records WHERE id = ?", recordID).Scan(&notes); err != nil {
		t.Fatalf("query notes: %v", err)
	}
	if !notes.Valid || notes.String != "replaced notes" {
		t.Fatalf("expected replaced notes, got %q", notes.String)
	}

	// Verify immutable fields unchanged
	var dateAfter, dayOrderAfter string
	if err := db.QueryRow("SELECT date, day_order FROM records WHERE id = ?", recordID).Scan(&dateAfter, &dayOrderAfter); err != nil {
		t.Fatalf("query after edit: %v", err)
	}
	if dateAfter != dateBefore {
		t.Fatalf("date changed: %q -> %q", dateBefore, dateAfter)
	}
	if dayOrderAfter != dayOrderBefore {
		t.Fatalf("day_order changed: %q -> %q", dayOrderBefore, dayOrderAfter)
	}
}

func TestEditReplacesNotes(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add record with notes
	addDir := createInputFolder(t, inputFolderOpts{
		Notes: "initial notes",
	})
	stdout := runPCSuccess(t, homeDir, "add", addDir)
	recordID := strings.TrimSpace(stdout)

	// Edit with different notes
	editDir := createInputFolder(t, inputFolderOpts{
		Notes: "updated notes",
	})
	runPCSuccess(t, homeDir, "edit", recordID, editDir)

	db := openTestDB(t, homeDir)
	var notes sql.NullString
	if err := db.QueryRow("SELECT notes FROM records WHERE id = ?", recordID).Scan(&notes); err != nil {
		t.Fatalf("query notes: %v", err)
	}
	if !notes.Valid || notes.String != "updated notes" {
		t.Fatalf("expected 'updated notes', got %q", notes.String)
	}

	// Edit with no notes (removes notes)
	editDir2 := createInputFolder(t, inputFolderOpts{})
	runPCSuccess(t, homeDir, "edit", recordID, editDir2)

	if err := db.QueryRow("SELECT notes FROM records WHERE id = ?", recordID).Scan(&notes); err != nil {
		t.Fatalf("query notes after removal: %v", err)
	}
	if notes.Valid {
		t.Fatalf("expected NULL notes after removing notes.md, got %q", notes.String)
	}
}

func TestEditReplacesFigures(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add record with fig1.png
	addDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/fig1.png"></html>`,
		Figures:     map[string][]byte{"fig1.png": []byte("fig1-data")},
	})
	stdout := runPCSuccess(t, homeDir, "add", addDir)
	recordID := strings.TrimSpace(stdout)

	// Verify fig1.png exists on disk
	fig1Path := filepath.Join(homeDir, "personal-context", "figures", recordID, "fig1.png")
	if _, err := os.Stat(fig1Path); err != nil {
		t.Fatalf("fig1.png should exist after add: %v", err)
	}

	// Edit with fig2.png
	editDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/fig2.png"></html>`,
		Figures:     map[string][]byte{"fig2.png": []byte("fig2-data")},
	})
	runPCSuccess(t, homeDir, "edit", recordID, editDir)

	// Verify old figure deleted from disk (best-effort)
	if _, err := os.Stat(fig1Path); !os.IsNotExist(err) {
		t.Fatalf("expected fig1.png to be deleted, but it still exists")
	}

	// Verify new figure exists on disk
	fig2Path := filepath.Join(homeDir, "personal-context", "figures", recordID, "fig2.png")
	content, err := os.ReadFile(fig2Path)
	if err != nil {
		t.Fatalf("read fig2.png: %v", err)
	}
	if string(content) != "fig2-data" {
		t.Fatalf("unexpected fig2 content: %q", string(content))
	}

	// Verify DB has 1 figure row with filename fig2.png
	db := openTestDB(t, homeDir)
	figCount := queryRowCount(t, db, "record_figures")
	if figCount != 1 {
		t.Fatalf("expected 1 figure row, got %d", figCount)
	}
	var filename string
	if err := db.QueryRow("SELECT filename FROM record_figures WHERE record_id = ?", recordID).Scan(&filename); err != nil {
		t.Fatalf("query figure filename: %v", err)
	}
	if filename != "fig2.png" {
		t.Fatalf("expected filename=fig2.png, got %q", filename)
	}
}

func TestEditReplacesDataFiles(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add record with data1.csv
	addDir := createInputFolder(t, inputFolderOpts{
		DataFiles: map[string][]byte{"data1.csv": []byte("a,b\n1,2\n")},
	})
	stdout := runPCSuccess(t, homeDir, "add", addDir)
	recordID := strings.TrimSpace(stdout)

	// Verify data1.csv exists on disk
	data1Path := filepath.Join(homeDir, "personal-context", "data", recordID, "data1.csv")
	if _, err := os.Stat(data1Path); err != nil {
		t.Fatalf("data1.csv should exist after add: %v", err)
	}

	// Edit with data2.csv
	editDir := createInputFolder(t, inputFolderOpts{
		DataFiles: map[string][]byte{"data2.csv": []byte("x,y\n3,4\n")},
	})
	runPCSuccess(t, homeDir, "edit", recordID, editDir)

	// Verify old data file deleted from disk (best-effort)
	if _, err := os.Stat(data1Path); !os.IsNotExist(err) {
		t.Fatalf("expected data1.csv to be deleted, but it still exists")
	}

	// Verify new data file exists on disk
	data2Path := filepath.Join(homeDir, "personal-context", "data", recordID, "data2.csv")
	content, err := os.ReadFile(data2Path)
	if err != nil {
		t.Fatalf("read data2.csv: %v", err)
	}
	if string(content) != "x,y\n3,4\n" {
		t.Fatalf("unexpected data2 content: %q", string(content))
	}

	// Verify DB has 1 data file row with filename data2.csv
	db := openTestDB(t, homeDir)
	dfCount := queryRowCount(t, db, "record_data_files")
	if dfCount != 1 {
		t.Fatalf("expected 1 data file row, got %d", dfCount)
	}
	var filename string
	if err := db.QueryRow("SELECT filename FROM record_data_files WHERE record_id = ?", recordID).Scan(&filename); err != nil {
		t.Fatalf("query data file filename: %v", err)
	}
	if filename != "data2.csv" {
		t.Fatalf("expected filename=data2.csv, got %q", filename)
	}
}

func TestEditSameFilenameReplacementKeepsNewAssets(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	addDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/same.png"></html>`,
		Figures:     map[string][]byte{"same.png": []byte("old-figure")},
		DataFiles:   map[string][]byte{"same.csv": []byte("old,data\n1,2\n")},
	})
	stdout := runPCSuccess(t, homeDir, "add", addDir)
	recordID := strings.TrimSpace(stdout)

	editDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/same.png"></html>`,
		Figures:     map[string][]byte{"same.png": []byte("new-figure")},
		DataFiles:   map[string][]byte{"same.csv": []byte("new,data\n3,4\n")},
	})
	runPCSuccess(t, homeDir, "edit", recordID, editDir)

	figurePath := filepath.Join(homeDir, "personal-context", "figures", recordID, "same.png")
	figureContent, err := os.ReadFile(figurePath)
	if err != nil {
		t.Fatalf("read same.png: %v", err)
	}
	if string(figureContent) != "new-figure" {
		t.Fatalf("expected new figure content, got %q", string(figureContent))
	}

	dataPath := filepath.Join(homeDir, "personal-context", "data", recordID, "same.csv")
	dataContent, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("read same.csv: %v", err)
	}
	if string(dataContent) != "new,data\n3,4\n" {
		t.Fatalf("expected new data content, got %q", string(dataContent))
	}

	db := openTestDB(t, homeDir)
	var figureCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM record_figures WHERE record_id = ?", recordID).Scan(&figureCount); err != nil {
		t.Fatalf("count record_figures: %v", err)
	}
	if figureCount != 1 {
		t.Fatalf("expected 1 figure row, got %d", figureCount)
	}

	var dataCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM record_data_files WHERE record_id = ?", recordID).Scan(&dataCount); err != nil {
		t.Fatalf("count record_data_files: %v", err)
	}
	if dataCount != 1 {
		t.Fatalf("expected 1 data file row, got %d", dataCount)
	}
}

func TestEditFailureDoesNotMutateExistingDataFileOnStageError(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	addDir := createInputFolder(t, inputFolderOpts{
		DataFiles: map[string][]byte{"x.csv": []byte("old,data\n1,2\n")},
	})
	stdout := runPCSuccess(t, homeDir, "add", addDir)
	recordID := strings.TrimSpace(stdout)

	db := openTestDB(t, homeDir)
	var hashBefore string
	if err := db.QueryRow("SELECT hash FROM record_data_files WHERE record_id = ? AND filename = ?", recordID, "x.csv").Scan(&hashBefore); err != nil {
		t.Fatalf("query hash before edit: %v", err)
	}

	dataPath := filepath.Join(homeDir, "personal-context", "data", recordID, "x.csv")
	fileBefore, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("read x.csv before edit: %v", err)
	}

	editDir := createInputFolder(t, inputFolderOpts{
		DataFiles: map[string][]byte{
			"x.csv": []byte("new,data\n3,4\n"),
			"y.csv": []byte("blocked,data\n5,6\n"),
		},
	})
	blockedPath := filepath.Join(editDir, "data", "y.csv")
	if err := os.Chmod(blockedPath, 0o000); err != nil {
		t.Fatalf("chmod y.csv: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blockedPath, 0o644) })

	stderr := runPCFailure(t, homeDir, "edit", recordID, editDir)
	if !strings.Contains(stderr, "stage data file y.csv") {
		t.Fatalf("expected stage error for y.csv, got %q", stderr)
	}

	fileAfter, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("read x.csv after failed edit: %v", err)
	}
	if string(fileAfter) != string(fileBefore) {
		t.Fatalf("expected x.csv content unchanged after failed edit")
	}

	var hashAfter string
	if err := db.QueryRow("SELECT hash FROM record_data_files WHERE record_id = ? AND filename = ?", recordID, "x.csv").Scan(&hashAfter); err != nil {
		t.Fatalf("query hash after edit: %v", err)
	}
	if hashAfter != hashBefore {
		t.Fatalf("expected x.csv hash unchanged after failed edit: before=%s after=%s", hashBefore, hashAfter)
	}
}

func TestEditPreservesImmutableFields(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add record with specific date
	addDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>V1</body></html>",
	})
	stdout := runPCSuccess(t, homeDir, "add", "--date", "2025-06-15", addDir)
	recordID := strings.TrimSpace(stdout)

	db := openTestDB(t, homeDir)
	var dateBefore, dayOrderBefore, createdAtBefore, updatedAtBefore string
	if err := db.QueryRow(
		"SELECT date, day_order, created_at, updated_at FROM records WHERE id = ?", recordID,
	).Scan(&dateBefore, &dayOrderBefore, &createdAtBefore, &updatedAtBefore); err != nil {
		t.Fatalf("query before edit: %v", err)
	}

	// Backdate updated_at so the trigger produces a distinguishable timestamp.
	pastTime := time.Now().UTC().Add(-time.Hour).Format("2006-01-02T15:04:05.000Z")
	if _, err := db.Exec(`UPDATE records SET updated_at = ? WHERE id = ?`, pastTime, recordID); err != nil {
		t.Fatalf("backdate updated_at: %v", err)
	}
	// Re-read baseline after backdating.
	if err := db.QueryRow(
		"SELECT date, day_order, created_at, updated_at FROM records WHERE id = ?", recordID,
	).Scan(&dateBefore, &dayOrderBefore, &createdAtBefore, &updatedAtBefore); err != nil {
		t.Fatalf("query after backdate: %v", err)
	}

	// Edit with different content
	editDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>V2</body></html>",
	})
	runPCSuccess(t, homeDir, "edit", recordID, editDir)

	var dateAfter, dayOrderAfter, createdAtAfter, updatedAtAfter string
	if err := db.QueryRow(
		"SELECT date, day_order, created_at, updated_at FROM records WHERE id = ?", recordID,
	).Scan(&dateAfter, &dayOrderAfter, &createdAtAfter, &updatedAtAfter); err != nil {
		t.Fatalf("query after edit: %v", err)
	}

	// date must not change
	if dateAfter != dateBefore {
		t.Fatalf("date changed: %q -> %q", dateBefore, dateAfter)
	}

	// day_order must not change
	if dayOrderAfter != dayOrderBefore {
		t.Fatalf("day_order changed: %q -> %q", dayOrderBefore, dayOrderAfter)
	}

	// created_at must not change
	if createdAtAfter != createdAtBefore {
		t.Fatalf("created_at changed: %q -> %q", createdAtBefore, createdAtAfter)
	}

	// updated_at must change (trigger fires on content change)
	if updatedAtAfter <= updatedAtBefore {
		t.Fatalf("expected updated_at to advance: before=%q after=%q", updatedAtBefore, updatedAtAfter)
	}
}

func TestEditNonexistentID(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	editDir := createInputFolder(t, inputFolderOpts{})
	stderr := runPCFailure(t, homeDir, "edit", "nonexistent-id", editDir)
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("expected 'not found' in stderr, got %q", stderr)
	}
}

func TestEditUpdatesMetadata(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add record with metadata
	addDir := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"proj/original","git_remote_url":"https://github.com/org/repo1","git_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
	})
	stdout := runPCSuccess(t, homeDir, "add", addDir)
	recordID := strings.TrimSpace(stdout)

	// Edit with different metadata
	editDir := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"proj/updated","git_remote_url":"https://github.com/org/repo2","git_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`,
	})
	runPCSuccess(t, homeDir, "edit", recordID, editDir)

	db := openTestDB(t, homeDir)
	var projectID, gitRemoteURL, gitHash string
	if err := db.QueryRow(
		"SELECT project_id, git_remote_url, git_hash FROM records WHERE id = ?", recordID,
	).Scan(&projectID, &gitRemoteURL, &gitHash); err != nil {
		t.Fatalf("query metadata: %v", err)
	}
	if projectID != "proj/updated" {
		t.Fatalf("expected project_id=proj/updated, got %q", projectID)
	}
	if gitRemoteURL != "https://github.com/org/repo2" {
		t.Fatalf("expected git_remote_url repo2, got %q", gitRemoteURL)
	}
	if gitHash != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("expected git_hash bbbb..., got %q", gitHash)
	}
}

func TestEditPreservesDeletedAtForSoftDeletedRecord(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	addDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", addDir)
	recordID := strings.TrimSpace(stdout)

	runPCSuccess(t, homeDir, "delete", recordID)

	db := openTestDB(t, homeDir)
	before := queryDeletedAt(t, db, recordID)
	if !before.Valid {
		t.Fatal("expected deleted_at to be set before edit")
	}

	editDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>edited while deleted</body></html>",
	})
	runPCSuccess(t, homeDir, "edit", recordID, editDir)

	after := queryDeletedAt(t, db, recordID)
	if !after.Valid {
		t.Fatal("expected deleted_at to remain set after edit")
	}
}
