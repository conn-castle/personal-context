package sync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

// TestSyncChatStrictKeyValidationRejectsInvalidRawSourceKey verifies that the
// sync raw-transfer path refuses to operate on a key that does not match the
// owning chat session.
func TestSyncChatStrictKeyValidationRejectsInvalidRawSourceKey(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	badKey := "chats/raw/other-id/source.json"
	source.chatSessions["20260514-aabbccdd"] = repository.ChatSession{
		ID:              "20260514-aabbccdd",
		Source:          "codex",
		SourceSessionID: "rejected",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
		RawSourceKey:    &badKey,
	}

	transferCalled := false
	rejectingTransfer := func(_ context.Context, _ string, session repository.ChatSession, _ *chatRawSyncReport) error {
		transferCalled = true
		if session.RawSourceKey == nil {
			return nil
		}
		// Inline validation matching what the real Service.transferChatRawSource
		// asks of fs.ResolveChatSourcePath: the key must match the owning
		// chat session.
		key := *session.RawSourceKey
		if !strings.Contains(key, session.ID) {
			return errors.New("validate chat source key: mismatched chat id")
		}
		return nil
	}

	err := syncChangedChats(context.Background(), time.Time{}, "push", source, target, rejectingTransfer, nil)
	if err == nil {
		t.Fatal("expected sync to fail on strict key validation")
	}
	if !transferCalled {
		t.Fatal("expected raw transfer hook to be invoked")
	}
	if !strings.Contains(err.Error(), "mismatched chat id") {
		t.Fatalf("expected mismatched chat id error, got %v", err)
	}
}

// TestSyncChatDuplicateSourceIdentityFailsBeforeRawTransfer verifies that a
// source transcript already present under a different Personal Context chat ID
// fails before raw bytes are uploaded or downloaded.
func TestSyncChatDuplicateSourceIdentityFailsBeforeRawTransfer(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	sourceKey := "chats/raw/20260514-aaaabbbb/source.json"
	targetKey := "chats/raw/20260514-ccccdddd/source.json"
	source.chatSessions["20260514-aaaabbbb"] = repository.ChatSession{
		ID:              "20260514-aaaabbbb",
		Source:          "codex",
		SourceSessionID: "same-source",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
		RawSourceKey:    &sourceKey,
	}
	target.chatSessions["20260514-ccccdddd"] = repository.ChatSession{
		ID:              "20260514-ccccdddd",
		Source:          "codex",
		SourceSessionID: "same-source",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
		RawSourceKey:    &targetKey,
	}

	transferCalled := false
	transfer := func(context.Context, string, repository.ChatSession, *chatRawSyncReport) error {
		transferCalled = true
		return nil
	}
	err := syncChangedChats(context.Background(), time.Time{}, "push", source, target, transfer, nil)
	if err == nil {
		t.Fatal("expected duplicate source identity to fail")
	}
	if transferCalled {
		t.Fatal("raw transfer ran before duplicate source identity was rejected")
	}
	if !strings.Contains(err.Error(), "already exists with id 20260514-ccccdddd") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncChatTargetSourceLookupError(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	rawKey := "chats/raw/20260514-aabbccdd/source.json"
	source.chatSessions["20260514-aabbccdd"] = repository.ChatSession{
		ID:              "20260514-aabbccdd",
		Source:          "codex",
		SourceSessionID: "lookup-error",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
		RawSourceKey:    &rawKey,
	}
	target.getChatBySourceErr = errors.New("source lookup failed")

	err := syncChangedChats(context.Background(), time.Time{}, "push", source, target, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "load target chat source codex/lookup-error") {
		t.Fatalf("expected target source lookup error, got %v", err)
	}
}

func TestSyncChatTargetWinnerSkipsRawTransfer(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	rawKey := "chats/raw/20260514-aabbccdd/source.json"
	source.chatSessions["20260514-aabbccdd"] = repository.ChatSession{
		ID:              "20260514-aabbccdd",
		Source:          "codex",
		SourceSessionID: "same-chat",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
		RawSourceKey:    &rawKey,
	}
	target.chatSessions["20260514-aabbccdd"] = repository.ChatSession{
		ID:              "20260514-aabbccdd",
		Source:          "codex",
		SourceSessionID: "same-chat",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now.Add(time.Minute),
	}

	transferCalled := false
	transfer := func(context.Context, string, repository.ChatSession, *chatRawSyncReport) error {
		transferCalled = true
		return nil
	}
	if err := syncChangedChatsDirected(context.Background(), time.Time{}, "push", source, target, WinnerLocal, transfer, nil); err != nil {
		t.Fatalf("syncChangedChatsDirected() error = %v", err)
	}
	if transferCalled {
		t.Fatal("raw transfer should be skipped when the target side wins")
	}
}

type failingWarnWriter struct{}

func (failingWarnWriter) Write([]byte) (int, error) {
	return 0, errors.New("warning write failed")
}

func TestSyncChatWarningWriterError(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	rawKey := "chats/raw/20260514-aabbccdd/source.json"
	source.chatSessions["20260514-aabbccdd"] = repository.ChatSession{
		ID:              "20260514-aabbccdd",
		Source:          "codex",
		SourceSessionID: "missing-local",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
		RawSourceKey:    &rawKey,
	}
	transfer := func(_ context.Context, _ string, session repository.ChatSession, report *chatRawSyncReport) error {
		report.MissingLocal = append(report.MissingLocal, session.ID)
		return nil
	}

	err := syncChangedChats(context.Background(), time.Time{}, "push", source, target, transfer, failingWarnWriter{})
	if err == nil || !strings.Contains(err.Error(), "write local chat raw source warning") {
		t.Fatalf("expected warning writer error, got %v", err)
	}
}

// TestSyncChatMissingObjectsAggregateWarningOnStderr verifies that multiple
// missing raw chat sources are reported once with an aggregated count, with
// guidance to run pc doctor --verbose for per-chat details.
func TestSyncChatMissingObjectsAggregateWarningOnStderr(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	key1 := "chats/raw/20260514-feedbeef/source.json"
	key2 := "chats/raw/20260514-cafef00d/source.jsonl"
	key3 := "chats/raw/20260514-d00fa11d/source.ndjson"
	for id, key := range map[string]string{
		"20260514-feedbeef": key1,
		"20260514-cafef00d": key2,
		"20260514-d00fa11d": key3,
	} {
		k := key
		source.chatSessions[id] = repository.ChatSession{
			ID:              id,
			Source:          "codex",
			SourceSessionID: "src-" + id,
			SourceDeviceID:  "device",
			StartedAt:       now,
			LastActivityAt:  now,
			CreatedAt:       now,
			UpdatedAt:       now,
			RawSourceKey:    &k,
		}
	}

	// Every transfer reports MissingLocal — simulating all raw files absent.
	transfer := func(_ context.Context, _ string, session repository.ChatSession, report *chatRawSyncReport) error {
		report.MissingLocal = append(report.MissingLocal, session.ID)
		return nil
	}

	captured := &bytes.Buffer{}
	if err := syncChangedChats(context.Background(), time.Time{}, "push", source, target, transfer, captured); err != nil {
		t.Fatalf("syncChangedChats: %v", err)
	}

	if !strings.Contains(captured.String(), "3 raw chat source files missing locally") {
		t.Fatalf("expected aggregated MissingLocal count of 3, got %q", captured.String())
	}
	if !strings.Contains(captured.String(), "pc doctor --verbose") {
		t.Fatalf("expected guidance to run pc doctor --verbose, got %q", captured.String())
	}
}

// TestSyncChatMissingCloudAggregatesPullWarnings tests the pull-side
// MissingCloud branch produces a similar single aggregated stderr line.
func TestSyncChatMissingCloudAggregatesPullWarnings(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	for _, id := range []string{"20260514-aaaaaaaa", "20260514-bbbbbbbb"} {
		key := "chats/raw/" + id + "/source.jsonl"
		source.chatSessions[id] = repository.ChatSession{
			ID:              id,
			Source:          "codex",
			SourceSessionID: "src-" + id,
			SourceDeviceID:  "device",
			StartedAt:       now,
			LastActivityAt:  now,
			CreatedAt:       now,
			UpdatedAt:       now,
			RawSourceKey:    &key,
		}
	}
	transfer := func(_ context.Context, _ string, session repository.ChatSession, report *chatRawSyncReport) error {
		report.MissingCloud = append(report.MissingCloud, session.ID)
		return nil
	}

	captured := &bytes.Buffer{}
	if err := syncChangedChats(context.Background(), time.Time{}, "pull", source, target, transfer, captured); err != nil {
		t.Fatalf("syncChangedChats pull: %v", err)
	}

	if !strings.Contains(captured.String(), "2 raw chat source objects missing in cloud") {
		t.Fatalf("expected aggregated MissingCloud count of 2, got %q", captured.String())
	}
}

// TestTransferChatRawSourcePushUploadsExistingFile drives the service-level
// push path with a real on-disk source file and a mock object store, and
// verifies the upload succeeds without recording the session as MissingLocal.
func TestTransferChatRawSourcePushUploadsExistingFile(t *testing.T) {
	service, _, _, localFS, objects, _ := newTestService(t, nil, nil)
	chatID := "20260514-feedbeef"
	key := "chats/raw/" + chatID + "/source.json"
	path, err := localFS.ResolveChatSourcePath(chatID, key)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := os.MkdirAll(filepathDir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("raw payload"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rkey := key
	session := repository.ChatSession{ID: chatID, RawSourceKey: &rkey}
	report := chatRawSyncReport{}
	if err := service.transferChatRawSource(context.Background(), "push", session, &report); err != nil {
		t.Fatalf("transferChatRawSource: %v", err)
	}
	if len(report.MissingLocal) != 0 {
		t.Fatalf("expected no MissingLocal entries, got %v", report.MissingLocal)
	}
	if _, ok := objects.objects[key]; !ok {
		t.Fatalf("expected key %s uploaded, store has: %v", key, objects.objects)
	}
}

// TestTransferChatRawSourcePushReportsMissingLocal verifies that a missing
// local file marks the session as MissingLocal without halting the sync.
func TestTransferChatRawSourcePushReportsMissingLocal(t *testing.T) {
	service, _, _, _, _, _ := newTestService(t, nil, nil)
	chatID := "20260514-cafef00d"
	rkey := "chats/raw/" + chatID + "/source.json"
	session := repository.ChatSession{ID: chatID, RawSourceKey: &rkey}
	report := chatRawSyncReport{}
	if err := service.transferChatRawSource(context.Background(), "push", session, &report); err != nil {
		t.Fatalf("transferChatRawSource: %v", err)
	}
	if len(report.MissingLocal) != 1 || report.MissingLocal[0] != chatID {
		t.Fatalf("expected MissingLocal=[%s], got %v", chatID, report.MissingLocal)
	}
}

// TestTransferChatRawSourcePullDownloadsExistingObject seeds the mock object
// store and verifies pull writes the downloaded bytes to the resolved path.
func TestTransferChatRawSourcePullDownloadsExistingObject(t *testing.T) {
	service, _, _, localFS, objects, _ := newTestService(t, nil, nil)
	chatID := "20260514-bbbb1111"
	key := "chats/raw/" + chatID + "/source.jsonl"
	objects.objects[key] = "pulled-bytes"
	rkey := key
	session := repository.ChatSession{ID: chatID, RawSourceKey: &rkey}
	report := chatRawSyncReport{}
	if err := service.transferChatRawSource(context.Background(), "pull", session, &report); err != nil {
		t.Fatalf("transferChatRawSource pull: %v", err)
	}
	if len(report.MissingCloud) != 0 {
		t.Fatalf("expected no MissingCloud, got %v", report.MissingCloud)
	}
	path, err := localFS.ResolveChatSourcePath(chatID, key)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if string(got) != "pulled-bytes" {
		t.Fatalf("expected pulled bytes, got %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat pulled file: %v", err)
	}
	if info.Mode().Perm() != syncedChatRawFilePermission {
		t.Fatalf("pulled raw source perm = %o, want %o", info.Mode().Perm(), syncedChatRawFilePermission)
	}
}

// TestTransferChatRawSourcePullReportsMissingCloud verifies that a NoSuchKey
// download error is mapped to MissingCloud and the sync continues.
func TestTransferChatRawSourcePullReportsMissingCloud(t *testing.T) {
	service, _, _, _, objects, _ := newTestService(t, nil, nil)
	objects.downloadErr = &s3types.NoSuchKey{Message: strPtr("not found")}
	chatID := "20260514-ddddeeee"
	rkey := "chats/raw/" + chatID + "/source.json"
	session := repository.ChatSession{ID: chatID, RawSourceKey: &rkey}
	report := chatRawSyncReport{}
	if err := service.transferChatRawSource(context.Background(), "pull", session, &report); err != nil {
		t.Fatalf("transferChatRawSource pull: %v", err)
	}
	if len(report.MissingCloud) != 1 || report.MissingCloud[0] != chatID {
		t.Fatalf("expected MissingCloud=[%s], got %v", chatID, report.MissingCloud)
	}
}

// TestTransferChatRawSourceInvalidKeyRejected verifies that a raw_source_key
// that does not match the owning chat session is rejected by the filesystem
// resolver before any S3 traffic happens.
func TestTransferChatRawSourcePushReturnsUploadError(t *testing.T) {
	service, _, _, localFS, objects, _ := newTestService(t, nil, nil)
	objects.uploadErr = errors.New("upload blocked")
	chatID := "20260514-bbbb2222"
	key := "chats/raw/" + chatID + "/source.json"
	path, err := localFS.ResolveChatSourcePath(chatID, key)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := os.MkdirAll(filepathDir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	rkey := key
	session := repository.ChatSession{ID: chatID, RawSourceKey: &rkey}
	report := chatRawSyncReport{}
	if err := service.transferChatRawSource(context.Background(), "push", session, &report); err == nil || !strings.Contains(err.Error(), "upload chat raw source") {
		t.Fatalf("expected upload-error to surface, got %v", err)
	}
}

func TestTransferChatRawSourcePullReturnsGenericError(t *testing.T) {
	service, _, _, _, objects, _ := newTestService(t, nil, nil)
	objects.downloadErr = errors.New("network reset")
	chatID := "20260514-cccc3333"
	rkey := "chats/raw/" + chatID + "/source.json"
	session := repository.ChatSession{ID: chatID, RawSourceKey: &rkey}
	report := chatRawSyncReport{}
	if err := service.transferChatRawSource(context.Background(), "pull", session, &report); err == nil || !strings.Contains(err.Error(), "download chat raw source") {
		t.Fatalf("expected wrapped download error, got %v", err)
	}
}

func TestTransferChatRawSourceInvalidKeyRejected(t *testing.T) {
	service, _, _, _, _, _ := newTestService(t, nil, nil)
	chatID := "20260514-12345678"
	bad := "chats/raw/other-id/source.json"
	session := repository.ChatSession{ID: chatID, RawSourceKey: &bad}
	report := chatRawSyncReport{}
	err := service.transferChatRawSource(context.Background(), "push", session, &report)
	if err == nil || !strings.Contains(err.Error(), "chat raw source path") {
		t.Fatalf("expected rejection for invalid key, got %v", err)
	}
}

// TestIsCloudObjectNotFoundDirect exercises the typed-error detection: typed
// S3 NoSuchKey / NotFound count as object misses; NoSuchBucket and arbitrary
// error strings (including ones containing the substring "not found") do not.
func TestIsCloudObjectNotFoundDirect(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"NoSuchKey typed", &s3types.NoSuchKey{Message: strPtr("not found")}, true},
		{"NotFound typed", &s3types.NotFound{Message: strPtr("not found")}, true},
		{"NoSuchBucket typed (must not be treated as missing object)", &s3types.NoSuchBucket{Message: strPtr("bucket not found")}, false},
		{"plain error with substring 'not found'", errors.New("not found: x"), false},
		{"plain error containing '404'", errors.New("HTTP 404"), false},
		{"unrelated error", errors.New("unexpected"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isCloudObjectNotFound(c.err); got != c.want {
				t.Fatalf("isCloudObjectNotFound(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}


// filepathDir is a tiny indirection so the test file's only import addition
// is the standard library os package (already present).
func filepathDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}

// TestSyncChatSameKeyRawByteReplacementUpload verifies that a same-key raw
// re-import triggers a fresh upload during push: the transfer hook is invoked
// once per changed session even when the raw_source_key string is unchanged
// across passes, mirroring the plan's "same-extension re-import" contract.
func TestSyncChatSameKeyRawByteReplacementUpload(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	source := newMemoryRepo(nil)
	target := newMemoryRepo(nil)
	chatID := "20260514-1234abcd"
	key := "chats/raw/" + chatID + "/source.jsonl"

	source.chatSessions[chatID] = repository.ChatSession{
		ID:              chatID,
		Source:          "codex",
		SourceSessionID: "same-key",
		SourceDeviceID:  "device",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
		RawSourceKey:    &key,
	}

	// First pass: upload one. Second pass with same key but bumped
	// UpdatedAt should still upload again.
	uploads := 0
	transfer := func(_ context.Context, _ string, session repository.ChatSession, _ *chatRawSyncReport) error {
		uploads++
		return nil
	}
	if err := syncChangedChats(context.Background(), time.Time{}, "push", source, target, transfer, nil); err != nil {
		t.Fatalf("first push: %v", err)
	}
	bumped := source.chatSessions[chatID]
	bumped.UpdatedAt = now.Add(time.Minute)
	source.chatSessions[chatID] = bumped
	if err := syncChangedChats(context.Background(), now, "push", source, target, transfer, nil); err != nil {
		t.Fatalf("second push: %v", err)
	}
	if uploads != 2 {
		t.Fatalf("expected 2 raw uploads across same-key reruns, got %d", uploads)
	}
}
