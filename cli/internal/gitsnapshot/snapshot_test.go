package gitsnapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWriteReadAndManifestRoundTrip(t *testing.T) {
	notes := "snapshot notes"
	altText := "alt text"
	description := "data description"
	chatProjectID := "phase7/unit"
	chatCWD := "/tmp/phase7/unit"
	chatTitle := "Snapshot chat"
	chatSourcePath := "/tmp/phase7/unit/session.jsonl"
	chatRawSourceKey := "chats/raw/20260309-ddddeeee/source.jsonl"
	chatRawSourceContent := []byte(`{"session_id":"session-jsonl","role":"user","content":"chat snapshot text"}`)
	chatText := "chat snapshot text"
	chatRaw := `{"role":"user","content":"chat snapshot text"}`
	snapshot := Snapshot{
		Templates: []Template{
			{Name: "single-image", HTMLContent: "<html>single-image</html>"},
			{Name: "text-only", HTMLContent: "<html>text-only</html>"},
		},
		Projects: []RegistryEntry{{
			ID:        "phase7/unit",
			CreatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
		}},
		Devices: []RegistryEntry{{
			ID:        "device/unit",
			CreatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
		}},
		Records: []Record{
			{
				ID:             "20260309-aaaabbbb",
				Date:           "2026-03-09",
				DayOrder:       "a0",
				ProjectID:      "phase7/unit",
				SourceDeviceID: "device/unit",
				GitRemoteURL:   strPtrSnapshot("https://github.com/org/repo"),
				GitHash:        strPtrSnapshot(strings.Repeat("a", 40)),
				HTMLContent:    strPtrSnapshot("<html><body><img src=\"figures/plot.png\"></body></html>"),
				Notes:          &notes,
				Figures: []Figure{{
					Filename: "plot.png",
					S3Key:    "figures/20260309-aaaabbbb/plot.png",
					AltText:  &altText,
					Content:  []byte("plot-bytes"),
				}},
				DataFiles: []DataFile{{
					Filename:    "metrics.csv",
					S3Key:       "data/20260309-aaaabbbb/metrics.csv",
					Size:        11,
					Hash:        "abc123",
					Description: &description,
				}},
				CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 123000000, time.UTC),
				UpdatedAt: time.Date(2026, 3, 9, 12, 30, 0, 456000000, time.UTC),
			},
			{
				ID:             "20260309-ccccdddd",
				Date:           "2026-03-09",
				DayOrder:       "b0",
				ProjectID:      "phase7/unit",
				SourceDeviceID: "device/unit",
				HTMLContent:    strPtrSnapshot("<html><body>minimal</body></html>"),
				Figures:        []Figure{},
				DataFiles:      []DataFile{},
				CreatedAt:      time.Date(2026, 3, 9, 13, 0, 0, 0, time.UTC),
				UpdatedAt:      time.Date(2026, 3, 9, 13, 0, 0, 0, time.UTC),
			},
		},
		Chats: []ChatSession{{
			ID:                 "20260309-ddddeeee",
			Source:             "codex",
			SourceSessionID:    "session-jsonl",
			SourceDeviceID:     "device/unit",
			ProjectID:          &chatProjectID,
			CWD:                &chatCWD,
			Title:              &chatTitle,
			StartedAt:          time.Date(2026, 3, 9, 14, 0, 0, 0, time.UTC),
			LastActivityAt:     time.Date(2026, 3, 9, 14, 5, 0, 0, time.UTC),
			OriginalSourcePath: &chatSourcePath,
			RawSourceKey:       &chatRawSourceKey,
			RawSourceContent:   chatRawSourceContent,
			CreatedAt:          time.Date(2026, 3, 9, 14, 0, 0, 0, time.UTC),
			UpdatedAt:          time.Date(2026, 3, 9, 14, 5, 0, 0, time.UTC),
			Items: []ChatItem{{
				Ordinal:    0,
				Role:       "user",
				ItemType:   "message",
				Text:       &chatText,
				SearchText: chatText,
				RawJSON:    &chatRaw,
				CreatedAt:  time.Date(2026, 3, 9, 14, 1, 0, 0, time.UTC),
			}, {
				Ordinal:    1,
				Role:       "assistant",
				ItemType:   "message",
				SearchText: "assistant second",
				CreatedAt:  time.Date(2026, 3, 9, 14, 5, 0, 0, time.UTC),
			}},
		}, {
			ID:                    "20260309-eeeeffff",
			Source:                "claude_code",
			SourceSessionID:       "parent-session-id:agent-aaa",
			ParentSourceSessionID: strPtrSnapshot("parent-session-id"),
			SourceDeviceID:        "device/unit",
			StartedAt:             time.Date(2026, 3, 9, 15, 0, 0, 0, time.UTC),
			LastActivityAt:        time.Date(2026, 3, 9, 15, 0, 0, 0, time.UTC),
			CreatedAt:             time.Date(2026, 3, 9, 15, 0, 0, 0, time.UTC),
			UpdatedAt:             time.Date(2026, 3, 9, 15, 0, 0, 0, time.UTC),
		}},
	}

	firstDir := t.TempDir()
	secondDir := t.TempDir()
	if err := Write(firstDir, snapshot); err != nil {
		t.Fatalf("Write(firstDir) error = %v", err)
	}
	if err := Write(secondDir, snapshot); err != nil {
		t.Fatalf("Write(secondDir) error = %v", err)
	}

	loaded, err := Read(firstDir)
	if err != nil {
		t.Fatalf("Read(firstDir) error = %v", err)
	}
	if !reflect.DeepEqual(snapshot, loaded) {
		t.Fatalf("round-trip snapshot mismatch\nwant=%#v\ngot=%#v", snapshot, loaded)
	}

	firstManifest, err := Manifest(firstDir)
	if err != nil {
		t.Fatalf("Manifest(firstDir) error = %v", err)
	}
	secondManifest, err := Manifest(secondDir)
	if err != nil {
		t.Fatalf("Manifest(secondDir) error = %v", err)
	}
	if !reflect.DeepEqual(firstManifest, secondManifest) {
		t.Fatalf("deterministic manifest mismatch\nfirst=%v\nsecond=%v", firstManifest, secondManifest)
	}

	metadataBytes, err := os.ReadFile(filepath.Join(firstDir, "records", "20260309-aaaabbbb", "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		t.Fatalf("parse metadata.json: %v", err)
	}
	if metadata["format_version"].(float64) != 1 {
		t.Fatalf("format_version = %v, want 1", metadata["format_version"])
	}

	chatItemsBytes, err := os.ReadFile(filepath.Join(firstDir, "chats", "20260309-ddddeeee", "items.jsonl"))
	if err != nil {
		t.Fatalf("read chat items.jsonl: %v", err)
	}
	if firstLine := strings.Split(strings.TrimSpace(string(chatItemsBytes)), "\n")[0]; !strings.Contains(firstLine, `"ordinal":0`) {
		t.Fatalf("expected chat items to be sorted by ordinal, got %q", firstLine)
	}
}

func TestReadChatsRejectsMalformedChatExports(t *testing.T) {
	if chats, err := readChats(filepath.Join(t.TempDir(), "missing")); err != nil || len(chats) != 0 {
		t.Fatalf("readChats(missing) = %+v, %v; want empty nil", chats, err)
	}
	readDirFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(readDirFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write read dir file: %v", err)
	}
	if _, err := readChats(readDirFile); err == nil || !strings.Contains(err.Error(), "read chats dir") {
		t.Fatalf("expected read chats dir error, got %v", err)
	}
	if _, err := readChat(t.TempDir(), "../bad"); err == nil {
		t.Fatal("expected invalid chat id to fail")
	}
	if _, err := readChat(t.TempDir(), "20260309-eeeeffff"); err == nil || !strings.Contains(err.Error(), "read chat metadata") {
		t.Fatalf("expected missing chat metadata to fail, got %v", err)
	}
	if _, err := readChatItems(filepath.Join(t.TempDir(), "missing.jsonl"), "20260309-eeeeffff"); err == nil {
		t.Fatal("expected missing chat items file to fail")
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "unexpected.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write unexpected file: %v", err)
	}
	if _, err := readChats(root); err == nil {
		t.Fatal("expected unexpected file in chats export to fail")
	}

	root = t.TempDir()
	chatDir := filepath.Join(root, "20260309-eeeeffff")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatalf("mkdir chat dir: %v", err)
	}
	metadata := `{
  "format_version": 2,
  "id": "20260309-eeeeffff",
  "source": "codex",
  "source_session_id": "source",
  "source_device_id": "device/unit",
  "started_at": "2026-03-09T14:00:00Z",
  "last_activity_at": "2026-03-09T14:05:00Z",
  "created_at": "2026-03-09T14:00:00Z",
  "updated_at": "2026-03-09T14:05:00Z"
}`
	if err := os.WriteFile(filepath.Join(chatDir, "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	items := strings.Join([]string{
		`{"ordinal":0,"role":"user","item_type":"message","search_text":"one","created_at":"2026-03-09T14:01:00Z"}`,
		`{"ordinal":0,"role":"assistant","item_type":"message","search_text":"duplicate","created_at":"2026-03-09T14:02:00Z"}`,
		``,
	}, "\n")
	if err := os.WriteFile(filepath.Join(chatDir, "items.jsonl"), []byte(items), 0o644); err != nil {
		t.Fatalf("write items: %v", err)
	}
	if _, err := readChats(root); err == nil || !strings.Contains(err.Error(), "duplicate chat item ordinal") {
		t.Fatalf("expected duplicate ordinal error, got %v", err)
	}

	if err := os.WriteFile(filepath.Join(chatDir, "metadata.json"), []byte(strings.Replace(metadata, `"format_version": 2`, `"format_version": 99`, 1)), 0o644); err != nil {
		t.Fatalf("write bad metadata: %v", err)
	}
	if _, err := readChat(chatDir, "20260309-eeeeffff"); err == nil || !strings.Contains(err.Error(), "unsupported chat format_version") {
		t.Fatalf("expected format version error, got %v", err)
	}

	if err := os.WriteFile(filepath.Join(chatDir, "metadata.json"), []byte(strings.Replace(metadata, `"id": "20260309-eeeeffff"`, `"id": "20260309-aaaabbbb"`, 1)), 0o644); err != nil {
		t.Fatalf("write mismatched metadata: %v", err)
	}
	if _, err := readChat(chatDir, "20260309-eeeeffff"); err == nil || !strings.Contains(err.Error(), "does not match dir") {
		t.Fatalf("expected metadata id mismatch error, got %v", err)
	}

	legacyMetadata := strings.Replace(metadata, `"source_session_id": "source",`, `"source_session_id": "source",`+"\n  "+`"source_path": "/tmp/legacy.json",`, 1)
	if err := os.WriteFile(filepath.Join(chatDir, "metadata.json"), []byte(legacyMetadata), 0o644); err != nil {
		t.Fatalf("write legacy metadata: %v", err)
	}
	if _, err := readChat(chatDir, "20260309-eeeeffff"); err == nil || !strings.Contains(err.Error(), "legacy source_path") {
		t.Fatalf("expected legacy source_path error, got %v", err)
	}

	if err := os.WriteFile(filepath.Join(chatDir, "metadata.json"), []byte(strings.Replace(metadata, `"started_at": "2026-03-09T14:00:00Z"`, `"started_at": "bad"`, 1)), 0o644); err != nil {
		t.Fatalf("write bad time metadata: %v", err)
	}
	if _, err := readChat(chatDir, "20260309-eeeeffff"); err == nil || !strings.Contains(err.Error(), "parse started_at") {
		t.Fatalf("expected started_at parse error, got %v", err)
	}

	for _, tc := range []struct {
		name string
		from string
		to   string
		want string
	}{
		{name: "last_activity_at", from: `"last_activity_at": "2026-03-09T14:05:00Z"`, to: `"last_activity_at": "bad"`, want: "parse last_activity_at"},
		{name: "created_at", from: `"created_at": "2026-03-09T14:00:00Z"`, to: `"created_at": "bad"`, want: "parse created_at"},
		{name: "updated_at", from: `"updated_at": "2026-03-09T14:05:00Z"`, to: `"updated_at": "bad"`, want: "parse updated_at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(chatDir, "metadata.json"), []byte(strings.Replace(metadata, tc.from, tc.to, 1)), 0o644); err != nil {
				t.Fatalf("write bad metadata: %v", err)
			}
			if _, err := readChat(chatDir, "20260309-eeeeffff"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s error, got %v", tc.want, err)
			}
		})
	}

	if err := os.WriteFile(filepath.Join(chatDir, "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("restore metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chatDir, "metadata.json"), []byte("{bad"), 0o644); err != nil {
		t.Fatalf("write malformed metadata: %v", err)
	}
	if _, err := readChat(chatDir, "20260309-eeeeffff"); err == nil || !strings.Contains(err.Error(), "parse chat metadata") {
		t.Fatalf("expected malformed metadata error, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(chatDir, "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("restore metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chatDir, "items.jsonl"), []byte(`{"ordinal":0,"role":"user","item_type":"message","search_text":"bad time","created_at":"bad"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write bad item time: %v", err)
	}
	if _, err := readChat(chatDir, "20260309-eeeeffff"); err == nil || !strings.Contains(err.Error(), "parse chat item created_at") {
		t.Fatalf("expected chat item timestamp error, got %v", err)
	}

	if err := os.WriteFile(filepath.Join(chatDir, "items.jsonl"), []byte("{bad\n"), 0o644); err != nil {
		t.Fatalf("write bad item json: %v", err)
	}
	if _, err := readChat(chatDir, "20260309-eeeeffff"); err == nil || !strings.Contains(err.Error(), "parse chat item") {
		t.Fatalf("expected chat item json error, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(chatDir, "items.jsonl"), []byte("\n"+`{"ordinal":0,"role":"user","item_type":"message","search_text":"ok","created_at":"2026-03-09T14:01:00Z"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write item with blank line: %v", err)
	}
	if items, err := readChatItems(filepath.Join(chatDir, "items.jsonl"), "20260309-eeeeffff"); err != nil || len(items) != 1 {
		t.Fatalf("expected item with blank line to parse, items=%+v err=%v", items, err)
	}

	rawKeyMetadata := strings.Replace(metadata, `"source_device_id": "device/unit",`, `"source_device_id": "device/unit",`+"\n  "+`"raw_source_key": "chats/raw/other/source.json",`, 1)
	if err := os.WriteFile(filepath.Join(chatDir, "metadata.json"), []byte(rawKeyMetadata), 0o644); err != nil {
		t.Fatalf("write raw-key metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chatDir, "items.jsonl"), []byte(""), 0o644); err != nil {
		t.Fatalf("write empty items: %v", err)
	}
	if _, err := readChat(chatDir, "20260309-eeeeffff"); err == nil || !strings.Contains(err.Error(), "raw source key") {
		t.Fatalf("expected raw source key error, got %v", err)
	}

	if err := Write("", Snapshot{}); err == nil {
		t.Fatal("expected empty root write to fail")
	}
	if _, err := Read(""); err == nil {
		t.Fatal("expected empty root read to fail")
	}
	if err := Write(t.TempDir(), Snapshot{Chats: []ChatSession{{ID: "../bad"}}}); err == nil {
		t.Fatal("expected invalid chat id write to fail")
	}
	rawKey := "chats/raw/20260309-eeeeffff/source.jsonl"
	if err := Write(t.TempDir(), Snapshot{Chats: []ChatSession{{
		ID:              "20260309-eeeeffff",
		Source:          "codex",
		SourceSessionID: "source",
		SourceDeviceID:  "device",
		StartedAt:       time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		LastActivityAt:  time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		RawSourceKey:    &rawKey,
		CreatedAt:       time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
	}}}); err == nil || !strings.Contains(err.Error(), "raw_source_key is set") {
		t.Fatalf("expected empty raw source content error, got %v", err)
	}
	if got, err := rawSourceExportFilename("20260309-eeeeffff", rawKey); err != nil || got != "source.jsonl" {
		t.Fatalf("rawSourceExportFilename() = %q, %v", got, err)
	}
	for _, badKey := range []string{
		"bad/raw/20260309-eeeeffff/source.jsonl",
		"chats/raw/20260309-deadbeef/source.jsonl",
		"chats/raw/20260309-eeeeffff/source.txt",
	} {
		if _, err := rawSourceExportFilename("20260309-eeeeffff", badKey); err == nil {
			t.Fatalf("expected rawSourceExportFilename(%q) to fail", badKey)
		}
	}
}

func TestReadChatsAndItemsSortTieBreakers(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	snapshot := Snapshot{Chats: []ChatSession{{
		ID:              "20260309-ffff0000",
		Source:          "codex",
		SourceSessionID: "source-z",
		SourceDeviceID:  "device/unit",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, {
		ID:              "20260309-aaaa0000",
		Source:          "codex",
		SourceSessionID: "source-a",
		SourceDeviceID:  "device/unit",
		StartedAt:       now,
		LastActivityAt:  now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}}}
	if err := Write(root, snapshot); err != nil {
		t.Fatalf("Write(root) error = %v", err)
	}
	chats, err := readChats(filepath.Join(root, "chats"))
	if err != nil {
		t.Fatalf("readChats() error = %v", err)
	}
	if len(chats) != 2 || chats[0].ID != "20260309-aaaa0000" || chats[1].ID != "20260309-ffff0000" {
		t.Fatalf("chat tie-break order = %+v, want ids ascending", chats)
	}

	itemsPath := filepath.Join(t.TempDir(), "items.jsonl")
	lines := strings.Join([]string{
		`{"ordinal":2,"role":"assistant","item_type":"message","search_text":"second","created_at":"2026-03-09T12:02:00Z"}`,
		`{"ordinal":0,"role":"user","item_type":"message","search_text":"first","created_at":"2026-03-09T12:01:00Z"}`,
		``,
	}, "\n")
	if err := os.WriteFile(itemsPath, []byte(lines), 0o644); err != nil {
		t.Fatalf("write item fixture: %v", err)
	}
	items, err := readChatItems(itemsPath, "20260309-aaaa0000")
	if err != nil {
		t.Fatalf("readChatItems() error = %v", err)
	}
	if len(items) != 2 || items[0].Ordinal != 0 || items[1].Ordinal != 2 {
		t.Fatalf("item order = %+v, want ordinal ascending", items)
	}
}

func TestReadChatRequiresRawSourceFileWhenKeyIsSet(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	rawKey := "chats/raw/20260309-abcddcba/source.jsonl"
	snapshot := Snapshot{Chats: []ChatSession{{
		ID:               "20260309-abcddcba",
		Source:           "codex",
		SourceSessionID:  "source",
		SourceDeviceID:   "device",
		RawSourceKey:     &rawKey,
		RawSourceContent: []byte("{}\n"),
		StartedAt:        now,
		LastActivityAt:   now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}}}
	if err := Write(root, snapshot); err != nil {
		t.Fatalf("Write(root) error = %v", err)
	}
	if err := os.Remove(filepath.Join(root, "chats", "20260309-abcddcba", "source.jsonl")); err != nil {
		t.Fatalf("remove raw source: %v", err)
	}
	if _, err := Read(root); err == nil || !strings.Contains(err.Error(), "read chat raw source") {
		t.Fatalf("expected missing chat raw source error, got %v", err)
	}
}

func TestPathSegmentAndLFSPointerBranches(t *testing.T) {
	for _, value := range []string{"", "   ", ".", "..", "dir/name", `dir\name`} {
		if err := validatePathSegment("field", value); err == nil {
			t.Fatalf("expected validatePathSegment(%q) to fail", value)
		}
	}
	if err := validatePathSegment("field", "valid-name"); err != nil {
		t.Fatalf("validatePathSegment(valid-name) error = %v", err)
	}
	if isLFSPointer(bytes.Repeat([]byte("x"), 513)) {
		t.Fatal("expected oversized content not to be treated as an LFS pointer")
	}
}

func TestReadRejectsGitLFSPointerFigures(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "records", "20260309-aaaabbbb", "figures"), 0o755); err != nil {
		t.Fatalf("mkdir record dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "templates", "text-only.html"), []byte("<html>template</html>"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	writeDefaultSnapshotRegistries(t, root)
	metadata := `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "project_id": "phase7/unit",
  "source_device_id": "device/unit",
  "has_notes": false,
  "figures": [
    {
      "filename": "plot.png",
      "s3_key": "figures/20260309-aaaabbbb/plot.png",
      "alt_text": null
    }
  ],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`
	if err := os.WriteFile(filepath.Join(root, "records", "20260309-aaaabbbb", "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "records", "20260309-aaaabbbb", "record.html"), []byte("<html><img src=\"figures/plot.png\"></html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "records", "20260309-aaaabbbb", "figures", "plot.png"),
		[]byte("version https://git-lfs.github.com/spec/v1\noid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\nsize 42\n"),
		0o644,
	); err != nil {
		t.Fatalf("write figure: %v", err)
	}

	_, err := Read(root)
	if err == nil {
		t.Fatal("expected Read() to reject Git LFS pointer figure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "git lfs") {
		t.Fatalf("expected git lfs error, got %v", err)
	}
}

func TestReadRejectsGitLFSPointerWithCRLF(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "records", "20260309-aaaabbbb", "figures"), 0o755); err != nil {
		t.Fatalf("mkdir record dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "templates", "text-only.html"), []byte("<html>template</html>"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	writeDefaultSnapshotRegistries(t, root)
	metadata := `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "project_id": "phase7/unit",
  "source_device_id": "device/unit",
  "has_notes": false,
  "figures": [
    {
      "filename": "plot.png",
      "s3_key": "figures/20260309-aaaabbbb/plot.png",
      "alt_text": null
    }
  ],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`
	if err := os.WriteFile(filepath.Join(root, "records", "20260309-aaaabbbb", "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "records", "20260309-aaaabbbb", "record.html"), []byte("<html>record</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
	// CRLF line endings
	if err := os.WriteFile(
		filepath.Join(root, "records", "20260309-aaaabbbb", "figures", "plot.png"),
		[]byte("version https://git-lfs.github.com/spec/v1\r\noid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\r\nsize 42\r\n"),
		0o644,
	); err != nil {
		t.Fatalf("write figure: %v", err)
	}

	_, err := Read(root)
	if err == nil {
		t.Fatal("expected Read() to reject Git LFS pointer with CRLF line endings")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "git lfs") {
		t.Fatalf("expected git lfs error, got %v", err)
	}
}

func TestWriteRejectsInvalidPathSegments(t *testing.T) {
	err := Write(t.TempDir(), Snapshot{
		Templates: []Template{{Name: "bad/name", HTMLContent: "x"}},
	})
	if err == nil {
		t.Fatal("expected invalid template name to fail")
	}

	err = Write(t.TempDir(), Snapshot{
		Records: []Record{{
			ID:          "bad/name",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: strPtrSnapshot("x"),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}},
	})
	if err == nil {
		t.Fatal("expected invalid record id to fail")
	}

	// data file filename with path separator
	err = Write(t.TempDir(), Snapshot{
		Records: []Record{{
			ID:          "20260309-aaaabbbb",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: strPtrSnapshot("x"),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
			DataFiles: []DataFile{{
				Filename: "../escape.csv",
				S3Key:    "data/escape.csv",
				Size:     10,
				Hash:     "abcd",
			}},
		}},
	})
	if err == nil {
		t.Fatal("expected invalid data file filename to fail")
	}
	if !strings.Contains(err.Error(), "data file filename") {
		t.Fatalf("expected data file filename error, got: %v", err)
	}

	// backslash path separator (portable validation)
	err = Write(t.TempDir(), Snapshot{
		Records: []Record{{
			ID:          "20260309-aaaabbbb",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: strPtrSnapshot("x"),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
			DataFiles: []DataFile{{
				Filename: "dir\\file.csv",
				S3Key:    "data/file.csv",
				Size:     10,
				Hash:     "abcd",
			}},
		}},
	})
	if err == nil {
		t.Fatal("expected backslash in data file filename to fail")
	}
}

func TestReadRejectsStructuralMismatches(t *testing.T) {
	t.Run("template directory entry", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "templates", "nested"), 0o755); err != nil {
			t.Fatalf("mkdir nested: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "records"), 0o755); err != nil {
			t.Fatalf("mkdir records: %v", err)
		}
		if _, err := Read(root); err == nil {
			t.Fatal("expected nested template directory to fail")
		}
	})

	t.Run("unsupported format version", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 2,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if _, err := Read(root); err == nil {
			t.Fatal("expected unsupported format_version to fail")
		}
	})

	t.Run("notes present despite has_notes false", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if err := os.WriteFile(filepath.Join(root, "records", "20260309-aaaabbbb", "notes.md"), []byte("unexpected"), 0o644); err != nil {
			t.Fatalf("write notes.md: %v", err)
		}
		if _, err := Read(root); err == nil {
			t.Fatal("expected notes mismatch to fail")
		}
	})

	t.Run("unexpected figure file", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if err := os.MkdirAll(filepath.Join(root, "records", "20260309-aaaabbbb", "figures"), 0o755); err != nil {
			t.Fatalf("mkdir figures: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "records", "20260309-aaaabbbb", "figures", "extra.png"), []byte("extra"), 0o644); err != nil {
			t.Fatalf("write extra figure: %v", err)
		}
		if _, err := Read(root); err == nil {
			t.Fatal("expected unexpected figure file to fail")
		}
	})

	t.Run("invalid data file filename on read", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [
    {"filename": "../escape.csv", "s3_key": "data/escape.csv", "size": 10, "hash": "abcd"}
  ],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		_, err := Read(root)
		if err == nil {
			t.Fatal("expected invalid data file filename to fail on read")
		}
		if !strings.Contains(err.Error(), "data file filename") {
			t.Fatalf("expected data file filename error, got: %v", err)
		}
	})

	t.Run("metadata id mismatch", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-wrongid",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if _, err := Read(root); err == nil {
			t.Fatal("expected metadata id mismatch to fail")
		}
	})

	t.Run("missing figure file", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [{"filename":"plot.png","s3_key":"figures/20260309-aaaabbbb/plot.png","alt_text":null}],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if err := os.MkdirAll(filepath.Join(root, "records", "20260309-aaaabbbb", "figures"), 0o755); err != nil {
			t.Fatalf("mkdir figures: %v", err)
		}
		if _, err := Read(root); err == nil {
			t.Fatal("expected missing figure file to fail")
		}
	})
}

func TestWriteFileErrorWhenParentIsFile(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "blocked")
	if err := os.WriteFile(parent, []byte("file"), 0o644); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	if err := writeFile(filepath.Join(parent, "child.txt"), []byte("x")); err == nil {
		t.Fatal("expected writeFile to fail when parent path is a file")
	}
}

func TestInternalReadersErrorPaths(t *testing.T) {
	t.Run("readTemplates bad extension", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "bad.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write bad template: %v", err)
		}
		if _, err := readTemplates(dir); err == nil {
			t.Fatal("expected readTemplates to reject non-html file")
		}
	})

	t.Run("readRecords unexpected file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "not-a-dir"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write unexpected file: %v", err)
		}
		if _, err := readRecords(dir); err == nil {
			t.Fatal("expected readRecords to reject plain files")
		}
	})

	t.Run("readRecord bad timestamp", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [],
  "created_at": "bad",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		if _, err := readRecord(filepath.Join(root, "records", "20260309-aaaabbbb"), "20260309-aaaabbbb"); err == nil {
			t.Fatal("expected readRecord to reject bad created_at")
		}
	})

	t.Run("readFigures nested dir", func(t *testing.T) {
		root := t.TempDir()
		writeMinimalSnapshotTree(t, root, `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [{"filename":"plot.png","s3_key":"figures/20260309-aaaabbbb/plot.png","alt_text":null}],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`)
		figuresDir := filepath.Join(root, "records", "20260309-aaaabbbb", "figures")
		if err := os.MkdirAll(filepath.Join(figuresDir, "nested"), 0o755); err != nil {
			t.Fatalf("mkdir nested: %v", err)
		}
		if err := os.WriteFile(filepath.Join(figuresDir, "plot.png"), []byte("plot"), 0o644); err != nil {
			t.Fatalf("write plot.png: %v", err)
		}
		if _, err := readFigures(filepath.Join(root, "records", "20260309-aaaabbbb"), "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}}); err == nil {
			t.Fatal("expected readFigures to reject nested directories")
		}
	})
}

func strPtrSnapshot(value string) *string {
	return &value
}

func writeDefaultSnapshotRegistries(t *testing.T, root string) {
	t.Helper()
	ts := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	if err := writeRegistryFile(filepath.Join(root, "projects.json"), []RegistryEntry{{
		ID:        "phase7/unit",
		CreatedAt: ts,
		UpdatedAt: ts,
	}}); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}
	if err := writeRegistryFile(filepath.Join(root, "devices.json"), []RegistryEntry{{
		ID:        "device/unit",
		CreatedAt: ts,
		UpdatedAt: ts,
	}}); err != nil {
		t.Fatalf("write devices.json: %v", err)
	}
}

func writeMinimalSnapshotTree(t *testing.T, root string, metadata string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "records", "20260309-aaaabbbb"), 0o755); err != nil {
		t.Fatalf("mkdir record dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "templates", "text-only.html"), []byte("<html>template</html>"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	var metadataMap map[string]any
	if err := json.Unmarshal([]byte(metadata), &metadataMap); err != nil {
		t.Fatalf("parse metadata fixture: %v", err)
	}
	if _, ok := metadataMap["project_id"]; !ok {
		metadataMap["project_id"] = "phase7/unit"
	}
	if _, ok := metadataMap["source_device_id"]; !ok {
		metadataMap["source_device_id"] = "device/unit"
	}
	metadataBytes, err := json.MarshalIndent(metadataMap, "", "  ")
	if err != nil {
		t.Fatalf("marshal metadata fixture: %v", err)
	}
	metadataBytes = append(metadataBytes, '\n')
	if err := os.WriteFile(filepath.Join(root, "records", "20260309-aaaabbbb", "metadata.json"), metadataBytes, 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
	if err := writeRegistryFile(filepath.Join(root, "projects.json"), []RegistryEntry{{
		ID:        "phase7/unit",
		CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}
	if err := writeRegistryFile(filepath.Join(root, "devices.json"), []RegistryEntry{{
		ID:        "device/unit",
		CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("write devices.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "records", "20260309-aaaabbbb", "record.html"), []byte("<html>record</html>"), 0o644); err != nil {
		t.Fatalf("write record.html: %v", err)
	}
}

func TestWriteRejectsInvalidInputs(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)

	if err := Write("   ", Snapshot{}); err == nil {
		t.Fatal("expected Write to reject an empty root")
	}

	rootFile := filepath.Join(t.TempDir(), "snapshot-root")
	if err := os.WriteFile(rootFile, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("write root file: %v", err)
	}
	if err := Write(rootFile, Snapshot{}); err == nil {
		t.Fatal("expected Write to reject a file path as the root")
	}

	cases := []struct {
		name     string
		snapshot Snapshot
		want     string
	}{
		{
			name: "invalid template name",
			snapshot: Snapshot{
				Templates: []Template{{Name: "bad/name", HTMLContent: "<html>bad</html>"}},
			},
			want: "template name",
		},
		{
			name: "invalid record id",
			snapshot: Snapshot{
				Records: []Record{{
					ID:          "bad/id",
					Date:        "2026-03-09",
					DayOrder:    "a0",
					HTMLContent: strPtrSnapshot("<html>bad</html>"),
					CreatedAt:   now,
					UpdatedAt:   now,
				}},
			},
			want: "record id",
		},
		{
			name: "invalid figure filename",
			snapshot: Snapshot{
				Records: []Record{{
					ID:          "20260309-aaaabbbb",
					Date:        "2026-03-09",
					DayOrder:    "a0",
					HTMLContent: strPtrSnapshot("<html>bad</html>"),
					Figures: []Figure{{
						Filename: "bad/name.png",
						S3Key:    "figures/20260309-aaaabbbb/bad/name.png",
						Content:  []byte("bad"),
					}},
					CreatedAt: now,
					UpdatedAt: now,
				}},
			},
			want: "figure filename",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Write(t.TempDir(), tc.snapshot)
			if err == nil {
				t.Fatalf("expected Write to fail for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Write error = %v, want substring %q", err, tc.want)
			}
		})
	}

	parentFile := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parentFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocking parent file: %v", err)
	}
	if err := writeFile(filepath.Join(parentFile, "child.txt"), []byte("content")); err == nil {
		t.Fatal("expected writeFile to fail when the parent path is a file")
	}
}

func TestWriteSortsRecordsFiguresAndDataFiles(t *testing.T) {
	root := t.TempDir()
	snapshot := Snapshot{
		Records: []Record{
			{
				ID:             "20260310-ccccdddd",
				Date:           "2026-03-10",
				DayOrder:       "a0",
				ProjectID:      "phase7/unit",
				SourceDeviceID: "device/unit",
				HTMLContent:    strPtrSnapshot("<html>later</html>"),
				CreatedAt:      time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
				UpdatedAt:      time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC),
			},
			{
				ID:             "20260309-bbbbbbbb",
				Date:           "2026-03-09",
				DayOrder:       "b0",
				ProjectID:      "phase7/unit",
				SourceDeviceID: "device/unit",
				HTMLContent:    strPtrSnapshot("<html>second</html>"),
				CreatedAt:      time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
				UpdatedAt:      time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			},
			{
				ID:             "20260309-aaaaaaaa",
				Date:           "2026-03-09",
				DayOrder:       "b0",
				ProjectID:      "phase7/unit",
				SourceDeviceID: "device/unit",
				HTMLContent:    strPtrSnapshot("<html>first</html>"),
				Figures: []Figure{
					{Filename: "zeta.png", S3Key: "figures/20260309-aaaaaaaa/zeta.png", Content: []byte("zeta")},
					{Filename: "alpha.png", S3Key: "figures/20260309-aaaaaaaa/alpha.png", Content: []byte("alpha")},
				},
				DataFiles: []DataFile{
					{Filename: "zeta.csv", S3Key: "data/20260309-aaaaaaaa/zeta.csv", Size: 4, Hash: "hash-zeta"},
					{Filename: "alpha.csv", S3Key: "data/20260309-aaaaaaaa/alpha.csv", Size: 5, Hash: "hash-alpha"},
				},
				CreatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
			},
		},
	}

	if err := Write(root, snapshot); err != nil {
		t.Fatalf("Write(root) error = %v", err)
	}

	records, err := readRecords(filepath.Join(root, "records"))
	if err != nil {
		t.Fatalf("readRecords(records) error = %v", err)
	}
	gotIDs := []string{records[0].ID, records[1].ID, records[2].ID}
	wantIDs := []string{"20260309-aaaaaaaa", "20260309-bbbbbbbb", "20260310-ccccdddd"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("record order = %v, want %v", gotIDs, wantIDs)
	}
	if len(records[0].Figures) != 2 || records[0].Figures[0].Filename != "alpha.png" || records[0].Figures[1].Filename != "zeta.png" {
		t.Fatalf("figure order = %#v, want alpha.png then zeta.png", records[0].Figures)
	}
	if len(records[0].DataFiles) != 2 || records[0].DataFiles[0].Filename != "alpha.csv" || records[0].DataFiles[1].Filename != "zeta.csv" {
		t.Fatalf("data file order = %#v, want alpha.csv then zeta.csv", records[0].DataFiles)
	}

	metadataBytes, err := os.ReadFile(filepath.Join(root, "records", "20260309-aaaaaaaa", "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var metadata metadataFile
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		t.Fatalf("parse metadata.json: %v", err)
	}
	if len(metadata.Figures) != 2 || metadata.Figures[0].Filename != "alpha.png" || metadata.Figures[1].Filename != "zeta.png" {
		t.Fatalf("metadata figures = %#v, want sorted by filename", metadata.Figures)
	}
	if len(metadata.DataFiles) != 2 || metadata.DataFiles[0].Filename != "alpha.csv" || metadata.DataFiles[1].Filename != "zeta.csv" {
		t.Fatalf("metadata data_files = %#v, want sorted by filename", metadata.DataFiles)
	}
}

func TestRegistryFilesReadWriteAndValidation(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	archived := now.Add(time.Hour)
	entries := []RegistryEntry{
		{ID: "zeta", CreatedAt: now, UpdatedAt: archived, ArchivedAt: &archived},
		{ID: "alpha", CreatedAt: now, UpdatedAt: now},
	}
	path := filepath.Join(root, "projects.json")
	if err := writeRegistryFile(path, entries); err != nil {
		t.Fatalf("writeRegistryFile() error = %v", err)
	}
	loaded, err := readRegistryFile(path)
	if err != nil {
		t.Fatalf("readRegistryFile() error = %v", err)
	}
	if len(loaded) != 2 || loaded[0].ID != "alpha" || loaded[1].ArchivedAt == nil {
		t.Fatalf("loaded registry entries = %#v", loaded)
	}

	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{name: "malformed", content: "{not-json}\n", want: "invalid character"},
		{name: "empty id", content: `[{"id":"","created_at":"2026-03-09T12:00:00Z","updated_at":"2026-03-09T12:00:00Z"}]`, want: "id is required"},
		{name: "bad created", content: `[{"id":"x","created_at":"bad","updated_at":"2026-03-09T12:00:00Z"}]`, want: "parse created_at"},
		{name: "bad updated", content: `[{"id":"x","created_at":"2026-03-09T12:00:00Z","updated_at":"bad"}]`, want: "parse updated_at"},
		{name: "bad archived", content: `[{"id":"x","created_at":"2026-03-09T12:00:00Z","updated_at":"2026-03-09T12:00:00Z","archived_at":"bad"}]`, want: "parse archived_at"},
		{name: "duplicate id", content: `[
			{"id":"x","created_at":"2026-03-09T12:00:00Z","updated_at":"2026-03-09T12:00:00Z"},
			{"id":"x","created_at":"2026-03-09T12:00:00Z","updated_at":"2026-03-09T12:00:00Z"}
		]`, want: "duplicate registry id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testPath := filepath.Join(t.TempDir(), "devices.json")
			if err := os.WriteFile(testPath, []byte(tc.content), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			_, err := readRegistryFile(testPath)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("readRegistryFile() error = %v, want %q", err, tc.want)
			}
		})
	}

	if err := writeRegistryFile(filepath.Join(t.TempDir(), "bad.json"), []RegistryEntry{{CreatedAt: now, UpdatedAt: now}}); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("expected writeRegistryFile to reject empty ID, got %v", err)
	}
	if err := writeRegistryFile(filepath.Join(t.TempDir(), "bad.json"), []RegistryEntry{{ID: "bad", CreatedAt: time.Time{}, UpdatedAt: now}}); err == nil || !strings.Contains(err.Error(), "must have created_at and updated_at") {
		t.Fatalf("expected writeRegistryFile to reject missing timestamp, got %v", err)
	}
	missingRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(missingRoot, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(missingRoot, "records"), 0o755); err != nil {
		t.Fatalf("mkdir records: %v", err)
	}
	if _, err := Read(missingRoot); err == nil || !strings.Contains(err.Error(), "read projects.json") {
		t.Fatalf("expected missing projects.json read error, got %v", err)
	}
	if err := writeRegistryFile(filepath.Join(missingRoot, "projects.json"), []RegistryEntry{{ID: "phase7/unit", CreatedAt: now, UpdatedAt: now}}); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}
	if _, err := Read(missingRoot); err == nil || !strings.Contains(err.Error(), "read devices.json") {
		t.Fatalf("expected missing devices.json read error, got %v", err)
	}
}

func TestWriteRejectsFilesystemFailures(t *testing.T) {
	t.Run("create staging dir", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o555); err != nil {
			t.Fatalf("chmod root: %v", err)
		}
		defer func() {
			if err := os.Chmod(root, 0o755); err != nil {
				t.Fatalf("restore root permissions: %v", err)
			}
		}()
		if err := Write(root, Snapshot{}); err == nil || !strings.Contains(err.Error(), "create staging snapshot dir") {
			t.Fatalf("Write(root) error = %v, want staging dir failure", err)
		}
	})

	t.Run("write template", func(t *testing.T) {
		root := t.TempDir()
		templateName := strings.Repeat("t", 251)
		err := Write(root, Snapshot{
			Templates: []Template{{Name: templateName, HTMLContent: "<html>too long</html>"}},
		})
		if err == nil || !strings.Contains(err.Error(), "write template") {
			t.Fatalf("Write(root) error = %v, want write template failure", err)
		}
	})

	t.Run("create record dir", func(t *testing.T) {
		root := t.TempDir()
		err := Write(root, Snapshot{
			Records: []Record{{
				ID:          strings.Repeat("s", 256),
				Date:        "2026-03-09",
				DayOrder:    "a0",
				HTMLContent: strPtrSnapshot("<html>too long</html>"),
				CreatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
				UpdatedAt:   time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "create record dir") {
			t.Fatalf("Write(root) error = %v, want create record dir failure", err)
		}
	})

	t.Run("write figure", func(t *testing.T) {
		root := t.TempDir()
		err := Write(root, Snapshot{
			Records: []Record{{
				ID:          "20260309-aaaabbbb",
				Date:        "2026-03-09",
				DayOrder:    "a0",
				HTMLContent: strPtrSnapshot("<html>figure</html>"),
				Figures: []Figure{{
					Filename: strings.Repeat("f", 256),
					S3Key:    "figures/20260309-aaaabbbb/too-long",
					Content:  []byte("figure-bytes"),
				}},
				CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "write figure") {
			t.Fatalf("Write(root) error = %v, want write figure failure", err)
		}
	})
}

func TestWritePreservesExistingSnapshotOnFailure(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	oldSnapshot := Snapshot{
		Templates: []Template{{Name: "old", HTMLContent: "<html>old</html>"}},
		Projects:  []RegistryEntry{{ID: "phase7/old", CreatedAt: now, UpdatedAt: now}},
		Devices:   []RegistryEntry{{ID: "device/old", CreatedAt: now, UpdatedAt: now}},
		Records: []Record{{
			ID:             "20260309-aaaabbbb",
			Date:           "2026-03-09",
			DayOrder:       "a0",
			ProjectID:      "phase7/old",
			SourceDeviceID: "device/old",
			HTMLContent:    strPtrSnapshot("<html>old record</html>"),
			CreatedAt:      now,
			UpdatedAt:      now,
		}},
	}
	newSnapshot := Snapshot{
		Templates: []Template{{Name: "new", HTMLContent: "<html>new</html>"}},
		Projects:  []RegistryEntry{{ID: "phase7/new", CreatedAt: now, UpdatedAt: now}},
		Devices:   []RegistryEntry{{ID: "device/new", CreatedAt: now, UpdatedAt: now}},
	}

	t.Run("staged write failure", func(t *testing.T) {
		root := t.TempDir()
		if err := Write(root, oldSnapshot); err != nil {
			t.Fatalf("Write(oldSnapshot): %v", err)
		}
		oldManifest, err := Manifest(root)
		if err != nil {
			t.Fatalf("Manifest(old): %v", err)
		}

		origCreateTemp := createTempFileFn
		createTempFileFn = func(string, string) (tempFile, error) {
			return &stubTempFile{name: filepath.Join(t.TempDir(), "template.tmp"), writeErr: errors.New("write failed")}, nil
		}
		t.Cleanup(func() {
			createTempFileFn = origCreateTemp
		})

		err = Write(root, newSnapshot)
		if err == nil || !strings.Contains(err.Error(), "write template new") {
			t.Fatalf("Write(newSnapshot) error = %v, want staged write failure", err)
		}
		currentManifest, err := Manifest(root)
		if err != nil {
			t.Fatalf("Manifest(current): %v", err)
		}
		if !reflect.DeepEqual(currentManifest, oldManifest) {
			t.Fatalf("snapshot changed after staged write failure\nold=%v\ncurrent=%v", oldManifest, currentManifest)
		}
	})

	t.Run("promotion failure", func(t *testing.T) {
		root := t.TempDir()
		if err := Write(root, oldSnapshot); err != nil {
			t.Fatalf("Write(oldSnapshot): %v", err)
		}
		oldManifest, err := Manifest(root)
		if err != nil {
			t.Fatalf("Manifest(old): %v", err)
		}

		origRename := renameFileFn
		renameFileFn = func(from string, to string) error {
			fromDir := filepath.Base(filepath.Dir(from))
			if filepath.Base(from) == "devices.json" && strings.HasPrefix(fromDir, ".snapshot-staging-") {
				return errors.New("promote failed")
			}
			return origRename(from, to)
		}
		t.Cleanup(func() {
			renameFileFn = origRename
		})

		err = Write(root, newSnapshot)
		if err == nil || !strings.Contains(err.Error(), "promote staged devices.json") {
			t.Fatalf("Write(newSnapshot) error = %v, want promotion failure", err)
		}
		currentManifest, err := Manifest(root)
		if err != nil {
			t.Fatalf("Manifest(current): %v", err)
		}
		if !reflect.DeepEqual(currentManifest, oldManifest) {
			t.Fatalf("snapshot changed after promotion failure\nold=%v\ncurrent=%v", oldManifest, currentManifest)
		}
	})
}

func TestReplaceSnapshotContentsErrorPaths(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	oldSnapshot := Snapshot{
		Templates: []Template{{Name: "old", HTMLContent: "<html>old</html>"}},
		Projects:  []RegistryEntry{{ID: "phase7/old", CreatedAt: now, UpdatedAt: now}},
		Devices:   []RegistryEntry{{ID: "device/old", CreatedAt: now, UpdatedAt: now}},
	}
	newSnapshot := Snapshot{
		Templates: []Template{{Name: "new", HTMLContent: "<html>new</html>"}},
		Projects:  []RegistryEntry{{ID: "phase7/new", CreatedAt: now, UpdatedAt: now}},
		Devices:   []RegistryEntry{{ID: "device/new", CreatedAt: now, UpdatedAt: now}},
	}

	t.Run("backup dir creation failure", func(t *testing.T) {
		rootFile := filepath.Join(t.TempDir(), "snapshot-root")
		if err := os.WriteFile(rootFile, []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("write root file: %v", err)
		}
		if err := replaceSnapshotContents(rootFile, t.TempDir()); err == nil || !strings.Contains(err.Error(), "create snapshot backup dir") {
			t.Fatalf("replaceSnapshotContents() error = %v, want backup dir creation failure", err)
		}
	})

	t.Run("backup existing failure restores moved entries", func(t *testing.T) {
		root := t.TempDir()
		stagingRoot := t.TempDir()
		if err := writeSnapshotContents(root, oldSnapshot); err != nil {
			t.Fatalf("writeSnapshotContents(old): %v", err)
		}
		oldManifest, err := Manifest(root)
		if err != nil {
			t.Fatalf("Manifest(old): %v", err)
		}
		if err := writeSnapshotContents(stagingRoot, newSnapshot); err != nil {
			t.Fatalf("writeSnapshotContents(new): %v", err)
		}

		origRename := renameFileFn
		renameFileFn = func(from string, to string) error {
			toDir := filepath.Base(filepath.Dir(to))
			if filepath.Base(from) == "devices.json" && strings.HasPrefix(toDir, ".snapshot-backup-") {
				return errors.New("backup failed")
			}
			return origRename(from, to)
		}
		t.Cleanup(func() {
			renameFileFn = origRename
		})

		err = replaceSnapshotContents(root, stagingRoot)
		if err == nil || !strings.Contains(err.Error(), "backup existing devices.json") {
			t.Fatalf("replaceSnapshotContents() error = %v, want backup failure", err)
		}
		currentManifest, err := Manifest(root)
		if err != nil {
			t.Fatalf("Manifest(current): %v", err)
		}
		if !reflect.DeepEqual(currentManifest, oldManifest) {
			t.Fatalf("snapshot changed after backup failure\nold=%v\ncurrent=%v", oldManifest, currentManifest)
		}
	})

	t.Run("restore failure reports backup path", func(t *testing.T) {
		root := t.TempDir()
		stagingRoot, err := os.MkdirTemp(root, ".snapshot-staging-*")
		if err != nil {
			t.Fatalf("create staging root: %v", err)
		}
		if err := writeSnapshotContents(root, oldSnapshot); err != nil {
			t.Fatalf("writeSnapshotContents(old): %v", err)
		}
		if err := writeSnapshotContents(stagingRoot, newSnapshot); err != nil {
			t.Fatalf("writeSnapshotContents(new): %v", err)
		}

		origRename := renameFileFn
		renameFileFn = func(from string, to string) error {
			fromDir := filepath.Base(filepath.Dir(from))
			if filepath.Base(from) == "devices.json" && strings.HasPrefix(fromDir, ".snapshot-staging-") {
				return errors.New("promote failed")
			}
			if filepath.Base(from) == "projects.json" && strings.HasPrefix(fromDir, ".snapshot-backup-") {
				return errors.New("restore failed")
			}
			return origRename(from, to)
		}
		t.Cleanup(func() {
			renameFileFn = origRename
		})

		err = replaceSnapshotContents(root, stagingRoot)
		if err == nil || !strings.Contains(err.Error(), "failed to restore previous snapshot state") {
			t.Fatalf("replaceSnapshotContents() error = %v, want restore failure detail", err)
		}
		if !strings.Contains(err.Error(), ".snapshot-backup-") {
			t.Fatalf("replaceSnapshotContents() error = %v, want backup path", err)
		}
	})
}

func TestReplaceContentsWithNestedRestoreEntries(t *testing.T) {
	root := t.TempDir()
	staging := t.TempDir()
	backupDir := filepath.Join(root, ".pc", "backups", "restore-db-test", ".restore-payload")
	entries := []string{
		filepath.Join(".pc", "pc.db"),
		filepath.Join(".pc", "last_sync"),
		"figures",
		filepath.Join("chats", "raw"),
	}
	writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "old-db")
	writeReplacementTestFile(t, filepath.Join(root, ".pc", "last_sync"), "sync")
	writeReplacementTestFile(t, filepath.Join(root, "figures", "old-record", "old.png"), "old-figure")
	writeReplacementTestFile(t, filepath.Join(root, "chats", "raw", "old-chat", "source.json"), "old-chat")
	writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
	writeReplacementTestFile(t, filepath.Join(staging, "figures", "new-record", "new.png"), "new-figure")

	if err := ReplaceContents(root, staging, ReplacementOptions{Entries: entries, BackupDir: backupDir}); err != nil {
		t.Fatalf("ReplaceContents() error = %v", err)
	}
	assertReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "new-db")
	assertReplacementTestFile(t, filepath.Join(root, "figures", "new-record", "new.png"), "new-figure")
	for _, removed := range []string{
		filepath.Join(root, ".pc", "last_sync"),
		filepath.Join(root, "figures", "old-record"),
		filepath.Join(root, "chats", "raw"),
		backupDir,
	} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", removed, err)
		}
	}
}

func TestCompleteReplacementRollsForwardRestoreStates(t *testing.T) {
	entries := []string{
		filepath.Join(".pc", "pc.db"),
		filepath.Join(".pc", "last_sync"),
		"figures",
	}

	t.Run("backs up old entries and promotes staged entries", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		backupDir := filepath.Join(t.TempDir(), "payload-backup")
		writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "old-db")
		writeReplacementTestFile(t, filepath.Join(root, ".pc", "last_sync"), "sync")
		writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
		writeReplacementTestFile(t, filepath.Join(staging, "figures", "new-record", "new.png"), "new-figure")

		if err := CompleteReplacement(root, staging, backupDir, entries, []string{filepath.Join(".pc", "pc.db"), "figures"}); err != nil {
			t.Fatalf("CompleteReplacement() error = %v", err)
		}
		assertReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "new-db")
		assertReplacementTestFile(t, filepath.Join(root, "figures", "new-record", "new.png"), "new-figure")
		assertReplacementTestFile(t, filepath.Join(backupDir, ".pc", "pc.db"), "old-db")
		assertReplacementTestFile(t, filepath.Join(backupDir, ".pc", "last_sync"), "sync")
		if _, err := os.Stat(filepath.Join(root, ".pc", "last_sync")); !os.IsNotExist(err) {
			t.Fatalf("last_sync should be removed during roll-forward, stat err=%v", err)
		}
	})

	t.Run("keeps already promoted entry that did not have an old backup", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		backupDir := filepath.Join(t.TempDir(), "payload-backup")
		writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "new-db")
		writeReplacementTestFile(t, filepath.Join(root, "figures", "new-record", "new.png"), "new-figure")
		writeReplacementTestFile(t, filepath.Join(backupDir, ".pc", "pc.db"), "old-db")

		if err := CompleteReplacement(root, staging, backupDir, entries, []string{filepath.Join(".pc", "pc.db"), "figures"}); err != nil {
			t.Fatalf("CompleteReplacement() error = %v", err)
		}
		assertReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "new-db")
		assertReplacementTestFile(t, filepath.Join(root, "figures", "new-record", "new.png"), "new-figure")
	})

	t.Run("replaces conflicting live entry when backup already exists", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		backupDir := filepath.Join(t.TempDir(), "payload-backup")
		writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "old-restored-db")
		writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
		writeReplacementTestFile(t, filepath.Join(backupDir, ".pc", "pc.db"), "old-backup-db")

		if err := CompleteReplacement(root, staging, backupDir, entries, []string{filepath.Join(".pc", "pc.db")}); err != nil {
			t.Fatalf("CompleteReplacement() error = %v", err)
		}
		assertReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "new-db")
		assertReplacementTestFile(t, filepath.Join(backupDir, ".pc", "pc.db"), "old-backup-db")
	})

	t.Run("removes deleted live entry when backup exists", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		backupDir := filepath.Join(t.TempDir(), "payload-backup")
		entry := filepath.Join(".pc", "last_sync")
		writeReplacementTestFile(t, filepath.Join(root, entry), "stale-sync")
		writeReplacementTestFile(t, filepath.Join(backupDir, entry), "old-sync")

		if err := CompleteReplacement(root, staging, backupDir, []string{entry}, nil); err != nil {
			t.Fatalf("CompleteReplacement() error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, entry)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("deleted live entry should be removed during roll-forward, stat err=%v", err)
		}
		assertReplacementTestFile(t, filepath.Join(backupDir, entry), "old-sync")
	})

	t.Run("syncs already promoted entry parents before completion", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		backupDir := filepath.Join(t.TempDir(), "payload-backup")
		nestedEntries := []string{filepath.Join(".pc", "pc.db"), filepath.Join("figures", "new-record", "new.png")}
		writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "new-db")
		writeReplacementTestFile(t, filepath.Join(root, "figures", "new-record", "new.png"), "new-figure")
		writeReplacementTestFile(t, filepath.Join(backupDir, ".pc", "pc.db"), "old-db")
		synced := map[string]bool{}
		origSync := syncDirFn
		syncDirFn = func(dir string) error {
			synced[filepath.Clean(dir)] = true
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })

		if err := CompleteReplacement(root, staging, backupDir, nestedEntries, nestedEntries); err != nil {
			t.Fatalf("CompleteReplacement() error = %v", err)
		}
		for _, dir := range []string{
			filepath.Join(root, ".pc"),
			filepath.Join(backupDir, ".pc"),
			filepath.Join(staging, ".pc"),
			filepath.Join(root, "figures", "new-record"),
			filepath.Join(staging, "figures", "new-record"),
		} {
			if !synced[filepath.Clean(dir)] {
				t.Fatalf("CompleteReplacement did not sync already-promoted parent %s; synced=%v", dir, synced)
			}
		}
	})
}

func TestCompleteReplacementErrorPaths(t *testing.T) {
	entries := []string{filepath.Join(".pc", "pc.db")}

	t.Run("staged stat failure", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		if err := os.WriteFile(filepath.Join(staging, ".pc"), []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("write staging blocker: %v", err)
		}
		err := CompleteReplacement(root, staging, t.TempDir(), entries, entries)
		if err == nil || !strings.Contains(err.Error(), "inspect staged") {
			t.Fatalf("CompleteReplacement() error = %v, want staged inspect failure", err)
		}
	})

	t.Run("backup rename failure", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "old-db")
		writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
		origRename := renameFileFn
		renameFileFn = func(string, string) error {
			return errors.New("rename failed")
		}
		t.Cleanup(func() { renameFileFn = origRename })

		err := CompleteReplacement(root, staging, t.TempDir(), entries, entries)
		if err == nil || !strings.Contains(err.Error(), "backup existing") {
			t.Fatalf("CompleteReplacement() error = %v, want backup failure", err)
		}
	})

	t.Run("existing target stat failure", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
		if err := os.WriteFile(filepath.Join(root, ".pc"), []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("write root blocker: %v", err)
		}

		err := CompleteReplacement(root, staging, t.TempDir(), entries, entries)
		if err == nil || !strings.Contains(err.Error(), "inspect existing") {
			t.Fatalf("CompleteReplacement() error = %v, want existing inspect failure", err)
		}
	})

	t.Run("backup stat failure", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		backupDir := t.TempDir()
		writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "old-db")
		writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
		if err := os.WriteFile(filepath.Join(backupDir, ".pc"), []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("write backup blocker: %v", err)
		}

		err := CompleteReplacement(root, staging, backupDir, entries, entries)
		if err == nil || !strings.Contains(err.Error(), "inspect backup") {
			t.Fatalf("CompleteReplacement() error = %v, want backup inspect failure", err)
		}
	})

	t.Run("promote rename failure", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
		origRename := renameFileFn
		renameFileFn = func(string, string) error {
			return errors.New("rename failed")
		}
		t.Cleanup(func() { renameFileFn = origRename })

		err := CompleteReplacement(root, staging, t.TempDir(), entries, entries)
		if err == nil || !strings.Contains(err.Error(), "promote staged") {
			t.Fatalf("CompleteReplacement() error = %v, want promote failure", err)
		}
	})

	t.Run("sync failure", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
		origSync := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(root) {
				return errors.New("sync failed")
			}
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })

		err := CompleteReplacement(root, staging, t.TempDir(), entries, entries)
		if err == nil || !strings.Contains(err.Error(), "create target parent") {
			t.Fatalf("CompleteReplacement() error = %v, want sync failure", err)
		}
	})
}

func TestRestoreReplacementBackupRestoresOldRestorePayload(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "payload-backup")
	entries := []string{
		filepath.Join(".pc", "pc.db"),
		filepath.Join(".pc", "last_sync"),
		"figures",
	}
	writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "new-db")
	writeReplacementTestFile(t, filepath.Join(root, "figures", "new-record", "new.png"), "new-figure")
	writeReplacementTestFile(t, filepath.Join(backupDir, ".pc", "pc.db"), "old-db")
	writeReplacementTestFile(t, filepath.Join(backupDir, ".pc", "last_sync"), "sync")

	if err := RestoreReplacementBackup(root, backupDir, entries, []string{
		filepath.Join(".pc", "pc.db"),
		filepath.Join(".pc", "last_sync"),
	}); err != nil {
		t.Fatalf("RestoreReplacementBackup() error = %v", err)
	}
	assertReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "old-db")
	assertReplacementTestFile(t, filepath.Join(root, ".pc", "last_sync"), "sync")
	if _, err := os.Stat(filepath.Join(root, "figures")); !os.IsNotExist(err) {
		t.Fatalf("figures should be removed during rollback, stat err=%v", err)
	}
}

func TestRestoreReplacementBackupErrorPaths(t *testing.T) {
	entries := []string{filepath.Join(".pc", "pc.db")}

	t.Run("original entries validation failure", func(t *testing.T) {
		err := RestoreReplacementBackup(t.TempDir(), t.TempDir(), entries, []string{"../outside"})
		if err == nil || !strings.Contains(err.Error(), "replacement entry must be a relative path") {
			t.Fatalf("RestoreReplacementBackup() error = %v, want original entry validation failure", err)
		}
	})

	t.Run("backup stat failure", func(t *testing.T) {
		backupDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(backupDir, ".pc"), []byte("not a directory"), 0o644); err != nil {
			t.Fatalf("write backup blocker: %v", err)
		}
		err := RestoreReplacementBackup(t.TempDir(), backupDir, entries, nil)
		if err == nil || !strings.Contains(err.Error(), "inspect backup") {
			t.Fatalf("RestoreReplacementBackup() error = %v, want backup inspect failure", err)
		}
	})

	t.Run("restore rename failure", func(t *testing.T) {
		root := t.TempDir()
		backupDir := t.TempDir()
		writeReplacementTestFile(t, filepath.Join(backupDir, ".pc", "pc.db"), "old-db")
		origRename := renameFileFn
		renameFileFn = func(string, string) error {
			return errors.New("rename failed")
		}
		t.Cleanup(func() { renameFileFn = origRename })

		err := RestoreReplacementBackup(root, backupDir, entries, entries)
		if err == nil || !strings.Contains(err.Error(), "restore .pc") {
			t.Fatalf("RestoreReplacementBackup() error = %v, want restore rename failure", err)
		}
	})

	t.Run("missing backup removes promoted entry", func(t *testing.T) {
		root := t.TempDir()
		writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "new-db")

		if err := RestoreReplacementBackup(root, t.TempDir(), entries, nil); err != nil {
			t.Fatalf("RestoreReplacementBackup() error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, ".pc", "pc.db")); !os.IsNotExist(err) {
			t.Fatalf("promoted db should be removed when backup is absent, stat err=%v", err)
		}
	})

	t.Run("missing backup preserves original entry", func(t *testing.T) {
		root := t.TempDir()
		writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "old-db")

		if err := RestoreReplacementBackup(root, t.TempDir(), entries, entries); err != nil {
			t.Fatalf("RestoreReplacementBackup() error = %v", err)
		}
		assertReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "old-db")
	})

	t.Run("sync failure", func(t *testing.T) {
		root := t.TempDir()
		writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "new-db")
		origSync := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(root) {
				return errors.New("sync failed")
			}
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })

		err := RestoreReplacementBackup(root, t.TempDir(), entries, nil)
		if err == nil || !strings.Contains(err.Error(), "sync export root") {
			t.Fatalf("RestoreReplacementBackup() error = %v, want sync failure", err)
		}
	})
}

func TestReplacementParentDurabilityErrorPaths(t *testing.T) {
	t.Run("inspect parent failure", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "blocker")
		if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
			t.Fatalf("write blocker: %v", err)
		}
		if err := ensureReplacementParent(filepath.Join(blocker, "child", "entry")); err == nil {
			t.Fatal("expected ensureReplacementParent to fail when parent inspection hits a file")
		}
	})

	t.Run("sync newly-created ancestor failure", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "a", "b", "entry")
		origSync := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(root) {
				return errors.New("sync failed")
			}
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })
		err := ensureReplacementParent(target)
		if err == nil || !strings.Contains(err.Error(), "sync failed") {
			t.Fatalf("ensureReplacementParent() error = %v, want sync failure", err)
		}
	})
}

func TestSyncReplacementDirsSkipsMissingNestedDirs(t *testing.T) {
	root := t.TempDir()
	touched := map[string]struct{}{
		filepath.Join(root, "already-removed"): {},
	}
	if err := syncReplacementDirs(root, touched); err != nil {
		t.Fatalf("syncReplacementDirs() error = %v", err)
	}
}

func TestReplaceContentsProvidedBackupSyncFailures(t *testing.T) {
	root := t.TempDir()
	staging := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "payload-backup")
	writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
	entries := []string{filepath.Join(".pc", "pc.db")}

	t.Run("initial backup parent sync failure", func(t *testing.T) {
		origSync := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(filepath.Dir(backupDir)) {
				return errors.New("sync failed")
			}
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })

		err := ReplaceContents(root, staging, ReplacementOptions{Entries: entries, BackupDir: backupDir})
		if err == nil || !strings.Contains(err.Error(), "sync replacement backup parent") {
			t.Fatalf("ReplaceContents() error = %v, want backup parent sync failure", err)
		}
	})

	t.Run("post-removal backup parent sync failure", func(t *testing.T) {
		root := t.TempDir()
		staging := t.TempDir()
		backupDir := filepath.Join(t.TempDir(), "payload-backup")
		writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
		origSync := syncDirFn
		parentSyncs := 0
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(filepath.Dir(backupDir)) {
				parentSyncs++
				if parentSyncs == 2 {
					return errors.New("sync failed")
				}
			}
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })

		err := ReplaceContents(root, staging, ReplacementOptions{Entries: entries, BackupDir: backupDir})
		if err == nil || !strings.Contains(err.Error(), "sync backup parent after backup removal") {
			t.Fatalf("ReplaceContents() error = %v, want post-removal sync failure", err)
		}
		var committedCleanupErr *CommittedReplacementCleanupError
		if !errors.As(err, &committedCleanupErr) {
			t.Fatalf("ReplaceContents() error type = %T, want CommittedReplacementCleanupError", err)
		}
	})

	t.Run("syncs each created backup ancestor", func(t *testing.T) {
		root := t.TempDir()
		backupDir := filepath.Join(root, ".pc", "backups", "restore-db-test", ".restore-payload")
		synced := map[string]bool{}
		origSync := syncDirFn
		syncDirFn = func(dir string) error {
			synced[filepath.Clean(dir)] = true
			return origSync(dir)
		}
		t.Cleanup(func() { syncDirFn = origSync })

		if _, err := createReplacementBackupDir(root, backupDir); err != nil {
			t.Fatalf("createReplacementBackupDir() error = %v", err)
		}
		for _, dir := range []string{
			root,
			filepath.Join(root, ".pc"),
			filepath.Join(root, ".pc", "backups"),
			filepath.Join(root, ".pc", "backups", "restore-db-test"),
		} {
			if !synced[filepath.Clean(dir)] {
				t.Fatalf("createReplacementBackupDir did not sync created ancestor parent %s; synced=%v", dir, synced)
			}
		}
	})
}

func TestReplaceContentsCallsHookBeforeRollback(t *testing.T) {
	root := t.TempDir()
	staging := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "payload-backup")
	entries := []string{filepath.Join(".pc", "pc.db")}
	writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "old-db")
	writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
	origSync := syncDirFn
	syncDirFn = func(dir string) error {
		if filepath.Clean(dir) == filepath.Clean(root) {
			return errors.New("sync failed")
		}
		return origSync(dir)
	}
	t.Cleanup(func() { syncDirFn = origSync })

	hookCalled := false
	err := ReplaceContents(root, staging, ReplacementOptions{
		Entries:   entries,
		BackupDir: backupDir,
		BeforeRollback: func() error {
			hookCalled = true
			assertReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "new-db")
			assertReplacementTestFile(t, filepath.Join(backupDir, ".pc", "pc.db"), "old-db")
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "sync export root") {
		t.Fatalf("ReplaceContents() error = %v, want sync failure", err)
	}
	if !hookCalled {
		t.Fatal("BeforeRollback hook was not called")
	}
	assertReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "old-db")
}

func TestReplaceContentsRetainsInterruptedStateWhenRollbackHookFails(t *testing.T) {
	root := t.TempDir()
	staging := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "payload-backup")
	entries := []string{filepath.Join(".pc", "pc.db")}
	writeReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "old-db")
	writeReplacementTestFile(t, filepath.Join(staging, ".pc", "pc.db"), "new-db")
	origSync := syncDirFn
	syncDirFn = func(dir string) error {
		if filepath.Clean(dir) == filepath.Clean(root) {
			return errors.New("sync failed")
		}
		return origSync(dir)
	}
	t.Cleanup(func() { syncDirFn = origSync })

	err := ReplaceContents(root, staging, ReplacementOptions{
		Entries:   entries,
		BackupDir: backupDir,
		BeforeRollback: func() error {
			return errors.New("marker write failed")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "prepare replacement rollback") {
		t.Fatalf("ReplaceContents() error = %v, want rollback hook failure", err)
	}
	assertReplacementTestFile(t, filepath.Join(root, ".pc", "pc.db"), "new-db")
	assertReplacementTestFile(t, filepath.Join(backupDir, ".pc", "pc.db"), "old-db")
	if _, err := os.Stat(filepath.Join(staging, ".pc", "pc.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged db exists after promotion = %v, want missing", err)
	}
}

func TestReplacementEntryValidation(t *testing.T) {
	for _, entries := range [][]string{
		nil,
		{"/absolute"},
		{"../outside"},
	} {
		if err := ReplaceContents(t.TempDir(), t.TempDir(), ReplacementOptions{Entries: entries}); err == nil {
			t.Fatalf("ReplaceContents(%v) succeeded, want validation error", entries)
		}
	}
	if err := CompleteReplacement(t.TempDir(), t.TempDir(), t.TempDir(), nil, nil); err == nil {
		t.Fatal("expected CompleteReplacement with no entries to fail")
	}
	if err := RestoreReplacementBackup(t.TempDir(), t.TempDir(), nil, nil); err == nil {
		t.Fatal("expected RestoreReplacementBackup with no entries to fail")
	}

	root := t.TempDir()
	staging := t.TempDir()
	writeReplacementTestFile(t, filepath.Join(staging, "entry"), "value")
	if err := ReplaceContents(root, staging, ReplacementOptions{Entries: []string{"entry", "entry"}}); err != nil {
		t.Fatalf("ReplaceContents() with duplicate entries error = %v", err)
	}
	assertReplacementTestFile(t, filepath.Join(root, "entry"), "value")
}

func TestSnapshotReplacementSyncsExportRoot(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	oldSnapshot := Snapshot{
		Templates: []Template{{Name: "old", HTMLContent: "<html>old</html>"}},
		Projects:  []RegistryEntry{{ID: "phase7/old", CreatedAt: now, UpdatedAt: now}},
		Devices:   []RegistryEntry{{ID: "device/old", CreatedAt: now, UpdatedAt: now}},
	}
	newSnapshot := Snapshot{
		Templates: []Template{{Name: "new", HTMLContent: "<html>new</html>"}},
		Projects:  []RegistryEntry{{ID: "phase7/new", CreatedAt: now, UpdatedAt: now}},
		Devices:   []RegistryEntry{{ID: "device/new", CreatedAt: now, UpdatedAt: now}},
	}

	t.Run("write fsyncs the export root after promoting entries", func(t *testing.T) {
		root := t.TempDir()

		var syncedRoot bool
		origSyncDir := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(root) {
				syncedRoot = true
			}
			return origSyncDir(dir)
		}
		t.Cleanup(func() { syncDirFn = origSyncDir })

		if err := Write(root, newSnapshot); err != nil {
			t.Fatalf("Write(newSnapshot): %v", err)
		}
		if !syncedRoot {
			t.Fatal("Write did not fsync the export root after promoting managed entries")
		}
	})

	t.Run("write fsyncs each managed container directory during staging", func(t *testing.T) {
		root := t.TempDir()
		// Include a chat so chats/ is created and its container fsync is exercised;
		// chats/ is created lazily and is otherwise correctly skipped.
		snapshotWithChat := newSnapshot
		snapshotWithChat.Chats = []ChatSession{{
			ID:              "20260309-aaaabbbb",
			Source:          "codex",
			SourceSessionID: "session-sync",
			SourceDeviceID:  "device/unit",
			StartedAt:       now,
			LastActivityAt:  now,
			CreatedAt:       now,
			UpdatedAt:       now,
		}}

		stagedSyncs := map[string]bool{}
		origSyncDir := syncDirFn
		syncDirFn = func(dir string) error {
			if strings.Contains(dir, ".snapshot-staging-") {
				stagedSyncs[filepath.Base(dir)] = true
			}
			return origSyncDir(dir)
		}
		t.Cleanup(func() { syncDirFn = origSyncDir })

		if err := Write(root, snapshotWithChat); err != nil {
			t.Fatalf("Write(snapshotWithChat): %v", err)
		}
		for _, name := range []string{"templates", "records", "chats"} {
			if !stagedSyncs[name] {
				t.Fatalf("Write did not fsync staged %s/ directory before promotion", name)
			}
		}
	})

	t.Run("write skips the chats container fsync when no chats are staged", func(t *testing.T) {
		root := t.TempDir()

		var chatsSynced bool
		origSyncDir := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Base(dir) == "chats" && strings.Contains(dir, ".snapshot-staging-") {
				chatsSynced = true
			}
			return origSyncDir(dir)
		}
		t.Cleanup(func() { syncDirFn = origSyncDir })

		// newSnapshot has no chats, so chats/ is never created and must be skipped.
		if err := Write(root, newSnapshot); err != nil {
			t.Fatalf("Write(newSnapshot): %v", err)
		}
		if chatsSynced {
			t.Fatal("Write fsynced a chats/ directory that was never created")
		}
	})

	t.Run("staged container sync failure rolls back to the prior snapshot", func(t *testing.T) {
		root := t.TempDir()
		if err := Write(root, oldSnapshot); err != nil {
			t.Fatalf("Write(oldSnapshot): %v", err)
		}
		oldManifest, err := Manifest(root)
		if err != nil {
			t.Fatalf("Manifest(old): %v", err)
		}

		origSyncDir := syncDirFn
		syncDirFn = func(dir string) error {
			// Fail the staged records/ container flush, before any promotion, so the
			// staged write is aborted and the prior snapshot is left intact.
			if filepath.Base(dir) == "records" && strings.Contains(dir, ".snapshot-staging-") {
				return errors.New("sync records failed")
			}
			return origSyncDir(dir)
		}
		t.Cleanup(func() { syncDirFn = origSyncDir })

		err = Write(root, newSnapshot)
		if err == nil || !strings.Contains(err.Error(), "sync staged records") {
			t.Fatalf("Write(newSnapshot) error = %v, want staged records sync failure", err)
		}

		syncDirFn = origSyncDir
		currentManifest, err := Manifest(root)
		if err != nil {
			t.Fatalf("Manifest(current): %v", err)
		}
		if !reflect.DeepEqual(currentManifest, oldManifest) {
			t.Fatalf("snapshot changed after staged container sync failure\nold=%v\ncurrent=%v", oldManifest, currentManifest)
		}
	})

	t.Run("export-root sync failure rolls back to the prior snapshot", func(t *testing.T) {
		root := t.TempDir()
		if err := Write(root, oldSnapshot); err != nil {
			t.Fatalf("Write(oldSnapshot): %v", err)
		}
		oldManifest, err := Manifest(root)
		if err != nil {
			t.Fatalf("Manifest(old): %v", err)
		}

		origSyncDir := syncDirFn
		rootSyncs := 0
		syncDirFn = func(dir string) error {
			// Fail only the FIRST export-root flush — the commit-point sync after
			// every managed entry was promoted. The rollback's own export-root sync
			// must succeed so the restore is durable and the backup dir is cleaned
			// up, proving the commit-point failure rolls back fully.
			if filepath.Clean(dir) == filepath.Clean(root) {
				rootSyncs++
				if rootSyncs == 1 {
					return errors.New("sync root failed")
				}
			}
			return origSyncDir(dir)
		}
		t.Cleanup(func() { syncDirFn = origSyncDir })

		err = Write(root, newSnapshot)
		if err == nil || !strings.Contains(err.Error(), "sync export root") {
			t.Fatalf("Write(newSnapshot) error = %v, want export-root sync failure", err)
		}
		if rootSyncs < 2 {
			t.Fatalf("rollback did not fsync the export root after restoring; rootSyncs=%d", rootSyncs)
		}

		currentManifest, err := Manifest(root)
		if err != nil {
			t.Fatalf("Manifest(current): %v", err)
		}
		if !reflect.DeepEqual(currentManifest, oldManifest) {
			t.Fatalf("snapshot changed after export-root sync failure\nold=%v\ncurrent=%v", oldManifest, currentManifest)
		}
	})

	t.Run("restore export-root sync failure preserves the backup directory", func(t *testing.T) {
		root := t.TempDir()
		stagingRoot, err := os.MkdirTemp(root, ".snapshot-staging-*")
		if err != nil {
			t.Fatalf("create staging root: %v", err)
		}
		if err := writeSnapshotContents(root, oldSnapshot); err != nil {
			t.Fatalf("writeSnapshotContents(old): %v", err)
		}
		if err := writeSnapshotContents(stagingRoot, newSnapshot); err != nil {
			t.Fatalf("writeSnapshotContents(new): %v", err)
		}

		// Force a promote failure so the restore path runs, and make every
		// export-root fsync fail so the restore's own durability flush also fails.
		// The combined failure must be reported and the backup dir preserved for
		// manual recovery.
		origRename := renameFileFn
		renameFileFn = func(from string, to string) error {
			fromDir := filepath.Base(filepath.Dir(from))
			if filepath.Base(from) == "devices.json" && strings.HasPrefix(fromDir, ".snapshot-staging-") {
				return errors.New("promote failed")
			}
			return origRename(from, to)
		}
		origSyncDir := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(root) {
				return errors.New("sync root failed")
			}
			return origSyncDir(dir)
		}
		t.Cleanup(func() {
			renameFileFn = origRename
			syncDirFn = origSyncDir
		})

		err = replaceSnapshotContents(root, stagingRoot)
		if err == nil || !strings.Contains(err.Error(), "failed to restore previous snapshot state") {
			t.Fatalf("replaceSnapshotContents() error = %v, want restore failure detail", err)
		}
		if !strings.Contains(err.Error(), "sync export root") {
			t.Fatalf("replaceSnapshotContents() error = %v, want restore export-root sync failure", err)
		}

		// Restore failed, so the backup dir must NOT have been cleaned up.
		syncDirFn = origSyncDir
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read root: %v", err)
		}
		var backupFound bool
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".snapshot-backup-") {
				backupFound = true
			}
		}
		if !backupFound {
			t.Fatal("backup directory was removed despite a restore failure; original data is unrecoverable")
		}
	})

	t.Run("writeFile fsyncs the parent directory after renaming the staged file", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "projects.json")

		origSyncDir := syncDirFn
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(root) {
				return errors.New("sync parent failed")
			}
			return origSyncDir(dir)
		}
		t.Cleanup(func() { syncDirFn = origSyncDir })

		err := writeFile(path, []byte("contents"))
		if err == nil || !strings.Contains(err.Error(), "sync parent failed") {
			t.Fatalf("writeFile() error = %v, want parent-directory sync failure", err)
		}
	})

	t.Run("successful write removes backup dir and fsyncs root a second time", func(t *testing.T) {
		root := t.TempDir()
		if err := Write(root, oldSnapshot); err != nil {
			t.Fatalf("Write(oldSnapshot): %v", err)
		}

		origSyncDir := syncDirFn
		rootSyncs := 0
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(root) {
				rootSyncs++
			}
			return origSyncDir(dir)
		}
		t.Cleanup(func() { syncDirFn = origSyncDir })

		if err := Write(root, newSnapshot); err != nil {
			t.Fatalf("Write(newSnapshot): %v", err)
		}
		// Expect at least two root fsyncs: one at the commit point and one after
		// the backup directory is removed, making the deletion durable.
		if rootSyncs < 2 {
			t.Fatalf("Write synced the export root %d time(s); want at least 2 (commit + post-backup-removal)", rootSyncs)
		}
		// Backup dir must be gone so no stale entries remain.
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("ReadDir(root): %v", err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".snapshot-backup-") {
				t.Fatalf("backup directory %q was not removed after a successful write", e.Name())
			}
		}
	})

	t.Run("second root fsync failure after backup removal propagates error", func(t *testing.T) {
		root := t.TempDir()
		if err := Write(root, oldSnapshot); err != nil {
			t.Fatalf("Write(oldSnapshot): %v", err)
		}

		origSyncDir := syncDirFn
		rootSyncs := 0
		syncDirFn = func(dir string) error {
			if filepath.Clean(dir) == filepath.Clean(root) {
				rootSyncs++
				// Fail only the second root sync (post-backup-removal).
				if rootSyncs == 2 {
					return errors.New("sync root after backup removal failed")
				}
			}
			return origSyncDir(dir)
		}
		t.Cleanup(func() { syncDirFn = origSyncDir })

		err := Write(root, newSnapshot)
		if err == nil || !strings.Contains(err.Error(), "sync export root after backup removal") {
			t.Fatalf("Write(newSnapshot) error = %v, want post-backup-removal sync failure", err)
		}
		// Despite the second fsync failure, the snapshot contents must be correct
		// (the promotion already succeeded and the first sync committed it).
		syncDirFn = origSyncDir
		got, rerr := Read(root)
		if rerr != nil {
			t.Fatalf("Read(root) after second-fsync failure: %v", rerr)
		}
		if !reflect.DeepEqual(got.Templates, newSnapshot.Templates) ||
			!reflect.DeepEqual(got.Projects, newSnapshot.Projects) ||
			!reflect.DeepEqual(got.Devices, newSnapshot.Devices) {
			t.Fatalf("snapshot contents after second-fsync failure:\ngot=%#v\nwant promoted snapshot=%#v", got, newSnapshot)
		}
	})
}

func TestSyncDirDurabilityHelper(t *testing.T) {
	t.Run("returns nil for a real directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := syncDir(dir); err != nil {
			t.Fatalf("syncDir(%q) = %v, want nil", dir, err)
		}
	})

	t.Run("propagates open failure for a missing directory", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		if err := syncDir(missing); err == nil {
			t.Fatalf("syncDir(%q) = nil, want open failure", missing)
		}
	})

	t.Run("propagates fsync failure", func(t *testing.T) {
		// Open then close a real directory handle so its Sync returns a genuine
		// "file already closed" error, exercising the durability-flush failure
		// path that a directory fsync would hit on a read-only or failing volume.
		dir := t.TempDir()
		closed, err := os.Open(dir)
		if err != nil {
			t.Fatalf("open dir: %v", err)
		}
		if err := closed.Close(); err != nil {
			t.Fatalf("close dir: %v", err)
		}

		origOpenDir := openDirFn
		openDirFn = func(string) (syncableDir, error) { return closed, nil }
		t.Cleanup(func() { openDirFn = origOpenDir })

		err = syncDir(dir)
		if err == nil {
			t.Fatalf("syncDir(%q) = nil, want fsync failure", dir)
		}
		// The Sync error must be surfaced verbatim, not masked by a later Close
		// error or swallowed.
		if !strings.Contains(err.Error(), "file already closed") {
			t.Fatalf("syncDir(%q) = %v, want the propagated fsync error", dir, err)
		}
	})
}

func TestReadRejectsTemplateDirectoryProblems(t *testing.T) {
	if _, err := Read(" "); err == nil {
		t.Fatal("expected Read to reject an empty root")
	}

	if _, err := readTemplates(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected readTemplates to fail for a missing directory")
	}

	t.Run("rejects nested directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "templates")
		if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
			t.Fatalf("mkdir nested dir: %v", err)
		}
		if _, err := readTemplates(dir); err == nil || !strings.Contains(err.Error(), "unexpected directory") {
			t.Fatalf("readTemplates error = %v, want unexpected directory", err)
		}
	})

	t.Run("rejects non-html template file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "templates")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir templates dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "text-only.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write bad template file: %v", err)
		}
		if _, err := readTemplates(dir); err == nil || !strings.Contains(err.Error(), ".html") {
			t.Fatalf("readTemplates error = %v, want .html failure", err)
		}
	})

	t.Run("rejects invalid template path segment", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "templates")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir templates dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "..html"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write invalid template file: %v", err)
		}
		if _, err := readTemplates(dir); err == nil || !strings.Contains(err.Error(), "template name") {
			t.Fatalf("readTemplates error = %v, want template name validation failure", err)
		}
	})

	t.Run("rejects unreadable template target", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "templates")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir templates dir: %v", err)
		}
		if err := os.Symlink(filepath.Join(dir, "missing-target.html"), filepath.Join(dir, "broken.html")); err != nil {
			t.Fatalf("create broken template symlink: %v", err)
		}
		if _, err := readTemplates(dir); err == nil || !strings.Contains(err.Error(), "read template broken.html") {
			t.Fatalf("readTemplates error = %v, want read failure for broken.html", err)
		}
	})
}

func TestReadRecordsRejectsUnexpectedFile(t *testing.T) {
	t.Run("missing records dir", func(t *testing.T) {
		if _, err := readRecords(filepath.Join(t.TempDir(), "missing")); err == nil || !strings.Contains(err.Error(), "read records dir") {
			t.Fatalf("readRecords error = %v, want missing dir failure", err)
		}
	})

	t.Run("unexpected file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "records")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir records dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "not-a-record.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write unexpected record file: %v", err)
		}
		if _, err := readRecords(dir); err == nil || !strings.Contains(err.Error(), "unexpected file in records export") {
			t.Fatalf("readRecords error = %v, want unexpected file failure", err)
		}
	})
}

func TestReadRecordRejectsMetadataAndNotesProblems(t *testing.T) {
	t.Run("invalid record id", func(t *testing.T) {
		_, recordDir := writeSnapshotFixture(t)
		if _, err := readRecord(recordDir, "bad/id"); err == nil || !strings.Contains(err.Error(), "record id") {
			t.Fatalf("readRecord error = %v, want record id validation failure", err)
		}
	})

	t.Run("missing metadata file", func(t *testing.T) {
		_, recordDir := writeSnapshotFixture(t)
		if err := os.Remove(filepath.Join(recordDir, "metadata.json")); err != nil {
			t.Fatalf("remove metadata.json: %v", err)
		}
		if _, err := readRecord(recordDir, filepath.Base(recordDir)); err == nil || !strings.Contains(err.Error(), "read metadata") {
			t.Fatalf("readRecord error = %v, want metadata read failure", err)
		}
	})

	t.Run("malformed metadata json", func(t *testing.T) {
		_, recordDir := writeSnapshotFixture(t)
		mustWriteFileSnapshot(t, filepath.Join(recordDir, "metadata.json"), "{not-json}\n")
		if _, err := readRecord(recordDir, filepath.Base(recordDir)); err == nil || !strings.Contains(err.Error(), "parse metadata") {
			t.Fatalf("readRecord error = %v, want metadata parse failure", err)
		}
	})

	t.Run("unsupported format version", func(t *testing.T) {
		root, recordDir := writeSnapshotFixture(t)
		writeMetadataJSON(t, recordDir, map[string]any{
			"format_version": 2,
			"id":             "20260309-aaaabbbb",
			"date":           "2026-03-09",
			"day_order":      "a0",
			"has_notes":      true,
			"figures":        []map[string]any{},
			"data_files":     []map[string]any{},
			"created_at":     "2026-03-09T12:00:00Z",
			"updated_at":     "2026-03-09T12:00:00Z",
		})
		_, err := readRecord(recordDir, filepath.Base(recordDir))
		if err == nil || !strings.Contains(err.Error(), "unsupported format_version") {
			t.Fatalf("readRecord(%s) error = %v", root, err)
		}
	})

	t.Run("metadata id mismatch", func(t *testing.T) {
		_, recordDir := writeSnapshotFixture(t)
		writeMetadataJSON(t, recordDir, map[string]any{
			"format_version": 1,
			"id":             "20260309-ccccdddd",
			"date":           "2026-03-09",
			"day_order":      "a0",
			"has_notes":      true,
			"figures":        []map[string]any{},
			"data_files":     []map[string]any{},
			"created_at":     "2026-03-09T12:00:00Z",
			"updated_at":     "2026-03-09T12:00:00Z",
		})
		_, err := readRecord(recordDir, filepath.Base(recordDir))
		if err == nil || !strings.Contains(err.Error(), "does not match record dir") {
			t.Fatalf("readRecord error = %v, want metadata id mismatch", err)
		}
	})

	t.Run("missing required project and device metadata", func(t *testing.T) {
		_, recordDir := writeSnapshotFixture(t)
		baseMetadata := `{
  "format_version": 1,
  "id": "20260309-aaaabbbb",
  "date": "2026-03-09",
  "day_order": "a0",
  "has_notes": false,
  "figures": [],
  "data_files": [],
  "created_at": "2026-03-09T12:00:00Z",
  "updated_at": "2026-03-09T12:00:00Z"
}`
		mustWriteFileSnapshot(t, filepath.Join(recordDir, "metadata.json"), baseMetadata)
		if _, err := readRecord(recordDir, filepath.Base(recordDir)); err == nil || !strings.Contains(err.Error(), "project_id is required") {
			t.Fatalf("readRecord error = %v, want missing project_id", err)
		}
		withProject := strings.Replace(baseMetadata, `"day_order": "a0",`, `"day_order": "a0",`+"\n  "+`"project_id": "phase7/unit",`, 1)
		mustWriteFileSnapshot(t, filepath.Join(recordDir, "metadata.json"), withProject)
		if _, err := readRecord(recordDir, filepath.Base(recordDir)); err == nil || !strings.Contains(err.Error(), "source_device_id is required") {
			t.Fatalf("readRecord error = %v, want missing source_device_id", err)
		}
	})

	t.Run("invalid timestamps", func(t *testing.T) {
		_, recordDir := writeSnapshotFixture(t)
		writeMetadataJSON(t, recordDir, map[string]any{
			"format_version": 1,
			"id":             "20260309-aaaabbbb",
			"date":           "2026-03-09",
			"day_order":      "a0",
			"has_notes":      true,
			"figures": []map[string]any{{
				"filename": "plot.png",
				"s3_key":   "figures/20260309-aaaabbbb/plot.png",
				"alt_text": nil,
			}},
			"data_files": []map[string]any{},
			"created_at": "not-a-time",
			"updated_at": "still-not-a-time",
		})
		_, err := readRecord(recordDir, filepath.Base(recordDir))
		if err == nil || !strings.Contains(err.Error(), "parse created_at") {
			t.Fatalf("readRecord error = %v, want created_at parse failure", err)
		}
	})

	t.Run("invalid updated_at", func(t *testing.T) {
		_, recordDir := writeSnapshotFixture(t)
		writeMetadataJSON(t, recordDir, map[string]any{
			"format_version": 1,
			"id":             "20260309-aaaabbbb",
			"date":           "2026-03-09",
			"day_order":      "a0",
			"has_notes":      true,
			"figures": []map[string]any{{
				"filename": "plot.png",
				"s3_key":   "figures/20260309-aaaabbbb/plot.png",
				"alt_text": nil,
			}},
			"data_files": []map[string]any{},
			"created_at": "2026-03-09T12:00:00Z",
			"updated_at": "not-a-time",
		})
		if _, err := readRecord(recordDir, filepath.Base(recordDir)); err == nil || !strings.Contains(err.Error(), "parse updated_at") {
			t.Fatalf("readRecord error = %v, want updated_at parse failure", err)
		}
	})

	t.Run("missing record html is null", func(t *testing.T) {
		_, recordDir := writeSnapshotFixture(t)
		if err := os.Remove(filepath.Join(recordDir, "record.html")); err != nil {
			t.Fatalf("remove record.html: %v", err)
		}
		record, err := readRecord(recordDir, filepath.Base(recordDir))
		if err != nil {
			t.Fatalf("readRecord error = %v", err)
		}
		if record.HTMLContent != nil {
			t.Fatalf("HTMLContent = %q, want nil", *record.HTMLContent)
		}
	})

	t.Run("record html read error", func(t *testing.T) {
		_, recordDir := writeSnapshotFixture(t)
		if err := os.Remove(filepath.Join(recordDir, "record.html")); err != nil {
			t.Fatalf("remove record.html: %v", err)
		}
		if err := os.Mkdir(filepath.Join(recordDir, "record.html"), 0o755); err != nil {
			t.Fatalf("mkdir record.html: %v", err)
		}
		if _, err := readRecord(recordDir, filepath.Base(recordDir)); err == nil || !strings.Contains(err.Error(), "read record.html") {
			t.Fatalf("readRecord error = %v, want record.html read failure", err)
		}
	})

	t.Run("notes required when has_notes is true", func(t *testing.T) {
		_, recordDir := writeSnapshotFixture(t)
		if err := os.Remove(filepath.Join(recordDir, "notes.md")); err != nil {
			t.Fatalf("remove notes.md: %v", err)
		}
		_, err := readRecord(recordDir, filepath.Base(recordDir))
		if err == nil || !strings.Contains(err.Error(), "read notes.md") {
			t.Fatalf("readRecord error = %v, want missing notes failure", err)
		}
	})

	t.Run("notes forbidden when has_notes is false", func(t *testing.T) {
		_, recordDir := writeSnapshotFixture(t)
		writeMetadataJSON(t, recordDir, map[string]any{
			"format_version": 1,
			"id":             "20260309-aaaabbbb",
			"date":           "2026-03-09",
			"day_order":      "a0",
			"has_notes":      false,
			"figures": []map[string]any{{
				"filename": "plot.png",
				"s3_key":   "figures/20260309-aaaabbbb/plot.png",
				"alt_text": nil,
			}},
			"data_files": []map[string]any{},
			"created_at": "2026-03-09T12:00:00Z",
			"updated_at": "2026-03-09T12:00:00Z",
		})
		_, err := readRecord(recordDir, filepath.Base(recordDir))
		if err == nil || !strings.Contains(err.Error(), "notes.md present") {
			t.Fatalf("readRecord error = %v, want forbidden notes failure", err)
		}
	})
}

func TestReadFiguresRejectsDirectoryAndPointerProblems(t *testing.T) {
	t.Run("invalid figure filename", func(t *testing.T) {
		root := t.TempDir()
		if _, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "bad/name.png",
			S3Key:    "figures/20260309-aaaabbbb/bad/name.png",
		}}); err == nil || !strings.Contains(err.Error(), "figure filename") {
			t.Fatalf("readFigures error = %v, want figure filename validation failure", err)
		}
	})

	t.Run("missing figure file", func(t *testing.T) {
		root := t.TempDir()
		if _, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}}); err == nil || !strings.Contains(err.Error(), "read figure") {
			t.Fatalf("readFigures error = %v, want missing figure failure", err)
		}
	})

	t.Run("extra figure file", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "record")
		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(figuresDir, 0o755); err != nil {
			t.Fatalf("mkdir figures dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "plot.png"), "plot")
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "extra.png"), "extra")
		_, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}})
		if err == nil || !strings.Contains(err.Error(), "not referenced by metadata") {
			t.Fatalf("readFigures error = %v, want unreferenced file failure", err)
		}
	})

	t.Run("nested figure directory", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "record")
		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(filepath.Join(figuresDir, "nested"), 0o755); err != nil {
			t.Fatalf("mkdir nested figures dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "plot.png"), "plot")
		_, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}})
		if err == nil || !strings.Contains(err.Error(), "unexpected nested dir") {
			t.Fatalf("readFigures error = %v, want nested dir failure", err)
		}
	})

	t.Run("unexpected figures when metadata is empty", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "record")
		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(figuresDir, 0o755); err != nil {
			t.Fatalf("mkdir figures dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "extra.png"), "extra")
		if _, err := readFigures(root, "20260309-aaaabbbb", nil); err == nil || !strings.Contains(err.Error(), "contains files not referenced") {
			t.Fatalf("readFigures error = %v, want unexpected figures failure", err)
		}
	})

	t.Run("missing figures dir is allowed when metadata is empty", func(t *testing.T) {
		root := t.TempDir()
		figures, err := readFigures(root, "20260309-aaaabbbb", nil)
		if err != nil {
			t.Fatalf("readFigures error = %v, want nil", err)
		}
		if len(figures) != 0 {
			t.Fatalf("readFigures returned %d figures, want 0", len(figures))
		}
	})

	t.Run("metadata empty but figures path is not a directory", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "record")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("mkdir record dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(root, "figures"), "not-a-directory")
		if _, err := readFigures(root, "20260309-aaaabbbb", nil); err == nil || !strings.Contains(err.Error(), "read figures dir") {
			t.Fatalf("readFigures error = %v, want read figures dir failure", err)
		}
	})

	t.Run("metadata present but figure dir cannot be listed", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "record")
		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(figuresDir, 0o755); err != nil {
			t.Fatalf("mkdir figures dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "plot.png"), "plot")
		if err := os.Chmod(figuresDir, 0o111); err != nil {
			t.Fatalf("chmod figures dir: %v", err)
		}
		defer func() {
			if err := os.Chmod(figuresDir, 0o755); err != nil {
				t.Fatalf("restore figures dir permissions: %v", err)
			}
		}()
		if _, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}}); err == nil || !strings.Contains(err.Error(), "read figures dir") {
			t.Fatalf("readFigures error = %v, want read figures dir failure", err)
		}
	})

	t.Run("rejects lfs pointer", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "record")
		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(figuresDir, 0o755); err != nil {
			t.Fatalf("mkdir figures dir: %v", err)
		}
		mustWriteFileSnapshot(t, filepath.Join(figuresDir, "plot.png"), "version https://git-lfs.github.com/spec/v1\noid sha256:DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF\nsize 42\n")
		_, err := readFigures(root, "20260309-aaaabbbb", []figureFile{{
			Filename: "plot.png",
			S3Key:    "figures/20260309-aaaabbbb/plot.png",
		}})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "git lfs") {
			t.Fatalf("readFigures error = %v, want git lfs failure", err)
		}
	})
}

func TestReadPropagatesRecordsDirFailureAfterTemplatesLoad(t *testing.T) {
	root := t.TempDir()
	templatesDir := filepath.Join(root, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("mkdir templates dir: %v", err)
	}
	mustWriteFileSnapshot(t, filepath.Join(templatesDir, "text-only.html"), "<html>template</html>")

	if _, err := Read(root); err == nil || !strings.Contains(err.Error(), "read records dir") {
		t.Fatalf("Read(root) error = %v, want records dir failure", err)
	}
}

func TestHelperFunctionsRejectInvalidInputs(t *testing.T) {
	t.Run("manifest rejects unreadable file target", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Symlink(filepath.Join(root, "missing.txt"), filepath.Join(root, "broken.txt")); err != nil {
			t.Fatalf("create broken manifest symlink: %v", err)
		}
		if _, err := Manifest(root); err == nil {
			t.Fatal("expected Manifest to fail for a broken symlink")
		}
	})

	t.Run("manifest missing root", func(t *testing.T) {
		if _, err := Manifest(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("expected Manifest to fail for a missing root")
		}
	})

	t.Run("validatePathSegment", func(t *testing.T) {
		cases := []struct {
			value string
			want  string
		}{
			{value: "", want: "required"},
			{value: ".", want: "must not be"},
			{value: "..", want: "must not be"},
			{value: "nested/name", want: "path separators"},
		}
		for _, tc := range cases {
			if err := validatePathSegment("field", tc.value); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validatePathSegment(%q) error = %v, want %q", tc.value, err, tc.want)
			}
		}
		if err := validatePathSegment("field", "valid-name"); err != nil {
			t.Fatalf("validatePathSegment(valid-name) error = %v", err)
		}
	})

	t.Run("isLFSPointer", func(t *testing.T) {
		if !isLFSPointer([]byte("version https://git-lfs.github.com/spec/v1\noid sha256:ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789\nsize 42\n")) {
			t.Fatal("expected uppercase Git LFS pointer to be detected")
		}
		if isLFSPointer([]byte(strings.Repeat("x", 513))) {
			t.Fatal("expected oversized content not to be treated as a Git LFS pointer")
		}
	})

	t.Run("writeFile create temp failure", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "blocked")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir blocked dir: %v", err)
		}
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatalf("chmod blocked dir: %v", err)
		}
		defer func() {
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatalf("restore blocked dir permissions: %v", err)
			}
		}()
		if err := writeFile(filepath.Join(dir, "child.txt"), []byte("content")); err == nil {
			t.Fatal("expected writeFile to fail when CreateTemp cannot write")
		}
	})

	t.Run("writeFile cleans up temp file after rename failure", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		if err := os.MkdirAll(filepath.Join(target, "nested"), 0o755); err != nil {
			t.Fatalf("mkdir target dir: %v", err)
		}
		if err := writeFile(target, []byte("content")); err == nil {
			t.Fatal("expected writeFile to fail when rename target is a directory")
		}
		entries, err := filepath.Glob(filepath.Join(root, ".snapshot-*.tmp"))
		if err != nil {
			t.Fatalf("glob temp files: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("temp files remaining after rename failure = %v, want none", entries)
		}
	})
}

func TestWriteFaultInjectionErrorPaths(t *testing.T) {
	origCreateTemp := createTempFileFn
	origRename := renameFileFn
	origRemove := removeFileFn
	origChmod := chmodFileFn
	t.Cleanup(func() {
		createTempFileFn = origCreateTemp
		renameFileFn = origRename
		removeFileFn = origRemove
		chmodFileFn = origChmod
	})

	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	baseSnapshot := Snapshot{
		Templates: []Template{{Name: "text-only", HTMLContent: "<html>template</html>"}},
		Records: []Record{{
			ID:          "20260309-aaaabbbb",
			Date:        "2026-03-09",
			DayOrder:    "a0",
			HTMLContent: strPtrSnapshot("<html>record</html>"),
			CreatedAt:   now,
			UpdatedAt:   now,
		}},
	}

	t.Run("template write failure", func(t *testing.T) {
		createTempFileFn = func(string, string) (tempFile, error) {
			return &stubTempFile{name: filepath.Join(t.TempDir(), "template.tmp"), writeErr: errors.New("write failed")}, nil
		}

		err := Write(t.TempDir(), baseSnapshot)
		if err == nil || !strings.Contains(err.Error(), "write template text-only") {
			t.Fatalf("Write() error = %v, want wrapped template write failure", err)
		}
	})

	t.Run("metadata sync failure", func(t *testing.T) {
		createTempFileFn = func(string, string) (tempFile, error) {
			return &stubTempFile{name: filepath.Join(t.TempDir(), "metadata.tmp"), syncErr: errors.New("sync failed")}, nil
		}

		err := Write(t.TempDir(), baseSnapshot)
		if err == nil || !strings.Contains(err.Error(), "write template text-only") {
			t.Fatalf("Write() error = %v, want wrapped sync failure", err)
		}
	})

	t.Run("record close failure", func(t *testing.T) {
		createTempFileFn = func(_ string, _ string) (tempFile, error) {
			return &stubTempFile{name: filepath.Join(t.TempDir(), "record.tmp"), closeErr: errors.New("close failed")}, nil
		}

		err := Write(t.TempDir(), Snapshot{
			Records: []Record{{
				ID:          "20260309-beadfeed",
				Date:        "2026-03-09",
				DayOrder:    "a0",
				HTMLContent: strPtrSnapshot("<html>record</html>"),
				CreatedAt:   now,
				UpdatedAt:   now,
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "write record.html for 20260309-beadfeed") {
			t.Fatalf("Write() error = %v, want wrapped record close failure", err)
		}
	})

	t.Run("record notes write failure", func(t *testing.T) {
		call := 0
		createTempFileFn = func(string, string) (tempFile, error) {
			call++
			if call == 2 {
				return &stubTempFile{name: filepath.Join(t.TempDir(), "notes.tmp"), writeErr: errors.New("notes failed")}, nil
			}
			return origCreateTemp(t.TempDir(), ".snapshot-*.tmp")
		}

		notes := "notes"
		err := Write(t.TempDir(), Snapshot{
			Records: []Record{{
				ID:          "20260309-badnotes",
				Date:        "2026-03-09",
				DayOrder:    "a0",
				HTMLContent: strPtrSnapshot("<html>record</html>"),
				Notes:       &notes,
				CreatedAt:   now,
				UpdatedAt:   now,
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "write notes.md for 20260309-badnotes") {
			t.Fatalf("Write() error = %v, want wrapped notes write failure", err)
		}
	})

	t.Run("record metadata write failure", func(t *testing.T) {
		call := 0
		createTempFileFn = func(string, string) (tempFile, error) {
			call++
			if call == 2 {
				return &stubTempFile{name: filepath.Join(t.TempDir(), "metadata.tmp"), writeErr: errors.New("metadata failed")}, nil
			}
			return origCreateTemp(t.TempDir(), ".snapshot-*.tmp")
		}

		err := Write(t.TempDir(), Snapshot{
			Records: []Record{{
				ID:          "20260309-badmeta0",
				Date:        "2026-03-09",
				DayOrder:    "a0",
				HTMLContent: strPtrSnapshot("<html>record</html>"),
				CreatedAt:   now,
				UpdatedAt:   now,
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "write metadata.json for 20260309-badmeta0") {
			t.Fatalf("Write() error = %v, want wrapped record metadata write failure", err)
		}
	})

	t.Run("chat metadata write failure", func(t *testing.T) {
		createTempFileFn = func(string, string) (tempFile, error) {
			return &stubTempFile{name: filepath.Join(t.TempDir(), "chat.tmp"), writeErr: errors.New("chat write failed")}, nil
		}

		err := Write(t.TempDir(), Snapshot{
			Chats: []ChatSession{{
				ID:              "20260309-c0ffee00",
				Source:          "codex",
				SourceSessionID: "source",
				SourceDeviceID:  "device/unit",
				StartedAt:       now,
				LastActivityAt:  now,
				CreatedAt:       now,
				UpdatedAt:       now,
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "write chat metadata for 20260309-c0ffee00") {
			t.Fatalf("Write() error = %v, want wrapped chat metadata write failure", err)
		}
	})

	t.Run("chat items write failure", func(t *testing.T) {
		call := 0
		createTempFileFn = func(string, string) (tempFile, error) {
			call++
			if call == 2 {
				return &stubTempFile{name: filepath.Join(t.TempDir(), "chat-items.tmp"), writeErr: errors.New("items failed")}, nil
			}
			return origCreateTemp(t.TempDir(), ".snapshot-*.tmp")
		}

		text := "item"
		err := Write(t.TempDir(), Snapshot{
			Chats: []ChatSession{{
				ID:              "20260309-c0ffee01",
				Source:          "codex",
				SourceSessionID: "source",
				SourceDeviceID:  "device/unit",
				StartedAt:       now,
				LastActivityAt:  now,
				CreatedAt:       now,
				UpdatedAt:       now,
				Items:           []ChatItem{{Ordinal: 0, Role: "user", ItemType: "message", Text: &text, SearchText: text, CreatedAt: now}},
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "write chat items for 20260309-c0ffee01") {
			t.Fatalf("Write() error = %v, want wrapped chat items write failure", err)
		}
	})

	t.Run("projects registry write failure", func(t *testing.T) {
		createTempFileFn = func(string, string) (tempFile, error) {
			return &stubTempFile{name: filepath.Join(t.TempDir(), "projects.tmp"), writeErr: errors.New("projects failed")}, nil
		}

		err := Write(t.TempDir(), Snapshot{Projects: []RegistryEntry{{ID: "phase7/unit", CreatedAt: now, UpdatedAt: now}}})
		if err == nil || !strings.Contains(err.Error(), "write projects.json") {
			t.Fatalf("Write() error = %v, want wrapped projects registry write failure", err)
		}
	})

	t.Run("devices registry write failure", func(t *testing.T) {
		call := 0
		createTempFileFn = func(string, string) (tempFile, error) {
			call++
			if call == 2 {
				return &stubTempFile{name: filepath.Join(t.TempDir(), "devices.tmp"), writeErr: errors.New("devices failed")}, nil
			}
			return origCreateTemp(t.TempDir(), ".snapshot-*.tmp")
		}

		err := Write(t.TempDir(), Snapshot{
			Projects: []RegistryEntry{{ID: "phase7/unit", CreatedAt: now, UpdatedAt: now}},
			Devices:  []RegistryEntry{{ID: "device/unit", CreatedAt: now, UpdatedAt: now}},
		})
		if err == nil || !strings.Contains(err.Error(), "write devices.json") {
			t.Fatalf("Write() error = %v, want wrapped devices registry write failure", err)
		}
	})

	t.Run("rename failure removes temp file", func(t *testing.T) {
		tempDir := t.TempDir()
		tempPath := filepath.Join(tempDir, ".snapshot-rename.tmp")
		if err := os.WriteFile(tempPath, []byte("stale"), 0o644); err != nil {
			t.Fatalf("write temp path: %v", err)
		}

		createTempFileFn = func(string, string) (tempFile, error) {
			file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				t.Fatalf("open temp path: %v", err)
			}
			return file, nil
		}
		renameFileFn = func(string, string) error {
			return errors.New("rename failed")
		}

		err := Write(tempDir, Snapshot{
			Records: []Record{{
				ID:          "20260309-feedface",
				Date:        "2026-03-09",
				DayOrder:    "a0",
				HTMLContent: strPtrSnapshot("<html>record</html>"),
				CreatedAt:   now,
				UpdatedAt:   now,
			}},
		})
		if err == nil || !strings.Contains(err.Error(), "write record.html for 20260309-feedface") {
			t.Fatalf("Write() error = %v, want wrapped rename failure", err)
		}
		if _, statErr := os.Stat(tempPath); !os.IsNotExist(statErr) {
			t.Fatalf("expected temp file cleanup after rename failure, stat err = %v", statErr)
		}
	})

	t.Run("chmod failure after rename", func(t *testing.T) {
		createTempFileFn = origCreateTemp
		renameFileFn = origRename
		chmodFileFn = func(string, os.FileMode) error {
			return errors.New("chmod failed")
		}

		err := Write(t.TempDir(), baseSnapshot)
		if err == nil || !strings.Contains(err.Error(), "chmod failed") {
			t.Fatalf("Write() error = %v, want chmod failure", err)
		}
	})
}

type stubTempFile struct {
	name     string
	writeErr error
	syncErr  error
	closeErr error
}

func (s *stubTempFile) Write(p []byte) (int, error) {
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return len(p), nil
}

func (s *stubTempFile) Sync() error {
	return s.syncErr
}

func (s *stubTempFile) Close() error {
	return s.closeErr
}

func (s *stubTempFile) Name() string {
	return s.name
}

func writeSnapshotFixture(t *testing.T) (root string, recordDir string) {
	t.Helper()

	root = t.TempDir()
	notes := "fixture notes"
	snapshot := Snapshot{
		Templates: []Template{{Name: "text-only", HTMLContent: "<html>template</html>"}},
		Records: []Record{{
			ID:             "20260309-aaaabbbb",
			Date:           "2026-03-09",
			DayOrder:       "a0",
			ProjectID:      "phase7/unit",
			SourceDeviceID: "device/unit",
			HTMLContent:    strPtrSnapshot("<html><body><img src=\"figures/plot.png\"></body></html>"),
			Notes:          &notes,
			Figures: []Figure{{
				Filename: "plot.png",
				S3Key:    "figures/20260309-aaaabbbb/plot.png",
				Content:  []byte("plot-bytes"),
			}},
			CreatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		}},
	}
	if err := Write(root, snapshot); err != nil {
		t.Fatalf("Write(root) error = %v", err)
	}
	return root, filepath.Join(root, "records", "20260309-aaaabbbb")
}

func writeMetadataJSON(t *testing.T, recordDir string, metadata map[string]any) {
	t.Helper()
	if _, ok := metadata["project_id"]; !ok {
		metadata["project_id"] = "phase7/unit"
	}
	if _, ok := metadata["source_device_id"]; !ok {
		metadata["source_device_id"] = "device/unit"
	}

	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(recordDir, "metadata.json"), raw, 0o644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}
}

func mustWriteFileSnapshot(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeReplacementTestFile(t *testing.T, path string, content string) {
	t.Helper()
	mustWriteFileSnapshot(t, path, content)
}

func assertReplacementTestFile(t *testing.T, path string, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s content = %q, want %q", path, got, want)
	}
}
