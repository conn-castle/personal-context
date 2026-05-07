package e2e_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	largeHTMLBytes  = 100 * 1024
	largeNotesBytes = 50 * 1024
)

type slideDetailsJSON struct {
	ID           string              `json:"id"`
	Date         string              `json:"date"`
	DayOrder     string              `json:"day_order"`
	HTMLContent  string              `json:"html_content"`
	Notes        *string             `json:"notes"`
	ProjectID    *string             `json:"project_id"`
	GitRemoteURL *string             `json:"git_remote_url"`
	GitHash      *string             `json:"git_hash"`
	CreatedAt    string              `json:"created_at"`
	UpdatedAt    string              `json:"updated_at"`
	DeletedAt    *string             `json:"deleted_at"`
	Figures      []slideFigureJSON   `json:"figures"`
	DataFiles    []slideDataFileJSON `json:"data_files"`
}

type slideFigureJSON struct {
	Filename string  `json:"filename"`
	S3Key    string  `json:"s3_key"`
	AltText  *string `json:"alt_text"`
}

type slideDataFileJSON struct {
	Filename    string  `json:"filename"`
	S3Key       string  `json:"s3_key"`
	Size        int64   `json:"size"`
	Hash        string  `json:"hash"`
	Description *string `json:"description"`
}

type slideExportJSON struct {
	FormatVersion  int                 `json:"format_version"`
	ID             string              `json:"id"`
	Date           string              `json:"date"`
	DayOrder       string              `json:"day_order"`
	ProjectID      *string             `json:"project_id,omitempty"`
	SourceDeviceID string              `json:"source_device_id"`
	GitRemoteURL   *string             `json:"git_remote_url,omitempty"`
	GitHash        *string             `json:"git_hash,omitempty"`
	HasNotes       bool                `json:"has_notes"`
	Figures        []slideFigureJSON   `json:"figures"`
	DataFiles      []slideDataFileJSON `json:"data_files"`
	CreatedAt      string              `json:"created_at"`
	UpdatedAt      string              `json:"updated_at"`
}

type manualExportSlide struct {
	Metadata slideExportJSON
	HTML     string
	Notes    *string
	Figures  map[string][]byte
}

func TestExportWritesDeterministicGitTreeAndSkipsDeletedSlides(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	minimalID := strings.TrimSpace(runPCSuccess(t, homeDir, "add", createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body><p>Minimal export slide</p></body></html>",
	})))

	largeHTML := buildLargeHTML()
	largeNotes := buildLargeNotes()
	specialFigures := map[string][]byte{
		"my figure 01.png":  []byte("figure-with-space"),
		"data-chart_v2.png": []byte("figure-with-hyphen"),
	}
	for i := 3; i <= 20; i++ {
		name := fmt.Sprintf("fig-%02d.png", i)
		specialFigures[name] = []byte(strings.Repeat(fmt.Sprintf("f%d", i), 32))
	}
	specialData := map[string][]byte{
		"results (final).csv": []byte("metric,value\naccuracy,0.99\n"),
	}
	largeMetadata := `{"project_id":"phase7/export","git_remote_url":"https://github.com/org/repo","git_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	largeID := strings.TrimSpace(runPCSuccess(t, homeDir, "add", createInputFolder(t, inputFolderOpts{
		HTMLContent:  largeHTML,
		Notes:        largeNotes,
		MetadataJSON: largeMetadata,
		Figures:      specialFigures,
		DataFiles:    specialData,
	})))

	sameDate := "2020-05-01"
	sameDateA := strings.TrimSpace(runPCSuccess(t, homeDir, "add", "--date", sameDate, createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>same-date-a</body></html>",
	})))
	sameDateB := strings.TrimSpace(runPCSuccess(t, homeDir, "add", "--date", sameDate, createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>same-date-b</body></html>",
	})))

	deletedID := strings.TrimSpace(runPCSuccess(t, homeDir, "add", createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>deleted export slide</body></html>",
	})))
	runPCSuccess(t, homeDir, "delete", deletedID)

	firstExportDir := t.TempDir()
	runPCSuccess(t, homeDir, "export", "--path", firstExportDir)

	assertTemplateExports(t, firstExportDir)

	minimalMetadata := readSlideExportMetadata(t, firstExportDir, minimalID)
	if minimalMetadata.FormatVersion != 1 {
		t.Fatalf("minimal export format_version = %d, want 1", minimalMetadata.FormatVersion)
	}
	if minimalMetadata.HasNotes {
		t.Fatal("minimal slide unexpectedly exported with notes")
	}
	if len(minimalMetadata.Figures) != 0 {
		t.Fatalf("minimal slide exported %d figures, want 0", len(minimalMetadata.Figures))
	}
	if len(minimalMetadata.DataFiles) != 0 {
		t.Fatalf("minimal slide exported %d data files, want 0", len(minimalMetadata.DataFiles))
	}
	if _, err := os.Stat(filepath.Join(firstExportDir, "slides", minimalID, "notes.md")); !os.IsNotExist(err) {
		t.Fatalf("minimal slide notes.md should be absent, stat err = %v", err)
	}

	largeMetadataOut := readSlideExportMetadata(t, firstExportDir, largeID)
	if !largeMetadataOut.HasNotes {
		t.Fatal("large slide notes were not exported")
	}
	if largeMetadataOut.ProjectID == nil || *largeMetadataOut.ProjectID != "phase7/export" {
		t.Fatalf("large slide project_id = %#v, want phase7/export", largeMetadataOut.ProjectID)
	}
	if largeMetadataOut.GitRemoteURL == nil || *largeMetadataOut.GitRemoteURL != "https://github.com/org/repo" {
		t.Fatalf("large slide git_remote_url = %#v", largeMetadataOut.GitRemoteURL)
	}
	if largeMetadataOut.GitHash == nil || *largeMetadataOut.GitHash != strings.Repeat("a", 40) {
		t.Fatalf("large slide git_hash = %#v", largeMetadataOut.GitHash)
	}
	if len(largeMetadataOut.Figures) != 20 {
		t.Fatalf("large slide exported %d figures, want 20", len(largeMetadataOut.Figures))
	}
	if len(largeMetadataOut.DataFiles) != 1 {
		t.Fatalf("large slide exported %d data file refs, want 1", len(largeMetadataOut.DataFiles))
	}
	exportedHTML, err := os.ReadFile(filepath.Join(firstExportDir, "slides", largeID, "slide.html"))
	if err != nil {
		t.Fatalf("read exported slide.html: %v", err)
	}
	if len(exportedHTML) < largeHTMLBytes {
		t.Fatalf("exported slide.html length = %d, want at least %d", len(exportedHTML), largeHTMLBytes)
	}
	exportedNotes, err := os.ReadFile(filepath.Join(firstExportDir, "slides", largeID, "notes.md"))
	if err != nil {
		t.Fatalf("read exported notes.md: %v", err)
	}
	if len(exportedNotes) < largeNotesBytes {
		t.Fatalf("exported notes length = %d, want at least %d", len(exportedNotes), largeNotesBytes)
	}
	for name, expected := range specialFigures {
		got, err := os.ReadFile(filepath.Join(firstExportDir, "slides", largeID, "figures", name))
		if err != nil {
			t.Fatalf("read exported figure %q: %v", name, err)
		}
		if string(got) != string(expected) {
			t.Fatalf("figure %q content mismatch", name)
		}
	}
	if _, err := os.Stat(filepath.Join(firstExportDir, "slides", largeID, "data")); !os.IsNotExist(err) {
		t.Fatalf("export should not write data binaries into git tree, stat err = %v", err)
	}

	if _, err := os.Stat(filepath.Join(firstExportDir, "slides", deletedID)); !os.IsNotExist(err) {
		t.Fatalf("soft-deleted slide %s should be excluded from export", deletedID)
	}

	sameDateAMetadata := readSlideExportMetadata(t, firstExportDir, sameDateA)
	sameDateBMetadata := readSlideExportMetadata(t, firstExportDir, sameDateB)
	if sameDateAMetadata.Date != sameDate || sameDateBMetadata.Date != sameDate {
		t.Fatalf("same-date export dates = %q and %q, want both %q", sameDateAMetadata.Date, sameDateBMetadata.Date, sameDate)
	}
	if sameDateAMetadata.DayOrder == sameDateBMetadata.DayOrder {
		t.Fatalf("same-date slides exported identical day_order %q", sameDateAMetadata.DayOrder)
	}
	if strings.HasPrefix(sameDateAMetadata.CreatedAt, sameDate) || strings.HasPrefix(sameDateBMetadata.CreatedAt, sameDate) {
		t.Fatalf("created_at should remain distinct from logical slide date, got %q and %q", sameDateAMetadata.CreatedAt, sameDateBMetadata.CreatedAt)
	}

	secondExportDir := t.TempDir()
	runPCSuccess(t, homeDir, "export", "--path", secondExportDir)
	assertDirectorySnapshotEqual(t, firstExportDir, secondExportDir)
}

func TestExportHandlesEmptyDatabase(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	exportDir := t.TempDir()
	runPCSuccess(t, homeDir, "export", "--path", exportDir)

	assertTemplateExports(t, exportDir)

	slideEntries, err := os.ReadDir(filepath.Join(exportDir, "slides"))
	if err != nil {
		t.Fatalf("read exported slides dir: %v", err)
	}
	if len(slideEntries) != 0 {
		names := make([]string, 0, len(slideEntries))
		for _, entry := range slideEntries {
			names = append(names, entry.Name())
		}
		t.Fatalf("empty export wrote unexpected slide directories: %v", names)
	}
}

func TestImportUsesUpdatedAtMergeRulesAndReplacesChildRows(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	sameID := strings.TrimSpace(runPCSuccess(t, homeDir, "add", createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>same-updated-at local</body></html>",
		Notes:       "same-local-notes",
		Figures:     map[string][]byte{"same.png": []byte("same-local-figure")},
		DataFiles:   map[string][]byte{"same.csv": []byte("a,b\n1,2\n")},
	})))
	olderID := strings.TrimSpace(runPCSuccess(t, homeDir, "add", createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>older local</body></html>",
		Notes:       "older-local-notes",
		Figures:     map[string][]byte{"older.png": []byte("older-local-figure")},
		DataFiles:   map[string][]byte{"older.csv": []byte("x,y\n3,4\n")},
	})))
	newerID := strings.TrimSpace(runPCSuccess(t, homeDir, "add", createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>newer local</body></html>",
		Notes:        "newer-local-notes",
		MetadataJSON: `{"project_id":"phase7/local-before","git_remote_url":"https://github.com/org/before","git_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`,
		Figures:      map[string][]byte{"old-figure.png": []byte("old-figure-bytes")},
		DataFiles:    map[string][]byte{"old-data.csv": []byte("before,data\n1,1\n")},
	})))

	sameSlideBefore := getLocalSlideJSON(t, homeDir, sameID)
	olderSlideBefore := getLocalSlideJSON(t, homeDir, olderID)
	newerSlideBefore := getLocalSlideJSON(t, homeDir, newerID)

	exportDir := t.TempDir()
	writeSeededTemplates(t, openTestDB(t, homeDir), exportDir)

	olderTime := mustParseTimestamp(t, olderSlideBefore.UpdatedAt).Add(-1 * time.Minute).Format(time.RFC3339)
	newerTime := mustParseTimestamp(t, newerSlideBefore.UpdatedAt).Add(1 * time.Minute).Format(time.RFC3339)

	writeManualExportSlide(t, exportDir, manualExportSlide{
		Metadata: slideExportJSON{
			FormatVersion: 1,
			ID:            sameID,
			Date:          sameSlideBefore.Date,
			DayOrder:      sameSlideBefore.DayOrder,
			HasNotes:      true,
			Figures: []slideFigureJSON{{
				Filename: "same-from-import.png",
				S3Key:    fmt.Sprintf("figures/%s/%s", sameID, "same-from-import.png"),
			}},
			DataFiles: []slideDataFileJSON{{
				Filename: "same-from-import.csv",
				S3Key:    fmt.Sprintf("data/%s/%s", sameID, "same-from-import.csv"),
				Size:     19,
				Hash:     hashString("same-from-import"),
			}},
			CreatedAt: sameSlideBefore.CreatedAt,
			UpdatedAt: sameSlideBefore.UpdatedAt,
		},
		HTML:  "<html><body>same-updated-at import</body></html>",
		Notes: strPtr("same-import-notes"),
		Figures: map[string][]byte{
			"same-from-import.png": []byte("same-import-figure"),
		},
	})
	writeManualExportSlide(t, exportDir, manualExportSlide{
		Metadata: slideExportJSON{
			FormatVersion: 1,
			ID:            olderID,
			Date:          olderSlideBefore.Date,
			DayOrder:      olderSlideBefore.DayOrder,
			HasNotes:      true,
			Figures: []slideFigureJSON{{
				Filename: "older-from-import.png",
				S3Key:    fmt.Sprintf("figures/%s/%s", olderID, "older-from-import.png"),
			}},
			DataFiles: []slideDataFileJSON{{
				Filename: "older-from-import.csv",
				S3Key:    fmt.Sprintf("data/%s/%s", olderID, "older-from-import.csv"),
				Size:     20,
				Hash:     hashString("older-from-import"),
			}},
			CreatedAt: olderSlideBefore.CreatedAt,
			UpdatedAt: olderTime,
		},
		HTML:  "<html><body>older import</body></html>",
		Notes: strPtr("older-import-notes"),
		Figures: map[string][]byte{
			"older-from-import.png": []byte("older-import-figure"),
		},
	})
	writeManualExportSlide(t, exportDir, manualExportSlide{
		Metadata: slideExportJSON{
			FormatVersion: 1,
			ID:            newerID,
			Date:          newerSlideBefore.Date,
			DayOrder:      newerSlideBefore.DayOrder,
			ProjectID:     strPtr("phase7/local-after"),
			GitRemoteURL:  strPtr("https://github.com/org/after"),
			GitHash:       strPtr("cccccccccccccccccccccccccccccccccccccccc"),
			HasNotes:      true,
			Figures: []slideFigureJSON{{
				Filename: "fresh.png",
				S3Key:    fmt.Sprintf("figures/%s/%s", newerID, "fresh.png"),
			}},
			DataFiles: []slideDataFileJSON{{
				Filename: "fresh.csv",
				S3Key:    fmt.Sprintf("data/%s/%s", newerID, "fresh.csv"),
				Size:     15,
				Hash:     hashString("fresh,data\n"),
			}},
			CreatedAt: newerSlideBefore.CreatedAt,
			UpdatedAt: newerTime,
		},
		HTML:  "<html><body>newer import</body></html>",
		Notes: strPtr("newer-import-notes"),
		Figures: map[string][]byte{
			"fresh.png": []byte("fresh-figure-bytes"),
		},
	})

	runPCSuccess(t, homeDir, "import", exportDir)

	sameSlideAfter := getLocalSlideJSON(t, homeDir, sameID)
	if sameSlideAfter.HTMLContent != sameSlideBefore.HTMLContent {
		t.Fatalf("same-updated-at slide should be unchanged, got %q", sameSlideAfter.HTMLContent)
	}
	if sameSlideAfter.Notes == nil || *sameSlideAfter.Notes != "same-local-notes" {
		t.Fatalf("same-updated-at notes = %#v, want local value", sameSlideAfter.Notes)
	}
	if fileNamesFromFigures(sameSlideAfter.Figures)[0] != "same.png" {
		t.Fatalf("same-updated-at figures = %v, want original", fileNamesFromFigures(sameSlideAfter.Figures))
	}

	olderSlideAfter := getLocalSlideJSON(t, homeDir, olderID)
	if olderSlideAfter.HTMLContent != olderSlideBefore.HTMLContent {
		t.Fatalf("older import should be skipped, got %q", olderSlideAfter.HTMLContent)
	}
	if olderSlideAfter.Notes == nil || *olderSlideAfter.Notes != "older-local-notes" {
		t.Fatalf("older import notes = %#v, want local value", olderSlideAfter.Notes)
	}
	if fileNamesFromFigures(olderSlideAfter.Figures)[0] != "older.png" {
		t.Fatalf("older import figures = %v, want original", fileNamesFromFigures(olderSlideAfter.Figures))
	}

	newerSlideAfter := getLocalSlideJSON(t, homeDir, newerID)
	if newerSlideAfter.HTMLContent != "<html><body>newer import</body></html>" {
		t.Fatalf("newer import html_content = %q", newerSlideAfter.HTMLContent)
	}
	if newerSlideAfter.Notes == nil || *newerSlideAfter.Notes != "newer-import-notes" {
		t.Fatalf("newer import notes = %#v", newerSlideAfter.Notes)
	}
	if newerSlideAfter.ProjectID == nil || *newerSlideAfter.ProjectID != "phase7/local-after" {
		t.Fatalf("newer import project_id = %#v", newerSlideAfter.ProjectID)
	}
	if newerSlideAfter.GitRemoteURL == nil || *newerSlideAfter.GitRemoteURL != "https://github.com/org/after" {
		t.Fatalf("newer import git_remote_url = %#v", newerSlideAfter.GitRemoteURL)
	}
	if newerSlideAfter.GitHash == nil || *newerSlideAfter.GitHash != strings.Repeat("c", 40) {
		t.Fatalf("newer import git_hash = %#v", newerSlideAfter.GitHash)
	}
	if got := fileNamesFromFigures(newerSlideAfter.Figures); !slices.Equal(got, []string{"fresh.png"}) {
		t.Fatalf("newer import figures = %v, want [fresh.png]", got)
	}
	if got := fileNamesFromDataFiles(newerSlideAfter.DataFiles); !slices.Equal(got, []string{"fresh.csv"}) {
		t.Fatalf("newer import data_files = %v, want [fresh.csv]", got)
	}
	freshFigurePath := filepath.Join(homeDir, "personal-context", "figures", newerID, "fresh.png")
	if got, err := os.ReadFile(freshFigurePath); err != nil {
		t.Fatalf("read new figure: %v", err)
	} else if string(got) != "fresh-figure-bytes" {
		t.Fatalf("new figure content = %q", string(got))
	}
	if _, err := os.Stat(filepath.Join(homeDir, "personal-context", "figures", newerID, "old-figure.png")); !os.IsNotExist(err) {
		t.Fatalf("old figure should be removed after newer import, stat err = %v", err)
	}
}

func TestImportRejectsGitLFSPointerFiles(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	exportDir := t.TempDir()
	writeSeededTemplates(t, openTestDB(t, homeDir), exportDir)

	writeManualExportSlide(t, exportDir, manualExportSlide{
		Metadata: slideExportJSON{
			FormatVersion: 1,
			ID:            "20200501-deadbeef",
			Date:          "2020-05-01",
			DayOrder:      "a",
			HasNotes:      false,
			Figures: []slideFigureJSON{{
				Filename: "pointer.png",
				S3Key:    "figures/20200501-deadbeef/pointer.png",
			}},
			DataFiles: nil,
			CreatedAt: "2020-05-01T12:00:00Z",
			UpdatedAt: "2020-05-01T12:00:00Z",
		},
		HTML: "<html><body><img src=\"figures/pointer.png\"></body></html>",
		Figures: map[string][]byte{
			"pointer.png": []byte("version https://git-lfs.github.com/spec/v1\noid sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\nsize 42\n"),
		},
	})

	stderr := runPCFailure(t, homeDir, "import", exportDir)
	if !strings.Contains(strings.ToLower(stderr), "git lfs") {
		t.Fatalf("expected git lfs pointer error, got %q", stderr)
	}

	db := openTestDB(t, homeDir)
	if got := queryRowCount(t, db, "slides"); got != 0 {
		t.Fatalf("LFS pointer import should not create slides, found %d rows", got)
	}
}

func TestRestoreDBCreatesRecoverableBackupBeforeReplacingLocalState(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	originalID := strings.TrimSpace(runPCSuccess(t, homeDir, "add", createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>original restore-db slide</body></html>",
		Notes:       "original-restore-notes",
	})))

	exportDir := t.TempDir()
	writeSeededTemplates(t, openTestDB(t, homeDir), exportDir)
	writeManualExportSlide(t, exportDir, manualExportSlide{
		Metadata: slideExportJSON{
			FormatVersion: 1,
			ID:            "20200502-feedface",
			Date:          "2020-05-02",
			DayOrder:      "a",
			HasNotes:      true,
			Figures:       nil,
			DataFiles:     nil,
			CreatedAt:     "2020-05-02T12:00:00Z",
			UpdatedAt:     "2020-05-02T12:00:00Z",
		},
		HTML:  "<html><body>replacement restore-db slide</body></html>",
		Notes: strPtr("replacement-restore-notes"),
	})

	pcDir := filepath.Join(homeDir, "personal-context", ".pc")
	stdout := runPCSuccess(t, homeDir, "restore-db", exportDir)

	stderr := runPCFailure(t, homeDir, "show", originalID)
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("original slide should be absent after restore-db, got %q", stderr)
	}
	replacement := getLocalSlideJSON(t, homeDir, "20200502-feedface")
	if replacement.HTMLContent != "<html><body>replacement restore-db slide</body></html>" {
		t.Fatalf("replacement slide html_content = %q", replacement.HTMLContent)
	}

	backupPath := parseReportedBackupPath(t, stdout)
	if !strings.HasPrefix(backupPath, filepath.Join(pcDir, "backups")) {
		t.Fatalf("backup path = %q, want it under %s", backupPath, filepath.Join(pcDir, "backups"))
	}
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("stat backup snapshot %s: %v", backupPath, err)
	}
	if info.IsDir() {
		runPCSuccess(t, homeDir, "restore-db", backupPath)
	} else {
		backupBytes, err := os.ReadFile(backupPath)
		if err != nil {
			t.Fatalf("read backup database %s: %v", backupPath, err)
		}
		currentDBPath := filepath.Join(pcDir, "pc.db")
		if err := os.WriteFile(currentDBPath, backupBytes, 0o600); err != nil {
			t.Fatalf("restore backup database to current pc.db: %v", err)
		}
	}

	restoredOriginal := getLocalSlideJSON(t, homeDir, originalID)
	if restoredOriginal.HTMLContent != "<html><body>original restore-db slide</body></html>" {
		t.Fatalf("restored backup html_content = %q", restoredOriginal.HTMLContent)
	}
	stderr = runPCFailure(t, homeDir, "show", "20200502-feedface")
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("replacement slide should be absent after backup restore, got %q", stderr)
	}
}

func TestVerifyRoundTripLocal(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	runPCSuccess(t, homeDir, "add", createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>verify local</body></html>",
		Notes:        "verify-local-notes",
		MetadataJSON: `{"project_id":"phase7/verify-local"}`,
		Figures: map[string][]byte{
			"verify.png": []byte("verify-figure"),
		},
		DataFiles: map[string][]byte{
			"verify.csv": []byte("metric,value\nloss,0.1\n"),
		},
	}))

	runPCSuccess(t, homeDir, "verify")
}

func getLocalSlideJSON(t *testing.T, homeDir string, slideID string) slideDetailsJSON {
	t.Helper()

	stdout := runPCSuccess(t, homeDir, "show", "--format", "json", slideID)
	var result slideDetailsJSON
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse show json for %s: %v\nraw: %s", slideID, err, stdout)
	}
	return result
}

func readSlideExportMetadata(t *testing.T, exportDir string, slideID string) slideExportJSON {
	t.Helper()

	path := filepath.Join(exportDir, "slides", slideID, "metadata.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read metadata %s: %v", path, err)
	}

	var result slideExportJSON
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse metadata %s: %v\nraw: %s", path, err, string(raw))
	}
	return result
}

func assertTemplateExports(t *testing.T, exportDir string) {
	t.Helper()

	for _, name := range []string{"text-only.html", "single-image.html"} {
		path := filepath.Join(exportDir, "templates", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read exported template %s: %v", name, err)
		}
		if len(raw) == 0 {
			t.Fatalf("exported template %s is empty", name)
		}
	}
}

func assertDirectorySnapshotEqual(t *testing.T, left string, right string) {
	t.Helper()

	leftSnapshot := snapshotDirectory(t, left)
	rightSnapshot := snapshotDirectory(t, right)
	if len(leftSnapshot) != len(rightSnapshot) {
		t.Fatalf("snapshot file count mismatch: %d != %d\nleft=%v\nright=%v", len(leftSnapshot), len(rightSnapshot), leftSnapshot, rightSnapshot)
	}

	for path, leftDigest := range leftSnapshot {
		rightDigest, ok := rightSnapshot[path]
		if !ok {
			t.Fatalf("snapshot missing path %s in second export", path)
		}
		if leftDigest != rightDigest {
			t.Fatalf("snapshot mismatch for %s: %s != %s", path, leftDigest, rightDigest)
		}
	}
}

func snapshotDirectory(t *testing.T, root string) map[string]string {
	t.Helper()

	snapshot := make(map[string]string)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		snapshot[filepath.ToSlash(rel)] = fmt.Sprintf("%d:%s", len(raw), hex.EncodeToString(sum[:]))
		return nil
	}); err != nil {
		t.Fatalf("snapshot directory %s: %v", root, err)
	}
	return snapshot
}

func buildLargeHTML() string {
	var b strings.Builder
	b.WriteString("<html><body><h1>Large export slide</h1>\n")
	b.WriteString("<p>Unicode payload: 日本語, café, naïve, 🚀.</p>\n")
	b.WriteString(`<img src="figures/my figure 01.png">` + "\n")
	b.WriteString(`<img src="figures/data-chart_v2.png">` + "\n")
	for i := 3; i <= 20; i++ {
		_, _ = fmt.Fprintf(&b, `<img src="figures/fig-%02d.png">`+"\n", i)
	}
	paragraph := "<p>Large export payload repeated to exceed the roadmap HTML threshold without changing semantics.</p>\n"
	for b.Len() < largeHTMLBytes {
		b.WriteString(paragraph)
	}
	b.WriteString("</body></html>\n")
	return b.String()
}

func buildLargeNotes() string {
	line := "Large notes payload repeated to exceed the roadmap note threshold while preserving round-trip readability.\n"
	var b strings.Builder
	for b.Len() < largeNotesBytes {
		b.WriteString(line)
	}
	return b.String()
}

func writeSeededTemplates(t *testing.T, db *sql.DB, exportDir string) {
	t.Helper()

	templateDir := filepath.Join(exportDir, "templates")
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}

	rows, err := db.Query("SELECT name, html_content FROM templates ORDER BY name")
	if err != nil {
		t.Fatalf("query templates: %v", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Fatalf("close template rows: %v", err)
		}
	}()

	var wrote int
	for rows.Next() {
		var name string
		var html string
		if err := rows.Scan(&name, &html); err != nil {
			t.Fatalf("scan template: %v", err)
		}
		if err := os.WriteFile(filepath.Join(templateDir, name+".html"), []byte(html), 0o644); err != nil {
			t.Fatalf("write template %s: %v", name, err)
		}
		wrote++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate templates: %v", err)
	}
	if wrote == 0 {
		t.Fatal("expected seeded templates to exist")
	}

	slidesDir := filepath.Join(exportDir, "slides")
	if err := os.MkdirAll(slidesDir, 0o755); err != nil {
		t.Fatalf("mkdir slides: %v", err)
	}
	ensureManualRegistryEntry(t, exportDir, "projects.json", "test/default-project")
	ensureManualRegistryEntry(t, exportDir, "devices.json", "test-device")
}

func writeManualExportSlide(t *testing.T, exportDir string, slide manualExportSlide) {
	t.Helper()

	slideDir := filepath.Join(exportDir, "slides", slide.Metadata.ID)
	if err := os.MkdirAll(filepath.Join(slideDir, "figures"), 0o755); err != nil {
		t.Fatalf("mkdir slide export dir: %v", err)
	}
	if slide.Metadata.ProjectID == nil || *slide.Metadata.ProjectID == "" {
		defaultProject := "test/default-project"
		slide.Metadata.ProjectID = &defaultProject
	}
	if slide.Metadata.SourceDeviceID == "" {
		slide.Metadata.SourceDeviceID = "test-device"
	}
	ensureManualRegistryEntry(t, exportDir, "projects.json", *slide.Metadata.ProjectID)
	ensureManualRegistryEntry(t, exportDir, "devices.json", slide.Metadata.SourceDeviceID)
	metadataBytes, err := json.MarshalIndent(slide.Metadata, "", "  ")
	if err != nil {
		t.Fatalf("marshal metadata for %s: %v", slide.Metadata.ID, err)
	}
	if err := os.WriteFile(filepath.Join(slideDir, "metadata.json"), metadataBytes, 0o644); err != nil {
		t.Fatalf("write metadata for %s: %v", slide.Metadata.ID, err)
	}
	if err := os.WriteFile(filepath.Join(slideDir, "slide.html"), []byte(slide.HTML), 0o644); err != nil {
		t.Fatalf("write slide.html for %s: %v", slide.Metadata.ID, err)
	}
	if slide.Notes != nil {
		if err := os.WriteFile(filepath.Join(slideDir, "notes.md"), []byte(*slide.Notes), 0o644); err != nil {
			t.Fatalf("write notes.md for %s: %v", slide.Metadata.ID, err)
		}
	}
	for name, content := range slide.Figures {
		if err := os.WriteFile(filepath.Join(slideDir, "figures", name), content, 0o644); err != nil {
			t.Fatalf("write figure %s for %s: %v", name, slide.Metadata.ID, err)
		}
	}
}

func ensureManualRegistryEntry(t *testing.T, exportDir string, filename string, id string) {
	t.Helper()
	if strings.TrimSpace(id) == "" {
		return
	}
	path := filepath.Join(exportDir, filename)
	type registryEntry struct {
		ID         string  `json:"id"`
		CreatedAt  string  `json:"created_at"`
		UpdatedAt  string  `json:"updated_at"`
		ArchivedAt *string `json:"archived_at,omitempty"`
	}
	entries := make([]registryEntry, 0)
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &entries); err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}
	}
	for _, entry := range entries {
		if entry.ID == id {
			return
		}
	}
	entries = append(entries, registryEntry{
		ID:        id,
		CreatedAt: "2026-03-09T12:00:00Z",
		UpdatedAt: "2026-03-09T12:00:00Z",
	})
	slices.SortFunc(entries, func(a, b registryEntry) int {
		return strings.Compare(a.ID, b.ID)
	})
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", filename, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
}

func mustParseTimestamp(t *testing.T, raw string) time.Time {
	t.Helper()

	layouts := []string{
		"2006-01-02T15:04:05.000Z",
		time.RFC3339,
		time.RFC3339Nano,
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts
		}
	}
	t.Fatalf("parse timestamp %q: unsupported format", raw)
	return time.Time{}
}

func fileNamesFromFigures(figures []slideFigureJSON) []string {
	names := make([]string, 0, len(figures))
	for _, figure := range figures {
		names = append(names, figure.Filename)
	}
	slices.Sort(names)
	return names
}

func fileNamesFromDataFiles(dataFiles []slideDataFileJSON) []string {
	names := make([]string, 0, len(dataFiles))
	for _, dataFile := range dataFiles {
		names = append(names, dataFile.Filename)
	}
	slices.Sort(names)
	return names
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func parseReportedBackupPath(t *testing.T, stdout string) string {
	t.Helper()

	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Backup created at ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Backup created at "))
		}
	}
	t.Fatalf("restore-db output did not report a backup path:\n%s", stdout)
	return ""
}

func strPtr(s string) *string {
	return &s
}
