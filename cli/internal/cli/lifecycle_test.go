package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

// importTrashableChat imports a single transcript and returns the chat ID and
// the path to the managed raw source file under chats/raw/.
func importTrashableChat(t *testing.T, homeDir string) (string, string) {
	t.Helper()
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "session.json")
	transcript := `{
  "id": "trash-session",
  "cwd": "/tmp/trash-chat",
  "title": "Trash chat",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [{"role": "user", "content": "hi"}]
}`
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import: %v", err)
	}
	stdout := &bytes.Buffer{}
	listCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	listCmd.SetArgs([]string{"chat", "list", "--format", "json"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("chat list: %v", err)
	}
	var page struct {
		Items []chatSessionJSON `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &page); err != nil {
		t.Fatalf("parse chat list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].RawSourceKey == nil {
		t.Fatalf("expected one chat with raw_source_key, got %+v", page.Items)
	}
	chatID := page.Items[0].ID
	rawPath := filepath.Join(homeDir, "personal-context", filepath.FromSlash(*page.Items[0].RawSourceKey))
	if _, err := os.Stat(rawPath); err != nil {
		t.Fatalf("expected managed raw path to exist: %v", err)
	}
	return chatID, rawPath
}

func TestTrashIncludesChatType(t *testing.T) {
	homeDir := setupEnv(t)
	recordID := addRecord(t)
	chatID, _ := importTrashableChat(t, homeDir)

	// Soft-delete both.
	for _, args := range [][]string{
		{"records", "delete", recordID},
		{"chat", "delete", chatID},
	} {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"trash"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("trash: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, recordID) || !strings.Contains(out, "\trecord\t") {
		t.Fatalf("expected record row in trash, got %q", out)
	}
	if !strings.Contains(out, chatID) || !strings.Contains(out, "\tchat\t") {
		t.Fatalf("expected chat row in trash with TYPE=chat, got %q", out)
	}
}

func TestChatSoftDeletePreservesRawSource(t *testing.T) {
	homeDir := setupEnv(t)
	chatID, rawPath := importTrashableChat(t, homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "delete", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat delete: %v", err)
	}
	if _, err := os.Stat(rawPath); err != nil {
		t.Fatalf("expected raw source to remain after soft-delete: %v", err)
	}
}

func TestChatRestoreRoundTrip(t *testing.T) {
	homeDir := setupEnv(t)
	chatID, rawPath := importTrashableChat(t, homeDir)

	// soft-delete
	delCmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	delCmd.SetArgs([]string{"chat", "delete", chatID})
	if err := delCmd.Execute(); err != nil {
		t.Fatalf("chat delete: %v", err)
	}

	// restore
	stdout := &bytes.Buffer{}
	restCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	restCmd.SetArgs([]string{"chat", "restore", chatID})
	if err := restCmd.Execute(); err != nil {
		t.Fatalf("chat restore: %v", err)
	}
	if !strings.Contains(stdout.String(), "restored") {
		t.Fatalf("expected restored output, got %q", stdout.String())
	}

	// chat is back in active listing, raw still present
	listOut := &bytes.Buffer{}
	listCmd := NewRootCommand(RootCommandOptions{Stdout: listOut, Stderr: &bytes.Buffer{}})
	listCmd.SetArgs([]string{"chat", "list", "--format", "ids"})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("chat list: %v", err)
	}
	if !strings.Contains(listOut.String(), chatID) {
		t.Fatalf("expected restored chat in active listing, got %q", listOut.String())
	}
	if _, err := os.Stat(rawPath); err != nil {
		t.Fatalf("expected raw path to remain after restore: %v", err)
	}
	_ = homeDir
}

func TestGCRemovesOnlyChatRawDirOnHardDelete(t *testing.T) {
	homeDir := setupEnv(t)
	chatID, rawPath := importTrashableChat(t, homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "delete", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat delete: %v", err)
	}
	backdateChatDeletedAtUnit(t, homeDir, chatID, 31)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errCloudNotConfigured
	}

	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	runAutoSyncFn = func(context.Context, io.Writer) error { return nil }

	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	if err := gcCmd.Execute(); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if !strings.Contains(stdout.String(), "Deleted "+chatID) {
		t.Fatalf("expected chat to be hard-deleted, got %q", stdout.String())
	}
	if _, err := os.Stat(rawPath); !os.IsNotExist(err) {
		t.Fatalf("expected raw source dir to be removed, stat err=%v", err)
	}
}

func TestGCCloudFirstChatDeletesRawObjectThenDBRow(t *testing.T) {
	homeDir := setupEnv(t)
	chatID, rawPath := importTrashableChat(t, homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "delete", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat delete: %v", err)
	}
	backdateChatDeletedAtUnit(t, homeDir, chatID, 31)

	deleteCalls := []string{}
	cloudRepoMock := &gcMockChatRepo{
		deleteChat: func(ctx context.Context, id string) error {
			deleteCalls = append(deleteCalls, "db:"+id)
			return nil
		},
	}
	s3 := newTestS3Client(t, &chatGCS3Handler{
		t: t,
		onDelete: func(key string) {
			deleteCalls = append(deleteCalls, "s3:"+key)
		},
	})
	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return &cloudStack{Repo: cloudRepoMock, S3: s3}, nil
	}

	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	runAutoSyncFn = func(context.Context, io.Writer) error { return nil }

	stdout := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	gcCmd.SetArgs([]string{"gc"})
	if err := gcCmd.Execute(); err != nil {
		t.Fatalf("gc: %v", err)
	}

	if len(deleteCalls) != 2 {
		t.Fatalf("expected 2 cloud calls (raw object then DB row), got %v", deleteCalls)
	}
	if !strings.HasPrefix(deleteCalls[0], "s3:") {
		t.Fatalf("expected raw S3 object deleted first, got %v", deleteCalls)
	}
	if !strings.HasPrefix(deleteCalls[1], "db:") {
		t.Fatalf("expected DB row deleted after S3 object, got %v", deleteCalls)
	}
	if _, err := os.Stat(rawPath); !os.IsNotExist(err) {
		t.Fatalf("expected local raw dir to be removed, stat err=%v", err)
	}
}

func TestGCCloudChatDeleteErrorSkipsLocalPurge(t *testing.T) {
	homeDir := setupEnv(t)
	chatID, rawPath := importTrashableChat(t, homeDir)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "delete", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat delete: %v", err)
	}
	backdateChatDeletedAtUnit(t, homeDir, chatID, 31)

	cloudRepoMock := &gcMockChatRepo{
		deleteChat: func(ctx context.Context, id string) error { return errors.New("cloud db down") },
	}
	s3 := newTestS3Client(t, &chatGCS3Handler{t: t})
	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return &cloudStack{Repo: cloudRepoMock, S3: s3}, nil
	}
	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	runAutoSyncFn = func(context.Context, io.Writer) error { return nil }

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	gcCmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	gcCmd.SetArgs([]string{"gc"})
	if err := gcCmd.Execute(); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if !strings.Contains(stderr.String(), "Warning: failed to delete chat") {
		t.Fatalf("expected cloud delete warning on stderr, got %q", stderr.String())
	}
	if _, err := os.Stat(rawPath); err != nil {
		t.Fatalf("expected raw source to remain when cloud delete fails: %v", err)
	}
}

// gcMockChatRepo extends mockRepo for chat-side gc behavior.
type gcMockChatRepo struct {
	mockRepo
	deleteChat func(ctx context.Context, id string) error
}

func (m *gcMockChatRepo) DeleteChatSession(ctx context.Context, id string) error {
	if m.deleteChat != nil {
		return m.deleteChat(ctx, id)
	}
	return nil
}

// chatGCS3Handler is a minimal http.Handler that satisfies S3 DELETE/HEAD
// requests issued by the chat gc cloud-first flow.
type chatGCS3Handler struct {
	t        *testing.T
	onDelete func(key string)
}

func TestIsCloudObjectNotFoundString(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"NoSuchKey typed", &s3types.NoSuchKey{}, true},
		{"NotFound typed", &s3types.NotFound{}, true},
		{"NoSuchBucket typed (must not be treated as missing object)", &s3types.NoSuchBucket{}, false},
		{"plain not-found string (no typed wrapping)", errors.New("delete x: not found: missing"), false},
		{"plain 404 string (no typed wrapping)", errors.New("status 404"), false},
		{"unrelated", errors.New("Other error"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isCloudObjectNotFoundString(c.err); got != c.want {
				t.Fatalf("isCloudObjectNotFoundString(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestDeleteCloudObjectTolerantNilClient(t *testing.T) {
	err := deleteCloudObjectTolerant(context.Background(), nil, "chats/raw/x/source.json")
	if err == nil {
		t.Fatal("expected error for nil s3 client")
	}
	if !strings.Contains(err.Error(), "S3 client is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteCloudObjectTolerantSuccessAnd404(t *testing.T) {
	// DELETE returns 204 — success path.
	successClient := newTestS3Client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))
	if err := deleteCloudObjectTolerant(context.Background(), successClient, "chats/raw/20250101-deadbeef/source.json"); err != nil {
		t.Fatalf("expected success delete to return nil, got %v", err)
	}

	// DELETE returns 404 — tolerated.
	notFoundClient := newTestS3Client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	if err := deleteCloudObjectTolerant(context.Background(), notFoundClient, "chats/raw/20250101-deadbeef/source.json"); err != nil {
		t.Fatalf("expected 404 delete to be tolerated, got %v", err)
	}

	// DELETE returns 500 — surfaces an error.
	errClient := newTestS3Client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	if err := deleteCloudObjectTolerant(context.Background(), errClient, "chats/raw/20250101-deadbeef/source.json"); err == nil {
		t.Fatal("expected non-404 error to be surfaced")
	}
}

func TestChatTrashDomainHardLocalFSError(t *testing.T) {
	homeDir := setupEnv(t)
	chatID, rawPath := importTrashableChat(t, homeDir)
	// Soft-delete first so the chat is in trash state.
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "delete", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rawDir := filepath.Dir(rawPath)
	// Make the raw dir's parent read-only — DeleteChatSource (RemoveAll) will fail.
	rawParent := filepath.Dir(rawDir)
	if err := os.Chmod(rawParent, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(rawParent, 0o755) })

	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })
	domain := chatTrashDomain()
	err = domain.HardLocal(context.Background(), stack, trashedItem{ID: chatID, Domain: "chat"})
	if err == nil {
		t.Fatal("expected FS cleanup failure")
	}
	var fsErr *lifecycleFSError
	if !errors.As(err, &fsErr) {
		t.Fatalf("expected lifecycleFSError, got %T (%v)", err, err)
	}
	if !fsErr.dbDeleted {
		t.Fatalf("expected dbDeleted=true on lifecycleFSError so caller treats FS failure as warn-and-continue")
	}
	if _, err := stack.Repo.GetChatSessionByID(context.Background(), chatID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("chat DB row should be deleted before FS cleanup runs; got err=%v", err)
	}
}

// TestRunGCAllReportsCloudUnreachableWarning covers the default cloud-open
// failure branch in runGCAll (non-nil err that is not errCloudNotConfigured).
func TestRunGCAllReportsCloudUnreachableWarning(t *testing.T) {
	homeDir := setupEnv(t)
	id := addRecord(t)
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "delete", id})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	backdateDeletedAtUnit(t, homeDir, id, 31)

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errors.New("cloud unreachable from test")
	}
	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	runAutoSyncFn = func(context.Context, io.Writer) error { return nil }

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runGCAll(context.Background(), stdout, stderr, allTrashDomains()); err != nil {
		t.Fatalf("runGCAll: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: cloud unreachable") {
		t.Fatalf("expected cloud unreachable warning, got %q", stderr.String())
	}
}

func TestChatTrashDomainHardLocalDBError(t *testing.T) {
	homeDir := setupEnv(t)
	chatID, _ := importTrashableChat(t, homeDir)
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "delete", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	corruptTable(t, homeDir, "chat_session")

	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })
	domain := chatTrashDomain()
	err = domain.HardLocal(context.Background(), stack, trashedItem{ID: chatID, Domain: "chat"})
	if err == nil {
		t.Fatal("expected DB delete failure")
	}
	var dbErr *lifecycleDBError
	if !errors.As(err, &dbErr) {
		t.Fatalf("expected lifecycleDBError, got %T", err)
	}
}

// TestChatTrashDomainHardCloudWithoutRawKey ensures the no-raw-object branch
// in chatTrashDomain.HardCloud is exercised (chat with nil RawSourceKey).
func TestChatTrashDomainHardCloudWithoutRawKey(t *testing.T) {
	domain := chatTrashDomain()
	calls := 0
	cloud := &cloudStack{Repo: &gcMockChatRepo{deleteChat: func(_ context.Context, _ string) error {
		calls++
		return nil
	}}, S3: nil}
	if err := domain.HardCloud(context.Background(), cloud, trashedItem{ID: "20250101-deadbeef", Domain: "chat"}); err != nil {
		t.Fatalf("HardCloud no-raw-key: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected DB delete to be called once, got %d", calls)
	}
}

// TestReportDoctorChatRawMissesWriteErrorBranches covers the warning header
// write-error and the verbose-detail write-error branches.
func TestReportDoctorChatRawMissesWriteErrorBranches(t *testing.T) {
	det := []chatRawMissDetail{{ChatID: "x", RawSourceKey: "chats/raw/x/source.json", ExpectedLocalPath: "/p", Origin: "local", OriginalSourcePath: "/old.json"}}
	if _, err := reportDoctorChatRawMisses(failingWriter{}, det, false); err == nil {
		t.Fatal("expected header write error")
	}
	if _, err := reportDoctorChatRawMisses(&failingWriterAfter{remaining: 1}, det, true); err == nil {
		t.Fatal("expected detail write error in verbose mode")
	}
}

// TestScanLocalChatRawMissesReportsDirectoryAtKey covers the IsDir branch.
func TestScanLocalChatRawMissesReportsDirectoryAtKey(t *testing.T) {
	homeDir := setupEnv(t)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = stack.Close() })
	chatID := "20250101-deadbeef"
	rawDir := filepath.Join(homeDir, "personal-context", "chats", "raw", chatID, "source.json")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatalf("mkdir trap: %v", err)
	}
	key := "chats/raw/" + chatID + "/source.json"
	misses, err := scanLocalChatRawMisses(stack.FS, []repository.ChatSession{{ID: chatID, RawSourceKey: &key}})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(misses) != 1 || !strings.Contains(misses[0].ExpectedLocalPath, "is a directory") {
		t.Fatalf("expected directory miss, got %+v", misses)
	}
}

// TestWriteTrashTableHeaderWriteError covers the header write-error branch.
func TestWriteTrashTableHeaderWriteError(t *testing.T) {
	w := &failingWriter{}
	if err := writeTrashTable(w, []trashedItem{{ID: "x", Domain: "record"}}); err == nil {
		t.Fatal("expected header write error")
	}
}

// TestWriteTrashTableRowWriteError covers the per-row write-error branch.
func TestWriteTrashTableRowWriteError(t *testing.T) {
	w := &failingWriterAfter{remaining: 1}
	if err := writeTrashTable(w, []trashedItem{{ID: "rec", Domain: "record"}}); err == nil {
		t.Fatal("expected row write error")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write blocked") }

type failingWriterAfter struct{ remaining int }

func (f *failingWriterAfter) Write(p []byte) (int, error) {
	if f.remaining <= 0 {
		return 0, errors.New("write blocked")
	}
	f.remaining--
	return len(p), nil
}

func TestRunGCAllUnknownDomainError(t *testing.T) {
	setupEnv(t)

	// Build a phantom domain that lists one item but is not in byDomain.
	listed := []trashedItem{{ID: "20260101-deadbeef", Domain: "phantom", DeletedAt: deletedAtPast()}}
	bogusDomain := trashDomain{
		Name:      "real",
		List:      func(_ context.Context, _ repository.Repository) ([]trashedItem, error) { return listed, nil },
		HardLocal: func(_ context.Context, _ *localStack, _ trashedItem) error { return nil },
		HardCloud: func(_ context.Context, _ *cloudStack, _ trashedItem) error { return nil },
	}

	origCloud := openCloudStackFn
	t.Cleanup(func() { openCloudStackFn = origCloud })
	openCloudStackFn = func(context.Context, string, string) (*cloudStack, error) {
		return nil, errCloudNotConfigured
	}
	origSync := runAutoSyncFn
	t.Cleanup(func() { runAutoSyncFn = origSync })
	runAutoSyncFn = func(context.Context, io.Writer) error { return nil }

	err := runGCAll(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, []trashDomain{bogusDomain})
	if err == nil || !strings.Contains(err.Error(), "no trash domain registered") {
		t.Fatalf("expected unknown-domain error, got %v", err)
	}
}

func deletedAtPast() *time.Time {
	t := time.Now().Add(-100 * 24 * time.Hour)
	return &t
}

func (h *chatGCS3Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "DELETE":
		key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")
		if h.onDelete != nil {
			h.onDelete(key)
		}
		w.WriteHeader(204)
	case "HEAD":
		w.WriteHeader(404)
	default:
		w.WriteHeader(200)
	}
}

var _ = repository.ErrNotFound
