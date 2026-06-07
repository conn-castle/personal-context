package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/repository"
)

// runChatImportAgent imports one agent from root and returns the parsed summary
// plus captured stderr (for warning assertions).
func runChatImportAgent(t *testing.T, agent string, root string) (chatImportSummary, string) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", agent, "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import %s: %v\nstderr=%s", agent, err, stderr.String())
	}
	var summary chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse import summary: %v\n%s", err, stdout.String())
	}
	return summary, stderr.String()
}

func chatListJSON(t *testing.T, args ...string) []chatSessionJSON {
	t.Helper()
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs(append([]string{"chat", "list", "--format", "json"}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat list %v: %v", args, err)
	}
	var page struct {
		Items []chatSessionJSON `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &page); err != nil {
		t.Fatalf("parse chat list json: %v\n%s", err, stdout.String())
	}
	return page.Items
}

// TestChatImportSkipsEmptyTranscriptFiles verifies metadata-only and empty
// transcript files are counted as scanned but never create chat sessions
// (round-6 Issue 4).
func TestChatImportSkipsEmptyTranscriptFiles(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	projectDir := filepath.Join(root, "-Users-me-repo")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	files := map[string]string{
		"zero.jsonl":   "",
		"marker.jsonl": `{"type":"last-prompt","leafUuid":"x","sessionId":"y"}` + "\n",
		"real.jsonl":   `{"type":"assistant","sessionId":"real-claude","timestamp":"2026-05-17T10:00:00Z","message":{"role":"assistant","content":"a real message"}}` + "\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(projectDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	summary, _ := runChatImportAgent(t, "claude", root)
	if summary.FilesScanned != 3 {
		t.Fatalf("FilesScanned = %d, want 3", summary.FilesScanned)
	}
	if summary.SessionsCreated != 1 {
		t.Fatalf("SessionsCreated = %d, want 1 (only the non-empty transcript)", summary.SessionsCreated)
	}
	sessions := chatListJSON(t)
	if len(sessions) != 1 || sessions[0].SourceSessionID != "real-claude" {
		t.Fatalf("expected one non-empty session, got %+v", sessions)
	}
}

func TestChatImportSkipsMalformedFileAndContinues(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	goodPath := filepath.Join(root, "a-good.jsonl")
	badPath := filepath.Join(root, "b-bad.json")
	good := strings.Join([]string{
		`{"timestamp":"2026-06-04T12:00:00Z","type":"session_meta","payload":{"id":"good-codex"}}`,
		`{"timestamp":"2026-06-04T12:00:01Z","type":"response_item","payload":{"role":"user","content":"surviving transcript"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(goodPath, []byte(good), 0o644); err != nil {
		t.Fatalf("write good transcript: %v", err)
	}
	if err := os.WriteFile(badPath, []byte(`{"messages":[`), 0o644); err != nil {
		t.Fatalf("write malformed transcript: %v", err)
	}

	summary, stderr := runChatImportAgent(t, "codex", root)
	if summary.FilesScanned != 2 || summary.FilesSkipped != 1 || summary.SessionsCreated != 1 || summary.ItemsImported != 1 {
		t.Fatalf("summary = %+v, want scanned/skipped/created/items = 2/1/1/1", summary)
	}
	if !strings.Contains(stderr, "Skipped (") || !strings.Contains(stderr, badPath) {
		t.Fatalf("expected skipped-file warning for %s, got %q", badPath, stderr)
	}
	sessions := chatListJSON(t)
	if len(sessions) != 1 || sessions[0].SourceSessionID != "good-codex" {
		t.Fatalf("expected only good transcript imported, got %+v", sessions)
	}
}

func TestChatImportCodexForksBecomeDistinctSessions(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	parentID := "019d7dea-parent"
	forkA := "019d7deb-fork-a"
	forkB := "019d7dec-fork-b"
	writeCodexRollout := func(name string, lines []string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeCodexRollout("a-parent.jsonl", []string{
		`{"timestamp":"2026-06-04T12:00:00Z","type":"session_meta","payload":{"id":"` + parentID + `"}}`,
		`{"timestamp":"2026-06-04T12:00:01Z","type":"response_item","payload":{"role":"user","content":"parent turn"}}`,
	})
	writeFork := func(name string, forkID string, text string) {
		t.Helper()
		writeCodexRollout(name, []string{
			`{"timestamp":"2026-06-04T12:00:00Z","type":"session_meta","payload":{"id":"` + forkID + `","forked_from_id":"` + parentID + `"}}`,
			`{"timestamp":"2026-06-04T12:00:01Z","type":"session_meta","payload":{"id":"` + parentID + `","forked_from_id":null}}`,
			`{"timestamp":"2026-06-04T12:00:02Z","type":"response_item","payload":{"role":"assistant","content":"` + text + `"}}`,
		})
	}
	writeFork("b-fork-a.jsonl", forkA, "fork a turn")
	writeFork("c-fork-b.jsonl", forkB, "fork b turn")

	summary, stderr := runChatImportAgent(t, "codex", root)
	if summary.SessionsCreated != 3 || summary.CollisionsSkipped != 0 || summary.FilesSkipped != 0 {
		t.Fatalf("summary = %+v, stderr=%q; want 3 created and no collisions/skips", summary, stderr)
	}
	sessions := chatListJSON(t)
	bySourceID := map[string]chatSessionJSON{}
	for _, session := range sessions {
		bySourceID[session.SourceSessionID] = session
	}
	for _, sourceID := range []string{parentID, forkA, forkB} {
		if _, ok := bySourceID[sourceID]; !ok {
			t.Fatalf("missing source session %q in %+v", sourceID, sessions)
		}
	}
	for _, forkID := range []string{forkA, forkB} {
		session := bySourceID[forkID]
		if session.ParentSourceSessionID == nil || *session.ParentSourceSessionID != parentID {
			t.Fatalf("fork %q parent = %v, want %q", forkID, session.ParentSourceSessionID, parentID)
		}
	}
	if bySourceID[parentID].ParentSourceSessionID != nil {
		t.Fatalf("parent session should not have fork lineage, got %+v", bySourceID[parentID])
	}
}

// TestChatImportClaudeSubagentsBecomeDistinctSessions verifies a parent
// transcript plus two subagent transcripts import as one parent and two
// distinct subagent sessions, and that parent/subagent navigation works through
// list/search/show (round-6 Issue 1).
func TestChatImportClaudeSubagentsBecomeDistinctSessions(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	parent := "2dbd6fef-2d8a-4ba9-9eef-32585b350874"
	projDir := filepath.Join(root, "proj")
	subDir := filepath.Join(projDir, parent, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	parentFile := filepath.Join(projDir, parent+".jsonl")
	parentRow := `{"type":"assistant","sessionId":"` + parent + `","timestamp":"2026-05-17T10:00:00Z","message":{"role":"assistant","content":"parent transcript text"}}` + "\n"
	if err := os.WriteFile(parentFile, []byte(parentRow), 0o644); err != nil {
		t.Fatalf("write parent: %v", err)
	}
	subRow := func(text string) string {
		return `{"type":"assistant","isSidechain":true,"sessionId":"` + parent + `","timestamp":"2026-05-17T10:01:00Z","message":{"role":"assistant","content":"` + text + `"}}` + "\n"
	}
	if err := os.WriteFile(filepath.Join(subDir, "agent-aaa.jsonl"), []byte(subRow("alpha subagent needle")), 0o644); err != nil {
		t.Fatalf("write sub a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "agent-bbb.jsonl"), []byte(subRow("beta subagent needle")), 0o644); err != nil {
		t.Fatalf("write sub b: %v", err)
	}

	summary, _ := runChatImportAgent(t, "claude", root)
	if summary.SessionsCreated != 3 {
		t.Fatalf("SessionsCreated = %d, want 3 (parent + 2 subagents)", summary.SessionsCreated)
	}

	// list --parent-source-session-id returns exactly the two subagents.
	children := chatListJSON(t, "--parent-source-session-id", parent)
	if len(children) != 2 {
		t.Fatalf("parent filter returned %d sessions, want 2: %+v", len(children), children)
	}
	var parentID string
	for _, c := range children {
		if c.ParentSourceSessionID == nil || *c.ParentSourceSessionID != parent {
			t.Fatalf("subagent %+v missing parent metadata", c)
		}
	}
	for _, s := range chatListJSON(t) {
		if s.SourceSessionID == parent {
			parentID = s.ID
		}
	}
	if parentID == "" {
		t.Fatal("parent session not found in list")
	}

	// search --parent-source-session-id restricts to subagents.
	searchOut := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: searchOut, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "search", "--parent-source-session-id", parent, "needle"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat search: %v", err)
	}
	if !strings.Contains(searchOut.String(), "alpha") && !strings.Contains(searchOut.String(), children[0].ID) {
		t.Fatalf("expected subagent hits in parent-scoped search, got %q", searchOut.String())
	}

	// show on the parent lists its subagents in text and JSON.
	showOut := &bytes.Buffer{}
	cmd = NewRootCommand(RootCommandOptions{Stdout: showOut, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", parentID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat show: %v", err)
	}
	if !strings.Contains(showOut.String(), "Subagents:       2") {
		t.Fatalf("expected subagent count in parent show text, got %q", showOut.String())
	}

	jsonOut := &bytes.Buffer{}
	cmd = NewRootCommand(RootCommandOptions{Stdout: jsonOut, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", parentID, "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat show json: %v", err)
	}
	var shown struct {
		Session   chatSessionJSON    `json:"session"`
		Subagents []chatRelationJSON `json:"subagents"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &shown); err != nil {
		t.Fatalf("parse show json: %v\n%s", err, jsonOut.String())
	}
	if len(shown.Subagents) != 2 {
		t.Fatalf("show json subagents = %d, want 2", len(shown.Subagents))
	}

	// Showing a subagent surfaces its parent in the text transcript.
	subShow := &bytes.Buffer{}
	cmd = NewRootCommand(RootCommandOptions{Stdout: subShow, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", children[0].ID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat show subagent: %v", err)
	}
	if !strings.Contains(subShow.String(), "Parent Session:") || !strings.Contains(subShow.String(), parent) {
		t.Fatalf("expected parent session line in subagent show, got %q", subShow.String())
	}
}

func TestChatShowSubagentsAreScopedBySource(t *testing.T) {
	homeDir := setupEnv(t)
	parentSID := "shared-source-session"
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	stack, err := openLocalStack(homeDir)
	if err != nil {
		t.Fatalf("openLocalStack: %v", err)
	}
	defer func() { _ = stack.Close() }()

	if _, _, err := stack.Repo.UpsertChatSession(context.Background(), repository.UpsertChatSessionInput{
		CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:              "20260528-aaaa1001",
			Source:          "claude_code",
			SourceSessionID: parentSID,
			SourceDeviceID:  "test-device",
			StartedAt:       now,
			LastActivityAt:  now,
			CreatedAt:       &now,
			UpdatedAt:       &now,
		},
		ClearDeleted: true,
	}); err != nil {
		t.Fatalf("upsert parent: %v", err)
	}
	if _, _, err := stack.Repo.UpsertChatSession(context.Background(), repository.UpsertChatSessionInput{
		CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:                    "20260528-bbbb1001",
			Source:                "codex",
			SourceSessionID:       "codex-child",
			ParentSourceSessionID: &parentSID,
			SourceDeviceID:        "test-device",
			StartedAt:             now,
			LastActivityAt:        now,
			CreatedAt:             &now,
			UpdatedAt:             &now,
		},
		ClearDeleted: true,
	}); err != nil {
		t.Fatalf("upsert cross-source child: %v", err)
	}

	out := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: out, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", "20260528-aaaa1001"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat show: %v", err)
	}
	if strings.Contains(out.String(), "Subagents:") || strings.Contains(out.String(), "20260528-bbbb1001") {
		t.Fatalf("chat show should not list cross-source child sessions, got %q", out.String())
	}
}

// TestChatImportGeminiDuplicatePaths verifies divergent same-basename copies get
// distinct sessions (never overwrite) while byte-identical copies are collapsed
// (round-6 Issue 6).
func TestChatImportGeminiDuplicatePaths(t *testing.T) {
	setupEnv(t)

	divergent := t.TempDir()
	writeGeminiCopy(t, filepath.Join(divergent, "castle-steward", "chats", "session-X.json"), "name divergent content")
	writeGeminiCopy(t, filepath.Join(divergent, "hash123", "chats", "session-X.json"), "hash divergent content")
	summary, _ := runChatImportAgent(t, "gemini", divergent)
	if summary.SessionsCreated != 2 || summary.DuplicatesSkipped != 0 {
		t.Fatalf("divergent gemini copies: SessionsCreated=%d DuplicatesSkipped=%d, want 2 / 0", summary.SessionsCreated, summary.DuplicatesSkipped)
	}

	identical := t.TempDir()
	writeGeminiCopy(t, filepath.Join(identical, "castle-steward", "chats", "session-Y.json"), "identical content")
	writeGeminiCopy(t, filepath.Join(identical, "hash456", "chats", "session-Y.json"), "identical content")
	summary2, _ := runChatImportAgent(t, "gemini", identical)
	if summary2.SessionsCreated != 1 || summary2.DuplicatesSkipped != 1 {
		t.Fatalf("identical gemini copies: SessionsCreated=%d DuplicatesSkipped=%d, want 1 / 1", summary2.SessionsCreated, summary2.DuplicatesSkipped)
	}
}

func TestChatImportGeminiDuplicateMergePreservesResolvedCWD(t *testing.T) {
	setupEnv(t)
	projectPath := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("mkdir project path: %v", err)
	}
	normalizedProjectPath, err := normalizeProjectPath(projectPath)
	if err != nil {
		t.Fatalf("normalize project path: %v", err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "register", "chat/gemini-duplicate", normalizedProjectPath, "--device", "test-device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project register: %v", err)
	}

	root := t.TempDir()
	writeGeminiCopy(t, filepath.Join(root, "0hash-first", "chats", "session-C.json"), "shared attribution content")
	namedDir := filepath.Join(root, "project-name-second")
	writeGeminiCopy(t, filepath.Join(namedDir, "chats", "session-C.json"), "shared attribution content")
	if err := os.WriteFile(filepath.Join(namedDir, ".project_root"), []byte(normalizedProjectPath+"\n"), 0o644); err != nil {
		t.Fatalf("write .project_root: %v", err)
	}

	summary, _ := runChatImportAgent(t, "gemini", root)
	if summary.SessionsCreated != 1 || summary.DuplicatesSkipped != 1 {
		t.Fatalf("duplicate import summary = %+v, want one retained session and one duplicate", summary)
	}
	if summary.DiskSessionsFound != 1 || summary.DiskSessionsStored != 1 || summary.CoverageShortfall != 0 {
		t.Fatalf("coverage summary = %+v, want 1 found / 1 stored / 0 shortfall", summary)
	}
	sessions := chatListJSON(t, "--project", "chat/gemini-duplicate")
	if len(sessions) != 1 {
		t.Fatalf("expected duplicate representative attributed to project, got %+v", sessions)
	}
	if sessions[0].CWD == nil || *sessions[0].CWD != normalizedProjectPath {
		t.Fatalf("representative cwd = %v, want %q", sessions[0].CWD, normalizedProjectPath)
	}
}

// TestChatImportGeminiDuplicateMergeBackfillsStoredRepresentative covers the
// merge path where the byte-identical representative is an already-stored
// CWD-null session (not a pending batch entry). A later duplicate scanned in a
// second run carries a resolvable cwd and must backfill the stored row through
// queueReplaceChatImport, the actual DB-write merge branch that
// mergePendingAttribution cannot reach.
func TestChatImportGeminiDuplicateMergeBackfillsStoredRepresentative(t *testing.T) {
	setupEnv(t)
	projectPath := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("mkdir project path: %v", err)
	}
	normalizedProjectPath, err := normalizeProjectPath(projectPath)
	if err != nil {
		t.Fatalf("normalize project path: %v", err)
	}

	// First run: import the representative copy alone with no resolvable cwd
	// (no .project_root, project not registered), so it stores with cwd=NULL.
	root := t.TempDir()
	firstDir := filepath.Join(root, "0hash-first")
	writeGeminiCopy(t, filepath.Join(firstDir, "chats", "session-E.json"), "stored merge content")
	first, _ := runChatImportAgent(t, "gemini", root)
	if first.SessionsCreated != 1 {
		t.Fatalf("first import = %+v, want one created session", first)
	}
	stored := chatListJSON(t)
	if len(stored) != 1 || stored[0].CWD != nil {
		t.Fatalf("first import should store a single cwd-null session, got %+v", stored)
	}

	// Register the project and add a byte-identical duplicate under a different
	// tmp dir that resolves the cwd via .project_root.
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "register", "chat/gemini-stored-merge", normalizedProjectPath, "--device", "test-device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project register: %v", err)
	}
	namedDir := filepath.Join(root, "project-name-second")
	writeGeminiCopy(t, filepath.Join(namedDir, "chats", "session-E.json"), "stored merge content")
	if err := os.WriteFile(filepath.Join(namedDir, ".project_root"), []byte(normalizedProjectPath+"\n"), 0o644); err != nil {
		t.Fatalf("write .project_root: %v", err)
	}

	second, _ := runChatImportAgent(t, "gemini", root)
	// The stored representative is skipped (unchanged), and the duplicate merges
	// into it via queueReplaceChatImport rather than creating a new session.
	if second.SessionsCreated != 0 || second.DuplicatesSkipped != 1 {
		t.Fatalf("second import = %+v, want zero created and one duplicate merged", second)
	}
	if second.DiskSessionsFound != 1 || second.DiskSessionsStored != 1 || second.CoverageShortfall != 0 {
		t.Fatalf("coverage summary = %+v, want 1 found / 1 stored / 0 shortfall", second)
	}
	sessions := chatListJSON(t, "--project", "chat/gemini-stored-merge")
	if len(sessions) != 1 {
		t.Fatalf("expected stored representative backfilled with project attribution, got %+v", sessions)
	}
	if sessions[0].CWD == nil || *sessions[0].CWD != normalizedProjectPath {
		t.Fatalf("stored representative cwd = %v, want %q", sessions[0].CWD, normalizedProjectPath)
	}
}

func TestChatImportGeminiDuplicatePathsStayCollapsedAcrossRuns(t *testing.T) {
	setupEnv(t)

	root := t.TempDir()
	namePath := filepath.Join(root, "castle-steward", "chats", "session-Z.json")
	hashPath := filepath.Join(root, "hash789", "chats", "session-Z.json")
	writeGeminiCopy(t, namePath, "stable duplicate content")
	writeGeminiCopy(t, hashPath, "stable duplicate content")

	first, _ := runChatImportAgent(t, "gemini", root)
	if first.SessionsCreated != 1 || first.DuplicatesSkipped != 1 {
		t.Fatalf("first import should collapse identical copies, got %+v", first)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "gemini", "--root", root, "--delete-source"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("second chat import: %v", err)
	}
	var second chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &second); err != nil {
		t.Fatalf("parse second import summary: %v\n%s", err, stdout.String())
	}
	if second.SessionsCreated != 0 || second.SessionsSkipped != 1 || second.DuplicatesSkipped != 1 || second.SourcesDeleted != 2 {
		t.Fatalf("second import should skip existing representative and duplicate copy, got %+v", second)
	}
	for _, path := range []string{namePath, hashPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s deleted with --delete-source, stat err = %v", path, err)
		}
	}
}

func TestApplyChatImportCoverageReportsShortfall(t *testing.T) {
	expected := map[chatImportCoverageKey]struct{}{
		{source: "codex", sourceSessionID: "stored"}:  {},
		{source: "codex", sourceSessionID: "missing"}: {},
	}
	repo := &mockRepo{
		getChatBySourceFn: func(_ context.Context, _ string, sourceSessionID string) (repository.ChatSession, error) {
			if sourceSessionID == "stored" {
				return repository.ChatSession{ID: "20260606-aaaabbbb"}, nil
			}
			return repository.ChatSession{}, repository.ErrNotFound
		},
	}
	summary := chatImportSummary{}
	if err := applyChatImportCoverage(context.Background(), repo, &summary, expected); err != nil {
		t.Fatalf("applyChatImportCoverage() error = %v", err)
	}
	if summary.DiskSessionsFound != 2 || summary.DiskSessionsStored != 1 || summary.CoverageShortfall != 1 {
		t.Fatalf("coverage summary = %+v, want 2 found / 1 stored / 1 shortfall", summary)
	}
}

func TestScanChatImportCoverageCountsUniqueParseableDiskSessions(t *testing.T) {
	root := t.TempDir()
	body := `{"id":"coverage-session","started_at":"2026-06-06T12:00:00Z","messages":[{"role":"user","content":"count me once"}]}`
	writeTestChatTranscript(t, root, "one.json", body)
	writeTestChatTranscript(t, root, "duplicate.json", body)
	writeTestChatTranscript(t, root, "bad.json", `{not-json`)

	expected, err := scanChatImportCoverage(context.Background(), map[string][]string{"codex": {root}})
	if err != nil {
		t.Fatalf("scanChatImportCoverage() error = %v", err)
	}
	if len(expected) != 1 {
		t.Fatalf("scan coverage expected %d unique parseable sessions, want 1: %+v", len(expected), expected)
	}
	if _, ok := expected[chatImportCoverageKey{source: "codex", sourceSessionID: "coverage-session"}]; !ok {
		t.Fatalf("scan coverage did not include codex/coverage-session: %+v", expected)
	}
}

func TestScanChatImportCoverageStopsAfterCancellation(t *testing.T) {
	root := t.TempDir()
	writeTestChatTranscript(t, root, "session.json", `{"id":"canceled-session","started_at":"2026-06-06T12:00:00Z","messages":[{"role":"user","content":"stop"}]}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	expected, err := scanChatImportCoverage(ctx, map[string][]string{"codex": {root}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scanChatImportCoverage(canceled) error = %v, want context.Canceled", err)
	}
	if expected != nil {
		t.Fatalf("scanChatImportCoverage(canceled) expected = %+v, want nil", expected)
	}
}

func TestApplyChatImportCoveragePropagatesCancellationAndStoreErrors(t *testing.T) {
	expected := map[chatImportCoverageKey]struct{}{
		{source: "codex", sourceSessionID: "blocked"}: {},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := &mockRepo{
		getChatBySourceFn: func(context.Context, string, string) (repository.ChatSession, error) {
			t.Fatal("GetChatSessionBySource should not run after context cancellation")
			return repository.ChatSession{}, nil
		},
	}
	if err := applyChatImportCoverage(ctx, repo, &chatImportSummary{}, expected); !errors.Is(err, context.Canceled) {
		t.Fatalf("applyChatImportCoverage(canceled) error = %v, want context.Canceled", err)
	}

	storeErr := errors.New("store unavailable")
	repo = &mockRepo{
		getChatBySourceFn: func(context.Context, string, string) (repository.ChatSession, error) {
			return repository.ChatSession{}, storeErr
		},
	}
	err := applyChatImportCoverage(context.Background(), repo, &chatImportSummary{}, expected)
	if !errors.Is(err, storeErr) || !strings.Contains(err.Error(), "check chat import coverage for codex/blocked") {
		t.Fatalf("applyChatImportCoverage(store error) = %v, want wrapped coverage lookup error", err)
	}
}

func TestMergeDuplicateChatImportAttributionReportsStageFailure(t *testing.T) {
	fsClient, err := filesystem.NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("filesystem client: %v", err)
	}
	cwd := "/workspace/project"
	queued, merged, err := mergeDuplicateChatImportAttribution(
		context.Background(),
		&localStack{FS: fsClient},
		&chatImportBatch{},
		&chatImportSessionIndex{},
		chatImportContentRepresentative{chatID: "20260606-aabbccdd", source: "gemini", sourceSessionID: "representative-session"},
		repository.CreateChatSessionInput{Source: "gemini", CWD: &cwd},
		nil,
		filepath.Join(t.TempDir(), "missing.json"),
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "stage chat source") {
		t.Fatalf("mergeDuplicateChatImportAttribution(stage failure) error = %v, want stage error", err)
	}
	if queued || merged {
		t.Fatalf("mergeDuplicateChatImportAttribution(stage failure) queued=%t merged=%t, want both false", queued, merged)
	}
}

func TestMergePendingAttributionBackfillsOnlyMatchingRepresentative(t *testing.T) {
	cwd := "/workspace/project"
	projectID := "chat/project"
	otherCWD := "/workspace/other"
	batch := chatImportBatch{entries: []pendingChatImport{
		{op: repository.ChatImportOp{Session: repository.UpsertChatSessionInput{CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:              "20260606-other",
			Source:          "gemini",
			SourceSessionID: "other-session",
			CWD:             &otherCWD,
		}}}},
		{op: repository.ChatImportOp{Session: repository.UpsertChatSessionInput{CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:              "20260606-representative",
			Source:          "gemini",
			SourceSessionID: "representative-session",
		}}}},
	}}

	merged := batch.mergePendingAttribution(chatImportContentRepresentative{
		chatID:          "20260606-representative",
		source:          "gemini",
		sourceSessionID: "representative-session",
	}, repository.CreateChatSessionInput{CWD: &cwd, ProjectID: &projectID})
	if !merged {
		t.Fatal("mergePendingAttribution() = false, want true for queued representative")
	}
	representative := batch.entries[1].op.Session.CreateChatSessionInput
	if representative.CWD == nil || *representative.CWD != cwd {
		t.Fatalf("representative CWD = %v, want %q", representative.CWD, cwd)
	}
	if representative.ProjectID == nil || *representative.ProjectID != projectID {
		t.Fatalf("representative project_id = %v, want %q", representative.ProjectID, projectID)
	}
	if got := batch.entries[0].op.Session.CWD; got == nil || *got != otherCWD {
		t.Fatalf("non-representative CWD = %v, want unchanged %q", got, otherCWD)
	}
	if batch.mergePendingAttribution(chatImportContentRepresentative{
		chatID:          "20260606-missing",
		source:          "gemini",
		sourceSessionID: "missing-session",
	}, repository.CreateChatSessionInput{CWD: &cwd}) {
		t.Fatal("mergePendingAttribution() = true for missing representative, want false")
	}
}

func TestChatImportContentHashesAreScopedBySource(t *testing.T) {
	homeDir := setupEnv(t)
	t.Setenv("HOME", homeDir)

	body := []byte(`{"id":"shared-bytes","started_at":"2026-05-17T10:00:00Z","messages":[{"role":"user","author":"user","content":"same bytes across agents"}]}`)
	codexPath := filepath.Join(homeDir, ".codex", "sessions", "shared.json")
	geminiPath := filepath.Join(homeDir, ".gemini", "tmp", "castle-steward", "chats", "shared.json")
	for _, path := range []string{codexPath, geminiPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import all sources: %v", err)
	}
	var summary chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse import summary: %v\n%s", err, stdout.String())
	}
	if summary.SessionsCreated != 2 || summary.DuplicatesSkipped != 0 {
		t.Fatalf("byte-identical files from different sources must both import, got %+v", summary)
	}
}

func TestChatImportOverlappingRootsFlushPendingSessionBeforeDuplicate(t *testing.T) {
	setupEnv(t)

	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	transcriptPath := filepath.Join(nested, "overlap.json")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested root: %v", err)
	}
	if err := os.WriteFile(transcriptPath, []byte(`{"id":"overlap","started_at":"2026-05-17T10:00:00Z","messages":[{"role":"user","content":"same file through two roots"}]}`), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root, "--root", nested})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import overlapping roots: %v", err)
	}
	var summary chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse import summary: %v\n%s", err, stdout.String())
	}
	if summary.SessionsCreated != 1 || summary.SessionsSkipped != 1 || summary.ItemsImported != 1 {
		t.Fatalf("overlapping roots should import once and skip the duplicate scan, got %+v", summary)
	}
}

// TestChatImportByteIdenticalCollisionSkipsAndDeletes verifies that a second
// file colliding on the same identity with byte-identical content is treated as
// an unchanged skip (not a divergent collision), and that --delete-source
// reclaims the redundant source.
func TestChatImportByteIdenticalCollisionSkipsAndDeletes(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	body := `{"id":"dup-codex","started_at":"2026-05-17T10:00:00Z","messages":[{"role":"user","content":"identical content"}]}`
	aPath := filepath.Join(root, "a.json")
	bPath := filepath.Join(root, "b.json")
	for _, p := range []string{aPath, bPath} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root, "--delete-source"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import: %v", err)
	}
	var summary chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary: %v\n%s", err, stdout.String())
	}
	if summary.SessionsCreated != 1 || summary.SessionsSkipped != 1 || summary.CollisionsSkipped != 0 {
		t.Fatalf("byte-identical collision: SessionsCreated=%d SessionsSkipped=%d CollisionsSkipped=%d, want 1/1/0: %+v", summary.SessionsCreated, summary.SessionsSkipped, summary.CollisionsSkipped, summary)
	}
	if _, err := os.Stat(bPath); !os.IsNotExist(err) {
		t.Fatalf("expected colliding source b.json deleted with --delete-source, stat err = %v", err)
	}
}

func writeGeminiCopy(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `{"messages":[{"author":"user","content":"` + text + `"}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestChatImportDivergentCollisionWarnsAndSkips verifies that two distinct files
// resolving to the same (source, source_session_id) with divergent content do
// not overwrite each other: the second is reported and skipped (Fix B).
func TestChatImportDivergentCollisionWarnsAndSkips(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	// Both files declare the same internal id but live at different paths with
	// different content. (a.json sorts before b.json.)
	if err := os.WriteFile(filepath.Join(root, "a.json"), []byte(`{"id":"shared-codex","started_at":"2026-05-17T10:00:00Z","messages":[{"role":"user","content":"alpha original content"}]}`), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.json"), []byte(`{"id":"shared-codex","started_at":"2026-05-17T10:00:00Z","messages":[{"role":"user","content":"beta colliding content"}]}`), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	summary, stderr := runChatImportAgent(t, "codex", root)
	if summary.SessionsCreated != 1 || summary.CollisionsSkipped != 1 {
		t.Fatalf("collision summary: SessionsCreated=%d CollisionsSkipped=%d, want 1 / 1: %+v", summary.SessionsCreated, summary.CollisionsSkipped, summary)
	}
	if !strings.Contains(stderr, "not overwriting") || !strings.Contains(stderr, "shared-codex") {
		t.Fatalf("expected collision warning naming the sid, got stderr=%q", stderr)
	}
	// The first file's content survives; the colliding file never replaced it.
	sessions := chatListJSON(t)
	if len(sessions) != 1 {
		t.Fatalf("expected exactly one session after collision, got %d", len(sessions))
	}
	showOut := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: showOut, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", sessions[0].ID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat show: %v", err)
	}
	if !strings.Contains(showOut.String(), "alpha original content") || strings.Contains(showOut.String(), "beta colliding content") {
		t.Fatalf("collision must preserve original content, got %q", showOut.String())
	}
}
