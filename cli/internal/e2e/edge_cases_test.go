package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEdgeCaseMinimalSlide(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Create input folder with only slide.html — no figures, notes, data, or metadata.
	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body><p>Bare slide</p></body></html>",
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)
	if slideID == "" {
		t.Fatal("expected slide ID in stdout")
	}

	// Verify text format works.
	textOut := runPCSuccess(t, homeDir, "show", slideID)
	if !strings.Contains(textOut, slideID) {
		t.Fatalf("show text output missing slide ID:\n%s", textOut)
	}

	// Verify JSON format: figures and data_files should be empty arrays.
	jsonOut := runPCSuccess(t, homeDir, "show", "--format", "json", slideID)
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, jsonOut)
	}

	figures, ok := result["figures"].([]interface{})
	if !ok {
		t.Fatalf("expected figures to be an array, got %T", result["figures"])
	}
	if len(figures) != 0 {
		t.Fatalf("expected 0 figures, got %d", len(figures))
	}

	dataFiles, ok := result["data_files"].([]interface{})
	if !ok {
		t.Fatalf("expected data_files to be an array, got %T", result["data_files"])
	}
	if len(dataFiles) != 0 {
		t.Fatalf("expected 0 data_files, got %d", len(dataFiles))
	}

	if result["notes"] != nil {
		t.Fatalf("expected notes to be null, got %v", result["notes"])
	}
	if result["project_id"] != nil {
		t.Fatalf("expected project_id to be null, got %v", result["project_id"])
	}
	if result["git_remote_url"] != nil {
		t.Fatalf("expected git_remote_url to be null, got %v", result["git_remote_url"])
	}
	if result["git_hash"] != nil {
		t.Fatalf("expected git_hash to be null, got %v", result["git_hash"])
	}
}

func TestEdgeCaseManyFigures(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Build 12 figures and HTML referencing all of them.
	figures := make(map[string][]byte)
	htmlParts := []string{"<html>"}
	for i := 1; i <= 12; i++ {
		name := fmt.Sprintf("fig_%02d.png", i)
		figures[name] = []byte(fmt.Sprintf("fake-png-%02d", i))
		htmlParts = append(htmlParts, fmt.Sprintf(`<img src="figures/%s">`, name))
	}
	htmlParts = append(htmlParts, "</html>")

	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: strings.Join(htmlParts, "\n"),
		Figures:     figures,
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	// Verify all 12 figure files exist on disk.
	for i := 1; i <= 12; i++ {
		name := fmt.Sprintf("fig_%02d.png", i)
		figPath := filepath.Join(homeDir, "personal-context", "figures", slideID, name)
		content, err := os.ReadFile(figPath)
		if err != nil {
			t.Fatalf("figure %s not found on disk: %v", name, err)
		}
		expected := fmt.Sprintf("fake-png-%02d", i)
		if string(content) != expected {
			t.Fatalf("figure %s: expected content %q, got %q", name, expected, string(content))
		}
	}

	// Verify DB has 12 figure rows.
	db := openTestDB(t, homeDir)
	var figCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM slide_figures WHERE slide_id = ?", slideID).Scan(&figCount); err != nil {
		t.Fatalf("query figures: %v", err)
	}
	if figCount != 12 {
		t.Fatalf("expected 12 figure rows in DB, got %d", figCount)
	}
}

func TestEdgeCaseSpecialCharactersInFilenames(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><img src="figures/my figure.png"><img src="figures/data-chart_v2.png"></html>`,
		Figures: map[string][]byte{
			"my figure.png":    []byte("fig-with-space"),
			"data-chart_v2.png": []byte("fig-with-hyphen-underscore"),
		},
		DataFiles: map[string][]byte{
			"results (final).csv": []byte("col1,col2\n1,2\n"),
		},
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	// Verify figure files with special characters exist on disk.
	for _, name := range []string{"my figure.png", "data-chart_v2.png"} {
		figPath := filepath.Join(homeDir, "personal-context", "figures", slideID, name)
		if _, err := os.Stat(figPath); err != nil {
			t.Fatalf("figure %q not found on disk: %v", name, err)
		}
	}

	// Verify data file with special characters exists on disk.
	dataPath := filepath.Join(homeDir, "personal-context", "data", slideID, "results (final).csv")
	if _, err := os.Stat(dataPath); err != nil {
		t.Fatalf("data file %q not found on disk: %v", "results (final).csv", err)
	}

	// Verify DB records.
	db := openTestDB(t, homeDir)
	var figCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM slide_figures WHERE slide_id = ?", slideID).Scan(&figCount); err != nil {
		t.Fatalf("query figures: %v", err)
	}
	if figCount != 2 {
		t.Fatalf("expected 2 figure rows, got %d", figCount)
	}

	var dataCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM slide_data_files WHERE slide_id = ?", slideID).Scan(&dataCount); err != nil {
		t.Fatalf("query data files: %v", err)
	}
	if dataCount != 1 {
		t.Fatalf("expected 1 data file row, got %d", dataCount)
	}
}

func TestEdgeCaseUnicodeInHTMLAndNotes(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	unicodeHTML := "<html><body>日本語テスト 🎉 naïve café</body></html>"
	unicodeNotes := "Notes with émojis 🚀 and ñ"

	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: unicodeHTML,
		Notes:       unicodeNotes,
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	// Verify via JSON that unicode round-trips correctly.
	jsonOut := runPCSuccess(t, homeDir, "show", "--format", "json", slideID)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, jsonOut)
	}

	gotHTML, _ := result["html_content"].(string)
	if gotHTML != unicodeHTML {
		t.Fatalf("html_content unicode mismatch:\nexpected: %s\ngot:      %s", unicodeHTML, gotHTML)
	}

	gotNotes, _ := result["notes"].(string)
	if gotNotes != unicodeNotes {
		t.Fatalf("notes unicode mismatch:\nexpected: %s\ngot:      %s", unicodeNotes, gotNotes)
	}
}

func TestEdgeCaseEmptyNotesFile(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// createInputFolder writes notes.md only if opts.Notes != "".
	// We need to manually create an empty notes.md file.
	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: "<html><body>Slide with empty notes</body></html>",
	})
	// Write an empty notes.md into the input folder.
	if err := os.WriteFile(filepath.Join(inputDir, "notes.md"), []byte(""), 0o644); err != nil {
		t.Fatalf("write empty notes.md: %v", err)
	}

	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	// Verify via JSON that notes is null (NormalizeString returns nil for empty).
	jsonOut := runPCSuccess(t, homeDir, "show", "--format", "json", slideID)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, jsonOut)
	}

	if result["notes"] != nil {
		t.Fatalf("expected notes to be null for empty notes.md, got %v", result["notes"])
	}
}

func TestEdgeCaseMultipleSlidesOnSameDate(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	date := "2025-06-01"
	slideIDs := make([]string, 5)

	for i := 0; i < 5; i++ {
		inputDir := createInputFolder(t, inputFolderOpts{
			HTMLContent: fmt.Sprintf("<html><body>Slide %d</body></html>", i+1),
		})
		stdout := runPCSuccess(t, homeDir, "add", "--date", date, inputDir)
		slideIDs[i] = strings.TrimSpace(stdout)
	}

	// Verify all 5 slides exist and have distinct, increasing day_orders.
	db := openTestDB(t, homeDir)

	dayOrders := make([]string, 5)
	for i, id := range slideIDs {
		var dayOrder string
		if err := db.QueryRow("SELECT day_order FROM slides WHERE id = ?", id).Scan(&dayOrder); err != nil {
			t.Fatalf("query day_order for slide %d (%s): %v", i+1, id, err)
		}
		dayOrders[i] = dayOrder
	}

	// Each subsequent slide (added as "last" by default) must have a strictly greater day_order.
	for i := 1; i < len(dayOrders); i++ {
		if dayOrders[i] <= dayOrders[i-1] {
			t.Fatalf("expected day_order[%d] > day_order[%d]: %q <= %q",
				i, i-1, dayOrders[i], dayOrders[i-1])
		}
	}

	// Verify the DB returns them in the expected order.
	rows, err := db.Query(
		"SELECT id FROM slides WHERE date = ? ORDER BY day_order ASC", date)
	if err != nil {
		t.Fatalf("query slides by date: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var orderedIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan slide id: %v", err)
		}
		orderedIDs = append(orderedIDs, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}

	if len(orderedIDs) != 5 {
		t.Fatalf("expected 5 slides on %s, got %d", date, len(orderedIDs))
	}
	for i, id := range slideIDs {
		if orderedIDs[i] != id {
			t.Fatalf("slide order mismatch at position %d: expected %s, got %s", i, id, orderedIDs[i])
		}
	}
}
