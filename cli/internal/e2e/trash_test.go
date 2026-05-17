package e2e_test

import (
	"strings"
	"testing"
)

func TestTrashListsSoftDeletedRecords(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add 3 records.
	input1 := createInputFolder(t, inputFolderOpts{HTMLContent: "<html>Record 1</html>"})
	id1 := strings.TrimSpace(runPCSuccess(t, homeDir, "records", "add", input1))

	input2 := createInputFolder(t, inputFolderOpts{HTMLContent: "<html>Record 2</html>"})
	id2 := strings.TrimSpace(runPCSuccess(t, homeDir, "records", "add", input2))

	input3 := createInputFolder(t, inputFolderOpts{HTMLContent: "<html>Record 3</html>"})
	_ = strings.TrimSpace(runPCSuccess(t, homeDir, "records", "add", input3))

	// Soft-delete 2 of them.
	runPCSuccess(t, homeDir, "records", "delete", id1)
	runPCSuccess(t, homeDir, "records", "delete", id2)

	// Trash should show exactly the 2 deleted records.
	stdout := runPCSuccess(t, homeDir, "trash")
	if !strings.Contains(stdout, id1) {
		t.Fatalf("expected trash output to contain deleted record %s, got:\n%s", id1, stdout)
	}
	if !strings.Contains(stdout, id2) {
		t.Fatalf("expected trash output to contain deleted record %s, got:\n%s", id2, stdout)
	}

	// Count non-header lines that contain a record ID (header is "ID  TYPE  DATE  DELETED AT").
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	dataLines := 0
	for _, line := range lines {
		if strings.Contains(line, "ID") && strings.Contains(line, "TYPE") && strings.Contains(line, "DATE") && strings.Contains(line, "DELETED AT") {
			continue
		}
		if strings.TrimSpace(line) != "" {
			dataLines++
		}
	}
	if dataLines != 2 {
		t.Fatalf("expected 2 data lines in trash output, got %d:\n%s", dataLines, stdout)
	}
}

func TestTrashShowsIDDateDeletedAt(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	input := createInputFolder(t, inputFolderOpts{HTMLContent: "<html>To Delete</html>"})
	id := strings.TrimSpace(runPCSuccess(t, homeDir, "records", "add", "--date", "2025-03-15", input))

	runPCSuccess(t, homeDir, "records", "delete", id)

	stdout := runPCSuccess(t, homeDir, "trash")

	// Should contain the header.
	if !strings.Contains(stdout, "ID") || !strings.Contains(stdout, "TYPE") || !strings.Contains(stdout, "DATE") || !strings.Contains(stdout, "DELETED AT") {
		t.Fatalf("expected header with ID, TYPE, DATE, DELETED AT, got:\n%s", stdout)
	}

	// Should contain the record ID.
	if !strings.Contains(stdout, id) {
		t.Fatalf("expected record ID %s in output, got:\n%s", id, stdout)
	}

	// Should contain the date.
	if !strings.Contains(stdout, "2025-03-15") {
		t.Fatalf("expected date 2025-03-15 in output, got:\n%s", stdout)
	}

	// Should contain a timestamp (deleted_at in UTC format like 20XXT).
	// The format is 2006-01-02T15:04:05Z so look for a "T" followed by digits and "Z".
	found := false
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, id) && strings.Contains(line, "T") && strings.Contains(line, "Z") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected deleted_at timestamp on record line, got:\n%s", stdout)
	}
}

func TestTrashEmptyReturnsCleanMessage(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Add a record but do NOT delete it.
	input := createInputFolder(t, inputFolderOpts{})
	runPCSuccess(t, homeDir, "records", "add", input)

	stdout := runPCSuccess(t, homeDir, "trash")
	if !strings.Contains(stdout, "Trash is empty.") {
		t.Fatalf("expected 'Trash is empty.' message, got:\n%s", stdout)
	}
}

func TestTrashNoRecordsAtAll(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	stdout := runPCSuccess(t, homeDir, "trash")
	if !strings.Contains(stdout, "Trash is empty.") {
		t.Fatalf("expected 'Trash is empty.' message, got:\n%s", stdout)
	}
}

func TestTrashExcludesRestoredRecords(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	input := createInputFolder(t, inputFolderOpts{HTMLContent: "<html>Restore Me</html>"})
	id := strings.TrimSpace(runPCSuccess(t, homeDir, "records", "add", input))

	// Delete then restore.
	runPCSuccess(t, homeDir, "records", "delete", id)
	runPCSuccess(t, homeDir, "records", "restore", id)

	stdout := runPCSuccess(t, homeDir, "trash")
	if !strings.Contains(stdout, "Trash is empty.") {
		t.Fatalf("expected 'Trash is empty.' after restore, got:\n%s", stdout)
	}
}
