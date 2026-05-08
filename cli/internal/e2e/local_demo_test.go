package e2e_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type showJSONResult struct {
	ID          string  `json:"id"`
	HTMLContent string  `json:"html_content"`
	Notes       *string `json:"notes"`
	DeletedAt   *string `json:"deleted_at"`
}

// TestLocalDemoWorkflow mirrors the generalized local demo flow used by the
// standalone verification script so the same narrative exists in the Go CLI
// e2e suite and the thin Playwright wrapper.
func TestLocalDemoWorkflow(t *testing.T) {
	homeDir := t.TempDir()
	dateValue := "2025-04-01"
	projectID := "demo/local"

	runPCSuccess(t, homeDir, "setup")

	recordIDs := make(map[int]string, 10)

	for i := 1; i <= 10; i++ {
		folder := createInputFolder(t, inputFolderOpts{
			HTMLContent:  demoRecordHTML(i),
			Notes:        demoRecordNotes(i),
			MetadataJSON: `{"project_id":"` + projectID + `","source_device_id":"test-device"}`,
		})
		recordIDs[i] = strings.TrimSpace(runPCSuccess(t, homeDir, "add", "--date", dateValue, folder))
	}

	for _, i := range []int{6, 7, 8, 9, 10} {
		stdout := runPCSuccess(t, homeDir, "delete", recordIDs[i])
		expected := "Record " + recordIDs[i] + " deleted"
		if !strings.Contains(stdout, expected) {
			t.Fatalf("expected delete output to contain %q, got %q", expected, stdout)
		}
	}

	restoreOut := runPCSuccess(t, homeDir, "restore", recordIDs[8])
	if !strings.Contains(restoreOut, "Record "+recordIDs[8]+" restored") {
		t.Fatalf("expected restore output to mention record %s, got %q", recordIDs[8], restoreOut)
	}

	moveOut := runPCSuccess(t, homeDir, "move", recordIDs[4], "--after", recordIDs[2])
	if !strings.Contains(moveOut, "Record "+recordIDs[4]+" moved") {
		t.Fatalf("expected move output to mention record %s, got %q", recordIDs[4], moveOut)
	}

	activeSearch := runPCSuccess(t, homeDir, "search", "--format", "ids", "Record")
	activeIDs := nonEmptyLines(activeSearch)
	expectedActive := []string{
		recordIDs[1],
		recordIDs[2],
		recordIDs[4],
		recordIDs[3],
		recordIDs[5],
		recordIDs[8],
	}
	assertExactOrder(t, activeIDs, expectedActive, "active record order")

	trashOut := runPCSuccess(t, homeDir, "trash")
	deletedIDs := trashIDs(trashOut)
	expectedDeleted := []string{
		recordIDs[6],
		recordIDs[7],
		recordIDs[9],
		recordIDs[10],
	}
	assertExactOrder(t, deletedIDs, expectedDeleted, "trash order")
	if strings.Contains(trashOut, recordIDs[8]) {
		t.Fatalf("restored record %s should not appear in trash output:\n%s", recordIDs[8], trashOut)
	}

	searchDeleted := runPCSuccess(t, homeDir, "search", "--deleted", "--format", "ids", "remain in trash")
	deletedSearchIDs := nonEmptyLines(searchDeleted)
	assertExactOrder(t, deletedSearchIDs, expectedDeleted, "deleted search order")

	projectOut := runPCSuccess(t, homeDir, "project", "list")
	if !strings.Contains(projectOut, projectID) {
		t.Fatalf("expected project list to contain %q, got:\n%s", projectID, projectOut)
	}

	doctorOut := runPCSuccess(t, homeDir, "doctor")
	if !strings.Contains(doctorOut, "All checks passed.") {
		t.Fatalf("expected doctor output to report success, got:\n%s", doctorOut)
	}

	firstRecord := parseShowJSON(t, runPCSuccess(t, homeDir, "show", "--format", "json", recordIDs[1]))
	if firstRecord.ID != recordIDs[1] {
		t.Fatalf("expected first record ID %s, got %s", recordIDs[1], firstRecord.ID)
	}
	if !strings.Contains(firstRecord.HTMLContent, "deleted records 06-10") {
		t.Fatalf("expected first record HTML to contain the demo narrative, got:\n%s", firstRecord.HTMLContent)
	}
	if firstRecord.Notes == nil || *firstRecord.Notes != "Narrative record for the local demo." {
		t.Fatalf("unexpected first record notes: %#v", firstRecord.Notes)
	}
	if firstRecord.DeletedAt != nil {
		t.Fatalf("expected first record to remain active, got deleted_at=%v", *firstRecord.DeletedAt)
	}

	lastActive := parseShowJSON(t, runPCSuccess(t, homeDir, "show", "--format", "json", recordIDs[8]))
	if lastActive.DeletedAt != nil {
		t.Fatalf("expected restored record 8 to be active, got deleted_at=%v", *lastActive.DeletedAt)
	}
	if !strings.Contains(lastActive.HTMLContent, "Expected final active order: 01, 02, 04, 03, 05, 08.") {
		t.Fatalf("expected final-state explanation in record 8 HTML, got:\n%s", lastActive.HTMLContent)
	}
	if lastActive.Notes == nil || *lastActive.Notes != "Final-state explanation for the local demo." {
		t.Fatalf("unexpected record 8 notes: %#v", lastActive.Notes)
	}

	db := openTestDB(t, homeDir)
	assertRecordCounts(t, db, 10, 4)
	assertProjectApplied(t, db, projectID)
	assertDateOrder(t, db, dateValue, expectedActive)
}

func demoRecordHTML(index int) string {
	title := fmt.Sprintf("Record %02d", index)
	body := fmt.Sprintf("<p>Local demo content for %s.</p><p>Sequence number: %d.</p>", title, index)

	switch index {
	case 1:
		body = "<p>This demo created 10 records, deleted records 06-10, restored Record 08, and moved Record 04 after Record 02.</p><p>Use the summary page to confirm the final order and trash membership.</p>"
	case 8:
		body = "<p>Expected final active order: 01, 02, 04, 03, 05, 08.</p><p>Expected trash: 06, 07, 09, 10.</p>"
	case 6, 7, 9, 10:
		body = fmt.Sprintf("<p>%s is expected to remain in trash at the end of the demo.</p><p>Sequence number: %d.</p>", title, index)
	}

	return fmt.Sprintf("<html><body><h1>%s</h1>%s</body></html>", title, body)
}

func demoRecordNotes(index int) string {
	switch index {
	case 1:
		return "Narrative record for the local demo."
	case 8:
		return "Final-state explanation for the local demo."
	case 6, 7, 9, 10:
		return "This record should remain deleted after the demo completes."
	default:
		return fmt.Sprintf("Notes for Record %02d.", index)
	}
}

func parseShowJSON(t *testing.T, raw string) showJSONResult {
	t.Helper()

	var result showJSONResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("parse show json: %v\noutput: %s", err, raw)
	}
	return result
}

func trashIDs(raw string) []string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	ids := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "ID") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		ids = append(ids, fields[0])
	}
	return ids
}

func assertExactOrder(t *testing.T, got []string, expected []string, label string) {
	t.Helper()

	if len(got) != len(expected) {
		t.Fatalf("%s length mismatch: expected %d, got %d (%v)", label, len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("%s mismatch at position %d: expected %s, got %s (full got=%v)", label, i, expected[i], got[i], got)
		}
	}
}

func assertRecordCounts(t *testing.T, db *sql.DB, total int, deleted int) {
	t.Helper()

	if got := queryRowCount(t, db, "records"); got != total {
		t.Fatalf("expected %d total records, got %d", total, got)
	}

	var deletedCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM records WHERE deleted_at IS NOT NULL").Scan(&deletedCount); err != nil {
		t.Fatalf("count deleted records: %v", err)
	}
	if deletedCount != deleted {
		t.Fatalf("expected %d deleted records, got %d", deleted, deletedCount)
	}
}

func assertProjectApplied(t *testing.T, db *sql.DB, projectID string) {
	t.Helper()

	var nullProjects int
	if err := db.QueryRow("SELECT COUNT(*) FROM records WHERE project_id IS NULL").Scan(&nullProjects); err != nil {
		t.Fatalf("count records without project: %v", err)
	}
	if nullProjects != 0 {
		t.Fatalf("expected all records to have explicit project provenance, found %d NULL project_id rows", nullProjects)
	}

	var mismatchedProjects int
	if err := db.QueryRow("SELECT COUNT(*) FROM records WHERE project_id <> ?", projectID).Scan(&mismatchedProjects); err != nil {
		t.Fatalf("count records with mismatched project: %v", err)
	}
	if mismatchedProjects != 0 {
		t.Fatalf("expected all records to use project %q, found %d mismatches", projectID, mismatchedProjects)
	}
}

func assertDateOrder(t *testing.T, db *sql.DB, date string, expected []string) {
	t.Helper()

	rows, err := db.Query("SELECT id FROM records WHERE date = ? AND deleted_at IS NULL ORDER BY day_order ASC", date)
	if err != nil {
		t.Fatalf("query active date order: %v", err)
	}
	defer func() { _ = rows.Close() }()

	got := make([]string, 0, len(expected))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan active record id: %v", err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	assertExactOrder(t, got, expected, "db active order")
}
