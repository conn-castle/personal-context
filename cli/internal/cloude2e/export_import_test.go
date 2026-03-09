//go:build integration

package cloude2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type cloudSlideExportJSON struct {
	FormatVersion int                      `json:"format_version"`
	ID            string                   `json:"id"`
	Date          string                   `json:"date"`
	DayOrder      string                   `json:"day_order"`
	ProjectID     *string                  `json:"project_id,omitempty"`
	GitRemoteURL  *string                  `json:"git_remote_url,omitempty"`
	GitHash       *string                  `json:"git_hash,omitempty"`
	HasNotes      bool                     `json:"has_notes"`
	Figures       []cloudSlideFigureJSON   `json:"figures"`
	DataFiles     []cloudSlideDataFileJSON `json:"data_files"`
	CreatedAt     string                   `json:"created_at"`
	UpdatedAt     string                   `json:"updated_at"`
}

type cloudSlideFigureJSON struct {
	Filename string  `json:"filename"`
	S3Key    string  `json:"s3_key"`
	AltText  *string `json:"alt_text"`
}

type cloudSlideDataFileJSON struct {
	Filename    string  `json:"filename"`
	S3Key       string  `json:"s3_key"`
	Size        int64   `json:"size"`
	Hash        string  `json:"hash"`
	Description *string `json:"description"`
}

type cloudSlideDetailsJSON struct {
	ID           string                   `json:"id"`
	Date         string                   `json:"date"`
	DayOrder     string                   `json:"day_order"`
	HTMLContent  string                   `json:"html_content"`
	Notes        *string                  `json:"notes"`
	ProjectID    *string                  `json:"project_id"`
	GitRemoteURL *string                  `json:"git_remote_url"`
	GitHash      *string                  `json:"git_hash"`
	CreatedAt    string                   `json:"created_at"`
	UpdatedAt    string                   `json:"updated_at"`
	Figures      []cloudSlideFigureJSON   `json:"figures"`
	DataFiles    []cloudSlideDataFileJSON `json:"data_files"`
}

func TestExportFromCloudWritesGitTreeAndSkipsDeletedSlides(t *testing.T) {
	cloud := newCloudTestEnv(t)

	homeWriter, userWriter := setupCloudHome(t, cloud)
	homeExporter, userExporter := setupCloudHomeNoSchema(t, cloud)

	activeInput := createInputFolder(t,
		`<html><body><img src="figures/plot.png">cloud export payload</body></html>`,
		"cloud export notes",
		map[string][]byte{"plot.png": []byte("cloud-plot-bytes")},
		map[string][]byte{"metrics.csv": []byte("metric,value\naccuracy,0.95\n")},
	)
	activeID := strings.TrimSpace(runPCSuccessNoStderr(t, homeWriter, userWriter,
		"add", "--project", "phase7/cloud-export", activeInput))

	deletedInput := createInputFolder(t,
		"<html><body>deleted cloud export payload</body></html>",
		"",
		nil,
		nil,
	)
	deletedID := strings.TrimSpace(runPCSuccessNoStderr(t, homeWriter, userWriter, "add", deletedInput))
	runPCSuccessNoStderr(t, homeWriter, userWriter, "delete", deletedID)
	runPCSuccessNoStderr(t, homeWriter, userWriter, "sync")

	exportDir := t.TempDir()
	runPCSuccessNoStderr(t, homeExporter, userExporter, "export", "--from-cloud", "--path", exportDir)

	assertCloudTemplateExports(t, exportDir)

	metadata := readCloudSlideExportMetadata(t, exportDir, activeID)
	if metadata.FormatVersion != 1 {
		t.Fatalf("format_version = %d, want 1", metadata.FormatVersion)
	}
	if metadata.ProjectID == nil || *metadata.ProjectID != "phase7/cloud-export" {
		t.Fatalf("project_id = %#v, want phase7/cloud-export", metadata.ProjectID)
	}
	if !metadata.HasNotes {
		t.Fatal("cloud export did not mark notes as present")
	}
	if len(metadata.Figures) != 1 {
		t.Fatalf("figures = %d, want 1", len(metadata.Figures))
	}
	if len(metadata.DataFiles) != 1 {
		t.Fatalf("data_files = %d, want 1", len(metadata.DataFiles))
	}

	exportedFigure, err := os.ReadFile(filepath.Join(exportDir, "slides", activeID, "figures", "plot.png"))
	if err != nil {
		t.Fatalf("read exported cloud figure: %v", err)
	}
	if string(exportedFigure) != "cloud-plot-bytes" {
		t.Fatalf("exported cloud figure = %q", string(exportedFigure))
	}
	if _, err := os.Stat(filepath.Join(exportDir, "slides", activeID, "data")); !os.IsNotExist(err) {
		t.Fatalf("cloud export should not write data binaries into git tree, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(exportDir, "slides", deletedID)); !os.IsNotExist(err) {
		t.Fatalf("soft-deleted cloud slide %s should be excluded from export", deletedID)
	}
}

func TestImportFromCloudExportUsesUpdatedAtMergeRulesAndPreservesExternalURLs(t *testing.T) {
	cloud := newCloudTestEnv(t)

	homeWriter, userWriter := setupCloudHome(t, cloud)
	homeExporter, userExporter := setupCloudHomeNoSchema(t, cloud)

	sameID := strings.TrimSpace(runPCSuccessNoStderr(t, homeWriter, userWriter, "add", createInputFolder(t,
		`<html><body><img src="figures/same.png">same from cloud</body></html>`,
		"same cloud notes",
		map[string][]byte{"same.png": []byte("same-cloud-figure")},
		map[string][]byte{"same.csv": []byte("kind,value\nsame,1\n")},
	)))
	olderID := strings.TrimSpace(runPCSuccessNoStderr(t, homeWriter, userWriter, "add", createInputFolder(t,
		`<html><body><img src="figures/older.png">older from cloud</body></html>`,
		"older cloud notes",
		map[string][]byte{"older.png": []byte("older-cloud-figure")},
		map[string][]byte{"older.csv": []byte("kind,value\nolder,1\n")},
	)))
	newerID := strings.TrimSpace(runPCSuccessNoStderr(t, homeWriter, userWriter, "add", "--project", "phase7/cloud-import", createInputFolder(t,
		`<html><body><img src="https://example.com/external-cloud.png"><img src="figures/cloud-newer.png">newer from cloud</body></html>`,
		"newer cloud notes",
		map[string][]byte{"cloud-newer.png": []byte("newer-cloud-figure")},
		map[string][]byte{"cloud-newer.csv": []byte("kind,value\nnewer,1\n")},
	)))
	runPCSuccessNoStderr(t, homeWriter, userWriter, "sync")

	exportDir := t.TempDir()
	runPCSuccessNoStderr(t, homeExporter, userExporter, "export", "--from-cloud", "--path", exportDir)

	localHome, localUserHome := setupLocalOnlyHome(t)
	runPCSuccessNoStderr(t, localHome, localUserHome, "restore-db", exportDir)

	runPCSuccessNoStderr(t, localHome, localUserHome, "edit", sameID, createInputFolder(t,
		`<html><body><img src="figures/same-local.png">same local edit</body></html>`,
		"same local notes",
		map[string][]byte{"same-local.png": []byte("same-local-figure")},
		map[string][]byte{"same-local.csv": []byte("kind,value\nsame-local,2\n")},
	))
	runPCSuccessNoStderr(t, localHome, localUserHome, "edit", olderID, createInputFolder(t,
		`<html><body><img src="figures/older-local.png">older local edit</body></html>`,
		"older local notes",
		map[string][]byte{"older-local.png": []byte("older-local-figure")},
		map[string][]byte{"older-local.csv": []byte("kind,value\nolder-local,2\n")},
	))
	runPCSuccessNoStderr(t, localHome, localUserHome, "edit", newerID, createInputFolder(t,
		`<html><body><img src="https://example.com/external-local.png"><img src="figures/local-only.png">newer local edit</body></html>`,
		"newer local notes",
		map[string][]byte{"local-only.png": []byte("newer-local-figure")},
		map[string][]byte{"local-only.csv": []byte("kind,value\nnewer-local,2\n")},
	))

	sameAfterEdit := getCloudSlideDetails(t, localHome, localUserHome, sameID)
	olderAfterEdit := getCloudSlideDetails(t, localHome, localUserHome, olderID)
	newerAfterEdit := getCloudSlideDetails(t, localHome, localUserHome, newerID)

	rewriteCloudExportUpdatedAt(t, exportDir, sameID, sameAfterEdit.UpdatedAt)
	rewriteCloudExportUpdatedAt(t, exportDir, olderID, mustParseCloudTimestamp(t, olderAfterEdit.UpdatedAt).Add(-1*time.Minute).Format(time.RFC3339))
	rewriteCloudExportUpdatedAt(t, exportDir, newerID, mustParseCloudTimestamp(t, newerAfterEdit.UpdatedAt).Add(1*time.Minute).Format(time.RFC3339))

	runPCSuccessNoStderr(t, localHome, localUserHome, "import", exportDir)

	sameFinal := getCloudSlideDetails(t, localHome, localUserHome, sameID)
	if sameFinal.HTMLContent != `<html><body><img src="figures/same-local.png">same local edit</body></html>` {
		t.Fatalf("same-updated-at import should be skipped, html_content = %q", sameFinal.HTMLContent)
	}
	if sameFinal.Notes == nil || *sameFinal.Notes != "same local notes" {
		t.Fatalf("same-updated-at notes = %#v", sameFinal.Notes)
	}
	if got := fileNamesFromCloudFigures(sameFinal.Figures); !slices.Equal(got, []string{"same-local.png"}) {
		t.Fatalf("same-updated-at figures = %v, want [same-local.png]", got)
	}

	olderFinal := getCloudSlideDetails(t, localHome, localUserHome, olderID)
	if olderFinal.HTMLContent != `<html><body><img src="figures/older-local.png">older local edit</body></html>` {
		t.Fatalf("older import should be skipped, html_content = %q", olderFinal.HTMLContent)
	}
	if olderFinal.Notes == nil || *olderFinal.Notes != "older local notes" {
		t.Fatalf("older import notes = %#v", olderFinal.Notes)
	}
	if got := fileNamesFromCloudFigures(olderFinal.Figures); !slices.Equal(got, []string{"older-local.png"}) {
		t.Fatalf("older import figures = %v, want [older-local.png]", got)
	}

	newerFinal := getCloudSlideDetails(t, localHome, localUserHome, newerID)
	if newerFinal.HTMLContent != `<html><body><img src="https://example.com/external-cloud.png"><img src="figures/cloud-newer.png">newer from cloud</body></html>` {
		t.Fatalf("newer import html_content = %q", newerFinal.HTMLContent)
	}
	if newerFinal.Notes == nil || *newerFinal.Notes != "newer cloud notes" {
		t.Fatalf("newer import notes = %#v", newerFinal.Notes)
	}
	if newerFinal.ProjectID == nil || *newerFinal.ProjectID != "phase7/cloud-import" {
		t.Fatalf("newer import project_id = %#v", newerFinal.ProjectID)
	}
	if got := fileNamesFromCloudFigures(newerFinal.Figures); !slices.Equal(got, []string{"cloud-newer.png"}) {
		t.Fatalf("newer import figures = %v, want [cloud-newer.png]", got)
	}
	if got := fileNamesFromCloudDataFiles(newerFinal.DataFiles); !slices.Equal(got, []string{"cloud-newer.csv"}) {
		t.Fatalf("newer import data_files = %v, want [cloud-newer.csv]", got)
	}
	if _, err := os.Stat(filepath.Join(localHome, "personal-context", "figures", newerID, "local-only.png")); !os.IsNotExist(err) {
		t.Fatalf("local-only figure should be removed after newer import, stat err = %v", err)
	}
	newerFigure, err := os.ReadFile(filepath.Join(localHome, "personal-context", "figures", newerID, "cloud-newer.png"))
	if err != nil {
		t.Fatalf("read imported cloud figure: %v", err)
	}
	if string(newerFigure) != "newer-cloud-figure" {
		t.Fatalf("imported cloud figure = %q", string(newerFigure))
	}
	if _, err := os.Stat(filepath.Join(localHome, "personal-context", "data", newerID, "cloud-newer.csv")); !os.IsNotExist(err) {
		t.Fatalf("import should preserve only cloud data references, stat err = %v", err)
	}
}

func TestRestoreDBFromCloudExportSyncsIntoFreshCloud(t *testing.T) {
	sourceCloud := newCloudTestEnv(t)

	sourceWriter, sourceUser := setupCloudHome(t, sourceCloud)
	sourceExporter, sourceExporterUser := setupCloudHomeNoSchema(t, sourceCloud)

	activeID := strings.TrimSpace(runPCSuccessNoStderr(t, sourceWriter, sourceUser, "add", "--project", "phase7/cloud-restore", createInputFolder(t,
		`<html><body><img src="https://example.com/external-restore.png"><img src="figures/restore figure.png">cloud restore café 日本語</body></html>`,
		"restore cloud notes",
		map[string][]byte{"restore figure.png": []byte("restore-cloud-figure")},
		map[string][]byte{"restore.csv": []byte("kind,value\nrestore,1\n")},
	)))
	deletedID := strings.TrimSpace(runPCSuccessNoStderr(t, sourceWriter, sourceUser, "add", createInputFolder(t,
		"<html><body>deleted cloud restore slide</body></html>",
		"",
		nil,
		nil,
	)))
	runPCSuccessNoStderr(t, sourceWriter, sourceUser, "delete", deletedID)
	runPCSuccessNoStderr(t, sourceWriter, sourceUser, "sync")

	exportDir := t.TempDir()
	runPCSuccessNoStderr(t, sourceExporter, sourceExporterUser, "export", "--from-cloud", "--path", exportDir)

	targetCloud := newCloudTestEnv(t)
	targetWriter, targetUser := setupCloudHome(t, targetCloud)
	targetReader, targetReaderUser := setupCloudHomeNoSchema(t, targetCloud)

	runPCSuccessNoStderr(t, targetWriter, targetUser, "restore-db", exportDir)
	runPCSuccessNoStderr(t, targetWriter, targetUser, "sync")
	runPCSuccessNoStderr(t, targetReader, targetReaderUser, "sync")

	restored := getCloudSlideDetails(t, targetReader, targetReaderUser, activeID)
	if restored.HTMLContent != `<html><body><img src="https://example.com/external-restore.png"><img src="figures/restore figure.png">cloud restore café 日本語</body></html>` {
		t.Fatalf("restored cloud html_content = %q", restored.HTMLContent)
	}
	if restored.Notes == nil || *restored.Notes != "restore cloud notes" {
		t.Fatalf("restored cloud notes = %#v", restored.Notes)
	}
	if restored.ProjectID == nil || *restored.ProjectID != "phase7/cloud-restore" {
		t.Fatalf("restored cloud project_id = %#v", restored.ProjectID)
	}
	if got := fileNamesFromCloudFigures(restored.Figures); !slices.Equal(got, []string{"restore figure.png"}) {
		t.Fatalf("restored cloud figures = %v, want [restore figure.png]", got)
	}
	if got := fileNamesFromCloudDataFiles(restored.DataFiles); !slices.Equal(got, []string{"restore.csv"}) {
		t.Fatalf("restored cloud data_files = %v, want [restore.csv]", got)
	}
	restoredFigure, err := os.ReadFile(filepath.Join(targetReader, "personal-context", "figures", activeID, "restore figure.png"))
	if err != nil {
		t.Fatalf("read restored cloud figure: %v", err)
	}
	if string(restoredFigure) != "restore-cloud-figure" {
		t.Fatalf("restored cloud figure = %q", string(restoredFigure))
	}
	if _, err := os.Stat(filepath.Join(targetReader, "personal-context", "data", activeID, "restore.csv")); !os.IsNotExist(err) {
		t.Fatalf("restore-db cloud path should preserve only data references, stat err = %v", err)
	}

	stderr := runPCFailure(t, targetReader, targetReaderUser, "show", deletedID)
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("deleted cloud slide should stay excluded after restore-db cloud path, got %q", stderr)
	}
}

func TestVerifyFromCloudRoundTrip(t *testing.T) {
	cloud := newCloudTestEnv(t)

	homeWriter, userWriter := setupCloudHome(t, cloud)
	homeVerifier, userVerifier := setupCloudHomeNoSchema(t, cloud)

	runPCSuccessNoStderr(t, homeWriter, userWriter, "add", "--project", "phase7/cloud-verify", createInputFolder(t,
		`<html><body><img src="figures/verify.png">cloud verify</body></html>`,
		"cloud verify notes",
		map[string][]byte{"verify.png": []byte("verify-cloud-figure")},
		map[string][]byte{"verify.csv": []byte("metric,value\nloss,0.05\n")},
	))
	runPCSuccessNoStderr(t, homeWriter, userWriter, "sync")

	stdout := runPCSuccessNoStderr(t, homeVerifier, userVerifier, "verify", "--from-cloud")
	if !strings.Contains(stdout, "Cloud round-trip verification passed") {
		t.Fatalf("verify output = %q, want success message", stdout)
	}
}

func readCloudSlideExportMetadata(t *testing.T, exportDir string, slideID string) cloudSlideExportJSON {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(exportDir, "slides", slideID, "metadata.json"))
	if err != nil {
		t.Fatalf("read cloud export metadata for %s: %v", slideID, err)
	}

	var result cloudSlideExportJSON
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse cloud export metadata for %s: %v\nraw: %s", slideID, err, string(raw))
	}
	return result
}

func assertCloudTemplateExports(t *testing.T, exportDir string) {
	t.Helper()

	for _, name := range []string{"text-only.html", "single-image.html"} {
		path := filepath.Join(exportDir, "templates", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read exported template %s: %v", name, err)
		}
		if len(raw) == 0 {
			t.Fatalf("exported template %s is empty", name)
		}
	}
}

func setupLocalOnlyHome(t *testing.T) (homeDir string, userHome string) {
	t.Helper()

	homeDir = t.TempDir()
	userHome = t.TempDir()

	result := runPCWithEnv(t, homeDir, userHome, strings.NewReader("n\n"), "setup")
	if result.ExitCode != 0 {
		t.Fatalf("local setup failed (exit %d):\nstdout: %s\nstderr: %s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if strings.TrimSpace(result.Stderr) != "" {
		t.Fatalf("local setup wrote unexpected stderr:\n%s", result.Stderr)
	}

	return homeDir, userHome
}

func getCloudSlideDetails(t *testing.T, homeDir string, userHome string, slideID string) cloudSlideDetailsJSON {
	t.Helper()

	stdout := runPCSuccessNoStderr(t, homeDir, userHome, "show", "--format", "json", slideID)
	var result cloudSlideDetailsJSON
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse show json for %s: %v\nraw: %s", slideID, err, stdout)
	}
	return result
}

func rewriteCloudExportUpdatedAt(t *testing.T, exportDir string, slideID string, updatedAt string) {
	t.Helper()

	metadata := readCloudSlideExportMetadata(t, exportDir, slideID)
	metadata.UpdatedAt = updatedAt

	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatalf("marshal metadata for %s: %v", slideID, err)
	}
	if err := os.WriteFile(filepath.Join(exportDir, "slides", slideID, "metadata.json"), raw, 0o644); err != nil {
		t.Fatalf("write metadata for %s: %v", slideID, err)
	}
}

func mustParseCloudTimestamp(t *testing.T, raw string) time.Time {
	t.Helper()

	for _, layout := range []string{"2006-01-02T15:04:05.000Z", time.RFC3339, time.RFC3339Nano} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts
		}
	}
	t.Fatalf("parse timestamp %q: unsupported format", raw)
	return time.Time{}
}

func fileNamesFromCloudFigures(figures []cloudSlideFigureJSON) []string {
	names := make([]string, 0, len(figures))
	for _, figure := range figures {
		names = append(names, figure.Filename)
	}
	slices.Sort(names)
	return names
}

func fileNamesFromCloudDataFiles(dataFiles []cloudSlideDataFileJSON) []string {
	names := make([]string, 0, len(dataFiles))
	for _, dataFile := range dataFiles {
		names = append(names, dataFile.Filename)
	}
	slices.Sort(names)
	return names
}
