package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAWSProfileCreatesCredentialsFile(t *testing.T) {
	homeDir := t.TempDir()

	if err := writeAWSSharedCredentialsProfile(homeDir, "personal-context", "test-key", "test-secret"); err != nil {
		t.Fatalf("writeAWSSharedCredentialsProfile() error = %v", err)
	}

	path := awsCredentialsPath(homeDir)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "[personal-context]") {
		t.Fatalf("expected profile header in credentials file, got %q", text)
	}
	if !strings.Contains(text, "aws_access_key_id = test-key") {
		t.Fatalf("expected access key in credentials file, got %q", text)
	}
	if !strings.Contains(text, "aws_secret_access_key = test-secret") {
		t.Fatalf("expected secret key in credentials file, got %q", text)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected 0600 permissions, got %04o", got)
	}
}

func TestWriteAWSProfileReplacesExistingSectionAndPreservesOthers(t *testing.T) {
	homeDir := t.TempDir()
	path := awsCredentialsPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	initial := `[other]
aws_access_key_id = other-key
aws_secret_access_key = other-secret

[personal-context]
aws_access_key_id = old-key
aws_secret_access_key = old-secret

[third]
aws_access_key_id = third-key
aws_secret_access_key = third-secret
`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := writeAWSSharedCredentialsProfile(homeDir, "personal-context", "new-key", "new-secret"); err != nil {
		t.Fatalf("writeAWSSharedCredentialsProfile() error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	text := string(content)
	if strings.Count(text, "[personal-context]") != 1 {
		t.Fatalf("expected one personal-context section, got %q", text)
	}
	if !strings.Contains(text, "[other]") || !strings.Contains(text, "[third]") {
		t.Fatalf("expected other profiles to be preserved, got %q", text)
	}
	if strings.Contains(text, "old-key") {
		t.Fatalf("expected old credentials to be removed, got %q", text)
	}
	if !strings.Contains(text, "aws_access_key_id = new-key") {
		t.Fatalf("expected new credentials to be written, got %q", text)
	}
}

func TestRemoveAWSProfileDeletesTargetSectionAndPreservesOthers(t *testing.T) {
	homeDir := t.TempDir()
	path := awsCredentialsPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	initial := `[other]
aws_access_key_id = other-key
aws_secret_access_key = other-secret

[personal-context]
aws_access_key_id = old-key
aws_secret_access_key = old-secret
`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := removeAWSSharedCredentialsProfile(homeDir, "personal-context"); err != nil {
		t.Fatalf("removeAWSSharedCredentialsProfile() error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	text := string(content)
	if strings.Contains(text, "[personal-context]") {
		t.Fatalf("expected target profile to be removed, got %q", text)
	}
	if !strings.Contains(text, "[other]") {
		t.Fatalf("expected other profile to remain, got %q", text)
	}
}

func TestRemoveAWSProfileMissingFileIsNoOp(t *testing.T) {
	homeDir := t.TempDir()

	if err := removeAWSSharedCredentialsProfile(homeDir, "personal-context"); err != nil {
		t.Fatalf("removeAWSSharedCredentialsProfile() error = %v", err)
	}
}

// --- Validation error paths ---

func TestWriteAWSProfileEmptyHomeDir(t *testing.T) {
	err := writeAWSSharedCredentialsProfile("", "profile", "key", "secret")
	if err == nil {
		t.Fatal("expected error for empty homeDir")
	}
	if !strings.Contains(err.Error(), "home directory is required") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestWriteAWSProfileEmptyProfileName(t *testing.T) {
	err := writeAWSSharedCredentialsProfile(t.TempDir(), "", "key", "secret")
	if err == nil {
		t.Fatal("expected error for empty profileName")
	}
	if !strings.Contains(err.Error(), "profile name is required") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestWriteAWSProfileEmptyAccessKeyID(t *testing.T) {
	err := writeAWSSharedCredentialsProfile(t.TempDir(), "profile", "", "secret")
	if err == nil {
		t.Fatal("expected error for empty accessKeyID")
	}
	if !strings.Contains(err.Error(), "aws access key id is required") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestWriteAWSProfileEmptySecretAccessKey(t *testing.T) {
	err := writeAWSSharedCredentialsProfile(t.TempDir(), "profile", "key", "")
	if err == nil {
		t.Fatal("expected error for empty secretAccessKey")
	}
	if !strings.Contains(err.Error(), "aws secret access key is required") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRemoveAWSProfileEmptyHomeDir(t *testing.T) {
	err := removeAWSSharedCredentialsProfile("", "profile")
	if err == nil {
		t.Fatal("expected error for empty homeDir")
	}
	if !strings.Contains(err.Error(), "home directory is required") {
		t.Fatalf("unexpected error = %v", err)
	}
}

func TestRemoveAWSProfileEmptyProfileName(t *testing.T) {
	err := removeAWSSharedCredentialsProfile(t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error for empty profileName")
	}
	if !strings.Contains(err.Error(), "profile name is required") {
		t.Fatalf("unexpected error = %v", err)
	}
}

// --- readINIFileLines edge cases ---

func TestReadINIFileLinesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.ini")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	lines, err := readINIFileLines(path)
	if err != nil {
		t.Fatalf("readINIFileLines() error = %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("expected empty slice, got %v", lines)
	}
}

func TestReadINIFileLinesReadError(t *testing.T) {
	// Create a directory where a file is expected so ReadFile fails with a non-NotExist error.
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-file")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	_, err := readINIFileLines(path)
	if err == nil {
		t.Fatal("expected error when reading a directory as file")
	}
}

// --- upsertINISection edge cases ---

func TestUpsertINISectionRemovesThenAppends(t *testing.T) {
	lines := []string{
		"[existing]",
		"key = old",
		"",
	}
	newSection := []string{
		"[existing]",
		"key = new",
	}

	result := upsertINISection(lines, "existing", newSection)
	joined := strings.Join(result, "\n")
	if strings.Contains(joined, "old") {
		t.Fatalf("expected old section to be removed, got %q", joined)
	}
	if !strings.Contains(joined, "key = new") {
		t.Fatalf("expected new section to be appended, got %q", joined)
	}
}

func TestUpsertINISectionIntoEmptyLines(t *testing.T) {
	newSection := []string{
		"[new-profile]",
		"key = value",
	}

	result := upsertINISection([]string{}, "new-profile", newSection)
	if len(result) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(result), result)
	}
}

// --- trimLeadingBlankLines edge cases ---

func TestTrimLeadingBlankLinesAllBlank(t *testing.T) {
	lines := []string{"", "  ", "\t", ""}
	result := trimLeadingBlankLines(lines)
	if len(result) != 0 {
		t.Fatalf("expected empty slice, got %v", result)
	}
}

func TestTrimLeadingBlankLinesNoBlanks(t *testing.T) {
	lines := []string{"[section]", "key = value"}
	result := trimLeadingBlankLines(lines)
	if len(result) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(result))
	}
}

// --- currentUserAWSCredentialsPath error path ---

func TestCurrentUserAWSCredentialsPathUserHomeDirError(t *testing.T) {
	original := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = original })
	userHomeDirFn = func() (string, error) {
		return "", fmt.Errorf("user home failed")
	}

	_, err := currentUserAWSCredentialsPath()
	if err == nil {
		t.Fatal("expected error when userHomeDirFn fails")
	}
}

// --- writeAWSSharedCredentialsProfile non-NotExist read error ---

func TestWriteAWSProfileReadError(t *testing.T) {
	homeDir := t.TempDir()
	path := awsCredentialsPath(homeDir)
	// Create .aws as a directory and credentials as a directory to cause a non-NotExist read error
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	err := writeAWSSharedCredentialsProfile(homeDir, "profile", "key", "secret")
	if err == nil {
		t.Fatal("expected error when credentials file is a directory")
	}
}

// --- removeAWSSharedCredentialsProfile non-NotExist read error ---

func TestRemoveAWSProfileReadError(t *testing.T) {
	homeDir := t.TempDir()
	path := awsCredentialsPath(homeDir)
	// Create credentials as a directory to cause read error
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	err := removeAWSSharedCredentialsProfile(homeDir, "profile")
	if err == nil {
		t.Fatal("expected error when credentials file is a directory")
	}
}

// --- removeAWSSharedCredentialsProfile with section not found ---

func TestRemoveAWSProfileSectionNotFound(t *testing.T) {
	homeDir := t.TempDir()
	path := awsCredentialsPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	initial := "[other]\naws_access_key_id = key\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Removing a non-existent section should be a no-op
	if err := removeAWSSharedCredentialsProfile(homeDir, "nonexistent"); err != nil {
		t.Fatalf("removeAWSSharedCredentialsProfile() error = %v", err)
	}

	// Verify the file was not modified
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(content), "[other]") {
		t.Fatalf("expected [other] section to remain, got %q", content)
	}
}

// --- upsertINISection with trailing blank lines after removal ---

func TestUpsertINISectionTrimsTrailingBlankAfterRemove(t *testing.T) {
	// Existing section with trailing blank lines should be trimmed before appending new.
	lines := []string{
		"[other]",
		"key = val",
		"",
		"[target]",
		"key = old",
		"",
		"",
	}
	newSection := []string{
		"[target]",
		"key = new",
	}

	result := upsertINISection(lines, "target", newSection)
	joined := strings.Join(result, "\n")
	if strings.Contains(joined, "old") {
		t.Fatalf("expected old content to be removed, got %q", joined)
	}
	if !strings.Contains(joined, "key = new") {
		t.Fatalf("expected new content, got %q", joined)
	}
	// Verify there isn't excessive blank lines between sections
	if strings.Contains(joined, "\n\n\n") {
		t.Fatalf("expected trailing blanks to be trimmed, got %q", joined)
	}
}

func TestCurrentUserAWSCredentialsPathIgnoresPCHome(t *testing.T) {
	t.Setenv(pcHomeEnvVar, t.TempDir())

	realHome := t.TempDir()
	original := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = original })
	userHomeDirFn = func() (string, error) {
		return realHome, nil
	}

	path, err := currentUserAWSCredentialsPath()
	if err != nil {
		t.Fatalf("currentUserAWSCredentialsPath() error = %v", err)
	}

	want := filepath.Join(realHome, ".aws", "credentials")
	if path != want {
		t.Fatalf("currentUserAWSCredentialsPath() = %q, want %q", path, want)
	}
}
