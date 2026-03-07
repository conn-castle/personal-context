package e2e_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSetStoresInConfig(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	runPCSuccess(t, homeDir, "project", "set", "my-project")

	cfg := readConfigJSON(t, homeDir)
	if cfg.ActiveProject != "my-project" {
		t.Fatalf("expected active_project=my-project, got %q", cfg.ActiveProject)
	}
}

func TestProjectSetOverwritesPrevious(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	runPCSuccess(t, homeDir, "project", "set", "first-project")
	runPCSuccess(t, homeDir, "project", "set", "second-project")

	cfg := readConfigJSON(t, homeDir)
	if cfg.ActiveProject != "second-project" {
		t.Fatalf("expected active_project=second-project, got %q", cfg.ActiveProject)
	}
}

func TestProjectClearRemoves(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	runPCSuccess(t, homeDir, "project", "set", "to-clear")

	cfg := readConfigJSON(t, homeDir)
	if cfg.ActiveProject != "to-clear" {
		t.Fatalf("expected active_project=to-clear, got %q", cfg.ActiveProject)
	}

	runPCSuccess(t, homeDir, "project", "clear")

	cfg = readConfigJSON(t, homeDir)
	if cfg.ActiveProject != "" {
		t.Fatalf("expected active_project to be empty after clear, got %q", cfg.ActiveProject)
	}
}

func TestProjectClearWhenNoneSet(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Clear without setting first should succeed (idempotent)
	stdout := runPCSuccess(t, homeDir, "project", "clear")
	if !strings.Contains(stdout, "Active project cleared.") {
		t.Fatalf("expected clear message, got %q", stdout)
	}
}

func TestProjectListReturnsDistinctFromDB(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add slides with different projects
	inputDir1 := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"alpha"}`,
	})
	runPCSuccess(t, homeDir, "add", inputDir1)

	inputDir2 := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"beta"}`,
	})
	runPCSuccess(t, homeDir, "add", inputDir2)

	// Add a second slide with "alpha" to verify distinct
	inputDir3 := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"alpha"}`,
	})
	runPCSuccess(t, homeDir, "add", inputDir3)

	stdout := runPCSuccess(t, homeDir, "project", "list")
	lines := nonEmptyLines(stdout)

	if len(lines) != 2 {
		t.Fatalf("expected 2 distinct projects, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(stdout, "alpha") {
		t.Fatalf("expected alpha in output, got %q", stdout)
	}
	if !strings.Contains(stdout, "beta") {
		t.Fatalf("expected beta in output, got %q", stdout)
	}
}

func TestProjectListEmpty(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// No slides at all
	stdout := runPCSuccess(t, homeDir, "project", "list")
	if !strings.Contains(stdout, "No projects found.") {
		t.Fatalf("expected 'No projects found.' message, got %q", stdout)
	}
}

func TestProjectListExcludesDeletedSlideProjects(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add a slide with a project, then delete it
	inputDir := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"deleted-proj"}`,
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	runPCSuccess(t, homeDir, "delete", slideID)

	listOut := runPCSuccess(t, homeDir, "project", "list")
	if !strings.Contains(listOut, "No projects found.") {
		t.Fatalf("expected 'No projects found.' after deleting only slide with project, got %q", listOut)
	}
}

func TestProjectActiveUsedByAdd(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Set active project
	runPCSuccess(t, homeDir, "project", "set", "active-proj")

	// Add slide without --project flag
	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	// Verify slide has active project in DB
	db := openTestDB(t, homeDir)
	var projectID sql.NullString
	if err := db.QueryRow("SELECT project_id FROM slides WHERE id = ?", slideID).Scan(&projectID); err != nil {
		t.Fatalf("query project_id: %v", err)
	}
	if !projectID.Valid {
		t.Fatal("expected project_id to be set from active project, got NULL")
	}
	if projectID.String != "active-proj" {
		t.Fatalf("expected project_id=active-proj, got %q", projectID.String)
	}
}

func TestProjectFlagOverridesActive(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Set active project
	runPCSuccess(t, homeDir, "project", "set", "active-proj")

	// Add slide WITH --project flag
	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", "--project", "flag-proj", inputDir)
	slideID := strings.TrimSpace(stdout)

	// Verify flag wins over active project
	db := openTestDB(t, homeDir)
	var projectID string
	if err := db.QueryRow("SELECT project_id FROM slides WHERE id = ?", slideID).Scan(&projectID); err != nil {
		t.Fatalf("query project_id: %v", err)
	}
	if projectID != "flag-proj" {
		t.Fatalf("expected project_id=flag-proj, got %q", projectID)
	}
}

func TestProjectActiveNotUsedWhenNotSet(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// No active project set, add without --project
	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	// Verify project_id is NULL
	db := openTestDB(t, homeDir)
	var projectID sql.NullString
	if err := db.QueryRow("SELECT project_id FROM slides WHERE id = ?", slideID).Scan(&projectID); err != nil {
		t.Fatalf("query project_id: %v", err)
	}
	if projectID.Valid {
		t.Fatalf("expected project_id to be NULL, got %q", projectID.String)
	}
}

func TestProjectSetEmptyString(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	stderr := runPCFailure(t, homeDir, "project", "set", "")
	if !strings.Contains(stderr, "empty") {
		t.Fatalf("expected empty error, got %q", stderr)
	}
}

func TestProjectListMarksActive(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add slides with different projects
	inputDir1 := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"alpha"}`,
	})
	runPCSuccess(t, homeDir, "add", inputDir1)

	inputDir2 := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"beta"}`,
	})
	runPCSuccess(t, homeDir, "add", inputDir2)

	// Set active project
	runPCSuccess(t, homeDir, "project", "set", "alpha")

	stdout := runPCSuccess(t, homeDir, "project", "list")
	if !strings.Contains(stdout, "alpha (active)") {
		t.Fatalf("expected 'alpha (active)' in output, got %q", stdout)
	}
	// beta should NOT have (active)
	for _, line := range nonEmptyLines(stdout) {
		if strings.Contains(line, "beta") && strings.Contains(line, "(active)") {
			t.Fatalf("beta should not be marked active, got %q", line)
		}
	}
}

// configJSON is the struct for reading config.json in tests.
type configJSON struct {
	NeonURL       string `json:"neon_url,omitempty"`
	S3Bucket      string `json:"s3_bucket,omitempty"`
	S3Region      string `json:"s3_region,omitempty"`
	AWSProfile    string `json:"aws_profile,omitempty"`
	ActiveProject string `json:"active_project,omitempty"`
}

// readConfigJSON reads and parses the config.json from the test home directory.
func readConfigJSON(t *testing.T, homeDir string) configJSON {
	t.Helper()
	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var cfg configJSON
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config.json: %v", err)
	}
	return cfg
}

// nonEmptyLines splits output into non-empty lines.
func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
