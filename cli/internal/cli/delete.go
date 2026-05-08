package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newDeleteCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Soft-delete a record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), stdout, stderr, args[0])
		},
	}
	return cmd
}

func runDelete(ctx context.Context, stdout io.Writer, stderr io.Writer, id string) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	if err := stack.Repo.SoftDeleteRecord(ctx, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("record %q not found", id)
		}
		return fmt.Errorf("delete record: %w", err)
	}

	_ = runAutoSyncFn(ctx, stderr)
	_, _ = fmt.Fprintf(stdout, "Record %s deleted\n", id)
	return nil
}
