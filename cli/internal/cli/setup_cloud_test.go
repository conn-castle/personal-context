package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/sqlite"
)

// --- setupOptions tests ---

func TestSetupOptionsHasCloudFlags(t *testing.T) {
	tests := []struct {
		name string
		opts setupOptions
		want bool
	}{
		{"empty", setupOptions{}, false},
		{"neon-url only", setupOptions{NeonURL: "postgres://x"}, true},
		{"s3-bucket only", setupOptions{S3Bucket: "b"}, true},
		{"s3-region only", setupOptions{S3Region: "us-east-1"}, true},
		{"aws-key only", setupOptions{AWSKey: "k"}, true},
		{"aws-secret only", setupOptions{AWSSecret: "s"}, true},
		{"api-key only", setupOptions{APIKey: "pc_key_test"}, true},
		{"all flags", setupOptions{NeonURL: "postgres://x", S3Bucket: "b", S3Region: "us-east-1", AWSKey: "k", AWSSecret: "s", APIKey: "pc_key_test"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.hasCloudFlags(); got != tt.want {
				t.Fatalf("hasCloudFlags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateNeonConnectivityConnectError(t *testing.T) {
	origNewPool := newPGXPoolFn
	newPGXPoolFn = func(context.Context, string) (*pgxpool.Pool, error) {
		return nil, errors.New("connect failed")
	}
	t.Cleanup(func() { newPGXPoolFn = origNewPool })

	err := validateNeonConnectivityFn(context.Background(), "postgres://example")
	if err == nil || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("expected connect error, got %v", err)
	}
}

func TestRunSetupInitCloudSchemaErrorBranches(t *testing.T) {
	if err := runSetupInitCloudSchema(context.Background(), &bytes.Buffer{}, "not-a-url"); err == nil || !strings.Contains(err.Error(), "invalid neon URL") {
		t.Fatalf("expected invalid neon URL error, got %v", err)
	}

	origValidate := validateNeonConnectivityFn
	origNewPool := newPGXPoolFn
	origApply := applyPostgresSchemaFn
	t.Cleanup(func() {
		validateNeonConnectivityFn = origValidate
		newPGXPoolFn = origNewPool
		applyPostgresSchemaFn = origApply
	})

	validateNeonConnectivityFn = func(context.Context, string) error { return nil }
	newPGXPoolFn = func(context.Context, string) (*pgxpool.Pool, error) {
		return nil, errors.New("schema connect failed")
	}
	if err := runSetupInitCloudSchema(context.Background(), &bytes.Buffer{}, "postgres://user:pass@example.com/db"); err == nil || !strings.Contains(err.Error(), "connect to postgres") {
		t.Fatalf("expected schema connect error, got %v", err)
	}

	newPGXPoolFn = func(context.Context, string) (*pgxpool.Pool, error) { return nil, nil }
	applyPostgresSchemaFn = func(context.Context, *pgxpool.Pool) error {
		return errors.New("schema apply failed")
	}
	if err := runSetupInitCloudSchema(context.Background(), &bytes.Buffer{}, "postgres://user:pass@example.com/db"); err == nil || !strings.Contains(err.Error(), "apply postgres schema") {
		t.Fatalf("expected schema apply error, got %v", err)
	}
}

func TestRunSetupCloudInteractiveRequiresLocalRepo(t *testing.T) {
	store, err := config.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	err = runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("y\n"),
		t.TempDir(), store,
		"postgres://user:pass@example.com/db", "bucket-name", "us-east-1", "aws-key", "aws-secret", "", false, "pc_key_test",
		nil, true)
	if err == nil || !strings.Contains(err.Error(), "localRepo is required") {
		t.Fatalf("expected localRepo required error, got %v", err)
	}
}

func TestSetupOptionsValidateCloudFlagsComplete(t *testing.T) {
	tests := []struct {
		name    string
		opts    setupOptions
		wantErr bool
		missing []string
	}{
		{
			"all present",
			setupOptions{NeonURL: "postgres://x", S3Bucket: "b", S3Region: "us-east-1", AWSKey: "k", AWSSecret: "s", APIKey: "pc_key_test"},
			false,
			nil,
		},
		{
			"missing neon-url",
			setupOptions{S3Bucket: "b", S3Region: "us-east-1", AWSKey: "k", AWSSecret: "s", APIKey: "pc_key_test"},
			true,
			[]string{"--neon-url"},
		},
		{
			"missing multiple",
			setupOptions{NeonURL: "postgres://x"},
			true,
			[]string{"--s3-bucket", "--s3-region", "--aws-key", "--aws-secret", "--api-key"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validateCloudFlagsComplete()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateCloudFlagsComplete() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				for _, m := range tt.missing {
					if !strings.Contains(err.Error(), m) {
						t.Fatalf("expected error to mention %q, got %q", m, err.Error())
					}
				}
			}
		})
	}
}

func TestDefaultValidateS3AccessUsesEndpointAndPathStyle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("method = %s, want HEAD", r.Method)
		}
		if r.URL.Path != "/test-bucket" {
			t.Fatalf("path = %s, want /test-bucket", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	if err := validateS3AccessFn(context.Background(), "test-bucket", "us-east-1", "access", "secret", server.URL, true); err != nil {
		t.Fatalf("validateS3AccessFn() error = %v", err)
	}
}

func TestDefaultValidateS3AccessReportsHeadBucketError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	err := validateS3AccessFn(context.Background(), "test-bucket", "us-east-1", "access", "secret", server.URL, true)
	if err == nil {
		t.Fatal("expected HeadBucket error")
	}
	if !strings.Contains(err.Error(), `check bucket "test-bucket"`) {
		t.Fatalf("error = %v, want bucket context", err)
	}
}

// --- runSetup cloud flag conflicts ---

func TestRunSetupRemoveCloudWithCloudFlagsError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	opts := setupOptions{RemoveCloud: true, NeonURL: "postgres://x"}
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err == nil {
		t.Fatal("expected error for --remove-cloud with cloud flags")
	}
	if !strings.Contains(err.Error(), "--remove-cloud cannot be used with cloud configuration flags") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupInitCloudSchemaSuccess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	stdout := &bytes.Buffer{}
	err := runSetup(
		context.Background(),
		stdout,
		&bytes.Buffer{},
		strings.NewReader(""),
		setupOptions{
			NeonURL:         "postgres://user:pass@host/db",
			InitCloudSchema: true,
		},
	)
	if err != nil {
		t.Fatalf("runSetup() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Cloud Postgres schema initialized successfully") {
		t.Fatalf("unexpected stdout = %q", stdout.String())
	}

	store, _ := config.NewStore(homeDir)
	if _, readErr := store.Read(); !errors.Is(readErr, os.ErrNotExist) {
		t.Fatalf("schema bootstrap should not write local config, got error %v", readErr)
	}
}

func TestRunSetupInitCloudSchemaRequiresNeonURLOnly(t *testing.T) {
	err := runSetup(
		context.Background(),
		&bytes.Buffer{},
		&bytes.Buffer{},
		strings.NewReader(""),
		setupOptions{InitCloudSchema: true},
	)
	if err == nil || !strings.Contains(err.Error(), "--init-cloud-schema requires --neon-url") {
		t.Fatalf("expected missing neon-url error, got %v", err)
	}

	err = runSetup(
		context.Background(),
		&bytes.Buffer{},
		&bytes.Buffer{},
		strings.NewReader(""),
		setupOptions{
			NeonURL:         "postgres://user:pass@host/db",
			S3Bucket:        "bucket",
			InitCloudSchema: true,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "can only be used with --neon-url") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestRunSetupInitCloudSchemaErrors(t *testing.T) {
	if err := runSetupInitCloudSchema(context.Background(), &bytes.Buffer{}, "http://not-postgres"); err == nil {
		t.Fatal("expected invalid URL error")
	}

	t.Run("connectivity", func(t *testing.T) {
		mockAllCloudDeps(t)
		origValidate := validateNeonConnectivityFn
		t.Cleanup(func() { validateNeonConnectivityFn = origValidate })
		validateNeonConnectivityFn = func(context.Context, string) error {
			return errors.New("connectivity failed")
		}

		err := runSetupInitCloudSchema(context.Background(), &bytes.Buffer{}, "postgres://user:pass@host/db")
		if err == nil || !strings.Contains(err.Error(), "neon connectivity check failed") {
			t.Fatalf("expected connectivity error, got %v", err)
		}
	})

	t.Run("pool", func(t *testing.T) {
		mockAllCloudDeps(t)
		origPool := newPGXPoolFn
		t.Cleanup(func() { newPGXPoolFn = origPool })
		newPGXPoolFn = func(context.Context, string) (*pgxpool.Pool, error) {
			return nil, errors.New("pool failed")
		}

		err := runSetupInitCloudSchema(context.Background(), &bytes.Buffer{}, "postgres://user:pass@host/db")
		if err == nil || !strings.Contains(err.Error(), "connect to postgres for schema") {
			t.Fatalf("expected pool error, got %v", err)
		}
	})

	t.Run("schema", func(t *testing.T) {
		mockAllCloudDeps(t)
		origSchema := applyPostgresSchemaFn
		t.Cleanup(func() { applyPostgresSchemaFn = origSchema })
		applyPostgresSchemaFn = func(context.Context, *pgxpool.Pool) error {
			return errors.New("schema failed")
		}

		err := runSetupInitCloudSchema(context.Background(), &bytes.Buffer{}, "postgres://user:pass@host/db")
		if err == nil || !strings.Contains(err.Error(), "apply postgres schema") {
			t.Fatalf("expected schema error, got %v", err)
		}
	})
}

// --- runSetup non-interactive cloud path ---

func TestRunSetupNonInteractiveCloudSuccess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	stdout := &bytes.Buffer{}
	opts := setupOptions{
		NeonURL:   "postgres://user:pass@host/db",
		S3Bucket:  "my-bucket",
		S3Region:  "us-east-1",
		AWSKey:    "AKIAIOSFODNN7EXAMPLE",
		AWSSecret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		APIKey:    "pc_key_valid",
	}
	err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err != nil {
		t.Fatalf("runSetup() error = %v", err)
	}

	// Verify config was written with cloud fields.
	store, _ := config.NewStore(homeDir)
	cfg, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	mode, err := cfg.Mode()
	if err != nil {
		t.Fatalf("Mode() error = %v", err)
	}
	if mode != config.ModeCloud {
		t.Fatalf("expected cloud mode, got %q", mode)
	}
	if cfg.NeonURL != "postgres://user:pass@host/db" {
		t.Fatalf("unexpected NeonURL = %q", cfg.NeonURL)
	}
	if cfg.S3Bucket != "my-bucket" {
		t.Fatalf("unexpected S3Bucket = %q", cfg.S3Bucket)
	}
	if cfg.AWSProfile != awsProfileName {
		t.Fatalf("unexpected AWSProfile = %q", cfg.AWSProfile)
	}
	if cfg.APIKey != "pc_key_valid" {
		t.Fatalf("unexpected APIKey = %q", cfg.APIKey)
	}

	// Verify output.
	out := stdout.String()
	if !strings.Contains(out, "Cloud sync configured successfully") {
		t.Fatalf("expected success message, got %q", out)
	}
	if !strings.Contains(out, "Personal Context initialized") {
		t.Fatalf("expected initialization message, got %q", out)
	}
}

func TestRunSetupNonInteractiveMissingFlags(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	opts := setupOptions{NeonURL: "postgres://x"}
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err == nil {
		t.Fatal("expected error for missing cloud flags")
	}
	if !strings.Contains(err.Error(), "--s3-bucket") {
		t.Fatalf("expected missing flags listed, got %q", err.Error())
	}
}

func TestRunSetupNonInteractiveNeonConnectivityFails(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	origValidate := validateNeonConnectivityFn
	t.Cleanup(func() { validateNeonConnectivityFn = origValidate })
	validateNeonConnectivityFn = func(context.Context, string) error {
		return errors.New("connection refused")
	}

	opts := fullCloudOpts()
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err == nil {
		t.Fatal("expected error when Neon connectivity fails")
	}
	if !strings.Contains(err.Error(), "neon connectivity check failed") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupNonInteractiveS3AccessFails(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	origValidate := validateS3AccessFn
	t.Cleanup(func() { validateS3AccessFn = origValidate })
	validateS3AccessFn = func(context.Context, string, string, string, string, string, bool) error {
		return errors.New("access denied")
	}

	opts := fullCloudOpts()
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err == nil {
		t.Fatal("expected error when S3 access fails")
	}
	if !strings.Contains(err.Error(), "S3 access check failed") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupNonInteractiveApplySchemaFails(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	origSchema := applyPostgresSchemaFn
	t.Cleanup(func() { applyPostgresSchemaFn = origSchema })
	applyPostgresSchemaFn = func(context.Context, *pgxpool.Pool) error {
		return errors.New("schema failed")
	}

	opts := fullCloudOpts()
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err == nil {
		t.Fatal("expected error when apply schema fails")
	}
	if !strings.Contains(err.Error(), "apply postgres schema") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupNonInteractivePoolConnectFails(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	// Override the pool factory to always fail. Since validateNeonConnectivityFn
	// is mocked (bypasses newPGXPoolFn), the first real call is from
	// runSetupCloud for schema application.
	origPool := newPGXPoolFn
	t.Cleanup(func() { newPGXPoolFn = origPool })
	newPGXPoolFn = func(context.Context, string) (*pgxpool.Pool, error) {
		return nil, errors.New("pool connect failed")
	}

	opts := fullCloudOpts()
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err == nil {
		t.Fatal("expected error when pool connect fails for schema")
	}
	if !strings.Contains(err.Error(), "connect to postgres for schema") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupNonInteractivePreservesActiveProject(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	// Do a local setup first with an active project.
	stdout := &bytes.Buffer{}
	err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts())
	if err != nil {
		t.Fatalf("local setup failed: %v", err)
	}

	// Set an active project.
	store, _ := config.NewStore(homeDir)
	cfg, _ := store.Read()
	cfg.ActiveProject = "org/my-project"
	_ = store.Write(cfg)

	// Now run cloud setup.
	stdout.Reset()
	opts := fullCloudOpts()
	err = runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err != nil {
		t.Fatalf("cloud setup failed: %v", err)
	}

	// Verify ActiveProject was preserved.
	cfg, err = store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if cfg.ActiveProject != "org/my-project" {
		t.Fatalf("expected ActiveProject preserved, got %q", cfg.ActiveProject)
	}
}

func TestRunSetupNonInteractiveInvalidNeonURL(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	opts := fullCloudOpts()
	opts.NeonURL = "http://not-postgres"
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err == nil {
		t.Fatal("expected error for invalid Neon URL")
	}
	if !strings.Contains(err.Error(), "invalid neon URL") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupNonInteractiveEmptyAWSKey(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	opts := fullCloudOpts()
	opts.AWSKey = ""
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err == nil {
		t.Fatal("expected error for missing partial flags")
	}
}

// --- runSetup --remove-cloud ---

func TestRunSetupRemoveCloudSuccess(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	// First do a local setup.
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts())
	if err != nil {
		t.Fatalf("local setup failed: %v", err)
	}

	// Write cloud config manually.
	store, _ := config.NewStore(homeDir)
	cloudCfg := config.Config{
		NeonURL:       "postgres://user:pass@host/db",
		S3Bucket:      "my-bucket",
		S3Region:      "us-east-1",
		AWSProfile:    awsProfileName,
		APIKey:        "pc_key_valid",
		ActiveProject: "org/proj",
	}
	if err := store.Write(cloudCfg); err != nil {
		t.Fatalf("Write cloud config: %v", err)
	}

	// Override userHomeDirFn so we never touch the real ~/.aws/credentials.
	fakeUserHome := t.TempDir()
	origUserHome := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = origUserHome })
	userHomeDirFn = func() (string, error) { return fakeUserHome, nil }

	// Write a fake AWS credentials file under the temp home.
	awsDir := filepath.Join(fakeUserHome, ".aws")
	if err := os.MkdirAll(awsDir, 0o700); err != nil {
		t.Fatalf("mkdir .aws: %v", err)
	}
	awsCredsPath := filepath.Join(awsDir, "credentials")
	existingCreds := "[personal-context]\naws_access_key_id = AKIA\naws_secret_access_key = SECRET\n"
	if err := os.WriteFile(awsCredsPath, []byte(existingCreds), 0o600); err != nil {
		t.Fatalf("write aws creds: %v", err)
	}

	// Mock removeAWSProfileFn to avoid touching real ~/.aws/credentials.
	origRemove := removeAWSProfileFn
	t.Cleanup(func() { removeAWSProfileFn = origRemove })
	removeCalled := false
	removeAWSProfileFn = func(home string, profile string) error {
		removeCalled = true
		if profile != awsProfileName {
			t.Fatalf("expected profile %q, got %q", awsProfileName, profile)
		}
		return nil
	}

	stdout := &bytes.Buffer{}
	opts := setupOptions{RemoveCloud: true}
	err = runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err != nil {
		t.Fatalf("runSetup --remove-cloud error = %v", err)
	}

	if !removeCalled {
		t.Fatal("expected removeAWSProfileFn to be called")
	}

	// Verify config is now local-only.
	cfg, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	mode, err := cfg.Mode()
	if err != nil {
		t.Fatalf("Mode() error = %v", err)
	}
	if mode != config.ModeLocalOnly {
		t.Fatalf("expected local-only mode, got %q", mode)
	}
	if cfg.ActiveProject != "org/proj" {
		t.Fatalf("expected ActiveProject preserved, got %q", cfg.ActiveProject)
	}

	if !strings.Contains(stdout.String(), "Cloud configuration removed") {
		t.Fatalf("expected removal message, got %q", stdout.String())
	}
}

func TestRunSetupRemoveCloudAlreadyLocalOnly(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	// Do local setup only.
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts())
	if err != nil {
		t.Fatalf("local setup failed: %v", err)
	}

	stdout := &bytes.Buffer{}
	opts := setupOptions{RemoveCloud: true}
	err = runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err != nil {
		t.Fatalf("runSetup --remove-cloud error = %v", err)
	}

	if !strings.Contains(stdout.String(), "not configured") {
		t.Fatalf("expected 'not configured' message, got %q", stdout.String())
	}
}

func TestRunSetupRemoveCloudWithoutConfigNoOp(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	origOpenSQLite := openSQLiteFn
	t.Cleanup(func() { openSQLiteFn = origOpenSQLite })
	openSQLiteFn = func(string) (*sqlite.Connection, error) {
		t.Fatal("openSQLiteFn should not be called for --remove-cloud without config")
		return nil, nil
	}

	stdout := &bytes.Buffer{}
	err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader(""), setupOptions{RemoveCloud: true})
	if err != nil {
		t.Fatalf("runSetup --remove-cloud without config error = %v", err)
	}
	if !strings.Contains(stdout.String(), "not configured") {
		t.Fatalf("expected not-configured message, got %q", stdout.String())
	}
}

func TestRunSetupRemoveCloudSkipsLocalSetupDependencies(t *testing.T) {
	homeDir := setupHomeWithCloudConfig(t)
	t.Setenv(pcHomeEnvVar, homeDir)

	origOpenSQLite := openSQLiteFn
	t.Cleanup(func() { openSQLiteFn = origOpenSQLite })
	openSQLiteFn = func(string) (*sqlite.Connection, error) {
		t.Fatal("openSQLiteFn should not be called for --remove-cloud")
		return nil, nil
	}

	origRemove := removeAWSProfileFn
	t.Cleanup(func() { removeAWSProfileFn = origRemove })
	removeAWSProfileFn = func(string, string) error { return nil }

	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""), setupOptions{RemoveCloud: true})
	if err != nil {
		t.Fatalf("runSetup --remove-cloud error = %v", err)
	}
}

func TestRunSetupRemoveCloudAWSProfileRemoveError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	// Do local setup and write cloud config.
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts())
	if err != nil {
		t.Fatalf("local setup failed: %v", err)
	}
	store, _ := config.NewStore(homeDir)
	_ = store.Write(config.Config{
		NeonURL:    "postgres://x",
		S3Bucket:   "b",
		S3Region:   "us-east-1",
		AWSProfile: awsProfileName,
		APIKey:     "pc_key_valid",
	})

	origRemove := removeAWSProfileFn
	t.Cleanup(func() { removeAWSProfileFn = origRemove })
	removeAWSProfileFn = func(string, string) error {
		return errors.New("permission denied")
	}

	opts := setupOptions{RemoveCloud: true}
	err = runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""), opts)
	if err == nil {
		t.Fatal("expected error when AWS profile removal fails")
	}
	if !strings.Contains(err.Error(), "remove AWS credentials profile") {
		t.Fatalf("unexpected error = %v", err)
	}

	cfg, readErr := store.Read()
	if readErr != nil {
		t.Fatalf("Read() error = %v", readErr)
	}
	mode, modeErr := cfg.Mode()
	if modeErr != nil {
		t.Fatalf("Mode() error = %v", modeErr)
	}
	if mode != config.ModeLocalOnly {
		t.Fatalf("expected local-only mode after profile removal error, got %q", mode)
	}
}

// --- runSetup interactive cloud path ---

func TestRunSetupInteractiveCloudAccepted(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	origResolve := resolveUserIDFn
	t.Cleanup(func() { resolveUserIDFn = origResolve })
	resolveUserIDFn = func(_ context.Context, _ *pgxpool.Pool, rawKey string) (string, error) {
		if rawKey != "pc_key_valid" {
			t.Fatalf("expected API key pc_key_valid, got %q", rawKey)
		}
		return "validated-user-id", nil
	}

	// Mock the merge preview: localRepo returns 3 records, cloudRepo returns 5.
	origPostgresRepo := newPostgresRepoFn
	t.Cleanup(func() { newPostgresRepoFn = origPostgresRepo })
	newPostgresRepoFn = func(_ *pgxpool.Pool, userID string) (repository.Repository, error) {
		if userID != "validated-user-id" {
			t.Fatalf("expected validated user ID for preview repo, got %q", userID)
		}
		return &mockRepoWithRecordCount{count: 5}, nil
	}

	// Interactive input: accept cloud, provide credentials, accept merge preview.
	input := strings.Join([]string{
		"y",                            // Configure cloud sync?
		"postgres://user:pass@host/db", // Neon URL
		"my-bucket",                    // S3 bucket
		"us-east-1",                    // S3 region
		"AKIAIOSFODNN7EXAMPLE",         // AWS key
		"wJalrXUtnFEMI/K7MDENG/SECRET", // AWS secret
		"pc_key_valid",                 // API key
		"y",                            // Proceed? (merge preview)
	}, "\n")

	stdout := &bytes.Buffer{}
	err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader(input+"\n"), defaultSetupOpts())
	if err != nil {
		t.Fatalf("runSetup() error = %v", err)
	}

	// Verify cloud was configured.
	store, _ := config.NewStore(homeDir)
	cfg, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	mode, _ := cfg.Mode()
	if mode != config.ModeCloud {
		t.Fatalf("expected cloud mode, got %q", mode)
	}

	out := stdout.String()
	if !strings.Contains(out, "Merge preview") {
		t.Fatalf("expected merge preview in output, got %q", out)
	}
	if !strings.Contains(out, "Cloud sync configured successfully") {
		t.Fatalf("expected success message, got %q", out)
	}
}

func TestRunSetupInteractiveCloudDeclined(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	stdout := &bytes.Buffer{}
	err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader("n\n"), defaultSetupOpts())
	if err != nil {
		t.Fatalf("runSetup() error = %v", err)
	}

	// Should be local-only.
	store, _ := config.NewStore(homeDir)
	cfg, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	mode, _ := cfg.Mode()
	if mode != config.ModeLocalOnly {
		t.Fatalf("expected local-only mode, got %q", mode)
	}
}

func TestRunSetupInteractiveMergePreviewDeclined(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	origResolve := resolveUserIDFn
	t.Cleanup(func() { resolveUserIDFn = origResolve })
	resolveUserIDFn = func(_ context.Context, _ *pgxpool.Pool, rawKey string) (string, error) {
		if rawKey != "pc_key_valid" {
			t.Fatalf("expected API key pc_key_valid, got %q", rawKey)
		}
		return "validated-user-id", nil
	}

	origPostgresRepo := newPostgresRepoFn
	t.Cleanup(func() { newPostgresRepoFn = origPostgresRepo })
	newPostgresRepoFn = func(_ *pgxpool.Pool, userID string) (repository.Repository, error) {
		if userID != "validated-user-id" {
			t.Fatalf("expected validated user ID for preview repo, got %q", userID)
		}
		return &mockRepoWithRecordCount{count: 0}, nil
	}

	// Accept cloud, provide creds, but decline at merge preview.
	input := strings.Join([]string{
		"y",
		"postgres://user:pass@host/db",
		"my-bucket",
		"us-east-1",
		"AKIAKEY",
		"AKIASECRET",
		"pc_key_valid",
		"n", // Decline merge preview
	}, "\n")

	stdout := &bytes.Buffer{}
	err := runSetup(context.Background(), stdout, &bytes.Buffer{}, strings.NewReader(input+"\n"), defaultSetupOpts())
	if err != nil {
		t.Fatalf("runSetup() error = %v", err)
	}

	// Config should still be local-only.
	store, _ := config.NewStore(homeDir)
	cfg, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	mode, _ := cfg.Mode()
	if mode != config.ModeLocalOnly {
		t.Fatalf("expected local-only mode after declining, got %q", mode)
	}

	if !strings.Contains(stdout.String(), "Cloud setup cancelled") {
		t.Fatalf("expected cancelled message, got %q", stdout.String())
	}
}

func TestRunSetupInteractiveEOFOnCredentialPrompt(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	// Accept cloud but then EOF on Neon URL prompt.
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("y\n"), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected error when credential prompt gets EOF")
	}
	if !strings.Contains(err.Error(), "read neon URL") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupInteractiveCloudPromptReaderError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	origPrompt := promptConfirmFn
	t.Cleanup(func() { promptConfirmFn = origPrompt })
	promptConfirmFn = func(_ io.Reader, _ io.Writer, _ string) (bool, error) {
		return false, errors.New("read error")
	}

	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected error on cloud prompt failure")
	}
	if !strings.Contains(err.Error(), "read cloud prompt") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupInteractiveEOFOnS3Bucket(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	input := "y\npostgres://host/db\n" // EOF after neon URL
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(input), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected error on S3 bucket EOF")
	}
	if !strings.Contains(err.Error(), "read S3 bucket") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupInteractiveEOFOnS3Region(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	input := "y\npostgres://host/db\nbucket\n" // EOF after S3 bucket
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(input), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected error on S3 region EOF")
	}
	if !strings.Contains(err.Error(), "read S3 region") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupInteractiveEOFOnAWSKey(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	input := "y\npostgres://host/db\nbucket\nus-east-1\n" // EOF after S3 region
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(input), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected error on AWS key EOF")
	}
	if !strings.Contains(err.Error(), "read AWS access key") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupInteractiveEOFOnAWSSecret(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	input := "y\npostgres://host/db\nbucket\nus-east-1\nAKIA\n" // EOF after AWS key
	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(input), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected error on AWS secret EOF")
	}
	if !strings.Contains(err.Error(), "read AWS secret key") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupInteractiveEmptyAPIKeyFails(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	mockAllCloudDeps(t)

	input := strings.Join([]string{
		"y",
		"postgres://user:pass@host/db",
		"my-bucket",
		"us-east-1",
		"AKIAKEY",
		"AKIASECRET",
		"",
	}, "\n")

	err := runSetup(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(input+"\n"), defaultSetupOpts())
	if err == nil {
		t.Fatal("expected error when interactive cloud setup API key is empty")
	}
	if !strings.Contains(err.Error(), "API key is required") {
		t.Fatalf("unexpected error = %v", err)
	}
}

// --- runSetupRemoveCloud edge cases ---

func TestRunSetupRemoveCloudConfigReadError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv(pcHomeEnvVar, homeDir)

	// Create .pc dir and corrupt config.
	store, _ := config.NewStore(homeDir)
	configPath := store.Path()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runSetupRemoveCloud(&bytes.Buffer{}, homeDir, store)
	if err == nil {
		t.Fatal("expected error when config is corrupt")
	}
}

func TestRunSetupRemoveCloudResolveUserHomeDirError(t *testing.T) {
	homeDir := setupHomeWithCloudConfig(t)

	origUserHome := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = origUserHome })
	userHomeDirFn = func() (string, error) {
		return "", errors.New("no home")
	}

	store, _ := config.NewStore(homeDir)
	err := runSetupRemoveCloud(&bytes.Buffer{}, homeDir, store)
	if err == nil {
		t.Fatal("expected error when resolveUserHomeDir fails")
	}
}

func TestRunSetupRemoveCloudConfigWriteError(t *testing.T) {
	homeDir := setupHomeWithCloudConfig(t)
	store, _ := config.NewStore(homeDir)

	origRemove := removeAWSProfileFn
	t.Cleanup(func() { removeAWSProfileFn = origRemove })
	removeCalled := false
	removeAWSProfileFn = func(string, string) error {
		removeCalled = true
		return nil
	}

	// Make config write fail by making .pc dir read-only.
	pcDir := filepath.Join(homeDir, "personal-context", ".pc")
	if err := os.Chmod(pcDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pcDir, 0o755) })

	err := runSetupRemoveCloud(&bytes.Buffer{}, homeDir, store)
	if err == nil {
		t.Fatal("expected error when config write fails")
	}
	if !strings.Contains(err.Error(), "write config") {
		t.Fatalf("unexpected error = %v", err)
	}
	if removeCalled {
		t.Fatal("removeAWSProfileFn should not be called when config write fails")
	}

	cfg, readErr := store.Read()
	if readErr != nil {
		t.Fatalf("Read() error = %v", readErr)
	}
	mode, modeErr := cfg.Mode()
	if modeErr != nil {
		t.Fatalf("Mode() error = %v", modeErr)
	}
	if mode != config.ModeCloud {
		t.Fatalf("expected cloud mode to remain after config write failure, got %q", mode)
	}
}

// --- runSetupCloud edge cases ---

func TestRunSetupCloudAWSProfileWriteError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	mockAllCloudDeps(t)

	origWrite := writeAWSProfileFn
	t.Cleanup(func() { writeAWSProfileFn = origWrite })
	writeAWSProfileFn = func(string, string, string, string) error {
		return errors.New("write failed")
	}

	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, nil,
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "SECRET",
		"", false, "pc_key_valid",
		nil, false)
	if err == nil {
		t.Fatal("expected error when AWS profile write fails")
	}
	if !strings.Contains(err.Error(), "write AWS credentials profile") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupCloudConfigWriteRollsBackAWSProfile(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	mockAllCloudDeps(t)

	// Mock userHomeDirFn to a temp dir without existing credentials.
	fakeUserHome := t.TempDir()
	origUserHome := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = origUserHome })
	userHomeDirFn = func() (string, error) { return fakeUserHome, nil }

	awsWritten := false
	origWrite := writeAWSProfileFn
	t.Cleanup(func() { writeAWSProfileFn = origWrite })
	writeAWSProfileFn = func(string, string, string, string) error {
		awsWritten = true
		return nil
	}

	awsRemoved := false
	origRemove := removeAWSProfileFn
	t.Cleanup(func() { removeAWSProfileFn = origRemove })
	removeAWSProfileFn = func(string, string) error {
		awsRemoved = true
		return nil
	}

	// Make config write fail by making the .pc dir read-only.
	pcDir := filepath.Join(homeDir, "personal-context", ".pc")
	if err := os.Chmod(pcDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pcDir, 0o755) })

	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, nil,
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "SECRET",
		"", false, "pc_key_valid",
		nil, false)
	if err == nil {
		t.Fatal("expected error when config write fails")
	}

	if !awsWritten {
		t.Fatal("expected AWS profile to be written before config write")
	}
	if !awsRemoved {
		t.Fatal("expected AWS profile to be rolled back after config write failure")
	}
}

func TestRunSetupCloudResolveUserHomeDirError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	mockAllCloudDeps(t)

	origUserHome := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = origUserHome })
	userHomeDirFn = func() (string, error) {
		return "", errors.New("no home")
	}

	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, nil,
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "SECRET",
		"", false, "pc_key_valid",
		nil, false)
	if err == nil {
		t.Fatal("expected error when resolveUserHomeDir fails")
	}
}

func TestRunSetupCloudExistingConfigReadError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	mockAllCloudDeps(t)

	configPath := store.Path()
	if err := os.WriteFile(configPath, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	writeCalled := false
	origWrite := writeAWSProfileFn
	t.Cleanup(func() { writeAWSProfileFn = origWrite })
	writeAWSProfileFn = func(string, string, string, string) error {
		writeCalled = true
		return nil
	}

	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, nil,
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "SECRET",
		"", false, "pc_key_valid",
		nil, false)
	if err == nil {
		t.Fatal("expected error when existing config is invalid")
	}
	if !strings.Contains(err.Error(), "read config") {
		t.Fatalf("unexpected error = %v", err)
	}
	if writeCalled {
		t.Fatal("writeAWSProfileFn should not be called when existing config cannot be read")
	}
}

func TestRunSetupCloudInteractiveMergePreviewRepoError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	mockAllCloudDeps(t)

	origPostgresRepo := newPostgresRepoFn
	t.Cleanup(func() { newPostgresRepoFn = origPostgresRepo })
	newPostgresRepoFn = func(*pgxpool.Pool, string) (repository.Repository, error) {
		return nil, errors.New("repo failed")
	}

	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("y\n"),
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "SECRET",
		"", false, "pc_key_valid",
		&mockRepo{}, true)
	if err == nil {
		t.Fatal("expected error when postgres repo creation fails in merge preview")
	}
}

func TestRunSetupCloudInteractiveLocalRecordsCountError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	mockAllCloudDeps(t)

	origPostgresRepo := newPostgresRepoFn
	t.Cleanup(func() { newPostgresRepoFn = origPostgresRepo })
	newPostgresRepoFn = func(*pgxpool.Pool, string) (repository.Repository, error) {
		return &mockRepo{}, nil
	}

	localRepo := &mockRepoWithListRecordsError{err: errors.New("db error")}
	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("y\n"),
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "SECRET",
		"", false, "pc_key_valid",
		localRepo, true)
	if err == nil {
		t.Fatal("expected error when local record count fails")
	}
	if !strings.Contains(err.Error(), "count local records") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupCloudInteractiveCloudRecordsCountError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	mockAllCloudDeps(t)

	origPostgresRepo := newPostgresRepoFn
	t.Cleanup(func() { newPostgresRepoFn = origPostgresRepo })
	newPostgresRepoFn = func(*pgxpool.Pool, string) (repository.Repository, error) {
		return &mockRepoWithListRecordsError{err: errors.New("cloud db error")}, nil
	}

	// localRepo succeeds, cloudRepo fails.
	localRepo := &mockRepoWithRecordCount{count: 2}
	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader("y\n"),
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "SECRET",
		"", false, "pc_key_valid",
		localRepo, true)
	if err == nil {
		t.Fatal("expected error when cloud record count fails")
	}
	if !strings.Contains(err.Error(), "count cloud records") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupCloudInteractiveConfirmReadError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	mockAllCloudDeps(t)

	origPostgresRepo := newPostgresRepoFn
	t.Cleanup(func() { newPostgresRepoFn = origPostgresRepo })
	newPostgresRepoFn = func(*pgxpool.Pool, string) (repository.Repository, error) {
		return &mockRepoWithRecordCount{count: 0}, nil
	}

	origPrompt := promptConfirmFn
	t.Cleanup(func() { promptConfirmFn = origPrompt })
	promptConfirmFn = func(_ io.Reader, _ io.Writer, _ string) (bool, error) {
		return false, errors.New("read error")
	}

	localRepo := &mockRepoWithRecordCount{count: 0}
	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""),
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "SECRET",
		"", false, "pc_key_valid",
		localRepo, true)
	if err == nil {
		t.Fatal("expected error when confirmation prompt fails")
	}
	if !strings.Contains(err.Error(), "read confirmation") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupCloudEmptyAWSKey(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, nil,
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "   ", "SECRET",
		"", false, "pc_key_valid",
		nil, false)
	if err == nil {
		t.Fatal("expected error for empty AWS key")
	}
	if !strings.Contains(err.Error(), "AWS access key is required") {
		t.Fatalf("unexpected error = %v", err)
	}
}

// --- promptLine tests ---

func TestPromptLineSuccess(t *testing.T) {
	stdout := &bytes.Buffer{}
	result, err := promptLine(strings.NewReader("hello world\n"), stdout, "Enter: ")
	if err != nil {
		t.Fatalf("promptLine() error = %v", err)
	}
	if result != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", result)
	}
	if stdout.String() != "Enter: " {
		t.Fatalf("expected prompt in stdout, got %q", stdout.String())
	}
}

func TestPromptLineTrimmed(t *testing.T) {
	result, err := promptLine(strings.NewReader("  padded  \n"), &bytes.Buffer{}, "")
	if err != nil {
		t.Fatalf("promptLine() error = %v", err)
	}
	if result != "padded" {
		t.Fatalf("expected %q, got %q", "padded", result)
	}
}

func TestPromptLineEOF(t *testing.T) {
	_, err := promptLine(strings.NewReader(""), &bytes.Buffer{}, "")
	if err == nil {
		t.Fatal("expected error on EOF")
	}
	if !strings.Contains(err.Error(), "no input received") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestPromptLineReaderError(t *testing.T) {
	_, err := promptLine(&failingReader{err: errors.New("read error")}, &bytes.Buffer{}, "")
	if err == nil {
		t.Fatal("expected error from failing reader")
	}
}

// --- promptConfirm tests ---

func TestPromptConfirmYes(t *testing.T) {
	for _, input := range []string{"y", "Y", " y ", " Y "} {
		result, err := promptConfirm(strings.NewReader(input+"\n"), &bytes.Buffer{}, "OK?")
		if err != nil {
			t.Fatalf("promptConfirm(%q) error = %v", input, err)
		}
		if !result {
			t.Fatalf("expected true for %q", input)
		}
	}
}

func TestPromptConfirmNo(t *testing.T) {
	for _, input := range []string{"n", "N", "no", "", "anything"} {
		result, err := promptConfirm(strings.NewReader(input+"\n"), &bytes.Buffer{}, "OK?")
		if err != nil {
			t.Fatalf("promptConfirm(%q) error = %v", input, err)
		}
		if result {
			t.Fatalf("expected false for %q", input)
		}
	}
}

func TestPromptConfirmEOFReturnsFalse(t *testing.T) {
	result, err := promptConfirm(strings.NewReader(""), &bytes.Buffer{}, "OK?")
	if err != nil {
		t.Fatalf("promptConfirm() error = %v", err)
	}
	if result {
		t.Fatal("expected false on EOF")
	}
}

func TestPromptConfirmReaderError(t *testing.T) {
	_, err := promptConfirm(&failingReader{err: errors.New("read error")}, &bytes.Buffer{}, "OK?")
	if err == nil {
		t.Fatal("expected error from failing reader")
	}
}

// --- runSetupCloud validation edge cases ---

func TestRunSetupCloudInvalidS3Bucket(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, nil,
		homeDir, store,
		"postgres://user:pass@host/db", "A", "us-east-1", "KEY", "SECRET",
		"", false, "pc_key_valid",
		nil, false)
	if err == nil {
		t.Fatal("expected error for invalid S3 bucket")
	}
}

func TestRunSetupCloudInvalidS3Region(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, nil,
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "invalid", "KEY", "SECRET",
		"", false, "pc_key_valid",
		nil, false)
	if err == nil {
		t.Fatal("expected error for invalid S3 region")
	}
}

func TestRunSetupCloudEmptyAWSSecret(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, nil,
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "   ",
		"", false, "pc_key_valid",
		nil, false)
	if err == nil {
		t.Fatal("expected error for empty AWS secret")
	}
}

func TestRunSetupCloudEmptyAPIKey(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, nil,
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "SECRET",
		"", false, "   ",
		nil, false)
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
	if !strings.Contains(err.Error(), "API key is required") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupCloudAPIKeyValidationError(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	mockAllCloudDeps(t)

	origResolve := resolveUserIDFn
	t.Cleanup(func() { resolveUserIDFn = origResolve })
	resolveUserIDFn = func(context.Context, *pgxpool.Pool, string) (string, error) {
		return "", errors.New("key not found")
	}

	err := runSetupCloud(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, nil,
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "SECRET",
		"", false, "pc_key_invalid",
		nil, false)
	if err == nil {
		t.Fatal("expected error for invalid API key")
	}
	if !strings.Contains(err.Error(), "validate API key") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRunSetupCloudAPIKeyValidationSuccess(t *testing.T) {
	homeDir := setupHomeWithConfig(t)
	store, _ := config.NewStore(homeDir)

	mockAllCloudDeps(t)

	origResolve := resolveUserIDFn
	t.Cleanup(func() { resolveUserIDFn = origResolve })
	resolveUserIDFn = func(context.Context, *pgxpool.Pool, string) (string, error) {
		return "validated-user-id", nil
	}

	stdout := &bytes.Buffer{}
	err := runSetupCloud(context.Background(), stdout, &bytes.Buffer{}, nil,
		homeDir, store,
		"postgres://user:pass@host/db", "my-bucket", "us-east-1", "KEY", "SECRET",
		"", false, "pc_key_valid",
		nil, false)
	if err != nil {
		t.Fatalf("runSetupCloud() error = %v", err)
	}

	// Verify API key was stored in config.
	cfg, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if cfg.APIKey != "pc_key_valid" {
		t.Fatalf("expected api_key %q, got %q", "pc_key_valid", cfg.APIKey)
	}
}

// --- Test helpers ---

// fullCloudOpts returns setupOptions with all cloud flags set.
func fullCloudOpts() setupOptions {
	return setupOptions{
		NeonURL:   "postgres://user:pass@host/db",
		S3Bucket:  "my-bucket",
		S3Region:  "us-east-1",
		AWSKey:    "AKIAIOSFODNN7EXAMPLE",
		AWSSecret: "wJalrXUtnFEMI/K7MDENG/SECRET",
		APIKey:    "pc_key_valid",
	}
}

// mockAllCloudDeps mocks all cloud infrastructure dependencies for unit tests.
func mockAllCloudDeps(t *testing.T) {
	t.Helper()

	origValidateNeon := validateNeonConnectivityFn
	t.Cleanup(func() { validateNeonConnectivityFn = origValidateNeon })
	validateNeonConnectivityFn = func(context.Context, string) error { return nil }

	origValidateS3 := validateS3AccessFn
	t.Cleanup(func() { validateS3AccessFn = origValidateS3 })
	validateS3AccessFn = func(context.Context, string, string, string, string, string, bool) error { return nil }

	origSchema := applyPostgresSchemaFn
	t.Cleanup(func() { applyPostgresSchemaFn = origSchema })
	applyPostgresSchemaFn = func(context.Context, *pgxpool.Pool) error { return nil }

	origPool := newPGXPoolFn
	t.Cleanup(func() { newPGXPoolFn = origPool })
	newPGXPoolFn = func(context.Context, string) (*pgxpool.Pool, error) { return nil, nil }

	origResolveUserID := resolveUserIDFn
	t.Cleanup(func() { resolveUserIDFn = origResolveUserID })
	resolveUserIDFn = func(context.Context, *pgxpool.Pool, string) (string, error) {
		return "validated-user-id", nil
	}

	origClose := closePGXPoolFn
	t.Cleanup(func() { closePGXPoolFn = origClose })
	closePGXPoolFn = func(*pgxpool.Pool) {}

	origWrite := writeAWSProfileFn
	t.Cleanup(func() { writeAWSProfileFn = origWrite })
	writeAWSProfileFn = func(string, string, string, string) error { return nil }

	origRemove := removeAWSProfileFn
	t.Cleanup(func() { removeAWSProfileFn = origRemove })
	removeAWSProfileFn = func(string, string) error { return nil }
}

// mockRepoWithRecordCount is a mock repo that returns a fixed number of records.
type mockRepoWithRecordCount struct {
	mockRepo
	count int
}

func (m *mockRepoWithRecordCount) ListRecords(_ context.Context, _ repository.ListRecordsFilter) ([]repository.Record, error) {
	records := make([]repository.Record, m.count)
	for i := range records {
		records[i] = repository.Record{ID: "test-record"}
	}
	return records, nil
}

// mockRepoWithListRecordsError is a mock repo that returns an error from ListRecords.
type mockRepoWithListRecordsError struct {
	mockRepo
	err error
}

func (m *mockRepoWithListRecordsError) ListRecords(_ context.Context, _ repository.ListRecordsFilter) ([]repository.Record, error) {
	return nil, m.err
}

// failingReader is an io.Reader that always returns an error.
type failingReader struct {
	err error
}

func (f *failingReader) Read([]byte) (int, error) {
	return 0, f.err
}
