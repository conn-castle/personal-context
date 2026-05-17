package cli

import (
	"io"

	"github.com/spf13/cobra"
)

func newTrashCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trash",
		Short: "List soft-deleted records and chats",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTrashAll(cmd.Context(), stdout, stderr, allTrashDomains())
		},
	}
	return cmd
}
