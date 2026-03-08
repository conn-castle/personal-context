package syncengine

import (
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestResolveSlideWinnerLocalOnly(t *testing.T) {
	local := slideAt("local", time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), nil)

	got, err := ResolveSlideWinner(&local, nil)
	if err != nil {
		t.Fatalf("ResolveSlideWinner() error = %v", err)
	}
	if got != OutcomeLocal {
		t.Fatalf("ResolveSlideWinner() = %q, want %q", got, OutcomeLocal)
	}
}

func TestResolveSlideWinnerRemoteOnly(t *testing.T) {
	remote := slideAt("remote", time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), nil)

	got, err := ResolveSlideWinner(nil, &remote)
	if err != nil {
		t.Fatalf("ResolveSlideWinner() error = %v", err)
	}
	if got != OutcomeRemote {
		t.Fatalf("ResolveSlideWinner() = %q, want %q", got, OutcomeRemote)
	}
}

func TestResolveSlideWinnerLaterEditWins(t *testing.T) {
	local := slideAt("slide", time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), nil)
	remote := slideAt("slide", time.Date(2026, 3, 8, 9, 59, 59, 0, time.UTC), nil)

	got, err := ResolveSlideWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveSlideWinner() error = %v", err)
	}
	if got != OutcomeLocal {
		t.Fatalf("ResolveSlideWinner() = %q, want %q", got, OutcomeLocal)
	}
}

func TestResolveSlideWinnerLaterDeleteWins(t *testing.T) {
	remoteDeleteAt := time.Date(2026, 3, 8, 10, 1, 0, 0, time.UTC)
	local := slideAt("slide", time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), nil)
	remote := slideAt("slide", time.Date(2026, 3, 8, 9, 59, 0, 0, time.UTC), &remoteDeleteAt)

	got, err := ResolveSlideWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveSlideWinner() error = %v", err)
	}
	if got != OutcomeRemote {
		t.Fatalf("ResolveSlideWinner() = %q, want %q", got, OutcomeRemote)
	}
}

func TestResolveSlideWinnerTimestampTieEditBeatsDelete(t *testing.T) {
	tie := time.Date(2026, 3, 8, 10, 0, 0, 123000000, time.UTC)
	local := slideAt("slide", tie, nil)
	remote := slideAt("slide", tie, &tie)

	got, err := ResolveSlideWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveSlideWinner() error = %v", err)
	}
	if got != OutcomeLocal {
		t.Fatalf("ResolveSlideWinner() = %q, want %q", got, OutcomeLocal)
	}
}

func TestResolveSlideWinnerEqualStateReturnsEqual(t *testing.T) {
	updatedAt := time.Date(2026, 3, 8, 10, 0, 0, 123456789, time.UTC)
	local := slideAt("slide", updatedAt, nil)
	remote := slideAt("slide", updatedAt, nil)

	got, err := ResolveSlideWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveSlideWinner() error = %v", err)
	}
	if got != OutcomeEqual {
		t.Fatalf("ResolveSlideWinner() = %q, want %q", got, OutcomeEqual)
	}
}

func TestFilterSlidesUpdatedSinceUsesInclusiveMillisecondComparison(t *testing.T) {
	since := time.Date(2026, 3, 8, 10, 0, 0, 123400000, time.UTC)
	slides := []repository.Slide{
		slideAt("before", time.Date(2026, 3, 8, 10, 0, 0, 122999999, time.UTC), nil),
		slideAt("equal-ms", time.Date(2026, 3, 8, 10, 0, 0, 123999999, time.UTC), nil),
		slideAt("after", time.Date(2026, 3, 8, 10, 0, 0, 124000000, time.UTC), nil),
	}

	filtered := FilterSlidesUpdatedSince(slides, since)
	if len(filtered) != 2 {
		t.Fatalf("len(FilterSlidesUpdatedSince()) = %d, want 2", len(filtered))
	}
	if filtered[0].ID != "equal-ms" || filtered[1].ID != "after" {
		t.Fatalf("FilterSlidesUpdatedSince() IDs = [%s %s], want [equal-ms after]", filtered[0].ID, filtered[1].ID)
	}
}

func TestFilterSlidesUpdatedSinceRestoreRemainsVisible(t *testing.T) {
	restoreAt := time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC)
	deleteAt := time.Date(2026, 3, 8, 10, 30, 0, 0, time.UTC)
	slide := slideAt("restored", restoreAt, &deleteAt)
	slide.DeletedAt = nil

	filtered := FilterSlidesUpdatedSince([]repository.Slide{slide}, time.Date(2026, 3, 8, 10, 59, 59, 0, time.UTC))
	if len(filtered) != 1 || filtered[0].ID != "restored" {
		t.Fatalf("FilterSlidesUpdatedSince() = %#v, want restored slide", filtered)
	}
}

func TestFigureMapByFilenameRejectsDuplicates(t *testing.T) {
	figures := []repository.SlideFigure{
		{Filename: "plot.png"},
		{Filename: "plot.png"},
	}

	if _, err := FigureMapByFilename(figures); err == nil {
		t.Fatal("FigureMapByFilename() error = nil, want duplicate failure")
	}
}

func TestDataFileMapByFilenameRejectsDuplicates(t *testing.T) {
	files := []repository.SlideDataFile{
		{Filename: "metrics.csv"},
		{Filename: "metrics.csv"},
	}

	if _, err := DataFileMapByFilename(files); err == nil {
		t.Fatal("DataFileMapByFilename() error = nil, want duplicate failure")
	}
}

func TestResolveSlideWinnerBothNilReturnsError(t *testing.T) {
	_, err := ResolveSlideWinner(nil, nil)
	if err == nil {
		t.Fatal("ResolveSlideWinner(nil, nil) error = nil, want validation failure")
	}
}

func TestResolveSlideWinnerDeletedTiebreaker(t *testing.T) {
	// Same effective timestamp; local is deleted, remote is active -> remote wins (active wins).
	// To hit lines 38-42 (localAction.deleted != remoteAction.deleted), one side's
	// deletedAt must be AFTER its updatedAt so latestSlideAction returns deleted=true.
	tie := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	laterDelete := tie.Add(time.Millisecond)
	localDeleted := slideAt("slide", tie, &laterDelete)
	// latestSlideAction for localDeleted: deletedAt (tie+1ms) > updatedAt (tie), so
	// returns {when: tie+1ms, deleted: true, active: false}.

	// Remote active: latestSlideAction returns {when: tie+1ms, deleted: false, active: true}.
	// We need remote to also have when == tie+1ms. Set remote updatedAt = tie+1ms.
	remoteActive := slideAt("slide", laterDelete, nil)

	// Now both have when = tie+1ms. local deleted, remote not -> lines 38-42.
	// !localAction.deleted is false, so return OutcomeRemote.
	got, err := ResolveSlideWinner(&localDeleted, &remoteActive)
	if err != nil {
		t.Fatalf("ResolveSlideWinner() error = %v", err)
	}
	if got != OutcomeRemote {
		t.Fatalf("ResolveSlideWinner() = %q, want %q (active beats deleted at same timestamp)", got, OutcomeRemote)
	}
}

func TestResolveSlideWinnerDeletedTiebreakerLocalActive(t *testing.T) {
	// Same effective timestamp; remote is deleted, local is active -> local wins.
	tie := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	laterDelete := tie.Add(time.Millisecond)

	localActive := slideAt("slide", laterDelete, nil)
	remoteDeleted := slideAt("slide", tie, &laterDelete)

	got, err := ResolveSlideWinner(&localActive, &remoteDeleted)
	if err != nil {
		t.Fatalf("ResolveSlideWinner() error = %v", err)
	}
	if got != OutcomeLocal {
		t.Fatalf("ResolveSlideWinner() = %q, want %q (active beats deleted at same timestamp)", got, OutcomeLocal)
	}
}

func TestResolveSlideWinnerMoreActiveLocalTiebreaker(t *testing.T) {
	// Same effective timestamp, same deleted state, but local is "more active"
	// (local has nil DeletedAt, remote has non-nil DeletedAt even though action is not deleted).
	// This hits lines 43-46: slideIsMoreActive.
	tie := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)

	// Local: updatedAt = tie, no DeletedAt -> latestSlideAction: {when: tie, deleted: false, active: true}
	local := slideAt("slide", tie, nil)

	// Remote: updatedAt = tie, DeletedAt = some time BEFORE updatedAt
	// latestSlideAction: DeletedAt != nil, but deletedAt.After(updatedAt) is false,
	// so returns {when: tie, deleted: false, active: true} — same deleted/active state.
	// But slideIsMoreActive(local, remote) checks left.DeletedAt==nil && right.DeletedAt!=nil.
	earlyDelete := tie.Add(-time.Hour)
	remote := slideAt("slide", tie, &earlyDelete)

	got, err := ResolveSlideWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveSlideWinner() error = %v", err)
	}
	if got != OutcomeLocal {
		t.Fatalf("ResolveSlideWinner() = %q, want %q (local more active)", got, OutcomeLocal)
	}
}

func TestResolveSlideWinnerMoreActiveRemoteTiebreaker(t *testing.T) {
	// Mirror: remote is "more active" than local.
	tie := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	earlyDelete := tie.Add(-time.Hour)

	local := slideAt("slide", tie, &earlyDelete)
	remote := slideAt("slide", tie, nil)

	got, err := ResolveSlideWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveSlideWinner() error = %v", err)
	}
	if got != OutcomeRemote {
		t.Fatalf("ResolveSlideWinner() = %q, want %q (remote more active)", got, OutcomeRemote)
	}
}

// Note: lines 47-51 in resolve.go (localAction.active != remoteAction.active)
// are unreachable through latestSlideAction because active is always the inverse
// of deleted. When deleted values are equal, active values are always equal too.
// This is defensive code that cannot be covered without modifying the source.

func TestFilterSlidesUpdatedSinceZeroSinceReturnsCopy(t *testing.T) {
	slides := []repository.Slide{
		slideAt("a", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil),
		slideAt("b", time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC), nil),
	}

	filtered := FilterSlidesUpdatedSince(slides, time.Time{})
	if len(filtered) != 2 {
		t.Fatalf("len(FilterSlidesUpdatedSince()) = %d, want 2", len(filtered))
	}
	if filtered[0].ID != "a" || filtered[1].ID != "b" {
		t.Fatalf("FilterSlidesUpdatedSince() IDs = [%s %s], want [a b]", filtered[0].ID, filtered[1].ID)
	}

	// Verify it's a copy, not the same slice.
	filtered[0].ID = "modified"
	if slides[0].ID == "modified" {
		t.Fatal("FilterSlidesUpdatedSince() returned the same slice, not a copy")
	}
}

func TestFilterSlidesUpdatedSinceEmptySlice(t *testing.T) {
	filtered := FilterSlidesUpdatedSince(nil, time.Now())
	if len(filtered) != 0 {
		t.Fatalf("len(FilterSlidesUpdatedSince(nil)) = %d, want 0", len(filtered))
	}
}

func TestFigureMapByFilenameHappyPath(t *testing.T) {
	figures := []repository.SlideFigure{
		{Filename: "plot.png", S3Key: "s3://bucket/plot.png"},
		{Filename: "chart.svg", S3Key: "s3://bucket/chart.svg"},
	}

	indexed, err := FigureMapByFilename(figures)
	if err != nil {
		t.Fatalf("FigureMapByFilename() error = %v", err)
	}
	if len(indexed) != 2 {
		t.Fatalf("len(indexed) = %d, want 2", len(indexed))
	}
	if indexed["plot.png"].S3Key != "s3://bucket/plot.png" {
		t.Fatalf("indexed[\"plot.png\"].S3Key = %q, want %q", indexed["plot.png"].S3Key, "s3://bucket/plot.png")
	}
	if indexed["chart.svg"].S3Key != "s3://bucket/chart.svg" {
		t.Fatalf("indexed[\"chart.svg\"].S3Key = %q, want %q", indexed["chart.svg"].S3Key, "s3://bucket/chart.svg")
	}
}

func TestFigureMapByFilenameNilSlice(t *testing.T) {
	indexed, err := FigureMapByFilename(nil)
	if err != nil {
		t.Fatalf("FigureMapByFilename(nil) error = %v", err)
	}
	if len(indexed) != 0 {
		t.Fatalf("len(indexed) = %d, want 0", len(indexed))
	}
}

func TestDataFileMapByFilenameHappyPath(t *testing.T) {
	files := []repository.SlideDataFile{
		{Filename: "metrics.csv", S3Key: "s3://bucket/metrics.csv"},
		{Filename: "data.json", S3Key: "s3://bucket/data.json"},
	}

	indexed, err := DataFileMapByFilename(files)
	if err != nil {
		t.Fatalf("DataFileMapByFilename() error = %v", err)
	}
	if len(indexed) != 2 {
		t.Fatalf("len(indexed) = %d, want 2", len(indexed))
	}
	if indexed["metrics.csv"].S3Key != "s3://bucket/metrics.csv" {
		t.Fatalf("indexed[\"metrics.csv\"].S3Key = %q, want %q", indexed["metrics.csv"].S3Key, "s3://bucket/metrics.csv")
	}
}

func TestDataFileMapByFilenameNilSlice(t *testing.T) {
	indexed, err := DataFileMapByFilename(nil)
	if err != nil {
		t.Fatalf("DataFileMapByFilename(nil) error = %v", err)
	}
	if len(indexed) != 0 {
		t.Fatalf("len(indexed) = %d, want 0", len(indexed))
	}
}

func slideAt(id string, updatedAt time.Time, deletedAt *time.Time) repository.Slide {
	return repository.Slide{
		ID:        id,
		UpdatedAt: updatedAt,
		DeletedAt: deletedAt,
	}
}
