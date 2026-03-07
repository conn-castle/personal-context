package sqlite

import (
	"context"
	_ "embed"
	"fmt"
	"io/fs"
	"testing/fstest"
)

// schemaVersion is the migration version recorded in schema_migrations when
// the canonical SQLite schema is applied. It uses the same naming convention
// as the former individual migration files so that databases created before
// consolidation remain compatible.
const schemaVersion = "001_initial.sql"

//go:embed sqlite_schema.sql
var schemaContent []byte

// SchemaSQL returns the canonical SQLite schema as a byte slice.
// The content is compiled into the binary via go:embed and is always available.
// Args: none.
// Returns: raw SQL bytes of the embedded schema.
func SchemaSQL() []byte {
	return schemaContent
}

// SchemaFS returns an fs.FS containing the canonical SQLite schema as a single
// migration file. The file is named with the standard migration version so
// that the migration runner applies and records it exactly once.
// Args: none.
// Returns: filesystem containing the schema as a single .sql migration file.
func SchemaFS() (fs.FS, error) {
	return schemaFS(), nil
}

// schemaFS returns a MapFS containing the canonical schema as a single migration file.
func schemaFS() fs.FS {
	return fstest.MapFS{
		schemaVersion: &fstest.MapFile{Data: schemaContent},
	}
}

// ApplySchema applies the canonical SQLite schema to the database.
// It uses the migration runner internally so that the schema is recorded in
// schema_migrations and applied at most once.
// Args: ctx controls cancellation.
// Returns: nil when the schema has been applied (or was already applied).
func (c *Connection) ApplySchema(ctx context.Context) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("connection is required")
	}

	return ApplyMigrationsFromFS(ctx, c.db, schemaFS())
}
