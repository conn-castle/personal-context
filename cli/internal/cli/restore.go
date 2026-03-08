package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newRestoreCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <id>",
		Short: "Restore a soft-deleted slide",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRestore(cmd.Context(), stdout, stderr, args[0])
		},
	}
	return cmd
}

func runRestore(ctx context.Context, stdout io.Writer, stderr io.Writer, id string) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	if err := stack.Repo.RestoreSlide(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("slide %q not found", id)
		}
		return fmt.Errorf("restore slide: %w", err)
	}

	_ = runAutoSyncFn(ctx, stderr)
	_, _ = fmt.Fprintf(stdout, "Slide %s restored\n", id)
	return nil
}
