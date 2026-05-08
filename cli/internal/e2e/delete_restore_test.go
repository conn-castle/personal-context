package e2e_test

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

// addRecordHelper sets up a home dir, runs pc setup, adds a minimal record,
// and returns the homeDir and the record ID.
func addRecordHelper(t *testing.T) (string, string) {
	t.Helper()
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	recordID := strings.TrimSpace(stdout)
	if len(recordID) == 0 {
		t.Fatal("expected record ID in stdout")
	}
	return homeDir, recordID
}

// queryDeletedAt returns the deleted_at value for a record as a sql.NullString.
func queryDeletedAt(t *testing.T, db *sql.DB, recordID string) sql.NullString {
	t.Helper()
	var deletedAt sql.NullString
	if err := db.QueryRow("SELECT deleted_at FROM records WHERE id = ?", recordID).Scan(&deletedAt); err != nil {
		t.Fatalf("query deleted_at: %v", err)
	}
	return deletedAt
}

// queryUpdatedAt returns the raw updated_at value for a record as a string.
// The column stores millisecond-precision timestamps (e.g. 2026-03-06T14:23:18.123Z).
func queryUpdatedAt(t *testing.T, db *sql.DB, recordID string) string {
	t.Helper()
	var updatedAt string
	if err := db.QueryRow("SELECT updated_at FROM records WHERE id = ?", recordID).Scan(&updatedAt); err != nil {
		t.Fatalf("query updated_at: %v", err)
	}
	return updatedAt
}

// backdateUpdatedAt sets updated_at to 1 hour in the past so that the next
// trigger-generated timestamp is guaranteed to differ, without relying on sleep.
func backdateUpdatedAt(t *testing.T, db *sql.DB, recordID string) {
	t.Helper()
	past := time.Now().UTC().Add(-time.Hour).Format("2006-01-02T15:04:05.000Z")
	if _, err := db.Exec(`UPDATE records SET updated_at = ? WHERE id = ?`, past, recordID); err != nil {
		t.Fatalf("backdate updated_at for %s: %v", recordID, err)
	}
}

func TestDeleteSetsDeletedAt(t *testing.T) {
	homeDir, recordID := addRecordHelper(t)

	// Confirm deleted_at is NULL before delete.
	db := openTestDB(t, homeDir)
	before := queryDeletedAt(t, db, recordID)
	if before.Valid {
		t.Fatalf("expected deleted_at to be NULL before delete, got %q", before.String)
	}

	runPCSuccess(t, homeDir, "delete", recordID)

	after := queryDeletedAt(t, db, recordID)
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
	homeDir, recordID := addRecordHelper(t)

	stdout := runPCSuccess(t, homeDir, "delete", recordID)
	expected := "Record " + recordID + " deleted"
	if !strings.Contains(stdout, expected) {
		t.Fatalf("expected stdout to contain %q, got %q", expected, stdout)
	}
}

func TestRestoreClearsDeletedAt(t *testing.T) {
	homeDir, recordID := addRecordHelper(t)

	runPCSuccess(t, homeDir, "delete", recordID)

	db := openTestDB(t, homeDir)
	afterDelete := queryDeletedAt(t, db, recordID)
	if !afterDelete.Valid {
		t.Fatal("expected deleted_at to be NOT NULL after delete")
	}

	runPCSuccess(t, homeDir, "restore", recordID)

	afterRestore := queryDeletedAt(t, db, recordID)
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
	homeDir, recordID := addRecordHelper(t)

	runPCSuccess(t, homeDir, "delete", recordID)
	stdout := runPCSuccess(t, homeDir, "restore", recordID)

	expected := "Record " + recordID + " restored"
	if !strings.Contains(stdout, expected) {
		t.Fatalf("expected stdout to contain %q, got %q", expected, stdout)
	}
}

func TestDeleteUpdatesUpdatedAt(t *testing.T) {
	homeDir, recordID := addRecordHelper(t)

	db := openTestDB(t, homeDir)

	// Backdate updated_at so the trigger produces a distinguishable timestamp.
	backdateUpdatedAt(t, db, recordID)
	before := queryUpdatedAt(t, db, recordID)

	runPCSuccess(t, homeDir, "delete", recordID)

	after := queryUpdatedAt(t, db, recordID)
	if after <= before {
		t.Fatalf("expected updated_at to advance after delete: before=%q after=%q", before, after)
	}
}

func TestRestoreUpdatesUpdatedAt(t *testing.T) {
	homeDir, recordID := addRecordHelper(t)

	runPCSuccess(t, homeDir, "delete", recordID)

	db := openTestDB(t, homeDir)

	// Backdate updated_at so the trigger produces a distinguishable timestamp.
	backdateUpdatedAt(t, db, recordID)
	before := queryUpdatedAt(t, db, recordID)

	runPCSuccess(t, homeDir, "restore", recordID)

	after := queryUpdatedAt(t, db, recordID)
	if after <= before {
		t.Fatalf("expected updated_at to advance after restore: before=%q after=%q", before, after)
	}
}

func TestDeletedRecordStillShowable(t *testing.T) {
	homeDir, recordID := addRecordHelper(t)

	runPCSuccess(t, homeDir, "delete", recordID)

	stdout := runPCSuccess(t, homeDir, "show", recordID)
	if !strings.Contains(strings.ToLower(stdout), "deleted") {
		t.Fatalf("expected 'deleted' in show output for soft-deleted record, got %q", stdout)
	}
}
