package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorHealthySystem(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add a record with a figure so there is actual data to check
	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/plot.png"></html>`,
		Figures:     map[string][]byte{"plot.png": []byte("fake-png-data")},
	})
	runPCSuccess(t, homeDir, "add", inputDir)

	result := runPC(t, homeDir, "doctor")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstdout: %s\nstderr: %s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "All checks passed.") {
		t.Fatalf("expected 'All checks passed.' in output, got %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "Database:           OK") {
		t.Fatalf("expected 'Database:           OK' in output, got %q", result.Stdout)
	}
}

func TestDoctorDatabaseInaccessible(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Delete the config.json to make openLocalStack fail (config read error).
	// Simply deleting pc.db is insufficient because SQLite re-creates it on open.
	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	if err := os.Remove(configPath); err != nil {
		t.Fatalf("remove config: %v", err)
	}

	result := runPC(t, homeDir, "doctor")
	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code\nstdout: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "FAIL") {
		t.Fatalf("expected FAIL in output, got %q", result.Stdout)
	}
}

func TestDoctorOrphanedFigures(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add a record with a figure
	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/fig.png"></html>`,
		Figures:     map[string][]byte{"fig.png": []byte("figure-data")},
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	recordID := strings.TrimSpace(stdout)

	// Hard-delete the record via direct SQL, leaving figure on disk
	db := openTestDB(t, homeDir)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := db.Exec("DELETE FROM records WHERE id = ?", recordID); err != nil {
		t.Fatalf("hard delete record: %v", err)
	}

	// Verify figure dir still exists on disk
	figDir := filepath.Join(homeDir, "personal-context", "figures", recordID)
	if _, err := os.Stat(figDir); err != nil {
		t.Fatalf("expected figure dir to remain after SQL delete: %v", err)
	}

	result := runPC(t, homeDir, "doctor")
	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for orphaned figures\nstdout: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "Orphaned figures:   WARN") {
		t.Fatalf("expected orphaned figures warning, got %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, recordID) {
		t.Fatalf("expected record ID %s in orphan warning, got %q", recordID, result.Stdout)
	}
}

func TestDoctorMissingFigureFiles(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add a record with a figure
	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/fig.png"></html>`,
		Figures:     map[string][]byte{"fig.png": []byte("figure-data")},
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	recordID := strings.TrimSpace(stdout)

	// Delete the figure file from disk but leave DB record
	figurePath := filepath.Join(homeDir, "personal-context", "figures", recordID, "fig.png")
	if err := os.Remove(figurePath); err != nil {
		t.Fatalf("remove figure file: %v", err)
	}

	result := runPC(t, homeDir, "doctor")
	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for missing figures\nstdout: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "Missing figures:    WARN") {
		t.Fatalf("expected missing figures warning, got %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, recordID+"/fig.png") {
		t.Fatalf("expected record/figure path in warning, got %q", result.Stdout)
	}
}

func TestDoctorMissingFigureFilesOnDeletedRecord(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/fig.png"></html>`,
		Figures:     map[string][]byte{"fig.png": []byte("figure-data")},
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	recordID := strings.TrimSpace(stdout)

	runPCSuccess(t, homeDir, "delete", recordID)

	figurePath := filepath.Join(homeDir, "personal-context", "figures", recordID, "fig.png")
	if err := os.Remove(figurePath); err != nil {
		t.Fatalf("remove figure file: %v", err)
	}

	result := runPC(t, homeDir, "doctor")
	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for missing deleted-record figure\nstdout: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "Missing figures:    WARN") {
		t.Fatalf("expected missing figures warning, got %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, recordID+"/fig.png") {
		t.Fatalf("expected record/figure path in warning, got %q", result.Stdout)
	}
}

func TestDoctorNoRecords(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// No records added — should be all OK
	result := runPC(t, homeDir, "doctor")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstdout: %s\nstderr: %s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "All checks passed.") {
		t.Fatalf("expected 'All checks passed.' in output, got %q", result.Stdout)
	}
}
