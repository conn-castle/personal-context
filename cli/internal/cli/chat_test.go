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
}
