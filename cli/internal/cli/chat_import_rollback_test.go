package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/syncengine"
)

// readSyncVersionUnit returns the current sync_version value from the local DB.
func readSyncVersionUnit(t *testing.T, homeDir string) int64 {
	t.Helper()
	dbPath := filepath.Join(homeDir, "personal-context", ".pc", "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	var version int64
	if err := db.QueryRow("SELECT version FROM sync_version WHERE id = 1").Scan(&version); err != nil {
		t.Fatalf("read sync_version: %v", err)
	}
	return version
}

// writeTestChatTranscript writes a json transcript at root/<name>.json and
// returns the path.
func writeTestChatTranscript(t *testing.T, dir string, name string, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write transcript %s: %v", p, err)
	}
	return p
}

const importTranscriptBody = `{
  "id": "rollback-session",
  "cwd": "/tmp/rollback",
  "title": "Rollback test",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [{"role": "user", "content": "hi"}]
}`

func runImportFor(t *testing.T, root string) error {
	t.Helper()
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	return cmd.Execute()
}

// TestChatImportReImportBumpsSyncVersionForSameKey covers the modified
// re-import case: unchanged-bytes re-imports are skipped before writes, while
// changed content that produces the same raw_source_key still bumps sync state
// so cloud push uploads the new bytes.
func TestChatImportReImportBumpsSyncVersionForSameKey(t *testing.T) {
	homeDir := setupEnv(t)
	root := t.TempDir()
	writeTestChatTranscript(t, root, "session.json", importTranscriptBody)

	if err := runImportFor(t, root); err != nil {
		t.Fatalf("first import: %v", err)
	}
	v1 := readSyncVersionUnit(t, homeDir)

	// Re-import the same chat: same raw_source_key (.json), but content changed.
	updated := strings.Replace(importTranscriptBody, "hi", "hi again", 1)
	writeTestChatTranscript(t, root, "session.json", updated)

	if err := runImportFor(t, root); err != nil {
		t.Fatalf("re-import: %v", err)
	}
	v2 := readSyncVersionUnit(t, homeDir)

	if v2 <= v1 {
		t.Fatalf("expected sync_version to bump on same-key re-import (was %d, now %d)", v1, v2)
	}
}

// TestChatImportRollsBackOnDBFailureKeepsPreviousManagedRaw seeds a successful
// import, drops the chat_session table to force a second-import DB upsert
// failure, and verifies the previous PC-owned raw source remains untouched.
func TestChatImportRollsBackOnDBFailureKeepsPreviousManagedRaw(t *testing.T) {
	homeDir := setupEnv(t)
	root := t.TempDir()
	writeTestChatTranscript(t, root, "session.json", importTranscriptBody)
	if err := runImportFor(t, root); err != nil {
		t.Fatalf("first import: %v", err)
	}
	// Find managed raw path via list.
	listOut := &bytes.Buffer{}
	listCmd := NewRootCommand(RootCommandOptions{Stdout: listOut, Stderr: &bytes.Buffer{}})
	listCmd.SetArgs([]string{"chat", "list", "--format", "ids"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("chat list: %v", err)
	}
	chatID := strings.TrimSpace(listOut.String())
	rawPath := filepath.Join(homeDir, "personal-context", "chats", "raw", chatID, "source.json")
	originalBytes, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read original raw: %v", err)
	}

	// Corrupt chat_session so the next upsert fails.
	corruptTable(t, homeDir, "chat_session")

	updated := strings.Replace(importTranscriptBody, "hi", "BROKEN", 1)
	writeTestChatTranscript(t, root, "session.json", updated)

	if err := runImportFor(t, root); err == nil {
		t.Fatal("expected chat import to fail after DB corruption")
	}

	// Previous PC-owned raw source must still exist with the original bytes.
	got, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read raw after rollback: %v", err)
	}
	if string(got) != string(originalBytes) {
		t.Fatalf("expected previous managed raw to be preserved after DB failure; got %q", got)
	}
}

// TestChatImportCleansUpStagingOnDBFailure verifies no .staging-* directories
// linger after a DB upsert failure aborts the import.
func TestChatImportCleansUpStagingOnDBFailure(t *testing.T) {
	homeDir := setupEnv(t)
	root := t.TempDir()
	writeTestChatTranscript(t, root, "session.json", importTranscriptBody)

	// Corrupt before any import so the very first upsert fails.
	corruptTable(t, homeDir, "chat_session")

	if err := runImportFor(t, root); err == nil {
		t.Fatal("expected chat import to fail")
	}

	rawDir := filepath.Join(homeDir, "personal-context", "chats", "raw")
	entries, err := os.ReadDir(rawDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read chats/raw: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			t.Fatalf("expected no leftover staging dir, found %s", e.Name())
		}
	}
}

func TestChatImportRebuildsFTSAfterCommittedBatchThenParseError(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	for i := range chatImportBatchSize {
		body := fmt.Sprintf(`{
  "id": "committed-before-error-%02d",
  "cwd": "/tmp/committed-before-error",
  "title": "Committed before error",
  "started_at": "2026-05-14T12:%02d:00Z",
  "messages": [{"role": "user", "content": "rescue-needle-%02d"}]
}`, i, i%60, i)
		writeTestChatTranscript(t, root, fmt.Sprintf("%03d-valid.json", i), body)
	}
	writeTestChatTranscript(t, root, "zzz-broken.json", "{not json")

	err := runImportFor(t, root)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error after committed batch, got %v", err)
	}

	stack, err := openLocalStackFromHome()
	if err != nil {
		t.Fatalf("open local stack: %v", err)
	}
	defer func() { _ = stack.Close() }()
	results, err := stack.Repo.SearchChatItems(context.Background(), repository.SearchChatItemsFilter{Query: "rescue-needle-00"})
	if err != nil {
		t.Fatalf("search committed import: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected committed batch to be searchable after failed import cleanup")
	}
}

func TestChatImportFailsWhileLocalSyncLockHeld(t *testing.T) {
	homeDir := setupEnv(t)
	root := t.TempDir()
	writeTestChatTranscript(t, root, "session.json", importTranscriptBody)

	lock, err := syncengine.AcquireFileLock(filepath.Join(basePath(homeDir), ".pc", "sync.lock"))
	if err != nil {
		t.Fatalf("acquire sync lock: %v", err)
	}
	defer func() { _ = lock.Release() }()

	err = runImportFor(t, root)
	if err == nil {
		t.Fatal("expected chat import to fail while sync lock is held")
	}
	if !strings.Contains(err.Error(), "acquire local sync lock for chat import") {
		t.Fatalf("expected chat import lock error, got %v", err)
	}
}

func TestChatImportFailsWhenChatFTSTableMissing(t *testing.T) {
	homeDir := setupEnv(t)
	root := t.TempDir()
	writeTestChatTranscript(t, root, "session.json", importTranscriptBody)

	db := openTestDBInternal(t, homeDir)
	if _, err := db.Exec(`
DROP TRIGGER IF EXISTS chat_item_fts_after_insert;
DROP TRIGGER IF EXISTS chat_item_fts_after_update;
DROP TRIGGER IF EXISTS chat_item_fts_after_delete;
DROP TABLE chat_item_fts;
`); err != nil {
		t.Fatalf("drop chat_item_fts table: %v", err)
	}
	_ = db.Close()

	err := runImportFor(t, root)
	if err == nil {
		t.Fatal("expected chat import to fail when chat_item_fts is missing")
	}
	if !strings.Contains(err.Error(), "chat_item_fts") {
		t.Fatalf("expected error to name chat_item_fts, got %v", err)
	}
}

// TestChatImportAutoSyncWarningSurfacesOnStderr injects an auto-sync hook
// that emits a warning and verifies the warning reaches the user via stderr,
// matching the existing record-mutation auto-sync convention.
func TestChatImportAutoSyncWarningSurfacesOnStderr(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	writeTestChatTranscript(t, root, "session.json", importTranscriptBody)

	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	calls := 0
	runAutoSyncFn = func(_ context.Context, stderr io.Writer) error {
		calls++
		_, _ = io.WriteString(stderr, "warning: auto-sync failed: boom\n")
		return nil
	}

	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: stderr})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one auto-sync call, got %d", calls)
	}
	if !strings.Contains(stderr.String(), "warning: auto-sync failed") {
		t.Fatalf("expected auto-sync warning on stderr, got %q", stderr.String())
	}
}

// TestChatDeleteAutoSyncWarning ensures chat delete also runs auto-sync after
// the local mutation and surfaces any warning from the hook.
func TestChatDeleteAutoSyncWarning(t *testing.T) {
	homeDir := setupEnv(t)
	_, _ = importTrashableChatHelper(t, homeDir)

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "list", "--format", "ids"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	chatID := strings.TrimSpace(stdout.String())

	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	runAutoSyncFn = func(_ context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, "warning: auto-sync failed for delete\n")
		return nil
	}
	stderr := &bytes.Buffer{}
	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: stderr})
	cmd.SetArgs([]string{"chat", "delete", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat delete: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: auto-sync failed for delete") {
		t.Fatalf("expected auto-sync warning, got %q", stderr.String())
	}
}

// importTrashableChatHelper imports one chat and returns its id and managed
// raw path. Shared between rollback and lifecycle tests so they do not
// duplicate the harness.
func importTrashableChatHelper(t *testing.T, homeDir string) (string, string) {
	t.Helper()
	return importTrashableChat(t, homeDir)
}

// TestChatRestoreNonexistentChatFails covers the missing-chat error branch
// in newChatRestoreCommand.
func TestChatRestoreNonexistentChatFails(t *testing.T) {
	setupEnv(t)
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "restore", "20250101-deadbeef"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected restore of missing chat to fail")
	}
}

// TestChatRestoreDBFailure injects a DB failure to cover the wrapping error
// branch in newChatRestoreCommand (not the missing-chat one).
func TestChatRestoreDBFailure(t *testing.T) {
	homeDir := setupEnv(t)
	corruptTable(t, homeDir, "chat_session")
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "restore", "20250101-deadbeef"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected DB failure to surface")
	}
}

// TestChatListAndSearchAndShowDBFailures injects chat_item / chat_session
// corruption to exercise the repository-error wrapping branches in
// runChatList, runChatSearch, and runChatShow.
func TestChatListAndSearchAndShowDBFailures(t *testing.T) {
	homeDir := setupEnv(t)
	corruptTable(t, homeDir, "chat_session")
	for _, args := range [][]string{
		{"chat", "list", "--format", "json"},
		{"chat", "search", "needle"},
		{"chat", "show", "20250101-deadbeef"},
		{"chat", "show", "--source-session-id", "some-source-id"},
	} {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected %v to fail after chat_session corruption", args)
		}
	}
}

func TestChatImportRollbackRestoreDeletesNewSession(t *testing.T) {
	ctx := context.Background()
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack: %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })

	chatID := "20260315-aaaabbbb"
	rawKey := "chats/raw/" + chatID + "/source.json"
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	if _, _, err := stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
		CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:              chatID,
			Source:          "codex",
			SourceSessionID: "new-session",
			SourceDeviceID:  "test-device",
			StartedAt:       now,
			LastActivityAt:  now,
			RawSourceKey:    &rawKey,
			CreatedAt:       &now,
			UpdatedAt:       &now,
		},
		ClearDeleted: true,
	}); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	rawPath, err := stack.FS.ResolveChatSourcePath(chatID, rawKey)
	if err != nil {
		t.Fatalf("ResolveChatSourcePath: %v", err)
	}
	if err := writeTextFileAtomically(rawPath, []byte(`{"id":"new-session"}`), 0o700, 0o600); err != nil {
		t.Fatalf("write raw source: %v", err)
	}

	chatImportRollbackState{session: repository.ChatSession{ID: chatID}}.restore(ctx, stack)

	if _, err := stack.Repo.GetChatSessionByID(ctx, chatID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected session to be deleted, got err %v", err)
	}
	if _, err := os.Stat(rawPath); !os.IsNotExist(err) {
		t.Fatalf("expected raw source to be deleted, stat err = %v", err)
	}
}

func TestChatImportRollbackRestoreReinstatesExistingSession(t *testing.T) {
	ctx := context.Background()
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack: %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })

	chatID := "20260315-ccccdddd"
	startedAt := time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC)
	updatedAt := startedAt.Add(time.Minute)
	oldTitle := strPtr("old title")
	stored, _, err := stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
		CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:              chatID,
			Source:          "codex",
			SourceSessionID: "existing-session",
			SourceDeviceID:  "test-device",
			Title:           oldTitle,
			StartedAt:       startedAt,
			LastActivityAt:  updatedAt,
			CreatedAt:       &startedAt,
			UpdatedAt:       &updatedAt,
		},
		ClearDeleted: true,
	})
	if err != nil {
		t.Fatalf("seed old session: %v", err)
	}
	oldItemTime := updatedAt
	if _, err := stack.Repo.CreateChatItem(ctx, repository.CreateChatItemInput{
		SessionID:  chatID,
		Ordinal:    0,
		Role:       "user",
		ItemType:   "message",
		Text:       strPtr("old"),
		SearchText: "old needle",
		RawJSON:    strPtr(`{"text":"old"}`),
		CreatedAt:  &oldItemTime,
	}); err != nil {
		t.Fatalf("seed old item: %v", err)
	}
	oldItems, err := stack.Repo.ListChatItems(ctx, chatID)
	if err != nil {
		t.Fatalf("ListChatItems(old): %v", err)
	}
	rollback := chatImportRollbackState{existed: true, session: stored, items: oldItems}

	newTitle := strPtr("new title")
	newUpdatedAt := updatedAt.Add(time.Hour)
	if _, _, err := stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
		CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:              chatID,
			Source:          "codex",
			SourceSessionID: "existing-session",
			SourceDeviceID:  "test-device",
			Title:           newTitle,
			StartedAt:       startedAt,
			LastActivityAt:  newUpdatedAt,
			CreatedAt:       &startedAt,
			UpdatedAt:       &newUpdatedAt,
		},
		ClearDeleted: true,
	}); err != nil {
		t.Fatalf("mutate session: %v", err)
	}
	if err := replaceChatItems(ctx, stack.Repo, chatID, []repository.CreateChatItemInput{{
		Ordinal:    0,
		Role:       "assistant",
		ItemType:   "message",
		SearchText: "new needle",
		CreatedAt:  &newUpdatedAt,
	}}); err != nil {
		t.Fatalf("mutate items: %v", err)
	}

	rollback.restore(ctx, stack)

	restored, err := stack.Repo.GetChatSessionByID(ctx, chatID)
	if err != nil {
		t.Fatalf("GetChatSessionByID(restored): %v", err)
	}
	if restored.Title == nil || *restored.Title != "old title" {
		t.Fatalf("expected old title after restore, got %+v", restored.Title)
	}
	items, err := stack.Repo.ListChatItems(ctx, chatID)
	if err != nil {
		t.Fatalf("ListChatItems(restored): %v", err)
	}
	if len(items) != 1 || items[0].SearchText != "old needle" {
		t.Fatalf("expected old items after restore, got %+v", items)
	}
}

func TestChatItemInputsCopiesStoredItems(t *testing.T) {
	createdAt := time.Date(2026, 3, 15, 13, 0, 0, 0, time.UTC)
	text := "hello"
	inputs := chatItemInputs([]repository.ChatItem{{
		SessionID:  "session-id",
		Ordinal:    7,
		Role:       "assistant",
		ItemType:   "tool_output",
		Text:       &text,
		SearchText: "hello world",
		RawJSON:    strPtr(`{"text":"hello"}`),
		CreatedAt:  createdAt,
	}})
	if len(inputs) != 1 {
		t.Fatalf("expected one input, got %+v", inputs)
	}
	got := inputs[0]
	if got.SessionID != "session-id" || got.Ordinal != 7 || got.Role != "assistant" || got.ItemType != "tool_output" || got.Text == nil || *got.Text != "hello" || got.SearchText != "hello world" || got.RawJSON == nil || *got.RawJSON != `{"text":"hello"}` {
		t.Fatalf("unexpected input copy: %+v", got)
	}
	if got.CreatedAt == nil || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected copied CreatedAt, got %+v", got.CreatedAt)
	}
}

// TestChatImportFailsOnUnregisteredDevice covers the validateActiveDevice
// error branch in runChatImport.
func TestChatImportFailsOnUnregisteredDevice(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	writeTestChatTranscript(t, root, "session.json", importTranscriptBody)
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "not-registered", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected unregistered device to fail import")
	}
}

// TestChatImportFailsOnInvalidRoot covers the chatimport.Roots error branch.
func TestChatImportFailsOnInvalidRoot(t *testing.T) {
	setupEnv(t)
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", "/path/that/does/not/exist"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid root to fail import")
	}
}

// TestChatImportFailsOnParseError exercises the parse-error branch.
func TestChatImportFailsOnParseError(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write broken: %v", err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected parse error to fail import")
	}
}

// TestIsSameAsManagedFile exercises both the truthy (path resolves to
// managed file) and falsy branches of isSameAsManagedFile.
func TestIsSameAsManagedFile(t *testing.T) {
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack: %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })
	chatID := "20260315-abcdef12"
	key := "chats/raw/" + chatID + "/source.json"

	managed, err := stack.FS.ResolveChatSourcePath(chatID, key)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(managed), 0o700); err != nil {
		t.Fatalf("create managed dir: %v", err)
	}
	if err := os.WriteFile(managed, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write managed file: %v", err)
	}
	if !isSameAsManagedFile(stack.FS, chatID, key, managed) {
		t.Fatal("expected identical path to match managed file")
	}
	if isSameAsManagedFile(stack.FS, chatID, key, "/tmp/something-else.json") {
		t.Fatal("expected different path to not match managed file")
	}
	// Invalid key produces a Resolve error, returning false.
	if isSameAsManagedFile(stack.FS, chatID, "chats/raw/other/source.json", managed) {
		t.Fatal("expected invalid key to short-circuit to false")
	}
	summary := chatImportSummary{}
	deleteImportedChatSourceIfSafe(&summary, stack.FS, chatID, key, managed)
	if summary.SourcesDeleted != 0 || len(summary.SourceDeleteWarnings) != 0 {
		t.Fatalf("managed source should not be deleted, summary=%+v", summary)
	}
	if _, err := os.Stat(managed); err != nil {
		t.Fatalf("managed source should still exist: %v", err)
	}
}

func TestSourceMatchesManagedRawBranches(t *testing.T) {
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack: %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })

	chatID := "20260315-1234abcd"
	key := "chats/raw/" + chatID + "/source.json"
	managed, err := stack.FS.ResolveChatSourcePath(chatID, key)
	if err != nil {
		t.Fatalf("resolve managed raw: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(managed), 0o700); err != nil {
		t.Fatalf("mkdir managed raw: %v", err)
	}
	source := filepath.Join(t.TempDir(), "source.json")
	writeBoth := func(t *testing.T, sourceBody string, managedBody string) {
		t.Helper()
		if err := os.WriteFile(source, []byte(sourceBody), 0o644); err != nil {
			t.Fatalf("write source: %v", err)
		}
		if err := os.WriteFile(managed, []byte(managedBody), 0o600); err != nil {
			t.Fatalf("write managed: %v", err)
		}
	}

	writeBoth(t, `{"a":1}`, `{"a":1}`)
	matches, err := sourceMatchesManagedRaw(context.Background(), stack.FS, chatID, key, source)
	if err != nil || !matches {
		t.Fatalf("equal files: matches=%v err=%v", matches, err)
	}

	writeBoth(t, `{"a":1}`, `{"a":2}`)
	matches, err = sourceMatchesManagedRaw(context.Background(), stack.FS, chatID, key, source)
	if err != nil || matches {
		t.Fatalf("same-size different files: matches=%v err=%v", matches, err)
	}

	writeBoth(t, `{"a":1}`, `{"longer":true}`)
	matches, err = sourceMatchesManagedRaw(context.Background(), stack.FS, chatID, key, source)
	if err != nil || matches {
		t.Fatalf("different-size files: matches=%v err=%v", matches, err)
	}

	appendChatID := "20260315-abcd1234"
	appendKey := "chats/raw/" + appendChatID + "/source.jsonl"
	appendManaged, err := stack.FS.ResolveChatSourcePath(appendChatID, appendKey)
	if err != nil {
		t.Fatalf("resolve append managed raw: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(appendManaged), 0o700); err != nil {
		t.Fatalf("mkdir append managed raw: %v", err)
	}
	appendSource := filepath.Join(t.TempDir(), "append.jsonl")
	if err := os.WriteFile(appendManaged, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write append managed raw: %v", err)
	}
	if err := os.WriteFile(appendSource, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("write append source: %v", err)
	}
	comparison, err := compareChatImportSource(context.Background(), stack.FS, appendChatID, appendKey, appendSource)
	if err != nil || !comparison.appendOnly || comparison.matches {
		t.Fatalf("append-only comparison = %+v err=%v", comparison, err)
	}
	if err := os.WriteFile(appendSource, []byte("other\ntwo\n"), 0o644); err != nil {
		t.Fatalf("write rewritten append source: %v", err)
	}
	comparison, err = compareChatImportSource(context.Background(), stack.FS, appendChatID, appendKey, appendSource)
	if err != nil || comparison.appendOnly || comparison.matches {
		t.Fatalf("rewritten comparison = %+v err=%v", comparison, err)
	}

	if err := os.Remove(managed); err != nil {
		t.Fatalf("remove managed raw: %v", err)
	}
	matches, err = sourceMatchesManagedRaw(context.Background(), stack.FS, chatID, key, source)
	if err != nil || matches {
		t.Fatalf("missing managed raw: matches=%v err=%v", matches, err)
	}

	writeBoth(t, `{"a":1}`, `{"a":1}`)
	missingSource := filepath.Join(t.TempDir(), "missing.json")
	if _, err := sourceMatchesManagedRaw(context.Background(), stack.FS, chatID, key, missingSource); err == nil {
		t.Fatal("expected missing source to fail")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sourceMatchesManagedRaw(canceled, stack.FS, chatID, key, source); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}

	if _, err := sourceMatchesManagedRaw(context.Background(), stack.FS, chatID, "chats/raw/other/source.json", source); err == nil {
		t.Fatal("expected invalid raw source key to fail")
	}

	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		writeBoth(t, ``, ``)
		if err := os.Chmod(source, 0o000); err != nil {
			t.Fatalf("chmod source unreadable: %v", err)
		}
		if _, err := sourceMatchesManagedRaw(context.Background(), stack.FS, chatID, key, source); err == nil {
			t.Fatal("expected unreadable source hash to fail")
		}
		if err := os.Chmod(source, 0o644); err != nil {
			t.Fatalf("restore source permissions: %v", err)
		}

		writeBoth(t, ``, ``)
		if err := os.Chmod(managed, 0o000); err != nil {
			t.Fatalf("chmod managed unreadable: %v", err)
		}
		if _, err := sourceMatchesManagedRaw(context.Background(), stack.FS, chatID, key, source); err == nil {
			t.Fatal("expected unreadable managed hash to fail")
		}
		if err := os.Chmod(managed, 0o600); err != nil {
			t.Fatalf("restore managed permissions: %v", err)
		}
	}
}

type chatImportBatchWriterFunc func(context.Context, []repository.ChatImportOp) ([]repository.ChatImportResult, error)

func (f chatImportBatchWriterFunc) WriteChatImportBatch(ctx context.Context, ops []repository.ChatImportOp) ([]repository.ChatImportResult, error) {
	return f(ctx, ops)
}

func (f chatImportBatchWriterFunc) RunChatImportBulkMode(ctx context.Context, fn func(context.Context) (bool, error)) error {
	_, err := fn(ctx)
	return err
}

func TestQueueAppendJSONLChatImportErrorBranches(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, "managed.jsonl")
	sourcePath := filepath.Join(root, "source.jsonl")
	rawKey := "chats/raw/20260514-deadbeef/source.jsonl"
	initial := `{"session_id":"existing","role":"user","content":"first","timestamp":"2026-05-14T12:00:00Z"}` + "\n"
	appended := `{"session_id":"existing","role":"assistant","content":"second","timestamp":"2026-05-14T12:01:00Z"}` + "\n"
	if err := os.WriteFile(managedPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(initial+appended), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatalf("stat source: %v", err)
	}
	existing := repository.ChatSession{ID: "20260514-deadbeef", Source: "codex", SourceSessionID: "existing", SourceDeviceID: "test-device", RawSourceKey: &rawKey}
	comparison := chatImportSourceComparison{
		appendOnly:  true,
		managedPath: managedPath,
		managedSize: int64(len(initial)),
		sourceSize:  sourceInfo.Size(),
	}

	repoOrdinalErr := &mockRepo{maxChatOrdinalFn: func(context.Context, string) (int, error) { return 0, errors.New("ordinal boom") }}
	if err := queueAppendJSONLChatImport(context.Background(), &localStack{Repo: repoOrdinalErr}, &chatImportBatch{}, nil, existing, sourcePath, comparison, "test-device", nil, false); err == nil || !strings.Contains(err.Error(), "find existing chat item ordinal") {
		t.Fatalf("expected wrapped ordinal error, got %v", err)
	}

	fsClient, err := filesystem.NewClient(root)
	if err != nil {
		t.Fatalf("filesystem client: %v", err)
	}
	badExisting := existing
	badExisting.ID = "bad-id"
	repoStageErr := &mockRepo{maxChatOrdinalFn: func(context.Context, string) (int, error) { return 0, nil }}
	if err := queueAppendJSONLChatImport(context.Background(), &localStack{Repo: repoStageErr, FS: fsClient}, &chatImportBatch{}, nil, badExisting, sourcePath, comparison, "test-device", nil, false); err == nil || !strings.Contains(err.Error(), "stage appended chat source") {
		t.Fatalf("expected wrapped stage error, got %v", err)
	}
	summary := chatImportSummary{}
	errBatch := chatImportBatch{
		writer: chatImportBatchWriterFunc(func(context.Context, []repository.ChatImportOp) ([]repository.ChatImportResult, error) {
			return nil, errors.New("batch boom")
		}),
		fs:      fsClient,
		summary: &summary,
	}
	repoBatchErr := &mockRepo{maxChatOrdinalFn: func(context.Context, string) (int, error) { return 0, nil }}
	idx := chatImportSessionIndex{byOriginalPath: make(map[chatImportSourcePathKey]repository.ChatSession), bySourceSession: make(map[chatImportSourceSessionKey]repository.ChatSession)}
	if err := queueAppendJSONLChatImport(context.Background(), &localStack{Repo: repoBatchErr, FS: fsClient}, &errBatch, &idx, existing, sourcePath, comparison, "test-device", nil, false); err != nil {
		t.Fatalf("queue append before explicit flush: %v", err)
	}
	if err := errBatch.flush(context.Background()); err == nil || !strings.Contains(err.Error(), "write chat import batch") {
		t.Fatalf("expected wrapped batch error, got %v", err)
	}
	if entries, readErr := os.ReadDir(filepath.Join(root, "chats", "raw")); readErr == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".staging-") {
				t.Fatalf("expected failed batch to clean staged raw source, found %s", entry.Name())
			}
		}
	}
}

func TestQueueAppendJSONLChatImportPromotesStagedRawAfterBatchWrite(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, "managed.jsonl")
	sourcePath := filepath.Join(root, "source.jsonl")
	initial := `{"session_id":"existing","role":"user","content":"first","timestamp":"2026-05-14T12:00:00Z"}` + "\n"
	appended := `{"session_id":"existing","role":"assistant","content":"second","timestamp":"2026-05-14T12:01:00Z"}` + "\n"
	if err := os.WriteFile(managedPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(initial+appended), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	fsClient, err := filesystem.NewClient(root)
	if err != nil {
		t.Fatalf("filesystem client: %v", err)
	}
	rawKey := "chats/raw/20260514-deadbeef/source.jsonl"
	existing := repository.ChatSession{ID: "20260514-deadbeef", Source: "codex", SourceSessionID: "existing", SourceDeviceID: "test-device", RawSourceKey: &rawKey}
	comparison := chatImportSourceComparison{appendOnly: true, managedPath: managedPath, managedSize: int64(len(initial)), sourceSize: int64(len(initial + appended))}
	repo := &mockRepo{maxChatOrdinalFn: func(context.Context, string) (int, error) { return 0, nil }}
	summary := chatImportSummary{}
	batch := chatImportBatch{
		writer: chatImportBatchWriterFunc(func(_ context.Context, ops []repository.ChatImportOp) ([]repository.ChatImportResult, error) {
			if len(ops) != 1 || ops[0].ItemMode != repository.ChatImportItemModeAppend || len(ops[0].Items) != 1 {
				t.Fatalf("unexpected append batch ops: %+v", ops)
			}
			input := ops[0].Session.CreateChatSessionInput
			return []repository.ChatImportResult{{Session: repository.ChatSession{
				ID:                 input.ID,
				Source:             input.Source,
				SourceSessionID:    input.SourceSessionID,
				SourceDeviceID:     input.SourceDeviceID,
				OriginalSourcePath: input.OriginalSourcePath,
				RawSourceKey:       input.RawSourceKey,
			}}}, nil
		}),
		fs:      fsClient,
		summary: &summary,
	}
	idx := chatImportSessionIndex{byOriginalPath: make(map[chatImportSourcePathKey]repository.ChatSession), bySourceSession: make(map[chatImportSourceSessionKey]repository.ChatSession)}
	if err := queueAppendJSONLChatImport(context.Background(), &localStack{Repo: repo, FS: fsClient}, &batch, &idx, existing, sourcePath, comparison, "test-device", nil, false); err != nil {
		t.Fatalf("queueAppendJSONLChatImport: %v", err)
	}
	if err := batch.flush(context.Background()); err != nil {
		t.Fatalf("flush append batch: %v", err)
	}
	if summary.SessionsUpdated != 1 || summary.ItemsImported != 1 || summary.RawSourcesCopied != 1 {
		t.Fatalf("unexpected append summary: %+v", summary)
	}
	promotedPath, err := fsClient.ResolveChatSourcePath(existing.ID, rawKey)
	if err != nil {
		t.Fatalf("resolve managed raw: %v", err)
	}
	if got, err := os.ReadFile(promotedPath); err != nil || string(got) != initial+appended {
		t.Fatalf("managed should be full source after staged promotion: got=%q err=%v", got, err)
	}
}

func TestQueueAppendJSONLChatImportRepairsDBAheadRawWithoutDuplicateItems(t *testing.T) {
	root := t.TempDir()
	managedPath := filepath.Join(root, "managed.jsonl")
	sourcePath := filepath.Join(root, "source.jsonl")
	initial := `{"session_id":"existing","role":"user","content":"first","timestamp":"2026-05-14T12:00:00Z"}` + "\n"
	appended := `{"session_id":"existing","role":"assistant","content":"second","timestamp":"2026-05-14T12:01:00Z"}` + "\n"
	if err := os.WriteFile(managedPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(initial+appended), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	fsClient, err := filesystem.NewClient(root)
	if err != nil {
		t.Fatalf("filesystem client: %v", err)
	}
	rawKey := "chats/raw/20260514-deadbeef/source.jsonl"
	existing := repository.ChatSession{ID: "20260514-deadbeef", Source: "codex", SourceSessionID: "existing", SourceDeviceID: "test-device", RawSourceKey: &rawKey}
	firstText := "first"
	secondText := "second"
	firstRaw := `{"content":"first","role":"user","session_id":"existing","timestamp":"2026-05-14T12:00:00Z"}`
	secondRaw := `{"content":"second","role":"assistant","session_id":"existing","timestamp":"2026-05-14T12:01:00Z"}`
	comparison := chatImportSourceComparison{appendOnly: true, managedPath: managedPath, managedSize: int64(len(initial)), sourceSize: int64(len(initial + appended))}
	repo := &mockRepo{
		maxChatOrdinalFn: func(context.Context, string) (int, error) { return 1, nil },
		listChatItemsFn: func(context.Context, string) ([]repository.ChatItem, error) {
			return []repository.ChatItem{
				{SessionID: existing.ID, Ordinal: 0, Role: "user", ItemType: "message", Text: &firstText, SearchText: firstText, RawJSON: &firstRaw},
				{SessionID: existing.ID, Ordinal: 1, Role: "assistant", ItemType: "message", Text: &secondText, SearchText: secondText, RawJSON: &secondRaw},
			}, nil
		},
	}
	summary := chatImportSummary{}
	batch := chatImportBatch{
		writer: chatImportBatchWriterFunc(func(_ context.Context, ops []repository.ChatImportOp) ([]repository.ChatImportResult, error) {
			if len(ops) != 1 || len(ops[0].Items) != 0 {
				t.Fatalf("expected repair batch with no duplicate items, got %+v", ops)
			}
			input := ops[0].Session.CreateChatSessionInput
			return []repository.ChatImportResult{{Session: repository.ChatSession{
				ID:                 input.ID,
				Source:             input.Source,
				SourceSessionID:    input.SourceSessionID,
				SourceDeviceID:     input.SourceDeviceID,
				OriginalSourcePath: input.OriginalSourcePath,
				RawSourceKey:       input.RawSourceKey,
			}}}, nil
		}),
		fs:      fsClient,
		summary: &summary,
	}
	idx := chatImportSessionIndex{byOriginalPath: make(map[chatImportSourcePathKey]repository.ChatSession), bySourceSession: make(map[chatImportSourceSessionKey]repository.ChatSession)}
	if err := queueAppendJSONLChatImport(context.Background(), &localStack{Repo: repo, FS: fsClient}, &batch, &idx, existing, sourcePath, comparison, "test-device", nil, false); err != nil {
		t.Fatalf("queueAppendJSONLChatImport: %v", err)
	}
	if err := batch.flush(context.Background()); err != nil {
		t.Fatalf("flush repair batch: %v", err)
	}
	if summary.SessionsUpdated != 1 || summary.ItemsImported != 0 || summary.RawSourcesCopied != 1 {
		t.Fatalf("unexpected repair summary: %+v", summary)
	}
	managed, err := fsClient.ResolveChatSourcePath(existing.ID, rawKey)
	if err != nil {
		t.Fatalf("resolve managed raw: %v", err)
	}
	if got, err := os.ReadFile(managed); err != nil || string(got) != initial+appended {
		t.Fatalf("managed raw should be promoted without duplicate item write: got=%q err=%v", got, err)
	}
}

func TestChatImportPureHelperBranches(t *testing.T) {
	a := "alpha"
	b := "beta"
	if !nullableTextEqual(nil, nil) {
		t.Fatalf("nullableTextEqual(nil, nil) = false, want true")
	}
	if nullableTextEqual(&a, nil) || nullableTextEqual(nil, &a) {
		t.Fatalf("nullableTextEqual treats nil and non-nil as equal")
	}
	if !nullableTextEqual(&a, &a) {
		t.Fatalf("nullableTextEqual(&a, &a) = false, want true")
	}
	if nullableTextEqual(&a, &b) {
		t.Fatalf("nullableTextEqual(&a, &b) = true, want false")
	}

	if got := chatImportSearchText(repository.CreateChatItemInput{SearchText: "indexed"}); got != "indexed" {
		t.Fatalf("chatImportSearchText(search) = %q, want indexed", got)
	}
	if got := chatImportSearchText(repository.CreateChatItemInput{Text: &a}); got != "alpha" {
		t.Fatalf("chatImportSearchText(text) = %q, want alpha", got)
	}
	if got := chatImportSearchText(repository.CreateChatItemInput{}); got != "" {
		t.Fatalf("chatImportSearchText(zero) = %q, want empty", got)
	}

	batch := chatImportBatch{entries: []pendingChatImport{{op: repository.ChatImportOp{Session: repository.UpsertChatSessionInput{CreateChatSessionInput: repository.CreateChatSessionInput{Source: "codex", SourceSessionID: "match"}}}}}}
	if !batch.hasSourceSession("codex", "match") {
		t.Fatalf("hasSourceSession(match) = false, want true")
	}
	if batch.hasSourceSession("codex", "miss") {
		t.Fatalf("hasSourceSession(miss) = true, want false")
	}
}

func TestChatImportBatchFlushReportsPromoteFailureAndCleansRemainingStages(t *testing.T) {
	root := t.TempDir()
	fsClient, err := filesystem.NewClient(root)
	if err != nil {
		t.Fatalf("filesystem client: %v", err)
	}
	sourcePath := filepath.Join(root, "src.json")
	if err := os.WriteFile(sourcePath, []byte(`{"id":"a","messages":[]}`), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	stageA, err := fsClient.CopyChatSourceToStage("20260514-aaaa1111", sourcePath)
	if err != nil {
		t.Fatalf("stage A: %v", err)
	}
	stageB, err := fsClient.CopyChatSourceToStage("20260514-bbbb2222", sourcePath)
	if err != nil {
		t.Fatalf("stage B: %v", err)
	}
	// Removing A's staged file makes PromoteChatSourceStage fail with "staged chat source missing".
	if err := os.Remove(stageA.StagedPath); err != nil {
		t.Fatalf("remove staged A file: %v", err)
	}
	summary := chatImportSummary{}
	idx := chatImportSessionIndex{byOriginalPath: make(map[chatImportSourcePathKey]repository.ChatSession), bySourceSession: make(map[chatImportSourceSessionKey]repository.ChatSession)}
	batch := chatImportBatch{
		writer: chatImportBatchWriterFunc(func(_ context.Context, ops []repository.ChatImportOp) ([]repository.ChatImportResult, error) {
			results := make([]repository.ChatImportResult, len(ops))
			for i, op := range ops {
				input := op.Session.CreateChatSessionInput
				results[i] = repository.ChatImportResult{Session: repository.ChatSession{ID: input.ID, Source: input.Source, SourceSessionID: input.SourceSessionID}, Created: true}
			}
			return results, nil
		}),
		fs:      fsClient,
		summary: &summary,
		entries: []pendingChatImport{
			{op: repository.ChatImportOp{Session: repository.UpsertChatSessionInput{CreateChatSessionInput: repository.CreateChatSessionInput{ID: stageA.ChatSessionID, Source: "codex", SourceSessionID: "a"}}}, stage: stageA, sessionIndex: &idx},
			{op: repository.ChatImportOp{Session: repository.UpsertChatSessionInput{CreateChatSessionInput: repository.CreateChatSessionInput{ID: stageB.ChatSessionID, Source: "codex", SourceSessionID: "b"}}}, stage: stageB, sessionIndex: &idx},
		},
	}
	err = batch.flush(context.Background())
	if err == nil || !strings.Contains(err.Error(), "promote chat source") {
		t.Fatalf("flush() error = %v, want promote-failure context", err)
	}
	if _, statErr := os.Stat(stageB.StagedPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected stage B cleaned after promote failure, stat=%v", statErr)
	}
}

func TestHandleExistingChatImportFlushesPendingDuplicateAppend(t *testing.T) {
	root := t.TempDir()
	fsClient, err := filesystem.NewClient(root)
	if err != nil {
		t.Fatalf("filesystem client: %v", err)
	}
	chatID := "20260514-deadbeef"
	rawKey := "chats/raw/" + chatID + "/source.jsonl"
	initial := `{"session_id":"existing","role":"user","content":"first","timestamp":"2026-05-14T12:00:00Z"}` + "\n"
	appended := `{"session_id":"existing","role":"assistant","content":"second","timestamp":"2026-05-14T12:01:00Z"}` + "\n"
	managedPath, err := fsClient.ResolveChatSourcePath(chatID, rawKey)
	if err != nil {
		t.Fatalf("resolve managed: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o700); err != nil {
		t.Fatalf("mkdir managed: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	sourceA := filepath.Join(root, "a.jsonl")
	sourceB := filepath.Join(root, "b.jsonl")
	for _, p := range []string{sourceA, sourceB} {
		if err := os.WriteFile(p, []byte(initial+appended), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	existing := repository.ChatSession{ID: chatID, Source: "codex", SourceSessionID: "existing", SourceDeviceID: "test-device", RawSourceKey: &rawKey}
	summary := chatImportSummary{}
	flushCount := 0
	maxOrdinal := 0
	batch := chatImportBatch{
		fs:      fsClient,
		summary: &summary,
	}
	batch.writer = chatImportBatchWriterFunc(func(_ context.Context, ops []repository.ChatImportOp) ([]repository.ChatImportResult, error) {
		flushCount++
		results := make([]repository.ChatImportResult, len(ops))
		for i, op := range ops {
			input := op.Session.CreateChatSessionInput
			results[i] = repository.ChatImportResult{Session: repository.ChatSession{ID: input.ID, Source: input.Source, SourceSessionID: input.SourceSessionID, RawSourceKey: input.RawSourceKey}}
			maxOrdinal += len(op.Items)
		}
		return results, nil
	})
	idx := chatImportSessionIndex{byOriginalPath: make(map[chatImportSourcePathKey]repository.ChatSession), bySourceSession: make(map[chatImportSourceSessionKey]repository.ChatSession)}
	repo := &mockRepo{
		maxChatOrdinalFn: func(context.Context, string) (int, error) { return maxOrdinal, nil },
	}
	stack := &localStack{Repo: repo, FS: fsClient}

	handled, _, err := handleExistingChatImport(context.Background(), stack, &batch, &idx, existing, sourceA, "test-device", nil, false)
	if err != nil {
		t.Fatalf("handleExistingChatImport(A) error = %v", err)
	}
	if !handled {
		t.Fatalf("handleExistingChatImport(A) handled=false, want true")
	}
	if flushCount != 0 {
		t.Fatalf("expected no flush after first append, got %d", flushCount)
	}
	if len(batch.entries) != 1 {
		t.Fatalf("expected one pending entry after first append, got %d", len(batch.entries))
	}

	handled, _, err = handleExistingChatImport(context.Background(), stack, &batch, &idx, existing, sourceB, "test-device", nil, false)
	if err != nil {
		t.Fatalf("handleExistingChatImport(B) error = %v", err)
	}
	if !handled {
		t.Fatalf("handleExistingChatImport(B) handled=false, want true")
	}
	if flushCount != 1 {
		t.Fatalf("expected pre-flush before second append, got flushCount=%d", flushCount)
	}
	if len(batch.entries) != 1 {
		t.Fatalf("expected the second op pending after pre-flush, got %d", len(batch.entries))
	}
}

func TestQueueAppendJSONLChatImportExtraErrorBranches(t *testing.T) {
	root := t.TempDir()
	fsClient, err := filesystem.NewClient(root)
	if err != nil {
		t.Fatalf("filesystem client: %v", err)
	}
	managedPath := filepath.Join(root, "managed.jsonl")
	sourcePath := filepath.Join(root, "src.jsonl")
	initial := `{"session_id":"existing","role":"user","content":"first","timestamp":"2026-05-14T12:00:00Z"}` + "\n"
	appended := `{"session_id":"existing","role":"assistant","content":"second","timestamp":"2026-05-14T12:01:00Z"}` + "\n"
	if err := os.WriteFile(managedPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(initial+appended), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	rawKey := "chats/raw/20260514-deadbeef/source.jsonl"
	existing := repository.ChatSession{ID: "20260514-deadbeef", Source: "codex", SourceSessionID: "existing", SourceDeviceID: "test-device", RawSourceKey: &rawKey}
	comparison := chatImportSourceComparison{appendOnly: true, managedPath: managedPath, managedSize: int64(len(initial)), sourceSize: int64(len(initial + appended))}
	idx := chatImportSessionIndex{byOriginalPath: make(map[chatImportSourcePathKey]repository.ChatSession), bySourceSession: make(map[chatImportSourceSessionKey]repository.ChatSession)}
	summary := chatImportSummary{}
	batch := chatImportBatch{fs: fsClient, summary: &summary}

	repoListErr := &mockRepo{
		maxChatOrdinalFn: func(context.Context, string) (int, error) { return 0, nil },
		listChatItemsFn:  func(context.Context, string) ([]repository.ChatItem, error) { return nil, errors.New("list boom") },
	}
	if err := queueAppendJSONLChatImport(context.Background(), &localStack{Repo: repoListErr, FS: fsClient}, &batch, &idx, existing, sourcePath, comparison, "test-device", nil, false); err == nil || !strings.Contains(err.Error(), "list existing chat items before append") {
		t.Fatalf("queueAppendJSONLChatImport(list error) = %v, want list-error context", err)
	}

	missingManaged := comparison
	missingManaged.managedPath = filepath.Join(root, "missing.jsonl")
	repoParseErr := &mockRepo{maxChatOrdinalFn: func(context.Context, string) (int, error) { return 0, nil }}
	if err := queueAppendJSONLChatImport(context.Background(), &localStack{Repo: repoParseErr, FS: fsClient}, &batch, &idx, existing, sourcePath, missingManaged, "test-device", nil, false); err == nil || !strings.Contains(err.Error(), "parse managed chat raw source") {
		t.Fatalf("queueAppendJSONLChatImport(parse error) = %v, want parse-error context", err)
	}

	repoMismatch := &mockRepo{maxChatOrdinalFn: func(context.Context, string) (int, error) { return 99, nil }}
	if err := queueAppendJSONLChatImport(context.Background(), &localStack{Repo: repoMismatch, FS: fsClient}, &batch, &idx, existing, sourcePath, comparison, "test-device", nil, false); err == nil || !strings.Contains(err.Error(), "append-only chat import item state mismatch") {
		t.Fatalf("queueAppendJSONLChatImport(ordinal mismatch) = %v, want mismatch context", err)
	}
}

func TestQueueReplaceAndQueueAppendStageErrors(t *testing.T) {
	root := t.TempDir()
	fsClient, err := filesystem.NewClient(root)
	if err != nil {
		t.Fatalf("filesystem client: %v", err)
	}
	missingSource := filepath.Join(root, "missing.jsonl")
	idx := chatImportSessionIndex{byOriginalPath: make(map[chatImportSourcePathKey]repository.ChatSession), bySourceSession: make(map[chatImportSourceSessionKey]repository.ChatSession)}
	summary := chatImportSummary{}
	batch := chatImportBatch{fs: fsClient, summary: &summary}
	session := repository.CreateChatSessionInput{
		ID:              "20260514-eeeeffff",
		Source:          "codex",
		SourceSessionID: "missing-src",
		SourceDeviceID:  "test-device",
	}
	stack := &localStack{Repo: &mockRepo{}, FS: fsClient}
	if err := queueReplaceChatImport(context.Background(), stack, &batch, &idx, session, nil, missingSource, false); err == nil || !strings.Contains(err.Error(), "stage chat source") {
		t.Fatalf("queueReplaceChatImport(missing source) error = %v, want stage error", err)
	}

	rawKey := "chats/raw/20260514-eeeeffff/source.jsonl"
	existing := repository.ChatSession{ID: "20260514-eeeeffff", Source: "codex", SourceSessionID: "missing-src", SourceDeviceID: "test-device", RawSourceKey: &rawKey}
	managedPath := filepath.Join(root, "managed.jsonl")
	initial := `{"session_id":"existing","role":"user","content":"first","timestamp":"2026-05-14T12:00:00Z"}` + "\n"
	appended := `{"session_id":"existing","role":"assistant","content":"second","timestamp":"2026-05-14T12:01:00Z"}` + "\n"
	if err := os.WriteFile(managedPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write managed: %v", err)
	}
	sourcePathExisting := filepath.Join(root, "src-append.jsonl")
	if err := os.WriteFile(sourcePathExisting, []byte(initial+appended), 0o644); err != nil {
		t.Fatalf("write append source: %v", err)
	}
	comparison := chatImportSourceComparison{appendOnly: true, managedPath: managedPath, managedSize: int64(len(initial)), sourceSize: int64(len(initial + appended))}
	badExisting := existing
	badExisting.ID = "bad-id"
	appendStack := &localStack{Repo: &mockRepo{maxChatOrdinalFn: func(context.Context, string) (int, error) { return 0, nil }}, FS: fsClient}
	if err := queueAppendJSONLChatImport(context.Background(), appendStack, &batch, &idx, badExisting, sourcePathExisting, comparison, "test-device", nil, false); err == nil || !strings.Contains(err.Error(), "stage appended chat source") {
		t.Fatalf("queueAppendJSONLChatImport(bad id) error = %v, want stage error", err)
	}
}

func TestChatImportBatchAddFlushesWhenFull(t *testing.T) {
	root := t.TempDir()
	fsClient, err := filesystem.NewClient(root)
	if err != nil {
		t.Fatalf("filesystem client: %v", err)
	}
	sourcePath := filepath.Join(root, "src.json")
	if err := os.WriteFile(sourcePath, []byte(`{"id":"a","messages":[]}`), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	idx := chatImportSessionIndex{byOriginalPath: make(map[chatImportSourcePathKey]repository.ChatSession), bySourceSession: make(map[chatImportSourceSessionKey]repository.ChatSession)}
	entries := make([]pendingChatImport, 0, chatImportBatchSize-1)
	for i := 0; i < chatImportBatchSize-1; i++ {
		sid := fmt.Sprintf("20260514-fff0%04x", i)
		stage, err := fsClient.CopyChatSourceToStage(sid, sourcePath)
		if err != nil {
			t.Fatalf("stage[%d]: %v", i, err)
		}
		entries = append(entries, pendingChatImport{
			op:           repository.ChatImportOp{Session: repository.UpsertChatSessionInput{CreateChatSessionInput: repository.CreateChatSessionInput{ID: sid, Source: "codex", SourceSessionID: fmt.Sprintf("filler-%d", i)}}},
			stage:        stage,
			sessionIndex: &idx,
		})
	}
	finalSID := "20260514-deadbeef"
	finalStage, err := fsClient.CopyChatSourceToStage(finalSID, sourcePath)
	if err != nil {
		t.Fatalf("final stage: %v", err)
	}
	summary := chatImportSummary{}
	flushed := 0
	batch := chatImportBatch{
		writer: chatImportBatchWriterFunc(func(_ context.Context, ops []repository.ChatImportOp) ([]repository.ChatImportResult, error) {
			flushed = len(ops)
			results := make([]repository.ChatImportResult, len(ops))
			for i, op := range ops {
				input := op.Session.CreateChatSessionInput
				results[i] = repository.ChatImportResult{Session: repository.ChatSession{ID: input.ID, Source: input.Source, SourceSessionID: input.SourceSessionID}, Created: true}
			}
			return results, nil
		}),
		fs:      fsClient,
		summary: &summary,
		entries: entries,
	}
	if err := batch.add(context.Background(), pendingChatImport{
		op:           repository.ChatImportOp{Session: repository.UpsertChatSessionInput{CreateChatSessionInput: repository.CreateChatSessionInput{ID: finalSID, Source: "codex", SourceSessionID: "final"}}},
		stage:        finalStage,
		sessionIndex: &idx,
	}); err != nil {
		t.Fatalf("add(full) error = %v", err)
	}
	if flushed != chatImportBatchSize {
		t.Fatalf("expected auto-flush at batch size %d, got flushed=%d", chatImportBatchSize, flushed)
	}
	if len(batch.entries) != 0 {
		t.Fatalf("expected entries cleared after auto-flush, got %d", len(batch.entries))
	}
}

func TestChatImportBatchDiscardPendingDeletesStagedSources(t *testing.T) {
	root := t.TempDir()
	fsClient, err := filesystem.NewClient(root)
	if err != nil {
		t.Fatalf("filesystem client: %v", err)
	}
	sourcePath := filepath.Join(root, "src.json")
	if err := os.WriteFile(sourcePath, []byte(`{"id":"20260514-deadbeef","messages":[]}`), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	stage, err := fsClient.CopyChatSourceToStage("20260514-deadbeef", sourcePath)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	if _, err := os.Stat(stage.StagedPath); err != nil {
		t.Fatalf("expected staged file present before discard: %v", err)
	}
	batch := chatImportBatch{
		fs:      fsClient,
		entries: []pendingChatImport{{stage: stage}},
	}
	batch.discardPending()
	if _, err := os.Stat(stage.StagedPath); !os.IsNotExist(err) {
		t.Fatalf("expected staged file removed after discardPending, stat err=%v", err)
	}
	if len(batch.entries) != 0 {
		t.Fatalf("expected entries cleared, got %d", len(batch.entries))
	}
}

func TestFilePrefixMatchesBranches(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "prefix")
	full := filepath.Join(root, "full")
	if err := os.WriteFile(prefix, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write prefix: %v", err)
	}
	if err := os.WriteFile(full, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatalf("write full: %v", err)
	}
	matches, err := filePrefixMatches(context.Background(), prefix, full, int64(len("one\n")))
	if err != nil || !matches {
		t.Fatalf("expected prefix match, got matches=%v err=%v", matches, err)
	}
	if _, err := filePrefixMatches(context.Background(), filepath.Join(root, "missing-prefix"), full, 1); err == nil {
		t.Fatal("expected missing prefix open error")
	}
	if _, err := filePrefixMatches(context.Background(), prefix, filepath.Join(root, "missing-full"), 1); err == nil {
		t.Fatal("expected missing full open error")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := filePrefixMatches(canceled, prefix, full, int64(len("one\n"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled prefix match error, got %v", err)
	}
	shortFull := filepath.Join(root, "short-full")
	if err := os.WriteFile(shortFull, []byte("on"), 0o600); err != nil {
		t.Fatalf("write short full: %v", err)
	}
	if _, err := filePrefixMatches(context.Background(), prefix, shortFull, int64(len("one\n"))); err == nil {
		t.Fatal("expected short full read error")
	}
	emptyPrefix := filepath.Join(root, "empty-prefix")
	if err := os.WriteFile(emptyPrefix, []byte{}, 0o600); err != nil {
		t.Fatalf("write empty prefix: %v", err)
	}
	if _, err := filePrefixMatches(context.Background(), emptyPrefix, full, int64(len("one\n"))); err == nil {
		t.Fatal("expected short prefix read error")
	}
}

func TestChatImportSessionIndexAndSkipHelperBranches(t *testing.T) {
	originalPath := filepath.Join(t.TempDir(), "indexed.json")
	source := "codex"
	deviceID := "test-device"
	repo := &mockRepo{
		listChatSessionsFn: func(_ context.Context, filter repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
			if !filter.IncludeDeleted || filter.Source == nil || *filter.Source != source || filter.DeviceID == nil || *filter.DeviceID != deviceID {
				t.Fatalf("unexpected filter: %+v", filter)
			}
			return []repository.ChatSession{
				{ID: "20260315-11111111", Source: source, SourceSessionID: "legacy", SourceDeviceID: deviceID},
				{ID: "20260315-22222222", Source: source, SourceSessionID: "indexed", SourceDeviceID: deviceID, OriginalSourcePath: &originalPath},
			}, nil
		},
	}
	idx, err := loadChatImportSessionIndex(context.Background(), repo, source, deviceID)
	if err != nil {
		t.Fatalf("loadChatImportSessionIndex() error = %v", err)
	}
	if got := idx.bySourceSession[chatImportSourceSessionKey{source: source, sourceSessionID: "legacy"}]; got.ID != "20260315-11111111" {
		t.Fatalf("expected legacy session to be indexed by source session, got %+v", got)
	}
	if _, ok, err := idx.existingByOriginalPath(source, deviceID, originalPath); err != nil || !ok {
		t.Fatalf("expected original path lookup to succeed, ok=%v err=%v", ok, err)
	}
	failingRepo := &mockRepo{
		listChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
			return nil, errors.New("list failed")
		},
	}
	if _, err := loadChatImportSessionIndex(context.Background(), failingRepo, source, deviceID); err == nil || !strings.Contains(err.Error(), "list existing chat sessions") {
		t.Fatalf("expected wrapped list error, got %v", err)
	}

	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack: %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })
	sourcePath := filepath.Join(t.TempDir(), "source.json")
	if err := os.WriteFile(sourcePath, []byte(`{"messages":[]}`), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	summary := chatImportSummary{}
	skipped, err := skipUnchangedChatImport(context.Background(), stack.FS, &summary, repository.ChatSession{ID: "20260315-33333333"}, sourcePath, false)
	if err != nil || skipped || summary.SessionsSkipped != 0 {
		t.Fatalf("nil raw key skip helper = skipped %v summary %+v err %v", skipped, summary, err)
	}
	badKey := "chats/raw/other/source.json"
	skipped, err = skipUnchangedChatImport(context.Background(), stack.FS, &summary, repository.ChatSession{ID: "20260315-33333333", RawSourceKey: &badKey}, sourcePath, false)
	if err == nil || skipped {
		t.Fatalf("expected invalid raw key error, skipped=%v err=%v", skipped, err)
	}
	goodKey := "chats/raw/20260315-33333333/source.json"
	managedPath, err := stack.FS.ResolveChatSourcePath("20260315-33333333", goodKey)
	if err != nil {
		t.Fatalf("resolve managed source: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o700); err != nil {
		t.Fatalf("mkdir managed source: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte(`{"messages":[]}`), 0o600); err != nil {
		t.Fatalf("write managed source: %v", err)
	}
	summary = chatImportSummary{}
	skipped, err = skipUnchangedChatImport(context.Background(), stack.FS, &summary, repository.ChatSession{ID: "20260315-33333333", RawSourceKey: &goodKey}, sourcePath, false)
	if err != nil || !skipped || summary.SessionsSkipped != 1 {
		t.Fatalf("expected matching source to skip, skipped=%v summary=%+v err=%v", skipped, summary, err)
	}
	deleteSourcePath := filepath.Join(t.TempDir(), "delete-source.json")
	if err := os.WriteFile(deleteSourcePath, []byte(`{"messages":[]}`), 0o644); err != nil {
		t.Fatalf("write delete source: %v", err)
	}
	skipped, err = skipUnchangedChatImport(context.Background(), stack.FS, &summary, repository.ChatSession{ID: "20260315-33333333", RawSourceKey: &goodKey}, deleteSourcePath, true)
	if err != nil || !skipped || summary.SourcesDeleted != 1 {
		t.Fatalf("expected matching delete-source to skip and delete, skipped=%v summary=%+v err=%v", skipped, summary, err)
	}
	if _, err := os.Stat(deleteSourcePath); !os.IsNotExist(err) {
		t.Fatalf("expected delete source path removed, stat err=%v", err)
	}

	batch := chatImportBatch{summary: &chatImportSummary{}}
	handled, mutated, err := handleExistingChatImport(context.Background(), stack, &batch, &idx, repository.ChatSession{ID: "20260315-44444444"}, sourcePath, deviceID, nil, false)
	if err != nil || handled || mutated {
		t.Fatalf("nil raw key existing import = handled %v mutated %v err %v", handled, mutated, err)
	}
	handled, mutated, err = handleExistingChatImport(context.Background(), stack, &batch, &idx, repository.ChatSession{ID: "20260315-44444444", RawSourceKey: &badKey}, sourcePath, deviceID, nil, false)
	if err == nil || handled || mutated {
		t.Fatalf("expected invalid raw key existing import error, handled %v mutated %v err %v", handled, mutated, err)
	}
}
