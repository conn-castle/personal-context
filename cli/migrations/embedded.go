package migrations

import (
	"embed"
	"io/fs"
)

//go:embed sqlite/*.sql
var sqliteFS embed.FS

// SQLiteFS returns an fs.FS rooted at the sqlite migrations directory.
// Args: none.
// Returns: filesystem containing .sql migration files.
func SQLiteFS() (fs.FS, error) {
	return fs.Sub(sqliteFS, "sqlite")
}
