package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestOpenConfiguresForeignKeysAndWAL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
	})

	var foreignKeys int
	if err := connection.DB().QueryRow(`PRAGMA foreign_keys;`).Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys pragma failed: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", foreignKeys)
	}

	var journalMode string
	if err := connection.DB().QueryRow(`PRAGMA journal_mode;`).Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode pragma failed: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", journalMode)
	}

	var synchronous int
	if err := connection.DB().QueryRow(`PRAGMA synchronous;`).Scan(&synchronous); err != nil {
		t.Fatalf("query synchronous pragma failed: %v", err)
	}
	if synchronous != 1 {
		t.Fatalf("expected synchronous=NORMAL(1), got %d", synchronous)
	}

	var busyTimeout int
	if err := connection.DB().QueryRow(`PRAGMA busy_timeout;`).Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout pragma failed: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("expected busy_timeout=5000, got %d", busyTimeout)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestOpenFailsWhenWALCannotBeEnabled(t *testing.T) {
	_, err := Open(":memory:")
	if err == nil {
		t.Fatal("expected Open(:memory:) to fail because WAL mode is unavailable")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "journal mode") {
		t.Fatalf("expected journal mode context in error, got %v", err)
	}
}

func TestApplyMigrationsIsIdempotentAndCreatesExpectedSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
	})

	if err := connection.ApplySchema(context.Background()); err != nil {
		t.Fatalf("first ApplySchema() error = %v", err)
	}
	if err := connection.ApplySchema(context.Background()); err != nil {
		t.Fatalf("second ApplySchema() error = %v", err)
	}

	assertTableExists(t, connection.DB(), "records")
	assertTableExists(t, connection.DB(), "record_figures")
	assertTableExists(t, connection.DB(), "record_data_files")
	assertTableExists(t, connection.DB(), "templates")
	assertTableExists(t, connection.DB(), "sync_version")
	assertTableExists(t, connection.DB(), "schema_migrations")

	assertUniqueIndexOnRecordAndFilename(t, connection.DB(), "record_figures")
	assertUniqueIndexOnRecordAndFilename(t, connection.DB(), "record_data_files")

	assertTriggerExists(t, connection.DB(), "records_sync_bump_after_insert")
	assertTriggerExists(t, connection.DB(), "templates_auto_update_updated_at")

	var syncVersionCount int
	if err := connection.DB().QueryRow(`SELECT COUNT(*) FROM sync_version WHERE id = 1`).Scan(&syncVersionCount); err != nil {
		t.Fatalf("query sync_version count failed: %v", err)
	}
	if syncVersionCount != 1 {
		t.Fatalf("expected one sync_version singleton row, got %d", syncVersionCount)
	}

	var migrationCount int
	if err := connection.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("query schema_migrations failed: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected one applied migration, got %d", migrationCount)
	}

}

func TestApplyMigrationsRejectsInvalidInput(t *testing.T) {
	if err := ApplyMigrations(context.Background(), nil, "any"); err == nil {
		t.Fatal("expected error for nil db")
	}

	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	if err := ApplyMigrations(context.Background(), connection.DB(), ""); err == nil {
		t.Fatal("expected error for empty migration dir")
	}
	if err := ApplyMigrations(context.Background(), connection.DB(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error for missing migration dir")
	}
}

func TestConnectionApplyMigrationsRequiresLiveConnection(t *testing.T) {
	if err := (&Connection{}).ApplyMigrations(context.Background(), "any"); err == nil {
		t.Fatal("expected nil db connection to fail")
	}

	var nilConnection *Connection
	if err := nilConnection.ApplyMigrations(context.Background(), "any"); err == nil {
		t.Fatal("expected nil connection to fail")
	}
}

func TestApplyMigrationsFailsOnInvalidSQL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	migrationsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(migrationsDir, "001_bad.sql"), []byte("CREATE TABL broken;"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err = connection.ApplyMigrations(context.Background(), migrationsDir)
	if err == nil {
		t.Fatal("expected migration failure for invalid SQL")
	}
	if !strings.Contains(err.Error(), "apply migration") {
		t.Fatalf("expected apply migration error, got %v", err)
	}

	var appliedCount int
	if queryErr := connection.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&appliedCount); queryErr != nil {
		t.Fatalf("query schema_migrations failed: %v", queryErr)
	}
	if appliedCount != 0 {
		t.Fatalf("expected failed migration to leave no applied rows, got %d", appliedCount)
	}
}

func TestApplyMigrationsSkipsNonSQLFiles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	migrationsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(migrationsDir, "README.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatalf("WriteFile(readme) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(migrationsDir, "001_one.sql"), []byte(`CREATE TABLE demo(id INTEGER PRIMARY KEY);`), 0o644); err != nil {
		t.Fatalf("WriteFile(sql) error = %v", err)
	}

	if err := connection.ApplyMigrations(context.Background(), migrationsDir); err != nil {
		t.Fatalf("ApplyMigrations() error = %v", err)
	}

	var count int
	if err := connection.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations;`).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations count error = %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one applied SQL migration, got %d", count)
	}
}

func TestApplyMigrationsFailsWhenMigrationFileUnreadable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	migrationsDir := t.TempDir()
	sqlPath := filepath.Join(migrationsDir, "001_unreadable.sql")
	if err := os.WriteFile(sqlPath, []byte(`CREATE TABLE nope(id INTEGER);`), 0o000); err != nil {
		t.Fatalf("WriteFile(unreadable) error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sqlPath, 0o644) })

	err = connection.ApplyMigrations(context.Background(), migrationsDir)
	if err == nil {
		t.Fatal("expected ApplyMigrations() to fail for unreadable migration file")
	}
	if !strings.Contains(err.Error(), "read migration") {
		t.Fatalf("expected read migration error, got %v", err)
	}
}

func TestApplyMigrationsFailsWhenMigrationFileCannotBeRead(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	migrationsDir := t.TempDir()
	// Broken symlink guarantees os.ReadFile failure without permission-specific behavior.
	brokenLinkPath := filepath.Join(migrationsDir, "001_broken.sql")
	if err := os.Symlink(filepath.Join(migrationsDir, "does-not-exist.sql"), brokenLinkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	err = connection.ApplyMigrations(context.Background(), migrationsDir)
	if err == nil {
		t.Fatal("expected ApplyMigrations() to fail for unreadable migration file")
	}
	if !strings.Contains(err.Error(), "read migration") {
		t.Fatalf("expected read migration error, got %v", err)
	}
}

func TestApplyMigrationsFailsWhenRecordingVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	migrationsDir := t.TempDir()
	migrationSQL := `
CREATE TABLE demo(id INTEGER PRIMARY KEY);
CREATE TRIGGER block_schema_migrations_insert
BEFORE INSERT ON schema_migrations
BEGIN
    SELECT RAISE(ABORT, 'blocked');
END;
`
	if err := os.WriteFile(filepath.Join(migrationsDir, "001_block_record.sql"), []byte(migrationSQL), 0o644); err != nil {
		t.Fatalf("WriteFile(migration) error = %v", err)
	}

	err = connection.ApplyMigrations(context.Background(), migrationsDir)
	if err == nil {
		t.Fatal("expected ApplyMigrations() to fail when migration record insert is blocked")
	}
	if !strings.Contains(err.Error(), "record migration") {
		t.Fatalf("expected record migration context, got %v", err)
	}

	var count int
	if queryErr := connection.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations;`).Scan(&count); queryErr != nil {
		t.Fatalf("query schema_migrations count error = %v", queryErr)
	}
	if count != 0 {
		t.Fatalf("expected zero applied migrations after rollback, got %d", count)
	}
}

func TestMigrationTriggersUpdateUpdatedAtAndSyncVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	if err := connection.ApplySchema(context.Background()); err != nil {
		t.Fatalf("ApplySchema() error = %v", err)
	}

	if _, err := connection.DB().Exec(`INSERT INTO projects(id) VALUES(?);`, "sqlite/project"); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := connection.DB().Exec(`INSERT INTO devices(id) VALUES(?);`, "sqlite-device"); err != nil {
		t.Fatalf("insert device: %v", err)
	}
	if _, err := connection.DB().Exec(`INSERT INTO records(id, date, day_order, html_content, project_id, source_device_id) VALUES(?, ?, ?, ?, ?, ?);`, "20260305-abcddcba", "2026-03-05", "n", "<h1>a</h1>", "sqlite/project", "sqlite-device"); err != nil {
		t.Fatalf("insert record: %v", err)
	}

	var beforeUpdatedAt string
	if err := connection.DB().QueryRow(`SELECT updated_at FROM records WHERE id = ?;`, "20260305-abcddcba").Scan(&beforeUpdatedAt); err != nil {
		t.Fatalf("select updated_at before: %v", err)
	}
	var beforeVersion int64
	if err := connection.DB().QueryRow(`SELECT version FROM sync_version WHERE id = 1;`).Scan(&beforeVersion); err != nil {
		t.Fatalf("select version before: %v", err)
	}

	// Backdate updated_at to ensure the trigger produces a distinguishable timestamp.
	pastTime := time.Now().UTC().Add(-time.Hour).Format("2006-01-02T15:04:05.000Z")
	if _, err := connection.DB().Exec(`UPDATE records SET updated_at = ? WHERE id = ?;`, pastTime, "20260305-abcddcba"); err != nil {
		t.Fatalf("backdate updated_at: %v", err)
	}
	// Re-read beforeUpdatedAt after backdating, since that's our new baseline.
	if err := connection.DB().QueryRow(`SELECT updated_at FROM records WHERE id = ?;`, "20260305-abcddcba").Scan(&beforeUpdatedAt); err != nil {
		t.Fatalf("select updated_at after backdate: %v", err)
	}
	if _, err := connection.DB().Exec(`UPDATE records SET html_content = ? WHERE id = ?;`, "<h1>b</h1>", "20260305-abcddcba"); err != nil {
		t.Fatalf("update record: %v", err)
	}

	var afterUpdatedAt string
	if err := connection.DB().QueryRow(`SELECT updated_at FROM records WHERE id = ?;`, "20260305-abcddcba").Scan(&afterUpdatedAt); err != nil {
		t.Fatalf("select updated_at after: %v", err)
	}
	if beforeUpdatedAt == afterUpdatedAt {
		t.Fatalf("expected updated_at to change after update: %q", afterUpdatedAt)
	}

	var afterVersion int64
	if err := connection.DB().QueryRow(`SELECT version FROM sync_version WHERE id = 1;`).Scan(&afterVersion); err != nil {
		t.Fatalf("select version after: %v", err)
	}
	if afterVersion != beforeVersion+1 {
		t.Fatalf("expected sync version to increment by exactly one: before=%d after=%d", beforeVersion, afterVersion)
	}
}

func TestMigrationEnforcesStrictIDAndHashConstraints(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	if err := connection.ApplySchema(context.Background()); err != nil {
		t.Fatalf("ApplySchema() error = %v", err)
	}
	if _, err := connection.DB().Exec(`INSERT INTO projects(id) VALUES(?);`, "sqlite/project"); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := connection.DB().Exec(`INSERT INTO devices(id) VALUES(?);`, "sqlite-device"); err != nil {
		t.Fatalf("insert device: %v", err)
	}

	if _, err := connection.DB().Exec(
		`INSERT INTO records(id, date, day_order, html_content, project_id, source_device_id) VALUES(?, ?, ?, ?, ?, ?);`,
		"20260305-zzzzzzzz",
		"2026-03-05",
		"n",
		"<h1>bad id</h1>",
		"sqlite/project",
		"sqlite-device",
	); err == nil {
		t.Fatal("expected invalid record id to violate CHECK constraint")
	}

	if _, err := connection.DB().Exec(
		`INSERT INTO records(id, date, day_order, html_content, project_id, source_device_id, git_hash) VALUES(?, ?, ?, ?, ?, ?, ?);`,
		"20260305-a1b2c3d4",
		"2026-03-05",
		"n",
		"<h1>bad git hash</h1>",
		"sqlite/project",
		"sqlite-device",
		"gggggggggggggggggggggggggggggggggggggggg",
	); err == nil {
		t.Fatal("expected invalid git_hash to violate CHECK constraint")
	}

	if _, err := connection.DB().Exec(
		`INSERT INTO records(id, date, day_order, html_content, project_id, source_device_id) VALUES(?, ?, ?, ?, ?, ?);`,
		"20260305-c1c2c3c4",
		"March 5",
		"n",
		"<h1>bad date</h1>",
		"sqlite/project",
		"sqlite-device",
	); err == nil {
		t.Fatal("expected non-YYYY-MM-DD date to violate CHECK constraint")
	}

	if _, err := connection.DB().Exec(
		`INSERT INTO records(id, date, day_order, html_content, project_id, source_device_id) VALUES(?, ?, ?, ?, ?, ?);`,
		"20260305-b1b2c3d4",
		"2026-03-05",
		"n",
		"<h1>good record</h1>",
		"sqlite/project",
		"sqlite-device",
	); err != nil {
		t.Fatalf("insert valid record failed: %v", err)
	}

	if _, err := connection.DB().Exec(
		`INSERT INTO record_data_files(record_id, filename, s3_key, size, hash) VALUES(?, ?, ?, ?, ?);`,
		"20260305-b1b2c3d4",
		"metrics.csv",
		"data/20260305-b1b2c3d4/metrics.csv",
		1,
		"fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffZ",
	); err == nil {
		t.Fatal("expected invalid data hash to violate CHECK constraint")
	}
}

func TestConnectionCloseHandlesNil(t *testing.T) {
	var connection *Connection
	if err := connection.Close(); err != nil {
		t.Fatalf("expected nil close to be safe, got %v", err)
	}
}

func TestApplyMigrationsRejectsEmptyVersionLookup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	if err := ensureMigrationTable(context.Background(), connection.DB()); err != nil {
		t.Fatalf("ensureMigrationTable() error = %v", err)
	}
	if _, err := isMigrationApplied(context.Background(), connection.DB(), ""); err == nil {
		t.Fatal("expected empty version lookup to fail")
	}
}

func TestEnsureMigrationTableAndVersionLookupFailOnClosedDB(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "closed-migrations.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close() error = %v", err)
	}

	if err := ensureMigrationTable(context.Background(), db); err == nil {
		t.Fatal("expected ensureMigrationTable() to fail on closed db")
	}

	if _, err := isMigrationApplied(context.Background(), db, "001.sql"); err == nil {
		t.Fatal("expected isMigrationApplied() to fail on closed db")
	}
}

func TestConfigureWithClosedDBFails(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if err := configure(context.Background(), db); err == nil {
		t.Fatal("expected configure to fail with closed db")
	}
}

func TestErrorContainsOpenFailureContext(t *testing.T) {
	_, err := Open(filepath.Join(string([]byte{0x00}), "bad.db"))
	if err != nil && !errors.Is(err, os.ErrInvalid) {
		// Assert only that non-empty contextual errors are returned.
		if !strings.Contains(err.Error(), "create sqlite parent directory") {
			t.Fatalf("unexpected open error: %v", err)
		}
	}
}

func TestOpenFailsWhenParentPathBlocked(t *testing.T) {
	root := t.TempDir()
	blockedParent := filepath.Join(root, "blocked")
	if err := os.WriteFile(blockedParent, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(blockedParent) error = %v", err)
	}

	_, err := Open(filepath.Join(blockedParent, "pc.db"))
	if err == nil {
		t.Fatal("expected Open to fail when parent path is blocked by file")
	}
}

func TestConnectionApplyMigrationsRejectsNilReceiver(t *testing.T) {
	var nilConnection *Connection
	if err := nilConnection.ApplyMigrations(context.Background(), "any"); err == nil {
		t.Fatal("expected nil connection receiver to fail")
	}

	connection := &Connection{}
	if err := connection.ApplyMigrations(context.Background(), "any"); err == nil {
		t.Fatal("expected nil db in connection to fail")
	}
}

func TestConfigureRejectsUnsupportedJournalMode(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	err = configure(context.Background(), db)
	if err == nil {
		t.Fatal("expected configure on in-memory sqlite to fail WAL check")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "journal mode") {
		t.Fatalf("expected journal mode error, got %v", err)
	}
}

func TestOpenPropagatesSQLOpenFailure(t *testing.T) {
	original := sqlOpenFn
	t.Cleanup(func() {
		sqlOpenFn = original
	})

	sqlOpenFn = func(driverName string, dataSourceName string) (*sql.DB, error) {
		return nil, errors.New("open boom")
	}

	_, err := Open(filepath.Join(t.TempDir(), "pc.db"))
	if err == nil {
		t.Fatal("expected Open() to fail when sqlOpenFn fails")
	}
	if !strings.Contains(err.Error(), "open sqlite database") {
		t.Fatalf("expected open sqlite context, got %v", err)
	}
}

func TestApplyMigrationsPropagatesEnsureMigrationTableFailure(t *testing.T) {
	original := ensureMigrationTableFn
	t.Cleanup(func() {
		ensureMigrationTableFn = original
	})

	ensureMigrationTableFn = func(ctx context.Context, db *sql.DB) error {
		return errors.New("ensure boom")
	}

	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	err = ApplyMigrations(context.Background(), connection.DB(), t.TempDir())
	if err == nil {
		t.Fatal("expected ApplyMigrations() to fail when ensureMigrationTableFn fails")
	}
	if !strings.Contains(err.Error(), "ensure boom") {
		t.Fatalf("expected ensure error to propagate, got %v", err)
	}
}

func TestApplyMigrationsPropagatesIsMigrationAppliedFailure(t *testing.T) {
	original := isMigrationAppliedFn
	t.Cleanup(func() {
		isMigrationAppliedFn = original
	})

	isMigrationAppliedFn = func(ctx context.Context, db *sql.DB, version string) (bool, error) {
		return false, fmt.Errorf("lookup boom")
	}

	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	migrationsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(migrationsDir, "001_ok.sql"), []byte(`CREATE TABLE demo(id INTEGER PRIMARY KEY);`), 0o644); err != nil {
		t.Fatalf("WriteFile(migration) error = %v", err)
	}

	err = ApplyMigrations(context.Background(), connection.DB(), migrationsDir)
	if err == nil {
		t.Fatal("expected ApplyMigrations() to fail when isMigrationAppliedFn fails")
	}
	if !strings.Contains(err.Error(), "lookup boom") {
		t.Fatalf("expected lookup error to propagate, got %v", err)
	}
}

func TestApplyMigrationsPropagatesReadFileFailure(t *testing.T) {
	original := readFileFn
	t.Cleanup(func() {
		readFileFn = original
	})

	readFileFn = func(path string) ([]byte, error) {
		return nil, errors.New("read boom")
	}

	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	migrationsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(migrationsDir, "001_ok.sql"), []byte(`CREATE TABLE demo(id INTEGER PRIMARY KEY);`), 0o644); err != nil {
		t.Fatalf("WriteFile(migration) error = %v", err)
	}

	err = ApplyMigrations(context.Background(), connection.DB(), migrationsDir)
	if err == nil {
		t.Fatal("expected ApplyMigrations() to fail when readFileFn fails")
	}
	if !strings.Contains(err.Error(), "read migration") || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected read migration context and read boom, got %v", err)
	}
}

func TestApplyMigrationsPropagatesBeginTxFailure(t *testing.T) {
	original := beginTxFn
	t.Cleanup(func() {
		beginTxFn = original
	})

	beginTxFn = func(db *sql.DB, ctx context.Context) (*sql.Tx, error) {
		return nil, errors.New("begin boom")
	}

	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	migrationsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(migrationsDir, "001_ok.sql"), []byte(`CREATE TABLE demo(id INTEGER PRIMARY KEY);`), 0o644); err != nil {
		t.Fatalf("WriteFile(migration) error = %v", err)
	}

	err = ApplyMigrations(context.Background(), connection.DB(), migrationsDir)
	if err == nil {
		t.Fatal("expected ApplyMigrations() to fail when beginTxFn fails")
	}
	if !strings.Contains(err.Error(), "begin migration transaction") || !strings.Contains(err.Error(), "begin boom") {
		t.Fatalf("expected begin migration transaction context and begin boom, got %v", err)
	}
}

func TestForeignKeysEnabledOnAllPoolConnections(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	connection, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	db := connection.DB()

	// Hold one connection open via rows.
	rows, err := db.Query("SELECT 1")
	if err != nil {
		t.Fatalf("first query error = %v", err)
	}
	defer func() { _ = rows.Close() }()

	// Force a second connection from the pool and verify FK pragma.
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys on second conn error = %v", err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1 on second pool connection, got %d", fk)
	}
}

func TestConfigurePropagatesJournalModeQueryFailure(t *testing.T) {
	originalQuery := queryJournalModeFn
	t.Cleanup(func() {
		queryJournalModeFn = originalQuery
	})

	queryJournalModeFn = func(ctx context.Context, db *sql.DB) (string, error) {
		return "", errors.New("journal query boom")
	}

	dbPath := filepath.Join(t.TempDir(), "pc.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := configure(context.Background(), db); err == nil {
		t.Fatal("expected configure() to fail when journal mode query fails")
	} else if !strings.Contains(err.Error(), "enable wal mode") {
		t.Fatalf("expected wal mode context, got %v", err)
	}
}

func assertTableExists(t *testing.T, db *sql.DB, name string) {
	t.Helper()

	var exists int
	err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`,
		name,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("table existence query for %q failed: %v", name, err)
	}
	if exists != 1 {
		t.Fatalf("expected table %q to exist", name)
	}
}

func assertTriggerExists(t *testing.T, db *sql.DB, name string) {
	t.Helper()

	var exists int
	err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'trigger' AND name = ?)`,
		name,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("trigger existence query for %q failed: %v", name, err)
	}
	if exists != 1 {
		t.Fatalf("expected trigger %q to exist", name)
	}
}

func TestApplyMigrationsFromFSSuccess(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	conn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	fsys := fstest.MapFS{
		"001_demo.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE demo(id INTEGER PRIMARY KEY);`)},
	}

	if err := conn.ApplyMigrationsFS(context.Background(), fsys); err != nil {
		t.Fatalf("ApplyMigrationsFS() error = %v", err)
	}

	assertTableExists(t, conn.DB(), "demo")

	var count int
	if err := conn.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migration, got %d", count)
	}
}

func TestApplyMigrationsFromFSIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	conn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	fsys := fstest.MapFS{
		"001_demo.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE demo(id INTEGER PRIMARY KEY);`)},
	}

	if err := conn.ApplyMigrationsFS(context.Background(), fsys); err != nil {
		t.Fatalf("first ApplyMigrationsFS() error = %v", err)
	}
	if err := conn.ApplyMigrationsFS(context.Background(), fsys); err != nil {
		t.Fatalf("second ApplyMigrationsFS() error = %v", err)
	}
}

func TestApplyMigrationsFromFSRejectsNilDB(t *testing.T) {
	fsys := fstest.MapFS{}
	if err := ApplyMigrationsFromFS(context.Background(), nil, fsys); err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestApplyMigrationsFromFSRejectsNilFS(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	conn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := ApplyMigrationsFromFS(context.Background(), conn.DB(), nil); err == nil {
		t.Fatal("expected error for nil fs")
	}
}

func TestConnectionApplyMigrationsFSRejectsNilReceiver(t *testing.T) {
	var nilConn *Connection
	if err := nilConn.ApplyMigrationsFS(context.Background(), fstest.MapFS{}); err == nil {
		t.Fatal("expected error for nil connection")
	}

	conn := &Connection{}
	if err := conn.ApplyMigrationsFS(context.Background(), fstest.MapFS{}); err == nil {
		t.Fatal("expected error for nil db in connection")
	}
}

func TestApplyMigrationsFromFSSkipsNonSQL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	conn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	fsys := fstest.MapFS{
		"README.txt":   &fstest.MapFile{Data: []byte("ignore")},
		"001_demo.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE demo(id INTEGER PRIMARY KEY);`)},
	}

	if err := conn.ApplyMigrationsFS(context.Background(), fsys); err != nil {
		t.Fatalf("ApplyMigrationsFS() error = %v", err)
	}

	var count int
	if err := conn.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migration (non-SQL skipped), got %d", count)
	}
}

func TestSQLMigrationFilenamesFiltersAndSorts(t *testing.T) {
	fsys := fstest.MapFS{
		"003_third.sql":  &fstest.MapFile{Data: []byte("SELECT 3;")},
		"README.txt":     &fstest.MapFile{Data: []byte("ignore")},
		"001_first.sql":  &fstest.MapFile{Data: []byte("SELECT 1;")},
		"002_second.sql": &fstest.MapFile{Data: []byte("SELECT 2;")},
		"nested":         &fstest.MapFile{Mode: fs.ModeDir},
	}

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatalf("ReadDir(.) error = %v", err)
	}

	got := sqlMigrationFilenames(entries)
	want := []string{"001_first.sql", "002_second.sql", "003_third.sql"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sqlMigrationFilenames() = %v, want %v", got, want)
	}
}

func TestApplyMigrationsFromFSFailsOnInvalidSQL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	conn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	fsys := fstest.MapFS{
		"001_bad.sql": &fstest.MapFile{Data: []byte("CREATE TABL broken;")},
	}

	if err := conn.ApplyMigrationsFS(context.Background(), fsys); err == nil {
		t.Fatal("expected error for invalid SQL")
	}
}

func TestApplyMigrationsFromFSPropagatesBeginTxFailure(t *testing.T) {
	original := beginTxFn
	t.Cleanup(func() { beginTxFn = original })

	beginTxFn = func(db *sql.DB, ctx context.Context) (*sql.Tx, error) {
		return nil, errors.New("begin fs boom")
	}

	dbPath := filepath.Join(t.TempDir(), "pc.db")
	conn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	fsys := fstest.MapFS{
		"001_demo.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE demo(id INTEGER PRIMARY KEY);`)},
	}

	err = ApplyMigrationsFromFS(context.Background(), conn.DB(), fsys)
	if err == nil {
		t.Fatal("expected error when beginTxFn fails")
	}
	if !strings.Contains(err.Error(), "begin migration transaction") {
		t.Fatalf("expected begin migration error, got %v", err)
	}
}

func TestApplyMigrationsFromFSPropagatesRecordFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	conn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Migration that creates a trigger blocking schema_migrations inserts
	fsys := fstest.MapFS{
		"001_block.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE demo(id INTEGER PRIMARY KEY);
CREATE TRIGGER block_schema_migrations_insert_fs
BEFORE INSERT ON schema_migrations
BEGIN
    SELECT RAISE(ABORT, 'blocked');
END;
`)},
	}

	err = ApplyMigrationsFromFS(context.Background(), conn.DB(), fsys)
	if err == nil {
		t.Fatal("expected error when record migration is blocked")
	}
	if !strings.Contains(err.Error(), "record migration") {
		t.Fatalf("expected record migration error, got %v", err)
	}
}

func TestApplyMigrationsFromFSPropagatesEnsureTableFailure(t *testing.T) {
	original := ensureMigrationTableFn
	t.Cleanup(func() { ensureMigrationTableFn = original })

	ensureMigrationTableFn = func(ctx context.Context, db *sql.DB) error {
		return errors.New("ensure fs boom")
	}

	dbPath := filepath.Join(t.TempDir(), "pc.db")
	conn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	fsys := fstest.MapFS{}
	err = ApplyMigrationsFromFS(context.Background(), conn.DB(), fsys)
	if err == nil {
		t.Fatal("expected error when ensureMigrationTableFn fails")
	}
}

func TestApplyMigrationsFromFSPropagatesLookupFailure(t *testing.T) {
	original := isMigrationAppliedFn
	t.Cleanup(func() { isMigrationAppliedFn = original })

	isMigrationAppliedFn = func(ctx context.Context, db *sql.DB, version string) (bool, error) {
		return false, errors.New("lookup fs boom")
	}

	dbPath := filepath.Join(t.TempDir(), "pc.db")
	conn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	fsys := fstest.MapFS{
		"001_demo.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE demo(id INTEGER PRIMARY KEY);`)},
	}

	err = ApplyMigrationsFromFS(context.Background(), conn.DB(), fsys)
	if err == nil {
		t.Fatal("expected error when isMigrationAppliedFn fails")
	}
}

func TestSchemaSQL(t *testing.T) {
	content := SchemaSQL()
	if len(content) == 0 {
		t.Fatal("SchemaSQL() returned empty content")
	}
	if !strings.Contains(string(content), "CREATE TABLE IF NOT EXISTS records") {
		t.Fatal("SchemaSQL() missing records table definition")
	}
}

func TestSchemaFS(t *testing.T) {
	fsys, err := SchemaFS()
	if err != nil {
		t.Fatalf("SchemaFS() error = %v", err)
	}
	if fsys == nil {
		t.Fatal("SchemaFS() returned nil filesystem")
	}
}

func TestApplySchemaIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pc.db")
	conn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.ApplySchema(context.Background()); err != nil {
		t.Fatalf("first ApplySchema() error = %v", err)
	}
	if err := conn.ApplySchema(context.Background()); err != nil {
		t.Fatalf("second ApplySchema() error = %v", err)
	}

	assertTableExists(t, conn.DB(), "records")
	assertTableExists(t, conn.DB(), "record_figures")
	assertTableExists(t, conn.DB(), "record_data_files")
	assertTableExists(t, conn.DB(), "templates")
	assertTableExists(t, conn.DB(), "sync_version")
	assertTableExists(t, conn.DB(), "schema_migrations")

	var count int
	if err := conn.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 migration record, got %d", count)
	}

	var version string
	if err := conn.DB().QueryRow("SELECT version FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("query version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("expected version %q, got %q", schemaVersion, version)
	}
}

func TestSingleFileFSOpen(t *testing.T) {
	sfs := &singleFileFS{name: "001_schema.sql", data: []byte("CREATE TABLE t(id INTEGER);")}

	t.Run("open root directory", func(t *testing.T) {
		f, err := sfs.Open(".")
		if err != nil {
			t.Fatalf("Open(.) error = %v", err)
		}
		defer func() { _ = f.Close() }()

		info, err := f.Stat()
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		if !info.IsDir() {
			t.Fatal("expected root to be a directory")
		}
		if info.Name() != "." {
			t.Fatalf("expected name '.', got %q", info.Name())
		}
	})

	t.Run("open file by name", func(t *testing.T) {
		f, err := sfs.Open("001_schema.sql")
		if err != nil {
			t.Fatalf("Open(file) error = %v", err)
		}
		defer func() { _ = f.Close() }()

		info, err := f.Stat()
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		if info.IsDir() {
			t.Fatal("expected file, not directory")
		}
		if info.Name() != "001_schema.sql" {
			t.Fatalf("expected name '001_schema.sql', got %q", info.Name())
		}
		if info.Size() != int64(len(sfs.data)) {
			t.Fatalf("expected size %d, got %d", len(sfs.data), info.Size())
		}
		if info.Mode() != 0o444 {
			t.Fatalf("expected mode 0444, got %v", info.Mode())
		}
		if info.Sys() != nil {
			t.Fatal("expected Sys() to be nil")
		}

		buf := make([]byte, 100)
		n, err := f.Read(buf)
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if string(buf[:n]) != string(sfs.data) {
			t.Fatalf("read content mismatch: got %q", buf[:n])
		}
	})

	t.Run("open nonexistent file", func(t *testing.T) {
		_, err := sfs.Open("missing.sql")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("open invalid path", func(t *testing.T) {
		_, err := sfs.Open("../escape")
		if err == nil {
			t.Fatal("expected error for invalid path")
		}
	})

	t.Run("read on directory returns error", func(t *testing.T) {
		f, err := sfs.Open(".")
		if err != nil {
			t.Fatalf("Open(.) error = %v", err)
		}
		defer func() { _ = f.Close() }()

		buf := make([]byte, 10)
		if _, err := f.Read(buf); err == nil {
			t.Fatal("expected error reading from directory")
		}
	})
}

func TestSingleFileFSReadFile(t *testing.T) {
	content := []byte("SELECT 1;")
	sfs := &singleFileFS{name: "test.sql", data: content}

	t.Run("read existing file", func(t *testing.T) {
		got, err := sfs.ReadFile("test.sql")
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		if string(got) != string(content) {
			t.Fatalf("content mismatch: got %q", got)
		}
		// Verify it's a copy, not a reference
		got[0] = 'X'
		if sfs.data[0] == 'X' {
			t.Fatal("ReadFile returned a reference instead of a copy")
		}
	})

	t.Run("read missing file", func(t *testing.T) {
		_, err := sfs.ReadFile("missing.sql")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})
}

func TestSingleFileFSReadDir(t *testing.T) {
	sfs := &singleFileFS{name: "001.sql", data: []byte("data")}

	t.Run("read root directory", func(t *testing.T) {
		entries, err := sfs.ReadDir(".")
		if err != nil {
			t.Fatalf("ReadDir(.) error = %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}
		e := entries[0]
		if e.Name() != "001.sql" {
			t.Fatalf("expected name '001.sql', got %q", e.Name())
		}
		if e.IsDir() {
			t.Fatal("expected file entry, not directory")
		}
		if e.Type() != 0 {
			t.Fatalf("expected type 0, got %v", e.Type())
		}
		info, err := e.Info()
		if err != nil {
			t.Fatalf("Info() error = %v", err)
		}
		if info.Size() != int64(len(sfs.data)) {
			t.Fatalf("expected size %d, got %d", len(sfs.data), info.Size())
		}
	})

	t.Run("read non-root directory", func(t *testing.T) {
		_, err := sfs.ReadDir("subdir")
		if err == nil {
			t.Fatal("expected error for non-root directory")
		}
	})
}

func TestSingleDirReadDirPagination(t *testing.T) {
	d := &singleDir{name: "test.sql", data: []byte("data")}

	// First call with n > 0 should return entries + io.EOF
	entries, err := d.ReadDir(1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}

	// Subsequent call should return empty + io.EOF
	entries, err = d.ReadDir(1)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries on second call, got %d", len(entries))
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF on second call, got %v", err)
	}

	// Reset and call with n <= 0 (return all at once, no EOF)
	d2 := &singleDir{name: "test.sql", data: []byte("data")}
	entries, err = d2.ReadDir(-1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry with n=-1, got %d", len(entries))
	}
	if err != nil {
		t.Fatalf("expected nil error with n=-1, got %v", err)
	}

	// After exhausting, n <= 0 returns nil, nil
	entries, err = d2.ReadDir(0)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after exhaustion, got %d", len(entries))
	}
	if err != nil {
		t.Fatalf("expected nil error after exhaustion with n<=0, got %v", err)
	}
}

func TestApplySchemaRejectsNilConnection(t *testing.T) {
	var nilConn *Connection
	if err := nilConn.ApplySchema(context.Background()); err == nil {
		t.Fatal("expected error for nil connection")
	}

	conn := &Connection{}
	if err := conn.ApplySchema(context.Background()); err == nil {
		t.Fatal("expected error for nil db in connection")
	}
}

func assertUniqueIndexOnRecordAndFilename(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()

	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ?`, tableName)
	if err != nil {
		t.Fatalf("index list query for %q failed: %v", tableName, err)
	}
	defer func() { _ = rows.Close() }()

	indexNames := make([]string, 0)
	for rows.Next() {
		var indexName string
		if scanErr := rows.Scan(&indexName); scanErr != nil {
			t.Fatalf("index row scan failed: %v", scanErr)
		}
		indexNames = append(indexNames, indexName)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index rows iteration failed: %v", err)
	}
	if len(indexNames) == 0 {
		t.Fatalf("expected at least one index for %q", tableName)
	}

	sort.Strings(indexNames)
	for _, indexName := range indexNames {
		infoRows, infoErr := db.Query(`PRAGMA index_info(` + indexName + `);`)
		if infoErr != nil {
			continue
		}

		columns := make([]string, 0)
		for infoRows.Next() {
			var seqNo int
			var cid int
			var name string
			if scanErr := infoRows.Scan(&seqNo, &cid, &name); scanErr != nil {
				_ = infoRows.Close()
				t.Fatalf("index_info scan failed: %v", scanErr)
			}
			columns = append(columns, name)
		}
		_ = infoRows.Close()
		if len(columns) == 2 && columns[0] == "record_id" && columns[1] == "filename" {
			return
		}
	}

	t.Fatalf("expected unique constraint index on (record_id, filename) for table %q", tableName)
}
