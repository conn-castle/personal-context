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
	// CountRecords counts records matching non-pagination filters. It ignores
	// ListRecordsFilter.Limit and any cursor applied by callers.
	CountRecords(ctx context.Context, filter ListRecordsFilter) (int, error)
	// CountRecordChildren returns child-row counts keyed by record ID. Nil or
	// empty inputs return an empty map; records with no child rows may be absent.
	CountRecordChildren(ctx context.Context, recordIDs []string) (map[string]ChildCounts, error)
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
	UpsertProjectPath(ctx context.Context, input CreateProjectPathInput) (ProjectPath, bool, error)
	ListProjectPaths(ctx context.Context, projectID *string) ([]ProjectPath, error)
	BackfillChatProjects(ctx context.Context) (int, error)

	CreateDevice(ctx context.Context, input CreateRegistryInput) (Device, error)
	GetDeviceByID(ctx context.Context, id string) (Device, error)
	ListDevices(ctx context.Context, includeArchived bool) ([]Device, error)
	ArchiveDevice(ctx context.Context, id string) (Device, error)
	RestoreDevice(ctx context.Context, id string) (Device, error)
	UpsertDeviceForImport(ctx context.Context, device Device) (bool, error)

	UpsertChatSession(ctx context.Context, input UpsertChatSessionInput) (ChatSession, bool, error)
	// UpsertChatSessionWithItems writes a chat session and its complete
	// replacement item set in one backend transaction.
	UpsertChatSessionWithItems(ctx context.Context, input UpsertChatSessionInput, items []CreateChatItemInput) (ChatSession, bool, error)
	GetChatSessionByID(ctx context.Context, id string) (ChatSession, error)
	GetChatSessionBySource(ctx context.Context, source string, sourceSessionID string) (ChatSession, error)
	ListChatSessions(ctx context.Context, filter ListChatSessionsFilter) ([]ChatSession, error)
	CountChatSessions(ctx context.Context, filter ListChatSessionsFilter) (int, error)
	SoftDeleteChatSession(ctx context.Context, id string) error
	RestoreChatSession(ctx context.Context, id string) error
	DeleteChatSession(ctx context.Context, id string) error
	MaxChatItemOrdinal(ctx context.Context, sessionID string) (int, error)
	CreateChatItem(ctx context.Context, input CreateChatItemInput) (ChatItem, error)
	AppendChatItems(ctx context.Context, sessionID string, items []CreateChatItemInput) error
	ReplaceChatItems(ctx context.Context, sessionID string, items []CreateChatItemInput) error
	ListChatItems(ctx context.Context, sessionID string) ([]ChatItem, error)
	// CountChatItems returns the authoritative number of chat items matching the
	// filter. It is the single source of truth for absolute post-import item
	// counts so summaries reconcile with stored state instead of accumulating
	// CLI-side deltas.
	CountChatItems(ctx context.Context, filter CountChatItemsFilter) (int, error)
	SearchChatItems(ctx context.Context, filter SearchChatItemsFilter) ([]ChatSearchResult, error)
	// CountSearchChatItems returns the total number of chat items matching the
	// search filter, ignoring Limit and Offset. It is the authoritative `total`
	// for `pc chat search --format json` and shares predicate construction with
	// SearchChatItems so the page and count can never drift.
	CountSearchChatItems(ctx context.Context, filter SearchChatItemsFilter) (int, error)
	SearchAll(ctx context.Context, filter UnifiedSearchFilter) ([]DomainSearchResult, error)

	// CountActiveRecords returns the number of non-deleted records.
	CountActiveRecords(ctx context.Context) (int, error)

	// CountTrashedRecords returns the number of soft-deleted records.
	CountTrashedRecords(ctx context.Context) (int, error)

	// PurgeDeletedRecords hard-deletes all soft-deleted records (CASCADE removes child rows)
	// and returns the IDs of the purged records for filesystem/S3 cleanup by the caller.
	PurgeDeletedRecords(ctx context.Context) ([]string, error)
}
