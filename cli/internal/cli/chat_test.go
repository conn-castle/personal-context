package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestChatImportListSearchShowDelete(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "session-1.json")
	transcript := `{
  "id": "session-1",
  "cwd": "/tmp/unassigned-chat",
  "title": "Chat title",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [
    {"role": "user", "content": "hello chat needle"},
    {"role": "assistant", "content": "assistant reply"}
  ]
}`
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import: %v", err)
	}
	var importSummary chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &importSummary); err != nil {
		t.Fatalf("parse first import summary: %v\n%s", err, stdout.String())
	}
	if importSummary.SessionsCreated != 1 || importSummary.ItemsCreated != 2 {
		t.Fatalf("unexpected first import summary: %+v", importSummary)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import update: %v", err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &importSummary); err != nil {
		t.Fatalf("parse second import summary: %v\n%s", err, stdout.String())
	}
	if importSummary.SessionsUpdated != 1 || importSummary.ItemsCreated != 0 {
		t.Fatalf("unexpected second import summary: %+v", importSummary)
	}
	updatedTranscript := `{
  "id": "session-1",
  "cwd": "/tmp/unassigned-chat",
  "title": "Chat title",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [
    {"role": "user", "content": "replacement chat needle"}
  ]
}`
	if err := os.WriteFile(transcriptPath, []byte(updatedTranscript), 0o644); err != nil {
		t.Fatalf("write updated transcript: %v", err)
	}
	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import replacement: %v", err)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "list", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat list: %v", err)
	}
	var page struct {
		Items []chatSessionJSON `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &page); err != nil {
		t.Fatalf("parse chat list json: %v\n%s", err, stdout.String())
	}
	if len(page.Items) != 1 || page.Items[0].SourceSessionID != "session-1" {
		t.Fatalf("unexpected chat list: %+v", page.Items)
	}
	chatID := page.Items[0].ID

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "search", "needle"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat search: %v", err)
	}
	if !strings.Contains(stdout.String(), chatID) {
		t.Fatalf("expected chat id in search output, got %q", stdout.String())
	}
	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "search", "--project", "missing-project", "needle"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat search project filter: %v", err)
	}
	if !strings.Contains(stdout.String(), "No matching chats found.") {
		t.Fatalf("expected empty project-filtered chat search, got %q", stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat show: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "replacement chat needle") || strings.Contains(out, "assistant reply") {
		t.Fatalf("expected replaced transcript text, got %q", out)
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "delete", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat delete: %v", err)
	}
	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "delete", "missing-chat"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected deleting a missing chat to fail")
	}
}

func TestChatImportDefaultScanIncludesRegisteredClaudeConfigProjectRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setupEnv(t)
	projectPath := t.TempDir()
	normalizedProjectPath, err := normalizeProjectPath(projectPath)
	if err != nil {
		t.Fatalf("normalize project path: %v", err)
	}
	transcriptRoot := filepath.Join(normalizedProjectPath, ".claude-config", "projects")
	if err := os.MkdirAll(transcriptRoot, 0o700); err != nil {
		t.Fatalf("create claude config transcript root: %v", err)
	}
	claudeConfigRoot := filepath.Join(normalizedProjectPath, ".claude")
	if err := os.MkdirAll(filepath.Join(claudeConfigRoot, "worktrees", "example"), 0o700); err != nil {
		t.Fatalf("create claude config root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeConfigRoot, "settings.json"), []byte(`{"permissions":{"allow":[]}}`), 0o644); err != nil {
		t.Fatalf("write claude settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeConfigRoot, "worktrees", "example", "manifest.json"), []byte(`{"not":"a transcript"}`), 0o644); err != nil {
		t.Fatalf("write claude worktree json: %v", err)
	}
	geminiConfigRoot := filepath.Join(normalizedProjectPath, ".gemini", "antigravity-cli", "cache")
	if err := os.MkdirAll(geminiConfigRoot, 0o700); err != nil {
		t.Fatalf("create gemini config root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(geminiConfigRoot, "onboarding.json"), []byte(`{"not":"a transcript"}`), 0o644); err != nil {
		t.Fatalf("write gemini config json: %v", err)
	}
	transcriptPath := filepath.Join(transcriptRoot, "default-claude-config.jsonl")
	cwd := filepath.ToSlash(filepath.Join(normalizedProjectPath, "nested"))
	lines := []string{
		`{"type":"user","timestamp":"2026-05-18T12:00:00.000Z","cwd":"` + cwd + `","sessionId":"default-claude-config","message":{"role":"user","content":[{"type":"text","text":"registered claude config needle"}]}}`,
		``,
	}
	if err := os.WriteFile(transcriptPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "register", "chat/default-scan", projectPath, "--device", "test-device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project register path: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("default chat import: %v", err)
	}
	var summary chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse import summary: %v\n%s", err, stdout.String())
	}
	if summary.FilesScanned != 1 || summary.SessionsCreated != 1 || summary.ItemsCreated != 1 {
		t.Fatalf("unexpected import summary: %+v", summary)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "list", "--format", "json", "--project", "chat/default-scan"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat list: %v", err)
	}
	var page struct {
		Items []chatSessionJSON `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &page); err != nil {
		t.Fatalf("parse chat list json: %v\n%s", err, stdout.String())
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected one imported chat, got %+v", page.Items)
	}
	got := page.Items[0]
	if got.Source != "claude_code" || got.SourceSessionID != "default-claude-config" {
		t.Fatalf("unexpected imported session: %+v", got)
	}
	if got.ProjectID == nil || *got.ProjectID != "chat/default-scan" {
		t.Fatalf("expected project assignment, got %+v", got.ProjectID)
	}
	if got.OriginalSourcePath == nil {
		t.Fatal("expected original source path")
	}
	if !strings.HasPrefix(*got.OriginalSourcePath, transcriptRoot+string(os.PathSeparator)) {
		t.Fatalf("expected original source under %q, got %q", transcriptRoot, *got.OriginalSourcePath)
	}
}

// TestChatSearchRealCodexEnvelopeContract is the end-to-end contract for
// `pc chat search --format json` against a real-shape codex rollout. The
// rollout uses the `{type:"response_item",payload:{...}}` envelope; before
// the envelope fix, search would silently return zero results because items
// had empty SearchText. This test now asserts the
// envelope shape (items[], total, next_cursor), confirms hits include the
// session, and verifies items carry the original role and non-empty text
// snippet.
func TestChatSearchRealCodexEnvelopeContract(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	rolloutPath := filepath.Join(root, "rollout-real.jsonl")
	lines := []string{
		`{"timestamp":"2026-01-05T21:25:33.024Z","type":"session_meta","payload":{"id":"contract-codex","cwd":"/tmp/contract","timestamp":"2026-01-05T21:25:33.002Z"}}`,
		`{"timestamp":"2026-01-05T21:25:34.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"please find the haystack token"}]}}`,
		`{"timestamp":"2026-01-05T21:25:35.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"sure, here is the haystack"}]}}`,
	}
	if err := os.WriteFile(rolloutPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "search", "--format", "json", "--limit", "10", "haystack"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat search: %v", err)
	}

	var page struct {
		Items      []chatSearchJSON `json:"items"`
		Total      int              `json:"total"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &page); err != nil {
		t.Fatalf("decode envelope: %v (body=%q)", err, stdout.String())
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 hits (both items match 'haystack'), got %d (body=%q)", len(page.Items), stdout.String())
	}
	if page.Total != 2 {
		t.Fatalf("expected total=2, got %d", page.Total)
	}
	if page.NextCursor != nil {
		t.Fatalf("expected no next_cursor when limit > results, got %q", *page.NextCursor)
	}
	for i, item := range page.Items {
		if item.Session.SourceSessionID != "contract-codex" {
			t.Fatalf("item %d session id = %q, want contract-codex", i, item.Session.SourceSessionID)
		}
		if item.Role != "user" && item.Role != "assistant" {
			t.Fatalf("item %d role = %q, want user or assistant", i, item.Role)
		}
		if item.Text == nil || strings.TrimSpace(*item.Text) == "" {
			t.Fatalf("item %d Text must be populated (envelope-unwrap regression check), got %+v", i, item.Text)
		}
		if !strings.Contains(strings.ToLower(*item.Text), "haystack") {
			t.Fatalf("item %d Text should contain the search term, got %q", i, *item.Text)
		}
	}
}

func TestChatSearchRealClaudeToolResultEnvelopeHiddenByDefault(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "claude-tool-result.jsonl")
	lines := []string{
		`{"type":"user","timestamp":"2026-05-03T02:00:00.000Z","cwd":"/repo","sessionId":"claude-tool-session","message":{"role":"user","content":[{"type":"tool_result","content":"sensitive tool result token"}]}}`,
		``,
	}
	if err := os.WriteFile(transcriptPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "claude", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "search", "--format", "json", "sensitive"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat search default: %v", err)
	}
	if strings.Contains(stdout.String(), "sensitive tool result token") {
		t.Fatalf("default chat search should hide tool_result output, got %q", stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "search", "--format", "json", "--include-tool-outputs", "sensitive"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat search include tools: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, `"tool_output"`) || !strings.Contains(out, "sensitive tool result token") {
		t.Fatalf("include-tool search should return tool output, got %q", out)
	}
}

// TestChatSearchJSONLimitEmitsNextCursor verifies that the chat search
// JSON envelope sets next_cursor and trims results to opts.Limit when the
// repository returns more than the requested page size (the over-fetched
// extra row is detected via the Limit+1 pattern in runChatSearch).
func TestChatSearchJSONLimitEmitsNextCursor(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	// Two items containing "needle" so --limit 1 must return one item
	// plus next_cursor and drop the second.
	transcript := `{
  "id": "limit-cursor",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [
    {"role": "user", "content": "first needle"},
    {"role": "assistant", "content": "second needle"}
  ]
}`
	if err := os.WriteFile(filepath.Join(root, "limit.json"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "search", "--format", "json", "--limit", "1", "needle"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat search --limit 1: %v", err)
	}
	body := stdout.String()
	if !strings.Contains(body, `"next_cursor": "1"`) {
		t.Fatalf("expected next_cursor=\"1\", got %q", body)
	}
	// Decode to verify items length is trimmed to the page size.
	var page struct {
		Items      []chatSearchJSON `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("parse json envelope: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected exactly 1 item per Limit, got %d (%+v)", len(page.Items), page.Items)
	}
	if page.NextCursor == nil || *page.NextCursor != "1" {
		t.Fatalf("expected next_cursor=\"1\", got %+v", page.NextCursor)
	}
}

// TestRunChatImportBailsOnCancelledContext verifies the ctx.Err() guard
// inside the per-file import loop: a context cancelled before the loop
// hits its first file must short-circuit the import rather than continuing
// to walk and process additional transcripts.
func TestRunChatImportBailsOnCancelledContext(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	for _, name := range []string{"a.json", "b.json"} {
		body := `{"id":"` + name + `","messages":[{"role":"user","content":"hi"}]}`
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runChatImport(ctx, &bytes.Buffer{}, &bytes.Buffer{}, chatImportOptions{
		DeviceID: "test-device",
		Agent:    "codex",
		Roots:    []string{root},
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestChatImportDeleteSourceRemovesOriginal(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "delete-source.json")
	transcript := `{
  "id": "delete-source-session",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [{"role": "user", "content": "delete source needle"}]
}`
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root, "--delete-source"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import --delete-source: %v", err)
	}
	var summary chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse import summary: %v\n%s", err, stdout.String())
	}
	if summary.SessionsCreated != 1 || summary.ItemsCreated != 1 || summary.SourcesDeleted != 1 {
		t.Fatalf("unexpected import summary: %+v", summary)
	}
	if _, err := os.Stat(transcriptPath); !os.IsNotExist(err) {
		t.Fatalf("expected source transcript to be removed, stat err = %v", err)
	}
}

func TestChatImportDeleteSourceWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write permission semantics differ on Windows")
	}
	setupEnv(t)
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "delete-source-warning.json")
	transcript := `{
  "id": "delete-source-warning-session",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [{"role": "user", "content": "delete source warning needle"}]
}`
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod root read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: stderr})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root, "--delete-source"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import --delete-source: %v", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("restore root permissions: %v", err)
	}
	var summary chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse import summary: %v\n%s", err, stdout.String())
	}
	if summary.SourcesDeleted != 0 || len(summary.SourceDeleteWarnings) != 1 {
		t.Fatalf("expected one delete warning, got %+v", summary)
	}
	if !strings.Contains(stderr.String(), "warning: failed to delete source after import") {
		t.Fatalf("expected delete warning on stderr, got %q", stderr.String())
	}
}

func TestChatImportReplacementCountsOnlyNewItems(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "count-session.json")
	firstTranscript := `{
  "id": "count-session",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [{"role": "user", "content": "first item"}]
}`
	if err := os.WriteFile(transcriptPath, []byte(firstTranscript), 0o644); err != nil {
		t.Fatalf("write first transcript: %v", err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first chat import: %v", err)
	}

	secondTranscript := `{
  "id": "count-session",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [
    {"role": "user", "content": "first item"},
    {"role": "assistant", "content": "second item"}
  ]
}`
	if err := os.WriteFile(transcriptPath, []byte(secondTranscript), 0o644); err != nil {
		t.Fatalf("write second transcript: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("second chat import: %v", err)
	}
	var summary chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse second import summary: %v\n%s", err, stdout.String())
	}
	if summary.SessionsUpdated != 1 || summary.ItemsCreated != 1 {
		t.Fatalf("unexpected replacement import summary: %+v", summary)
	}
}

func TestRunChatPagerBranches(t *testing.T) {
	t.Setenv("PAGER", "")
	if err := runChatPager("no pager content"); err != nil {
		t.Fatalf("runChatPager(empty PAGER): %v", err)
	}

	if runtime.GOOS == "windows" {
		t.Skip("pager script branch uses a POSIX shell script and is not portable to Windows")
	}

	pagerPath := filepath.Join(t.TempDir(), "pager")
	if err := os.WriteFile(pagerPath, []byte("#!/bin/sh\ncat >/dev/null\n"), 0o700); err != nil {
		t.Fatalf("write pager script: %v", err)
	}
	t.Setenv("PAGER", pagerPath)
	if err := runChatPager("paged content"); err != nil {
		t.Fatalf("runChatPager(script): %v", err)
	}
}

func TestChatCommandValidationAndStackErrors(t *testing.T) {
	setupEnv(t)

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "list", "--agent", "invalid-agent"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected chat list with invalid agent to fail")
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "search", "--agent", "invalid-agent", "needle"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected chat search with invalid agent to fail")
	}

	origResolveHomeDirFn := resolveHomeDirFn
	resolveHomeDirFn = func() (string, error) {
		return "", errors.New("home failed")
	}
	t.Cleanup(func() { resolveHomeDirFn = origResolveHomeDirFn })

	for _, args := range [][]string{
		{"chat", "show", "20260315-aaaabbbb"},
		{"chat", "delete", "20260315-aaaabbbb"},
		{"chat", "restore", "20260315-aaaabbbb"},
	} {
		cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "home failed") {
			t.Fatalf("expected %v to fail with home error, got %v", args, err)
		}
	}
}

func TestChatImportJSONLTableListAndTopLevelShow(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "jsonl-session.jsonl")
	transcript := strings.Join([]string{
		`{"session_id":"jsonl-session","cwd":"/tmp/jsonl-chat","title":"JSONL title","created_at":"2026-05-14T12:00:00Z"}`,
		`{"role":"user","content":[{"type":"text","text":"jsonl needle from user"}],"timestamp":"2026-05-14T12:01:00Z"}`,
		`{"role":"tool","type":"tool_result","content":"tool output hidden by default","timestamp":"2026-05-14T12:02:00Z"}`,
		`{"role":"assistant","message":"assistant jsonl response","timestamp":"2026-05-14T12:03:00Z"}`,
		``,
	}, "\n")
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "claude", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import jsonl: %v", err)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat list table: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "JSONL title") || !strings.Contains(out, "claude_code") {
		t.Fatalf("expected table list to include imported session, got %q", out)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "list", "--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat list json: %v", err)
	}
	var page struct {
		Items []chatSessionJSON `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &page); err != nil {
		t.Fatalf("parse chat list json: %v\n%s", err, stdout.String())
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected one chat session, got %+v", page.Items)
	}
	chatID := page.Items[0].ID

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"show", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("top-level show chat: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "jsonl needle from user") || !strings.Contains(out, "tool output hidden by default") {
		t.Fatalf("expected chat transcript in top-level show output, got %q", out)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", "--format", "json", "--source-session-id", "jsonl-session"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat show by source session json: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, `"source_session_id": "jsonl-session"`) {
		t.Fatalf("expected source session id in json output, got %q", out)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", "--raw", chatID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat show raw: %v", err)
	}
	if !strings.Contains(stdout.String(), `"tool_result"`) {
		t.Fatalf("expected raw tool json, got %q", stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "search", "--format", "json", "--include-tool-outputs", "--agent", "claude", "tool output"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat search json include tools: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, `"tool_output"`) {
		t.Fatalf("expected tool output search result, got %q", out)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "list", "--format", "ids", "--agent", "claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat list ids: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != chatID {
		t.Fatalf("expected chat id list output %q, got %q", chatID, stdout.String())
	}

	recordID := addRecord(t)
	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"records", "show", recordID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("records show: %v", err)
	}
	if !strings.Contains(stdout.String(), recordID) {
		t.Fatalf("expected record id in records show output, got %q", stdout.String())
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"search", "--domain", "chats", "--format", "json", "jsonl needle"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("top-level chat search json: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, `"domain": "chats"`) || !strings.Contains(out, chatID) {
		t.Fatalf("expected chat domain search result, got %q", out)
	}

	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", "--format", "xml", chatID})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected invalid chat show format to fail")
	}
}

func TestChatShowSourceSessionIDRejectsAmbiguousMatches(t *testing.T) {
	setupEnv(t)
	for _, agent := range []string{"codex", "claude"} {
		root := t.TempDir()
		transcript := `{
  "id": "duplicate-source",
  "started_at": "2026-05-14T12:00:00Z",
  "messages": [{"role": "user", "content": "` + agent + ` duplicate"}]
}`
		if err := os.WriteFile(filepath.Join(root, agent+".json"), []byte(transcript), 0o644); err != nil {
			t.Fatalf("write %s transcript: %v", agent, err)
		}
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", agent, "--root", root})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("chat import %s: %v", agent, err)
		}
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", "--source-session-id", "duplicate-source"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous source session error, got %v", err)
	}
}

func TestChatImportGeneratedIDAndParseErrorBranches(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	generatedPath := filepath.Join(root, "generated.json")
	if err := os.WriteFile(generatedPath, []byte(`{"messages":[{"role":"user","content":"generated id needle"}]}`), 0o644); err != nil {
		t.Fatalf("write generated transcript: %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import generated id: %v", err)
	}
	var summary chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse generated import summary: %v\n%s", err, stdout.String())
	}
	if summary.SessionsCreated != 1 || summary.ItemsCreated != 1 {
		t.Fatalf("unexpected generated import summary: %+v", summary)
	}

	stdout.Reset()
	cmd = NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "search", "generated id needle"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat search generated import: %v", err)
	}
	if !strings.Contains(stdout.String(), "generated id needle") && !strings.Contains(stdout.String(), "codex") {
		t.Fatalf("expected generated chat in search output, got %q", stdout.String())
	}

	badRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(badRoot, "bad.json"), []byte(`{bad`), 0o644); err != nil {
		t.Fatalf("write bad transcript: %v", err)
	}
	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", badRoot})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error for bad transcript, got %v", err)
	}
}

func TestChatImportRepositoryErrorBranches(t *testing.T) {
	homeDir := setupEnv(t)
	root := t.TempDir()
	transcriptPath := filepath.Join(root, "session.json")
	if err := os.WriteFile(transcriptPath, []byte(`{"id":"repo-error-session","messages":[{"role":"user","content":"repo error"}]}`), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	origNewSQLiteRepo := newSQLiteRepoFn
	t.Cleanup(func() { newSQLiteRepoFn = origNewSQLiteRepo })

	runWithRepo := func(t *testing.T, repo repository.Repository, want string) {
		t.Helper()
		if mock, ok := repo.(*mockRepo); ok && mock.getRecordByIDFn == nil {
			mock.getRecordByIDFn = func(context.Context, string) (repository.Record, error) {
				return repository.Record{}, repository.ErrNotFound
			}
		}
		newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
			return repo, nil
		}
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "codex", "--root", root})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("chat import error = %v, want substring %q", err, want)
		}
	}

	activeDevice := repository.Device{ID: "test-device"}
	runWithRepo(t, &mockRepo{
		getDeviceByIDFn: func(context.Context, string) (repository.Device, error) {
			return activeDevice, nil
		},
		listProjectPathsFn: func(context.Context, *string) ([]repository.ProjectPath, error) {
			return nil, errors.New("paths failed")
		},
	}, "list project paths")

	runWithRepo(t, &mockRepo{
		getDeviceByIDFn: func(context.Context, string) (repository.Device, error) {
			return activeDevice, nil
		},
		upsertChatSessionFn: func(_ context.Context, input repository.UpsertChatSessionInput) (repository.ChatSession, bool, error) {
			return repository.ChatSession{}, false, errors.New("upsert failed")
		},
	}, "upsert chat session")

	runWithRepo(t, &mockRepo{
		getDeviceByIDFn: func(context.Context, string) (repository.Device, error) {
			return activeDevice, nil
		},
		upsertChatSessionFn: func(_ context.Context, input repository.UpsertChatSessionInput) (repository.ChatSession, bool, error) {
			return repository.ChatSession{ID: input.ID}, true, nil
		},
		replaceChatItemsFn: func(context.Context, string, []repository.CreateChatItemInput) error {
			return errors.New("replace failed")
		},
	}, "replace chat items")

	t.Setenv(pcHomeEnvVar, homeDir)
}

func TestChatCommandValidationBranches(t *testing.T) {
	setupEnv(t)
	helpOut := &bytes.Buffer{}
	helpCmd := NewRootCommand(RootCommandOptions{Stdout: helpOut, Stderr: &bytes.Buffer{}})
	helpCmd.SetArgs([]string{"chat"})
	if err := helpCmd.Execute(); err != nil {
		t.Fatalf("chat help command: %v", err)
	}
	if !strings.Contains(helpOut.String(), "Import and browse agent chats") {
		t.Fatalf("expected chat help output, got %q", helpOut.String())
	}
	cases := [][]string{
		{"chat", "import"},
		{"chat", "import", "--device", "test-device", "--agent", "unknown"},
		{"chat", "import", "--device", "test-device", "--root", "/tmp/example-no-agent"},
		{"chat", "list", "--format", "xml"},
		{"chat", "list", "--limit", "0"},
		{"chat", "list", "--offset", "-1"},
		{"chat", "search", "   "},
		{"chat", "search", "--limit", "0", "needle"},
		{"chat", "search", "--offset", "-1", "needle"},
		{"chat", "search", "--format", "xml", "needle"},
		{"chat", "show", "missing-chat"},
		{"chat", "show", "--source-session-id", "missing-source"},
	}
	for _, args := range cases {
		cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected %v to fail", args)
		}
	}
}

// TestChatImportRequiresAgentWithRoot prevents duplicate-import regression:
// when --root is supplied without --agent, the importer used to fan the root
// across all three sources and import the same transcript three times.
func TestChatImportRequiresAgentWithRoot(t *testing.T) {
	setupEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "session.json"),
		[]byte(`{"id":"agent-required","messages":[{"role":"user","content":"x"}]}`), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--root", root})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected --root without --agent to fail")
	}
	if !strings.Contains(err.Error(), "--agent is required") {
		t.Fatalf("expected --agent-required error, got %v", err)
	}
}

func TestChatRepositoryErrorBranches(t *testing.T) {
	setupEnv(t)
	origNewSQLiteRepo := newSQLiteRepoFn
	t.Cleanup(func() { newSQLiteRepoFn = origNewSQLiteRepo })

	newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
		return &mockRepo{
			countChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) (int, error) {
				return 0, errors.New("count failed")
			},
		}, nil
	}
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "list"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "count chats") {
		t.Fatalf("expected count chats error, got %v", err)
	}

	newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
		return &mockRepo{
			listChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
				return nil, errors.New("list failed")
			},
		}, nil
	}
	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "list"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "list chats") {
		t.Fatalf("expected list chats error, got %v", err)
	}

	newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
		return &mockRepo{
			searchChatItemsFn: func(context.Context, repository.SearchChatItemsFilter) ([]repository.ChatSearchResult, error) {
				return nil, errors.New("search failed")
			},
		}, nil
	}
	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "search", "needle"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "search chats") {
		t.Fatalf("expected search chats error, got %v", err)
	}

	newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
		return &mockRepo{
			listChatSessionsFn: func(context.Context, repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
				return nil, errors.New("source list failed")
			},
		}, nil
	}
	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", "--source-session-id", "session-id"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "list chats") {
		t.Fatalf("expected source-session list error, got %v", err)
	}

	newSQLiteRepoFn = func(*sql.DB) (repository.Repository, error) {
		now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
		return &mockRepo{
			getChatByIDFn: func(context.Context, string) (repository.ChatSession, error) {
				return repository.ChatSession{ID: "chat-id", Source: "codex", SourceSessionID: "source-id", StartedAt: now, LastActivityAt: now}, nil
			},
			listChatItemsFn: func(context.Context, string) ([]repository.ChatItem, error) {
				return nil, errors.New("items failed")
			},
		}, nil
	}
	cmd = NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "show", "chat-id"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "list chat items") {
		t.Fatalf("expected list chat items error, got %v", err)
	}
}

func TestChatWritersAndGeneratedID(t *testing.T) {
	millisTime := time.Date(2026, 5, 14, 12, 0, 0, 123000000, time.UTC)

	tableOut := &bytes.Buffer{}
	if err := writeChatListTable(tableOut, nil); err != nil {
		t.Fatalf("writeChatListTable(empty) error = %v", err)
	}
	if !strings.Contains(tableOut.String(), "No chat sessions found.") {
		t.Fatalf("expected empty chat list message, got %q", tableOut.String())
	}
	searchOut := &bytes.Buffer{}
	if err := writeChatSearchTable(searchOut, nil); err != nil {
		t.Fatalf("writeChatSearchTable(empty) error = %v", err)
	}
	if !strings.Contains(searchOut.String(), "No matching chats found.") {
		t.Fatalf("expected empty chat search message, got %q", searchOut.String())
	}
	tableOut.Reset()
	projectID := "chat/helper-project"
	title := strings.Repeat("long title ", 10)
	if err := writeChatListTable(tableOut, []repository.ChatSession{{
		ID:             "20260514-facefeed",
		Source:         "codex",
		ProjectID:      &projectID,
		Title:          &title,
		LastActivityAt: millisTime,
	}}); err != nil {
		t.Fatalf("writeChatListTable(populated) error = %v", err)
	}
	if out := tableOut.String(); !strings.Contains(out, projectID) || !strings.Contains(out, "...") {
		t.Fatalf("expected populated chat list table to include project and truncated title, got %q", out)
	}
	searchOut.Reset()
	if err := writeChatSearchTable(searchOut, []repository.ChatSearchResult{{
		Session: repository.ChatSession{ID: "20260514-facefeed"},
		Item:    repository.ChatItem{Ordinal: 3, Role: "assistant", SearchText: "fallback\nsnippet"},
	}}); err != nil {
		t.Fatalf("writeChatSearchTable(populated) error = %v", err)
	}
	if out := searchOut.String(); !strings.Contains(out, "fallback snippet") {
		t.Fatalf("expected fallback chat snippet with newline normalized, got %q", out)
	}
	transcriptOut := &bytes.Buffer{}
	transcriptProject := "chat/transcript-project"
	if err := writeChatTranscript(transcriptOut, repository.ChatSession{
		ID:              "20260514-facefeed",
		Source:          "codex",
		SourceSessionID: "source",
		ProjectID:       &transcriptProject,
		LastActivityAt:  millisTime,
	}, []repository.ChatItem{{
		Ordinal:  0,
		Role:     "tool",
		ItemType: "tool_output",
		Text:     strPtr(strings.Repeat("tool output ", 40)),
	}}, chatShowOptions{}); err != nil {
		t.Fatalf("writeChatTranscript(project/tool) error = %v", err)
	}
	if out := transcriptOut.String(); !strings.Contains(out, transcriptProject) || !strings.Contains(out, "...") {
		t.Fatalf("expected transcript project and truncated tool output, got %q", out)
	}
	origIsTerminal := chatStdoutIsTerminal
	origRunPager := runChatPager
	t.Cleanup(func() {
		chatStdoutIsTerminal = origIsTerminal
		runChatPager = origRunPager
	})
	t.Setenv("PAGER", "test-pager")
	paged := ""
	chatStdoutIsTerminal = func(io.Writer) bool { return true }
	runChatPager = func(content string) error {
		paged = content
		return nil
	}
	transcriptOut.Reset()
	if err := writeChatTranscript(transcriptOut, repository.ChatSession{
		ID:              "20260514-facefeed",
		Source:          "codex",
		SourceSessionID: "source",
		LastActivityAt:  millisTime,
	}, []repository.ChatItem{{Ordinal: 0, Role: "user", ItemType: "message", Text: strPtr("paged text")}}, chatShowOptions{}); err != nil {
		t.Fatalf("writeChatTranscript(paged) error = %v", err)
	}
	if !strings.Contains(paged, "paged text") || transcriptOut.Len() != 0 {
		t.Fatalf("expected paged content and no direct output, paged=%q stdout=%q", paged, transcriptOut.String())
	}
	t.Setenv("PAGER", "")
	runChatPager = func(content string) error {
		paged = content
		return nil
	}
	transcriptOut.Reset()
	if err := writeChatTranscript(transcriptOut, repository.ChatSession{
		ID:              "20260514-facefeed",
		Source:          "codex",
		SourceSessionID: "source",
		LastActivityAt:  millisTime,
	}, []repository.ChatItem{{Ordinal: 0, Role: "user", ItemType: "message", Text: strPtr("direct terminal text")}}, chatShowOptions{}); err != nil {
		t.Fatalf("writeChatTranscript(terminal without pager) error = %v", err)
	}
	if out := transcriptOut.String(); !strings.Contains(out, "direct terminal text") {
		t.Fatalf("expected direct terminal output without PAGER, got %q", out)
	}
	runChatPager = func(string) error {
		return errors.New("pager failed")
	}
	if err := writeChatTranscript(&bytes.Buffer{}, repository.ChatSession{
		ID:              "20260514-facefeed",
		Source:          "codex",
		SourceSessionID: "source",
		LastActivityAt:  millisTime,
	}, nil, chatShowOptions{}); err == nil || !strings.Contains(err.Error(), "page chat transcript") {
		t.Fatalf("expected pager error, got %v", err)
	}

	if _, err := generateUniqueChatID(context.Background(), &mockRepo{
		getRecordByIDFn: func(context.Context, string) (repository.Record, error) {
			return repository.Record{ID: "existing"}, nil
		},
	}, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)); err == nil || !strings.Contains(err.Error(), "exhausted retries") {
		t.Fatalf("expected exhausted unique chat id error, got %v", err)
	}

	// A non-NotFound error from GetRecordByID must surface, not be silently
	// swallowed as "id is taken, retry" — otherwise transient DB outages
	// burn the 16-attempt budget and report a misleading "exhausted retries".
	dbBoom := errors.New("db boom")
	if _, err := generateUniqueChatID(context.Background(), &mockRepo{
		getRecordByIDFn: func(context.Context, string) (repository.Record, error) {
			return repository.Record{}, dbBoom
		},
	}, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)); err == nil || !errors.Is(err, dbBoom) || !strings.Contains(err.Error(), "check record") {
		t.Fatalf("expected GetRecordByID error to surface, got %v", err)
	}

	// Same contract for GetChatSessionByID: a non-NotFound error must
	// terminate the loop with a wrapped error rather than retrying.
	chatBoom := errors.New("chat db boom")
	if _, err := generateUniqueChatID(context.Background(), &mockRepo{
		getRecordByIDFn: func(context.Context, string) (repository.Record, error) {
			return repository.Record{}, repository.ErrNotFound
		},
		getChatByIDFn: func(context.Context, string) (repository.ChatSession, error) {
			return repository.ChatSession{}, chatBoom
		},
	}, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)); err == nil || !errors.Is(err, chatBoom) || !strings.Contains(err.Error(), "check chat") {
		t.Fatalf("expected GetChatSessionByID error to surface, got %v", err)
	}
}
