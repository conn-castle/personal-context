package cli

import (
	"io"

	"github.com/spf13/cobra"
)

func newGCCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Hard-delete record and chat trash older than the configured retention window (default 30 days)",
		Long: "Hard-delete record and chat trash older than the configured retention window.\n\n" +
			"The window defaults to 30 days. To change it, set \"gc_retention_days\" (a positive\n" +
			"integer number of days) in ~/personal-context/.pc/config.json.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGCAll(cmd.Context(), stdout, stderr, allTrashDomains())
		},
	}
	return cmd
}
