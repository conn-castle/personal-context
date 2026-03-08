package syncengine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	lastSyncFilename = "last_sync"
	lockFilename     = "sync.lock"
	filePermission   = 0o600
	dirPermission    = 0o700
)

// osFileChmod, osFileWriteString, osFileSync, and osFileClose wrap the
// corresponding *os.File methods so tests can inject I/O failures without
// modifying the filesystem. Production code never reassigns these variables.
var (
	osFileChmod       = func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }
	osFileWriteString = func(f *os.File, s string) (int, error) { return f.WriteString(s) }
	osFileSync        = func(f *os.File) error { return f.Sync() }
	osFileClose       = func(f *os.File) error { return f.Close() }
)

// SyncWindow captures the sync cursor observed at the beginning of a sync run.
type SyncWindow struct {
	LastSync  time.Time
	StartedAt time.Time
}

// CursorStore persists the canonical last-sync timestamp under {pcDir}/last_sync.
type CursorStore struct {
	path string
}

// Manager coordinates last-sync persistence with the on-disk sync lock.
type Manager struct {
	store    *CursorStore
	lockPath string
	nowFn    func() time.Time
}

// FileLock represents an acquired sync lock file.
type FileLock struct {
	path     string
	file     *os.File
	released bool
}

// NewCursorStore constructs a last-sync store rooted at the given .pc directory.
func NewCursorStore(pcDir string) (*CursorStore, error) {
	if strings.TrimSpace(pcDir) == "" {
		return nil, fmt.Errorf(".pc directory is required")
	}
	return &CursorStore{path: filepath.Join(pcDir, lastSyncFilename)}, nil
}

// Path returns the last-sync file path.
func (s *CursorStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Read loads the persisted last-sync timestamp.
// Returns exists=false when the file has not been created yet.
func (s *CursorStore) Read() (ts time.Time, exists bool, err error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return time.Time{}, false, fmt.Errorf("cursor store is required")
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("read last sync %s: %w", s.path, err)
	}

	value := strings.TrimSpace(string(data))
	if value == "" {
		return time.Time{}, false, fmt.Errorf("parse last sync %s: timestamp is empty", s.path)
	}

	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse last sync %s: %w", s.path, err)
	}

	return truncateToMillisecond(parsed.UTC()), true, nil
}

// Write stores the sync-start timestamp atomically.
func (s *CursorStore) Write(ts time.Time) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("cursor store is required")
	}
	if ts.IsZero() {
		return fmt.Errorf("last sync timestamp is required")
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirPermission); err != nil {
		return fmt.Errorf("create last sync dir %s: %w", dir, err)
	}

	tempFile, err := os.CreateTemp(dir, ".last-sync-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp last sync file in %s: %w", dir, err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := osFileChmod(tempFile, filePermission); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("set temp last sync permissions %s: %w", tempPath, err)
	}

	value := truncateToMillisecond(ts.UTC()).Format(time.RFC3339Nano) + "\n"
	if _, err := osFileWriteString(tempFile, value); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp last sync %s: %w", tempPath, err)
	}
	if err := osFileSync(tempFile); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync temp last sync %s: %w", tempPath, err)
	}
	if err := osFileClose(tempFile); err != nil {
		return fmt.Errorf("close temp last sync %s: %w", tempPath, err)
	}

	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace last sync %s: %w", s.path, err)
	}
	cleanupTemp = false
	return nil
}

// NewManager constructs a session manager rooted at the given .pc directory.
func NewManager(pcDir string) (*Manager, error) {
	store, err := NewCursorStore(pcDir)
	if err != nil {
		return nil, err
	}
	return &Manager{
		store:    store,
		lockPath: filepath.Join(pcDir, lockFilename),
		nowFn:    time.Now,
	}, nil
}

// Begin acquires the sync lock and captures the sync window start time.
func (m *Manager) Begin() (SyncWindow, *FileLock, error) {
	if m == nil || m.store == nil || strings.TrimSpace(m.lockPath) == "" {
		return SyncWindow{}, nil, fmt.Errorf("sync manager is required")
	}

	lock, err := AcquireFileLock(m.lockPath)
	if err != nil {
		return SyncWindow{}, nil, err
	}

	lastSync, exists, err := m.store.Read()
	if err != nil {
		_ = lock.Release()
		return SyncWindow{}, nil, err
	}
	if !exists {
		lastSync = time.Time{}
	}

	startedAt := truncateToMillisecond(m.nowFn().UTC())
	if startedAt.IsZero() {
		_ = lock.Release()
		return SyncWindow{}, nil, fmt.Errorf("sync start timestamp is required")
	}

	return SyncWindow{
		LastSync:  lastSync,
		StartedAt: startedAt,
	}, lock, nil
}

// Complete records the captured sync-start timestamp as the new cursor.
func (m *Manager) Complete(window SyncWindow) error {
	if m == nil || m.store == nil {
		return fmt.Errorf("sync manager is required")
	}
	if window.StartedAt.IsZero() {
		return fmt.Errorf("sync start timestamp is required")
	}
	return m.store.Write(window.StartedAt)
}

// AcquireFileLock creates the sync lock file using O_EXCL to prevent concurrent syncs.
func AcquireFileLock(path string) (*FileLock, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("lock path is required")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPermission); err != nil {
		return nil, fmt.Errorf("create lock dir %s: %w", dir, err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePermission)
	if err != nil {
		if os.IsExist(err) {
			return nil, ErrSyncLocked
		}
		return nil, fmt.Errorf("create sync lock %s: %w", path, err)
	}

	if _, err := osFileWriteString(file, "locked\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("write sync lock %s: %w", path, err)
	}
	if err := osFileSync(file); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("sync lock %s: %w", path, err)
	}

	return &FileLock{path: path, file: file}, nil
}

// Release removes the sync lock file.
func (l *FileLock) Release() error {
	if l == nil || l.file == nil || strings.TrimSpace(l.path) == "" {
		return fmt.Errorf("sync lock is required")
	}
	if l.released {
		return fmt.Errorf("sync lock %s already released", l.path)
	}
	l.released = true

	closeErr := osFileClose(l.file)
	removeErr := os.Remove(l.path)

	if closeErr != nil {
		return fmt.Errorf("close sync lock %s: %w", l.path, closeErr)
	}
	if removeErr != nil {
		return fmt.Errorf("remove sync lock %s: %w", l.path, removeErr)
	}
	return nil
}

func truncateToMillisecond(ts time.Time) time.Time {
	return ts.UTC().Truncate(time.Millisecond)
}
