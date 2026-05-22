//go:build integration

package cloude2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	postgresrepo "github.com/conn-castle/personal-context/cli/internal/repository/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	miniomodule "github.com/testcontainers/testcontainers-go/modules/minio"
	pgmodule "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	binaryPath string

	cloudEnv struct {
		postgresConnString string
		minioEndpoint      string
		minioUsername      string
		minioPassword      string
	}

	schemaCounter int
	bucketCounter int

	registeredProjects = map[string]bool{}
	registeredDevices  = map[string]bool{}
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Build the pc binary.
	tmpDir, err := os.MkdirTemp("", "pc-cloud-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	binaryPath = filepath.Join(tmpDir, "pc")
	cmd := exec.Command("go", "build", "-o", binaryPath, "github.com/conn-castle/personal-context/cli/cmd/pc")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build pc binary: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	// Start Postgres container.
	postgresContainer, err := pgmodule.Run(ctx,
		"postgres:16-alpine",
		pgmodule.WithDatabase("testdb"),
		pgmodule.WithUsername("test"),
		pgmodule.WithPassword("test"),
		pgmodule.WithSQLDriver("pgx"),
		pgmodule.BasicWaitStrategies(),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}
	connString, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = postgresContainer.Terminate(ctx)
		fmt.Fprintf(os.Stderr, "postgres connection string: %v\n", err)
		os.Exit(1)
	}
	cloudEnv.postgresConnString = connString

	// Start MinIO container.
	minioContainer, err := miniomodule.Run(ctx,
		"minio/minio:RELEASE.2024-01-16T16-07-38Z",
		miniomodule.WithUsername("minioadmin"),
		miniomodule.WithPassword("minioadmin"),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/minio/health/live").
				WithPort("9000/tcp").
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		_ = postgresContainer.Terminate(ctx)
		fmt.Fprintf(os.Stderr, "start minio: %v\n", err)
		os.Exit(1)
	}

	endpoint, err := minioContainer.ConnectionString(ctx)
	if err != nil {
		_ = minioContainer.Terminate(ctx)
		_ = postgresContainer.Terminate(ctx)
		fmt.Fprintf(os.Stderr, "minio endpoint: %v\n", err)
		os.Exit(1)
	}
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = "http://" + endpoint
	}
	cloudEnv.minioEndpoint = endpoint
	cloudEnv.minioUsername = minioContainer.Username
	cloudEnv.minioPassword = minioContainer.Password

	exitCode := m.Run()
	_ = minioContainer.Terminate(ctx)
	_ = postgresContainer.Terminate(ctx)
	_ = os.RemoveAll(tmpDir)
	os.Exit(exitCode)
}

// cloudTestEnv holds isolated cloud resources for a single test.
type cloudTestEnv struct {
	NeonURL    string
	BucketName string
	S3Client   *awss3.Client
	APIKey     string
	UserID     string
}

// newCloudTestEnv creates an isolated Postgres schema and S3 bucket for a test.
func newCloudTestEnv(t *testing.T) cloudTestEnv {
	t.Helper()
	ctx := context.Background()

	// Create isolated Postgres schema.
	schemaCounter++
	schemaName := fmt.Sprintf("cloude2e_%d_%d", time.Now().UnixNano(), schemaCounter)

	pool, err := pgxpool.New(ctx, cloudEnv.postgresConnString)
	if err != nil {
		t.Fatalf("pgxpool.New(): %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		pool.Close()
		t.Fatalf("CREATE SCHEMA: %v", err)
	}
	pool.Close()
	t.Cleanup(func() {
		cleanupPool, err := pgxpool.New(context.Background(), cloudEnv.postgresConnString)
		if err != nil {
			t.Logf("schema cleanup: pgxpool.New: %v", err)
			return
		}
		defer cleanupPool.Close()
		if _, err := cleanupPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName)); err != nil {
			t.Logf("schema cleanup: DROP SCHEMA %s: %v", schemaName, err)
		}
	})

	connWithSchema := cloudEnv.postgresConnString + fmt.Sprintf("&search_path=%s", schemaName)
	apiKey := fmt.Sprintf("pc_key_cloude2e_%d_%d", time.Now().UnixNano(), schemaCounter)
	userID := fmt.Sprintf("cloude2e-user-%d-%d", time.Now().UnixNano(), schemaCounter)
	seedCloudUserAndAPIKey(t, connWithSchema, userID, apiKey)

	// Create isolated S3 bucket.
	bucketCounter++
	bucketName := fmt.Sprintf("cloude2e-%d-%d", time.Now().UnixNano(), bucketCounter)
	s3Client := awss3.New(awss3.Options{
		BaseEndpoint: aws.String(cloudEnv.minioEndpoint),
		Credentials: credentials.NewStaticCredentialsProvider(
			cloudEnv.minioUsername,
			cloudEnv.minioPassword,
			"",
		),
		Region:       "us-east-1",
		UsePathStyle: true,
	})
	if _, err := s3Client.CreateBucket(ctx, &awss3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}); err != nil {
		t.Fatalf("CreateBucket(%s): %v", bucketName, err)
	}
	t.Cleanup(func() {
		listOut, err := s3Client.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{
			Bucket: aws.String(bucketName),
		})
		if err != nil {
			t.Logf("bucket cleanup: ListObjectsV2(%s): %v", bucketName, err)
			return
		}
		for _, obj := range listOut.Contents {
			if _, err := s3Client.DeleteObject(ctx, &awss3.DeleteObjectInput{
				Bucket: aws.String(bucketName),
				Key:    obj.Key,
			}); err != nil {
				t.Logf("bucket cleanup: DeleteObject(%s/%s): %v", bucketName, *obj.Key, err)
			}
		}
		if _, err := s3Client.DeleteBucket(ctx, &awss3.DeleteBucketInput{
			Bucket: aws.String(bucketName),
		}); err != nil {
			t.Logf("bucket cleanup: DeleteBucket(%s): %v", bucketName, err)
		}
	})

	return cloudTestEnv{
		NeonURL:    connWithSchema,
		BucketName: bucketName,
		S3Client:   s3Client,
		APIKey:     apiKey,
		UserID:     userID,
	}
}

func seedCloudUserAndAPIKey(t *testing.T, neonURL string, userID string, apiKey string) {
	t.Helper()

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, neonURL)
	if err != nil {
		t.Fatalf("pgxpool.New(): %v", err)
	}
	defer pool.Close()

	if err := postgresrepo.ApplySchema(ctx, pool); err != nil {
		t.Fatalf("apply cloud schema: %v", err)
	}

	sum := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(sum[:])
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash)
		 VALUES ($1, $2, $3)`,
		userID,
		userID+"@example.test",
		"hash-placeholder",
	); err != nil {
		t.Fatalf("insert cloud user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO api_keys (user_id, key_hash, label)
		 VALUES ($1, $2, $3)`,
		userID,
		keyHash,
		"cloud e2e",
	); err != nil {
		t.Fatalf("insert cloud api key: %v", err)
	}
}

// setupCloudHome creates a temp PC_HOME, runs `pc setup` with cloud flags, and
// returns the homeDir. The HOME env is set to a temp dir so AWS credentials
// writes don't affect the real user home. This applies the Postgres schema, so
// only the first call per cloudTestEnv should use this. Subsequent homes against
// the same cloud should use setupCloudHomeNoSchema.
func setupCloudHome(t *testing.T, cloud cloudTestEnv) (homeDir string, fakeUserHome string) {
	t.Helper()

	homeDir = t.TempDir()
	fakeUserHome = t.TempDir()

	// Run pc setup with cloud flags.
	result := runPCWithEnv(t, homeDir, fakeUserHome, nil,
		"setup",
		"--neon-url", cloud.NeonURL,
		"--s3-bucket", cloud.BucketName,
		"--s3-region", "us-east-1",
		"--aws-key", cloudEnv.minioUsername,
		"--aws-secret", cloudEnv.minioPassword,
		"--s3-endpoint", cloudEnv.minioEndpoint,
		"--s3-force-path-style",
		"--api-key", cloud.APIKey,
	)
	if result.ExitCode != 0 {
		t.Fatalf("pc setup failed (exit %d):\nstdout: %s\nstderr: %s",
			result.ExitCode, result.Stdout, result.Stderr)
	}

	return homeDir, fakeUserHome
}

// setupCloudHomeNoSchema creates a second home against the same cloud backend.
// It initializes the local SQLite database and writes cloud config directly,
// bypassing schema application (since the schema already exists from the first home).
func setupCloudHomeNoSchema(t *testing.T, cloud cloudTestEnv) (homeDir string, fakeUserHome string) {
	t.Helper()

	homeDir = t.TempDir()
	fakeUserHome = t.TempDir()

	// First do a local-only setup to initialize SQLite.
	result := runPCWithEnv(t, homeDir, fakeUserHome,
		strings.NewReader("n\n"), "setup")
	if result.ExitCode != 0 {
		t.Fatalf("local setup failed (exit %d):\nstdout: %s\nstderr: %s",
			result.ExitCode, result.Stdout, result.Stderr)
	}

	// Write cloud config directly to config.json.
	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	cfg := map[string]interface{}{
		"neon_url":            cloud.NeonURL,
		"s3_bucket":           cloud.BucketName,
		"s3_region":           "us-east-1",
		"aws_profile":         "personal-context",
		"s3_endpoint":         cloudEnv.minioEndpoint,
		"s3_force_path_style": true,
		"api_key":             cloud.APIKey,
	}
	cfgBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(configPath, cfgBytes, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Write AWS credentials so the CLI can load them.
	awsDir := filepath.Join(fakeUserHome, ".aws")
	if err := os.MkdirAll(awsDir, 0o700); err != nil {
		t.Fatalf("mkdir .aws: %v", err)
	}
	awsCreds := fmt.Sprintf("[personal-context]\naws_access_key_id = %s\naws_secret_access_key = %s\n",
		cloudEnv.minioUsername, cloudEnv.minioPassword)
	if err := os.WriteFile(filepath.Join(awsDir, "credentials"), []byte(awsCreds), 0o600); err != nil {
		t.Fatalf("write aws creds: %v", err)
	}

	return homeDir, fakeUserHome
}

// runResult captures the output of a pc command execution.
type runResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// runPCWithEnv runs the pc binary with PC_HOME and HOME overrides.
func runPCWithEnv(t *testing.T, homeDir string, userHome string, stdin io.Reader, args ...string) runResult {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	cmd.Env = cloudCommandEnv(homeDir, userHome)
	cmd.Stdin = stdin

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run pc: %v", err)
		}
	}

	return runResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// runPCSuccess runs pc and fails if exit code is non-zero.
func runPCSuccess(t *testing.T, homeDir string, userHome string, args ...string) string {
	t.Helper()
	args = withMutationProvenance(t, homeDir, userHome, args)
	result := runPCWithEnv(t, homeDir, userHome, nil, args...)
	if result.ExitCode != 0 {
		t.Fatalf("pc %v failed (exit %d):\nstdout: %s\nstderr: %s",
			args, result.ExitCode, result.Stdout, result.Stderr)
	}
	return result.Stdout
}

// runPCSuccessNoStderr runs pc and fails if exit code is non-zero or stderr is not empty.
func runPCSuccessNoStderr(t *testing.T, homeDir string, userHome string, args ...string) string {
	t.Helper()

	args = withMutationProvenance(t, homeDir, userHome, args)
	result := runPCWithEnv(t, homeDir, userHome, nil, args...)
	if result.ExitCode != 0 {
		t.Fatalf("pc %v failed (exit %d):\nstdout: %s\nstderr: %s",
			args, result.ExitCode, result.Stdout, result.Stderr)
	}
	if strings.TrimSpace(result.Stderr) != "" {
		t.Fatalf("pc %v wrote unexpected stderr:\n%s", args, result.Stderr)
	}
	return result.Stdout
}

func withMutationProvenance(t *testing.T, homeDir string, userHome string, args []string) []string {
	t.Helper()
	if len(args) < 2 || args[0] != "records" {
		return args
	}
	subcommand := args[1]
	recordArgs := args[2:]
	if subcommand == "edit" && len(recordArgs) >= 2 {
		ensureEditMetadata(t, homeDir, userHome, recordArgs[0], recordArgs[1])
		return args
	}
	for _, recordCommand := range []string{"delete", "restore", "move", "list", "stats", "files"} {
		if subcommand == recordCommand {
			return args
		}
	}
	if subcommand != "add" {
		return args
	}

	const defaultProjectID = "cloud-e2e/default"
	const defaultDeviceID = "cloud-e2e-device"

	projectID, hasProject := flagValue(recordArgs, "--project")
	deviceID, hasDevice := flagValue(recordArgs, "--device")
	if !hasProject {
		projectID = defaultProjectID
	}
	if !hasDevice {
		deviceID = defaultDeviceID
	}
	ensureProjectRegistered(t, homeDir, userHome, projectID)
	ensureDeviceRegistered(t, homeDir, userHome, deviceID)

	withProvenance := append([]string{}, args...)
	insertAt := 2
	if !hasDevice {
		withProvenance = append(withProvenance[:insertAt], append([]string{"--device", deviceID}, withProvenance[insertAt:]...)...)
	}
	if !hasProject {
		withProvenance = append(withProvenance[:insertAt], append([]string{"--project", projectID}, withProvenance[insertAt:]...)...)
	}
	return withProvenance
}

func ensureEditMetadata(t *testing.T, homeDir string, userHome string, recordID string, inputDir string) {
	t.Helper()
	metadataPath := filepath.Join(inputDir, "metadata.json")
	if _, err := os.Stat(metadataPath); err == nil {
		return
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat edit metadata: %v", err)
	}
	record := getRecordJSON(t, homeDir, userHome, recordID)
	raw, err := json.MarshalIndent(map[string]string{
		"project_id":       record.ProjectID,
		"source_device_id": record.SourceDeviceID,
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal edit metadata: %v", err)
	}
	if err := os.WriteFile(metadataPath, raw, 0o644); err != nil {
		t.Fatalf("write edit metadata: %v", err)
	}
}

func flagValue(args []string, name string) (string, bool) {
	for idx, arg := range args {
		if arg == name && idx+1 < len(args) {
			return args[idx+1], true
		}
		if strings.HasPrefix(arg, name+"=") {
			return strings.TrimPrefix(arg, name+"="), true
		}
	}
	return "", false
}

func ensureProjectRegistered(t *testing.T, homeDir string, userHome string, projectID string) {
	t.Helper()
	key := homeDir + "\x00" + projectID
	if registeredProjects[key] {
		return
	}
	result := runPCWithEnv(t, homeDir, userHome, nil, "project", "register", projectID)
	if result.ExitCode != 0 {
		t.Fatalf("pc project register %q failed (exit %d):\nstdout: %s\nstderr: %s", projectID, result.ExitCode, result.Stdout, result.Stderr)
	}
	registeredProjects[key] = true
}

func ensureDeviceRegistered(t *testing.T, homeDir string, userHome string, deviceID string) {
	t.Helper()
	key := homeDir + "\x00" + deviceID
	if registeredDevices[key] {
		return
	}
	result := runPCWithEnv(t, homeDir, userHome, nil, "device", "register", deviceID)
	if result.ExitCode != 0 {
		t.Fatalf("pc device register %q failed (exit %d):\nstdout: %s\nstderr: %s", deviceID, result.ExitCode, result.Stdout, result.Stderr)
	}
	registeredDevices[key] = true
}

// runPCFailure runs pc and fails if exit code is zero.
func runPCFailure(t *testing.T, homeDir string, userHome string, args ...string) string {
	t.Helper()
	result := runPCWithEnv(t, homeDir, userHome, nil, args...)
	if result.ExitCode == 0 {
		t.Fatalf("pc %v unexpectedly succeeded:\nstdout: %s", args, result.Stdout)
	}
	return result.Stderr
}

// backdateRecordCloud sets a record's updated_at in Postgres to 5 seconds earlier
// than its current value. This makes conflict-resolution tests deterministic by
// ensuring that a later edit against the same record has an unambiguously later
// timestamp, without relying on time.Sleep.
func backdateRecordCloud(t *testing.T, neonURL string, recordID string) {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, neonURL)
	if err != nil {
		t.Fatalf("pgxpool for backdate: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx,
		"UPDATE records SET updated_at = updated_at - interval '5 seconds' WHERE id = $1",
		recordID); err != nil {
		t.Fatalf("backdate cloud updated_at for %s: %v", recordID, err)
	}
}

// cloudCommandEnv builds the environment for the pc binary with PC_HOME and HOME overrides.
func cloudCommandEnv(homeDir string, userHome string) []string {
	envMap := make(map[string]string)
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		key := parts[0]
		value := ""
		if len(parts) == 2 {
			value = parts[1]
		}
		envMap[key] = value
	}
	envMap["PC_HOME"] = homeDir
	envMap["HOME"] = userHome
	envMap["AWS_EC2_METADATA_DISABLED"] = "true"

	env := make([]string, 0, len(envMap))
	for key, value := range envMap {
		env = append(env, key+"="+value)
	}
	return env
}

// createInputFolder creates a temp folder suitable for `pc add` input.
func createInputFolder(t *testing.T, htmlContent string, notes string, figures map[string][]byte, dataFiles map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()

	if htmlContent == "" {
		htmlContent = "<html><body>Test</body></html>"
	}
	if err := os.WriteFile(filepath.Join(dir, "record.html"), []byte(htmlContent), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	if notes != "" {
		if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(notes), 0o644); err != nil {
			t.Fatalf("write notes.md: %v", err)
		}
	}
	if len(figures) > 0 {
		figDir := filepath.Join(dir, "figures")
		if err := os.MkdirAll(figDir, 0o700); err != nil {
			t.Fatalf("create figures dir: %v", err)
		}
		for name, data := range figures {
			if err := os.WriteFile(filepath.Join(figDir, name), data, 0o644); err != nil {
				t.Fatalf("write figure %s: %v", name, err)
			}
		}
	}
	if len(dataFiles) > 0 {
		dataDir := filepath.Join(dir, "data")
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			t.Fatalf("create data dir: %v", err)
		}
		for name, data := range dataFiles {
			if err := os.WriteFile(filepath.Join(dataDir, name), data, 0o644); err != nil {
				t.Fatalf("write data file %s: %v", name, err)
			}
		}
	}
	return dir
}

// showJSON represents the relevant fields from `pc show --format json`.
type showJSON struct {
	ID             string `json:"id"`
	Date           string `json:"date"`
	HTMLContent    string `json:"html_content"`
	Notes          string `json:"notes"`
	ProjectID      string `json:"project_id"`
	SourceDeviceID string `json:"source_device_id"`
}

// getRecordJSON runs `pc show --format json` and parses the output.
func getRecordJSON(t *testing.T, homeDir string, userHome string, recordID string) showJSON {
	t.Helper()
	stdout := runPCSuccess(t, homeDir, userHome, "show", "--format", "json", recordID)
	var s showJSON
	if err := json.Unmarshal([]byte(stdout), &s); err != nil {
		t.Fatalf("parse show json for %s: %v\nraw: %s", recordID, err, stdout)
	}
	return s
}
