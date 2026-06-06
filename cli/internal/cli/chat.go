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
	"github.com/conn-castle/personal-context/cli/internal/syncengine"
	"github.com/conn-castle/personal-context/cli/internal/timeutil"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

const (
	defaultChatListLimit = 50
	maxChatListLimit     = 500
	chatImportBatchSize  = 50
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

// chatImportSummary reports both work performed and the resulting stored state.
//
// Work-performed counters (sessions_created/updated, items_imported,
// raw_sources_copied, *_skipped) describe what the run did. The state counters
// items_delta and items_after_import are derived from the repository's
// authoritative CountChatItems so the summary reconciles with the database
// instead of accumulating CLI-side guesses.
type chatImportSummary struct {
	SessionsCreated int `json:"sessions_created"`
	SessionsUpdated int `json:"sessions_updated"`
	SessionsSkipped int `json:"sessions_skipped,omitempty"`
	// DuplicatesSkipped counts files collapsed as exact byte-identical
	// duplicates scanned under a different path (e.g. Gemini's project-name vs
	// project-hash copies of one session).
	DuplicatesSkipped int `json:"duplicates_skipped,omitempty"`
	// CollisionsSkipped counts files skipped because they collided on an
	// existing (source, source_session_id) owned by a different source file and
	// diverged from it; the run refuses to overwrite the unrelated source.
	CollisionsSkipped int `json:"collisions_skipped,omitempty"`
	// FilesSkipped counts files that could not be parsed as a transcript. The
	// importer reports each path but continues scanning other files.
	FilesSkipped int `json:"files_skipped,omitempty"`
	// ItemsImported is the number of chat item rows written this run (work
	// performed). Replaced sessions count every re-inserted row.
	ItemsImported int `json:"items_imported"`
	// ItemsDelta is the signed net change in stored chat items (after - before),
	// derived from CountChatItems.
	ItemsDelta int `json:"items_delta"`
	// ItemsAfterImport is the authoritative absolute number of stored chat items
	// (in non-deleted sessions) after the run.
	ItemsAfterImport int `json:"items_after_import"`
	FilesScanned     int `json:"files_scanned"`
	// RawSourcesCopied is the number of distinct chat sessions whose managed raw
	// source was written this run (retained state, not per-file work).
	RawSourcesCopied     int `json:"raw_sources_copied"`
	rawSourceSessions    map[string]struct{}
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

type chatImportContentHashKey struct {
	source string
	hash   string
}

type chatImportSessionIndex struct {
	byOriginalPath  map[chatImportSourcePathKey]repository.ChatSession
	bySourceSession map[chatImportSourceSessionKey]repository.ChatSession
}

type pendingChatImport struct {
	op           repository.ChatImportOp
	stage        filesystem.ChatSourceStage
	sourcePath   string
	deleteSource bool
	sessionIndex *chatImportSessionIndex
}

type chatImportBatch struct {
	writer  repository.ChatImportBatchWriter
	fs      *filesystem.Client
	summary *chatImportSummary
	entries []pendingChatImport
}

func (b *chatImportBatch) add(ctx context.Context, pending pendingChatImport) error {
	b.entries = append(b.entries, pending)
	if len(b.entries) >= chatImportBatchSize {
		return b.flush(ctx)
	}
	return nil
}

func (b *chatImportBatch) hasSourceSession(source string, sourceSessionID string) bool {
	for _, entry := range b.entries {
		session := entry.op.Session.CreateChatSessionInput
		if session.Source == source && session.SourceSessionID == sourceSessionID {
			return true
		}
	}
	return false
}

func (b *chatImportBatch) discardPending() {
	for _, entry := range b.entries {
		_ = b.fs.DeleteChatSourceStage(entry.stage)
	}
	b.entries = nil
}

func (b *chatImportBatch) flush(ctx context.Context) error {
	if len(b.entries) == 0 {
		return nil
	}
	entries := b.entries
	b.entries = nil
	ops := make([]repository.ChatImportOp, len(entries))
	for i, entry := range entries {
		ops[i] = entry.op
	}
	results, err := b.writer.WriteChatImportBatch(ctx, ops)
	if err != nil {
		for _, entry := range entries {
			_ = b.fs.DeleteChatSourceStage(entry.stage)
		}
		return fmt.Errorf("write chat import batch: %w", err)
	}
	for i, result := range results {
		entry := entries[i]
		if _, err := b.fs.PromoteChatSourceStage(entry.stage); err != nil {
			_ = b.fs.DeleteChatSourceStage(entry.stage)
			for j := i + 1; j < len(entries); j++ {
				_ = b.fs.DeleteChatSourceStage(entries[j].stage)
			}
			return fmt.Errorf("promote chat source for %s from staged file %s: %w", result.Session.ID, entry.stage.StagedPath, err)
		}
		if b.summary.rawSourceSessions == nil {
			b.summary.rawSourceSessions = make(map[string]struct{})
		}
		b.summary.rawSourceSessions[result.Session.ID] = struct{}{}
		b.summary.RawSourcesCopied = len(b.summary.rawSourceSessions)
		// items_imported is work performed: every row written this run, whether
		// inserted into a new session, appended, or re-inserted on replace. Net
		// state is reported separately via items_delta/items_after_import.
		b.summary.ItemsImported += len(entry.op.Items)
		if result.Created {
			b.summary.SessionsCreated++
		} else {
			b.summary.SessionsUpdated++
		}
		if err := entry.sessionIndex.remember(result.Session); err != nil {
			return err
		}
		if entry.deleteSource {
			deleteImportedChatSourceIfSafe(b.summary, b.fs, result.Session.ID, entry.stage.RawSourceKey, entry.sourcePath)
		}
	}
	return nil
}

func runChatImport(ctx context.Context, stdout io.Writer, stderr io.Writer, opts chatImportOptions) (rerr error) {
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
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}
	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	if stack.FS == nil {
		return fmt.Errorf("filesystem client is not configured")
	}
	batchWriter, ok := stack.Repo.(repository.ChatImportBatchWriter)
	if !ok {
		return fmt.Errorf("local repository does not support batched chat import writes")
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
	batch := chatImportBatch{
		writer:  batchWriter,
		fs:      stack.FS,
		summary: &summary,
	}
	defer func() {
		if rerr != nil {
			batch.discardPending()
		}
	}()
	lock, err := syncengine.AcquireFileLock(filepath.Join(basePath(homeDir), ".pc", "sync.lock"))
	if err != nil {
		return fmt.Errorf("acquire local sync lock for chat import: %w", err)
	}
	hadMutations := false
	var itemsBefore int
	bulkErr := batchWriter.RunChatImportBulkMode(ctx, func(ctx context.Context) (bool, error) {
		before, err := stack.Repo.CountChatItems(ctx, repository.CountChatItemsFilter{})
		if err != nil {
			return hadMutations, fmt.Errorf("count chat items before import: %w", err)
		}
		itemsBefore = before
		// seenContentHashes collapses exact byte-identical duplicate files that
		// are scanned under different paths within this run (round-6 Issue 6).
		seenContentHashes := map[chatImportContentHashKey]struct{}{}
		for source, sourceRoots := range roots {
			sessionIndex, err := loadChatImportSessionIndex(ctx, stack.Repo, source, deviceID)
			if err != nil {
				return hadMutations, err
			}
			for _, root := range sourceRoots {
				files, err := chatimport.TranscriptFiles(source, root)
				if err != nil {
					return hadMutations, err
				}
				for _, file := range files {
					if err := ctx.Err(); err != nil {
						return hadMutations, err
					}
					summary.FilesScanned++
					var priorSession *repository.ChatSession
					existing, ok, err := sessionIndex.existingByOriginalPath(source, deviceID, file)
					if err != nil {
						return hadMutations, err
					}
					if ok {
						existingHash, hashErr := recordio.HashFile(file)
						if hashErr != nil {
							return hadMutations, fmt.Errorf("hash existing chat source %s: %w", file, hashErr)
						}
						prior := existing
						priorSession = &prior
						handled, mutated, err := handleExistingChatImport(ctx, stack, &batch, &sessionIndex, existing, file, deviceID, projectPaths, opts.DeleteSource)
						if err != nil {
							return hadMutations, err
						}
						if mutated {
							hadMutations = true
						}
						if handled {
							seenContentHashes[chatImportContentHashKey{source: source, hash: existingHash}] = struct{}{}
							continue
						}
					}
					session, items, err := chatimport.ParseTranscriptFile(source, file)
					if err != nil {
						if errors.Is(err, chatimport.ErrEmptyTranscript) {
							// Metadata-only / empty transcript: counted as scanned
							// (above) but never creates a chat session (round-6 Issue 4).
							continue
						}
						summary.FilesSkipped++
						_, _ = fmt.Fprintf(stderr, "Skipped (%s): %s\n", err, file)
						continue
					}
					session.SourceDeviceID = deviceID
					resolveGeminiCWD(&session, file, projectPaths, deviceID)
					session.ProjectID = chatimport.MatchProjectPath(projectPaths, session.CWD, deviceID)
					sourceSessionKey := chatImportSourceSessionKey{source: session.Source, sourceSessionID: session.SourceSessionID}
					if batch.hasSourceSession(session.Source, session.SourceSessionID) {
						if err := batch.flush(ctx); err != nil {
							return hadMutations, err
						}
					}
					if existing, ok := sessionIndex.bySourceSession[sourceSessionKey]; ok {
						if priorSession == nil {
							// Cross-file collision: a DIFFERENT source file already
							// owns this (source, source_session_id). After the
							// subagent/Gemini identity fixes this should not happen;
							// when it does, never overwrite the unrelated managed
							// source — collapse byte-identical duplicates, warn-and-
							// skip divergent ones (round-6 Issues 1 & 6 / Fix B).
							if err := defendChatImportCollision(ctx, stack, &batch, stderr, existing, file, opts.DeleteSource); err != nil {
								return hadMutations, err
							}
							continue
						}
						prior := existing
						priorSession = &prior
						handled, mutated, err := handleExistingChatImport(ctx, stack, &batch, &sessionIndex, existing, file, deviceID, projectPaths, opts.DeleteSource)
						if err != nil {
							return hadMutations, err
						}
						if mutated {
							hadMutations = true
						}
						if handled {
							continue
						}
					}
					// Link a not-yet-indexed existing session for this source id
					// before deciding whether this is a brand-new session.
					if priorSession == nil {
						if existing, lookupErr := stack.Repo.GetChatSessionBySource(ctx, session.Source, session.SourceSessionID); lookupErr == nil {
							prior := existing
							priorSession = &prior
						} else if !errors.Is(lookupErr, repository.ErrNotFound) {
							return hadMutations, fmt.Errorf("look up existing chat session: %w", lookupErr)
						}
					}
					if priorSession == nil {
						// Brand-new session: collapse exact byte-identical duplicate
						// files scanned under a different path this run. The first
						// file in scan order is the deterministic representative.
						hash, hashErr := recordio.HashFile(file)
						if hashErr != nil {
							return hadMutations, fmt.Errorf("hash chat source %s: %w", file, hashErr)
						}
						hashKey := chatImportContentHashKey{source: source, hash: hash}
						if _, dup := seenContentHashes[hashKey]; dup {
							summary.DuplicatesSkipped++
							if opts.DeleteSource {
								deleteOriginalChatSource(&summary, file)
							}
							continue
						}
						seenContentHashes[hashKey] = struct{}{}
					}
					if session.ID == "" {
						if priorSession != nil {
							session.ID = priorSession.ID
						} else {
							id, err := generateUniqueChatID(ctx, stack.Repo, session.LastActivityAt)
							if err != nil {
								return hadMutations, err
							}
							session.ID = id
						}
					}
					if err := queueReplaceChatImport(ctx, stack, &batch, &sessionIndex, session, items, file, opts.DeleteSource); err != nil {
						return hadMutations, err
					}
					hadMutations = true
				}
			}
			if err := batch.flush(ctx); err != nil {
				return hadMutations, err
			}
		}
		return hadMutations, nil
	})
	releaseErr := lock.Release()
	if bulkErr != nil {
		if releaseErr != nil {
			return errors.Join(bulkErr, releaseErr)
		}
		return bulkErr
	}
	if releaseErr != nil {
		return releaseErr
	}
	// Reconcile the summary with stored state using the authoritative item
	// count rather than CLI-side accumulators (round-6 Fix C).
	itemsAfter, err := stack.Repo.CountChatItems(ctx, repository.CountChatItemsFilter{})
	if err != nil {
		return fmt.Errorf("count chat items after import: %w", err)
	}
	summary.ItemsAfterImport = itemsAfter
	summary.ItemsDelta = itemsAfter - itemsBefore
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
func handleExistingChatImport(ctx context.Context, stack *localStack, batch *chatImportBatch, sessionIndex *chatImportSessionIndex, existing repository.ChatSession, sourcePath string, deviceID string, projectPaths []repository.ProjectPath, deleteSource bool) (bool, bool, error) {
	if existing.RawSourceKey == nil {
		return false, false, nil
	}
	comparison, err := compareChatImportSource(ctx, stack.FS, existing.ID, *existing.RawSourceKey, sourcePath)
	if err != nil {
		return false, false, fmt.Errorf("compare chat source with managed raw %s: %w", sourcePath, err)
	}
	if comparison.matches {
		return handleExactMatchChatImport(ctx, stack, batch, sessionIndex, existing, sourcePath, comparison, deviceID, projectPaths, deleteSource)
	}
	if comparison.appendOnly {
		// Append ops compute ordinals from pre-batch DB state. If the same
		// (source, source_session_id) is already queued (e.g., duplicate
		// --root entries or overlapping scan roots), the second op would
		// reuse those ordinals and trip the chat_item UNIQUE constraint
		// when WriteChatImportBatch flushes. Flush the pending batch first
		// so the next ordinal calculation sees the prior items committed.
		if batch.hasSourceSession(existing.Source, existing.SourceSessionID) {
			if err := batch.flush(ctx); err != nil {
				return false, false, err
			}
		}
		if err := queueAppendJSONLChatImport(ctx, stack, batch, sessionIndex, existing, sourcePath, comparison, deviceID, projectPaths, deleteSource); err != nil {
			return false, false, err
		}
		return true, true, nil
	}
	return false, false, nil
}

// handleExactMatchChatImport preserves the unchanged-import fast path only
// after confirming that normalized raw items still match stored database rows.
func handleExactMatchChatImport(ctx context.Context, stack *localStack, batch *chatImportBatch, sessionIndex *chatImportSessionIndex, existing repository.ChatSession, sourcePath string, comparison chatImportSourceComparison, deviceID string, projectPaths []repository.ProjectPath, deleteSource bool) (bool, bool, error) {
	session, items, err := chatimport.ParseTranscriptFile(existing.Source, comparison.managedPath)
	if err != nil {
		if errors.Is(err, chatimport.ErrEmptyTranscript) {
			batch.summary.SessionsSkipped++
			if deleteSource {
				deleteImportedChatSourceIfSafe(batch.summary, stack.FS, existing.ID, *existing.RawSourceKey, sourcePath)
			}
			return true, false, nil
		}
		return false, false, fmt.Errorf("parse managed chat raw source %s: %w", comparison.managedPath, err)
	}
	// Gemini transcripts carry no cwd, so an already-imported Gemini session is
	// NULL-attributed until the repo root is recovered from the source on disk
	// (the `.project_root` file or `projectHash`). Resolve it here so a re-import
	// repairs attribution even though the transcript bytes are unchanged; gate on
	// the unattributed case so already-attributed sessions skip the extra reads.
	if existing.ProjectID == nil {
		resolveGeminiCWD(&session, sourcePath, projectPaths, deviceID)
	}
	session.ID = existing.ID
	session.Source = existing.Source
	session.SourceSessionID = existing.SourceSessionID
	session.SourceDeviceID = deviceID
	session.ProjectID = chatimport.MatchProjectPath(projectPaths, session.CWD, deviceID)
	if session.ProjectID == nil {
		session.ProjectID = existing.ProjectID
	}
	if session.CWD == nil {
		session.CWD = existing.CWD
	}
	if session.Title == nil {
		session.Title = existing.Title
	}
	storedItems, err := stack.Repo.ListChatItems(ctx, existing.ID)
	if err != nil {
		return false, false, fmt.Errorf("list existing chat items before exact-match repair: %w", err)
	}
	// Skip only when neither the items NOR the attribution changed. An
	// attribution-only change (e.g. a Gemini session whose project just became
	// resolvable) still needs a write even though the transcript is byte-identical.
	if len(storedItems) == len(items) && chatImportItemsAlreadyStored(storedItems, items) &&
		nullableTextEqual(session.ProjectID, existing.ProjectID) && nullableTextEqual(session.CWD, existing.CWD) {
		batch.summary.SessionsSkipped++
		if deleteSource {
			deleteImportedChatSourceIfSafe(batch.summary, stack.FS, existing.ID, *existing.RawSourceKey, sourcePath)
		}
		return true, false, nil
	}
	absSourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return false, false, fmt.Errorf("resolve chat source path %q: %w", sourcePath, err)
	}
	session.OriginalSourcePath = &absSourcePath
	if err := queueReplaceChatImport(ctx, stack, batch, sessionIndex, session, items, sourcePath, deleteSource); err != nil {
		return false, false, err
	}
	return true, true, nil
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

func queueAppendJSONLChatImport(ctx context.Context, stack *localStack, batch *chatImportBatch, sessionIndex *chatImportSessionIndex, existing repository.ChatSession, sourcePath string, comparison chatImportSourceComparison, deviceID string, projectPaths []repository.ProjectPath, deleteSource bool) error {
	maxOrdinal, err := stack.Repo.MaxChatItemOrdinal(ctx, existing.ID)
	if err != nil {
		return fmt.Errorf("find existing chat item ordinal: %w", err)
	}
	existingItems, err := stack.Repo.ListChatItems(ctx, existing.ID)
	if err != nil {
		return fmt.Errorf("list existing chat items before append: %w", err)
	}
	_, managedItems, err := chatimport.ParseTranscriptFile(existing.Source, comparison.managedPath)
	if err != nil {
		return fmt.Errorf("parse managed chat raw source %s: %w", comparison.managedPath, err)
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
	ordinalStart := len(managedItems)
	session, items, err := chatimport.ParseAppendedJSONLTranscript(existing.Source, sourcePath, comparison.managedSize, comparison.sourceSize-comparison.managedSize, base, ordinalStart)
	if err != nil {
		return fmt.Errorf("parse appended chat source %s: %w", sourcePath, err)
	}
	if chatImportItemsAlreadyStored(existingItems, items) {
		items = nil
	} else if maxOrdinal+1 != ordinalStart {
		return fmt.Errorf("append-only chat import item state mismatch for %s/%s: managed raw has %d normalized items, database max ordinal is %d", existing.Source, existing.SourceSessionID, ordinalStart, maxOrdinal)
	}
	session.ID = existing.ID
	session.Source = existing.Source
	session.SourceSessionID = existing.SourceSessionID
	session.SourceDeviceID = deviceID
	session.ProjectID = chatimport.MatchProjectPath(projectPaths, session.CWD, deviceID)
	if session.ProjectID == nil {
		session.ProjectID = existing.ProjectID
	}

	stage, err := stack.FS.CopyChatSourceToStage(session.ID, sourcePath)
	if err != nil {
		return fmt.Errorf("stage appended chat source %s: %w", sourcePath, err)
	}
	rawSourceKey = stage.RawSourceKey
	session.RawSourceKey = &rawSourceKey
	if err := batch.add(ctx, pendingChatImport{
		op: repository.ChatImportOp{
			Session: repository.UpsertChatSessionInput{
				CreateChatSessionInput: session,
				ClearDeleted:           true,
			},
			ItemMode: repository.ChatImportItemModeAppend,
			Items:    items,
		},
		stage:        stage,
		sourcePath:   sourcePath,
		deleteSource: deleteSource,
		sessionIndex: sessionIndex,
	}); err != nil {
		return err
	}
	return nil
}

func chatImportItemsAlreadyStored(existing []repository.ChatItem, appended []repository.CreateChatItemInput) bool {
	if len(appended) == 0 {
		return true
	}
	byOrdinal := make(map[int]repository.ChatItem, len(existing))
	for _, item := range existing {
		byOrdinal[item.Ordinal] = item
	}
	for _, item := range appended {
		stored, ok := byOrdinal[item.Ordinal]
		if !ok || !chatImportItemMatches(stored, item) {
			return false
		}
	}
	return true
}

func chatImportItemMatches(stored repository.ChatItem, input repository.CreateChatItemInput) bool {
	return stored.Role == input.Role &&
		stored.ItemType == input.ItemType &&
		stored.SearchText == chatImportSearchText(input) &&
		nullableTextEqual(stored.Text, input.Text) &&
		nullableTextEqual(stored.RawJSON, input.RawJSON)
}

func chatImportSearchText(input repository.CreateChatItemInput) string {
	if input.SearchText != "" {
		return input.SearchText
	}
	if input.Text != nil {
		return *input.Text
	}
	return ""
}

func nullableTextEqual(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

// resolveGeminiCWD fills a Gemini session's working directory from the source on
// disk so it can be attributed like codex/claude. Gemini transcripts carry no
// `cwd`; chatimport.ResolveGeminiProjectCWD recovers the repo root from the
// sibling `.project_root` file or the `projectHash == sha256(repo root)` field.
// It is a no-op for other sources or when the cwd is already known.
func resolveGeminiCWD(session *repository.CreateChatSessionInput, sourcePath string, projectPaths []repository.ProjectPath, deviceID string) {
	if session.Source != "gemini" || session.CWD != nil {
		return
	}
	session.CWD = chatimport.ResolveGeminiProjectCWD(sourcePath, projectPaths, deviceID)
}

func queueReplaceChatImport(ctx context.Context, stack *localStack, batch *chatImportBatch, sessionIndex *chatImportSessionIndex, session repository.CreateChatSessionInput, items []repository.CreateChatItemInput, sourcePath string, deleteSource bool) error {
	stage, err := stack.FS.CopyChatSourceToStage(session.ID, sourcePath)
	if err != nil {
		return fmt.Errorf("stage chat source %s: %w", sourcePath, err)
	}
	rawSourceKey := stage.RawSourceKey
	session.RawSourceKey = &rawSourceKey
	if err := batch.add(ctx, pendingChatImport{
		op: repository.ChatImportOp{
			Session: repository.UpsertChatSessionInput{
				CreateChatSessionInput: session,
				ClearDeleted:           true,
			},
			ItemMode: repository.ChatImportItemModeReplace,
			Items:    items,
		},
		stage:        stage,
		sourcePath:   sourcePath,
		deleteSource: deleteSource,
		sessionIndex: sessionIndex,
	}); err != nil {
		return err
	}
	return nil
}

// defendChatImportCollision handles a scanned file whose parsed identity
// collides with an existing chat session owned by a DIFFERENT source file. It
// never overwrites the existing managed source: byte-identical content is
// collapsed as a duplicate, divergent content is reported on stderr and skipped
// so distinct transcripts can never silently replace one another.
func defendChatImportCollision(ctx context.Context, stack *localStack, batch *chatImportBatch, stderr io.Writer, existing repository.ChatSession, sourcePath string, deleteSource bool) error {
	if existing.RawSourceKey != nil {
		matches, err := sourceMatchesManagedRaw(ctx, stack.FS, existing.ID, *existing.RawSourceKey, sourcePath)
		if err != nil {
			return fmt.Errorf("compare chat source with managed raw %s: %w", sourcePath, err)
		}
		if matches {
			// Same content already imported (e.g. a session whose
			// original_source_path was lost, re-scanned under a different
			// path): treat as an unchanged skip and let --delete-source reclaim
			// the redundant source.
			batch.summary.SessionsSkipped++
			if deleteSource {
				deleteImportedChatSourceIfSafe(batch.summary, stack.FS, existing.ID, *existing.RawSourceKey, sourcePath)
			}
			return nil
		}
	}
	batch.summary.CollisionsSkipped++
	owner := "an earlier import"
	if existing.OriginalSourcePath != nil {
		owner = *existing.OriginalSourcePath
	}
	_, _ = fmt.Fprintf(stderr, "warning: skipping %s: source session %q is already imported from %s with different content; not overwriting\n", sourcePath, existing.SourceSessionID, owner)
	return nil
}

// deleteImportedChatSourceIfSafe removes the original source only when it is
// not the managed raw file Personal Context owns.
func deleteImportedChatSourceIfSafe(summary *chatImportSummary, fs *filesystem.Client, chatID string, rawSourceKey string, sourcePath string) {
	if isSameAsManagedFile(fs, chatID, rawSourceKey, sourcePath) {
		return
	}
	deleteOriginalChatSource(summary, sourcePath)
}

func deleteOriginalChatSource(summary *chatImportSummary, sourcePath string) {
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
	var parentSourceSessionID string
	var limit int
	var offset int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List chat sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runChatList(cmd.Context(), stdout, stderr, chatListOptions{Format: format, Source: source, ProjectID: project, Unassigned: unassigned, ParentSourceSessionID: parentSourceSessionID, Limit: limit, Offset: offset})
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "Output format (table|json|ids)")
	cmd.Flags().StringVar(&source, "agent", "", "Filter by agent (codex|claude|gemini)")
	cmd.Flags().StringVar(&project, "project", "", "Filter by project ID")
	cmd.Flags().BoolVar(&unassigned, "unassigned", false, "Show only sessions without a project")
	cmd.Flags().StringVar(&parentSourceSessionID, "parent-source-session-id", "", "Show only subagent sessions whose parent has this source session ID")
	cmd.Flags().IntVar(&limit, "limit", defaultChatListLimit, "Maximum sessions to return")
	cmd.Flags().IntVar(&offset, "offset", 0, "Offset for pagination")
	return cmd
}

type chatListOptions struct {
	Format                string
	Source                string
	ProjectID             string
	Unassigned            bool
	ParentSourceSessionID string
	Limit                 int
	Offset                int
}

type chatSessionJSON struct {
	ID                    string  `json:"id"`
	Source                string  `json:"source"`
	SourceSessionID       string  `json:"source_session_id"`
	ParentSourceSessionID *string `json:"parent_source_session_id"`
	SourceDeviceID        string  `json:"source_device_id"`
	ProjectID             *string `json:"project_id"`
	CWD                   *string `json:"cwd"`
	Title                 *string `json:"title"`
	StartedAt             string  `json:"started_at"`
	LastActivityAt        string  `json:"last_activity_at"`
	OriginalSourcePath    *string `json:"original_source_path"`
	RawSourceKey          *string `json:"raw_source_key"`
	DeletedAt             *string `json:"deleted_at"`
}

// chatRelationJSON is a concise reference to a related chat session (e.g. a
// subagent of a parent transcript) for pc chat show JSON output.
type chatRelationJSON struct {
	ID              string  `json:"id"`
	SourceSessionID string  `json:"source_session_id"`
	Title           *string `json:"title"`
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
	if strings.TrimSpace(opts.ParentSourceSessionID) != "" {
		parent := strings.TrimSpace(opts.ParentSourceSessionID)
		filter.ParentSourceSessionID = &parent
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
	var parentSourceSessionID string
	var limit int
	var offset int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search chat transcripts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChatSearch(cmd.Context(), stdout, stderr, args[0], chatSearchOptions{Format: format, Source: source, ProjectID: project, IncludeTools: includeTools, ParentSourceSessionID: parentSourceSessionID, Limit: limit, Offset: offset})
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "Output format (table|json)")
	cmd.Flags().StringVar(&source, "agent", "", "Filter by agent (codex|claude|gemini)")
	cmd.Flags().StringVar(&project, "project", "", "Filter by project ID")
	cmd.Flags().BoolVar(&includeTools, "include-tool-outputs", false, "Include tool output items")
	cmd.Flags().StringVar(&parentSourceSessionID, "parent-source-session-id", "", "Search only subagent sessions whose parent has this source session ID")
	cmd.Flags().IntVar(&limit, "limit", defaultChatListLimit, "Maximum results to return")
	cmd.Flags().IntVar(&offset, "offset", 0, "Offset for pagination")
	return cmd
}

type chatSearchOptions struct {
	Format                string
	Source                string
	ProjectID             string
	IncludeTools          bool
	ParentSourceSessionID string
	Limit                 int
	Offset                int
}

// chatSearchJSON carries the chat hit plus backward-compatible top-level
// fields. The nested `session` object is preserved for existing consumers; the
// top-level `id`, `source`, `project_id`, `date`, and `score` fields give the
// same flat shape `pc search` exposes so clients reading top-level keys work
// against both commands. `score` is the FTS relevance rank (higher is better).
type chatSearchJSON struct {
	ID        string          `json:"id"`
	Source    string          `json:"source"`
	ProjectID *string         `json:"project_id"`
	Date      string          `json:"date"`
	Score     float64         `json:"score"`
	Session   chatSessionJSON `json:"session"`
	Ordinal   int             `json:"ordinal"`
	Role      string          `json:"role"`
	Type      string          `json:"type"`
	Text      *string         `json:"text"`
	Snippet   string          `json:"snippet"`
}

func runChatSearch(ctx context.Context, stdout io.Writer, _ io.Writer, query string, opts chatSearchOptions) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("query must not be empty")
	}
	if err := repository.ValidateSearchQuery(query); err != nil {
		return err
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
	if strings.TrimSpace(opts.ParentSourceSessionID) != "" {
		parent := strings.TrimSpace(opts.ParentSourceSessionID)
		filter.ParentSourceSessionID = &parent
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
		// JSON `total` must be the full match count under the same filter, not
		// the returned page size (DECISIONS.md 2026-05-08 e8f9g0).
		total, err := stack.Repo.CountSearchChatItems(ctx, filter)
		if err != nil {
			return fmt.Errorf("count chat search matches: %w", err)
		}
		items := make([]chatSearchJSON, 0, len(results))
		for _, result := range results {
			items = append(items, chatSearchJSON{
				ID:        result.Session.ID,
				Source:    result.Session.Source,
				ProjectID: result.Session.ProjectID,
				Date:      result.Session.LastActivityAt.Format("2006-01-02"),
				Score:     result.Rank,
				Session:   chatSessionToJSON(result.Session),
				Ordinal:   result.Item.Ordinal,
				Role:      result.Item.Role,
				Type:      result.Item.ItemType,
				Text:      result.Item.Text,
				Snippet:   result.Snippet,
			})
		}
		var nextCursor *string
		if hasMore {
			next := fmt.Sprintf("%d", opts.Offset+opts.Limit)
			nextCursor = &next
		}
		return listpage.WriteJSON(stdout, listpage.Response[chatSearchJSON]{Items: items, Total: total, NextCursor: nextCursor})
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
	// A session is a parent transcript when other sessions name it as their
	// parent. Surface those subagents so the relationship is navigable.
	subagents, err := stack.Repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{
		Source:                &session.Source,
		ParentSourceSessionID: &session.SourceSessionID,
	})
	if err != nil {
		return fmt.Errorf("list subagent chats: %w", err)
	}
	switch opts.Format {
	case "json":
		relations := make([]chatRelationJSON, 0, len(subagents))
		for _, subagent := range subagents {
			relations = append(relations, chatRelationJSON{
				ID:              subagent.ID,
				SourceSessionID: subagent.SourceSessionID,
				Title:           subagent.Title,
			})
		}
		return writeIndentedJSON(stdout, struct {
			Session   chatSessionJSON       `json:"session"`
			Subagents []chatRelationJSON    `json:"subagents,omitempty"`
			Items     []repository.ChatItem `json:"items"`
		}{Session: chatSessionToJSON(session), Subagents: relations, Items: items})
	case "text":
		return writeChatTranscript(stdout, session, items, subagents, opts)
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
		ParentSourceSessionID: session.ParentSourceSessionID,
		SourceDeviceID:        session.SourceDeviceID, ProjectID: session.ProjectID, CWD: session.CWD,
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
	_, _ = fmt.Fprintln(tw, "CHAT\tDATE\tPROJECT\tORD\tROLE\tSNIPPET")
	for _, result := range results {
		snippet := result.Snippet
		if snippet == "" {
			snippet = result.Item.SearchText
		}
		project := ""
		if result.Session.ProjectID != nil {
			project = *result.Session.ProjectID
		}
		date := result.Session.LastActivityAt.Format("2006-01-02")
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", result.Session.ID, date, project, result.Item.Ordinal, result.Item.Role, truncate(strings.ReplaceAll(snippet, "\n", " "), 100))
	}
	return tw.Flush()
}

func writeChatTranscript(w io.Writer, session repository.ChatSession, items []repository.ChatItem, subagents []repository.ChatSession, opts chatShowOptions) error {
	if chatStdoutIsTerminal(w) {
		buffer := &bytes.Buffer{}
		_ = renderChatTranscript(buffer, session, items, subagents, opts)
		if err := runChatPager(buffer.String()); err != nil {
			return fmt.Errorf("page chat transcript: %w", err)
		}
		if strings.TrimSpace(os.Getenv("PAGER")) != "" {
			return nil
		}
		_, err := io.WriteString(w, buffer.String())
		return err
	}
	return renderChatTranscript(w, session, items, subagents, opts)
}

func renderChatTranscript(w io.Writer, session repository.ChatSession, items []repository.ChatItem, subagents []repository.ChatSession, opts chatShowOptions) error {
	_, _ = fmt.Fprintf(w, "ID:              %s\n", session.ID)
	_, _ = fmt.Fprintf(w, "Agent:           %s\n", session.Source)
	_, _ = fmt.Fprintf(w, "Source Session:  %s\n", session.SourceSessionID)
	if session.ParentSourceSessionID != nil {
		_, _ = fmt.Fprintf(w, "Parent Session:  %s\n", *session.ParentSourceSessionID)
	}
	if session.ProjectID != nil {
		_, _ = fmt.Fprintf(w, "Project:         %s\n", *session.ProjectID)
	}
	_, _ = fmt.Fprintf(w, "Last Activity:   %s\n", timeutil.FormatUTCMillis(session.LastActivityAt))
	if len(subagents) > 0 {
		_, _ = fmt.Fprintf(w, "Subagents:       %d\n", len(subagents))
		for _, sub := range subagents {
			title := ""
			if sub.Title != nil {
				title = " " + truncate(*sub.Title, 60)
			}
			_, _ = fmt.Fprintf(w, "  - %s (%s)%s\n", sub.ID, sub.SourceSessionID, title)
		}
	}
	_, _ = fmt.Fprintln(w)
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
