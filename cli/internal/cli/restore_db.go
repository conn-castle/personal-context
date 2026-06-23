package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/conn-castle/personal-context/cli/internal/gitsnapshot"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newRestoreDBCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore-db <path>",
		Short: "Replace the local SQLite store with a git snapshot backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRestoreDB(cmd.Context(), stdout, stderr, args[0])
		},
	}
	return cmd
}

func runRestoreDB(ctx context.Context, stdout io.Writer, _ io.Writer, path string) error {
	snapshot, err := gitsnapshot.Read(path)
	if err != nil {
		return fmt.Errorf("read restore snapshot: %w", err)
	}

	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}
	if err := ensureLocalEnvironment(ctx, homeDir); err != nil {
		return err
	}

	backupPath := createRestoreBackupPath(homeDir)
	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	currentSnapshot, err := buildLocalSnapshot(ctx, stack, repository.ListRecordsFilter{})
	if closeErr := stack.Close(); closeErr != nil {
		return closeErr
	}
	if err != nil {
		return err
	}
	if err := gitsnapshot.Write(backupPath, currentSnapshot); err != nil {
		return fmt.Errorf("write restore backup: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "Backup created at %s\n", backupPath)

	originalEntries, err := listExistingRestorePayloadEntries(basePath(homeDir))
	if err != nil {
		return err
	}
	stagingDir, err := createRestoreStagingDir(homeDir)
	if err != nil {
		return err
	}
	cleanupUnmarkedStaging := true
	defer func() {
		if cleanupUnmarkedStaging {
			_ = cleanupRestoreStaging(newRestoreMarker(restorePhaseStaging, stagingDir, backupPath))
		}
	}()
	stats, err := buildRestoreStagedStore(ctx, homeDir, stagingDir, snapshot)
	if err != nil {
		return err
	}
	stagedEntries, err := listExistingRestorePayloadEntries(stagingDir)
	if err != nil {
		return err
	}

	marker := newRestoreMarker(restorePhaseStaging, stagingDir, backupPath)
	marker.StagedEntries = stagedEntries
	marker.OriginalEntries = originalEntries
	if err := writeRestoreMarker(homeDir, marker); err != nil {
		return err
	}
	cleanupUnmarkedStaging = false
	marker.Phase = restorePhaseCommitting
	marker.Timestamp = newRestoreMarker(restorePhaseCommitting, stagingDir, backupPath).Timestamp
	if err := writeRestoreMarker(homeDir, marker); err != nil {
		return err
	}
	if err := replaceRestoreContentsFn(basePath(homeDir), stagingDir, gitsnapshot.ReplacementOptions{
		Entries:   restorePayloadEntries,
		BackupDir: restorePayloadBackupDir(backupPath),
	}); err != nil {
		if cleanupErr := abortFailedRestorePromotion(homeDir, marker); cleanupErr != nil {
			return fmt.Errorf("promote restored store: %w; abort cleanup failed: %v", err, cleanupErr)
		}
		return fmt.Errorf("promote restored store: %w", err)
	}
	marker.Phase = restorePhaseDone
	marker.Timestamp = newRestoreMarker(restorePhaseDone, stagingDir, backupPath).Timestamp
	if err := writeRestoreMarker(homeDir, marker); err != nil {
		return err
	}
	if err := cleanupCompletedRestore(marker); err != nil {
		return err
	}
	if err := removeRestoreMarker(homeDir); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Restore complete: created %d, updated %d, skipped %d\n", stats.Created, stats.Updated, stats.Skipped)
	return nil
}

var replaceRestoreContentsFn = gitsnapshot.ReplaceContents

func abortFailedRestorePromotion(homeDir string, marker restoreMarker) error {
	if err := gitsnapshot.RestoreReplacementBackup(basePath(homeDir), restorePayloadBackupDir(marker.BackupDir), restorePayloadEntries, marker.OriginalEntries); err != nil {
		return fmt.Errorf("roll back failed restore promotion: %w", err)
	}
	marker.Phase = restorePhaseDone
	marker.Timestamp = newRestoreMarker(restorePhaseDone, marker.StagingDir, marker.BackupDir).Timestamp
	if err := writeRestoreMarker(homeDir, marker); err != nil {
		return err
	}
	if err := cleanupCompletedRestore(marker); err != nil {
		return err
	}
	return removeRestoreMarker(homeDir)
}

func buildRestoreStagedStore(ctx context.Context, homeDir string, stagingDir string, snapshot gitsnapshot.Snapshot) (importStats, error) {
	stagingDBPath := filepath.Join(stagingDir, ".pc", "pc.db")
	if err := ensureLocalEnvironmentAt(ctx, homeDir, stagingDir, stagingDBPath, false); err != nil {
		return importStats{}, err
	}
	stack, err := openLocalStackAt(homeDir, stagingDir, stagingDBPath)
	if err != nil {
		return importStats{}, err
	}
	stats, importErr := importSnapshotIntoStack(ctx, stack, snapshot)
	closeErr := stack.Close()
	if importErr != nil {
		return stats, importErr
	}
	if closeErr != nil {
		return stats, closeErr
	}
	if err := syncRestorePayload(stagingDir); err != nil {
		return stats, err
	}
	return stats, nil
}
