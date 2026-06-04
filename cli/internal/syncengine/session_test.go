package syncengine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestCursorStoreReadWriteRoundTrip(t *testing.T) {
	store, err := NewCursorStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	want := time.Date(2026, 3, 8, 14, 15, 16, 987654321, time.FixedZone("EST", -5*60*60))
	if err := store.Write(want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, exists, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !exists {
		t.Fatal("Read() exists = false, want true")
	}

	want = want.UTC().Truncate(time.Millisecond)
	if !got.Equal(want) {
		t.Fatalf("Read() = %s, want %s", got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

func TestCursorStoreReadMissingReturnsExistsFalse(t *testing.T) {
	store, err := NewCursorStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	got, exists, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if exists {
		t.Fatal("Read() exists = true, want false")
	}
	if !got.IsZero() {
		t.Fatalf("Read() = %s, want zero time", got.Format(time.RFC3339Nano))
	}
}

func TestCursorStoreReadRejectsMalformedTimestamp(t *testing.T) {
	store, err := NewCursorStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	if err := os.WriteFile(store.Path(), []byte("not-a-timestamp\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, _, err := store.Read(); err == nil {
		t.Fatal("Read() error = nil, want parse failure")
	}
}

func TestManagerBeginCapturesStartAndCompleteWritesIt(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	lastSync := time.Date(2026, 3, 8, 9, 0, 0, 999999999, time.UTC)
	if err := manager.store.Write(lastSync); err != nil {
		t.Fatalf("store.Write() error = %v", err)
	}

	startedAt := time.Date(2026, 3, 8, 9, 30, 0, 123456789, time.FixedZone("EDT", -4*60*60))
	manager.nowFn = func() time.Time { return startedAt }

	window, lock, err := manager.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			t.Fatalf("Release() error = %v", releaseErr)
		}
	}()

	wantLastSync := lastSync.Truncate(time.Millisecond)
	if !window.LastSync.Equal(wantLastSync) {
		t.Fatalf("window.LastSync = %s, want %s", window.LastSync.Format(time.RFC3339Nano), wantLastSync.Format(time.RFC3339Nano))
	}

	wantStartedAt := startedAt.UTC().Truncate(time.Millisecond)
	if !window.StartedAt.Equal(wantStartedAt) {
		t.Fatalf("window.StartedAt = %s, want %s", window.StartedAt.Format(time.RFC3339Nano), wantStartedAt.Format(time.RFC3339Nano))
	}

	if err := manager.Complete(window); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	got, exists, err := manager.store.Read()
	if err != nil {
		t.Fatalf("store.Read() error = %v", err)
	}
	if !exists {
		t.Fatal("store.Read() exists = false, want true")
	}
	if !got.Equal(wantStartedAt) {
		t.Fatalf("store.Read() = %s, want %s", got.Format(time.RFC3339Nano), wantStartedAt.Format(time.RFC3339Nano))
	}
}

func TestManagerBeginPreventsConcurrentSync(t *testing.T) {
	managerOne, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	managerTwo, err := NewManager(filepathDir(managerOne.lockPath))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	_, lock, err := managerOne.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	_, _, err = managerTwo.Begin()
	if !errors.Is(err, ErrSyncLocked) {
		t.Fatalf("Begin() error = %v, want %v", err, ErrSyncLocked)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	_, secondLock, err := managerTwo.Begin()
	if err != nil {
		t.Fatalf("Begin() after release error = %v", err)
	}
	if err := secondLock.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func TestManagerBeginReleasesLockWhenCursorReadFails(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if err := os.WriteFile(manager.store.Path(), []byte("bad\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, _, err := manager.Begin(); err == nil {
		t.Fatal("Begin() error = nil, want parse failure")
	}

	if _, err := os.Stat(manager.lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock file stat error = %v, want not-exist", err)
	}
}

func TestNewCursorStoreRejectsEmptyDir(t *testing.T) {
	for _, input := range []string{"", "  ", "\t"} {
		store, err := NewCursorStore(input)
		if err == nil {
			t.Fatalf("NewCursorStore(%q) error = nil, want validation failure", input)
		}
		if store != nil {
			t.Fatalf("NewCursorStore(%q) returned non-nil store on error", input)
		}
	}
}

func TestCursorStorePathNilReceiver(t *testing.T) {
	var store *CursorStore
	got := store.Path()
	if got != "" {
		t.Fatalf("Path() = %q, want empty string for nil receiver", got)
	}
}

func TestCursorStoreReadNilReceiver(t *testing.T) {
	var store *CursorStore
	_, _, err := store.Read()
	if err == nil {
		t.Fatal("Read() on nil receiver: error = nil, want validation failure")
	}
}

func TestCursorStoreReadEmptyFileContent(t *testing.T) {
	store, err := NewCursorStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	// Write an empty file (just whitespace).
	if err := os.WriteFile(store.Path(), []byte("   \n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, _, err = store.Read()
	if err == nil {
		t.Fatal("Read() with empty content: error = nil, want empty-timestamp failure")
	}
}

func TestCursorStoreWriteNilReceiver(t *testing.T) {
	var store *CursorStore
	err := store.Write(time.Now())
	if err == nil {
		t.Fatal("Write() on nil receiver: error = nil, want validation failure")
	}
}

func TestCursorStoreWriteZeroTimestamp(t *testing.T) {
	store, err := NewCursorStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	err = store.Write(time.Time{})
	if err == nil {
		t.Fatal("Write(zero) error = nil, want validation failure")
	}
}

func TestNewManagerRejectsEmptyDir(t *testing.T) {
	mgr, err := NewManager("")
	if err == nil {
		t.Fatal("NewManager(\"\") error = nil, want validation failure")
	}
	if mgr != nil {
		t.Fatal("NewManager(\"\") returned non-nil manager on error")
	}
}

func TestManagerCompleteNilManager(t *testing.T) {
	var mgr *Manager
	err := mgr.Complete(SyncWindow{StartedAt: time.Now()})
	if err == nil {
		t.Fatal("Complete() on nil manager: error = nil, want validation failure")
	}
}

func TestManagerCompleteZeroStartedAt(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = mgr.Complete(SyncWindow{StartedAt: time.Time{}})
	if err == nil {
		t.Fatal("Complete(zero StartedAt) error = nil, want validation failure")
	}
}

func TestManagerBeginNilManager(t *testing.T) {
	var mgr *Manager
	_, _, err := mgr.Begin()
	if err == nil {
		t.Fatal("Begin() on nil manager: error = nil, want validation failure")
	}
}

func TestAcquireFileLockRejectsEmptyPath(t *testing.T) {
	lock, err := AcquireFileLock("")
	if err == nil {
		t.Fatal("AcquireFileLock(\"\") error = nil, want validation failure")
	}
	if lock != nil {
		t.Fatal("AcquireFileLock(\"\") returned non-nil lock on error")
	}
}

func TestFileLockReleaseNilLock(t *testing.T) {
	var lock *FileLock
	err := lock.Release()
	if err == nil {
		t.Fatal("Release() on nil lock: error = nil, want validation failure")
	}
}

func TestFileLockReleaseAlreadyReleased(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	lock, err := AcquireFileLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireFileLock() error = %v", err)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("first Release() error = %v", err)
	}

	err = lock.Release()
	if err == nil {
		t.Fatal("second Release() error = nil, want already-released failure")
	}
}

func TestAcquireFileLockCreatesDirAndWritesContent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	lockPath := filepath.Join(dir, "test.lock")

	lock, err := AcquireFileLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireFileLock() error = %v", err)
	}
	defer func() { _ = lock.Release() }()

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var metadata lockFileMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("lock file content is not JSON metadata: %v", err)
	}
	if metadata.PID != os.Getpid() {
		t.Fatalf("lock metadata PID = %d, want %d", metadata.PID, os.Getpid())
	}
	if strings.TrimSpace(metadata.Hostname) == "" {
		t.Fatal("lock metadata hostname is empty")
	}
	if metadata.StartedAt.IsZero() {
		t.Fatal("lock metadata started_at is zero")
	}
}

func TestCursorStoreWriteCreatesNestedDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "pc")
	store, err := NewCursorStore(dir)
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	ts := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	if err := store.Write(ts); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, exists, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !exists {
		t.Fatal("Read() exists = false, want true")
	}
	if !got.Equal(ts) {
		t.Fatalf("Read() = %s, want %s", got.Format(time.RFC3339Nano), ts.Format(time.RFC3339Nano))
	}
}

func TestManagerBeginFirstSyncNoExistingCursor(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	fixedNow := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	mgr.nowFn = func() time.Time { return fixedNow }

	window, lock, err := mgr.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = lock.Release() }()

	if !window.LastSync.IsZero() {
		t.Fatalf("window.LastSync = %s, want zero (first sync)", window.LastSync.Format(time.RFC3339Nano))
	}
	if !window.StartedAt.Equal(fixedNow) {
		t.Fatalf("window.StartedAt = %s, want %s", window.StartedAt.Format(time.RFC3339Nano), fixedNow.Format(time.RFC3339Nano))
	}
}

func TestCursorStoreReadWithEmptyPathField(t *testing.T) {
	store := &CursorStore{path: "   "}
	_, _, err := store.Read()
	if err == nil {
		t.Fatal("Read() with blank path: error = nil, want validation failure")
	}
}

func TestCursorStoreWriteWithEmptyPathField(t *testing.T) {
	store := &CursorStore{path: "   "}
	err := store.Write(time.Now())
	if err == nil {
		t.Fatal("Write() with blank path: error = nil, want validation failure")
	}
}

func TestCursorStoreReadNonNotExistError(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCursorStore(dir)
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	// Place a directory at the file path so os.ReadFile returns a non-IsNotExist error.
	if err := os.MkdirAll(store.Path(), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	_, _, err = store.Read()
	if err == nil {
		t.Fatal("Read() with directory at path: error = nil, want read failure")
	}
}

func TestCursorStoreWriteAtomicRenameSucceeds(t *testing.T) {
	// Write twice to confirm the atomic rename path works when overwriting.
	store, err := NewCursorStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	ts1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 6, 15, 12, 30, 0, 0, time.UTC)

	if err := store.Write(ts1); err != nil {
		t.Fatalf("Write(ts1) error = %v", err)
	}
	if err := store.Write(ts2); err != nil {
		t.Fatalf("Write(ts2) error = %v", err)
	}

	got, exists, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !exists {
		t.Fatal("Read() exists = false, want true")
	}
	if !got.Equal(ts2) {
		t.Fatalf("Read() = %s, want %s", got.Format(time.RFC3339Nano), ts2.Format(time.RFC3339Nano))
	}
}

func TestCursorStoreWriteReadonlyDirFails(t *testing.T) {
	dir := t.TempDir()
	readonlyDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(readonlyDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	store, err := NewCursorStore(readonlyDir)
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	// Make the directory readonly so temp file creation fails.
	if err := os.Chmod(readonlyDir, 0o500); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonlyDir, 0o700) })

	err = store.Write(time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("Write() to readonly dir: error = nil, want failure")
	}
}

func TestAcquireFileLockExistingLockReturnsErrSyncLocked(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	lock1, err := AcquireFileLock(lockPath)
	if err != nil {
		t.Fatalf("first AcquireFileLock() error = %v", err)
	}

	_, err = AcquireFileLock(lockPath)
	if !errors.Is(err, ErrSyncLocked) {
		t.Fatalf("second AcquireFileLock() error = %v, want %v", err, ErrSyncLocked)
	}

	if err := lock1.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func TestAcquireFileLockRecoversStaleSameHostLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("Hostname() error = %v", err)
	}
	staleMetadata := lockFileMetadata{
		PID:       99_999_999,
		Hostname:  hostname,
		StartedAt: time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(staleMetadata)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(lockPath, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	lock, err := AcquireFileLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireFileLock() error = %v, want stale lock recovery", err)
	}
	defer func() { _ = lock.Release() }()

	recoveredData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var recoveredMetadata lockFileMetadata
	if err := json.Unmarshal(recoveredData, &recoveredMetadata); err != nil {
		t.Fatalf("recovered lock metadata unmarshal error = %v", err)
	}
	if recoveredMetadata.PID != os.Getpid() {
		t.Fatalf("recovered lock PID = %d, want %d", recoveredMetadata.PID, os.Getpid())
	}
}

func TestAcquireFileLockKeepsActiveSameHostLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("Hostname() error = %v", err)
	}
	activeMetadata := lockFileMetadata{
		PID:       os.Getpid(),
		Hostname:  hostname,
		StartedAt: time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(activeMetadata)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(lockPath, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err = AcquireFileLock(lockPath)
	if !errors.Is(err, ErrSyncLocked) {
		t.Fatalf("AcquireFileLock() error = %v, want %v", err, ErrSyncLocked)
	}
}

func TestAcquireFileLockKeepsUnparseableLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	if err := os.WriteFile(lockPath, []byte("locked\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := AcquireFileLock(lockPath)
	if !errors.Is(err, ErrSyncLocked) {
		t.Fatalf("AcquireFileLock() error = %v, want %v", err, ErrSyncLocked)
	}
}

func TestInspectFileLockClassifiesUnparseableLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	inspection, err := InspectFileLock(lockPath)
	if err != nil {
		t.Fatalf("InspectFileLock(absent) error = %v", err)
	}
	if inspection.Exists || inspection.HasMetadata {
		t.Fatalf("absent lock inspection = %+v, want no file and no metadata", inspection)
	}

	if err := os.WriteFile(lockPath, []byte("locked\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	inspection, err = InspectFileLock(lockPath)
	if err != nil {
		t.Fatalf("InspectFileLock(unparseable) error = %v", err)
	}
	if !inspection.Exists || inspection.HasMetadata {
		t.Fatalf("unparseable lock inspection = %+v, want file without metadata", inspection)
	}
}

func TestAcquireFileLockSerializesStaleRecovery(t *testing.T) {
	origHostname := osHostname
	origKill := syscallKill
	defer func() {
		osHostname = origHostname
		syscallKill = origKill
	}()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")
	hostname := "test-host"
	staleMetadata := lockFileMetadata{
		PID:       99_999_999,
		Hostname:  hostname,
		StartedAt: time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(staleMetadata)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(lockPath, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	osHostname = func() (string, error) {
		return hostname, nil
	}
	enteredProcessCheck := make(chan struct{})
	allowRecovery := make(chan struct{})
	var processCheckCalls int32
	syscallKill = func(pid int, sig syscall.Signal) error {
		call := atomic.AddInt32(&processCheckCalls, 1)
		if call == 1 {
			close(enteredProcessCheck)
			<-allowRecovery
		}
		return syscall.ESRCH
	}

	type lockResult struct {
		lock *FileLock
		err  error
	}
	firstResult := make(chan lockResult, 1)
	go func() {
		lock, err := AcquireFileLock(lockPath)
		firstResult <- lockResult{lock: lock, err: err}
	}()

	<-enteredProcessCheck
	secondLock, err := AcquireFileLock(lockPath)
	if !errors.Is(err, ErrSyncLocked) {
		if secondLock != nil {
			_ = secondLock.Release()
		}
		t.Fatalf("concurrent AcquireFileLock() error = %v, want %v", err, ErrSyncLocked)
	}
	if secondLock != nil {
		t.Fatal("concurrent AcquireFileLock() returned a lock while stale recovery was in progress")
	}

	close(allowRecovery)
	first := <-firstResult
	if first.err != nil {
		t.Fatalf("first AcquireFileLock() error = %v", first.err)
	}
	defer func() { _ = first.lock.Release() }()

	if got := atomic.LoadInt32(&processCheckCalls); got != 1 {
		t.Fatalf("process checks = %d, want 1 serialized stale recovery check", got)
	}
}

func TestAcquireLockOperationGuardRejectsConcurrentHolder(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "test.lock")

	guard, err := acquireLockOperationGuard(lockPath)
	if err != nil {
		t.Fatalf("acquireLockOperationGuard() error = %v", err)
	}
	defer func() { _ = guard.Release() }()

	secondGuard, err := acquireLockOperationGuard(lockPath)
	if !errors.Is(err, ErrSyncLocked) {
		if secondGuard != nil {
			_ = secondGuard.Release()
		}
		t.Fatalf("second acquireLockOperationGuard() error = %v, want %v", err, ErrSyncLocked)
	}
	if secondGuard != nil {
		t.Fatal("second acquireLockOperationGuard() returned guard while first guard is held")
	}
}

func TestAcquireLockOperationGuardChmodError(t *testing.T) {
	origChmod := osFileChmod
	defer func() { osFileChmod = origChmod }()

	osFileChmod = func(f *os.File, mode os.FileMode) error {
		return fmt.Errorf("injected chmod error")
	}

	guard, err := acquireLockOperationGuard(filepath.Join(t.TempDir(), "test.lock"))
	if err == nil {
		if guard != nil {
			_ = guard.Release()
		}
		t.Fatal("acquireLockOperationGuard() error = nil, want chmod failure")
	}
	if errors.Is(err, ErrSyncLocked) {
		t.Fatal("acquireLockOperationGuard() error is ErrSyncLocked, want chmod failure")
	}
}

func TestAcquireLockOperationGuardFlockError(t *testing.T) {
	origFlock := syscallFlock
	defer func() { syscallFlock = origFlock }()

	syscallFlock = func(fd int, how int) error {
		return syscall.EINVAL
	}

	guard, err := acquireLockOperationGuard(filepath.Join(t.TempDir(), "test.lock"))
	if err == nil {
		if guard != nil {
			_ = guard.Release()
		}
		t.Fatal("acquireLockOperationGuard() error = nil, want flock failure")
	}
	if errors.Is(err, ErrSyncLocked) {
		t.Fatal("acquireLockOperationGuard() error is ErrSyncLocked, want flock failure")
	}
}

func TestLockOperationGuardReleaseValidationAndDoubleRelease(t *testing.T) {
	var nilGuard *lockOperationGuard
	if err := nilGuard.Release(); err == nil {
		t.Fatal("Release() on nil guard: error = nil, want validation failure")
	}

	guard, err := acquireLockOperationGuard(filepath.Join(t.TempDir(), "test.lock"))
	if err != nil {
		t.Fatalf("acquireLockOperationGuard() error = %v", err)
	}
	if err := guard.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := guard.Release(); err == nil {
		t.Fatal("second Release() error = nil, want already-released failure")
	}
}

func TestLockOperationGuardReleaseUnlockError(t *testing.T) {
	origFlock := syscallFlock
	defer func() { syscallFlock = origFlock }()

	guard, err := acquireLockOperationGuard(filepath.Join(t.TempDir(), "test.lock"))
	if err != nil {
		t.Fatalf("acquireLockOperationGuard() error = %v", err)
	}
	syscallFlock = func(fd int, how int) error {
		if how == syscall.LOCK_UN {
			return syscall.EINVAL
		}
		return origFlock(fd, how)
	}

	if err := guard.Release(); err == nil {
		t.Fatal("Release() error = nil, want unlock failure")
	}
}

func TestLockOperationGuardReleaseCloseError(t *testing.T) {
	origClose := osFileClose
	defer func() { osFileClose = origClose }()

	guard, err := acquireLockOperationGuard(filepath.Join(t.TempDir(), "test.lock"))
	if err != nil {
		t.Fatalf("acquireLockOperationGuard() error = %v", err)
	}
	osFileClose = func(f *os.File) error {
		_ = f.Close()
		return fmt.Errorf("injected close error")
	}

	if err := guard.Release(); err == nil {
		t.Fatal("Release() error = nil, want close failure")
	}
}

func TestAcquireFileLockReadonlyDirFails(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "lockdir")
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Make the directory readonly so file creation fails with a permission error.
	if err := os.Chmod(lockDir, 0o500); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockDir, 0o700) })

	lockPath := filepath.Join(lockDir, "test.lock")
	_, err := AcquireFileLock(lockPath)
	if err == nil {
		t.Fatal("AcquireFileLock() to readonly dir: error = nil, want failure")
	}
	// The error should NOT be ErrSyncLocked since the file doesn't exist.
	if errors.Is(err, ErrSyncLocked) {
		t.Fatal("AcquireFileLock() error is ErrSyncLocked, want permission error")
	}
}

func TestFileLockReleaseAfterRemove(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	lock, err := AcquireFileLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireFileLock() error = %v", err)
	}

	// Externally remove the lock file before releasing. The close should succeed,
	// but remove will fail.
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("os.Remove() error = %v", err)
	}

	err = lock.Release()
	if err == nil {
		t.Fatal("Release() after external remove: error = nil, want remove failure")
	}
}

func TestManagerBeginCompleteFullCycle(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	now := time.Date(2026, 3, 8, 16, 0, 0, 0, time.UTC)
	mgr.nowFn = func() time.Time { return now }

	window, lock, err := mgr.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := mgr.Complete(window); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	// Second sync should see the previous cursor.
	later := time.Date(2026, 3, 8, 17, 0, 0, 0, time.UTC)
	mgr.nowFn = func() time.Time { return later }

	window2, lock2, err := mgr.Begin()
	if err != nil {
		t.Fatalf("second Begin() error = %v", err)
	}
	defer func() { _ = lock2.Release() }()

	if !window2.LastSync.Equal(now) {
		t.Fatalf("window2.LastSync = %s, want %s", window2.LastSync.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	}
	if !window2.StartedAt.Equal(later) {
		t.Fatalf("window2.StartedAt = %s, want %s", window2.StartedAt.Format(time.RFC3339Nano), later.Format(time.RFC3339Nano))
	}
}

func TestCursorStoreWriteTruncatesSubMillisecondPrecision(t *testing.T) {
	store, err := NewCursorStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	// Write a timestamp with sub-millisecond precision.
	ts := time.Date(2026, 3, 8, 12, 0, 0, 123456789, time.UTC)
	if err := store.Write(ts); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, _, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	want := time.Date(2026, 3, 8, 12, 0, 0, 123000000, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("Read() = %s, want %s (truncated)", got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

func TestManagerBeginZeroNowFnReleasesLock(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Return a zero time from nowFn to trigger the startedAt.IsZero() branch.
	mgr.nowFn = func() time.Time { return time.Time{} }

	_, _, err = mgr.Begin()
	if err == nil {
		t.Fatal("Begin() with zero nowFn: error = nil, want validation failure")
	}

	// Verify the lock was released (we should be able to acquire it again).
	lock, err := AcquireFileLock(mgr.lockPath)
	if err != nil {
		t.Fatalf("AcquireFileLock() after failed Begin: error = %v (lock was not released)", err)
	}
	_ = lock.Release()
}

func TestCursorStoreWriteMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file where the directory should be, so MkdirAll fails.
	parentPath := filepath.Join(dir, "blocker")
	if err := os.WriteFile(parentPath, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store, err := NewCursorStore(filepath.Join(parentPath, "sub"))
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	err = store.Write(time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("Write() with blocked MkdirAll: error = nil, want failure")
	}
}

func TestCursorStoreWriteRenameFails(t *testing.T) {
	dir := t.TempDir()
	store, err := NewCursorStore(dir)
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	// Place a directory at the last_sync path so os.Rename fails
	// (can't rename a regular file over a directory on most systems).
	if err := os.MkdirAll(store.Path(), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	err = store.Write(time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("Write() with directory at target path: error = nil, want rename failure")
	}
}

func TestAcquireFileLockMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file where the lock directory should be.
	blockerPath := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blockerPath, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	lockPath := filepath.Join(blockerPath, "sub", "test.lock")
	_, err := AcquireFileLock(lockPath)
	if err == nil {
		t.Fatal("AcquireFileLock() with blocked MkdirAll: error = nil, want failure")
	}
	if errors.Is(err, ErrSyncLocked) {
		t.Fatal("AcquireFileLock() error should not be ErrSyncLocked for MkdirAll failure")
	}
}

func TestFileLockReleaseCloseError(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	origClose := osFileClose
	defer func() { osFileClose = origClose }()

	lock, err := AcquireFileLock(lockPath)
	if err != nil {
		t.Fatalf("AcquireFileLock() error = %v", err)
	}

	// Inject a close error for the Release call.
	osFileClose = func(f *os.File) error {
		_ = f.Close() // actually close to avoid leaking fd
		return fmt.Errorf("injected close error")
	}

	err = lock.Release()
	if err == nil {
		t.Fatal("Release() with injected close error: error = nil, want failure")
	}
}

func TestCursorStoreWriteChmodError(t *testing.T) {
	origChmod := osFileChmod
	defer func() { osFileChmod = origChmod }()

	store, err := NewCursorStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	osFileChmod = func(f *os.File, mode os.FileMode) error {
		return fmt.Errorf("injected chmod error")
	}

	err = store.Write(time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("Write() with injected Chmod error: error = nil, want failure")
	}
}

func TestCursorStoreWriteWriteStringError(t *testing.T) {
	origWriteString := osFileWriteString
	defer func() { osFileWriteString = origWriteString }()

	store, err := NewCursorStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	osFileWriteString = func(f *os.File, s string) (int, error) {
		return 0, fmt.Errorf("injected write error")
	}

	err = store.Write(time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("Write() with injected WriteString error: error = nil, want failure")
	}
}

func TestCursorStoreWriteSyncError(t *testing.T) {
	origSync := osFileSync
	defer func() { osFileSync = origSync }()

	store, err := NewCursorStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	osFileSync = func(f *os.File) error {
		return fmt.Errorf("injected sync error")
	}

	err = store.Write(time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("Write() with injected Sync error: error = nil, want failure")
	}
}

func TestCursorStoreWriteCloseError(t *testing.T) {
	origClose := osFileClose
	defer func() { osFileClose = origClose }()

	store, err := NewCursorStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}

	osFileClose = func(f *os.File) error {
		_ = f.Close() // actually close to avoid leaking fd
		return fmt.Errorf("injected close error")
	}

	err = store.Write(time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("Write() with injected Close error: error = nil, want failure")
	}
}

func TestAcquireFileLockWriteStringError(t *testing.T) {
	origWriteString := osFileWriteString
	defer func() { osFileWriteString = origWriteString }()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	osFileWriteString = func(f *os.File, s string) (int, error) {
		return 0, fmt.Errorf("injected write error")
	}

	_, err := AcquireFileLock(lockPath)
	if err == nil {
		t.Fatal("AcquireFileLock() with injected WriteString error: error = nil, want failure")
	}
	if errors.Is(err, ErrSyncLocked) {
		t.Fatal("error should not be ErrSyncLocked for WriteString failure")
	}

	// Verify lock file was cleaned up.
	if _, statErr := os.Stat(lockPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("lock file still exists after WriteString failure (stat error = %v)", statErr)
	}
}

func TestAcquireFileLockSyncError(t *testing.T) {
	origSync := osFileSync
	defer func() { osFileSync = origSync }()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	osFileSync = func(f *os.File) error {
		return fmt.Errorf("injected sync error")
	}

	_, err := AcquireFileLock(lockPath)
	if err == nil {
		t.Fatal("AcquireFileLock() with injected Sync error: error = nil, want failure")
	}
	if errors.Is(err, ErrSyncLocked) {
		t.Fatal("error should not be ErrSyncLocked for Sync failure")
	}

	// Verify lock file was cleaned up.
	if _, statErr := os.Stat(lockPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("lock file still exists after Sync failure (stat error = %v)", statErr)
	}
}

func TestAcquireFileLockHostnameErrorCleansLock(t *testing.T) {
	origHostname := osHostname
	defer func() { osHostname = origHostname }()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")
	osHostname = func() (string, error) {
		return "", fmt.Errorf("injected hostname error")
	}

	_, err := AcquireFileLock(lockPath)
	if err == nil {
		t.Fatal("AcquireFileLock() with hostname error: error = nil, want failure")
	}
	if errors.Is(err, ErrSyncLocked) {
		t.Fatal("error should not be ErrSyncLocked for hostname failure")
	}
	if _, statErr := os.Stat(lockPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("lock file still exists after hostname failure (stat error = %v)", statErr)
	}
}

func TestAcquireFileLockMarshalErrorCleansLock(t *testing.T) {
	origMarshal := jsonMarshalLock
	defer func() { jsonMarshalLock = origMarshal }()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")
	jsonMarshalLock = func(lockFileMetadata) ([]byte, error) {
		return nil, fmt.Errorf("injected marshal error")
	}

	_, err := AcquireFileLock(lockPath)
	if err == nil {
		t.Fatal("AcquireFileLock() with marshal error: error = nil, want failure")
	}
	if errors.Is(err, ErrSyncLocked) {
		t.Fatal("error should not be ErrSyncLocked for marshal failure")
	}
	if _, statErr := os.Stat(lockPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("lock file still exists after marshal failure (stat error = %v)", statErr)
	}
}

func TestNewLockFileMetadataRejectsEmptyHostname(t *testing.T) {
	origHostname := osHostname
	defer func() { osHostname = origHostname }()

	osHostname = func() (string, error) {
		return "   ", nil
	}

	_, err := newLockFileMetadata()
	if err == nil {
		t.Fatal("newLockFileMetadata() error = nil, want empty-hostname failure")
	}
}

func TestRecoverStaleLockMissingPathReturnsRecovered(t *testing.T) {
	recovered, err := recoverStaleLock(filepath.Join(t.TempDir(), "missing.lock"))
	if err != nil {
		t.Fatalf("recoverStaleLock() error = %v", err)
	}
	if !recovered {
		t.Fatal("recoverStaleLock() recovered = false, want true for missing lock")
	}
}

func TestRecoverStaleLockReadError(t *testing.T) {
	dir := t.TempDir()

	recovered, err := recoverStaleLock(dir)
	if err == nil {
		t.Fatal("recoverStaleLock() error = nil, want read failure for directory path")
	}
	if recovered {
		t.Fatal("recoverStaleLock() recovered = true, want false on read failure")
	}
}

func TestRecoverStaleLockKeepsDifferentHostLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "test.lock")
	metadata := lockFileMetadata{
		PID:       99_999_999,
		Hostname:  "another-host",
		StartedAt: time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(lockPath, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	recovered, err := recoverStaleLock(lockPath)
	if err != nil {
		t.Fatalf("recoverStaleLock() error = %v", err)
	}
	if recovered {
		t.Fatal("recoverStaleLock() recovered = true, want false for different host")
	}
}

func TestRecoverStaleLockChangedByAnotherProcessReturnsRecovered(t *testing.T) {
	origHostname := osHostname
	origKill := syscallKill
	defer func() {
		osHostname = origHostname
		syscallKill = origKill
	}()

	lockPath := filepath.Join(t.TempDir(), "test.lock")
	hostname := "test-host"
	staleMetadata := lockFileMetadata{
		PID:       99_999_999,
		Hostname:  hostname,
		StartedAt: time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
	}
	currentMetadata := lockFileMetadata{
		PID:       os.Getpid(),
		Hostname:  hostname,
		StartedAt: time.Date(2026, 3, 8, 12, 1, 0, 0, time.UTC),
	}
	staleData, err := json.Marshal(staleMetadata)
	if err != nil {
		t.Fatalf("Marshal(stale) error = %v", err)
	}
	currentData, err := json.Marshal(currentMetadata)
	if err != nil {
		t.Fatalf("Marshal(current) error = %v", err)
	}
	if err := os.WriteFile(lockPath, append(staleData, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile(stale) error = %v", err)
	}

	osHostname = func() (string, error) {
		return hostname, nil
	}
	syscallKill = func(pid int, sig syscall.Signal) error {
		if err := os.WriteFile(lockPath, append(currentData, '\n'), 0o600); err != nil {
			t.Fatalf("WriteFile(current) error = %v", err)
		}
		return syscall.ESRCH
	}

	recovered, err := recoverStaleLock(lockPath)
	if err != nil {
		t.Fatalf("recoverStaleLock() error = %v", err)
	}
	if !recovered {
		t.Fatal("recoverStaleLock() recovered = false, want true after concurrent replacement")
	}

	got, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.TrimSpace(string(got)) != string(currentData) {
		t.Fatalf("lock content = %q, want current metadata %q", strings.TrimSpace(string(got)), string(currentData))
	}
}

func TestRecoverStaleLockRemovedByAnotherProcessReturnsRecovered(t *testing.T) {
	origHostname := osHostname
	origKill := syscallKill
	defer func() {
		osHostname = origHostname
		syscallKill = origKill
	}()

	lockPath := filepath.Join(t.TempDir(), "test.lock")
	hostname := "test-host"
	metadata := lockFileMetadata{
		PID:       99_999_999,
		Hostname:  hostname,
		StartedAt: time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(lockPath, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	osHostname = func() (string, error) {
		return hostname, nil
	}
	syscallKill = func(pid int, sig syscall.Signal) error {
		if err := os.Remove(lockPath); err != nil {
			t.Fatalf("Remove() error = %v", err)
		}
		return syscall.ESRCH
	}

	recovered, err := recoverStaleLock(lockPath)
	if err != nil {
		t.Fatalf("recoverStaleLock() error = %v", err)
	}
	if !recovered {
		t.Fatal("recoverStaleLock() recovered = false, want true after concurrent removal")
	}
}

func TestRecoverStaleLockRereadError(t *testing.T) {
	origHostname := osHostname
	origKill := syscallKill
	defer func() {
		osHostname = origHostname
		syscallKill = origKill
	}()

	lockPath := filepath.Join(t.TempDir(), "test.lock")
	hostname := "test-host"
	metadata := lockFileMetadata{
		PID:       99_999_999,
		Hostname:  hostname,
		StartedAt: time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(lockPath, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	osHostname = func() (string, error) {
		return hostname, nil
	}
	syscallKill = func(pid int, sig syscall.Signal) error {
		if err := os.Remove(lockPath); err != nil {
			t.Fatalf("Remove() error = %v", err)
		}
		if err := os.Mkdir(lockPath, 0o700); err != nil {
			t.Fatalf("Mkdir() error = %v", err)
		}
		return syscall.ESRCH
	}

	recovered, err := recoverStaleLock(lockPath)
	if err == nil {
		t.Fatal("recoverStaleLock() error = nil, want re-read failure")
	}
	if recovered {
		t.Fatal("recoverStaleLock() recovered = true, want false on re-read failure")
	}
}

func TestRecoverStaleLockHostnameError(t *testing.T) {
	origHostname := osHostname
	defer func() { osHostname = origHostname }()

	lockPath := filepath.Join(t.TempDir(), "test.lock")
	metadata := lockFileMetadata{
		PID:       99_999_999,
		Hostname:  "test-host",
		StartedAt: time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(lockPath, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	osHostname = func() (string, error) {
		return "", fmt.Errorf("injected hostname error")
	}

	recovered, err := recoverStaleLock(lockPath)
	if err == nil {
		t.Fatal("recoverStaleLock() error = nil, want hostname failure")
	}
	if recovered {
		t.Fatal("recoverStaleLock() recovered = true, want false on hostname failure")
	}
}

func TestProcessExistsBranches(t *testing.T) {
	origKill := syscallKill
	defer func() { syscallKill = origKill }()

	exists, err := processExists(0)
	if err != nil {
		t.Fatalf("processExists(0) error = %v", err)
	}
	if exists {
		t.Fatal("processExists(0) exists = true, want false")
	}

	tests := []struct {
		name       string
		killErr    error
		wantExists bool
		wantErr    bool
	}{
		{name: "alive", killErr: nil, wantExists: true},
		{name: "missing", killErr: syscall.ESRCH, wantExists: false},
		{name: "permission", killErr: syscall.EPERM, wantExists: true},
		{name: "unexpected", killErr: syscall.EINVAL, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syscallKill = func(pid int, sig syscall.Signal) error {
				return tt.killErr
			}

			got, gotErr := processExists(123)
			if tt.wantErr {
				if gotErr == nil {
					t.Fatal("processExists() error = nil, want failure")
				}
				return
			}
			if gotErr != nil {
				t.Fatalf("processExists() error = %v", gotErr)
			}
			if got != tt.wantExists {
				t.Fatalf("processExists() = %v, want %v", got, tt.wantExists)
			}
		})
	}
}

func TestParseLockFileMetadataRejectsInvalidMetadata(t *testing.T) {
	validTime := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		data []byte
	}{
		{name: "bad json", data: []byte("{")},
		{name: "missing pid", data: mustMarshalLockMetadata(t, lockFileMetadata{Hostname: "host", StartedAt: validTime})},
		{name: "empty host", data: mustMarshalLockMetadata(t, lockFileMetadata{PID: 1, Hostname: " ", StartedAt: validTime})},
		{name: "zero started at", data: mustMarshalLockMetadata(t, lockFileMetadata{PID: 1, Hostname: "host"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := parseLockFileMetadata(tt.data); ok {
				t.Fatal("parseLockFileMetadata() ok = true, want false")
			}
		})
	}
}

func mustMarshalLockMetadata(t *testing.T, metadata lockFileMetadata) []byte {
	t.Helper()
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return data
}

func filepathDir(path string) string {
	return filepath.Dir(path)
}
