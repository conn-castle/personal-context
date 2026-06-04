package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// writeGeminiSessionUnder lays out a Gemini transcript under
// <root>/<subdir>/chats/session.json, mirroring Gemini's tmp layout. A non-empty
// projectHash is written into the session JSON ("hash" dirs); a non-empty
// projectRoot is written to the sibling <root>/<subdir>/.project_root ("named"
// dirs). Gemini carries no cwd field, so neither is on the session row itself.
func writeGeminiSessionUnder(t *testing.T, root string, subdir string, projectHash string, projectRoot string) {
	t.Helper()
	dir := filepath.Join(root, subdir)
	chats := filepath.Join(dir, "chats")
	if err := os.MkdirAll(chats, 0o755); err != nil {
		t.Fatalf("mkdir gemini chats: %v", err)
	}
	if projectRoot != "" {
		if err := os.WriteFile(filepath.Join(dir, ".project_root"), []byte(projectRoot+"\n"), 0o644); err != nil {
			t.Fatalf("write .project_root: %v", err)
		}
	}
	payload := `{"started_at":"2026-05-18T12:00:00Z","messages":[{"role":"user","content":"gemini needle"}]}`
	if projectHash != "" {
		payload = `{"projectHash":"` + projectHash + `","started_at":"2026-05-18T12:00:00Z","messages":[{"role":"user","content":"gemini needle"}]}`
	}
	if err := os.WriteFile(filepath.Join(chats, "session.json"), []byte(payload), 0o644); err != nil {
		t.Fatalf("write gemini session: %v", err)
	}
}

func importGeminiRoot(t *testing.T, root string) chatImportSummary {
	t.Helper()
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"chat", "import", "--device", "test-device", "--agent", "gemini", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat import: %v", err)
	}
	var summary chatImportSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse import summary: %v\n%s", err, stdout.String())
	}
	return summary
}

func listChatSessions(t *testing.T, filterArgs ...string) []chatSessionJSON {
	t.Helper()
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs(append([]string{"chat", "list", "--format", "json"}, filterArgs...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("chat list %v: %v", filterArgs, err)
	}
	var page struct {
		Items []chatSessionJSON `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &page); err != nil {
		t.Fatalf("parse chat list json: %v\n%s", err, stdout.String())
	}
	return page.Items
}

func listChatSessionsByProject(t *testing.T, projectID string) []chatSessionJSON {
	t.Helper()
	return listChatSessions(t, "--project", projectID)
}

// TestChatImportGeminiAttributesByProjectRootAndHash proves a Gemini session is
// attributed at import time from both on-disk signals: a sibling .project_root
// literal path and a projectHash == sha256(repo root). Both resolve to the
// registered project's cwd so the existing MatchProjectPath assigns the project.
func TestChatImportGeminiAttributesByProjectRootAndHash(t *testing.T) {
	setupEnv(t)
	projectPath := t.TempDir()
	normalized, err := normalizeProjectPath(projectPath)
	if err != nil {
		t.Fatalf("normalize project path: %v", err)
	}

	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "register", "chat/gem", projectPath, "--device", "test-device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project register: %v", err)
	}

	geminiRoot := t.TempDir()
	writeGeminiSessionUnder(t, geminiRoot, "named", "", normalized)
	writeGeminiSessionUnder(t, geminiRoot, sha256Hex(normalized), sha256Hex(normalized), "")

	if got := importGeminiRoot(t, geminiRoot); got.SessionsCreated != 2 {
		t.Fatalf("expected 2 gemini sessions created, got %+v", got)
	}

	items := listChatSessionsByProject(t, "chat/gem")
	if len(items) != 2 {
		t.Fatalf("expected 2 attributed gemini sessions, got %d: %+v", len(items), items)
	}
	for _, it := range items {
		if it.Source != "gemini" {
			t.Fatalf("unexpected source %q", it.Source)
		}
		if it.ProjectID == nil || *it.ProjectID != "chat/gem" {
			t.Fatalf("gemini session %q not attributed: %+v", it.SourceSessionID, it.ProjectID)
		}
		if it.CWD == nil || *it.CWD != normalized {
			t.Fatalf("gemini session %q cwd = %v; want %q", it.SourceSessionID, it.CWD, normalized)
		}
	}
}

// TestChatImportGeminiBackfillAndRepairAfterRegister proves the two repair paths
// for Gemini sessions imported BEFORE their project is registered:
//   - the .project_root session persists its cwd, so `pc project register`
//     backfills it (no schema column needed); and
//   - the hash-only session has no cwd until the registry is known, so a
//     re-import repairs it via resolve-on-skip — after which a further re-import
//     does not churn.
func TestChatImportGeminiBackfillAndRepairAfterRegister(t *testing.T) {
	setupEnv(t)
	projectPath := t.TempDir()
	normalized, err := normalizeProjectPath(projectPath)
	if err != nil {
		t.Fatalf("normalize project path: %v", err)
	}

	geminiRoot := t.TempDir()
	writeGeminiSessionUnder(t, geminiRoot, "named", "", normalized)
	writeGeminiSessionUnder(t, geminiRoot, sha256Hex(normalized), sha256Hex(normalized), "")

	// Import before the project exists: both sessions land unattributed.
	if got := importGeminiRoot(t, geminiRoot); got.SessionsCreated != 2 {
		t.Fatalf("expected 2 created on first import, got %+v", got)
	}

	// Registering the project backfills only the session whose cwd was persisted
	// from .project_root; the hash-only session has no cwd to match yet.
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand(RootCommandOptions{Stdout: stdout, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "register", "chat/gem", projectPath, "--device", "test-device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project register: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "Backfilled 1 chat session") {
		t.Fatalf("expected the .project_root session to backfill on register, got %q", out)
	}
	if items := listChatSessionsByProject(t, "chat/gem"); len(items) != 1 {
		t.Fatalf("expected 1 attributed session after register, got %d", len(items))
	}

	// Re-import: the hash-only session is repaired via resolve-on-skip now that
	// its project is registered.
	importGeminiRoot(t, geminiRoot)
	if items := listChatSessionsByProject(t, "chat/gem"); len(items) != 2 {
		t.Fatalf("expected 2 attributed sessions after re-import repair, got %d", len(items))
	}

	// A stable re-import must not churn: both sessions are attributed and
	// unchanged, so both are skipped with no new writes.
	if got := importGeminiRoot(t, geminiRoot); got.SessionsSkipped != 2 || got.SessionsCreated != 0 {
		t.Fatalf("expected 2 skipped / 0 created on stable re-import (no churn), got %+v", got)
	}
}

// TestChatImportGeminiUnattributableStaysUnassigned proves the documented
// residual: a Gemini session whose repo root is not a registered project (an
// ephemeral checkout, recoverable only as a one-way projectHash) imports
// unattributed and is reported by `chat list --unassigned` rather than guessed
// onto another project.
func TestChatImportGeminiUnattributableStaysUnassigned(t *testing.T) {
	setupEnv(t)
	projectPath := t.TempDir()
	cmd := NewRootCommand(RootCommandOptions{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	cmd.SetArgs([]string{"project", "register", "chat/gem", projectPath, "--device", "test-device"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("project register: %v", err)
	}

	geminiRoot := t.TempDir()
	// projectHash for a path that is not registered: a one-way hash that cannot
	// be reversed to a project, so the session must remain unassigned.
	writeGeminiSessionUnder(t, geminiRoot, "ephemeral", sha256Hex("/Users/someone/tmp/ephemeral-checkout"), "")

	if got := importGeminiRoot(t, geminiRoot); got.SessionsCreated != 1 {
		t.Fatalf("expected the unattributable session to import, got %+v", got)
	}
	if items := listChatSessionsByProject(t, "chat/gem"); len(items) != 0 {
		t.Fatalf("unregistered-root gemini must not attach to a project, got %d", len(items))
	}
	unassigned := listChatSessions(t, "--unassigned")
	if len(unassigned) != 1 || unassigned[0].Source != "gemini" || unassigned[0].ProjectID != nil {
		t.Fatalf("expected 1 unassigned gemini session, got %+v", unassigned)
	}
}
