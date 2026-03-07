package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// newProjectCommand creates the `pc project` subcommand group.
func newProjectCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage active project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newProjectSetCommand(stdout, stderr))
	cmd.AddCommand(newProjectClearCommand(stdout, stderr))
	cmd.AddCommand(newProjectListCommand(stdout, stderr))
	return cmd
}

// newProjectSetCommand creates the `pc project set <name>` subcommand.
func newProjectSetCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "set <name>",
		Short: "Set the active project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectSet(cmd.Context(), stdout, stderr, args[0])
		},
	}
}

// runProjectSet stores the active project name in config.
// Args: ctx is the command context, stdout/stderr are output writers, name is the project name.
// Returns: nil on success or an error.
func runProjectSet(_ context.Context, stdout io.Writer, _ io.Writer, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("project name must not be empty")
	}

	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	store, err := newConfigStoreFn(homeDir)
	if err != nil {
		return fmt.Errorf("create config store: %w", err)
	}

	cfg, err := store.Read()
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	cfg.ActiveProject = name

	if err := store.Write(cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "Active project set to %q\n", name)
	return nil
}

// newProjectClearCommand creates the `pc project clear` subcommand.
func newProjectClearCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear the active project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProjectClear(cmd.Context(), stdout, stderr)
		},
	}
}

// runProjectClear removes the active project from config.
// Args: ctx is the command context, stdout/stderr are output writers.
// Returns: nil on success or an error.
func runProjectClear(_ context.Context, stdout io.Writer, _ io.Writer) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	store, err := newConfigStoreFn(homeDir)
	if err != nil {
		return fmt.Errorf("create config store: %w", err)
	}

	cfg, err := store.Read()
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	cfg.ActiveProject = ""

	if err := store.Write(cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	_, _ = fmt.Fprintln(stdout, "Active project cleared.")
	return nil
}

// newProjectListCommand creates the `pc project list` subcommand.
func newProjectListCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List distinct project IDs from slides",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProjectList(cmd.Context(), stdout, stderr)
		},
	}
}

// runProjectList prints distinct project IDs from the database, marking the active one.
// Args: ctx is the command context, stdout/stderr are output writers.
// Returns: nil on success or an error.
func runProjectList(ctx context.Context, stdout io.Writer, _ io.Writer) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	projects, err := stack.Repo.ListDistinctProjectIDs(ctx)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	if len(projects) == 0 {
		_, _ = fmt.Fprintln(stdout, "No projects found.")
		return nil
	}

	for _, p := range projects {
		if p == stack.Config.ActiveProject {
			_, _ = fmt.Fprintf(stdout, "%s (active)\n", p)
		} else {
			_, _ = fmt.Fprintln(stdout, p)
		}
	}

	return nil
}
