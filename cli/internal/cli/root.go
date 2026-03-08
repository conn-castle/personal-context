package cli

import (
	"io"
	"os"

	"github.com/spf13/cobra"
)

const DefaultVersion = "dev"

type RootCommandOptions struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Stdin   io.Reader
	Version string
}

// NewRootCommand builds the root `pc` command.
// Args: opts contains optional output writers and version string.
// Returns: a fully configured Cobra command that is ready to execute.
func NewRootCommand(opts RootCommandOptions) *cobra.Command {
	stdout := opts.Stdout
	stderr := opts.Stderr
	stdin := opts.Stdin
	version := opts.Version

	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if stdin == nil {
		stdin = os.Stdin
	}
	if version == "" {
		version = DefaultVersion
	}

	root := &cobra.Command{
		Use:           "pc",
		Short:         "Personal Context CLI",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetIn(stdin)
	root.Version = version
	root.SetVersionTemplate("pc version {{.Version}}\n")

	root.AddCommand(newSetupCommand(stdout, stderr, stdin))
	root.AddCommand(newAddCommand(stdout, stderr))
	root.AddCommand(newShowCommand(stdout, stderr))
	root.AddCommand(newEditCommand(stdout, stderr))
	root.AddCommand(newDeleteCommand(stdout, stderr))
	root.AddCommand(newRestoreCommand(stdout, stderr))
	root.AddCommand(newMoveCommand(stdout, stderr))
	root.AddCommand(newTrashCommand(stdout, stderr))
	root.AddCommand(newSearchCommand(stdout, stderr))
	root.AddCommand(newProjectCommand(stdout, stderr))
	root.AddCommand(newGCCommand(stdout, stderr))
	root.AddCommand(newDoctorCommand(stdout, stderr))
	root.AddCommand(newSyncCommand(stdout, stderr))

	return root
}
