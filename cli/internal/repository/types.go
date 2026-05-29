package repository

import (
	"context"
	"time"
)

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

// ProjectPath links a registered project to an absolute source path on a device.
type ProjectPath struct {
	ID        int64
	ProjectID string
	Path      string
	DeviceID  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChatSession is one imported agent chat transcript.
//
// OriginalSourcePath is the original imported transcript path (provenance,
// machine-specific). RawSourceKey is the canonical relative key for the
// Personal Context-owned raw transcript copy, resolved locally under
// <PC_HOME>/personal-context/ and used as the S3 object suffix after
// users/{user_id}/ in cloud mode.
type ChatSession struct {
	ID                    string
	UserID                string // Postgres only; empty in SQLite (local mode)
	Source                string
	SourceSessionID       string
	ParentSourceSessionID *string // parent transcript's source_session_id for subagent sessions; nil otherwise
	SourceDeviceID        string
	ProjectID             *string
	CWD                   *string
	Title                 *string
	StartedAt             time.Time
	LastActivityAt        time.Time
	OriginalSourcePath    *string
	RawSourceKey          *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             *time.Time
}

// ChatItem is one normalized message/tool event inside a chat session.
type ChatItem struct {
	ID         int64
	SessionID  string
	Ordinal    int
	Role       string
	ItemType   string
	Text       *string
	SearchText string
	RawJSON    *string
	CreatedAt  time.Time
}

// ChatSearchResult is one matched chat item with its parent session metadata.
type ChatSearchResult struct {
	Session ChatSession
	Item    ChatItem
	Snippet string
	Rank    float64
}

// DomainSearchResult is a cross-domain search hit.
type DomainSearchResult struct {
	Domain string
	Record *Record
	Chat   *ChatSearchResult
	Rank   float64
}

// RecordFigure is a row from the record_figures table.
type RecordFigure struct {
	ID        int64
	RecordID  string
	Filename  string
	S3Key     string
	AltText   *string
	CreatedAt time.Time
}

// RecordDataFile is a row from the record_data_files table.
type RecordDataFile struct {
	ID          int64
	RecordID    string
	Filename    string
	S3Key       string
	Size        int64
	Hash        string
	Description *string
	CreatedAt   time.Time
}

// ChildCounts contains aggregate child-row counts for one record.
type ChildCounts struct {
	Figures   int
	DataFiles int
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

// CreateProjectPathInput contains required fields for path registration.
type CreateProjectPathInput struct {
	ProjectID string
	Path      string
	DeviceID  string
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

// CreateChatSessionInput contains required and optional chat session fields.
type CreateChatSessionInput struct {
	ID                    string
	Source                string
	SourceSessionID       string
	ParentSourceSessionID *string // parent transcript's source_session_id for subagent sessions; nil otherwise
	SourceDeviceID        string
	ProjectID             *string
	CWD                   *string
	Title                 *string
	StartedAt             time.Time
	LastActivityAt        time.Time
	OriginalSourcePath    *string
	RawSourceKey          *string
	CreatedAt             *time.Time
	UpdatedAt             *time.Time
	DeletedAt             *time.Time
}

// UpsertChatSessionInput creates or updates an imported chat by source identity.
type UpsertChatSessionInput struct {
	CreateChatSessionInput
	ClearDeleted bool
}

// ChatImportItemMode selects how imported items should be applied for one
// chat session inside a batch import write.
type ChatImportItemMode string

const (
	ChatImportItemModeReplace ChatImportItemMode = "replace"
	ChatImportItemModeAppend  ChatImportItemMode = "append"
)

// ChatImportOp contains one session upsert plus its normalized item mutation
// for a local chat import batch.
type ChatImportOp struct {
	Session  UpsertChatSessionInput
	ItemMode ChatImportItemMode
	Items    []CreateChatItemInput
}

// ChatImportResult reports the stored session for one batch operation.
type ChatImportResult struct {
	Session ChatSession
	Created bool
}

// ChatImportBatchWriter is required by pc chat import: the local repository
// must implement it so many session/item changes commit in one SQLite
// transaction. Cloud-mode imports do not use this interface — they go through
// the standard Repository methods over the API server.
type ChatImportBatchWriter interface {
	WriteChatImportBatch(ctx context.Context, ops []ChatImportOp) ([]ChatImportResult, error)
	// RunChatImportBulkMode executes fn with repository-level bulk import
	// optimizations and restores search invariants after mutating imports.
	RunChatImportBulkMode(ctx context.Context, fn func(context.Context) (bool, error)) error
}

// CreateChatItemInput contains required and optional chat item fields.
type CreateChatItemInput struct {
	SessionID  string
	Ordinal    int
	Role       string
	ItemType   string
	Text       *string
	SearchText string
	RawJSON    *string
	CreatedAt  *time.Time
}

// ListChatSessionsFilter controls chat-list query behavior.
type ListChatSessionsFilter struct {
	IncludeDeleted bool
	OnlyDeleted    bool
	ProjectID      *string
	Unassigned     bool
	Source         *string
	DeviceID       *string
	// ParentSourceSessionID, when set, restricts results to sessions whose
	// parent_source_session_id equals this value (the subagents of one parent).
	ParentSourceSessionID *string
	DateFrom              *time.Time
	DateTo                *time.Time
	Limit                 int
	Offset                int
	UpdatedAfter          *time.Time
}

// SearchChatItemsFilter controls chat FTS query behavior.
type SearchChatItemsFilter struct {
	Query              string
	IncludeDeleted     bool
	IncludeToolOutputs bool
	ProjectID          *string
	Source             *string
	// ParentSourceSessionID, when set, restricts matches to items whose session
	// has parent_source_session_id equal to this value (one parent's subagents).
	ParentSourceSessionID *string
	DateFrom              *time.Time
	DateTo                *time.Time
	Limit                 int
	Offset                int
}

// CountChatItemsFilter controls the authoritative chat-item count used to
// reconcile import summaries with stored state.
type CountChatItemsFilter struct {
	// IncludeDeleted counts items belonging to soft-deleted sessions too. When
	// false, only items in non-deleted sessions are counted (the user-visible
	// total).
	IncludeDeleted bool
}

// UnifiedSearchFilter controls top-level cross-domain search.
type UnifiedSearchFilter struct {
	Query              string
	Domain             *string
	ProjectID          *string
	DateFrom           *time.Time
	DateTo             *time.Time
	IncludeDeleted     bool
	IncludeToolOutputs bool
	Limit              int
	Offset             int
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
	HasHTML        bool
	HasData        bool
}

// CreateRecordFigureInput contains required and optional fields for figure inserts.
type CreateRecordFigureInput struct {
	RecordID string
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
	RecordID    string
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
