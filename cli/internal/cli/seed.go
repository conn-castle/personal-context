package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/fractionalindex"
	"github.com/conn-castle/personal-context/cli/internal/notes"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/slideid"
	"github.com/spf13/cobra"
)

func newSeedCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Create tutorial slides for development",
		Long:  "Creates 6 tutorial slides under the personal-context/tutorial project. Idempotent — backfills any missing built-in tutorial slides and skips only when the full set already exists.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSeed(cmd.Context(), stdout, stderr)
		},
	}
	return cmd
}

func runSeed(ctx context.Context, stdout io.Writer, _ io.Writer) error {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	// Check which built-in tutorial slides already exist.
	existing, err := stack.Repo.ListSlides(ctx, repository.ListSlidesFilter{
		ProjectID: strPtr("personal-context/tutorial"),
	})
	if err != nil {
		return fmt.Errorf("check existing seeds: %w", err)
	}

	seeds := builtinSeeds()
	existingByHTML := make(map[string]string, len(existing))
	for _, slide := range existing {
		existingByHTML[slide.HTMLContent] = slide.DayOrder
	}

	now := time.Now().UTC()
	prevOrder := ""
	created := 0

	for i, seed := range seeds {
		if order, ok := existingByHTML[seed.HTMLContent]; ok {
			prevOrder = order
			continue
		}

		id, err := slideid.GenerateForDate(now)
		if err != nil {
			return fmt.Errorf("generate slide ID: %w", err)
		}

		nextOrder := ""
		for j := i + 1; j < len(seeds); j++ {
			if order, ok := existingByHTML[seeds[j].HTMLContent]; ok {
				nextOrder = order
				break
			}
		}

		order, idxErr := fractionalindex.GenerateBetween(prevOrder, nextOrder)
		if idxErr != nil {
			switch {
			case prevOrder == "":
				order = "a0"
			case nextOrder == "":
				order = prevOrder + "V"
			default:
				return fmt.Errorf("compute seed order: %w", idxErr)
			}
		}
		prevOrder = order

		normalizedNotes := notes.NormalizeString(seed.Notes)

		input := repository.CreateSlideInput{
			ID:          id,
			Date:        now.Format("2006-01-02"),
			DayOrder:    order,
			HTMLContent: seed.HTMLContent,
			Notes:       normalizedNotes,
			ProjectID:   &seed.ProjectID,
		}

		if _, err := stack.Repo.CreateSlide(ctx, input); err != nil {
			return fmt.Errorf("create seed slide %d: %w", i+1, err)
		}
		existingByHTML[seed.HTMLContent] = order
		created++

		_, _ = fmt.Fprintf(stdout, "  %s  %s\n", id, seedTitle(i))
	}

	if created == 0 {
		_, _ = fmt.Fprintln(stdout, "Tutorial slides already exist — skipping seed.")
		return nil
	}

	_, _ = fmt.Fprintf(stdout, "\nCreated %d tutorial slides (project: personal-context/tutorial)\n", created)
	return nil
}

// seedTitle returns a human-readable title for each seed slide by index.
func seedTitle(i int) string {
	titles := []string{
		"Welcome to Personal Context",
		"Adding Slides",
		"Managing Slides",
		"Projects",
		"Web UI",
		"Cloud Sync & Backup",
	}
	if i < len(titles) {
		return titles[i]
	}
	return fmt.Sprintf("Slide %d", i+1)
}
