package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

// setupOptions holds the flags for the setup command.
type setupOptions struct {
	NeonURL          string
	S3Bucket         string
	S3Region         string
	AWSKey           string
	AWSSecret        string
	S3Endpoint       string
	S3ForcePathStyle bool
	RemoveCloud      bool
	APIKey           string
	InitCloudSchema  bool
}

// hasCloudFlags returns true if any cloud flag was provided.
func (o setupOptions) hasCloudFlags() bool {
	return o.NeonURL != "" || o.S3Bucket != "" || o.S3Region != "" ||
		o.AWSKey != "" || o.AWSSecret != "" || o.APIKey != ""
}

func (o setupOptions) hasCloudSchemaOnlyConflictFlags() bool {
	return o.S3Bucket != "" || o.S3Region != "" || o.AWSKey != "" ||
		o.AWSSecret != "" || o.S3Endpoint != "" || o.S3ForcePathStyle ||
		strings.TrimSpace(o.APIKey) != ""
}

// validateCloudFlagsComplete returns an error listing any missing cloud flags.
func (o setupOptions) validateCloudFlagsComplete() error {
	var missing []string
	if o.NeonURL == "" {
		missing = append(missing, "--neon-url")
	}
	if o.S3Bucket == "" {
		missing = append(missing, "--s3-bucket")
	}
	if o.S3Region == "" {
		missing = append(missing, "--s3-region")
	}
	if o.AWSKey == "" {
		missing = append(missing, "--aws-key")
	}
	if o.AWSSecret == "" {
		missing = append(missing, "--aws-secret")
	}
	if strings.TrimSpace(o.APIKey) == "" {
		missing = append(missing, "--api-key")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required cloud flags: %s", strings.Join(missing, ", "))
	}
	return nil
}

// newSetupCommand creates the `pc setup` subcommand.
func newSetupCommand(stdout io.Writer, stderr io.Writer, stdin io.Reader) *cobra.Command {
	var opts setupOptions
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize local Personal Context environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSetup(cmd.Context(), stdout, stderr, stdin, opts)
		},
	}
	cmd.SetIn(stdin)

	cmd.Flags().StringVar(&opts.NeonURL, "neon-url", "", "Neon Postgres connection URL")
	cmd.Flags().StringVar(&opts.S3Bucket, "s3-bucket", "", "S3 bucket name")
	cmd.Flags().StringVar(&opts.S3Region, "s3-region", "", "AWS region for S3 bucket")
	cmd.Flags().StringVar(&opts.AWSKey, "aws-key", "", "AWS access key ID")
	cmd.Flags().StringVar(&opts.AWSSecret, "aws-secret", "", "AWS secret access key")
	cmd.Flags().StringVar(&opts.S3Endpoint, "s3-endpoint", "", "Custom S3 endpoint URL (for S3-compatible services)")
	cmd.Flags().BoolVar(&opts.S3ForcePathStyle, "s3-force-path-style", false, "Use path-style S3 addressing (for S3-compatible services)")
	cmd.Flags().BoolVar(&opts.RemoveCloud, "remove-cloud", false, "Remove cloud configuration")
	cmd.Flags().StringVar(&opts.APIKey, "api-key", "", "API key for CLI authentication (generate from authenticated web app)")
	cmd.Flags().BoolVar(&opts.InitCloudSchema, "init-cloud-schema", false, "Initialize the cloud Postgres schema only")

	return cmd
}

// runSetup initializes the local environment and optionally configures cloud sync.
func runSetup(ctx context.Context, stdout io.Writer, stderr io.Writer, stdin io.Reader, opts setupOptions) error {
	// Validate flag combinations.
	if opts.RemoveCloud && (opts.hasCloudFlags() || opts.InitCloudSchema) {
		return fmt.Errorf("--remove-cloud cannot be used with cloud configuration flags")
	}
	if opts.InitCloudSchema {
		if opts.NeonURL == "" {
			return fmt.Errorf("--init-cloud-schema requires --neon-url")
		}
		if opts.hasCloudSchemaOnlyConflictFlags() {
			return fmt.Errorf("--init-cloud-schema can only be used with --neon-url")
		}
		return runSetupInitCloudSchema(ctx, stdout, opts.NeonURL)
	}

	// Wrap stdin so all prompt calls share the same buffered reader.
	br := bufio.NewReader(stdin)

	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	store, err := newConfigStoreFn(homeDir)
	if err != nil {
		return fmt.Errorf("create config store: %w", err)
	}

	if opts.RemoveCloud {
		return runSetupRemoveCloud(stdout, homeDir, store)
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

	if opts.hasCloudFlags() {
		if err := opts.validateCloudFlagsComplete(); err != nil {
			return err
		}
		if err := runSetupCloud(ctx, stdout, stderr, nil, homeDir, store,
			opts.NeonURL, opts.S3Bucket, opts.S3Region, opts.AWSKey, opts.AWSSecret,
			opts.S3Endpoint, opts.S3ForcePathStyle, opts.APIKey,
			nil, false); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Personal Context initialized at %s\n", base)
		return nil
	}

	// No cloud flags — ensure local config exists and prompt interactively.
	if err := ensureConfig(store); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "Personal Context initialized at %s\n", base)

	wantCloud, err := promptConfirmFn(br, stdout, "\nConfigure cloud sync?")
	if err != nil {
		return fmt.Errorf("read cloud prompt: %w", err)
	}
	if !wantCloud {
		return nil
	}

	neonURL, err := promptLineFn(br, stdout, "Neon Postgres URL: ")
	if err != nil {
		return fmt.Errorf("read neon URL: %w", err)
	}
	s3Bucket, err := promptLineFn(br, stdout, "S3 bucket: ")
	if err != nil {
		return fmt.Errorf("read S3 bucket: %w", err)
	}
	s3Region, err := promptLineFn(br, stdout, "S3 region: ")
	if err != nil {
		return fmt.Errorf("read S3 region: %w", err)
	}
	awsKey, err := promptLineFn(br, stdout, "AWS access key: ")
	if err != nil {
		return fmt.Errorf("read AWS access key: %w", err)
	}
	awsSecret, err := promptLineFn(br, stdout, "AWS secret key: ")
	if err != nil {
		return fmt.Errorf("read AWS secret key: %w", err)
	}
	apiKey, err := promptLineFn(br, stdout, "API key (generate from authenticated web app): ")
	if err != nil {
		return fmt.Errorf("read API key: %w", err)
	}

	return runSetupCloud(ctx, stdout, stderr, br, homeDir, store,
		neonURL, s3Bucket, s3Region, awsKey, awsSecret,
		"", false, apiKey,
		repo, true)
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
