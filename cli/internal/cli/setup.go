package cli

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/syncengine"
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

	// The chat feature ships as a clean-cut schema update (DECISIONS.md
	// n2p3q4): there is no forward migration that adds the chat tables to
	// a pre-existing v0.1.1 store. The migration runner records the
	// schema as a single `001_initial.sql` row in `schema_migrations`, so
	// re-running `pc setup` against an old DB silently skips schema
	// re-application and leaves `chat_session`/`chat_item` missing.
	// Detect that here and fail loudly instead of declaring success.
	if err := verifyCanonicalSchemaTables(ctx, conn.DB(), base); err != nil {
		return err
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

// canonicalSchemaTables is a curated probe (not a mirror of
// cli/internal/sqlite/sqlite_schema.sql) used to detect stores that predate
// the chat-tables clean-cut schema update (DECISIONS.md n2p3q4). It includes:
//   - chat_session, chat_item: the tables a v0.1.1-vintage store will be
//     missing - the actual failure mode we want to surface.
//   - chat_item_fts: the search table whose pre-release shape changed from
//     standalone FTS5 to external-content FTS5.
//   - records, project_paths: sanity-check tables that any working store
//     must have; absent means the migration runner did not run at all.
//
// New canonical tables should be added here only if their absence would
// indicate the same pre-chat-tables mismatch, not because they exist in
// the schema.
var canonicalSchemaTables = []string{
	"records",
	"chat_session",
	"chat_item",
	"project_paths",
}

const chatSessionParentSourceColumn = "parent_source_session_id"

// verifyCanonicalSchemaTables fails loudly when expected tables are missing
// after schema application. The recovery message names the actual store
// directory (`base`, which honors PC_HOME) so users back up the right path.
func verifyCanonicalSchemaTables(ctx context.Context, db *sql.DB, base string) error {
	return verifyCanonicalSchemaTablesWithLock(ctx, db, base, "")
}

func verifyCanonicalSchemaTablesDuringOpen(ctx context.Context, db *sql.DB, base string) error {
	if err := verifyChatSessionParentSourceColumnForExistingTable(ctx, db, base); err != nil {
		return err
	}
	return verifyChatItemFTSShapeWithLock(ctx, db, base, filepath.Join(base, ".pc", "sync.lock"))
}

func verifyCanonicalSchemaTablesWithLock(ctx context.Context, db *sql.DB, base string, lockPath string) error {
	for _, table := range canonicalSchemaTables {
		var name string
		err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf(
				"local store is missing required table %q: this database predates the current Personal Context schema and cannot be upgraded in place. Back up your existing store (e.g. `mv %s %s.backup-$(date +%%Y%%m%%dT%%H%%M%%S)`) and re-run `pc setup` to initialize a fresh store",
				table, base, base,
			)
		}
		if err != nil {
			return fmt.Errorf("verify schema table %q: %w", table, err)
		}
	}
	if err := verifyChatSessionParentSourceColumn(ctx, db, base); err != nil {
		return err
	}
	if err := verifyChatItemFTSShapeWithLock(ctx, db, base, lockPath); err != nil {
		return err
	}
	return nil
}

func verifyChatSessionParentSourceColumn(ctx context.Context, db *sql.DB, base string) error {
	found, err := chatSessionParentSourceColumnExists(ctx, db)
	if err != nil {
		return err
	}
	if !found {
		return missingChatSessionParentSourceColumnError(base)
	}
	return nil
}

func verifyChatSessionParentSourceColumnForExistingTable(ctx context.Context, db *sql.DB, base string) error {
	var name string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, "chat_session").Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("verify schema table %q: %w", "chat_session", err)
	}
	return verifyChatSessionParentSourceColumn(ctx, db, base)
}

func missingChatSessionParentSourceColumnError(base string) error {
	return fmt.Errorf(
		"local store is missing required column %q on table %q: this database predates the current Personal Context schema and cannot be upgraded in place. Back up your existing store (e.g. `mv %s %s.backup-$(date +%%Y%%m%%dT%%H%%M%%S)`) and re-run `pc setup` to initialize a fresh store",
		chatSessionParentSourceColumn, "chat_session", base, base,
	)
}

func chatSessionParentSourceColumnExists(ctx context.Context, db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('chat_session') WHERE name = ?`, chatSessionParentSourceColumn).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("verify schema columns for table %q: %w", "chat_session", err)
	}
	return count > 0, nil
}

func verifyChatItemFTSShape(ctx context.Context, db *sql.DB, base string) error {
	return verifyChatItemFTSShapeWithLock(ctx, db, base, "")
}

func verifyChatItemFTSShapeWithLock(ctx context.Context, db *sql.DB, base string, lockPath string) error {
	if err := verifyChatItemFTSExternalContentShape(ctx, db, base); err != nil {
		return err
	}
	for _, trigger := range []string{
		"chat_item_fts_after_insert",
		"chat_item_fts_after_update",
		"chat_item_fts_after_delete",
	} {
		var name string
		err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='trigger' AND name=?`, trigger).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			if strings.TrimSpace(lockPath) != "" {
				if err := checkSchemaValidationSyncLock(lockPath); err != nil {
					return err
				}
			}
			return fmt.Errorf(
				"local store is missing required trigger %q: this database predates or was interrupted while applying the current Personal Context schema and cannot be upgraded in place. Back up your existing store (e.g. `mv %s %s.backup-$(date +%%Y%%m%%dT%%H%%M%%S)`) and re-run `pc setup` to initialize a fresh store",
				trigger, base, base,
			)
		}
		if err != nil {
			return fmt.Errorf("verify schema trigger %q: %w", trigger, err)
		}
	}
	return nil
}

func checkSchemaValidationSyncLock(lockPath string) error {
	lock, err := syncengine.AcquireFileLock(lockPath)
	if err == nil {
		if err := lock.Release(); err != nil {
			return fmt.Errorf("release local sync lock check: %w", err)
		}
		return nil
	}
	if errors.Is(err, syncengine.ErrSyncLocked) {
		return fmt.Errorf("local store is temporarily locked for sync or chat import; retry after the current operation finishes: %w", err)
	}
	return fmt.Errorf("check local sync lock before chat FTS trigger validation: %w", err)
}

func verifyChatItemFTSExternalContentShape(ctx context.Context, db *sql.DB, base string) error {
	var ddl string
	err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='chat_item_fts'`).Scan(&ddl)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"local store is missing required table %q: this database predates the current Personal Context schema and cannot be upgraded in place. Back up your existing store (e.g. `mv %s %s.backup-$(date +%%Y%%m%%dT%%H%%M%%S)`) and re-run `pc setup` to initialize a fresh store",
			"chat_item_fts", base, base,
		)
	}
	if err != nil {
		return fmt.Errorf("verify schema table %q: %w", "chat_item_fts", err)
	}
	if !strings.Contains(ddl, "content='chat_item'") || !strings.Contains(ddl, "content_rowid='id'") {
		return fmt.Errorf(
			"local store has incompatible table %q: this database predates the current Personal Context schema and cannot be upgraded in place. Back up your existing store (e.g. `mv %s %s.backup-$(date +%%Y%%m%%dT%%H%%M%%S)`) and re-run `pc setup` to initialize a fresh store",
			"chat_item_fts", base, base,
		)
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
