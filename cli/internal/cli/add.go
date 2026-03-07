package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/fractionalindex"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/slideid"
	"github.com/conn-castle/personal-context/cli/internal/slideio"
	"github.com/spf13/cobra"
)

func newAddCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var dateFlag string
	var projectFlag string
	var posFirst bool
	var posLast bool
	var afterFlag string
	var beforeFlag string

	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Create a slide from an input folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pos, err := resolvePositionFlags(posFirst, posLast, afterFlag, beforeFlag)
			if err != nil {
				return err
			}
			return runAdd(cmd.Context(), stdout, stderr, args[0], dateFlag, projectFlag, pos)
		},
	}

	cmd.Flags().StringVar(&dateFlag, "date", "", "Slide date (YYYY-MM-DD, default: today)")
	cmd.Flags().StringVar(&projectFlag, "project", "", "Project ID (overrides metadata.json)")
	cmd.Flags().BoolVar(&posFirst, "first", false, "Insert at the start of the day")
	cmd.Flags().BoolVar(&posLast, "last", false, "Insert at the end of the day")
	cmd.Flags().StringVar(&afterFlag, "after", "", "Insert after this slide ID")
	cmd.Flags().StringVar(&beforeFlag, "before", "", "Insert before this slide ID")

	return cmd
}

// position represents a resolved position for day_order computation.
type position struct {
	kind        string // "first", "last", "after", "before", or "" (default=last)
	referenceID string
}

func resolvePositionFlags(first, last bool, after, before string) (position, error) {
	count := 0
	if first {
		count++
	}
	if last {
		count++
	}
	if after != "" {
		count++
	}
	if before != "" {
		count++
	}
	if count > 1 {
		return position{}, fmt.Errorf("only one position flag allowed (--first, --last, --after, --before)")
	}

	switch {
	case first:
		return position{kind: "first"}, nil
	case last:
		return position{kind: "last"}, nil
	case after != "":
		return position{kind: "after", referenceID: after}, nil
	case before != "":
		return position{kind: "before", referenceID: before}, nil
	default:
		return position{kind: "last"}, nil
	}
}

func runAdd(ctx context.Context, stdout io.Writer, _ io.Writer, inputPath string, dateStr string, projectOverride string, pos position) (err error) {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	var createdSlideID string
	copiedFigures := make([]string, 0)
	copiedDataFiles := make([]string, 0)
	defer func() {
		if err == nil || createdSlideID == "" {
			return
		}
		rollbackCtx := context.Background()
		_ = stack.Repo.DeleteSlide(rollbackCtx, createdSlideID)
		for _, filename := range copiedFigures {
			_ = stack.FS.DeleteFigure(createdSlideID, filename)
		}
		for _, filename := range copiedDataFiles {
			_ = stack.FS.DeleteDataFile(createdSlideID, filename)
		}
	}()

	input, err := slideio.ParseInputFolder(inputPath)
	if err != nil {
		return err
	}

	// Resolve project_id: --project flag overrides metadata.json
	if projectOverride != "" {
		input.ProjectID = &projectOverride
	}

	// Resolve date
	now := time.Now()
	var slideDate time.Time
	if dateStr != "" {
		parsed, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return fmt.Errorf("invalid date %q: expected YYYY-MM-DD", dateStr)
		}
		slideDate = parsed
	} else {
		slideDate = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	}

	// Generate slide ID using UTC time
	id, err := slideid.GenerateForDate(now)
	if err != nil {
		return fmt.Errorf("generate slide ID: %w", err)
	}

	dateField := slideDate.Format("2006-01-02")

	// Compute day_order
	dayOrder, err := computeDayOrder(ctx, stack.Repo, dateField, id, pos)
	if err != nil {
		return fmt.Errorf("compute position: %w", err)
	}

	// Create slide in DB
	slide, err := stack.Repo.CreateSlide(ctx, repository.CreateSlideInput{
		ID:           id,
		Date:         dateField,
		DayOrder:     dayOrder,
		HTMLContent:  input.HTMLContent,
		Notes:        input.Notes,
		ProjectID:    input.ProjectID,
		GitRemoteURL: input.GitRemoteURL,
		GitHash:      input.GitHash,
	})
	if err != nil {
		return fmt.Errorf("create slide: %w", err)
	}
	createdSlideID = slide.ID

	// Copy figures
	for _, figurePath := range input.Figures {
		stored, err := stack.FS.CopyFigure(slide.ID, figurePath)
		if err != nil {
			return fmt.Errorf("copy figure %s: %w", filepath.Base(figurePath), err)
		}
		copiedFigures = append(copiedFigures, stored.Filename)
		if _, err := stack.Repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
			SlideID:  slide.ID,
			Filename: stored.Filename,
			S3Key:    stored.S3Key,
		}); err != nil {
			return fmt.Errorf("create figure record %s: %w", stored.Filename, err)
		}
	}

	// Copy data files
	for _, dataPath := range input.DataFiles {
		stored, err := stack.FS.CopyDataFile(slide.ID, dataPath)
		if err != nil {
			return fmt.Errorf("copy data file %s: %w", filepath.Base(dataPath), err)
		}
		copiedDataFiles = append(copiedDataFiles, stored.Filename)
		hash, err := slideio.HashFile(dataPath)
		if err != nil {
			return fmt.Errorf("hash data file %s: %w", stored.Filename, err)
		}
		if _, err := stack.Repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
			SlideID:  slide.ID,
			Filename: stored.Filename,
			S3Key:    stored.S3Key,
			Size:     stored.Size,
			Hash:     hash,
		}); err != nil {
			return fmt.Errorf("create data file record %s: %w", stored.Filename, err)
		}
	}

	_, _ = fmt.Fprintln(stdout, slide.ID)
	return nil
}

// computeDayOrder determines the fractional index for the slide within a given date.
func computeDayOrder(ctx context.Context, repo repository.Repository, date string, excludeID string, pos position) (string, error) {
	slides, err := repo.ListSlides(ctx, repository.ListSlidesFilter{
		DateFrom: &date,
		DateTo:   &date,
	})
	if err != nil {
		return "", fmt.Errorf("list slides for date %s: %w", date, err)
	}

	// Filter out the slide being moved (for pc move)
	filtered := make([]repository.Slide, 0, len(slides))
	for _, s := range slides {
		if s.ID != excludeID {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) == 0 {
		return fractionalindex.GenerateAtEnd("")
	}

	switch pos.kind {
	case "first":
		return fractionalindex.GenerateAtStart(filtered[0].DayOrder)
	case "last", "":
		return fractionalindex.GenerateAtEnd(filtered[len(filtered)-1].DayOrder)
	case "after":
		for i, s := range filtered {
			if s.ID == pos.referenceID {
				if i == len(filtered)-1 {
					return fractionalindex.GenerateAtEnd(s.DayOrder)
				}
				return fractionalindex.GenerateBetween(s.DayOrder, filtered[i+1].DayOrder)
			}
		}
		return "", fmt.Errorf("reference slide %q not found on date %s", pos.referenceID, date)
	case "before":
		for i, s := range filtered {
			if s.ID == pos.referenceID {
				if i == 0 {
					return fractionalindex.GenerateAtStart(s.DayOrder)
				}
				return fractionalindex.GenerateBetween(filtered[i-1].DayOrder, s.DayOrder)
			}
		}
		return "", fmt.Errorf("reference slide %q not found on date %s", pos.referenceID, date)
	default:
		return "", fmt.Errorf("unknown position kind: %q", pos.kind)
	}
}
