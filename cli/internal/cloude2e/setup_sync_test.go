//go:build integration

package cloude2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudOnboardingFirstSyncAndDoctor(t *testing.T) {
	cloud := newCloudTestEnv(t)

	// --- Phase 1: Set up a local-only home with one record before cloud onboarding ---
	homeDir := t.TempDir()
	fakeUserHome := t.TempDir()

	// Initialize local-only first.
	result := runPCWithEnv(t, homeDir, fakeUserHome, strings.NewReader("n\n"), "setup")
	if result.ExitCode != 0 {
		t.Fatalf("local setup failed (exit %d):\nstdout: %s\nstderr: %s",
			result.ExitCode, result.Stdout, result.Stderr)
	}

	// Add a record before cloud setup so sync has something to push.
	inputDir := createInputFolder(t, "<html>Pre-cloud record</html>", "Pre-cloud notes", nil, nil)
	preCloudID := strings.TrimSpace(runPCSuccess(t, homeDir, fakeUserHome, "add", inputDir))
	if preCloudID == "" {
		t.Fatal("expected record ID from add")
	}

	// --- Phase 2: Configure cloud ---
	result = runPCWithEnv(t, homeDir, fakeUserHome, nil,
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
		t.Fatalf("cloud setup failed (exit %d):\nstdout: %s\nstderr: %s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "Cloud sync configured successfully") {
		t.Fatalf("expected success message, got:\n%s", result.Stdout)
	}

	// Verify config is in cloud mode.
	configPath := filepath.Join(homeDir, "personal-context", ".pc", "config.json")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(configBytes, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg["aws_profile"] != "personal-context" {
		t.Fatalf("expected aws_profile=personal-context, got %v", cfg["aws_profile"])
	}
	if cfg["s3_endpoint"] != cloudEnv.minioEndpoint {
		t.Fatalf("expected s3_endpoint=%s, got %v", cloudEnv.minioEndpoint, cfg["s3_endpoint"])
	}

	// Verify AWS credentials were written.
	awsCredsPath := filepath.Join(fakeUserHome, ".aws", "credentials")
	if _, err := os.Stat(awsCredsPath); err != nil {
		t.Fatalf("expected AWS credentials at %s: %v", awsCredsPath, err)
	}

	// --- Phase 3: Sync --- the pre-existing record should be pushed to cloud.
	syncOut := runPCSuccess(t, homeDir, fakeUserHome, "sync")
	if !strings.Contains(syncOut, "Sync complete") {
		t.Fatalf("expected 'Sync complete', got:\n%s", syncOut)
	}

	// Verify the record exists in the cloud by checking Postgres.
	record := getRecordJSON(t, homeDir, fakeUserHome, preCloudID)
	if record.HTMLContent != "<html>Pre-cloud record</html>" {
		t.Fatalf("record content mismatch after sync: %q", record.HTMLContent)
	}

	// --- Phase 4: Doctor --- should report OK with cloud checks.
	doctorOut := runPCSuccess(t, homeDir, fakeUserHome, "doctor")
	if !strings.Contains(doctorOut, "Cloud") {
		t.Fatalf("expected doctor to show Cloud line, got:\n%s", doctorOut)
	}
	// Doctor should not report errors.
	if strings.Contains(strings.ToLower(doctorOut), "error") {
		t.Fatalf("doctor reported errors:\n%s", doctorOut)
	}
}
