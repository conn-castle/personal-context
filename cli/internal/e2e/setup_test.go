package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupCreatesLocalEnvironment(t *testing.T) {
	homeDir := t.TempDir()
	stdout := runPCSuccess(t, homeDir, "setup")

	base := filepath.Join(homeDir, "personal-context")

	if !strings.Contains(stdout, base) {
		t.Fatalf("expected stdout to contain base path %q, got %q", base, stdout)
	}

	// Verify .pc directory exists
	pcDir := filepath.Join(base, ".pc")
	if info, err := os.Stat(pcDir); err != nil {
		t.Fatalf(".pc directory not created: %v", err)
	} else if !info.IsDir() {
		t.Fatalf(".pc is not a directory")
	}

	// Verify SQLite DB exists
	dbPath := filepath.Join(pcDir, "pc.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("pc.db not created: %v", err)
	}

	// Verify DB has expected tables
	db := openTestDB(t, homeDir)

	tables := []string{"slides", "slide_figures", "slide_data_files", "templates", "sync_version", "schema_migrations"}
	for _, table := range tables {
		var exists int
		if err := db.QueryRow(
			"SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)", table,
		).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if exists != 1 {
			t.Fatalf("table %s not found in database", table)
		}
	}

	// Verify templates seeded
	templateCount := queryRowCount(t, db, "templates")
	if templateCount != 2 {
		t.Fatalf("expected 2 templates, got %d", templateCount)
	}

	var textOnlyExists int
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM templates WHERE name='text-only')").Scan(&textOnlyExists); err != nil {
		t.Fatalf("check text-only template: %v", err)
	}
	if textOnlyExists != 1 {
		t.Fatal("text-only template not seeded")
	}

	var singleImageExists int
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM templates WHERE name='single-image')").Scan(&singleImageExists); err != nil {
		t.Fatalf("check single-image template: %v", err)
	}
	if singleImageExists != 1 {
		t.Fatal("single-image template not seeded")
	}

	// Verify config.json exists and is valid local-only
	configPath := filepath.Join(pcDir, "config.json")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(configBytes, &cfg); err != nil {
		t.Fatalf("parse config.json: %v", err)
	}
	if len(cfg) != 0 {
		t.Fatalf("expected empty config (local-only), got %v", cfg)
	}
}

func TestSetupIsIdempotent(t *testing.T) {
	homeDir := t.TempDir()

	// First run
	runPCSuccess(t, homeDir, "setup")

	db := openTestDB(t, homeDir)
	firstTemplateCount := queryRowCount(t, db, "templates")
	firstMigrationCount := queryRowCount(t, db, "schema_migrations")
	db.Close()

	// Second run
	stdout := runPCSuccess(t, homeDir, "setup")

	if !strings.Contains(stdout, "initialized") {
		t.Fatalf("expected success message on second run, got %q", stdout)
	}

	db = openTestDB(t, homeDir)
	secondTemplateCount := queryRowCount(t, db, "templates")
	secondMigrationCount := queryRowCount(t, db, "schema_migrations")

	if secondTemplateCount != firstTemplateCount {
		t.Fatalf("template count changed: %d -> %d", firstTemplateCount, secondTemplateCount)
	}
	if secondMigrationCount != firstMigrationCount {
		t.Fatalf("migration count changed: %d -> %d", firstMigrationCount, secondMigrationCount)
	}
}

func TestSetupExitCodeIsZero(t *testing.T) {
	homeDir := t.TempDir()
	result := runPC(t, homeDir, "setup")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}
}

func TestSetupConfigPermissions(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config.json: %v", err)
	}

	// config.json should be 0600 (owner read/write only)
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Fatalf("expected config.json permissions 0600, got %04o", perm)
	}
}
