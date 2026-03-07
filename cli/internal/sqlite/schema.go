package sqlite

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"time"
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

// schemaFS returns an fs.FS containing the canonical schema as a single migration file.
func schemaFS() fs.FS {
	return &singleFileFS{name: schemaVersion, data: schemaContent}
}

// singleFileFS is a minimal read-only fs.FS that presents a single in-memory
// file. It replaces testing/fstest.MapFS so production code does not import
// the testing package.
type singleFileFS struct {
	name string
	data []byte
}

// compile-time interface checks
var (
	_ fs.FS         = (*singleFileFS)(nil)
	_ fs.ReadFileFS = (*singleFileFS)(nil)
	_ fs.ReadDirFS  = (*singleFileFS)(nil)
)

// Open implements fs.FS. It supports opening the root directory (".") and the
// single embedded file by name.
// Args: name is the path to open.
// Returns: an fs.File for the requested path, or an error.
func (f *singleFileFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		return &singleDir{name: f.name, data: f.data}, nil
	}
	if name == f.name {
		return &memFile{name: f.name, reader: bytes.NewReader(f.data), size: len(f.data)}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadFile implements fs.ReadFileFS.
// Args: name is the file to read.
// Returns: the file contents or an error.
func (f *singleFileFS) ReadFile(name string) ([]byte, error) {
	if name == f.name {
		cp := make([]byte, len(f.data))
		copy(cp, f.data)
		return cp, nil
	}
	return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
}

// ReadDir implements fs.ReadDirFS.
// Args: name is the directory to list (only "." is valid).
// Returns: directory entries or an error.
func (f *singleFileFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "." {
		return []fs.DirEntry{&memDirEntry{name: f.name, size: len(f.data)}}, nil
	}
	return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
}

// memFile is an in-memory fs.File backed by a bytes.Reader.
type memFile struct {
	name   string
	reader *bytes.Reader
	size   int
}

func (f *memFile) Stat() (fs.FileInfo, error) {
	return &memFileInfo{name: f.name, size: int64(f.size)}, nil
}
func (f *memFile) Read(b []byte) (int, error) { return f.reader.Read(b) }
func (f *memFile) Close() error               { return nil }

// singleDir is an fs.File representing the root directory of a singleFileFS.
type singleDir struct {
	name    string
	data    []byte
	readPos int // 0 = not yet read, 1 = already read
}

func (d *singleDir) Stat() (fs.FileInfo, error) {
	return &memFileInfo{name: ".", isDir: true}, nil
}
func (d *singleDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: fmt.Errorf("is a directory")}
}
func (d *singleDir) Close() error { return nil }

// ReadDir implements fs.ReadDirFile so that fs.ReadDir works via Open(".").
func (d *singleDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.readPos > 0 {
		if n <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}
	d.readPos = 1
	entries := []fs.DirEntry{&memDirEntry{name: d.name, size: len(d.data)}}
	if n > 0 {
		return entries, io.EOF
	}
	return entries, nil
}

// memFileInfo is a minimal fs.FileInfo for in-memory files.
type memFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (fi *memFileInfo) Name() string      { return fi.name }
func (fi *memFileInfo) Size() int64       { return fi.size }
func (fi *memFileInfo) Mode() fs.FileMode { return 0o444 }
func (fi *memFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *memFileInfo) IsDir() bool       { return fi.isDir }
func (fi *memFileInfo) Sys() any          { return nil }

// memDirEntry is a minimal fs.DirEntry for in-memory files.
type memDirEntry struct {
	name string
	size int
}

func (e *memDirEntry) Name() string               { return e.name }
func (e *memDirEntry) IsDir() bool                { return false }
func (e *memDirEntry) Type() fs.FileMode          { return 0 }
func (e *memDirEntry) Info() (fs.FileInfo, error) {
	return &memFileInfo{name: e.name, size: int64(e.size)}, nil
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
