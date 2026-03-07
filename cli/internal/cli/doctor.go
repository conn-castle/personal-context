package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

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
		if err := writeDoctorf(stdout, "write database failure", "Database:           FAIL -- %v\n", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: database check failed: %w", err)
	}
	defer func() { _ = stack.Close() }()

	// Database readable check
	if _, err := stack.Repo.GetSyncVersion(ctx); err != nil {
		if err := writeDoctorf(stdout, "write database read failure", "Database:           FAIL -- %v\n", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: database read failed: %w", err)
	}

	if err := writeDoctorln(stdout, "write database success", "Database:           OK"); err != nil {
		return err
	}

	hasWarnings := false

	// Check orphaned directories
	figDirs, dataDirs, err := stack.FS.ListSlideIDsOnDisk()
	if err != nil {
		return fmt.Errorf("list disk directories: %w", err)
	}

	orphanFigs, err := findOrphans(ctx, stack.Repo, figDirs)
	if err != nil {
		if err := writeDoctorf(stdout, "write orphaned figures failure", "Orphaned figures:   FAIL -- %v\n", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: orphaned figures check failed: %w", err)
	}
	if len(orphanFigs) > 0 {
		if err := writeDoctorf(stdout, "write orphaned figures warning", "Orphaned figures:   WARN -- %d orphaned figure directories: %v\n", len(orphanFigs), orphanFigs); err != nil {
			return err
		}
		hasWarnings = true
	} else {
		if err := writeDoctorln(stdout, "write orphaned figures success", "Orphaned figures:   OK"); err != nil {
			return err
		}
	}

	orphanData, err := findOrphans(ctx, stack.Repo, dataDirs)
	if err != nil {
		if err := writeDoctorf(stdout, "write orphaned data failure", "Orphaned data:      FAIL -- %v\n", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: orphaned data check failed: %w", err)
	}
	if len(orphanData) > 0 {
		if err := writeDoctorf(stdout, "write orphaned data warning", "Orphaned data:      WARN -- %d orphaned data directories: %v\n", len(orphanData), orphanData); err != nil {
			return err
		}
		hasWarnings = true
	} else {
		if err := writeDoctorln(stdout, "write orphaned data success", "Orphaned data:      OK"); err != nil {
			return err
		}
	}

	// Check missing files for all tracked slides, including items currently in trash.
	slides, err := stack.Repo.ListSlides(ctx, repository.ListSlidesFilter{IncludeDeleted: true})
	if err != nil {
		return fmt.Errorf("list slides: %w", err)
	}

	missingFigs, err := checkMissingFigures(ctx, stack.Repo, stack.FS, slides)
	if err != nil {
		if err := writeDoctorf(stdout, "write missing figures failure", "Missing figures:    FAIL -- %v\n", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: missing figures check failed: %w", err)
	}

	if len(missingFigs) > 0 {
		if err := writeDoctorf(stdout, "write missing figures warning", "Missing figures:    WARN -- %d missing figure files\n", len(missingFigs)); err != nil {
			return err
		}
		for _, path := range missingFigs {
			if err := writeDoctorf(stdout, "write missing figure path", "  %s\n", path); err != nil {
				return err
			}
		}
		hasWarnings = true
	} else {
		if err := writeDoctorln(stdout, "write missing figures success", "Missing figures:    OK"); err != nil {
			return err
		}
	}

	missingDataFiles, err := checkMissingDataFiles(ctx, stack.Repo, stack.FS, slides)
	if err != nil {
		if err := writeDoctorf(stdout, "write missing data files failure", "Missing data files: FAIL -- %v\n", err); err != nil {
			return err
		}
		return fmt.Errorf("doctor: missing data files check failed: %w", err)
	}

	if len(missingDataFiles) > 0 {
		if err := writeDoctorf(stdout, "write missing data files warning", "Missing data files: WARN -- %d missing data files\n", len(missingDataFiles)); err != nil {
			return err
		}
		for _, path := range missingDataFiles {
			if err := writeDoctorf(stdout, "write missing data file path", "  %s\n", path); err != nil {
				return err
			}
		}
		hasWarnings = true
	} else {
		if err := writeDoctorln(stdout, "write missing data files success", "Missing data files: OK"); err != nil {
			return err
		}
	}

	if hasWarnings {
		return fmt.Errorf("doctor: warnings found")
	}

	if err := writeDoctorln(stdout, "write doctor success summary", "\nAll checks passed."); err != nil {
		return err
	}
	return nil
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

// findOrphans returns slide IDs from the given list that do not exist in the repository.
// Args: ctx is the context; repo is the repository; slideIDs is the list of IDs found on disk.
// Returns: a slice of orphaned slide IDs or an error when the repository lookup fails unexpectedly.
func findOrphans(ctx context.Context, repo repository.Repository, slideIDs []string) ([]string, error) {
	orphans := make([]string, 0)
	for _, id := range slideIDs {
		_, err := repo.GetSlideByID(ctx, id)
		if errors.Is(err, repository.ErrNotFound) {
			orphans = append(orphans, id)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("look up slide %s: %w", id, err)
		}
	}
	return orphans, nil
}

// checkMissingFigures checks all tracked slides for figure files that are
// recorded in the database but missing from disk.
// Args: ctx is the context; repo is the repository; fs is the filesystem client; slides is the list of slides.
// Returns: missing figure paths (as slideID/filename), or an error when inspection fails.
func checkMissingFigures(ctx context.Context, repo repository.Repository, fs *filesystem.Client, slides []repository.Slide) ([]string, error) {
	missingFigures := make([]string, 0)
	for _, slide := range slides {
		figures, err := repo.ListSlideFiguresBySlideID(ctx, slide.ID)
		if err != nil {
			return nil, fmt.Errorf("list figures for %s: %w", slide.ID, err)
		}
		for _, fig := range figures {
			path, err := fs.ResolveFigurePath(slide.ID, fig.Filename)
			if err != nil {
				return nil, fmt.Errorf("resolve figure path for %s/%s: %w", slide.ID, fig.Filename, err)
			}
			if _, err := os.Stat(path); err != nil {
				if os.IsNotExist(err) {
					missingFigures = append(missingFigures, slide.ID+"/"+fig.Filename)
					continue
				}
				return nil, fmt.Errorf("stat figure path for %s/%s: %w", slide.ID, fig.Filename, err)
			}
		}
	}
	return missingFigures, nil
}

// checkMissingDataFiles checks all tracked slides for data files that are
// recorded in the database but missing from disk.
// Args: ctx is the context; repo is the repository; fs is the filesystem client; slides is the list of slides.
// Returns: missing data file paths (as slideID/filename), or an error when inspection fails.
func checkMissingDataFiles(ctx context.Context, repo repository.Repository, fs *filesystem.Client, slides []repository.Slide) ([]string, error) {
	missingDataFiles := make([]string, 0)
	for _, slide := range slides {
		dataFiles, err := repo.ListSlideDataFilesBySlideID(ctx, slide.ID)
		if err != nil {
			return nil, fmt.Errorf("list data files for %s: %w", slide.ID, err)
		}
		for _, df := range dataFiles {
			path, err := fs.ResolveDataFilePath(slide.ID, df.Filename)
			if err != nil {
				return nil, fmt.Errorf("resolve data file path for %s/%s: %w", slide.ID, df.Filename, err)
			}
			if _, err := os.Stat(path); err != nil {
				if os.IsNotExist(err) {
					missingDataFiles = append(missingDataFiles, slide.ID+"/"+df.Filename)
					continue
				}
				return nil, fmt.Errorf("stat data file path for %s/%s: %w", slide.ID, df.Filename, err)
			}
		}
	}
	return missingDataFiles, nil
}
