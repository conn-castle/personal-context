package chatimport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/timeutil"
)

func TestParseSourceFixtures(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		path      string
		sessionID string
		wantItems int
		wantTool  bool
	}{
		{name: "codex", source: "codex", path: "testdata/codex-session.jsonl", sessionID: "codex-fixture", wantItems: 2},
		{name: "claude", source: "claude_code", path: "testdata/claude-session.jsonl", sessionID: "claude-fixture", wantItems: 2, wantTool: true},
		{name: "gemini", source: "gemini", path: "testdata/gemini-session.json", sessionID: "gemini-fixture", wantItems: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			session, items, err := ParseTranscriptFile(tc.source, tc.path)
			if err != nil {
				t.Fatalf("ParseTranscriptFile() error = %v", err)
			}
			if session.Source != tc.source || session.SourceSessionID != tc.sessionID {
				t.Fatalf("unexpected session source/id: %+v", session)
			}
			if session.CWD == nil || session.Title == nil || session.StartedAt.IsZero() || session.OriginalSourcePath == nil {
				t.Fatalf("expected cwd/title/timestamps/original source path, got %+v", session)
			}
			if len(items) != tc.wantItems {
				t.Fatalf("items len = %d, want %d: %+v", len(items), tc.wantItems, items)
			}
			foundTool := false
			for i, item := range items {
				if item.Ordinal != i {
					t.Fatalf("item ordinal = %d, want %d", item.Ordinal, i)
				}
				if strings.TrimSpace(item.SearchText) == "" || item.RawJSON == nil {
					t.Fatalf("expected search text and raw json, got %+v", item)
				}
				foundTool = foundTool || item.ItemType == "tool_output"
			}
			if foundTool != tc.wantTool {
				t.Fatalf("found tool output = %v, want %v", foundTool, tc.wantTool)
			}
		})
	}
}

func TestJSONLItemFieldsDoNotOverwriteSessionIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fallback-session.jsonl")
	transcript := strings.Join([]string{
		`{"role":"user","id":"message-1","content":"first item","timestamp":"2026-05-14T12:01:00Z"}`,
		`{"role":"assistant","id":"message-2","content":"second item","timestamp":"2026-05-14T12:02:00Z"}`,
		``,
	}, "\n")
	if err := os.WriteFile(path, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	session, items, err := ParseTranscriptFile("codex", path)
	if err != nil {
		t.Fatalf("ParseTranscriptFile() error = %v", err)
	}
	if session.SourceSessionID != "fallback-session" {
		t.Fatalf("source session id = %q, want filename fallback", session.SourceSessionID)
	}
	if !session.StartedAt.Equal(time.Date(2026, 5, 14, 12, 1, 0, 0, time.UTC)) {
		t.Fatalf("started_at = %v", session.StartedAt)
	}
	if !session.LastActivityAt.Equal(time.Date(2026, 5, 14, 12, 2, 0, 0, time.UTC)) {
		t.Fatalf("last_activity_at = %v", session.LastActivityAt)
	}
	if len(items) != 2 || items[0].CreatedAt == nil || !items[0].CreatedAt.Equal(time.Date(2026, 5, 14, 12, 1, 0, 0, time.UTC)) {
		t.Fatalf("unexpected item timestamps: %+v", items)
	}
}

func TestJSONLItemSessionScopedFieldsCanFillFallbackMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fallback-session.jsonl")
	transcript := strings.Join([]string{
		`{"role":"user","session_id":"real-session","cwd":"/tmp/from-item","title":"From item","content":"first item","timestamp":"2026-05-14T12:01:00Z"}`,
		`{"role":"assistant","session_id":"other-session","content":"second item","timestamp":"2026-05-14T12:02:00Z"}`,
		``,
	}, "\n")
	if err := os.WriteFile(path, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	session, _, err := ParseTranscriptFile("codex", path)
	if err != nil {
		t.Fatalf("ParseTranscriptFile() error = %v", err)
	}
	if session.SourceSessionID != "real-session" {
		t.Fatalf("source session id = %q, want item session_id", session.SourceSessionID)
	}
	if session.CWD == nil || *session.CWD != "/tmp/from-item" || session.Title == nil || *session.Title != "From item" {
		t.Fatalf("expected item cwd/title metadata, got %+v", session)
	}
}

func TestJSONTranscriptStreamingPreservesPriority(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "priority.json")
	transcript := `{
  "id": "generic-id",
  "ignored": {"nested": [{"large": "discarded"}]},
  "items": [{"role": "user", "content": "lower priority item"}],
  "session_id": "canonical-id",
  "messages": [42, {"role": "assistant", "content": "preferred message", "timestamp": "2026-05-14T12:00:00Z"}],
  "transcript": [{"role": "user", "content": "lowest priority item"}],
  "cwd": "/tmp/stream-json",
  "title": "Streamed JSON"
}`
	if err := os.WriteFile(path, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	session, items, err := ParseTranscriptFile("codex", path)
	if err != nil {
		t.Fatalf("ParseTranscriptFile() error = %v", err)
	}
	if session.SourceSessionID != "canonical-id" {
		t.Fatalf("source session id = %q, want canonical-id", session.SourceSessionID)
	}
	if session.CWD == nil || *session.CWD != "/tmp/stream-json" || session.Title == nil || *session.Title != "Streamed JSON" {
		t.Fatalf("expected streamed cwd/title metadata, got %+v", session)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1: %+v", len(items), items)
	}
	if items[0].SearchText != "preferred message" || items[0].Ordinal != 1 {
		t.Fatalf("expected messages array with original ordinal preserved, got %+v", items[0])
	}
}

func TestParseAppendedJSONLTranscriptUsesBaseSession(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "append-session.jsonl")
	initial := strings.Join([]string{
		`{"session_id":"append-session","role":"user","content":"first item","timestamp":"2026-05-14T12:00:00Z"}`,
		`{"session_id":"append-session","role":"assistant","content":"second item","timestamp":"2026-05-14T12:01:00Z"}`,
	}, "\n") + "\n"
	appended := `{"session_id":"different-session","role":"user","content":"appended item","timestamp":"2026-05-14T12:02:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(initial+appended), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	cwd := "/tmp/append-project"
	title := "Existing title"
	base := repository.CreateChatSessionInput{
		ID:                 "20260514-abcdef12",
		Source:             "codex",
		SourceSessionID:    "append-session",
		SourceDeviceID:     "test-device",
		CWD:                &cwd,
		Title:              &title,
		StartedAt:          time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		LastActivityAt:     time.Date(2026, 5, 14, 12, 1, 0, 0, time.UTC),
		OriginalSourcePath: &path,
	}

	session, items, err := ParseAppendedJSONLTranscript("codex", path, int64(len(initial)), int64(len(appended)), base, 7)
	if err != nil {
		t.Fatalf("ParseAppendedJSONLTranscript() error = %v", err)
	}
	if session.SourceSessionID != "append-session" || session.CWD == nil || *session.CWD != cwd || session.Title == nil || *session.Title != title {
		t.Fatalf("expected base session metadata to be preserved, got %+v", session)
	}
	if !session.StartedAt.Equal(base.StartedAt) || !session.LastActivityAt.Equal(time.Date(2026, 5, 14, 12, 2, 0, 0, time.UTC)) {
		t.Fatalf("unexpected appended session times: %+v", session)
	}
	if len(items) != 1 || items[0].Ordinal != 7 || items[0].SearchText != "appended item" {
		t.Fatalf("unexpected appended items: %+v", items)
	}

	if _, _, err := ParseAppendedJSONLTranscript("codex", path, -1, int64(len(appended)), base, 0); err == nil {
		t.Fatal("expected invalid append range error")
	}
	unsupported := filepath.Join(root, "append.json")
	if err := os.WriteFile(unsupported, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write unsupported transcript: %v", err)
	}
	if _, _, err := ParseAppendedJSONLTranscript("codex", unsupported, 0, 0, base, 0); err == nil {
		t.Fatal("expected unsupported append transcript extension error")
	}
	if _, _, err := ParseAppendedJSONLTranscript("codex", filepath.Join(root, "missing.jsonl"), 0, 0, base, 0); err == nil {
		t.Fatal("expected missing appended transcript error")
	}
}

func TestTranscriptFilesSkipsGeminiLogsJSON(t *testing.T) {
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "gemini-session.json")
	logsPath := filepath.Join(root, "logs.json")
	if err := os.WriteFile(transcriptPath, []byte(`{"messages":[]}`), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	if err := os.WriteFile(logsPath, []byte(`[]`), 0o644); err != nil {
		t.Fatalf("write logs sidecar: %v", err)
	}

	files, err := TranscriptFiles(root)
	if err != nil {
		t.Fatalf("TranscriptFiles(directory) error = %v", err)
	}
	if len(files) != 1 || files[0] != transcriptPath {
		t.Fatalf("TranscriptFiles(directory) = %+v, want only %q", files, transcriptPath)
	}

	files, err = TranscriptFiles(logsPath)
	if err != nil {
		t.Fatalf("TranscriptFiles(file root) error = %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("TranscriptFiles(file root logs.json) = %+v, want none", files)
	}
}

func TestHelpersAndParseErrors(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	jsonFile := filepath.Join(root, "a.json")
	jsonlFile := filepath.Join(nested, "b.jsonl")
	ndjsonFile := filepath.Join(nested, "c.ndjson")
	ignoredFile := filepath.Join(root, "ignored.txt")
	for path, content := range map[string]string{
		jsonFile:    `{}`,
		jsonlFile:   `{}`,
		ndjsonFile:  `{}`,
		ignoredFile: `ignored`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	files, err := TranscriptFiles(root)
	if err != nil {
		t.Fatalf("TranscriptFiles() error = %v", err)
	}
	if len(files) != 3 || files[0] != jsonFile || files[1] != jsonlFile || files[2] != ndjsonFile {
		t.Fatalf("unexpected transcript files: %+v", files)
	}
	fileRoot := filepath.Join(root, "not-a-dir.json")
	if err := os.WriteFile(fileRoot, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write file root: %v", err)
	}
	files, err = TranscriptFiles(fileRoot)
	if err != nil || len(files) != 1 || files[0] != fileRoot {
		t.Fatalf("file transcript root files=%+v err=%v", files, err)
	}
	files, err = TranscriptFiles(filepath.Join(root, "missing"))
	if err != nil || len(files) != 0 {
		t.Fatalf("missing transcript root files=%+v err=%v", files, err)
	}
	roots, err := Roots([]string{root}, "gemini", nil)
	if err != nil {
		t.Fatalf("Roots(extra) error = %v", err)
	}
	if len(roots) != 1 || len(roots["gemini"]) != 1 {
		t.Fatalf("unexpected explicit roots: %+v", roots)
	}
	projectRoots, err := Roots(nil, "codex", []repository.ProjectPath{{Path: root}})
	if err != nil {
		t.Fatalf("Roots(project paths) error = %v", err)
	}
	if len(projectRoots["codex"]) < 2 || !strings.HasSuffix(projectRoots["codex"][1], filepath.Join(".codex", "sessions")) {
		t.Fatalf("unexpected project-derived roots: %+v", projectRoots)
	}
	allProjectRoots, err := Roots(nil, "", []repository.ProjectPath{{Path: root}})
	if err != nil {
		t.Fatalf("Roots(all project paths) error = %v", err)
	}
	// Each source must contribute at least one project-derived root in
	// addition to the home-dir root; for source==claude_code the
	// per-project pass contributes both `.claude/projects` and
	// `.claude-config/projects` (see
	// TestRootsIncludesClaudeProjectDirsForPerProjectScans).
	suffixesBySource := map[string][]string{
		"codex":       {filepath.Join(".codex", "sessions")},
		"claude_code": {filepath.Join(".claude", "projects"), filepath.Join(".claude-config", "projects")},
		"gemini":      {filepath.Join(".gemini", "tmp")},
	}
	for source, suffixes := range suffixesBySource {
		roots := allProjectRoots[source]
		for _, suffix := range suffixes {
			matched := false
			for _, r := range roots {
				if strings.HasSuffix(r, suffix) {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("expected %s project root with suffix %q, got %+v", source, suffix, roots)
			}
		}
	}
	for _, agent := range []string{"", "codex", "claude", "claude-code", "claude_code", "gemini"} {
		if _, err := NormalizeAgentName(agent); err != nil {
			t.Fatalf("NormalizeAgentName(%q) error = %v", agent, err)
		}
	}
	if _, err := NormalizeAgentName("bad"); err == nil {
		t.Fatal("expected unknown agent error")
	}
	if _, err := ParseChatTime("not-a-time"); err == nil {
		t.Fatal("expected bad timestamp error")
	}
	millisTime := time.Date(2026, 5, 14, 12, 0, 0, 123000000, time.UTC)
	if parsed, err := ParseChatTime(timeutil.FormatUTCMillis(millisTime)); err != nil || !parsed.Equal(millisTime) {
		t.Fatalf("ParseChatTime(UTC millis) = %v, %v; want %v", parsed, err, millisTime)
	}

	deviceID := "device-a"
	child := filepath.Join(root, "nested", "child")
	paths := []repository.ProjectPath{
		{ProjectID: "short", Path: root, DeviceID: deviceID},
		{ProjectID: "best", Path: nested, DeviceID: deviceID},
		{ProjectID: "wrong-device", Path: child, DeviceID: "other"},
	}
	if got := MatchProjectPath(paths, &child, deviceID); got == nil || *got != "best" {
		t.Fatalf("expected longest matching project path, got %+v", got)
	}
	if got := MatchProjectPath(paths, nil, deviceID); got != nil {
		t.Fatalf("expected nil project match without cwd, got %+v", got)
	}

	unsupported := filepath.Join(root, "bad.txt")
	if _, _, err := ParseTranscriptFile("codex", unsupported); err == nil {
		t.Fatal("expected unsupported transcript extension error")
	}
	badJSON := filepath.Join(root, "bad.json")
	if err := os.WriteFile(badJSON, []byte(`{bad`), 0o644); err != nil {
		t.Fatalf("write bad json: %v", err)
	}
	if _, _, err := ParseTranscriptFile("codex", badJSON); err == nil {
		t.Fatal("expected bad json transcript error")
	}
	badJSONL := filepath.Join(root, "bad.jsonl")
	if err := os.WriteFile(badJSONL, []byte("{bad\n"), 0o644); err != nil {
		t.Fatalf("write bad jsonl: %v", err)
	}
	if _, _, err := ParseTranscriptFile("codex", badJSONL); err == nil {
		t.Fatal("expected bad jsonl transcript error")
	}
	blankJSONL := filepath.Join(root, "blank.jsonl")
	if err := os.WriteFile(blankJSONL, []byte("\n"+`{"role":"user","content":"blank branch","timestamp":"2026-05-14T12:00:00Z"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write blank jsonl: %v", err)
	}
	if _, items, err := ParseTranscriptFile("codex", blankJSONL); err != nil || len(items) != 1 {
		t.Fatalf("expected blank jsonl transcript to parse one item, items=%+v err=%v", items, err)
	}
	mixedJSON := filepath.Join(root, "mixed.json")
	if err := os.WriteFile(mixedJSON, []byte(`{"messages":[42,{"role":"user","content":"mixed branch"}]}`), 0o644); err != nil {
		t.Fatalf("write mixed json: %v", err)
	}
	if _, items, err := ParseTranscriptFile("codex", mixedJSON); err != nil || len(items) != 1 {
		t.Fatalf("expected mixed json transcript to skip non-object item, items=%+v err=%v", items, err)
	}
}

func TestUnexportedNormalizationBranches(t *testing.T) {
	root := t.TempDir()
	session := repository.CreateChatSessionInput{
		StartedAt:      time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC),
	}
	applySessionFields(&session, map[string]any{"timestamp": "2026-05-14T12:30:00Z"})
	if !session.LastActivityAt.Equal(time.Date(2026, 5, 14, 12, 30, 0, 0, time.UTC)) {
		t.Fatalf("expected last activity to advance, got %v", session.LastActivityAt)
	}
	session = repository.CreateChatSessionInput{}
	finalizeSessionTimes(&session, []repository.CreateChatItemInput{{Text: strPtr("first message becomes title")}})
	if session.StartedAt.IsZero() || session.LastActivityAt.IsZero() || session.Title == nil {
		t.Fatalf("expected session times and title to be finalized, got %+v", session)
	}
	if got := contentText(map[string]any{"content": "from map"}); got != "from map" {
		t.Fatalf("contentText(map) = %q", got)
	}
	if got := contentText([]any{map[string]any{"text": "from array"}, "ignored"}); got != "from array" {
		t.Fatalf("contentText(array) = %q", got)
	}
	if got := contentText(42); got != "" {
		t.Fatalf("contentText(default) = %q", got)
	}
	if got := stringField(map[string]any{"empty": "   "}, "empty"); got != nil {
		t.Fatalf("stringField(empty) = %+v, want nil", got)
	}
	if got := itemFromPayload(map[string]any{"content": map[string]any{"content": "nested content"}}, 7); got.Role != "unknown" || got.ItemType != "message" || got.Text == nil {
		t.Fatalf("unexpected nested content item: %+v", got)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("truncate small max = %q", got)
	}
	if got := truncate("abcdef", 5); got != "ab..." {
		t.Fatalf("truncate with ellipsis = %q", got)
	}
	if got := truncate("abc", 5); got != "abc" {
		t.Fatalf("truncate short = %q", got)
	}
	if _, err := Roots([]string{" "}, "codex", nil); err == nil {
		t.Fatal("expected empty explicit root error")
	}
	if _, err := Roots([]string{filepath.Join(root, "missing-explicit")}, "codex", nil); err == nil {
		t.Fatal("expected missing explicit root error")
	}
	wrongDevicePath := filepath.Join(root, "other-device")
	paths := []repository.ProjectPath{{ProjectID: "wrong-device", Path: wrongDevicePath, DeviceID: "other"}}
	if got := MatchProjectPath(paths, &wrongDevicePath, "device-a"); got != nil {
		t.Fatalf("expected nil project match for wrong device, got %+v", got)
	}
	if parsed, err := ParseChatTime("2026-05-14T12:00:00Z"); err != nil || parsed.Location() != time.UTC {
		t.Fatalf("ParseChatTime(RFC3339) = %v, %v", parsed, err)
	}

	// hasItemPayload requires actual content (content/text/message) — a
	// bare `{"type":"session_start"}` is metadata, not a transcript item.
	if hasItemPayload(map[string]any{"type": "session_start"}) {
		t.Fatal("hasItemPayload should reject metadata-only objects")
	}
	if !hasItemPayload(map[string]any{"content": "hi"}) {
		t.Fatal("hasItemPayload should accept content-only objects")
	}

	// parseJSONTranscript rejects .json files that lack a transcript
	// array so arbitrary agent state/config JSON under a scanned root is
	// not silently imported as an empty chat session.
	noArrayPath := filepath.Join(root, "no-array.json")
	if err := os.WriteFile(noArrayPath, []byte(`{"id":"x","title":"no items"}`), 0o600); err != nil {
		t.Fatalf("write no-array: %v", err)
	}
	if _, _, err := parseJSONTranscript("codex", noArrayPath); err == nil || !strings.Contains(err.Error(), "no transcript array") {
		t.Fatalf("expected transcript-array rejection, got %v", err)
	}

	// Roots de-duplicates overlapping project paths so the importer
	// doesn't scan the same root twice (which would also break
	// --delete-source).
	pa := filepath.Join(root, "overlap")
	pb := filepath.Join(root, "overlap") // identical second entry
	gotRoots, err := Roots(nil, "codex", []repository.ProjectPath{
		{Path: pa, DeviceID: ""}, {Path: pb, DeviceID: ""},
	})
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	codexRoots := gotRoots["codex"]
	// We expect at most one (home).codex/sessions plus one duplicate-collapsed
	// project root. The exact count depends on the platform, but the same
	// project path appearing twice in input must collapse to one entry.
	seen := map[string]bool{}
	for _, p := range codexRoots {
		if seen[p] {
			t.Fatalf("Roots returned duplicate path %q in %v", p, codexRoots)
		}
		seen[p] = true
	}
}

func strPtr(value string) *string {
	return &value
}

// TestJSONLCodexResponseItemEnvelope reproduces bug #1 from
// `.agent-layer/tmp/chat-import-smoke-test-findings.md`: real codex rollouts
// wrap chat items inside {timestamp, type:"response_item", payload:{...}},
// and the parser was dropping every line because it only inspected top-level
// fields. session_meta lines must lift cwd/id from their inner payload.
func TestJSONLCodexResponseItemEnvelope(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rollout-codex.jsonl")
	lines := []string{
		`{"timestamp":"2026-01-05T21:25:33.024Z","type":"session_meta","payload":{"id":"019b900d-codex","cwd":"/tmp/codex-real","timestamp":"2026-01-05T21:25:33.002Z"}}`,
		`{"timestamp":"2026-01-05T21:25:34.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello codex"}]}}`,
		`{"timestamp":"2026-01-05T21:25:35.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi user"}]}}`,
		`{"timestamp":"2026-01-05T21:25:35.500Z","type":"event_msg","payload":{"type":"agent_reasoning","text":"thinking..."}}`,
		`{"timestamp":"2026-01-05T21:25:36.000Z","type":"turn_context","payload":{"cwd":"/tmp/codex-real","model":"gpt-5.2-codex"}}`,
		``,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	session, items, err := ParseTranscriptFile("codex", path)
	if err != nil {
		t.Fatalf("ParseTranscriptFile() error = %v", err)
	}
	if got := len(items); got != 2 {
		t.Fatalf("expected 2 chat items (user+assistant message payloads), got %d: %+v", got, items)
	}
	if items[0].Role != "user" || items[0].SearchText != "hello codex" {
		t.Fatalf("first item not extracted from response_item.payload: %+v", items[0])
	}
	if items[0].Text == nil || *items[0].Text != "hello codex" {
		t.Fatalf("first item text not populated: text=%v search=%q", items[0].Text, items[0].SearchText)
	}
	if items[1].Role != "assistant" || items[1].SearchText != "hi user" {
		t.Fatalf("second item not extracted: %+v", items[1])
	}
	if session.SourceSessionID != "019b900d-codex" {
		t.Fatalf("session id should come from session_meta.payload.id, got %q", session.SourceSessionID)
	}
	if session.CWD == nil || *session.CWD != "/tmp/codex-real" {
		t.Fatalf("session cwd should be lifted from session_meta.payload.cwd, got %v", session.CWD)
	}
}

// TestJSONLClaudeMessageEnvelope reproduces bug #2 from
// `.agent-layer/tmp/chat-import-smoke-test-findings.md`: real Claude rows
// look like {type:"user"|"assistant", message:{role,content}, ...} where the
// text lives inside message.content. The parser was treating `message` as if
// it were a string field and producing items with empty Text/SearchText. It
// Also asserts that queue-operation / file-history-snapshot / permission-mode
// metadata rows that carry top-level content must not be imported as items.
func TestJSONLClaudeMessageEnvelope(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "abc.jsonl")
	lines := []string{
		`{"type":"last-prompt","leafUuid":"prelude","sessionId":"abc-session"}`,
		// queue-operation lines carry top-level content that duplicates
		// the next user message; they must be dropped, not imported.
		`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-05-03T01:59:59.000Z","sessionId":"abc-session","content":"hello claude"}`,
		`{"parentUuid":null,"type":"user","timestamp":"2026-05-03T02:00:00.000Z","cwd":"/repo","sessionId":"abc-session","message":{"role":"user","content":"hello claude"}}`,
		`{"parentUuid":"x","type":"assistant","timestamp":"2026-05-03T02:00:01.000Z","cwd":"/repo","sessionId":"abc-session","message":{"role":"assistant","content":[{"type":"text","text":"hi back"}]}}`,
		// permission-mode and file-history-snapshot are pure metadata.
		`{"type":"permission-mode","mode":"plan","sessionId":"abc-session"}`,
		`{"type":"file-history-snapshot","sessionId":"abc-session","content":"snapshot blob"}`,
		``,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	session, items, err := ParseTranscriptFile("claude_code", path)
	if err != nil {
		t.Fatalf("ParseTranscriptFile() error = %v", err)
	}
	if got := len(items); got != 2 {
		t.Fatalf("expected exactly 2 items (user+assistant, no queue-operation/metadata duplicates), got %d: %+v", got, items)
	}
	if items[0].Role != "user" || items[0].SearchText != "hello claude" {
		t.Fatalf("user item text not extracted from message.content (string form): %+v", items[0])
	}
	if items[0].Text == nil || *items[0].Text != "hello claude" {
		t.Fatalf("user item Text pointer not set: %+v", items[0].Text)
	}
	if items[1].Role != "assistant" || items[1].SearchText != "hi back" {
		t.Fatalf("assistant item text not extracted from message.content (array form): %+v", items[1])
	}
	if session.SourceSessionID != "abc-session" {
		t.Fatalf("session id should come from outer envelope sessionId, got %q", session.SourceSessionID)
	}
	if session.CWD == nil || *session.CWD != "/repo" {
		t.Fatalf("session cwd should come from outer envelope cwd, got %v", session.CWD)
	}
}

// TestJSONLCodexCWDLiftedFromTurnContext asserts that when session_meta
// lacks a cwd but turn_context carries one, the session's cwd must be
// populated so MatchProjectPath can auto-assign the project.
func TestJSONLCodexCWDLiftedFromTurnContext(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rollout-no-meta-cwd.jsonl")
	lines := []string{
		// session_meta omits cwd; older codex versions did this.
		`{"timestamp":"2026-01-05T21:25:33.024Z","type":"session_meta","payload":{"id":"codex-no-cwd","timestamp":"2026-01-05T21:25:33.002Z"}}`,
		`{"timestamp":"2026-01-05T21:25:34.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`,
		`{"timestamp":"2026-01-05T21:25:34.500Z","type":"turn_context","payload":{"cwd":"/from/turn_context","model":"gpt-5"}}`,
		``,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	session, _, err := ParseTranscriptFile("codex", path)
	if err != nil {
		t.Fatalf("ParseTranscriptFile() error = %v", err)
	}
	if session.CWD == nil || *session.CWD != "/from/turn_context" {
		t.Fatalf("session cwd should be lifted from turn_context.payload.cwd, got %v", session.CWD)
	}
}

func TestJSONLCodexResponseItemSessionFields(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rollout-item-session-fields.jsonl")
	lines := []string{
		`{"timestamp":"2026-01-05T21:25:34.000Z","type":"response_item","payload":{"type":"message","role":"user","session_id":"codex-from-item","cwd":"/from/response_item","content":[{"type":"input_text","text":"hi"}]}}`,
		``,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	session, items, err := ParseTranscriptFile("codex", path)
	if err != nil {
		t.Fatalf("ParseTranscriptFile() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one imported item, got %+v", items)
	}
	if session.SourceSessionID != "codex-from-item" {
		t.Fatalf("session id should be lifted from response_item.payload.session_id, got %q", session.SourceSessionID)
	}
	if session.CWD == nil || *session.CWD != "/from/response_item" {
		t.Fatalf("session cwd should be lifted from response_item.payload.cwd, got %v", session.CWD)
	}
}

func TestJSONLClaudeToolResultEnvelopeIsToolOutput(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tool-result.jsonl")
	lines := []string{
		`{"type":"user","timestamp":"2026-05-03T02:00:00.000Z","cwd":"/repo","sessionId":"tool-session","message":{"role":"user","content":[{"type":"tool_result","content":"hidden tool output"}]}}`,
		``,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	_, items, err := ParseTranscriptFile("claude_code", path)
	if err != nil {
		t.Fatalf("ParseTranscriptFile() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one imported item, got %+v", items)
	}
	if items[0].ItemType != "tool_output" {
		t.Fatalf("Claude tool_result envelope should import as tool_output, got %+v", items[0])
	}
	if items[0].SearchText != "hidden tool output" {
		t.Fatalf("tool output text should still be indexed for explicit include-tool searches, got %q", items[0].SearchText)
	}
}

// TestTranscriptFilesSkipsSubAgentMetaSidecars asserts that Claude sub-agent
// `*.meta.json` sidecars (e.g. `agent-foo.meta.json` next to a sibling
// `agent-foo.jsonl`) are not transcripts and must not be returned by
// TranscriptFiles, which would otherwise trigger noisy parse errors.
func TestTranscriptFilesSkipsSubAgentMetaSidecars(t *testing.T) {
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "agent-foo.jsonl")
	sidecarPath := filepath.Join(root, "agent-foo.meta.json")
	for _, path := range []string{transcriptPath, sidecarPath} {
		if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	files, err := TranscriptFiles(root)
	if err != nil {
		t.Fatalf("TranscriptFiles error = %v", err)
	}
	if len(files) != 1 || files[0] != transcriptPath {
		t.Fatalf("expected only the .jsonl transcript, got %+v", files)
	}
}

func TestTranscriptFilesSkipsClaudeSessionsIndexSidecar(t *testing.T) {
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "claude-session.jsonl")
	sidecarPath := filepath.Join(root, "sessions-index.json")
	if err := os.WriteFile(transcriptPath, []byte(`{"type":"user","message":{"role":"user","content":"hello"}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	if err := os.WriteFile(sidecarPath, []byte(`{"sessions":["claude-session"]}`), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	files, err := TranscriptFiles(root)
	if err != nil {
		t.Fatalf("TranscriptFiles error = %v", err)
	}
	if len(files) != 1 || files[0] != transcriptPath {
		t.Fatalf("expected only the .jsonl transcript, got %+v", files)
	}
}

// TestRootsIncludesClaudeProjectDirsForPerProjectScans asserts that
// per-project scans include Claude transcript directories without scanning the
// broad `.claude` config/worktree root.
func TestRootsIncludesClaudeProjectDirsForPerProjectScans(t *testing.T) {
	projectPath := t.TempDir()
	roots, err := Roots(nil, "claude_code", []repository.ProjectPath{{Path: projectPath}})
	if err != nil {
		t.Fatalf("Roots error = %v", err)
	}
	claudeRoots := roots["claude_code"]
	wantClaude := filepath.Join(projectPath, ".claude", "projects")
	wantClaudeConfig := filepath.Join(projectPath, ".claude-config", "projects")
	hasClaude := false
	hasClaudeConfig := false
	for _, r := range claudeRoots {
		if r == wantClaude {
			hasClaude = true
		}
		if r == wantClaudeConfig {
			hasClaudeConfig = true
		}
	}
	if !hasClaude || !hasClaudeConfig {
		t.Fatalf("expected both %q and %q in per-project claude roots, got %+v", wantClaude, wantClaudeConfig, claudeRoots)
	}
	for _, r := range claudeRoots {
		if r == filepath.Join(projectPath, ".claude") {
			t.Fatalf("per-project claude roots must not scan broad .claude root: %+v", claudeRoots)
		}
	}
}

// TestClaudeSubagentFilesGetDistinctIdentities verifies that two Task-tool
// subagent transcripts under the same parent (which both carry the PARENT
// sessionId on every row) get file-unique source identities plus the parent
// recorded in ParentSourceSessionID — instead of colliding and overwriting
// each other (round-6 Issue 1).
func TestClaudeSubagentFilesGetDistinctIdentities(t *testing.T) {
	root := t.TempDir()
	parentSID := "2dbd6fef-2d8a-4ba9-9eef-32585b350874"
	subDir := filepath.Join(root, parentSID, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	row := func(text string) string {
		return `{"type":"assistant","isSidechain":true,"sessionId":"` + parentSID +
			`","cwd":"/repo","timestamp":"2026-05-17T10:00:00Z","message":{"role":"assistant","content":"` + text + `"}}`
	}
	files := map[string]string{
		"agent-aaa.jsonl": row("alpha subagent text"),
		"agent-bbb.jsonl": row("beta subagent text"),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(subDir, name), []byte(content+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	for name, wantSuffix := range map[string]string{"agent-aaa.jsonl": "agent-aaa", "agent-bbb.jsonl": "agent-bbb"} {
		session, items, err := ParseTranscriptFile("claude_code", filepath.Join(subDir, name))
		if err != nil {
			t.Fatalf("ParseTranscriptFile(%s) error = %v", name, err)
		}
		wantSID := parentSID + ":" + wantSuffix
		if session.SourceSessionID != wantSID {
			t.Fatalf("%s SourceSessionID = %q, want %q", name, session.SourceSessionID, wantSID)
		}
		if session.ParentSourceSessionID == nil || *session.ParentSourceSessionID != parentSID {
			t.Fatalf("%s ParentSourceSessionID = %v, want %q", name, session.ParentSourceSessionID, parentSID)
		}
		if len(items) != 1 {
			t.Fatalf("%s items = %d, want 1", name, len(items))
		}
	}
}

// TestClaudeNonSubagentKeepsParentNil confirms a normal Claude transcript (not
// under subagents/, no isSidechain) resolves its source id from sessionId and
// leaves ParentSourceSessionID nil.
func TestClaudeNonSubagentKeepsParentNil(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.jsonl")
	content := `{"type":"assistant","sessionId":"main-sid","timestamp":"2026-05-17T10:00:00Z","message":{"role":"assistant","content":"hello"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	session, _, err := ParseTranscriptFile("claude_code", path)
	if err != nil {
		t.Fatalf("ParseTranscriptFile() error = %v", err)
	}
	if session.SourceSessionID != "main-sid" {
		t.Fatalf("SourceSessionID = %q, want main-sid", session.SourceSessionID)
	}
	if session.ParentSourceSessionID != nil {
		t.Fatalf("ParentSourceSessionID = %v, want nil", session.ParentSourceSessionID)
	}
}

// TestGeminiItemTypeNormalization verifies Gemini's model/info/error labels are
// normalized so no agent-specific value leaks into item_type (round-6 Issue 3).
func TestGeminiItemTypeNormalization(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session-norm.jsonl")
	lines := []string{
		`{"sessionId":"abc","kind":"main","startTime":"2026-05-17T10:00:00Z"}`,
		`{"type":"user","content":"hi there","timestamp":"2026-05-17T10:00:01Z"}`,
		`{"type":"gemini","content":"hello from the model","timestamp":"2026-05-17T10:00:02Z"}`,
		`{"type":"info","content":"mcp server list","timestamp":"2026-05-17T10:00:03Z"}`,
		`{"type":"error","content":"something failed","timestamp":"2026-05-17T10:00:04Z"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, items, err := ParseTranscriptFile("gemini", path)
	if err != nil {
		t.Fatalf("ParseTranscriptFile() error = %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("items = %d, want 4: %+v", len(items), items)
	}
	want := []struct{ role, itemType string }{
		{"user", "message"},
		{"assistant", "message"},
		{"info", "event"},
		{"error", "event"},
	}
	for i, w := range want {
		if items[i].Role != w.role || items[i].ItemType != w.itemType {
			t.Fatalf("item %d = role=%q type=%q, want role=%q type=%q", i, items[i].Role, items[i].ItemType, w.role, w.itemType)
		}
	}
	for _, item := range items {
		switch item.ItemType {
		case "gemini", "info", "error":
			t.Fatalf("agent-specific item_type leaked: %q", item.ItemType)
		}
	}
}

// TestEmptyTranscriptsReturnErrEmptyTranscript verifies metadata-only / empty
// transcript files report ErrEmptyTranscript so the caller skips session
// creation (round-6 Issue 4).
func TestEmptyTranscriptsReturnErrEmptyTranscript(t *testing.T) {
	root := t.TempDir()
	cases := map[string]struct {
		source, name, content string
	}{
		"claude cursor marker": {"claude_code", "marker.jsonl", `{"type":"last-prompt","leafUuid":"x","sessionId":"y"}` + "\n"},
		"gemini header only":   {"gemini", "session-empty.jsonl", `{"sessionId":"z","kind":"main","startTime":"2026-05-17T10:00:00Z"}` + "\n"},
		"empty json messages":  {"gemini", "empty.json", `{"messages":[]}`},
		"zero bytes":           {"claude_code", "zero.jsonl", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, tc.name)
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			_, _, err := ParseTranscriptFile(tc.source, path)
			if !errors.Is(err, ErrEmptyTranscript) {
				t.Fatalf("error = %v, want ErrEmptyTranscript", err)
			}
		})
	}
}

// TestGeminiDuplicatePathsGetDistinctIdentities verifies the project-name and
// project-hash copies of one Gemini session get distinct, path-derived source
// identities instead of colliding on the shared basename (round-6 Issue 6).
func TestGeminiDuplicatePathsGetDistinctIdentities(t *testing.T) {
	root := t.TempDir()
	body := `{"messages":[{"author":"user","content":"%s"}]}`
	paths := map[string]string{
		filepath.Join(root, "castle-steward", "chats", "session-X.json"): "name-copy text",
		filepath.Join(root, "hash123abc", "chats", "session-X.json"):     "hash-copy text",
	}
	var ids []string
	for p, text := range paths {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(strings.Replace(body, "%s", text, 1)), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		session, _, err := ParseTranscriptFile("gemini", p)
		if err != nil {
			t.Fatalf("ParseTranscriptFile(%s) error = %v", p, err)
		}
		ids = append(ids, session.SourceSessionID)
	}
	if ids[0] == ids[1] {
		t.Fatalf("expected distinct source ids for divergent duplicate paths, both = %q", ids[0])
	}
	for _, id := range ids {
		if !strings.HasSuffix(id, "chats/session-X") {
			t.Fatalf("source id %q should be path-derived (chats/session-X suffix)", id)
		}
	}
}
