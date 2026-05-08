package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/filesystem"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newDoctorCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local system health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd.Context(), stdout, stderr)
		},
	}
	return cmd
}

// runDoctor performs local health checks and reports results.
// Returns nil if all checks pass, or an error if any check is FAIL or WARN.
func runDoctor(ctx context.Context, stdout io.Writer, _ io.Writer) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		if err := reportDoctorFailure(stdout, "write database failure", "Database", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: database check failed: %w", err)
	}
	defer func() { _ = stack.Close() }()

	// Database readable check
	if _, err := stack.Repo.GetSyncVersion(ctx); err != nil {
		if err := reportDoctorFailure(stdout, "write database read failure", "Database", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: database read failed: %w", err)
	}

	if err := reportDoctorSuccess(stdout, "write database success", "Database"); err != nil {
		return err
	}

	hasWarnings := false

	// Check orphaned directories
	figDirs, dataDirs, err := stack.FS.ListRecordIDsOnDisk()
	if err != nil {
		return fmt.Errorf("list disk directories: %w", err)
	}

	orphanFigs, err := findOrphans(ctx, stack.Repo, figDirs)
	if err != nil {
		if err := reportDoctorFailure(stdout, "write orphaned figures failure", "Orphaned figures", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: orphaned figures check failed: %w", err)
	}
	warned, err := reportDoctorOrphans(stdout, "Orphaned figures", "figure directories", orphanFigs)
	if err != nil {
		return err
	}
	hasWarnings = hasWarnings || warned

	orphanData, err := findOrphans(ctx, stack.Repo, dataDirs)
	if err != nil {
		if err := reportDoctorFailure(stdout, "write orphaned data failure", "Orphaned data", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: orphaned data check failed: %w", err)
	}
	warned, err = reportDoctorOrphans(stdout, "Orphaned data", "data directories", orphanData)
	if err != nil {
		return err
	}
	hasWarnings = hasWarnings || warned

	// Check missing files for all tracked records, including items currently in trash.
	records, err := stack.Repo.ListRecords(ctx, repository.ListRecordsFilter{IncludeDeleted: true})
	if err != nil {
		return fmt.Errorf("list records: %w", err)
	}

	missingFigs, err := checkMissingFigures(ctx, stack.Repo, stack.FS, records)
	if err != nil {
		if err := reportDoctorFailure(stdout, "write missing figures failure", "Missing figures", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: missing figures check failed: %w", err)
	}
	warned, err = reportDoctorMissingPaths(stdout, "Missing figures", "figure files", missingFigs)
	if err != nil {
		return err
	}
	hasWarnings = hasWarnings || warned

	missingDataFiles, err := checkMissingDataFiles(ctx, stack.Repo, stack.FS, records)
	if err != nil {
		if err := reportDoctorFailure(stdout, "write missing data files failure", "Missing data files", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: missing data files check failed: %w", err)
	}
	warned, err = reportDoctorMissingPaths(stdout, "Missing data files", "data files", missingDataFiles)
	if err != nil {
		return err
	}
	hasWarnings = hasWarnings || warned

	// Cloud connectivity check (only when cloud is configured).
	cloud, cloudErr := openCloudStackFn(ctx, homeDir, "")
	if cloudErr == nil {
		// Verify the Postgres connection is actually alive.
		if _, pingErr := cloud.Repo.GetSyncVersion(ctx); pingErr != nil {
			cloudErr = fmt.Errorf("cloud DB unreachable: %w", pingErr)
		}
		_ = cloud.Close()
	}
	switch {
	case cloudErr == nil:
		if err := reportDoctorSuccess(stdout, "write cloud success", "Cloud"); err != nil {
			return err
		}
	case errors.Is(cloudErr, errCloudNotConfigured):
		// Not configured — skip cloud check.
	default:
		if err := writeDoctorf(stdout, "write cloud warning", "%sWARN -- %v\n", doctorStatusPrefix("Cloud"), cloudErr); err != nil {
			return err
		}
		hasWarnings = true
	}

	if hasWarnings {
		return fmt.Errorf("doctor: warnings found")
	}

	if err := writeDoctorln(stdout, "write doctor success summary", "\nAll checks passed."); err != nil {
		return err
	}
	return nil
}

func doctorStatusPrefix(label string) string {
	return fmt.Sprintf("%-20s", label+":")
}

// reportDoctorSuccess emits an OK line for a completed health check.
func reportDoctorSuccess(w io.Writer, context string, label string) error {
	return writeDoctorln(w, context, doctorStatusPrefix(label)+"OK")
}

// reportDoctorFailure emits a FAIL line that includes the check error.
func reportDoctorFailure(w io.Writer, context string, label string, checkErr error) error {
	return writeDoctorf(w, context, "%sFAIL -- %v\n", doctorStatusPrefix(label), checkErr)
}

// reportDoctorOrphans emits either an OK line or a WARN line for orphaned
// directories and returns whether the check produced warnings.
func reportDoctorOrphans(w io.Writer, label string, noun string, paths []string) (bool, error) {
	if len(paths) == 0 {
		return false, reportDoctorSuccess(w, "write "+strings.ToLower(label)+" success", label)
	}
	if err := writeDoctorf(
		w,
		"write "+strings.ToLower(label)+" warning",
		"%sWARN -- %d orphaned %s: %v\n",
		doctorStatusPrefix(label),
		len(paths),
		noun,
		paths,
	); err != nil {
		return false, err
	}
	return true, nil
}

// reportDoctorMissingPaths emits either an OK line or a WARN line plus the
// missing path list for figure/data-file checks.
func reportDoctorMissingPaths(w io.Writer, label string, noun string, paths []string) (bool, error) {
	if len(paths) == 0 {
		return false, reportDoctorSuccess(w, "write "+strings.ToLower(label)+" success", label)
	}
	if err := writeDoctorf(
		w,
		"write "+strings.ToLower(label)+" warning",
		"%sWARN -- %d missing %s\n",
		doctorStatusPrefix(label),
		len(paths),
		noun,
	); err != nil {
		return false, err
	}
	for _, path := range paths {
		if err := writeDoctorf(w, "write "+strings.ToLower(label[:len(label)-1])+" path", "  %s\n", path); err != nil {
			return false, err
		}
	}
	return true, nil
}

func writeDoctorf(w io.Writer, context string, format string, args ...any) error {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	return nil
}

func writeDoctorln(w io.Writer, context string, line string) error {
	if _, err := fmt.Fprintln(w, line); err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	return nil
}

// findOrphans returns record IDs from the given list that do not exist in the repository.
// Args: ctx is the context; repo is the repository; recordIDs is the list of IDs found on disk.
// Returns: a slice of orphaned record IDs or an error when the repository lookup fails unexpectedly.
func findOrphans(ctx context.Context, repo repository.Repository, recordIDs []string) ([]string, error) {
	orphans := make([]string, 0)
	for _, id := range recordIDs {
		_, err := repo.GetRecordByID(ctx, id)
		if errors.Is(err, repository.ErrNotFound) {
			orphans = append(orphans, id)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("look up record %s: %w", id, err)
		}
	}
	return orphans, nil
}

// checkMissingFigures checks all tracked records for figure files that are
// recorded in the database but missing from disk.
// Args: ctx is the context; repo is the repository; fs is the filesystem client; records is the list of records.
// Returns: missing figure paths (as recordID/filename), or an error when inspection fails.
func checkMissingFigures(ctx context.Context, repo repository.Repository, fs *filesystem.Client, records []repository.Record) ([]string, error) {
	missingFigures := make([]string, 0)
	for _, record := range records {
		figures, err := repo.ListRecordFiguresByRecordID(ctx, record.ID)
		if err != nil {
			return nil, fmt.Errorf("list figures for %s: %w", record.ID, err)
		}
		for _, fig := range figures {
			path, err := fs.ResolveFigurePath(record.ID, fig.Filename)
			if err != nil {
				return nil, fmt.Errorf("resolve figure path for %s/%s: %w", record.ID, fig.Filename, err)
			}
			if _, err := os.Stat(path); err != nil {
				if os.IsNotExist(err) {
					missingFigures = append(missingFigures, record.ID+"/"+fig.Filename)
					continue
				}
				return nil, fmt.Errorf("stat figure path for %s/%s: %w", record.ID, fig.Filename, err)
			}
		}
	}
	return missingFigures, nil
}

// checkMissingDataFiles checks all tracked records for data files that are
// recorded in the database but missing from disk.
// Args: ctx is the context; repo is the repository; fs is the filesystem client; records is the list of records.
// Returns: missing data file paths (as recordID/filename), or an error when inspection fails.
func checkMissingDataFiles(ctx context.Context, repo repository.Repository, fs *filesystem.Client, records []repository.Record) ([]string, error) {
	missingDataFiles := make([]string, 0)
	for _, record := range records {
		dataFiles, err := repo.ListRecordDataFilesByRecordID(ctx, record.ID)
		if err != nil {
			return nil, fmt.Errorf("list data files for %s: %w", record.ID, err)
		}
		for _, df := range dataFiles {
			path, err := fs.ResolveDataFilePath(record.ID, df.Filename)
			if err != nil {
				return nil, fmt.Errorf("resolve data file path for %s/%s: %w", record.ID, df.Filename, err)
			}
			if _, err := os.Stat(path); err != nil {
				if os.IsNotExist(err) {
					missingDataFiles = append(missingDataFiles, record.ID+"/"+df.Filename)
					continue
				}
				return nil, fmt.Errorf("stat data file path for %s/%s: %w", record.ID, df.Filename, err)
			}
		}
	}
	return missingDataFiles, nil
}
