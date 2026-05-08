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
	"github.com/conn-castle/personal-context/cli/internal/syncengine"
)

const syncedFilePermission = 0o644

// osFileChmod wraps *os.File.Chmod so tests can inject I/O failures without
// modifying the filesystem. Production code never reassigns this variable.
var osFileChmod = func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }

// LocalFiles resolves canonical local figure/data paths for sync operations.
type LocalFiles interface {
	ResolveFigurePath(recordID string, filename string) (string, error)
	ResolveDataFilePath(recordID string, filename string) (string, error)
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
		}, nil
	}
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
	if err := s.pullChangedRecords(ctx, window.LastSync); err != nil {
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
		Record:     record,
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

	if err := osFileChmod(tempFile, syncedFilePermission); err != nil {
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
