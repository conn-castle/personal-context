package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
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
	var deviceID string
	cmd := &cobra.Command{
		Use:   "add <id> [path]",
		Short: "Register a project",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) == 2 {
				path = args[1]
			}
			return runProjectAdd(cmd.Context(), stdout, stderr, args[0], path, deviceID)
		},
	}
	cmd.Flags().StringVar(&deviceID, "device", "", "Registered source device for the project path")
	return cmd
}

// runProjectAdd registers `id` as a project. When `path` is non-empty it also
// registers the project path on `deviceID` (which becomes required). The
// helper is also called directly from tests with empty `path`/`deviceID`.
func runProjectAdd(ctx context.Context, stdout io.Writer, _ io.Writer, id string, path string, deviceID string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("project id must not be empty")
	}
	stack, err := openLocalStackFromHome()
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()
	projectID := strings.TrimSpace(id)
	var normalizedPath string
	if strings.TrimSpace(path) != "" {
		deviceID = strings.TrimSpace(deviceID)
		if deviceID == "" {
			return fmt.Errorf("--device is required when registering a project path")
		}
		if err := validateActiveDevice(ctx, stack.Repo, deviceID); err != nil {
			return err
		}
		normalized, err := normalizeProjectPath(path)
		if err != nil {
			return err
		}
		normalizedPath = normalized
	}
	project, err := stack.Repo.CreateProject(ctx, repository.CreateRegistryInput{ID: projectID})
	if err != nil {
		if !errors.Is(err, repository.ErrConflict) {
			return fmt.Errorf("add project: %w", err)
		}
		project, err = stack.Repo.GetProjectByID(ctx, projectID)
		if err != nil {
			return fmt.Errorf("get existing project: %w", err)
		}
		_, _ = fmt.Fprintf(stdout, "%s already registered\n", project.ID)
	} else {
		_, _ = fmt.Fprintf(stdout, "%s registered\n", project.ID)
	}
	if strings.TrimSpace(path) == "" {
		return nil
	}
	registered, created, err := stack.Repo.UpsertProjectPath(ctx, repository.CreateProjectPathInput{
		ProjectID: project.ID,
		Path:      normalizedPath,
		DeviceID:  deviceID,
	})
	if err != nil {
		return fmt.Errorf("register project path: %w", err)
	}
	if created {
		_, _ = fmt.Fprintf(stdout, "%s path registered for %s on %s\n", project.ID, registered.Path, registered.DeviceID)
	} else {
		_, _ = fmt.Fprintf(stdout, "%s path already registered for %s on %s\n", project.ID, registered.Path, registered.DeviceID)
	}
	backfilled, err := stack.Repo.BackfillChatProjects(ctx)
	if err != nil {
		return fmt.Errorf("backfill chat projects: %w", err)
	}
	if backfilled > 0 {
		_, _ = fmt.Fprintf(stdout, "Backfilled %d chat session(s)\n", backfilled)
	}
	return nil
}

func validateActiveDevice(ctx context.Context, repo repository.Repository, deviceID string) error {
	device, err := repo.GetDeviceByID(ctx, deviceID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("device %q is not registered; run `pc device list` or `pc device register %s`", deviceID, deviceID)
		}
		return fmt.Errorf("get device %q: %w", deviceID, err)
	}
	if device.ArchivedAt != nil {
		return fmt.Errorf("device %q is archived; run `pc device restore %s` before using it", deviceID, deviceID)
	}
	return nil
}

func normalizeProjectPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("project path must not be empty")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve project path symlinks: %w", err)
	}
	return filepath.Clean(resolved), nil
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
