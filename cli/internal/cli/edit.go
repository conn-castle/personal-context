package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/slideio"
	"github.com/spf13/cobra"
)

func newEditCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <id> <path>",
		Short: "Replace slide content from an input folder",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdit(cmd.Context(), stdout, stderr, args[0], args[1])
		},
	}
	return cmd
}

func runEdit(ctx context.Context, stdout io.Writer, _ io.Writer, id string, inputPath string) (err error) {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	// Fail fast if slide doesn't exist
	existing, err := stack.Repo.GetSlideByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("slide %q not found", id)
		}
		return fmt.Errorf("get slide: %w", err)
	}

	slideUpdated := false
	childRowsMutated := false
	var createdFigureIDs []int64
	var updatedFigures []repository.SlideFigure
	var deletedFigures []repository.SlideFigure
	var createdDataFileIDs []int64
	var updatedDataFiles []repository.SlideDataFile
	var deletedDataFiles []repository.SlideDataFile
	defer func() {
		if err == nil {
			return
		}

		rollbackCtx := context.Background()
		if childRowsMutated {
			for _, createdID := range createdFigureIDs {
				_ = stack.Repo.DeleteSlideFigure(rollbackCtx, createdID)
			}
			for _, oldFigure := range updatedFigures {
				_, _ = stack.Repo.UpdateSlideFigure(rollbackCtx, repository.UpdateSlideFigureInput{
					ID:       oldFigure.ID,
					Filename: oldFigure.Filename,
					S3Key:    oldFigure.S3Key,
					AltText:  oldFigure.AltText,
				})
			}
			for _, oldFigure := range deletedFigures {
				_, _ = stack.Repo.CreateSlideFigure(rollbackCtx, repository.CreateSlideFigureInput{
					SlideID:  oldFigure.SlideID,
					Filename: oldFigure.Filename,
					S3Key:    oldFigure.S3Key,
					AltText:  oldFigure.AltText,
				})
			}
			for _, createdID := range createdDataFileIDs {
				_ = stack.Repo.DeleteSlideDataFile(rollbackCtx, createdID)
			}
			for _, oldDataFile := range updatedDataFiles {
				size := oldDataFile.Size
				hash := oldDataFile.Hash
				_, _ = stack.Repo.UpdateSlideDataFile(rollbackCtx, repository.UpdateSlideDataFileInput{
					ID:          oldDataFile.ID,
					Filename:    oldDataFile.Filename,
					S3Key:       oldDataFile.S3Key,
					Size:        &size,
					Hash:        &hash,
					Description: oldDataFile.Description,
				})
			}
			for _, oldDataFile := range deletedDataFiles {
				_, _ = stack.Repo.CreateSlideDataFile(rollbackCtx, repository.CreateSlideDataFileInput{
					SlideID:     oldDataFile.SlideID,
					Filename:    oldDataFile.Filename,
					S3Key:       oldDataFile.S3Key,
					Size:        oldDataFile.Size,
					Hash:        oldDataFile.Hash,
					Description: oldDataFile.Description,
				})
			}
		}

		if !slideUpdated {
			return
		}
		_, _ = stack.Repo.UpdateSlide(rollbackCtx, repository.UpdateSlideInput{
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
	}()

	input, err := slideio.ParseInputFolder(inputPath)
	if err != nil {
		return err
	}

	oldFigures, err := stack.Repo.ListSlideFiguresBySlideID(ctx, id)
	if err != nil {
		return fmt.Errorf("list old figures: %w", err)
	}
	oldDataFiles, err := stack.Repo.ListSlideDataFilesBySlideID(ctx, id)
	if err != nil {
		return fmt.Errorf("list old data files: %w", err)
	}

	oldFigureByFilename := make(map[string]repository.SlideFigure, len(oldFigures))
	for _, figure := range oldFigures {
		oldFigureByFilename[figure.Filename] = figure
	}
	oldDataFileByFilename := make(map[string]repository.SlideDataFile, len(oldDataFiles))
	for _, dataFile := range oldDataFiles {
		oldDataFileByFilename[dataFile.Filename] = dataFile
	}

	stagedFiles := make([]stagedReplacementFile, 0, len(input.Figures)+len(input.DataFiles))
	committedFiles := make([]committedReplacementFile, 0, len(input.Figures)+len(input.DataFiles))
	defer func() {
		if err == nil {
			return
		}
		for _, staged := range stagedFiles {
			_ = os.Remove(staged.TempPath)
		}
	}()
	defer func() {
		if err == nil {
			for _, committed := range committedFiles {
				if committed.BackupPath == "" {
					continue
				}
				_ = os.Remove(committed.BackupPath)
			}
			return
		}
		for idx := len(committedFiles) - 1; idx >= 0; idx-- {
			committed := committedFiles[idx]
			_ = os.Remove(committed.FinalPath)
			if committed.BackupPath != "" {
				_ = os.Rename(committed.BackupPath, committed.FinalPath)
			}
		}
	}()

	// Update slide (full replacement, preserve immutable fields)
	if _, err := stack.Repo.UpdateSlide(ctx, repository.UpdateSlideInput{
		ID:           id,
		Date:         existing.Date,
		DayOrder:     existing.DayOrder,
		HTMLContent:  input.HTMLContent,
		Notes:        input.Notes,
		ProjectID:    input.ProjectID,
		GitRemoteURL: input.GitRemoteURL,
		GitHash:      input.GitHash,
		DeletedAt:    existing.DeletedAt,
	}); err != nil {
		return fmt.Errorf("update slide: %w", err)
	}
	slideUpdated = true

	// Stage new figures without mutating canonical paths yet.
	var newFigures []repository.CreateSlideFigureInput
	for _, figurePath := range input.Figures {
		filename := filepath.Base(figurePath)
		finalPath, err := stack.FS.ResolveFigurePath(id, filename)
		if err != nil {
			return fmt.Errorf("resolve figure path %s: %w", filename, err)
		}
		tempPath, _, err := stageReplacementFile(finalPath, figurePath)
		if err != nil {
			return fmt.Errorf("stage figure %s: %w", filename, err)
		}
		stagedFiles = append(stagedFiles, stagedReplacementFile{
			TempPath:  tempPath,
			FinalPath: finalPath,
		})
		newFigures = append(newFigures, repository.CreateSlideFigureInput{
			SlideID:  id,
			Filename: filename,
			S3Key:    filepath.ToSlash(filepath.Join("figures", id, filename)),
		})
	}

	// Stage new data files without mutating canonical paths yet.
	var newDataFiles []repository.CreateSlideDataFileInput
	for _, dataPath := range input.DataFiles {
		filename := filepath.Base(dataPath)
		finalPath, err := stack.FS.ResolveDataFilePath(id, filename)
		if err != nil {
			return fmt.Errorf("resolve data file path %s: %w", filename, err)
		}
		tempPath, size, err := stageReplacementFile(finalPath, dataPath)
		if err != nil {
			return fmt.Errorf("stage data file %s: %w", filename, err)
		}
		stagedFiles = append(stagedFiles, stagedReplacementFile{
			TempPath:  tempPath,
			FinalPath: finalPath,
		})
		hash, err := slideio.HashFile(dataPath)
		if err != nil {
			return fmt.Errorf("hash data file %s: %w", filename, err)
		}
		newDataFiles = append(newDataFiles, repository.CreateSlideDataFileInput{
			SlideID:  id,
			Filename: filename,
			S3Key:    filepath.ToSlash(filepath.Join("data", id, filename)),
			Size:     size,
			Hash:     hash,
		})
	}

	newFigureNames := make(map[string]struct{}, len(newFigures))
	for _, figure := range newFigures {
		newFigureNames[figure.Filename] = struct{}{}
		if oldFigure, exists := oldFigureByFilename[figure.Filename]; exists {
			if _, err := stack.Repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{
				ID:       oldFigure.ID,
				Filename: figure.Filename,
				S3Key:    figure.S3Key,
			}); err != nil {
				return fmt.Errorf("update figure record %s: %w", figure.Filename, err)
			}
			childRowsMutated = true
			updatedFigures = append(updatedFigures, oldFigure)
			continue
		}
		createdFigure, err := stack.Repo.CreateSlideFigure(ctx, figure)
		if err != nil {
			return fmt.Errorf("create figure record %s: %w", figure.Filename, err)
		}
		childRowsMutated = true
		createdFigureIDs = append(createdFigureIDs, createdFigure.ID)
	}

	newDataFileNames := make(map[string]struct{}, len(newDataFiles))
	for _, dataFile := range newDataFiles {
		newDataFileNames[dataFile.Filename] = struct{}{}
		if oldDataFile, exists := oldDataFileByFilename[dataFile.Filename]; exists {
			size := dataFile.Size
			hash := dataFile.Hash
			if _, err := stack.Repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{
				ID:       oldDataFile.ID,
				Filename: dataFile.Filename,
				S3Key:    dataFile.S3Key,
				Size:     &size,
				Hash:     &hash,
			}); err != nil {
				return fmt.Errorf("update data file record %s: %w", dataFile.Filename, err)
			}
			childRowsMutated = true
			updatedDataFiles = append(updatedDataFiles, oldDataFile)
			continue
		}
		createdDataFile, err := stack.Repo.CreateSlideDataFile(ctx, dataFile)
		if err != nil {
			return fmt.Errorf("create data file record %s: %w", dataFile.Filename, err)
		}
		childRowsMutated = true
		createdDataFileIDs = append(createdDataFileIDs, createdDataFile.ID)
	}

	// Commit staged file replacements after DB reconciliation succeeds.
	// Existing files are backed up so partial commit failures can be rolled back.
	for _, staged := range stagedFiles {
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
		committedFiles = append(committedFiles, committedReplacementFile{
			FinalPath:  staged.FinalPath,
			BackupPath: backupPath,
		})
	}

	figureFilenamesToDelete := make([]string, 0)
	// Delete old figure DB rows and files that are no longer present.
	for _, oldFigure := range oldFigures {
		if _, stillPresent := newFigureNames[oldFigure.Filename]; stillPresent {
			continue
		}
		if err := stack.Repo.DeleteSlideFigure(ctx, oldFigure.ID); err != nil {
			return fmt.Errorf("delete old figure record %s: %w", oldFigure.Filename, err)
		}
		childRowsMutated = true
		deletedFigures = append(deletedFigures, oldFigure)
		figureFilenamesToDelete = append(figureFilenamesToDelete, oldFigure.Filename)
	}

	dataFilenamesToDelete := make([]string, 0)
	// Delete old data file DB rows and files that are no longer present.
	for _, oldDataFile := range oldDataFiles {
		if _, stillPresent := newDataFileNames[oldDataFile.Filename]; stillPresent {
			continue
		}
		if err := stack.Repo.DeleteSlideDataFile(ctx, oldDataFile.ID); err != nil {
			return fmt.Errorf("delete old data file record %s: %w", oldDataFile.Filename, err)
		}
		childRowsMutated = true
		deletedDataFiles = append(deletedDataFiles, oldDataFile)
		dataFilenamesToDelete = append(dataFilenamesToDelete, oldDataFile.Filename)
	}

	for _, filename := range figureFilenamesToDelete {
		_ = stack.FS.DeleteFigure(id, filename) // best-effort cleanup
	}
	for _, filename := range dataFilenamesToDelete {
		_ = stack.FS.DeleteDataFile(id, filename) // best-effort cleanup
	}

	_, _ = fmt.Fprintf(stdout, "Slide %s updated\n", id)
	return nil
}

type stagedReplacementFile struct {
	TempPath  string
	FinalPath string
}

type committedReplacementFile struct {
	FinalPath  string
	BackupPath string
}

func stageReplacementFile(finalPath string, sourcePath string) (string, int64, error) {
	targetDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
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
