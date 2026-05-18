package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestShowTextFormat(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		Notes:        "Important notes here",
		MetadataJSON: `{"project_id":"show-test"}`,
	})
	stdout := runPCSuccess(t, homeDir, "records", "add", inputDir)
	recordID := strings.TrimSpace(stdout)

	showOut := runPCSuccess(t, homeDir, "show", recordID)

	checks := []string{"ID:", recordID, "Date:", "Project:", "show-test", "Notes:", "Important notes here"}
	for _, check := range checks {
		if !strings.Contains(showOut, check) {
			t.Fatalf("expected output to contain %q, got:\n%s", check, showOut)
		}
	}
}

func TestShowJSONFormat(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/plot.png"></html>`,
		Figures:     map[string][]byte{"plot.png": []byte("data")},
		DataFiles:   map[string][]byte{"metrics.csv": []byte("a,b\n1,2\n")},
	})
	stdout := runPCSuccess(t, homeDir, "records", "add", inputDir)
	recordID := strings.TrimSpace(stdout)

	showOut := runPCSuccess(t, homeDir, "show", "--format", "json", recordID)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(showOut), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, showOut)
	}

	if result["id"] != recordID {
		t.Fatalf("expected id=%s, got %v", recordID, result["id"])
	}
	if result["html_content"] == nil {
		t.Fatal("expected html_content in JSON output")
	}

	figures, ok := result["figures"].([]interface{})
	if !ok || len(figures) != 1 {
		t.Fatalf("expected 1 figure in JSON, got %v", result["figures"])
	}

	dataFiles, ok := result["data_files"].([]interface{})
	if !ok || len(dataFiles) != 1 {
		t.Fatalf("expected 1 data file in JSON, got %v", result["data_files"])
	}
}

func TestShowNonexistentID(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	stderr := runPCFailure(t, homeDir, "show", "99999999-ffffffff")
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("expected not found error, got %q", stderr)
	}
}

func TestShowWithNoNotes(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "records", "add", inputDir)
	recordID := strings.TrimSpace(stdout)

	showOut := runPCSuccess(t, homeDir, "show", recordID)
	if !strings.Contains(showOut, "(none)") {
		t.Fatalf("expected (none) for notes, got:\n%s", showOut)
	}
}

func TestShowInvalidFormat(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "records", "add", inputDir)
	recordID := strings.TrimSpace(stdout)

	stderr := runPCFailure(t, homeDir, "show", "--format", "xml", recordID)
	if !strings.Contains(stderr, "unknown format") {
		t.Fatalf("expected format error, got %q", stderr)
	}
}
