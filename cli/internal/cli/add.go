package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/fractionalindex"
	"github.com/conn-castle/personal-context/cli/internal/recordid"
	"github.com/conn-castle/personal-context/cli/internal/recordio"
	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/spf13/cobra"
)

func newAddCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var dateFlag string
	var projectFlag string
	var deviceFlag string
	var sourceRefFlag string
	var posFirst bool
	var posLast bool
	var afterFlag string
	var beforeFlag string

	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Create a record from an input folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pos, err := resolvePositionFlags(posFirst, posLast, afterFlag, beforeFlag)
			if err != nil {
				return err
			}
			return runAdd(cmd.Context(), stdout, stderr, args[0], dateFlag, projectFlag, deviceFlag, sourceRefFlag, pos)
		},
	}

	cmd.Flags().StringVar(&dateFlag, "date", "", "Record date (YYYY-MM-DD, default: today)")
	cmd.Flags().StringVar(&projectFlag, "project", "", "Project ID (must match metadata.json when both are present)")
	cmd.Flags().StringVar(&deviceFlag, "device", "", "Source device ID (must match metadata.json when both are present)")
	cmd.Flags().StringVar(&sourceRefFlag, "source-ref", "", "Opaque source reference (must match metadata.json when both are present)")
	cmd.Flags().BoolVar(&posFirst, "first", false, "Insert at the start of the day")
	cmd.Flags().BoolVar(&posLast, "last", false, "Insert at the end of the day")
	cmd.Flags().StringVar(&afterFlag, "after", "", "Insert after this record ID")
	cmd.Flags().StringVar(&beforeFlag, "before", "", "Insert before this record ID")

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

func runAdd(ctx context.Context, stdout io.Writer, stderr io.Writer, inputPath string, dateStr string, projectOverride string, deviceOverride string, sourceRefOverride string, pos position) (err error) {
	homeDir, err := resolveHomeDir()
	if err != nil {
		return err
	}

	stack, err := openLocalStack(homeDir)
	if err != nil {
		return err
	}
	defer func() { _ = stack.Close() }()

	var createdRecordID string
	copiedFigures := make([]string, 0)
	copiedDataFiles := make([]string, 0)
	defer func() {
		if err == nil || createdRecordID == "" {
			return
		}
		rollbackCtx := context.Background()
		_ = stack.Repo.DeleteRecord(rollbackCtx, createdRecordID)
		for _, filename := range copiedFigures {
			_ = stack.FS.DeleteFigure(createdRecordID, filename)
		}
		for _, filename := range copiedDataFiles {
			_ = stack.FS.DeleteDataFile(createdRecordID, filename)
		}
	}()

	input, err := recordio.ParseInputFolder(inputPath)
	if err != nil {
		return err
	}

	projectID, deviceID, sourceRef, err := resolveRecordProvenance(input.ProjectID, input.SourceDeviceID, input.SourceRef, projectOverride, deviceOverride, sourceRefOverride)
	if err != nil {
		return err
	}
	if err := validateActiveProjectAndDevice(ctx, stack.Repo, projectID, deviceID); err != nil {
		return err
	}

	// Resolve date
	now := time.Now()
	var recordDate time.Time
	if dateStr != "" {
		parsed, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return fmt.Errorf("invalid date %q: expected YYYY-MM-DD", dateStr)
		}
		recordDate = parsed
	} else {
		recordDate = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	}

	// Generate record ID. The record's date field (set below from recordDate, a
	// local calendar day) is the source of truth. The ID's date prefix is
	// UTC-based (recordid.GenerateForDate) and may differ from the date field by
	// a day near midnight; they are intentionally not required to match.
	id, err := recordid.GenerateForDate(recordDate)
	if err != nil {
		return fmt.Errorf("generate record ID: %w", err)
	}

	dateField := recordDate.Format("2006-01-02")

	// Compute day_order
	dayOrder, err := computeDayOrder(ctx, stack.Repo, dateField, id, pos)
	if err != nil {
		return fmt.Errorf("compute position: %w", err)
	}

	// Create record in DB
	record, err := stack.Repo.CreateRecord(ctx, repository.CreateRecordInput{
		ID:             id,
		Date:           dateField,
		DayOrder:       dayOrder,
		HTMLContent:    input.HTMLContent,
		Notes:          input.Notes,
		ProjectID:      projectID,
		SourceDeviceID: deviceID,
		SourceRef:      sourceRef,
		GitRemoteURL:   input.GitRemoteURL,
		GitHash:        input.GitHash,
	})
	if err != nil {
		return fmt.Errorf("create record: %w", err)
	}
	createdRecordID = record.ID

	// Copy figures
	for _, figurePath := range input.Figures {
		stored, err := stack.FS.CopyFigure(record.ID, figurePath)
		if err != nil {
			return fmt.Errorf("copy figure %s: %w", filepath.Base(figurePath), err)
		}
		copiedFigures = append(copiedFigures, stored.Filename)
		if _, err := stack.Repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
			RecordID: record.ID,
			Filename: stored.Filename,
			S3Key:    stored.S3Key,
		}); err != nil {
			return fmt.Errorf("create figure record %s: %w", stored.Filename, err)
		}
	}

	// Copy data files
	for _, dataPath := range input.DataFiles {
		stored, err := stack.FS.CopyDataFile(record.ID, dataPath)
		if err != nil {
			return fmt.Errorf("copy data file %s: %w", filepath.Base(dataPath), err)
		}
		copiedDataFiles = append(copiedDataFiles, stored.Filename)
		hash, err := recordio.HashFile(dataPath)
		if err != nil {
			return fmt.Errorf("hash data file %s: %w", stored.Filename, err)
		}
		if _, err := stack.Repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
			RecordID: record.ID,
			Filename: stored.Filename,
			S3Key:    stored.S3Key,
			Size:     stored.Size,
			Hash:     hash,
		}); err != nil {
			return fmt.Errorf("create data file record %s: %w", stored.Filename, err)
		}
	}

	_ = runAutoSyncFn(ctx, stderr)
	_, _ = fmt.Fprintln(stdout, record.ID)
	return nil
}

// computeDayOrder determines the fractional index for the record within a given date.
func computeDayOrder(ctx context.Context, repo repository.Repository, date string, excludeID string, pos position) (string, error) {
	records, err := repo.ListRecords(ctx, repository.ListRecordsFilter{
		DateFrom: &date,
		DateTo:   &date,
	})
	if err != nil {
		return "", fmt.Errorf("list records for date %s: %w", date, err)
	}

	// Filter out the record being moved (for pc move)
	filtered := make([]repository.Record, 0, len(records))
	for _, s := range records {
		if s.ID != excludeID {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) == 0 {
		if pos.kind == "after" || pos.kind == "before" {
			return "", fmt.Errorf("reference record %q not found on date %s", pos.referenceID, date)
		}
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
		return "", fmt.Errorf("reference record %q not found on date %s", pos.referenceID, date)
	case "before":
		for i, s := range filtered {
			if s.ID == pos.referenceID {
				if i == 0 {
					return fractionalindex.GenerateAtStart(s.DayOrder)
				}
				return fractionalindex.GenerateBetween(filtered[i-1].DayOrder, s.DayOrder)
			}
		}
		return "", fmt.Errorf("reference record %q not found on date %s", pos.referenceID, date)
	default:
		return "", fmt.Errorf("unknown position kind: %q", pos.kind)
	}
}
