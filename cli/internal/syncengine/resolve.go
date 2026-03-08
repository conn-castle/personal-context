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

// ResolveSlideWinner compares local and remote slide states using the Phase 6 rules.
func ResolveSlideWinner(local *repository.Slide, remote *repository.Slide) (Outcome, error) {
	switch {
	case local == nil && remote == nil:
		return "", fmt.Errorf("at least one slide is required")
	case local == nil:
		return OutcomeRemote, nil
	case remote == nil:
		return OutcomeLocal, nil
	}

	localAction := latestSlideAction(*local)
	remoteAction := latestSlideAction(*remote)

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
	case slideIsMoreActive(*local, *remote):
		// Both classified as edits (tie rule), but one still has DeletedAt set.
		// Prefer the truly active slide.
		return OutcomeLocal, nil
	case slideIsMoreActive(*remote, *local):
		return OutcomeRemote, nil
	default:
		return OutcomeEqual, nil
	}
}

// FilterSlidesUpdatedSince applies the inclusive millisecond-precision sync cursor filter.
func FilterSlidesUpdatedSince(slides []repository.Slide, since time.Time) []repository.Slide {
	if since.IsZero() {
		filtered := make([]repository.Slide, len(slides))
		copy(filtered, slides)
		return filtered
	}

	threshold := truncateToMillisecond(since)
	filtered := make([]repository.Slide, 0, len(slides))
	for _, slide := range slides {
		if !truncateToMillisecond(slide.UpdatedAt).Before(threshold) {
			filtered = append(filtered, slide)
		}
	}
	return filtered
}

// FigureMapByFilename indexes child rows by filename for sync matching.
func FigureMapByFilename(figures []repository.SlideFigure) (map[string]repository.SlideFigure, error) {
	indexed := make(map[string]repository.SlideFigure, len(figures))
	for _, figure := range figures {
		if _, exists := indexed[figure.Filename]; exists {
			return nil, fmt.Errorf("duplicate figure filename %q", figure.Filename)
		}
		indexed[figure.Filename] = figure
	}
	return indexed, nil
}

// DataFileMapByFilename indexes data-file rows by filename for sync matching.
func DataFileMapByFilename(files []repository.SlideDataFile) (map[string]repository.SlideDataFile, error) {
	indexed := make(map[string]repository.SlideDataFile, len(files))
	for _, file := range files {
		if _, exists := indexed[file.Filename]; exists {
			return nil, fmt.Errorf("duplicate data file filename %q", file.Filename)
		}
		indexed[file.Filename] = file
	}
	return indexed, nil
}

type slideAction struct {
	when    time.Time
	deleted bool
}

func latestSlideAction(slide repository.Slide) slideAction {
	updatedAt := truncateToMillisecond(slide.UpdatedAt)
	if slide.DeletedAt == nil {
		return slideAction{when: updatedAt, deleted: false}
	}

	deletedAt := truncateToMillisecond(slide.DeletedAt.UTC())
	if deletedAt.After(updatedAt) {
		return slideAction{when: deletedAt, deleted: true}
	}

	// Timestamp ties are resolved in favor of edits, not deletes.
	return slideAction{when: updatedAt, deleted: false}
}

// slideIsMoreActive returns true when left is active and right has a DeletedAt set.
// This handles the case where latestSlideAction classifies both as "edits" (tie rule)
// but their raw slide states differ.
func slideIsMoreActive(left repository.Slide, right repository.Slide) bool {
	return left.DeletedAt == nil && right.DeletedAt != nil
}
