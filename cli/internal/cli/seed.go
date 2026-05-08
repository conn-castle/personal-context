package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/fractionalindex"
	"github.com/conn-castle/personal-context/cli/internal/notes"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/recordid"
	"github.com/spf13/cobra"
)

func newSeedCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Create tutorial records for development",
		Long:  "Creates 6 tutorial records under the personal-context/tutorial project. Idempotent — backfills any missing built-in tutorial records and skips only when the full set already exists.",
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

	// Check which built-in tutorial records already exist.
	existing, err := stack.Repo.ListRecords(ctx, repository.ListRecordsFilter{
		ProjectID: strPtr("personal-context/tutorial"),
	})
	if err != nil {
		return fmt.Errorf("check existing seeds: %w", err)
	}

	seeds := builtinSeeds()
	// Keyed by HTML content: if a user edits a seeded record's HTML, re-running
	// seed will not recognise it and will create a duplicate. This is a known
	// limitation of the v1 content-based identity approach. Stable seed IDs
	// would require a schema column (e.g. seed_key) and migration support.
	// See ISSUES.md t1u2v3a.
	existingByHTML := make(map[string]string, len(existing))
	for _, record := range existing {
		if record.HTMLContent != nil {
			existingByHTML[*record.HTMLContent] = record.DayOrder
		}
	}

	now := time.Now().UTC()
	prevOrder := ""
	created := 0

	for i, seed := range seeds {
		if order, ok := existingByHTML[seed.HTMLContent]; ok {
			prevOrder = order
			continue
		}

		id, err := recordid.GenerateForDate(now)
		if err != nil {
			return fmt.Errorf("generate record ID: %w", err)
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
		if _, err := stack.Repo.GetProjectByID(ctx, seed.ProjectID); err != nil {
			if !errors.Is(err, repository.ErrNotFound) {
				return fmt.Errorf("get seed project: %w", err)
			}
			if _, err := stack.Repo.CreateProject(ctx, repository.CreateRegistryInput{ID: seed.ProjectID}); err != nil {
				return fmt.Errorf("create seed project: %w", err)
			}
		}
		const seedDeviceID = "personal-context-seed"
		if _, err := stack.Repo.GetDeviceByID(ctx, seedDeviceID); err != nil {
			if !errors.Is(err, repository.ErrNotFound) {
				return fmt.Errorf("get seed device: %w", err)
			}
			if _, err := stack.Repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: seedDeviceID}); err != nil {
				return fmt.Errorf("create seed device: %w", err)
			}
		}

		input := repository.CreateRecordInput{
			ID:             id,
			Date:           now.Format("2006-01-02"),
			DayOrder:       order,
			HTMLContent:    &seed.HTMLContent,
			Notes:          normalizedNotes,
			ProjectID:      seed.ProjectID,
			SourceDeviceID: seedDeviceID,
		}

		if _, err := stack.Repo.CreateRecord(ctx, input); err != nil {
			return fmt.Errorf("create seed record %d: %w", i+1, err)
		}
		existingByHTML[seed.HTMLContent] = order
		created++

		_, _ = fmt.Fprintf(stdout, "  %s  %s\n", id, seedTitle(i))
	}

	if created == 0 {
		_, _ = fmt.Fprintln(stdout, "Tutorial records already exist — skipping seed.")
		return nil
	}

	_, _ = fmt.Fprintf(stdout, "\nCreated %d tutorial records (project: personal-context/tutorial)\n", created)
	return nil
}

// seedTitle returns a human-readable title for each seed record by index.
func seedTitle(i int) string {
	titles := []string{
		"Welcome to Personal Context",
		"Adding Records",
		"Managing Records",
		"Projects",
		"Web UI",
		"Cloud Sync & Backup",
	}
	if i < len(titles) {
		return titles[i]
	}
	return fmt.Sprintf("Record %d", i+1)
}
