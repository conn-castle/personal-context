package cli

import (
	"os"
	"path/filepath"
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
