package e2e_test

import (
	"strings"
	"testing"
)

func TestSyncWithoutCloudConfiguredFails(t *testing.T) {
	homeDir := t.TempDir()

	setup := runPC(t, homeDir, "setup")
	if setup.ExitCode != 0 {
		t.Fatalf("pc setup failed:\nstdout: %s\nstderr: %s", setup.Stdout, setup.Stderr)
	}

	result := runPC(t, homeDir, "sync")
	if result.ExitCode == 0 {
		t.Fatalf("pc sync unexpectedly succeeded:\nstdout: %s\nstderr: %s", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "cloud is not configured") {
		t.Fatalf("expected cloud configuration error, got stderr %q", result.Stderr)
	}
}
