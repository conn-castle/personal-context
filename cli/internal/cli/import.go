package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/conn-castle/personal-context/cli/internal/gitsnapshot"
	"github.com/spf13/cobra"
)

func newImportCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <path>",
		Short: "Merge a git snapshot into the local SQLite store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd.Context(), stdout, stderr, args[0])
		},
	}
	return cmd
}

func runImport(ctx context.Context, stdout io.Writer, _ io.Writer, path string) error {
	snapshot, err := gitsnapshot.Read(path)
	if err != nil {
		return fmt.Errorf("read import snapshot: %w", err)
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

	stats, err := importSnapshotIntoStack(ctx, stack, snapshot)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Import complete: created %d, updated %d, skipped %d\n", stats.Created, stats.Updated, stats.Skipped)
	return nil
}
