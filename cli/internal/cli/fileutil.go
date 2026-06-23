package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

type atomicTempFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
	Chmod(os.FileMode) error
	Name() string
}

var (
	createTempFileFn = func(dir string, pattern string) (atomicTempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
	renameFileFn = os.Rename
	syncDirFn    = syncDir
	openDirFn    = func(dir string) (syncableDir, error) {
		return os.Open(dir)
	}
)

// syncableDir is the subset of *os.File used to durably flush a directory.
type syncableDir interface {
	Sync() error
	Close() error
}

// syncDir fsyncs a directory so previous renames in that directory survive a crash.
func syncDir(dir string) error {
	d, err := openDirFn(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

// ensureDirDurably creates dir and fsyncs each parent directory that receives
// a new child directory entry, so subsequent file renames under dir have a
// durable ancestor path.
func ensureDirDurably(dir string, perm os.FileMode) error {
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", dir)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	parent := filepath.Dir(dir)
	if parent != dir {
		if err := ensureDirDurably(parent, perm); err != nil {
			return err
		}
	}
	if err := os.Mkdir(dir, perm); err != nil {
		if os.IsExist(err) {
			info, statErr := os.Stat(dir)
			if statErr == nil && info.IsDir() {
				return nil
			}
		}
		return err
	}
	if err := syncDirFn(parent); err != nil {
		return fmt.Errorf("sync parent directory for created directory %s: %w", dir, err)
	}
	return nil
}

// writeTextFileAtomically replaces path with content using a temp file + rename.
// Args: dirPerm controls parent directory creation; filePerm controls final file permissions.
// Returns: nil on success or an error describing the failed write step.
func writeTextFileAtomically(path string, content []byte, dirPerm os.FileMode, filePerm os.FileMode) error {
	if err := ensureDirDurably(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", path, err)
	}

	tempFile, err := createTempFileFn(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(filePerm); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("set permissions on %s: %w", tempPath, err)
	}
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp file %s: %w", tempPath, err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync temp file %s: %w", tempPath, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tempPath, err)
	}
	if err := renameFileFn(tempPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	cleanupTemp = false
	if err := syncDirFn(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync parent directory for %s: %w", path, err)
	}
	return nil
}
