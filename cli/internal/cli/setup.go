package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

// newSetupCommand creates the `pc setup` subcommand.
func newSetupCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize local Personal Context environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSetup(cmd.Context(), stdout, stderr)
		},
	}
	return cmd
}

// runSetup initializes the local environment: directories, SQLite DB, templates, config.
func runSetup(ctx context.Context, stdout io.Writer, _ io.Writer) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	base := basePath(homeDir)
	pcDir := filepath.Join(base, ".pc")

	if err := os.MkdirAll(pcDir, 0o700); err != nil {
		return fmt.Errorf("create .pc directory %q: %w", pcDir, err)
	}

	conn, err := openSQLiteFn(dbPath(homeDir))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = conn.Close() }()

	migrationsFS, err := sqliteMigrationsFSFn()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	if err := conn.ApplyMigrationsFS(ctx, migrationsFS); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	repo, err := newSQLiteRepoFn(conn.DB())
	if err != nil {
		return fmt.Errorf("create repository: %w", err)
	}

	if err := seedTemplates(ctx, repo); err != nil {
		return fmt.Errorf("seed templates: %w", err)
	}

	store, err := newConfigStoreFn(homeDir)
	if err != nil {
		return fmt.Errorf("create config store: %w", err)
	}

	if err := ensureConfig(store); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "Personal Context initialized at %s\n", base)
	return nil
}

// seedTemplates inserts builtin templates, skipping any that already exist.
func seedTemplates(ctx context.Context, repo repository.Repository) error {
	for _, tmpl := range builtinTemplates {
		_, err := repo.GetTemplateByName(ctx, tmpl.Name)
		if err == nil {
			continue
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("check template %q: %w", tmpl.Name, err)
		}
		if _, err := repo.CreateTemplate(ctx, tmpl); err != nil {
			return fmt.Errorf("create template %q: %w", tmpl.Name, err)
		}
	}
	return nil
}

// ensureConfig writes a local-only config if no config file exists.
func ensureConfig(store config.Store) error {
	if _, err := store.Read(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store.Write(config.Config{})
		}
		return err
	}
	return nil
}
