package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newProjectCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage project registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newProjectListCommand(stdout, stderr))
	cmd.AddCommand(newProjectAddCommand(stdout, stderr))
	cmd.AddCommand(newProjectArchiveCommand(stdout, stderr))
	cmd.AddCommand(newProjectRestoreCommand(stdout, stderr))
	return cmd
}

func newProjectListCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var includeArchived bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProjectList(cmd.Context(), stdout, stderr, includeArchived)
		},
	}
	cmd.Flags().BoolVar(&includeArchived, "all", false, "Include archived projects")
	return cmd
}

func runProjectList(ctx context.Context, stdout io.Writer, _ io.Writer, includeArchived bool) error {
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	projects, err := stack.Repo.ListProjects(ctx, includeArchived)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	if len(projects) == 0 {
		_, _ = fmt.Fprintln(stdout, "No projects registered.")
		return nil
	}
	for _, project := range projects {
		if project.ArchivedAt != nil {
			_, _ = fmt.Fprintf(stdout, "%s (archived)\n", project.ID)
		} else {
			_, _ = fmt.Fprintln(stdout, project.ID)
		}
	}
	return nil
}

func newProjectAddCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "add <id>",
		Short: "Register a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectAdd(cmd.Context(), stdout, stderr, args[0])
		},
	}
}

func runProjectAdd(ctx context.Context, stdout io.Writer, _ io.Writer, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("project id must not be empty")
	}
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	project, err := stack.Repo.CreateProject(ctx, repository.CreateRegistryInput{ID: strings.TrimSpace(id)})
	if err != nil {
		return fmt.Errorf("add project: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "%s\n", project.ID)
	return nil
}

func newProjectArchiveCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <id>",
		Short: "Archive a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectArchive(cmd.Context(), stdout, stderr, args[0])
		},
	}
}

func runProjectArchive(ctx context.Context, stdout io.Writer, _ io.Writer, id string) error {
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	project, err := stack.Repo.ArchiveProject(ctx, id)
	if err != nil {
		return fmt.Errorf("archive project: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "%s archived\n", project.ID)
	return nil
}

func newProjectRestoreCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <id>",
		Short: "Restore an archived project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectRestore(cmd.Context(), stdout, stderr, args[0])
		},
	}
}

func runProjectRestore(ctx context.Context, stdout io.Writer, _ io.Writer, id string) error {
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	project, err := stack.Repo.RestoreProject(ctx, id)
	if err != nil {
		return fmt.Errorf("restore project: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "%s restored\n", project.ID)
	return nil
}

func openLocalStackFromHome() (*localStack, error) {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return nil, err
	}
	return openLocalStack(homeDir)
}
