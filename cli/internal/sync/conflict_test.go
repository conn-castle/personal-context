package sync

import (
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestResolveBundleLocalWinsWhenUpdatedLater(t *testing.T) {
	base := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)

	local := newBundle("20260308-a1b2c3d4", base.Add(2*time.Minute), nil)
	cloud := newBundle("20260308-a1b2c3d4", base, nil)

	got, winner, err := ResolveBundle(local, cloud)
	if err != nil {
		t.Fatalf("ResolveBundle() error = %v", err)
	}
	if winner != WinnerLocal {
		t.Fatalf("winner = %q, want %q", winner, WinnerLocal)
	}
	if got.Record.UpdatedAt != local.Record.UpdatedAt {
		t.Fatalf("unexpected winner bundle: %+v", got.Record)
	}
}

func TestResolveBundleCloudWinsWhenUpdatedLater(t *testing.T) {
	base := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)

	local := newBundle("20260308-a1b2c3d4", base, nil)
	cloud := newBundle("20260308-a1b2c3d4", base.Add(2*time.Minute), nil)

	got, winner, err := ResolveBundle(local, cloud)
	if err != nil {
		t.Fatalf("ResolveBundle() error = %v", err)
	}
	if winner != WinnerCloud {
		t.Fatalf("winner = %q, want %q", winner, WinnerCloud)
	}
	if got.Record.UpdatedAt != cloud.Record.UpdatedAt {
		t.Fatalf("unexpected winner bundle: %+v", got.Record)
	}
}

func TestResolveBundleDeleteWinsWhenDeletedAfterEdit(t *testing.T) {
	base := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	deletedAt := base.Add(3 * time.Minute)

	local := newBundle("20260308-a1b2c3d4", base, nil)
	cloud := newBundle("20260308-a1b2c3d4", base.Add(2*time.Minute), &deletedAt)

	got, winner, err := ResolveBundle(local, cloud)
	if err != nil {
		t.Fatalf("ResolveBundle() error = %v", err)
	}
	if winner != WinnerCloud {
		t.Fatalf("winner = %q, want %q", winner, WinnerCloud)
	}
	if got.Record.DeletedAt == nil || !got.Record.DeletedAt.Equal(deletedAt) {
		t.Fatalf("expected deleted bundle to win, got %+v", got.Record)
	}
}

func TestResolveBundleEditWinsOnDeleteTie(t *testing.T) {
	base := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)

	local := newBundle("20260308-a1b2c3d4", base, nil)
	cloud := newBundle("20260308-a1b2c3d4", base, &base)

	got, winner, err := ResolveBundle(local, cloud)
	if err != nil {
		t.Fatalf("ResolveBundle() error = %v", err)
	}
	if winner != WinnerLocal {
		t.Fatalf("winner = %q, want %q", winner, WinnerLocal)
	}
	if got.Record.DeletedAt != nil {
		t.Fatalf("expected edit to win tie, got deleted bundle %+v", got.Record)
	}
}

func TestResolveBundleExactEditTieReturnsNone(t *testing.T) {
	base := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)

	local := newBundle("20260308-a1b2c3d4", base, nil)
	cloud := newBundle("20260308-a1b2c3d4", base, nil)
	cloud.Record.HTMLContent = strPtr("<h1>cloud</h1>")

	_, winner, err := ResolveBundle(local, cloud)
	if err != nil {
		t.Fatalf("ResolveBundle() error = %v", err)
	}
	if winner != WinnerNone {
		t.Fatalf("winner = %q, want %q", winner, WinnerNone)
	}
}

func TestResolveBundleRejectsMismatchedIDs(t *testing.T) {
	_, _, err := ResolveBundle(
		newBundle("20260308-a1b2c3d4", time.Now().UTC(), nil),
		newBundle("20260308-deadbeef", time.Now().UTC(), nil),
	)
	if err == nil {
		t.Fatal("expected error for mismatched ids")
	}
}

func TestPlanFigureReconciliationMatchesByFilename(t *testing.T) {
	plan, err := PlanFigureReconciliation(
		"20260308-a1b2c3d4",
		[]repository.RecordFigure{{
			ID:       1,
			RecordID:  "20260308-a1b2c3d4",
			Filename: "plot.png",
			S3Key:    "figures/20260308-a1b2c3d4/plot.png",
			AltText:  strPtr("before"),
		}},
		[]repository.RecordFigure{{
			ID:       99,
			RecordID:  "20260308-a1b2c3d4",
			Filename: "plot.png",
			S3Key:    "figures/20260308-a1b2c3d4/plot-v2.png",
			AltText:  strPtr("after"),
		}},
	)
	if err != nil {
		t.Fatalf("PlanFigureReconciliation() error = %v", err)
	}
	if len(plan.Creates) != 0 {
		t.Fatalf("expected no creates, got %+v", plan.Creates)
	}
	if len(plan.DeleteIDs) != 0 {
		t.Fatalf("expected no deletes, got %+v", plan.DeleteIDs)
	}
	if len(plan.Updates) != 1 {
		t.Fatalf("expected one update, got %+v", plan.Updates)
	}
	if plan.Updates[0].ID != 1 {
		t.Fatalf("expected existing ID 1 to be updated, got %+v", plan.Updates[0])
	}
}

func TestPlanFigureReconciliationDeletesMissingRows(t *testing.T) {
	plan, err := PlanFigureReconciliation(
		"20260308-a1b2c3d4",
		[]repository.RecordFigure{{
			ID:       1,
			RecordID:  "20260308-a1b2c3d4",
			Filename: "plot.png",
			S3Key:    "figures/20260308-a1b2c3d4/plot.png",
		}},
		nil,
	)
	if err != nil {
		t.Fatalf("PlanFigureReconciliation() error = %v", err)
	}
	if len(plan.DeleteIDs) != 1 || plan.DeleteIDs[0] != 1 {
		t.Fatalf("unexpected delete plan: %+v", plan.DeleteIDs)
	}
}

func TestPlanDataFileReconciliationMatchesByFilename(t *testing.T) {
	plan, err := PlanDataFileReconciliation(
		"20260308-a1b2c3d4",
		[]repository.RecordDataFile{{
			ID:          7,
			RecordID:     "20260308-a1b2c3d4",
			Filename:    "metrics.csv",
			S3Key:       "data/20260308-a1b2c3d4/metrics.csv",
			Size:        10,
			Hash:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Description: strPtr("before"),
		}},
		[]repository.RecordDataFile{{
			ID:          42,
			RecordID:     "20260308-a1b2c3d4",
			Filename:    "metrics.csv",
			S3Key:       "data/20260308-a1b2c3d4/metrics-v2.csv",
			Size:        12,
			Hash:        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Description: strPtr("after"),
		}},
	)
	if err != nil {
		t.Fatalf("PlanDataFileReconciliation() error = %v", err)
	}
	if len(plan.Creates) != 0 {
		t.Fatalf("expected no creates, got %+v", plan.Creates)
	}
	if len(plan.DeleteIDs) != 0 {
		t.Fatalf("expected no deletes, got %+v", plan.DeleteIDs)
	}
	if len(plan.Updates) != 1 {
		t.Fatalf("expected one update, got %+v", plan.Updates)
	}
	if plan.Updates[0].ID != 7 {
		t.Fatalf("expected existing ID 7 to be updated, got %+v", plan.Updates[0])
	}
}

func TestPlanDataFileReconciliationCreatesNewRows(t *testing.T) {
	plan, err := PlanDataFileReconciliation(
		"20260308-a1b2c3d4",
		nil,
		[]repository.RecordDataFile{{
			Filename: "metrics.csv",
			S3Key:    "data/20260308-a1b2c3d4/metrics.csv",
			Size:     10,
			Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	)
	if err != nil {
		t.Fatalf("PlanDataFileReconciliation() error = %v", err)
	}
	if len(plan.Creates) != 1 {
		t.Fatalf("expected one create, got %+v", plan.Creates)
	}
	if plan.Creates[0].RecordID != "20260308-a1b2c3d4" {
		t.Fatalf("expected create to target record, got %+v", plan.Creates[0])
	}
}

func TestResolveBundleErrorsOnEmptyLocalRecordID(t *testing.T) {
	base := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	local := newBundle("", base, nil)
	cloud := newBundle("20260308-a1b2c3d4", base, nil)

	_, _, err := ResolveBundle(local, cloud)
	if err == nil {
		t.Fatal("expected error for empty local record id")
	}
	if got := err.Error(); got != "local record id is required" {
		t.Fatalf("error = %q, want 'local record id is required'", got)
	}
}

func TestResolveBundleErrorsOnEmptyCloudRecordID(t *testing.T) {
	base := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	local := newBundle("20260308-a1b2c3d4", base, nil)
	cloud := newBundle("", base, nil)

	_, _, err := ResolveBundle(local, cloud)
	if err == nil {
		t.Fatal("expected error for empty cloud record id")
	}
	if got := err.Error(); got != "cloud record id is required" {
		t.Fatalf("error = %q, want 'cloud record id is required'", got)
	}
}

func TestResolveBundleErrorsOnWhitespaceOnlyRecordID(t *testing.T) {
	base := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	local := newBundle("   ", base, nil)
	cloud := newBundle("20260308-a1b2c3d4", base, nil)

	_, _, err := ResolveBundle(local, cloud)
	if err == nil {
		t.Fatal("expected error for whitespace-only local record id")
	}
}

func TestPlanFigureReconciliationErrorsOnEmptyRecordID(t *testing.T) {
	_, err := PlanFigureReconciliation("", nil, nil)
	if err == nil {
		t.Fatal("expected error for empty record id")
	}
	if got := err.Error(); got != "record id is required" {
		t.Fatalf("error = %q, want 'record id is required'", got)
	}
}

func TestPlanFigureReconciliationErrorsOnWhitespaceRecordID(t *testing.T) {
	_, err := PlanFigureReconciliation("   ", nil, nil)
	if err == nil {
		t.Fatal("expected error for whitespace-only record id")
	}
}

func TestPlanFigureReconciliationErrorsOnEmptyDesiredFilename(t *testing.T) {
	_, err := PlanFigureReconciliation(
		"20260308-a1b2c3d4",
		nil,
		[]repository.RecordFigure{{
			Filename: "",
			S3Key:    "figures/20260308-a1b2c3d4/plot.png",
		}},
	)
	if err == nil {
		t.Fatal("expected error for empty desired figure filename")
	}
}

func TestPlanFigureReconciliationErrorsOnEmptyDesiredS3Key(t *testing.T) {
	_, err := PlanFigureReconciliation(
		"20260308-a1b2c3d4",
		nil,
		[]repository.RecordFigure{{
			Filename: "plot.png",
			S3Key:    "",
		}},
	)
	if err == nil {
		t.Fatal("expected error for empty desired figure s3key")
	}
}

func TestPlanFigureReconciliationCreatesNewFigures(t *testing.T) {
	plan, err := PlanFigureReconciliation(
		"20260308-a1b2c3d4",
		nil,
		[]repository.RecordFigure{{
			RecordID:  "20260308-a1b2c3d4",
			Filename: "new.png",
			S3Key:    "figures/20260308-a1b2c3d4/new.png",
			AltText:  strPtr("a new figure"),
		}},
	)
	if err != nil {
		t.Fatalf("PlanFigureReconciliation() error = %v", err)
	}
	if len(plan.Creates) != 1 {
		t.Fatalf("expected one create, got %d", len(plan.Creates))
	}
	if plan.Creates[0].RecordID != "20260308-a1b2c3d4" {
		t.Fatalf("create RecordID = %q, want %q", plan.Creates[0].RecordID, "20260308-a1b2c3d4")
	}
	if plan.Creates[0].Filename != "new.png" {
		t.Fatalf("create Filename = %q, want %q", plan.Creates[0].Filename, "new.png")
	}
	if *plan.Creates[0].AltText != "a new figure" {
		t.Fatalf("create AltText = %q, want %q", *plan.Creates[0].AltText, "a new figure")
	}
	if len(plan.Updates) != 0 {
		t.Fatalf("expected no updates, got %d", len(plan.Updates))
	}
	if len(plan.DeleteIDs) != 0 {
		t.Fatalf("expected no deletes, got %d", len(plan.DeleteIDs))
	}
}

func TestPlanFigureReconciliationExactMatchNoChanges(t *testing.T) {
	figures := []repository.RecordFigure{{
		ID:       1,
		RecordID:  "20260308-a1b2c3d4",
		Filename: "plot.png",
		S3Key:    "figures/20260308-a1b2c3d4/plot.png",
		AltText:  strPtr("same alt"),
	}}
	plan, err := PlanFigureReconciliation("20260308-a1b2c3d4", figures, figures)
	if err != nil {
		t.Fatalf("PlanFigureReconciliation() error = %v", err)
	}
	if len(plan.Creates) != 0 {
		t.Fatalf("expected no creates, got %d", len(plan.Creates))
	}
	if len(plan.Updates) != 0 {
		t.Fatalf("expected no updates, got %d", len(plan.Updates))
	}
	if len(plan.DeleteIDs) != 0 {
		t.Fatalf("expected no deletes, got %d", len(plan.DeleteIDs))
	}
}

func TestPlanFigureReconciliationExactMatchNilAltText(t *testing.T) {
	figures := []repository.RecordFigure{{
		ID:       1,
		RecordID:  "20260308-a1b2c3d4",
		Filename: "plot.png",
		S3Key:    "figures/20260308-a1b2c3d4/plot.png",
		AltText:  nil,
	}}
	plan, err := PlanFigureReconciliation("20260308-a1b2c3d4", figures, figures)
	if err != nil {
		t.Fatalf("PlanFigureReconciliation() error = %v", err)
	}
	if len(plan.Updates) != 0 {
		t.Fatalf("expected no updates for identical nil alt text, got %d", len(plan.Updates))
	}
}

func TestPlanDataFileReconciliationErrorsOnEmptyRecordID(t *testing.T) {
	_, err := PlanDataFileReconciliation("", nil, nil)
	if err == nil {
		t.Fatal("expected error for empty record id")
	}
	if got := err.Error(); got != "record id is required" {
		t.Fatalf("error = %q, want 'record id is required'", got)
	}
}

func TestPlanDataFileReconciliationErrorsOnWhitespaceRecordID(t *testing.T) {
	_, err := PlanDataFileReconciliation("  \t ", nil, nil)
	if err == nil {
		t.Fatal("expected error for whitespace-only record id")
	}
}

func TestPlanDataFileReconciliationErrorsOnEmptyDesiredFilename(t *testing.T) {
	_, err := PlanDataFileReconciliation(
		"20260308-a1b2c3d4",
		nil,
		[]repository.RecordDataFile{{
			Filename: "",
			S3Key:    "data/20260308-a1b2c3d4/metrics.csv",
			Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	)
	if err == nil {
		t.Fatal("expected error for empty desired data file filename")
	}
}

func TestPlanDataFileReconciliationErrorsOnEmptyDesiredS3Key(t *testing.T) {
	_, err := PlanDataFileReconciliation(
		"20260308-a1b2c3d4",
		nil,
		[]repository.RecordDataFile{{
			Filename: "metrics.csv",
			S3Key:    "",
			Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	)
	if err == nil {
		t.Fatal("expected error for empty desired data file s3key")
	}
}

func TestPlanDataFileReconciliationErrorsOnEmptyDesiredHash(t *testing.T) {
	_, err := PlanDataFileReconciliation(
		"20260308-a1b2c3d4",
		nil,
		[]repository.RecordDataFile{{
			Filename: "metrics.csv",
			S3Key:    "data/20260308-a1b2c3d4/metrics.csv",
			Hash:     "",
		}},
	)
	if err == nil {
		t.Fatal("expected error for empty desired data file hash")
	}
}

func TestPlanDataFileReconciliationDeletesMissingRows(t *testing.T) {
	plan, err := PlanDataFileReconciliation(
		"20260308-a1b2c3d4",
		[]repository.RecordDataFile{{
			ID:       5,
			RecordID:  "20260308-a1b2c3d4",
			Filename: "old.csv",
			S3Key:    "data/20260308-a1b2c3d4/old.csv",
			Size:     10,
			Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
		nil,
	)
	if err != nil {
		t.Fatalf("PlanDataFileReconciliation() error = %v", err)
	}
	if len(plan.DeleteIDs) != 1 || plan.DeleteIDs[0] != 5 {
		t.Fatalf("expected delete ID 5, got %+v", plan.DeleteIDs)
	}
	if len(plan.Creates) != 0 {
		t.Fatalf("expected no creates, got %d", len(plan.Creates))
	}
	if len(plan.Updates) != 0 {
		t.Fatalf("expected no updates, got %d", len(plan.Updates))
	}
}

func TestPlanDataFileReconciliationExactMatchNoChanges(t *testing.T) {
	dataFiles := []repository.RecordDataFile{{
		ID:          1,
		RecordID:     "20260308-a1b2c3d4",
		Filename:    "metrics.csv",
		S3Key:       "data/20260308-a1b2c3d4/metrics.csv",
		Size:        10,
		Hash:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Description: strPtr("same desc"),
	}}
	plan, err := PlanDataFileReconciliation("20260308-a1b2c3d4", dataFiles, dataFiles)
	if err != nil {
		t.Fatalf("PlanDataFileReconciliation() error = %v", err)
	}
	if len(plan.Creates) != 0 {
		t.Fatalf("expected no creates, got %d", len(plan.Creates))
	}
	if len(plan.Updates) != 0 {
		t.Fatalf("expected no updates, got %d", len(plan.Updates))
	}
	if len(plan.DeleteIDs) != 0 {
		t.Fatalf("expected no deletes, got %d", len(plan.DeleteIDs))
	}
}

func TestPlanDataFileReconciliationExactMatchNilDescription(t *testing.T) {
	dataFiles := []repository.RecordDataFile{{
		ID:          1,
		RecordID:     "20260308-a1b2c3d4",
		Filename:    "metrics.csv",
		S3Key:       "data/20260308-a1b2c3d4/metrics.csv",
		Size:        10,
		Hash:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Description: nil,
	}}
	plan, err := PlanDataFileReconciliation("20260308-a1b2c3d4", dataFiles, dataFiles)
	if err != nil {
		t.Fatalf("PlanDataFileReconciliation() error = %v", err)
	}
	if len(plan.Updates) != 0 {
		t.Fatalf("expected no updates for identical nil description, got %d", len(plan.Updates))
	}
}

// TestPlanFigureReconciliationRejectsDuplicateDesiredFilename guards against
// silent overwrite when the desired source slice contains the same filename
// twice. Prior to the syncengine.FigureMapByFilename adoption, the second
// entry replaced the first in the in-flight map.
func TestPlanFigureReconciliationRejectsDuplicateDesiredFilename(t *testing.T) {
	_, err := PlanFigureReconciliation(
		"20260308-a1b2c3d4",
		nil,
		[]repository.RecordFigure{
			{Filename: "plot.png", S3Key: "figures/20260308-a1b2c3d4/plot.png"},
			{Filename: "plot.png", S3Key: "figures/20260308-a1b2c3d4/plot-alt.png"},
		},
	)
	if err == nil {
		t.Fatal("expected error for duplicate desired figure filename")
	}
}

// TestPlanFigureReconciliationRejectsDuplicateExistingFilename ensures the
// existing slice is also validated through syncengine.FigureMapByFilename.
func TestPlanFigureReconciliationRejectsDuplicateExistingFilename(t *testing.T) {
	_, err := PlanFigureReconciliation(
		"20260308-a1b2c3d4",
		[]repository.RecordFigure{
			{ID: 1, Filename: "plot.png", S3Key: "figures/20260308-a1b2c3d4/plot.png"},
			{ID: 2, Filename: "plot.png", S3Key: "figures/20260308-a1b2c3d4/plot-2.png"},
		},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for duplicate existing figure filename")
	}
}

// TestPlanDataFileReconciliationRejectsDuplicateDesiredFilename mirrors the
// figure case for data files.
func TestPlanDataFileReconciliationRejectsDuplicateDesiredFilename(t *testing.T) {
	_, err := PlanDataFileReconciliation(
		"20260308-a1b2c3d4",
		nil,
		[]repository.RecordDataFile{
			{Filename: "metrics.csv", S3Key: "data/20260308-a1b2c3d4/metrics.csv", Hash: "deadbeef"},
			{Filename: "metrics.csv", S3Key: "data/20260308-a1b2c3d4/metrics-alt.csv", Hash: "cafef00d"},
		},
	)
	if err == nil {
		t.Fatal("expected error for duplicate desired data file filename")
	}
}

// TestPlanDataFileReconciliationRejectsDuplicateExistingFilename guards the
// existing-side duplicate path for data files.
func TestPlanDataFileReconciliationRejectsDuplicateExistingFilename(t *testing.T) {
	_, err := PlanDataFileReconciliation(
		"20260308-a1b2c3d4",
		[]repository.RecordDataFile{
			{ID: 1, Filename: "metrics.csv", S3Key: "data/20260308-a1b2c3d4/metrics.csv", Hash: "deadbeef"},
			{ID: 2, Filename: "metrics.csv", S3Key: "data/20260308-a1b2c3d4/metrics-2.csv", Hash: "cafef00d"},
		},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for duplicate existing data file filename")
	}
}

func newBundle(id string, updatedAt time.Time, deletedAt *time.Time) RecordBundle {
	return RecordBundle{
		Record: repository.Record{
			ID:          id,
			HTMLContent: strPtr("<h1>local</h1>"),
			UpdatedAt:   updatedAt,
			DeletedAt:   deletedAt,
		},
	}
}

func strPtr(value string) *string {
	return &value
}
