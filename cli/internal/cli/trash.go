package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newTrashCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trash",
		Short: "List soft-deleted records",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTrash(cmd.Context(), stdout, stderr)
		},
	}
	return cmd
}

func runTrash(ctx context.Context, stdout io.Writer, _ io.Writer) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	records, err := stack.Repo.ListRecords(ctx, repository.ListRecordsFilter{OnlyDeleted: true})
	if err != nil {
		return fmt.Errorf("list deleted records: %w", err)
	}

	if len(records) == 0 {
		_, _ = fmt.Fprintln(stdout, "Trash is empty.")
		return nil
	}

	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tDate\tDeleted At")
	for _, s := range records {
		deletedAt := ""
		if s.DeletedAt != nil {
			deletedAt = s.DeletedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", s.ID, s.Date, deletedAt)
	}
	return w.Flush()
}
