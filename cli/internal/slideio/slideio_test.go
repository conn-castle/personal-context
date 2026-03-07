package slideio

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseInputFolderMinimal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", "<h1>Hello</h1>")

	input, err := ParseInputFolder(dir)
	if err != nil {
		t.Fatalf("ParseInputFolder() error = %v", err)
	}
	if input.HTMLContent != "<h1>Hello</h1>" {
		t.Fatalf("unexpected HTML: %q", input.HTMLContent)
	}
	if input.Notes != nil {
		t.Fatalf("expected nil notes, got %q", *input.Notes)
	}
	if input.ProjectID != nil {
		t.Fatalf("expected nil project_id, got %q", *input.ProjectID)
	}
	if len(input.Figures) != 0 {
		t.Fatalf("expected no figures, got %d", len(input.Figures))
	}
	if len(input.DataFiles) != 0 {
		t.Fatalf("expected no data files, got %d", len(input.DataFiles))
	}
}

func TestParseInputFolderFull(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", `<html><img src="figures/plot.png" alt="Plot"></html>`)
	writeFile(t, dir, "notes.md", "Some notes\n")
	writeFile(t, dir, "metadata.json", `{"project_id":"my-project","git_remote_url":"https://github.com/org/repo","git_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)

	figDir := filepath.Join(dir, "figures")
	os.MkdirAll(figDir, 0o755)
	writeFile(t, figDir, "plot.png", "fake-png-data")

	dataDir := filepath.Join(dir, "data")
	os.MkdirAll(dataDir, 0o755)
	writeFile(t, dataDir, "metrics.csv", "col1,col2\n1,2\n")

	input, err := ParseInputFolder(dir)
	if err != nil {
		t.Fatalf("ParseInputFolder() error = %v", err)
	}
	if input.Notes == nil || *input.Notes != "Some notes" {
		t.Fatalf("unexpected notes: %v", input.Notes)
	}
	if input.ProjectID == nil || *input.ProjectID != "my-project" {
		t.Fatalf("unexpected project_id: %v", input.ProjectID)
	}
	if input.GitRemoteURL == nil || *input.GitRemoteURL != "https://github.com/org/repo" {
		t.Fatalf("unexpected git_remote_url: %v", input.GitRemoteURL)
	}
	if input.GitHash == nil || *input.GitHash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected git_hash: %v", input.GitHash)
	}
	if len(input.Figures) != 1 {
		t.Fatalf("expected 1 figure, got %d", len(input.Figures))
	}
	if filepath.Base(input.Figures[0]) != "plot.png" {
		t.Fatalf("unexpected figure: %s", input.Figures[0])
	}
	if len(input.DataFiles) != 1 {
		t.Fatalf("expected 1 data file, got %d", len(input.DataFiles))
	}
}

func TestParseInputFolderMissingSlideHTML(t *testing.T) {
	dir := t.TempDir()
	// No slide.html

	_, err := ParseInputFolder(dir)
	if err == nil {
		t.Fatal("expected error for missing slide.html")
	}
}

func TestParseInputFolderInvalidMetadataJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", "<h1>Test</h1>")
	writeFile(t, dir, "metadata.json", "not-json")

	_, err := ParseInputFolder(dir)
	if err == nil {
		t.Fatal("expected error for invalid metadata.json")
	}
}

func TestParseInputFolderEmptyDir(t *testing.T) {
	_, err := ParseInputFolder("")
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}

func TestParseInputFolderNotADirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	writeFileAbs(t, f, "not a dir")

	_, err := ParseInputFolder(f)
	if err == nil {
		t.Fatal("expected error for non-directory path")
	}
}

func TestParseInputFolderNonexistentPath(t *testing.T) {
	_, err := ParseInputFolder(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestParseInputFolderWhitespaceOnlyNotes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", "<h1>Test</h1>")
	writeFile(t, dir, "notes.md", "   \n   \n")

	input, err := ParseInputFolder(dir)
	if err != nil {
		t.Fatalf("ParseInputFolder() error = %v", err)
	}
	if input.Notes != nil {
		t.Fatalf("expected nil notes for whitespace-only, got %q", *input.Notes)
	}
}

func TestParseInputFolderFigureRefValidation(t *testing.T) {
	dir := t.TempDir()
	// HTML references figures/plot.png but no figure file provided
	writeFile(t, dir, "slide.html", `<img src="figures/plot.png">`)

	_, err := ParseInputFolder(dir)
	if err == nil {
		t.Fatal("expected error for missing figure reference")
	}
}

func TestParseInputFolderFigureRefValid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", `<img src="figures/plot.png">`)

	figDir := filepath.Join(dir, "figures")
	os.MkdirAll(figDir, 0o755)
	writeFile(t, figDir, "plot.png", "data")

	_, err := ParseInputFolder(dir)
	if err != nil {
		t.Fatalf("ParseInputFolder() error = %v", err)
	}
}

func TestParseInputFolderMultipleFigureRefs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", `<img src="figures/a.png"><img src='figures/b.jpg'>`)

	figDir := filepath.Join(dir, "figures")
	os.MkdirAll(figDir, 0o755)
	writeFile(t, figDir, "a.png", "data")
	// Missing b.jpg

	_, err := ParseInputFolder(dir)
	if err == nil {
		t.Fatal("expected error for missing figure b.jpg")
	}
}

func TestParseInputFolderFigureDirSkipsSubdirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", "<h1>Test</h1>")

	figDir := filepath.Join(dir, "figures")
	os.MkdirAll(filepath.Join(figDir, "subdir"), 0o755)
	writeFile(t, figDir, "plot.png", "data")

	input, err := ParseInputFolder(dir)
	if err != nil {
		t.Fatalf("ParseInputFolder() error = %v", err)
	}
	if len(input.Figures) != 1 {
		t.Fatalf("expected 1 figure (subdir skipped), got %d", len(input.Figures))
	}
}

func TestParseInputFolderDataDirSkipsSubdirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", "<h1>Test</h1>")

	dataDir := filepath.Join(dir, "data")
	os.MkdirAll(filepath.Join(dataDir, "subdir"), 0o755)
	writeFile(t, dataDir, "metrics.csv", "col\n1\n")

	input, err := ParseInputFolder(dir)
	if err != nil {
		t.Fatalf("ParseInputFolder() error = %v", err)
	}
	if len(input.DataFiles) != 1 {
		t.Fatalf("expected 1 data file (subdir skipped), got %d", len(input.DataFiles))
	}
}

func TestParseInputFolderNotesReadError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", "<h1>Test</h1>")
	if err := os.Mkdir(filepath.Join(dir, "notes.md"), 0o755); err != nil {
		t.Fatalf("Mkdir notes.md: %v", err)
	}

	_, err := ParseInputFolder(dir)
	if err == nil {
		t.Fatal("expected error for unreadable notes.md")
	}
	if !strings.Contains(err.Error(), "read notes.md") {
		t.Fatalf("expected notes read error, got %v", err)
	}
}

func TestParseInputFolderMetadataReadError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", "<h1>Test</h1>")
	if err := os.Mkdir(filepath.Join(dir, "metadata.json"), 0o755); err != nil {
		t.Fatalf("Mkdir metadata.json: %v", err)
	}

	_, err := ParseInputFolder(dir)
	if err == nil {
		t.Fatal("expected error for unreadable metadata.json")
	}
	if !strings.Contains(err.Error(), "read metadata.json") {
		t.Fatalf("expected metadata read error, got %v", err)
	}
}

func TestParseInputFolderFiguresReadError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", "<h1>Test</h1>")
	writeFile(t, dir, "figures", "not-a-directory")

	_, err := ParseInputFolder(dir)
	if err == nil {
		t.Fatal("expected error for unreadable figures directory")
	}
	if !strings.Contains(err.Error(), "read figures directory") {
		t.Fatalf("expected figures read error, got %v", err)
	}
}

func TestParseInputFolderDataReadError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "slide.html", "<h1>Test</h1>")
	writeFile(t, dir, "data", "not-a-directory")

	_, err := ParseInputFolder(dir)
	if err == nil {
		t.Fatal("expected error for unreadable data directory")
	}
	if !strings.Contains(err.Error(), "read data directory") {
		t.Fatalf("expected data read error, got %v", err)
	}
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello world")
	path := filepath.Join(dir, "test.txt")
	writeFileAbs(t, path, string(content))

	expected := sha256.Sum256(content)
	expectedHex := hex.EncodeToString(expected[:])

	got, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile() error = %v", err)
	}
	if got != expectedHex {
		t.Fatalf("expected %q, got %q", expectedHex, got)
	}
}

func TestHashFileNonexistent(t *testing.T) {
	_, err := HashFile(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestValidateFigureRefsNoRefs(t *testing.T) {
	err := validateFigureRefs("<h1>No figures</h1>", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateFigureRefsExternalURLsIgnored(t *testing.T) {
	html := `<img src="https://example.com/image.png">`
	err := validateFigureRefs(html, nil)
	if err != nil {
		t.Fatalf("expected external URLs to be ignored, got %v", err)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func writeFileAbs(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
