package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sqlitedriver "modernc.org/sqlite"
)

// migrationTimestampFormat matches SQLite's strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
// output: always 3 fractional-second digits (millisecond precision).
const migrationTimestampFormat = "2006-01-02T15:04:05.000Z"

func init() {
	sqlitedriver.RegisterConnectionHook(func(conn sqlitedriver.ExecQuerierContext, dsn string) error {
		_, err := conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON", nil)
		return err
	})
}

var (
	sqlOpenFn = sql.Open

	ensureMigrationTableFn = ensureMigrationTable
	isMigrationAppliedFn   = isMigrationApplied
	readFileFn             = os.ReadFile
	beginTxFn              = func(db *sql.DB, ctx context.Context) (*sql.Tx, error) {
		return db.BeginTx(ctx, nil)
	}

	queryJournalModeFn = func(ctx context.Context, db *sql.DB) (string, error) {
		var mode string
		err := db.QueryRowContext(ctx, `PRAGMA journal_mode = WAL;`).Scan(&mode)
		return mode, err
	}
)

// Connection wraps an SQLite database handle configured for Personal Context.
type Connection struct {
	db *sql.DB
}

// Open creates a configured SQLite connection wrapper.
// Args: path is the SQLite database file path.
// Returns: a connection wrapper with WAL and foreign key enforcement enabled.
func Open(path string) (*Connection, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite parent directory: %w", err)
	}

	db, err := sqlOpenFn("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if err := configure(context.Background(), db); err != nil {
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
	if db == nil {
		return fmt.Errorf("db is required")
	}
	if strings.TrimSpace(migrationsDir) == "" {
		return fmt.Errorf("migrations directory is required")
	}

	if err := ensureMigrationTableFn(ctx, db); err != nil {
		return err
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations directory: %w", err)
	}

	migrations := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		migrations = append(migrations, entry.Name())
	}
	sort.Strings(migrations)

	for _, migration := range migrations {
		applied, err := isMigrationAppliedFn(ctx, db, migration)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		content, err := readFileFn(filepath.Join(migrationsDir, migration))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", migration, err)
		}

		tx, err := beginTxFn(db, ctx)
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
			time.Now().UTC().Format(migrationTimestampFormat),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", migration, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", migration, err)
		}
	}

	return nil
}

func configure(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite database: %w", err)
	}

	mode, err := queryJournalModeFn(ctx, db)
	if err != nil {
		return fmt.Errorf("enable wal mode: %w", err)
	}
	if strings.ToLower(mode) != "wal" {
		return fmt.Errorf("unexpected journal mode %q", mode)
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
