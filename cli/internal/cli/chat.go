package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/chatimport"
	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/listpage"
	"github.com/conn-castle/personal-context/cli/internal/recordid"
	"github.com/conn-castle/personal-context/cli/internal/recordio"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/timeutil"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

const (
	defaultChatListLimit = 50
	maxChatListLimit     = 500
)

type chatShowOptions struct {
	Format          string
	Full            bool
	Raw             bool
	SourceSessionID bool
}

var chatStdoutIsTerminal = func(w io.Writer) bool {
	file, ok := w.(interface{ Fd() uintptr })
	return ok && isatty.IsTerminal(file.Fd())
}

var runChatPager = func(content string) error {
	pager := strings.TrimSpace(os.Getenv("PAGER"))
	if pager == "" {
		return nil
	}
	fields := strings.Fields(pager)
	if len(fields) == 0 {
		return nil
	}
	cmd := exec.Command(fields[0], fields[1:]...)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func newChatCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Import and browse agent chats",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newChatImportCommand(stdout, stderr))
	cmd.AddCommand(newChatListCommand(stdout, stderr))
	cmd.AddCommand(newChatSearchCommand(stdout, stderr))
	cmd.AddCommand(newChatShowCommand(stdout, stderr))
	cmd.AddCommand(newChatDeleteCommand(stdout, stderr))
	cmd.AddCommand(newChatRestoreCommand(stdout, stderr))
	return cmd
}

func newChatImportCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var deviceID string
	var agent string
	var roots []string
	var deleteSource bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import agent chat transcripts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runChatImport(cmd.Context(), stdout, stderr, chatImportOptions{DeviceID: deviceID, Agent: agent, Roots: roots, DeleteSource: deleteSource})
		},
	}
	cmd.Flags().StringVar(&deviceID, "device", "", "Registered source device for imported chats")
	cmd.Flags().StringVar(&agent, "agent", "", "Import only one agent (codex|claude|gemini)")
	cmd.Flags().StringArrayVar(&roots, "root", nil, "Transcript root to scan (overrides default agent roots; requires --agent)")
	cmd.Flags().BoolVar(&deleteSource, "delete-source", false, "Delete each original transcript file after Personal Context safely owns a managed copy")
	return cmd
}

type chatImportOptions struct {
	DeviceID     string
	Agent        string
	Roots        []string
	DeleteSource bool
}

type chatImportSummary struct {
	SessionsCreated      int      `json:"sessions_created"`
	SessionsUpdated      int      `json:"sessions_updated"`
	SessionsSkipped      int      `json:"sessions_skipped,omitempty"`
	ItemsCreated         int      `json:"items_created"`
	FilesScanned         int      `json:"files_scanned"`
	RawSourcesCopied     int      `json:"raw_sources_copied"`
	SourcesDeleted       int      `json:"sources_deleted,omitempty"`
	SourceDeleteWarnings []string `json:"source_delete_warnings,omitempty"`
}

type chatImportSourcePathKey struct {
	source   string
	deviceID string
	absPath  string
}

type chatImportSourceSessionKey struct {
	source          string
	sourceSessionID string
}

type chatImportSessionIndex struct {
	byOriginalPath  map[chatImportSourcePathKey]repository.ChatSession
	bySourceSession map[chatImportSourceSessionKey]repository.ChatSession
}

func runChatImport(ctx context.Context, stdout io.Writer, stderr io.Writer, opts chatImportOptions) error {
	deviceID := strings.TrimSpace(opts.DeviceID)
	if deviceID == "" {
		return fmt.Errorf("--device is required")
	}
	sourceFilter, err := chatimport.NormalizeAgentName(opts.Agent)
	if err != nil {
		return err
	}
	// Each explicit --root is treated as a transcript directory for a single
	// agent: pairing it with every source produces duplicate imports of the
	// same file (one chat session per source) and breaks --delete-source on
	// the second pass. Require --agent when --root is set so the importer
	// knows which source the transcripts belong to.
	if len(opts.Roots) > 0 && sourceFilter == "" {
		return fmt.Errorf("--agent is required when --root is set (codex, claude, or gemini)")
	}
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	if stack.FS == nil {
		return fmt.Errorf("filesystem client is not configured")
	}
	if err := validateActiveDevice(ctx, stack.Repo, deviceID); err != nil {
		return err
	}
	projectPaths, err := stack.Repo.ListProjectPaths(ctx, nil)
	if err != nil {
		return fmt.Errorf("list project paths: %w", err)
	}
	roots, err := chatimport.Roots(opts.Roots, sourceFilter, projectPaths)
	if err != nil {
		return err
	}
	summary := chatImportSummary{}
	hadMutations := false
	for source, sourceRoots := range roots {
		sessionIndex, err := loadChatImportSessionIndex(ctx, stack.Repo, source, deviceID)
		if err != nil {
			return err
		}
		for _, root := range sourceRoots {
			files, err := chatimport.TranscriptFiles(root)
			if err != nil {
				return err
			}
			for _, file := range files {
				if err := ctx.Err(); err != nil {
					return err
				}
				summary.FilesScanned++
				existing, ok, err := sessionIndex.existingByOriginalPath(source, deviceID, file)
				if err != nil {
					return err
				}
				if ok {
					handled, mutated, err := handleExistingChatImport(ctx, stack, &summary, existing, file, deviceID, projectPaths, opts.DeleteSource)
					if err != nil {
						return err
					}
					if mutated {
						hadMutations = true
					}
					if handled {
						continue
					}
				}
				session, items, err := chatimport.ParseTranscriptFile(source, file)
				if err != nil {
					return fmt.Errorf("parse %s: %w", file, err)
				}
				session.SourceDeviceID = deviceID
				session.ProjectID = chatimport.MatchProjectPath(projectPaths, session.CWD, deviceID)
				sourceSessionKey := chatImportSourceSessionKey{source: session.Source, sourceSessionID: session.SourceSessionID}
				if existing, ok := sessionIndex.bySourceSession[sourceSessionKey]; ok {
					handled, mutated, err := handleExistingChatImport(ctx, stack, &summary, existing, file, deviceID, projectPaths, opts.DeleteSource)
					if err != nil {
						return err
					}
					if mutated {
						hadMutations = true
					}
					if handled {
						continue
					}
				}
				if session.ID == "" {
					if existing, ok := sessionIndex.bySourceSession[sourceSessionKey]; ok {
						session.ID = existing.ID
					} else if existing, err := stack.Repo.GetChatSessionBySource(ctx, session.Source, session.SourceSessionID); err == nil {
						session.ID = existing.ID
					} else if !errors.Is(err, repository.ErrNotFound) {
						return fmt.Errorf("look up existing chat session: %w", err)
					} else {
						id, err := generateUniqueChatID(ctx, stack.Repo, session.LastActivityAt)
						if err != nil {
							return err
						}
						session.ID = id
					}
				}
				stage, err := stack.FS.CopyChatSourceToStage(session.ID, file)
				if err != nil {
					return fmt.Errorf("stage chat source %s: %w", file, err)
				}
				rollbackState, err := captureChatImportRollback(ctx, stack, session.Source, session.SourceSessionID)
				if err != nil {
					_ = stack.FS.DeleteChatSourceStage(stage)
					return err
				}
				stagedKey := stage.RawSourceKey
				session.RawSourceKey = &stagedKey
				stored, created, err := stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
					CreateChatSessionInput: session,
					ClearDeleted:           true,
				})
				if err != nil {
					_ = stack.FS.DeleteChatSourceStage(stage)
					rollbackState.cleanup(stack)
					return fmt.Errorf("upsert chat session %s/%s: %w", session.Source, session.SourceSessionID, err)
				}
				if !rollbackState.existed {
					rollbackState.session = stored
				}
				if _, err := stack.FS.PromoteChatSourceStage(stage); err != nil {
					// Promote restores any previous active dir from its backup,
					// but it does not delete the staged dir on failure — clean
					// it up here so re-running `pc chat import` does not leave
					// behind an ever-growing pile of .staging-* directories.
					_ = stack.FS.DeleteChatSourceStage(stage)
					rollbackState.restore(ctx, stack)
					return fmt.Errorf("promote chat source for %s: %w", stored.ID, err)
				}
				summary.RawSourcesCopied++
				hadMutations = true
				if created {
					summary.SessionsCreated++
				} else {
					summary.SessionsUpdated++
				}
				if err := replaceChatItems(ctx, stack.Repo, stored.ID, items); err != nil {
					rollbackState.restore(ctx, stack)
					return fmt.Errorf("replace chat items: %w", err)
				}
				if err := sessionIndex.remember(stored); err != nil {
					rollbackState.restore(ctx, stack)
					return err
				}
				rollbackState.cleanup(stack)
				if created {
					summary.ItemsCreated += len(items)
				} else if len(items) > len(rollbackState.items) {
					summary.ItemsCreated += len(items) - len(rollbackState.items)
				}
				if opts.DeleteSource {
					deleteImportedChatSourceIfSafe(&summary, stack.FS, stored.ID, stagedKey, file)
				}
			}
		}
	}
	if hadMutations {
		_ = runAutoSyncFn(ctx, stderr)
	}
	for _, warning := range summary.SourceDeleteWarnings {
		_, _ = fmt.Fprintf(stderr, "warning: failed to delete source after import: %s\n", warning)
	}
	return writeIndentedJSON(stdout, summary)
}

// loadChatImportSessionIndex builds the lookup tables used to skip unchanged
// transcripts before the importer performs parse, copy, DB, item, or sync work.
func loadChatImportSessionIndex(ctx context.Context, repo repository.Repository, source string, deviceID string) (chatImportSessionIndex, error) {
	idx := chatImportSessionIndex{
		byOriginalPath:  make(map[chatImportSourcePathKey]repository.ChatSession),
		bySourceSession: make(map[chatImportSourceSessionKey]repository.ChatSession),
	}
	sessions, err := repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{
		IncludeDeleted: true,
		Source:         &source,
		DeviceID:       &deviceID,
	})
	if err != nil {
		return chatImportSessionIndex{}, fmt.Errorf("list existing chat sessions for %s: %w", source, err)
	}
	for _, session := range sessions {
		if err := idx.remember(session); err != nil {
			return chatImportSessionIndex{}, err
		}
	}
	return idx, nil
}

// remember updates the source-path and source-session lookup tables after a
// successful import so duplicates later in the same scan see the new session.
func (idx *chatImportSessionIndex) remember(session repository.ChatSession) error {
	idx.bySourceSession[chatImportSourceSessionKey{source: session.Source, sourceSessionID: session.SourceSessionID}] = session
	if session.OriginalSourcePath == nil {
		return nil
	}
	absPath, err := filepath.Abs(*session.OriginalSourcePath)
	if err != nil {
		return fmt.Errorf("resolve original chat source path %q: %w", *session.OriginalSourcePath, err)
	}
	idx.byOriginalPath[chatImportSourcePathKey{source: session.Source, deviceID: session.SourceDeviceID, absPath: absPath}] = session
	return nil
}

// existingByOriginalPath returns a previously imported session for the exact
// source/device/source-path tuple before parsing the transcript body.
func (idx chatImportSessionIndex) existingByOriginalPath(source string, deviceID string, sourcePath string) (repository.ChatSession, bool, error) {
	absPath, err := filepath.Abs(sourcePath)
	if err != nil {
		return repository.ChatSession{}, false, fmt.Errorf("resolve chat source path %q: %w", sourcePath, err)
	}
	session, ok := idx.byOriginalPath[chatImportSourcePathKey{source: source, deviceID: deviceID, absPath: absPath}]
	return session, ok, nil
}

type chatImportSourceComparison struct {
	matches     bool
	appendOnly  bool
	managedPath string
	managedSize int64
	sourceSize  int64
}

// handleExistingChatImport processes fast paths for an existing imported
// source: byte-identical sources are skipped, while append-only JSONL/NDJSON
// sources import just the appended suffix.
func handleExistingChatImport(ctx context.Context, stack *localStack, summary *chatImportSummary, existing repository.ChatSession, sourcePath string, deviceID string, projectPaths []repository.ProjectPath, deleteSource bool) (bool, bool, error) {
	if existing.RawSourceKey == nil {
		return false, false, nil
	}
	comparison, err := compareChatImportSource(ctx, stack.FS, existing.ID, *existing.RawSourceKey, sourcePath)
	if err != nil {
		return false, false, fmt.Errorf("compare chat source with managed raw %s: %w", sourcePath, err)
	}
	if comparison.matches {
		summary.SessionsSkipped++
		if deleteSource {
			deleteImportedChatSourceIfSafe(summary, stack.FS, existing.ID, *existing.RawSourceKey, sourcePath)
		}
		return true, false, nil
	}
	if comparison.appendOnly {
		if err := appendJSONLChatImport(ctx, stack, summary, existing, sourcePath, comparison, deviceID, projectPaths, deleteSource); err != nil {
			return false, false, err
		}
		return true, true, nil
	}
	return false, false, nil
}

// compareChatImportSource compares sourcePath with the managed raw source. A
// missing managed raw file is reported as neither matching nor append-only so
// the full import path can repair the managed copy.
func compareChatImportSource(ctx context.Context, fs *filesystem.Client, chatID string, rawSourceKey string, sourcePath string) (chatImportSourceComparison, error) {
	managedPath, err := fs.ResolveChatSourcePath(chatID, rawSourceKey)
	if err != nil {
		return chatImportSourceComparison{}, err
	}
	managedInfo, err := os.Stat(managedPath)
	if errors.Is(err, os.ErrNotExist) {
		return chatImportSourceComparison{}, nil
	}
	if err != nil {
		return chatImportSourceComparison{}, err
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return chatImportSourceComparison{}, err
	}
	comparison := chatImportSourceComparison{
		managedPath: managedPath,
		managedSize: managedInfo.Size(),
		sourceSize:  sourceInfo.Size(),
	}
	if managedInfo.Size() != sourceInfo.Size() {
		if sourceInfo.Size() > managedInfo.Size() && isAppendableChatSourcePath(sourcePath) && isAppendableChatSourcePath(managedPath) {
			matches, err := filePrefixMatches(ctx, managedPath, sourcePath, managedInfo.Size())
			if err != nil {
				return chatImportSourceComparison{}, err
			}
			comparison.appendOnly = matches
		}
		return comparison, nil
	}
	if err := ctx.Err(); err != nil {
		return chatImportSourceComparison{}, err
	}
	sourceHash, err := recordio.HashFile(sourcePath)
	if err != nil {
		return chatImportSourceComparison{}, err
	}
	if err := ctx.Err(); err != nil {
		return chatImportSourceComparison{}, err
	}
	managedHash, err := recordio.HashFile(managedPath)
	if err != nil {
		return chatImportSourceComparison{}, err
	}
	comparison.matches = sourceHash == managedHash
	return comparison, nil
}

// sourceMatchesManagedRaw reports whether sourcePath has the same bytes as the
// managed raw source.
func sourceMatchesManagedRaw(ctx context.Context, fs *filesystem.Client, chatID string, rawSourceKey string, sourcePath string) (bool, error) {
	comparison, err := compareChatImportSource(ctx, fs, chatID, rawSourceKey, sourcePath)
	if err != nil {
		return false, err
	}
	return comparison.matches, nil
}

// skipUnchangedChatImport compares the scanned source with the managed raw
// source and records a skipped import when the bytes are identical.
func skipUnchangedChatImport(ctx context.Context, fs *filesystem.Client, summary *chatImportSummary, existing repository.ChatSession, sourcePath string, deleteSource bool) (bool, error) {
	if existing.RawSourceKey == nil {
		return false, nil
	}
	matches, err := sourceMatchesManagedRaw(ctx, fs, existing.ID, *existing.RawSourceKey, sourcePath)
	if err != nil {
		return false, fmt.Errorf("compare chat source with managed raw %s: %w", sourcePath, err)
	}
	if !matches {
		return false, nil
	}
	summary.SessionsSkipped++
	if deleteSource {
		deleteImportedChatSourceIfSafe(summary, fs, existing.ID, *existing.RawSourceKey, sourcePath)
	}
	return true, nil
}

func isAppendableChatSourcePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsonl", ".ndjson":
		return true
	default:
		return false
	}
}

func filePrefixMatches(ctx context.Context, prefixPath string, fullPath string, prefixSize int64) (bool, error) {
	prefixFile, err := os.Open(prefixPath)
	if err != nil {
		return false, err
	}
	defer func() { _ = prefixFile.Close() }()
	fullFile, err := os.Open(fullPath)
	if err != nil {
		return false, err
	}
	defer func() { _ = fullFile.Close() }()
	prefixBuffer := make([]byte, 1024*1024)
	fullBuffer := make([]byte, 1024*1024)
	remaining := prefixSize
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		chunkSize := int64(len(prefixBuffer))
		if remaining < chunkSize {
			chunkSize = remaining
		}
		prefixChunk := prefixBuffer[:chunkSize]
		fullChunk := fullBuffer[:chunkSize]
		if _, err := io.ReadFull(prefixFile, prefixChunk); err != nil {
			return false, err
		}
		if _, err := io.ReadFull(fullFile, fullChunk); err != nil {
			return false, err
		}
		if !bytes.Equal(prefixChunk, fullChunk) {
			return false, nil
		}
		remaining -= chunkSize
	}
	return true, nil
}

func appendJSONLChatImport(ctx context.Context, stack *localStack, summary *chatImportSummary, existing repository.ChatSession, sourcePath string, comparison chatImportSourceComparison, deviceID string, projectPaths []repository.ProjectPath, deleteSource bool) error {
	maxOrdinal, err := stack.Repo.MaxChatItemOrdinal(ctx, existing.ID)
	if err != nil {
		return fmt.Errorf("find existing chat item ordinal: %w", err)
	}
	absSourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve chat source path %q: %w", sourcePath, err)
	}
	rawSourceKey := *existing.RawSourceKey
	base := repository.CreateChatSessionInput{
		ID:                 existing.ID,
		Source:             existing.Source,
		SourceSessionID:    existing.SourceSessionID,
		SourceDeviceID:     deviceID,
		ProjectID:          existing.ProjectID,
		CWD:                existing.CWD,
		Title:              existing.Title,
		StartedAt:          existing.StartedAt,
		LastActivityAt:     existing.LastActivityAt,
		OriginalSourcePath: &absSourcePath,
		RawSourceKey:       &rawSourceKey,
	}
	session, items, err := chatimport.ParseAppendedJSONLTranscript(existing.Source, sourcePath, comparison.managedSize, comparison.sourceSize-comparison.managedSize, base, maxOrdinal+1)
	if err != nil {
		return fmt.Errorf("parse appended chat source %s: %w", sourcePath, err)
	}
	session.ID = existing.ID
	session.Source = existing.Source
	session.SourceSessionID = existing.SourceSessionID
	session.SourceDeviceID = deviceID
	session.ProjectID = chatimport.MatchProjectPath(projectPaths, session.CWD, deviceID)
	if session.ProjectID == nil {
		session.ProjectID = existing.ProjectID
	}
	session.RawSourceKey = &rawSourceKey

	rollbackRaw, err := appendManagedRawSuffix(ctx, comparison.managedPath, sourcePath, comparison.managedSize, comparison.sourceSize)
	if err != nil {
		return fmt.Errorf("append managed chat raw source: %w", err)
	}
	stored, created, err := stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
		CreateChatSessionInput: session,
		ClearDeleted:           true,
	})
	if err != nil {
		rollbackRaw()
		return fmt.Errorf("upsert appended chat session %s/%s: %w", session.Source, session.SourceSessionID, err)
	}
	if err := appendChatItems(ctx, stack.Repo, stored.ID, items); err != nil {
		rollbackRaw()
		return fmt.Errorf("append chat items: %w", err)
	}
	if created {
		summary.SessionsCreated++
	} else {
		summary.SessionsUpdated++
	}
	summary.ItemsCreated += len(items)
	if deleteSource {
		deleteImportedChatSourceIfSafe(summary, stack.FS, stored.ID, rawSourceKey, sourcePath)
	}
	return nil
}

func appendManagedRawSuffix(ctx context.Context, managedPath string, sourcePath string, offset int64, sourceSize int64) (func(), error) {
	if offset < 0 || sourceSize < offset {
		return nil, fmt.Errorf("invalid append range")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	managedInfo, err := os.Stat(managedPath)
	if err != nil {
		return nil, err
	}
	if managedInfo.Size() != offset {
		return nil, fmt.Errorf("managed raw source changed during import: size=%d want=%d", managedInfo.Size(), offset)
	}
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sourceFile.Close() }()
	if _, err := sourceFile.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	managedFile, err := os.OpenFile(managedPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, err
	}
	rollback := func() {
		_ = os.Truncate(managedPath, offset)
	}
	written, copyErr := io.Copy(managedFile, io.LimitReader(sourceFile, sourceSize-offset))
	syncErr := managedFile.Sync()
	closeErr := managedFile.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		rollback()
		if copyErr != nil {
			return nil, copyErr
		}
		if syncErr != nil {
			return nil, syncErr
		}
		return nil, closeErr
	}
	if written != sourceSize-offset {
		rollback()
		return nil, io.ErrUnexpectedEOF
	}
	return rollback, nil
}

// deleteImportedChatSourceIfSafe removes the original source only when it is
// not the managed raw file Personal Context owns.
func deleteImportedChatSourceIfSafe(summary *chatImportSummary, fs *filesystem.Client, chatID string, rawSourceKey string, sourcePath string) {
	if isSameAsManagedFile(fs, chatID, rawSourceKey, sourcePath) {
		return
	}
	if err := os.Remove(sourcePath); err != nil {
		summary.SourceDeleteWarnings = append(summary.SourceDeleteWarnings, fmt.Sprintf("%s: %v", sourcePath, err))
	} else {
		summary.SourcesDeleted++
	}
}

type chatImportRollbackState struct {
	existed   bool
	session   repository.ChatSession
	items     []repository.ChatItem
	rawBackup *filesystem.ChatSourceStage
}

func captureChatImportRollback(ctx context.Context, stack *localStack, source string, sourceSessionID string) (chatImportRollbackState, error) {
	existing, err := stack.Repo.GetChatSessionBySource(ctx, source, sourceSessionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return chatImportRollbackState{}, nil
		}
		return chatImportRollbackState{}, fmt.Errorf("look up existing chat rollback state: %w", err)
	}
	items, err := stack.Repo.ListChatItems(ctx, existing.ID)
	if err != nil {
		return chatImportRollbackState{}, fmt.Errorf("list existing chat items for rollback: %w", err)
	}
	state := chatImportRollbackState{existed: true, session: existing, items: items}
	if existing.RawSourceKey != nil && stack.FS != nil {
		rawPath, err := stack.FS.ResolveChatSourcePath(existing.ID, *existing.RawSourceKey)
		if err != nil {
			return chatImportRollbackState{}, fmt.Errorf("resolve existing chat raw source for rollback: %w", err)
		}
		backup, err := stack.FS.CopyChatSourceToStage(existing.ID, rawPath)
		if err == nil {
			state.rawBackup = &backup
		} else if !errors.Is(err, os.ErrNotExist) {
			return chatImportRollbackState{}, fmt.Errorf("back up existing chat raw source for rollback: %w", err)
		}
	}
	return state, nil
}

func (s chatImportRollbackState) restore(ctx context.Context, stack *localStack) {
	if stack == nil || stack.Repo == nil {
		return
	}
	if s.existed {
		_ = stack.Repo.DeleteChatSession(ctx, s.session.ID)
		createdAt := s.session.CreatedAt
		updatedAt := s.session.UpdatedAt
		_, _, _ = stack.Repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:                 s.session.ID,
				Source:             s.session.Source,
				SourceSessionID:    s.session.SourceSessionID,
				SourceDeviceID:     s.session.SourceDeviceID,
				ProjectID:          s.session.ProjectID,
				CWD:                s.session.CWD,
				Title:              s.session.Title,
				StartedAt:          s.session.StartedAt,
				LastActivityAt:     s.session.LastActivityAt,
				OriginalSourcePath: s.session.OriginalSourcePath,
				RawSourceKey:       s.session.RawSourceKey,
				CreatedAt:          &createdAt,
				UpdatedAt:          &updatedAt,
				DeletedAt:          s.session.DeletedAt,
			},
			ClearDeleted: s.session.DeletedAt == nil,
		})
		_ = replaceChatItems(ctx, stack.Repo, s.session.ID, chatItemInputs(s.items))
		if stack.FS != nil {
			if s.rawBackup != nil {
				_, _ = stack.FS.PromoteChatSourceStage(*s.rawBackup)
			} else {
				_ = stack.FS.DeleteChatSource(s.session.ID)
			}
		}
		return
	}
	if stack.FS != nil && s.session.ID != "" {
		_ = stack.FS.DeleteChatSource(s.session.ID)
	}
	if s.session.ID != "" {
		_ = stack.Repo.DeleteChatSession(ctx, s.session.ID)
	}
}

func (s chatImportRollbackState) cleanup(stack *localStack) {
	if stack == nil || stack.FS == nil || s.rawBackup == nil {
		return
	}
	_ = stack.FS.DeleteChatSourceStage(*s.rawBackup)
}

func replaceChatItems(ctx context.Context, repo repository.Repository, sessionID string, items []repository.CreateChatItemInput) error {
	inputs := make([]repository.CreateChatItemInput, len(items))
	for i, item := range items {
		item.SessionID = sessionID
		inputs[i] = item
	}
	return repo.ReplaceChatItems(ctx, sessionID, inputs)
}

func appendChatItems(ctx context.Context, repo repository.Repository, sessionID string, items []repository.CreateChatItemInput) error {
	inputs := make([]repository.CreateChatItemInput, len(items))
	for i, item := range items {
		item.SessionID = sessionID
		inputs[i] = item
	}
	return repo.AppendChatItems(ctx, sessionID, inputs)
}

func chatItemInputs(items []repository.ChatItem) []repository.CreateChatItemInput {
	inputs := make([]repository.CreateChatItemInput, 0, len(items))
	for _, item := range items {
		createdAt := item.CreatedAt
		inputs = append(inputs, repository.CreateChatItemInput{
			SessionID:  item.SessionID,
			Ordinal:    item.Ordinal,
			Role:       item.Role,
			ItemType:   item.ItemType,
			Text:       item.Text,
			SearchText: item.SearchText,
			RawJSON:    item.RawJSON,
			CreatedAt:  &createdAt,
		})
	}
	return inputs
}

// isSameAsManagedFile reports whether the imported source path resolves to
// the same on-disk file Personal Context just took ownership of. Used to
// short-circuit --delete-source when the user pointed pc chat import at a
// path already inside the managed chats/raw/ tree.
func isSameAsManagedFile(fs *filesystem.Client, chatID string, rawSourceKey string, sourcePath string) bool {
	managed, err := fs.ResolveChatSourcePath(chatID, rawSourceKey)
	if err != nil {
		return false
	}
	managedAbs, err := filepath.Abs(managed)
	if err != nil {
		return false
	}
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return false
	}
	return managedAbs == sourceAbs
}

func newChatListCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var format string
	var source string
	var project string
	var unassigned bool
	var limit int
	var offset int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List chat sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runChatList(cmd.Context(), stdout, stderr, chatListOptions{Format: format, Source: source, ProjectID: project, Unassigned: unassigned, Limit: limit, Offset: offset})
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "Output format (table|json|ids)")
	cmd.Flags().StringVar(&source, "agent", "", "Filter by agent (codex|claude|gemini)")
	cmd.Flags().StringVar(&project, "project", "", "Filter by project ID")
	cmd.Flags().BoolVar(&unassigned, "unassigned", false, "Show only sessions without a project")
	cmd.Flags().IntVar(&limit, "limit", defaultChatListLimit, "Maximum sessions to return")
	cmd.Flags().IntVar(&offset, "offset", 0, "Offset for pagination")
	return cmd
}

type chatListOptions struct {
	Format     string
	Source     string
	ProjectID  string
	Unassigned bool
	Limit      int
	Offset     int
}

type chatSessionJSON struct {
	ID                 string  `json:"id"`
	Source             string  `json:"source"`
	SourceSessionID    string  `json:"source_session_id"`
	SourceDeviceID     string  `json:"source_device_id"`
	ProjectID          *string `json:"project_id"`
	CWD                *string `json:"cwd"`
	Title              *string `json:"title"`
	StartedAt          string  `json:"started_at"`
	LastActivityAt     string  `json:"last_activity_at"`
	OriginalSourcePath *string `json:"original_source_path"`
	RawSourceKey       *string `json:"raw_source_key"`
	DeletedAt          *string `json:"deleted_at"`
}

func runChatList(ctx context.Context, stdout io.Writer, _ io.Writer, opts chatListOptions) error {
	if opts.Limit < 1 || opts.Limit > maxChatListLimit {
		return fmt.Errorf("limit must be between 1 and %d", maxChatListLimit)
	}
	if opts.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	source, err := chatimport.NormalizeAgentName(opts.Source)
	if err != nil {
		return err
	}
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	filter := repository.ListChatSessionsFilter{Limit: opts.Limit, Offset: opts.Offset, Unassigned: opts.Unassigned}
	if source != "" {
		filter.Source = &source
	}
	if strings.TrimSpace(opts.ProjectID) != "" {
		project := strings.TrimSpace(opts.ProjectID)
		filter.ProjectID = &project
	}
	total, err := stack.Repo.CountChatSessions(ctx, filter)
	if err != nil {
		return fmt.Errorf("count chats: %w", err)
	}
	sessions, err := stack.Repo.ListChatSessions(ctx, filter)
	if err != nil {
		return fmt.Errorf("list chats: %w", err)
	}
	switch opts.Format {
	case "table":
		return writeChatListTable(stdout, sessions)
	case "ids":
		for _, session := range sessions {
			_, _ = fmt.Fprintln(stdout, session.ID)
		}
		return nil
	case "json":
		items := make([]chatSessionJSON, 0, len(sessions))
		for _, session := range sessions {
			items = append(items, chatSessionToJSON(session))
		}
		return listpage.WriteJSON(stdout, listpage.Response[chatSessionJSON]{Items: items, Total: total, NextCursor: nil})
	default:
		return fmt.Errorf("unknown format %q: expected table, ids, or json", opts.Format)
	}
}

func newChatSearchCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var format string
	var source string
	var project string
	var includeTools bool
	var limit int
	var offset int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search chat transcripts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChatSearch(cmd.Context(), stdout, stderr, args[0], chatSearchOptions{Format: format, Source: source, ProjectID: project, IncludeTools: includeTools, Limit: limit, Offset: offset})
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "Output format (table|json)")
	cmd.Flags().StringVar(&source, "agent", "", "Filter by agent (codex|claude|gemini)")
	cmd.Flags().StringVar(&project, "project", "", "Filter by project ID")
	cmd.Flags().BoolVar(&includeTools, "include-tool-outputs", false, "Include tool output items")
	cmd.Flags().IntVar(&limit, "limit", defaultChatListLimit, "Maximum results to return")
	cmd.Flags().IntVar(&offset, "offset", 0, "Offset for pagination")
	return cmd
}

type chatSearchOptions struct {
	Format       string
	Source       string
	ProjectID    string
	IncludeTools bool
	Limit        int
	Offset       int
}

type chatSearchJSON struct {
	Session chatSessionJSON `json:"session"`
	Ordinal int             `json:"ordinal"`
	Role    string          `json:"role"`
	Type    string          `json:"type"`
	Text    *string         `json:"text"`
	Snippet string          `json:"snippet"`
}

func runChatSearch(ctx context.Context, stdout io.Writer, _ io.Writer, query string, opts chatSearchOptions) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("query must not be empty")
	}
	if opts.Limit < 1 || opts.Limit > maxChatListLimit {
		return fmt.Errorf("limit must be between 1 and %d", maxChatListLimit)
	}
	if opts.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	source, err := chatimport.NormalizeAgentName(opts.Source)
	if err != nil {
		return err
	}
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	// Fetch one extra row beyond the requested page so we can emit a
	// next-cursor for the JSON envelope without issuing a second COUNT.
	filter := repository.SearchChatItemsFilter{Query: query, IncludeToolOutputs: opts.IncludeTools, Limit: opts.Limit + 1, Offset: opts.Offset}
	if source != "" {
		filter.Source = &source
	}
	if strings.TrimSpace(opts.ProjectID) != "" {
		project := strings.TrimSpace(opts.ProjectID)
		filter.ProjectID = &project
	}
	results, err := stack.Repo.SearchChatItems(ctx, filter)
	if err != nil {
		return fmt.Errorf("search chats: %w", err)
	}
	hasMore := len(results) > opts.Limit
	if hasMore {
		results = results[:opts.Limit]
	}
	switch opts.Format {
	case "table":
		return writeChatSearchTable(stdout, results)
	case "json":
		items := make([]chatSearchJSON, 0, len(results))
		for _, result := range results {
			items = append(items, chatSearchJSON{Session: chatSessionToJSON(result.Session), Ordinal: result.Item.Ordinal, Role: result.Item.Role, Type: result.Item.ItemType, Text: result.Item.Text, Snippet: result.Snippet})
		}
		var nextCursor *string
		if hasMore {
			next := fmt.Sprintf("%d", opts.Offset+opts.Limit)
			nextCursor = &next
		}
		return listpage.WriteJSON(stdout, listpage.Response[chatSearchJSON]{Items: items, Total: len(items), NextCursor: nextCursor})
	default:
		return fmt.Errorf("unknown format %q: expected table or json", opts.Format)
	}
}

func newChatShowCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	opts := chatShowOptions{Format: "text"}
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Display a chat transcript",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChatShow(cmd.Context(), stdout, stderr, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Format, "format", "text", "Output format (text|json)")
	cmd.Flags().BoolVar(&opts.Full, "full", false, "Show full tool outputs")
	cmd.Flags().BoolVar(&opts.Raw, "raw", false, "Show raw JSON item payloads")
	cmd.Flags().BoolVar(&opts.SourceSessionID, "source-session-id", false, "Resolve id as a source session ID")
	return cmd
}

func runChatShow(ctx context.Context, stdout io.Writer, _ io.Writer, id string, opts chatShowOptions) error {
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	var session repository.ChatSession
	if opts.SourceSessionID {
		sessions, err := stack.Repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{IncludeDeleted: true})
		if err != nil {
			return fmt.Errorf("list chats: %w", err)
		}
		var matches []repository.ChatSession
		for _, candidate := range sessions {
			if candidate.SourceSessionID == id {
				matches = append(matches, candidate)
			}
		}
		switch len(matches) {
		case 0:
			return fmt.Errorf("chat source session %q not found", id)
		case 1:
			session = matches[0]
		default:
			return fmt.Errorf("chat source session %q is ambiguous across %d chats; use the Personal Context chat ID", id, len(matches))
		}
	} else {
		session, err = stack.Repo.GetChatSessionByID(ctx, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return fmt.Errorf("chat %q not found", id)
			}
			return fmt.Errorf("get chat: %w", err)
		}
	}
	return showChatFromStack(ctx, stdout, stack, session, opts)
}

func showChatFromStack(ctx context.Context, stdout io.Writer, stack *localStack, session repository.ChatSession, opts chatShowOptions) error {
	items, err := stack.Repo.ListChatItems(ctx, session.ID)
	if err != nil {
		return fmt.Errorf("list chat items: %w", err)
	}
	switch opts.Format {
	case "json":
		return writeIndentedJSON(stdout, struct {
			Session chatSessionJSON       `json:"session"`
			Items   []repository.ChatItem `json:"items"`
		}{Session: chatSessionToJSON(session), Items: items})
	case "text":
		return writeChatTranscript(stdout, session, items, opts)
	default:
		return fmt.Errorf("unknown format %q: expected text or json", opts.Format)
	}
}

func newChatDeleteCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Soft-delete a chat session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stack, err := openLocalStackFromHome()
			if err != nil {
				return err
			}
			defer func() { _ = stack.Close() }()
			if err := stack.Repo.SoftDeleteChatSession(cmd.Context(), args[0]); err != nil {
				return fmt.Errorf("delete chat: %w", err)
			}
			_, _ = fmt.Fprintf(stdout, "%s deleted\n", args[0])
			_ = runAutoSyncFn(cmd.Context(), stderr)
			return nil
		},
	}
	return cmd
}

func newChatRestoreCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <id>",
		Short: "Restore a soft-deleted chat session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stack, err := openLocalStackFromHome()
			if err != nil {
				return err
			}
			defer func() { _ = stack.Close() }()
			if err := stack.Repo.RestoreChatSession(cmd.Context(), args[0]); err != nil {
				return fmt.Errorf("restore chat: %w", err)
			}
			_, _ = fmt.Fprintf(stdout, "%s restored\n", args[0])
			_ = runAutoSyncFn(cmd.Context(), stderr)
			return nil
		},
	}
	return cmd
}

func generateUniqueChatID(ctx context.Context, repo repository.Repository, at time.Time) (string, error) {
	for i := 0; i < 16; i++ {
		id, err := recordid.GenerateForDate(at)
		if err != nil {
			return "", err
		}
		_, recordErr := repo.GetRecordByID(ctx, id)
		if recordErr != nil && !errors.Is(recordErr, repository.ErrNotFound) {
			return "", fmt.Errorf("generate unique chat id: check record %s: %w", id, recordErr)
		}
		_, chatErr := repo.GetChatSessionByID(ctx, id)
		if chatErr != nil && !errors.Is(chatErr, repository.ErrNotFound) {
			return "", fmt.Errorf("generate unique chat id: check chat %s: %w", id, chatErr)
		}
		if errors.Is(recordErr, repository.ErrNotFound) && errors.Is(chatErr, repository.ErrNotFound) {
			return id, nil
		}
	}
	return "", fmt.Errorf("generate unique chat id: exhausted retries")
}

func chatSessionToJSON(session repository.ChatSession) chatSessionJSON {
	return chatSessionJSON{
		ID: session.ID, Source: session.Source, SourceSessionID: session.SourceSessionID,
		SourceDeviceID: session.SourceDeviceID, ProjectID: session.ProjectID, CWD: session.CWD,
		Title: session.Title, StartedAt: timeutil.FormatUTCMillis(session.StartedAt),
		LastActivityAt:     timeutil.FormatUTCMillis(session.LastActivityAt),
		OriginalSourcePath: session.OriginalSourcePath,
		RawSourceKey:       session.RawSourceKey,
		DeletedAt:          timeutil.FormatUTCMillisPtr(session.DeletedAt),
	}
}

func writeChatListTable(w io.Writer, sessions []repository.ChatSession) error {
	if len(sessions) == 0 {
		_, _ = fmt.Fprintln(w, "No chat sessions found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tAgent\tProject\tLast Activity\tTitle")
	for _, session := range sessions {
		project := ""
		if session.ProjectID != nil {
			project = *session.ProjectID
		}
		title := ""
		if session.Title != nil {
			title = truncate(*session.Title, 60)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", session.ID, session.Source, project, timeutil.FormatUTCMillis(session.LastActivityAt), title)
	}
	return tw.Flush()
}

func writeChatSearchTable(w io.Writer, results []repository.ChatSearchResult) error {
	if len(results) == 0 {
		_, _ = fmt.Fprintln(w, "No matching chats found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "CHAT\tORD\tROLE\tSNIPPET")
	for _, result := range results {
		snippet := result.Snippet
		if snippet == "" {
			snippet = result.Item.SearchText
		}
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", result.Session.ID, result.Item.Ordinal, result.Item.Role, truncate(strings.ReplaceAll(snippet, "\n", " "), 100))
	}
	return tw.Flush()
}

func writeChatTranscript(w io.Writer, session repository.ChatSession, items []repository.ChatItem, opts chatShowOptions) error {
	if chatStdoutIsTerminal(w) {
		buffer := &bytes.Buffer{}
		if err := renderChatTranscript(buffer, session, items, opts); err != nil {
			return err
		}
		if err := runChatPager(buffer.String()); err != nil {
			return fmt.Errorf("page chat transcript: %w", err)
		}
		if strings.TrimSpace(os.Getenv("PAGER")) != "" {
			return nil
		}
		_, err := io.WriteString(w, buffer.String())
		return err
	}
	return renderChatTranscript(w, session, items, opts)
}

func renderChatTranscript(w io.Writer, session repository.ChatSession, items []repository.ChatItem, opts chatShowOptions) error {
	_, _ = fmt.Fprintf(w, "ID:              %s\n", session.ID)
	_, _ = fmt.Fprintf(w, "Agent:           %s\n", session.Source)
	_, _ = fmt.Fprintf(w, "Source Session:  %s\n", session.SourceSessionID)
	if session.ProjectID != nil {
		_, _ = fmt.Fprintf(w, "Project:         %s\n", *session.ProjectID)
	}
	_, _ = fmt.Fprintf(w, "Last Activity:   %s\n\n", timeutil.FormatUTCMillis(session.LastActivityAt))
	for _, item := range items {
		_, _ = fmt.Fprintf(w, "[%d] %s/%s\n", item.Ordinal, item.Role, item.ItemType)
		if opts.Raw && item.RawJSON != nil {
			_, _ = fmt.Fprintln(w, *item.RawJSON)
		} else if item.Text != nil {
			text := *item.Text
			if item.ItemType == "tool_output" && !opts.Full {
				text = truncate(text, 240)
			}
			_, _ = fmt.Fprintln(w, text)
		}
		_, _ = fmt.Fprintln(w)
	}
	return nil
}
