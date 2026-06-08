package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCopyFigureAndDataFile(t *testing.T) {
	root := t.TempDir()
	client, err := NewClient(root)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	sourceDir := t.TempDir()
	figureSource := filepath.Join(sourceDir, "plot.png")
	if err := os.WriteFile(figureSource, []byte("figure-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile(figureSource) error = %v", err)
	}
	dataSource := filepath.Join(sourceDir, "metrics.csv")
	if err := os.WriteFile(dataSource, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(dataSource) error = %v", err)
	}

	figure, err := client.CopyFigure("20260305-a1b2c3d4", figureSource)
	if err != nil {
		t.Fatalf("CopyFigure() error = %v", err)
	}
	if figure.S3Key != "figures/20260305-a1b2c3d4/plot.png" {
		t.Fatalf("unexpected figure s3_key: %s", figure.S3Key)
	}
	if figure.Size != int64(len("figure-bytes")) {
		t.Fatalf("unexpected figure size: %d", figure.Size)
	}

	dataFile, err := client.CopyDataFile("20260305-a1b2c3d4", dataSource)
	if err != nil {
		t.Fatalf("CopyDataFile() error = %v", err)
	}
	if dataFile.S3Key != "data/20260305-a1b2c3d4/metrics.csv" {
		t.Fatalf("unexpected data-file s3_key: %s", dataFile.S3Key)
	}
	if dataFile.Size != int64(len("a,b\n1,2\n")) {
		t.Fatalf("unexpected data file size: %d", dataFile.Size)
	}

	copiedFigure, err := os.ReadFile(figure.Path)
	if err != nil {
		t.Fatalf("ReadFile(copiedFigure) error = %v", err)
	}
	if string(copiedFigure) != "figure-bytes" {
		t.Fatalf("unexpected copied figure content: %q", string(copiedFigure))
	}

	copiedDataFile, err := os.ReadFile(dataFile.Path)
	if err != nil {
		t.Fatalf("ReadFile(copiedDataFile) error = %v", err)
	}
	if string(copiedDataFile) != "a,b\n1,2\n" {
		t.Fatalf("unexpected copied data-file content: %q", string(copiedDataFile))
	}
}

func TestDelete(t *testing.T) {
	root := t.TempDir()
	client, err := NewClient(root)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	source := filepath.Join(t.TempDir(), "figure.png")
	if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	stored, err := client.CopyFigure("20260305-a1b2c3d4", source)
	if err != nil {
		t.Fatalf("CopyFigure() error = %v", err)
	}

	if err := client.DeleteFigure("20260305-a1b2c3d4", stored.Filename); err != nil {
		t.Fatalf("DeleteFigure() error = %v", err)
	}

	if _, err := os.Stat(stored.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected copied figure to be deleted, got err=%v", err)
	}
}

func TestPathResolution(t *testing.T) {
	client, err := NewClient("/tmp/personal-context")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	figurePath, err := client.ResolveFigurePath("record-1", "figure.png")
	if err != nil {
		t.Fatalf("ResolveFigurePath() error = %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(figurePath), "/figures/record-1/figure.png") {
		t.Fatalf("unexpected figure path: %s", figurePath)
	}

	dataPath, err := client.ResolveDataFilePath("record-1", "data.csv")
	if err != nil {
		t.Fatalf("ResolveDataFilePath() error = %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(dataPath), "/data/record-1/data.csv") {
		t.Fatalf("unexpected data-file path: %s", dataPath)
	}
}

func TestSpecialCharactersInFilename(t *testing.T) {
	root := t.TempDir()
	client, err := NewClient(root)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	source := filepath.Join(t.TempDir(), "plot (v2) #1 @final!.png")
	if err := os.WriteFile(source, []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	stored, err := client.CopyFigure("20260305-a1b2c3d4", source)
	if err != nil {
		t.Fatalf("CopyFigure() error = %v", err)
	}

	if stored.Filename != "plot (v2) #1 @final!.png" {
		t.Fatalf("unexpected stored filename: %s", stored.Filename)
	}
	if _, err := os.Stat(stored.Path); err != nil {
		t.Fatalf("expected copied special-char file to exist: %v", err)
	}
}

func TestMissingDirectoriesFailLoudly(t *testing.T) {
	client, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	missingSource := filepath.Join(t.TempDir(), "missing", "figure.png")
	if _, err := client.CopyFigure("20260305-a1b2c3d4", missingSource); err == nil {
		t.Fatal("expected error for missing source directory/file")
	}
}

func TestNewClientRejectsEmptyBasePath(t *testing.T) {
	if _, err := NewClient(""); err == nil {
		t.Fatal("expected error for empty base path")
	}
	if _, err := NewClient("   "); err == nil {
		t.Fatal("expected error for whitespace-only base path")
	}
}

func TestResolvePathRejectsTraversalFilename(t *testing.T) {
	client, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.ResolveFigurePath("record-1", "../bad.png"); err == nil {
		t.Fatal("expected traversal filename to fail")
	}
	if _, err := client.ResolveFigurePath("../../etc", "passwd"); err == nil {
		t.Fatal("expected traversal record id to fail")
	}
}

func TestResolvePathRejectsDotSegments(t *testing.T) {
	client, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.ResolveFigurePath(".", "file.png"); err == nil {
		t.Fatal("expected dot record id to fail")
	}
	if _, err := client.ResolveFigurePath("..", "file.png"); err == nil {
		t.Fatal("expected dot-dot record id to fail")
	}
	if _, err := client.ResolveFigurePath("record-1", "."); err == nil {
		t.Fatal("expected dot filename to fail")
	}
	if _, err := client.ResolveFigurePath("record-1", ".."); err == nil {
		t.Fatal("expected dot-dot filename to fail")
	}
}

func TestDeleteDataFileAndMissingDeleteErrors(t *testing.T) {
	root := t.TempDir()
	client, err := NewClient(root)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	source := filepath.Join(t.TempDir(), "file.csv")
	if err := os.WriteFile(source, []byte("1,2"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	stored, err := client.CopyDataFile("20260305-a1b2c3d4", source)
	if err != nil {
		t.Fatalf("CopyDataFile() error = %v", err)
	}
	if err := client.DeleteDataFile("20260305-a1b2c3d4", stored.Filename); err != nil {
		t.Fatalf("DeleteDataFile() error = %v", err)
	}
	if err := client.DeleteDataFile("20260305-a1b2c3d4", stored.Filename); err == nil {
		t.Fatal("expected error when deleting missing data file")
	}
	if err := client.DeleteFigure("20260305-a1b2c3d4", "missing.png"); err == nil {
		t.Fatal("expected error when deleting missing figure")
	}
}

func TestCopyRejectsDirectoryAndNilClient(t *testing.T) {
	root := t.TempDir()
	client, err := NewClient(root)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.CopyFigure("20260305-a1b2c3d4", t.TempDir()); err == nil {
		t.Fatal("expected directory source to fail")
	}

	var nilClient *Client
	if _, err := nilClient.ResolveFigurePath("record-1", "file.png"); err == nil {
		t.Fatal("expected nil client resolve to fail")
	}
	if err := nilClient.DeleteFigure("record-1", "file.png"); err == nil {
		t.Fatal("expected nil client delete to fail")
	}
}

func TestResolveRejectsMissingFields(t *testing.T) {
	client, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.ResolveFigurePath("", "file.png"); err == nil {
		t.Fatal("expected empty record id to fail")
	}
	if _, err := client.ResolveDataFilePath("record-1", ""); err == nil {
		t.Fatal("expected empty filename to fail")
	}
}

func TestCopyRejectsEmptySourcePath(t *testing.T) {
	client, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.CopyFigure("record-1", ""); err == nil {
		t.Fatal("expected empty source path to fail")
	}
	if _, err := client.CopyDataFile("record-1", ""); err == nil {
		t.Fatal("expected empty source path to fail")
	}
}

func TestCopyRejectsInvalidRecordID(t *testing.T) {
	client, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	source := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	if _, err := client.CopyFigure("", source); err == nil {
		t.Fatal("expected CopyFigure() to fail for empty record id")
	}
	if _, err := client.CopyDataFile("", source); err == nil {
		t.Fatal("expected CopyDataFile() to fail for empty record id")
	}
}

func TestDeleteRejectsInvalidInput(t *testing.T) {
	client, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if err := client.DeleteFigure("", "x.png"); err == nil {
		t.Fatal("expected DeleteFigure() to fail for empty record id")
	}
	if err := client.DeleteFigure("record-1", ""); err == nil {
		t.Fatal("expected DeleteFigure() to fail for empty filename")
	}
	if err := client.DeleteDataFile("", "x.csv"); err == nil {
		t.Fatal("expected DeleteDataFile() to fail for empty record id")
	}
	if err := client.DeleteDataFile("record-1", ""); err == nil {
		t.Fatal("expected DeleteDataFile() to fail for empty filename")
	}
}

func TestCopyFailsWhenDestinationDirectoryCannotBeCreated(t *testing.T) {
	root := t.TempDir()
	client, err := NewClient(root)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	source := filepath.Join(t.TempDir(), "figure.png")
	if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	blockingParent := filepath.Join(root, "figures")
	if err := os.WriteFile(blockingParent, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("WriteFile(blockingParent) error = %v", err)
	}

	if _, err := client.CopyFigure("20260305-a1b2c3d4", source); err == nil {
		t.Fatal("expected CopyFigure() to fail when destination parent cannot be created")
	}
}

func TestCopyFailsWhenDestinationPathIsDirectory(t *testing.T) {
	root := t.TempDir()
	client, err := NewClient(root)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "plot.png")
	if err := os.WriteFile(source, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	targetDir := filepath.Join(root, "figures", "20260305-a1b2c3d4", "plot.png")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(targetDir) error = %v", err)
	}

	if _, err := client.CopyFigure("20260305-a1b2c3d4", source); err == nil {
		t.Fatal("expected CopyFigure() to fail when destination path is an existing directory")
	}
}

func TestCopyFailsWhenDestinationParentCannotBeCreated(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "data")
	if err := os.WriteFile(blocked, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("WriteFile(blocked) error = %v", err)
	}

	client, err := NewClient(root)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	source := filepath.Join(t.TempDir(), "metrics.csv")
	if err := os.WriteFile(source, []byte("1,2"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	if _, err := client.CopyDataFile("record-1", source); err == nil {
		t.Fatal("expected copy to fail when destination parent is blocked")
	}
}

func TestCopyRejectsNonCanonicalFilenameSegments(t *testing.T) {
	root := t.TempDir()
	client, err := NewClient(root)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	for _, name := range []string{" leading.png", "trailing.png ", "bad\\name.png"} {
		source := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", source, err)
		}
		if _, err := client.CopyFigure("20260305-a1b2c3d4", source); err == nil {
			t.Fatalf("expected CopyFigure(%q) to reject non-canonical filename", name)
		}
	}
}

func TestCopyPropagatesSyncFailure(t *testing.T) {
	root := t.TempDir()
	client, err := newClientWithHooks(root, fileOperationHooks{
		syncFile: func(*os.File) error { return errors.New("sync boom") },
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	source := filepath.Join(t.TempDir(), "file.png")
	if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	_, err = client.CopyFigure("20260305-a1b2c3d4", source)
	if err == nil {
		t.Fatal("expected CopyFigure() to fail when sync fails")
	}
	if !strings.Contains(err.Error(), "sync destination file") {
		t.Fatalf("expected sync error context, got %v", err)
	}
}

func TestCopyPropagatesCloseFailure(t *testing.T) {
	root := t.TempDir()
	client, err := newClientWithHooks(root, fileOperationHooks{
		closeFile: func(*os.File) error { return errors.New("close boom") },
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	source := filepath.Join(t.TempDir(), "file.png")
	if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) error = %v", err)
	}

	_, err = client.CopyFigure("20260305-a1b2c3d4", source)
	if err == nil {
		t.Fatal("expected CopyFigure() to fail when close fails")
	}
	if !strings.Contains(err.Error(), "close destination file") {
		t.Fatalf("expected close error context, got %v", err)
	}
}

func TestCopyFailsWhenSourceFileCannotBeOpened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based test not reliable on Windows")
	}

	source := filepath.Join(t.TempDir(), "unreadable.png")
	if err := os.WriteFile(source, []byte("x"), 0o200); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(source, 0o644) })

	client, err := NewClient(t.TempDir())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := client.CopyFigure("20260305-a1b2c3d4", source); err == nil {
		t.Fatal("expected CopyFigure() to fail when source file is not readable")
	}
}

func TestDeleteDataFileRejectsInvalidInputOnNilClient(t *testing.T) {
	var nilClient *Client
	if err := nilClient.DeleteDataFile("record-1", "f.csv"); err == nil {
		t.Fatal("expected nil client delete data file to fail")
	}
}

func TestBasePathReturnsConfiguredPath(t *testing.T) {
	root := t.TempDir()
	client, err := NewClient(root)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if got := client.BasePath(); got != root {
		t.Fatalf("BasePath() = %q, want %q", got, root)
	}
}

func TestListRecordIDsOnDisk(t *testing.T) {
	t.Run("no directories exist", func(t *testing.T) {
		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		figures, data, err := client.ListRecordIDsOnDisk()
		if err != nil {
			t.Fatalf("ListRecordIDsOnDisk() error = %v", err)
		}
		if len(figures) != 0 {
			t.Fatalf("expected empty figures, got %v", figures)
		}
		if len(data) != 0 {
			t.Fatalf("expected empty data, got %v", data)
		}
	})

	t.Run("figures and data dirs with record ID subdirs", func(t *testing.T) {
		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		for _, dir := range []string{
			filepath.Join(root, "figures", "record-a"),
			filepath.Join(root, "figures", "record-b"),
			filepath.Join(root, "data", "record-a"),
			filepath.Join(root, "data", "record-c"),
		} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("MkdirAll(%s) error = %v", dir, err)
			}
		}

		figures, data, err := client.ListRecordIDsOnDisk()
		if err != nil {
			t.Fatalf("ListRecordIDsOnDisk() error = %v", err)
		}
		if len(figures) != 2 {
			t.Fatalf("expected 2 figure dirs, got %v", figures)
		}
		if len(data) != 2 {
			t.Fatalf("expected 2 data dirs, got %v", data)
		}
	})

	t.Run("only figures exists", func(t *testing.T) {
		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		if err := os.MkdirAll(filepath.Join(root, "figures", "record-x"), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}

		figures, data, err := client.ListRecordIDsOnDisk()
		if err != nil {
			t.Fatalf("ListRecordIDsOnDisk() error = %v", err)
		}
		if len(figures) != 1 || figures[0] != "record-x" {
			t.Fatalf("expected [record-x], got %v", figures)
		}
		if len(data) != 0 {
			t.Fatalf("expected empty data, got %v", data)
		}
	})

	t.Run("non-directory entries are skipped", func(t *testing.T) {
		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(figuresDir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		// Create a regular file inside figures/ — should be skipped
		if err := os.WriteFile(filepath.Join(figuresDir, "stray-file.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		// Also create a valid subdir
		if err := os.MkdirAll(filepath.Join(figuresDir, "record-valid"), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}

		figures, _, err := client.ListRecordIDsOnDisk()
		if err != nil {
			t.Fatalf("ListRecordIDsOnDisk() error = %v", err)
		}
		if len(figures) != 1 || figures[0] != "record-valid" {
			t.Fatalf("expected [record-valid], got %v", figures)
		}
	})

	t.Run("dot and dotdot entries are skipped", func(t *testing.T) {
		// The OS never returns . and .. from ReadDir, but validatePathSegment
		// would reject them. We verify indirectly by creating dirs named "."
		// or ".." which is not possible on most OS, so we just confirm that
		// the function doesn't return them and that listing a directory with
		// only valid subdirs works correctly.
		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		dataDir := filepath.Join(root, "data")
		if err := os.MkdirAll(filepath.Join(dataDir, "valid-record"), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}

		_, data, err := client.ListRecordIDsOnDisk()
		if err != nil {
			t.Fatalf("ListRecordIDsOnDisk() error = %v", err)
		}
		if len(data) != 1 || data[0] != "valid-record" {
			t.Fatalf("expected [valid-record], got %v", data)
		}
	})

	t.Run("figures dir unreadable returns error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("permission-based test not reliable on Windows")
		}

		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		figuresDir := filepath.Join(root, "figures")
		if err := os.MkdirAll(figuresDir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.Chmod(figuresDir, 0o000); err != nil {
			t.Fatalf("Chmod() error = %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(figuresDir, 0o755) })

		_, _, err = client.ListRecordIDsOnDisk()
		if err == nil {
			t.Fatal("expected error when figures directory is unreadable")
		}
		if !strings.Contains(err.Error(), "list figure directories") {
			t.Fatalf("expected 'list figure directories' in error, got %v", err)
		}
	})

	t.Run("data dir unreadable returns error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("permission-based test not reliable on Windows")
		}

		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		// figures must succeed first — create it normally
		if err := os.MkdirAll(filepath.Join(root, "figures"), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}

		dataDir := filepath.Join(root, "data")
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.Chmod(dataDir, 0o000); err != nil {
			t.Fatalf("Chmod() error = %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dataDir, 0o755) })

		_, _, err = client.ListRecordIDsOnDisk()
		if err == nil {
			t.Fatal("expected error when data directory is unreadable")
		}
		if !strings.Contains(err.Error(), "list data directories") {
			t.Fatalf("expected 'list data directories' in error, got %v", err)
		}
	})
}

func TestNilReceiverGuards(t *testing.T) {
	var nilClient *Client

	t.Run("BasePath returns empty string", func(t *testing.T) {
		if got := nilClient.BasePath(); got != "" {
			t.Fatalf("BasePath() = %q, want empty string", got)
		}
	})

	t.Run("ListRecordIDsOnDisk returns error", func(t *testing.T) {
		_, _, err := nilClient.ListRecordIDsOnDisk()
		if err == nil {
			t.Fatal("expected nil client ListRecordIDsOnDisk to fail")
		}
		if !strings.Contains(err.Error(), "filesystem client is required") {
			t.Fatalf("expected 'filesystem client is required' in error, got %v", err)
		}
	})

	t.Run("DeleteRecordDir returns error", func(t *testing.T) {
		err := nilClient.DeleteRecordDir("record-1")
		if err == nil {
			t.Fatal("expected nil client DeleteRecordDir to fail")
		}
		if !strings.Contains(err.Error(), "filesystem client is required") {
			t.Fatalf("expected 'filesystem client is required' in error, got %v", err)
		}
	})
}

func TestDeleteRecordDir(t *testing.T) {
	t.Run("removes existing figure and data directories", func(t *testing.T) {
		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		// Create figure and data directories with files inside
		figDir := filepath.Join(root, "figures", "record-1")
		dataDir := filepath.Join(root, "data", "record-1")
		if err := os.MkdirAll(figDir, 0o755); err != nil {
			t.Fatalf("MkdirAll(figDir) error = %v", err)
		}
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			t.Fatalf("MkdirAll(dataDir) error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(figDir, "plot.png"), []byte("img"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, "data.csv"), []byte("1,2"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		if err := client.DeleteRecordDir("record-1"); err != nil {
			t.Fatalf("DeleteRecordDir() error = %v", err)
		}

		if _, err := os.Stat(figDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected figure dir to be removed, got err=%v", err)
		}
		if _, err := os.Stat(dataDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected data dir to be removed, got err=%v", err)
		}
	})

	t.Run("tolerates missing directories", func(t *testing.T) {
		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		// Neither figures/record-1 nor data/record-1 exist
		if err := client.DeleteRecordDir("record-1"); err != nil {
			t.Fatalf("DeleteRecordDir() should tolerate missing dirs, got error = %v", err)
		}
	})

	t.Run("rejects empty recordID", func(t *testing.T) {
		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		if err := client.DeleteRecordDir(""); err == nil {
			t.Fatal("expected error for empty recordID")
		}
	})

	t.Run("returns remove errors", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Getuid() == 0 {
			t.Skip("permission-based remove failure is not reliable")
		}
		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		parent := filepath.Join(root, "figures")
		recordDir := filepath.Join(parent, "record-1")
		if err := os.MkdirAll(recordDir, 0o700); err != nil {
			t.Fatalf("MkdirAll(recordDir) error = %v", err)
		}
		if err := os.Chmod(parent, 0o500); err != nil {
			t.Fatalf("Chmod(parent) error = %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
		if err := client.DeleteRecordDir("record-1"); err == nil || !strings.Contains(err.Error(), "remove figures/record-1") {
			t.Fatalf("expected remove error, got %v", err)
		}
	})

	t.Run("files inside directories are also removed", func(t *testing.T) {
		root := t.TempDir()
		client, err := NewClient(root)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		figDir := filepath.Join(root, "figures", "record-2")
		if err := os.MkdirAll(figDir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		fileA := filepath.Join(figDir, "a.png")
		fileB := filepath.Join(figDir, "b.png")
		if err := os.WriteFile(fileA, []byte("a"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if err := os.WriteFile(fileB, []byte("b"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		if err := client.DeleteRecordDir("record-2"); err != nil {
			t.Fatalf("DeleteRecordDir() error = %v", err)
		}

		if _, err := os.Stat(fileA); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected file a.png to be removed, got err=%v", err)
		}
		if _, err := os.Stat(fileB); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected file b.png to be removed, got err=%v", err)
		}
	})
}
