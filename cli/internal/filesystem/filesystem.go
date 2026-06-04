package filesystem

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type fileOperationHooks struct {
	syncFile   func(*os.File) error
	closeFile  func(*os.File) error
	renameFile func(string, string) error
}

func defaultFileOperationHooks() fileOperationHooks {
	return fileOperationHooks{
		syncFile:   func(f *os.File) error { return f.Sync() },
		closeFile:  func(f *os.File) error { return f.Close() },
		renameFile: os.Rename,
	}
}

func (h fileOperationHooks) withDefaults() fileOperationHooks {
	defaults := defaultFileOperationHooks()
	if h.syncFile == nil {
		h.syncFile = defaults.syncFile
	}
	if h.closeFile == nil {
		h.closeFile = defaults.closeFile
	}
	if h.renameFile == nil {
		h.renameFile = defaults.renameFile
	}
	return h
}

// StoredFile describes a copied asset on disk.
type StoredFile struct {
	Filename string
	S3Key    string
	Path     string
	Size     int64
}

// Client manages local figure/data file storage rooted at basePath.
type Client struct {
	basePath string
	hooks    fileOperationHooks
}

// NewClient creates a filesystem client.
// Args: basePath is the root local data directory (e.g., ~/personal-context).
// Returns: a configured client or error when basePath is empty.
func NewClient(basePath string) (*Client, error) {
	return newClientWithHooks(basePath, defaultFileOperationHooks())
}

func newClientWithHooks(basePath string, hooks fileOperationHooks) (*Client, error) {
	if strings.TrimSpace(basePath) == "" {
		return nil, fmt.Errorf("base path is required")
	}
	return &Client{basePath: basePath, hooks: hooks.withDefaults()}, nil
}

// ResolveFigurePath returns the absolute path for a figure file.
// Args: recordID identifies the record; filename identifies the file.
// Returns: absolute path under {base}/figures/{recordID}/{filename}.
func (c *Client) ResolveFigurePath(recordID string, filename string) (string, error) {
	return c.resolvePath("figures", recordID, filename)
}

// ResolveDataFilePath returns the absolute path for a data file.
// Args: recordID identifies the record; filename identifies the file.
// Returns: absolute path under {base}/data/{recordID}/{filename}.
func (c *Client) ResolveDataFilePath(recordID string, filename string) (string, error) {
	return c.resolvePath("data", recordID, filename)
}

// CopyFigure copies a source file into the figures directory for a record.
// Args: recordID identifies destination record; sourcePath is the source file path.
// Returns: destination metadata including canonical relative key.
func (c *Client) CopyFigure(recordID string, sourcePath string) (StoredFile, error) {
	return c.copyInto("figures", recordID, sourcePath)
}

// CopyDataFile copies a source file into the data directory for a record.
// Args: recordID identifies destination record; sourcePath is the source file path.
// Returns: destination metadata including canonical relative key.
func (c *Client) CopyDataFile(recordID string, sourcePath string) (StoredFile, error) {
	return c.copyInto("data", recordID, sourcePath)
}

// DeleteFigure removes a figure file for a record.
// Args: recordID identifies record; filename identifies file.
// Returns: nil on success or a descriptive filesystem error.
func (c *Client) DeleteFigure(recordID string, filename string) error {
	path, err := c.ResolveFigurePath(recordID, filename)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete figure %s: %w", path, err)
	}
	return nil
}

// DeleteDataFile removes a data file for a record.
// Args: recordID identifies record; filename identifies file.
// Returns: nil on success or a descriptive filesystem error.
func (c *Client) DeleteDataFile(recordID string, filename string) error {
	path, err := c.ResolveDataFilePath(recordID, filename)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete data file %s: %w", path, err)
	}
	return nil
}

// BasePath returns the root data directory path.
// Returns: empty string if receiver is nil.
func (c *Client) BasePath() string {
	if c == nil {
		return ""
	}
	return c.basePath
}

// ListRecordIDsOnDisk returns record IDs that have figure or data directories on disk.
// Args: none.
// Returns: figure record IDs, data record IDs, and any error.
func (c *Client) ListRecordIDsOnDisk() (figures []string, data []string, err error) {
	if c == nil {
		return nil, nil, fmt.Errorf("filesystem client is required")
	}
	figures, err = listSubdirs(filepath.Join(c.basePath, "figures"))
	if err != nil {
		return nil, nil, fmt.Errorf("list figure directories: %w", err)
	}
	data, err = listSubdirs(filepath.Join(c.basePath, "data"))
	if err != nil {
		return nil, nil, fmt.Errorf("list data directories: %w", err)
	}
	return figures, data, nil
}

// DeleteRecordDir removes the entire figure and data directories for a record.
// Args: recordID identifies the record.
// Returns: nil on success; tolerates missing directories.
func (c *Client) DeleteRecordDir(recordID string) error {
	if c == nil {
		return fmt.Errorf("filesystem client is required")
	}
	if err := validatePathSegment("record id", recordID); err != nil {
		return err
	}
	for _, prefix := range []string{"figures", "data"} {
		dir := filepath.Join(c.basePath, prefix, recordID)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s/%s: %w", prefix, recordID, err)
		}
	}
	return nil
}

func listSubdirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	result := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && validatePathSegment("entry", e.Name()) == nil {
			result = append(result, e.Name())
		}
	}
	return result, nil
}

func (c *Client) copyInto(prefix string, recordID string, sourcePath string) (StoredFile, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return StoredFile{}, fmt.Errorf("source path is required")
	}

	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return StoredFile{}, fmt.Errorf("stat source file %s: %w", sourcePath, err)
	}
	if sourceInfo.IsDir() {
		return StoredFile{}, fmt.Errorf("source path must be a file: %s", sourcePath)
	}

	filename := filepath.Base(sourcePath)
	targetPath, err := c.resolvePath(prefix, recordID, filename)
	if err != nil {
		return StoredFile{}, err
	}

	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return StoredFile{}, fmt.Errorf("create destination directory: %w", err)
	}

	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return StoredFile{}, fmt.Errorf("read source file %s: %w", sourcePath, err)
	}
	defer func() { _ = sourceFile.Close() }()

	tempFile, err := os.CreateTemp(targetDir, ".copy-*.tmp")
	if err != nil {
		return StoredFile{}, fmt.Errorf("create temp file for %s: %w", targetPath, err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	writtenBytes, err := io.Copy(tempFile, sourceFile)
	if err != nil {
		_ = tempFile.Close()
		return StoredFile{}, fmt.Errorf("copy source file %s to %s: %w", sourcePath, targetPath, err)
	}
	hooks := c.hooks.withDefaults()
	if err := hooks.syncFile(tempFile); err != nil {
		_ = tempFile.Close()
		return StoredFile{}, fmt.Errorf("sync destination file %s: %w", targetPath, err)
	}
	if err := hooks.closeFile(tempFile); err != nil {
		return StoredFile{}, fmt.Errorf("close destination file %s: %w", targetPath, err)
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return StoredFile{}, fmt.Errorf("rename to destination %s: %w", targetPath, err)
	}
	cleanupTemp = false

	return StoredFile{
		Filename: filename,
		S3Key:    filepath.ToSlash(filepath.Join(prefix, recordID, filename)),
		Path:     targetPath,
		Size:     writtenBytes,
	}, nil
}

func (c *Client) resolvePath(prefix string, recordID string, filename string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("filesystem client is required")
	}
	if err := validatePathSegment("record id", recordID); err != nil {
		return "", err
	}
	if err := validatePathSegment("filename", filename); err != nil {
		return "", err
	}

	return filepath.Join(c.basePath, prefix, recordID, filename), nil
}

func validatePathSegment(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must not contain surrounding whitespace", field)
	}
	if value == "." || value == ".." {
		return fmt.Errorf("%s must not be %q", field, value)
	}
	if strings.Contains(value, "\\") {
		return fmt.Errorf("%s must not include path separators: %q", field, value)
	}
	if value != filepath.Base(value) {
		return fmt.Errorf("%s must not include path separators: %q", field, value)
	}
	return nil
}
