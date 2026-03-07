package cli

import (
	"io"

	"github.com/spf13/cobra"
)

const DefaultVersion = "dev"

type RootCommandOptions struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
}

// NewRootCommand builds the root `pc` command.
// Args: opts contains optional output writers and version string.
// Returns: a fully configured Cobra command that is ready to execute.
func NewRootCommand(opts RootCommandOptions) *cobra.Command {
	stdout := opts.Stdout
	stderr := opts.Stderr
	version := opts.Version

	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
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
	root.Version = version
	root.SetVersionTemplate("pc version {{.Version}}\n")

	root.AddCommand(newSetupCommand(stdout, stderr))
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

	return root
}
