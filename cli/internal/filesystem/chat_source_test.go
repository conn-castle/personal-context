package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDeriveChatSourceKey(t *testing.T) {
	tests := []struct {
		name     string
		chatID   string
		path     string
		want     string
		wantErr  bool
		errMatch string
	}{
		{name: "jsonl", chatID: "20260315-abcdef12", path: "/tmp/codex/session.jsonl", want: "chats/raw/20260315-abcdef12/source.jsonl"},
		{name: "json", chatID: "20260315-abcdef12", path: "/tmp/claude_code/session.json", want: "chats/raw/20260315-abcdef12/source.json"},
		{name: "ndjson", chatID: "20260315-abcdef12", path: "session.ndjson", want: "chats/raw/20260315-abcdef12/source.ndjson"},
		{name: "uppercase ext", chatID: "20260315-abcdef12", path: "session.JSONL", want: "chats/raw/20260315-abcdef12/source.jsonl"},
		{name: "missing ext", chatID: "20260315-abcdef12", path: "session", wantErr: true, errMatch: "unsupported"},
		{name: "wrong ext", chatID: "20260315-abcdef12", path: "session.txt", wantErr: true, errMatch: "unsupported"},
		{name: "bad chat id", chatID: "bad", path: "session.jsonl", wantErr: true, errMatch: "chat session id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DeriveChatSourceKey(tc.chatID, tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error matching %q, got nil", tc.errMatch)
				}
				if tc.errMatch != "" && !strings.Contains(err.Error(), tc.errMatch) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("DeriveChatSourceKey() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateChatSourceKey(t *testing.T) {
	chatID := "20260315-abcdef12"
	tests := []struct {
		name    string
		key     string
		wantErr string
	}{
		{name: "valid jsonl", key: "chats/raw/20260315-abcdef12/source.jsonl"},
		{name: "valid json", key: "chats/raw/20260315-abcdef12/source.json"},
		{name: "valid ndjson", key: "chats/raw/20260315-abcdef12/source.ndjson"},
		{name: "wrong chat id", key: "chats/raw/20260315-99999999/source.jsonl", wantErr: "chat id"},
		{name: "wrong prefix", key: "data/raw/20260315-abcdef12/source.jsonl", wantErr: "chats/raw"},
		{name: "absolute", key: "/chats/raw/20260315-abcdef12/source.jsonl", wantErr: "relative"},
		{name: "traversal", key: "chats/raw/20260315-abcdef12/../source.jsonl", wantErr: "chats/raw/{chat_session_id}/source.{ext}"},
		{name: "wrong basename", key: "chats/raw/20260315-abcdef12/other.jsonl", wantErr: "source.{json|jsonl|ndjson}"},
		{name: "unsupported ext", key: "chats/raw/20260315-abcdef12/source.txt", wantErr: "source.{json|jsonl|ndjson}"},
		{name: "empty", key: "", wantErr: "required"},
		{name: "extra segment", key: "chats/raw/20260315-abcdef12/extra/source.jsonl", wantErr: "chats/raw/{chat_session_id}/source.{ext}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateChatSourceKey(chatID, tc.key)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error matching %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
	// An invalid chat session id must surface from validateChatSessionID
	// before the key shape checks run.
	if err := ValidateChatSourceKey("not-a-valid-id", "chats/raw/not-a-valid-id/source.json"); err == nil {
		t.Fatal("expected ValidateChatSourceKey to reject invalid chat session id")
	}
}

func TestChatSourceStageAndPromote(t *testing.T) {
	base := t.TempDir()
	client, err := NewClient(base)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	chatID := "20260315-abcdef12"
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "transcript.jsonl")
	if err := os.WriteFile(srcPath, []byte("{\"a\":1}\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	stage, err := client.CopyChatSourceToStage(chatID, srcPath)
	if err != nil {
		t.Fatalf("CopyChatSourceToStage() error = %v", err)
	}
	if stage.RawSourceKey != "chats/raw/"+chatID+"/source.jsonl" {
		t.Fatalf("unexpected raw_source_key: %s", stage.RawSourceKey)
	}
	if _, err := os.Stat(stage.StagedPath); err != nil {
		t.Fatalf("staged file missing: %v", err)
	}

	stored, err := client.PromoteChatSourceStage(stage)
	if err != nil {
		t.Fatalf("PromoteChatSourceStage() error = %v", err)
	}
	expectedPath := filepath.Join(base, "chats", "raw", chatID, "source.jsonl")
	if stored.Path != expectedPath {
		t.Fatalf("got %s, want %s", stored.Path, expectedPath)
	}
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("active file missing after promote: %v", err)
	}
	if _, err := os.Stat(stage.StagedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging dir should be gone after promote, got err = %v", err)
	}
}

func TestChatSourceRollsBackPreviousOnReimport(t *testing.T) {
	base := t.TempDir()
	client, err := NewClient(base)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	chatID := "20260315-abcdef12"

	// Place a pre-existing active file.
	activeDir := filepath.Join(base, "chats", "raw", chatID)
	if err := os.MkdirAll(activeDir, 0o700); err != nil {
		t.Fatalf("mkdir active: %v", err)
	}
	if err := os.WriteFile(filepath.Join(activeDir, "source.jsonl"), []byte("OLD"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}

	// Stage a replacement.
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "transcript.json")
	if err := os.WriteFile(srcPath, []byte("NEW"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	stage, err := client.CopyChatSourceToStage(chatID, srcPath)
	if err != nil {
		t.Fatalf("CopyChatSourceToStage() error = %v", err)
	}
	stored, err := client.PromoteChatSourceStage(stage)
	if err != nil {
		t.Fatalf("PromoteChatSourceStage() error = %v", err)
	}

	// Active file is the new content under the new extension.
	bytes, err := os.ReadFile(stored.Path)
	if err != nil {
		t.Fatalf("read promoted: %v", err)
	}
	if string(bytes) != "NEW" {
		t.Fatalf("content not replaced: %q", string(bytes))
	}
	// Old basename should not survive.
	if _, err := os.Stat(filepath.Join(activeDir, "source.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old extension file should be gone, err = %v", err)
	}
	// No leftover staging/backup dirs.
	entries, err := os.ReadDir(filepath.Join(base, "chats", "raw"))
	if err != nil {
		t.Fatalf("read raw dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") || strings.HasPrefix(e.Name(), ".backup-") {
			t.Fatalf("leftover work dir: %s", e.Name())
		}
	}
}

func TestPromoteChatSourceStageSyncsRawParentAfterPromote(t *testing.T) {
	base := t.TempDir()
	chatID := "20260315-abcdef12"
	rawDir := filepath.Join(base, "chats", "raw")
	var syncedDirs []string
	client, err := newClientWithHooks(base, fileOperationHooks{
		syncDir: func(dir string) error {
			syncedDirs = append(syncedDirs, dir)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("newClientWithHooks() error = %v", err)
	}
	source := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(source, []byte("NEW"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	stage, err := client.CopyChatSourceToStage(chatID, source)
	if err != nil {
		t.Fatalf("CopyChatSourceToStage() error = %v", err)
	}

	if _, err := client.PromoteChatSourceStage(stage); err != nil {
		t.Fatalf("PromoteChatSourceStage() error = %v", err)
	}

	if len(syncedDirs) != 1 || syncedDirs[0] != rawDir {
		t.Fatalf("synced dirs = %v, want [%s]", syncedDirs, rawDir)
	}
}

func TestPromoteChatSourceStageSyncDirFailureRollsBackBackup(t *testing.T) {
	base := t.TempDir()
	chatID := "20260315-abcdef12"
	syncCalls := 0
	client, err := newClientWithHooks(base, fileOperationHooks{
		syncDir: func(string) error {
			syncCalls++
			return errors.New("sync raw dir")
		},
	})
	if err != nil {
		t.Fatalf("newClientWithHooks() error = %v", err)
	}
	activeDir := filepath.Join(base, "chats", "raw", chatID)
	if err := os.MkdirAll(activeDir, 0o700); err != nil {
		t.Fatalf("mkdir active: %v", err)
	}
	activePath := filepath.Join(activeDir, "source.json")
	if err := os.WriteFile(activePath, []byte("OLD"), 0o600); err != nil {
		t.Fatalf("write active: %v", err)
	}
	source := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(source, []byte("NEW"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	stage, err := client.CopyChatSourceToStage(chatID, source)
	if err != nil {
		t.Fatalf("CopyChatSourceToStage() error = %v", err)
	}

	_, err = client.PromoteChatSourceStage(stage)
	if err == nil || !strings.Contains(err.Error(), "sync promoted chat raw parent") {
		t.Fatalf("expected sync error, got %v", err)
	}
	got, readErr := os.ReadFile(activePath)
	if readErr != nil {
		t.Fatalf("read restored active: %v", readErr)
	}
	if string(got) != "OLD" {
		t.Fatalf("expected backup rollback to restore old content, got %q", string(got))
	}
	if syncCalls != 2 {
		t.Fatalf("syncDir calls = %d, want 2 including rollback sync", syncCalls)
	}
}

func TestPromoteChatSourceStageSyncDirFailureWithoutBackup(t *testing.T) {
	base := t.TempDir()
	chatID := "20260315-abcdef12"
	client, err := newClientWithHooks(base, fileOperationHooks{
		syncDir: func(string) error {
			return errors.New("sync raw dir")
		},
	})
	if err != nil {
		t.Fatalf("newClientWithHooks() error = %v", err)
	}
	source := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(source, []byte("NEW"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	stage, err := client.CopyChatSourceToStage(chatID, source)
	if err != nil {
		t.Fatalf("CopyChatSourceToStage() error = %v", err)
	}

	_, err = client.PromoteChatSourceStage(stage)
	if err == nil || !strings.Contains(err.Error(), "sync promoted chat raw parent") {
		t.Fatalf("expected sync error, got %v", err)
	}
}

func TestRestoreChatRawBackupFailures(t *testing.T) {
	base := t.TempDir()
	rawDir := filepath.Join(base, "chats", "raw")
	activeDir := filepath.Join(rawDir, "20260315-abcdef12")
	backupDir := filepath.Join(rawDir, ".backup-20260315-abcdef12-test")

	t.Run("rename failure", func(t *testing.T) {
		err := restoreChatRawBackup(fileOperationHooks{
			renameFile: func(string, string) error {
				return errors.New("rename restore boom")
			},
			syncDir: func(string) error { return nil },
		}.withDefaults(), activeDir, backupDir)
		if err == nil || !strings.Contains(err.Error(), "restore previous chat raw source") {
			t.Fatalf("expected restore rename error, got %v", err)
		}
	})

	t.Run("sync failure", func(t *testing.T) {
		if err := os.MkdirAll(backupDir, 0o700); err != nil {
			t.Fatalf("mkdir backup: %v", err)
		}
		if err := os.WriteFile(filepath.Join(backupDir, "source.json"), []byte("OLD"), 0o600); err != nil {
			t.Fatalf("write backup: %v", err)
		}

		err := restoreChatRawBackup(fileOperationHooks{
			renameFile: os.Rename,
			syncDir: func(string) error {
				return errors.New("sync restore boom")
			},
		}.withDefaults(), activeDir, backupDir)
		if err == nil || !strings.Contains(err.Error(), "sync restored chat raw parent") {
			t.Fatalf("expected restore sync error, got %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(activeDir, "source.json")); statErr != nil {
			t.Fatalf("expected backup renamed to active before sync failure, stat err = %v", statErr)
		}
	})
}

func TestDeleteChatSource(t *testing.T) {
	base := t.TempDir()
	client, _ := NewClient(base)
	chatID := "20260315-abcdef12"
	dir := filepath.Join(base, "chats", "raw", chatID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source.jsonl"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := client.DeleteChatSource(chatID); err != nil {
		t.Fatalf("DeleteChatSource() error = %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected dir removed, got err = %v", err)
	}
	// Idempotent.
	if err := client.DeleteChatSource(chatID); err != nil {
		t.Fatalf("idempotent delete error: %v", err)
	}
}

func TestDeleteChatSourceSyncsRawParent(t *testing.T) {
	base := t.TempDir()
	var syncedDirs []string
	client, err := newClientWithHooks(base, fileOperationHooks{
		syncDir: func(dir string) error {
			syncedDirs = append(syncedDirs, dir)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("newClientWithHooks() error = %v", err)
	}
	chatID := "20260315-abcdef12"
	dir := filepath.Join(base, "chats", "raw", chatID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := client.DeleteChatSource(chatID); err != nil {
		t.Fatalf("DeleteChatSource() error = %v", err)
	}
	want := []string{filepath.Join(base, "chats", "raw")}
	if len(syncedDirs) != len(want) || syncedDirs[0] != want[0] {
		t.Fatalf("synced dirs = %v, want %v", syncedDirs, want)
	}
}

func TestDeleteChatSourceSyncFailure(t *testing.T) {
	base := t.TempDir()
	client, err := newClientWithHooks(base, fileOperationHooks{
		syncDir: func(string) error {
			return errors.New("sync boom")
		},
	})
	if err != nil {
		t.Fatalf("newClientWithHooks() error = %v", err)
	}
	chatID := "20260315-abcdef12"
	if err := os.MkdirAll(filepath.Join(base, "chats", "raw", chatID), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := client.DeleteChatSource(chatID); err == nil || !strings.Contains(err.Error(), "sync chat raw directory") {
		t.Fatalf("expected sync failure, got %v", err)
	}
}

// TestPromoteChatSourceStageMkdirAllError covers the MkdirAll error branch
// by pre-creating chats/raw as a regular file so MkdirAll for the parent
// fails.
func TestPromoteChatSourceStageMkdirAllError(t *testing.T) {
	base := t.TempDir()
	client, _ := NewClient(base)
	chatID := "20260315-abcdef12"
	chatsDir := filepath.Join(base, "chats")
	if err := os.MkdirAll(chatsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chatsDir, "raw"), []byte("blocker"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	stageDir := filepath.Join(base, "stage-dir")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatalf("mkdir stage: %v", err)
	}
	stagedPath := filepath.Join(stageDir, "source.json")
	if err := os.WriteFile(stagedPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write staged: %v", err)
	}
	if _, err := client.PromoteChatSourceStage(ChatSourceStage{
		ChatSessionID: chatID,
		RawSourceKey:  "chats/raw/" + chatID + "/source.json",
		StagedPath:    stagedPath,
	}); err == nil {
		t.Fatal("expected MkdirAll failure when chats/raw is a regular file")
	}
}

// TestPromoteChatSourceStageActiveDirStatError covers the !IsNotExist Stat
// error branch by making chats/raw/{chatID} unreadable to Stat (a directory
// whose parent has no x-bit means children can't be stat'd).
func TestPromoteChatSourceStageActiveDirStatError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	base := t.TempDir()
	client, _ := NewClient(base)
	chatID := "20260315-abcdef12"
	rawDir := filepath.Join(base, "chats", "raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	chatDir := filepath.Join(rawDir, chatID)
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatalf("mkdir chatDir: %v", err)
	}
	// Strip x-bit on chats/raw so Stat on chats/raw/{chatID} fails.
	if err := os.Chmod(rawDir, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(rawDir, 0o755) })

	stageDir := filepath.Join(base, "stage-dir")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatalf("mkdir stage: %v", err)
	}
	stagedPath := filepath.Join(stageDir, "source.json")
	if err := os.WriteFile(stagedPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write staged: %v", err)
	}
	if _, err := client.PromoteChatSourceStage(ChatSourceStage{
		ChatSessionID: chatID,
		RawSourceKey:  "chats/raw/" + chatID + "/source.json",
		StagedPath:    stagedPath,
	}); err == nil {
		t.Fatal("expected stat error to bubble up")
	}
}

func TestPromoteChatSourceStageBackupRenameFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permission-based backup rename failure is not reliable")
	}
	base := t.TempDir()
	client, _ := NewClient(base)
	chatID := "20260315-abcdef12"
	rawDir := filepath.Join(base, "chats", "raw")
	activeDir := filepath.Join(rawDir, chatID)
	if err := os.MkdirAll(activeDir, 0o700); err != nil {
		t.Fatalf("mkdir active: %v", err)
	}
	if err := os.WriteFile(filepath.Join(activeDir, "source.json"), []byte("OLD"), 0o600); err != nil {
		t.Fatalf("write active: %v", err)
	}
	// Put the stage under the managed chats/raw/.staging-* layout so it
	// passes the resolveManagedStageDir guard; rely on the chmod 0o500
	// on rawDir to make the active→backup rename inside rawDir fail.
	stageDir := filepath.Join(rawDir, ".staging-"+chatID)
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		t.Fatalf("mkdir stage: %v", err)
	}
	stagedPath := filepath.Join(stageDir, "source.json")
	if err := os.WriteFile(stagedPath, []byte("NEW"), 0o600); err != nil {
		t.Fatalf("write staged: %v", err)
	}
	if err := os.Chmod(rawDir, 0o500); err != nil {
		t.Fatalf("chmod raw: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(rawDir, 0o700) })

	_, err := client.PromoteChatSourceStage(ChatSourceStage{
		ChatSessionID: chatID,
		RawSourceKey:  "chats/raw/" + chatID + "/source.json",
		StagedPath:    stagedPath,
	})
	if err == nil || !strings.Contains(err.Error(), "backup previous chat raw source") {
		t.Fatalf("expected backup rename error, got %v", err)
	}
}

// TestCopyChatSourceToStageRejectsDirectorySource verifies that a directory
// supplied as the source path is rejected before any staging directory is
// created — chat sources must be files.
func TestCopyChatSourceToStageRejectsDirectorySource(t *testing.T) {
	base := t.TempDir()
	client, _ := NewClient(base)
	// Give the dir a transcript extension so DeriveChatSourceKey accepts
	// the path and we reach the IsDir() check.
	dirSrc := filepath.Join(t.TempDir(), "src-dir.json")
	if err := os.MkdirAll(dirSrc, 0o700); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if _, err := client.CopyChatSourceToStage("20260315-abcdef12", dirSrc); err == nil ||
		!strings.Contains(err.Error(), "chat source must be a file") {
		t.Fatalf("expected directory rejection, got %v", err)
	}
}

// TestPromoteChatSourceStageRenameFailureRollsBackBackup injects a
// stage→active Rename failure via the client rename hook and asserts the
// previously-backed-up active directory is restored.
func TestPromoteChatSourceStageRenameFailureRollsBackBackup(t *testing.T) {
	base := t.TempDir()
	chatID := "20260315-abcdef12"
	rawDir := filepath.Join(base, "chats", "raw")
	activeDir := filepath.Join(rawDir, chatID)
	stageDir := filepath.Join(rawDir, ".staging-"+chatID)
	var syncedDirs []string
	client, _ := newClientWithHooks(base, fileOperationHooks{
		renameFile: func(oldPath string, newPath string) error {
			if oldPath == stageDir && newPath == activeDir {
				return errors.New("simulated stage rename failure")
			}
			return os.Rename(oldPath, newPath)
		},
		syncDir: func(dir string) error {
			syncedDirs = append(syncedDirs, dir)
			return nil
		},
	})
	if err := os.MkdirAll(activeDir, 0o700); err != nil {
		t.Fatalf("mkdir active: %v", err)
	}
	activePath := filepath.Join(activeDir, "source.json")
	if err := os.WriteFile(activePath, []byte("OLD"), 0o600); err != nil {
		t.Fatalf("write active: %v", err)
	}
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		t.Fatalf("mkdir stage: %v", err)
	}
	stagedPath := filepath.Join(stageDir, "source.json")
	if err := os.WriteFile(stagedPath, []byte("NEW"), 0o600); err != nil {
		t.Fatalf("write staged: %v", err)
	}

	_, err := client.PromoteChatSourceStage(ChatSourceStage{
		ChatSessionID: chatID,
		RawSourceKey:  "chats/raw/" + chatID + "/source.json",
		StagedPath:    stagedPath,
	})
	if err == nil || !strings.Contains(err.Error(), "promote chat source stage") {
		t.Fatalf("expected promote failure, got %v", err)
	}
	// Backup must have been restored to activeDir with the original content.
	got, readErr := os.ReadFile(activePath)
	if readErr != nil {
		t.Fatalf("read restored active: %v", readErr)
	}
	if string(got) != "OLD" {
		t.Fatalf("expected backup rollback to restore old content, got %q", string(got))
	}
	if len(syncedDirs) != 1 || syncedDirs[0] != rawDir {
		t.Fatalf("synced dirs = %v, want rollback sync [%s]", syncedDirs, rawDir)
	}
}

func TestPromoteChatSourceStageRenameFailureReportsRollbackFailure(t *testing.T) {
	base := t.TempDir()
	chatID := "20260315-abcdef12"
	rawDir := filepath.Join(base, "chats", "raw")
	activeDir := filepath.Join(rawDir, chatID)
	stageDir := filepath.Join(rawDir, ".staging-"+chatID)
	client, _ := newClientWithHooks(base, fileOperationHooks{
		renameFile: func(oldPath string, newPath string) error {
			switch {
			case oldPath == activeDir && strings.Contains(filepath.Base(newPath), ".backup-"+chatID):
				return os.Rename(oldPath, newPath)
			case oldPath == stageDir && newPath == activeDir:
				return errors.New("simulated stage rename failure")
			case strings.Contains(filepath.Base(oldPath), ".backup-"+chatID) && newPath == activeDir:
				return errors.New("simulated rollback rename failure")
			default:
				return os.Rename(oldPath, newPath)
			}
		},
	})
	if err := os.MkdirAll(activeDir, 0o700); err != nil {
		t.Fatalf("mkdir active: %v", err)
	}
	if err := os.WriteFile(filepath.Join(activeDir, "source.json"), []byte("OLD"), 0o600); err != nil {
		t.Fatalf("write active: %v", err)
	}
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		t.Fatalf("mkdir stage: %v", err)
	}
	stagedPath := filepath.Join(stageDir, "source.json")
	if err := os.WriteFile(stagedPath, []byte("NEW"), 0o600); err != nil {
		t.Fatalf("write staged: %v", err)
	}

	_, err := client.PromoteChatSourceStage(ChatSourceStage{
		ChatSessionID: chatID,
		RawSourceKey:  "chats/raw/" + chatID + "/source.json",
		StagedPath:    stagedPath,
	})
	if err == nil || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("expected rollback failure, got %v", err)
	}
}

// TestPromoteChatSourceStageRejectsUnmanagedStagedPath verifies that the
// staging-path validator rejects anything that isn't a direct child of
// `<basePath>/chats/raw/.staging-*` before any RemoveAll/Rename runs. This
// is defense-in-depth so a malformed ChatSourceStage cannot trick us into
// deleting or moving unrelated directories.
func TestPromoteChatSourceStageRejectsUnmanagedStagedPath(t *testing.T) {
	base := t.TempDir()
	client, _ := NewClient(base)
	chatID := "20260315-abcdef12"

	cases := []struct {
		name     string
		stageDir string
	}{
		{name: "outside chats/raw", stageDir: filepath.Join(base, "stage-dir")},
		{name: "under chats/raw but missing .staging- prefix", stageDir: filepath.Join(base, "chats", "raw", "not-staging")},
		{name: "two levels deep under chats/raw", stageDir: filepath.Join(base, "chats", "raw", "sub", ".staging-"+chatID)},
		{name: "file in parent of chats/raw", stageDir: filepath.Join(base, "chats")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.MkdirAll(tc.stageDir, 0o700); err != nil {
				t.Fatalf("mkdir stage: %v", err)
			}
			stagedPath := filepath.Join(tc.stageDir, "source.json")
			if err := os.WriteFile(stagedPath, []byte("{}"), 0o600); err != nil {
				t.Fatalf("write staged: %v", err)
			}
			_, err := client.PromoteChatSourceStage(ChatSourceStage{
				ChatSessionID: chatID,
				RawSourceKey:  "chats/raw/" + chatID + "/source.json",
				StagedPath:    stagedPath,
			})
			if err == nil || !strings.Contains(err.Error(), "managed chats/raw/.staging-* directory") {
				t.Fatalf("expected validator rejection, got %v", err)
			}
		})
	}
}

func TestPromoteChatSourceStageVerifyFailure(t *testing.T) {
	base := t.TempDir()
	chatID := "20260315-abcdef12"
	rawDir := filepath.Join(base, "chats", "raw")
	var syncedDirs []string
	client, _ := newClientWithHooks(base, fileOperationHooks{
		syncDir: func(dir string) error {
			syncedDirs = append(syncedDirs, dir)
			return nil
		},
	})
	// Pre-create active so the backup rollback path also runs.
	activeDir := filepath.Join(rawDir, chatID)
	if err := os.MkdirAll(activeDir, 0o700); err != nil {
		t.Fatalf("mkdir active: %v", err)
	}
	activePath := filepath.Join(activeDir, "source.json")
	if err := os.WriteFile(activePath, []byte("OLD"), 0o600); err != nil {
		t.Fatalf("write active: %v", err)
	}
	stageDir := filepath.Join(base, "chats", "raw", ".staging-"+chatID)
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		t.Fatalf("mkdir stage: %v", err)
	}
	// File exists at the staged path (so stat passes the early check) but
	// its basename doesn't match the RawSourceKey, so after Rename the
	// verify stat on activePath fails.
	stagedPath := filepath.Join(stageDir, "wrong-name.json")
	if err := os.WriteFile(stagedPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write staged: %v", err)
	}

	_, err := client.PromoteChatSourceStage(ChatSourceStage{
		ChatSessionID: chatID,
		RawSourceKey:  "chats/raw/" + chatID + "/source.json",
		StagedPath:    stagedPath,
	})
	if err == nil || !strings.Contains(err.Error(), "verify promoted chat source") {
		t.Fatalf("expected verify failure, got %v", err)
	}
	// Verify the backup was restored: the original "OLD" content must
	// still be present at activePath after rollback.
	got, readErr := os.ReadFile(activePath)
	if readErr != nil {
		t.Fatalf("read restored active: %v", readErr)
	}
	if string(got) != "OLD" {
		t.Fatalf("expected backup rollback to restore old content, got %q", string(got))
	}
	want := []string{rawDir, rawDir}
	if len(syncedDirs) != len(want) {
		t.Fatalf("synced dirs = %v, want %v", syncedDirs, want)
	}
	for i := range want {
		if syncedDirs[i] != want[i] {
			t.Fatalf("synced dirs = %v, want %v", syncedDirs, want)
		}
	}
}

func TestPromoteChatSourceStageBackupCleanupFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permission-based backup cleanup failure is not reliable")
	}
	base := t.TempDir()
	client, _ := NewClient(base)
	chatID := "20260315-abcdef12"
	activeDir := filepath.Join(base, "chats", "raw", chatID)
	lockedDir := filepath.Join(activeDir, "locked")
	if err := os.MkdirAll(lockedDir, 0o700); err != nil {
		t.Fatalf("mkdir locked: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lockedDir, "child"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write locked child: %v", err)
	}
	if err := os.Chmod(lockedDir, 0o000); err != nil {
		t.Fatalf("chmod locked: %v", err)
	}
	t.Cleanup(func() {
		matches, _ := filepath.Glob(filepath.Join(base, "chats", "raw", ".backup-"+chatID+"-*"))
		for _, match := range matches {
			_ = os.Chmod(filepath.Join(match, "locked"), 0o700)
			_ = os.RemoveAll(match)
		}
	})

	source := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(source, []byte("NEW"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	stage, err := client.CopyChatSourceToStage(chatID, source)
	if err != nil {
		t.Fatalf("CopyChatSourceToStage() error = %v", err)
	}
	_, err = client.PromoteChatSourceStage(stage)
	if err == nil || !strings.Contains(err.Error(), "clean up previous chat raw backup") {
		t.Fatalf("expected backup cleanup error, got %v", err)
	}
}

func TestDeleteChatSourceNilAndInvalid(t *testing.T) {
	var nilClient *Client
	if err := nilClient.DeleteChatSource("20260315-abcdef12"); err == nil {
		t.Fatal("expected nil-client error")
	}
	base := t.TempDir()
	client, _ := NewClient(base)
	if err := client.DeleteChatSource(""); err == nil {
		t.Fatal("expected error for empty chat session id")
	}
	if err := client.DeleteChatSource("not-an-id"); err == nil {
		t.Fatal("expected error for invalid chat session id pattern")
	}
}

func TestDeleteChatSourceRemoveFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permission-based remove failure is not reliable")
	}
	base := t.TempDir()
	client, _ := NewClient(base)
	chatID := "20260315-abcdef12"
	rawDir := filepath.Join(base, "chats", "raw")
	chatDir := filepath.Join(rawDir, chatID)
	if err := os.MkdirAll(chatDir, 0o700); err != nil {
		t.Fatalf("mkdir chat dir: %v", err)
	}
	if err := os.Chmod(rawDir, 0o500); err != nil {
		t.Fatalf("chmod raw: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(rawDir, 0o700) })
	if err := client.DeleteChatSource(chatID); err == nil || !strings.Contains(err.Error(), "remove chat raw source dir") {
		t.Fatalf("expected remove failure, got %v", err)
	}
}

func TestCopyChatSourceToStageInputRejections(t *testing.T) {
	base := t.TempDir()
	client, _ := NewClient(base)

	// Nil client
	var nilClient *Client
	if _, err := nilClient.CopyChatSourceToStage("20260315-abcdef12", "/tmp/x.json"); err == nil {
		t.Fatal("expected nil-client error")
	}

	// Invalid id
	if _, err := client.CopyChatSourceToStage("bad", "/tmp/x.json"); err == nil {
		t.Fatal("expected invalid id error")
	}

	// Empty source path
	if _, err := client.CopyChatSourceToStage("20260315-abcdef12", "   "); err == nil {
		t.Fatal("expected empty source path error")
	}

	// Unsupported extension
	dir := t.TempDir()
	bad := filepath.Join(dir, "transcript.xml")
	_ = os.WriteFile(bad, []byte("x"), 0o600)
	if _, err := client.CopyChatSourceToStage("20260315-abcdef12", bad); err == nil {
		t.Fatal("expected unsupported extension error")
	}

	// Source is a directory
	bdir := filepath.Join(dir, "asDir.jsonl")
	_ = os.Mkdir(bdir, 0o755)
	if _, err := client.CopyChatSourceToStage("20260315-abcdef12", bdir); err == nil {
		t.Fatal("expected directory rejection")
	}

	// Source path does not exist
	if _, err := client.CopyChatSourceToStage("20260315-abcdef12", filepath.Join(dir, "missing.jsonl")); err == nil {
		t.Fatal("expected stat error")
	}
}

func TestCopyChatSourceToStageFileOperationFailures(t *testing.T) {
	t.Run("create stage dir", func(t *testing.T) {
		base := t.TempDir()
		if err := os.MkdirAll(filepath.Join(base, "chats"), 0o755); err != nil {
			t.Fatalf("mkdir chats: %v", err)
		}
		if err := os.WriteFile(filepath.Join(base, "chats", "raw"), []byte("blocker"), 0o644); err != nil {
			t.Fatalf("write raw blocker: %v", err)
		}
		client, _ := NewClient(base)
		source := filepath.Join(t.TempDir(), "session.jsonl")
		if err := os.WriteFile(source, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write source: %v", err)
		}
		if _, err := client.CopyChatSourceToStage("20260315-abcdef12", source); err == nil || !strings.Contains(err.Error(), "create chat source stage dir") {
			t.Fatalf("expected stage dir error, got %v", err)
		}
	})

	t.Run("open source", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Getuid() == 0 {
			t.Skip("permission-based source open failure is not reliable")
		}
		client, _ := NewClient(t.TempDir())
		source := filepath.Join(t.TempDir(), "session.jsonl")
		if err := os.WriteFile(source, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write source: %v", err)
		}
		if err := os.Chmod(source, 0o000); err != nil {
			t.Fatalf("chmod source: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(source, 0o600) })
		if _, err := client.CopyChatSourceToStage("20260315-abcdef12", source); err == nil || !strings.Contains(err.Error(), "open chat source") {
			t.Fatalf("expected open source error, got %v", err)
		}
	})

	t.Run("sync staged file", func(t *testing.T) {
		client, _ := newClientWithHooks(t.TempDir(), fileOperationHooks{
			syncFile: func(*os.File) error { return errors.New("sync boom") },
		})
		source := filepath.Join(t.TempDir(), "session.json")
		if err := os.WriteFile(source, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write source: %v", err)
		}
		if _, err := client.CopyChatSourceToStage("20260315-abcdef12", source); err == nil || !strings.Contains(err.Error(), "sync staged chat source") {
			t.Fatalf("expected sync error, got %v", err)
		}
	})

	t.Run("close staged file", func(t *testing.T) {
		client, _ := newClientWithHooks(t.TempDir(), fileOperationHooks{
			closeFile: func(*os.File) error { return errors.New("close boom") },
		})
		source := filepath.Join(t.TempDir(), "session.json")
		if err := os.WriteFile(source, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write source: %v", err)
		}
		if _, err := client.CopyChatSourceToStage("20260315-abcdef12", source); err == nil || !strings.Contains(err.Error(), "close staged chat source") {
			t.Fatalf("expected close error, got %v", err)
		}
	})
}

func TestPromoteChatSourceStageRejectsInvalidStages(t *testing.T) {
	base := t.TempDir()
	client, _ := NewClient(base)

	// Nil client
	var nilClient *Client
	if _, err := nilClient.PromoteChatSourceStage(ChatSourceStage{}); err == nil {
		t.Fatal("expected nil-client error")
	}

	// Invalid key in stage
	if _, err := client.PromoteChatSourceStage(ChatSourceStage{ChatSessionID: "20260315-abcdef12", RawSourceKey: "wrong"}); err == nil {
		t.Fatal("expected key validation error")
	}

	// Missing staged path
	if _, err := client.PromoteChatSourceStage(ChatSourceStage{ChatSessionID: "20260315-abcdef12", RawSourceKey: "chats/raw/20260315-abcdef12/source.json"}); err == nil {
		t.Fatal("expected missing-staged-path error")
	}

	// Staged path does not exist on disk
	bogus := filepath.Join(t.TempDir(), "missing", "source.json")
	if _, err := client.PromoteChatSourceStage(ChatSourceStage{
		ChatSessionID: "20260315-abcdef12",
		RawSourceKey:  "chats/raw/20260315-abcdef12/source.json",
		StagedPath:    bogus,
	}); err == nil {
		t.Fatal("expected stat error for missing staged file")
	}
}

func TestDeleteChatSourceStage(t *testing.T) {
	base := t.TempDir()
	client, _ := NewClient(base)
	chatID := "20260315-abcdef12"
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "transcript.jsonl")
	if err := os.WriteFile(srcPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}
	stage, err := client.CopyChatSourceToStage(chatID, srcPath)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	stageDir := filepath.Dir(stage.StagedPath)
	if err := client.DeleteChatSourceStage(stage); err != nil {
		t.Fatalf("DeleteChatSourceStage: %v", err)
	}
	if _, err := os.Stat(stageDir); !os.IsNotExist(err) {
		t.Fatalf("expected staging dir removed, stat err = %v", err)
	}
	// Idempotent: deleting again must not error.
	if err := client.DeleteChatSourceStage(stage); err != nil {
		t.Fatalf("idempotent DeleteChatSourceStage: %v", err)
	}
	// Empty staged path is a no-op.
	if err := client.DeleteChatSourceStage(ChatSourceStage{}); err != nil {
		t.Fatalf("empty stage: %v", err)
	}
	// Nil client is a no-op.
	var nilClient *Client
	if err := nilClient.DeleteChatSourceStage(stage); err != nil {
		t.Fatalf("nil client: %v", err)
	}
	if err := client.DeleteChatSourceStage(ChatSourceStage{StagedPath: "bad\x00path/source.json"}); err == nil {
		t.Fatal("expected invalid stage path cleanup error")
	}
	// Path outside the managed chats/raw/.staging-* layout must be
	// rejected by the validator before any RemoveAll runs.
	if err := client.DeleteChatSourceStage(ChatSourceStage{StagedPath: filepath.Join(base, "other-dir", "source.json")}); err == nil ||
		!strings.Contains(err.Error(), "managed chats/raw/.staging-* directory") {
		t.Fatalf("expected validator rejection for outside-of-managed path, got %v", err)
	}
}

func TestDeleteChatSourceStageRemoveFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permission-based stage cleanup failure is not reliable")
	}
	base := t.TempDir()
	client, _ := NewClient(base)
	chatID := "20260315-abcdef12"
	rawDir := filepath.Join(base, "chats", "raw")
	stageDir := filepath.Join(rawDir, ".staging-"+chatID+"-locked")
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		t.Fatalf("mkdir stage: %v", err)
	}
	stagedPath := filepath.Join(stageDir, "source.json")
	if err := os.WriteFile(stagedPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write staged: %v", err)
	}
	if err := os.Chmod(rawDir, 0o500); err != nil {
		t.Fatalf("chmod raw: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(rawDir, 0o700)
		_ = os.RemoveAll(stageDir)
	})

	err := client.DeleteChatSourceStage(ChatSourceStage{StagedPath: stagedPath})
	if err == nil || !strings.Contains(err.Error(), "remove chat source stage dir") {
		t.Fatalf("expected remove failure, got %v", err)
	}
}

func TestListChatSessionIDsOnDisk(t *testing.T) {
	base := t.TempDir()
	client, _ := NewClient(base)
	// Empty base path → nil result without error.
	ids, err := client.ListChatSessionIDsOnDisk()
	if err != nil {
		t.Fatalf("empty list: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty list, got %v", ids)
	}
	// Populate one valid id, one .staging-*, one .backup-*, and one invalid.
	for _, name := range []string{"20260315-abcdef12", ".staging-20260315-abcdef12-aa", ".backup-20260315-abcdef12-bb", "not-a-valid-id"} {
		if err := os.MkdirAll(filepath.Join(base, "chats", "raw", name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	// Also drop a non-directory file to exercise the !IsDir skip.
	if err := os.WriteFile(filepath.Join(base, "chats", "raw", "stray-file"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}
	ids, err = client.ListChatSessionIDsOnDisk()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ids) != 1 || ids[0] != "20260315-abcdef12" {
		t.Fatalf("expected single valid id, got %v", ids)
	}
	var nilClient *Client
	if _, err := nilClient.ListChatSessionIDsOnDisk(); err == nil {
		t.Fatal("expected nil-client error")
	}
}

func TestListChatSessionIDsOnDiskErrorsWhenRawPathIsFile(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "chats"), 0o755); err != nil {
		t.Fatalf("mkdir chats: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "chats", "raw"), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write raw file: %v", err)
	}
	client, _ := NewClient(base)
	if _, err := client.ListChatSessionIDsOnDisk(); err == nil || !strings.Contains(err.Error(), "list chat raw directories") {
		t.Fatalf("expected list chat raw directories error, got %v", err)
	}
}

func TestResolveChatSourcePath(t *testing.T) {
	base := t.TempDir()
	client, _ := NewClient(base)
	chatID := "20260315-abcdef12"
	path, err := client.ResolveChatSourcePath(chatID, "chats/raw/"+chatID+"/source.jsonl")
	if err != nil {
		t.Fatalf("ResolveChatSourcePath() error = %v", err)
	}
	want := filepath.Join(base, "chats", "raw", chatID, "source.jsonl")
	if path != want {
		t.Fatalf("got %s, want %s", path, want)
	}
	if _, err := client.ResolveChatSourcePath(chatID, "../etc/passwd"); err == nil {
		t.Fatalf("expected error for traversal key")
	}
	var nilClient *Client
	if _, err := nilClient.ResolveChatSourcePath(chatID, "chats/raw/"+chatID+"/source.jsonl"); err == nil {
		t.Fatalf("expected nil-client error")
	}
}
