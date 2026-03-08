package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/repository"
)

// defaultSetupOpts returns setupOptions with no cloud flags for local-only tests.
func defaultSetupOpts() setupOptions {
	return setupOptions{}
}

func TestResolveUserHomeDirError(t *testing.T) {
	original := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = original })
	userHomeDirFn = func() (string, error) {
		return "", errors.New("no home")
	}

	_, err := resolveUserHomeDir()
	if err == nil {
		t.Fatal("expected error when userHomeDirFn fails")
	}
	if !strings.Contains(err.Error(), "resolve user home directory") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestDefaultResolveHomeDirUserHomeError(t *testing.T) {
	t.Setenv(pcHomeEnvVar, "")
	original := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = original })
	userHomeDirFn = func() (string, error) {
		return "", errors.New("home lookup failed")
	}

	if _, err := defaultResolveHomeDir(); err == nil {
		t.Fatal("expected defaultResolveHomeDir to fail when user home lookup fails")
	}
}

func TestOpenLocalStackConfigStoreFactoryError(t *testing.T) {
	homeDir := t.TempDir()

	original := newConfigStoreFn
	t.Cleanup(func() { newConfigStoreFn = original })
	newConfigStoreFn = func(string) (config.Store, error) {
		return config.Store{}, errors.New("store factory failed")
	}

	if _, err := openLocalStack(homeDir); err == nil {
		t.Fatal("expected openLocalStack to fail when config store creation fails")
	}
}

func TestOpenLocalStackMigrationsLoadError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)

	original := sqliteMigrationsFSFn
	t.Cleanup(func() { sqliteMigrationsFSFn = original })
	sqliteMigrationsFSFn = func() (fs.FS, error) {
		return nil, errors.New("migration fs failed")
	}

	if _, err := openLocalStack(homeDir); err == nil {
		t.Fatal("expected openLocalStack to fail when migrations load fails")
	}
}

func TestOpenLocalStackApplyMigrationsError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)

	original := sqliteMigrationsFSFn
	t.Cleanup(func() { sqliteMigrationsFSFn = original })
	sqliteMigrationsFSFn = func() (fs.FS, error) {
		return fstest.MapFS{
			"001_bad.sql": {Data: []byte("THIS IS INVALID SQL;")},
		}, nil
	}

	if _, err := openLocalStack(homeDir); err == nil {
		t.Fatal("expected openLocalStack to fail when migrations cannot be applied")
	}
}

func TestOpenLocalStackRepositoryFactoryError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)

	original := newSQLiteRepoFn
	t.Cleanup(func() { newSQLiteRepoFn = original })
	newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
		return nil, errors.New("repo factory failed")
	}

	if _, err := openLocalStack(homeDir); err == nil {
		t.Fatal("expected openLocalStack to fail when repository creation fails")
	}
}

func TestOpenLocalStackFilesystemClientFactoryError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)

	original := newFilesystemClientFn
	t.Cleanup(func() { newFilesystemClientFn = original })
	newFilesystemClientFn = func(string) (*filesystem.Client, error) {
		return nil, errors.New("fs client factory failed")
	}

	if _, err := openLocalStack(homeDir); err == nil {
		t.Fatal("expected openLocalStack to fail when filesystem client creation fails")
	}
}

func TestRunSetupApplyMigrationsError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	original := sqliteMigrationsFSFn
	t.Cleanup(func() { sqliteMigrationsFSFn = original })
	sqliteMigrationsFSFn = func() (fs.FS, error) {
		return fstest.MapFS{
			"001_bad.sql": {Data: []byte("THIS IS INVALID SQL;")},
		}, nil
	}

	if err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts()); err == nil {
		t.Fatal("expected runSetup to fail when migrations cannot be applied")
	}
}

func TestRunSetupLoadMigrationsError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	original := sqliteMigrationsFSFn
	t.Cleanup(func() { sqliteMigrationsFSFn = original })
	sqliteMigrationsFSFn = func() (fs.FS, error) {
		return nil, errors.New("migration fs failed")
	}

	if err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts()); err == nil {
		t.Fatal("expected runSetup to fail when migrations load fails")
	}
}

func TestRunSetupRepositoryFactoryError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	original := newSQLiteRepoFn
	t.Cleanup(func() { newSQLiteRepoFn = original })
	newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
		return nil, errors.New("repo factory failed")
	}

	if err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts()); err == nil {
		t.Fatal("expected runSetup to fail when repository creation fails")
	}
}

func TestRunSetupConfigStoreFactoryError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	original := newConfigStoreFn
	t.Cleanup(func() { newConfigStoreFn = original })
	newConfigStoreFn = func(string) (config.Store, error) {
		return config.Store{}, errors.New("store factory failed")
	}

	if err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts()); err == nil {
		t.Fatal("expected runSetup to fail when config store creation fails")
	}
}

func setupHomeWithConfig(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	if err := store.Write(config.Config{}); err != nil {
		t.Fatalf("Write config error = %v", err)
	}
	return homeDir
}

// --- Project command: config store factory errors ---

func TestProjectSetConfigStoreFactoryError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	original := newConfigStoreFn
	t.Cleanup(func() { newConfigStoreFn = original })
	newConfigStoreFn = func(string) (config.Store, error) {
		return config.Store{}, errors.New("store factory failed")
	}

	err := runProjectSet(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "proj")
	if err == nil {
		t.Fatal("expected error when config store creation fails")
	}
}

func TestProjectClearConfigStoreFactoryError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	original := newConfigStoreFn
	t.Cleanup(func() { newConfigStoreFn = original })
	newConfigStoreFn = func(string) (config.Store, error) {
		return config.Store{}, errors.New("store factory failed")
	}

	err := runProjectClear(context.Background(), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when config store creation fails")
	}
}

func TestProjectSetWriteError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	// Create valid config so read succeeds
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(config.Config{}); err != nil {
		t.Fatal(err)
	}

	// Make .pc dir read-only so write fails
	pcDir := filepath.Join(homeDir, "personal-context", ".pc")
	if err := os.Chmod(pcDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pcDir, 0o755) })

	err = runProjectSet(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, "proj")
	if err == nil {
		t.Fatal("expected error when config write fails")
	}
}

func TestProjectClearWriteError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	// Create valid config so read succeeds
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(config.Config{}); err != nil {
		t.Fatal(err)
	}

	// Make .pc dir read-only so write fails
	pcDir := filepath.Join(homeDir, "personal-context", ".pc")
	if err := os.Chmod(pcDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pcDir, 0o755) })

	err = runProjectClear(context.Background(), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when config write fails")
	}
}

func TestProjectListDBError(t *testing.T) {
	homeDir := setupEnv(t)

	corruptTable(t, homeDir, "slides")

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "list"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when slides table missing")
	}
}
