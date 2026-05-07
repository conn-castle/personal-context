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

	// 2. pc add slide1 (date 2025-01-15, explicit metadata project)
	folder1 := createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>Slide one neural nets</body></html>",
		Notes:        "First slide notes",
		MetadataJSON: `{"project_id":"workflow/test","source_device_id":"test-device"}`,
	})
	stdout := runPCSuccess(t, homeDir, "add", folder1, "--date", "2025-01-15")
	slide1 := strings.TrimSpace(stdout)

	// Verify slide1 has its explicit project.
	db := openTestDB(t, homeDir)
	var projectID string
	if err := db.QueryRow("SELECT project_id FROM slides WHERE id = ?", slide1).Scan(&projectID); err != nil {
		t.Fatalf("query slide1 project: %v", err)
	}
	if projectID != "workflow/test" {
		t.Fatalf("expected project 'workflow/test', got %v", projectID)
	}

	// 3. pc add slide2 (date 2025-01-16, explicit project flag)
	folder2 := createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>Slide two transformers</body></html>",
		Notes:        "Second slide notes",
		MetadataJSON: `{"project_id":"other/proj","source_device_id":"test-device"}`,
	})
	stdout = runPCSuccess(t, homeDir, "add", "--project", "other/proj", folder2, "--date", "2025-01-16")
	slide2 := strings.TrimSpace(stdout)

	// 4. pc add slide3 (date 2025-01-15, same date as slide1)
	folder3 := createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>Slide three experiment</body></html>",
		Figures:      map[string][]byte{"chart.png": []byte("fake-png-data")},
		MetadataJSON: `{"project_id":"workflow/test","source_device_id":"test-device"}`,
	})
	stdout = runPCSuccess(t, homeDir, "add", folder3, "--date", "2025-01-15")
	slide3 := strings.TrimSpace(stdout)

	// 6. pc search "neural" -- should find slide1
	stdout = runPCSuccess(t, homeDir, "search", "neural")
	if !strings.Contains(stdout, slide1) {
		t.Fatalf("search 'neural' should find slide1 %s, got: %s", slide1, stdout)
	}
	if strings.Contains(stdout, slide2) {
		t.Fatalf("search 'neural' should not find slide2")
	}

	// 7. pc search --format json "Slide" -- should find all 3
	stdout = runPCSuccess(t, homeDir, "search", "--format", "json", "Slide")
	var results []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatalf("parse search json: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// 8. pc search --project "other/proj" "Slide" -- should find only slide2
	stdout = runPCSuccess(t, homeDir, "search", "--project", "other/proj", "Slide")
	if !strings.Contains(stdout, slide2) {
		t.Fatalf("project-filtered search should find slide2")
	}
	if strings.Contains(stdout, slide1) || strings.Contains(stdout, slide3) {
		t.Fatalf("project-filtered search should not find slide1 or slide3")
	}

	// 9. pc edit slide1 with new content
	editFolder := createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>Updated slide one content</body></html>",
		MetadataJSON: `{"project_id":"workflow/test","source_device_id":"test-device"}`,
	})
	runPCSuccess(t, homeDir, "edit", slide1, editFolder)

	// 10. pc search "Updated" -- should find slide1
	stdout = runPCSuccess(t, homeDir, "search", "Updated")
	if !strings.Contains(stdout, slide1) {
		t.Fatalf("search 'Updated' should find edited slide1")
	}

	// 11. pc move slide3 to different date
	runPCSuccess(t, homeDir, "move", slide3, "--date", "2025-01-16")

	// 12. pc delete slide2
	runPCSuccess(t, homeDir, "delete", slide2)

	// 13. pc trash -- should show slide2
	stdout = runPCSuccess(t, homeDir, "trash")
	if !strings.Contains(stdout, slide2) {
		t.Fatalf("trash should show deleted slide2")
	}
	if strings.Contains(stdout, slide1) || strings.Contains(stdout, slide3) {
		t.Fatalf("trash should not show active slides")
	}

	// 14. pc search "transformers" -- excluded (deleted)
	stdout = runPCSuccess(t, homeDir, "search", "transformers")
	if strings.Contains(stdout, slide2) {
		t.Fatalf("default search should exclude deleted slide2")
	}

	// 15. pc search --deleted "transformers" -- included
	stdout = runPCSuccess(t, homeDir, "search", "--deleted", "transformers")
	if !strings.Contains(stdout, slide2) {
		t.Fatalf("--deleted search should include deleted slide2")
	}

	// 16. Backdate slide2's deleted_at to 31 days ago for gc
	backdateDeletedAt(t, db, slide2, 31)

	// 17. pc gc
	stdout = runPCSuccess(t, homeDir, "gc")
	if !strings.Contains(stdout, slide2) {
		t.Fatalf("gc should report deleting slide2")
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

	// 21. Final DB verification: exactly 2 slides remain
	var slideCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM slides").Scan(&slideCount); err != nil {
		t.Fatalf("count slides: %v", err)
	}
	if slideCount != 2 {
		t.Fatalf("expected 2 slides after gc, got %d", slideCount)
	}
}
