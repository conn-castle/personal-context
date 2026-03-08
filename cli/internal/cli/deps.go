package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/config"
	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	sqliterepo "github.com/conn-castle/personal-context/cli/internal/repository/sqlite"
	"github.com/conn-castle/personal-context/cli/internal/sqlite"
)

const pcHomeEnvVar = "PC_HOME"

// localStack holds all dependencies needed by local CLI commands.
type localStack struct {
	Config     config.Config
	Store      config.Store
	Repo       repository.Repository
	FS         *filesystem.Client
	connection *sqlite.Connection
}

var (
	userHomeDirFn         = os.UserHomeDir
	newConfigStoreFn      = config.NewStore
	openSQLiteFn          = sqlite.Open
	sqliteMigrationsFSFn  = sqlite.SchemaFS
	newSQLiteRepoFn       = func(db *sql.DB) (repository.Repository, error) { return sqliterepo.New(db) }
	newFilesystemClientFn = filesystem.NewClient
)

// Close releases all resources held by the local stack.
func (s *localStack) Close() error {
	if s.connection != nil {
		return s.connection.Close()
	}
	return nil
}

// resolveHomeDirFn is the function used by all commands to resolve the home directory.
// It is a variable to allow test-only overrides.
var resolveHomeDirFn = defaultResolveHomeDir

// resolveHomeDir returns the effective home directory via the current resolveHomeDirFn.
func resolveHomeDir() (string, error) {
	return resolveHomeDirFn()
}

// resolveUserHomeDir returns the OS user home directory without honoring PC_HOME.
func resolveUserHomeDir() (string, error) {
	home, err := userHomeDirFn()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return home, nil
}

// defaultResolveHomeDir checks PC_HOME env var first, then falls back to os.UserHomeDir.
func defaultResolveHomeDir() (string, error) {
	if home := os.Getenv(pcHomeEnvVar); strings.TrimSpace(home) != "" {
		return home, nil
	}
	home, err := userHomeDirFn()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

// basePath returns the personal-context data directory.
func basePath(homeDir string) string {
	return filepath.Join(homeDir, "personal-context")
}

// dbPath returns the SQLite database path.
func dbPath(homeDir string) string {
	return filepath.Join(basePath(homeDir), ".pc", "pc.db")
}

// openLocalStack initializes all local dependencies: config, SQLite, repository, filesystem.
// The caller must call Close() when done.
func openLocalStack(homeDir string) (*localStack, error) {
	store, err := newConfigStoreFn(homeDir)
	if err != nil {
		return nil, fmt.Errorf("create config store: %w", err)
	}

	cfg, err := store.Read()
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	conn, err := openSQLiteFn(dbPath(homeDir))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	migrationsFS, err := sqliteMigrationsFSFn()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("load migrations: %w", err)
	}

	if err := conn.ApplyMigrationsFS(context.Background(), migrationsFS); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	repo, err := newSQLiteRepoFn(conn.DB())
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("create repository: %w", err)
	}

	fsClient, err := newFilesystemClientFn(basePath(homeDir))
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("create filesystem client: %w", err)
	}

	return &localStack{
		Config:     cfg,
		Store:      store,
		Repo:       repo,
		FS:         fsClient,
		connection: conn,
	}, nil
}
