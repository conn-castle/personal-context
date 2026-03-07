package repository

import (
	"context"
)

// Repository defines backend-agnostic CRUD and query behavior for all local tables.
//
// Implementations must map backend-specific errors to the sentinel errors in this
// package so tests can be reused against different backends.
type Repository interface {
	CreateSlide(ctx context.Context, input CreateSlideInput) (Slide, error)
	GetSlideByID(ctx context.Context, id string) (Slide, error)
	UpdateSlide(ctx context.Context, input UpdateSlideInput) (Slide, error)
	ListSlides(ctx context.Context, filter ListSlidesFilter) ([]Slide, error)
	SoftDeleteSlide(ctx context.Context, id string) error
	RestoreSlide(ctx context.Context, id string) error
	DeleteSlide(ctx context.Context, id string) error

	CreateSlideFigure(ctx context.Context, input CreateSlideFigureInput) (SlideFigure, error)
	GetSlideFigureByID(ctx context.Context, id int64) (SlideFigure, error)
	UpdateSlideFigure(ctx context.Context, input UpdateSlideFigureInput) (SlideFigure, error)
	ListSlideFiguresBySlideID(ctx context.Context, slideID string) ([]SlideFigure, error)
	DeleteSlideFigure(ctx context.Context, id int64) error

	CreateSlideDataFile(ctx context.Context, input CreateSlideDataFileInput) (SlideDataFile, error)
	GetSlideDataFileByID(ctx context.Context, id int64) (SlideDataFile, error)
	UpdateSlideDataFile(ctx context.Context, input UpdateSlideDataFileInput) (SlideDataFile, error)
	ListSlideDataFilesBySlideID(ctx context.Context, slideID string) ([]SlideDataFile, error)
	DeleteSlideDataFile(ctx context.Context, id int64) error

	CreateTemplate(ctx context.Context, input CreateTemplateInput) (Template, error)
	GetTemplateByName(ctx context.Context, name string) (Template, error)
	UpdateTemplate(ctx context.Context, input UpdateTemplateInput) (Template, error)
	ListTemplates(ctx context.Context) ([]Template, error)
	DeleteTemplate(ctx context.Context, name string) error

	GetSyncVersion(ctx context.Context) (SyncVersion, error)

	// ListDistinctProjectIDs returns sorted distinct non-NULL project_id values from active slides.
	ListDistinctProjectIDs(ctx context.Context) ([]string, error)
}
