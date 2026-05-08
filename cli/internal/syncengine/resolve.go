package syncengine

import (
	"fmt"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

// Outcome states which side should win a conflict comparison.
type Outcome string

const (
	OutcomeEqual  Outcome = "equal"
	OutcomeLocal  Outcome = "local"
	OutcomeRemote Outcome = "remote"
)

// ResolveRecordWinner compares local and remote record states using the Phase 6 rules.
func ResolveRecordWinner(local *repository.Record, remote *repository.Record) (Outcome, error) {
	switch {
	case local == nil && remote == nil:
		return "", fmt.Errorf("at least one record is required")
	case local == nil:
		return OutcomeRemote, nil
	case remote == nil:
		return OutcomeLocal, nil
	}

	localAction := latestRecordAction(*local)
	remoteAction := latestRecordAction(*remote)

	switch {
	case localAction.when.After(remoteAction.when):
		return OutcomeLocal, nil
	case remoteAction.when.After(localAction.when):
		return OutcomeRemote, nil
	case localAction.deleted != remoteAction.deleted:
		// Edit wins over delete at the same timestamp.
		if !localAction.deleted {
			return OutcomeLocal, nil
		}
		return OutcomeRemote, nil
	case recordIsMoreActive(*local, *remote):
		// Both classified as edits (tie rule), but one still has DeletedAt set.
		// Prefer the truly active record.
		return OutcomeLocal, nil
	case recordIsMoreActive(*remote, *local):
		return OutcomeRemote, nil
	default:
		return OutcomeEqual, nil
	}
}

// FilterRecordsUpdatedSince applies the inclusive millisecond-precision sync cursor filter.
func FilterRecordsUpdatedSince(records []repository.Record, since time.Time) []repository.Record {
	if since.IsZero() {
		filtered := make([]repository.Record, len(records))
		copy(filtered, records)
		return filtered
	}

	threshold := truncateToMillisecond(since)
	filtered := make([]repository.Record, 0, len(records))
	for _, record := range records {
		if !truncateToMillisecond(record.UpdatedAt).Before(threshold) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

// FigureMapByFilename indexes child rows by filename for sync matching.
func FigureMapByFilename(figures []repository.RecordFigure) (map[string]repository.RecordFigure, error) {
	indexed := make(map[string]repository.RecordFigure, len(figures))
	for _, figure := range figures {
		if _, exists := indexed[figure.Filename]; exists {
			return nil, fmt.Errorf("duplicate figure filename %q", figure.Filename)
		}
		indexed[figure.Filename] = figure
	}
	return indexed, nil
}

// DataFileMapByFilename indexes data-file rows by filename for sync matching.
func DataFileMapByFilename(files []repository.RecordDataFile) (map[string]repository.RecordDataFile, error) {
	indexed := make(map[string]repository.RecordDataFile, len(files))
	for _, file := range files {
		if _, exists := indexed[file.Filename]; exists {
			return nil, fmt.Errorf("duplicate data file filename %q", file.Filename)
		}
		indexed[file.Filename] = file
	}
	return indexed, nil
}

type recordAction struct {
	when    time.Time
	deleted bool
}

func latestRecordAction(record repository.Record) recordAction {
	updatedAt := truncateToMillisecond(record.UpdatedAt)
	if record.DeletedAt == nil {
		return recordAction{when: updatedAt, deleted: false}
	}

	deletedAt := truncateToMillisecond(record.DeletedAt.UTC())
	if deletedAt.After(updatedAt) {
		return recordAction{when: deletedAt, deleted: true}
	}

	// Timestamp ties are resolved in favor of edits, not deletes.
	return recordAction{when: updatedAt, deleted: false}
}

// recordIsMoreActive returns true when left is active and right has a DeletedAt set.
// This handles the case where latestRecordAction classifies both as "edits" (tie rule)
// but their raw record states differ.
func recordIsMoreActive(left repository.Record, right repository.Record) bool {
	return left.DeletedAt == nil && right.DeletedAt != nil
}
