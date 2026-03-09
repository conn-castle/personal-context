package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/gitsnapshot"
	"github.com/spf13/cobra"
)

type exportOptions struct {
	Path         string
	FromCloud    bool
	GitHubRemote string
}

func newExportCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var opts exportOptions

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export active slides to deterministic git snapshot format",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExport(cmd.Context(), stdout, stderr, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Path, "path", "", "Destination directory for templates/ and slides/")
	cmd.Flags().BoolVar(&opts.FromCloud, "from-cloud", false, "Read from configured cloud Postgres/S3 instead of local SQLite/files")
	cmd.Flags().StringVar(&opts.GitHubRemote, "github-remote", "", "Require this git remote to exist at --path before exporting")

	return cmd
}

func runExport(ctx context.Context, stdout io.Writer, _ io.Writer, opts exportOptions) error {
	if strings.TrimSpace(opts.Path) == "" {
		return fmt.Errorf("--path is required")
	}
	if err := validateGitRemote(opts.Path, opts.GitHubRemote); err != nil {
		return err
	}

	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	var snapshot gitsnapshot.Snapshot
	if opts.FromCloud {
		cloud, err := openCloudStackFn(ctx, homeDir)
		if err != nil {
			if errors.Is(err, errCloudNotConfigured) {
				return fmt.Errorf("cloud is not configured; run 'pc setup' first")
			}
			return fmt.Errorf("open cloud: %w", err)
		}
		defer func() { _ = cloud.Close() }()
		snapshot, err = buildCloudSnapshot(ctx, homeDir, cloud)
		if err != nil {
			return err
		}
	} else {
		stack, err := openLocalStack(homeDir)
		if err != nil {
			return err
		}
		defer func() { _ = stack.Close() }()
		snapshot, err = buildLocalSnapshot(ctx, stack)
		if err != nil {
			return err
		}
	}

	if err := gitsnapshot.Write(opts.Path, snapshot); err != nil {
		return fmt.Errorf("write export snapshot: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "Exported snapshot to %s\n", opts.Path)
	return nil
}

func validateGitRemote(path string, remote string) error {
	if strings.TrimSpace(remote) == "" {
		return nil
	}
	cmd := exec.Command("git", "-C", path, "remote", "get-url", remote)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("verify git remote %q at %s: %w", remote, path, err)
	}
	return nil
}
