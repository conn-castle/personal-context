package cli

import (
	"context"
	"fmt"
	"io"

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

	if err := wipeLocalState(homeDir); err != nil {
		return err
	}
	if err := ensureLocalEnvironment(ctx, homeDir); err != nil {
		return err
	}
	stack, err = openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	stats, err := importSnapshotIntoStack(ctx, stack, snapshot)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Backup created at %s\n", backupPath)
	_, _ = fmt.Fprintf(stdout, "Restore complete: created %d, updated %d, skipped %d\n", stats.Created, stats.Updated, stats.Skipped)
	return nil
}
