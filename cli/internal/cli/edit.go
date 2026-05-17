package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/conn-castle/personal-context/cli/internal/recordio"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newEditCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <id> <path>",
		Short: "Replace record content from an input folder",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdit(cmd.Context(), stdout, stderr, args[0], args[1])
		},
	}
	return cmd
}

// runEdit replaces a record and its local assets from an input folder, rolling
// back database and filesystem mutations if any stage fails.
func runEdit(ctx context.Context, stdout io.Writer, stderr io.Writer, id string, inputPath string) (err error) {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	// Fail fast if record doesn't exist
	existing, err := stack.Repo.GetRecordByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("record %q not found", id)
		}
		return fmt.Errorf("get record: %w", err)
	}

	mutations := &editMutationState{}
	defer func() {
		if err == nil {
			return
		}
		mutations.rollbackRepository(context.Background(), stack.Repo, existing)
	}()

	input, err := recordio.ParseInputFolder(inputPath)
	if err != nil {
		return err
	}
	projectID, deviceID, sourceRef, err := resolveRecordProvenance(input.ProjectID, input.SourceDeviceID, input.SourceRef, "", "", "")
	if err != nil {
		return err
	}
	if err := validateActiveProjectAndDevice(ctx, stack.Repo, projectID, deviceID); err != nil {
		return err
	}

	existingAssets, err := loadExistingEditAssets(ctx, stack.Repo, id)
	if err != nil {
		return err
	}

	defer func() {
		if err == nil {
			return
		}
		mutations.cleanupStagedFiles()
	}()
	defer func() {
		mutations.finalizeCommittedFiles(err == nil)
	}()

	// Update record (full replacement, preserve immutable fields)
	if _, err := stack.Repo.UpdateRecord(ctx, repository.UpdateRecordInput{
		ID:             id,
		Date:           existing.Date,
		DayOrder:       existing.DayOrder,
		HTMLContent:    input.HTMLContent,
		Notes:          input.Notes,
		ProjectID:      projectID,
		SourceDeviceID: deviceID,
		SourceRef:      sourceRef,
		GitRemoteURL:   input.GitRemoteURL,
		GitHash:        input.GitHash,
		DeletedAt:      existing.DeletedAt,
	}); err != nil {
		return fmt.Errorf("update record: %w", err)
	}
	mutations.recordUpdated = true

	stagedInput, err := stageEditInputFiles(id, input, stack, mutations)
	if err != nil {
		return err
	}

	newFigureNames, err := upsertEditFigureRecords(ctx, stack.Repo, stagedInput.Figures, existingAssets.FigureByFilename, mutations)
	if err != nil {
		return err
	}
	newDataFileNames, err := upsertEditDataFileRecords(ctx, stack.Repo, stagedInput.DataFiles, existingAssets.DataFileByFilename, mutations)
	if err != nil {
		return err
	}
	if err := mutations.commitStagedFiles(); err != nil {
		return err
	}

	figureFilenamesToDelete, err := deleteRemovedEditFigures(ctx, stack.Repo, existingAssets.Figures, newFigureNames, mutations)
	if err != nil {
		return err
	}
	dataFilenamesToDelete, err := deleteRemovedEditDataFiles(ctx, stack.Repo, existingAssets.DataFiles, newDataFileNames, mutations)
	if err != nil {
		return err
	}
	deleteEditFiles(stack, id, figureFilenamesToDelete, dataFilenamesToDelete)

	_ = runAutoSyncFn(ctx, stderr)
	_, _ = fmt.Fprintf(stdout, "Record %s updated\n", id)
	return nil
}
