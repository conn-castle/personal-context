package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	pcs3 "github.com/conn-castle/personal-context/cli/internal/s3client"
	pcsync "github.com/conn-castle/personal-context/cli/internal/sync"
	"github.com/conn-castle/personal-context/cli/internal/syncengine"
)

func TestRunSyncNonCloudError(t *testing.T) {
	// Override resolveHomeDirFn to return an error that is NOT errCloudNotConfigured.
	original := resolveHomeDirFn
	t.Cleanup(func() { resolveHomeDirFn = original })
	resolveHomeDirFn = func() (string, error) {
		return "", errors.New("resolve home failed")
	}

	err := runSync(context.Background(), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error from resolveHomeDirFn")
	}
	if err.Error() != "resolve home failed" {
		t.Fatalf("runSync() error = %v, want resolve home failed", err)
	}
}

func TestRunSyncRunnerSyncError(t *testing.T) {
	homeDir := setupHomeWithCloudConfig(t)
	t.Setenv(pcHomeEnvVar, homeDir)

	originalLoad := loadAWSConfigFn
	t.Cleanup(func() { loadAWSConfigFn = originalLoad })
	loadAWSConfigFn = func(context.Context, string) (aws.Config, error) {
		return aws.Config{}, nil
	}

	originalPool := newPGXPoolFn
	t.Cleanup(func() { newPGXPoolFn = originalPool })
	newPGXPoolFn = func(context.Context, string) (*pgxpool.Pool, error) { return nil, nil }

	originalResolve := resolveUserIDFn
	t.Cleanup(func() { resolveUserIDFn = originalResolve })
	resolveUserIDFn = func(context.Context, *pgxpool.Pool, string) (string, error) {
		return "test-user-id", nil
	}

	originalRepo := newPostgresRepoFn
	t.Cleanup(func() { newPostgresRepoFn = originalRepo })
	newPostgresRepoFn = func(*pgxpool.Pool, string) (repository.Repository, error) { return &mockRepo{}, nil }

	originalS3 := newCloudS3ClientFn
	t.Cleanup(func() { newCloudS3ClientFn = originalS3 })
	newCloudS3ClientFn = func(*awss3.Client, string, string) (*pcs3.Client, error) {
		return &pcs3.Client{}, nil
	}

	originalSession := newSyncSessionManagerFn
	t.Cleanup(func() { newSyncSessionManagerFn = originalSession })
	newSyncSessionManagerFn = func(string) (pcsync.SessionManager, error) {
		return &fakeSyncSessionManager{}, nil
	}

	originalService := newSyncServiceFn
	t.Cleanup(func() { newSyncServiceFn = originalService })
	newSyncServiceFn = func(*localStack, *cloudStack, pcsync.SessionManager) (syncRunner, error) {
		return &failingSyncRunner{err: errors.New("sync engine failed")}, nil
	}

	err := runSync(context.Background(), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error from runner.Sync()")
	}
	if !strings.Contains(err.Error(), "run sync") {
		t.Fatalf("runSync() error = %v, want wrapped run sync error", err)
	}
}

func TestRunSyncErrorsWhenCloudNotConfigured(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	t.Setenv(pcHomeEnvVar, homeDir)

	err := runSync(context.Background(), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected runSync to fail without cloud configuration")
	}
	if err.Error() != "cloud is not configured" {
		t.Fatalf("runSync() error = %v", err)
	}
}

func TestRunSyncInvokesServiceAndPrintsSuccess(t *testing.T) {
	homeDir := setupHomeWithCloudConfig(t)
	t.Setenv(pcHomeEnvVar, homeDir)

	originalLoad := loadAWSConfigFn
	t.Cleanup(func() { loadAWSConfigFn = originalLoad })
	loadAWSConfigFn = func(context.Context, string) (aws.Config, error) {
		return aws.Config{}, nil
	}

	originalPool := newPGXPoolFn
	t.Cleanup(func() { newPGXPoolFn = originalPool })
	newPGXPoolFn = func(context.Context, string) (*pgxpool.Pool, error) { return nil, nil }

	originalResolve := resolveUserIDFn
	t.Cleanup(func() { resolveUserIDFn = originalResolve })
	resolveUserIDFn = func(context.Context, *pgxpool.Pool, string) (string, error) {
		return "test-user-id", nil
	}

	originalRepo := newPostgresRepoFn
	t.Cleanup(func() { newPostgresRepoFn = originalRepo })
	newPostgresRepoFn = func(*pgxpool.Pool, string) (repository.Repository, error) { return &mockRepo{}, nil }

	originalS3 := newCloudS3ClientFn
	t.Cleanup(func() { newCloudS3ClientFn = originalS3 })
	newCloudS3ClientFn = func(*awss3.Client, string, string) (*pcs3.Client, error) {
		return &pcs3.Client{}, nil
	}

	originalSession := newSyncSessionManagerFn
	t.Cleanup(func() { newSyncSessionManagerFn = originalSession })
	newSyncSessionManagerFn = func(string) (pcsync.SessionManager, error) {
		return &fakeSyncSessionManager{}, nil
	}

	originalService := newSyncServiceFn
	t.Cleanup(func() { newSyncServiceFn = originalService })
	runner := &fakeSyncRunner{}
	newSyncServiceFn = func(local *localStack, cloud *cloudStack, session pcsync.SessionManager) (syncRunner, error) {
		if local == nil || cloud == nil || session == nil {
			t.Fatal("expected non-nil sync dependencies")
		}
		return runner, nil
	}

	stdout := &bytes.Buffer{}
	if err := runSync(context.Background(), stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runSync() error = %v", err)
	}

	if runner.calls != 1 {
		t.Fatalf("expected Sync() to be called once, got %d", runner.calls)
	}
	if stdout.String() != "Sync complete\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunSyncReturnsCleanupErrorAfterSuccessfulSync(t *testing.T) {
	originalOpen := openSyncRunnerFn
	t.Cleanup(func() { openSyncRunnerFn = originalOpen })

	openSyncRunnerFn = func(context.Context) (syncRunner, func() error, error) {
		return &fakeSyncRunner{}, func() error {
			return errors.New("cleanup failed")
		}, nil
	}

	stdout := &bytes.Buffer{}
	err := runSync(context.Background(), stdout, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("runSync() error = %v, want cleanup failure", err)
	}
	if stdout.String() != "Sync complete\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunSyncJoinsSyncAndCleanupErrors(t *testing.T) {
	originalOpen := openSyncRunnerFn
	t.Cleanup(func() { openSyncRunnerFn = originalOpen })

	openSyncRunnerFn = func(context.Context) (syncRunner, func() error, error) {
		return &failingSyncRunner{err: errors.New("sync failed")}, func() error {
			return errors.New("cleanup failed")
		}, nil
	}

	err := runSync(context.Background(), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("runSync() error = nil, want joined failure")
	}
	if !strings.Contains(err.Error(), "run sync") || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("runSync() error = %v, want sync and cleanup failures", err)
	}
}

func TestNewSyncCommandExecutesRunE(t *testing.T) {
	// Exercise the sync subcommand through the root command, covering the RunE closure.
	homeDir := setupHomeWithConfig(t)
	t.Setenv(pcHomeEnvVar, homeDir)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"sync"})

	// This will fail because cloud is not configured, but it covers the RunE path.
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error because cloud is not configured")
	}
}

func TestDefaultSyncFactoriesCreateConcreteDependencies(t *testing.T) {
	session, err := newSyncSessionManagerFn(t.TempDir())
	if err != nil {
		t.Fatalf("newSyncSessionManagerFn() error = %v", err)
	}
	if session == nil {
		t.Fatal("expected session manager")
	}

	fsClient, err := filesystem.NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("filesystem.NewClient() error = %v", err)
	}
	runner, err := newSyncServiceFn(
		&localStack{Repo: &mockRepo{}, FS: fsClient},
		&cloudStack{Repo: &mockRepo{}, S3: &pcs3.Client{}},
		&fakeSyncSessionManager{},
	)
	if err != nil {
		t.Fatalf("newSyncServiceFn() error = %v", err)
	}
	if runner == nil {
		t.Fatal("expected sync runner")
	}
}

type fakeSyncRunner struct {
	calls int
}

func (f *fakeSyncRunner) Sync(context.Context) error {
	f.calls++
	return nil
}

type fakeSyncSessionManager struct{}

func (f *fakeSyncSessionManager) Begin() (syncengine.SyncWindow, *syncengine.FileLock, error) {
	return syncengine.SyncWindow{}, nil, nil
}

func (f *fakeSyncSessionManager) Complete(syncengine.SyncWindow) error {
	return nil
}
