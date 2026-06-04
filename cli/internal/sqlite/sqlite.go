package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/timeutil"
	sqlitedriver "modernc.org/sqlite"
)

func init() {
	sqlitedriver.RegisterConnectionHook(func(conn sqlitedriver.ExecQuerierContext, dsn string) error {
		_, err := conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON", nil)
		return err
	})
}

type sqliteHooks struct {
	sqlOpen              func(string, string) (*sql.DB, error)
	ensureMigrationTable func(context.Context, *sql.DB) error
	isMigrationApplied   func(context.Context, *sql.DB, string) (bool, error)
	readFile             func(string) ([]byte, error)
	beginTx              func(*sql.DB, context.Context) (*sql.Tx, error)
	queryJournalMode     func(context.Context, *sql.DB) (string, error)
}

func defaultSQLiteHooks() sqliteHooks {
	return sqliteHooks{
		sqlOpen:              sql.Open,
		ensureMigrationTable: ensureMigrationTable,
		isMigrationApplied:   isMigrationApplied,
		readFile:             os.ReadFile,
		beginTx: func(db *sql.DB, ctx context.Context) (*sql.Tx, error) {
			return db.BeginTx(ctx, nil)
		},
		queryJournalMode: func(ctx context.Context, db *sql.DB) (string, error) {
			var mode string
			err := db.QueryRowContext(ctx, `PRAGMA journal_mode = WAL;`).Scan(&mode)
			return mode, err
		},
	}
}

func (h sqliteHooks) withDefaults() sqliteHooks {
	defaults := defaultSQLiteHooks()
	if h.sqlOpen == nil {
		h.sqlOpen = defaults.sqlOpen
	}
	if h.ensureMigrationTable == nil {
		h.ensureMigrationTable = defaults.ensureMigrationTable
	}
	if h.isMigrationApplied == nil {
		h.isMigrationApplied = defaults.isMigrationApplied
	}
	if h.readFile == nil {
		h.readFile = defaults.readFile
	}
	if h.beginTx == nil {
		h.beginTx = defaults.beginTx
	}
	if h.queryJournalMode == nil {
		h.queryJournalMode = defaults.queryJournalMode
	}
	return h
}

// Connection wraps an SQLite database handle configured for Personal Context.
type Connection struct {
	db *sql.DB
}

// Open creates a configured SQLite connection wrapper.
// Args: path is the SQLite database file path.
// Returns: a connection wrapper with WAL and foreign key enforcement enabled.
func Open(path string) (*Connection, error) {
	return openWithHooks(path, defaultSQLiteHooks())
}

func openWithHooks(path string, hooks sqliteHooks) (*Connection, error) {
	hooks = hooks.withDefaults()
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create sqlite parent directory: %w", err)
	}

	db, err := hooks.sqlOpen("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if err := configureWithHooks(context.Background(), db, hooks); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Connection{db: db}, nil
}

// DB exposes the underlying sql.DB handle.
// Args: none.
// Returns: configured database handle.
func (c *Connection) DB() *sql.DB {
	return c.db
}

// Close closes the underlying database handle.
// Args: none.
// Returns: close error from sql.DB.
func (c *Connection) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// ApplyMigrations applies migration SQL files from migrationsDir in lexical order.
// Args: ctx controls cancellation; migrationsDir points to SQL migration files.
// Returns: nil when all unapplied migrations are recorded and applied.
func (c *Connection) ApplyMigrations(ctx context.Context, migrationsDir string) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("connection is required")
	}
	return ApplyMigrations(ctx, c.db, migrationsDir)
}

// ApplyMigrations applies migration SQL files from migrationsDir in lexical order.
// Args: ctx controls cancellation; db is the target sqlite handle; migrationsDir contains .sql files.
// Returns: nil when all pending migrations are applied and recorded.
func ApplyMigrations(ctx context.Context, db *sql.DB, migrationsDir string) error {
	return applyMigrationsWithHooks(ctx, db, migrationsDir, defaultSQLiteHooks())
}

func applyMigrationsWithHooks(ctx context.Context, db *sql.DB, migrationsDir string, hooks sqliteHooks) error {
	hooks = hooks.withDefaults()
	if db == nil {
		return fmt.Errorf("db is required")
	}
	if strings.TrimSpace(migrationsDir) == "" {
		return fmt.Errorf("migrations directory is required")
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations directory: %w", err)
	}

	return applyPendingMigrations(ctx, db, sqlMigrationFilenames(entries), func(migration string) ([]byte, error) {
		return hooks.readFile(filepath.Join(migrationsDir, migration))
	}, hooks)
}

// ApplyMigrationsFS applies migration SQL files from an fs.FS in lexical order.
// Args: ctx controls cancellation; fsys contains .sql files at the root.
// Returns: nil when all pending migrations are applied and recorded.
func (c *Connection) ApplyMigrationsFS(ctx context.Context, fsys fs.FS) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("connection is required")
	}
	return ApplyMigrationsFromFS(ctx, c.db, fsys)
}

// ApplyMigrationsFromFS applies migration SQL files from an fs.FS in lexical order.
// Args: ctx controls cancellation; db is the target sqlite handle; fsys contains .sql files.
// Returns: nil when all pending migrations are applied and recorded.
func ApplyMigrationsFromFS(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	return applyMigrationsFromFSWithHooks(ctx, db, fsys, defaultSQLiteHooks())
}

func applyMigrationsFromFSWithHooks(ctx context.Context, db *sql.DB, fsys fs.FS, hooks sqliteHooks) error {
	hooks = hooks.withDefaults()
	if db == nil {
		return fmt.Errorf("db is required")
	}
	if fsys == nil {
		return fmt.Errorf("filesystem is required")
	}

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("read migrations filesystem: %w", err)
	}

	return applyPendingMigrations(ctx, db, sqlMigrationFilenames(entries), func(migration string) ([]byte, error) {
		return fs.ReadFile(fsys, migration)
	}, hooks)
}

// sqlMigrationFilenames keeps only root-level SQL migration files and returns
// them in the lexical order expected by the migration runner.
func sqlMigrationFilenames(entries []fs.DirEntry) []string {
	migrations := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		migrations = append(migrations, entry.Name())
	}
	sort.Strings(migrations)
	return migrations
}

// applyPendingMigrations executes any unapplied migrations returned by the
// caller-provided reader.
func applyPendingMigrations(
	ctx context.Context,
	db *sql.DB,
	migrations []string,
	readMigration func(string) ([]byte, error),
	hooks sqliteHooks,
) error {
	hooks = hooks.withDefaults()
	if err := hooks.ensureMigrationTable(ctx, db); err != nil {
		return err
	}

	for _, migration := range migrations {
		applied, err := hooks.isMigrationApplied(ctx, db, migration)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		content, err := readMigration(migration)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", migration, err)
		}

		if err := applyMigration(ctx, db, migration, content, hooks); err != nil {
			return err
		}
	}

	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, migration string, content []byte, hooks sqliteHooks) error {
	tx, err := hooks.withDefaults().beginTx(db, ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("apply migration %s: %w", migration, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
		migration,
		timeutil.FormatUTCMillis(time.Now()),
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record migration %s: %w", migration, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", migration, err)
	}

	return nil
}

func configure(ctx context.Context, db *sql.DB) error {
	return configureWithHooks(ctx, db, defaultSQLiteHooks())
}

func configureWithHooks(ctx context.Context, db *sql.DB, hooks sqliteHooks) error {
	hooks = hooks.withDefaults()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite database: %w", err)
	}

	mode, err := hooks.queryJournalMode(ctx, db)
	if err != nil {
		return fmt.Errorf("enable wal mode: %w", err)
	}
	if strings.ToLower(mode) != "wal" {
		return fmt.Errorf("unexpected journal mode %q", mode)
	}

	if _, err := db.ExecContext(ctx, `PRAGMA synchronous = NORMAL;`); err != nil {
		return fmt.Errorf("set synchronous mode: %w", err)
	}

	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 5000;`); err != nil {
		return fmt.Errorf("set busy timeout: %w", err)
	}

	return nil
}

func ensureMigrationTable(ctx context.Context, db *sql.DB) error {
	query := `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL
);
`
	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}
	return nil
}

func isMigrationApplied(ctx context.Context, db *sql.DB, version string) (bool, error) {
	if strings.TrimSpace(version) == "" {
		return false, errors.New("migration version is required")
	}

	var exists int
	if err := db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?);`,
		version,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("query migration %s: %w", version, err)
	}

	return exists == 1, nil
}
