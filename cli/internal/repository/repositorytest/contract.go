package repositorytest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

// RepositoryFactory creates a fresh repository per test case.
type RepositoryFactory func(t *testing.T) repository.Repository

// RunContractSuite executes backend-agnostic repository contract tests.
// Args: t is the parent testing context; factory returns a fresh repository.
// Returns: none. The test fails on contract violations.
func RunContractSuite(t *testing.T, factory RepositoryFactory) {
	t.Helper()

	t.Run("slides CRUD and sort order", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		slideA := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250304-a3f2b7e1",
			Date:        "2025-03-04",
			DayOrder:    "b",
			HTMLContent: "<h1>A</h1>",
		})

		notes := "updated notes"
		projectID := "org/project"
		updated, err := repo.UpdateSlide(ctx, repository.UpdateSlideInput{
			ID:          slideA.ID,
			Date:        "2025-03-04",
			DayOrder:    "c",
			HTMLContent: "<h1>A2</h1>",
			Notes:       &notes,
			ProjectID:   &projectID,
		})
		if err != nil {
			t.Fatalf("UpdateSlide() error = %v", err)
		}
		if updated.DayOrder != "c" || updated.HTMLContent != "<h1>A2</h1>" {
			t.Fatalf("unexpected updated slide DayOrder/HTMLContent: %+v", updated)
		}
		if updated.Notes == nil || *updated.Notes != "updated notes" {
			t.Fatalf("expected Notes=%q after update, got %v", "updated notes", updated.Notes)
		}
		if updated.ProjectID == nil || *updated.ProjectID != "org/project" {
			t.Fatalf("expected ProjectID=%q after update, got %v", "org/project", updated.ProjectID)
		}
		if updated.UpdatedAt.IsZero() {
			t.Fatal("expected non-zero UpdatedAt after update")
		}
		if updated.CreatedAt != slideA.CreatedAt {
			t.Fatalf("expected CreatedAt preserved after update: got %v, want %v", updated.CreatedAt, slideA.CreatedAt)
		}

		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250304-b7e1c9d3",
			Date:        "2025-03-04",
			DayOrder:    "a",
			HTMLContent: "<h1>B</h1>",
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250303-c0ffee01",
			Date:        "2025-03-03",
			DayOrder:    "z",
			HTMLContent: "<h1>C</h1>",
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250304-a3f2b700",
			Date:        "2025-03-04",
			DayOrder:    "c",
			HTMLContent: "<h1>D</h1>",
		})

		slides, err := repo.ListSlides(ctx, repository.ListSlidesFilter{})
		if err != nil {
			t.Fatalf("ListSlides() error = %v", err)
		}
		ids := slideIDs(slides)
		expected := []string{
			"20250303-c0ffee01",
			"20250304-b7e1c9d3",
			"20250304-a3f2b700",
			"20250304-a3f2b7e1",
		}
		assertExactOrder(t, ids, expected)
	})

	t.Run("slides soft delete and restore", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		slide := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250305-deadbeef",
			Date:        "2025-03-05",
			DayOrder:    "n",
			HTMLContent: "<h1>Trash me</h1>",
		})

		if err := repo.SoftDeleteSlide(ctx, slide.ID); err != nil {
			t.Fatalf("SoftDeleteSlide() error = %v", err)
		}
		active, err := repo.ListSlides(ctx, repository.ListSlidesFilter{})
		if err != nil {
			t.Fatalf("ListSlides(active) error = %v", err)
		}
		if len(active) != 0 {
			t.Fatalf("expected active list to be empty, got %d rows", len(active))
		}

		deleted, err := repo.ListSlides(ctx, repository.ListSlidesFilter{IncludeDeleted: true})
		if err != nil {
			t.Fatalf("ListSlides(includeDeleted) error = %v", err)
		}
		if len(deleted) != 1 || deleted[0].DeletedAt == nil {
			t.Fatalf("expected one deleted slide with deleted_at, got %+v", deleted)
		}

		if err := repo.RestoreSlide(ctx, slide.ID); err != nil {
			t.Fatalf("RestoreSlide() error = %v", err)
		}
		restored, err := repo.ListSlides(ctx, repository.ListSlidesFilter{})
		if err != nil {
			t.Fatalf("ListSlides(after restore) error = %v", err)
		}
		if len(restored) != 1 || restored[0].DeletedAt != nil {
			t.Fatalf("expected restored active slide, got %+v", restored)
		}
	})

	t.Run("slide figures and data files unique constraints", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		slide := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250306-1111aaaa",
			Date:        "2025-03-06",
			DayOrder:    "n",
			HTMLContent: "<h1>Assets</h1>",
		})

		figure, err := repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
			SlideID:  slide.ID,
			Filename: "plot.png",
			S3Key:    "figures/20250306-1111aaaa/plot.png",
		})
		if err != nil {
			t.Fatalf("CreateSlideFigure() error = %v", err)
		}
		_, err = repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
			SlideID:  slide.ID,
			Filename: "plot.png",
			S3Key:    "figures/20250306-1111aaaa/plot.png",
		})
		if !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("expected ErrConflict for duplicate figure filename, got %v", err)
		}

		dataFile, err := repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
			SlideID:  slide.ID,
			Filename: "metrics.csv",
			S3Key:    "data/20250306-1111aaaa/metrics.csv",
			Size:     12,
			Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		})
		if err != nil {
			t.Fatalf("CreateSlideDataFile() error = %v", err)
		}
		_, err = repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
			SlideID:  slide.ID,
			Filename: "metrics.csv",
			S3Key:    "data/20250306-1111aaaa/metrics.csv",
			Size:     12,
			Hash:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		})
		if !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("expected ErrConflict for duplicate data filename, got %v", err)
		}

		if err := repo.DeleteSlideFigure(ctx, figure.ID); err != nil {
			t.Fatalf("DeleteSlideFigure() error = %v", err)
		}
		if err := repo.DeleteSlideDataFile(ctx, dataFile.ID); err != nil {
			t.Fatalf("DeleteSlideDataFile() error = %v", err)
		}
	})

	t.Run("figure and data-file get/list/update", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		slide := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250306-2222bbbb",
			Date:        "2025-03-06",
			DayOrder:    "n",
			HTMLContent: "<h1>Asset updates</h1>",
		})

		figure, err := repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
			SlideID:  slide.ID,
			Filename: "before.png",
			S3Key:    "figures/20250306-2222bbbb/before.png",
			AltText:  strPtr("before"),
		})
		if err != nil {
			t.Fatalf("CreateSlideFigure() error = %v", err)
		}

		figures, err := repo.ListSlideFiguresBySlideID(ctx, slide.ID)
		if err != nil {
			t.Fatalf("ListSlideFiguresBySlideID() error = %v", err)
		}
		if len(figures) != 1 || figures[0].ID != figure.ID {
			t.Fatalf("unexpected figures list: %+v", figures)
		}

		updatedFigure, err := repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{
			ID:       figure.ID,
			Filename: "after.png",
			S3Key:    "figures/20250306-2222bbbb/after.png",
			AltText:  strPtr("after"),
		})
		if err != nil {
			t.Fatalf("UpdateSlideFigure() error = %v", err)
		}
		if updatedFigure.Filename != "after.png" || updatedFigure.S3Key != "figures/20250306-2222bbbb/after.png" {
			t.Fatalf("unexpected updated figure Filename/S3Key: %+v", updatedFigure)
		}
		if updatedFigure.AltText == nil || *updatedFigure.AltText != "after" {
			t.Fatalf("expected AltText=%q after figure update, got %v", "after", updatedFigure.AltText)
		}

		// Verify nil AltText preserves existing value (patch semantics).
		patchedFigure, err := repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{
			ID:      figure.ID,
			AltText: nil,
		})
		if err != nil {
			t.Fatalf("UpdateSlideFigure(nil AltText) error = %v", err)
		}
		if patchedFigure.AltText == nil || *patchedFigure.AltText != "after" {
			t.Fatalf("expected AltText preserved as %q when nil, got %v", "after", patchedFigure.AltText)
		}

		dataFile, err := repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
			SlideID:     slide.ID,
			Filename:    "before.csv",
			S3Key:       "data/20250306-2222bbbb/before.csv",
			Size:        7,
			Hash:        "abababababababababababababababababababababababababababababababab",
			Description: strPtr("before"),
		})
		if err != nil {
			t.Fatalf("CreateSlideDataFile() error = %v", err)
		}

		files, err := repo.ListSlideDataFilesBySlideID(ctx, slide.ID)
		if err != nil {
			t.Fatalf("ListSlideDataFilesBySlideID() error = %v", err)
		}
		if len(files) != 1 || files[0].ID != dataFile.ID {
			t.Fatalf("unexpected data-file list: %+v", files)
		}

		updatedDataFile, err := repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{
			ID:          dataFile.ID,
			Filename:    "after.csv",
			S3Key:       "data/20250306-2222bbbb/after.csv",
			Size:        int64Ptr(11),
			Hash:        strPtr("cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"),
			Description: strPtr("after"),
		})
		if err != nil {
			t.Fatalf("UpdateSlideDataFile() error = %v", err)
		}
		if updatedDataFile.Filename != "after.csv" ||
			updatedDataFile.S3Key != "data/20250306-2222bbbb/after.csv" ||
			updatedDataFile.Size != 11 ||
			updatedDataFile.Hash != "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd" {
			t.Fatalf("unexpected updated data file Filename/S3Key/Size/Hash: %+v", updatedDataFile)
		}
		if updatedDataFile.Description == nil || *updatedDataFile.Description != "after" {
			t.Fatalf("expected Description=%q after data file update, got %v", "after", updatedDataFile.Description)
		}

		// Verify nil Description preserves existing value (patch semantics).
		patchedDataFile, err := repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{
			ID:          dataFile.ID,
			Description: nil,
		})
		if err != nil {
			t.Fatalf("UpdateSlideDataFile(nil Description) error = %v", err)
		}
		if patchedDataFile.Description == nil || *patchedDataFile.Description != "after" {
			t.Fatalf("expected Description preserved as %q when nil, got %v", "after", patchedDataFile.Description)
		}
	})

	t.Run("list filters and invalid arguments", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250301-aa11aa11",
			Date:        "2025-03-01",
			DayOrder:    "a",
			HTMLContent: "<h1>1</h1>",
			ProjectID:   strPtr("org/p1"),
		})
		second := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250302-bb22bb22",
			Date:        "2025-03-02",
			DayOrder:    "a",
			HTMLContent: "<h1>2</h1>",
			ProjectID:   strPtr("org/p2"),
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250303-cc33cc33",
			Date:        "2025-03-03",
			DayOrder:    "a",
			HTMLContent: "<h1>3</h1>",
			ProjectID:   strPtr("org/p2"),
		})

		if err := repo.SoftDeleteSlide(ctx, second.ID); err != nil {
			t.Fatalf("SoftDeleteSlide() error = %v", err)
		}

		projectID := "org/p2"
		dateFrom := "2025-03-01"
		dateTo := "2025-03-03"
		filtered, err := repo.ListSlides(ctx, repository.ListSlidesFilter{
			IncludeDeleted: false,
			ProjectID:      &projectID,
			DateFrom:       &dateFrom,
			DateTo:         &dateTo,
			Limit:          1,
		})
		if err != nil {
			t.Fatalf("ListSlides(filter) error = %v", err)
		}
		if len(filtered) != 1 || filtered[0].ID != "20250303-cc33cc33" {
			t.Fatalf("unexpected filtered result: %+v", filtered)
		}

		if _, err := repo.GetSlideByID(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty slide id, got %v", err)
		}
		if err := repo.DeleteSlide(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty delete id, got %v", err)
		}
		if err := repo.RestoreSlide(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty restore id, got %v", err)
		}
		if _, err := repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid figure input, got %v", err)
		}
		if _, err := repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid data-file input, got %v", err)
		}
		if _, err := repo.CreateTemplate(ctx, repository.CreateTemplateInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid template input, got %v", err)
		}
		if _, err := repo.UpdateTemplate(ctx, repository.UpdateTemplateInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid template update, got %v", err)
		}
		if _, err := repo.GetTemplateByName(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty template name, got %v", err)
		}
		if err := repo.DeleteTemplate(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty template delete, got %v", err)
		}
		if _, err := repo.GetSlideFigureByID(ctx, 0); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid figure id, got %v", err)
		}
		if _, err := repo.GetSlideDataFileByID(ctx, 0); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid data-file id, got %v", err)
		}
		if _, err := repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{ID: 0}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid figure update id, got %v", err)
		}
		if _, err := repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{ID: 0}); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid data-file update id, got %v", err)
		}
		if err := repo.DeleteSlideFigure(ctx, 0); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid figure delete id, got %v", err)
		}
		if err := repo.DeleteSlideDataFile(ctx, 0); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for invalid data-file delete id, got %v", err)
		}
		if err := repo.SoftDeleteSlide(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for empty soft-delete id, got %v", err)
		}

		if err := repo.DeleteSlide(ctx, "20259999-missing00"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for missing slide delete, got %v", err)
		}
	})

	t.Run("updated_at window filters are inclusive", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		firstUpdatedAt := time.Date(2025, 3, 4, 10, 0, 0, 0, time.UTC)
		middleUpdatedAt := time.Date(2025, 3, 4, 11, 0, 0, 0, time.UTC)
		lastUpdatedAt := time.Date(2025, 3, 4, 12, 0, 0, 0, time.UTC)

		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250304-a1b2c3d4",
			Date:        "2025-03-04",
			DayOrder:    "a",
			HTMLContent: "<h1>First</h1>",
			UpdatedAt:   &firstUpdatedAt,
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250304-b2c3d4e5",
			Date:        "2025-03-04",
			DayOrder:    "b",
			HTMLContent: "<h1>Middle</h1>",
			UpdatedAt:   &middleUpdatedAt,
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250304-c3d4e5f6",
			Date:        "2025-03-04",
			DayOrder:    "c",
			HTMLContent: "<h1>Last</h1>",
			UpdatedAt:   &lastUpdatedAt,
		})

		afterResults, err := repo.ListSlides(ctx, repository.ListSlidesFilter{
			UpdatedAfter: &middleUpdatedAt,
		})
		if err != nil {
			t.Fatalf("ListSlides(UpdatedAfter) error = %v", err)
		}
		assertExactOrder(t, slideIDs(afterResults), []string{
			"20250304-b2c3d4e5",
			"20250304-c3d4e5f6",
		})

		beforeResults, err := repo.ListSlides(ctx, repository.ListSlidesFilter{
			UpdatedBefore: &middleUpdatedAt,
		})
		if err != nil {
			t.Fatalf("ListSlides(UpdatedBefore) error = %v", err)
		}
		assertExactOrder(t, slideIDs(beforeResults), []string{
			"20250304-a1b2c3d4",
			"20250304-b2c3d4e5",
		})

		windowResults, err := repo.ListSlides(ctx, repository.ListSlidesFilter{
			UpdatedAfter:  &middleUpdatedAt,
			UpdatedBefore: &middleUpdatedAt,
		})
		if err != nil {
			t.Fatalf("ListSlides(UpdatedAfter+UpdatedBefore) error = %v", err)
		}
		assertExactOrder(t, slideIDs(windowResults), []string{
			"20250304-b2c3d4e5",
		})
	})

	t.Run("templates CRUD", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		template, err := repo.CreateTemplate(ctx, repository.CreateTemplateInput{
			Name:        "text-only",
			HTMLContent: "<main></main>",
		})
		if err != nil {
			t.Fatalf("CreateTemplate() error = %v", err)
		}

		updated, err := repo.UpdateTemplate(ctx, repository.UpdateTemplateInput{
			Name:        template.Name,
			HTMLContent: "<section></section>",
		})
		if err != nil {
			t.Fatalf("UpdateTemplate() error = %v", err)
		}
		if updated.HTMLContent != "<section></section>" {
			t.Fatalf("unexpected updated template: %+v", updated)
		}

		listed, err := repo.ListTemplates(ctx)
		if err != nil {
			t.Fatalf("ListTemplates() error = %v", err)
		}
		if len(listed) != 1 {
			t.Fatalf("expected one template, got %d", len(listed))
		}

		if err := repo.DeleteTemplate(ctx, template.Name); err != nil {
			t.Fatalf("DeleteTemplate() error = %v", err)
		}
		_, err = repo.GetTemplateByName(ctx, template.Name)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected ErrNotFound after template delete, got %v", err)
		}
	})

	t.Run("foreign key rejection and cascading delete", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		_, err := repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
			SlideID:  "20250309-missing0",
			Filename: "orphan.png",
			S3Key:    "figures/20250309-missing0/orphan.png",
		})
		if !errors.Is(err, repository.ErrForeignKeyViolation) {
			t.Fatalf("expected ErrForeignKeyViolation for orphan figure, got %v", err)
		}

		slide := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250309-ca5cad01",
			Date:        "2025-03-09",
			DayOrder:    "n",
			HTMLContent: "<h1>Cascade</h1>",
		})
		figure, err := repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
			SlideID:  slide.ID,
			Filename: "f.png",
			S3Key:    "figures/20250309-ca5cad01/f.png",
		})
		if err != nil {
			t.Fatalf("CreateSlideFigure() error = %v", err)
		}
		dataFile, err := repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
			SlideID:  slide.ID,
			Filename: "d.csv",
			S3Key:    "data/20250309-ca5cad01/d.csv",
			Size:     4,
			Hash:     "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		})
		if err != nil {
			t.Fatalf("CreateSlideDataFile() error = %v", err)
		}

		if err := repo.DeleteSlide(ctx, slide.ID); err != nil {
			t.Fatalf("DeleteSlide() error = %v", err)
		}
		_, err = repo.GetSlideByID(ctx, slide.ID)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for deleted slide, got %v", err)
		}
		_, err = repo.GetSlideFigureByID(ctx, figure.ID)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for cascaded figure, got %v", err)
		}
		_, err = repo.GetSlideDataFileByID(ctx, dataFile.ID)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for cascaded data file, got %v", err)
		}
	})

	t.Run("sync version changes", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		before, err := repo.GetSyncVersion(ctx)
		if err != nil {
			t.Fatalf("GetSyncVersion(before) error = %v", err)
		}

		_, err = repo.CreateTemplate(ctx, repository.CreateTemplateInput{
			Name:        "sync-version",
			HTMLContent: "<main>sync</main>",
		})
		if err != nil {
			t.Fatalf("CreateTemplate() error = %v", err)
		}

		after, err := repo.GetSyncVersion(ctx)
		if err != nil {
			t.Fatalf("GetSyncVersion(after) error = %v", err)
		}
		if after.Version <= before.Version {
			t.Fatalf("expected sync version to increase, before=%d after=%d", before.Version, after.Version)
		}
	})

	t.Run("OnlyDeleted filter", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250401-aa010101",
			Date:        "2025-04-01",
			DayOrder:    "a",
			HTMLContent: "<h1>Active 1</h1>",
		})
		deletedSlide := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250401-bb020202",
			Date:        "2025-04-01",
			DayOrder:    "b",
			HTMLContent: "<h1>Will be deleted</h1>",
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250401-cc030303",
			Date:        "2025-04-01",
			DayOrder:    "c",
			HTMLContent: "<h1>Active 2</h1>",
		})

		if err := repo.SoftDeleteSlide(ctx, deletedSlide.ID); err != nil {
			t.Fatalf("SoftDeleteSlide() error = %v", err)
		}

		onlyDeleted, err := repo.ListSlides(ctx, repository.ListSlidesFilter{OnlyDeleted: true})
		if err != nil {
			t.Fatalf("ListSlides(OnlyDeleted) error = %v", err)
		}
		if len(onlyDeleted) != 1 {
			t.Fatalf("expected 1 deleted slide, got %d", len(onlyDeleted))
		}
		if onlyDeleted[0].ID != deletedSlide.ID {
			t.Fatalf("expected deleted slide %s, got %s", deletedSlide.ID, onlyDeleted[0].ID)
		}
		if onlyDeleted[0].DeletedAt == nil {
			t.Fatal("expected deleted_at to be set on OnlyDeleted result")
		}
	})

	t.Run("Query filter search", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250402-a0a0a0a1",
			Date:        "2025-04-02",
			DayOrder:    "a",
			HTMLContent: "<p>Advances in machine learning</p>",
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250402-b0b0b0b2",
			Date:        "2025-04-02",
			DayOrder:    "b",
			HTMLContent: "<p>Unrelated content</p>",
			Notes:       strPtr("learning about rust"),
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250402-c0c0c0c3",
			Date:        "2025-04-02",
			DayOrder:    "c",
			HTMLContent: "<p>Some other topic</p>",
			ProjectID:   strPtr("org/learning-project"),
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250402-d0d0d0d4",
			Date:        "2025-04-02",
			DayOrder:    "d",
			HTMLContent: "<p>unrelated content only</p>",
		})

		query := "learning"
		results, err := repo.ListSlides(ctx, repository.ListSlidesFilter{Query: &query})
		if err != nil {
			t.Fatalf("ListSlides(Query=learning) error = %v", err)
		}
		ids := slideIDs(results)
		expected := []string{
			"20250402-a0a0a0a1",
			"20250402-b0b0b0b2",
			"20250402-c0c0c0c3",
		}
		assertExactOrder(t, ids, expected)

		// Case-insensitive search returns the same results.
		upperQuery := "LEARNING"
		upperResults, err := repo.ListSlides(ctx, repository.ListSlidesFilter{Query: &upperQuery})
		if err != nil {
			t.Fatalf("ListSlides(Query=LEARNING) error = %v", err)
		}
		assertExactOrder(t, slideIDs(upperResults), expected)
	})

	t.Run("Query with project filter", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250403-a1a1a1a1",
			Date:        "2025-04-03",
			DayOrder:    "a",
			HTMLContent: "<p>golang concurrency patterns</p>",
			ProjectID:   strPtr("org/backend"),
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250403-b2b2b2b2",
			Date:        "2025-04-03",
			DayOrder:    "b",
			HTMLContent: "<p>golang generics tutorial</p>",
			ProjectID:   strPtr("org/frontend"),
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250403-c3c3c3c3",
			Date:        "2025-04-03",
			DayOrder:    "c",
			HTMLContent: "<p>python asyncio</p>",
			ProjectID:   strPtr("org/backend"),
		})

		query := "golang"
		projectID := "org/backend"
		results, err := repo.ListSlides(ctx, repository.ListSlidesFilter{
			Query:     &query,
			ProjectID: &projectID,
		})
		if err != nil {
			t.Fatalf("ListSlides(Query+ProjectID) error = %v", err)
		}
		if len(results) != 1 || results[0].ID != "20250403-a1a1a1a1" {
			t.Fatalf("expected only backend golang slide, got %v", slideIDs(results))
		}
	})

	t.Run("Query with deleted flag", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		slide := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250404-de1e1e01",
			Date:        "2025-04-04",
			DayOrder:    "a",
			HTMLContent: "<p>searchable content</p>",
		})
		if err := repo.SoftDeleteSlide(ctx, slide.ID); err != nil {
			t.Fatalf("SoftDeleteSlide() error = %v", err)
		}

		query := "searchable"

		// Default search excludes deleted slides.
		defaultResults, err := repo.ListSlides(ctx, repository.ListSlidesFilter{Query: &query})
		if err != nil {
			t.Fatalf("ListSlides(Query, default) error = %v", err)
		}
		if len(defaultResults) != 0 {
			t.Fatalf("expected no results for deleted slide in default search, got %d", len(defaultResults))
		}

		// IncludeDeleted=true includes the deleted slide.
		includeResults, err := repo.ListSlides(ctx, repository.ListSlidesFilter{Query: &query, IncludeDeleted: true})
		if err != nil {
			t.Fatalf("ListSlides(Query, IncludeDeleted) error = %v", err)
		}
		if len(includeResults) != 1 || includeResults[0].ID != slide.ID {
			t.Fatalf("expected deleted slide with IncludeDeleted, got %v", slideIDs(includeResults))
		}
	})

	t.Run("LIKE wildcard escaping in Query", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250405-e5c01aa1",
			Date:        "2025-04-05",
			DayOrder:    "a",
			HTMLContent: "<p>100% complete</p>",
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250405-e5c02bb2",
			Date:        "2025-04-05",
			DayOrder:    "b",
			HTMLContent: "<p>1000 items</p>",
		})

		// "100%" should only match the slide with the literal percent sign,
		// not "1000" (which would match if % were treated as a wildcard).
		query := "100%"
		results, err := repo.ListSlides(ctx, repository.ListSlidesFilter{Query: &query})
		if err != nil {
			t.Fatalf("ListSlides(Query=100%%) error = %v", err)
		}
		if len(results) != 1 || results[0].ID != "20250405-e5c01aa1" {
			t.Fatalf("expected only the slide with literal '100%%', got %v", slideIDs(results))
		}

		// Also test underscore escaping: "_" should not match arbitrary single chars.
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250405-e5c03cc3",
			Date:        "2025-04-05",
			DayOrder:    "c",
			HTMLContent: "<p>item_count is 5</p>",
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250405-e5c04dd4",
			Date:        "2025-04-05",
			DayOrder:    "d",
			HTMLContent: "<p>itemXcount is 9</p>",
		})

		underscoreQuery := "item_count"
		underscoreResults, err := repo.ListSlides(ctx, repository.ListSlidesFilter{Query: &underscoreQuery})
		if err != nil {
			t.Fatalf("ListSlides(Query=item_count) error = %v", err)
		}
		if len(underscoreResults) != 1 || underscoreResults[0].ID != "20250405-e5c03cc3" {
			t.Fatalf("expected only the slide with literal 'item_count', got %v", slideIDs(underscoreResults))
		}
	})

	t.Run("LIKE backslash escaping in Query", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250405-e5c05ee5",
			Date:        "2025-04-05",
			DayOrder:    "e",
			HTMLContent: `<p>path is C:\Users\docs</p>`,
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250405-e5c06ff6",
			Date:        "2025-04-05",
			DayOrder:    "f",
			HTMLContent: "<p>path is C:Usersdocs</p>",
		})

		// A query containing backslashes should only match the slide with
		// literal backslashes, not the one without them.
		bsQuery := `C:\Users`
		bsResults, err := repo.ListSlides(ctx, repository.ListSlidesFilter{Query: &bsQuery})
		if err != nil {
			t.Fatalf("ListSlides(Query with backslash) error = %v", err)
		}
		if len(bsResults) != 1 || bsResults[0].ID != "20250405-e5c05ee5" {
			t.Fatalf("expected only the slide with literal backslashes, got %v", slideIDs(bsResults))
		}
	})

	t.Run("Whitespace-only Query rejected", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		query := "   "
		_, err := repo.ListSlides(ctx, repository.ListSlidesFilter{Query: &query})
		if !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for whitespace-only query, got %v", err)
		}
	})

	t.Run("Negative Limit rejected", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		_, err := repo.ListSlides(ctx, repository.ListSlidesFilter{Limit: -1})
		if !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for negative limit, got %v", err)
		}
	})

	t.Run("CountActiveSlides and CountTrashedSlides", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		// Empty DB: both counts are zero.
		active, err := repo.CountActiveSlides(ctx)
		if err != nil {
			t.Fatalf("CountActiveSlides(empty) error = %v", err)
		}
		if active != 0 {
			t.Fatalf("expected 0 active slides, got %d", active)
		}
		trashed, err := repo.CountTrashedSlides(ctx)
		if err != nil {
			t.Fatalf("CountTrashedSlides(empty) error = %v", err)
		}
		if trashed != 0 {
			t.Fatalf("expected 0 trashed slides, got %d", trashed)
		}

		// Create two slides, trash one.
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID: "20250410-c0a1b2c3", Date: "2025-04-10", DayOrder: "a", HTMLContent: "<h1>A</h1>",
		})
		toTrash := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID: "20250410-c0d4e5f6", Date: "2025-04-10", DayOrder: "b", HTMLContent: "<h1>B</h1>",
		})
		if err := repo.SoftDeleteSlide(ctx, toTrash.ID); err != nil {
			t.Fatalf("SoftDeleteSlide() error = %v", err)
		}

		active, err = repo.CountActiveSlides(ctx)
		if err != nil {
			t.Fatalf("CountActiveSlides error = %v", err)
		}
		if active != 1 {
			t.Fatalf("expected 1 active slide, got %d", active)
		}
		trashed, err = repo.CountTrashedSlides(ctx)
		if err != nil {
			t.Fatalf("CountTrashedSlides error = %v", err)
		}
		if trashed != 1 {
			t.Fatalf("expected 1 trashed slide, got %d", trashed)
		}
	})

	t.Run("PurgeDeletedSlides", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		// Purge on empty DB returns empty slice.
		ids, err := repo.PurgeDeletedSlides(ctx)
		if err != nil {
			t.Fatalf("PurgeDeletedSlides(empty) error = %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("expected 0 purged IDs, got %d", len(ids))
		}

		// Create 3 slides, trash 2, purge.
		activeSlide := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID: "20250411-a0a1a1a1", Date: "2025-04-11", DayOrder: "a", HTMLContent: "<h1>Active</h1>",
		})
		trash1 := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID: "20250411-b0b2b2b2", Date: "2025-04-11", DayOrder: "b", HTMLContent: "<h1>Trash1</h1>",
		})
		trash2 := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID: "20250411-c0c3c3c3", Date: "2025-04-11", DayOrder: "c", HTMLContent: "<h1>Trash2</h1>",
		})

		if err := repo.SoftDeleteSlide(ctx, trash1.ID); err != nil {
			t.Fatalf("SoftDeleteSlide(1) error = %v", err)
		}
		if err := repo.SoftDeleteSlide(ctx, trash2.ID); err != nil {
			t.Fatalf("SoftDeleteSlide(2) error = %v", err)
		}

		ids, err = repo.PurgeDeletedSlides(ctx)
		if err != nil {
			t.Fatalf("PurgeDeletedSlides error = %v", err)
		}
		if len(ids) != 2 {
			t.Fatalf("expected 2 purged IDs, got %d: %v", len(ids), ids)
		}

		// Active slide should still exist.
		_, err = repo.GetSlideByID(ctx, activeSlide.ID)
		if err != nil {
			t.Fatalf("active slide should still exist: %v", err)
		}

		// Trashed slides should be hard-deleted.
		for _, trashID := range []string{trash1.ID, trash2.ID} {
			_, err = repo.GetSlideByID(ctx, trashID)
			if !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("expected ErrNotFound for purged slide %s, got %v", trashID, err)
			}
		}

		// Count should reflect the purge.
		trashed, err := repo.CountTrashedSlides(ctx)
		if err != nil {
			t.Fatalf("CountTrashedSlides after purge error = %v", err)
		}
		if trashed != 0 {
			t.Fatalf("expected 0 trashed after purge, got %d", trashed)
		}
	})

	t.Run("ListDistinctProjectIDs", func(t *testing.T) {
		repo := factory(t)
		ctx := context.Background()

		// Empty DB returns empty slice.
		ids, err := repo.ListDistinctProjectIDs(ctx)
		if err != nil {
			t.Fatalf("ListDistinctProjectIDs(empty) error = %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("expected empty slice for empty DB, got %v", ids)
		}

		// Create slides with different project_ids, including duplicates and nil.
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250406-a1d01aa1",
			Date:        "2025-04-06",
			DayOrder:    "a",
			HTMLContent: "<h1>1</h1>",
			ProjectID:   strPtr("org/zebra"),
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250406-b2d02bb2",
			Date:        "2025-04-06",
			DayOrder:    "b",
			HTMLContent: "<h1>2</h1>",
			ProjectID:   strPtr("org/alpha"),
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250406-c3d03cc3",
			Date:        "2025-04-06",
			DayOrder:    "c",
			HTMLContent: "<h1>3</h1>",
			ProjectID:   strPtr("org/alpha"), // duplicate
		})
		mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250406-d4d04dd4",
			Date:        "2025-04-06",
			DayOrder:    "d",
			HTMLContent: "<h1>4</h1>",
			// nil ProjectID — should be excluded
		})
		deletedSlide := mustCreateSlide(t, ctx, repo, repository.CreateSlideInput{
			ID:          "20250406-e5d05ee5",
			Date:        "2025-04-06",
			DayOrder:    "e",
			HTMLContent: "<h1>5</h1>",
			ProjectID:   strPtr("org/deleted-proj"),
		})
		if err := repo.SoftDeleteSlide(ctx, deletedSlide.ID); err != nil {
			t.Fatalf("SoftDeleteSlide() error = %v", err)
		}

		ids, err = repo.ListDistinctProjectIDs(ctx)
		if err != nil {
			t.Fatalf("ListDistinctProjectIDs() error = %v", err)
		}

		expectedIDs := []string{"org/alpha", "org/zebra"}
		if len(ids) != len(expectedIDs) {
			t.Fatalf("expected %d project IDs, got %d: %v", len(expectedIDs), len(ids), ids)
		}
		for i, want := range expectedIDs {
			if ids[i] != want {
				t.Fatalf("expected project ID at index %d to be %q, got %q (full list: %v)", i, want, ids[i], ids)
			}
		}
	})

}

func mustCreateSlide(t *testing.T, ctx context.Context, repo repository.Repository, input repository.CreateSlideInput) repository.Slide {
	t.Helper()

	slide, err := repo.CreateSlide(ctx, input)
	if err != nil {
		t.Fatalf("CreateSlide failed: %v", err)
	}
	if slide.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt after CreateSlide")
	}
	if slide.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero UpdatedAt after CreateSlide")
	}
	return slide
}

func slideIDs(slides []repository.Slide) []string {
	ids := make([]string, 0, len(slides))
	for _, slide := range slides {
		ids = append(ids, slide.ID)
	}
	return ids
}

func assertExactOrder(t *testing.T, got []string, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d (%v)", len(want), len(got), got)
	}
	for idx := range got {
		if got[idx] != want[idx] {
			t.Fatalf("unexpected ordering at %d: got=%v want=%v", idx, got, want)
		}
	}
}

func strPtr(value string) *string {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}
