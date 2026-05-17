package filesystem

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// chatSessionIDPattern enforces the canonical chat session id shape used in
// raw_source_key validation. Must match schema check constraints.
var chatSessionIDPattern = regexp.MustCompile(`^\d{8}-[0-9a-f]{8}$`)

// supportedChatSourceExtensions are the only file extensions the importer
// accepts for raw chat transcripts. The leading dot is preserved here for
// direct comparison with filepath.Ext output.
var supportedChatSourceExtensions = []string{".json", ".jsonl", ".ndjson"}

// ChatSourceStage holds metadata for a staged raw chat source copy that has
// not yet been promoted to the active chats/raw/{chatID}/ directory.
type ChatSourceStage struct {
	// ChatSessionID is the owning chat session id.
	ChatSessionID string
	// RawSourceKey is the canonical relative key the staged file will hold
	// once promoted. It is the same key that gets stored in
	// chat_session.raw_source_key and used as the S3 object suffix.
	RawSourceKey string
	// StagedPath is the absolute filesystem path of the staged file. It lives
	// outside the active chats/raw/{chatID}/ directory.
	StagedPath string
	// Size is the number of bytes copied into the staged file.
	Size int64
}

// DeriveChatSourceKey returns the managed raw chat source key for a chat
// session, derived from the imported source path's supported extension.
// It rejects unsupported or missing extensions.
func DeriveChatSourceKey(chatSessionID string, sourcePath string) (string, error) {
	if err := validateChatSessionID(chatSessionID); err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(sourcePath))
	if !isSupportedChatSourceExt(ext) {
		return "", fmt.Errorf("unsupported chat source extension %q: want one of %s", ext, strings.Join(supportedChatSourceExtensions, ", "))
	}
	return "chats/raw/" + chatSessionID + "/source" + ext, nil
}

// ValidateChatSourceKey enforces the exact managed key shape for the owning
// chat session. It rejects absolute paths, traversal segments, wrong chat
// session ids, wrong basenames, and unsupported extensions.
func ValidateChatSourceKey(chatSessionID string, rawSourceKey string) error {
	if err := validateChatSessionID(chatSessionID); err != nil {
		return err
	}
	if strings.TrimSpace(rawSourceKey) == "" {
		return fmt.Errorf("raw_source_key is required")
	}
	if rawSourceKey != filepath.ToSlash(rawSourceKey) {
		return fmt.Errorf("raw_source_key must use forward slashes: %q", rawSourceKey)
	}
	if strings.HasPrefix(rawSourceKey, "/") {
		return fmt.Errorf("raw_source_key must be relative: %q", rawSourceKey)
	}
	parts := strings.Split(rawSourceKey, "/")
	if len(parts) != 4 {
		return fmt.Errorf("raw_source_key must be chats/raw/{chat_session_id}/source.{ext}: %q", rawSourceKey)
	}
	if parts[0] != "chats" || parts[1] != "raw" {
		return fmt.Errorf("raw_source_key must be under chats/raw/: %q", rawSourceKey)
	}
	if parts[2] != chatSessionID {
		return fmt.Errorf("raw_source_key chat id %q does not match owning chat session %q", parts[2], chatSessionID)
	}
	basename := parts[3]
	for _, ext := range supportedChatSourceExtensions {
		if basename == "source"+ext {
			return nil
		}
	}
	return fmt.Errorf("raw_source_key basename must be source.{json|jsonl|ndjson}, got %q", basename)
}

// ResolveChatSourcePath resolves a managed raw chat source key to an absolute
// local path under the filesystem client base path, after strict validation
// against the owning chat session.
func (c *Client) ResolveChatSourcePath(chatSessionID string, rawSourceKey string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("filesystem client is required")
	}
	if err := ValidateChatSourceKey(chatSessionID, rawSourceKey); err != nil {
		return "", err
	}
	return filepath.Join(c.basePath, filepath.FromSlash(rawSourceKey)), nil
}

// CopyChatSourceToStage copies the original transcript source into a staging
// location outside the active chats/raw/{chatID}/ directory, returning the
// staged path and the canonical relative key it will occupy once promoted.
// The caller must either promote the stage or call DeleteChatSourceStage to
// clean up.
func (c *Client) CopyChatSourceToStage(chatSessionID string, sourcePath string) (ChatSourceStage, error) {
	if c == nil {
		return ChatSourceStage{}, fmt.Errorf("filesystem client is required")
	}
	if err := validateChatSessionID(chatSessionID); err != nil {
		return ChatSourceStage{}, err
	}
	if strings.TrimSpace(sourcePath) == "" {
		return ChatSourceStage{}, fmt.Errorf("source path is required")
	}
	rawSourceKey, err := DeriveChatSourceKey(chatSessionID, sourcePath)
	if err != nil {
		return ChatSourceStage{}, err
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return ChatSourceStage{}, fmt.Errorf("stat chat source %s: %w", sourcePath, err)
	}
	if info.IsDir() {
		return ChatSourceStage{}, fmt.Errorf("chat source must be a file: %s", sourcePath)
	}
	nonce, err := randomHexNonce()
	if err != nil {
		return ChatSourceStage{}, err
	}
	stageDir := filepath.Join(c.basePath, "chats", "raw", ".staging-"+chatSessionID+"-"+nonce)
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		return ChatSourceStage{}, fmt.Errorf("create chat source stage dir: %w", err)
	}
	stagedPath := filepath.Join(stageDir, filepath.Base(rawSourceKey))

	srcFile, err := os.Open(sourcePath)
	if err != nil {
		_ = os.RemoveAll(stageDir)
		return ChatSourceStage{}, fmt.Errorf("open chat source %s: %w", sourcePath, err)
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		_ = os.RemoveAll(stageDir)
		return ChatSourceStage{}, fmt.Errorf("create staged chat source %s: %w", stagedPath, err)
	}
	written, copyErr := io.Copy(dstFile, srcFile)
	syncErr := syncFileFn(dstFile)
	closeErr := closeFileFn(dstFile)
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.RemoveAll(stageDir)
		if copyErr != nil {
			return ChatSourceStage{}, fmt.Errorf("copy chat source bytes: %w", copyErr)
		}
		if syncErr != nil {
			return ChatSourceStage{}, fmt.Errorf("sync staged chat source: %w", syncErr)
		}
		return ChatSourceStage{}, fmt.Errorf("close staged chat source: %w", closeErr)
	}
	return ChatSourceStage{
		ChatSessionID: chatSessionID,
		RawSourceKey:  rawSourceKey,
		StagedPath:    stagedPath,
		Size:          written,
	}, nil
}

// DeleteChatSourceStage removes any staging directory the stage occupies. It
// is safe to call on a stage that has already been promoted or partially
// cleaned up; missing directories are not an error.
func (c *Client) DeleteChatSourceStage(stage ChatSourceStage) error {
	if c == nil || stage.StagedPath == "" {
		return nil
	}
	stageDir := filepath.Dir(stage.StagedPath)
	if err := os.RemoveAll(stageDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove chat source stage dir: %w", err)
	}
	return nil
}

// PromoteChatSourceStage moves a staged chat raw source into the active
// chats/raw/{chatID}/ directory, replacing any previous active raw source.
// The replacement is rollback-safe: the previous active directory is moved
// to a sibling backup before promotion and only deleted after success. On
// promotion failure the previous active directory is restored.
func (c *Client) PromoteChatSourceStage(stage ChatSourceStage) (StoredFile, error) {
	if c == nil {
		return StoredFile{}, fmt.Errorf("filesystem client is required")
	}
	if err := ValidateChatSourceKey(stage.ChatSessionID, stage.RawSourceKey); err != nil {
		return StoredFile{}, err
	}
	if strings.TrimSpace(stage.StagedPath) == "" {
		return StoredFile{}, fmt.Errorf("stage has no staged path")
	}
	if _, err := os.Stat(stage.StagedPath); err != nil {
		return StoredFile{}, fmt.Errorf("staged chat source missing: %w", err)
	}
	activeDir := filepath.Join(c.basePath, "chats", "raw", stage.ChatSessionID)
	activePath := filepath.Join(c.basePath, filepath.FromSlash(stage.RawSourceKey))
	stageDir := filepath.Dir(stage.StagedPath)

	if err := os.MkdirAll(filepath.Dir(activeDir), 0o700); err != nil {
		return StoredFile{}, fmt.Errorf("ensure chats/raw exists: %w", err)
	}

	var backupDir string
	if _, err := os.Stat(activeDir); err == nil {
		nonce, nonceErr := randomHexNonce()
		if nonceErr != nil {
			return StoredFile{}, nonceErr
		}
		backupDir = filepath.Join(c.basePath, "chats", "raw", ".backup-"+stage.ChatSessionID+"-"+nonce)
		if err := os.Rename(activeDir, backupDir); err != nil {
			return StoredFile{}, fmt.Errorf("backup previous chat raw source: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return StoredFile{}, fmt.Errorf("stat active chat raw dir: %w", err)
	}

	if err := os.Rename(stageDir, activeDir); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, activeDir)
		}
		return StoredFile{}, fmt.Errorf("promote chat source stage: %w", err)
	}

	info, err := os.Stat(activePath)
	if err != nil {
		if backupDir != "" {
			_ = os.RemoveAll(activeDir)
			_ = os.Rename(backupDir, activeDir)
		}
		return StoredFile{}, fmt.Errorf("verify promoted chat source: %w", err)
	}
	if backupDir != "" {
		if err := os.RemoveAll(backupDir); err != nil {
			return StoredFile{}, fmt.Errorf("clean up previous chat raw backup: %w", err)
		}
	}
	return StoredFile{
		Filename: filepath.Base(activePath),
		S3Key:    stage.RawSourceKey,
		Path:     activePath,
		Size:     info.Size(),
	}, nil
}

// DeleteChatSource removes the entire managed raw-source directory for a chat
// session (chats/raw/{chatID}/). Missing directories are tolerated.
func (c *Client) DeleteChatSource(chatSessionID string) error {
	if c == nil {
		return fmt.Errorf("filesystem client is required")
	}
	if err := validateChatSessionID(chatSessionID); err != nil {
		return err
	}
	dir := filepath.Join(c.basePath, "chats", "raw", chatSessionID)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove chat raw source dir: %w", err)
	}
	return nil
}

// ListChatSessionIDsOnDisk returns chat session IDs that have managed raw
// source directories on disk under chats/raw/. Staging and backup directories
// are filtered out.
func (c *Client) ListChatSessionIDsOnDisk() ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("filesystem client is required")
	}
	entries, err := os.ReadDir(filepath.Join(c.basePath, "chats", "raw"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list chat raw directories: %w", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !chatSessionIDPattern.MatchString(name) {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

func validateChatSessionID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("chat session id is required")
	}
	if !chatSessionIDPattern.MatchString(id) {
		return fmt.Errorf("chat session id must match YYYYMMDD-xxxxxxxx: %q", id)
	}
	return nil
}

func isSupportedChatSourceExt(ext string) bool {
	for _, supported := range supportedChatSourceExtensions {
		if ext == supported {
			return true
		}
	}
	return false
}

func randomHexNonce() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
