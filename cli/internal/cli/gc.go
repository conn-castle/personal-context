package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newGCCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Hard-delete trash older than 30 days",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGC(cmd.Context(), stdout, stderr)
		},
	}
	return cmd
}

func runGC(ctx context.Context, stdout io.Writer, _ io.Writer) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	slides, err := stack.Repo.ListSlides(ctx, repository.ListSlidesFilter{OnlyDeleted: true})
	if err != nil {
		return fmt.Errorf("list deleted slides: %w", err)
	}

	const gcThreshold = 30 * 24 * time.Hour

	var expired []repository.Slide
	now := time.Now()
	for _, s := range slides {
		if s.DeletedAt != nil && now.Sub(*s.DeletedAt) > gcThreshold {
			expired = append(expired, s)
		}
	}

	if len(expired) == 0 {
		_, _ = fmt.Fprintln(stdout, "No expired trash to clean up.")
		return nil
	}

	removed := 0
	for _, s := range expired {
		if err := stack.Repo.DeleteSlide(ctx, s.ID); err != nil {
			return fmt.Errorf("hard delete slide %s: %w", s.ID, err)
		}
		if err := stack.FS.DeleteSlideDir(s.ID); err != nil {
			_, _ = fmt.Fprintf(stdout, "Warning: failed to remove files for slide %s: %v\n", s.ID, err)
			continue
		}
		removed++
		_, _ = fmt.Fprintf(stdout, "Deleted %s\n", s.ID)
	}

	_, _ = fmt.Fprintf(stdout, "Removed %d slide(s).\n", removed)
	return nil
}
