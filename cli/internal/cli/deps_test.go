package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveHomeDirUsesEnvVar(t *testing.T) {
	expected := t.TempDir()
	t.Setenv("PC_HOME", expected)

	got, err := resolveHomeDir()
	if err != nil {
		t.Fatalf("resolveHomeDir() error = %v", err)
	}
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestResolveHomeDirFallsBackToUserHome(t *testing.T) {
	t.Setenv("PC_HOME", "")

	got, err := resolveHomeDir()
	if err != nil {
		t.Fatalf("resolveHomeDir() error = %v", err)
	}

	expected, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestBasePathAndDBPath(t *testing.T) {
	homeDir := "/home/test"

	base := basePath(homeDir)
	if base != filepath.Join(homeDir, "personal-context") {
		t.Fatalf("unexpected base path: %q", base)
	}

	db := dbPath(homeDir)
	expected := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	if db != expected {
		t.Fatalf("expected %q, got %q", expected, db)
	}
}

func TestOpenLocalStackRequiresConfig(t *testing.T) {
	homeDir := t.TempDir()
	// No setup done, so config doesn't exist
	_, err := openLocalStack(homeDir)
	if err == nil {
		t.Fatal("expected error when config doesn't exist")
	}
}

func TestOpenLocalStackAfterSetup(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("PC_HOME", homeDir)

	// Run setup first to create the environment
	stdout := &discardWriter{}
	stderr := &discardWriter{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"setup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack() error = %v", err)
	}
	defer func() { _ = stack.Close() }()

	if stack.Repo == nil {
		t.Fatal("expected non-nil Repo")
	}
	if stack.FS == nil {
		t.Fatal("expected non-nil FS")
	}
	if stack.Store.Path() == "" {
		t.Fatal("expected non-empty Store path")
	}
}

func TestLocalStackClose(t *testing.T) {
	// Close on nil connection should not panic
	s := &localStack{}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
