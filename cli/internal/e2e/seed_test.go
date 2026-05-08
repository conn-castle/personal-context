package e2e_test

import (
	"strings"
	"testing"
)

func TestSeed(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// First seed — should create tutorial records.
	stdout := runPCSuccess(t, homeDir, "seed")
	if !strings.Contains(stdout, "Created 6 tutorial records") {
		t.Fatalf("expected creation message, got: %s", stdout)
	}
	if !strings.Contains(stdout, "personal-context/tutorial") {
		t.Errorf("expected project name in output, got: %s", stdout)
	}

	// Verify each title is listed.
	for _, title := range []string{
		"Welcome to Personal Context",
		"Adding Records",
		"Managing Records",
		"Projects",
		"Web UI",
		"Cloud Sync & Backup",
	} {
		if !strings.Contains(stdout, title) {
			t.Errorf("expected title %q in seed output, got: %s", title, stdout)
		}
	}

	// Verify records are searchable.
	searchOut := runPCSuccess(t, homeDir, "search", "Personal Context")
	if !strings.Contains(searchOut, "personal-context/tutorial") {
		t.Errorf("expected search to find tutorial records, got: %s", searchOut)
	}
}

func TestSeedIdempotent(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// First seed.
	stdout1 := runPCSuccess(t, homeDir, "seed")
	if !strings.Contains(stdout1, "Created 6 tutorial records") {
		t.Fatalf("first seed should create records, got: %s", stdout1)
	}

	// Count record IDs in first output (lines containing YYYYMMDD- pattern).
	firstIDs := countRecordIDs(stdout1)

	// Second seed — should skip.
	stdout2 := runPCSuccess(t, homeDir, "seed")
	if !strings.Contains(stdout2, "already exist") {
		t.Errorf("expected 'already exist' on second seed, got: %s", stdout2)
	}

	// Verify the count hasn't doubled by searching again.
	searchOut := runPCSuccess(t, homeDir, "search", "tutorial", "--format", "json")
	if firstIDs != 6 {
		t.Errorf("expected 6 record IDs in first seed output, got %d", firstIDs)
	}
	// The search should still return exactly 6 tutorial records, not 12.
	searchIDs := strings.Count(searchOut, `"id"`)
	if searchIDs != 6 {
		t.Errorf("expected 6 record IDs in search output after second seed, got %d", searchIDs)
	}
}

func TestSeedNoSetup(t *testing.T) {
	homeDir := t.TempDir()
	// Running seed without setup should fail.
	stderr := runPCFailure(t, homeDir, "seed")
	if stderr == "" {
		t.Error("expected error output when running seed without setup")
	}
}

// countRecordIDs counts lines containing a record ID pattern (8 hex chars after a dash).
func countRecordIDs(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		// Record IDs look like "20260311-abcd1234".
		if len(line) > 17 && line[8] == '-' {
			count++
		}
	}
	return count
}
