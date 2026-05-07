package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddMinimalSlide(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")
	runPCSuccess(t, homeDir, "project", "add", "test/default-project")
	runPCSuccess(t, homeDir, "device", "register", "test-device")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)

	slideID := strings.TrimSpace(stdout)
	if len(slideID) == 0 {
		t.Fatal("expected slide ID in stdout")
	}

	// Verify DB
	db := openTestDB(t, homeDir)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM slides WHERE id = ?", slideID).Scan(&count); err != nil {
		t.Fatalf("query slide: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 slide, got %d", count)
	}
}

func TestAddMissingSlideHTML(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	emptyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(emptyDir, "metadata.json"), []byte(`{"project_id":"test/default-project","source_device_id":"test-device"}`), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	stdout := runPCSuccess(t, homeDir, "add", emptyDir)
	if strings.TrimSpace(stdout) == "" {
		t.Fatal("expected slide ID for folder without slide.html")
	}
}

func TestAddWithMetadataJSON(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"test/proj","git_remote_url":"https://github.com/org/repo","git_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	db := openTestDB(t, homeDir)
	var projectID, gitRemoteURL, gitHash string
	if err := db.QueryRow("SELECT project_id, git_remote_url, git_hash FROM slides WHERE id = ?", slideID).Scan(&projectID, &gitRemoteURL, &gitHash); err != nil {
		t.Fatalf("query slide: %v", err)
	}
	if projectID != "test/proj" {
		t.Fatalf("expected project_id=test/proj, got %q", projectID)
	}
	if gitRemoteURL != "https://github.com/org/repo" {
		t.Fatalf("expected git_remote_url, got %q", gitRemoteURL)
	}
	if gitHash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("expected git_hash, got %q", gitHash)
	}
}

func TestAddProjectFlagMustMatchMetadata(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"from-metadata"}`,
	})
	stderr := runPCFailure(t, homeDir, "add", "--project", "from-flag", inputDir)
	if !strings.Contains(stderr, "project_id conflict") {
		t.Fatalf("expected project conflict, got %q", stderr)
	}
}

func TestAddWithDateFlag(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")
	runPCSuccess(t, homeDir, "project", "add", "test/default-project")
	runPCSuccess(t, homeDir, "device", "register", "test-device")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", "--date", "2025-06-15", inputDir)
	slideID := strings.TrimSpace(stdout)

	db := openTestDB(t, homeDir)
	var date string
	if err := db.QueryRow("SELECT date FROM slides WHERE id = ?", slideID).Scan(&date); err != nil {
		t.Fatalf("query slide: %v", err)
	}
	if date != "2025-06-15" {
		t.Fatalf("expected date=2025-06-15, got %q", date)
	}
}

func TestAddWithFigures(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/plot.png"></html>`,
		Figures:     map[string][]byte{"plot.png": []byte("fake-png-data")},
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	// Verify figure file on disk
	figPath := filepath.Join(homeDir, "personal-context", "figures", slideID, "plot.png")
	content, err := os.ReadFile(figPath)
	if err != nil {
		t.Fatalf("read figure file: %v", err)
	}
	if string(content) != "fake-png-data" {
		t.Fatalf("unexpected figure content: %q", string(content))
	}

	// Verify DB figure row
	db := openTestDB(t, homeDir)
	var figCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM slide_figures WHERE slide_id = ?", slideID).Scan(&figCount); err != nil {
		t.Fatalf("query figures: %v", err)
	}
	if figCount != 1 {
		t.Fatalf("expected 1 figure, got %d", figCount)
	}
}

func TestAddWithDataFiles(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		DataFiles: map[string][]byte{"metrics.csv": []byte("col1,col2\n1,2\n")},
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	// Verify data file on disk
	dataPath := filepath.Join(homeDir, "personal-context", "data", slideID, "metrics.csv")
	if _, err := os.Stat(dataPath); err != nil {
		t.Fatalf("data file not found: %v", err)
	}

	// Verify hash in DB
	db := openTestDB(t, homeDir)
	var hash string
	if err := db.QueryRow("SELECT hash FROM slide_data_files WHERE slide_id = ?", slideID).Scan(&hash); err != nil {
		t.Fatalf("query data file hash: %v", err)
	}
	if len(hash) != 64 {
		t.Fatalf("expected 64-char SHA-256 hash, got %q", hash)
	}
}

func TestAddDayOrderIncreases(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	date := "2025-03-05"
	inputDir1 := createInputFolder(t, inputFolderOpts{})
	stdout1 := runPCSuccess(t, homeDir, "add", "--date", date, inputDir1)
	id1 := strings.TrimSpace(stdout1)

	inputDir2 := createInputFolder(t, inputFolderOpts{})
	stdout2 := runPCSuccess(t, homeDir, "add", "--date", date, inputDir2)
	id2 := strings.TrimSpace(stdout2)

	db := openTestDB(t, homeDir)
	var order1, order2 string
	if err := db.QueryRow("SELECT day_order FROM slides WHERE id = ?", id1).Scan(&order1); err != nil {
		t.Fatalf("query day_order 1: %v", err)
	}
	if err := db.QueryRow("SELECT day_order FROM slides WHERE id = ?", id2).Scan(&order2); err != nil {
		t.Fatalf("query day_order 2: %v", err)
	}

	if order2 <= order1 {
		t.Fatalf("expected order2 > order1: %q vs %q", order2, order1)
	}
}

func TestAddPositionFirst(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	date := "2025-03-05"
	inputDir1 := createInputFolder(t, inputFolderOpts{})
	runPCSuccess(t, homeDir, "add", "--date", date, inputDir1)

	inputDir2 := createInputFolder(t, inputFolderOpts{})
	stdout2 := runPCSuccess(t, homeDir, "add", "--date", date, "--first", inputDir2)
	id2 := strings.TrimSpace(stdout2)

	// Verify id2 has the smallest day_order
	db := openTestDB(t, homeDir)
	var firstID string
	if err := db.QueryRow("SELECT id FROM slides WHERE date = ? ORDER BY day_order ASC LIMIT 1", date).Scan(&firstID); err != nil {
		t.Fatalf("query first slide: %v", err)
	}
	if firstID != id2 {
		t.Fatalf("expected slide %s to be first, but %s is first", id2, firstID)
	}
}

func TestAddInvalidDate(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")
	runPCSuccess(t, homeDir, "project", "add", "test/default-project")
	runPCSuccess(t, homeDir, "device", "register", "test-device")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stderr := runPCFailure(t, homeDir, "add", "--date", "not-a-date", inputDir)
	if !strings.Contains(stderr, "invalid date") {
		t.Fatalf("expected invalid date error, got %q", stderr)
	}
}

func TestAddMutuallyExclusivePositionFlags(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stderr := runPCFailure(t, homeDir, "add", "--first", "--last", inputDir)
	if !strings.Contains(stderr, "only one position flag") {
		t.Fatalf("expected position flag error, got %q", stderr)
	}
}
