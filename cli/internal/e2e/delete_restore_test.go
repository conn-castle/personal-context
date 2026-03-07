package e2e_test

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

// addSlideHelper sets up a home dir, runs pc setup, adds a minimal slide,
// and returns the homeDir and the slide ID.
func addSlideHelper(t *testing.T) (string, string) {
	t.Helper()
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)
	if len(slideID) == 0 {
		t.Fatal("expected slide ID in stdout")
	}
	return homeDir, slideID
}

// queryDeletedAt returns the deleted_at value for a slide as a sql.NullString.
func queryDeletedAt(t *testing.T, db *sql.DB, slideID string) sql.NullString {
	t.Helper()
	var deletedAt sql.NullString
	if err := db.QueryRow("SELECT deleted_at FROM slides WHERE id = ?", slideID).Scan(&deletedAt); err != nil {
		t.Fatalf("query deleted_at: %v", err)
	}
	return deletedAt
}

// queryUpdatedAt returns the raw updated_at value for a slide as a string.
// The column stores millisecond-precision timestamps (e.g. 2026-03-06T14:23:18.123Z).
func queryUpdatedAt(t *testing.T, db *sql.DB, slideID string) string {
	t.Helper()
	var updatedAt string
	if err := db.QueryRow("SELECT updated_at FROM slides WHERE id = ?", slideID).Scan(&updatedAt); err != nil {
		t.Fatalf("query updated_at: %v", err)
	}
	return updatedAt
}

func TestDeleteSetsDeletedAt(t *testing.T) {
	homeDir, slideID := addSlideHelper(t)

	// Confirm deleted_at is NULL before delete.
	db := openTestDB(t, homeDir)
	before := queryDeletedAt(t, db, slideID)
	if before.Valid {
		t.Fatalf("expected deleted_at to be NULL before delete, got %q", before.String)
	}

	runPCSuccess(t, homeDir, "delete", slideID)

	after := queryDeletedAt(t, db, slideID)
	if !after.Valid {
		t.Fatal("expected deleted_at to be NOT NULL after delete")
	}
}

func TestDeleteNonexistentID(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	stderr := runPCFailure(t, homeDir, "delete", "nonexistent-id")
	if !strings.Contains(strings.ToLower(stderr), "not found") {
		t.Fatalf("expected 'not found' in stderr, got %q", stderr)
	}
}

func TestDeleteOutputMessage(t *testing.T) {
	homeDir, slideID := addSlideHelper(t)

	stdout := runPCSuccess(t, homeDir, "delete", slideID)
	expected := "Slide " + slideID + " deleted"
	if !strings.Contains(stdout, expected) {
		t.Fatalf("expected stdout to contain %q, got %q", expected, stdout)
	}
}

func TestRestoreClearsDeletedAt(t *testing.T) {
	homeDir, slideID := addSlideHelper(t)

	runPCSuccess(t, homeDir, "delete", slideID)

	db := openTestDB(t, homeDir)
	afterDelete := queryDeletedAt(t, db, slideID)
	if !afterDelete.Valid {
		t.Fatal("expected deleted_at to be NOT NULL after delete")
	}

	runPCSuccess(t, homeDir, "restore", slideID)

	afterRestore := queryDeletedAt(t, db, slideID)
	if afterRestore.Valid {
		t.Fatalf("expected deleted_at to be NULL after restore, got %q", afterRestore.String)
	}
}

func TestRestoreNonexistentID(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	stderr := runPCFailure(t, homeDir, "restore", "nonexistent-id")
	if !strings.Contains(strings.ToLower(stderr), "not found") {
		t.Fatalf("expected 'not found' in stderr, got %q", stderr)
	}
}

func TestRestoreOutputMessage(t *testing.T) {
	homeDir, slideID := addSlideHelper(t)

	runPCSuccess(t, homeDir, "delete", slideID)
	stdout := runPCSuccess(t, homeDir, "restore", slideID)

	expected := "Slide " + slideID + " restored"
	if !strings.Contains(stdout, expected) {
		t.Fatalf("expected stdout to contain %q, got %q", expected, stdout)
	}
}

func TestDeleteUpdatesUpdatedAt(t *testing.T) {
	homeDir, slideID := addSlideHelper(t)

	db := openTestDB(t, homeDir)
	before := queryUpdatedAt(t, db, slideID)

	// Small delay to ensure the trigger-generated timestamp differs.
	time.Sleep(50 * time.Millisecond)

	runPCSuccess(t, homeDir, "delete", slideID)

	after := queryUpdatedAt(t, db, slideID)
	if after == before {
		t.Fatalf("expected updated_at to change after delete, but both are %q", before)
	}
}

func TestRestoreUpdatesUpdatedAt(t *testing.T) {
	homeDir, slideID := addSlideHelper(t)

	runPCSuccess(t, homeDir, "delete", slideID)

	db := openTestDB(t, homeDir)
	before := queryUpdatedAt(t, db, slideID)

	// Small delay to ensure the trigger-generated timestamp differs.
	time.Sleep(50 * time.Millisecond)

	runPCSuccess(t, homeDir, "restore", slideID)

	after := queryUpdatedAt(t, db, slideID)
	if after == before {
		t.Fatalf("expected updated_at to change after restore, but both are %q", before)
	}
}

func TestDeletedSlideStillShowable(t *testing.T) {
	homeDir, slideID := addSlideHelper(t)

	runPCSuccess(t, homeDir, "delete", slideID)

	stdout := runPCSuccess(t, homeDir, "show", slideID)
	if !strings.Contains(strings.ToLower(stdout), "deleted") {
		t.Fatalf("expected 'deleted' in show output for soft-deleted slide, got %q", stdout)
	}
}
