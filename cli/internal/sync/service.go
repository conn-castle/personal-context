package sync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/s3client"
	"github.com/conn-castle/personal-context/cli/internal/syncengine"
)

// defaultWarnWriter is the destination used when callers do not provide one.
// Wrapped in a function variable so tests can capture warnings without
// reaching into the process-global os.Stderr.
var defaultWarnWriter io.Writer = os.Stderr

const (
	syncedFilePermission        = 0o644
	syncedChatRawFilePermission = 0o600
)

// osFileChmod wraps *os.File.Chmod so tests can inject I/O failures without
// modifying the filesystem. Production code never reassigns this variable.
var osFileChmod = func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }

// LocalFiles resolves canonical local figure/data paths for sync operations.
type LocalFiles interface {
	ResolveFigurePath(recordID string, filename string) (string, error)
	ResolveDataFilePath(recordID string, filename string) (string, error)
	ResolveChatSourcePath(chatSessionID string, rawSourceKey string) (string, error)
}

// ObjectStore provides the cloud object operations required by sync.
type ObjectStore interface {
	Upload(ctx context.Context, key string, body io.Reader) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	UpdateVersion(ctx context.Context, version int64, updatedAt string) error
}

// SessionManager manages the on-disk sync lock and last-sync cursor.
type SessionManager interface {
	Begin() (syncengine.SyncWindow, *syncengine.FileLock, error)
	Complete(syncengine.SyncWindow) error
}

// Service coordinates bidirectional sync between a local repo/filesystem and cloud repo/object store.
type Service struct {
	localRepo    repository.Repository
	cloudRepo    repository.Repository
	localFS      LocalFiles
	cloudObjects ObjectStore
	session      SessionManager
	warnWriter   io.Writer
}

// syncDirection describes one source-to-target reconciliation pass.
type syncDirection struct {
	name        string
	sourceLabel string
	targetLabel string
	sourceRepo  repository.Repository
	targetRepo  repository.Repository
	winningSide Winner
	apply       func(context.Context, RecordBundle, *RecordBundle) error
}

// NewService validates and constructs a sync service.
func NewService(
	localRepo repository.Repository,
	cloudRepo repository.Repository,
	localFS LocalFiles,
	objects ObjectStore,
	session SessionManager,
) (*Service, error) {
	switch {
	case localRepo == nil:
		return nil, fmt.Errorf("local repository is required")
	case cloudRepo == nil:
		return nil, fmt.Errorf("cloud repository is required")
	case localFS == nil:
		return nil, fmt.Errorf("local filesystem is required")
	case objects == nil:
		return nil, fmt.Errorf("object store is required")
	case session == nil:
		return nil, fmt.Errorf("session manager is required")
	default:
		return &Service{
			localRepo:    localRepo,
			cloudRepo:    cloudRepo,
			localFS:      localFS,
			cloudObjects: objects,
			session:      session,
			warnWriter:   defaultWarnWriter,
		}, nil
	}
}

// SetWarnWriter overrides where degraded-durability and similar non-fatal
// sync warnings are written. nil restores the package default. Callers who
// want warnings captured by their own logger should call this before Sync.
func (s *Service) SetWarnWriter(w io.Writer) {
	if w == nil {
		s.warnWriter = defaultWarnWriter
		return
	}
	s.warnWriter = w
}

// Sync performs a full push-then-pull cycle and advances the cursor only after success.
func (s *Service) Sync(ctx context.Context) (err error) {
	window, lock, err := s.session.Begin()
	if err != nil {
		return err
	}
	defer func() {
		releaseErr := lock.Release()
		if releaseErr != nil {
			if err == nil {
				err = releaseErr
			} else {
				err = errors.Join(err, releaseErr)
			}
		}
	}()

	if err := s.pushChangedRecords(ctx, window.LastSync); err != nil {
		return err
	}
	if err := s.pushChangedChats(ctx, window.LastSync); err != nil {
		return err
	}
	if err := s.pullChangedRecords(ctx, window.LastSync); err != nil {
		return err
	}
	if err := s.pullChangedChats(ctx, window.LastSync); err != nil {
		return err
	}
	if err := s.updateCloudVersion(ctx); err != nil {
		return err
	}
	if err := s.session.Complete(window); err != nil {
		return err
	}
	return nil
}

func (s *Service) pushChangedRecords(ctx context.Context, since time.Time) error {
	return s.syncChangedRecords(ctx, since, syncDirection{
		name:        "push",
		sourceLabel: "local",
		targetLabel: "cloud",
		sourceRepo:  s.localRepo,
		targetRepo:  s.cloudRepo,
		winningSide: WinnerLocal,
		apply:       s.applyBundleToCloud,
	})
}

func (s *Service) pullChangedRecords(ctx context.Context, since time.Time) error {
	return s.syncChangedRecords(ctx, since, syncDirection{
		name:        "pull",
		sourceLabel: "cloud",
		targetLabel: "local",
		sourceRepo:  s.cloudRepo,
		targetRepo:  s.localRepo,
		winningSide: WinnerCloud,
		apply:       s.applyBundleToLocal,
	})
}

func (s *Service) pushChangedChats(ctx context.Context, since time.Time) error {
	return syncChangedChatsDirected(ctx, since, "push", s.localRepo, s.cloudRepo, WinnerLocal, s.transferChatRawSource, s.warnWriter)
}

func (s *Service) pullChangedChats(ctx context.Context, since time.Time) error {
	return syncChangedChatsDirected(ctx, since, "pull", s.cloudRepo, s.localRepo, WinnerCloud, s.transferChatRawSource, s.warnWriter)
}

// chatRawSyncReport aggregates degraded-durability warnings observed during
// raw chat source push/pull. Sync continues on missing local files (push) or
// missing cloud objects (pull); doctor is the failing integrity gate.
type chatRawSyncReport struct {
	MissingLocal []string
	MissingCloud []string
}

// chatRawTransferFn is the optional raw-source push/pull hook installed by
// Service. Unit tests that call syncChangedChats directly pass nil to skip
// raw transfer.
type chatRawTransferFn func(ctx context.Context, name string, session repository.ChatSession, report *chatRawSyncReport) error

func syncChangedChats(ctx context.Context, since time.Time, name string, source repository.Repository, target repository.Repository, rawTransfer chatRawTransferFn, warnWriter io.Writer) error {
	return syncChangedChatsDirected(ctx, since, name, source, target, WinnerLocal, rawTransfer, warnWriter)
}

func syncChangedChatsDirected(ctx context.Context, since time.Time, name string, source repository.Repository, target repository.Repository, winningSide Winner, rawTransfer chatRawTransferFn, warnWriter io.Writer) error {
	sessions, err := source.ListChatSessions(ctx, repository.ListChatSessionsFilter{IncludeDeleted: true, UpdatedAfter: &since})
	if err != nil {
		return fmt.Errorf("list %s chat changes: %w", name, err)
	}
	report := chatRawSyncReport{}
	for _, session := range sessions {
		if err := ctx.Err(); err != nil {
			return err
		}
		targetSession, targetExists, err := loadChatSession(ctx, target, session.ID)
		if err != nil {
			return fmt.Errorf("load target chat session %s: %w", session.ID, err)
		}
		targetSourceSession, targetSourceExists, err := loadChatSessionBySource(ctx, target, session.Source, session.SourceSessionID)
		if err != nil {
			return fmt.Errorf("load target chat source %s/%s: %w", session.Source, session.SourceSessionID, err)
		}
		if targetSourceExists && targetSourceSession.ID != session.ID {
			return fmt.Errorf(
				"%s chat source %s/%s already exists with id %s; source id %s cannot be synced without manual resolution",
				name,
				session.Source,
				session.SourceSessionID,
				targetSourceSession.ID,
				session.ID,
			)
		}
		if targetExists {
			winner, err := resolveChatWinnerForDirection(session, targetSession, winningSide)
			if err != nil {
				return fmt.Errorf("resolve %s chat session %s: %w", name, session.ID, err)
			}
			if winner != winningSide {
				shouldTransferRaw := rawTransfer != nil && session.RawSourceKey != nil && session.DeletedAt == nil
				if winner == WinnerNone && shouldTransferRaw {
					if err := rawTransfer(ctx, name, session, &report); err != nil {
						return err
					}
				}
				continue
			}
		}
		pulling := winningSide == WinnerCloud
		shouldTransferRaw := rawTransfer != nil && session.RawSourceKey != nil && session.DeletedAt == nil
		// Push raw source bytes before metadata so the cloud row never
		// advertises a key whose object is absent. Pull does the inverse below:
		// write metadata first so a failed local upsert cannot orphan a file.
		if shouldTransferRaw && !pulling {
			if err := rawTransfer(ctx, name, session, &report); err != nil {
				return err
			}
		}
		createdAt := session.CreatedAt
		updatedAt := session.UpdatedAt
		stored, _, err := target.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:                    session.ID,
				Source:                session.Source,
				SourceSessionID:       session.SourceSessionID,
				ParentSourceSessionID: session.ParentSourceSessionID,
				SourceDeviceID:        session.SourceDeviceID,
				ProjectID:             session.ProjectID,
				CWD:                   session.CWD,
				Title:                 session.Title,
				StartedAt:             session.StartedAt,
				LastActivityAt:        session.LastActivityAt,
				OriginalSourcePath:    session.OriginalSourcePath,
				RawSourceKey:          session.RawSourceKey,
				CreatedAt:             &createdAt,
				UpdatedAt:             &updatedAt,
				DeletedAt:             session.DeletedAt,
			},
			ClearDeleted: session.DeletedAt == nil,
		})
		if err != nil {
			return fmt.Errorf("%s chat session %s: %w", name, session.ID, err)
		}
		items, err := source.ListChatItems(ctx, session.ID)
		if err != nil {
			return fmt.Errorf("list %s chat items %s: %w", name, session.ID, err)
		}
		inputs := make([]repository.CreateChatItemInput, 0, len(items))
		for _, item := range items {
			createdAt := item.CreatedAt
			inputs = append(inputs, repository.CreateChatItemInput{
				SessionID:  stored.ID,
				Ordinal:    item.Ordinal,
				Role:       item.Role,
				ItemType:   item.ItemType,
				Text:       item.Text,
				SearchText: item.SearchText,
				RawJSON:    item.RawJSON,
				CreatedAt:  &createdAt,
			})
		}
		if err := target.ReplaceChatItems(ctx, stored.ID, inputs); err != nil {
			return fmt.Errorf("%s chat items %s: %w", name, stored.ID, err)
		}
		if shouldTransferRaw && pulling {
			if err := rawTransfer(ctx, name, session, &report); err != nil {
				return err
			}
		}
	}
	if warnWriter == nil {
		warnWriter = defaultWarnWriter
	}
	if total := len(report.MissingLocal); total > 0 {
		if _, err := fmt.Fprintf(warnWriter, "%d raw chat source files missing locally; run pc doctor --verbose for details\n", total); err != nil {
			return fmt.Errorf("write local chat raw source warning: %w", err)
		}
	}
	if total := len(report.MissingCloud); total > 0 {
		if _, err := fmt.Fprintf(warnWriter, "%d raw chat source objects missing in cloud; run pc doctor --verbose for details\n", total); err != nil {
			return fmt.Errorf("write cloud chat raw source warning: %w", err)
		}
	}
	return nil
}

func loadChatSession(ctx context.Context, repo repository.Repository, id string) (repository.ChatSession, bool, error) {
	session, err := repo.GetChatSessionByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.ChatSession{}, false, nil
		}
		return repository.ChatSession{}, false, err
	}
	return session, true, nil
}

func loadChatSessionBySource(ctx context.Context, repo repository.Repository, source string, sourceSessionID string) (repository.ChatSession, bool, error) {
	session, err := repo.GetChatSessionBySource(ctx, source, sourceSessionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.ChatSession{}, false, nil
		}
		return repository.ChatSession{}, false, err
	}
	return session, true, nil
}

func resolveChatWinnerForDirection(source repository.ChatSession, target repository.ChatSession, winningSide Winner) (Winner, error) {
	local := source
	cloud := target
	if winningSide == WinnerCloud {
		local = target
		cloud = source
	}
	outcome, err := syncengine.ResolveRecordWinner(
		&repository.Record{ID: local.ID, UpdatedAt: local.UpdatedAt, DeletedAt: local.DeletedAt},
		&repository.Record{ID: cloud.ID, UpdatedAt: cloud.UpdatedAt, DeletedAt: cloud.DeletedAt},
	)
	if err != nil {
		return "", err
	}
	switch outcome {
	case syncengine.OutcomeLocal:
		return WinnerLocal, nil
	case syncengine.OutcomeRemote:
		return WinnerCloud, nil
	default:
		return WinnerNone, nil
	}
}

// transferChatRawSource uploads (push) or downloads (pull) the managed raw
// chat source object for one chat session. Missing files at either end are
// recorded as warnings without halting sync.
func (s *Service) transferChatRawSource(ctx context.Context, name string, session repository.ChatSession, report *chatRawSyncReport) error {
	if session.RawSourceKey == nil {
		return nil
	}
	key := *session.RawSourceKey
	localPath, err := s.localFS.ResolveChatSourcePath(session.ID, key)
	if err != nil {
		return fmt.Errorf("%s chat raw source path %s: %w", name, session.ID, err)
	}
	switch name {
	case "push":
		uploaded, err := s.uploadChatSourceIfPresent(ctx, key, localPath)
		if err != nil {
			return fmt.Errorf("upload chat raw source %s: %w", session.ID, err)
		}
		if !uploaded {
			report.MissingLocal = append(report.MissingLocal, session.ID)
		}
	case "pull":
		if err := s.downloadChatSourceIfPresent(ctx, key, localPath); err != nil {
			if errors.Is(err, errObjectNotFound) {
				report.MissingCloud = append(report.MissingCloud, session.ID)
				return nil
			}
			return fmt.Errorf("download chat raw source %s: %w", session.ID, err)
		}
	}
	return nil
}

// errObjectNotFound signals a confirmed cloud object miss as distinct from
// auth or network errors that the caller should still surface.
var errObjectNotFound = errors.New("cloud object not found")

func (s *Service) uploadChatSourceIfPresent(ctx context.Context, key string, path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat local chat source %s: %w", path, err)
	}
	if err := s.uploadFile(ctx, key, path); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) downloadChatSourceIfPresent(ctx context.Context, key string, path string) error {
	body, err := s.cloudObjects.Download(ctx, key)
	if err != nil {
		if isCloudObjectNotFound(err) {
			return errObjectNotFound
		}
		return fmt.Errorf("download %s: %w", key, err)
	}
	defer func() { _ = body.Close() }()
	if err := writeReaderToPathWithPerm(path, body, syncedChatRawFilePermission); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// isCloudObjectNotFound reports whether an ObjectStore.Download error is a
// confirmed object miss as opposed to auth/network or missing-bucket failure.
// Delegating to s3client.IsNotFound keeps detection rooted in typed AWS errors
// rather than fragile error-message substrings that could match unrelated
// failures (e.g., "bucket not found", "endpoint not found").
func isCloudObjectNotFound(err error) bool {
	return s3client.IsNotFound(err)
}

// syncChangedRecords runs one mirrored push/pull pass with consistent error labels.
func (s *Service) syncChangedRecords(
	ctx context.Context,
	since time.Time,
	direction syncDirection,
) error {
	if err := syncRegistries(ctx, direction.sourceRepo, direction.targetRepo); err != nil {
		return fmt.Errorf("sync %s registries: %w", direction.name, err)
	}

	records, err := s.listChangedRecords(ctx, direction.sourceRepo, since)
	if err != nil {
		return fmt.Errorf("list %s changes: %w", direction.sourceLabel, err)
	}

	for _, record := range records {
		sourceBundle, err := s.bundleForRecord(ctx, direction.sourceRepo, record)
		if err != nil {
			return fmt.Errorf("load %s bundle %s: %w", direction.sourceLabel, record.ID, err)
		}

		targetBundle, exists, err := s.loadBundle(ctx, direction.targetRepo, record.ID)
		if err != nil {
			return fmt.Errorf("load %s bundle %s: %w", direction.targetLabel, record.ID, err)
		}
		if !exists {
			if err := direction.apply(ctx, sourceBundle, nil); err != nil {
				return fmt.Errorf("%s new %s record %s: %w", direction.name, direction.sourceLabel, record.ID, err)
			}
			continue
		}

		localBundle := sourceBundle
		cloudBundle := targetBundle
		if direction.winningSide == WinnerCloud {
			localBundle = targetBundle
			cloudBundle = sourceBundle
		}

		_, winner, err := ResolveBundle(localBundle, cloudBundle)
		if err != nil {
			return fmt.Errorf("resolve %s bundle %s: %w", direction.name, record.ID, err)
		}
		if winner != direction.winningSide {
			if winner == WinnerNone && sourceBundleCanRepairTargetChildren(sourceBundle, targetBundle) {
				if err := direction.apply(ctx, sourceBundle, &targetBundle); err != nil {
					return fmt.Errorf("%s repair %s record %s: %w", direction.name, direction.sourceLabel, record.ID, err)
				}
			}
			continue
		}
		if err := direction.apply(ctx, sourceBundle, &targetBundle); err != nil {
			return fmt.Errorf("%s %s record %s: %w", direction.name, direction.sourceLabel, record.ID, err)
		}
	}

	return nil
}

func syncRegistries(ctx context.Context, source repository.Repository, target repository.Repository) error {
	projects, err := source.ListProjects(ctx, true)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	for _, project := range projects {
		if _, err := target.UpsertProjectForImport(ctx, project); err != nil {
			return fmt.Errorf("upsert project %s: %w", project.ID, err)
		}
	}
	devices, err := source.ListDevices(ctx, true)
	if err != nil {
		return fmt.Errorf("list devices: %w", err)
	}
	for _, device := range devices {
		if _, err := target.UpsertDeviceForImport(ctx, device); err != nil {
			return fmt.Errorf("upsert device %s: %w", device.ID, err)
		}
	}
	projectPaths, err := source.ListProjectPaths(ctx, nil)
	if err != nil {
		return fmt.Errorf("list project paths: %w", err)
	}
	for _, path := range projectPaths {
		createdAt := path.CreatedAt
		updatedAt := path.UpdatedAt
		if _, _, err := target.UpsertProjectPath(ctx, repository.CreateProjectPathInput{
			ProjectID: path.ProjectID,
			Path:      path.Path,
			DeviceID:  path.DeviceID,
			CreatedAt: &createdAt,
			UpdatedAt: &updatedAt,
		}); err != nil {
			return fmt.Errorf("upsert project path %s: %w", path.Path, err)
		}
	}
	return nil
}

func (s *Service) updateCloudVersion(ctx context.Context) error {
	version, err := s.cloudRepo.GetSyncVersion(ctx)
	if err != nil {
		return fmt.Errorf("get cloud sync version: %w", err)
	}
	updatedAt := version.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	if err := s.cloudObjects.UpdateVersion(ctx, version.Version, updatedAt); err != nil {
		return fmt.Errorf("update cloud sync version object: %w", err)
	}
	return nil
}

func (s *Service) listChangedRecords(
	ctx context.Context,
	repo repository.Repository,
	since time.Time,
) ([]repository.Record, error) {
	records, err := repo.ListRecords(ctx, repository.ListRecordsFilter{IncludeDeleted: true})
	if err != nil {
		return nil, err
	}
	return syncengine.FilterRecordsUpdatedSince(records, since), nil
}

func (s *Service) loadBundle(
	ctx context.Context,
	repo repository.Repository,
	recordID string,
) (RecordBundle, bool, error) {
	record, err := repo.GetRecordByID(ctx, recordID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return RecordBundle{}, false, nil
		}
		return RecordBundle{}, false, err
	}
	bundle, err := s.bundleForRecord(ctx, repo, record)
	if err != nil {
		return RecordBundle{}, false, err
	}
	return bundle, true, nil
}

func (s *Service) bundleForRecord(
	ctx context.Context,
	repo repository.Repository,
	record repository.Record,
) (RecordBundle, error) {
	figures, err := repo.ListRecordFiguresByRecordID(ctx, record.ID)
	if err != nil {
		return RecordBundle{}, fmt.Errorf("list figures: %w", err)
	}
	dataFiles, err := repo.ListRecordDataFilesByRecordID(ctx, record.ID)
	if err != nil {
		return RecordBundle{}, fmt.Errorf("list data files: %w", err)
	}
	return RecordBundle{
		Record:    record,
		Figures:   figures,
		DataFiles: dataFiles,
	}, nil
}

func (s *Service) applyBundleToCloud(
	ctx context.Context,
	desired RecordBundle,
	existing *RecordBundle,
) error {
	if err := applyRecord(ctx, s.cloudRepo, desired.Record, existing != nil); err != nil {
		return fmt.Errorf("apply record to cloud: %w", err)
	}
	if err := s.applyFiguresToCloud(ctx, desired.Record.ID, desired.Figures, figuresOf(existing)); err != nil {
		return fmt.Errorf("apply figures to cloud: %w", err)
	}
	if err := s.applyDataFilesToCloud(ctx, desired.Record.ID, desired.DataFiles, dataFilesOf(existing)); err != nil {
		return fmt.Errorf("apply data files to cloud: %w", err)
	}
	return nil
}

func (s *Service) applyBundleToLocal(
	ctx context.Context,
	desired RecordBundle,
	existing *RecordBundle,
) error {
	if err := applyRecord(ctx, s.localRepo, desired.Record, existing != nil); err != nil {
		return fmt.Errorf("apply record to local: %w", err)
	}
	if err := s.applyFiguresToLocal(ctx, desired.Record.ID, desired.Figures, figuresOf(existing)); err != nil {
		return fmt.Errorf("apply figures to local: %w", err)
	}
	if err := s.applyDataFilesToLocal(ctx, desired.Record.ID, desired.DataFiles, dataFilesOf(existing)); err != nil {
		return fmt.Errorf("apply data files to local: %w", err)
	}
	return nil
}

func applyRecord(
	ctx context.Context,
	repo repository.Repository,
	record repository.Record,
	exists bool,
) error {
	if exists {
		_, err := repo.UpdateRecord(ctx, repository.UpdateRecordInput{
			ID:             record.ID,
			Date:           record.Date,
			DayOrder:       record.DayOrder,
			HTMLContent:    record.HTMLContent,
			Notes:          record.Notes,
			ProjectID:      record.ProjectID,
			SourceDeviceID: record.SourceDeviceID,
			SourceRef:      record.SourceRef,
			GitRemoteURL:   record.GitRemoteURL,
			GitHash:        record.GitHash,
			UpdatedAt:      &record.UpdatedAt,
			SetDeletedAt:   true,
			DeletedAt:      record.DeletedAt,
		})
		return err
	}

	_, err := repo.CreateRecord(ctx, repository.CreateRecordInput{
		ID:             record.ID,
		Date:           record.Date,
		DayOrder:       record.DayOrder,
		HTMLContent:    record.HTMLContent,
		Notes:          record.Notes,
		ProjectID:      record.ProjectID,
		SourceDeviceID: record.SourceDeviceID,
		SourceRef:      record.SourceRef,
		GitRemoteURL:   record.GitRemoteURL,
		GitHash:        record.GitHash,
		CreatedAt:      &record.CreatedAt,
		UpdatedAt:      &record.UpdatedAt,
		DeletedAt:      record.DeletedAt,
	})
	return err
}

func (s *Service) applyFiguresToCloud(
	ctx context.Context,
	recordID string,
	desired []repository.RecordFigure,
	existing []repository.RecordFigure,
) error {
	if err := s.uploadDesiredFigureFiles(ctx, recordID, desired); err != nil {
		return err
	}

	plan, err := PlanFigureReconciliation(recordID, existing, desired)
	if err != nil {
		return err
	}

	existingByID := indexFiguresByID(existing)

	for _, create := range plan.Creates {
		if _, err := s.cloudRepo.CreateRecordFigure(ctx, create); err != nil {
			return err
		}
	}

	for _, update := range plan.Updates {
		old := existingByID[update.ID]
		if _, err := s.cloudRepo.UpdateRecordFigure(ctx, update); err != nil {
			return err
		}
		if old.S3Key != update.S3Key {
			if err := s.cloudObjects.Delete(ctx, old.S3Key); err != nil {
				return err
			}
		}
	}

	for _, deleteID := range plan.DeleteIDs {
		old := existingByID[deleteID]
		if err := s.cloudRepo.DeleteRecordFigure(ctx, deleteID); err != nil {
			return err
		}
		if err := s.cloudObjects.Delete(ctx, old.S3Key); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) applyDataFilesToCloud(
	ctx context.Context,
	recordID string,
	desired []repository.RecordDataFile,
	existing []repository.RecordDataFile,
) error {
	plan, err := PlanDataFileReconciliation(recordID, existing, desired)
	if err != nil {
		return err
	}

	existingByID := indexDataFilesByID(existing)

	for _, create := range plan.Creates {
		path, err := s.localFS.ResolveDataFilePath(recordID, create.Filename)
		if err != nil {
			return err
		}
		if _, err := s.uploadDataFileIfPresent(ctx, create.S3Key, path); err != nil {
			return err
		}
		if _, err := s.cloudRepo.CreateRecordDataFile(ctx, create); err != nil {
			return err
		}
	}

	for _, update := range plan.Updates {
		path, err := s.localFS.ResolveDataFilePath(recordID, update.Filename)
		if err != nil {
			return err
		}
		uploaded, err := s.uploadDataFileIfPresent(ctx, update.S3Key, path)
		if err != nil {
			return err
		}
		old := existingByID[update.ID]
		if !uploaded && old.S3Key != update.S3Key {
			return fmt.Errorf(
				"local data file %s is required to change data file s3_key from %s to %s",
				path,
				old.S3Key,
				update.S3Key,
			)
		}
		if _, err := s.cloudRepo.UpdateRecordDataFile(ctx, update); err != nil {
			return err
		}
		if uploaded && old.S3Key != update.S3Key {
			if err := s.cloudObjects.Delete(ctx, old.S3Key); err != nil {
				return err
			}
		}
	}

	for _, deleteID := range plan.DeleteIDs {
		old := existingByID[deleteID]
		if err := s.cloudRepo.DeleteRecordDataFile(ctx, deleteID); err != nil {
			return err
		}
		if err := s.cloudObjects.Delete(ctx, old.S3Key); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) applyFiguresToLocal(
	ctx context.Context,
	recordID string,
	desired []repository.RecordFigure,
	existing []repository.RecordFigure,
) error {
	if err := s.downloadDesiredFigureFiles(ctx, recordID, desired); err != nil {
		return err
	}

	plan, err := PlanFigureReconciliation(recordID, existing, desired)
	if err != nil {
		return err
	}

	existingByID := indexFiguresByID(existing)

	for _, create := range plan.Creates {
		if _, err := s.localRepo.CreateRecordFigure(ctx, create); err != nil {
			return err
		}
	}

	for _, update := range plan.Updates {
		if _, err := s.localRepo.UpdateRecordFigure(ctx, update); err != nil {
			return err
		}
	}

	for _, deleteID := range plan.DeleteIDs {
		old := existingByID[deleteID]
		if err := s.localRepo.DeleteRecordFigure(ctx, deleteID); err != nil {
			return err
		}
		if err := s.removeLocalFigureFileIfPresent(recordID, old.Filename); err != nil {
			return err
		}
	}

	return nil
}

// uploadDesiredFigureFiles refreshes cloud figure binaries before metadata reconciliation.
func (s *Service) uploadDesiredFigureFiles(
	ctx context.Context,
	recordID string,
	desired []repository.RecordFigure,
) error {
	// File content may change without metadata changes, so metadata reconciliation
	// alone is not enough to keep S3 current.
	for _, figure := range desired {
		path, err := s.localFS.ResolveFigurePath(recordID, figure.Filename)
		if err != nil {
			return err
		}
		if err := s.uploadFile(ctx, figure.S3Key, path); err != nil {
			return err
		}
	}
	return nil
}

// downloadDesiredFigureFiles refreshes local figure binaries before metadata reconciliation.
func (s *Service) downloadDesiredFigureFiles(
	ctx context.Context,
	recordID string,
	desired []repository.RecordFigure,
) error {
	// Even if metadata is unchanged, the local binary may be stale when cloud wins.
	for _, figure := range desired {
		path, err := s.localFS.ResolveFigurePath(recordID, figure.Filename)
		if err != nil {
			return err
		}
		if err := s.downloadFile(ctx, figure.S3Key, path); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyDataFilesToLocal(
	ctx context.Context,
	recordID string,
	desired []repository.RecordDataFile,
	existing []repository.RecordDataFile,
) error {
	plan, err := PlanDataFileReconciliation(recordID, existing, desired)
	if err != nil {
		return err
	}

	existingByID := indexDataFilesByID(existing)

	for _, create := range plan.Creates {
		if _, err := s.localRepo.CreateRecordDataFile(ctx, create); err != nil {
			return err
		}
		if err := s.removeLocalDataFileIfPresent(recordID, create.Filename); err != nil {
			return err
		}
	}

	for _, update := range plan.Updates {
		if _, err := s.localRepo.UpdateRecordDataFile(ctx, update); err != nil {
			return err
		}
		if err := s.removeLocalDataFileIfPresent(recordID, update.Filename); err != nil {
			return err
		}
	}

	for _, deleteID := range plan.DeleteIDs {
		old := existingByID[deleteID]
		if err := s.localRepo.DeleteRecordDataFile(ctx, deleteID); err != nil {
			return err
		}
		if err := s.removeLocalDataFileIfPresent(recordID, old.Filename); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) uploadFile(ctx context.Context, key string, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open local file %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	if err := s.cloudObjects.Upload(ctx, key, file); err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

func (s *Service) uploadDataFileIfPresent(ctx context.Context, key string, path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat local file %s: %w", path, err)
	}
	if err := s.uploadFile(ctx, key, path); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) downloadFile(ctx context.Context, key string, path string) error {
	body, err := s.cloudObjects.Download(ctx, key)
	if err != nil {
		return fmt.Errorf("download %s: %w", key, err)
	}
	defer func() { _ = body.Close() }()

	if err := writeReaderToPath(path, body); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeReaderToPath(path string, body io.Reader) error {
	return writeReaderToPathWithPerm(path, body, syncedFilePermission)
}

func writeReaderToPathWithPerm(path string, body io.Reader, filePerm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	tempFile, err := os.CreateTemp(dir, ".sync-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := osFileChmod(tempFile, filePerm); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod temp file %s: %w", tempPath, err)
	}
	if _, err := io.Copy(tempFile, body); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("copy temp file %s: %w", tempPath, err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync temp file %s: %w", tempPath, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	cleanupTemp = false
	return nil
}

func removeFileIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func indexFiguresByID(figures []repository.RecordFigure) map[int64]repository.RecordFigure {
	byID := make(map[int64]repository.RecordFigure, len(figures))
	for _, figure := range figures {
		byID[figure.ID] = figure
	}
	return byID
}

func indexDataFilesByID(dataFiles []repository.RecordDataFile) map[int64]repository.RecordDataFile {
	byID := make(map[int64]repository.RecordDataFile, len(dataFiles))
	for _, dataFile := range dataFiles {
		byID[dataFile.ID] = dataFile
	}
	return byID
}

func (s *Service) removeLocalFigureFileIfPresent(recordID string, filename string) error {
	path, err := s.localFS.ResolveFigurePath(recordID, filename)
	if err != nil {
		return err
	}
	return removeFileIfPresent(path)
}

func (s *Service) removeLocalDataFileIfPresent(recordID string, filename string) error {
	path, err := s.localFS.ResolveDataFilePath(recordID, filename)
	if err != nil {
		return err
	}
	return removeFileIfPresent(path)
}

func figuresOf(bundle *RecordBundle) []repository.RecordFigure {
	if bundle == nil {
		return nil
	}
	return bundle.Figures
}

func dataFilesOf(bundle *RecordBundle) []repository.RecordDataFile {
	if bundle == nil {
		return nil
	}
	return bundle.DataFiles
}
