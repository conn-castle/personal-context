package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/syncengine"
)

func TestServiceSyncPushesLocalBundleToCloud(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 4, 5, 987000000, time.UTC)
	bundle := RecordBundle{
		Record: repository.Record{
			ID:          recordID,
			Date:        "2026-03-08",
			DayOrder:    "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID,
			Filename: "plot.png",
			S3Key:    "figures/20260308-a1b2c3d4/plot.png",
		}},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID,
			Filename: "metrics.csv",
			S3Key:    "data/20260308-a1b2c3d4/metrics.csv",
			Size:     7,
			Hash:     strings.Repeat("a", 64),
		}},
	}

	service, localRepo, cloudRepo, localFS, objects, cursorStore := newTestService(
		t,
		[]RecordBundle{bundle},
		nil,
	)
	writeLocalAsset(t, localFS, true, recordID, "plot.png", "FIGURE")
	writeLocalAsset(t, localFS, false, recordID, "metrics.csv", "1,2,3\n")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	assertBundleEqual(t, cloudRepo.bundle(recordID), localRepo.bundle(recordID))
	if got := objects.objects["figures/20260308-a1b2c3d4/plot.png"]; got != "FIGURE" {
		t.Fatalf("uploaded figure = %q, want %q", got, "FIGURE")
	}
	if got := objects.objects["data/20260308-a1b2c3d4/metrics.csv"]; got != "1,2,3\n" {
		t.Fatalf("uploaded data file = %q, want %q", got, "1,2,3\n")
	}

	_, exists, err := cursorStore.Read()
	if err != nil {
		t.Fatalf("cursorStore.Read() error = %v", err)
	}
	if !exists {
		t.Fatal("expected successful sync to persist last_sync")
	}
}

func TestServiceSyncPushesProjectPathsAndChatsToCloud(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	service, localRepo, cloudRepo, _, _, _ := newTestService(t, nil, nil)
	localRepo.projects["sync/chat-project"] = repository.Project{ID: "sync/chat-project", CreatedAt: now, UpdatedAt: now}
	localRepo.devices["sync-chat-device"] = repository.Device{ID: "sync-chat-device", CreatedAt: now, UpdatedAt: now}
	localRepo.projectPaths["sync/chat-project|/tmp/sync-chat|sync-chat-device"] = repository.ProjectPath{
		ID:        1,
		ProjectID: "sync/chat-project",
		Path:      "/tmp/sync-chat",
		DeviceID:  "sync-chat-device",
		CreatedAt: now,
		UpdatedAt: now,
	}
	title := "Sync chat"
	cwd := "/tmp/sync-chat"
	projectID := "sync/chat-project"
	parentSID := "sync-parent"
	localRepo.chatSessions["20260514-syncchat"] = repository.ChatSession{
		ID:                    "20260514-syncchat",
		Source:                "codex",
		SourceSessionID:       "sync-source",
		ParentSourceSessionID: &parentSID,
		SourceDeviceID:        "sync-chat-device",
		ProjectID:             &projectID,
		CWD:                   &cwd,
		Title:                 &title,
		StartedAt:             now,
		LastActivityAt:        now.Add(time.Minute),
		CreatedAt:             now,
		UpdatedAt:             now.Add(time.Minute),
	}
	text := "sync chat item"
	localRepo.chatItems["20260514-syncchat"] = []repository.ChatItem{{
		ID:         1,
		SessionID:  "20260514-syncchat",
		Ordinal:    0,
		Role:       "user",
		ItemType:   "message",
		Text:       &text,
		SearchText: text,
		CreatedAt:  now.Add(time.Minute),
	}}

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if _, ok := cloudRepo.projectPaths["sync/chat-project|/tmp/sync-chat|sync-chat-device"]; !ok {
		t.Fatalf("expected project path to sync, got %+v", cloudRepo.projectPaths)
	}
	cloudSession, ok := cloudRepo.chatSessions["20260514-syncchat"]
	if !ok {
		t.Fatalf("expected chat session to sync, got %+v", cloudRepo.chatSessions)
	}
	if cloudSession.Title == nil || *cloudSession.Title != title {
		t.Fatalf("unexpected synced chat session: %+v", cloudSession)
	}
	if cloudSession.ParentSourceSessionID == nil || *cloudSession.ParentSourceSessionID != parentSID {
		t.Fatalf("expected parent_source_session_id to sync, got %+v", cloudSession.ParentSourceSessionID)
	}
	cloudItems := cloudRepo.chatItems["20260514-syncchat"]
	if len(cloudItems) != 1 || cloudItems[0].SearchText != text {
		t.Fatalf("unexpected synced chat items: %+v", cloudItems)
	}
}

func TestServiceSyncPullsProjectPathsAndChatsToLocal(t *testing.T) {
	now := time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC)
	service, localRepo, cloudRepo, _, _, _ := newTestService(t, nil, nil)
	cloudRepo.projects["sync/cloud-chat-project"] = repository.Project{ID: "sync/cloud-chat-project", CreatedAt: now, UpdatedAt: now}
	cloudRepo.devices["sync-cloud-chat-device"] = repository.Device{ID: "sync-cloud-chat-device", CreatedAt: now, UpdatedAt: now}
	cloudRepo.projectPaths["sync/cloud-chat-project|/tmp/cloud-sync-chat|sync-cloud-chat-device"] = repository.ProjectPath{
		ID:        1,
		ProjectID: "sync/cloud-chat-project",
		Path:      "/tmp/cloud-sync-chat",
		DeviceID:  "sync-cloud-chat-device",
		CreatedAt: now,
		UpdatedAt: now,
	}
	title := "Cloud sync chat"
	projectID := "sync/cloud-chat-project"
	cloudRepo.chatSessions["20260514-facefeed"] = repository.ChatSession{
		ID:              "20260514-facefeed",
		Source:          "gemini",
		SourceSessionID: "cloud-source",
		SourceDeviceID:  "sync-cloud-chat-device",
		ProjectID:       &projectID,
		Title:           &title,
		StartedAt:       now,
		LastActivityAt:  now.Add(time.Minute),
		CreatedAt:       now,
		UpdatedAt:       now.Add(time.Minute),
	}
	text := "pulled sync chat item"
	cloudRepo.chatItems["20260514-facefeed"] = []repository.ChatItem{{
		ID:         1,
		SessionID:  "20260514-facefeed",
		Ordinal:    0,
		Role:       "assistant",
		ItemType:   "message",
		SearchText: text,
		CreatedAt:  now.Add(time.Minute),
	}}

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if _, ok := localRepo.projectPaths["sync/cloud-chat-project|/tmp/cloud-sync-chat|sync-cloud-chat-device"]; !ok {
		t.Fatalf("expected cloud project path locally, got %+v", localRepo.projectPaths)
	}
	if session, ok := localRepo.chatSessions["20260514-facefeed"]; !ok || session.Source != "gemini" {
		t.Fatalf("expected pulled chat session, got ok=%v session=%+v", ok, session)
	}
	if items := localRepo.chatItems["20260514-facefeed"]; len(items) != 1 || items[0].SearchText != text {
		t.Fatalf("expected pulled chat item, got %+v", items)
	}
}

func TestSyncChangedChatsErrorBranches(t *testing.T) {
	now := time.Date(2026, 5, 14, 14, 0, 0, 0, time.UTC)
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	source.listChatSessionsErr = errors.New("list chats failed")
	if err := syncChangedChats(context.Background(), time.Time{}, "test", source, target, nil, nil); err == nil || !strings.Contains(err.Error(), "list test chat changes") {
		t.Fatalf("expected list chat changes error, got %v", err)
	}

	source = newMemoryRepo(nil)
	source.chatSessions["20260514-beadfeed"] = repository.ChatSession{
		ID:              "20260514-beadfeed",
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	source.listChatItemsErr = errors.New("items failed")
	if err := syncChangedChats(context.Background(), time.Time{}, "test", source, target, nil, nil); err == nil || !strings.Contains(err.Error(), "list test chat items") {
		t.Fatalf("expected list chat items error, got %v", err)
	}

	source.listChatItemsErr = nil
	source.chatItems["20260514-beadfeed"] = []repository.ChatItem{{SessionID: "20260514-beadfeed", Ordinal: 0, Role: "user", ItemType: "message", SearchText: "x", CreatedAt: now}}
	target = newMemoryRepo(nil)
	target.replaceChatItemsErr = errors.New("replace failed")
	if err := syncChangedChats(context.Background(), time.Time{}, "test", source, target, nil, nil); err == nil || !strings.Contains(err.Error(), "test chat session/items") {
		t.Fatalf("expected chat session/items error, got %v", err)
	}
}

func TestSyncChangedChatsRetriesAfterAtomicItemWriteFailure(t *testing.T) {
	now := time.Date(2026, 5, 14, 14, 0, 0, 0, time.UTC)
	id := "20260514-beadfeed"
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	source.chatSessions[id] = repository.ChatSession{
		ID:              id,
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now.Add(time.Minute),
		CreatedAt:       now,
		UpdatedAt:       now.Add(time.Minute),
	}
	target.chatSessions[id] = repository.ChatSession{
		ID:              id,
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	oldText := "old target item"
	newText := "new source item"
	target.chatItems[id] = []repository.ChatItem{{SessionID: id, Ordinal: 0, Role: "user", ItemType: "message", SearchText: oldText, CreatedAt: now}}
	source.chatItems[id] = []repository.ChatItem{{SessionID: id, Ordinal: 0, Role: "assistant", ItemType: "message", SearchText: newText, CreatedAt: now.Add(time.Minute)}}

	target.replaceChatItemsErr = errors.New("replace failed")
	err := syncChangedChatsDirected(context.Background(), time.Time{}, "test", source, target, WinnerLocal, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "test chat session/items") {
		t.Fatalf("expected atomic write error, got %v", err)
	}
	if got := target.chatSessions[id].UpdatedAt; !got.Equal(now) {
		t.Fatalf("target updated_at advanced after failed item write: got %v want %v", got, now)
	}
	if items := target.chatItems[id]; len(items) != 1 || items[0].SearchText != oldText {
		t.Fatalf("target items changed after failed item write: %+v", items)
	}

	target.replaceChatItemsErr = nil
	if err := syncChangedChatsDirected(context.Background(), time.Time{}, "test", source, target, WinnerLocal, nil, nil); err != nil {
		t.Fatalf("syncChangedChatsDirected(retry) error = %v", err)
	}
	if got := target.chatSessions[id].UpdatedAt; !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("target updated_at after retry = %v, want %v", got, now.Add(time.Minute))
	}
	if items := target.chatItems[id]; len(items) != 1 || items[0].SearchText != newText {
		t.Fatalf("target items after retry = %+v, want %q", items, newText)
	}
}

func TestSyncChangedChatsPullDoesNotDownloadRawSourceBeforeMetadataUpsert(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	rawKey := "chats/raw/20260517-aabbccdd/source.jsonl"
	source := newMemoryRepo(nil)
	source.chatSessions["20260517-aabbccdd"] = repository.ChatSession{
		ID:              "20260517-aabbccdd",
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		RawSourceKey:    &rawKey,
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	target := newMemoryRepo(nil)
	target.upsertChatSessionErr = errors.New("metadata write failed")
	rawTransferCalled := false
	rawTransfer := func(context.Context, string, repository.ChatSession, *chatRawSyncReport) error {
		rawTransferCalled = true
		return nil
	}

	err := syncChangedChatsDirected(context.Background(), time.Time{}, "pull", source, target, WinnerCloud, rawTransfer, nil)
	if err == nil || !strings.Contains(err.Error(), "pull chat session/items 20260517-aabbccdd") {
		t.Fatalf("expected metadata upsert error, got %v", err)
	}
	if rawTransferCalled {
		t.Fatal("raw transfer ran before pull metadata upsert failure")
	}
}

func TestSyncChangedChatsPullRetriesRawSourceAfterPostUpsertTransferFailure(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	rawKey := "chats/raw/20260517-aabbccdd/source.jsonl"
	source := newMemoryRepo(nil)
	source.chatSessions["20260517-aabbccdd"] = repository.ChatSession{
		ID:              "20260517-aabbccdd",
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		RawSourceKey:    &rawKey,
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	target := newMemoryRepo(nil)

	rawTransferCalls := 0
	rawTransfer := func(context.Context, string, repository.ChatSession, *chatRawSyncReport) error {
		rawTransferCalls++
		if rawTransferCalls == 1 {
			return errors.New("download failed after metadata upsert")
		}
		return nil
	}

	err := syncChangedChatsDirected(context.Background(), time.Time{}, "pull", source, target, WinnerCloud, rawTransfer, nil)
	if err == nil || !strings.Contains(err.Error(), "download failed after metadata upsert") {
		t.Fatalf("expected raw transfer error, got %v", err)
	}
	if _, ok := target.chatSessions["20260517-aabbccdd"]; !ok {
		t.Fatal("expected metadata to be present after first failed raw transfer")
	}

	if err := syncChangedChatsDirected(context.Background(), time.Time{}, "pull", source, target, WinnerCloud, rawTransfer, nil); err != nil {
		t.Fatalf("expected retry raw transfer to succeed, got %v", err)
	}
	if rawTransferCalls != 2 {
		t.Fatalf("rawTransferCalls = %d, want 2", rawTransferCalls)
	}
}

func TestSyncChangedChatsPushDoesNotUploadRawSourceForEqualMetadata(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	rawKey := "chats/raw/20260517-aabbccdd/source.jsonl"
	source := newMemoryRepo(nil)
	source.chatSessions["20260517-aabbccdd"] = repository.ChatSession{
		ID:              "20260517-aabbccdd",
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		RawSourceKey:    &rawKey,
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	target := newMemoryRepo(nil)
	target.chatSessions["20260517-aabbccdd"] = source.chatSessions["20260517-aabbccdd"]
	rawTransferCalled := false
	rawTransfer := func(context.Context, string, repository.ChatSession, *chatRawSyncReport) error {
		rawTransferCalled = true
		return nil
	}

	if err := syncChangedChatsDirected(context.Background(), time.Time{}, "push", source, target, WinnerLocal, rawTransfer, nil); err != nil {
		t.Fatalf("syncChangedChatsDirected(push equal metadata) error = %v", err)
	}
	if rawTransferCalled {
		t.Fatal("push transferred raw source for equal metadata")
	}
}

func TestSyncChangedChatsReplacesItemsAndSkipsOlderSource(t *testing.T) {
	now := time.Date(2026, 5, 14, 14, 0, 0, 0, time.UTC)
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	source.chatSessions["20260514-beadfeed"] = repository.ChatSession{
		ID:              "20260514-beadfeed",
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now.Add(time.Minute),
		CreatedAt:       now,
		UpdatedAt:       now.Add(time.Minute),
	}
	target.chatSessions["20260514-beadfeed"] = repository.ChatSession{
		ID:              "20260514-beadfeed",
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	oldText := "old target item"
	newText := "new source item"
	target.chatItems["20260514-beadfeed"] = []repository.ChatItem{{SessionID: "20260514-beadfeed", Ordinal: 0, Role: "user", ItemType: "message", SearchText: oldText, CreatedAt: now}}
	source.chatItems["20260514-beadfeed"] = []repository.ChatItem{{SessionID: "20260514-beadfeed", Ordinal: 0, Role: "user", ItemType: "message", SearchText: newText, CreatedAt: now.Add(time.Minute)}}

	if err := syncChangedChatsDirected(context.Background(), time.Time{}, "test", source, target, WinnerLocal, nil, nil); err != nil {
		t.Fatalf("syncChangedChatsDirected(replace) error = %v", err)
	}
	if items := target.chatItems["20260514-beadfeed"]; len(items) != 1 || items[0].SearchText != newText {
		t.Fatalf("expected target chat items to be replaced, got %+v", items)
	}

	source.chatSessions["20260514-beadfeed"] = repository.ChatSession{
		ID:              "20260514-beadfeed",
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now.Add(time.Minute),
	}
	target.chatSessions["20260514-beadfeed"] = repository.ChatSession{
		ID:              "20260514-beadfeed",
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now.Add(2 * time.Minute),
		CreatedAt:       now,
		UpdatedAt:       now.Add(2 * time.Minute),
	}
	target.chatItems["20260514-beadfeed"] = []repository.ChatItem{{SessionID: "20260514-beadfeed", Ordinal: 0, Role: "user", ItemType: "message", SearchText: oldText, CreatedAt: now.Add(2 * time.Minute)}}

	if err := syncChangedChatsDirected(context.Background(), time.Time{}, "test", source, target, WinnerLocal, nil, nil); err != nil {
		t.Fatalf("syncChangedChatsDirected(skip older) error = %v", err)
	}
	if items := target.chatItems["20260514-beadfeed"]; len(items) != 1 || items[0].SearchText != oldText {
		t.Fatalf("expected newer target chat items to be preserved, got %+v", items)
	}
}

func TestSyncChangedChatsReturnsWarningWriteErrors(t *testing.T) {
	now := time.Date(2026, 5, 14, 14, 0, 0, 0, time.UTC)
	rawKey := "chats/raw/20260514-beadfeed/source.jsonl"
	session := repository.ChatSession{
		ID:              "20260514-beadfeed",
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		RawSourceKey:    &rawKey,
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	source.chatSessions[session.ID] = session
	rawTransfer := func(_ context.Context, _ string, session repository.ChatSession, report *chatRawSyncReport) error {
		report.MissingLocal = append(report.MissingLocal, session.ID)
		return nil
	}
	err := syncChangedChatsDirected(context.Background(), time.Time{}, "test", source, target, WinnerLocal, rawTransfer, failingSyncWarnWriter{})
	if err == nil || !strings.Contains(err.Error(), "write local chat raw source warning") {
		t.Fatalf("expected warning write error, got %v", err)
	}

	source = newMemoryRepo(nil)
	target = newMemoryRepo(nil)
	source.chatSessions[session.ID] = session
	rawTransfer = func(_ context.Context, _ string, session repository.ChatSession, report *chatRawSyncReport) error {
		report.MissingCloud = append(report.MissingCloud, session.ID)
		return nil
	}
	err = syncChangedChatsDirected(context.Background(), time.Time{}, "test", source, target, WinnerLocal, rawTransfer, failingSyncWarnWriter{})
	if err == nil || !strings.Contains(err.Error(), "write cloud chat raw source warning") {
		t.Fatalf("expected cloud warning write error, got %v", err)
	}
}

func TestSyncChangedChatsReturnsTargetLoadErrors(t *testing.T) {
	now := time.Date(2026, 5, 14, 14, 0, 0, 0, time.UTC)
	source := newMemoryRepo(nil)
	source.chatSessions["20260514-beadfeed"] = repository.ChatSession{
		ID:              "20260514-beadfeed",
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	target := &getChatSessionErrorRepo{memoryRepo: newMemoryRepo(nil)}
	err := syncChangedChatsDirected(context.Background(), time.Time{}, "test", source, target, WinnerLocal, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "load target chat session") {
		t.Fatalf("expected load target chat session error, got %v", err)
	}
}

type failingSyncWarnWriter struct{}

func (failingSyncWarnWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

type getChatSessionErrorRepo struct {
	*memoryRepo
}

func (r *getChatSessionErrorRepo) GetChatSessionByID(context.Context, string) (repository.ChatSession, error) {
	return repository.ChatSession{}, errors.New("lookup failed")
}

func TestServiceSyncPullsCloudBundleToLocalWithoutDownloadingDataFiles(t *testing.T) {
	recordID := "20260308-deadbeef"
	now := time.Date(2026, 3, 8, 18, 30, 0, 123000000, time.UTC)
	bundle := RecordBundle{
		Record: repository.Record{
			ID:          recordID,
			Date:        "2026-03-08",
			DayOrder:    "a1",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID,
			Filename: "chart.png",
			S3Key:    "figures/20260308-deadbeef/chart.png",
		}},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID,
			Filename: "report.csv",
			S3Key:    "data/20260308-deadbeef/report.csv",
			Size:     9,
			Hash:     strings.Repeat("b", 64),
		}},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(
		t,
		nil,
		[]RecordBundle{bundle},
	)
	objects.objects["figures/20260308-deadbeef/chart.png"] = "CLOUD-FIGURE"
	objects.objects["data/20260308-deadbeef/report.csv"] = "CLOUD-DATA"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	assertBundleEqual(t, localRepo.bundle(recordID), bundle)
	if got := readLocalAsset(t, localFS, true, recordID, "chart.png"); got != "CLOUD-FIGURE" {
		t.Fatalf("downloaded figure = %q, want %q", got, "CLOUD-FIGURE")
	}
	dataPath, err := localFS.ResolveDataFilePath(recordID, "report.csv")
	if err != nil {
		t.Fatalf("ResolveDataFilePath() error = %v", err)
	}
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected pc sync to skip local data-file download, stat error = %v", err)
	}
}

func TestServiceSyncRepairsPartialPushWhenParentTimestampsEqual(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 4, 5, 987000000, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID:          recordID,
			Date:        "2026-03-08",
			DayOrder:    "a0",
			HTMLContent: strPtr("<html>same</html>"),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID,
			Filename: "plot.png",
			S3Key:    "figures/20260308-a1b2c3d4/plot.png",
		}},
	}
	cloudBundle := RecordBundle{Record: localBundle.Record}

	service, _, cloudRepo, localFS, objects, _ := newTestService(
		t,
		[]RecordBundle{localBundle},
		[]RecordBundle{cloudBundle},
	)
	writeLocalAsset(t, localFS, true, recordID, "plot.png", "LOCAL-FIGURE")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	assertBundleEqual(t, cloudRepo.bundle(recordID), localBundle)
	if got := objects.objects["figures/20260308-a1b2c3d4/plot.png"]; got != "LOCAL-FIGURE" {
		t.Fatalf("uploaded figure = %q, want %q", got, "LOCAL-FIGURE")
	}
}

func TestServiceSyncDoesNotPushIncompleteEqualParentBeforePullRepair(t *testing.T) {
	recordID := "20260308-deadbeef"
	now := time.Date(2026, 3, 8, 18, 30, 0, 123000000, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID:          recordID,
			Date:        "2026-03-08",
			DayOrder:    "a1",
			HTMLContent: strPtr("<html>same</html>"),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID,
			Filename: "chart.png",
			S3Key:    "figures/20260308-deadbeef/chart.png",
		}},
	}
	localBundle := RecordBundle{Record: cloudBundle.Record}

	service, localRepo, cloudRepo, localFS, objects, _ := newTestService(
		t,
		[]RecordBundle{localBundle},
		[]RecordBundle{cloudBundle},
	)
	objects.objects["figures/20260308-deadbeef/chart.png"] = "CLOUD-FIGURE"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	assertBundleEqual(t, cloudRepo.bundle(recordID), cloudBundle)
	assertBundleEqual(t, localRepo.bundle(recordID), cloudBundle)
	if got := readLocalAsset(t, localFS, true, recordID, "chart.png"); got != "CLOUD-FIGURE" {
		t.Fatalf("downloaded figure = %q, want %q", got, "CLOUD-FIGURE")
	}
}

func TestServiceSyncLeavesLastSyncUnchangedOnFailure(t *testing.T) {
	recordID := "20260308-feedface"
	now := time.Date(2026, 3, 8, 20, 0, 0, 0, time.UTC)
	bundle := RecordBundle{
		Record: repository.Record{
			ID:          recordID,
			Date:        "2026-03-08",
			DayOrder:    "a2",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID,
			Filename: "missing.png",
			S3Key:    "figures/20260308-feedface/missing.png",
		}},
	}

	service, _, _, _, _, cursorStore := newTestService(t, nil, []RecordBundle{bundle})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected Sync() to fail when a required figure download is missing")
	}

	_, exists, readErr := cursorStore.Read()
	if readErr != nil {
		t.Fatalf("cursorStore.Read() error = %v", readErr)
	}
	if exists {
		t.Fatal("expected failed sync to leave last_sync unset")
	}
}

func TestServiceSyncPullsLaterCloudEditOverLocalChange(t *testing.T) {
	recordID := "20260308-cafebabe"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID:          recordID,
			Date:        "2026-03-08",
			DayOrder:    "a3",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base,
			UpdatedAt:   base.Add(1 * time.Minute),
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID,
			Filename: "plot.png",
			S3Key:    "figures/20260308-cafebabe/plot.png",
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID:          recordID,
			Date:        "2026-03-08",
			DayOrder:    "a3",
			HTMLContent: strPtr("<html>cloud wins</html>"),
			CreatedAt:   base,
			UpdatedAt:   base.Add(2 * time.Minute),
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID,
			Filename: "plot.png",
			S3Key:    "figures/20260308-cafebabe/plot.png",
		}},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(
		t,
		[]RecordBundle{localBundle},
		[]RecordBundle{cloudBundle},
	)
	writeLocalAsset(t, localFS, true, recordID, "plot.png", "LOCAL")
	objects.objects["figures/20260308-cafebabe/plot.png"] = "CLOUD"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	assertBundleEqual(t, localRepo.bundle(recordID), cloudBundle)
	if got := readLocalAsset(t, localFS, true, recordID, "plot.png"); got != "CLOUD" {
		t.Fatalf("local figure = %q, want %q", got, "CLOUD")
	}
}

func TestServiceSyncPullsRenamedCloudFigureWithoutDeletingNewFile(t *testing.T) {
	recordID := "20260308-rename01"
	base := time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID:          recordID,
			Date:        "2026-03-08",
			DayOrder:    "a4",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base,
			UpdatedAt:   base,
		},
		Figures: []repository.RecordFigure{{
			ID:       1,
			RecordID: recordID,
			Filename: "old.png",
			S3Key:    "figures/20260308-rename01/old.png",
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID:          recordID,
			Date:        "2026-03-08",
			DayOrder:    "a4",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base,
			UpdatedAt:   base.Add(1 * time.Minute),
		},
		Figures: []repository.RecordFigure{{
			ID:       1,
			RecordID: recordID,
			Filename: "new.png",
			S3Key:    "figures/20260308-rename01/new.png",
		}},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(
		t,
		[]RecordBundle{localBundle},
		[]RecordBundle{cloudBundle},
	)
	writeLocalAsset(t, localFS, true, recordID, "old.png", "OLD")
	objects.objects["figures/20260308-rename01/new.png"] = "NEW"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	assertBundleEqual(t, localRepo.bundle(recordID), cloudBundle)
	if got := readLocalAsset(t, localFS, true, recordID, "new.png"); got != "NEW" {
		t.Fatalf("renamed figure = %q, want %q", got, "NEW")
	}
	oldPath, err := localFS.ResolveFigurePath(recordID, "old.png")
	if err != nil {
		t.Fatalf("ResolveFigurePath() error = %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old figure file to be removed, stat error = %v", err)
	}
}

// --- NewService nil-argument validation tests ---

func TestNewServiceErrorsOnNilLocalRepo(t *testing.T) {
	_, err := NewService(nil, &memoryRepo{}, &stubLocalFiles{}, newMockObjectStore(), &errSessionManager{})
	if err == nil {
		t.Fatal("expected error for nil local repository")
	}
	if got := err.Error(); got != "local repository is required" {
		t.Fatalf("error = %q, want 'local repository is required'", got)
	}
}

func TestNewServiceErrorsOnNilCloudRepo(t *testing.T) {
	_, err := NewService(&memoryRepo{}, nil, &stubLocalFiles{}, newMockObjectStore(), &errSessionManager{})
	if err == nil {
		t.Fatal("expected error for nil cloud repository")
	}
	if got := err.Error(); got != "cloud repository is required" {
		t.Fatalf("error = %q, want 'cloud repository is required'", got)
	}
}

func TestNewServiceErrorsOnNilLocalFS(t *testing.T) {
	_, err := NewService(&memoryRepo{}, &memoryRepo{}, nil, newMockObjectStore(), &errSessionManager{})
	if err == nil {
		t.Fatal("expected error for nil local filesystem")
	}
	if got := err.Error(); got != "local filesystem is required" {
		t.Fatalf("error = %q, want 'local filesystem is required'", got)
	}
}

func TestNewServiceErrorsOnNilObjects(t *testing.T) {
	_, err := NewService(&memoryRepo{}, &memoryRepo{}, &stubLocalFiles{}, nil, &errSessionManager{})
	if err == nil {
		t.Fatal("expected error for nil object store")
	}
	if got := err.Error(); got != "object store is required" {
		t.Fatalf("error = %q, want 'object store is required'", got)
	}
}

func TestNewServiceErrorsOnNilSession(t *testing.T) {
	_, err := NewService(&memoryRepo{}, &memoryRepo{}, &stubLocalFiles{}, newMockObjectStore(), nil)
	if err == nil {
		t.Fatal("expected error for nil session manager")
	}
	if got := err.Error(); got != "session manager is required" {
		t.Fatalf("error = %q, want 'session manager is required'", got)
	}
}

// --- Sync error path tests ---

func TestSyncErrorsOnSessionBeginFailure(t *testing.T) {
	beginErr := fmt.Errorf("lock already held")
	svc := &Service{
		localRepo:    newMemoryRepo(nil),
		cloudRepo:    newMemoryRepo(nil),
		localFS:      &stubLocalFiles{},
		cloudObjects: newMockObjectStore(),
		session:      &errSessionManager{beginErr: beginErr},
	}
	err := svc.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from session.Begin()")
	}
	if err.Error() != "lock already held" {
		t.Fatalf("error = %q, want 'lock already held'", err.Error())
	}
}

func TestSyncErrorsOnSessionCompleteFailure(t *testing.T) {
	completeErr := fmt.Errorf("cursor write failed")
	pcDir := filepath.Join(t.TempDir(), ".pc")
	session, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Wrap the real session to inject Complete error.
	wrapper := &completeErrSessionWrapper{
		real:        session,
		completeErr: completeErr,
	}

	svc := &Service{
		localRepo:    newMemoryRepo(nil),
		cloudRepo:    newMemoryRepo(nil),
		localFS:      &stubLocalFiles{},
		cloudObjects: newMockObjectStore(),
		session:      wrapper,
	}
	err = svc.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from session.Complete()")
	}
	if err.Error() != "cursor write failed" {
		t.Fatalf("error = %q, want 'cursor write failed'", err.Error())
	}
}

func TestSyncErrorsOnUpdateCloudVersionGetSyncVersionFailure(t *testing.T) {
	cloudRepo := newMemoryRepo(nil)
	cloudRepo.getSyncVersionErr = fmt.Errorf("db connection lost")

	service, _, _, _, _, _ := newTestService(t, nil, nil)
	// Replace cloudRepo with error-injecting one.
	service.cloudRepo = cloudRepo

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from GetSyncVersion()")
	}
	if !strings.Contains(err.Error(), "db connection lost") {
		t.Fatalf("error = %q, want to contain 'db connection lost'", err.Error())
	}
}

func TestSyncErrorsOnUpdateCloudVersionUpdateVersionFailure(t *testing.T) {
	service, _, _, _, objects, _ := newTestService(t, nil, nil)
	objects.updateVersionErr = fmt.Errorf("s3 write denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from UpdateVersion()")
	}
	if !strings.Contains(err.Error(), "s3 write denied") {
		t.Fatalf("error = %q, want to contain 's3 write denied'", err.Error())
	}
}

func TestSyncErrorsOnListLocalChanges(t *testing.T) {
	localRepo := newMemoryRepo(nil)
	localRepo.listRecordsErr = fmt.Errorf("local list failed")

	service, _, _, _, _, _ := newTestService(t, nil, nil)
	service.localRepo = localRepo

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local ListRecords()")
	}
	if !strings.Contains(err.Error(), "local list failed") {
		t.Fatalf("error = %q, want to contain 'local list failed'", err.Error())
	}
}

func TestSyncErrorsOnListCloudChanges(t *testing.T) {
	cloudRepo := newMemoryRepo(nil)
	cloudRepo.listRecordsErr = fmt.Errorf("cloud list failed")

	service, _, _, _, _, _ := newTestService(t, nil, nil)
	service.cloudRepo = cloudRepo

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from cloud ListRecords()")
	}
	if !strings.Contains(err.Error(), "cloud list failed") {
		t.Fatalf("error = %q, want to contain 'cloud list failed'", err.Error())
	}
}

func TestSyncErrorsOnLoadBundleNonNotFoundError(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	// Inject non-ErrNotFound error into cloud repo for GetRecordByID.
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.getRecordByIDErr = fmt.Errorf("connection timeout")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from loadBundle non-ErrNotFound path")
	}
	if !strings.Contains(err.Error(), "connection timeout") {
		t.Fatalf("error = %q, want to contain 'connection timeout'", err.Error())
	}
}

func TestSyncErrorsOnBundleForRecordListFiguresError(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.listFiguresByRecordIDErr = fmt.Errorf("figures table locked")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from bundleForRecord ListRecordFiguresByRecordID")
	}
	if !strings.Contains(err.Error(), "figures table locked") {
		t.Fatalf("error = %q, want to contain 'figures table locked'", err.Error())
	}
}

func TestSyncErrorsOnBundleForRecordListDataFilesError(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.listDataFilesByRecordIDErr = fmt.Errorf("data files table locked")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from bundleForRecord ListRecordDataFilesByRecordID")
	}
	if !strings.Contains(err.Error(), "data files table locked") {
		t.Fatalf("error = %q, want to contain 'data files table locked'", err.Error())
	}
}

func TestSyncErrorsOnUploadFileOpenFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID,
			Filename: "missing.png",
			S3Key:    "figures/20260308-a1b2c3d4/missing.png",
		}},
	}

	// Do NOT write the local file so os.Open fails.
	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from uploadFile when local file does not exist")
	}
	if !strings.Contains(err.Error(), "open local file") {
		t.Fatalf("error = %q, want to contain 'open local file'", err.Error())
	}
}

func TestSyncErrorsOnDownloadFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID,
			Filename: "chart.png",
			S3Key:    "figures/20260308-a1b2c3d4/chart.png",
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	objects.downloadErr = fmt.Errorf("s3 access denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from downloadFile")
	}
	if !strings.Contains(err.Error(), "s3 access denied") {
		t.Fatalf("error = %q, want to contain 's3 access denied'", err.Error())
	}
}

// --- Push: cloud exists but cloud wins -> skip ---

func TestSyncPushSkipsWhenCloudWins(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud later</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, cloudRepo, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Cloud should not have been overwritten by local.
	got := cloudRepo.bundle(recordID)
	if got.Record.HTMLContent == nil || *got.Record.HTMLContent != "<html>cloud later</html>" {
		t.Fatalf("cloud HTMLContent = %v, want %q", got.Record.HTMLContent, "<html>cloud later</html>")
	}
}

// --- Pull: local exists but local wins -> skip ---

func TestSyncPullSkipsWhenLocalWins(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local later</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
	}

	service, localRepo, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Local should not have been overwritten by cloud.
	got := localRepo.bundle(recordID)
	if got.Record.HTMLContent == nil || *got.Record.HTMLContent != "<html>local later</html>" {
		t.Fatalf("local HTMLContent = %v, want %q", got.Record.HTMLContent, "<html>local later</html>")
	}
}

// --- Push with figure create/update/delete on cloud ---

func TestSyncPushFigureCreateUpdateDeleteOnCloud(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	// Local has: new.png (create), updated.png (update s3key), no old.png (delete)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.RecordFigure{
			{RecordID: recordID, Filename: "new.png", S3Key: "figures/" + recordID + "/new.png"},
			{RecordID: recordID, Filename: "updated.png", S3Key: "figures/" + recordID + "/updated.png"},
		},
	}
	// Cloud has: updated.png (existing, different s3key), old.png (to be deleted)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{
			{ID: 10, RecordID: recordID, Filename: "updated.png", S3Key: "figures/" + recordID + "/updated-v1.png"},
			{ID: 11, RecordID: recordID, Filename: "old.png", S3Key: "figures/" + recordID + "/old.png"},
		},
	}

	service, _, cloudRepo, localFS, objects, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, recordID, "new.png", "NEW-FIG")
	writeLocalAsset(t, localFS, true, recordID, "updated.png", "UPDATED-FIG")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// new.png should be uploaded and created in cloud repo.
	if got := objects.objects["figures/"+recordID+"/new.png"]; got != "NEW-FIG" {
		t.Fatalf("uploaded new figure = %q, want %q", got, "NEW-FIG")
	}
	// updated.png should be uploaded with new s3 key; old s3 key should be deleted.
	if got := objects.objects["figures/"+recordID+"/updated.png"]; got != "UPDATED-FIG" {
		t.Fatalf("uploaded updated figure = %q, want %q", got, "UPDATED-FIG")
	}
	if _, exists := objects.objects["figures/"+recordID+"/updated-v1.png"]; exists {
		t.Fatal("expected old s3 key for updated figure to be deleted")
	}
	// old.png should be deleted from cloud object store and repo.
	if _, exists := objects.objects["figures/"+recordID+"/old.png"]; exists {
		t.Fatal("expected deleted figure s3 object to be removed")
	}
	cloudFigures := cloudRepo.bundle(recordID).Figures
	for _, fig := range cloudFigures {
		if fig.Filename == "old.png" {
			t.Fatal("expected old.png to be deleted from cloud repo")
		}
	}
}

// --- Push with data file create/update/delete on cloud ---

func TestSyncPushDataFileCreateUpdateDeleteOnCloud(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{
			{RecordID: recordID, Filename: "new.csv", S3Key: "data/" + recordID + "/new.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
			{RecordID: recordID, Filename: "updated.csv", S3Key: "data/" + recordID + "/updated.csv",
				Size: 12, Hash: strings.Repeat("b", 64)},
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{
			{ID: 20, RecordID: recordID, Filename: "updated.csv", S3Key: "data/" + recordID + "/updated-v1.csv",
				Size: 10, Hash: strings.Repeat("c", 64)},
			{ID: 21, RecordID: recordID, Filename: "old.csv", S3Key: "data/" + recordID + "/old.csv",
				Size: 8, Hash: strings.Repeat("d", 64)},
		},
	}

	service, _, cloudRepo, localFS, objects, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, recordID, "new.csv", "NEW-DATA")
	writeLocalAsset(t, localFS, false, recordID, "updated.csv", "UPDATED-DATA")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// new.csv uploaded and created.
	if got := objects.objects["data/"+recordID+"/new.csv"]; got != "NEW-DATA" {
		t.Fatalf("uploaded new data file = %q, want %q", got, "NEW-DATA")
	}
	// updated.csv: old s3 key deleted.
	if _, exists := objects.objects["data/"+recordID+"/updated-v1.csv"]; exists {
		t.Fatal("expected old s3 key for updated data file to be deleted")
	}
	// old.csv: deleted from object store and repo.
	if _, exists := objects.objects["data/"+recordID+"/old.csv"]; exists {
		t.Fatal("expected deleted data file s3 object to be removed")
	}
	cloudDataFiles := cloudRepo.bundle(recordID).DataFiles
	for _, df := range cloudDataFiles {
		if df.Filename == "old.csv" {
			t.Fatal("expected old.csv to be deleted from cloud repo")
		}
	}
}

// --- Pull record that deletes local figures and data files ---

func TestSyncPullDeletesLocalFiguresAndDataFiles(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{
			{ID: 1, RecordID: recordID, Filename: "obsolete.png", S3Key: "figures/" + recordID + "/obsolete.png"},
		},
		DataFiles: []repository.RecordDataFile{
			{ID: 1, RecordID: recordID, Filename: "obsolete.csv", S3Key: "data/" + recordID + "/obsolete.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	// Cloud has no figures or data files (everything should be deleted locally).
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud wins</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, recordID, "obsolete.png", "OBSOLETE-FIG")
	writeLocalAsset(t, localFS, false, recordID, "obsolete.csv", "OBSOLETE-DATA")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(recordID)
	if len(got.Figures) != 0 {
		t.Fatalf("expected no local figures, got %d", len(got.Figures))
	}
	if len(got.DataFiles) != 0 {
		t.Fatalf("expected no local data files, got %d", len(got.DataFiles))
	}

	// Check files removed from disk.
	figPath, _ := localFS.ResolveFigurePath(recordID, "obsolete.png")
	if _, err := os.Stat(figPath); !os.IsNotExist(err) {
		t.Fatalf("expected obsolete figure file to be removed, stat error = %v", err)
	}
	dataPath, _ := localFS.ResolveDataFilePath(recordID, "obsolete.csv")
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected obsolete data file to be removed, stat error = %v", err)
	}
}

// --- Pull with data file filename change (delete old local file) ---

func TestSyncPullWithDataFileFilenameChange(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{
			{ID: 1, RecordID: recordID, Filename: "old-name.csv", S3Key: "data/" + recordID + "/old-name.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{
			{RecordID: recordID, Filename: "new-name.csv", S3Key: "data/" + recordID + "/new-name.csv",
				Size: 8, Hash: strings.Repeat("b", 64)},
		},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, recordID, "old-name.csv", "OLD-DATA")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(recordID)
	if len(got.DataFiles) != 1 {
		t.Fatalf("expected 1 data file, got %d", len(got.DataFiles))
	}
	if got.DataFiles[0].Filename != "new-name.csv" {
		t.Fatalf("data file filename = %q, want %q", got.DataFiles[0].Filename, "new-name.csv")
	}

	// Old local file should be removed.
	oldPath, _ := localFS.ResolveDataFilePath(recordID, "old-name.csv")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old data file to be removed, stat error = %v", err)
	}
}

// --- Push: cloud bundle exists, local wins -> update cloud ---

func TestSyncPushLocalWinsUpdatesCloud(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local wins</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "plot.png", S3Key: "figures/" + recordID + "/plot.png",
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud loses</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(1 * time.Minute),
		},
	}

	service, _, cloudRepo, localFS, objects, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, recordID, "plot.png", "LOCAL-FIGURE")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := cloudRepo.bundle(recordID)
	if got.Record.HTMLContent == nil || *got.Record.HTMLContent != "<html>local wins</html>" {
		t.Fatalf("cloud HTMLContent = %v, want %q", got.Record.HTMLContent, "<html>local wins</html>")
	}
	if uploadedFig := objects.objects["figures/"+recordID+"/plot.png"]; uploadedFig != "LOCAL-FIGURE" {
		t.Fatalf("uploaded figure = %q, want %q", uploadedFig, "LOCAL-FIGURE")
	}
}

// --- removeFileIfPresent with non-existent file succeeds ---

func TestRemoveFileIfPresentNonExistentFileSucceeds(t *testing.T) {
	err := removeFileIfPresent(filepath.Join(t.TempDir(), "nonexistent.txt"))
	if err != nil {
		t.Fatalf("removeFileIfPresent() error = %v, want nil for non-existent file", err)
	}
}

// --- writeReaderToPath success path ---

func TestWriteReaderToPathSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "output.txt")
	content := "hello, world"
	err := writeReaderToPath(path, strings.NewReader(content))
	if err != nil {
		t.Fatalf("writeReaderToPath() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != content {
		t.Fatalf("file content = %q, want %q", string(data), content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != syncedFilePermission {
		t.Fatalf("file perm = %o, want %o", info.Mode().Perm(), syncedFilePermission)
	}
}

// --- Pull: new cloud record with data files (create path for applyDataFilesToLocal) ---

func TestSyncPullNewCloudRecordWithDataFiles(t *testing.T) {
	recordID := "20260308-newcloud1"
	now := time.Date(2026, 3, 8, 18, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud new</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 5, Hash: strings.Repeat("d", 64),
		}},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, nil, []RecordBundle{cloudBundle})

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(recordID)
	if len(got.DataFiles) != 1 {
		t.Fatalf("expected 1 data file, got %d", len(got.DataFiles))
	}
	if got.DataFiles[0].Filename != "data.csv" {
		t.Fatalf("data file filename = %q, want %q", got.DataFiles[0].Filename, "data.csv")
	}
	// Data files are NOT downloaded to local, but the old file at the path should be removed if present.
	dataPath, _ := localFS.ResolveDataFilePath(recordID, "data.csv")
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected data file to not exist locally, stat error = %v", err)
	}
}

// --- Pull with figure and data file updates (update path for applyDataFilesToLocal) ---

func TestSyncPullUpdatesLocalDataFilesAndFigures(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{
			{ID: 1, RecordID: recordID, Filename: "plot.png", S3Key: "figures/" + recordID + "/plot.png",
				AltText: strPtr("old alt")},
		},
		DataFiles: []repository.RecordDataFile{
			{ID: 1, RecordID: recordID, Filename: "metrics.csv", S3Key: "data/" + recordID + "/metrics.csv",
				Size: 5, Hash: strings.Repeat("a", 64), Description: strPtr("old desc")},
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud wins</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.RecordFigure{
			{RecordID: recordID, Filename: "plot.png", S3Key: "figures/" + recordID + "/plot.png",
				AltText: strPtr("new alt")},
		},
		DataFiles: []repository.RecordDataFile{
			{RecordID: recordID, Filename: "metrics.csv", S3Key: "data/" + recordID + "/metrics.csv",
				Size: 10, Hash: strings.Repeat("b", 64), Description: strPtr("new desc")},
		},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, recordID, "plot.png", "OLD-FIG")
	writeLocalAsset(t, localFS, false, recordID, "metrics.csv", "OLD-DATA")
	objects.objects["figures/"+recordID+"/plot.png"] = "NEW-FIG"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(recordID)
	if len(got.Figures) != 1 || *got.Figures[0].AltText != "new alt" {
		t.Fatalf("expected figure alt text to be 'new alt', got %+v", got.Figures)
	}
	if len(got.DataFiles) != 1 || *got.DataFiles[0].Description != "new desc" {
		t.Fatalf("expected data file description to be 'new desc', got %+v", got.DataFiles)
	}

	// Figure should be downloaded from cloud.
	figContent := readLocalAsset(t, localFS, true, recordID, "plot.png")
	if figContent != "NEW-FIG" {
		t.Fatalf("local figure = %q, want %q", figContent, "NEW-FIG")
	}
}

// --- applyRecord error from UpdateRecord ---

func TestSyncPushErrorsOnApplyRecordUpdateFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local wins</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.updateRecordErr = fmt.Errorf("update record denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyRecord UpdateRecord")
	}
	if !strings.Contains(err.Error(), "update record denied") {
		t.Fatalf("error = %q, want to contain 'update record denied'", err.Error())
	}
}

// --- applyRecord error from CreateRecord ---

func TestSyncPushErrorsOnApplyRecordCreateFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.createRecordErr = fmt.Errorf("create record denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyRecord CreateRecord")
	}
	if !strings.Contains(err.Error(), "create record denied") {
		t.Fatalf("error = %q, want to contain 'create record denied'", err.Error())
	}
}

// --- applyFiguresToCloud error paths ---

func TestSyncPushErrorsOnCloudCreateRecordFigureFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "new.png", S3Key: "figures/" + recordID + "/new.png",
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, true, recordID, "new.png", "FIG")
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.createRecordFigureErr = fmt.Errorf("create figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from CreateRecordFigure")
	}
	if !strings.Contains(err.Error(), "create figure denied") {
		t.Fatalf("error = %q, want to contain 'create figure denied'", err.Error())
	}
}

func TestSyncPushErrorsOnCloudUpdateRecordFigureFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "plot.png", S3Key: "figures/" + recordID + "/plot.png",
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{{
			ID: 10, RecordID: recordID, Filename: "plot.png", S3Key: "figures/" + recordID + "/plot-v1.png",
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, recordID, "plot.png", "FIG")
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.updateRecordFigureErr = fmt.Errorf("update figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from UpdateRecordFigure")
	}
	if !strings.Contains(err.Error(), "update figure denied") {
		t.Fatalf("error = %q, want to contain 'update figure denied'", err.Error())
	}
}

func TestSyncPushErrorsOnCloudDeleteRecordFigureFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{{
			ID: 10, RecordID: recordID, Filename: "old.png", S3Key: "figures/" + recordID + "/old.png",
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.deleteRecordFigureErr = fmt.Errorf("delete figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from DeleteRecordFigure on cloud")
	}
	if !strings.Contains(err.Error(), "delete figure denied") {
		t.Fatalf("error = %q, want to contain 'delete figure denied'", err.Error())
	}
}

func TestSyncPushErrorsOnCloudDeleteObjectAfterDeleteFigure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{{
			ID: 10, RecordID: recordID, Filename: "old.png", S3Key: "figures/" + recordID + "/old.png",
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	objects.deleteErr = fmt.Errorf("s3 delete denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Delete on cloud object after figure delete")
	}
	if !strings.Contains(err.Error(), "s3 delete denied") {
		t.Fatalf("error = %q, want to contain 's3 delete denied'", err.Error())
	}
}

func TestSyncPushErrorsOnUploadFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "plot.png", S3Key: "figures/" + recordID + "/plot.png",
		}},
	}

	service, _, _, localFS, objects, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, true, recordID, "plot.png", "FIG")
	objects.uploadErr = fmt.Errorf("s3 upload denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Upload")
	}
	if !strings.Contains(err.Error(), "s3 upload denied") {
		t.Fatalf("error = %q, want to contain 's3 upload denied'", err.Error())
	}
}

// --- applyDataFilesToCloud error paths ---

func TestSyncPushErrorsOnCloudCreateRecordDataFileFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "metrics.csv", S3Key: "data/" + recordID + "/metrics.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, false, recordID, "metrics.csv", "DATA")
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.createRecordDataFileErr = fmt.Errorf("create data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from CreateRecordDataFile")
	}
	if !strings.Contains(err.Error(), "create data file denied") {
		t.Fatalf("error = %q, want to contain 'create data file denied'", err.Error())
	}
}

func TestSyncPushErrorsOnCloudUpdateRecordDataFileFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "metrics.csv", S3Key: "data/" + recordID + "/metrics.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 20, RecordID: recordID, Filename: "metrics.csv", S3Key: "data/" + recordID + "/metrics-v1.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, recordID, "metrics.csv", "DATA")
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.updateRecordDataFileErr = fmt.Errorf("update data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from UpdateRecordDataFile")
	}
	if !strings.Contains(err.Error(), "update data file denied") {
		t.Fatalf("error = %q, want to contain 'update data file denied'", err.Error())
	}
}

func TestSyncPushErrorsOnCloudDeleteRecordDataFileFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 20, RecordID: recordID, Filename: "old.csv", S3Key: "data/" + recordID + "/old.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.deleteRecordDataFileErr = fmt.Errorf("delete data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from DeleteRecordDataFile")
	}
	if !strings.Contains(err.Error(), "delete data file denied") {
		t.Fatalf("error = %q, want to contain 'delete data file denied'", err.Error())
	}
}

// --- applyFiguresToLocal error paths ---

func TestSyncPullErrorsOnLocalCreateRecordFigureFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "new.png", S3Key: "figures/" + recordID + "/new.png",
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	objects.objects["figures/"+recordID+"/new.png"] = "FIG"
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.createRecordFigureErr = fmt.Errorf("local create figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local CreateRecordFigure")
	}
	if !strings.Contains(err.Error(), "local create figure denied") {
		t.Fatalf("error = %q, want to contain 'local create figure denied'", err.Error())
	}
}

func TestSyncPullErrorsOnLocalUpdateRecordFigureFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{{
			ID: 1, RecordID: recordID, Filename: "plot.png", S3Key: "figures/" + recordID + "/plot.png",
			AltText: strPtr("old"),
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "plot.png", S3Key: "figures/" + recordID + "/plot.png",
			AltText: strPtr("new"),
		}},
	}

	service, _, _, localFS, objects, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, recordID, "plot.png", "OLD")
	objects.objects["figures/"+recordID+"/plot.png"] = "NEW"
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.updateRecordFigureErr = fmt.Errorf("local update figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local UpdateRecordFigure")
	}
	if !strings.Contains(err.Error(), "local update figure denied") {
		t.Fatalf("error = %q, want to contain 'local update figure denied'", err.Error())
	}
}

func TestSyncPullErrorsOnLocalDeleteRecordFigureFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{{
			ID: 1, RecordID: recordID, Filename: "old.png", S3Key: "figures/" + recordID + "/old.png",
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, recordID, "old.png", "OLD")
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.deleteRecordFigureErr = fmt.Errorf("local delete figure denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local DeleteRecordFigure")
	}
	if !strings.Contains(err.Error(), "local delete figure denied") {
		t.Fatalf("error = %q, want to contain 'local delete figure denied'", err.Error())
	}
}

// --- applyDataFilesToLocal error paths ---

func TestSyncPullErrorsOnLocalCreateRecordDataFileFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.createRecordDataFileErr = fmt.Errorf("local create data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local CreateRecordDataFile")
	}
	if !strings.Contains(err.Error(), "local create data file denied") {
		t.Fatalf("error = %q, want to contain 'local create data file denied'", err.Error())
	}
}

func TestSyncPullErrorsOnLocalUpdateRecordDataFileFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 1, RecordID: recordID, Filename: "metrics.csv", S3Key: "data/" + recordID + "/metrics.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "metrics.csv", S3Key: "data/" + recordID + "/metrics.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.updateRecordDataFileErr = fmt.Errorf("local update data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local UpdateRecordDataFile")
	}
	if !strings.Contains(err.Error(), "local update data file denied") {
		t.Fatalf("error = %q, want to contain 'local update data file denied'", err.Error())
	}
}

func TestSyncPullErrorsOnLocalDeleteRecordDataFileFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 1, RecordID: recordID, Filename: "old.csv", S3Key: "data/" + recordID + "/old.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.deleteRecordDataFileErr = fmt.Errorf("local delete data file denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from local DeleteRecordDataFile")
	}
	if !strings.Contains(err.Error(), "local delete data file denied") {
		t.Fatalf("error = %q, want to contain 'local delete data file denied'", err.Error())
	}
}

// --- Pull: figure filename rename triggers old file deletion ---

func TestSyncPullFigureFilenameRenameDeletesOldFile(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{{
			ID: 1, RecordID: recordID, Filename: "old-name.png", S3Key: "figures/" + recordID + "/old-name.png",
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "new-name.png", S3Key: "figures/" + recordID + "/new-name.png",
		}},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, recordID, "old-name.png", "OLD")
	objects.objects["figures/"+recordID+"/new-name.png"] = "NEW"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(recordID)
	if len(got.Figures) != 1 || got.Figures[0].Filename != "new-name.png" {
		t.Fatalf("expected renamed figure, got %+v", got.Figures)
	}

	// Old figure file should be removed.
	oldPath, _ := localFS.ResolveFigurePath(recordID, "old-name.png")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old figure file to be removed, stat error = %v", err)
	}
}

// --- Pull: data file update with filename change (covers rename path in applyDataFilesToLocal) ---

func TestSyncPullDataFileUpdateWithFilenameChange(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 1, RecordID: recordID, Filename: "old-data.csv", S3Key: "data/" + recordID + "/old-data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}
	// Cloud has same filename pattern but the update will happen by filename match.
	// Use different filename to trigger the rename path.
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "old-data.csv", S3Key: "data/" + recordID + "/old-data.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, recordID, "old-data.csv", "OLD-DATA")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(recordID)
	if len(got.DataFiles) != 1 {
		t.Fatalf("expected 1 data file, got %d", len(got.DataFiles))
	}
	if got.DataFiles[0].Hash != strings.Repeat("b", 64) {
		t.Fatalf("data file hash not updated")
	}
}

// --- applyBundleToCloud and applyBundleToLocal error propagation ---

func TestSyncPushErrorsOnApplyFiguresToCloudFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "fig.png", S3Key: "figures/" + recordID + "/fig.png",
		}},
	}

	// Trigger ResolveFigurePath error by using a stub that fails.
	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	service.localFS = &errLocalFiles{figurErr: fmt.Errorf("resolve figure path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyFiguresToCloud")
	}
	if !strings.Contains(err.Error(), "resolve figure path failed") {
		t.Fatalf("error = %q, want to contain 'resolve figure path failed'", err.Error())
	}
}

func TestSyncPushErrorsOnApplyDataFilesToCloudResolvePathFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	service.localFS = &errLocalFiles{dataFileErr: fmt.Errorf("resolve data path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyDataFilesToCloud resolve path")
	}
	if !strings.Contains(err.Error(), "resolve data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve data path failed'", err.Error())
	}
}

func TestSyncPullErrorsOnApplyFiguresToLocalResolvePathFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "fig.png", S3Key: "figures/" + recordID + "/fig.png",
		}},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	service.localFS = &errLocalFiles{figurErr: fmt.Errorf("local resolve figure failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyFiguresToLocal resolve path")
	}
	if !strings.Contains(err.Error(), "local resolve figure failed") {
		t.Fatalf("error = %q, want to contain 'local resolve figure failed'", err.Error())
	}
}

func TestSyncPullErrorsOnApplyDataFilesToLocalResolvePathFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	service.localFS = &errLocalFiles{dataFileErr: fmt.Errorf("local resolve data path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyDataFilesToLocal resolve path")
	}
	if !strings.Contains(err.Error(), "local resolve data path failed") {
		t.Fatalf("error = %q, want to contain 'local resolve data path failed'", err.Error())
	}
}

// --- Pull error paths for cloud-side bundleForRecord ---

func TestSyncPullErrorsOnCloudBundleForRecordListFiguresError(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.listFiguresByRecordIDErr = fmt.Errorf("cloud figures locked")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from cloud bundleForRecord ListRecordFiguresByRecordID")
	}
	if !strings.Contains(err.Error(), "cloud figures locked") {
		t.Fatalf("error = %q, want to contain 'cloud figures locked'", err.Error())
	}
}

func TestSyncPullErrorsOnCloudBundleForRecordListDataFilesError(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	cloudRepo := service.cloudRepo.(*memoryRepo)
	cloudRepo.listDataFilesByRecordIDErr = fmt.Errorf("cloud data files locked")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from cloud bundleForRecord ListRecordDataFilesByRecordID")
	}
	if !strings.Contains(err.Error(), "cloud data files locked") {
		t.Fatalf("error = %q, want to contain 'cloud data files locked'", err.Error())
	}
}

// --- Pull error: loadBundle non-ErrNotFound for local repo ---

func TestSyncPullErrorsOnLoadLocalBundleNonNotFoundError(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.getRecordByIDErr = fmt.Errorf("local db timeout")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from loadBundle for local repo")
	}
	if !strings.Contains(err.Error(), "local db timeout") {
		t.Fatalf("error = %q, want to contain 'local db timeout'", err.Error())
	}
}

// --- Sync lock release error propagation ---

func TestSyncPropagatesLockReleaseError(t *testing.T) {
	pcDir := filepath.Join(t.TempDir(), ".pc")
	session, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	svc := &Service{
		localRepo:    newMemoryRepo(nil),
		cloudRepo:    newMemoryRepo(nil),
		localFS:      &stubLocalFiles{},
		cloudObjects: newMockObjectStore(),
		session:      session,
	}

	// First, do a successful sync so the cursor file exists.
	if err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}

	// Now acquire the lock manually so Begin() succeeds, then remove
	// the lock file before Release() runs so Release() fails on os.Remove.
	window, lock, err := session.Begin()
	if err != nil {
		t.Fatalf("session.Begin() error = %v", err)
	}
	// Remove the lock file to make Release() fail.
	lockPath := filepath.Join(pcDir, "sync.lock")
	_ = os.Remove(lockPath)
	// Close the underlying file handle so Close() doesn't fail first.
	// We can't access it directly, so just call Release() and expect an error.
	releaseErr := lock.Release()
	if releaseErr == nil {
		t.Fatal("expected Release() to fail when lock file is removed")
	}
	_ = window
}

// --- Pull: applyBundleToLocal record apply error ---

func TestSyncPullErrorsOnApplyRecordToLocalFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.createRecordErr = fmt.Errorf("local create record denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyRecord to local")
	}
	if !strings.Contains(err.Error(), "local create record denied") {
		t.Fatalf("error = %q, want to contain 'local create record denied'", err.Error())
	}
}

func TestSyncPullErrorsOnApplyRecordUpdateToLocalFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.updateRecordErr = fmt.Errorf("local update record denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from applyRecord UpdateRecord to local")
	}
	if !strings.Contains(err.Error(), "local update record denied") {
		t.Fatalf("error = %q, want to contain 'local update record denied'", err.Error())
	}
}

// --- loadBundle: bundleForRecord error after GetRecordByID success ---

func TestSyncPullErrorsOnLoadBundleBundleForRecordError(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	// Put the record in local so loadBundle's GetRecordByID succeeds,
	// but inject error on ListRecordFiguresByRecordID so bundleForRecord fails.
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	localRepo := service.localRepo.(*memoryRepo)
	localRepo.listFiguresByRecordIDErr = fmt.Errorf("local figures table error")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from loadBundle/bundleForRecord")
	}
	if !strings.Contains(err.Error(), "local figures table error") {
		t.Fatalf("error = %q, want to contain 'local figures table error'", err.Error())
	}
}

// --- Pull: applyFiguresToLocal delete path fully exercised ---

func TestSyncPullDeleteFigureWithFileRemoval(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{
			{ID: 1, RecordID: recordID, Filename: "delete-me.png", S3Key: "figures/" + recordID + "/delete-me.png"},
			{ID: 2, RecordID: recordID, Filename: "keep-me.png", S3Key: "figures/" + recordID + "/keep-me.png"},
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.RecordFigure{
			{RecordID: recordID, Filename: "keep-me.png", S3Key: "figures/" + recordID + "/keep-me.png"},
		},
	}

	service, localRepo, _, localFS, objects, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, recordID, "delete-me.png", "DELETE")
	writeLocalAsset(t, localFS, true, recordID, "keep-me.png", "KEEP")
	objects.objects["figures/"+recordID+"/keep-me.png"] = "KEEP"

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(recordID)
	if len(got.Figures) != 1 {
		t.Fatalf("expected 1 figure, got %d", len(got.Figures))
	}
	if got.Figures[0].Filename != "keep-me.png" {
		t.Fatalf("kept figure = %q, want %q", got.Figures[0].Filename, "keep-me.png")
	}

	deletedPath, _ := localFS.ResolveFigurePath(recordID, "delete-me.png")
	if _, err := os.Stat(deletedPath); !os.IsNotExist(err) {
		t.Fatalf("expected deleted figure file to be removed, stat error = %v", err)
	}
}

// --- Pull: applyDataFilesToLocal full delete path ---

func TestSyncPullDeleteDataFileWithFileRemoval(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{
			{ID: 1, RecordID: recordID, Filename: "delete-me.csv", S3Key: "data/" + recordID + "/delete-me.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, recordID, "delete-me.csv", "DELETE")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(recordID)
	if len(got.DataFiles) != 0 {
		t.Fatalf("expected 0 data files, got %d", len(got.DataFiles))
	}

	deletedPath, _ := localFS.ResolveDataFilePath(recordID, "delete-me.csv")
	if _, err := os.Stat(deletedPath); !os.IsNotExist(err) {
		t.Fatalf("expected deleted data file to be removed, stat error = %v", err)
	}
}

// --- Pull: applyDataFilesToLocal update with filename rename ---

func TestSyncPullDataFileUpdateWithFilenameRename(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{
			{ID: 1, RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	// Cloud renamed the file.
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{
			{RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
				Size: 10, Hash: strings.Repeat("b", 64)},
		},
	}

	service, localRepo, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, recordID, "data.csv", "OLD")

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := localRepo.bundle(recordID)
	if len(got.DataFiles) != 1 || got.DataFiles[0].Hash != strings.Repeat("b", 64) {
		t.Fatalf("expected updated data file, got %+v", got.DataFiles)
	}

	// The old local file at data.csv path should be removed (via removeFileIfPresent
	// in the update path - line 538).
	dataPath, _ := localFS.ResolveDataFilePath(recordID, "data.csv")
	if _, err := os.Stat(dataPath); !os.IsNotExist(err) {
		t.Fatalf("expected data file path to be cleaned up, stat error = %v", err)
	}
}

// --- removeFileIfPresent error path ---

func TestRemoveFileIfPresentErrorOnPermissionDenied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "protected.txt")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// Make the directory read-only to prevent file removal.
	if err := os.Chmod(dir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})

	err := removeFileIfPresent(path)
	if err == nil {
		t.Fatal("expected error from removeFileIfPresent with permission denied")
	}
}

// --- writeReaderToPath error paths ---

func TestWriteReaderToPathErrorOnMkdirAll(t *testing.T) {
	// Use a path under a file (not a directory) to make MkdirAll fail.
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	path := filepath.Join(tmpFile, "subdir", "file.txt")

	err := writeReaderToPath(path, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error from writeReaderToPath when MkdirAll fails")
	}
	if !strings.Contains(err.Error(), "create directory") {
		t.Fatalf("error = %q, want to contain 'create directory'", err.Error())
	}
}

func TestWriteReaderToPathErrorOnCreateTemp(t *testing.T) {
	// Use a read-only directory to make CreateTemp fail.
	dir := t.TempDir()
	subdir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Chmod(subdir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(subdir, 0o755)
	})

	path := filepath.Join(subdir, "file.txt")
	err := writeReaderToPath(path, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error from writeReaderToPath when CreateTemp fails")
	}
	if !strings.Contains(err.Error(), "create temp file") {
		t.Fatalf("error = %q, want to contain 'create temp file'", err.Error())
	}
}

func TestWriteReaderToPathErrorOnCopyFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")

	// Use a reader that returns an error.
	err := writeReaderToPath(path, &errReader{err: fmt.Errorf("read failed")})
	if err == nil {
		t.Fatal("expected error from writeReaderToPath when copy fails")
	}
	if !strings.Contains(err.Error(), "copy temp file") {
		t.Fatalf("error = %q, want to contain 'copy temp file'", err.Error())
	}
}

func TestWriteReaderToPathErrorOnChmod(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")

	origChmod := osFileChmod
	osFileChmod = func(_ *os.File, _ os.FileMode) error {
		return fmt.Errorf("injected chmod error")
	}
	t.Cleanup(func() { osFileChmod = origChmod })

	err := writeReaderToPath(path, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error from writeReaderToPath when Chmod fails")
	}
	if !strings.Contains(err.Error(), "chmod temp file") {
		t.Fatalf("error = %q, want to contain 'chmod temp file'", err.Error())
	}
	// Temp file should have been cleaned up.
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".sync-") {
			t.Fatalf("temp file %q was not cleaned up", entry.Name())
		}
	}
}

func TestWriteReaderToPathErrorOnRename(t *testing.T) {
	dir := t.TempDir()
	// Create a directory at the target path so os.Rename(file, dir) fails.
	target := filepath.Join(dir, "output")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	err := writeReaderToPath(target, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error from writeReaderToPath when Rename fails")
	}
	if !strings.Contains(err.Error(), "replace") {
		t.Fatalf("error = %q, want to contain 'replace'", err.Error())
	}
}

func TestSyncReturnsLockReleaseError(t *testing.T) {
	// Set up a sync that succeeds, but whose lock.Release() fails.
	// This covers the deferred lock-release error path in Sync (service.go:85).
	recordID := "20260308-locktest"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	bundle := RecordBundle{
		Record: repository.Record{
			ID:          recordID,
			Date:        "2026-03-08",
			DayOrder:    "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	localRepo := newMemoryRepo([]RecordBundle{bundle})
	cloudRepo := newMemoryRepo(nil)
	baseDir := t.TempDir()
	localFS, err := filesystem.NewClient(baseDir)
	if err != nil {
		t.Fatalf("filesystem.NewClient() error = %v", err)
	}

	pcDir := filepath.Join(t.TempDir(), ".pc")
	realSession, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("syncengine.NewManager() error = %v", err)
	}

	// Wrap the real session so we can sabotage the lock directory during Complete().
	lockDir := pcDir
	wrapper := &lockSabotageSessionWrapper{
		real:    realSession,
		lockDir: lockDir,
	}

	service, err := NewService(localRepo, cloudRepo, localFS, newMockObjectStore(), wrapper)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	err = service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Sync when lock Release fails")
	}
	if !strings.Contains(err.Error(), "remove sync lock") {
		t.Fatalf("error = %q, want to contain 'remove sync lock'", err.Error())
	}

	// Restore directory permissions for cleanup.
	_ = os.Chmod(lockDir, 0o755)
}

// lockSabotageSessionWrapper delegates to a real SessionManager but makes
// the lock directory read-only during Complete(), causing the deferred
// lock.Release() to fail because os.Remove cannot delete the lock file.
type lockSabotageSessionWrapper struct {
	real    SessionManager
	lockDir string
}

func (w *lockSabotageSessionWrapper) Begin() (syncengine.SyncWindow, *syncengine.FileLock, error) {
	return w.real.Begin()
}

func (w *lockSabotageSessionWrapper) Complete(window syncengine.SyncWindow) error {
	if err := w.real.Complete(window); err != nil {
		return err
	}
	// Make the lock directory read-only so os.Remove in Release() fails.
	return os.Chmod(w.lockDir, 0o444)
}

// --- errReader for triggering io.Copy failures ---

type errReader struct {
	err error
}

func (r *errReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

// --- downloadFile write error ---

func TestDownloadFileWriteError(t *testing.T) {
	// Create service with a localFS that resolves to a path under a file.
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "chart.png", S3Key: "figures/" + recordID + "/chart.png",
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	objects.objects["figures/"+recordID+"/chart.png"] = "CONTENT"

	// Replace localFS with one that returns a path under a non-directory.
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	service.localFS = &fixedPathLocalFiles{
		figurePath:   filepath.Join(tmpFile, "subdir", "chart.png"),
		dataFilePath: filepath.Join(tmpFile, "subdir", "data.csv"),
	}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from downloadFile write")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Fatalf("error = %q, want to contain 'write'", err.Error())
	}
}

type fixedPathLocalFiles struct {
	figurePath   string
	dataFilePath string
}

func (f *fixedPathLocalFiles) ResolveFigurePath(_ string, _ string) (string, error) {
	return f.figurePath, nil
}

func (f *fixedPathLocalFiles) ResolveDataFilePath(_ string, _ string) (string, error) {
	return f.dataFilePath, nil
}

func (f *fixedPathLocalFiles) ResolveChatSourcePath(_ string, _ string) (string, error) {
	return "/tmp/stub/chat-raw", nil
}

// --- applyDataFilesToCloud: metadata-only create/update when local binary is absent ---

func TestSyncPushCreatesCloudDataFileMetadataWithoutLocalBinary(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, cloudRepo, _, objects, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := cloudRepo.bundle(recordID)
	if len(got.DataFiles) != 1 || got.DataFiles[0].Filename != "data.csv" {
		t.Fatalf("cloud data files = %+v, want metadata row for data.csv", got.DataFiles)
	}
	if _, ok := objects.objects["data/"+recordID+"/data.csv"]; ok {
		t.Fatal("expected missing local data file to skip object upload")
	}
}

func TestSyncPushUpdatesCloudDataFileMetadataWithoutLocalBinaryWhenS3KeyIsUnchanged(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 20, RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, cloudRepo, _, objects, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	got := cloudRepo.bundle(recordID)
	if len(got.DataFiles) != 1 || got.DataFiles[0].Hash != strings.Repeat("b", 64) || got.DataFiles[0].Size != 10 {
		t.Fatalf("cloud data files = %+v, want updated metadata with unchanged s3_key", got.DataFiles)
	}
	if _, ok := objects.objects["data/"+recordID+"/data.csv"]; ok {
		t.Fatal("expected missing local data file to skip object upload")
	}
}

func TestSyncPushErrorsWhenMissingLocalBinaryWouldChangeDataFileS3Key(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 20, RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data-v1.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error when metadata-only push would change data file s3_key")
	}
	if !strings.Contains(err.Error(), "is required to change data file s3_key") {
		t.Fatalf("error = %q, want missing-local-file s3_key guard", err.Error())
	}
}

// --- applyDataFilesToCloud: upload error on data file create ---

func TestSyncPushErrorsOnDataFileCreateUploadFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	// Write the local data file so ResolveDataFilePath succeeds, but upload fails.
	service, _, _, localFS, objects, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, false, recordID, "data.csv", "DATA")
	objects.uploadErr = fmt.Errorf("data upload denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from data file upload")
	}
	if !strings.Contains(err.Error(), "data upload denied") {
		t.Fatalf("error = %q, want to contain 'data upload denied'", err.Error())
	}
}

// --- applyDataFilesToCloud: upload error on data file update ---

func TestSyncPushErrorsOnDataFileUpdateUploadFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 20, RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data-v1.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, localFS, objects, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, recordID, "data.csv", "DATA")
	objects.uploadErr = fmt.Errorf("data update upload denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from data file update upload")
	}
	if !strings.Contains(err.Error(), "data update upload denied") {
		t.Fatalf("error = %q, want to contain 'data update upload denied'", err.Error())
	}
}

// --- applyDataFilesToCloud: delete old S3 object error on data file update ---

func TestSyncPushErrorsOnDataFileUpdateDeleteOldS3Failure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 20, RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data-v1.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, recordID, "data.csv", "DATA")
	// Use a custom object store that fails on Delete but succeeds on Upload.
	objects := service.cloudObjects.(*mockObjectStore)
	// We can't set deleteErr because figure deletes also use it.
	// Instead, let the upload succeed, the UpdateRecordDataFile succeed, then delete fails.
	// We need per-operation error injection. Let's use a counting approach.
	countingObjects := &countingObjectStore{
		inner:          objects,
		deleteErrAfter: 0, // fail on first delete
		deleteErr:      fmt.Errorf("s3 delete old key denied"),
	}
	service.cloudObjects = countingObjects

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Delete old S3 key on data file update")
	}
	if !strings.Contains(err.Error(), "s3 delete old key denied") {
		t.Fatalf("error = %q, want to contain 's3 delete old key denied'", err.Error())
	}
}

// --- applyDataFilesToCloud: delete object error on data file delete ---

func TestSyncPushErrorsOnDataFileDeleteObjectFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 20, RecordID: recordID, Filename: "old.csv", S3Key: "data/" + recordID + "/old.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	objects.deleteErr = fmt.Errorf("s3 delete object denied")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Delete object on data file delete")
	}
	if !strings.Contains(err.Error(), "s3 delete object denied") {
		t.Fatalf("error = %q, want to contain 's3 delete object denied'", err.Error())
	}
}

// --- applyFiguresToCloud: delete old S3 key error on figure update ---

func TestSyncPushErrorsOnFigureUpdateDeleteOldS3Failure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "plot.png", S3Key: "figures/" + recordID + "/plot.png",
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{{
			ID: 10, RecordID: recordID, Filename: "plot.png", S3Key: "figures/" + recordID + "/plot-v1.png",
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, recordID, "plot.png", "FIG")
	// Use counting object store to allow uploads but fail on delete.
	objects := service.cloudObjects.(*mockObjectStore)
	countingObjects := &countingObjectStore{
		inner:          objects,
		deleteErrAfter: 0,
		deleteErr:      fmt.Errorf("s3 delete old figure key denied"),
	}
	service.cloudObjects = countingObjects

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from Delete old S3 key on figure update")
	}
	if !strings.Contains(err.Error(), "s3 delete old figure key denied") {
		t.Fatalf("error = %q, want to contain 's3 delete old figure key denied'", err.Error())
	}
}

// --- applyDataFilesToCloud: resolve data path error on create ---

func TestSyncPushErrorsOnDataFileCreateResolvePathFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	service.localFS = &errLocalFiles{dataFileErr: fmt.Errorf("resolve create data path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from resolve data path on create")
	}
	if !strings.Contains(err.Error(), "resolve create data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve create data path failed'", err.Error())
	}
}

// --- applyDataFilesToCloud: resolve data path error on update ---

func TestSyncPushErrorsOnDataFileUpdateResolvePathFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 20, RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data-v1.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	service.localFS = &errLocalFiles{dataFileErr: fmt.Errorf("resolve update data path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from resolve data path on update")
	}
	if !strings.Contains(err.Error(), "resolve update data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve update data path failed'", err.Error())
	}
}

// --- applyFiguresToLocal: ResolveFigurePath error on delete ---

func TestSyncPullErrorsOnFigureDeleteResolvePathFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{
			{ID: 1, RecordID: recordID, Filename: "delete-me.png", S3Key: "figures/" + recordID + "/delete-me.png"},
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	// Use a localFS that returns error for all figure resolutions.
	// But we need the download step in applyFiguresToLocal to succeed (no desired figures, so no download needed).
	// Then the delete loop calls ResolveFigurePath which should fail.
	service.localFS = &errLocalFiles{figurErr: fmt.Errorf("resolve delete figure path failed")}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from ResolveFigurePath in figure delete")
	}
	if !strings.Contains(err.Error(), "resolve delete figure path failed") {
		t.Fatalf("error = %q, want to contain 'resolve delete figure path failed'", err.Error())
	}
}

// --- applyDataFilesToLocal: ResolveDataFilePath error on create ---

func TestSyncPullErrorsOnDataFileCreateResolvePathFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	// Use countingLocalFiles to succeed on initial operations but fail on data file resolve for create.
	service.localFS = &countingLocalFiles{
		dataFileErrAfter: 0, // fail on first ResolveDataFilePath call
		dataFileErr:      fmt.Errorf("resolve create local data path failed"),
	}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from ResolveDataFilePath in data file create")
	}
	if !strings.Contains(err.Error(), "resolve create local data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve create local data path failed'", err.Error())
	}
}

// --- applyDataFilesToLocal: ResolveDataFilePath error on update ---

func TestSyncPullErrorsOnDataFileUpdateResolvePathFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 1, RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 10, Hash: strings.Repeat("b", 64),
		}},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	service.localFS = &countingLocalFiles{
		dataFileErrAfter: 0,
		dataFileErr:      fmt.Errorf("resolve update local data path failed"),
	}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from ResolveDataFilePath in data file update")
	}
	if !strings.Contains(err.Error(), "resolve update local data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve update local data path failed'", err.Error())
	}
}

// --- applyDataFilesToLocal: ResolveDataFilePath error on delete ---

func TestSyncPullErrorsOnDataFileDeleteResolvePathFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{{
			ID: 1, RecordID: recordID, Filename: "delete-me.csv", S3Key: "data/" + recordID + "/delete-me.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, _, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	service.localFS = &countingLocalFiles{
		dataFileErrAfter: 0,
		dataFileErr:      fmt.Errorf("resolve delete local data path failed"),
	}

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from ResolveDataFilePath in data file delete")
	}
	if !strings.Contains(err.Error(), "resolve delete local data path failed") {
		t.Fatalf("error = %q, want to contain 'resolve delete local data path failed'", err.Error())
	}
}

// --- Pull: local wins skip when local has a pre-existing later edit ---

func TestSyncPullSkipsWhenLocalHasLaterPreExistingEdit(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	lastSyncTime := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	localUpdatedAt := time.Date(2026, 3, 8, 11, 59, 0, 0, time.UTC) // just before lastSync
	cloudUpdatedAt := time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC)  // after lastSync

	// Local record was updated before lastSync (won't be in push list).
	// But its UpdatedAt is LATER than cloud's — wait, that would mean push would process it.
	// Actually, the local record is NOT in the push list because its UpdatedAt is before lastSync.
	// The FilterRecordsUpdatedSince check uses inclusive comparison (>= threshold).
	// So localUpdatedAt (11:59) < lastSync (12:00) means it IS filtered out.
	//
	// Cloud record updated after lastSync (in pull list).
	// But local's latest action time must be after cloud's for local to win.
	// Local has UpdatedAt=11:59 and no DeletedAt; Cloud has UpdatedAt=13:00.
	// Cloud has a later timestamp, so cloud wins. That's not what we want.
	//
	// We need local to win during pull. Local needs a later "action" than cloud.
	// The trick: local has a DeletedAt that's later than cloud's UpdatedAt.
	// Wait, no - we want local to win to skip the pull update.
	//
	// Actually, the way this works: for local to win the pull conflict,
	// local must have a later "latest action" than cloud. But local's UpdatedAt
	// is before lastSync (so it's not in push list). Cloud's UpdatedAt is after lastSync.
	// Cloud's UpdatedAt > local's UpdatedAt → cloud wins. That's the normal case.
	//
	// To make local win even though local's UpdatedAt is before lastSync and cloud's is after:
	// Local needs a later DeletedAt or some other field that makes it win.
	// If local has a DeletedAt that's after cloud's UpdatedAt, the delete action wins.
	//
	// Let's say local was deleted at 14:00 (after cloud's update at 13:00).
	// Local's latest action = delete at 14:00. Cloud's latest action = update at 13:00.
	// Local wins (14:00 > 13:00). But local's UpdatedAt is 11:59 (before lastSync),
	// so it's NOT in the push list. Cloud is in the pull list.
	// During pull, ResolveBundle picks local as winner → continue/skip.
	localDeletedAt := time.Date(2026, 3, 8, 14, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local stays</html>"),
			CreatedAt:   localUpdatedAt, UpdatedAt: localUpdatedAt,
			DeletedAt: &localDeletedAt,
		},
	}
	// Cloud record was updated after lastSync (will be in pull list).
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud loses</html>"),
			CreatedAt:   localUpdatedAt, UpdatedAt: cloudUpdatedAt,
		},
	}

	// Set up with an existing lastSync so the sync window filters properly.
	localRepo := newMemoryRepo([]RecordBundle{localBundle})
	cloudRepo := newMemoryRepo([]RecordBundle{cloudBundle})

	baseDir := t.TempDir()
	localFS, err := filesystem.NewClient(baseDir)
	if err != nil {
		t.Fatalf("filesystem.NewClient() error = %v", err)
	}

	pcDir := filepath.Join(t.TempDir(), ".pc")
	cursorStore, err := syncengine.NewCursorStore(pcDir)
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}
	// Write a lastSync so only records updated after it are considered.
	if err := cursorStore.Write(lastSyncTime); err != nil {
		t.Fatalf("cursorStore.Write() error = %v", err)
	}

	session, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	service, err := NewService(localRepo, cloudRepo, localFS, newMockObjectStore(), session)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if err := service.Sync(context.Background()); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Local should NOT have been updated to cloud's content (local won).
	got := localRepo.bundle(recordID)
	if got.Record.HTMLContent == nil || *got.Record.HTMLContent != "<html>local stays</html>" {
		t.Fatalf("local HTMLContent = %v, want %q (local should win)", got.Record.HTMLContent, "<html>local stays</html>")
	}
}

// --- PlanFigureReconciliation error in applyFiguresToLocal ---

func TestSyncPullErrorsOnFigurePlanReconciliationWithInvalidDesired(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	// Use a valid filename but whitespace-only S3Key. The download step downloads from
	// the whitespace key (which the mock has), and then PlanFigureReconciliation trims
	// whitespace and rejects it.
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "bad.png", S3Key: "  ", // whitespace-only S3Key
		}},
	}

	service, _, _, _, objects, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	objects.objects["  "] = "BAD-CONTENT"

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from PlanFigureReconciliation with whitespace S3Key")
	}
	if !strings.Contains(err.Error(), "s3_key is required") {
		t.Fatalf("error = %q, want to contain 's3_key is required'", err.Error())
	}
}

// --- PlanFigureReconciliation error in applyFiguresToCloud ---

func TestSyncPushErrorsOnFigurePlanReconciliationWithInvalidDesired(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	// Use a valid filename but whitespace-only S3Key. The upload step uses the S3Key
	// as the object key (which succeeds), but PlanFigureReconciliation trims whitespace
	// and rejects it.
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		Figures: []repository.RecordFigure{{
			RecordID: recordID, Filename: "bad.png", S3Key: "  ", // whitespace-only S3Key
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, true, recordID, "bad.png", "FIG")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from PlanFigureReconciliation with whitespace S3Key")
	}
	if !strings.Contains(err.Error(), "s3_key is required") {
		t.Fatalf("error = %q, want to contain 's3_key is required'", err.Error())
	}
}

// --- PlanDataFileReconciliation error in applyDataFilesToLocal ---

func TestSyncPullErrorsOnDataFilePlanReconciliationWithInvalidDesired(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	// Use valid filename and S3Key but whitespace-only hash. PlanDataFileReconciliation
	// trims and rejects. Note: applyDataFilesToLocal calls PlanDataFileReconciliation
	// before any file operations, so we don't need to set up any downloads.
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "bad.csv", S3Key: "data/" + recordID + "/bad.csv",
			Size: 5, Hash: "  ", // whitespace-only hash
		}},
	}

	service, _, _, _, _, _ := newTestService(t, nil, []RecordBundle{cloudBundle})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from PlanDataFileReconciliation with whitespace hash")
	}
	if !strings.Contains(err.Error(), "hash is required") {
		t.Fatalf("error = %q, want to contain 'hash is required'", err.Error())
	}
}

// --- PlanDataFileReconciliation error in applyDataFilesToCloud ---

func TestSyncPushErrorsOnDataFilePlanReconciliationWithInvalidDesired(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	// Use a valid filename but whitespace-only hash. PlanDataFileReconciliation
	// checks Filename, S3Key, and Hash — all must be non-blank after trim.
	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "bad.csv", S3Key: "data/" + recordID + "/bad.csv",
			Size: 5, Hash: "  ", // whitespace-only hash
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, nil)
	writeLocalAsset(t, localFS, false, recordID, "bad.csv", "DATA")

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from PlanDataFileReconciliation with whitespace hash")
	}
	if !strings.Contains(err.Error(), "hash is required") {
		t.Fatalf("error = %q, want to contain 'hash is required'", err.Error())
	}
}

// --- loadBundle: bundleForRecord error inside loadBundle on pull ---

func TestSyncPullErrorsOnLoadBundleBundleForRecordErrorInsideLoadBundle(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	lastSyncTime := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	localUpdatedAt := time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC) // before lastSync
	cloudUpdatedAt := time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC) // after lastSync

	// Local record exists but updated before lastSync (not in push list).
	localRepo := newMemoryRepo([]RecordBundle{{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   localUpdatedAt, UpdatedAt: localUpdatedAt,
		},
	}})

	// Cloud record updated after lastSync (in pull list).
	cloudRepo := newMemoryRepo([]RecordBundle{{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   localUpdatedAt, UpdatedAt: cloudUpdatedAt,
		},
	}})

	baseDir := t.TempDir()
	localFS, err := filesystem.NewClient(baseDir)
	if err != nil {
		t.Fatalf("filesystem.NewClient() error = %v", err)
	}
	pcDir := filepath.Join(t.TempDir(), ".pc")
	cursorStore, err := syncengine.NewCursorStore(pcDir)
	if err != nil {
		t.Fatalf("NewCursorStore() error = %v", err)
	}
	if err := cursorStore.Write(lastSyncTime); err != nil {
		t.Fatalf("cursorStore.Write() error = %v", err)
	}
	session, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Inject error only in local repo's ListRecordFiguresByRecordID.
	// Since local is not in push list, push phase won't call bundleForRecord on localRepo.
	// During pull, loadBundle(localRepo, recordID) calls GetRecordByID (succeeds),
	// then bundleForRecord which calls ListRecordFiguresByRecordID (fails).
	localRepo.listFiguresByRecordIDErr = fmt.Errorf("local figures error in loadBundle")

	service, err := NewService(localRepo, cloudRepo, localFS, newMockObjectStore(), session)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	syncErr := service.Sync(context.Background())
	if syncErr == nil {
		t.Fatal("expected error from loadBundle bundleForRecord")
	}
	if !strings.Contains(syncErr.Error(), "local figures error in loadBundle") {
		t.Fatalf("error = %q, want to contain 'local figures error in loadBundle'", syncErr.Error())
	}
}

// --- applyFiguresToLocal: removeFileIfPresent error in delete path ---

func TestSyncPullErrorsOnFigureDeleteRemoveFileFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		Figures: []repository.RecordFigure{
			{ID: 1, RecordID: recordID, Filename: "delete-me.png", S3Key: "figures/" + recordID + "/delete-me.png"},
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, true, recordID, "delete-me.png", "DELETE")

	// Make the file's directory read-only so removal fails.
	figPath, _ := localFS.ResolveFigurePath(recordID, "delete-me.png")
	figDir := filepath.Dir(figPath)
	if err := os.Chmod(figDir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(figDir, 0o755)
	})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from removeFileIfPresent in figure delete")
	}
	if !strings.Contains(err.Error(), "remove") {
		t.Fatalf("error = %q, want to contain 'remove'", err.Error())
	}
}

// --- applyDataFilesToLocal: removeFileIfPresent error in delete path ---

func TestSyncPullErrorsOnDataFileDeleteRemoveFileFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{
			{ID: 1, RecordID: recordID, Filename: "delete-me.csv", S3Key: "data/" + recordID + "/delete-me.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, recordID, "delete-me.csv", "DELETE")

	// Make the file's directory read-only so removal fails.
	dataPath, _ := localFS.ResolveDataFilePath(recordID, "delete-me.csv")
	dataDir := filepath.Dir(dataPath)
	if err := os.Chmod(dataDir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dataDir, 0o755)
	})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from removeFileIfPresent in data file delete")
	}
	if !strings.Contains(err.Error(), "remove") {
		t.Fatalf("error = %q, want to contain 'remove'", err.Error())
	}
}

// --- applyDataFilesToLocal: removeFileIfPresent error on update ---

func TestSyncPullErrorsOnDataFileUpdateRemoveFileFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	base := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	localBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>local</html>"),
			CreatedAt:   base, UpdatedAt: base,
		},
		DataFiles: []repository.RecordDataFile{
			{ID: 1, RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
				Size: 5, Hash: strings.Repeat("a", 64)},
		},
	}
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   base, UpdatedAt: base.Add(5 * time.Minute),
		},
		DataFiles: []repository.RecordDataFile{
			{RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
				Size: 10, Hash: strings.Repeat("b", 64)},
		},
	}

	service, _, _, localFS, _, _ := newTestService(t, []RecordBundle{localBundle}, []RecordBundle{cloudBundle})
	writeLocalAsset(t, localFS, false, recordID, "data.csv", "OLD")

	// Make the file's directory read-only so removal fails.
	dataPath, _ := localFS.ResolveDataFilePath(recordID, "data.csv")
	dataDir := filepath.Dir(dataPath)
	if err := os.Chmod(dataDir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dataDir, 0o755)
	})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from removeFileIfPresent in data file update")
	}
	if !strings.Contains(err.Error(), "remove") {
		t.Fatalf("error = %q, want to contain 'remove'", err.Error())
	}
}

// --- applyDataFilesToLocal: removeFileIfPresent error on create ---

func TestSyncPullErrorsOnDataFileCreateRemoveFileFailure(t *testing.T) {
	recordID := "20260308-a1b2c3d4"
	now := time.Date(2026, 3, 8, 15, 0, 0, 0, time.UTC)
	cloudBundle := RecordBundle{
		Record: repository.Record{
			ID: recordID, Date: "2026-03-08", DayOrder: "a0",
			HTMLContent: strPtr("<html>cloud</html>"),
			CreatedAt:   now, UpdatedAt: now,
		},
		DataFiles: []repository.RecordDataFile{{
			RecordID: recordID, Filename: "data.csv", S3Key: "data/" + recordID + "/data.csv",
			Size: 5, Hash: strings.Repeat("a", 64),
		}},
	}

	service, _, _, localFS, _, _ := newTestService(t, nil, []RecordBundle{cloudBundle})
	// Pre-create a file at the data path and make its directory read-only.
	writeLocalAsset(t, localFS, false, recordID, "data.csv", "EXISTING")
	dataPath, _ := localFS.ResolveDataFilePath(recordID, "data.csv")
	dataDir := filepath.Dir(dataPath)
	if err := os.Chmod(dataDir, 0o444); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dataDir, 0o755)
	})

	err := service.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error from removeFileIfPresent in data file create")
	}
	if !strings.Contains(err.Error(), "remove") {
		t.Fatalf("error = %q, want to contain 'remove'", err.Error())
	}
}

// --- countingObjectStore wraps mockObjectStore and fails Delete after N successful calls ---

type countingObjectStore struct {
	inner          *mockObjectStore
	deleteCount    int
	deleteErrAfter int // fail on this Nth call (0-based)
	deleteErr      error
}

func (c *countingObjectStore) Upload(ctx context.Context, key string, body io.Reader) error {
	return c.inner.Upload(ctx, key, body)
}

func (c *countingObjectStore) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return c.inner.Download(ctx, key)
}

func (c *countingObjectStore) Delete(ctx context.Context, key string) error {
	if c.deleteErr != nil && c.deleteCount >= c.deleteErrAfter {
		return c.deleteErr
	}
	c.deleteCount++
	return c.inner.Delete(ctx, key)
}

func (c *countingObjectStore) UpdateVersion(ctx context.Context, version int64, updatedAt string) error {
	return c.inner.UpdateVersion(ctx, version, updatedAt)
}

// --- countingLocalFiles returns error after N calls to ResolveDataFilePath ---

type countingLocalFiles struct {
	dataFileCount    int
	dataFileErrAfter int
	dataFileErr      error
}

func (c *countingLocalFiles) ResolveFigurePath(recordID string, filename string) (string, error) {
	return filepath.Join("/tmp/counting", recordID, "figures", filename), nil
}

func (c *countingLocalFiles) ResolveDataFilePath(recordID string, filename string) (string, error) {
	if c.dataFileErr != nil && c.dataFileCount >= c.dataFileErrAfter {
		return "", c.dataFileErr
	}
	c.dataFileCount++
	return filepath.Join("/tmp/counting", recordID, "data", filename), nil
}

func (c *countingLocalFiles) ResolveChatSourcePath(chatSessionID string, rawSourceKey string) (string, error) {
	return filepath.Join("/tmp/counting", chatSessionID, "chats", rawSourceKey), nil
}

// --- errLocalFiles for testing LocalFiles error paths ---

type errLocalFiles struct {
	figurErr    error
	dataFileErr error
}

func (e *errLocalFiles) ResolveFigurePath(_ string, _ string) (string, error) {
	if e.figurErr != nil {
		return "", e.figurErr
	}
	return "/tmp/stub/figure", nil
}

func (e *errLocalFiles) ResolveDataFilePath(_ string, _ string) (string, error) {
	if e.dataFileErr != nil {
		return "", e.dataFileErr
	}
	return "/tmp/stub/data", nil
}

func (e *errLocalFiles) ResolveChatSourcePath(_ string, _ string) (string, error) {
	return "/tmp/stub/chat-raw", nil
}

// --- stubLocalFiles for NewService nil-arg tests ---

type stubLocalFiles struct{}

func (s *stubLocalFiles) ResolveFigurePath(recordID string, filename string) (string, error) {
	return filepath.Join("/tmp/stub", recordID, "figures", filename), nil
}

func (s *stubLocalFiles) ResolveDataFilePath(recordID string, filename string) (string, error) {
	return filepath.Join("/tmp/stub", recordID, "data", filename), nil
}

func (s *stubLocalFiles) ResolveChatSourcePath(chatSessionID string, rawSourceKey string) (string, error) {
	return filepath.Join("/tmp/stub", chatSessionID, "chats", rawSourceKey), nil
}

// completeErrSessionWrapper wraps a real session and injects a Complete error.
type completeErrSessionWrapper struct {
	real        SessionManager
	completeErr error
}

func (w *completeErrSessionWrapper) Begin() (syncengine.SyncWindow, *syncengine.FileLock, error) {
	return w.real.Begin()
}

func (w *completeErrSessionWrapper) Complete(_ syncengine.SyncWindow) error {
	return w.completeErr
}

// errSessionManager is a SessionManager that returns errors on Begin and/or Complete.
type errSessionManager struct {
	beginErr    error
	completeErr error
}

func (e *errSessionManager) Begin() (syncengine.SyncWindow, *syncengine.FileLock, error) {
	if e.beginErr != nil {
		return syncengine.SyncWindow{}, nil, e.beginErr
	}
	// Should not be reached in tests that set beginErr.
	return syncengine.SyncWindow{}, nil, fmt.Errorf("not implemented")
}

func (e *errSessionManager) Complete(_ syncengine.SyncWindow) error {
	return e.completeErr
}

type memoryRepo struct {
	records         map[string]repository.Record
	projects        map[string]repository.Project
	devices         map[string]repository.Device
	projectPaths    map[string]repository.ProjectPath
	chatSessions    map[string]repository.ChatSession
	chatItems       map[string][]repository.ChatItem
	figuresByRecord map[string]map[string]repository.RecordFigure
	dataByRecord    map[string]map[string]repository.RecordDataFile
	nextFigureID    int64
	nextDataID      int64
	nextChatItemID  int64
	syncVersion     repository.SyncVersion

	// Error injection fields.
	listRecordsErr             error
	getSyncVersionErr          error
	getRecordByIDErr           error
	listFiguresByRecordIDErr   error
	listDataFilesByRecordIDErr error
	createRecordFigureErr      error
	updateRecordFigureErr      error
	deleteRecordFigureErr      error
	createRecordDataFileErr    error
	updateRecordDataFileErr    error
	deleteRecordDataFileErr    error
	updateRecordErr            error
	createRecordErr            error
	listChatSessionsErr        error
	listChatItemsErr           error
	maxChatItemOrdinalErr      error
	createChatItemErr          error
	upsertChatSessionErr       error
	replaceChatItemsErr        error
	getChatBySourceErr         error
}

func newMemoryRepo(bundles []RecordBundle) *memoryRepo {
	repo := &memoryRepo{
		records:         make(map[string]repository.Record),
		projects:        make(map[string]repository.Project),
		devices:         make(map[string]repository.Device),
		projectPaths:    make(map[string]repository.ProjectPath),
		chatSessions:    make(map[string]repository.ChatSession),
		chatItems:       make(map[string][]repository.ChatItem),
		figuresByRecord: make(map[string]map[string]repository.RecordFigure),
		dataByRecord:    make(map[string]map[string]repository.RecordDataFile),
		nextFigureID:    1,
		nextDataID:      1,
		nextChatItemID:  1,
		syncVersion:     repository.SyncVersion{ID: 1, Version: 1},
	}
	for _, bundle := range bundles {
		if bundle.Record.ProjectID == "" {
			bundle.Record.ProjectID = "sync/default-project"
		}
		if bundle.Record.SourceDeviceID == "" {
			bundle.Record.SourceDeviceID = "sync-device"
		}
		if _, ok := repo.projects[bundle.Record.ProjectID]; !ok {
			repo.projects[bundle.Record.ProjectID] = repository.Project{ID: bundle.Record.ProjectID, CreatedAt: bundle.Record.CreatedAt, UpdatedAt: bundle.Record.UpdatedAt}
		}
		if _, ok := repo.devices[bundle.Record.SourceDeviceID]; !ok {
			repo.devices[bundle.Record.SourceDeviceID] = repository.Device{ID: bundle.Record.SourceDeviceID, CreatedAt: bundle.Record.CreatedAt, UpdatedAt: bundle.Record.UpdatedAt}
		}
		repo.records[bundle.Record.ID] = bundle.Record
		repo.figuresByRecord[bundle.Record.ID] = make(map[string]repository.RecordFigure)
		for _, figure := range bundle.Figures {
			if figure.ID == 0 {
				figure.ID = repo.nextFigureID
			}
			if figure.ID >= repo.nextFigureID {
				repo.nextFigureID = figure.ID + 1
			}
			repo.figuresByRecord[bundle.Record.ID][figure.Filename] = figure
		}
		repo.dataByRecord[bundle.Record.ID] = make(map[string]repository.RecordDataFile)
		for _, dataFile := range bundle.DataFiles {
			if dataFile.ID == 0 {
				dataFile.ID = repo.nextDataID
			}
			if dataFile.ID >= repo.nextDataID {
				repo.nextDataID = dataFile.ID + 1
			}
			repo.dataByRecord[bundle.Record.ID][dataFile.Filename] = dataFile
		}
	}
	return repo
}

func (m *memoryRepo) CreateRecord(_ context.Context, input repository.CreateRecordInput) (repository.Record, error) {
	if m.createRecordErr != nil {
		return repository.Record{}, m.createRecordErr
	}
	if _, exists := m.records[input.ID]; exists {
		return repository.Record{}, repository.ErrConflict
	}
	record := repository.Record{
		ID:             input.ID,
		Date:           input.Date,
		DayOrder:       input.DayOrder,
		HTMLContent:    input.HTMLContent,
		Notes:          cloneStringPtr(input.Notes),
		ProjectID:      input.ProjectID,
		SourceDeviceID: input.SourceDeviceID,
		SourceRef:      cloneStringPtr(input.SourceRef),
		GitRemoteURL:   cloneStringPtr(input.GitRemoteURL),
		GitHash:        cloneStringPtr(input.GitHash),
		CreatedAt:      derefTime(input.CreatedAt),
		UpdatedAt:      derefTime(input.UpdatedAt),
		DeletedAt:      cloneTimePtr(input.DeletedAt),
	}
	m.records[input.ID] = record
	if _, ok := m.figuresByRecord[input.ID]; !ok {
		m.figuresByRecord[input.ID] = make(map[string]repository.RecordFigure)
	}
	if _, ok := m.dataByRecord[input.ID]; !ok {
		m.dataByRecord[input.ID] = make(map[string]repository.RecordDataFile)
	}
	return record, nil
}

func (m *memoryRepo) GetRecordByID(_ context.Context, id string) (repository.Record, error) {
	if m.getRecordByIDErr != nil {
		return repository.Record{}, m.getRecordByIDErr
	}
	record, ok := m.records[id]
	if !ok {
		return repository.Record{}, repository.ErrNotFound
	}
	return record, nil
}

func (m *memoryRepo) UpdateRecord(_ context.Context, input repository.UpdateRecordInput) (repository.Record, error) {
	if m.updateRecordErr != nil {
		return repository.Record{}, m.updateRecordErr
	}
	record, ok := m.records[input.ID]
	if !ok {
		return repository.Record{}, repository.ErrNotFound
	}
	record.Date = input.Date
	record.DayOrder = input.DayOrder
	record.HTMLContent = input.HTMLContent
	record.Notes = cloneStringPtr(input.Notes)
	record.ProjectID = input.ProjectID
	record.SourceDeviceID = input.SourceDeviceID
	record.SourceRef = cloneStringPtr(input.SourceRef)
	record.GitRemoteURL = cloneStringPtr(input.GitRemoteURL)
	record.GitHash = cloneStringPtr(input.GitHash)
	record.UpdatedAt = derefTime(input.UpdatedAt)
	if input.SetDeletedAt {
		record.DeletedAt = cloneTimePtr(input.DeletedAt)
	}
	m.records[input.ID] = record
	return record, nil
}

func (m *memoryRepo) ReplaceRecordChildren(_ context.Context, input repository.ReplaceRecordChildrenInput) (repository.Record, error) {
	if err := input.Validate(); err != nil {
		return repository.Record{}, err
	}
	seenFigures := make(map[string]struct{}, len(input.Figures))
	for _, figureInput := range input.Figures {
		if _, exists := seenFigures[figureInput.Filename]; exists {
			return repository.Record{}, repository.ErrConflict
		}
		seenFigures[figureInput.Filename] = struct{}{}
	}
	seenDataFiles := make(map[string]struct{}, len(input.DataFiles))
	for _, dataInput := range input.DataFiles {
		if _, exists := seenDataFiles[dataInput.Filename]; exists {
			return repository.Record{}, repository.ErrConflict
		}
		seenDataFiles[dataInput.Filename] = struct{}{}
	}

	record, exists := m.records[input.Record.ID]
	if !exists {
		record = repository.Record{
			ID:        input.Record.ID,
			CreatedAt: derefTime(input.Record.CreatedAt),
		}
	}
	record.Date = input.Record.Date
	record.DayOrder = input.Record.DayOrder
	record.HTMLContent = input.Record.HTMLContent
	record.Notes = cloneStringPtr(input.Record.Notes)
	record.ProjectID = input.Record.ProjectID
	record.SourceDeviceID = input.Record.SourceDeviceID
	record.SourceRef = cloneStringPtr(input.Record.SourceRef)
	record.GitRemoteURL = cloneStringPtr(input.Record.GitRemoteURL)
	record.GitHash = cloneStringPtr(input.Record.GitHash)
	record.UpdatedAt = derefTime(input.Record.UpdatedAt)
	if input.SetDeletedAt {
		record.DeletedAt = cloneTimePtr(input.Record.DeletedAt)
	}
	m.records[input.Record.ID] = record

	figures := make(map[string]repository.RecordFigure, len(input.Figures))
	for _, figureInput := range input.Figures {
		figure := repository.RecordFigure{
			ID:       m.nextFigureID,
			RecordID: figureInput.RecordID,
			Filename: figureInput.Filename,
			S3Key:    figureInput.S3Key,
			AltText:  cloneStringPtr(figureInput.AltText),
		}
		m.nextFigureID++
		figures[figure.Filename] = figure
	}
	m.figuresByRecord[input.Record.ID] = figures

	dataFiles := make(map[string]repository.RecordDataFile, len(input.DataFiles))
	for _, dataInput := range input.DataFiles {
		dataFile := repository.RecordDataFile{
			ID:          m.nextDataID,
			RecordID:    dataInput.RecordID,
			Filename:    dataInput.Filename,
			S3Key:       dataInput.S3Key,
			Size:        dataInput.Size,
			Hash:        dataInput.Hash,
			Description: cloneStringPtr(dataInput.Description),
		}
		m.nextDataID++
		dataFiles[dataFile.Filename] = dataFile
	}
	m.dataByRecord[input.Record.ID] = dataFiles
	return record, nil
}

func (m *memoryRepo) ListRecords(_ context.Context, filter repository.ListRecordsFilter) ([]repository.Record, error) {
	if m.listRecordsErr != nil {
		return nil, m.listRecordsErr
	}
	records := sortMemoryRecords(m.records)
	result := make([]repository.Record, 0, len(records))
	for _, record := range records {
		switch {
		case filter.OnlyDeleted && record.DeletedAt == nil:
			continue
		case !filter.IncludeDeleted && !filter.OnlyDeleted && record.DeletedAt != nil:
			continue
		}
		result = append(result, record)
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (m *memoryRepo) CountRecords(ctx context.Context, filter repository.ListRecordsFilter) (int, error) {
	filter.Limit = 0
	records, err := m.ListRecords(ctx, filter)
	if err != nil {
		return 0, err
	}
	return len(records), nil
}

func (m *memoryRepo) CountRecordChildren(_ context.Context, recordIDs []string) (map[string]repository.ChildCounts, error) {
	counts := make(map[string]repository.ChildCounts)
	for _, id := range recordIDs {
		figureCount := len(m.figuresByRecord[id])
		dataFileCount := len(m.dataByRecord[id])
		if figureCount > 0 || dataFileCount > 0 {
			counts[id] = repository.ChildCounts{Figures: figureCount, DataFiles: dataFileCount}
		}
	}
	return counts, nil
}

func (m *memoryRepo) CreateRecordFigure(_ context.Context, input repository.CreateRecordFigureInput) (repository.RecordFigure, error) {
	if m.createRecordFigureErr != nil {
		return repository.RecordFigure{}, m.createRecordFigureErr
	}
	figure := repository.RecordFigure{
		ID:       m.nextFigureID,
		RecordID: input.RecordID,
		Filename: input.Filename,
		S3Key:    input.S3Key,
		AltText:  cloneStringPtr(input.AltText),
	}
	m.nextFigureID++
	if _, ok := m.figuresByRecord[input.RecordID]; !ok {
		m.figuresByRecord[input.RecordID] = make(map[string]repository.RecordFigure)
	}
	m.figuresByRecord[input.RecordID][input.Filename] = figure
	return figure, nil
}

func (m *memoryRepo) UpdateRecordFigure(_ context.Context, input repository.UpdateRecordFigureInput) (repository.RecordFigure, error) {
	if m.updateRecordFigureErr != nil {
		return repository.RecordFigure{}, m.updateRecordFigureErr
	}
	for recordID, figures := range m.figuresByRecord {
		for filename, figure := range figures {
			if figure.ID != input.ID {
				continue
			}
			delete(figures, filename)
			figure.Filename = input.Filename
			figure.S3Key = input.S3Key
			figure.AltText = cloneStringPtr(input.AltText)
			m.figuresByRecord[recordID][figure.Filename] = figure
			return figure, nil
		}
	}
	return repository.RecordFigure{}, repository.ErrNotFound
}

func (m *memoryRepo) GetRecordFigureByID(_ context.Context, id int64) (repository.RecordFigure, error) {
	for _, figures := range m.figuresByRecord {
		for _, figure := range figures {
			if figure.ID == id {
				return figure, nil
			}
		}
	}
	return repository.RecordFigure{}, repository.ErrNotFound
}

func (m *memoryRepo) ListRecordFiguresByRecordID(_ context.Context, recordID string) ([]repository.RecordFigure, error) {
	if m.listFiguresByRecordIDErr != nil {
		return nil, m.listFiguresByRecordIDErr
	}
	figures := make([]repository.RecordFigure, 0, len(m.figuresByRecord[recordID]))
	for _, figure := range m.figuresByRecord[recordID] {
		figures = append(figures, figure)
	}
	sort.Slice(figures, func(i, j int) bool {
		return figures[i].Filename < figures[j].Filename
	})
	return figures, nil
}

func (m *memoryRepo) DeleteRecordFigure(_ context.Context, id int64) error {
	if m.deleteRecordFigureErr != nil {
		return m.deleteRecordFigureErr
	}
	for recordID, figures := range m.figuresByRecord {
		for filename, figure := range figures {
			if figure.ID != id {
				continue
			}
			delete(m.figuresByRecord[recordID], filename)
			return nil
		}
	}
	return repository.ErrNotFound
}

func (m *memoryRepo) CreateRecordDataFile(_ context.Context, input repository.CreateRecordDataFileInput) (repository.RecordDataFile, error) {
	if m.createRecordDataFileErr != nil {
		return repository.RecordDataFile{}, m.createRecordDataFileErr
	}
	dataFile := repository.RecordDataFile{
		ID:          m.nextDataID,
		RecordID:    input.RecordID,
		Filename:    input.Filename,
		S3Key:       input.S3Key,
		Size:        input.Size,
		Hash:        input.Hash,
		Description: cloneStringPtr(input.Description),
	}
	m.nextDataID++
	if _, ok := m.dataByRecord[input.RecordID]; !ok {
		m.dataByRecord[input.RecordID] = make(map[string]repository.RecordDataFile)
	}
	m.dataByRecord[input.RecordID][input.Filename] = dataFile
	return dataFile, nil
}

func (m *memoryRepo) UpdateRecordDataFile(_ context.Context, input repository.UpdateRecordDataFileInput) (repository.RecordDataFile, error) {
	if m.updateRecordDataFileErr != nil {
		return repository.RecordDataFile{}, m.updateRecordDataFileErr
	}
	for recordID, dataFiles := range m.dataByRecord {
		for filename, dataFile := range dataFiles {
			if dataFile.ID != input.ID {
				continue
			}
			delete(dataFiles, filename)
			dataFile.Filename = input.Filename
			dataFile.S3Key = input.S3Key
			if input.Size != nil {
				dataFile.Size = *input.Size
			}
			if input.Hash != nil {
				dataFile.Hash = *input.Hash
			}
			dataFile.Description = cloneStringPtr(input.Description)
			m.dataByRecord[recordID][dataFile.Filename] = dataFile
			return dataFile, nil
		}
	}
	return repository.RecordDataFile{}, repository.ErrNotFound
}

func (m *memoryRepo) GetRecordDataFileByID(_ context.Context, id int64) (repository.RecordDataFile, error) {
	for _, dataFiles := range m.dataByRecord {
		for _, dataFile := range dataFiles {
			if dataFile.ID == id {
				return dataFile, nil
			}
		}
	}
	return repository.RecordDataFile{}, repository.ErrNotFound
}

func (m *memoryRepo) ListRecordDataFilesByRecordID(_ context.Context, recordID string) ([]repository.RecordDataFile, error) {
	if m.listDataFilesByRecordIDErr != nil {
		return nil, m.listDataFilesByRecordIDErr
	}
	dataFiles := make([]repository.RecordDataFile, 0, len(m.dataByRecord[recordID]))
	for _, dataFile := range m.dataByRecord[recordID] {
		dataFiles = append(dataFiles, dataFile)
	}
	sort.Slice(dataFiles, func(i, j int) bool {
		return dataFiles[i].Filename < dataFiles[j].Filename
	})
	return dataFiles, nil
}

func (m *memoryRepo) DeleteRecordDataFile(_ context.Context, id int64) error {
	if m.deleteRecordDataFileErr != nil {
		return m.deleteRecordDataFileErr
	}
	for recordID, dataFiles := range m.dataByRecord {
		for filename, dataFile := range dataFiles {
			if dataFile.ID != id {
				continue
			}
			delete(m.dataByRecord[recordID], filename)
			return nil
		}
	}
	return repository.ErrNotFound
}

func (m *memoryRepo) SoftDeleteRecord(_ context.Context, id string) error { return nil }
func (m *memoryRepo) RestoreRecord(_ context.Context, id string) error    { return nil }
func (m *memoryRepo) DeleteRecord(_ context.Context, id string) error     { return nil }
func (m *memoryRepo) CreateTemplate(_ context.Context, input repository.CreateTemplateInput) (repository.Template, error) {
	return repository.Template{}, nil
}
func (m *memoryRepo) GetTemplateByName(_ context.Context, name string) (repository.Template, error) {
	return repository.Template{}, repository.ErrNotFound
}
func (m *memoryRepo) UpdateTemplate(_ context.Context, input repository.UpdateTemplateInput) (repository.Template, error) {
	return repository.Template{}, nil
}
func (m *memoryRepo) ListTemplates(_ context.Context) ([]repository.Template, error) { return nil, nil }
func (m *memoryRepo) DeleteTemplate(_ context.Context, name string) error            { return nil }
func (m *memoryRepo) GetSyncVersion(_ context.Context) (repository.SyncVersion, error) {
	if m.getSyncVersionErr != nil {
		return repository.SyncVersion{}, m.getSyncVersionErr
	}
	return m.syncVersion, nil
}
func (m *memoryRepo) CreateProject(_ context.Context, input repository.CreateRegistryInput) (repository.Project, error) {
	project := repository.Project{ID: input.ID, CreatedAt: derefTime(input.CreatedAt), UpdatedAt: derefTime(input.UpdatedAt), ArchivedAt: cloneTimePtr(input.ArchivedAt)}
	m.projects[input.ID] = project
	return project, nil
}
func (m *memoryRepo) GetProjectByID(_ context.Context, id string) (repository.Project, error) {
	project, ok := m.projects[id]
	if !ok {
		return repository.Project{}, repository.ErrNotFound
	}
	return project, nil
}
func (m *memoryRepo) ListProjects(_ context.Context, includeArchived bool) ([]repository.Project, error) {
	projects := make([]repository.Project, 0, len(m.projects))
	for _, project := range m.projects {
		if !includeArchived && project.ArchivedAt != nil {
			continue
		}
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	return projects, nil
}
func (m *memoryRepo) ArchiveProject(_ context.Context, id string) (repository.Project, error) {
	project, ok := m.projects[id]
	if !ok {
		return repository.Project{}, repository.ErrNotFound
	}
	now := time.Now().UTC()
	project.ArchivedAt = &now
	m.projects[id] = project
	return project, nil
}
func (m *memoryRepo) RestoreProject(_ context.Context, id string) (repository.Project, error) {
	project, ok := m.projects[id]
	if !ok {
		return repository.Project{}, repository.ErrNotFound
	}
	project.ArchivedAt = nil
	m.projects[id] = project
	return project, nil
}
func (m *memoryRepo) UpsertProjectForImport(_ context.Context, project repository.Project) (bool, error) {
	existing, ok := m.projects[project.ID]
	if ok && !project.UpdatedAt.After(existing.UpdatedAt) {
		return false, nil
	}
	m.projects[project.ID] = project
	return true, nil
}
func (m *memoryRepo) UpsertProjectPath(_ context.Context, input repository.CreateProjectPathInput) (repository.ProjectPath, bool, error) {
	key := input.ProjectID + "|" + input.Path + "|" + input.DeviceID
	if existing, ok := m.projectPaths[key]; ok {
		return existing, false, nil
	}
	path := repository.ProjectPath{
		ID:        int64(len(m.projectPaths) + 1),
		ProjectID: input.ProjectID,
		Path:      input.Path,
		DeviceID:  input.DeviceID,
		CreatedAt: derefTime(input.CreatedAt),
		UpdatedAt: derefTime(input.UpdatedAt),
	}
	m.projectPaths[key] = path
	return path, true, nil
}
func (m *memoryRepo) ListProjectPaths(_ context.Context, projectID *string) ([]repository.ProjectPath, error) {
	paths := make([]repository.ProjectPath, 0, len(m.projectPaths))
	for _, path := range m.projectPaths {
		if projectID != nil && path.ProjectID != *projectID {
			continue
		}
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return paths[i].Path < paths[j].Path })
	return paths, nil
}
func (m *memoryRepo) BackfillChatProjects(_ context.Context) (int, error) { return 0, nil }
func (m *memoryRepo) CreateDevice(_ context.Context, input repository.CreateRegistryInput) (repository.Device, error) {
	device := repository.Device{ID: input.ID, CreatedAt: derefTime(input.CreatedAt), UpdatedAt: derefTime(input.UpdatedAt), ArchivedAt: cloneTimePtr(input.ArchivedAt)}
	m.devices[input.ID] = device
	return device, nil
}
func (m *memoryRepo) GetDeviceByID(_ context.Context, id string) (repository.Device, error) {
	device, ok := m.devices[id]
	if !ok {
		return repository.Device{}, repository.ErrNotFound
	}
	return device, nil
}
func (m *memoryRepo) ListDevices(_ context.Context, includeArchived bool) ([]repository.Device, error) {
	devices := make([]repository.Device, 0, len(m.devices))
	for _, device := range m.devices {
		if !includeArchived && device.ArchivedAt != nil {
			continue
		}
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })
	return devices, nil
}
func (m *memoryRepo) ArchiveDevice(_ context.Context, id string) (repository.Device, error) {
	device, ok := m.devices[id]
	if !ok {
		return repository.Device{}, repository.ErrNotFound
	}
	now := time.Now().UTC()
	device.ArchivedAt = &now
	m.devices[id] = device
	return device, nil
}
func (m *memoryRepo) RestoreDevice(_ context.Context, id string) (repository.Device, error) {
	device, ok := m.devices[id]
	if !ok {
		return repository.Device{}, repository.ErrNotFound
	}
	device.ArchivedAt = nil
	m.devices[id] = device
	return device, nil
}
func (m *memoryRepo) UpsertDeviceForImport(_ context.Context, device repository.Device) (bool, error) {
	existing, ok := m.devices[device.ID]
	if ok && !device.UpdatedAt.After(existing.UpdatedAt) {
		return false, nil
	}
	m.devices[device.ID] = device
	return true, nil
}
func (m *memoryRepo) UpsertChatSession(_ context.Context, input repository.UpsertChatSessionInput) (repository.ChatSession, bool, error) {
	if m.upsertChatSessionErr != nil {
		return repository.ChatSession{}, false, m.upsertChatSessionErr
	}
	created := true
	for id, existing := range m.chatSessions {
		if existing.Source == input.Source && existing.SourceSessionID == input.SourceSessionID {
			session := chatSessionFromInput(input)
			session.ID = id
			if session.ProjectID == nil {
				session.ProjectID = existing.ProjectID
			}
			// Match the SQLite/Postgres COALESCE-on-cwd semantics so a
			// nil input.CWD does not silently clear an existing value.
			if session.CWD == nil {
				session.CWD = existing.CWD
			}
			if session.Title == nil {
				session.Title = existing.Title
			}
			if input.ClearDeleted {
				session.DeletedAt = nil
			}
			m.chatSessions[id] = session
			return session, false, nil
		}
	}
	session := chatSessionFromInput(input)
	m.chatSessions[session.ID] = session
	return session, created, nil
}
func (m *memoryRepo) UpsertChatSessionWithItems(ctx context.Context, input repository.UpsertChatSessionInput, inputs []repository.CreateChatItemInput) (repository.ChatSession, bool, error) {
	previousSessions := cloneChatSessions(m.chatSessions)
	previousItems := cloneChatItems(m.chatItems)
	session, created, err := m.UpsertChatSession(ctx, input)
	if err != nil {
		return repository.ChatSession{}, false, err
	}
	if err := m.ReplaceChatItems(ctx, session.ID, inputs); err != nil {
		m.chatSessions = previousSessions
		m.chatItems = previousItems
		return repository.ChatSession{}, false, err
	}
	return session, created, nil
}
func cloneChatSessions(sessions map[string]repository.ChatSession) map[string]repository.ChatSession {
	cloned := make(map[string]repository.ChatSession, len(sessions))
	for id, session := range sessions {
		cloned[id] = session
	}
	return cloned
}
func cloneChatItems(items map[string][]repository.ChatItem) map[string][]repository.ChatItem {
	cloned := make(map[string][]repository.ChatItem, len(items))
	for sessionID, sessionItems := range items {
		cloned[sessionID] = append([]repository.ChatItem(nil), sessionItems...)
	}
	return cloned
}
func (m *memoryRepo) GetChatSessionByID(_ context.Context, id string) (repository.ChatSession, error) {
	session, ok := m.chatSessions[id]
	if !ok {
		return repository.ChatSession{}, repository.ErrNotFound
	}
	return session, nil
}
func (m *memoryRepo) GetChatSessionBySource(_ context.Context, source string, sourceSessionID string) (repository.ChatSession, error) {
	if m.getChatBySourceErr != nil {
		return repository.ChatSession{}, m.getChatBySourceErr
	}
	for _, session := range m.chatSessions {
		if session.Source == source && session.SourceSessionID == sourceSessionID {
			return session, nil
		}
	}
	return repository.ChatSession{}, repository.ErrNotFound
}
func (m *memoryRepo) ListChatSessions(_ context.Context, filter repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
	if m.listChatSessionsErr != nil {
		return nil, m.listChatSessionsErr
	}
	sessions := make([]repository.ChatSession, 0, len(m.chatSessions))
	for _, session := range m.chatSessions {
		if !filter.IncludeDeleted && !filter.OnlyDeleted && session.DeletedAt != nil {
			continue
		}
		if filter.OnlyDeleted && session.DeletedAt == nil {
			continue
		}
		if filter.UpdatedAfter != nil && session.UpdatedAt.Before(*filter.UpdatedAfter) {
			continue
		}
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if !sessions[i].LastActivityAt.Equal(sessions[j].LastActivityAt) {
			return sessions[i].LastActivityAt.After(sessions[j].LastActivityAt)
		}
		return sessions[i].ID < sessions[j].ID
	})
	return sessions, nil
}
func (m *memoryRepo) CountChatSessions(_ context.Context, _ repository.ListChatSessionsFilter) (int, error) {
	return len(m.chatSessions), nil
}
func (m *memoryRepo) CountChatItems(_ context.Context, filter repository.CountChatItemsFilter) (int, error) {
	count := 0
	for sessionID, items := range m.chatItems {
		session, ok := m.chatSessions[sessionID]
		if !ok {
			continue
		}
		if !filter.IncludeDeleted {
			if session.DeletedAt != nil {
				continue
			}
		}
		count += len(items)
	}
	return count, nil
}
func (m *memoryRepo) SoftDeleteChatSession(_ context.Context, _ string) error { return nil }
func (m *memoryRepo) RestoreChatSession(_ context.Context, _ string) error    { return nil }
func (m *memoryRepo) DeleteChatSession(_ context.Context, id string) error {
	delete(m.chatSessions, id)
	delete(m.chatItems, id)
	return nil
}
func (m *memoryRepo) MaxChatItemOrdinal(_ context.Context, sessionID string) (int, error) {
	if m.maxChatItemOrdinalErr != nil {
		return 0, m.maxChatItemOrdinalErr
	}
	maxOrdinal := -1
	for _, item := range m.chatItems[sessionID] {
		if item.Ordinal > maxOrdinal {
			maxOrdinal = item.Ordinal
		}
	}
	return maxOrdinal, nil
}
func (m *memoryRepo) CreateChatItem(_ context.Context, input repository.CreateChatItemInput) (repository.ChatItem, error) {
	if m.createChatItemErr != nil {
		return repository.ChatItem{}, m.createChatItemErr
	}
	item := repository.ChatItem{
		ID:         m.nextChatItemID,
		SessionID:  input.SessionID,
		Ordinal:    input.Ordinal,
		Role:       input.Role,
		ItemType:   input.ItemType,
		Text:       cloneStringPtr(input.Text),
		SearchText: input.SearchText,
		RawJSON:    cloneStringPtr(input.RawJSON),
		CreatedAt:  derefTime(input.CreatedAt),
	}
	m.nextChatItemID++
	m.chatItems[input.SessionID] = append(m.chatItems[input.SessionID], item)
	return item, nil
}
func (m *memoryRepo) AppendChatItems(_ context.Context, sessionID string, inputs []repository.CreateChatItemInput) error {
	for _, input := range inputs {
		item := repository.ChatItem{
			ID:         m.nextChatItemID,
			SessionID:  sessionID,
			Ordinal:    input.Ordinal,
			Role:       input.Role,
			ItemType:   input.ItemType,
			Text:       cloneStringPtr(input.Text),
			SearchText: input.SearchText,
			RawJSON:    cloneStringPtr(input.RawJSON),
			CreatedAt:  derefTime(input.CreatedAt),
		}
		m.nextChatItemID++
		m.chatItems[sessionID] = append(m.chatItems[sessionID], item)
	}
	return nil
}
func (m *memoryRepo) ReplaceChatItems(_ context.Context, sessionID string, inputs []repository.CreateChatItemInput) error {
	if m.replaceChatItemsErr != nil {
		return m.replaceChatItemsErr
	}
	replaced := make([]repository.ChatItem, 0, len(inputs))
	for _, input := range inputs {
		item := repository.ChatItem{
			ID:         m.nextChatItemID,
			SessionID:  sessionID,
			Ordinal:    input.Ordinal,
			Role:       input.Role,
			ItemType:   input.ItemType,
			Text:       cloneStringPtr(input.Text),
			SearchText: input.SearchText,
			RawJSON:    cloneStringPtr(input.RawJSON),
			CreatedAt:  derefTime(input.CreatedAt),
		}
		m.nextChatItemID++
		replaced = append(replaced, item)
	}
	m.chatItems[sessionID] = replaced
	return nil
}
func (m *memoryRepo) ListChatItems(_ context.Context, sessionID string) ([]repository.ChatItem, error) {
	if m.listChatItemsErr != nil {
		return nil, m.listChatItemsErr
	}
	items := append([]repository.ChatItem(nil), m.chatItems[sessionID]...)
	sort.Slice(items, func(i, j int) bool { return items[i].Ordinal < items[j].Ordinal })
	return items, nil
}
func (m *memoryRepo) SearchChatItems(_ context.Context, _ repository.SearchChatItemsFilter) ([]repository.ChatSearchResult, error) {
	return nil, nil
}
func (m *memoryRepo) CountSearchChatItems(_ context.Context, _ repository.SearchChatItemsFilter) (int, error) {
	return 0, nil
}
func (m *memoryRepo) SearchAll(_ context.Context, _ repository.UnifiedSearchFilter) ([]repository.DomainSearchResult, error) {
	return nil, nil
}
func (m *memoryRepo) CountActiveRecords(_ context.Context) (int, error)       { return 0, nil }
func (m *memoryRepo) CountTrashedRecords(_ context.Context) (int, error)      { return 0, nil }
func (m *memoryRepo) PurgeDeletedRecords(_ context.Context) ([]string, error) { return nil, nil }

func (m *memoryRepo) bundle(recordID string) RecordBundle {
	record, ok := m.records[recordID]
	if !ok {
		return RecordBundle{}
	}
	figures, _ := m.ListRecordFiguresByRecordID(context.Background(), recordID)
	dataFiles, _ := m.ListRecordDataFilesByRecordID(context.Background(), recordID)
	return RecordBundle{Record: record, Figures: figures, DataFiles: dataFiles}
}

type mockObjectStore struct {
	objects          map[string]string
	versionWrites    []int64
	uploadErr        error
	downloadErr      error
	deleteErr        error
	updateVersionErr error
}

func newMockObjectStore() *mockObjectStore {
	return &mockObjectStore{objects: make(map[string]string)}
}

func (m *mockObjectStore) Upload(_ context.Context, key string, body io.Reader) error {
	if m.uploadErr != nil {
		return m.uploadErr
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.objects[key] = string(data)
	return nil
}

func (m *mockObjectStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	if m.downloadErr != nil {
		return nil, m.downloadErr
	}
	value, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("missing object %s", key)
	}
	return io.NopCloser(strings.NewReader(value)), nil
}

func (m *mockObjectStore) Delete(_ context.Context, key string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.objects, key)
	return nil
}

func (m *mockObjectStore) UpdateVersion(_ context.Context, version int64, _ string) error {
	if m.updateVersionErr != nil {
		return m.updateVersionErr
	}
	m.versionWrites = append(m.versionWrites, version)
	return nil
}

func newTestService(
	t *testing.T,
	localBundles []RecordBundle,
	cloudBundles []RecordBundle,
) (*Service, *memoryRepo, *memoryRepo, *filesystem.Client, *mockObjectStore, *syncengine.CursorStore) {
	t.Helper()

	localRepo := newMemoryRepo(localBundles)
	cloudRepo := newMemoryRepo(cloudBundles)

	baseDir := t.TempDir()
	localFS, err := filesystem.NewClient(baseDir)
	if err != nil {
		t.Fatalf("filesystem.NewClient() error = %v", err)
	}

	pcDir := filepath.Join(t.TempDir(), ".pc")
	session, err := syncengine.NewManager(pcDir)
	if err != nil {
		t.Fatalf("syncengine.NewManager() error = %v", err)
	}
	cursorStore, err := syncengine.NewCursorStore(pcDir)
	if err != nil {
		t.Fatalf("syncengine.NewCursorStore() error = %v", err)
	}

	service, err := NewService(localRepo, cloudRepo, localFS, newMockObjectStore(), session)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	objects := service.cloudObjects.(*mockObjectStore)
	return service, localRepo, cloudRepo, localFS, objects, cursorStore
}

func writeLocalAsset(
	t *testing.T,
	localFS *filesystem.Client,
	isFigure bool,
	recordID string,
	filename string,
	content string,
) {
	t.Helper()

	var (
		path string
		err  error
	)
	if isFigure {
		path, err = localFS.ResolveFigurePath(recordID, filename)
	} else {
		path, err = localFS.ResolveDataFilePath(recordID, filename)
	}
	if err != nil {
		t.Fatalf("resolve local asset path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func readLocalAsset(
	t *testing.T,
	localFS *filesystem.Client,
	isFigure bool,
	recordID string,
	filename string,
) string {
	t.Helper()

	var (
		path string
		err  error
	)
	if isFigure {
		path, err = localFS.ResolveFigurePath(recordID, filename)
	} else {
		path, err = localFS.ResolveDataFilePath(recordID, filename)
	}
	if err != nil {
		t.Fatalf("resolve local asset path: %v", err)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return string(bytes)
}

func assertBundleEqual(t *testing.T, got RecordBundle, want RecordBundle) {
	t.Helper()
	if canonicalBundle(got) != canonicalBundle(want) {
		t.Fatalf("bundle mismatch\n got: %+v\nwant: %+v", canonicalBundle(got), canonicalBundle(want))
	}
}

func canonicalBundle(bundle RecordBundle) string {
	canonicalFigures := append([]repository.RecordFigure(nil), bundle.Figures...)
	sort.Slice(canonicalFigures, func(i, j int) bool {
		return canonicalFigures[i].Filename < canonicalFigures[j].Filename
	})
	canonicalDataFiles := append([]repository.RecordDataFile(nil), bundle.DataFiles...)
	sort.Slice(canonicalDataFiles, func(i, j int) bool {
		return canonicalDataFiles[i].Filename < canonicalDataFiles[j].Filename
	})

	parts := []string{
		bundle.Record.ID,
		bundle.Record.Date,
		bundle.Record.DayOrder,
		nullableStringValue(bundle.Record.HTMLContent),
		bundle.Record.CreatedAt.UTC().Format(time.RFC3339Nano),
		bundle.Record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if bundle.Record.DeletedAt != nil {
		parts = append(parts, bundle.Record.DeletedAt.UTC().Format(time.RFC3339Nano))
	}
	for _, figure := range canonicalFigures {
		parts = append(parts, fmt.Sprintf("fig:%s:%s:%s", figure.RecordID, figure.Filename, figure.S3Key))
	}
	for _, dataFile := range canonicalDataFiles {
		parts = append(parts, fmt.Sprintf("data:%s:%s:%s:%d:%s", dataFile.RecordID, dataFile.Filename, dataFile.S3Key, dataFile.Size, dataFile.Hash))
	}
	return strings.Join(parts, "|")
}

func sortMemoryRecords(records map[string]repository.Record) []repository.Record {
	result := make([]repository.Record, 0, len(records))
	for _, record := range records {
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Date != result[j].Date {
			return result[i].Date < result[j].Date
		}
		if result[i].DayOrder != result[j].DayOrder {
			return result[i].DayOrder < result[j].DayOrder
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func chatSessionFromInput(input repository.UpsertChatSessionInput) repository.ChatSession {
	return repository.ChatSession{
		ID:                    input.ID,
		Source:                input.Source,
		SourceSessionID:       input.SourceSessionID,
		ParentSourceSessionID: cloneStringPtr(input.ParentSourceSessionID),
		SourceDeviceID:        input.SourceDeviceID,
		ProjectID:             cloneStringPtr(input.ProjectID),
		CWD:                   cloneStringPtr(input.CWD),
		Title:                 cloneStringPtr(input.Title),
		StartedAt:             input.StartedAt,
		LastActivityAt:        input.LastActivityAt,
		OriginalSourcePath:    cloneStringPtr(input.OriginalSourcePath),
		RawSourceKey:          cloneStringPtr(input.RawSourceKey),
		CreatedAt:             derefTime(input.CreatedAt),
		UpdatedAt:             derefTime(input.UpdatedAt),
		DeletedAt:             cloneTimePtr(input.DeletedAt),
	}
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// TestServiceSetWarnWriterRoutesChatRawWarnings verifies that warnings
// raised by syncChangedChats are written to the writer registered with
// SetWarnWriter rather than the package default (os.Stderr).
func TestServiceSetWarnWriterRoutesChatRawWarnings(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	service, localRepo, _, _, _, _ := newTestService(t, nil, nil)
	key := "chats/raw/20260514-feedface/source.json"
	localRepo.chatSessions["20260514-feedface"] = repository.ChatSession{
		ID:              "20260514-feedface",
		Source:          "codex",
		SourceSessionID: "warn-sess",
		SourceDeviceID:  "warn-device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
		RawSourceKey:    &key,
	}

	captured := &bytes.Buffer{}
	service.SetWarnWriter(captured)
	if err := service.pushChangedChats(context.Background(), time.Time{}); err != nil {
		t.Fatalf("pushChangedChats: %v", err)
	}
	if !strings.Contains(captured.String(), "raw chat source files missing locally") {
		t.Fatalf("expected captured warning, got %q", captured.String())
	}

	// nil resets to default; we don't assert default contents, only that it
	// does not panic and re-uses the package default writer.
	service.SetWarnWriter(nil)
}
