package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTextFileAtomicallyHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "test.txt")
	content := []byte("hello world\n")

	if err := writeTextFileAtomically(path, content, 0o755, 0o644); err != nil {
		t.Fatalf("writeTextFileAtomically() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %q, want %q", got, content)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("permissions = %04o, want 0644", perm)
	}
}

func TestWriteTextFileAtomicallyCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "file.txt")

	if err := writeTextFileAtomically(path, []byte("nested"), 0o700, 0o600); err != nil {
		t.Fatalf("writeTextFileAtomically() error = %v", err)
	}

	parentInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(parent) error = %v", err)
	}
	if !parentInfo.IsDir() {
		t.Fatal("expected parent to be a directory")
	}
}

func TestWriteTextFileAtomicallySyncsCreatedParentDirs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "b", "file.txt")
	originalSyncDirFn := syncDirFn
	t.Cleanup(func() { syncDirFn = originalSyncDirFn })
	var syncedDirs []string
	syncDirFn = func(dir string) error {
		syncedDirs = append(syncedDirs, dir)
		return nil
	}

	if err := writeTextFileAtomically(path, []byte("durable dirs"), 0o755, 0o644); err != nil {
		t.Fatalf("writeTextFileAtomically() error = %v", err)
	}

	want := []string{root, filepath.Join(root, "a"), filepath.Join(root, "a", "b")}
	if len(syncedDirs) != len(want) {
		t.Fatalf("synced dirs = %v, want %v", syncedDirs, want)
	}
	for i := range want {
		if syncedDirs[i] != want[i] {
			t.Fatalf("synced dirs = %v, want %v", syncedDirs, want)
		}
	}
}

func TestWriteTextFileAtomicallyCreatedParentDirSyncError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "file.txt")
	originalSyncDirFn := syncDirFn
	t.Cleanup(func() { syncDirFn = originalSyncDirFn })
	syncDirFn = func(string) error {
		return errors.New("mkdir sync boom")
	}

	err := writeTextFileAtomically(path, []byte("data"), 0o755, 0o644)
	if err == nil || !strings.Contains(err.Error(), "sync parent directory for created directory") {
		t.Fatalf("expected created directory sync error, got %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target file should not be written after created-dir sync failure, stat err = %v", statErr)
	}
}

func TestWriteTextFileAtomicallyParentPathIsFile(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "a")
	if err := os.WriteFile(blocker, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("WriteFile(blocker) error = %v", err)
	}

	err := writeTextFileAtomically(filepath.Join(blocker, "file.txt"), []byte("data"), 0o755, 0o644)
	if err == nil || !strings.Contains(err.Error(), "exists and is not a directory") {
		t.Fatalf("expected parent path file error, got %v", err)
	}

	err = writeTextFileAtomically(filepath.Join(blocker, "b", "file.txt"), []byte("data"), 0o755, 0o644)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected nested parent path file error, got %v", err)
	}
}

func TestEnsureDirDurablyMkdirRace(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "a", "b")
	originalSyncDirFn := syncDirFn
	t.Cleanup(func() { syncDirFn = originalSyncDirFn })
	syncDirFn = func(dir string) error {
		if dir == root {
			if err := os.Mkdir(target, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				t.Fatalf("Mkdir(target) during parent sync error = %v", err)
			}
		}
		return nil
	}

	if err := ensureDirDurably(target, 0o755); err != nil {
		t.Fatalf("ensureDirDurably() should tolerate concurrent directory creation, got %v", err)
	}
}

func TestEnsureDirDurablyPropagatesParentCreationError(t *testing.T) {
	root := t.TempDir()
	originalSyncDirFn := syncDirFn
	t.Cleanup(func() { syncDirFn = originalSyncDirFn })
	syncDirFn = func(dir string) error {
		if dir == root {
			return errors.New("parent sync boom")
		}
		return nil
	}

	err := ensureDirDurably(filepath.Join(root, "a", "b"), 0o755)
	if err == nil || !strings.Contains(err.Error(), "parent sync boom") {
		t.Fatalf("expected parent creation error, got %v", err)
	}
}

func TestWriteTextFileAtomicallyParentDirError(t *testing.T) {
	// Use a path rooted under a file (not a directory) to fail MkdirAll.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	path := filepath.Join(blocker, "subdir", "file.txt")
	err := writeTextFileAtomically(path, []byte("data"), 0o755, 0o644)
	if err == nil {
		t.Fatal("expected error when parent directory creation fails")
	}
}

func TestWriteTextFileAtomicallyOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")

	if err := os.WriteFile(path, []byte("old content"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	newContent := []byte("new content")
	if err := writeTextFileAtomically(path, newContent, 0o755, 0o600); err != nil {
		t.Fatalf("writeTextFileAtomically() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(newContent) {
		t.Fatalf("content = %q, want %q", got, newContent)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permissions = %04o, want 0600", perm)
	}
}

func TestWriteTextFileAtomicallyEmptyContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")

	if err := writeTextFileAtomically(path, []byte{}, 0o755, 0o644); err != nil {
		t.Fatalf("writeTextFileAtomically() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty file, got %q", got)
	}
}

func TestWriteTextFileAtomicallySyncsParentDirAfterRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "synced.txt")
	originalSyncDirFn := syncDirFn
	t.Cleanup(func() { syncDirFn = originalSyncDirFn })
	var syncedDirs []string
	syncDirFn = func(dir string) error {
		syncedDirs = append(syncedDirs, dir)
		return nil
	}

	if err := writeTextFileAtomically(path, []byte("durable"), 0o755, 0o644); err != nil {
		t.Fatalf("writeTextFileAtomically() error = %v", err)
	}

	if len(syncedDirs) != 1 || syncedDirs[0] != filepath.Dir(path) {
		t.Fatalf("synced dirs = %v, want [%s]", syncedDirs, filepath.Dir(path))
	}
}

func TestWriteTextFileAtomicallyParentDirSyncError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync-error.txt")
	originalSyncDirFn := syncDirFn
	t.Cleanup(func() { syncDirFn = originalSyncDirFn })
	syncDirFn = func(string) error {
		return errors.New("sync boom")
	}

	err := writeTextFileAtomically(path, []byte("renamed"), 0o755, 0o644)
	if err == nil || !strings.Contains(err.Error(), "sync parent directory") {
		t.Fatalf("expected parent sync error, got %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("renamed file should remain inspectable after sync failure: %v", readErr)
	}
	if string(got) != "renamed" {
		t.Fatalf("content after sync failure = %q, want renamed", got)
	}
}

type failingAtomicTempFile struct {
	path     string
	failOp   string
	closeCnt int
}

func (f *failingAtomicTempFile) Write([]byte) (int, error) {
	if f.failOp == "write" {
		return 0, errors.New("write boom")
	}
	return 0, nil
}

func (f *failingAtomicTempFile) Sync() error {
	if f.failOp == "sync" {
		return errors.New("sync boom")
	}
	return nil
}

func (f *failingAtomicTempFile) Close() error {
	f.closeCnt++
	if f.failOp == "close" {
		return errors.New("close boom")
	}
	return nil
}

func (f *failingAtomicTempFile) Chmod(os.FileMode) error {
	if f.failOp == "chmod" {
		return errors.New("chmod boom")
	}
	return nil
}

func (f *failingAtomicTempFile) Name() string {
	return f.path
}

func TestWriteTextFileAtomicallyTempFileFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failOp  string
		wantErr string
	}{
		{name: "chmod", failOp: "chmod", wantErr: "set permissions"},
		{name: "write", failOp: "write", wantErr: "write temp file"},
		{name: "sync", failOp: "sync", wantErr: "sync temp file"},
		{name: "close", failOp: "close", wantErr: "close temp file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalCreateTempFileFn := createTempFileFn
			t.Cleanup(func() { createTempFileFn = originalCreateTempFileFn })

			temp := &failingAtomicTempFile{
				path:   filepath.Join(t.TempDir(), "staged.tmp"),
				failOp: tc.failOp,
			}
			createTempFileFn = func(string, string) (atomicTempFile, error) {
				return temp, nil
			}

			path := filepath.Join(t.TempDir(), "out.txt")
			err := writeTextFileAtomically(path, []byte("data"), 0o755, 0o644)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("writeTextFileAtomically() error = %v, want %q", err, tc.wantErr)
			}
			if tc.failOp != "close" && temp.closeCnt == 0 {
				t.Fatalf("%s failure did not close temp file", tc.failOp)
			}
		})
	}
}

type stubSyncableDir struct {
	syncErr  error
	closeErr error
	closed   bool
}

func (d *stubSyncableDir) Sync() error {
	return d.syncErr
}

func (d *stubSyncableDir) Close() error {
	d.closed = true
	return d.closeErr
}

func TestSyncDirErrors(t *testing.T) {
	originalOpenDirFn := openDirFn
	t.Cleanup(func() { openDirFn = originalOpenDirFn })

	openDirFn = func(string) (syncableDir, error) {
		return nil, errors.New("open boom")
	}
	if err := syncDir(t.TempDir()); err == nil || !strings.Contains(err.Error(), "open boom") {
		t.Fatalf("syncDir(open failure) error = %v", err)
	}

	syncFail := &stubSyncableDir{syncErr: errors.New("sync boom")}
	openDirFn = func(string) (syncableDir, error) {
		return syncFail, nil
	}
	if err := syncDir(t.TempDir()); err == nil || !strings.Contains(err.Error(), "sync boom") {
		t.Fatalf("syncDir(sync failure) error = %v", err)
	}
	if !syncFail.closed {
		t.Fatal("syncDir should close directory after sync failure")
	}

	closeFail := &stubSyncableDir{closeErr: errors.New("close boom")}
	openDirFn = func(string) (syncableDir, error) {
		return closeFail, nil
	}
	if err := syncDir(t.TempDir()); err == nil || !strings.Contains(err.Error(), "close boom") {
		t.Fatalf("syncDir(close failure) error = %v", err)
	}
}

func TestWriteTextFileAtomicallyCreateTempError(t *testing.T) {
	// Create the parent dir, then make it read-only so CreateTemp fails.
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(subDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(subDir, 0o755) })

	path := filepath.Join(subDir, "file.txt")
	err := writeTextFileAtomically(path, []byte("data"), 0o755, 0o644)
	if err == nil {
		t.Fatal("expected error when directory is read-only")
	}
}

func TestWriteTextFileAtomicallyRenameError(t *testing.T) {
	// Write to a path where the target is a directory (rename will fail).
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a file inside target to prevent rename from replacing the directory.
	if err := os.WriteFile(filepath.Join(targetDir, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := writeTextFileAtomically(targetDir, []byte("data"), 0o755, 0o644)
	if err == nil {
		t.Fatal("expected error when target is a non-empty directory")
	}
}
