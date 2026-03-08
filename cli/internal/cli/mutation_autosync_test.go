package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestRunAddAutoSyncWarningDoesNotPolluteStdout(t *testing.T) {
	setupEnv(t)

	inputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "slide.html"), []byte("<h1>slide</h1>"), 0o644); err != nil {
		t.Fatalf("WriteFile(slide.html) error = %v", err)
	}

	original := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = original })
	calls := 0
	runAutoSyncFn = func(_ context.Context, stderr io.Writer) error {
		calls++
		_, _ = io.WriteString(stderr, "warning: auto-sync failed: boom\n")
		return nil
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runAdd(context.Background(), stdout, stderr, inputDir, "", "", position{kind: "last"}); err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected auto-sync to run once, got %d", calls)
	}
	if strings.Contains(stdout.String(), "warning:") {
		t.Fatalf("stdout should not contain warning text, got %q", stdout.String())
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatalf("expected stdout to contain the new slide id, got %q", stdout.String())
	}
	if stderr.String() != "warning: auto-sync failed: boom\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunDeleteCallsAutoSyncAfterLocalSuccess(t *testing.T) {
	homeDir := setupEnv(t)

	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack() error = %v", err)
	}
	defer func() { _ = stack.Close() }()

	if _, err := stack.Repo.CreateSlide(context.Background(), repository.CreateSlideInput{
		ID:          "20260308-a1b2c3d4",
		Date:        "2026-03-08",
		DayOrder:    "a",
		HTMLContent: "<h1>x</h1>",
	}); err != nil {
		t.Fatalf("CreateSlide() error = %v", err)
	}

	original := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = original })
	calls := 0
	runAutoSyncFn = func(_ context.Context, stderr io.Writer) error {
		calls++
		_, _ = io.WriteString(stderr, "warning: auto-sync failed: boom\n")
		return nil
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runDelete(context.Background(), stdout, stderr, "20260308-a1b2c3d4"); err != nil {
		t.Fatalf("runDelete() error = %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected auto-sync to run once, got %d", calls)
	}
	if stdout.String() != "Slide 20260308-a1b2c3d4 deleted\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "warning: auto-sync failed: boom\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
