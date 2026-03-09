package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	pcs3 "github.com/conn-castle/personal-context/cli/internal/s3client"
)

func TestLoadAWSConfigEmptyProfile(t *testing.T) {
	_, err := loadAWSConfig(context.Background(), t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error for empty profile")
	}
	if !strings.Contains(err.Error(), "profile is required") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestLoadAWSConfigWhitespaceOnlyProfile(t *testing.T) {
	_, err := loadAWSConfig(context.Background(), t.TempDir(), "   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only profile")
	}
}

func TestOpenCloudStackEmptyHomeDir(t *testing.T) {
	_, err := openCloudStack(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty homeDir")
	}
	if !strings.Contains(err.Error(), "home directory is required") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestOpenCloudStackS3ClientFactoryError(t *testing.T) {
	homeDir := setupHomeWithCloudConfig(t)

	originalLoad := loadAWSConfigFn
	t.Cleanup(func() { loadAWSConfigFn = originalLoad })
	loadAWSConfigFn = func(context.Context, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}

	originalPool := newPGXPoolFn
	t.Cleanup(func() { newPGXPoolFn = originalPool })
	expectedPool := &pgxpool.Pool{}
	newPGXPoolFn = func(context.Context, string) (*pgxpool.Pool, error) {
		return expectedPool, nil
	}

	originalRepo := newPostgresRepoFn
	t.Cleanup(func() { newPostgresRepoFn = originalRepo })
	newPostgresRepoFn = func(*pgxpool.Pool) (repository.Repository, error) {
		return &mockRepo{}, nil
	}

	originalS3 := newCloudS3ClientFn
	t.Cleanup(func() { newCloudS3ClientFn = originalS3 })
	newCloudS3ClientFn = func(_ *awss3.Client, _ string) (*pcs3.Client, error) {
		return nil, errors.New("s3 client failed")
	}

	originalClose := closePGXPoolFn
	t.Cleanup(func() { closePGXPoolFn = originalClose })
	closeCalled := false
	closePGXPoolFn = func(pool *pgxpool.Pool) {
		closeCalled = true
		if pool != expectedPool {
			t.Fatalf("closePGXPoolFn() got unexpected pool")
		}
	}

	_, err := openCloudStack(context.Background(), homeDir)
	if err == nil {
		t.Fatal("expected s3 client factory error")
	}
	if !closeCalled {
		t.Fatal("expected S3 failure to close the opened pool")
	}
}

func TestOpenCloudStackConfigStoreError(t *testing.T) {
	homeDir := t.TempDir()

	original := newConfigStoreFn
	t.Cleanup(func() { newConfigStoreFn = original })
	newConfigStoreFn = func(string) (config.Store, error) {
		return config.Store{}, errors.New("store failed")
	}

	_, err := openCloudStack(context.Background(), homeDir)
	if err == nil {
		t.Fatal("expected error when config store creation fails")
	}
}

func TestOpenCloudStackConfigReadError(t *testing.T) {
	homeDir := t.TempDir()
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	// Write an invalid config to make Read fail
	configPath := store.Path()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err = openCloudStack(context.Background(), homeDir)
	if err == nil {
		t.Fatal("expected error when config read fails")
	}
}

func TestOpenCloudStackModeError(t *testing.T) {
	// Partial cloud config causes Mode() to return an error.
	homeDir := t.TempDir()
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	// Write a valid local config first to create the config file/dir.
	if err := store.Write(config.Config{}); err != nil {
		t.Fatalf("Write config error = %v", err)
	}
	// Overwrite with partial cloud config directly to bypass Write validation.
	partialConfig := []byte(`{"neon_url":"postgres://x"}`)
	if err := os.WriteFile(store.Path(), partialConfig, 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err = openCloudStack(context.Background(), homeDir)
	if err == nil {
		t.Fatal("expected error for partial cloud config (Mode() error)")
	}
}

func TestOpenCloudStackRejectsLocalOnlyConfig(t *testing.T) {
	homeDir := setupHomeWithConfig(t)

	_, err := openCloudStack(context.Background(), homeDir)
	if !errors.Is(err, errCloudNotConfigured) {
		t.Fatalf("expected errCloudNotConfigured, got %v", err)
	}
}

func TestOpenCloudStackValidateCloudConfigError(t *testing.T) {
	homeDir := setupHomeWithCloudConfig(t)

	original := validateCloudConfigFn
	t.Cleanup(func() { validateCloudConfigFn = original })
	validateCloudConfigFn = func(config.Config) error {
		return errors.New("cloud config invalid")
	}

	_, err := openCloudStack(context.Background(), homeDir)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestOpenCloudStackAWSConfigLoadError(t *testing.T) {
	homeDir := setupHomeWithCloudConfig(t)

	original := loadAWSConfigFn
	t.Cleanup(func() { loadAWSConfigFn = original })
	loadAWSConfigFn = func(context.Context, string, string) (aws.Config, error) {
		return aws.Config{}, errors.New("aws config failed")
	}

	_, err := openCloudStack(context.Background(), homeDir)
	if err == nil {
		t.Fatal("expected aws config load error")
	}
}

func TestOpenCloudStackPGPoolFactoryError(t *testing.T) {
	homeDir := setupHomeWithCloudConfig(t)

	originalLoad := loadAWSConfigFn
	t.Cleanup(func() { loadAWSConfigFn = originalLoad })
	loadAWSConfigFn = func(context.Context, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}

	originalPool := newPGXPoolFn
	t.Cleanup(func() { newPGXPoolFn = originalPool })
	newPGXPoolFn = func(context.Context, string) (*pgxpool.Pool, error) {
		return nil, errors.New("pg pool failed")
	}

	_, err := openCloudStack(context.Background(), homeDir)
	if err == nil {
		t.Fatal("expected pg pool factory error")
	}
}

func TestOpenCloudStackPostgresRepoFactoryError(t *testing.T) {
	homeDir := setupHomeWithCloudConfig(t)

	originalLoad := loadAWSConfigFn
	t.Cleanup(func() { loadAWSConfigFn = originalLoad })
	loadAWSConfigFn = func(context.Context, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	}

	originalPool := newPGXPoolFn
	t.Cleanup(func() { newPGXPoolFn = originalPool })
	expectedPool := &pgxpool.Pool{}
	newPGXPoolFn = func(context.Context, string) (*pgxpool.Pool, error) {
		return expectedPool, nil
	}

	originalClose := closePGXPoolFn
	t.Cleanup(func() { closePGXPoolFn = originalClose })
	closeCalled := false
	closePGXPoolFn = func(pool *pgxpool.Pool) {
		closeCalled = true
		if pool != expectedPool {
			t.Fatalf("closePGXPoolFn() got unexpected pool %#v", pool)
		}
	}

	originalRepo := newPostgresRepoFn
	t.Cleanup(func() { newPostgresRepoFn = originalRepo })
	newPostgresRepoFn = func(*pgxpool.Pool) (repository.Repository, error) {
		return nil, errors.New("postgres repo failed")
	}

	_, err := openCloudStack(context.Background(), homeDir)
	if err == nil {
		t.Fatal("expected postgres repo factory error")
	}
	if !closeCalled {
		t.Fatal("expected postgres repo failure to close the opened pool")
	}
}

func TestOpenCloudStackSuccess(t *testing.T) {
	homeDir := setupHomeWithCloudConfig(t)

	originalLoad := loadAWSConfigFn
	t.Cleanup(func() { loadAWSConfigFn = originalLoad })
	loadAWSConfigFn = func(_ context.Context, _ string, profile string) (aws.Config, error) {
		if profile != "personal-context" {
			t.Fatalf("expected profile personal-context, got %q", profile)
		}
		return aws.Config{}, nil
	}

	originalPool := newPGXPoolFn
	t.Cleanup(func() { newPGXPoolFn = originalPool })
	expectedPool := &pgxpool.Pool{}
	newPGXPoolFn = func(_ context.Context, url string) (*pgxpool.Pool, error) {
		if url != "postgres://user:pass@localhost:5432/personal_context" {
			t.Fatalf("unexpected neon url %q", url)
		}
		return expectedPool, nil
	}

	originalRepo := newPostgresRepoFn
	t.Cleanup(func() { newPostgresRepoFn = originalRepo })
	newPostgresRepoFn = func(pool *pgxpool.Pool) (repository.Repository, error) {
		if pool != expectedPool {
			t.Fatal("openCloudStack passed unexpected pool to repository factory")
		}
		return &mockRepo{}, nil
	}

	stack, err := openCloudStack(context.Background(), homeDir)
	if err != nil {
		t.Fatalf("openCloudStack() error = %v", err)
	}

	if stack.Config.AWSProfile != "personal-context" {
		t.Fatalf("expected AWSProfile personal-context, got %q", stack.Config.AWSProfile)
	}
	if stack.Repo == nil {
		t.Fatal("expected non-nil Repo")
	}
	if stack.S3 == nil {
		t.Fatal("expected non-nil S3 client")
	}
	if stack.Store.Path() == "" {
		t.Fatal("expected non-empty Store path")
	}
}

func TestCloudStackCloseNilPool(t *testing.T) {
	stack := &cloudStack{}
	_ = stack.Close()
}

func TestCloudStackCloseUsesClosePGXPoolFn(t *testing.T) {
	expectedPool := &pgxpool.Pool{}

	originalClose := closePGXPoolFn
	t.Cleanup(func() { closePGXPoolFn = originalClose })

	closed := false
	closePGXPoolFn = func(pool *pgxpool.Pool) {
		closed = true
		if pool != expectedPool {
			t.Fatalf("closePGXPoolFn() got unexpected pool %#v", pool)
		}
	}

	stack := &cloudStack{pool: expectedPool}
	if err := stack.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !closed {
		t.Fatal("expected Close() to delegate to closePGXPoolFn")
	}
}

func TestLoadAWSConfigCredentialsPathError(t *testing.T) {
	original := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = original })
	userHomeDirFn = func() (string, error) {
		return "", fmt.Errorf("user home unavailable")
	}

	_, err := loadAWSConfig(context.Background(), t.TempDir(), "my-profile")
	if err == nil {
		t.Fatal("expected error when credentials path resolution fails")
	}
	if !strings.Contains(err.Error(), "user home unavailable") {
		t.Fatalf("expected user home error to propagate, got %v", err)
	}
}

func TestLoadAWSConfigSuccessfulLoad(t *testing.T) {
	homeDir := t.TempDir()
	credentialsPath := filepath.Join(homeDir, ".aws", "credentials")
	if err := os.MkdirAll(filepath.Dir(credentialsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "[test-profile]\naws_access_key_id = AKID\naws_secret_access_key = SECRET\n"
	if err := os.WriteFile(credentialsPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	original := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = original })
	userHomeDirFn = func() (string, error) { return homeDir, nil }

	cfg, err := loadAWSConfig(context.Background(), homeDir, "test-profile")
	if err != nil {
		t.Fatalf("loadAWSConfig() error = %v", err)
	}
	// Verify the config was loaded (region defaults to empty since we didn't set it)
	if cfg.Region != "" {
		t.Fatalf("expected empty default region, got %q", cfg.Region)
	}
}

func setupHomeWithCloudConfig(t *testing.T) string {
	t.Helper()

	homeDir := t.TempDir()
	store, err := config.NewStore(homeDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	cfg := config.Config{
		NeonURL:    "postgres://user:pass@localhost:5432/personal_context",
		S3Bucket:   "personal-context-test",
		S3Region:   "us-east-1",
		AWSProfile: "personal-context",
	}
	if err := store.Write(cfg); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	return homeDir
}
