package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestFullLocalWorkflow exercises the complete local CLI workflow end-to-end
// in a single sequential test: setup, registry, add, search, edit, move,
// delete, trash, gc, project list, and doctor.
func TestFullLocalWorkflow(t *testing.T) {
	homeDir := t.TempDir()

	// 1. pc setup
	runPCSuccess(t, homeDir, "setup")

	// 2. pc add record1 (date 2025-01-15, explicit metadata project)
	folder1 := createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>Record one neural nets</body></html>",
		Notes:        "First record notes",
		MetadataJSON: `{"project_id":"workflow/test","source_device_id":"test-device"}`,
	})
	stdout := runPCSuccess(t, homeDir, "add", folder1, "--date", "2025-01-15")
	record1 := strings.TrimSpace(stdout)

	// Verify record1 has its explicit project.
	db := openTestDB(t, homeDir)
	var projectID string
	if err := db.QueryRow("SELECT project_id FROM records WHERE id = ?", record1).Scan(&projectID); err != nil {
		t.Fatalf("query record1 project: %v", err)
	}
	if projectID != "workflow/test" {
		t.Fatalf("expected project 'workflow/test', got %v", projectID)
	}

	// 3. pc add record2 (date 2025-01-16, explicit project flag)
	folder2 := createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>Record two transformers</body></html>",
		Notes:        "Second record notes",
		MetadataJSON: `{"project_id":"other/proj","source_device_id":"test-device"}`,
	})
	stdout = runPCSuccess(t, homeDir, "add", "--project", "other/proj", folder2, "--date", "2025-01-16")
	record2 := strings.TrimSpace(stdout)

	// 4. pc add record3 (date 2025-01-15, same date as record1)
	folder3 := createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>Record three experiment</body></html>",
		Figures:      map[string][]byte{"chart.png": []byte("fake-png-data")},
		MetadataJSON: `{"project_id":"workflow/test","source_device_id":"test-device"}`,
	})
	stdout = runPCSuccess(t, homeDir, "add", folder3, "--date", "2025-01-15")
	record3 := strings.TrimSpace(stdout)

	// 6. pc search "neural" -- should find record1
	stdout = runPCSuccess(t, homeDir, "search", "neural")
	if !strings.Contains(stdout, record1) {
		t.Fatalf("search 'neural' should find record1 %s, got: %s", record1, stdout)
	}
	if strings.Contains(stdout, record2) {
		t.Fatalf("search 'neural' should not find record2")
	}

	// 7. pc search --format json "Record" -- should find all 3
	stdout = runPCSuccess(t, homeDir, "search", "--format", "json", "Record")
	var results []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatalf("parse search json: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// 8. pc search --project "other/proj" "Record" -- should find only record2
	stdout = runPCSuccess(t, homeDir, "search", "--project", "other/proj", "Record")
	if !strings.Contains(stdout, record2) {
		t.Fatalf("project-filtered search should find record2")
	}
	if strings.Contains(stdout, record1) || strings.Contains(stdout, record3) {
		t.Fatalf("project-filtered search should not find record1 or record3")
	}

	// 9. pc edit record1 with new content
	editFolder := createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>Updated record one content</body></html>",
		MetadataJSON: `{"project_id":"workflow/test","source_device_id":"test-device"}`,
	})
	runPCSuccess(t, homeDir, "edit", record1, editFolder)

	// 10. pc search "Updated" -- should find record1
	stdout = runPCSuccess(t, homeDir, "search", "Updated")
	if !strings.Contains(stdout, record1) {
		t.Fatalf("search 'Updated' should find edited record1")
	}

	// 11. pc move record3 to different date
	runPCSuccess(t, homeDir, "move", record3, "--date", "2025-01-16")

	// 12. pc delete record2
	runPCSuccess(t, homeDir, "delete", record2)

	// 13. pc trash -- should show record2
	stdout = runPCSuccess(t, homeDir, "trash")
	if !strings.Contains(stdout, record2) {
		t.Fatalf("trash should show deleted record2")
	}
	if strings.Contains(stdout, record1) || strings.Contains(stdout, record3) {
		t.Fatalf("trash should not show active records")
	}

	// 14. pc search "transformers" -- excluded (deleted)
	stdout = runPCSuccess(t, homeDir, "search", "transformers")
	if strings.Contains(stdout, record2) {
		t.Fatalf("default search should exclude deleted record2")
	}

	// 15. pc search --deleted "transformers" -- included
	stdout = runPCSuccess(t, homeDir, "search", "--deleted", "transformers")
	if !strings.Contains(stdout, record2) {
		t.Fatalf("--deleted search should include deleted record2")
	}

	// 16. Backdate record2's deleted_at to 31 days ago for gc
	backdateDeletedAt(t, db, record2, 31)

	// 17. pc gc
	stdout = runPCSuccess(t, homeDir, "gc")
	if !strings.Contains(stdout, record2) {
		t.Fatalf("gc should report deleting record2")
	}

	// 18. pc trash -- should be empty
	stdout = runPCSuccess(t, homeDir, "trash")
	if !strings.Contains(stdout, "Trash is empty") {
		t.Fatalf("trash should be empty after gc, got: %s", stdout)
	}

	// 19. pc project list is registry-backed and retains both projects.
	stdout = runPCSuccess(t, homeDir, "project", "list")
	if !strings.Contains(stdout, "workflow/test") || !strings.Contains(stdout, "other/proj") {
		t.Fatalf("project list should show both registered projects, got %q", stdout)
	}

	// 20. pc doctor -- should be healthy
	runPCSuccess(t, homeDir, "doctor")

	// 21. Final DB verification: exactly 2 records remain
	var recordCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM records").Scan(&recordCount); err != nil {
		t.Fatalf("count records: %v", err)
	}
	if recordCount != 2 {
		t.Fatalf("expected 2 records after gc, got %d", recordCount)
	}
}
