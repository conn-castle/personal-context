package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newMoveCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var dateFlag string
	var posFirst bool
	var posLast bool
	var afterFlag string
	var beforeFlag string

	cmd := &cobra.Command{
		Use:   "move <id>",
		Short: "Change a slide's date and/or position",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			posExplicit := posFirst || posLast || afterFlag != "" || beforeFlag != ""

			pos, err := resolvePositionFlags(posFirst, posLast, afterFlag, beforeFlag)
			if err != nil {
				return err
			}

			return runMove(cmd.Context(), stdout, stderr, args[0], dateFlag, pos, posExplicit)
		},
	}

	cmd.Flags().StringVar(&dateFlag, "date", "", "New slide date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&posFirst, "first", false, "Move to the start of the day")
	cmd.Flags().BoolVar(&posLast, "last", false, "Move to the end of the day")
	cmd.Flags().StringVar(&afterFlag, "after", "", "Move after this slide ID")
	cmd.Flags().StringVar(&beforeFlag, "before", "", "Move before this slide ID")

	return cmd
}

func runMove(ctx context.Context, stdout io.Writer, _ io.Writer, id string, dateStr string, pos position, posExplicit bool) error {
	if dateStr == "" && !posExplicit {
		return fmt.Errorf("at least one of --date or a position flag (--first, --last, --after, --before) is required")
	}

	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	// Fail fast if slide doesn't exist
	existing, err := stack.Repo.GetSlideByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("slide %q not found", id)
		}
		return fmt.Errorf("get slide: %w", err)
	}

	// Resolve date
	dateField := existing.Date
	if dateStr != "" {
		if _, err := time.Parse("2006-01-02", dateStr); err != nil {
			return fmt.Errorf("invalid date %q: expected YYYY-MM-DD", dateStr)
		}
		dateField = dateStr
	}

	// Compute day_order (exclude current slide so it doesn't conflict with itself)
	dayOrder, err := computeDayOrder(ctx, stack.Repo, dateField, id, pos)
	if err != nil {
		return fmt.Errorf("compute position: %w", err)
	}

	// Update slide (full replacement, preserve all fields except Date and DayOrder)
	if _, err := stack.Repo.UpdateSlide(ctx, repository.UpdateSlideInput{
		ID:           id,
		Date:         dateField,
		DayOrder:     dayOrder,
		HTMLContent:  existing.HTMLContent,
		Notes:        existing.Notes,
		ProjectID:    existing.ProjectID,
		GitRemoteURL: existing.GitRemoteURL,
		GitHash:      existing.GitHash,
		DeletedAt:    existing.DeletedAt,
	}); err != nil {
		return fmt.Errorf("update slide: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "Slide %s moved\n", id)
	return nil
}
