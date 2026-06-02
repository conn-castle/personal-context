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

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/repository"

	_ "modernc.org/sqlite"
)

// --- GC cloud-aware tests ---

func TestGCCloudNotConfiguredStillDeletesLocally(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecord(t)

	// Soft-delete.
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
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
	if err := runGCAll(context.Background(), stdout, stderr, allTrashDomains()); err != nil {
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
	id := addRecord(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
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
	if err := runGCAll(context.Background(), stdout, stderr, allTrashDomains()); err != nil {
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
	id := addRecord(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	backdateDeletedAtUnit(t, homeDir, id, 31)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })

	cloudDeleteCalls := make([]string, 0)
	cloudMock := &gcMockRepo{
		deleteRecord: func(_ context.Context, recordID string) error {
			cloudDeleteCalls = append(cloudDeleteCalls, recordID)
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
	if err := runGCAll(context.Background(), stdout, &bytes.Buffer{}, allTrashDomains()); err != nil {
		t.Fatalf("runGC() error = %v", err)
	}

	if len(cloudDeleteCalls) != 1 || cloudDeleteCalls[0] != id {
		t.Fatalf("expected cloud DeleteRecord(%s), got %v", id, cloudDeleteCalls)
	}
	if !strings.Contains(stdout.String(), "Deleted "+id) {
		t.Fatalf("expected local deletion, got %q", stdout.String())
	}
}

func TestGCCloudDeleteNotFoundIsIgnored(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecord(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	backdateDeletedAtUnit(t, homeDir, id, 31)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })

	cloudMock := &gcMockRepo{
		deleteRecord: func(context.Context, string) error {
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
	if err := runGCAll(context.Background(), stdout, &bytes.Buffer{}, allTrashDomains()); err != nil {
		t.Fatalf("runGC() error = %v", err)
	}

	// Should still delete locally despite cloud ErrNotFound.
	if !strings.Contains(stdout.String(), "Deleted "+id) {
		t.Fatalf("expected local deletion after cloud ErrNotFound, got %q", stdout.String())
	}
}

func TestGCCloudDeleteErrorSkipsRecord(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecord(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	backdateDeletedAtUnit(t, homeDir, id, 31)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })

	cloudMock := &gcMockRepo{
		deleteRecord: func(context.Context, string) error {
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
	if err := runGCAll(context.Background(), stdout, stderr, allTrashDomains()); err != nil {
		t.Fatalf("runGC() error = %v", err)
	}

	// Record should be skipped with a warning, not deleted locally.
	if !strings.Contains(stderr.String(), "Warning: failed to delete record") {
		t.Fatalf("expected cloud delete warning on stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Removed 0 item(s)") {
		t.Fatalf("expected 0 removed, got %q", stdout.String())
	}
}

func TestGCAutoSyncCalledAfterDeletion(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecord(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
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
	if err := runGCAll(context.Background(), stdout, stderr, allTrashDomains()); err != nil {
		t.Fatalf("runGC() error = %v", err)
	}

	if syncCalls != 1 {
		t.Fatalf("expected auto-sync called once, got %d", syncCalls)
	}
	if stderr.String() != "warning: auto-sync failed: boom\n" {
		t.Fatalf("expected auto-sync warning on stderr, got %q", stderr.String())
	}
}

// gcMockRepo implements repository.Repository with configurable DeleteRecord for gc tests.
type gcMockRepo struct {
	mockRepo
	deleteRecord func(ctx context.Context, id string) error
}

func (m *gcMockRepo) DeleteRecord(ctx context.Context, id string) error {
	if m.deleteRecord != nil {
		return m.deleteRecord(ctx, id)
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

	id := addRecordWithContent(t,
		`<html><img src="figures/fig.png">content</html>`, "", "",
		map[string][]byte{"fig.png": []byte("image")},
		map[string][]byte{"data.csv": []byte("a,b")},
	)

	// Soft-delete.
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
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
	if !strings.Contains(out, "Removed 1 item(s).") {
		t.Fatalf("expected summary, got %q", out)
	}

	// Verify record is gone from DB.
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM records WHERE id = ?", id).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected record to be hard-deleted, got count=%d", count)
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

	id := addRecord(t)

	// Soft-delete (just now).
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
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

	idOld := addRecord(t, "--date", "2025-01-01")
	idYoung := addRecord(t, "--date", "2025-01-02")

	// Soft-delete both.
	for _, id := range []string{idOld, idYoung} {
		delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		delCmd.SetArgs([]string{"records", "delete", id})
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
		t.Fatalf("expected old record %s to be deleted, got %q", idOld, out)
	}
	if strings.Contains(out, idYoung) {
		t.Fatalf("expected young record %s to NOT be deleted, got %q", idYoung, out)
	}
}

func TestGCUsesConfiguredRetention(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecord(t)

	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Backdate 10 days: inside the default 30-day window, but outside a
	// configured 7-day window.
	backdateDeletedAtUnit(t, homeDir, id, 10)

	// With the default retention, the record is too young to collect.
	defaultOut := &bytes.Buffer{}
	defaultGC := NewRootCommand(RootCommandOptions{Stdout: defaultOut, Stderr: &bytes.Buffer{}})
	defaultGC.SetArgs([]string{"gc"})
	if err := defaultGC.Execute(); err != nil {
		t.Fatalf("gc (default): %v", err)
	}
	if !strings.Contains(defaultOut.String(), "No expired trash to clean up.") {
		t.Fatalf("expected record to survive default retention, got %q", defaultOut.String())
	}

	// Shorten the retention window to 7 days; now the record is expired.
	setGCRetentionDays(t, homeDir, 7)

	shortOut := &bytes.Buffer{}
	shortGC := NewRootCommand(RootCommandOptions{Stdout: shortOut, Stderr: &bytes.Buffer{}})
	shortGC.SetArgs([]string{"gc"})
	if err := shortGC.Execute(); err != nil {
		t.Fatalf("gc (7-day): %v", err)
	}
	if !strings.Contains(shortOut.String(), "Deleted "+id) {
		t.Fatalf("expected record to be collected under 7-day retention, got %q", shortOut.String())
	}
}

// setGCRetentionDays updates the persisted config to use a custom gc retention.
func setGCRetentionDays(t *testing.T, homeDir string, days int) {
	t.Helper()
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	cfg, err := store.Read()
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg.GCRetentionDays = &days
	if err := store.Write(cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestGCListRecordsDBError(t *testing.T) {
	homeDir := setupEnv(t)

	// Corrupt records table.
	corruptTable(t, homeDir, "records")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"gc"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when records table missing")
	}
}

func TestGCDeleteRecordError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecord(t)

	// Soft-delete
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Backdate to 31 days ago
	backdateDeletedAtUnit(t, homeDir, id, 31)

	// Drop the records table so DeleteRecord fails
	// But first we need to make ListRecords succeed... so we use a different approach:
	// Corrupt the record_figures table referenced by cascade delete
	// Actually, we need a simpler approach: drop sync_version to make delete trigger fail.
	// DeleteRecord is a hard DELETE FROM records, which triggers records_sync_bump_after_delete.
	corruptTable(t, homeDir, "sync_version")

	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	err := gcCmd.Execute()
	if err == nil {
		t.Fatal("expected error when DeleteRecord trigger fails")
	}
	if !strings.Contains(err.Error(), "hard delete record") {
		t.Fatalf("expected 'hard delete record' error, got %v", err)
	}
}

func TestGCDeleteRecordDirError(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecordWithContent(t,
		`<html><img src="figures/fig.png">content</html>`, "", "",
		map[string][]byte{"fig.png": []byte("image")},
		nil,
	)

	// Soft-delete
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"records", "delete", id})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Backdate to 31 days ago
	backdateDeletedAtUnit(t, homeDir, id, 31)

	// Make the figures directory unwritable so RemoveAll fails
	figureRecordDir := filepath.Join(homeDir, "personal-context", "figures", id)
	if err := os.Chmod(figureRecordDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(figureRecordDir, 0o755) })

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	gcCmd.SetArgs([]string{"gc"})
	err := gcCmd.Execute()
	if err != nil {
		t.Fatalf("expected gc to succeed with a warning, got error: %v", err)
	}
	output := stderr.String()
	if !strings.Contains(output, "Warning: failed to remove files for record") {
		t.Fatalf("expected warning about failed file removal in stderr, got %q", output)
	}
}
