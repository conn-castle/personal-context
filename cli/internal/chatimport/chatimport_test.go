package chatimport

import (
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
	for source, suffix := range map[string]string{
		"codex":       filepath.Join(".codex", "sessions"),
		"claude_code": ".claude",
		"gemini":      ".gemini",
	} {
		roots := allProjectRoots[source]
		if len(roots) < 2 || !strings.HasSuffix(roots[len(roots)-1], suffix) {
			t.Fatalf("expected %s project root suffix %q, got %+v", source, suffix, roots)
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
