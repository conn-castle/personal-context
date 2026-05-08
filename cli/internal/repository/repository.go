package repository

import (
	"context"
)

// Repository defines backend-agnostic CRUD and query behavior for all local tables.
//
// Implementations must map backend-specific errors to the sentinel errors in this
// package so tests can be reused against different backends.
type Repository interface {
	CreateRecord(ctx context.Context, input CreateRecordInput) (Record, error)
	GetRecordByID(ctx context.Context, id string) (Record, error)
	UpdateRecord(ctx context.Context, input UpdateRecordInput) (Record, error)
	ListRecords(ctx context.Context, filter ListRecordsFilter) ([]Record, error)
	SoftDeleteRecord(ctx context.Context, id string) error
	RestoreRecord(ctx context.Context, id string) error
	DeleteRecord(ctx context.Context, id string) error

	CreateRecordFigure(ctx context.Context, input CreateRecordFigureInput) (RecordFigure, error)
	GetRecordFigureByID(ctx context.Context, id int64) (RecordFigure, error)
	UpdateRecordFigure(ctx context.Context, input UpdateRecordFigureInput) (RecordFigure, error)
	ListRecordFiguresByRecordID(ctx context.Context, recordID string) ([]RecordFigure, error)
	DeleteRecordFigure(ctx context.Context, id int64) error

	CreateRecordDataFile(ctx context.Context, input CreateRecordDataFileInput) (RecordDataFile, error)
	GetRecordDataFileByID(ctx context.Context, id int64) (RecordDataFile, error)
	UpdateRecordDataFile(ctx context.Context, input UpdateRecordDataFileInput) (RecordDataFile, error)
	ListRecordDataFilesByRecordID(ctx context.Context, recordID string) ([]RecordDataFile, error)
	DeleteRecordDataFile(ctx context.Context, id int64) error

	CreateTemplate(ctx context.Context, input CreateTemplateInput) (Template, error)
	GetTemplateByName(ctx context.Context, name string) (Template, error)
	UpdateTemplate(ctx context.Context, input UpdateTemplateInput) (Template, error)
	ListTemplates(ctx context.Context) ([]Template, error)
	DeleteTemplate(ctx context.Context, name string) error

	GetSyncVersion(ctx context.Context) (SyncVersion, error)

	CreateProject(ctx context.Context, input CreateRegistryInput) (Project, error)
	GetProjectByID(ctx context.Context, id string) (Project, error)
	ListProjects(ctx context.Context, includeArchived bool) ([]Project, error)
	ArchiveProject(ctx context.Context, id string) (Project, error)
	RestoreProject(ctx context.Context, id string) (Project, error)
	UpsertProjectForImport(ctx context.Context, project Project) (bool, error)

	CreateDevice(ctx context.Context, input CreateRegistryInput) (Device, error)
	GetDeviceByID(ctx context.Context, id string) (Device, error)
	ListDevices(ctx context.Context, includeArchived bool) ([]Device, error)
	ArchiveDevice(ctx context.Context, id string) (Device, error)
	RestoreDevice(ctx context.Context, id string) (Device, error)
	UpsertDeviceForImport(ctx context.Context, device Device) (bool, error)

	// CountActiveRecords returns the number of non-deleted records.
	CountActiveRecords(ctx context.Context) (int, error)

	// CountTrashedRecords returns the number of soft-deleted records.
	CountTrashedRecords(ctx context.Context) (int, error)

	// PurgeDeletedRecords hard-deletes all soft-deleted records (CASCADE removes child rows)
	// and returns the IDs of the purged records for filesystem/S3 cleanup by the caller.
	PurgeDeletedRecords(ctx context.Context) ([]string, error)
}
