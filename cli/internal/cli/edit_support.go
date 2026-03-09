package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/slideio"
)

type editExistingAssets struct {
	Figures            []repository.SlideFigure
	DataFiles          []repository.SlideDataFile
	FigureByFilename   map[string]repository.SlideFigure
	DataFileByFilename map[string]repository.SlideDataFile
}

type stagedEditInput struct {
	Figures   []repository.CreateSlideFigureInput
	DataFiles []repository.CreateSlideDataFileInput
}

type editMutationState struct {
	slideUpdated       bool
	createdFigureIDs   []int64
	updatedFigures     []repository.SlideFigure
	deletedFigures     []repository.SlideFigure
	createdDataFileIDs []int64
	updatedDataFiles   []repository.SlideDataFile
	deletedDataFiles   []repository.SlideDataFile
	stagedFiles        []stagedReplacementFile
	committedFiles     []committedReplacementFile
}

type stagedReplacementFile struct {
	TempPath  string
	FinalPath string
}

type committedReplacementFile struct {
	FinalPath  string
	BackupPath string
}

// loadExistingEditAssets gathers the current figure and data-file rows for a
// slide, plus filename indexes used during reconciliation.
func loadExistingEditAssets(ctx context.Context, repo repository.Repository, id string) (editExistingAssets, error) {
	figures, err := repo.ListSlideFiguresBySlideID(ctx, id)
	if err != nil {
		return editExistingAssets{}, fmt.Errorf("list old figures: %w", err)
	}
	dataFiles, err := repo.ListSlideDataFilesBySlideID(ctx, id)
	if err != nil {
		return editExistingAssets{}, fmt.Errorf("list old data files: %w", err)
	}

	existingAssets := editExistingAssets{
		Figures:            figures,
		DataFiles:          dataFiles,
		FigureByFilename:   make(map[string]repository.SlideFigure, len(figures)),
		DataFileByFilename: make(map[string]repository.SlideDataFile, len(dataFiles)),
	}
	for _, figure := range figures {
		existingAssets.FigureByFilename[figure.Filename] = figure
	}
	for _, dataFile := range dataFiles {
		existingAssets.DataFileByFilename[dataFile.Filename] = dataFile
	}
	return existingAssets, nil
}

// stageEditInputFiles copies incoming assets into temporary files and builds
// the repository payloads that point at their canonical destinations.
func stageEditInputFiles(id string, input slideio.SlideInput, stack *localStack, mutations *editMutationState) (stagedEditInput, error) {
	staged := stagedEditInput{
		Figures:   make([]repository.CreateSlideFigureInput, 0, len(input.Figures)),
		DataFiles: make([]repository.CreateSlideDataFileInput, 0, len(input.DataFiles)),
	}

	for _, figurePath := range input.Figures {
		filename := filepath.Base(figurePath)
		finalPath, err := stack.FS.ResolveFigurePath(id, filename)
		if err != nil {
			return stagedEditInput{}, fmt.Errorf("resolve figure path %s: %w", filename, err)
		}
		tempPath, _, err := stageReplacementFile(finalPath, figurePath)
		if err != nil {
			return stagedEditInput{}, fmt.Errorf("stage figure %s: %w", filename, err)
		}
		mutations.stagedFiles = append(mutations.stagedFiles, stagedReplacementFile{
			TempPath:  tempPath,
			FinalPath: finalPath,
		})
		staged.Figures = append(staged.Figures, repository.CreateSlideFigureInput{
			SlideID:  id,
			Filename: filename,
			S3Key:    filepath.ToSlash(filepath.Join("figures", id, filename)),
		})
	}

	for _, dataPath := range input.DataFiles {
		filename := filepath.Base(dataPath)
		finalPath, err := stack.FS.ResolveDataFilePath(id, filename)
		if err != nil {
			return stagedEditInput{}, fmt.Errorf("resolve data file path %s: %w", filename, err)
		}
		tempPath, size, err := stageReplacementFile(finalPath, dataPath)
		if err != nil {
			return stagedEditInput{}, fmt.Errorf("stage data file %s: %w", filename, err)
		}
		mutations.stagedFiles = append(mutations.stagedFiles, stagedReplacementFile{
			TempPath:  tempPath,
			FinalPath: finalPath,
		})
		hash, err := slideio.HashFile(dataPath)
		if err != nil {
			return stagedEditInput{}, fmt.Errorf("hash data file %s: %w", filename, err)
		}
		staged.DataFiles = append(staged.DataFiles, repository.CreateSlideDataFileInput{
			SlideID:  id,
			Filename: filename,
			S3Key:    filepath.ToSlash(filepath.Join("data", id, filename)),
			Size:     size,
			Hash:     hash,
		})
	}

	return staged, nil
}

// upsertEditFigureRecords creates or updates figure rows to match the staged
// input and returns the set of filenames that should remain after the edit.
func upsertEditFigureRecords(
	ctx context.Context,
	repo repository.Repository,
	figures []repository.CreateSlideFigureInput,
	existingByFilename map[string]repository.SlideFigure,
	mutations *editMutationState,
) (map[string]struct{}, error) {
	newFigureNames := make(map[string]struct{}, len(figures))
	for _, figure := range figures {
		newFigureNames[figure.Filename] = struct{}{}
		if oldFigure, exists := existingByFilename[figure.Filename]; exists {
			if _, err := repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{
				ID:       oldFigure.ID,
				Filename: figure.Filename,
				S3Key:    figure.S3Key,
			}); err != nil {
				return nil, fmt.Errorf("update figure record %s: %w", figure.Filename, err)
			}
			mutations.updatedFigures = append(mutations.updatedFigures, oldFigure)
			continue
		}

		createdFigure, err := repo.CreateSlideFigure(ctx, figure)
		if err != nil {
			return nil, fmt.Errorf("create figure record %s: %w", figure.Filename, err)
		}
		mutations.createdFigureIDs = append(mutations.createdFigureIDs, createdFigure.ID)
	}
	return newFigureNames, nil
}

// upsertEditDataFileRecords creates or updates data-file rows to match the
// staged input and returns the set of filenames that should remain after the
// edit.
func upsertEditDataFileRecords(
	ctx context.Context,
	repo repository.Repository,
	dataFiles []repository.CreateSlideDataFileInput,
	existingByFilename map[string]repository.SlideDataFile,
	mutations *editMutationState,
) (map[string]struct{}, error) {
	newDataFileNames := make(map[string]struct{}, len(dataFiles))
	for _, dataFile := range dataFiles {
		newDataFileNames[dataFile.Filename] = struct{}{}
		if oldDataFile, exists := existingByFilename[dataFile.Filename]; exists {
			size := dataFile.Size
			hash := dataFile.Hash
			if _, err := repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{
				ID:       oldDataFile.ID,
				Filename: dataFile.Filename,
				S3Key:    dataFile.S3Key,
				Size:     &size,
				Hash:     &hash,
			}); err != nil {
				return nil, fmt.Errorf("update data file record %s: %w", dataFile.Filename, err)
			}
			mutations.updatedDataFiles = append(mutations.updatedDataFiles, oldDataFile)
			continue
		}

		createdDataFile, err := repo.CreateSlideDataFile(ctx, dataFile)
		if err != nil {
			return nil, fmt.Errorf("create data file record %s: %w", dataFile.Filename, err)
		}
		mutations.createdDataFileIDs = append(mutations.createdDataFileIDs, createdDataFile.ID)
	}
	return newDataFileNames, nil
}

// deleteRemovedEditFigures deletes figure rows omitted from the new input and
// returns the local files that can be best-effort removed afterwards.
func deleteRemovedEditFigures(
	ctx context.Context,
	repo repository.Repository,
	figures []repository.SlideFigure,
	newFigureNames map[string]struct{},
	mutations *editMutationState,
) ([]string, error) {
	figureFilenamesToDelete := make([]string, 0)
	for _, oldFigure := range figures {
		if _, stillPresent := newFigureNames[oldFigure.Filename]; stillPresent {
			continue
		}
		if err := repo.DeleteSlideFigure(ctx, oldFigure.ID); err != nil {
			return nil, fmt.Errorf("delete old figure record %s: %w", oldFigure.Filename, err)
		}
		mutations.deletedFigures = append(mutations.deletedFigures, oldFigure)
		figureFilenamesToDelete = append(figureFilenamesToDelete, oldFigure.Filename)
	}
	return figureFilenamesToDelete, nil
}

// deleteRemovedEditDataFiles deletes data-file rows omitted from the new input
// and returns the local files that can be best-effort removed afterwards.
func deleteRemovedEditDataFiles(
	ctx context.Context,
	repo repository.Repository,
	dataFiles []repository.SlideDataFile,
	newDataFileNames map[string]struct{},
	mutations *editMutationState,
) ([]string, error) {
	dataFilenamesToDelete := make([]string, 0)
	for _, oldDataFile := range dataFiles {
		if _, stillPresent := newDataFileNames[oldDataFile.Filename]; stillPresent {
			continue
		}
		if err := repo.DeleteSlideDataFile(ctx, oldDataFile.ID); err != nil {
			return nil, fmt.Errorf("delete old data file record %s: %w", oldDataFile.Filename, err)
		}
		mutations.deletedDataFiles = append(mutations.deletedDataFiles, oldDataFile)
		dataFilenamesToDelete = append(dataFilenamesToDelete, oldDataFile.Filename)
	}
	return dataFilenamesToDelete, nil
}

// commitStagedFiles moves each staged temp file into its canonical location and
// records the backup path needed for rollback if a later step fails.
func (s *editMutationState) commitStagedFiles() error {
	for _, staged := range s.stagedFiles {
		backupPath, err := backupExistingFileForEdit(staged.FinalPath)
		if err != nil {
			return fmt.Errorf("backup existing file %s: %w", filepath.Base(staged.FinalPath), err)
		}
		if err := os.Rename(staged.TempPath, staged.FinalPath); err != nil {
			if backupPath != "" {
				_ = os.Rename(backupPath, staged.FinalPath)
			}
			return fmt.Errorf("commit staged file %s: %w", filepath.Base(staged.FinalPath), err)
		}
		s.committedFiles = append(s.committedFiles, committedReplacementFile{
			FinalPath:  staged.FinalPath,
			BackupPath: backupPath,
		})
	}
	return nil
}

// cleanupStagedFiles removes any temp files that were never committed.
func (s *editMutationState) cleanupStagedFiles() {
	for _, staged := range s.stagedFiles {
		_ = os.Remove(staged.TempPath)
	}
}

// finalizeCommittedFiles removes backup files on success or restores them on
// failure after any partially committed replacements.
func (s *editMutationState) finalizeCommittedFiles(success bool) {
	if success {
		for _, committed := range s.committedFiles {
			if committed.BackupPath == "" {
				continue
			}
			_ = os.Remove(committed.BackupPath)
		}
		return
	}

	for idx := len(s.committedFiles) - 1; idx >= 0; idx-- {
		committed := s.committedFiles[idx]
		_ = os.Remove(committed.FinalPath)
		if committed.BackupPath != "" {
			_ = os.Rename(committed.BackupPath, committed.FinalPath)
		}
	}
}

// rollbackRepository restores slide and child-row state after a failed edit.
func (s *editMutationState) rollbackRepository(ctx context.Context, repo repository.Repository, existing repository.Slide) {
	if s.hasChildRowMutations() {
		for _, createdID := range s.createdFigureIDs {
			_ = repo.DeleteSlideFigure(ctx, createdID)
		}
		for _, oldFigure := range s.updatedFigures {
			_, _ = repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{
				ID:       oldFigure.ID,
				Filename: oldFigure.Filename,
				S3Key:    oldFigure.S3Key,
				AltText:  oldFigure.AltText,
			})
		}
		for _, oldFigure := range s.deletedFigures {
			_, _ = repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
				SlideID:  oldFigure.SlideID,
				Filename: oldFigure.Filename,
				S3Key:    oldFigure.S3Key,
				AltText:  oldFigure.AltText,
			})
		}
		for _, createdID := range s.createdDataFileIDs {
			_ = repo.DeleteSlideDataFile(ctx, createdID)
		}
		for _, oldDataFile := range s.updatedDataFiles {
			size := oldDataFile.Size
			hash := oldDataFile.Hash
			_, _ = repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{
				ID:          oldDataFile.ID,
				Filename:    oldDataFile.Filename,
				S3Key:       oldDataFile.S3Key,
				Size:        &size,
				Hash:        &hash,
				Description: oldDataFile.Description,
			})
		}
		for _, oldDataFile := range s.deletedDataFiles {
			_, _ = repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
				SlideID:     oldDataFile.SlideID,
				Filename:    oldDataFile.Filename,
				S3Key:       oldDataFile.S3Key,
				Size:        oldDataFile.Size,
				Hash:        oldDataFile.Hash,
				Description: oldDataFile.Description,
			})
		}
	}

	if !s.slideUpdated {
		return
	}
	_, _ = repo.UpdateSlide(ctx, repository.UpdateSlideInput{
		ID:           existing.ID,
		Date:         existing.Date,
		DayOrder:     existing.DayOrder,
		HTMLContent:  existing.HTMLContent,
		Notes:        existing.Notes,
		ProjectID:    existing.ProjectID,
		GitRemoteURL: existing.GitRemoteURL,
		GitHash:      existing.GitHash,
		DeletedAt:    existing.DeletedAt,
		UpdatedAt:    &existing.UpdatedAt,
	})
}

func (s *editMutationState) hasChildRowMutations() bool {
	return len(s.createdFigureIDs) > 0 ||
		len(s.updatedFigures) > 0 ||
		len(s.deletedFigures) > 0 ||
		len(s.createdDataFileIDs) > 0 ||
		len(s.updatedDataFiles) > 0 ||
		len(s.deletedDataFiles) > 0
}

// deleteEditFiles removes now-stale local assets after the database state is
// fully updated. File deletion is best effort so metadata is authoritative.
func deleteEditFiles(stack *localStack, id string, figureFilenames []string, dataFilenames []string) {
	for _, filename := range figureFilenames {
		_ = stack.FS.DeleteFigure(id, filename)
	}
	for _, filename := range dataFilenames {
		_ = stack.FS.DeleteDataFile(id, filename)
	}
}

// stageReplacementFile copies a source asset into the destination directory as
// a temp file so edits can commit atomically after metadata reconciliation.
func stageReplacementFile(finalPath string, sourcePath string) (string, int64, error) {
	targetDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return "", 0, fmt.Errorf("create destination directory: %w", err)
	}

	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return "", 0, fmt.Errorf("open source file %s: %w", sourcePath, err)
	}
	defer func() { _ = sourceFile.Close() }()

	tempFile, err := os.CreateTemp(targetDir, ".edit-stage-*")
	if err != nil {
		return "", 0, fmt.Errorf("create staging file for %s: %w", filepath.Base(finalPath), err)
	}

	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	writtenBytes, err := io.Copy(tempFile, sourceFile)
	if err != nil {
		_ = tempFile.Close()
		return "", 0, fmt.Errorf("copy source file %s to staging: %w", sourcePath, err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return "", 0, fmt.Errorf("sync staged file for %s: %w", filepath.Base(finalPath), err)
	}
	if err := tempFile.Close(); err != nil {
		return "", 0, fmt.Errorf("close staged file for %s: %w", filepath.Base(finalPath), err)
	}

	cleanupTemp = false
	return tempPath, writtenBytes, nil
}

// backupExistingFileForEdit moves any existing destination file aside so a
// later failure can restore the pre-edit contents.
func backupExistingFileForEdit(finalPath string) (string, error) {
	info, err := os.Stat(finalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("stat destination: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("destination path %s is a directory", finalPath)
	}

	backupFile, err := os.CreateTemp(filepath.Dir(finalPath), ".edit-backup-*")
	if err != nil {
		return "", fmt.Errorf("create backup file: %w", err)
	}
	backupPath := backupFile.Name()
	if err := backupFile.Close(); err != nil {
		_ = os.Remove(backupPath)
		return "", fmt.Errorf("close backup file: %w", err)
	}
	if err := os.Remove(backupPath); err != nil {
		return "", fmt.Errorf("prepare backup path: %w", err)
	}
	if err := os.Rename(finalPath, backupPath); err != nil {
		return "", fmt.Errorf("move existing file to backup: %w", err)
	}
	return backupPath, nil
}
