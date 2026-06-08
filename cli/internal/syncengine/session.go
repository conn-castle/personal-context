package syncengine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	lastSyncFilename = "last_sync"
	lockFilename     = "sync.lock"
	filePermission   = 0o600
	dirPermission    = 0o700
)

// These wrappers let tests inject I/O and process-inspection failures without
// modifying the filesystem. Production code never reassigns these variables.
var (
	osFileChmod       = func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }
	osFileWriteString = func(f *os.File, s string) (int, error) { return f.WriteString(s) }
	osFileSync        = func(f *os.File) error { return f.Sync() }
	osFileClose       = func(f *os.File) error { return f.Close() }
	osHostname        = os.Hostname
	jsonMarshalLock   = func(metadata lockFileMetadata) ([]byte, error) { return json.Marshal(metadata) }
	syscallKill       = syscall.Kill
	syscallFlock      = syscall.Flock
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

type lockOperationGuard struct {
	path     string
	file     *os.File
	released bool
}

type lockFileMetadata struct {
	PID       int       `json:"pid"`
	Hostname  string    `json:"hostname"`
	StartedAt time.Time `json:"started_at"`
}

// LockFileInspection reports whether a lock file exists and whether its
// content is parseable lock metadata.
type LockFileInspection struct {
	Path        string
	Exists      bool
	HasMetadata bool
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

	guard, err := acquireLockOperationGuard(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = guard.Release() }()

	lock, err := createFileLock(path)
	if err == nil {
		return lock, nil
	}
	if !os.IsExist(err) {
		return nil, fmt.Errorf("create sync lock %s: %w", path, err)
	}

	recovered, recoverErr := recoverStaleLock(path)
	if recoverErr != nil {
		return nil, recoverErr
	}
	if !recovered {
		return nil, ErrSyncLocked
	}

	lock, err = createFileLock(path)
	if err == nil {
		return lock, nil
	}
	if os.IsExist(err) {
		return nil, ErrSyncLocked
	}
	return nil, fmt.Errorf("create sync lock %s after stale recovery: %w", path, err)
}

// InspectFileLock reads a sync lock without acquiring or modifying it.
func InspectFileLock(path string) (LockFileInspection, error) {
	if strings.TrimSpace(path) == "" {
		return LockFileInspection{}, fmt.Errorf("lock path is required")
	}
	inspection := LockFileInspection{Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return inspection, nil
		}
		return LockFileInspection{}, fmt.Errorf("read sync lock %s: %w", path, err)
	}
	inspection.Exists = true
	_, inspection.HasMetadata = parseLockFileMetadata(data)
	return inspection, nil
}

func acquireLockOperationGuard(lockPath string) (*lockOperationGuard, error) {
	guardPath := lockPath + ".guard"
	file, err := os.OpenFile(guardPath, os.O_CREATE|os.O_RDWR, filePermission)
	if err != nil {
		return nil, fmt.Errorf("open sync lock guard %s: %w", guardPath, err)
	}
	if err := osFileChmod(file, filePermission); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("set sync lock guard permissions %s: %w", guardPath, err)
	}
	if err := syscallFlock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrSyncLocked
		}
		return nil, fmt.Errorf("acquire sync lock guard %s: %w", guardPath, err)
	}
	return &lockOperationGuard{path: guardPath, file: file}, nil
}

func (g *lockOperationGuard) Release() error {
	if g == nil || g.file == nil || strings.TrimSpace(g.path) == "" {
		return fmt.Errorf("sync lock guard is required")
	}
	if g.released {
		return fmt.Errorf("sync lock guard %s already released", g.path)
	}
	g.released = true

	unlockErr := syscallFlock(int(g.file.Fd()), syscall.LOCK_UN)
	closeErr := osFileClose(g.file)

	if unlockErr != nil {
		return fmt.Errorf("release sync lock guard %s: %w", g.path, unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close sync lock guard %s: %w", g.path, closeErr)
	}
	return nil
}

func createFileLock(path string) (*FileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePermission)
	if err != nil {
		return nil, err
	}

	metadata, err := newLockFileMetadata()
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("build sync lock metadata: %w", err)
	}
	data, err := jsonMarshalLock(metadata)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("marshal sync lock metadata: %w", err)
	}

	if _, err := osFileWriteString(file, string(data)+"\n"); err != nil {
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

func newLockFileMetadata() (lockFileMetadata, error) {
	hostname, err := osHostname()
	if err != nil {
		return lockFileMetadata{}, err
	}
	if strings.TrimSpace(hostname) == "" {
		return lockFileMetadata{}, fmt.Errorf("hostname is empty")
	}
	return lockFileMetadata{
		PID:       os.Getpid(),
		Hostname:  hostname,
		StartedAt: time.Now().UTC(),
	}, nil
}

func recoverStaleLock(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("read existing sync lock %s: %w", path, err)
	}

	metadata, ok := parseLockFileMetadata(data)
	if !ok {
		return false, nil
	}

	hostname, err := osHostname()
	if err != nil {
		return false, fmt.Errorf("read hostname for sync lock recovery: %w", err)
	}
	if metadata.Hostname != hostname {
		return false, nil
	}

	alive, err := processExists(metadata.PID)
	if err != nil {
		return false, fmt.Errorf("inspect sync lock process %d: %w", metadata.PID, err)
	}
	if alive {
		return false, nil
	}

	currentData, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("re-read sync lock %s: %w", path, err)
	}
	if !bytes.Equal(data, currentData) {
		return true, nil
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("remove stale sync lock %s: %w", path, err)
	}
	return true, nil
}

func parseLockFileMetadata(data []byte) (lockFileMetadata, bool) {
	var metadata lockFileMetadata
	if err := json.Unmarshal(bytes.TrimSpace(data), &metadata); err != nil {
		return lockFileMetadata{}, false
	}
	if metadata.PID <= 0 || strings.TrimSpace(metadata.Hostname) == "" || metadata.StartedAt.IsZero() {
		return lockFileMetadata{}, false
	}
	return metadata, true
}

func processExists(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	err := syscallKill(pid, 0)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.ESRCH):
		return false, nil
	case errors.Is(err, syscall.EPERM):
		return true, nil
	default:
		return false, err
	}
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
