package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/personal-context/cli/internal/repository"

	_ "modernc.org/sqlite"
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
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
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
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
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
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
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
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
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
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
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
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
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

// --- GC coverage tests (from coverage_test.go) ---

func TestGCEmptyTrash(t *testing.T) {
	setupEnv(t)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"gc"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if !strings.Contains(stdout.String(), "No expired trash to clean up.") {
		t.Fatalf("expected clean message, got %q", stdout.String())
	}
}

func TestGCDeletesExpiredTrash(t *testing.T) {
	homeDir := setupEnv(t)

	id := addSlideWithContent(t,
		`<html><img src="figures/fig.png">content</html>`, "", "",
		map[string][]byte{"fig.png": []byte("image")},
		map[string][]byte{"data.csv": []byte("a,b")},
	)

	// Soft-delete.
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Backdate to 31 days ago.
	backdateDeletedAtUnit(t, homeDir, id, 31)

	// Run gc.
	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	if err := gcCmd.Execute(); err != nil {
		t.Fatalf("gc: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, fmt.Sprintf("Deleted %s", id)) {
		t.Fatalf("expected 'Deleted %s' in output, got %q", id, out)
	}
	if !strings.Contains(out, "Removed 1 slide(s).") {
		t.Fatalf("expected summary, got %q", out)
	}

	// Verify slide is gone from DB.
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM slides WHERE id = ?", id).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected slide to be hard-deleted, got count=%d", count)
	}

	// Verify files are removed.
	figurePath := filepath.Join(homeDir, "personal-context", "figures", id, "fig.png")
	if _, err := os.Stat(figurePath); !os.IsNotExist(err) {
		t.Fatalf("expected figure file to be removed, stat err=%v", err)
	}
	dataPath := filepath.Join(homeDir, "personal-context", "data", id, "data.csv")
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected data file to be removed, stat err=%v", err)
	}
}

func TestGCLeavesYoungTrashUnit(t *testing.T) {
	setupEnv(t)

	id := addSlide(t)

	// Soft-delete (just now).
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Run gc.
	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	if err := gcCmd.Execute(); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if !strings.Contains(stdout.String(), "No expired trash to clean up.") {
		t.Fatalf("expected young trash to be left alone, got %q", stdout.String())
	}
}

func TestGCMixedAgesUnit(t *testing.T) {
	homeDir := setupEnv(t)

	idOld := addSlide(t, "--date", "2025-01-01")
	idYoung := addSlide(t, "--date", "2025-01-02")

	// Soft-delete both.
	for _, id := range []string{idOld, idYoung} {
		delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		delCmd.SetArgs([]string{"delete", id})
		if err := delCmd.Execute(); err != nil {
			t.Fatalf("delete %s: %v", id, err)
		}
	}

	// Backdate only the old one.
	backdateDeletedAtUnit(t, homeDir, idOld, 31)

	// Run gc.
	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	if err := gcCmd.Execute(); err != nil {
		t.Fatalf("gc: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, idOld) {
		t.Fatalf("expected old slide %s to be deleted, got %q", idOld, out)
	}
	if strings.Contains(out, idYoung) {
		t.Fatalf("expected young slide %s to NOT be deleted, got %q", idYoung, out)
	}
}

func TestGCListSlidesDBError(t *testing.T) {
	homeDir := setupEnv(t)

	// Corrupt slides table.
	corruptTable(t, homeDir, "slides")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"gc"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}

func TestGCDeleteSlideError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlide(t)

	// Soft-delete
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Backdate to 31 days ago
	backdateDeletedAtUnit(t, homeDir, id, 31)

	// Drop the slides table so DeleteSlide fails
	// But first we need to make ListSlides succeed... so we use a different approach:
	// Corrupt the slide_figures table referenced by cascade delete
	// Actually, we need a simpler approach: drop sync_version to make delete trigger fail.
	// DeleteSlide is a hard DELETE FROM slides, which triggers slides_sync_bump_after_delete.
	corruptTable(t, homeDir, "sync_version")

	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	err := gcCmd.Execute()
	if err == nil {
		t.Fatal("expected error when DeleteSlide trigger fails")
	}
	if !strings.Contains(err.Error(), "hard delete slide") {
		t.Fatalf("expected 'hard delete slide' error, got %v", err)
	}
}

func TestGCDeleteSlideDirError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addSlideWithContent(t,
		`<html><img src="figures/fig.png">content</html>`, "", "",
		map[string][]byte{"fig.png": []byte("image")},
		nil,
	)

	// Soft-delete
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Backdate to 31 days ago
	backdateDeletedAtUnit(t, homeDir, id, 31)

	// Make the figures directory unwritable so RemoveAll fails
	figureSlideDir := filepath.Join(homeDir, "personal-context", "figures", id)
	if err := os.Chmod(figureSlideDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(figureSlideDir, 0o755) })

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	gcCmd.SetArgs([]string{"gc"})
	err := gcCmd.Execute()
	if err != nil {
		t.Fatalf("expected gc to succeed with a warning, got error: %v", err)
	}
	output := stderr.String()
	if !strings.Contains(output, "Warning: failed to remove files for slide") {
		t.Fatalf("expected warning about failed file removal in stderr, got %q", output)
	}
}
