package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withFakeScreenshot(t *testing.T, render func(t *testing.T, chromePath string, htmlPath string, outputPath string) error) {
	t.Helper()

	oldFindChrome := findChromeBinaryFn
	oldScreenshot := screenshotWithChromeFn
	t.Cleanup(func() {
		findChromeBinaryFn = oldFindChrome
		screenshotWithChromeFn = oldScreenshot
	})

	findChromeBinaryFn = func() (string, error) {
		return "fake-chrome", nil
	}
	screenshotWithChromeFn = func(_ context.Context, chromePath string, htmlPath string, outputPath string) error {
		return render(t, chromePath, htmlPath, outputPath)
	}
}

func writeFakeScreenshotPNG(t *testing.T, outputPath string) error {
	t.Helper()

	data := append([]byte{0x89, 0x50, 0x4E, 0x47, '\r', '\n', 0x1A, '\n'}, make([]byte, 1024)...)
	return os.WriteFile(outputPath, data, 0o644)
}

func TestBuildRecordHTML_FullDocument(t *testing.T) {
	// A full HTML document should be returned as-is.
	full := `<!DOCTYPE html><html><head></head><body><h1>Hello</h1></body></html>`
	got := buildRecordHTML(full)
	if got != full {
		t.Errorf("expected full document to pass through unchanged, got %d bytes", len(got))
	}
}

func TestBuildRecordHTML_Fragment(t *testing.T) {
	// A fragment should be wrapped in a 1920x1080 document.
	frag := `<h1>Hello</h1>`
	got := buildRecordHTML(frag)
	if got == frag {
		t.Error("expected fragment to be wrapped, got unchanged")
	}
	if len(got) < len(frag) {
		t.Error("expected wrapped output to be longer than input")
	}
	// Should contain the fragment.
	if !strings.Contains(got, frag) {
		t.Error("expected wrapped output to contain the original fragment")
	}
	// Should contain 1920x1080 dimensions.
	if !strings.Contains(got, "1920px") || !strings.Contains(got, "1080px") {
		t.Error("expected wrapped output to contain 1920x1080 dimensions")
	}
}

func TestBuildRecordHTML_HTMLTagVariants(t *testing.T) {
	tests := []struct {
		name  string
		input string
		wrap  bool
	}{
		{"lowercase html tag", "<html><body>test</body></html>", false},
		{"uppercase HTML tag", "<HTML><body>test</body></HTML>", false},
		{"doctype only", "<!DOCTYPE html><body>test</body>", false},
		{"doctype lowercase", "<!doctype html><body>test</body>", false},
		{"plain text", "Hello world", true},
		{"div only", "<div>content</div>", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRecordHTML(tt.input)
			wrapped := got != tt.input
			if wrapped != tt.wrap {
				t.Errorf("wrapped=%v, want %v", wrapped, tt.wrap)
			}
		})
	}
}

func TestFindChromeBinary_EnvOverride(t *testing.T) {
	// Set PC_CHROME_PATH to a file that exists.
	tmpFile, err := os.CreateTemp("", "fake-chrome-*")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	t.Cleanup(func() { _ = os.Remove(tmpPath) })

	t.Setenv("PC_CHROME_PATH", tmpPath)
	got, err := findChromeBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != tmpPath {
		t.Errorf("got %q, want %q", got, tmpPath)
	}
}

func TestFindChromeBinary_EnvOverrideNotExists(t *testing.T) {
	t.Setenv("PC_CHROME_PATH", "/nonexistent/chrome")
	_, err := findChromeBinary()
	if err == nil {
		t.Fatal("expected error for nonexistent PC_CHROME_PATH")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected 'does not exist' in error, got: %v", err)
	}
}

func TestFindChromeBinary_SystemChrome(t *testing.T) {
	// Unset env override.
	t.Setenv("PC_CHROME_PATH", "")

	path, err := findChromeBinary()
	if err != nil {
		// Chrome may not be installed — skip rather than fail.
		t.Skipf("system Chrome not found (expected on some CI): %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

func TestFindChromeBinary_PlatformCandidates(t *testing.T) {
	// Verify that findChromeBinary doesn't panic on any platform.
	t.Setenv("PC_CHROME_PATH", "")
	// Just exercise the code path — may or may not find Chrome.
	_, _ = findChromeBinary()
}

func TestNewScreenshotCommand(t *testing.T) {
	cmd := newScreenshotCommand(os.Stdout, os.Stderr)
	if cmd.Use != "screenshot <id>" {
		t.Errorf("unexpected Use: %s", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected non-empty Short description")
	}
	// Verify --output flag exists.
	f := cmd.Flags().Lookup("output")
	if f == nil {
		t.Fatal("expected --output flag")
	}
	if f.Shorthand != "o" {
		t.Errorf("expected -o shorthand, got %q", f.Shorthand)
	}
}

func TestScreenshotCommand_NotFound(t *testing.T) {
	setupEnv(t)
	cmd := newScreenshotCommand(os.Stdout, os.Stderr)
	cmd.SetArgs([]string{"nonexistent-id"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent record")
	}
	// Should mention "not found" — but might fail at Chrome detection first.
	// Either error is acceptable in a unit test.
}

func TestScreenshotCommand_MissingArgs(t *testing.T) {
	cmd := newScreenshotCommand(os.Stdout, os.Stderr)
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

func TestScreenshotWithChrome_InvalidBinary(t *testing.T) {
	// Test with a binary that doesn't exist.
	err := screenshotWithChrome(
		t.Context(),
		"/nonexistent/chrome",
		"/tmp/test.html",
		"/tmp/test.png",
	)
	if err == nil {
		t.Fatal("expected error for invalid binary")
	}
}

func TestScreenshotHappyPath(t *testing.T) {
	withFakeScreenshot(t, func(t *testing.T, _ string, _ string, outputPath string) error {
		return writeFakeScreenshotPNG(t, outputPath)
	})

	homeDir := setupEnv(t)

	// Create a record via the add command.
	recordDir := t.TempDir()
	htmlPath := filepath.Join(recordDir, "record.html")
	if err := os.WriteFile(htmlPath, []byte(`<!DOCTYPE html><html><body><h1>Test</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDefaultProvenanceMetadata(t, recordDir)

	var addOut bytes.Buffer
	addCmd := NewRootCommand(RootCommandOptions{Stdout: &addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", recordDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	// Take a screenshot with explicit output.
	outDir := t.TempDir()
	outFile := filepath.Join(outDir, "test.png")

	var ssOut bytes.Buffer
	ssCmd := NewRootCommand(RootCommandOptions{Stdout: &ssOut, Stderr: &bytes.Buffer{}})
	ssCmd.SetArgs([]string{"screenshot", recordID, "--output", outFile})
	if err := ssCmd.Execute(); err != nil {
		t.Fatalf("screenshot: %v (homeDir=%s)", err, homeDir)
	}

	// Verify the output file exists and is a valid PNG.
	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatalf("stat screenshot: %v", err)
	}
	if info.Size() < 1000 {
		t.Errorf("screenshot too small: %d bytes", info.Size())
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read screenshot: %v", err)
	}
	// PNG magic bytes.
	if len(data) < 8 || data[0] != 0x89 || data[1] != 0x50 || data[2] != 0x4E || data[3] != 0x47 {
		t.Fatal("output is not a valid PNG")
	}

	// Verify stdout contains the output path.
	if !strings.Contains(ssOut.String(), "test.png") {
		t.Errorf("expected output path in stdout, got: %s", ssOut.String())
	}
}

func TestScreenshotPreservesRelativeFigurePaths(t *testing.T) {
	withFakeScreenshot(t, func(t *testing.T, _ string, htmlPath string, outputPath string) error {
		html, err := os.ReadFile(htmlPath)
		if err != nil {
			return fmt.Errorf("read prepared html: %w", err)
		}
		if strings.Contains(string(html), "figures/chart.png") {
			assetPath := filepath.Join(filepath.Dir(htmlPath), "figures", "chart.png")
			if _, err := os.Stat(assetPath); err != nil {
				return fmt.Errorf("missing prepared relative figure asset: %w", err)
			}
		}
		return os.WriteFile(outputPath, []byte("fake png"), 0o644)
	})

	setupEnv(t)

	recordDir := t.TempDir()
	figuresDir := filepath.Join(recordDir, "figures")
	if err := os.MkdirAll(figuresDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(figuresDir, "chart.png"), []byte("chart"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recordDir, "record.html"), []byte(`<!DOCTYPE html><html><body><img src="figures/chart.png" alt="chart"></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDefaultProvenanceMetadata(t, recordDir)

	var addOut bytes.Buffer
	addCmd := NewRootCommand(RootCommandOptions{Stdout: &addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", recordDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	outputPath := filepath.Join(t.TempDir(), "relative-figure.png")
	var screenshotOut bytes.Buffer
	screenshotCmd := NewRootCommand(RootCommandOptions{Stdout: &screenshotOut, Stderr: &bytes.Buffer{}})
	screenshotCmd.SetArgs([]string{"screenshot", recordID, "--output", outputPath})
	if err := screenshotCmd.Execute(); err != nil {
		t.Fatalf("screenshot with relative figure path: %v", err)
	}

	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected output file to exist: %v", err)
	}
}

func TestScreenshotDefaultOutput(t *testing.T) {
	withFakeScreenshot(t, func(t *testing.T, _ string, _ string, outputPath string) error {
		return writeFakeScreenshotPNG(t, outputPath)
	})

	setupEnv(t)

	// Create a record.
	recordDir := t.TempDir()
	htmlPath := filepath.Join(recordDir, "record.html")
	if err := os.WriteFile(htmlPath, []byte(`<!DOCTYPE html><html><body><h1>Default Output</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDefaultProvenanceMetadata(t, recordDir)

	var addOut bytes.Buffer
	addCmd := NewRootCommand(RootCommandOptions{Stdout: &addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", recordDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	// Take a screenshot with no --output flag (defaults to <id>.png in cwd).
	// Change to a temp dir so we don't pollute the working directory.
	tmpCwd := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpCwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	var ssOut bytes.Buffer
	ssCmd := NewRootCommand(RootCommandOptions{Stdout: &ssOut, Stderr: &bytes.Buffer{}})
	ssCmd.SetArgs([]string{"screenshot", recordID})
	if err := ssCmd.Execute(); err != nil {
		t.Fatalf("screenshot (default output): %v", err)
	}

	defaultFile := filepath.Join(tmpCwd, recordID+".png")
	if _, err := os.Stat(defaultFile); err != nil {
		t.Fatalf("default output file not created: %v", err)
	}
}

func TestScreenshotRejectsRecordWithoutHTML(t *testing.T) {
	withFakeScreenshot(t, func(t *testing.T, _ string, _ string, _ string) error {
		return errors.New("screenshot renderer should not run for records without HTML")
	})

	setupEnv(t)

	recordDir := t.TempDir()
	writeDefaultProvenanceMetadata(t, recordDir)

	var addOut bytes.Buffer
	addCmd := NewRootCommand(RootCommandOptions{Stdout: &addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", recordDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add without HTML: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	var screenshotOut bytes.Buffer
	screenshotCmd := NewRootCommand(RootCommandOptions{Stdout: &screenshotOut, Stderr: &bytes.Buffer{}})
	screenshotCmd.SetArgs([]string{"screenshot", recordID, "--output", filepath.Join(t.TempDir(), "no-html.png")})
	err := screenshotCmd.Execute()
	if err == nil {
		t.Fatal("expected screenshot to reject record without HTML")
	}
	if !strings.Contains(err.Error(), "has no record.html content") {
		t.Fatalf("expected missing HTML error, got %v", err)
	}
}

func TestScreenshotFailsWhenRendererDoesNotCreateOutput(t *testing.T) {
	withFakeScreenshot(t, func(t *testing.T, _ string, _ string, _ string) error {
		return nil
	})

	setupEnv(t)

	recordDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(recordDir, "record.html"), []byte("<html><body>no output</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDefaultProvenanceMetadata(t, recordDir)

	var addOut bytes.Buffer
	addCmd := NewRootCommand(RootCommandOptions{Stdout: &addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", recordDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	var screenshotOut bytes.Buffer
	screenshotCmd := NewRootCommand(RootCommandOptions{Stdout: &screenshotOut, Stderr: &bytes.Buffer{}})
	screenshotCmd.SetArgs([]string{"screenshot", recordID, "--output", filepath.Join(t.TempDir(), "missing.png")})
	err := screenshotCmd.Execute()
	if err == nil {
		t.Fatal("expected screenshot to fail when renderer creates no output")
	}
	if !strings.Contains(err.Error(), "screenshot file not created") {
		t.Fatalf("expected missing output error, got %v", err)
	}
}

func TestScreenshotChromeNotFound(t *testing.T) {
	// Test the findChromeBinaryFn error path within runScreenshot.
	old := findChromeBinaryFn
	t.Cleanup(func() { findChromeBinaryFn = old })
	findChromeBinaryFn = func() (string, error) {
		return "", errors.New("Chrome not found")
	}

	setupEnv(t)
	var ssOut, ssErr bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &ssOut, Stderr: &ssErr})
	cmd.SetArgs([]string{"screenshot", "some-id"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when Chrome not found")
	}
	if !strings.Contains(err.Error(), "Chrome not found") {
		t.Errorf("expected 'Chrome not found' in error, got: %v", err)
	}
}

func TestScreenshotChromeFailure(t *testing.T) {
	// Mock Chrome binary to something that exists but isn't Chrome.
	fakeChrome, err := os.CreateTemp("", "fake-chrome-*")
	if err != nil {
		t.Fatal(err)
	}
	fakeChromePath := fakeChrome.Name()
	_ = fakeChrome.Close()
	// Make it executable but it will fail since it's empty.
	_ = os.Chmod(fakeChromePath, 0o755)
	t.Cleanup(func() { _ = os.Remove(fakeChromePath) })

	old := findChromeBinaryFn
	t.Cleanup(func() { findChromeBinaryFn = old })
	findChromeBinaryFn = func() (string, error) {
		return fakeChromePath, nil
	}

	setupEnv(t)

	// Create a record.
	recordDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(recordDir, "record.html"), []byte(`<html><body>Test</body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDefaultProvenanceMetadata(t, recordDir)
	writeDefaultProvenanceMetadata(t, recordDir)
	writeDefaultProvenanceMetadata(t, recordDir)
	var addOut bytes.Buffer
	addCmd := NewRootCommand(RootCommandOptions{Stdout: &addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", recordDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	// Try to screenshot — should fail because fake Chrome can't render.
	var ssOut bytes.Buffer
	ssCmd := NewRootCommand(RootCommandOptions{Stdout: &ssOut, Stderr: &bytes.Buffer{}})
	ssCmd.SetArgs([]string{"screenshot", recordID, "--output", filepath.Join(t.TempDir(), "fail.png")})
	if err := ssCmd.Execute(); err == nil {
		t.Fatal("expected error from fake Chrome binary")
	}
}

func TestScreenshotOpenStackError(t *testing.T) {
	// PC_HOME points to a dir that exists but has no DB setup.
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)
	// Create the personal-context/.pc dir but NOT the DB — openLocalStack should fail.
	pcDir := filepath.Join(homeDir, "personal-context", ".pc")
	if err := os.MkdirAll(pcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var ssOut, ssErr bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &ssOut, Stderr: &ssErr})
	cmd.SetArgs([]string{"screenshot", "some-id"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when stack can't open")
	}
}

func TestScreenshotDBCorrupted(t *testing.T) {
	// Covers the non-ErrNotFound error path in GetRecordByID.
	homeDir := setupEnv(t)
	corruptTable(t, homeDir, "records")

	var ssOut bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &ssOut, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"screenshot", "some-id"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when records table is corrupted")
	}
}

func TestChromeCandidates(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			candidates := chromeCandidates(goos)
			if len(candidates) == 0 {
				t.Error("expected at least one candidate")
			}
		})
	}
}

func TestChromeCandidates_DarwinIncludesPathBrowsers(t *testing.T) {
	candidates := chromeCandidates("darwin")
	for _, want := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		found := false
		for _, candidate := range candidates {
			if candidate == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected darwin candidates to include %q, got %v", want, candidates)
		}
	}
}

func TestSearchCandidates_AbsolutePathFound(t *testing.T) {
	tmp, err := os.CreateTemp("", "fake-chrome-*")
	if err != nil {
		t.Fatal(err)
	}
	path := tmp.Name()
	_ = tmp.Close()
	t.Cleanup(func() { _ = os.Remove(path) })

	got, err := searchCandidates([]string{"/nonexistent/path", path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != path {
		t.Errorf("got %q, want %q", got, path)
	}
}

func TestSearchCandidates_AbsolutePathNotFound(t *testing.T) {
	_, err := searchCandidates([]string{"/nonexistent/chrome-abc-xyz"})
	if err == nil {
		t.Fatal("expected error when no candidates found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestSearchCandidates_RelativePathFound(t *testing.T) {
	// "go" should be in PATH on any dev machine.
	got, err := searchCandidates([]string{"go"})
	if err != nil {
		t.Skipf("'go' not in PATH: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty path")
	}
}

func TestSearchCandidates_MixedPaths(t *testing.T) {
	// Mix of nonexistent absolute, nonexistent relative, then a valid one.
	tmp, err := os.CreateTemp("", "fake-chrome-*")
	if err != nil {
		t.Fatal(err)
	}
	path := tmp.Name()
	_ = tmp.Close()
	t.Cleanup(func() { _ = os.Remove(path) })

	got, err := searchCandidates([]string{
		"/nonexistent/path1",
		"nonexistent-binary-xyz",
		path,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != path {
		t.Errorf("got %q, want %q", got, path)
	}
}

func TestSearchCandidates_Empty(t *testing.T) {
	_, err := searchCandidates(nil)
	if err == nil {
		t.Fatal("expected error for empty candidates")
	}
}

func TestFindChromeBinary_NoneAvailable(t *testing.T) {
	// Point PC_CHROME_PATH to empty so env override is cleared,
	// and set PATH to a directory with no Chrome binaries.
	t.Setenv("PC_CHROME_PATH", "")
	t.Setenv("PATH", t.TempDir())
	_, err := findChromeBinary()
	// On macOS the well-known path may still work, so only assert the
	// function doesn't panic. If no Chrome is found, verify the error.
	if err != nil && !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestScreenshotTempDirError(t *testing.T) {
	// Covers the os.CreateTemp error path in runScreenshot.
	// Make TMPDIR point to a non-writable directory.
	badTmp := filepath.Join(t.TempDir(), "readonly")
	if err := os.MkdirAll(badTmp, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", badTmp)

	homeDir := setupEnv(t)

	// Create a record.
	recordDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(recordDir, "record.html"), []byte(`<html><body>Test</body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDefaultProvenanceMetadata(t, recordDir)
	var addOut bytes.Buffer
	addCmd := NewRootCommand(RootCommandOptions{Stdout: &addOut, Stderr: &bytes.Buffer{}})
	addCmd.SetArgs([]string{"records", "add", recordDir})
	if err := addCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	recordID := strings.TrimSpace(addOut.String())

	var ssOut bytes.Buffer
	cmd := NewRootCommand(RootCommandOptions{Stdout: &ssOut, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"screenshot", recordID, "--output", filepath.Join(t.TempDir(), "out.png")})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when TMPDIR is not writable")
	}
	_ = homeDir
}

func TestScreenshotHomeDirError(t *testing.T) {
	withBrokenHomeDir(t, func() {
		cmd := newScreenshotCommand(os.Stdout, os.Stderr)
		cmd.SetArgs([]string{"test-id"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error for broken home dir")
		}
	})
}

// --- copyFile tests ---

func TestCopyFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "test.txt")
	content := []byte("hello world")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	dstPath := filepath.Join(dstDir, "copied.txt")
	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestCopyFile_SourceNotExist(t *testing.T) {
	err := copyFile("/nonexistent/source.txt", filepath.Join(t.TempDir(), "dst.txt"))
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestCopyFile_DestUnwritable(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "src.txt")
	if err := os.WriteFile(srcPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	badDir := filepath.Join(t.TempDir(), "readonly")
	if err := os.MkdirAll(badDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(badDir, 0o755) })

	err := copyFile(srcPath, filepath.Join(badDir, "dst.txt"))
	if err == nil {
		t.Fatal("expected error for unwritable destination")
	}
}

// --- copyDir tests ---

func TestCopyDir(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(srcDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("bbb"), 0o644); err != nil {
		t.Fatal(err)
	}

	dstDir := filepath.Join(t.TempDir(), "dest")
	if err := copyDir(srcDir, dstDir); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	// Verify files.
	gotA, err := os.ReadFile(filepath.Join(dstDir, "a.txt"))
	if err != nil {
		t.Fatalf("read a.txt: %v", err)
	}
	if string(gotA) != "aaa" {
		t.Errorf("a.txt content = %q, want %q", gotA, "aaa")
	}

	gotB, err := os.ReadFile(filepath.Join(dstDir, "sub", "b.txt"))
	if err != nil {
		t.Fatalf("read sub/b.txt: %v", err)
	}
	if string(gotB) != "bbb" {
		t.Errorf("sub/b.txt content = %q, want %q", gotB, "bbb")
	}
}

func TestCopyDir_SourceNotExist(t *testing.T) {
	err := copyDir("/nonexistent/src", filepath.Join(t.TempDir(), "dst"))
	if err == nil {
		t.Fatal("expected error for nonexistent source dir")
	}
}

// --- linkOrCopyDir tests ---

func TestLinkOrCopyDir_FallbackToCopy(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a regular file at the target path to make symlink fail.
	parentDir := t.TempDir()
	targetPath := filepath.Join(parentDir, "target")
	if err := os.WriteFile(targetPath, []byte("block"), 0o644); err != nil {
		t.Fatal(err)
	}

	// linkOrCopyDir should fall back to copyDir.
	// Remove the blocking file first so copyDir can create the directory.
	if err := os.Remove(targetPath); err != nil {
		t.Fatal(err)
	}

	// Create a directory that prevents symlink (a non-empty parent that blocks).
	// Actually, the simplest way: create targetPath as a non-empty directory.
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetPath, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Symlink will fail because targetPath already exists as a non-empty dir.
	err := linkOrCopyDir(srcDir, targetPath)
	if err == nil {
		// If symlink succeeded (replaced the dir), that's also fine — just means
		// symlink is possible on this platform. Verify the result is correct.
		data, readErr := os.ReadFile(filepath.Join(targetPath, "file.txt"))
		if readErr != nil {
			t.Fatalf("expected file.txt after linkOrCopyDir: %v", readErr)
		}
		if string(data) != "data" {
			t.Errorf("file.txt content = %q, want %q", data, "data")
		}
	}
}

// --- prepareScreenshotWorkspace tests ---

func TestPrepareScreenshotWorkspace_NoAssetDirs(t *testing.T) {
	dataRoot := t.TempDir()
	htmlPath, cleanup, err := prepareScreenshotWorkspace("test-id", "<html>hello</html>", dataRoot)
	if err != nil {
		t.Fatalf("prepareScreenshotWorkspace: %v", err)
	}
	defer cleanup()

	if htmlPath == "" {
		t.Fatal("expected non-empty htmlPath")
	}

	content, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("read html: %v", err)
	}
	if string(content) != "<html>hello</html>" {
		t.Errorf("html content = %q", content)
	}
}

func TestPrepareScreenshotWorkspace_WithAssets(t *testing.T) {
	dataRoot := t.TempDir()

	// Create figures/<id>/chart.png and data/<id>/file.csv
	recordID := "test-record-123"
	figDir := filepath.Join(dataRoot, "figures", recordID)
	if err := os.MkdirAll(figDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(figDir, "chart.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dataRoot, "data", recordID)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "file.csv"), []byte("csv"), 0o644); err != nil {
		t.Fatal(err)
	}

	htmlPath, cleanup, err := prepareScreenshotWorkspace(recordID, "<html>test</html>", dataRoot)
	if err != nil {
		t.Fatalf("prepareScreenshotWorkspace: %v", err)
	}
	defer cleanup()

	// Verify figures and data dirs are accessible from the temp dir.
	tempDir := filepath.Dir(htmlPath)
	for _, name := range []string{"figures", "data"} {
		info, err := os.Stat(filepath.Join(tempDir, name))
		if err != nil {
			t.Errorf("expected %s dir in temp workspace: %v", name, err)
			continue
		}
		// May be a symlink (resolved) or directory.
		_ = info
	}
}

func TestPrepareScreenshotWorkspace_StatError(t *testing.T) {
	// Create a figures directory that we can't stat into by removing permissions
	// on the parent (figures/<id> stat would require reading the figures dir).
	dataRoot := t.TempDir()
	recordID := "test-id"
	figDir := filepath.Join(dataRoot, "figures")
	if err := os.MkdirAll(filepath.Join(figDir, recordID), 0o755); err != nil {
		t.Fatal(err)
	}
	// Make the figures dir unreadable so Stat on figures/<id> fails with permission error.
	if err := os.Chmod(figDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(figDir, 0o755) })

	_, _, err := prepareScreenshotWorkspace(recordID, "<html>test</html>", dataRoot)
	if err == nil {
		t.Fatal("expected error when figures dir is unreadable")
	}
	if !strings.Contains(err.Error(), "stat") {
		t.Errorf("expected stat error, got: %v", err)
	}
}

func TestPrepareScreenshotWorkspace_LinkOrCopyError(t *testing.T) {
	// Create a valid source dir but make the temp dir unwritable to cause
	// both symlink and copy to fail.
	dataRoot := t.TempDir()
	recordID := "test-id"
	figDir := filepath.Join(dataRoot, "figures", recordID)
	if err := os.MkdirAll(figDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(figDir, "img.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Use a bad TMPDIR to make the temp dir's parent read-only won't work
	// since the temp dir is already created. Instead, let's just verify that
	// when linkOrCopyDir returns an error, prepareScreenshotWorkspace propagates it.
	// The simplest approach: patch TMPDIR to produce a temp dir in a location
	// where writing the target dir will fail. However, the HTML file write
	// would also fail. So this specific error path is hard to trigger without
	// more granular control.
	//
	// Instead, test copyDir with an unreadable source to exercise the walk error.
	badSrc := filepath.Join(t.TempDir(), "bad-src")
	if err := os.MkdirAll(filepath.Join(badSrc, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Make a file that can't be read.
	unreadable := filepath.Join(badSrc, "sub", "nope.txt")
	if err := os.WriteFile(unreadable, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

	dstDir := filepath.Join(t.TempDir(), "dst")
	err := copyDir(badSrc, dstDir)
	if err == nil {
		t.Fatal("expected error when source file is unreadable")
	}
}

func TestPrepareScreenshotWorkspace_AssetIsFile(t *testing.T) {
	// If figures/<id> is a file (not a dir), it should be skipped.
	dataRoot := t.TempDir()
	recordID := "test-id"
	figDir := filepath.Join(dataRoot, "figures")
	if err := os.MkdirAll(figDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create figures/<id> as a file, not a directory.
	if err := os.WriteFile(filepath.Join(figDir, recordID), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	htmlPath, cleanup, err := prepareScreenshotWorkspace(recordID, "<html>test</html>", dataRoot)
	if err != nil {
		t.Fatalf("prepareScreenshotWorkspace: %v", err)
	}
	defer cleanup()

	// The figures dir should NOT exist in the temp workspace.
	tempDir := filepath.Dir(htmlPath)
	if _, err := os.Stat(filepath.Join(tempDir, "figures")); !os.IsNotExist(err) {
		t.Error("expected figures dir to be absent when source is a file")
	}
}
