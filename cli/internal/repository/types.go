package repository

import "time"

// Slide is a row from the slides table.
type Slide struct {
	ID           string
	UserID       string // Postgres only; empty in SQLite (local mode)
	Date         string
	DayOrder     string
	HTMLContent  string
	Notes        *string
	ProjectID    *string
	GitRemoteURL *string
	GitHash      *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

// SlideFigure is a row from the slide_figures table.
type SlideFigure struct {
	ID        int64
	SlideID   string
	Filename  string
	S3Key     string
	AltText   *string
	CreatedAt time.Time
}

// SlideDataFile is a row from the slide_data_files table.
type SlideDataFile struct {
	ID          int64
	SlideID     string
	Filename    string
	S3Key       string
	Size        int64
	Hash        string
	Description *string
	CreatedAt   time.Time
}

// Template is a row from the templates table.
type Template struct {
	Name        string
	HTMLContent string
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SyncVersion is a row from sync_version.
// SQLite: singleton (id=1). Postgres: per-user (user_id is PK).
type SyncVersion struct {
	ID        int    // SQLite: always 1. Postgres: unused (0).
	UserID    string // Postgres: user_id PK. SQLite: empty.
	Version   int64
	UpdatedAt time.Time
}

// CreateSlideInput contains required and optional fields for inserts.
type CreateSlideInput struct {
	ID           string
	Date         string
	DayOrder     string
	HTMLContent  string
	Notes        *string
	ProjectID    *string
	GitRemoteURL *string
	GitHash      *string
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
	DeletedAt    *time.Time
}

// UpdateSlideInput contains mutable slide fields.
type UpdateSlideInput struct {
	ID           string
	Date         string
	DayOrder     string
	HTMLContent  string
	Notes        *string
	ProjectID    *string
	GitRemoteURL *string
	GitHash      *string
	UpdatedAt    *time.Time
	DeletedAt    *time.Time
}

// ListSlidesFilter controls slide-list query behavior.
type ListSlidesFilter struct {
	IncludeDeleted bool
	OnlyDeleted    bool
	ProjectID      *string
	DateFrom       *string
	DateTo         *string
	Limit          int
	Query          *string
	UpdatedAfter   *time.Time
	UpdatedBefore  *time.Time
}

// CreateSlideFigureInput contains required and optional fields for figure inserts.
type CreateSlideFigureInput struct {
	SlideID  string
	Filename string
	S3Key    string
	AltText  *string
}

// UpdateSlideFigureInput contains mutable figure fields.
type UpdateSlideFigureInput struct {
	ID       int64
	Filename string
	S3Key    string
	AltText  *string
}

// CreateSlideDataFileInput contains required and optional fields for data-file inserts.
type CreateSlideDataFileInput struct {
	SlideID     string
	Filename    string
	S3Key       string
	Size        int64
	Hash        string
	Description *string
}

// UpdateSlideDataFileInput contains mutable data-file fields.
type UpdateSlideDataFileInput struct {
	ID          int64
	Filename    string
	S3Key       string
	Size        *int64
	Hash        *string
	Description *string
}

// CreateTemplateInput contains required and optional fields for template inserts.
type CreateTemplateInput struct {
	Name        string
	HTMLContent string
	Description *string
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
}

// UpdateTemplateInput contains mutable template fields.
type UpdateTemplateInput struct {
	Name        string
	HTMLContent string
	Description *string
	UpdatedAt   *time.Time
}
