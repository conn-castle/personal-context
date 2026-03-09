package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSearchMatchesHTMLContent(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder := createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>neural network training</body></html>",
	})
	stdout := runPCSuccess(t, homeDir, "add", folder, "--date", "2025-01-15")
	slideID := strings.TrimSpace(stdout)

	searchOut := runPCSuccess(t, homeDir, "search", "neural")
	if !strings.Contains(searchOut, slideID) {
		t.Fatalf("expected search output to contain slide ID %q, got:\n%s", slideID, searchOut)
	}
}

func TestSearchMatchesNotes(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder := createInputFolder(t, inputFolderOpts{
		Notes: "experiment alpha",
	})
	stdout := runPCSuccess(t, homeDir, "add", folder, "--date", "2025-01-16")
	slideID := strings.TrimSpace(stdout)

	searchOut := runPCSuccess(t, homeDir, "search", "alpha")
	if !strings.Contains(searchOut, slideID) {
		t.Fatalf("expected search output to contain slide ID %q, got:\n%s", slideID, searchOut)
	}
}

func TestSearchMatchesProjectID(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id": "happy-ai/sleep"}`,
	})
	stdout := runPCSuccess(t, homeDir, "add", folder, "--date", "2025-01-17")
	slideID := strings.TrimSpace(stdout)

	searchOut := runPCSuccess(t, homeDir, "search", "sleep")
	if !strings.Contains(searchOut, slideID) {
		t.Fatalf("expected search output to contain slide ID %q, got:\n%s", slideID, searchOut)
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder := createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>MachineLearning</body></html>",
	})
	stdout := runPCSuccess(t, homeDir, "add", folder, "--date", "2025-01-18")
	slideID := strings.TrimSpace(stdout)

	searchOut := runPCSuccess(t, homeDir, "search", "machinelearning")
	if !strings.Contains(searchOut, slideID) {
		t.Fatalf("expected case-insensitive match for slide ID %q, got:\n%s", slideID, searchOut)
	}
}

func TestSearchProjectFilter(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder1 := createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>shared keyword</body></html>",
		MetadataJSON: `{"project_id": "proj-alpha"}`,
	})
	stdout1 := runPCSuccess(t, homeDir, "add", folder1, "--date", "2025-03-02")
	id1 := strings.TrimSpace(stdout1)

	folder2 := createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>shared keyword</body></html>",
		MetadataJSON: `{"project_id": "proj-beta"}`,
	})
	stdout2 := runPCSuccess(t, homeDir, "add", folder2, "--date", "2025-03-02")
	id2 := strings.TrimSpace(stdout2)

	searchOut := runPCSuccess(t, homeDir, "search", "--format", "ids", "--project", "proj-alpha", "shared keyword")
	if !strings.Contains(searchOut, id1) {
		t.Fatalf("expected search to include slide %q from proj-alpha, got:\n%s", id1, searchOut)
	}
	if strings.Contains(searchOut, id2) {
		t.Fatalf("expected search to exclude slide %q from proj-beta, got:\n%s", id2, searchOut)
	}
}

func TestSearchDeletedFlagIncludesSoftDeleted(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder := createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>recoverable slide data</body></html>",
	})
	stdout := runPCSuccess(t, homeDir, "add", folder, "--date", "2025-03-04")
	slideID := strings.TrimSpace(stdout)

	runPCSuccess(t, homeDir, "delete", slideID)

	searchOut := runPCSuccess(t, homeDir, "search", "--format", "ids", "--deleted", "recoverable slide data")
	if !strings.Contains(searchOut, slideID) {
		t.Fatalf("expected --deleted flag to include soft-deleted slide %q, got:\n%s", slideID, searchOut)
	}
}

func TestSearchFormatJSON(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	folder := createInputFolder(t, inputFolderOpts{
		HTMLContent:  "<html><body>json output test</body></html>",
		MetadataJSON: `{"project_id": "json-proj"}`,
	})
	stdout := runPCSuccess(t, homeDir, "add", folder, "--date", "2025-02-03")
	slideID := strings.TrimSpace(stdout)

	searchOut := runPCSuccess(t, homeDir, "search", "--format", "json", "json output")

	var results []map[string]interface{}
	if err := json.Unmarshal([]byte(searchOut), &results); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, searchOut)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r["id"] != slideID {
		t.Fatalf("expected id=%s, got %v", slideID, r["id"])
	}
	if r["date"] != "2025-02-03" {
		t.Fatalf("expected date=2025-02-03, got %v", r["date"])
	}
	if r["project_id"] != "json-proj" {
		t.Fatalf("expected project_id=json-proj, got %v", r["project_id"])
	}
	if _, ok := r["day_order"]; !ok {
		t.Fatal("expected day_order in JSON output")
	}
	if r["deleted_at"] != nil {
		t.Fatalf("expected deleted_at to be null, got %v", r["deleted_at"])
	}
}

func TestSearchNoQueryArg(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	result := runPC(t, homeDir, "search")
	if result.ExitCode == 0 {
		t.Fatal("expected non-zero exit code when no query arg provided")
	}
}
