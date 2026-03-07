package e2e_test

import (
	"strings"
	"testing"
)

func TestMoveChangesDate(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", "--date", "2025-01-01", inputDir)
	slideID := strings.TrimSpace(stdout)

	runPCSuccess(t, homeDir, "move", slideID, "--date", "2025-02-15")

	db := openTestDB(t, homeDir)
	var date, dayOrder string
	if err := db.QueryRow("SELECT date, day_order FROM slides WHERE id = ?", slideID).Scan(&date, &dayOrder); err != nil {
		t.Fatalf("query slide: %v", err)
	}
	if date != "2025-02-15" {
		t.Fatalf("expected date=2025-02-15, got %q", date)
	}
	if dayOrder == "" {
		t.Fatal("expected non-empty day_order after move")
	}
}

func TestMovePreservesContent(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	htmlContent := "<html><body><h1>Preserved</h1></body></html>"
	notes := "These are important notes"
	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent:  htmlContent,
		Notes:        notes,
		MetadataJSON: `{"project_id":"test/preserve"}`,
	})
	stdout := runPCSuccess(t, homeDir, "add", "--date", "2025-01-01", inputDir)
	slideID := strings.TrimSpace(stdout)

	runPCSuccess(t, homeDir, "move", slideID, "--date", "2025-06-01")

	db := openTestDB(t, homeDir)
	var gotHTML, gotNotes, gotProject string
	if err := db.QueryRow("SELECT html_content, notes, project_id FROM slides WHERE id = ?", slideID).Scan(&gotHTML, &gotNotes, &gotProject); err != nil {
		t.Fatalf("query slide: %v", err)
	}
	if gotHTML != htmlContent {
		t.Fatalf("html_content changed: got %q", gotHTML)
	}
	if gotNotes != notes {
		t.Fatalf("notes changed: got %q", gotNotes)
	}
	if gotProject != "test/preserve" {
		t.Fatalf("project_id changed: got %q", gotProject)
	}
}

func TestMovePositionFirst(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	date := "2025-04-10"
	inputDir1 := createInputFolder(t, inputFolderOpts{})
	stdout1 := runPCSuccess(t, homeDir, "add", "--date", date, inputDir1)
	id1 := strings.TrimSpace(stdout1)

	inputDir2 := createInputFolder(t, inputFolderOpts{})
	stdout2 := runPCSuccess(t, homeDir, "add", "--date", date, inputDir2)
	id2 := strings.TrimSpace(stdout2)

	// id1 is first, id2 is second. Move id2 to --first.
	runPCSuccess(t, homeDir, "move", id2, "--first")

	db := openTestDB(t, homeDir)
	var order1, order2 string
	if err := db.QueryRow("SELECT day_order FROM slides WHERE id = ?", id1).Scan(&order1); err != nil {
		t.Fatalf("query day_order id1: %v", err)
	}
	if err := db.QueryRow("SELECT day_order FROM slides WHERE id = ?", id2).Scan(&order2); err != nil {
		t.Fatalf("query day_order id2: %v", err)
	}
	if order2 >= order1 {
		t.Fatalf("expected id2 day_order < id1 day_order: %q vs %q", order2, order1)
	}
}

func TestMovePositionLast(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	date := "2025-04-10"
	inputDir1 := createInputFolder(t, inputFolderOpts{})
	stdout1 := runPCSuccess(t, homeDir, "add", "--date", date, inputDir1)
	id1 := strings.TrimSpace(stdout1)

	inputDir2 := createInputFolder(t, inputFolderOpts{})
	stdout2 := runPCSuccess(t, homeDir, "add", "--date", date, inputDir2)
	id2 := strings.TrimSpace(stdout2)

	// id1 is first, id2 is second. Move id1 to --last.
	runPCSuccess(t, homeDir, "move", id1, "--last")

	db := openTestDB(t, homeDir)
	var order1, order2 string
	if err := db.QueryRow("SELECT day_order FROM slides WHERE id = ?", id1).Scan(&order1); err != nil {
		t.Fatalf("query day_order id1: %v", err)
	}
	if err := db.QueryRow("SELECT day_order FROM slides WHERE id = ?", id2).Scan(&order2); err != nil {
		t.Fatalf("query day_order id2: %v", err)
	}
	if order1 <= order2 {
		t.Fatalf("expected id1 day_order > id2 day_order: %q vs %q", order1, order2)
	}
}

func TestMovePositionAfter(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	date := "2025-04-10"
	inputDirA := createInputFolder(t, inputFolderOpts{})
	stdoutA := runPCSuccess(t, homeDir, "add", "--date", date, inputDirA)
	idA := strings.TrimSpace(stdoutA)

	inputDirB := createInputFolder(t, inputFolderOpts{})
	stdoutB := runPCSuccess(t, homeDir, "add", "--date", date, inputDirB)
	idB := strings.TrimSpace(stdoutB)

	inputDirC := createInputFolder(t, inputFolderOpts{})
	stdoutC := runPCSuccess(t, homeDir, "add", "--date", date, inputDirC)
	idC := strings.TrimSpace(stdoutC)

	// Initial order: A, B, C. Move C to --after A. Expected: A, C, B.
	runPCSuccess(t, homeDir, "move", idC, "--after", idA)

	db := openTestDB(t, homeDir)
	rows, err := db.Query("SELECT id FROM slides WHERE date = ? ORDER BY day_order ASC", date)
	if err != nil {
		t.Fatalf("query slides: %v", err)
	}
	defer rows.Close()

	var order []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		order = append(order, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	expected := []string{idA, idC, idB}
	if len(order) != len(expected) {
		t.Fatalf("expected %d slides, got %d", len(expected), len(order))
	}
	for i := range expected {
		if order[i] != expected[i] {
			t.Fatalf("position %d: expected %s, got %s (full order: %v)", i, expected[i], order[i], order)
		}
	}
}

func TestMovePositionBefore(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	date := "2025-04-10"
	inputDirA := createInputFolder(t, inputFolderOpts{})
	stdoutA := runPCSuccess(t, homeDir, "add", "--date", date, inputDirA)
	idA := strings.TrimSpace(stdoutA)

	inputDirB := createInputFolder(t, inputFolderOpts{})
	stdoutB := runPCSuccess(t, homeDir, "add", "--date", date, inputDirB)
	idB := strings.TrimSpace(stdoutB)

	inputDirC := createInputFolder(t, inputFolderOpts{})
	stdoutC := runPCSuccess(t, homeDir, "add", "--date", date, inputDirC)
	idC := strings.TrimSpace(stdoutC)

	// Initial order: A, B, C. Move A to --before C. Expected: B, A, C.
	runPCSuccess(t, homeDir, "move", idA, "--before", idC)

	db := openTestDB(t, homeDir)
	rows, err := db.Query("SELECT id FROM slides WHERE date = ? ORDER BY day_order ASC", date)
	if err != nil {
		t.Fatalf("query slides: %v", err)
	}
	defer rows.Close()

	var order []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		order = append(order, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	expected := []string{idB, idA, idC}
	if len(order) != len(expected) {
		t.Fatalf("expected %d slides, got %d", len(expected), len(order))
	}
	for i := range expected {
		if order[i] != expected[i] {
			t.Fatalf("position %d: expected %s, got %s (full order: %v)", i, expected[i], order[i], order)
		}
	}
}

func TestMoveNoFlagsError(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	stderr := runPCFailure(t, homeDir, "move", slideID)
	if !strings.Contains(stderr, "--date") || !strings.Contains(stderr, "position flag") {
		t.Fatalf("expected error about --date or position flag, got %q", stderr)
	}
}

func TestMoveNonexistentID(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	stderr := runPCFailure(t, homeDir, "move", "nonexistent-id", "--first")
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("expected 'not found' error, got %q", stderr)
	}
}

func TestMoveInvalidDate(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	stderr := runPCFailure(t, homeDir, "move", slideID, "--date", "bad-date")
	if !strings.Contains(stderr, "invalid date") {
		t.Fatalf("expected 'invalid date' error, got %q", stderr)
	}
}

func TestMoveOnlyPositionSameDate(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	date := "2025-05-20"
	inputDir1 := createInputFolder(t, inputFolderOpts{})
	stdout1 := runPCSuccess(t, homeDir, "add", "--date", date, inputDir1)
	id1 := strings.TrimSpace(stdout1)

	inputDir2 := createInputFolder(t, inputFolderOpts{})
	stdout2 := runPCSuccess(t, homeDir, "add", "--date", date, inputDir2)
	id2 := strings.TrimSpace(stdout2)

	// Record original day_order for id2
	db := openTestDB(t, homeDir)
	var originalOrder string
	if err := db.QueryRow("SELECT day_order FROM slides WHERE id = ?", id2).Scan(&originalOrder); err != nil {
		t.Fatalf("query original day_order: %v", err)
	}

	// Move id2 to --first without --date; date should stay the same.
	runPCSuccess(t, homeDir, "move", id2, "--first")

	var gotDate, newOrder string
	if err := db.QueryRow("SELECT date, day_order FROM slides WHERE id = ?", id2).Scan(&gotDate, &newOrder); err != nil {
		t.Fatalf("query slide after move: %v", err)
	}
	if gotDate != date {
		t.Fatalf("expected date to stay %s, got %s", date, gotDate)
	}
	if newOrder >= originalOrder {
		t.Fatalf("expected day_order to decrease (moved to first): old=%q new=%q", originalOrder, newOrder)
	}

	// Also verify id2 is now first
	var firstID string
	if err := db.QueryRow("SELECT id FROM slides WHERE date = ? ORDER BY day_order ASC LIMIT 1", date).Scan(&firstID); err != nil {
		t.Fatalf("query first slide: %v", err)
	}
	if firstID != id2 {
		t.Fatalf("expected %s to be first, got %s", id2, firstID)
	}
	_ = id1 // used to create a second slide for ordering context
}

func TestMoveUpdatesUpdatedAt(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", "--date", "2025-01-01", inputDir)
	slideID := strings.TrimSpace(stdout)

	db := openTestDB(t, homeDir)
	var originalUpdatedAt string
	if err := db.QueryRow("SELECT updated_at FROM slides WHERE id = ?", slideID).Scan(&originalUpdatedAt); err != nil {
		t.Fatalf("query original updated_at: %v", err)
	}

	runPCSuccess(t, homeDir, "move", slideID, "--date", "2025-03-01")

	var newUpdatedAt string
	if err := db.QueryRow("SELECT updated_at FROM slides WHERE id = ?", slideID).Scan(&newUpdatedAt); err != nil {
		t.Fatalf("query new updated_at: %v", err)
	}
	if newUpdatedAt <= originalUpdatedAt {
		t.Fatalf("expected updated_at to increase: old=%q new=%q", originalUpdatedAt, newUpdatedAt)
	}
}

func TestMovePreservesDeletedAtForSoftDeletedSlide(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", "--date", "2025-01-01", inputDir)
	slideID := strings.TrimSpace(stdout)

	runPCSuccess(t, homeDir, "delete", slideID)

	db := openTestDB(t, homeDir)
	before := queryDeletedAt(t, db, slideID)
	if !before.Valid {
		t.Fatal("expected deleted_at to be set before move")
	}

	runPCSuccess(t, homeDir, "move", slideID, "--date", "2025-03-01")

	after := queryDeletedAt(t, db, slideID)
	if !after.Valid {
		t.Fatal("expected deleted_at to remain set after move")
	}
}
