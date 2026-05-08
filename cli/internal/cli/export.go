package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/gitsnapshot"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

type exportOptions struct {
	Path         string
	FromCloud    bool
	GitHubRemote string
	ProjectID    string
	DateFrom     string
	DateTo       string
}

func newExportCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var opts exportOptions

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export active records to deterministic git snapshot format",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExport(cmd.Context(), stdout, stderr, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Path, "path", "", "Destination directory for templates/ and records/")
	cmd.Flags().BoolVar(&opts.FromCloud, "from-cloud", false, "Read from configured cloud Postgres/S3 instead of local SQLite/files")
	cmd.Flags().StringVar(&opts.GitHubRemote, "github-remote", "", "Require this git remote to exist at --path before exporting")
	cmd.Flags().StringVar(&opts.ProjectID, "project", "", "Export only records in this project ID")
	cmd.Flags().StringVar(&opts.DateFrom, "from", "", "Export records on or after date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&opts.DateTo, "to", "", "Export records on or before date (YYYY-MM-DD)")

	return cmd
}

func runExport(ctx context.Context, stdout io.Writer, _ io.Writer, opts exportOptions) error {
	if strings.TrimSpace(opts.Path) == "" {
		return fmt.Errorf("--path is required")
	}
	if err := validateGitRemote(opts.Path, opts.GitHubRemote); err != nil {
		return err
	}
	filter, err := buildExportFilter(opts)
	if err != nil {
		return err
	}

	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	var snapshot gitsnapshot.Snapshot
	if opts.FromCloud {
		cloud, err := openCloudStackFn(ctx, homeDir, "")
		if err != nil {
			if errors.Is(err, errCloudNotConfigured) {
				return fmt.Errorf("cloud is not configured; run 'pc setup' first")
			}
			return fmt.Errorf("open cloud: %w", err)
		}
		defer func() { _ = cloud.Close() }()
		snapshot, err = buildCloudSnapshot(ctx, homeDir, cloud, filter)
		if err != nil {
			return err
		}
	} else {
		stack, err := openLocalStack(homeDir)
		if err != nil {
			return err
		}
		defer func() { _ = stack.Close() }()
		snapshot, err = buildLocalSnapshot(ctx, stack, filter)
		if err != nil {
			return err
		}
	}

	if err := gitsnapshot.Write(opts.Path, snapshot); err != nil {
		return fmt.Errorf("write export snapshot: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "Exported %d records to %s\n", len(snapshot.Records), opts.Path)
	return nil
}

func buildExportFilter(opts exportOptions) (repository.ListRecordsFilter, error) {
	return buildBaseRecordFilter(recordFilterOptions{
		ProjectID: opts.ProjectID,
		DateFrom:  opts.DateFrom,
		DateTo:    opts.DateTo,
	})
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
