package e2e_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

var binaryPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "pc-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	binaryPath = filepath.Join(tmpDir, "pc")
	cmd := exec.Command("go", "build", "-o", binaryPath, "github.com/conn-castle/personal-context/cli/cmd/pc")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build pc binary: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	exitCode := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(exitCode)
}

// runResult captures the output of a pc command execution.
type runResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type runOptions struct {
	Stdin io.Reader
}

// runPC executes the pc binary with the given args and PC_HOME set to homeDir.
func runPC(t *testing.T, homeDir string, args ...string) runResult {
	t.Helper()
	return runPCWithOptions(t, homeDir, runOptions{}, args...)
}

// runPCWithOptions executes the pc binary with the provided options and PC_HOME set to homeDir.
func runPCWithOptions(t *testing.T, homeDir string, opts runOptions, args ...string) runResult {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "PC_HOME="+homeDir)
	cmd.Stdin = opts.Stdin

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run pc: %v", err)
		}
	}

	return runResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// runPCSuccess runs pc and fails the test if exit code is non-zero.
func runPCSuccess(t *testing.T, homeDir string, args ...string) string {
	t.Helper()
	if len(args) > 0 && args[0] == "add" {
		ensureRegistryForAdd(t, homeDir, args[1:])
	}
	if len(args) > 0 && args[0] == "edit" {
		ensureRegistryForEdit(t, homeDir, args[1:])
	}
	result := runPC(t, homeDir, args...)
	if result.ExitCode != 0 {
		t.Fatalf("pc %v failed (exit %d):\nstdout: %s\nstderr: %s",
			args, result.ExitCode, result.Stdout, result.Stderr)
	}
	return result.Stdout
}

// runPCFailure runs pc and fails the test if exit code is zero.
func runPCFailure(t *testing.T, homeDir string, args ...string) string {
	t.Helper()
	result := runPC(t, homeDir, args...)
	if result.ExitCode == 0 {
		t.Fatalf("pc %v unexpectedly succeeded:\nstdout: %s", args, result.Stdout)
	}
	return result.Stderr
}

// openTestDB opens the SQLite database for the given home directory.
func openTestDB(t *testing.T, homeDir string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// queryRowCount returns the count of rows in a table.
func queryRowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

// createInputFolder creates a temp folder suitable for `pc add` input.
func createInputFolder(t *testing.T, opts inputFolderOpts) string {
	t.Helper()
	dir := t.TempDir()

	if opts.HTMLContent == "" {
		opts.HTMLContent = "<html><body><h1>Test Record</h1></body></html>"
	}

	if err := os.WriteFile(filepath.Join(dir, "record.html"), []byte(opts.HTMLContent), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}

	if opts.Notes != "" {
		if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(opts.Notes), 0o644); err != nil {
			t.Fatalf("write notes.md: %v", err)
		}
	}

	if opts.MetadataJSON != "" {
		opts.MetadataJSON = mergeDefaultProvenanceMetadata(t, opts.MetadataJSON)
	} else {
		opts.MetadataJSON = `{"project_id":"test/default-project","source_device_id":"test-device"}`
	}
	if opts.MetadataJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(opts.MetadataJSON), 0o644); err != nil {
			t.Fatalf("write metadata.json: %v", err)
		}
	}

	if len(opts.Figures) > 0 {
		figDir := filepath.Join(dir, "figures")
		if err := os.MkdirAll(figDir, 0o755); err != nil {
			t.Fatalf("create figures dir: %v", err)
		}
		for name, content := range opts.Figures {
			if err := os.WriteFile(filepath.Join(figDir, name), content, 0o644); err != nil {
				t.Fatalf("write figure %s: %v", name, err)
			}
		}
	}

	if len(opts.DataFiles) > 0 {
		dataDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			t.Fatalf("create data dir: %v", err)
		}
		for name, content := range opts.DataFiles {
			if err := os.WriteFile(filepath.Join(dataDir, name), content, 0o644); err != nil {
				t.Fatalf("write data file %s: %v", name, err)
			}
		}
	}

	return dir
}

// inputFolderOpts configures a test input folder.
type inputFolderOpts struct {
	HTMLContent  string
	Notes        string
	MetadataJSON string
	Figures      map[string][]byte
	DataFiles    map[string][]byte
}

func mergeDefaultProvenanceMetadata(t *testing.T, metadata string) string {
	t.Helper()
	var meta map[string]any
	if err := json.Unmarshal([]byte(metadata), &meta); err != nil {
		return metadata
	}
	if _, ok := meta["project_id"]; !ok {
		meta["project_id"] = "test/default-project"
	}
	if _, ok := meta["source_device_id"]; !ok {
		meta["source_device_id"] = "test-device"
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	return string(raw)
}

func ensureRegistryForAdd(t *testing.T, homeDir string, args []string) {
	t.Helper()
	projectID, deviceID, inputPath := parseAddProvenance(t, args)
	if inputPath != "" {
		metadataPath := filepath.Join(inputPath, "metadata.json")
		if data, err := os.ReadFile(metadataPath); err == nil {
			var meta struct {
				ProjectID      string `json:"project_id"`
				SourceDeviceID string `json:"source_device_id"`
			}
			if err := json.Unmarshal(data, &meta); err == nil {
				if projectID == "" {
					projectID = meta.ProjectID
				}
				if deviceID == "" {
					deviceID = meta.SourceDeviceID
				}
			}
		}
	}
	ensureRegistryEntry(t, homeDir, "project", "add", projectID)
	ensureRegistryEntry(t, homeDir, "device", "register", deviceID)
}

func ensureRegistryForEdit(t *testing.T, homeDir string, args []string) {
	t.Helper()
	if len(args) < 2 {
		return
	}
	inputPath := args[len(args)-1]
	metadataPath := filepath.Join(inputPath, "metadata.json")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return
	}
	var meta struct {
		ProjectID      string `json:"project_id"`
		SourceDeviceID string `json:"source_device_id"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return
	}
	ensureRegistryEntry(t, homeDir, "project", "add", meta.ProjectID)
	ensureRegistryEntry(t, homeDir, "device", "register", meta.SourceDeviceID)
}

func parseAddProvenance(t *testing.T, args []string) (string, string, string) {
	t.Helper()
	var projectID, deviceID, inputPath string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--project" && i+1 < len(args):
			projectID = args[i+1]
			i++
		case strings.HasPrefix(arg, "--project="):
			projectID = strings.TrimPrefix(arg, "--project=")
		case arg == "--device" && i+1 < len(args):
			deviceID = args[i+1]
			i++
		case strings.HasPrefix(arg, "--device="):
			deviceID = strings.TrimPrefix(arg, "--device=")
		case arg == "--date" || arg == "--after" || arg == "--before" || arg == "--source-ref":
			i++
		case strings.HasPrefix(arg, "--date=") || strings.HasPrefix(arg, "--after=") || strings.HasPrefix(arg, "--before=") || strings.HasPrefix(arg, "--source-ref="):
		case strings.HasPrefix(arg, "-"):
		default:
			inputPath = arg
		}
	}
	return projectID, deviceID, inputPath
}

func ensureRegistryEntry(t *testing.T, homeDir string, command string, subcommand string, id string) {
	t.Helper()
	if strings.TrimSpace(id) == "" {
		return
	}
	result := runPC(t, homeDir, command, subcommand, id)
	if result.ExitCode != 0 && !strings.Contains(result.Stderr, "already") && !strings.Contains(result.Stderr, "UNIQUE constraint failed") {
		t.Fatalf("pc [%s %s %s] failed (exit %d):\nstdout: %s\nstderr: %s",
			command, subcommand, id, result.ExitCode, result.Stdout, result.Stderr)
	}
}

func nonEmptyLines(s string) []string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
