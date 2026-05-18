//go:build integration

package cloude2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudSyncChatRoundTripAndSnapshotExport(t *testing.T) {
	cloud := newCloudTestEnv(t)
	homeA, userHomeA := setupCloudHome(t, cloud)
	homeB, userHomeB := setupCloudHomeNoSchema(t, cloud)

	const (
		deviceID        = "cloud-chat-device"
		projectID       = "phase7/cloud-chat"
		sourceSessionID = "cloud-chat-source-session"
		needle          = "cloud chat needle"
	)

	projectRoot := filepath.Join(userHomeA, "work", "cloud-chat")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	resolvedProjectRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	runPCSuccessNoStderr(t, homeA, userHomeA, "device", "register", deviceID)
	runPCSuccessNoStderr(t, homeA, userHomeA, "project", "add", projectID, projectRoot, "--device", deviceID)

	transcriptRoot := filepath.Join(userHomeA, ".codex", "sessions")
	if err := os.MkdirAll(transcriptRoot, 0o755); err != nil {
		t.Fatalf("create transcript root: %v", err)
	}
	transcript := map[string]any{
		"id":         sourceSessionID,
		"cwd":        filepath.Join(resolvedProjectRoot, "subdir"),
		"title":      "Cloud chat sync",
		"started_at": "2026-05-14T12:00:00Z",
		"messages": []map[string]string{
			{"role": "user", "content": needle + " from home A"},
			{"role": "assistant", "content": "synced assistant reply"},
		},
	}
	transcriptBytes, err := json.Marshal(transcript)
	if err != nil {
		t.Fatalf("marshal transcript: %v", err)
	}
	if err := os.WriteFile(filepath.Join(transcriptRoot, "cloud-chat.json"), transcriptBytes, 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	importOut := runPCSuccessNoStderr(t, homeA, userHomeA, "chat", "import", "--device", deviceID, "--agent", "codex", "--root", transcriptRoot)
	if !strings.Contains(importOut, `"sessions_created": 1`) || !strings.Contains(importOut, `"items_created": 2`) {
		t.Fatalf("unexpected chat import summary:\n%s", importOut)
	}
	runPCSuccessNoStderr(t, homeA, userHomeA, "sync")
	runPCSuccessNoStderr(t, homeB, userHomeB, "sync")

	listOut := runPCSuccessNoStderr(t, homeB, userHomeB, "chat", "list", "--format", "json")
	var listPage struct {
		Items []struct {
			ID              string  `json:"id"`
			SourceSessionID string  `json:"source_session_id"`
			ProjectID       *string `json:"project_id"`
			Title           *string `json:"title"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listOut), &listPage); err != nil {
		t.Fatalf("parse chat list json: %v\nraw: %s", err, listOut)
	}
	if len(listPage.Items) != 1 {
		t.Fatalf("expected one synced chat, got %+v", listPage.Items)
	}
	chat := listPage.Items[0]
	if chat.SourceSessionID != sourceSessionID {
		t.Fatalf("source_session_id = %q, want %q", chat.SourceSessionID, sourceSessionID)
	}
	if chat.ProjectID == nil || *chat.ProjectID != projectID {
		t.Fatalf("project_id = %#v, want %q", chat.ProjectID, projectID)
	}

	searchOut := runPCSuccessNoStderr(t, homeB, userHomeB, "chat", "search", "--format", "json", needle)
	if !strings.Contains(searchOut, chat.ID) || !strings.Contains(searchOut, needle) {
		t.Fatalf("expected synced chat search result for %s, got:\n%s", chat.ID, searchOut)
	}
	showOut := runPCSuccessNoStderr(t, homeB, userHomeB, "chat", "show", chat.ID)
	if !strings.Contains(showOut, needle) || !strings.Contains(showOut, "synced assistant reply") {
		t.Fatalf("expected synced chat transcript, got:\n%s", showOut)
	}

	exportDir := t.TempDir()
	runPCSuccessNoStderr(t, homeB, userHomeB, "export", "--path", exportDir)
	metadataPath := filepath.Join(exportDir, "chats", chat.ID, "metadata.json")
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read exported chat metadata: %v", err)
	}
	if !strings.Contains(string(metadataBytes), `"source_session_id": "`+sourceSessionID+`"`) {
		t.Fatalf("exported chat metadata missing source session id:\n%s", string(metadataBytes))
	}
	itemsBytes, err := os.ReadFile(filepath.Join(exportDir, "chats", chat.ID, "items.jsonl"))
	if err != nil {
		t.Fatalf("read exported chat items: %v", err)
	}
	if !strings.Contains(string(itemsBytes), needle) {
		t.Fatalf("exported chat items missing synced text:\n%s", string(itemsBytes))
	}
}

// TestCloudSyncChatRawSourceRoundTripAndSameKeyReplacement verifies the raw
// chat transcript bytes round-trip from home A to home B via S3, and that a
// same-key re-import on home A republishes the changed bytes so the next
// sync delivers them to home B.
func TestCloudSyncChatRawSourceRoundTripAndSameKeyReplacement(t *testing.T) {
	cloud := newCloudTestEnv(t)
	homeA, userHomeA := setupCloudHome(t, cloud)
	homeB, userHomeB := setupCloudHomeNoSchema(t, cloud)

	const (
		deviceID        = "raw-roundtrip-device"
		projectID       = "phase4/raw-roundtrip"
		sourceSessionID = "raw-roundtrip-session"
	)

	projectRoot := filepath.Join(userHomeA, "work", "raw-roundtrip")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	runPCSuccessNoStderr(t, homeA, userHomeA, "device", "register", deviceID)
	runPCSuccessNoStderr(t, homeA, userHomeA, "project", "add", projectID, projectRoot, "--device", deviceID)

	transcriptRoot := filepath.Join(userHomeA, ".codex", "sessions")
	if err := os.MkdirAll(transcriptRoot, 0o755); err != nil {
		t.Fatalf("create transcript root: %v", err)
	}
	first := map[string]any{
		"id":         sourceSessionID,
		"cwd":        projectRoot,
		"title":      "Raw round-trip",
		"started_at": "2026-05-14T12:00:00Z",
		"messages":   []map[string]string{{"role": "user", "content": "raw original"}},
	}
	firstBytes, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first transcript: %v", err)
	}
	transcriptPath := filepath.Join(transcriptRoot, "raw.json")
	if err := os.WriteFile(transcriptPath, firstBytes, 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	runPCSuccessNoStderr(t, homeA, userHomeA, "chat", "import", "--device", deviceID, "--agent", "codex", "--root", transcriptRoot)
	runPCSuccessNoStderr(t, homeA, userHomeA, "sync")
	runPCSuccessNoStderr(t, homeB, userHomeB, "sync")

	idsOut := runPCSuccessNoStderr(t, homeB, userHomeB, "chat", "list", "--format", "ids")
	chatID := strings.TrimSpace(idsOut)
	if chatID == "" {
		t.Fatalf("expected at least one chat id on home B, got %q", idsOut)
	}
	rawPathB := filepath.Join(homeB, "personal-context", "chats", "raw", chatID, "source.json")
	gotFirst, err := os.ReadFile(rawPathB)
	if err != nil {
		t.Fatalf("read raw on home B (first pass): %v", err)
	}
	if string(gotFirst) != string(firstBytes) {
		t.Fatalf("home B raw bytes differ from imported transcript:\nwant %q\n got %q", firstBytes, gotFirst)
	}

	// Same-key replacement: overwrite the source transcript with new content
	// and re-import on home A. Key stays as .json, only bytes change.
	second := map[string]any{
		"id":         sourceSessionID,
		"cwd":        projectRoot,
		"title":      "Raw round-trip",
		"started_at": "2026-05-14T12:00:00Z",
		"messages":   []map[string]string{{"role": "user", "content": "raw replaced"}},
	}
	secondBytes, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second transcript: %v", err)
	}
	if err := os.WriteFile(transcriptPath, secondBytes, 0o644); err != nil {
		t.Fatalf("rewrite transcript: %v", err)
	}
	runPCSuccessNoStderr(t, homeA, userHomeA, "chat", "import", "--device", deviceID, "--agent", "codex", "--root", transcriptRoot)
	runPCSuccessNoStderr(t, homeA, userHomeA, "sync")
	runPCSuccessNoStderr(t, homeB, userHomeB, "sync")

	gotSecond, err := os.ReadFile(rawPathB)
	if err != nil {
		t.Fatalf("read raw on home B (second pass): %v", err)
	}
	if string(gotSecond) != string(secondBytes) {
		t.Fatalf("expected home B raw to update to replacement bytes:\nwant %q\n got %q", secondBytes, gotSecond)
	}
}
