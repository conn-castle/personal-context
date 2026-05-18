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
	root.AddCommand(newRecordsCommand(stdout, stderr))
	root.AddCommand(newChatCommand(stdout, stderr))
	root.AddCommand(newShowCommand(stdout, stderr))
	root.AddCommand(newRestoreDBCommand(stdout, stderr))
	root.AddCommand(newTrashCommand(stdout, stderr))
	root.AddCommand(newSearchCommand(stdout, stderr))
	root.AddCommand(newProjectCommand(stdout, stderr))
	root.AddCommand(newDeviceCommand(stdout, stderr))
	root.AddCommand(newGCCommand(stdout, stderr))
	root.AddCommand(newDoctorCommand(stdout, stderr))
	root.AddCommand(newSyncCommand(stdout, stderr))
	root.AddCommand(newFetchCommand(stdout, stderr))
	root.AddCommand(newExportCommand(stdout, stderr))
	root.AddCommand(newImportCommand(stdout, stderr))
	root.AddCommand(newVerifyCommand(stdout, stderr))
	root.AddCommand(newServeCommand(stdout, stderr, version))
	root.AddCommand(newScreenshotCommand(stdout, stderr))
	root.AddCommand(newSeedCommand(stdout, stderr))

	return root
}
