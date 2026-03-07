package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupCommandSuccess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"setup"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if !strings.Contains(stdout.String(), "initialized") {
		t.Fatalf("expected success message, got %q", stdout.String())
	}

	// Verify DB exists
	dbFile := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	if _, err := os.Stat(dbFile); err != nil {
		t.Fatalf("database not created: %v", err)
	}

	// Verify config exists
	configFile := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	if _, err := os.Stat(configFile); err != nil {
		t.Fatalf("config not created: %v", err)
	}
}

func TestSetupCommandIdempotent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first setup failed: %v", err)
	}

	stdout.Reset()
	stderr.Reset()

	cmd2 := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd2.SetArgs([]string{"setup"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("second setup failed: %v", err)
	}

	if !strings.Contains(stdout.String(), "initialized") {
		t.Fatalf("expected success message on second run, got %q", stdout.String())
	}
}

func TestSetupCommandRejectsArgs(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"setup", "extra"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for extra args")
	}
}
