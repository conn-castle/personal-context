package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

// findChromeBinaryFn is the function used to locate Chrome. Variable for test overrides.
var findChromeBinaryFn = findChromeBinary

func newScreenshotCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var outputFlag string

	cmd := &cobra.Command{
		Use:   "screenshot <id>",
		Short: "Capture a PNG screenshot of a slide",
		Long:  "Renders the slide HTML at 1920x1080 using headless Chrome and saves it as a PNG.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScreenshot(cmd.Context(), stdout, stderr, args[0], outputFlag)
		},
	}

	cmd.Flags().StringVarP(&outputFlag, "output", "o", "", "Output file path (default: <id>.png in current directory)")

	return cmd
}

// findChromeBinary searches for Chrome or Chromium on the system.
// Returns the path to the binary or an error if not found.
func findChromeBinary() (string, error) {
	// Check environment variable override first.
	if p := os.Getenv("PC_CHROME_PATH"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("PC_CHROME_PATH %q does not exist: %w", p, err)
		}
		return p, nil
	}

	candidates := chromeCandidates(runtime.GOOS)
	return searchCandidates(candidates)
}

// chromeCandidates returns platform-specific Chrome/Chromium binary paths.
func chromeCandidates(goos string) []string {
	switch goos {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
		}
	case "linux":
		return []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
		}
	default:
		return []string{
			"chrome",
			"chromium",
		}
	}
}

// searchCandidates searches for the first available binary from the candidate list.
// Absolute paths are checked via os.Stat; relative names via exec.LookPath.
func searchCandidates(candidates []string) (string, error) {
	for _, c := range candidates {
		if filepath.IsAbs(c) {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf(
		"chrome or Chromium not found; install Chrome, or set PC_CHROME_PATH to the browser binary path",
	)
}

// buildSlideHTML wraps slide HTML content in a full 1920x1080 document suitable
// for headless Chrome screenshot capture.
func buildSlideHTML(htmlContent string) string {
	// If the content is a full document (has <html> or <!DOCTYPE>), use it as-is
	// but inject the viewport constraint.
	lower := strings.ToLower(htmlContent)
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype") {
		return htmlContent
	}

	// Fragment — wrap in a minimal 1920x1080 document.
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
html, body { width: 1920px; height: 1080px; overflow: hidden; background: white; }
</style>
</head>
<body>%s</body>
</html>`, htmlContent)
}

func prepareScreenshotWorkspace(slideID string, htmlContent string, dataRoot string) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "pc-screenshot-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	htmlPath := filepath.Join(tempDir, "slide.html")
	if err := os.WriteFile(htmlPath, []byte(htmlContent), 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write temp file: %w", err)
	}

	for _, assetDir := range []string{"figures", "data"} {
		sourceDir := filepath.Join(dataRoot, assetDir, slideID)
		info, err := os.Stat(sourceDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			cleanup()
			return "", nil, fmt.Errorf("stat %s directory: %w", assetDir, err)
		}
		if !info.IsDir() {
			continue
		}

		targetDir := filepath.Join(tempDir, assetDir)
		if err := linkOrCopyDir(sourceDir, targetDir); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("prepare %s directory: %w", assetDir, err)
		}
	}

	return htmlPath, cleanup, nil
}

func linkOrCopyDir(sourceDir string, targetDir string) error {
	if err := os.Symlink(sourceDir, targetDir); err == nil {
		return nil
	}
	return copyDir(sourceDir, targetDir)
}

func copyDir(sourceDir string, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	return filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceDir {
			return nil
		}

		relativePath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		destinationPath := filepath.Join(targetDir, relativePath)

		if entry.IsDir() {
			return os.MkdirAll(destinationPath, 0o755)
		}

		return copyFile(path, destinationPath)
	})
}

func copyFile(sourcePath string, destinationPath string) (err error) {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() { _ = sourceFile.Close() }()

	info, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return err
	}

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer func() {
		if cerr := destinationFile.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	_, err = io.Copy(destinationFile, sourceFile)
	return err
}

// screenshotWithChrome invokes headless Chrome to capture a screenshot.
func screenshotWithChrome(ctx context.Context, chromePath string, htmlPath string, outputPath string) error {
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-software-rasterizer",
		"--hide-scrollbars",
		fmt.Sprintf("--screenshot=%s", outputPath),
		"--window-size=1920,1080",
		fmt.Sprintf("file://%s", htmlPath),
	}

	cmd := exec.CommandContext(ctx, chromePath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("chrome screenshot failed: %w\n%s", err, string(out))
	}
	return nil
}

func runScreenshot(ctx context.Context, stdout io.Writer, _ io.Writer, id string, output string) error {
	// Find Chrome.
	chromePath, err := findChromeBinaryFn()
	if err != nil {
		return err
	}

	// Open local stack to access the repository.
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	// Get the slide.
	slide, err := stack.Repo.GetSlideByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("slide %q not found", id)
		}
		return fmt.Errorf("get slide: %w", err)
	}

	// Determine output path.
	if output == "" {
		output = id + ".png"
	}

	html := buildSlideHTML(slide.HTMLContent)
	htmlPath, cleanup, err := prepareScreenshotWorkspace(id, html, basePath(homeDir))
	if err != nil {
		return err
	}
	defer cleanup()

	// Capture screenshot.
	if err := screenshotWithChrome(ctx, chromePath, htmlPath, output); err != nil {
		return err
	}

	// Verify output exists.
	info, err := os.Stat(output)
	if err != nil {
		return fmt.Errorf("screenshot file not created: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "%s (%d bytes)\n", output, info.Size())
	return nil
}
