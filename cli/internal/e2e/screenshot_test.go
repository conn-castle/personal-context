package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// chromeAvailable returns true if a headless Chrome binary can be found.
func chromeAvailable() bool {
	// Check PC_CHROME_PATH first.
	if p := os.Getenv("PC_CHROME_PATH"); p != "" {
		_, err := os.Stat(p)
		return err == nil
	}
	// Check well-known macOS path.
	if _, err := os.Stat("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"); err == nil {
		return true
	}
	// Check PATH.
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

func TestScreenshot(t *testing.T) {
	if !chromeAvailable() {
		t.Skip("Chrome/Chromium not found — skipping screenshot e2e test")
	}

	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Create a slide with known content.
	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<!DOCTYPE html>
<html>
<head><style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { width: 1920px; height: 1080px; background: #1e293b; color: white;
       font-family: sans-serif; display: flex; align-items: center; justify-content: center; }
h1 { font-size: 120px; }
</style></head>
<body><h1>Screenshot Test</h1></body>
</html>`,
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	// Take a screenshot with explicit --output.
	outputDir := t.TempDir()
	explicitOut := filepath.Join(outputDir, "test-output.png")
	stdout = runPCSuccess(t, homeDir, "screenshot", slideID, "--output", explicitOut)

	// Verify output file exists and has reasonable size.
	info, err := os.Stat(explicitOut)
	if err != nil {
		t.Fatalf("screenshot file not created: %v", err)
	}
	if info.Size() < 1000 {
		t.Errorf("screenshot file too small (%d bytes), likely invalid", info.Size())
	}

	// Verify PNG magic bytes.
	data, err := os.ReadFile(explicitOut)
	if err != nil {
		t.Fatalf("read screenshot: %v", err)
	}
	pngMagic := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if len(data) < 8 {
		t.Fatal("screenshot file too short to be a valid PNG")
	}
	for i, b := range pngMagic {
		if data[i] != b {
			t.Fatalf("invalid PNG magic bytes at position %d: got 0x%02X, want 0x%02X", i, data[i], b)
		}
	}

	// Verify stdout mentions the output path and size.
	if !strings.Contains(stdout, "test-output.png") {
		t.Errorf("stdout should mention output path, got: %s", stdout)
	}

	t.Logf("screenshot created: %s (%d bytes)", explicitOut, info.Size())
}

func TestScreenshot_NotFound(t *testing.T) {
	if !chromeAvailable() {
		t.Skip("Chrome/Chromium not found — skipping screenshot e2e test")
	}

	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	stderr := runPCFailure(t, homeDir, "screenshot", "nonexistent-id")
	if !strings.Contains(stderr, "not found") {
		t.Errorf("expected 'not found' in stderr, got: %s", stderr)
	}
}

func TestScreenshot_NoArgs(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	stderr := runPCFailure(t, homeDir, "screenshot")
	if !strings.Contains(stderr, "accepts 1 arg") {
		t.Errorf("expected argument error, got: %s", stderr)
	}
}

func TestScreenshot_ShortFlag(t *testing.T) {
	if !chromeAvailable() {
		t.Skip("Chrome/Chromium not found — skipping screenshot e2e test")
	}

	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		HTMLContent: `<html><body><h1>Flag Test</h1></body></html>`,
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	slideID := strings.TrimSpace(stdout)

	outputDir := t.TempDir()
	out := filepath.Join(outputDir, "short-flag.png")
	runPCSuccess(t, homeDir, "screenshot", slideID, "-o", out)

	if _, err := os.Stat(out); err != nil {
		t.Fatalf("screenshot file not created with -o flag: %v", err)
	}
}
