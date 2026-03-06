package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRunHelpReturnsSuccess(t *testing.T) {
	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	exitCode := run([]string{"--help"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !strings.Contains(stdout.String(), "Personal Context CLI") {
		t.Fatalf("expected help output, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}

func TestRunInvalidCommandReturnsFailure(t *testing.T) {
	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	exitCode := run([]string{"not-a-command"}, stdout, stderr)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("expected unknown command error, got %q", stderr.String())
	}
}

func TestRunVersionFlagUsesInjectedVersion(t *testing.T) {
	oldVersion := version
	version = "test-version"
	t.Cleanup(func() {
		version = oldVersion
	})

	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	exitCode := run([]string{"--version"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !strings.Contains(stdout.String(), "pc version test-version") {
		t.Fatalf("expected version output, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}

func TestMainUsesExitFunction(t *testing.T) {
	oldArgs := os.Args
	oldExit := exitFn
	oldVersion := version
	t.Cleanup(func() {
		os.Args = oldArgs
		exitFn = oldExit
		version = oldVersion
	})

	version = "test-main"
	os.Args = []string{"pc", "--version"}

	var capturedExitCode int
	exitFn = func(code int) {
		capturedExitCode = code
	}

	main()

	if capturedExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", capturedExitCode)
	}
}
