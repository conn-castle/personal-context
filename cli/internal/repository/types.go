package repository

import "time"

// Record is a row from the records table.
type Record struct {
	ID             string
	UserID         string // Postgres only; empty in SQLite (local mode)
	Date           string
	DayOrder       string
	HTMLContent    *string
	Notes          *string
	ProjectID      string
	SourceDeviceID string
	SourceRef      *string
	GitRemoteURL   *string
	GitHash        *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

// Project is a project registry row.
type Project struct {
	ID         string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt *time.Time
}

// Device is a source-device registry row.
type Device struct {
	ID         string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt *time.Time
}

// RecordFigure is a row from the record_figures table.
type RecordFigure struct {
	ID        int64
	RecordID   string
	Filename  string
	S3Key     string
	AltText   *string
	CreatedAt time.Time
}

// RecordDataFile is a row from the record_data_files table.
type RecordDataFile struct {
	ID          int64
	RecordID     string
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

// CreateRegistryInput contains fields for creating project/device registry rows.
type CreateRegistryInput struct {
	ID         string
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
	ArchivedAt *time.Time
}

// CreateRecordInput contains required and optional fields for inserts.
type CreateRecordInput struct {
	ID             string
	Date           string
	DayOrder       string
	HTMLContent    *string
	Notes          *string
	ProjectID      string
	SourceDeviceID string
	SourceRef      *string
	GitRemoteURL   *string
	GitHash        *string
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
	DeletedAt      *time.Time
}

// UpdateRecordInput contains mutable record fields.
type UpdateRecordInput struct {
	ID             string
	Date           string
	DayOrder       string
	HTMLContent    *string
	Notes          *string
	ProjectID      string
	SourceDeviceID string
	SourceRef      *string
	GitRemoteURL   *string
	GitHash        *string
	UpdatedAt      *time.Time
	DeletedAt      *time.Time
}

// ListRecordsFilter controls record-list query behavior.
type ListRecordsFilter struct {
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

// CreateRecordFigureInput contains required and optional fields for figure inserts.
type CreateRecordFigureInput struct {
	RecordID  string
	Filename string
	S3Key    string
	AltText  *string
}

// UpdateRecordFigureInput contains mutable figure fields.
type UpdateRecordFigureInput struct {
	ID       int64
	Filename string
	S3Key    string
	AltText  *string
}

// CreateRecordDataFileInput contains required and optional fields for data-file inserts.
type CreateRecordDataFileInput struct {
	RecordID     string
	Filename    string
	S3Key       string
	Size        int64
	Hash        string
	Description *string
}

// UpdateRecordDataFileInput contains mutable data-file fields.
type UpdateRecordDataFileInput struct {
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
