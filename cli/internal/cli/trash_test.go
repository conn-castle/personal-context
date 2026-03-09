package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestTrashEmptyList(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"trash"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if !strings.Contains(stdout.String(), "Trash is empty.") {
		t.Fatalf("expected 'Trash is empty.', got %q", stdout.String())
	}
}

func TestTrashWithDeletedSlides(t *testing.T) {
	setupEnv(t)
	id := addSlide(t)

	// Soft-delete the slide
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"trash"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trash: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "Date") || !strings.Contains(out, "Deleted At") {
		t.Fatalf("expected table header, got %q", out)
	}
	if !strings.Contains(out, id) {
		t.Fatalf("expected slide ID %s in trash output, got %q", id, out)
	}
}

func TestTrashListSlidesDBError(t *testing.T) {
	homeDir := setupEnv(t)

	corruptTable(t, homeDir, "slides")

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"trash"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

func TestTrashDeletedAtFormat(t *testing.T) {
	setupEnv(t)
	id := addSlide(t)

	// Soft-delete the slide
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify that trash output includes a properly formatted ISO 8601 timestamp
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"trash"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trash: %v", err)
	}

	out := stdout.String()
	// Match an ISO 8601 timestamp pattern: YYYY-MM-DDThh:mm:ss...Z
	matched := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, id) {
			// The line with the slide ID should contain a timestamp like 2026-03-08T12:34:56Z
			for _, field := range strings.Fields(line) {
				if len(field) >= 20 && field[4] == '-' && field[7] == '-' && field[10] == 'T' && field[len(field)-1] == 'Z' {
					matched = true
					break
				}
			}
		}
	}
	if !matched {
		t.Fatalf("expected ISO 8601 timestamp in trash output line for slide %s, got %q", id, out)
	}
}
