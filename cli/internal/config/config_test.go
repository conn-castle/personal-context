package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadWriteRoundTrip(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	original := Config{
		NeonURL:    "postgres://user:pass@localhost:5432/db",
		S3Bucket:   "my-bucket",
		S3Region:   "us-east-1",
		AWSProfile: "personal-context",
	}
	if err := store.Write(original); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	loaded, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if loaded != original {
		t.Fatalf("expected %+v, got %+v", original, loaded)
	}
}

func TestReadWriteRoundTripWithS3Endpoint(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	original := Config{
		NeonURL:          "postgres://user:pass@localhost:5432/db",
		S3Bucket:         "my-bucket",
		S3Region:         "us-east-1",
		AWSProfile:       "personal-context",
		S3Endpoint:       "http://localhost:9000",
		S3ForcePathStyle: true,
	}
	if err := store.Write(original); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	loaded, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if loaded != original {
		t.Fatalf("expected %+v, got %+v", original, loaded)
	}
}

func TestModeIgnoresS3EndpointFields(t *testing.T) {
	// S3Endpoint and S3ForcePathStyle should not affect Mode detection.
	cfg := Config{
		NeonURL:          "postgres://url",
		S3Bucket:         "bucket",
		S3Region:         "us-east-1",
		AWSProfile:       "profile",
		S3Endpoint:       "http://localhost:9000",
		S3ForcePathStyle: true,
	}
	mode, err := cfg.Mode()
	if err != nil {
		t.Fatalf("Mode() error = %v", err)
	}
	if mode != ModeCloud {
		t.Fatalf("expected %q, got %q", ModeCloud, mode)
	}
}

func TestNewStoreRejectsEmptyHomeDir(t *testing.T) {
	if _, err := NewStore(""); err == nil {
		t.Fatal("expected error for empty home directory")
	}
	if _, err := NewStore("   "); err == nil {
		t.Fatal("expected error for whitespace-only home directory")
	}
}

func TestModeDetection(t *testing.T) {
	localMode, err := (Config{}).Mode()
	if err != nil {
		t.Fatalf("local config Mode() error = %v", err)
	}
	if localMode != ModeLocalOnly {
		t.Fatalf("expected %q, got %q", ModeLocalOnly, localMode)
	}

	cloudMode, err := (Config{
		NeonURL:    "postgres://url",
		S3Bucket:   "bucket",
		S3Region:   "us-east-1",
		AWSProfile: "profile",
	}).Mode()
	if err != nil {
		t.Fatalf("cloud config Mode() error = %v", err)
	}
	if cloudMode != ModeCloud {
		t.Fatalf("expected %q, got %q", ModeCloud, cloudMode)
	}
}

func TestModeRejectsPartialCloudConfig(t *testing.T) {
	_, err := (Config{NeonURL: "postgres://url"}).Mode()
	if err == nil {
		t.Fatal("expected error for partial cloud config")
	}
}

func TestModeTreatsWhitespaceOnlyAsUnset(t *testing.T) {
	mode, err := (Config{
		NeonURL:    "   ",
		S3Bucket:   "\t",
		S3Region:   "\n",
		AWSProfile: " ",
	}).Mode()
	if err != nil {
		t.Fatalf("Mode() error = %v", err)
	}
	if mode != ModeLocalOnly {
		t.Fatalf("expected %q, got %q", ModeLocalOnly, mode)
	}
}

func TestReadMissingFile(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.Read()
	if err == nil {
		t.Fatal("expected error when config file is missing")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestReadCorruptFile(t *testing.T) {
	home := t.TempDir()
	store, err := NewStore(home)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	configPath := store.Path()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{not-json}"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := store.Read(); err == nil {
		t.Fatal("expected parse error for corrupt config")
	}
}

func TestWriteEnforces0600Permissions(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if err := store.Write(Config{}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %o", info.Mode().Perm())
	}
}

func TestWriteEnforces0600OnPreExistingFile(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Create the config file with overly broad permissions.
	path := store.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Write should tighten permissions to 0600.
	if err := store.Write(Config{}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected pre-existing file to be tightened to 0600, got %o", info.Mode().Perm())
	}
}

func TestReadRejectsPartialCloudConfig(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	path := store.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"neon_url":"postgres://url"}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := store.Read(); err == nil {
		t.Fatal("expected error for partial cloud config, got nil")
	}
}

func TestWriteRejectsPartialCloudConfig(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	err = store.Write(Config{NeonURL: "postgres://url"})
	if err == nil {
		t.Fatal("expected error for partial cloud config write, got nil")
	}
}

func TestWriteFailsWhenConfigParentCannotBeCreated(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(filepath.Join(home, "personal-context"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	blockingPath := filepath.Join(home, "personal-context", ".pc")
	if err := os.WriteFile(blockingPath, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store, err := NewStore(home)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if err := store.Write(Config{}); err == nil {
		t.Fatal("expected write error when config parent path is blocked by file")
	}
}

func TestWriteFailsWhenConfigPathIsDirectory(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	configPath := store.Path()
	if err := os.MkdirAll(configPath, 0o700); err != nil {
		t.Fatalf("MkdirAll(configPath) error = %v", err)
	}
	// Place a file inside so os.Remove cannot remove the directory.
	if err := os.WriteFile(filepath.Join(configPath, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(blocker) error = %v", err)
	}

	if err := store.Write(Config{}); err == nil {
		t.Fatal("expected write failure when config path is a directory")
	}
}

func TestWritePropagatesChmodFailure(t *testing.T) {
	original := chmodFileFn
	t.Cleanup(func() { chmodFileFn = original })
	chmodFileFn = func(f *os.File, mode os.FileMode) error { return errors.New("chmod boom") }

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := store.Write(Config{}); err == nil {
		t.Fatal("expected Write() to fail when chmod fails")
	}
}

func TestWritePropagatesSyncFailure(t *testing.T) {
	original := syncFileFn
	t.Cleanup(func() { syncFileFn = original })
	syncFileFn = func(f *os.File) error { return errors.New("sync boom") }

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := store.Write(Config{}); err == nil {
		t.Fatal("expected Write() to fail when sync fails")
	}
}

func TestWritePropagatesCloseFailure(t *testing.T) {
	original := closeFileFn
	t.Cleanup(func() { closeFileFn = original })
	closeFileFn = func(f *os.File) error { return errors.New("close boom") }

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := store.Write(Config{}); err == nil {
		t.Fatal("expected Write() to fail when close fails")
	}
}

func TestWriteFailurePreservesExistingConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission test is not reliable on Windows")
	}

	home := t.TempDir()
	store, err := NewStore(home)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	if err := store.Write(Config{}); err != nil {
		t.Fatalf("initial Write() error = %v", err)
	}

	path := store.Path()
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(original) error = %v", err)
	}

	configDir := filepath.Dir(path)
	if err := os.Chmod(configDir, 0o500); err != nil {
		t.Fatalf("Chmod(configDir, 0500) error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(configDir, 0o700)
	})

	err = store.Write(Config{
		NeonURL:    "postgres://user:pass@localhost:5432/db",
		S3Bucket:   "bucket",
		S3Region:   "us-east-1",
		AWSProfile: "profile",
	})
	if err == nil {
		t.Fatal("expected write failure when config directory is not writable")
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(current) error = %v", err)
	}
	if string(current) != string(original) {
		t.Fatalf("expected config file to remain unchanged on failed write")
	}
}
