package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chatTranscript is the canonical jsonl transcript used by chat e2e tests.
const chatTranscriptBody = `{
  "id": "e2e-chat",
  "cwd": "/tmp/e2e-chat",
  "title": "E2E chat",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [{"role": "user", "content": "hello e2e"}]
}`

func setupChatHome(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	runPCSuccess(t, homeDir, "setup")
	runPCSuccess(t, homeDir, "device", "register", "test-device")
	return homeDir
}

// TestChatImportCreatesManagedRawSourceLocally checks that a successful
// `pc chat import` materializes chats/raw/{id}/source.json under PC_HOME.
func TestChatImportCreatesManagedRawSourceLocally(t *testing.T) {
	homeDir := setupChatHome(t)
	root := t.TempDir()
	src := filepath.Join(root, "session.json")
	if err := os.WriteFile(src, []byte(chatTranscriptBody), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	stdout := runPCSuccess(t, homeDir, "chat", "import", "--device", "test-device", "--agent", "codex", "--root", root)
	var summary struct {
		SessionsCreated  int `json:"sessions_created"`
		RawSourcesCopied int `json:"raw_sources_copied"`
	}
	if err := json.Unmarshal([]byte(stdout), &summary); err != nil {
		t.Fatalf("parse import summary: %v\n%s", err, stdout)
	}
	if summary.RawSourcesCopied != 1 {
		t.Fatalf("expected raw_sources_copied=1, got %d", summary.RawSourcesCopied)
	}

	idsOut := runPCSuccess(t, homeDir, "chat", "list", "--format", "ids")
	chatID := strings.TrimSpace(idsOut)
	rawPath := filepath.Join(homeDir, "personal-context", "chats", "raw", chatID, "source.json")
	if _, err := os.Stat(rawPath); err != nil {
		t.Fatalf("expected managed raw source at %s, stat err=%v", rawPath, err)
	}
	got, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if string(got) != chatTranscriptBody {
		t.Fatalf("managed raw content differs from imported transcript")
	}
}

// TestChatImportDeleteSourceRemovesOriginalAfterCopy verifies the
// `--delete-source` flag deletes only the imported transcript file after the
// managed copy succeeds.
func TestChatImportDeleteSourceRemovesOriginalAfterCopy(t *testing.T) {
	homeDir := setupChatHome(t)
	root := t.TempDir()
	src := filepath.Join(root, "session.json")
	if err := os.WriteFile(src, []byte(chatTranscriptBody), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	stdout := runPCSuccess(t, homeDir, "chat", "import",
		"--device", "test-device", "--agent", "codex",
		"--root", root, "--delete-source",
	)
	var summary struct {
		RawSourcesCopied int `json:"raw_sources_copied"`
		SourcesDeleted   int `json:"sources_deleted"`
	}
	if err := json.Unmarshal([]byte(stdout), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout)
	}
	if summary.SourcesDeleted != 1 {
		t.Fatalf("expected sources_deleted=1, got %d", summary.SourcesDeleted)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected original transcript to be deleted, stat err=%v", err)
	}
}

// TestChatImportDeleteSourceFailureKeepsImport verifies that when the
// original-file deletion fails after a successful managed copy and DB write,
// the PC-owned import remains committed and only a warning is surfaced.
func TestChatImportDeleteSourceFailureKeepsImport(t *testing.T) {
	homeDir := setupChatHome(t)
	root := t.TempDir()
	src := filepath.Join(root, "session.json")
	if err := os.WriteFile(src, []byte(chatTranscriptBody), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	// Make the parent directory read-only so os.Remove on the transcript fails
	// with EACCES after PC has already copied the bytes.
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	result := runPC(t, homeDir, "chat", "import",
		"--device", "test-device", "--agent", "codex",
		"--root", root, "--delete-source",
	)
	if result.ExitCode != 0 {
		t.Fatalf("expected success-with-warning, got exit %d\nstderr: %s\nstdout: %s",
			result.ExitCode, result.Stderr, result.Stdout)
	}
	var summary struct {
		SessionsCreated      int      `json:"sessions_created"`
		RawSourcesCopied     int      `json:"raw_sources_copied"`
		SourcesDeleted       int      `json:"sources_deleted"`
		SourceDeleteWarnings []string `json:"source_delete_warnings"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, result.Stdout)
	}
	if summary.RawSourcesCopied != 1 {
		t.Fatalf("expected raw_sources_copied=1, got %d", summary.RawSourcesCopied)
	}
	if summary.SourcesDeleted != 0 {
		t.Fatalf("expected sources_deleted=0 when deletion fails, got %d", summary.SourcesDeleted)
	}
	if len(summary.SourceDeleteWarnings) == 0 {
		t.Fatalf("expected a source_delete_warnings entry, got none")
	}
	if !strings.Contains(result.Stderr, "warning: failed to delete source after import") {
		t.Fatalf("expected stderr warning, got %q", result.Stderr)
	}
	// Managed copy must still exist after a deletion-failure scenario.
	idsOut := runPCSuccess(t, homeDir, "chat", "list", "--format", "ids")
	chatID := strings.TrimSpace(idsOut)
	rawPath := filepath.Join(homeDir, "personal-context", "chats", "raw", chatID, "source.json")
	if _, err := os.Stat(rawPath); err != nil {
		t.Fatalf("expected managed copy to survive deletion-failure, got stat err=%v", err)
	}
}
