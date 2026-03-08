package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

// --- GC cloud-aware tests ---

func TestGCCloudNotConfiguredStillDeletesLocally(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	// Soft-delete.
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	backdateDeletedAtUnit(t, homeDir, id, 31)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string) (*cloudStack, error) {
		return nil, errCloudNotConfigured
	}

	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	syncCalls := 0
	runAutoSyncFn = func(context.Context, io.Writer) error {
		syncCalls++
		return nil
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runGC(context.Background(), stdout, stderr); err != nil {
		t.Fatalf("runGC() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "Deleted "+id) {
		t.Fatalf("expected 'Deleted %s' in stdout, got %q", id, stdout.String())
	}
	if syncCalls != 1 {
		t.Fatalf("expected auto-sync to be called once, got %d", syncCalls)
	}
}

func TestGCCloudUnreachableWarnsOnStderr(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	backdateDeletedAtUnit(t, homeDir, id, 31)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string) (*cloudStack, error) {
		return nil, errors.New("connection refused")
	}

	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	runAutoSyncFn = func(context.Context, io.Writer) error { return nil }

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runGC(context.Background(), stdout, stderr); err != nil {
		t.Fatalf("runGC() error = %v", err)
	}

	if !strings.Contains(stderr.String(), "warning: cloud unreachable") {
		t.Fatalf("expected cloud unreachable warning on stderr, got %q", stderr.String())
	}
	// Should still delete locally.
	if !strings.Contains(stdout.String(), "Deleted "+id) {
		t.Fatalf("expected local deletion, got %q", stdout.String())
	}
}

func TestGCCloudDeletesFromCloudRepo(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	backdateDeletedAtUnit(t, homeDir, id, 31)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })

	cloudDeleteCalls := make([]string, 0)
	cloudMock := &gcMockRepo{
		deleteSlide: func(_ context.Context, slideID string) error {
			cloudDeleteCalls = append(cloudDeleteCalls, slideID)
			return nil
		},
	}
	openCloudStackFn = func(context.Context, string) (*cloudStack, error) {
		return &cloudStack{Repo: cloudMock}, nil
	}

	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	runAutoSyncFn = func(context.Context, io.Writer) error { return nil }

	stdout := &bytes.Buffer{}
	if err := runGC(context.Background(), stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runGC() error = %v", err)
	}

	if len(cloudDeleteCalls) != 1 || cloudDeleteCalls[0] != id {
		t.Fatalf("expected cloud DeleteSlide(%s), got %v", id, cloudDeleteCalls)
	}
	if !strings.Contains(stdout.String(), "Deleted "+id) {
		t.Fatalf("expected local deletion, got %q", stdout.String())
	}
}

func TestGCCloudDeleteNotFoundIsIgnored(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	backdateDeletedAtUnit(t, homeDir, id, 31)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })

	cloudMock := &gcMockRepo{
		deleteSlide: func(context.Context, string) error {
			return repository.ErrNotFound
		},
	}
	openCloudStackFn = func(context.Context, string) (*cloudStack, error) {
		return &cloudStack{Repo: cloudMock}, nil
	}

	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	runAutoSyncFn = func(context.Context, io.Writer) error { return nil }

	stdout := &bytes.Buffer{}
	if err := runGC(context.Background(), stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runGC() error = %v", err)
	}

	// Should still delete locally despite cloud ErrNotFound.
	if !strings.Contains(stdout.String(), "Deleted "+id) {
		t.Fatalf("expected local deletion after cloud ErrNotFound, got %q", stdout.String())
	}
}

func TestGCCloudDeleteErrorSkipsSlide(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	backdateDeletedAtUnit(t, homeDir, id, 31)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })

	cloudMock := &gcMockRepo{
		deleteSlide: func(context.Context, string) error {
			return errors.New("cloud db error")
		},
	}
	openCloudStackFn = func(context.Context, string) (*cloudStack, error) {
		return &cloudStack{Repo: cloudMock}, nil
	}

	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	runAutoSyncFn = func(context.Context, io.Writer) error { return nil }

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runGC(context.Background(), stdout, stderr); err != nil {
		t.Fatalf("runGC() error = %v", err)
	}

	// Slide should be skipped with a warning, not deleted locally.
	if !strings.Contains(stderr.String(), "Warning: failed to delete slide") {
		t.Fatalf("expected cloud delete warning on stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Removed 0 slide(s)") {
		t.Fatalf("expected 0 removed, got %q", stdout.String())
	}
}

func TestGCAutoSyncCalledAfterDeletion(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	backdateDeletedAtUnit(t, homeDir, id, 31)

	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	syncCalls := 0
	runAutoSyncFn = func(_ context.Context, stderr io.Writer) error {
		syncCalls++
		_, _ = io.WriteString(stderr, "warning: auto-sync failed: boom\n")
		return nil
	}

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string) (*cloudStack, error) {
		return nil, errCloudNotConfigured
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runGC(context.Background(), stdout, stderr); err != nil {
		t.Fatalf("runGC() error = %v", err)
	}

	if syncCalls != 1 {
		t.Fatalf("expected auto-sync called once, got %d", syncCalls)
	}
	if stderr.String() != "warning: auto-sync failed: boom\n" {
		t.Fatalf("expected auto-sync warning on stderr, got %q", stderr.String())
	}
}

// gcMockRepo implements repository.Repository with configurable DeleteSlide for gc tests.
type gcMockRepo struct {
	mockRepo
	deleteSlide func(ctx context.Context, id string) error
}

func (m *gcMockRepo) DeleteSlide(ctx context.Context, id string) error {
	if m.deleteSlide != nil {
		return m.deleteSlide(ctx, id)
	}
	return nil
}
