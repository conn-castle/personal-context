package syncengine

import (
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestResolveRecordWinnerLocalOnly(t *testing.T) {
	local := recordAt("local", time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), nil)

	got, err := ResolveRecordWinner(&local, nil)
	if err != nil {
		t.Fatalf("ResolveRecordWinner() error = %v", err)
	}
	if got != OutcomeLocal {
		t.Fatalf("ResolveRecordWinner() = %q, want %q", got, OutcomeLocal)
	}
}

func TestResolveRecordWinnerRemoteOnly(t *testing.T) {
	remote := recordAt("remote", time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), nil)

	got, err := ResolveRecordWinner(nil, &remote)
	if err != nil {
		t.Fatalf("ResolveRecordWinner() error = %v", err)
	}
	if got != OutcomeRemote {
		t.Fatalf("ResolveRecordWinner() = %q, want %q", got, OutcomeRemote)
	}
}

func TestResolveRecordWinnerLaterEditWins(t *testing.T) {
	local := recordAt("record", time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), nil)
	remote := recordAt("record", time.Date(2026, 3, 8, 9, 59, 59, 0, time.UTC), nil)

	got, err := ResolveRecordWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveRecordWinner() error = %v", err)
	}
	if got != OutcomeLocal {
		t.Fatalf("ResolveRecordWinner() = %q, want %q", got, OutcomeLocal)
	}
}

func TestResolveRecordWinnerLaterDeleteWins(t *testing.T) {
	remoteDeleteAt := time.Date(2026, 3, 8, 10, 1, 0, 0, time.UTC)
	local := recordAt("record", time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC), nil)
	remote := recordAt("record", time.Date(2026, 3, 8, 9, 59, 0, 0, time.UTC), &remoteDeleteAt)

	got, err := ResolveRecordWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveRecordWinner() error = %v", err)
	}
	if got != OutcomeRemote {
		t.Fatalf("ResolveRecordWinner() = %q, want %q", got, OutcomeRemote)
	}
}

func TestResolveRecordWinnerTimestampTieEditBeatsDelete(t *testing.T) {
	tie := time.Date(2026, 3, 8, 10, 0, 0, 123000000, time.UTC)
	local := recordAt("record", tie, nil)
	remote := recordAt("record", tie, &tie)

	got, err := ResolveRecordWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveRecordWinner() error = %v", err)
	}
	if got != OutcomeLocal {
		t.Fatalf("ResolveRecordWinner() = %q, want %q", got, OutcomeLocal)
	}
}

func TestResolveRecordWinnerEqualStateReturnsEqual(t *testing.T) {
	updatedAt := time.Date(2026, 3, 8, 10, 0, 0, 123456789, time.UTC)
	local := recordAt("record", updatedAt, nil)
	remote := recordAt("record", updatedAt, nil)

	got, err := ResolveRecordWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveRecordWinner() error = %v", err)
	}
	if got != OutcomeEqual {
		t.Fatalf("ResolveRecordWinner() = %q, want %q", got, OutcomeEqual)
	}
}

func TestFilterRecordsUpdatedSinceUsesInclusiveMillisecondComparison(t *testing.T) {
	since := time.Date(2026, 3, 8, 10, 0, 0, 123400000, time.UTC)
	records := []repository.Record{
		recordAt("before", time.Date(2026, 3, 8, 10, 0, 0, 122999999, time.UTC), nil),
		recordAt("equal-ms", time.Date(2026, 3, 8, 10, 0, 0, 123999999, time.UTC), nil),
		recordAt("after", time.Date(2026, 3, 8, 10, 0, 0, 124000000, time.UTC), nil),
	}

	filtered := FilterRecordsUpdatedSince(records, since)
	if len(filtered) != 2 {
		t.Fatalf("len(FilterRecordsUpdatedSince()) = %d, want 2", len(filtered))
	}
	if filtered[0].ID != "equal-ms" || filtered[1].ID != "after" {
		t.Fatalf("FilterRecordsUpdatedSince() IDs = [%s %s], want [equal-ms after]", filtered[0].ID, filtered[1].ID)
	}
}

func TestFilterRecordsUpdatedSinceRestoreRemainsVisible(t *testing.T) {
	restoreAt := time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC)
	deleteAt := time.Date(2026, 3, 8, 10, 30, 0, 0, time.UTC)
	record := recordAt("restored", restoreAt, &deleteAt)
	record.DeletedAt = nil

	filtered := FilterRecordsUpdatedSince([]repository.Record{record}, time.Date(2026, 3, 8, 10, 59, 59, 0, time.UTC))
	if len(filtered) != 1 || filtered[0].ID != "restored" {
		t.Fatalf("FilterRecordsUpdatedSince() = %#v, want restored record", filtered)
	}
}

func TestFigureMapByFilenameRejectsDuplicates(t *testing.T) {
	figures := []repository.RecordFigure{
		{Filename: "plot.png"},
		{Filename: "plot.png"},
	}

	if _, err := FigureMapByFilename(figures); err == nil {
		t.Fatal("FigureMapByFilename() error = nil, want duplicate failure")
	}
}

func TestDataFileMapByFilenameRejectsDuplicates(t *testing.T) {
	files := []repository.RecordDataFile{
		{Filename: "metrics.csv"},
		{Filename: "metrics.csv"},
	}

	if _, err := DataFileMapByFilename(files); err == nil {
		t.Fatal("DataFileMapByFilename() error = nil, want duplicate failure")
	}
}

func TestResolveRecordWinnerBothNilReturnsError(t *testing.T) {
	_, err := ResolveRecordWinner(nil, nil)
	if err == nil {
		t.Fatal("ResolveRecordWinner(nil, nil) error = nil, want validation failure")
	}
}

func TestResolveRecordWinnerDeletedTiebreaker(t *testing.T) {
	// Same effective timestamp; local is deleted, remote is active -> remote wins (active wins).
	// To hit lines 38-42 (localAction.deleted != remoteAction.deleted), one side's
	// deletedAt must be AFTER its updatedAt so latestRecordAction returns deleted=true.
	tie := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	laterDelete := tie.Add(time.Millisecond)
	localDeleted := recordAt("record", tie, &laterDelete)
	// latestRecordAction for localDeleted: deletedAt (tie+1ms) > updatedAt (tie), so
	// returns {when: tie+1ms, deleted: true, active: false}.

	// Remote active: latestRecordAction returns {when: tie+1ms, deleted: false, active: true}.
	// We need remote to also have when == tie+1ms. Set remote updatedAt = tie+1ms.
	remoteActive := recordAt("record", laterDelete, nil)

	// Now both have when = tie+1ms. local deleted, remote not -> lines 38-42.
	// !localAction.deleted is false, so return OutcomeRemote.
	got, err := ResolveRecordWinner(&localDeleted, &remoteActive)
	if err != nil {
		t.Fatalf("ResolveRecordWinner() error = %v", err)
	}
	if got != OutcomeRemote {
		t.Fatalf("ResolveRecordWinner() = %q, want %q (active beats deleted at same timestamp)", got, OutcomeRemote)
	}
}

func TestResolveRecordWinnerDeletedTiebreakerLocalActive(t *testing.T) {
	// Same effective timestamp; remote is deleted, local is active -> local wins.
	tie := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	laterDelete := tie.Add(time.Millisecond)

	localActive := recordAt("record", laterDelete, nil)
	remoteDeleted := recordAt("record", tie, &laterDelete)

	got, err := ResolveRecordWinner(&localActive, &remoteDeleted)
	if err != nil {
		t.Fatalf("ResolveRecordWinner() error = %v", err)
	}
	if got != OutcomeLocal {
		t.Fatalf("ResolveRecordWinner() = %q, want %q (active beats deleted at same timestamp)", got, OutcomeLocal)
	}
}

func TestResolveRecordWinnerMoreActiveLocalTiebreaker(t *testing.T) {
	// Same effective timestamp, same deleted state, but local is "more active"
	// (local has nil DeletedAt, remote has non-nil DeletedAt even though action is not deleted).
	// This hits lines 43-46: recordIsMoreActive.
	tie := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)

	// Local: updatedAt = tie, no DeletedAt -> latestRecordAction: {when: tie, deleted: false, active: true}
	local := recordAt("record", tie, nil)

	// Remote: updatedAt = tie, DeletedAt = some time BEFORE updatedAt
	// latestRecordAction: DeletedAt != nil, but deletedAt.After(updatedAt) is false,
	// so returns {when: tie, deleted: false, active: true} — same deleted/active state.
	// But recordIsMoreActive(local, remote) checks left.DeletedAt==nil && right.DeletedAt!=nil.
	earlyDelete := tie.Add(-time.Hour)
	remote := recordAt("record", tie, &earlyDelete)

	got, err := ResolveRecordWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveRecordWinner() error = %v", err)
	}
	if got != OutcomeLocal {
		t.Fatalf("ResolveRecordWinner() = %q, want %q (local more active)", got, OutcomeLocal)
	}
}

func TestResolveRecordWinnerMoreActiveRemoteTiebreaker(t *testing.T) {
	// Mirror: remote is "more active" than local.
	tie := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	earlyDelete := tie.Add(-time.Hour)

	local := recordAt("record", tie, &earlyDelete)
	remote := recordAt("record", tie, nil)

	got, err := ResolveRecordWinner(&local, &remote)
	if err != nil {
		t.Fatalf("ResolveRecordWinner() error = %v", err)
	}
	if got != OutcomeRemote {
		t.Fatalf("ResolveRecordWinner() = %q, want %q (remote more active)", got, OutcomeRemote)
	}
}

func TestFilterRecordsUpdatedSinceZeroSinceReturnsCopy(t *testing.T) {
	records := []repository.Record{
		recordAt("a", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil),
		recordAt("b", time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC), nil),
	}

	filtered := FilterRecordsUpdatedSince(records, time.Time{})
	if len(filtered) != 2 {
		t.Fatalf("len(FilterRecordsUpdatedSince()) = %d, want 2", len(filtered))
	}
	if filtered[0].ID != "a" || filtered[1].ID != "b" {
		t.Fatalf("FilterRecordsUpdatedSince() IDs = [%s %s], want [a b]", filtered[0].ID, filtered[1].ID)
	}

	// Verify it's a copy, not the same slice.
	filtered[0].ID = "modified"
	if records[0].ID == "modified" {
		t.Fatal("FilterRecordsUpdatedSince() returned the same slice, not a copy")
	}
}

func TestFilterRecordsUpdatedSinceEmptySlice(t *testing.T) {
	filtered := FilterRecordsUpdatedSince(nil, time.Now())
	if len(filtered) != 0 {
		t.Fatalf("len(FilterRecordsUpdatedSince(nil)) = %d, want 0", len(filtered))
	}
}

func TestFigureMapByFilenameHappyPath(t *testing.T) {
	figures := []repository.RecordFigure{
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
	files := []repository.RecordDataFile{
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

func recordAt(id string, updatedAt time.Time, deletedAt *time.Time) repository.Record {
	return repository.Record{
		ID:        id,
		UpdatedAt: updatedAt,
		DeletedAt: deletedAt,
	}
}
