package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"

	_ "modernc.org/sqlite"
)

// setupEnv is a shared helper for coverage tests — runs pc setup and returns homeDir.
func setupEnv(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	ensureRegisteredProjectAndDevice(t, "test/default-project", "test-device")
	return homeDir
}

func writeDefaultProvenanceMetadata(t *testing.T, dir string) {
	t.Helper()
	ensureRegisteredProjectAndDevice(t, "test/default-project", "test-device")
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"project_id":"test/default-project","source_device_id":"test-device"}`), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
}

// addRecord is a helper that adds a minimal record and returns the record ID.
func addRecord(t *testing.T, extraArgs ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "record.html"), []byte("<html>Test</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	extraArgs = withDefaultProvenanceArgs(t, "", extraArgs)
	args := append([]string{"add"}, extraArgs...)
	args = append(args, dir)
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	return strings.TrimSpace(stdout.String())
}

// addRecordWithContent adds a record with specific HTML, notes, and optional metadata/figures.
func addRecordWithContent(t *testing.T, html, notes, metadata string, figures map[string][]byte, dataFiles map[string][]byte, extraArgs ...string) string {
	t.Helper()
	dir := t.TempDir()
	if html == "" {
		html = "<html>Test</html>"
	}
	if err := os.WriteFile(filepath.Join(dir, "record.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	if notes != "" {
		if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(notes), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if metadata != "" {
		if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metadata), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if len(figures) > 0 {
		figDir := filepath.Join(dir, "figures")
		if err := os.MkdirAll(figDir, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, data := range figures {
			if err := os.WriteFile(filepath.Join(figDir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(dataFiles) > 0 {
		dataDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, data := range dataFiles {
			if err := os.WriteFile(filepath.Join(dataDir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	stdout := &bytes.Buffer{}
	extraArgs = withDefaultProvenanceArgs(t, metadata, extraArgs)
	args := append([]string{"add"}, extraArgs...)
	args = append(args, dir)
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add with content: %v", err)
	}
	return strings.TrimSpace(stdout.String())
}

func withDefaultProvenanceArgs(t *testing.T, metadata string, args []string) []string {
	t.Helper()
	const defaultProjectID = "test/default-project"
	const defaultDeviceID = "test-device"

	projectID := projectArgValue(args)
	deviceID := deviceArgValue(args)
	if projectID == "" || deviceID == "" {
		var meta struct {
			ProjectID      string `json:"project_id"`
			SourceDeviceID string `json:"source_device_id"`
		}
		if metadata != "" {
			_ = json.Unmarshal([]byte(metadata), &meta)
		}
		if projectID == "" {
			projectID = meta.ProjectID
		}
		if deviceID == "" {
			deviceID = meta.SourceDeviceID
		}
	}
	if projectID == "" {
		projectID = defaultProjectID
		args = append([]string{"--project", projectID}, args...)
	}
	if deviceID == "" {
		deviceID = defaultDeviceID
		args = append([]string{"--device", deviceID}, args...)
	}
	ensureRegisteredProjectAndDevice(t, projectID, deviceID)
	return args
}

func projectArgValue(args []string) string {
	for i, arg := range args {
		if arg == "--project" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--project=") {
			return strings.TrimPrefix(arg, "--project=")
		}
	}
	return ""
}

func deviceArgValue(args []string) string {
	for i, arg := range args {
		if arg == "--device" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--device=") {
			return strings.TrimPrefix(arg, "--device=")
		}
	}
	return ""
}

func ensureRegisteredProjectAndDevice(t *testing.T, projectID string, deviceID string) {
	t.Helper()
	stack, err := openLocalStack(os.Getenv("PC_HOME"))
	if err != nil {
		t.Fatalf("open stack for registry setup: %v", err)
	}
	defer func() { _ = stack.Close() }()
	ctx := context.Background()
	if _, err := stack.Repo.GetProjectByID(ctx, projectID); err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("get project %s: %v", projectID, err)
		}
		if _, err := stack.Repo.CreateProject(ctx, repository.CreateRegistryInput{ID: projectID}); err != nil {
			t.Fatalf("create project %s: %v", projectID, err)
		}
	}
	if _, err := stack.Repo.GetDeviceByID(ctx, deviceID); err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("get device %s: %v", deviceID, err)
		}
		if _, err := stack.Repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: deviceID}); err != nil {
			t.Fatalf("create device %s: %v", deviceID, err)
		}
	}
}

// getDayOrder returns the day_order field for a record by running show --format json.
func getDayOrder(t *testing.T, recordID string) string {
	t.Helper()
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"show", "--format", "json", recordID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show %s: %v", recordID, err)
	}
	var s recordJSON
	if err := json.Unmarshal(stdout.Bytes(), &s); err != nil {
		t.Fatalf("parse show json for %s: %v", recordID, err)
	}
	return s.DayOrder
}

// backdateDeletedAtUnit updates deleted_at in the DB for a record to simulate aging.
func backdateDeletedAtUnit(t *testing.T, homeDir string, recordID string, daysAgo int) {
	t.Helper()
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	past := time.Now().UTC().Add(-time.Duration(daysAgo) * 24 * time.Hour)
	ts := past.Format("2006-01-02T15:04:05.000Z")
	_, err = db.Exec(`UPDATE records SET deleted_at = ? WHERE id = ?`, ts, recordID)
	if err != nil {
		t.Fatalf("backdate deleted_at for %s: %v", recordID, err)
	}
}
