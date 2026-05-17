package cli

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
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

// TestChatImportReImportBumpsSyncVersionForSameKey verifies that re-importing
// a chat that produces the same raw_source_key still bumps the chat session
// sync version. The plan requires this so cloud push uploads the new bytes
// even when the key string is unchanged.
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

func TestCaptureChatImportRollbackIgnoresMissingManagedRaw(t *testing.T) {
	ctx := context.Background()
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack: %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })

	chatID := "20260315-eeeeffff"
	rawKey := "chats/raw/" + chatID + "/source.json"
	now := time.Date(2026, 3, 15, 12, 30, 0, 0, time.UTC)
	if _, _, err := stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
		CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:              chatID,
			Source:          "codex",
			SourceSessionID: "missing-raw-session",
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

	state, err := captureChatImportRollback(ctx, stack, "codex", "missing-raw-session")
	if err != nil {
		t.Fatalf("captureChatImportRollback: %v", err)
	}
	if !state.existed || state.session.ID != chatID {
		t.Fatalf("unexpected rollback state: %+v", state)
	}
	if state.rawBackup != nil {
		t.Fatalf("expected missing managed raw source to skip backup, got %+v", state.rawBackup)
	}
}

func TestCaptureChatImportRollbackErrorBranches(t *testing.T) {
	ctx := context.Background()

	_, err := captureChatImportRollback(ctx, &localStack{Repo: &mockRepo{
		getChatBySourceFn: func(context.Context, string, string) (repository.ChatSession, error) {
			return repository.ChatSession{}, errors.New("lookup failed")
		},
	}}, "codex", "source")
	if err == nil || !strings.Contains(err.Error(), "look up existing chat rollback state") {
		t.Fatalf("captureChatImportRollback(lookup) error = %v, want lookup context", err)
	}

	_, err = captureChatImportRollback(ctx, &localStack{Repo: &mockRepo{
		getChatBySourceFn: func(context.Context, string, string) (repository.ChatSession, error) {
			return repository.ChatSession{ID: "20260315-11112222"}, nil
		},
		listChatItemsFn: func(context.Context, string) ([]repository.ChatItem, error) {
			return nil, errors.New("list failed")
		},
	}}, "codex", "source")
	if err == nil || !strings.Contains(err.Error(), "list existing chat items for rollback") {
		t.Fatalf("captureChatImportRollback(list items) error = %v, want list context", err)
	}

	homeDir := setupEnv(t)
	stack, openErr := openLocalStack(homeDir)
	if openErr != nil {
		t.Fatalf("openLocalStack: %v", openErr)
	}
	t.Cleanup(func() { _ = stack.Close() })

	invalidRawKey := "chats/raw/other/source.json"
	stack.Repo = &mockRepo{
		getChatBySourceFn: func(context.Context, string, string) (repository.ChatSession, error) {
			return repository.ChatSession{ID: "20260315-33334444", RawSourceKey: &invalidRawKey}, nil
		},
	}
	_, err = captureChatImportRollback(ctx, stack, "codex", "source")
	if err == nil || !strings.Contains(err.Error(), "resolve existing chat raw source for rollback") {
		t.Fatalf("captureChatImportRollback(resolve raw) error = %v, want resolve context", err)
	}

	chatID := "20260315-55556666"
	rawKey := "chats/raw/" + chatID + "/source.json"
	rawPath, err := stack.FS.ResolveChatSourcePath(chatID, rawKey)
	if err != nil {
		t.Fatalf("ResolveChatSourcePath: %v", err)
	}
	if err := os.MkdirAll(rawPath, 0o700); err != nil {
		t.Fatalf("create raw source directory: %v", err)
	}
	stack.Repo = &mockRepo{
		getChatBySourceFn: func(context.Context, string, string) (repository.ChatSession, error) {
			return repository.ChatSession{ID: chatID, RawSourceKey: &rawKey}, nil
		},
	}
	_, err = captureChatImportRollback(ctx, stack, "codex", "source")
	if err == nil || !strings.Contains(err.Error(), "back up existing chat raw source for rollback") {
		t.Fatalf("captureChatImportRollback(backup raw) error = %v, want backup context", err)
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
}
