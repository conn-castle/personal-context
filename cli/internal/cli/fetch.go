package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	pcs3 "github.com/conn-castle/personal-context/cli/internal/s3client"
	"github.com/spf13/cobra"
)

// downloadS3FileFn downloads a file from S3 and writes it to the local path.
// Uses atomic write (temp file + rename) to avoid leaving partial files on failure.
var downloadS3FileFn = func(ctx context.Context, s3Client *pcs3.Client, key string, destPath string) error {
	body, err := s3Client.Download(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(destDir, ".pc-fetch-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	closed := false
	renamed := false
	defer func() {
		if !closed {
			_ = tmpFile.Close()
		}
		if !renamed {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmpFile, body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	closed = true

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	renamed = true

	return nil
}

// fetchOptions holds the flags for the fetch command.
type fetchOptions struct {
	Project string
	Recent  string
	Output  string
}

func newFetchCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var opts fetchOptions

	cmd := &cobra.Command{
		Use:   "fetch [record_id]",
		Short: "Download data files from cloud S3",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var recordID string
			if len(args) == 1 {
				recordID = args[0]
			}
			return runFetch(cmd.Context(), stdout, stderr, recordID, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Project, "project", "", "Download data files for all records in a project")
	cmd.Flags().StringVar(&opts.Recent, "recent", "", "Download data files for recent records (e.g. 3d, 2w, 1m, 1y)")
	cmd.Flags().StringVar(&opts.Output, "output", "", "Write downloads to this directory instead of the default data path")

	return cmd
}

// runFetch downloads data files from cloud S3 to local disk.
func runFetch(ctx context.Context, stdout io.Writer, _ io.Writer, recordID string, opts fetchOptions) error {
	// Validate exactly one mode selector.
	modeCount := 0
	if recordID != "" {
		modeCount++
	}
	if opts.Project != "" {
		modeCount++
	}
	if opts.Recent != "" {
		modeCount++
	}
	if modeCount == 0 {
		return fmt.Errorf("specify a record ID, --project, or --recent")
	}
	if modeCount > 1 {
		return fmt.Errorf("record ID, --project, and --recent are mutually exclusive")
	}

	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	cloud, err := openCloudStackFn(ctx, homeDir, "")
	if err != nil {
		if errors.Is(err, errCloudNotConfigured) {
			return fmt.Errorf("cloud is not configured; run 'pc setup' first")
		}
		return fmt.Errorf("open cloud: %w", err)
	}
	defer func() { _ = cloud.Close() }()

	// Determine the output base directory.
	outputBase := opts.Output
	if outputBase == "" {
		outputBase = filepath.Join(basePath(homeDir), "data")
	}

	// Resolve record IDs and their data files.
	var dataFiles []repository.RecordDataFile
	switch {
	case recordID != "":
		dataFiles, err = fetchRecordDataFiles(ctx, cloud.Repo, recordID)
	case opts.Project != "":
		dataFiles, err = fetchProjectDataFiles(ctx, cloud.Repo, opts.Project)
	case opts.Recent != "":
		dataFiles, err = fetchRecentDataFiles(ctx, cloud.Repo, opts.Recent)
	}
	if err != nil {
		return err
	}

	if len(dataFiles) == 0 {
		_, _ = fmt.Fprintln(stdout, "No data files to download.")
		return nil
	}

	// Download each file from S3.
	downloaded := 0
	for _, df := range dataFiles {
		// Sanitize path components to prevent directory traversal from cloud metadata.
		recordDir := filepath.Base(df.RecordID)
		fileName := filepath.Base(df.Filename)
		if recordDir == "." || recordDir == ".." || fileName == "." || fileName == ".." {
			return fmt.Errorf("invalid path component in record %s file %s", df.RecordID, df.Filename)
		}
		destPath := filepath.Join(outputBase, recordDir, fileName)
		if err := downloadS3FileFn(ctx, cloud.S3, df.S3Key, destPath); err != nil {
			return fmt.Errorf("download %s: %w", df.S3Key, err)
		}
		downloaded++
	}

	_, _ = fmt.Fprintf(stdout, "Downloaded %d file(s) to %s\n", downloaded, outputBase)
	return nil
}

// fetchRecordDataFiles retrieves data files for a single record.
func fetchRecordDataFiles(ctx context.Context, repo repository.Repository, recordID string) ([]repository.RecordDataFile, error) {
	_, err := repo.GetRecordByID(ctx, recordID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("record %q not found", recordID)
		}
		return nil, fmt.Errorf("get record: %w", err)
	}

	files, err := repo.ListRecordDataFilesByRecordID(ctx, recordID)
	if err != nil {
		return nil, fmt.Errorf("list data files: %w", err)
	}
	return files, nil
}

// fetchProjectDataFiles retrieves data files for all records in a project.
func fetchProjectDataFiles(ctx context.Context, repo repository.Repository, projectID string) ([]repository.RecordDataFile, error) {
	records, err := repo.ListRecords(ctx, repository.ListRecordsFilter{
		ProjectID: &projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("list project records: %w", err)
	}

	return collectDataFiles(ctx, repo, records)
}

// fetchRecentDataFiles retrieves data files for records within a time window.
func fetchRecentDataFiles(ctx context.Context, repo repository.Repository, window string) ([]repository.RecordDataFile, error) {
	dateFrom, err := parseRecentWindow(window)
	if err != nil {
		return nil, err
	}

	records, err := repo.ListRecords(ctx, repository.ListRecordsFilter{
		DateFrom: &dateFrom,
	})
	if err != nil {
		return nil, fmt.Errorf("list recent records: %w", err)
	}

	return collectDataFiles(ctx, repo, records)
}

// collectDataFiles gathers all data files for a list of records.
func collectDataFiles(ctx context.Context, repo repository.Repository, records []repository.Record) ([]repository.RecordDataFile, error) {
	var all []repository.RecordDataFile
	for _, s := range records {
		files, err := repo.ListRecordDataFilesByRecordID(ctx, s.ID)
		if err != nil {
			return nil, fmt.Errorf("list data files for record %s: %w", s.ID, err)
		}
		all = append(all, files...)
	}
	return all, nil
}

// parseRecentWindow parses a relative time window string (e.g., "3d", "2w", "1m", "1y")
// and returns the corresponding YYYY-MM-DD date string.
func parseRecentWindow(window string) (string, error) {
	window = strings.TrimSpace(window)
	if len(window) < 2 {
		return "", fmt.Errorf("invalid --recent value %q: expected format like 3d, 2w, 1m, 1y", window)
	}

	suffix := window[len(window)-1]
	numStr := window[:len(window)-1]

	n, err := strconv.Atoi(numStr)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("invalid --recent value %q: expected a positive number followed by d, w, m, or y", window)
	}

	now := time.Now().UTC()
	var from time.Time
	switch suffix {
	case 'd':
		from = now.AddDate(0, 0, -n)
	case 'w':
		from = now.AddDate(0, 0, -n*7)
	case 'm':
		from = now.AddDate(0, -n, 0)
	case 'y':
		from = now.AddDate(-n, 0, 0)
	default:
		return "", fmt.Errorf("invalid --recent suffix %q: expected d, w, m, or y", string(suffix))
	}

	return from.Format("2006-01-02"), nil
}
