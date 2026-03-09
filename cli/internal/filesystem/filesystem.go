package filesystem

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	syncFileFn  = func(f *os.File) error { return f.Sync() }
	closeFileFn = func(f *os.File) error { return f.Close() }
)

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
}

// NewClient creates a filesystem client.
// Args: basePath is the root local data directory (e.g., ~/personal-context).
// Returns: a configured client or error when basePath is empty.
func NewClient(basePath string) (*Client, error) {
	if strings.TrimSpace(basePath) == "" {
		return nil, fmt.Errorf("base path is required")
	}
	return &Client{basePath: basePath}, nil
}

// ResolveFigurePath returns the absolute path for a figure file.
// Args: slideID identifies the slide; filename identifies the file.
// Returns: absolute path under {base}/figures/{slideID}/{filename}.
func (c *Client) ResolveFigurePath(slideID string, filename string) (string, error) {
	return c.resolvePath("figures", slideID, filename)
}

// ResolveDataFilePath returns the absolute path for a data file.
// Args: slideID identifies the slide; filename identifies the file.
// Returns: absolute path under {base}/data/{slideID}/{filename}.
func (c *Client) ResolveDataFilePath(slideID string, filename string) (string, error) {
	return c.resolvePath("data", slideID, filename)
}

// CopyFigure copies a source file into the figures directory for a slide.
// Args: slideID identifies destination slide; sourcePath is the source file path.
// Returns: destination metadata including canonical relative key.
func (c *Client) CopyFigure(slideID string, sourcePath string) (StoredFile, error) {
	return c.copyInto("figures", slideID, sourcePath)
}

// CopyDataFile copies a source file into the data directory for a slide.
// Args: slideID identifies destination slide; sourcePath is the source file path.
// Returns: destination metadata including canonical relative key.
func (c *Client) CopyDataFile(slideID string, sourcePath string) (StoredFile, error) {
	return c.copyInto("data", slideID, sourcePath)
}

// DeleteFigure removes a figure file for a slide.
// Args: slideID identifies slide; filename identifies file.
// Returns: nil on success or a descriptive filesystem error.
func (c *Client) DeleteFigure(slideID string, filename string) error {
	path, err := c.ResolveFigurePath(slideID, filename)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete figure %s: %w", path, err)
	}
	return nil
}

// DeleteDataFile removes a data file for a slide.
// Args: slideID identifies slide; filename identifies file.
// Returns: nil on success or a descriptive filesystem error.
func (c *Client) DeleteDataFile(slideID string, filename string) error {
	path, err := c.ResolveDataFilePath(slideID, filename)
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

// ListSlideIDsOnDisk returns slide IDs that have figure or data directories on disk.
// Args: none.
// Returns: figure slide IDs, data slide IDs, and any error.
func (c *Client) ListSlideIDsOnDisk() (figures []string, data []string, err error) {
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

// DeleteSlideDir removes the entire figure and data directories for a slide.
// Args: slideID identifies the slide.
// Returns: nil on success; tolerates missing directories.
func (c *Client) DeleteSlideDir(slideID string) error {
	if c == nil {
		return fmt.Errorf("filesystem client is required")
	}
	if err := validatePathSegment("slide id", slideID); err != nil {
		return err
	}
	for _, prefix := range []string{"figures", "data"} {
		dir := filepath.Join(c.basePath, prefix, slideID)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s/%s: %w", prefix, slideID, err)
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

func (c *Client) copyInto(prefix string, slideID string, sourcePath string) (StoredFile, error) {
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
	targetPath, err := c.resolvePath(prefix, slideID, filename)
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
	if err := syncFileFn(tempFile); err != nil {
		_ = tempFile.Close()
		return StoredFile{}, fmt.Errorf("sync destination file %s: %w", targetPath, err)
	}
	if err := closeFileFn(tempFile); err != nil {
		return StoredFile{}, fmt.Errorf("close destination file %s: %w", targetPath, err)
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return StoredFile{}, fmt.Errorf("rename to destination %s: %w", targetPath, err)
	}
	cleanupTemp = false

	return StoredFile{
		Filename: filename,
		S3Key:    filepath.ToSlash(filepath.Join(prefix, slideID, filename)),
		Path:     targetPath,
		Size:     writtenBytes,
	}, nil
}

func (c *Client) resolvePath(prefix string, slideID string, filename string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("filesystem client is required")
	}
	if err := validatePathSegment("slide id", slideID); err != nil {
		return "", err
	}
	if err := validatePathSegment("filename", filename); err != nil {
		return "", err
	}

	return filepath.Join(c.basePath, prefix, slideID, filename), nil
}

func validatePathSegment(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if value == "." || value == ".." {
		return fmt.Errorf("%s must not be %q", field, value)
	}
	if value != filepath.Base(value) {
		return fmt.Errorf("%s must not include path separators: %q", field, value)
	}
	return nil
}
