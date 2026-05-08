package e2e_test

import (
	"strings"
	"testing"
)

// --- pc fetch ---

func TestFetchWithoutCloudConfiguredFails(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	result := runPC(t, homeDir, "fetch", "some-record-id")
	if result.ExitCode == 0 {
		t.Fatalf("pc fetch unexpectedly succeeded:\nstdout: %s\nstderr: %s", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "cloud is not configured") {
		t.Fatalf("expected cloud configuration error, got stderr %q", result.Stderr)
	}
}

func TestFetchWithoutModeSelector(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	result := runPC(t, homeDir, "fetch")
	if result.ExitCode == 0 {
		t.Fatalf("pc fetch without args unexpectedly succeeded:\nstdout: %s", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "specify a record ID") {
		t.Fatalf("expected mode selector error, got stderr %q", result.Stderr)
	}
}

func TestFetchMutuallyExclusiveModes(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	result := runPC(t, homeDir, "fetch", "record-id", "--project", "org/proj")
	if result.ExitCode == 0 {
		t.Fatal("expected error for mutually exclusive modes")
	}
	if !strings.Contains(result.Stderr, "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got stderr %q", result.Stderr)
	}
}

// --- pc doctor local-only (no Cloud line) ---

func TestDoctorLocalOnlySkipsCloudCheck(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	result := runPC(t, homeDir, "doctor")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstdout: %s\nstderr: %s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	if strings.Contains(result.Stdout, "Cloud") {
		t.Fatalf("expected no Cloud line in local-only doctor output, got %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "All checks passed.") {
		t.Fatalf("expected 'All checks passed.', got %q", result.Stdout)
	}
}

// --- pc setup --remove-cloud in local mode ---

func TestSetupRemoveCloudInLocalModeNoOp(t *testing.T) {
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")

	// Removing cloud when not configured should succeed (no-op).
	result := runPC(t, homeDir, "setup", "--remove-cloud")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0 for remove-cloud on local mode, got %d\nstdout: %s\nstderr: %s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "not configured") {
		t.Fatalf("expected 'not configured' message, got %q", result.Stdout)
	}
}

func TestSetupRemoveCloudWithoutExistingConfigNoOp(t *testing.T) {
	homeDir := t.TempDir()

	result := runPC(t, homeDir, "setup", "--remove-cloud")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0 for remove-cloud without existing config, got %d\nstdout: %s\nstderr: %s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "not configured") {
		t.Fatalf("expected 'not configured' message, got %q", result.Stdout)
	}
}
