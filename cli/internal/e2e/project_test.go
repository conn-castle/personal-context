package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectAddListArchiveRestore(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	stdout := runPCSuccess(t, homeDir, "project", "list")
	if !strings.Contains(stdout, "No projects registered.") {
		t.Fatalf("expected empty registry message, got %q", stdout)
	}

	runPCSuccess(t, homeDir, "project", "add", "alpha")
	runPCSuccess(t, homeDir, "project", "add", "beta")

	stdout = runPCSuccess(t, homeDir, "project", "list", "--all")
	if !strings.Contains(stdout, "alpha") || !strings.Contains(stdout, "beta") {
		t.Fatalf("expected alpha and beta in project list, got %q", stdout)
	}

	runPCSuccess(t, homeDir, "project", "archive", "alpha")
	stdout = runPCSuccess(t, homeDir, "project", "list", "--all")
	if !strings.Contains(stdout, "alpha (archived)") {
		t.Fatalf("expected archived marker for alpha, got %q", stdout)
	}

	runPCSuccess(t, homeDir, "project", "restore", "alpha")
	stdout = runPCSuccess(t, homeDir, "project", "list")
	if strings.Contains(stdout, "alpha (archived)") {
		t.Fatalf("expected alpha to be restored, got %q", stdout)
	}
}

func TestDeviceRegisterListArchiveRestore(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	stdout := runPCSuccess(t, homeDir, "device", "list")
	if !strings.Contains(stdout, "No devices registered.") {
		t.Fatalf("expected empty registry message, got %q", stdout)
	}

	runPCSuccess(t, homeDir, "device", "register", "laptop")
	runPCSuccess(t, homeDir, "device", "register", "desktop")

	stdout = runPCSuccess(t, homeDir, "device", "list", "--all")
	if !strings.Contains(stdout, "desktop") || !strings.Contains(stdout, "laptop") {
		t.Fatalf("expected registered devices in list, got %q", stdout)
	}

	runPCSuccess(t, homeDir, "device", "archive", "laptop")
	stdout = runPCSuccess(t, homeDir, "device", "list", "--all")
	if !strings.Contains(stdout, "laptop (archived)") {
		t.Fatalf("expected archived marker for laptop, got %q", stdout)
	}

	runPCSuccess(t, homeDir, "device", "restore", "laptop")
	stdout = runPCSuccess(t, homeDir, "device", "list")
	if strings.Contains(stdout, "laptop (archived)") {
		t.Fatalf("expected laptop to be restored, got %q", stdout)
	}
}

func TestAddRequiresRegisteredProjectAndDevice(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"missing-project","source_device_id":"missing-device"}`,
	})
	stderr := runPCFailure(t, homeDir, "add", inputDir)
	if !strings.Contains(stderr, "project") || !strings.Contains(stderr, "not registered") {
		t.Fatalf("expected missing project error, got %q", stderr)
	}
}

func TestAddUsesExplicitMetadataProjectAndDevice(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"alpha","source_device_id":"laptop"}`,
	})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	recordID := strings.TrimSpace(stdout)

	db := openTestDB(t, homeDir)
	var projectID string
	var deviceID string
	if err := db.QueryRow("SELECT project_id, source_device_id FROM records WHERE id = ?", recordID).Scan(&projectID, &deviceID); err != nil {
		t.Fatalf("query provenance: %v", err)
	}
	if projectID != "alpha" || deviceID != "laptop" {
		t.Fatalf("expected alpha/laptop provenance, got %q/%q", projectID, deviceID)
	}
}

func TestProjectFlagMustMatchMetadata(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	inputDir := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"from-metadata","source_device_id":"test-device"}`,
	})
	stderr := runPCFailure(t, homeDir, "add", "--project", "from-flag", inputDir)
	if !strings.Contains(stderr, "project_id conflict") {
		t.Fatalf("expected metadata conflict, got %q", stderr)
	}
}

func TestArchivedProjectRejectedForNewAdd(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")
	runPCSuccess(t, homeDir, "project", "add", "archived-proj")
	runPCSuccess(t, homeDir, "device", "register", "test-device")
	runPCSuccess(t, homeDir, "project", "archive", "archived-proj")

	inputDir := createInputFolder(t, inputFolderOpts{
		MetadataJSON: `{"project_id":"archived-proj","source_device_id":"test-device"}`,
	})
	stderr := runPCFailure(t, homeDir, "add", inputDir)
	if !strings.Contains(stderr, "archived") {
		t.Fatalf("expected archived project error, got %q", stderr)
	}
}

func TestStaleActiveProjectConfigIsIgnored(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	db := openTestDB(t, homeDir)
	if _, err := db.Exec(`UPDATE records SET project_id = project_id WHERE 1 = 0`); err != nil {
		t.Fatalf("database sanity check: %v", err)
	}
	writeStaleActiveProjectConfig(t, homeDir)

	inputDir := createInputFolder(t, inputFolderOpts{})
	stdout := runPCSuccess(t, homeDir, "add", inputDir)
	recordID := strings.TrimSpace(stdout)

	var projectID string
	if err := db.QueryRow("SELECT project_id FROM records WHERE id = ?", recordID).Scan(&projectID); err != nil {
		t.Fatalf("query project_id: %v", err)
	}
	if projectID != "test/default-project" {
		t.Fatalf("expected metadata project, got %q", projectID)
	}
}

func writeStaleActiveProjectConfig(t *testing.T, homeDir string) {
	t.Helper()
	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	if err := os.WriteFile(configPath, []byte(`{"active_project":"stale-project"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
