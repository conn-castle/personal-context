package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewRootCommandDefaults(t *testing.T) {
	command := NewRootCommand(RootCommandOptions{})

	if command.Use != "pc" {
		t.Fatalf("expected use=pc, got %q", command.Use)
	}
	if command.Version != DefaultVersion {
		t.Fatalf("expected default version %q, got %q", DefaultVersion, command.Version)
	}
}

func TestNewRootCommandHelpOutput(t *testing.T) {
	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	command := NewRootCommand(RootCommandOptions{
		Stdout: stdout,
		Stderr: stderr,
	})
	command.SetArgs([]string{"--help"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(stdout.String(), "Personal Context CLI") {
		t.Fatalf("expected help text, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}

func TestNewRootCommandVersionOutput(t *testing.T) {
	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	command := NewRootCommand(RootCommandOptions{
		Stdout:  stdout,
		Stderr:  stderr,
		Version: "1.2.3",
	})
	command.SetArgs([]string{"--version"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(stdout.String(), "pc version 1.2.3") {
		t.Fatalf("expected version output, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}

func TestNewRootCommandNoArgsShowsHelp(t *testing.T) {
	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	command := NewRootCommand(RootCommandOptions{
		Stdout: stdout,
		Stderr: stderr,
	})
	command.SetArgs([]string{})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("expected usage output, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}

func TestNewRootCommandRejectsUnexpectedArgs(t *testing.T) {
	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	command := NewRootCommand(RootCommandOptions{
		Stdout: stdout,
		Stderr: stderr,
	})
	command.SetArgs([]string{"unexpected"})

	if err := command.Execute(); err == nil {
		t.Fatal("expected error for unexpected args, got nil")
	}
}
