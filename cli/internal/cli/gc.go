package cli

import (
	"io"

	"github.com/spf13/cobra"
)

func newGCCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Hard-delete record and chat trash older than 30 days",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGCAll(cmd.Context(), stdout, stderr, allTrashDomains())
		},
	}
	return cmd
}
