package chatimport

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/timeutil"
)

// maxJSONLLineBytes caps the per-line size that bufio.Scanner will buffer
// when reading JSONL transcripts. Large Claude tool-output payloads can
// easily exceed the default 64 KiB; 256 MiB is more than enough for any
// realistic session while still bounding memory use on a malformed file.
const maxJSONLLineBytes = 256 * 1024 * 1024

// ErrEmptyTranscript is returned by ParseTranscriptFile when a transcript file
// parses to zero chat items (for example a 0-byte file, a Claude cursor-marker
// line, or a Gemini session-header-only file). Callers should count the file as
// scanned but must not create or update a chat session for it.
var ErrEmptyTranscript = errors.New("transcript has no chat items")

// subagentContext describes whether a Claude Code transcript file is a
// Task-tool subagent (sidechain) transcript and, if so, the file basename used
// to derive a file-unique source identity. Claude writes every subagent row
// with the PARENT transcript's sessionId, so without a per-file suffix sibling
// subagents collide on (source, source_session_id) and overwrite each other.
type subagentContext struct {
	basename string // file basename (no extension) used as the subagent suffix
	byPath   bool   // file lives under a subagents/ directory (definitive signal)
}

// NormalizeAgentName maps user-facing agent names to stable storage source IDs.
func NormalizeAgentName(agent string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(agent)) {
	case "":
		return "", nil
	case "codex":
		return "codex", nil
	case "claude", "claude-code", "claude_code":
		return "claude_code", nil
	case "gemini":
		return "gemini", nil
	default:
		return "", fmt.Errorf("unknown agent %q: expected codex, claude, or gemini", agent)
	}
}

// Roots returns transcript roots for the selected source set.
func Roots(extra []string, sourceFilter string, projectPaths []repository.ProjectPath) (map[string][]string, error) {
	roots := map[string][]string{}
	sources := []string{"codex", "claude_code", "gemini"}
	if sourceFilter != "" {
		sources = []string{sourceFilter}
	}
	if len(extra) > 0 {
		for _, root := range extra {
			normalized, err := normalizePath(root)
			if err != nil {
				return nil, err
			}
			for _, source := range sources {
				roots[source] = append(roots[source], normalized)
			}
		}
		return roots, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home: %w", err)
	}
	for _, source := range sources {
		switch source {
		case "codex":
			roots[source] = append(roots[source], filepath.Join(home, ".codex", "sessions"))
		case "claude_code":
			roots[source] = append(roots[source], filepath.Join(home, ".claude", "projects"))
		case "gemini":
			roots[source] = append(roots[source], filepath.Join(home, ".gemini", "tmp"))
		}
	}
	for _, path := range projectPaths {
		for _, source := range sources {
			switch source {
			case "codex":
				roots[source] = append(roots[source], filepath.Join(path.Path, ".codex", "sessions"))
			case "claude_code":
				roots[source] = append(roots[source], filepath.Join(path.Path, ".claude", "projects"))
				// Some repos redirect Claude Code state into
				// `.claude-config/` (e.g. agent-layer projects) so the
				// in-repo transcripts live under `.claude-config/projects`
				// rather than the canonical `.claude/projects`. Including
				// both avoids requiring users to symlink or pass --root.
				roots[source] = append(roots[source], filepath.Join(path.Path, ".claude-config", "projects"))
			case "gemini":
				roots[source] = append(roots[source], filepath.Join(path.Path, ".gemini", "tmp"))
			}
		}
	}
	// Two project paths can resolve to the same scan root, and the home-dir +
	// project-path pass can repeat the same directory. De-duplicate per source
	// so the caller doesn't re-scan the same tree, which would double-import
	// and (worse) cause `--delete-source` to fail the second pass.
	for source, paths := range roots {
		seen := make(map[string]struct{}, len(paths))
		deduped := paths[:0]
		for _, p := range paths {
			cleaned := filepath.Clean(p)
			if _, ok := seen[cleaned]; ok {
				continue
			}
			seen[cleaned] = struct{}{}
			deduped = append(deduped, cleaned)
		}
		roots[source] = deduped
	}
	return roots, nil
}

// TranscriptFiles returns supported transcript files under root in deterministic order.
func TranscriptFiles(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		// Skip known agent sidecars that live beside real transcripts but are
		// not chat transcript payloads.
		name := strings.ToLower(filepath.Base(path))
		if strings.HasSuffix(name, ".meta.json") || name == "sessions-index.json" || name == "logs.json" {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".json", ".jsonl", ".ndjson":
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	sort.Strings(files)
	return files, nil
}

// ParseTranscriptFile normalizes a supported source transcript into a chat session and items.
func ParseTranscriptFile(source string, path string) (repository.CreateChatSessionInput, []repository.CreateChatItemInput, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jsonl", ".ndjson":
		return parseJSONLTranscript(source, path)
	case ".json":
		return parseJSONTranscript(source, path)
	default:
		return repository.CreateChatSessionInput{}, nil, fmt.Errorf("unsupported transcript extension %q", ext)
	}
}

// MatchProjectPath returns the best project match for cwd on the given device.
func MatchProjectPath(paths []repository.ProjectPath, cwd *string, deviceID string) *string {
	if cwd == nil {
		return nil
	}
	clean := filepath.Clean(*cwd)
	var best *repository.ProjectPath
	for i := range paths {
		path := paths[i]
		if path.DeviceID != deviceID {
			continue
		}
		if clean == path.Path || strings.HasPrefix(clean, path.Path+string(os.PathSeparator)) {
			if best == nil || len(path.Path) > len(best.Path) {
				best = &path
			}
		}
	}
	if best == nil {
		return nil
	}
	return &best.ProjectID
}

func parseJSONLTranscript(source string, path string) (repository.CreateChatSessionInput, []repository.CreateChatItemInput, error) {
	file, err := os.Open(path)
	if err != nil {
		return repository.CreateChatSessionInput{}, nil, err
	}
	defer func() { _ = file.Close() }()
	session := newTranscriptSession(source, path)
	sub := subagentContext{}
	if source == "claude_code" {
		sub = subagentContext{
			basename: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			byPath:   strings.Contains(filepath.ToSlash(path), "/subagents/"),
		}
	}
	session, items, err := parseJSONLTranscriptReader(source, path, file, session, 0, session.SourceSessionID, sub)
	if err != nil {
		return repository.CreateChatSessionInput{}, nil, err
	}
	if len(items) == 0 {
		return repository.CreateChatSessionInput{}, nil, ErrEmptyTranscript
	}
	return session, items, nil
}

// ParseAppendedJSONLTranscript parses only the appended byte range of a JSONL
// or NDJSON transcript. The base session is used as the canonical session
// metadata, so appended rows cannot accidentally change the source identity
// that was established by an earlier full import.
func ParseAppendedJSONLTranscript(source string, path string, offset int64, limit int64, base repository.CreateChatSessionInput, ordinalStart int) (repository.CreateChatSessionInput, []repository.CreateChatItemInput, error) {
	if offset < 0 || limit < 0 || ordinalStart < 0 {
		return repository.CreateChatSessionInput{}, nil, fmt.Errorf("invalid appended transcript range")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".jsonl" && ext != ".ndjson" {
		return repository.CreateChatSessionInput{}, nil, fmt.Errorf("unsupported append transcript extension %q", ext)
	}
	file, err := os.Open(path)
	if err != nil {
		return repository.CreateChatSessionInput{}, nil, err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return repository.CreateChatSessionInput{}, nil, fmt.Errorf("seek appended transcript range: %w", err)
	}
	reader := io.Reader(io.LimitReader(file, limit))
	// Appended parsing keeps the established base identity: pass an empty
	// fallback and no subagent context so appended rows cannot change the
	// source/parent identity fixed by the original full import.
	return parseJSONLTranscriptReader(source, path, reader, base, ordinalStart, "", subagentContext{})
}

func parseJSONLTranscriptReader(source string, path string, reader io.Reader, session repository.CreateChatSessionInput, ordinalStart int, fallbackSourceSessionID string, sub subagentContext) (repository.CreateChatSessionInput, []repository.CreateChatItemInput, error) {
	var items []repository.CreateChatItemInput
	scanner := bufio.NewScanner(reader)
	// Cap at 256 MiB so a single oversized JSONL row (e.g., a Claude tool
	// output embedding a large pasted file) doesn't fail with the default
	// 64 KiB Scanner limit. Truly larger lines surface as a wrapped
	// bufio.ErrTooLong with the source path so users can locate the offender.
	scanner.Buffer(make([]byte, 0, 64*1024), maxJSONLLineBytes)
	ordinal := ordinalStart
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		var payload map[string]any
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return repository.CreateChatSessionInput{}, nil, fmt.Errorf("parse %s line %d: %w", path, lineNumber, err)
		}
		itemPayload, sessionPayload := unwrapChatLine(source, payload)
		if itemPayload == nil || !hasItemPayload(itemPayload) {
			if sessionPayload != nil {
				applySessionFields(&session, sessionPayload)
			}
			continue
		}
		if sessionPayload != nil {
			applyItemSessionFields(&session, sessionPayload, fallbackSourceSessionID, sub)
		}
		item := itemFromPayload(itemPayload, ordinal)
		if item.SearchText != "" || item.RawJSON != nil {
			items = append(items, item)
			ordinal++
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return repository.CreateChatSessionInput{}, nil, fmt.Errorf("parse %s line %d: line exceeds %d-byte limit: %w", path, lineNumber+1, maxJSONLLineBytes, err)
		}
		return repository.CreateChatSessionInput{}, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	finalizeSessionTimes(&session, items)
	return session, items, nil
}

func applyItemSessionFields(session *repository.CreateChatSessionInput, payload map[string]any, fallbackSourceSessionID string, sub subagentContext) {
	// sessionId (camelCase) is the canonical key emitted in real Claude Code
	// transcripts; session_id / conversation_id / chat_id cover snake_case
	// variants used by other agents.
	for _, key := range []string{"session_id", "sessionId", "conversation_id", "chat_id"} {
		if value := stringField(payload, key); value != nil && *value != "" {
			isSidechain, _ := payload["isSidechain"].(bool)
			if sub.basename != "" && (sub.byPath || isSidechain) {
				// Subagent (Task-tool sidechain) transcript: every row carries
				// the PARENT transcript's session id. Keep the parent id as
				// relationship metadata and derive a file-unique source identity
				// (parent_sid:subagent_basename) so sibling subagents of the same
				// parent become distinct sessions instead of overwriting each
				// other. Set once; the value is identical on every row.
				if session.ParentSourceSessionID == nil {
					parent := *value
					session.ParentSourceSessionID = &parent
					session.SourceSessionID = parent + ":" + sub.basename
				}
			} else if session.SourceSessionID == fallbackSourceSessionID || session.SourceSessionID == *value {
				session.SourceSessionID = *value
			}
			break
		}
	}
	if session.CWD == nil {
		session.CWD = stringField(payload, "cwd")
	}
	if session.Title == nil {
		session.Title = stringField(payload, "title")
	}
}

// unwrapChatLine peels the agent-specific envelope off a single JSONL row and
// returns (itemPayload, sessionMetaPayload). Real transcripts use different
// shapes than the loose `{role, content, ...}` synthetic fixtures:
//
//   - codex rollouts wrap each row as
//     `{timestamp, type:"response_item"|"session_meta"|..., payload:{...}}`
//     and the actual chat item (or session metadata) lives inside payload.
//   - Claude transcripts wrap each row as
//     `{type:"user"|"assistant", message:{role,content,...}, timestamp,
//     sessionId, cwd, ...}` so the text lives inside `message.content`.
//
// Without unwrapping, top-level lookups for `content` / `role` find nothing
// for codex (all items silently dropped) and for Claude they find a non-string
// `message` field so Text/SearchText come out empty. For unknown shapes the
// row is returned unchanged so the existing loose `{role, content}` fallback
// keeps working.
func unwrapChatLine(source string, line map[string]any) (item map[string]any, sessionMeta map[string]any) {
	switch source {
	case "codex":
		outerType := firstString(line, "type")
		inner, _ := line["payload"].(map[string]any)
		switch outerType {
		case "session_meta":
			return nil, inner
		case "response_item":
			if inner == nil {
				return nil, nil
			}
			flat := make(map[string]any, len(inner)+1)
			for k, v := range inner {
				flat[k] = v
			}
			if _, has := flat["timestamp"]; !has {
				if ts, ok := line["timestamp"]; ok {
					flat["timestamp"] = ts
				}
			}
			return flat, inner
		case "turn_context":
			// turn_context lines carry the working directory per turn.
			// When session_meta lacked cwd (older rollouts), feeding the
			// inner payload to applySessionFields lets the first
			// turn_context's cwd populate session.CWD; subsequent ones
			// don't overwrite because applySessionFields preserves the
			// first non-nil value. This is what unlocks project auto-
			// assignment for codex sessions that previously stayed
			// project_id=NULL.
			return nil, inner
		case "event_msg":
			return nil, nil
		}
		// No recognized codex envelope (e.g. synthetic loose-format
		// fixtures) - fall through to passthrough.
		return line, line
	case "claude_code":
		outerType := firstString(line, "type")
		if outerType == "user" || outerType == "assistant" {
			if message, ok := line["message"].(map[string]any); ok {
				flat := make(map[string]any, len(message)+4)
				for k, v := range message {
					flat[k] = v
				}
				// Outer envelope carries the canonical timestamp, cwd,
				// sessionId, and (sometimes) the role hint when the
				// nested message omits it.
				for _, key := range []string{"timestamp", "cwd", "sessionId"} {
					if _, has := flat[key]; !has {
						if v, ok := line[key]; ok {
							flat[key] = v
						}
					}
				}
				if _, has := flat["role"]; !has {
					flat["role"] = outerType
				}
				return flat, line
			}
		}
		// Drop real-Claude metadata rows. Real Claude transcripts carry
		// `sessionId` (camelCase) on every row; a row with that marker
		// but no user/assistant type is metadata (queue-operation,
		// which duplicates the next user message via its top-level
		// `content` field, plus file-history-snapshot, ai-title,
		// permission-mode, progress, task_reminder, pr-link, ...). The
		// synthetic loose fixtures use snake_case `session_id` and lack
		// this marker, so they still fall through to passthrough.
		if _, hasRealClaudeMarker := line["sessionId"]; hasRealClaudeMarker {
			return nil, nil
		}
		return line, line
	case "gemini":
		// Gemini labels model turns with type:"gemini" and emits type:"info" /
		// type:"error" status/error lines that carry content. Normalize them so
		// no agent-specific label leaks into item_type: model turns become
		// role=assistant/item_type=message, and info/error become
		// item_type=event with role=info/error. sessionMeta is intentionally nil
		// so row fields never rewrite the path-derived Gemini source identity
		// (Gemini transcripts carry no usable internal session id).
		geminiType := strings.ToLower(firstString(line, "type"))
		switch geminiType {
		case "gemini":
			return geminiItem(line, "assistant", "message"), nil
		case "user":
			return geminiItem(line, "user", "message"), nil
		case "info", "error":
			return geminiItem(line, geminiType, "event"), nil
		}
		// Unknown Gemini row: import its content if present (hasItemPayload drops
		// contentless metadata such as the session-header line), without letting
		// it alter the source identity.
		return line, nil
	}
	return line, line
}

// geminiItem clones a Gemini transcript row with a normalized role and
// item_type, dropping the agent-specific `type` label so it cannot leak into
// item_type downstream.
func geminiItem(line map[string]any, role string, itemType string) map[string]any {
	flat := make(map[string]any, len(line)+2)
	for k, v := range line {
		flat[k] = v
	}
	flat["role"] = role
	flat["item_type"] = itemType
	delete(flat, "type")
	return flat
}

func hasItemPayload(payload map[string]any) bool {
	// An object qualifies as a transcript item only if it carries a content
	// field. Bare-`type` rows are common in session metadata / event streams
	// (e.g. `{"type":"session_start"}`) and previously got imported as
	// empty chat items, polluting the chat table and search index.
	hasContent := false
	for _, key := range []string{"content", "text", "message"} {
		if _, ok := payload[key]; ok {
			hasContent = true
			break
		}
	}
	if !hasContent {
		return false
	}
	for _, key := range []string{"role", "type", "author", "item_type"} {
		if _, ok := payload[key]; ok {
			return true
		}
	}
	// Content present but no role-typing hint at all: still treat as item
	// so we don't silently drop user-content lines that omit role metadata.
	return true
}

func parseJSONTranscript(source string, path string) (repository.CreateChatSessionInput, []repository.CreateChatItemInput, error) {
	file, err := os.Open(path)
	if err != nil {
		return repository.CreateChatSessionInput{}, nil, err
	}
	defer func() { _ = file.Close() }()
	var payload map[string]any
	if err := json.NewDecoder(file).Decode(&payload); err != nil {
		return repository.CreateChatSessionInput{}, nil, err
	}
	session := newTranscriptSession(source, path)
	applySessionFields(&session, payload)
	var rawItems []any
	matched := false
	for _, key := range []string{"messages", "items", "transcript"} {
		if value, ok := payload[key].([]any); ok {
			rawItems = value
			matched = true
			break
		}
	}
	if !matched {
		// Reject any .json file that doesn't carry one of the supported
		// transcript array keys, so arbitrary agent config/state JSON
		// under a scanned root doesn't get silently imported as an empty
		// chat session.
		return repository.CreateChatSessionInput{}, nil, fmt.Errorf("no transcript array (expected one of messages/items/transcript)")
	}
	items := make([]repository.CreateChatItemInput, 0, len(rawItems))
	for i, raw := range rawItems {
		object, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		item := itemFromPayload(object, i)
		if item.SearchText != "" || item.RawJSON != nil {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return repository.CreateChatSessionInput{}, nil, ErrEmptyTranscript
	}
	finalizeSessionTimes(&session, items)
	return session, items, nil
}

func newTranscriptSession(source string, path string) repository.CreateChatSessionInput {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	sourceSessionID := base
	if source == "gemini" {
		sourceSessionID = geminiSourceSessionID(path, base)
	}
	abs, _ := filepath.Abs(path)
	return repository.CreateChatSessionInput{
		Source:             source,
		SourceSessionID:    sourceSessionID,
		OriginalSourcePath: &abs,
	}
}

// geminiSourceSessionID derives a stable, file-unique source identity for a
// Gemini transcript. Gemini stores the same logical session under both a
// project-name directory and a project-hash directory with an identical
// basename (e.g. tmp/<name>/chats/session-X.json and tmp/<hash>/chats/
// session-X.json). Keying on the basename alone collides those two paths and
// silently overwrites one. Including the disambiguating parent directories (the
// project-key segment, plus its container when present) gives each path a
// distinct, human-readable identity that stays stable across re-imports. A JSON
// transcript carrying an explicit conversation/session id still overrides this
// via applySessionFields.
func geminiSourceSessionID(path string, base string) string {
	dir := filepath.Dir(path)
	parent := filepath.Base(dir)
	if parent == "." || parent == string(filepath.Separator) {
		return base
	}
	grandparent := filepath.Base(filepath.Dir(dir))
	if grandparent != "." && grandparent != string(filepath.Separator) {
		return grandparent + "/" + parent + "/" + base
	}
	return parent + "/" + base
}

func applySessionFields(session *repository.CreateChatSessionInput, payload map[string]any) {
	// Prefer the canonical session keys before falling back to the generic
	// "id" field, which some vendors reuse for unrelated draft/internal ids.
	for _, key := range []string{"session_id", "conversation_id", "chat_id", "id"} {
		if value := stringField(payload, key); value != nil && *value != "" {
			session.SourceSessionID = *value
			break
		}
	}
	if value := stringField(payload, "cwd"); value != nil {
		session.CWD = value
	}
	if value := stringField(payload, "title"); value != nil {
		session.Title = value
	}
	for _, key := range []string{"timestamp", "created_at", "started_at"} {
		if value := stringField(payload, key); value != nil {
			if parsed, err := ParseChatTime(*value); err == nil {
				applySessionTime(session, parsed)
				break
			}
		}
	}
}

func applySessionTime(session *repository.CreateChatSessionInput, parsed time.Time) {
	parsed = parsed.UTC()
	if session.StartedAt.IsZero() || parsed.Before(session.StartedAt) {
		session.StartedAt = parsed
	}
	if session.LastActivityAt.IsZero() || parsed.After(session.LastActivityAt) {
		session.LastActivityAt = parsed
	}
}

func itemFromPayload(payload map[string]any, ordinal int) repository.CreateChatItemInput {
	role := firstString(payload, "role", "type", "author")
	if role == "" {
		role = "unknown"
	}
	itemType := firstString(payload, "item_type", "type")
	if itemType == "" {
		itemType = "message"
	}
	// Role-shaped values like "user"/"assistant"/"system" must not also be
	// stored as item_type. Without this, a Claude jsonl entry of
	// `{"type":"user","content":...}` ends up with role="user" AND
	// item_type="user", breaking item_type filters in search/transcript code
	// that expect item_type ∈ {message, tool_output, ...}.
	if itemTypeIsRoleShape(itemType) {
		itemType = "message"
	}
	text := contentText(payload["content"])
	if text == "" {
		text = firstString(payload, "text", "message")
	}
	rawBytes, _ := json.Marshal(payload)
	raw := string(rawBytes)
	if (strings.Contains(strings.ToLower(itemType), "tool") || contentHasToolPayload(payload["content"])) && !strings.Contains(strings.ToLower(role), "assistant") {
		itemType = "tool_output"
	}
	var textPtr *string
	if strings.TrimSpace(text) != "" {
		textPtr = &text
	}
	var createdAt *time.Time
	for _, key := range []string{"timestamp", "created_at", "started_at"} {
		if value := stringField(payload, key); value != nil {
			if parsed, err := ParseChatTime(*value); err == nil {
				parsed = parsed.UTC()
				createdAt = &parsed
				break
			}
		}
	}
	return repository.CreateChatItemInput{
		Ordinal:    ordinal,
		Role:       role,
		ItemType:   itemType,
		Text:       textPtr,
		SearchText: text,
		RawJSON:    &raw,
		CreatedAt:  createdAt,
	}
}

func finalizeSessionTimes(session *repository.CreateChatSessionInput, items []repository.CreateChatItemInput) {
	for _, item := range items {
		if item.CreatedAt != nil {
			applySessionTime(session, *item.CreatedAt)
		}
	}
	if session.StartedAt.IsZero() {
		session.StartedAt = time.Now().UTC()
	}
	if session.LastActivityAt.IsZero() || session.LastActivityAt.Before(session.StartedAt) {
		session.LastActivityAt = session.StartedAt
	}
	if session.Title == nil && len(items) > 0 && items[0].Text != nil {
		title := truncate(strings.ReplaceAll(*items[0].Text, "\n", " "), 80)
		session.Title = &title
	}
}

// itemTypeIsRoleShape reports whether a string value looks like a chat role
// (and therefore should not also be used as item_type when both fields were
// inferred from the same source field).
func itemTypeIsRoleShape(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "user", "assistant", "system", "human", "model":
		return true
	}
	return false
}

// ParseChatTime parses transcript timestamps into UTC.
func ParseChatTime(raw string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.Parse(timeutil.UTCMillisFormat, raw); err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp %q", raw)
}

func stringField(payload map[string]any, key string) *string {
	value, ok := payload[key].(string)
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func firstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringField(payload, key); value != nil {
			return *value
		}
	}
	return ""
}

func contentText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				if text := firstString(object, "text", "content"); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		return firstString(typed, "text", "content")
	default:
		return ""
	}
}

// contentHasToolPayload reports whether a content value or nested content
// block carries a tool-shaped type marker, such as Claude's `tool_result`.
func contentHasToolPayload(value any) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if contentHasToolPayload(item) {
				return true
			}
		}
	case map[string]any:
		if strings.Contains(strings.ToLower(firstString(typed, "type", "item_type")), "tool") {
			return true
		}
		return contentHasToolPayload(typed["content"])
	}
	return false
}

func normalizePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("project path must not be empty")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve project path symlinks: %w", err)
	}
	return filepath.Clean(resolved), nil
}

// truncate caps value at max runes (not bytes) so multibyte characters
// — common in chat titles containing emoji or non-ASCII text — are not
// split mid-codepoint and rendered as garbage.
func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
