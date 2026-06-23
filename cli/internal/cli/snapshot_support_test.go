package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"testing/iotest"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/gitsnapshot"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/sqlite"
)

func TestSnapshotSupportRoundTripAndUpdatePaths(t *testing.T) {
	ctx := context.Background()
	sourceHome := setupEnv(t)

	recordID := addRecordWithContent(
		t,
		`<html><body><img src="figures/original.png">source</body></html>`,
		"source-notes",
		`{"project_id":"phase7/source","git_remote_url":"https://github.com/org/source","git_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
		map[string][]byte{"original.png": []byte("original-figure")},
		map[string][]byte{"original.csv": []byte("x,y\n1,2\n")},
	)
	chatRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(chatRoot, "snapshot-chat.json"), []byte(`{
  "id": "snapshot-chat",
  "title": "Snapshot support chat",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [{"role": "user", "content": "snapshot support chat needle"}]
}`), 0o644); err != nil {
		t.Fatalf("write chat transcript: %v", err)
	}
	chatCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	chatCmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", chatRoot})
	if err := chatCmd.Execute(); err != nil {
		t.Fatalf("chat import: %v", err)
	}

	sourceStack, err := openLocalStack(sourceHome)
	if err != nil {
		t.Fatalf("open source stack: %v", err)
	}
	defer func() { _ = sourceStack.Close() }()

	if _, err := sourceStack.Repo.CreateTemplate(ctx, repository.CreateTemplateInput{
		Name:        "phase7-custom",
		HTMLContent: "<html>custom</html>",
	}); err != nil {
		t.Fatalf("create custom template: %v", err)
	}

	snapshot, err := buildLocalSnapshot(ctx, sourceStack, repository.ListRecordsFilter{})
	if err != nil {
		t.Fatalf("buildLocalSnapshot(): %v", err)
	}
	if len(snapshot.Templates) < 3 {
		t.Fatalf("expected builtin templates plus custom template, got %d", len(snapshot.Templates))
	}
	if len(snapshot.Records) != 1 {
		t.Fatalf("expected 1 record in snapshot, got %d", len(snapshot.Records))
	}
	if len(snapshot.Chats) != 1 || len(snapshot.Chats[0].Items) != 1 {
		t.Fatalf("expected 1 chat with item in snapshot, got %+v", snapshot.Chats)
	}
	if snapshot.Chats[0].RawSourceKey == nil || len(snapshot.Chats[0].RawSourceContent) == 0 {
		t.Fatalf("expected chat snapshot to carry raw source bytes, got %+v", snapshot.Chats[0])
	}

	targetHome := t.TempDir()
	if err := ensureLocalEnvironment(ctx, targetHome); err != nil {
		t.Fatalf("ensureLocalEnvironment(): %v", err)
	}
	targetStack, err := openLocalStack(targetHome)
	if err != nil {
		t.Fatalf("open target stack: %v", err)
	}
	defer func() { _ = targetStack.Close() }()

	stats, err := importSnapshotIntoStack(ctx, targetStack, snapshot)
	if err != nil {
		t.Fatalf("importSnapshotIntoStack(create): %v", err)
	}
	// One record + one chat are both created.
	if stats.Created != 2 || stats.Updated != 0 || stats.Skipped != 0 {
		t.Fatalf("create stats = %+v", stats)
	}
	importedChat, err := targetStack.Repo.GetChatSessionBySource(ctx, "codex", "snapshot-chat")
	if err != nil {
		t.Fatalf("GetChatSessionBySource(imported): %v", err)
	}
	importedItems, err := targetStack.Repo.ListChatItems(ctx, importedChat.ID)
	if err != nil {
		t.Fatalf("ListChatItems(imported): %v", err)
	}
	if len(importedItems) != 1 || importedItems[0].SearchText != "snapshot support chat needle" {
		t.Fatalf("unexpected imported chat items: %+v", importedItems)
	}
	if importedChat.RawSourceKey == nil {
		t.Fatalf("expected imported chat raw_source_key, got %+v", importedChat)
	}
	importedRawPath, err := targetStack.FS.ResolveChatSourcePath(importedChat.ID, *importedChat.RawSourceKey)
	if err != nil {
		t.Fatalf("ResolveChatSourcePath(imported): %v", err)
	}
	importedRaw, err := os.ReadFile(importedRawPath)
	if err != nil {
		t.Fatalf("read imported chat raw source: %v", err)
	}
	if !strings.Contains(string(importedRaw), "snapshot support chat needle") {
		t.Fatalf("unexpected imported chat raw source: %q", string(importedRaw))
	}

	updatedSnapshot := snapshot
	updatedSnapshot.Templates = append([]gitsnapshot.Template(nil), snapshot.Templates...)
	updatedSnapshot.Records = append([]gitsnapshot.Record(nil), snapshot.Records...)
	updatedSnapshot.Chats = append([]gitsnapshot.ChatSession(nil), snapshot.Chats...)

	for i := range updatedSnapshot.Templates {
		if updatedSnapshot.Templates[i].Name == "text-only" {
			updatedSnapshot.Templates[i].HTMLContent = "<html>text-only-updated</html>"
		}
	}
	updatedRecord := updatedSnapshot.Records[0]
	updatedRecord.HTMLContent = strPtr(`<html><body><img src="figures/fresh.png">updated</body></html>`)
	updatedRecord.UpdatedAt = updatedRecord.UpdatedAt.Add(time.Minute)
	updatedRecord.Notes = strPtr("updated-notes")
	updatedRecord.ProjectID = "phase7/updated"
	updatedSnapshot.Projects = append(updatedSnapshot.Projects, gitsnapshot.RegistryEntry{
		ID:        "phase7/updated",
		CreatedAt: updatedRecord.UpdatedAt,
		UpdatedAt: updatedRecord.UpdatedAt,
	})
	updatedRecord.GitRemoteURL = strPtr("https://github.com/org/updated")
	updatedRecord.GitHash = strPtr("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	updatedRecord.Figures = []gitsnapshot.Figure{{
		Filename: "fresh.png",
		S3Key:    filepath.ToSlash(filepath.Join("figures", recordID, "fresh.png")),
		Content:  []byte("fresh-figure"),
	}}
	updatedRecord.DataFiles = []gitsnapshot.DataFile{{
		Filename: "fresh.csv",
		S3Key:    filepath.ToSlash(filepath.Join("data", recordID, "fresh.csv")),
		Size:     int64(len("fresh,data\n")),
		Hash:     hashData("fresh,data\n"),
	}}
	updatedSnapshot.Records[0] = updatedRecord
	updatedChat := updatedSnapshot.Chats[0]
	updatedChat.UpdatedAt = updatedChat.UpdatedAt.Add(time.Minute)
	updatedChat.LastActivityAt = updatedChat.LastActivityAt.Add(time.Minute)
	updatedChat.RawSourceContent = []byte(`{"id":"snapshot-chat","messages":[{"role":"user","content":"snapshot support chat replaced"}]}`)
	updatedChat.Items = []gitsnapshot.ChatItem{{
		Ordinal:    0,
		Role:       "user",
		ItemType:   "message",
		SearchText: "snapshot support chat replaced",
		CreatedAt:  updatedChat.LastActivityAt,
	}}
	updatedSnapshot.Chats[0] = updatedChat

	stats, err = importSnapshotIntoStack(ctx, targetStack, updatedSnapshot)
	if err != nil {
		t.Fatalf("importSnapshotIntoStack(update): %v", err)
	}
	if stats.Created != 0 || stats.Updated != 2 || stats.Skipped != 0 {
		t.Fatalf("update stats = %+v", stats)
	}

	figures, err := targetStack.Repo.ListRecordFiguresByRecordID(ctx, recordID)
	if err != nil {
		t.Fatalf("ListRecordFiguresByRecordID(): %v", err)
	}
	if len(figures) != 1 || figures[0].Filename != "fresh.png" {
		t.Fatalf("figures after update = %+v", figures)
	}
	dataFiles, err := targetStack.Repo.ListRecordDataFilesByRecordID(ctx, recordID)
	if err != nil {
		t.Fatalf("ListRecordDataFilesByRecordID(): %v", err)
	}
	if len(dataFiles) != 1 || dataFiles[0].Filename != "fresh.csv" {
		t.Fatalf("data files after update = %+v", dataFiles)
	}
	if _, err := os.Stat(filepath.Join(basePath(targetHome), "figures", recordID, "original.png")); !os.IsNotExist(err) {
		t.Fatalf("original figure should be removed after update, stat err = %v", err)
	}
	importedItems, err = targetStack.Repo.ListChatItems(ctx, importedChat.ID)
	if err != nil {
		t.Fatalf("ListChatItems(updated): %v", err)
	}
	if len(importedItems) != 1 || importedItems[0].SearchText != "snapshot support chat replaced" {
		t.Fatalf("expected snapshot import to replace chat items, got %+v", importedItems)
	}
	importedRaw, err = os.ReadFile(importedRawPath)
	if err != nil {
		t.Fatalf("read updated chat raw source: %v", err)
	}
	if !strings.Contains(string(importedRaw), "snapshot support chat replaced") {
		t.Fatalf("expected updated chat raw source, got %q", string(importedRaw))
	}

	stats, err = importSnapshotIntoStack(ctx, targetStack, snapshot)
	if err != nil {
		t.Fatalf("importSnapshotIntoStack(stale chat snapshot): %v", err)
	}
	if stats.Created != 0 || stats.Updated != 0 || stats.Skipped != 2 {
		t.Fatalf("stale snapshot stats = %+v", stats)
	}
	importedItems, err = targetStack.Repo.ListChatItems(ctx, importedChat.ID)
	if err != nil {
		t.Fatalf("ListChatItems(after stale): %v", err)
	}
	if len(importedItems) != 1 || importedItems[0].SearchText != "snapshot support chat replaced" {
		t.Fatalf("expected stale snapshot to preserve newer chat items, got %+v", importedItems)
	}
	importedRaw, err = os.ReadFile(importedRawPath)
	if err != nil {
		t.Fatalf("read chat raw source after stale import: %v", err)
	}
	if !strings.Contains(string(importedRaw), "snapshot support chat replaced") {
		t.Fatalf("expected stale snapshot to preserve newer chat raw source, got %q", string(importedRaw))
	}

	stats, err = importSnapshotIntoStack(ctx, targetStack, updatedSnapshot)
	if err != nil {
		t.Fatalf("importSnapshotIntoStack(skip): %v", err)
	}
	if stats.Created != 0 || stats.Updated != 0 || stats.Skipped != 2 {
		t.Fatalf("skip stats = %+v", stats)
	}
}

func TestImportSnapshotCrashAtomicity(t *testing.T) {
	ctx := context.Background()

	t.Run("figure rename failure leaves no row and reimport converges", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })
		snapshot := atomicitySnapshot("20260310-aa11bb22", "plot.png", []byte("plot-v1"), time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC))

		origRenameFileFn := renameFileFn
		renameFileFn = func(string, string) error {
			return errors.New("rename boom")
		}
		_, err = importSnapshotIntoStack(ctx, stack, snapshot)
		renameFileFn = origRenameFileFn
		if err == nil || !strings.Contains(err.Error(), "write local figure") {
			t.Fatalf("importSnapshotIntoStack(rename failure) error = %v, want figure write failure", err)
		}
		if _, err := stack.Repo.GetRecordByID(ctx, "20260310-aa11bb22"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("record should not be committed after figure rename failure, err = %v", err)
		}

		stats, err := importSnapshotIntoStack(ctx, stack, snapshot)
		if err != nil {
			t.Fatalf("reimport after rename failure: %v", err)
		}
		if stats.Created != 1 || stats.Updated != 0 || stats.Skipped != 0 {
			t.Fatalf("reimport stats = %+v, want one created record", stats)
		}
		assertCommittedFigureFiles(t, ctx, stack, "20260310-aa11bb22", map[string]string{"plot.png": "plot-v1"})
	})

	t.Run("row commit failure leaves updated_at unchanged and reimport converges", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })
		snapshot := atomicitySnapshot("20260310-bb22cc33", "plot.png", []byte("plot-v2"), time.Date(2026, 3, 10, 13, 0, 0, 0, time.UTC))

		origCommitRecordChildrenFn := commitRecordChildrenFn
		commitRecordChildrenFn = func(context.Context, repository.Repository, repository.ReplaceRecordChildrenInput) (repository.Record, error) {
			return repository.Record{}, errors.New("commit boom")
		}
		_, err = importSnapshotIntoStack(ctx, stack, snapshot)
		commitRecordChildrenFn = origCommitRecordChildrenFn
		if err == nil || !strings.Contains(err.Error(), "replace record children rows") {
			t.Fatalf("importSnapshotIntoStack(commit failure) error = %v, want row commit failure", err)
		}
		if _, err := stack.Repo.GetRecordByID(ctx, "20260310-bb22cc33"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("record should not be committed after row failure, err = %v", err)
		}
		if _, err := os.Stat(filepath.Join(basePath(homeDir), "figures", "20260310-bb22cc33", "plot.png")); err != nil {
			t.Fatalf("figure bytes should have been written before row commit failure: %v", err)
		}

		stats, err := importSnapshotIntoStack(ctx, stack, snapshot)
		if err != nil {
			t.Fatalf("reimport after row failure: %v", err)
		}
		if stats.Created != 1 || stats.Updated != 0 || stats.Skipped != 0 {
			t.Fatalf("reimport stats = %+v, want one created record", stats)
		}
		assertCommittedFigureFiles(t, ctx, stack, "20260310-bb22cc33", map[string]string{"plot.png": "plot-v2"})
	})

	t.Run("post-commit cleanup failure leaves committed row and next import reconciles orphan", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })
		recordID := "20260310-cc33dd44"
		initial := atomicitySnapshot(recordID, "old.png", []byte("old"), time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC))
		if _, err := importSnapshotIntoStack(ctx, stack, initial); err != nil {
			t.Fatalf("initial import: %v", err)
		}
		updated := atomicitySnapshot(recordID, "new.png", []byte("new"), time.Date(2026, 3, 10, 14, 1, 0, 0, time.UTC))

		origDeleteRecordFigureFileFn := deleteRecordFigureFileFn
		deleteRecordFigureFileFn = func(*filesystem.Client, string, string) error {
			return errors.New("delete stale boom")
		}
		_, err = importSnapshotIntoStack(ctx, stack, updated)
		deleteRecordFigureFileFn = origDeleteRecordFigureFileFn
		if err == nil || !strings.Contains(err.Error(), "delete stale figure") {
			t.Fatalf("importSnapshotIntoStack(cleanup failure) error = %v, want stale cleanup failure", err)
		}
		record, err := stack.Repo.GetRecordByID(ctx, recordID)
		if err != nil {
			t.Fatalf("GetRecordByID(after cleanup failure): %v", err)
		}
		if !record.UpdatedAt.Equal(updated.Records[0].UpdatedAt) {
			t.Fatalf("record UpdatedAt = %s, want committed snapshot timestamp %s", record.UpdatedAt, updated.Records[0].UpdatedAt)
		}
		if _, err := os.Stat(filepath.Join(basePath(homeDir), "figures", recordID, "old.png")); err != nil {
			t.Fatalf("old figure should remain after injected cleanup failure: %v", err)
		}

		stats, err := importSnapshotIntoStack(ctx, stack, updated)
		if err != nil {
			t.Fatalf("reimport after cleanup failure: %v", err)
		}
		if stats.Created != 0 || stats.Updated != 0 || stats.Skipped != 1 {
			t.Fatalf("reimport stats = %+v, want skipped record plus reconcile", stats)
		}
		assertCommittedFigureFiles(t, ctx, stack, recordID, map[string]string{"new.png": "new"})
		if _, err := os.Stat(filepath.Join(basePath(homeDir), "figures", recordID, "old.png")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old orphan figure should be reconciled on reimport, stat err = %v", err)
		}
	})
}

func atomicitySnapshot(recordID string, filename string, content []byte, updatedAt time.Time) gitsnapshot.Snapshot {
	return withCLISnapshotDefaults(gitsnapshot.Snapshot{
		Records: []gitsnapshot.Record{{
			ID:             recordID,
			Date:           updatedAt.Format("2006-01-02"),
			DayOrder:       "a",
			ProjectID:      "test/default-project",
			SourceDeviceID: "test-device",
			HTMLContent:    strPtr(`<html><body><img src="figures/` + filename + `"></body></html>`),
			Figures: []gitsnapshot.Figure{{
				Filename: filename,
				S3Key:    filepath.ToSlash(filepath.Join("figures", recordID, filename)),
				Content:  content,
			}},
			CreatedAt: updatedAt.Add(-time.Hour),
			UpdatedAt: updatedAt,
		}},
	})
}

func assertCommittedFigureFiles(t *testing.T, ctx context.Context, stack *localStack, recordID string, want map[string]string) {
	t.Helper()
	figures, err := stack.Repo.ListRecordFiguresByRecordID(ctx, recordID)
	if err != nil {
		t.Fatalf("ListRecordFiguresByRecordID(%s): %v", recordID, err)
	}
	rows := make(map[string]struct{}, len(figures))
	for _, figure := range figures {
		rows[figure.Filename] = struct{}{}
	}
	diskFiles, err := stack.FS.ListFigureFilenames(recordID)
	if err != nil {
		t.Fatalf("ListFigureFilenames(%s): %v", recordID, err)
	}
	if len(diskFiles) != len(want) || len(rows) != len(want) {
		t.Fatalf("figure row/file counts rows=%v disk=%v want=%v", rows, diskFiles, want)
	}
	for _, filename := range diskFiles {
		if _, ok := rows[filename]; !ok {
			t.Fatalf("on-disk figure %s/%s has no committed row; rows=%v", recordID, filename, rows)
		}
		wantContent, ok := want[filename]
		if !ok {
			t.Fatalf("unexpected on-disk figure %s/%s; want filenames=%v", recordID, filename, want)
		}
		path, err := stack.FS.ResolveFigurePath(recordID, filename)
		if err != nil {
			t.Fatalf("ResolveFigurePath(%s/%s): %v", recordID, filename, err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		if string(got) != wantContent {
			t.Fatalf("figure %s content = %q, want %q", filename, got, wantContent)
		}
	}
}

func TestSnapshotSupportDurabilityErrorPaths(t *testing.T) {
	t.Run("stage raw source write failure cleans staging directory", func(t *testing.T) {
		fsClient, err := filesystem.NewClient(t.TempDir())
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		chatID := "20260514-deadbeef"
		rawKey := "chats/raw/" + chatID + "/source.json"

		originalCreateTempFileFn := createTempFileFn
		t.Cleanup(func() { createTempFileFn = originalCreateTempFileFn })
		createTempFileFn = func(string, string) (atomicTempFile, error) {
			return nil, errors.New("create temp boom")
		}

		stage, err := stageSnapshotChatRawSource(&localStack{FS: fsClient}, gitsnapshot.ChatSession{
			ID:               chatID,
			RawSourceKey:     &rawKey,
			RawSourceContent: []byte("raw"),
		})
		if err == nil || !strings.Contains(err.Error(), "stage chat raw source") {
			t.Fatalf("stageSnapshotChatRawSource() stage=%+v error=%v, want staging write error", stage, err)
		}
		rawRoot := filepath.Join(fsClient.BasePath(), "chats", "raw")
		entries, readErr := os.ReadDir(rawRoot)
		if readErr != nil {
			t.Fatalf("ReadDir(rawRoot) error = %v", readErr)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".staging-") {
				t.Fatalf("staging directory leaked after write failure: %s", entry.Name())
			}
		}
	})

	t.Run("imported figure directory sync failure is surfaced", func(t *testing.T) {
		fsClient, err := filesystem.NewClient(t.TempDir())
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		originalSyncDirFn := syncDirFn
		t.Cleanup(func() { syncDirFn = originalSyncDirFn })
		recordID := "20260514-aabbccdd"
		figureDirSyncs := 0
		syncDirFn = func(dir string) error {
			if dir == filepath.Join(fsClient.BasePath(), "figures", recordID) {
				figureDirSyncs++
			}
			if figureDirSyncs == 2 {
				return errors.New("figure dir sync boom")
			}
			return nil
		}

		_, err = writeImportedRecordFigures(&localStack{FS: fsClient}, gitsnapshot.Record{
			ID: recordID,
			Figures: []gitsnapshot.Figure{{
				Filename: "plot.png",
				S3Key:    "figures/" + recordID + "/plot.png",
				Content:  []byte("plot"),
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "sync local figure directory") {
			t.Fatalf("writeImportedRecordFigures() error = %v, want figure directory sync error", err)
		}
	})

	t.Run("stale figure list and sync failures are surfaced", func(t *testing.T) {
		fsClient, err := filesystem.NewClient(t.TempDir())
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		err = deleteStaleImportedFigures(context.Background(), &localStack{Repo: &mockRepo{}, FS: fsClient}, "../bad")
		if err == nil || !strings.Contains(err.Error(), "list local figures") {
			t.Fatalf("deleteStaleImportedFigures(invalid id) error = %v, want list local figures error", err)
		}

		recordID := "record-1"
		figurePath, err := fsClient.ResolveFigurePath(recordID, "stale.png")
		if err != nil {
			t.Fatalf("ResolveFigurePath() error = %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(figurePath), 0o700); err != nil {
			t.Fatalf("MkdirAll(figure dir) error = %v", err)
		}
		if err := os.WriteFile(figurePath, []byte("stale"), 0o644); err != nil {
			t.Fatalf("WriteFile(stale figure) error = %v", err)
		}
		originalSyncDirFn := syncDirFn
		t.Cleanup(func() { syncDirFn = originalSyncDirFn })
		syncDirFn = func(string) error {
			return errors.New("stale cleanup sync boom")
		}
		err = deleteStaleImportedFigures(context.Background(), &localStack{Repo: &mockRepo{}, FS: fsClient}, recordID)
		if err == nil || !strings.Contains(err.Error(), "sync stale figure cleanup") {
			t.Fatalf("deleteStaleImportedFigures(sync error) error = %v, want cleanup sync error", err)
		}
	})
}

func TestEnsureLocalEnvironmentErrorBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("config path is blocked", func(t *testing.T) {
		homeDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(basePath(homeDir), ".pc", "config.json"), 0o700); err != nil {
			t.Fatalf("create config path blocker: %v", err)
		}
		err := ensureLocalEnvironment(ctx, homeDir)
		if err == nil || !strings.Contains(err.Error(), "write config") {
			t.Fatalf("ensureLocalEnvironment(config blocker) error = %v, want write config error", err)
		}
	})

	t.Run("migration apply failure", func(t *testing.T) {
		originalSQLiteMigrationsFSFn := sqliteMigrationsFSFn
		t.Cleanup(func() { sqliteMigrationsFSFn = originalSQLiteMigrationsFSFn })
		sqliteMigrationsFSFn = func() (fs.FS, error) {
			return fstest.MapFS{
				"001_bad.sql": {Data: []byte("this is not sql")},
			}, nil
		}

		err := ensureLocalEnvironment(ctx, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "apply migrations") {
			t.Fatalf("ensureLocalEnvironment(bad migration) error = %v, want apply migrations error", err)
		}
	})
}

func TestRunExportImportRestoreAndVerifyLocal(t *testing.T) {
	ctx := context.Background()
	sourceHome := setupEnv(t)
	t.Setenv(pcHomeEnvVar, sourceHome)
	withResolvedHomeDir(t, sourceHome)

	originalID := addRecordWithContent(
		t,
		`<html><body><img src="figures/export.png">export</body></html>`,
		"export-notes",
		`{"project_id":"phase7/export","git_remote_url":"https://github.com/org/export","git_hash":"cccccccccccccccccccccccccccccccccccccccc"}`,
		map[string][]byte{"export.png": []byte("export-figure")},
		map[string][]byte{"export.csv": []byte("col1,col2\n5,6\n")},
	)

	exportDir := t.TempDir()
	stdout := &bytes.Buffer{}
	if err := runExport(ctx, stdout, &bytes.Buffer{}, exportOptions{Path: exportDir}); err != nil {
		t.Fatalf("runExport(): %v", err)
	}
	if !strings.Contains(stdout.String(), "Exported 1 records to") {
		t.Fatalf("unexpected export output: %q", stdout.String())
	}

	targetHome := t.TempDir()
	if err := ensureLocalEnvironment(ctx, targetHome); err != nil {
		t.Fatalf("ensureLocalEnvironment(target): %v", err)
	}
	t.Setenv(pcHomeEnvVar, targetHome)
	withResolvedHomeDir(t, targetHome)

	stdout.Reset()
	if err := runImport(ctx, stdout, &bytes.Buffer{}, exportDir); err != nil {
		t.Fatalf("runImport(): %v", err)
	}
	if !strings.Contains(stdout.String(), "Import complete: created 1, updated 0, skipped 0") {
		t.Fatalf("unexpected import output: %q", stdout.String())
	}

	stdout.Reset()
	if err := runVerify(ctx, stdout, &bytes.Buffer{}, false); err != nil {
		t.Fatalf("runVerify(local): %v", err)
	}
	if !strings.Contains(stdout.String(), "Local round-trip verification passed") {
		t.Fatalf("unexpected verify output: %q", stdout.String())
	}

	extraID := addRecordWithContent(
		t,
		"<html><body>extra-record</body></html>",
		"",
		"",
		nil,
		nil,
	)
	staleChatRawPath := filepath.Join(basePath(targetHome), "chats", "raw", "20260514-deadbeef", "source.json")
	if err := os.MkdirAll(filepath.Dir(staleChatRawPath), 0o700); err != nil {
		t.Fatalf("mkdir stale chat raw dir: %v", err)
	}
	if err := os.WriteFile(staleChatRawPath, []byte(`{"id":"stale"}`), 0o600); err != nil {
		t.Fatalf("write stale chat raw source: %v", err)
	}
	stdout.Reset()
	if err := runRestoreDB(ctx, stdout, &bytes.Buffer{}, exportDir); err != nil {
		t.Fatalf("runRestoreDB(): %v", err)
	}
	if !strings.Contains(stdout.String(), "Backup created at ") {
		t.Fatalf("restore-db output missing backup path: %q", stdout.String())
	}

	stack, err := openLocalStack(targetHome)
	if err != nil {
		t.Fatalf("openLocalStack(target after restore): %v", err)
	}
	defer func() { _ = stack.Close() }()
	if _, err := stack.Repo.GetRecordByID(ctx, originalID); err != nil {
		t.Fatalf("GetRecordByID(original): %v", err)
	}
	if _, err := stack.Repo.GetRecordByID(ctx, extraID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("extra record should be removed by restore-db, err = %v", err)
	}
	if _, err := os.Stat(staleChatRawPath); !os.IsNotExist(err) {
		t.Fatalf("stale chat raw source should be removed by restore-db, stat err = %v", err)
	}
}

func TestSnapshotCommandErrorPathsAndHelpers(t *testing.T) {
	ctx := context.Background()
	homeDir := setupEnv(t)
	t.Setenv(pcHomeEnvVar, homeDir)
	withResolvedHomeDir(t, homeDir)

	if err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{}); err == nil {
		t.Fatal("expected runExport to reject empty --path")
	}
	if err := runImport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected runImport to reject missing snapshot path")
	}
	if err := runRestoreDB(ctx, &bytes.Buffer{}, &bytes.Buffer{}, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected runRestoreDB to reject missing snapshot path")
	}

	exportDir := t.TempDir()
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatalf("mkdir export dir: %v", err)
	}
	if err := validateGitRemote(exportDir, "origin"); err == nil {
		t.Fatal("expected validateGitRemote to fail when remote is missing")
	}

	origOpenCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origOpenCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errCloudNotConfigured
	}
	if err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{Path: t.TempDir(), FromCloud: true}); err == nil {
		t.Fatal("expected runExport(fromCloud) to fail without cloud config")
	}
	if err := runVerify(ctx, &bytes.Buffer{}, &bytes.Buffer{}, true); err == nil {
		t.Fatal("expected runVerify(fromCloud) to fail without cloud config")
	}

	firstHome := t.TempDir()
	secondHome := t.TempDir()
	if err := ensureLocalEnvironment(ctx, firstHome); err != nil {
		t.Fatalf("ensureLocalEnvironment(first): %v", err)
	}
	if err := ensureLocalEnvironment(ctx, secondHome); err != nil {
		t.Fatalf("ensureLocalEnvironment(second): %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.WriteFile(dbPath(firstHome)+suffix, []byte("artifact"), 0o600); err != nil {
			t.Fatalf("write db artifact %s: %v", suffix, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(basePath(firstHome), "figures", "x"), 0o755); err != nil {
		t.Fatalf("mkdir figures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(basePath(firstHome), ".pc", "last_sync"), []byte("1"), 0o600); err != nil {
		t.Fatalf("write last_sync: %v", err)
	}
	if err := wipeLocalState(firstHome); err != nil {
		t.Fatalf("wipeLocalState(): %v", err)
	}
	for _, path := range []string{
		dbPath(firstHome),
		dbPath(firstHome) + "-wal",
		dbPath(firstHome) + "-shm",
		dbPath(firstHome) + "-journal",
		filepath.Join(basePath(firstHome), ".pc", "last_sync"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err = %v", path, err)
		}
	}

	snapshot := gitsnapshot.Snapshot{
		Templates: []gitsnapshot.Template{{Name: "text-only", HTMLContent: "<html>a</html>"}},
	}
	manifestA := t.TempDir()
	manifestB := t.TempDir()
	if err := gitsnapshot.Write(manifestA, snapshot); err != nil {
		t.Fatalf("gitsnapshot.Write(manifestA): %v", err)
	}
	if err := gitsnapshot.Write(manifestB, snapshot); err != nil {
		t.Fatalf("gitsnapshot.Write(manifestB): %v", err)
	}
	if err := compareSnapshotDirs(manifestA, manifestB); err != nil {
		t.Fatalf("compareSnapshotDirs(equal): %v", err)
	}
	if err := compareSnapshotDirs(manifestA, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected compareSnapshotDirs to fail when second manifest is missing")
	}
	if err := os.WriteFile(filepath.Join(manifestB, "templates", "text-only.html"), []byte("<html>changed</html>"), 0o644); err != nil {
		t.Fatalf("mutate manifestB: %v", err)
	}
	if err := compareSnapshotDirs(manifestA, manifestB); err == nil {
		t.Fatal("expected compareSnapshotDirs to detect manifest drift")
	}
}

func withResolvedHomeDir(t *testing.T, homeDir string) {
	t.Helper()

	origResolveHomeDirFn := resolveHomeDirFn
	resolveHomeDirFn = func() (string, error) {
		return homeDir, nil
	}
	t.Cleanup(func() {
		resolveHomeDirFn = origResolveHomeDirFn
	})
}

func hashData(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type snapshotRepoStub struct {
	mockRepo
	listTemplatesFn      func(context.Context) ([]repository.Template, error)
	listRecordsFn        func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error)
	listFiguresFn        func(context.Context, string) ([]repository.RecordFigure, error)
	listDataFilesFn      func(context.Context, string) ([]repository.RecordDataFile, error)
	getRecordByIDFn      func(context.Context, string) (repository.Record, error)
	createRecordFn       func(context.Context, repository.CreateRecordInput) (repository.Record, error)
	updateRecordFn       func(context.Context, repository.UpdateRecordInput) (repository.Record, error)
	replaceChildrenFn    func(context.Context, repository.ReplaceRecordChildrenInput) (repository.Record, error)
	deleteRecordFigureFn func(context.Context, int64) error
	deleteRecordDataFn   func(context.Context, int64) error
	createRecordFigureFn func(context.Context, repository.CreateRecordFigureInput) (repository.RecordFigure, error)
	createRecordDataFn   func(context.Context, repository.CreateRecordDataFileInput) (repository.RecordDataFile, error)
}

func (s *snapshotRepoStub) ListTemplates(ctx context.Context) ([]repository.Template, error) {
	if s.listTemplatesFn != nil {
		return s.listTemplatesFn(ctx)
	}
	return s.mockRepo.ListTemplates(ctx)
}

func (s *snapshotRepoStub) ListRecords(ctx context.Context, filter repository.ListRecordsFilter) ([]repository.Record, error) {
	if s.listRecordsFn != nil {
		return s.listRecordsFn(ctx, filter)
	}
	return s.mockRepo.ListRecords(ctx, filter)
}

func (s *snapshotRepoStub) ListRecordFiguresByRecordID(ctx context.Context, recordID string) ([]repository.RecordFigure, error) {
	if s.listFiguresFn != nil {
		return s.listFiguresFn(ctx, recordID)
	}
	return s.mockRepo.ListRecordFiguresByRecordID(ctx, recordID)
}

func (s *snapshotRepoStub) ListRecordDataFilesByRecordID(ctx context.Context, recordID string) ([]repository.RecordDataFile, error) {
	if s.listDataFilesFn != nil {
		return s.listDataFilesFn(ctx, recordID)
	}
	return s.mockRepo.ListRecordDataFilesByRecordID(ctx, recordID)
}

func (s *snapshotRepoStub) GetRecordByID(ctx context.Context, recordID string) (repository.Record, error) {
	if s.getRecordByIDFn != nil {
		return s.getRecordByIDFn(ctx, recordID)
	}
	return s.mockRepo.GetRecordByID(ctx, recordID)
}

func (s *snapshotRepoStub) CreateRecord(ctx context.Context, input repository.CreateRecordInput) (repository.Record, error) {
	if s.createRecordFn != nil {
		return s.createRecordFn(ctx, input)
	}
	return s.mockRepo.CreateRecord(ctx, input)
}

func (s *snapshotRepoStub) UpdateRecord(ctx context.Context, input repository.UpdateRecordInput) (repository.Record, error) {
	if s.updateRecordFn != nil {
		return s.updateRecordFn(ctx, input)
	}
	return s.mockRepo.UpdateRecord(ctx, input)
}

func (s *snapshotRepoStub) ReplaceRecordChildren(ctx context.Context, input repository.ReplaceRecordChildrenInput) (repository.Record, error) {
	if s.replaceChildrenFn != nil {
		return s.replaceChildrenFn(ctx, input)
	}
	return s.mockRepo.ReplaceRecordChildren(ctx, input)
}

func (s *snapshotRepoStub) DeleteRecordFigure(ctx context.Context, id int64) error {
	if s.deleteRecordFigureFn != nil {
		return s.deleteRecordFigureFn(ctx, id)
	}
	return s.mockRepo.DeleteRecordFigure(ctx, id)
}

func (s *snapshotRepoStub) DeleteRecordDataFile(ctx context.Context, id int64) error {
	if s.deleteRecordDataFn != nil {
		return s.deleteRecordDataFn(ctx, id)
	}
	return s.mockRepo.DeleteRecordDataFile(ctx, id)
}

func (s *snapshotRepoStub) CreateRecordFigure(ctx context.Context, input repository.CreateRecordFigureInput) (repository.RecordFigure, error) {
	if s.createRecordFigureFn != nil {
		return s.createRecordFigureFn(ctx, input)
	}
	return s.mockRepo.CreateRecordFigure(ctx, input)
}

func (s *snapshotRepoStub) CreateRecordDataFile(ctx context.Context, input repository.CreateRecordDataFileInput) (repository.RecordDataFile, error) {
	if s.createRecordDataFn != nil {
		return s.createRecordDataFn(ctx, input)
	}
	return s.mockRepo.CreateRecordDataFile(ctx, input)
}

type templateRepoStub struct {
	mockRepo
	updateTemplateFn func(context.Context, repository.UpdateTemplateInput) (repository.Template, error)
}

func (s *templateRepoStub) UpdateTemplate(ctx context.Context, input repository.UpdateTemplateInput) (repository.Template, error) {
	if s.updateTemplateFn != nil {
		return s.updateTemplateFn(ctx, input)
	}
	return s.mockRepo.UpdateTemplate(ctx, input)
}

func TestBuildSnapshotErrorPaths(t *testing.T) {
	ctx := context.Background()
	baseRecord := repository.Record{ID: "20260309-deadbeef"}
	baseFigure := repository.RecordFigure{RecordID: baseRecord.ID, Filename: "plot.png"}

	tests := []struct {
		name              string
		templateRepo      repository.Repository
		recordRepo        repository.Repository
		readFigure        func(context.Context, repository.RecordFigure) ([]byte, error)
		readChatRawSource func(context.Context, repository.ChatSession) ([]byte, error)
		wantSubstring     string
	}{
		{
			name: "list templates",
			templateRepo: &snapshotRepoStub{
				listTemplatesFn: func(context.Context) ([]repository.Template, error) {
					return nil, errors.New("templates failed")
				},
			},
			recordRepo:    &snapshotRepoStub{},
			readFigure:    func(context.Context, repository.RecordFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list templates",
		},
		{
			name:         "list records",
			templateRepo: &snapshotRepoStub{},
			recordRepo: &snapshotRepoStub{
				listRecordsFn: func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error) {
					return nil, errors.New("records failed")
				},
			},
			readFigure:    func(context.Context, repository.RecordFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list records",
		},
		{
			name:         "list projects",
			templateRepo: &snapshotRepoStub{},
			recordRepo: &snapshotRepoStub{mockRepo: mockRepo{
				listProjectsFn: func(context.Context, bool) ([]repository.Project, error) {
					return nil, errors.New("projects failed")
				},
			}},
			readFigure:    func(context.Context, repository.RecordFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list projects",
		},
		{
			name:         "list devices",
			templateRepo: &snapshotRepoStub{},
			recordRepo: &snapshotRepoStub{mockRepo: mockRepo{
				listDevicesFn: func(context.Context, bool) ([]repository.Device, error) {
					return nil, errors.New("devices failed")
				},
			}},
			readFigure:    func(context.Context, repository.RecordFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list devices",
		},
		{
			name:         "list figures",
			templateRepo: &snapshotRepoStub{},
			recordRepo: &snapshotRepoStub{
				listRecordsFn: func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error) {
					return []repository.Record{baseRecord}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
					return nil, errors.New("figures failed")
				},
			},
			readFigure:    func(context.Context, repository.RecordFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list figures",
		},
		{
			name:         "list data files",
			templateRepo: &snapshotRepoStub{},
			recordRepo: &snapshotRepoStub{
				listRecordsFn: func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error) {
					return []repository.Record{baseRecord}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
					return nil, nil
				},
				listDataFilesFn: func(context.Context, string) ([]repository.RecordDataFile, error) {
					return nil, errors.New("data files failed")
				},
			},
			readFigure:    func(context.Context, repository.RecordFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list data files",
		},
		{
			name:         "read figure",
			templateRepo: &snapshotRepoStub{},
			recordRepo: &snapshotRepoStub{
				listRecordsFn: func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error) {
					return []repository.Record{baseRecord}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
					return []repository.RecordFigure{baseFigure}, nil
				},
				listDataFilesFn: func(context.Context, string) ([]repository.RecordDataFile, error) {
					return nil, nil
				},
			},
			readFigure: func(context.Context, repository.RecordFigure) ([]byte, error) {
				return nil, errors.New("figure download failed")
			},
			wantSubstring: "load figure",
		},
		{
			name:         "list chats",
			templateRepo: &snapshotRepoStub{},
			recordRepo: &snapshotRepoStub{mockRepo: mockRepo{
				listChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
					return nil, errors.New("list chats failed")
				},
			}},
			readFigure:    func(context.Context, repository.RecordFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list chats",
		},
		{
			name:         "list chat items",
			templateRepo: &snapshotRepoStub{},
			recordRepo: &snapshotRepoStub{mockRepo: mockRepo{
				listChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
					return []repository.ChatSession{{ID: "20260309-feed0001"}}, nil
				},
				listChatItemsFn: func(context.Context, string) ([]repository.ChatItem, error) {
					return nil, errors.New("list chat items failed")
				},
			}},
			readFigure:    func(context.Context, repository.RecordFigure) ([]byte, error) { return nil, nil },
			wantSubstring: "list chat items",
		},
		{
			name:         "read chat raw source",
			templateRepo: &snapshotRepoStub{},
			recordRepo: &snapshotRepoStub{mockRepo: mockRepo{
				listChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
					rawKey := "chats/raw/20260309-chatbeef/source.json"
					return []repository.ChatSession{{
						ID:           "20260309-chatbeef",
						RawSourceKey: &rawKey,
					}}, nil
				},
			}},
			readFigure: func(context.Context, repository.RecordFigure) ([]byte, error) {
				return nil, nil
			},
			readChatRawSource: func(context.Context, repository.ChatSession) ([]byte, error) {
				return nil, errors.New("chat raw failed")
			},
			wantSubstring: "load chat raw source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readChatRawSource := tt.readChatRawSource
			if readChatRawSource == nil {
				readChatRawSource = func(context.Context, repository.ChatSession) ([]byte, error) {
					return nil, nil
				}
			}
			_, err := buildSnapshot(ctx, tt.templateRepo, tt.recordRepo, tt.readFigure, readChatRawSource, repository.ListRecordsFilter{})
			if err == nil || !strings.Contains(err.Error(), tt.wantSubstring) {
				t.Fatalf("buildSnapshot() error = %v, want substring %q", err, tt.wantSubstring)
			}
		})
	}

	if _, err := buildCloudSnapshot(ctx, filepath.Join(t.TempDir(), "missing-home"), &cloudStack{Repo: &snapshotRepoStub{}}, repository.ListRecordsFilter{}); err == nil {
		t.Fatal("expected buildCloudSnapshot to fail when local template home is missing")
	}
}

func TestImportSnapshotIntoStackErrorPaths(t *testing.T) {
	ctx := context.Background()
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack(): %v", err)
	}
	defer func() { _ = stack.Close() }()

	snapshot := gitsnapshot.Snapshot{
		Records: []gitsnapshot.Record{{
			ID:             "20260309-beadfeed",
			Date:           "2026-03-09",
			DayOrder:       "a",
			ProjectID:      "phase7/test",
			SourceDeviceID: "test-device",
			HTMLContent:    strPtr("<html>snapshot</html>"),
			Figures: []gitsnapshot.Figure{{
				Filename: "plot.png",
				S3Key:    "figures/20260309-beadfeed/plot.png",
				Content:  []byte("plot"),
			}},
			DataFiles: []gitsnapshot.DataFile{{
				Filename: "metrics.csv",
				S3Key:    "data/20260309-beadfeed/metrics.csv",
				Size:     7,
				Hash:     strings.Repeat("a", 64),
			}},
			CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		}},
	}

	tests := []struct {
		name          string
		repo          repository.Repository
		wantSubstring string
	}{
		{
			name: "get record",
			repo: &snapshotRepoStub{
				getRecordByIDFn: func(context.Context, string) (repository.Record, error) {
					return repository.Record{}, errors.New("lookup failed")
				},
			},
			wantSubstring: "get record",
		},
		{
			name: "replace children for new record",
			repo: &snapshotRepoStub{
				getRecordByIDFn: func(context.Context, string) (repository.Record, error) {
					return repository.Record{}, repository.ErrNotFound
				},
				replaceChildrenFn: func(context.Context, repository.ReplaceRecordChildrenInput) (repository.Record, error) {
					return repository.Record{}, errors.New("replace failed")
				},
			},
			wantSubstring: "replace record children rows",
		},
		{
			name: "replace children for existing record",
			repo: &snapshotRepoStub{
				getRecordByIDFn: func(context.Context, string) (repository.Record, error) {
					return repository.Record{ID: "20260309-beadfeed", UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC)}, nil
				},
				replaceChildrenFn: func(context.Context, repository.ReplaceRecordChildrenInput) (repository.Record, error) {
					return repository.Record{}, errors.New("replace failed")
				},
			},
			wantSubstring: "replace record children rows",
		},
		{
			name: "list committed figures after replace",
			repo: &snapshotRepoStub{
				getRecordByIDFn: func(context.Context, string) (repository.Record, error) {
					return repository.Record{ID: "20260309-beadfeed", UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC)}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
					return nil, errors.New("list figures failed")
				},
			},
			wantSubstring: "list committed figures",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origRepo := stack.Repo
			stack.Repo = tt.repo
			defer func() { stack.Repo = origRepo }()

			_, err := importSnapshotIntoStack(ctx, stack, snapshot)
			if err == nil || !strings.Contains(err.Error(), tt.wantSubstring) {
				t.Fatalf("importSnapshotIntoStack() error = %v, want substring %q", err, tt.wantSubstring)
			}
		})
	}
}

func TestImportSnapshotIntoStackChatErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	chatID := "20260309-cafe0001"
	baseChat := gitsnapshot.ChatSession{
		ID:              chatID,
		Source:          "codex",
		SourceSessionID: "snapshot-chat",
		SourceDeviceID:  "test-device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
		Items: []gitsnapshot.ChatItem{{
			Ordinal:    0,
			Role:       "user",
			ItemType:   "message",
			SearchText: "snapshot chat",
			CreatedAt:  now,
		}},
	}

	t.Run("empty raw source content", func(t *testing.T) {
		rawKey := "chats/raw/" + chatID + "/source.json"
		chat := baseChat
		chat.RawSourceKey = &rawKey

		_, err := importSnapshotIntoStack(ctx, &localStack{Repo: &mockRepo{}}, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{chat}})
		if err == nil || !strings.Contains(err.Error(), "raw_source_key is set but raw source content is empty") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want empty raw source error", err)
		}
	})

	t.Run("get chat", func(t *testing.T) {
		repo := &mockRepo{
			getChatByIDFn: func(context.Context, string) (repository.ChatSession, error) {
				return repository.ChatSession{}, errors.New("lookup failed")
			},
		}

		_, err := importSnapshotIntoStack(ctx, &localStack{Repo: repo}, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{baseChat}})
		if err == nil || !strings.Contains(err.Error(), "look up existing chat rollback state") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want rollback lookup error", err)
		}
	})

	t.Run("rollback list chat items", func(t *testing.T) {
		repo := &mockRepo{
			getChatByIDFn: func(context.Context, string) (repository.ChatSession, error) {
				return repository.ChatSession{ID: chatID}, nil
			},
			listChatItemsFn: func(context.Context, string) ([]repository.ChatItem, error) {
				return nil, errors.New("list items failed")
			},
		}

		_, err := importSnapshotIntoStack(ctx, &localStack{Repo: repo}, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{baseChat}})
		if err == nil || !strings.Contains(err.Error(), "list existing chat items for rollback") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want rollback item list error", err)
		}
	})

	t.Run("rollback resolve raw source", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })
		rawKey := "chats/raw/other/source.json"
		stack.Repo = &mockRepo{
			getChatByIDFn: func(context.Context, string) (repository.ChatSession, error) {
				return repository.ChatSession{ID: chatID, RawSourceKey: &rawKey}, nil
			},
			listChatItemsFn: func(context.Context, string) ([]repository.ChatItem, error) {
				return nil, nil
			},
		}

		_, err = importSnapshotIntoStack(ctx, stack, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{baseChat}})
		if err == nil || !strings.Contains(err.Error(), "resolve existing chat raw source for rollback") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want rollback raw resolve error", err)
		}
	})

	t.Run("rollback backup raw source", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })
		rawKey := "chats/raw/" + chatID + "/source.json"
		rawPath, err := stack.FS.ResolveChatSourcePath(chatID, rawKey)
		if err != nil {
			t.Fatalf("ResolveChatSourcePath(): %v", err)
		}
		if err := os.MkdirAll(rawPath, 0o700); err != nil {
			t.Fatalf("create raw source directory blocker: %v", err)
		}
		stack.Repo = &mockRepo{
			getChatByIDFn: func(context.Context, string) (repository.ChatSession, error) {
				return repository.ChatSession{ID: chatID, RawSourceKey: &rawKey}, nil
			},
			listChatItemsFn: func(context.Context, string) ([]repository.ChatItem, error) {
				return nil, nil
			},
		}

		_, err = importSnapshotIntoStack(ctx, stack, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{baseChat}})
		if err == nil || !strings.Contains(err.Error(), "back up existing chat raw source for rollback") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want rollback raw backup error", err)
		}
	})

	t.Run("source identity with different id fails before mutation", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })

		existingID := "20260309-cafe9999"
		createdAt := now.Add(-time.Hour)
		if _, _, err := stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              existingID,
				Source:          baseChat.Source,
				SourceSessionID: baseChat.SourceSessionID,
				SourceDeviceID:  baseChat.SourceDeviceID,
				StartedAt:       createdAt,
				LastActivityAt:  createdAt,
				CreatedAt:       &createdAt,
				UpdatedAt:       &createdAt,
			},
			ClearDeleted: true,
		}); err != nil {
			t.Fatalf("seed existing chat: %v", err)
		}
		if err := replaceChatItems(ctx, stack.Repo, existingID, []repository.CreateChatItemInput{{
			Ordinal:    0,
			Role:       "user",
			ItemType:   "message",
			SearchText: "existing item",
			CreatedAt:  &createdAt,
		}}); err != nil {
			t.Fatalf("seed existing chat item: %v", err)
		}

		_, err = importSnapshotIntoStack(ctx, stack, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{baseChat}})
		if err == nil || !strings.Contains(err.Error(), "already exists with id "+existingID) {
			t.Fatalf("importSnapshotIntoStack() error = %v, want source identity conflict", err)
		}
		if _, err := stack.Repo.GetChatSessionByID(ctx, chatID); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("snapshot chat id should not be created, err = %v", err)
		}
		items, err := stack.Repo.ListChatItems(ctx, existingID)
		if err != nil {
			t.Fatalf("ListChatItems(existing): %v", err)
		}
		if len(items) != 1 || items[0].SearchText != "existing item" {
			t.Fatalf("existing chat items should remain unchanged, got %+v", items)
		}
	})

	t.Run("duplicate source identity in snapshot fails before mutation", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })

		duplicateChat := baseChat
		duplicateChat.ID = "20260309-cafe0002"
		templateName := "duplicate-source-template"
		_, err = importSnapshotIntoStack(ctx, stack, gitsnapshot.Snapshot{
			Templates: []gitsnapshot.Template{{Name: templateName, HTMLContent: "<html>duplicate</html>"}},
			Chats:     []gitsnapshot.ChatSession{baseChat, duplicateChat},
		})
		if err == nil || !strings.Contains(err.Error(), "appears multiple times in snapshot") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want duplicate source identity error", err)
		}
		if _, err := stack.Repo.GetTemplateByName(ctx, templateName); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("template should not be created before duplicate chat preflight, err = %v", err)
		}
		if _, err := stack.Repo.GetChatSessionByID(ctx, baseChat.ID); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("first duplicate chat should not be created, err = %v", err)
		}
		if _, err := stack.Repo.GetChatSessionByID(ctx, duplicateChat.ID); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("second duplicate chat should not be created, err = %v", err)
		}
	})

	t.Run("invalid raw source key", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })

		rawKey := "chats/raw/other/source.json"
		chat := baseChat
		chat.RawSourceKey = &rawKey
		chat.RawSourceContent = []byte("raw")

		_, err = importSnapshotIntoStack(ctx, stack, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{chat}})
		if err == nil || !strings.Contains(err.Error(), "resolve chat raw source") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want raw source resolve error", err)
		}
	})

	t.Run("stage raw source", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })

		rawKey := "chats/raw/" + chatID + "/source.json"
		chat := baseChat
		chat.RawSourceKey = &rawKey
		chat.RawSourceContent = []byte("raw")
		blockerPath := filepath.Join(basePath(homeDir), "chats", "raw")
		if err := os.MkdirAll(filepath.Dir(blockerPath), 0o700); err != nil {
			t.Fatalf("create raw parent: %v", err)
		}
		if err := os.WriteFile(blockerPath, []byte("blocker"), 0o600); err != nil {
			t.Fatalf("write blocker: %v", err)
		}

		_, err = importSnapshotIntoStack(ctx, stack, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{chat}})
		if err == nil || !strings.Contains(err.Error(), "create chat raw staging root") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want raw source stage error", err)
		}
	})

	t.Run("create raw stage", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })

		rawRoot := filepath.Join(basePath(homeDir), "chats", "raw")
		if err := os.MkdirAll(rawRoot, 0o700); err != nil {
			t.Fatalf("create raw root: %v", err)
		}
		if err := os.Chmod(rawRoot, 0o500); err != nil {
			t.Fatalf("chmod raw root: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(rawRoot, 0o700) })

		rawKey := "chats/raw/" + chatID + "/source.json"
		chat := baseChat
		chat.RawSourceKey = &rawKey
		chat.RawSourceContent = []byte("raw")

		_, err = importSnapshotIntoStack(ctx, stack, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{chat}})
		if err == nil || !strings.Contains(err.Error(), "create chat raw stage") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want raw stage creation error", err)
		}
	})

	t.Run("upsert chat", func(t *testing.T) {
		repo := &mockRepo{
			upsertChatSessionFn: func(context.Context, repository.UpsertChatSessionInput) (repository.ChatSession, bool, error) {
				return repository.ChatSession{}, false, errors.New("upsert failed")
			},
		}

		_, err := importSnapshotIntoStack(ctx, &localStack{Repo: repo}, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{baseChat}})
		if err == nil || !strings.Contains(err.Error(), "upsert chat") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want upsert chat error", err)
		}
	})

	t.Run("replace chat items", func(t *testing.T) {
		repo := &mockRepo{
			upsertChatSessionFn: func(context.Context, repository.UpsertChatSessionInput) (repository.ChatSession, bool, error) {
				return repository.ChatSession{ID: chatID}, true, nil
			},
			replaceChatItemsFn: func(context.Context, string, []repository.CreateChatItemInput) error {
				return errors.New("replace failed")
			},
		}

		_, err := importSnapshotIntoStack(ctx, &localStack{Repo: repo}, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{baseChat}})
		if err == nil || !strings.Contains(err.Error(), "replace chat items") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want replace chat items error", err)
		}
	})

	t.Run("replace chat items failure does not publish new raw source", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })

		rawKey := "chats/raw/" + chatID + "/source.json"
		chat := baseChat
		chat.RawSourceKey = &rawKey
		chat.RawSourceContent = []byte(`{"id":"new"}`)
		chat.Items = []gitsnapshot.ChatItem{{
			Ordinal:   -1,
			Role:      "user",
			ItemType:  "message",
			CreatedAt: now,
		}}

		_, err = importSnapshotIntoStack(ctx, stack, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{chat}})
		if err == nil || !strings.Contains(err.Error(), "replace chat items") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want replace chat items error", err)
		}
		if _, err := stack.Repo.GetChatSessionByID(ctx, chatID); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("new chat row should be rolled back after item failure, err = %v", err)
		}
		rawPath, err := stack.FS.ResolveChatSourcePath(chatID, rawKey)
		if err != nil {
			t.Fatalf("ResolveChatSourcePath(): %v", err)
		}
		if _, err := os.Stat(rawPath); !os.IsNotExist(err) {
			t.Fatalf("new raw source should not be published after item failure, stat err = %v", err)
		}
	})

	t.Run("existing raw source survives replacement failure", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })

		rawKey := "chats/raw/" + chatID + "/source.json"
		createdAt := now.Add(-time.Hour)
		if _, _, err := stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              chatID,
				Source:          baseChat.Source,
				SourceSessionID: baseChat.SourceSessionID,
				SourceDeviceID:  baseChat.SourceDeviceID,
				StartedAt:       createdAt,
				LastActivityAt:  createdAt,
				RawSourceKey:    &rawKey,
				CreatedAt:       &createdAt,
				UpdatedAt:       &createdAt,
			},
			ClearDeleted: true,
		}); err != nil {
			t.Fatalf("seed existing chat: %v", err)
		}
		rawPath, err := stack.FS.ResolveChatSourcePath(chatID, rawKey)
		if err != nil {
			t.Fatalf("ResolveChatSourcePath(existing): %v", err)
		}
		if err := writeTextFileAtomically(rawPath, []byte(`{"id":"old"}`), 0o700, 0o600); err != nil {
			t.Fatalf("write existing raw source: %v", err)
		}

		chat := baseChat
		chat.RawSourceKey = &rawKey
		chat.RawSourceContent = []byte(`{"id":"new"}`)
		chat.Items = []gitsnapshot.ChatItem{{
			Ordinal:   -1,
			Role:      "user",
			ItemType:  "message",
			CreatedAt: now,
		}}

		_, err = importSnapshotIntoStack(ctx, stack, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{chat}})
		if err == nil || !strings.Contains(err.Error(), "replace chat items") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want replace chat items error", err)
		}
		got, err := os.ReadFile(rawPath)
		if err != nil {
			t.Fatalf("read existing raw source: %v", err)
		}
		if string(got) != `{"id":"old"}` {
			t.Fatalf("existing raw source should survive replacement failure, got %q", got)
		}
		restored, err := stack.Repo.GetChatSessionByID(ctx, chatID)
		if err != nil {
			t.Fatalf("existing chat should be restored after failure: %v", err)
		}
		if !restored.UpdatedAt.Equal(createdAt) {
			t.Fatalf("existing chat updated_at should be restored, got %s want %s", restored.UpdatedAt, createdAt)
		}
	})

	t.Run("promote raw source failure rolls back chat", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })
		rawRoot := filepath.Join(basePath(homeDir), "chats", "raw")
		t.Cleanup(func() { _ = os.Chmod(rawRoot, 0o700) })

		rawKey := "chats/raw/" + chatID + "/source.json"
		chat := baseChat
		chat.RawSourceKey = &rawKey
		chat.RawSourceContent = []byte(`{"id":"new"}`)
		stack.Repo = &mockRepo{
			upsertChatSessionFn: func(context.Context, repository.UpsertChatSessionInput) (repository.ChatSession, bool, error) {
				return repository.ChatSession{ID: chatID}, true, nil
			},
			replaceChatItemsFn: func(context.Context, string, []repository.CreateChatItemInput) error {
				if err := os.Chmod(rawRoot, 0o500); err != nil {
					return err
				}
				return nil
			},
		}

		_, err = importSnapshotIntoStack(ctx, stack, gitsnapshot.Snapshot{Chats: []gitsnapshot.ChatSession{chat}})
		if err == nil || !strings.Contains(err.Error(), "promote chat raw source") {
			t.Fatalf("importSnapshotIntoStack() error = %v, want promote failure", err)
		}
	})
}

func TestEnsureSnapshotChatSourceIdentity(t *testing.T) {
	ctx := context.Background()
	chat := gitsnapshot.ChatSession{
		ID:              "20260309-cafe0001",
		Source:          "codex",
		SourceSessionID: "source-session",
	}

	if err := ensureSnapshotChatSourceIdentity(ctx, &mockRepo{}, chat); err != nil {
		t.Fatalf("ensureSnapshotChatSourceIdentity(not found) error = %v", err)
	}

	sameIDRepo := &mockRepo{
		getChatBySourceFn: func(context.Context, string, string) (repository.ChatSession, error) {
			return repository.ChatSession{ID: chat.ID}, nil
		},
	}
	if err := ensureSnapshotChatSourceIdentity(ctx, sameIDRepo, chat); err != nil {
		t.Fatalf("ensureSnapshotChatSourceIdentity(same id) error = %v", err)
	}

	errorRepo := &mockRepo{
		getChatBySourceFn: func(context.Context, string, string) (repository.ChatSession, error) {
			return repository.ChatSession{}, errors.New("source lookup failed")
		},
	}
	if err := ensureSnapshotChatSourceIdentity(ctx, errorRepo, chat); err == nil || !strings.Contains(err.Error(), "load chat source") {
		t.Fatalf("ensureSnapshotChatSourceIdentity(error) = %v, want load chat source error", err)
	}
}

func TestReplaceRecordChildrenPathError(t *testing.T) {
	ctx := context.Background()
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack(): %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })

	err = replaceRecordChildren(ctx, stack, gitsnapshot.Record{
		ID: "20260309-badf100d",
		Figures: []gitsnapshot.Figure{{
			Filename: "../bad.png",
			S3Key:    "figures/20260309-badf100d/../bad.png",
			Content:  []byte("bad"),
		}},
	})
	if err == nil {
		t.Fatal("expected invalid figure path to fail")
	}
}

func TestPhase7CommandAdditionalErrorPaths(t *testing.T) {
	ctx := context.Background()
	snapshotDir := t.TempDir()
	if err := gitsnapshot.Write(snapshotDir, gitsnapshot.Snapshot{}); err != nil {
		t.Fatalf("gitsnapshot.Write(snapshotDir): %v", err)
	}

	t.Run("resolveHomeDir failures", func(t *testing.T) {
		origResolveHomeDirFn := resolveHomeDirFn
		resolveHomeDirFn = func() (string, error) {
			return "", errors.New("resolve failed")
		}
		t.Cleanup(func() {
			resolveHomeDirFn = origResolveHomeDirFn
		})

		if err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{Path: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("runExport() error = %v, want resolve failure", err)
		}
		if err := runImport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir); err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("runImport() error = %v, want resolve failure", err)
		}
		if err := runRestoreDB(ctx, &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir); err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("runRestoreDB() error = %v, want resolve failure", err)
		}
		if err := runVerify(ctx, &bytes.Buffer{}, &bytes.Buffer{}, false); err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("runVerify() error = %v, want resolve failure", err)
		}
	})

	t.Run("cloud open failures are wrapped", func(t *testing.T) {
		origResolveHomeDirFn := resolveHomeDirFn
		origOpenCloudStackFn := openCloudStackFn
		resolveHomeDirFn = func() (string, error) {
			return t.TempDir(), nil
		}
		openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
			return nil, errors.New("cloud dial failed")
		}
		t.Cleanup(func() {
			resolveHomeDirFn = origResolveHomeDirFn
			openCloudStackFn = origOpenCloudStackFn
		})

		if err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{Path: t.TempDir(), FromCloud: true}); err == nil || !strings.Contains(err.Error(), "open cloud") {
			t.Fatalf("runExport(fromCloud) error = %v, want wrapped cloud failure", err)
		}
		if err := runVerify(ctx, &bytes.Buffer{}, &bytes.Buffer{}, true); err == nil || !strings.Contains(err.Error(), "open cloud") {
			t.Fatalf("runVerify(fromCloud) error = %v, want wrapped cloud failure", err)
		}
	})

	t.Run("local stack open failures are surfaced", func(t *testing.T) {
		origResolveHomeDirFn := resolveHomeDirFn
		resolveHomeDirFn = func() (string, error) {
			return t.TempDir(), nil
		}
		t.Cleanup(func() {
			resolveHomeDirFn = origResolveHomeDirFn
		})

		if err := runImport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir); err == nil || !strings.Contains(err.Error(), "read config") {
			t.Fatalf("runImport() error = %v, want local stack failure", err)
		}
		if err := runVerify(ctx, &bytes.Buffer{}, &bytes.Buffer{}, false); err == nil || !strings.Contains(err.Error(), "read config") {
			t.Fatalf("runVerify() error = %v, want local stack failure", err)
		}
	})

	t.Run("export write failures are wrapped", func(t *testing.T) {
		homeDir := setupEnv(t)
		t.Setenv(pcHomeEnvVar, homeDir)
		withResolvedHomeDir(t, homeDir)

		blockedPath := filepath.Join(t.TempDir(), "snapshot-root")
		if err := os.WriteFile(blockedPath, []byte("not-a-directory"), 0o644); err != nil {
			t.Fatalf("write blocked path: %v", err)
		}

		err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{Path: blockedPath})
		if err == nil || !strings.Contains(err.Error(), "write export snapshot") {
			t.Fatalf("runExport() error = %v, want wrapped write failure", err)
		}
	})

	t.Run("restore backup write failures are wrapped", func(t *testing.T) {
		homeDir := setupEnv(t)
		t.Setenv(pcHomeEnvVar, homeDir)
		withResolvedHomeDir(t, homeDir)

		if err := os.WriteFile(filepath.Join(basePath(homeDir), ".pc", "backups"), []byte("blocked"), 0o644); err != nil {
			t.Fatalf("write backup blocker: %v", err)
		}

		err := runRestoreDB(ctx, &bytes.Buffer{}, &bytes.Buffer{}, snapshotDir)
		if err == nil || !strings.Contains(err.Error(), "write restore backup") {
			t.Fatalf("runRestoreDB() error = %v, want wrapped backup write failure", err)
		}
	})

	t.Run("cloud figure download failures bubble through commands", func(t *testing.T) {
		homeDir := setupEnv(t)
		t.Setenv(pcHomeEnvVar, homeDir)
		withResolvedHomeDir(t, homeDir)

		record := repository.Record{
			ID:             "20260309-clouddead",
			Date:           "2026-03-09",
			DayOrder:       "a0",
			ProjectID:      "phase7/test",
			SourceDeviceID: "test-device",
			HTMLContent:    strPtr(`<html><body><img src="figures/cloud.png"></body></html>`),
			CreatedAt:      time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			UpdatedAt:      time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		}
		figure := repository.RecordFigure{
			RecordID: record.ID,
			Filename: "cloud.png",
			S3Key:    "figures/20260309-clouddead/cloud.png",
		}
		repo := &snapshotRepoStub{
			listRecordsFn: func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error) {
				return []repository.Record{record}, nil
			},
			listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
				return []repository.RecordFigure{figure}, nil
			},
			listDataFilesFn: func(context.Context, string) ([]repository.RecordDataFile, error) {
				return nil, nil
			},
		}

		origOpenCloudStackFn := openCloudStackFn
		origDownloadCloudFigureFn := downloadCloudFigureFn
		openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
			return &cloudStack{Repo: repo}, nil
		}
		downloadCloudFigureFn = func(context.Context, *cloudStack, string) (io.ReadCloser, error) {
			return nil, errors.New("download failed")
		}
		t.Cleanup(func() {
			openCloudStackFn = origOpenCloudStackFn
			downloadCloudFigureFn = origDownloadCloudFigureFn
		})

		if err := runExport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, exportOptions{Path: t.TempDir(), FromCloud: true}); err == nil || !strings.Contains(err.Error(), "load figure") {
			t.Fatalf("runExport(fromCloud) error = %v, want figure download failure", err)
		}
		if err := runVerify(ctx, &bytes.Buffer{}, &bytes.Buffer{}, true); err == nil || !strings.Contains(err.Error(), "load figure") {
			t.Fatalf("runVerify(fromCloud) error = %v, want figure download failure", err)
		}
	})
}

func TestSnapshotSupportAdditionalHelperPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("buildLocalSnapshot missing figure content", func(t *testing.T) {
		homeDir := setupEnv(t)
		recordID := addRecordWithContent(
			t,
			`<html><body><img src="figures/missing.png">broken</body></html>`,
			"",
			"",
			map[string][]byte{"missing.png": []byte("figure")},
			nil,
		)
		if err := os.Remove(filepath.Join(basePath(homeDir), "figures", recordID, "missing.png")); err != nil {
			t.Fatalf("remove local figure: %v", err)
		}

		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		defer func() { _ = stack.Close() }()

		if _, err := buildLocalSnapshot(ctx, stack, repository.ListRecordsFilter{}); err == nil || !strings.Contains(err.Error(), "read local figure") {
			t.Fatalf("buildLocalSnapshot() error = %v, want local figure read failure", err)
		}
	})

	t.Run("buildLocalSnapshot missing chat raw source", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })

		chatID := "20260309-face0001"
		rawKey := "chats/raw/" + chatID + "/source.json"
		now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
		if _, _, err := stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              chatID,
				Source:          "codex",
				SourceSessionID: "missing-local-raw",
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

		if _, err := buildLocalSnapshot(ctx, stack, repository.ListRecordsFilter{}); err == nil || !strings.Contains(err.Error(), "read local chat raw source") {
			t.Fatalf("buildLocalSnapshot() error = %v, want local chat raw source failure", err)
		}
	})

	t.Run("buildLocalSnapshot invalid figure path", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })
		stack.Repo = &snapshotRepoStub{mockRepo: mockRepo{
			listRecordsFn: func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error) {
				return []repository.Record{{ID: "20260309-badf00d", UpdatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)}}, nil
			},
			listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
				return []repository.RecordFigure{{RecordID: "20260309-badf00d", Filename: "../bad.png"}}, nil
			},
			listDataFilesFn: func(context.Context, string) ([]repository.RecordDataFile, error) {
				return nil, nil
			},
		}}

		if _, err := buildLocalSnapshot(ctx, stack, repository.ListRecordsFilter{}); err == nil || !strings.Contains(err.Error(), "load figure") {
			t.Fatalf("buildLocalSnapshot() error = %v, want figure path failure", err)
		}
	})

	t.Run("buildLocalSnapshot invalid chat raw source key", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })
		rawKey := "chats/raw/other/source.json"
		stack.Repo = &snapshotRepoStub{mockRepo: mockRepo{
			listChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
				return []repository.ChatSession{{ID: "20260309-chatbeef", RawSourceKey: &rawKey}}, nil
			},
		}}

		if _, err := buildLocalSnapshot(ctx, stack, repository.ListRecordsFilter{}); err == nil || !strings.Contains(err.Error(), "load chat raw source") {
			t.Fatalf("buildLocalSnapshot() error = %v, want chat raw source failure", err)
		}
	})

	t.Run("buildLocalSnapshot chat without raw source", func(t *testing.T) {
		homeDir := setupEnv(t)
		stack, err := openLocalStack(homeDir)
		if err != nil {
			t.Fatalf("openLocalStack(): %v", err)
		}
		t.Cleanup(func() { _ = stack.Close() })

		now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
		if _, _, err := stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              "20260309-face0002",
				Source:          "codex",
				SourceSessionID: "no-local-raw",
				SourceDeviceID:  "test-device",
				StartedAt:       now,
				LastActivityAt:  now,
				CreatedAt:       &now,
				UpdatedAt:       &now,
			},
			ClearDeleted: true,
		}); err != nil {
			t.Fatalf("seed chat session: %v", err)
		}

		snapshot, err := buildLocalSnapshot(ctx, stack, repository.ListRecordsFilter{})
		if err != nil {
			t.Fatalf("buildLocalSnapshot(): %v", err)
		}
		if len(snapshot.Chats) != 1 || snapshot.Chats[0].RawSourceContent != nil {
			t.Fatalf("unexpected local chat snapshot: %+v", snapshot.Chats)
		}
	})

	t.Run("buildCloudSnapshot downloads figure content", func(t *testing.T) {
		homeDir := setupEnv(t)
		record := repository.Record{
			ID:             "20260309-cloudbeef",
			Date:           "2026-03-09",
			DayOrder:       "a0",
			ProjectID:      "phase7/test",
			SourceDeviceID: "test-device",
			HTMLContent:    strPtr(`<html><body><img src="figures/cloud.png"></body></html>`),
			CreatedAt:      time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			UpdatedAt:      time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		}
		figure := repository.RecordFigure{
			RecordID: record.ID,
			Filename: "cloud.png",
			S3Key:    "figures/20260309-cloudbeef/cloud.png",
		}
		s3 := newTestS3Client(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, figure.S3Key) {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, "cloud-bytes")
		}))

		snapshot, err := buildCloudSnapshot(ctx, homeDir, &cloudStack{
			Repo: &snapshotRepoStub{
				listRecordsFn: func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error) {
					return []repository.Record{record}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
					return []repository.RecordFigure{figure}, nil
				},
				listDataFilesFn: func(context.Context, string) ([]repository.RecordDataFile, error) {
					return nil, nil
				},
			},
			S3: s3,
		}, repository.ListRecordsFilter{})
		if err != nil {
			t.Fatalf("buildCloudSnapshot(): %v", err)
		}
		if got := string(snapshot.Records[0].Figures[0].Content); got != "cloud-bytes" {
			t.Fatalf("cloud figure content = %q, want %q", got, "cloud-bytes")
		}
	})

	t.Run("buildCloudSnapshot read figure content", func(t *testing.T) {
		homeDir := setupEnv(t)
		record := repository.Record{
			ID:             "20260309-cloudf00d",
			Date:           "2026-03-09",
			DayOrder:       "a0",
			ProjectID:      "phase7/test",
			SourceDeviceID: "test-device",
			CreatedAt:      time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			UpdatedAt:      time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		}
		figure := repository.RecordFigure{RecordID: record.ID, Filename: "cloud.png", S3Key: "figures/20260309-cloudf00d/cloud.png"}
		origDownloadCloudFigureFn := downloadCloudFigureFn
		downloadCloudFigureFn = func(context.Context, *cloudStack, string) (io.ReadCloser, error) {
			return io.NopCloser(iotest.ErrReader(errors.New("read failed"))), nil
		}
		t.Cleanup(func() { downloadCloudFigureFn = origDownloadCloudFigureFn })

		_, err := buildCloudSnapshot(ctx, homeDir, &cloudStack{
			Repo: &snapshotRepoStub{
				listRecordsFn: func(context.Context, repository.ListRecordsFilter) ([]repository.Record, error) {
					return []repository.Record{record}, nil
				},
				listFiguresFn: func(context.Context, string) ([]repository.RecordFigure, error) {
					return []repository.RecordFigure{figure}, nil
				},
				listDataFilesFn: func(context.Context, string) ([]repository.RecordDataFile, error) {
					return nil, nil
				},
			},
		}, repository.ListRecordsFilter{})
		if err == nil || !strings.Contains(err.Error(), "read cloud figure") {
			t.Fatalf("buildCloudSnapshot() error = %v, want cloud figure read error", err)
		}
	})

	t.Run("buildCloudSnapshot requires S3 for chat raw source", func(t *testing.T) {
		homeDir := setupEnv(t)
		chatID := "20260309-cloudchat"
		rawKey := "chats/raw/" + chatID + "/source.json"

		_, err := buildCloudSnapshot(ctx, homeDir, &cloudStack{
			Repo: &snapshotRepoStub{mockRepo: mockRepo{
				listChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
					return []repository.ChatSession{{
						ID:              chatID,
						Source:          "codex",
						SourceSessionID: "cloud-chat",
						RawSourceKey:    &rawKey,
					}}, nil
				},
			}},
		}, repository.ListRecordsFilter{})
		if err == nil || !strings.Contains(err.Error(), "cloud S3 client is required") {
			t.Fatalf("buildCloudSnapshot() error = %v, want missing S3 error", err)
		}
	})

	t.Run("buildCloudSnapshot chat without raw source", func(t *testing.T) {
		homeDir := setupEnv(t)
		chatID := "20260309-cloudnil"

		snapshot, err := buildCloudSnapshot(ctx, homeDir, &cloudStack{
			Repo: &snapshotRepoStub{mockRepo: mockRepo{
				listChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
					return []repository.ChatSession{{
						ID:              chatID,
						Source:          "codex",
						SourceSessionID: "cloud-chat-no-raw",
					}}, nil
				},
			}},
		}, repository.ListRecordsFilter{})
		if err != nil {
			t.Fatalf("buildCloudSnapshot(): %v", err)
		}
		if len(snapshot.Chats) != 1 || snapshot.Chats[0].RawSourceContent != nil {
			t.Fatalf("unexpected cloud chat snapshot: %+v", snapshot.Chats)
		}
	})

	t.Run("buildCloudSnapshot reports chat raw download error", func(t *testing.T) {
		homeDir := setupEnv(t)
		chatID := "20260309-clouderr"
		rawKey := "chats/raw/" + chatID + "/source.json"
		s3 := newTestS3Client(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))

		_, err := buildCloudSnapshot(ctx, homeDir, &cloudStack{
			Repo: &snapshotRepoStub{mockRepo: mockRepo{
				listChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
					return []repository.ChatSession{{
						ID:              chatID,
						Source:          "codex",
						SourceSessionID: "cloud-chat-error",
						RawSourceKey:    &rawKey,
					}}, nil
				},
			}},
			S3: s3,
		}, repository.ListRecordsFilter{})
		if err == nil || !strings.Contains(err.Error(), "load chat raw source") {
			t.Fatalf("buildCloudSnapshot() error = %v, want chat raw download error", err)
		}
	})

	t.Run("buildCloudSnapshot downloads chat raw source", func(t *testing.T) {
		homeDir := setupEnv(t)
		chatID := "20260309-cloudraw"
		rawKey := "chats/raw/" + chatID + "/source.json"
		s3 := newTestS3Client(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, rawKey) {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, `{"id":"cloud-chat"}`)
		}))

		snapshot, err := buildCloudSnapshot(ctx, homeDir, &cloudStack{
			Repo: &snapshotRepoStub{mockRepo: mockRepo{
				listChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
					return []repository.ChatSession{{
						ID:              chatID,
						Source:          "codex",
						SourceSessionID: "cloud-chat",
						RawSourceKey:    &rawKey,
					}}, nil
				},
			}},
			S3: s3,
		}, repository.ListRecordsFilter{})
		if err != nil {
			t.Fatalf("buildCloudSnapshot(): %v", err)
		}
		if len(snapshot.Chats) != 1 || string(snapshot.Chats[0].RawSourceContent) != `{"id":"cloud-chat"}` {
			t.Fatalf("unexpected chat raw source content: %+v", snapshot.Chats)
		}
	})

	t.Run("upsertTemplate variants", func(t *testing.T) {
		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{}, errors.New("lookup failed")
				},
			},
		}, gitsnapshot.Template{Name: "x", HTMLContent: "<html>x</html>"}); err == nil || !strings.Contains(err.Error(), "get template") {
			t.Fatalf("upsertTemplate(get error) = %v, want get template failure", err)
		}

		created := false
		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{}, repository.ErrNotFound
				},
				createTemplateFn: func(context.Context, repository.CreateTemplateInput) (repository.Template, error) {
					created = true
					return repository.Template{Name: "new-template"}, nil
				},
			},
		}, gitsnapshot.Template{Name: "new-template", HTMLContent: "<html>new</html>"}); err != nil {
			t.Fatalf("upsertTemplate(create): %v", err)
		}
		if !created {
			t.Fatal("expected create template path to run")
		}

		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{}, repository.ErrNotFound
				},
				createTemplateFn: func(context.Context, repository.CreateTemplateInput) (repository.Template, error) {
					return repository.Template{}, errors.New("create failed")
				},
			},
		}, gitsnapshot.Template{Name: "create-fail", HTMLContent: "<html>new</html>"}); err == nil || !strings.Contains(err.Error(), "create template") {
			t.Fatalf("upsertTemplate(create error) = %v, want create failure", err)
		}

		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{Name: "same", HTMLContent: "<html>same</html>"}, nil
				},
			},
			updateTemplateFn: func(context.Context, repository.UpdateTemplateInput) (repository.Template, error) {
				t.Fatal("UpdateTemplate should not run for identical HTML")
				return repository.Template{}, nil
			},
		}, gitsnapshot.Template{Name: "same", HTMLContent: "<html>same</html>"}); err != nil {
			t.Fatalf("upsertTemplate(no-op): %v", err)
		}

		updated := false
		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{Name: "update", HTMLContent: "<html>old</html>"}, nil
				},
			},
			updateTemplateFn: func(_ context.Context, input repository.UpdateTemplateInput) (repository.Template, error) {
				updated = input.HTMLContent == "<html>new</html>"
				return repository.Template{Name: input.Name, HTMLContent: input.HTMLContent}, nil
			},
		}, gitsnapshot.Template{Name: "update", HTMLContent: "<html>new</html>"}); err != nil {
			t.Fatalf("upsertTemplate(update): %v", err)
		}
		if !updated {
			t.Fatal("expected update template path to run")
		}

		if err := upsertTemplate(ctx, &templateRepoStub{
			mockRepo: mockRepo{
				getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
					return repository.Template{Name: "update-fail", HTMLContent: "<html>old</html>"}, nil
				},
			},
			updateTemplateFn: func(context.Context, repository.UpdateTemplateInput) (repository.Template, error) {
				return repository.Template{}, errors.New("update failed")
			},
		}, gitsnapshot.Template{Name: "update-fail", HTMLContent: "<html>new</html>"}); err == nil || !strings.Contains(err.Error(), "update template") {
			t.Fatalf("upsertTemplate(update error) = %v, want update failure", err)
		}
	})

	t.Run("import registry and record lookup errors", func(t *testing.T) {
		projectErr := errors.New("project upsert failed")
		_, err := importSnapshotIntoStack(ctx, &localStack{Repo: &mockRepo{
			upsertProjectFn: func(context.Context, repository.Project) (bool, error) {
				return false, projectErr
			},
		}}, gitsnapshot.Snapshot{
			Projects: []gitsnapshot.RegistryEntry{{ID: "project/a"}},
		})
		if !errors.Is(err, projectErr) || !strings.Contains(err.Error(), "upsert project") {
			t.Fatalf("importSnapshotIntoStack(project error) = %v", err)
		}

		deviceErr := errors.New("device upsert failed")
		_, err = importSnapshotIntoStack(ctx, &localStack{Repo: &mockRepo{
			upsertDeviceFn: func(context.Context, repository.Device) (bool, error) {
				return false, deviceErr
			},
		}}, gitsnapshot.Snapshot{
			Devices: []gitsnapshot.RegistryEntry{{ID: "device/a"}},
		})
		if !errors.Is(err, deviceErr) || !strings.Contains(err.Error(), "upsert device") {
			t.Fatalf("importSnapshotIntoStack(device error) = %v", err)
		}

		getRecordErr := errors.New("record lookup failed")
		_, err = importSnapshotIntoStack(ctx, &localStack{Repo: &mockRepo{
			getRecordByIDFn: func(context.Context, string) (repository.Record, error) {
				return repository.Record{}, getRecordErr
			},
		}}, gitsnapshot.Snapshot{
			Records: []gitsnapshot.Record{{ID: "record-a"}},
		})
		if !errors.Is(err, getRecordErr) || !strings.Contains(err.Error(), "get record") {
			t.Fatalf("importSnapshotIntoStack(record lookup error) = %v", err)
		}
	})

	t.Run("ensureLocalEnvironment error paths", func(t *testing.T) {
		homeDir := t.TempDir()

		t.Run("config store", func(t *testing.T) {
			origNewConfigStoreFn := newConfigStoreFn
			newConfigStoreFn = func(string) (config.Store, error) {
				return config.Store{}, errors.New("store failed")
			}
			t.Cleanup(func() { newConfigStoreFn = origNewConfigStoreFn })

			if err := ensureLocalEnvironment(ctx, homeDir); err == nil || !strings.Contains(err.Error(), "create config store") {
				t.Fatalf("ensureLocalEnvironment() error = %v, want config store failure", err)
			}
		})

		t.Run("open sqlite", func(t *testing.T) {
			origOpenSQLiteFn := openSQLiteFn
			openSQLiteFn = func(string) (*sqlite.Connection, error) {
				return nil, errors.New("sqlite open failed")
			}
			t.Cleanup(func() { openSQLiteFn = origOpenSQLiteFn })

			if err := ensureLocalEnvironment(ctx, homeDir); err == nil || !strings.Contains(err.Error(), "open database") {
				t.Fatalf("ensureLocalEnvironment() error = %v, want sqlite open failure", err)
			}
		})

		t.Run("load migrations", func(t *testing.T) {
			origSQLiteMigrationsFSFn := sqliteMigrationsFSFn
			sqliteMigrationsFSFn = func() (fs.FS, error) {
				return nil, errors.New("migrations failed")
			}
			t.Cleanup(func() { sqliteMigrationsFSFn = origSQLiteMigrationsFSFn })

			if err := ensureLocalEnvironment(ctx, homeDir); err == nil || !strings.Contains(err.Error(), "load migrations") {
				t.Fatalf("ensureLocalEnvironment() error = %v, want migrations failure", err)
			}
		})

		t.Run("repo factory", func(t *testing.T) {
			origNewSQLiteRepoFn := newSQLiteRepoFn
			newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
				return nil, errors.New("repo failed")
			}
			t.Cleanup(func() { newSQLiteRepoFn = origNewSQLiteRepoFn })

			if err := ensureLocalEnvironment(ctx, homeDir); err == nil || !strings.Contains(err.Error(), "create repository") {
				t.Fatalf("ensureLocalEnvironment() error = %v, want repo failure", err)
			}
		})

		t.Run("seed templates", func(t *testing.T) {
			origNewSQLiteRepoFn := newSQLiteRepoFn
			newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
				return &templateRepoStub{
					mockRepo: mockRepo{
						getTemplateByNameFn: func(context.Context, string) (repository.Template, error) {
							return repository.Template{}, errors.New("template lookup failed")
						},
					},
				}, nil
			}
			t.Cleanup(func() { newSQLiteRepoFn = origNewSQLiteRepoFn })

			if err := ensureLocalEnvironment(ctx, homeDir); err == nil || !strings.Contains(err.Error(), "seed templates") {
				t.Fatalf("ensureLocalEnvironment() error = %v, want seed template failure", err)
			}
		})
	})

	t.Run("wipeLocalState and compareSnapshotDirs manifest failures", func(t *testing.T) {
		homeDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(basePath(homeDir), ".pc", "last_sync"), 0o755); err != nil {
			t.Fatalf("mkdir last_sync dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(basePath(homeDir), ".pc", "last_sync", "marker"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write last_sync marker: %v", err)
		}
		if err := wipeLocalState(homeDir); err == nil || !strings.Contains(err.Error(), "remove last_sync") {
			t.Fatalf("wipeLocalState() error = %v, want last_sync removal failure", err)
		}

		if err := compareSnapshotDirs(filepath.Join(t.TempDir(), "missing"), t.TempDir()); err == nil {
			t.Fatal("expected compareSnapshotDirs to surface manifest errors")
		}
	})

	t.Run("wipeLocalState surfaces database artifact removal failures", func(t *testing.T) {
		homeDir := t.TempDir()
		if err := os.MkdirAll(dbPath(homeDir), 0o755); err != nil {
			t.Fatalf("mkdir db dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dbPath(homeDir), "marker"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write db marker: %v", err)
		}

		if err := wipeLocalState(homeDir); err == nil || !strings.Contains(err.Error(), "remove database artifact") {
			t.Fatalf("wipeLocalState() error = %v, want database artifact failure", err)
		}
	})
}
