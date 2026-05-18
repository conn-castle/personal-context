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
	if err := runProjectAdd(context.Background(), io.Discard, io.Discard, "test/project", "", ""); err != nil {
		t.Fatalf("runProjectAdd() error = %v", err)
	}
	inputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "record.html"), []byte("<h1>record</h1>"), 0o644); err != nil {
		t.Fatalf("WriteFile(record.html) error = %v", err)
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
	if err := runAdd(context.Background(), stdout, stderr, inputDir, "", "test/project", "test-device", "", position{kind: "last"}); err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected auto-sync to run once, got %d", calls)
	}
	if strings.Contains(stdout.String(), "warning:") {
		t.Fatalf("stdout should not contain warning text, got %q", stdout.String())
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatalf("expected stdout to contain the new record id, got %q", stdout.String())
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

	ctx := context.Background()
	if _, err := stack.Repo.CreateProject(ctx, repository.CreateRegistryInput{ID: "test/project"}); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if _, err := stack.Repo.CreateRecord(ctx, repository.CreateRecordInput{
		ID:             "20260308-a1b2c3d4",
		Date:           "2026-03-08",
		DayOrder:       "a",
		HTMLContent:    strPtr("<h1>x</h1>"),
		ProjectID:      "test/project",
		SourceDeviceID: "test-device",
	}); err != nil {
		t.Fatalf("CreateRecord() error = %v", err)
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
	if stdout.String() != "Record 20260308-a1b2c3d4 deleted\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "warning: auto-sync failed: boom\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
