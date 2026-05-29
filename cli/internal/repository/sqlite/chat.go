package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/timeutil"
)

const chatSessionColumns = `id, source, source_session_id, parent_source_session_id, source_device_id, project_id, cwd, title, started_at, last_activity_at, original_source_path, raw_source_key, created_at, updated_at, deleted_at`
const chatItemColumns = `id, session_id, ordinal, role, item_type, text, search_text, raw_json, created_at`
const insertChatItemSQL = `INSERT INTO chat_item (session_id, ordinal, role, item_type, text, search_text, raw_json, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')));`

// UpsertProjectPath registers a project path and reports whether a row was inserted.
func (r *Repository) UpsertProjectPath(ctx context.Context, input repository.CreateProjectPathInput) (repository.ProjectPath, bool, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.Path) == "" || strings.TrimSpace(input.DeviceID) == "" {
		return repository.ProjectPath{}, false, repository.ErrInvalidArgument
	}
	result, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO project_paths (project_id, path, device_id, created_at, updated_at)
         VALUES (?, ?, ?, COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')));`,
		input.ProjectID, input.Path, input.DeviceID, nullableTime(input.CreatedAt), nullableTime(input.UpdatedAt))
	if err != nil {
		return repository.ProjectPath{}, false, mapSQLiteError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return repository.ProjectPath{}, false, fmt.Errorf("rows affected: %w", err)
	}
	path, err := r.getProjectPath(ctx, input.ProjectID, input.Path, input.DeviceID)
	return path, affected > 0, err
}

// ListProjectPaths returns registered project paths sorted by path.
func (r *Repository) ListProjectPaths(ctx context.Context, projectID *string) ([]repository.ProjectPath, error) {
	query := `SELECT id, project_id, path, device_id, created_at, updated_at FROM project_paths`
	args := []any{}
	if projectID != nil {
		query += ` WHERE project_id = ?`
		args = append(args, *projectID)
	}
	query += ` ORDER BY path, project_id, device_id`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()
	var paths []repository.ProjectPath
	for rows.Next() {
		path, err := scanProjectPathRows(rows)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return paths, nil
}

// BackfillChatProjects assigns unassigned chat sessions by longest registered cwd prefix.
func (r *Repository) BackfillChatProjects(ctx context.Context) (int, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE chat_session
         SET project_id = (
             SELECT pp.project_id
             FROM project_paths AS pp
             WHERE pp.device_id = chat_session.source_device_id
               AND (
                 pp.path = chat_session.cwd
                 OR (
                   substr(chat_session.cwd, 1, length(pp.path)) = pp.path
                   AND substr(chat_session.cwd, length(pp.path) + 1, 1) IN ('/', char(92))
                 )
               )
             ORDER BY length(pp.path) DESC, pp.project_id
             LIMIT 1
         )
         WHERE project_id IS NULL
           AND cwd IS NOT NULL
           AND EXISTS (
             SELECT 1 FROM project_paths AS pp
             WHERE pp.device_id = chat_session.source_device_id
               AND (
                 pp.path = chat_session.cwd
                 OR (
                   substr(chat_session.cwd, 1, length(pp.path)) = pp.path
                   AND substr(chat_session.cwd, length(pp.path) + 1, 1) IN ('/', char(92))
                 )
               )
           );`)
	if err != nil {
		return 0, mapSQLiteError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return int(affected), nil
}

func (r *Repository) getProjectPath(ctx context.Context, projectID string, path string, deviceID string) (repository.ProjectPath, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, path, device_id, created_at, updated_at FROM project_paths WHERE project_id = ? AND path = ? AND device_id = ?;`,
		projectID, path, deviceID)
	return scanProjectPath(row)
}

// UpsertChatSession creates or updates a chat session by source identity.
func (r *Repository) UpsertChatSession(ctx context.Context, input repository.UpsertChatSessionInput) (repository.ChatSession, bool, error) {
	if err := validateUpsertChatSessionInput(input); err != nil {
		return repository.ChatSession{}, false, err
	}
	_, existingErr := r.GetChatSessionBySource(ctx, input.Source, input.SourceSessionID)
	if existingErr != nil && !errors.Is(existingErr, repository.ErrNotFound) {
		return repository.ChatSession{}, false, existingErr
	}
	created := errors.Is(existingErr, repository.ErrNotFound)
	if created {
		if exists, err := r.recordIDExists(ctx, input.ID); err != nil {
			return repository.ChatSession{}, false, err
		} else if exists {
			return repository.ChatSession{}, false, fmt.Errorf("%w: id %s already exists as record", repository.ErrConflict, input.ID)
		}
	}
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO chat_session (id, source, source_session_id, parent_source_session_id, source_device_id, project_id, cwd, title, started_at, last_activity_at, original_source_path, raw_source_key, created_at, updated_at, deleted_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), ?)
         ON CONFLICT(source, source_session_id) DO UPDATE SET
             parent_source_session_id = COALESCE(excluded.parent_source_session_id, chat_session.parent_source_session_id),
             source_device_id = excluded.source_device_id,
             project_id = COALESCE(excluded.project_id, chat_session.project_id),
             cwd = COALESCE(excluded.cwd, chat_session.cwd),
             title = COALESCE(excluded.title, chat_session.title),
             started_at = MIN(chat_session.started_at, excluded.started_at),
             last_activity_at = MAX(chat_session.last_activity_at, excluded.last_activity_at),
             original_source_path = excluded.original_source_path,
             raw_source_key = COALESCE(excluded.raw_source_key, chat_session.raw_source_key),
             updated_at = COALESCE(excluded.updated_at, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')),
             deleted_at = CASE WHEN ? THEN NULL ELSE excluded.deleted_at END;`,
		input.ID, input.Source, input.SourceSessionID, nullableString(input.ParentSourceSessionID), input.SourceDeviceID, nullableString(input.ProjectID), nullableString(input.CWD),
		nullableString(input.Title), timeutil.FormatUTCMillis(input.StartedAt), timeutil.FormatUTCMillis(input.LastActivityAt),
		nullableString(input.OriginalSourcePath), nullableString(input.RawSourceKey), nullableTime(input.CreatedAt), nullableTime(input.UpdatedAt), nullableTime(input.DeletedAt), input.ClearDeleted)
	if err != nil {
		return repository.ChatSession{}, false, mapSQLiteError(err)
	}
	if _, err := result.RowsAffected(); err != nil {
		return repository.ChatSession{}, false, fmt.Errorf("rows affected: %w", err)
	}
	session, err := r.GetChatSessionBySource(ctx, input.Source, input.SourceSessionID)
	return session, created, err
}

func validateUpsertChatSessionInput(input repository.UpsertChatSessionInput) error {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Source) == "" ||
		strings.TrimSpace(input.SourceSessionID) == "" || strings.TrimSpace(input.SourceDeviceID) == "" ||
		input.StartedAt.IsZero() || input.LastActivityAt.IsZero() {
		return repository.ErrInvalidArgument
	}
	return nil
}

// GetChatSessionByID fetches one chat session by PC chat ID.
func (r *Repository) GetChatSessionByID(ctx context.Context, id string) (repository.ChatSession, error) {
	if strings.TrimSpace(id) == "" {
		return repository.ChatSession{}, repository.ErrInvalidArgument
	}
	row := r.db.QueryRowContext(ctx, `SELECT `+chatSessionColumns+` FROM chat_session WHERE id = ?;`, id)
	return scanChatSession(row)
}

// GetChatSessionBySource fetches one chat session by source identity.
func (r *Repository) GetChatSessionBySource(ctx context.Context, source string, sourceSessionID string) (repository.ChatSession, error) {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(sourceSessionID) == "" {
		return repository.ChatSession{}, repository.ErrInvalidArgument
	}
	row := r.db.QueryRowContext(ctx, `SELECT `+chatSessionColumns+` FROM chat_session WHERE source = ? AND source_session_id = ?;`, source, sourceSessionID)
	return scanChatSession(row)
}

// ListChatSessions returns chat sessions by last activity descending.
func (r *Repository) ListChatSessions(ctx context.Context, filter repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
	where, args, err := chatSessionPredicateSQLite(filter)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + chatSessionColumns + ` FROM chat_session ` + where + ` ORDER BY last_activity_at DESC, id DESC`
	// SQLite rejects OFFSET without a LIMIT, so emit a sentinel LIMIT -1
	// (which SQLite interprets as "no limit") for offset-only filters.
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	} else if filter.Offset > 0 {
		query += ` LIMIT -1`
	}
	if filter.Offset > 0 {
		query += ` OFFSET ?`
		args = append(args, filter.Offset)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()
	var sessions []repository.ChatSession
	for rows.Next() {
		session, err := scanChatSessionRows(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return sessions, nil
}

// CountChatSessions returns the number of chat sessions matching filters.
func (r *Repository) CountChatSessions(ctx context.Context, filter repository.ListChatSessionsFilter) (int, error) {
	where, args, err := chatSessionPredicateSQLite(filter)
	if err != nil {
		return 0, err
	}
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_session `+where, args...).Scan(&count); err != nil {
		return 0, mapSQLiteError(err)
	}
	return count, nil
}

func chatSessionPredicateSQLite(filter repository.ListChatSessionsFilter) (string, []any, error) {
	if filter.Limit < 0 || filter.Offset < 0 {
		return "", nil, repository.ErrInvalidArgument
	}
	builder := strings.Builder{}
	builder.WriteString(`WHERE 1=1`)
	args := []any{}
	if filter.OnlyDeleted {
		builder.WriteString(` AND deleted_at IS NOT NULL`)
	} else if !filter.IncludeDeleted {
		builder.WriteString(` AND deleted_at IS NULL`)
	}
	if filter.ProjectID != nil {
		builder.WriteString(` AND project_id = ?`)
		args = append(args, *filter.ProjectID)
	}
	if filter.Unassigned {
		builder.WriteString(` AND project_id IS NULL`)
	}
	if filter.Source != nil {
		builder.WriteString(` AND source = ?`)
		args = append(args, *filter.Source)
	}
	if filter.DeviceID != nil {
		builder.WriteString(` AND source_device_id = ?`)
		args = append(args, *filter.DeviceID)
	}
	if filter.ParentSourceSessionID != nil {
		builder.WriteString(` AND parent_source_session_id = ?`)
		args = append(args, *filter.ParentSourceSessionID)
	}
	if filter.DateFrom != nil {
		builder.WriteString(` AND last_activity_at >= ?`)
		args = append(args, timeutil.FormatUTCMillis(*filter.DateFrom))
	}
	if filter.DateTo != nil {
		builder.WriteString(` AND last_activity_at <= ?`)
		args = append(args, timeutil.FormatUTCMillis(*filter.DateTo))
	}
	if filter.UpdatedAfter != nil {
		builder.WriteString(` AND updated_at >= ?`)
		args = append(args, timeutil.FormatUTCMillis(*filter.UpdatedAfter))
	}
	return builder.String(), args, nil
}

// SoftDeleteChatSession marks a chat session deleted. Re-deleting an
// already-soft-deleted chat preserves the original tombstone (COALESCE
// over the existing deleted_at) so trash-age / gc / sync stay stable on
// repeated soft-deletes — matches the SoftDeleteRecord contract.
func (r *Repository) SoftDeleteChatSession(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE chat_session SET deleted_at = COALESCE(deleted_at, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')) WHERE id = ?;`, id)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// RestoreChatSession clears the deleted_at marker on a chat session.
func (r *Repository) RestoreChatSession(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE chat_session SET deleted_at = NULL WHERE id = ? AND deleted_at IS NOT NULL;`, id)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// DeleteChatSession hard-deletes a chat session and its items.
func (r *Repository) DeleteChatSession(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM chat_session WHERE id = ?;`, id)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// MaxChatItemOrdinal returns the highest stored ordinal for a session.
func (r *Repository) MaxChatItemOrdinal(ctx context.Context, sessionID string) (int, error) {
	var ordinal sql.NullInt64
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(ordinal) FROM chat_item WHERE session_id = ?;`, sessionID).Scan(&ordinal); err != nil {
		return 0, mapSQLiteError(err)
	}
	if !ordinal.Valid {
		return -1, nil
	}
	return int(ordinal.Int64), nil
}

// CreateChatItem inserts one chat item.
func (r *Repository) CreateChatItem(ctx context.Context, input repository.CreateChatItemInput) (repository.ChatItem, error) {
	if strings.TrimSpace(input.SessionID) == "" || !validChatItemInput(input) {
		return repository.ChatItem{}, repository.ErrInvalidArgument
	}
	searchText := input.SearchText
	if searchText == "" && input.Text != nil {
		searchText = *input.Text
	}
	result, err := r.db.ExecContext(ctx,
		insertChatItemSQL,
		input.SessionID, input.Ordinal, input.Role, input.ItemType, nullableString(input.Text), searchText, nullableString(input.RawJSON), nullableTime(input.CreatedAt))
	if err != nil {
		return repository.ChatItem{}, mapSQLiteError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return repository.ChatItem{}, fmt.Errorf("last insert id: %w", err)
	}
	row := r.db.QueryRowContext(ctx, `SELECT `+chatItemColumns+` FROM chat_item WHERE id = ?;`, id)
	return scanChatItem(row)
}

func validChatItemInput(input repository.CreateChatItemInput) bool {
	return input.Ordinal >= 0 && strings.TrimSpace(input.Role) != "" && strings.TrimSpace(input.ItemType) != ""
}

// AppendChatItems inserts normalized items for an existing chat session atomically.
func (r *Repository) AppendChatItems(ctx context.Context, sessionID string, items []repository.CreateChatItemInput) error {
	if strings.TrimSpace(sessionID) == "" {
		return repository.ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return mapSQLiteError(err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM chat_session WHERE id = ?;`, sessionID).Scan(&exists); err != nil {
		return mapSQLiteError(err)
	}
	if err := insertChatItemsTx(ctx, tx, sessionID, items); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return mapSQLiteError(err)
	}
	return nil
}

// ReplaceChatItems replaces all normalized items for a chat session atomically.
func (r *Repository) ReplaceChatItems(ctx context.Context, sessionID string, items []repository.CreateChatItemInput) error {
	if strings.TrimSpace(sessionID) == "" {
		return repository.ErrInvalidArgument
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return mapSQLiteError(err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM chat_session WHERE id = ?;`, sessionID).Scan(&exists); err != nil {
		return mapSQLiteError(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chat_item WHERE session_id = ?;`, sessionID); err != nil {
		return mapSQLiteError(err)
	}
	if err := insertChatItemsTx(ctx, tx, sessionID, items); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return mapSQLiteError(err)
	}
	return nil
}

func insertChatItemsTx(ctx context.Context, tx *sql.Tx, sessionID string, items []repository.CreateChatItemInput) error {
	for _, item := range items {
		if !validChatItemInput(item) {
			return repository.ErrInvalidArgument
		}
		searchText := item.SearchText
		if searchText == "" && item.Text != nil {
			searchText = *item.Text
		}
		createdAt := nullableTime(item.CreatedAt)
		if _, err := tx.ExecContext(ctx,
			insertChatItemSQL,
			sessionID, item.Ordinal, item.Role, item.ItemType, nullableString(item.Text), searchText, nullableString(item.RawJSON), createdAt); err != nil {
			return mapSQLiteError(err)
		}
	}
	return nil
}

// WriteChatImportBatch writes multiple chat session/item import operations in
// one SQLite transaction. Existing schema triggers maintain FTS and sync state.
func (r *Repository) WriteChatImportBatch(ctx context.Context, ops []repository.ChatImportOp) ([]repository.ChatImportResult, error) {
	for _, op := range ops {
		if err := validateChatImportOp(op); err != nil {
			return nil, err
		}
	}
	if len(ops) == 0 {
		return []repository.ChatImportResult{}, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = tx.Rollback() }()

	results := make([]repository.ChatImportResult, 0, len(ops))
	for _, op := range ops {
		stored, created, err := upsertChatSessionTx(ctx, tx, op.Session)
		if err != nil {
			return nil, err
		}
		if op.ItemMode == repository.ChatImportItemModeReplace {
			if _, err := tx.ExecContext(ctx, `DELETE FROM chat_item WHERE session_id = ?;`, stored.ID); err != nil {
				return nil, mapSQLiteError(err)
			}
		}
		if err := insertChatItemsTx(ctx, tx, stored.ID, op.Items); err != nil {
			return nil, err
		}
		results = append(results, repository.ChatImportResult{Session: stored, Created: created})
	}

	if err := tx.Commit(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return results, nil
}

func validateChatImportOp(op repository.ChatImportOp) error {
	if err := validateUpsertChatSessionInput(op.Session); err != nil {
		return err
	}
	switch op.ItemMode {
	case repository.ChatImportItemModeReplace, repository.ChatImportItemModeAppend:
	default:
		return repository.ErrInvalidArgument
	}
	for _, item := range op.Items {
		if !validChatItemInput(item) {
			return repository.ErrInvalidArgument
		}
	}
	return nil
}

func upsertChatSessionTx(ctx context.Context, tx *sql.Tx, input repository.UpsertChatSessionInput) (repository.ChatSession, bool, error) {
	var existingID string
	existingErr := tx.QueryRowContext(ctx, `SELECT id FROM chat_session WHERE source = ? AND source_session_id = ?;`, input.Source, input.SourceSessionID).Scan(&existingID)
	if existingErr != nil && !errors.Is(mapSQLiteError(existingErr), repository.ErrNotFound) {
		return repository.ChatSession{}, false, mapSQLiteError(existingErr)
	}
	created := errors.Is(mapSQLiteError(existingErr), repository.ErrNotFound)
	if created {
		var recordExists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM records WHERE id = ?);`, input.ID).Scan(&recordExists); err != nil {
			return repository.ChatSession{}, false, mapSQLiteError(err)
		}
		if recordExists == 1 {
			return repository.ChatSession{}, false, fmt.Errorf("%w: id %s already exists as record", repository.ErrConflict, input.ID)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO chat_session (id, source, source_session_id, parent_source_session_id, source_device_id, project_id, cwd, title, started_at, last_activity_at, original_source_path, raw_source_key, created_at, updated_at, deleted_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), ?)
         ON CONFLICT(source, source_session_id) DO UPDATE SET
             parent_source_session_id = COALESCE(excluded.parent_source_session_id, chat_session.parent_source_session_id),
             source_device_id = excluded.source_device_id,
             project_id = COALESCE(excluded.project_id, chat_session.project_id),
             cwd = COALESCE(excluded.cwd, chat_session.cwd),
             title = COALESCE(excluded.title, chat_session.title),
             started_at = MIN(chat_session.started_at, excluded.started_at),
             last_activity_at = MAX(chat_session.last_activity_at, excluded.last_activity_at),
             original_source_path = excluded.original_source_path,
             raw_source_key = COALESCE(excluded.raw_source_key, chat_session.raw_source_key),
             updated_at = COALESCE(excluded.updated_at, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')),
             deleted_at = CASE WHEN ? THEN NULL ELSE excluded.deleted_at END;`,
		input.ID, input.Source, input.SourceSessionID, nullableString(input.ParentSourceSessionID), input.SourceDeviceID, nullableString(input.ProjectID), nullableString(input.CWD),
		nullableString(input.Title), timeutil.FormatUTCMillis(input.StartedAt), timeutil.FormatUTCMillis(input.LastActivityAt),
		nullableString(input.OriginalSourcePath), nullableString(input.RawSourceKey), nullableTime(input.CreatedAt), nullableTime(input.UpdatedAt), nullableTime(input.DeletedAt), input.ClearDeleted); err != nil {
		return repository.ChatSession{}, false, mapSQLiteError(err)
	}
	row := tx.QueryRowContext(ctx, `SELECT `+chatSessionColumns+` FROM chat_session WHERE source = ? AND source_session_id = ?;`, input.Source, input.SourceSessionID)
	session, err := scanChatSession(row)
	return session, created, err
}

// ListChatItems returns all items in ordinal order for a session.
func (r *Repository) ListChatItems(ctx context.Context, sessionID string) ([]repository.ChatItem, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, repository.ErrInvalidArgument
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+chatItemColumns+` FROM chat_item WHERE session_id = ? ORDER BY ordinal;`, sessionID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()
	var items []repository.ChatItem
	for rows.Next() {
		item, err := scanChatItemRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return items, nil
}

// CountChatItems returns the authoritative number of chat items. When
// filter.IncludeDeleted is false, items in soft-deleted sessions are excluded
// so the count matches what users can see.
func (r *Repository) CountChatItems(ctx context.Context, filter repository.CountChatItemsFilter) (int, error) {
	query := `SELECT COUNT(*) FROM chat_item`
	if !filter.IncludeDeleted {
		query += ` WHERE session_id IN (SELECT id FROM chat_session WHERE deleted_at IS NULL)`
	}
	var count int
	if err := r.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, mapSQLiteError(err)
	}
	return count, nil
}

// SearchChatItems searches chat item text.
func (r *Repository) SearchChatItems(ctx context.Context, filter repository.SearchChatItemsFilter) ([]repository.ChatSearchResult, error) {
	query := strings.TrimSpace(filter.Query)
	if query == "" || filter.Limit < 0 || filter.Offset < 0 {
		return nil, repository.ErrInvalidArgument
	}
	where := strings.Builder{}
	where.WriteString(`WHERE chat_item_fts MATCH ?`)
	args := []any{sqliteFTSQuery(query)}
	if !filter.IncludeDeleted {
		where.WriteString(` AND cs.deleted_at IS NULL`)
	}
	if !filter.IncludeToolOutputs {
		where.WriteString(` AND ci.item_type != 'tool_output'`)
	}
	if filter.ProjectID != nil {
		where.WriteString(` AND cs.project_id = ?`)
		args = append(args, *filter.ProjectID)
	}
	if filter.Source != nil {
		where.WriteString(` AND cs.source = ?`)
		args = append(args, *filter.Source)
	}
	if filter.ParentSourceSessionID != nil {
		where.WriteString(` AND cs.parent_source_session_id = ?`)
		args = append(args, *filter.ParentSourceSessionID)
	}
	if filter.DateFrom != nil {
		where.WriteString(` AND cs.last_activity_at >= ?`)
		args = append(args, timeutil.FormatUTCMillis(*filter.DateFrom))
	}
	if filter.DateTo != nil {
		where.WriteString(` AND cs.last_activity_at <= ?`)
		args = append(args, timeutil.FormatUTCMillis(*filter.DateTo))
	}
	sqlQuery := `SELECT ` + prefixedChatSessionColumns("cs") + `, ` + prefixedChatItemColumns("ci") + `,
            snippet(chat_item_fts, 0, '', '', '...', 12),
            -bm25(chat_item_fts)
        FROM chat_item_fts
        INNER JOIN chat_item AS ci ON ci.id = chat_item_fts.rowid
        INNER JOIN chat_session AS cs ON cs.id = ci.session_id ` + where.String() + `
        ORDER BY -bm25(chat_item_fts) DESC, cs.last_activity_at DESC, ci.ordinal`
	if filter.Limit > 0 {
		sqlQuery += ` LIMIT ?`
		args = append(args, filter.Limit)
	} else if filter.Offset > 0 {
		sqlQuery += ` LIMIT -1`
	}
	if filter.Offset > 0 {
		sqlQuery += ` OFFSET ?`
		args = append(args, filter.Offset)
	}
	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()
	return scanChatSearchRows(rows)
}

// SearchAll searches records and chats and returns a flat domain-discriminated list.
func (r *Repository) SearchAll(ctx context.Context, filter repository.UnifiedSearchFilter) ([]repository.DomainSearchResult, error) {
	query := strings.TrimSpace(filter.Query)
	if query == "" || filter.Limit < 0 || filter.Offset < 0 {
		return nil, repository.ErrInvalidArgument
	}
	results := []repository.DomainSearchResult{}
	includeRecords := filter.Domain == nil || *filter.Domain == "records"
	includeChats := filter.Domain == nil || *filter.Domain == "chats"
	if includeRecords {
		records, err := r.searchRecords(ctx, filter)
		if err != nil {
			return nil, err
		}
		results = append(results, records...)
	}
	if includeChats {
		chats, err := r.SearchChatItems(ctx, repository.SearchChatItemsFilter{
			Query:              query,
			IncludeDeleted:     filter.IncludeDeleted,
			IncludeToolOutputs: filter.IncludeToolOutputs,
			ProjectID:          filter.ProjectID,
			DateFrom:           filter.DateFrom,
			DateTo:             filter.DateTo,
		})
		if err != nil {
			return nil, err
		}
		for i := range chats {
			chat := chats[i]
			results = append(results, repository.DomainSearchResult{Domain: "chats", Chat: &chat, Rank: chat.Rank})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Rank != results[j].Rank {
			return results[i].Rank > results[j].Rank
		}
		if results[i].Domain != results[j].Domain {
			return results[i].Domain < results[j].Domain
		}
		return domainResultID(results[i]) < domainResultID(results[j])
	})
	start := filter.Offset
	if start > len(results) {
		return []repository.DomainSearchResult{}, nil
	}
	end := len(results)
	if filter.Limit > 0 && start+filter.Limit < end {
		end = start + filter.Limit
	}
	return results[start:end], nil
}

func (r *Repository) searchRecords(ctx context.Context, filter repository.UnifiedSearchFilter) ([]repository.DomainSearchResult, error) {
	recordFilter := repository.ListRecordsFilter{ProjectID: filter.ProjectID, IncludeDeleted: filter.IncludeDeleted}
	// Forward DateFrom/DateTo so a unified search with a date window
	// trims record hits the same way it already trims chat hits.
	if filter.DateFrom != nil {
		from := filter.DateFrom.UTC().Format("2006-01-02")
		recordFilter.DateFrom = &from
	}
	if filter.DateTo != nil {
		to := filter.DateTo.UTC().Format("2006-01-02")
		recordFilter.DateTo = &to
	}
	where, args, err := listRecordsPredicateSQL(recordFilter, "records", false)
	if err != nil {
		return nil, err
	}
	args = append(args, sqliteFTSQuery(filter.Query))
	query := `SELECT records.id, records.date, records.day_order, records.html_content, records.notes, records.project_id, records.source_device_id, records.source_ref, records.git_remote_url, records.git_hash, records.created_at, records.updated_at, records.deleted_at,
            -bm25(records_fts)
        FROM records INNER JOIN records_fts ON records_fts.id = records.id ` + where + ` AND records_fts MATCH ?
        ORDER BY -bm25(records_fts) DESC, records.date, records.day_order, records.id`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()
	results := []repository.DomainSearchResult{}
	for rows.Next() {
		record, rank, err := scanRecordSearchRows(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, repository.DomainSearchResult{Domain: "records", Record: &record, Rank: rank})
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return results, nil
}

func (r *Repository) recordIDExists(ctx context.Context, id string) (bool, error) {
	var exists int
	if err := r.db.QueryRowContext(ctx, `SELECT 1 FROM records WHERE id = ? LIMIT 1;`, id).Scan(&exists); err != nil {
		if errors.Is(mapSQLiteError(err), repository.ErrNotFound) {
			return false, nil
		}
		return false, mapSQLiteError(err)
	}
	return true, nil
}

func (r *Repository) chatSessionIDExists(ctx context.Context, id string) (bool, error) {
	var exists int
	if err := r.db.QueryRowContext(ctx, `SELECT 1 FROM chat_session WHERE id = ? LIMIT 1;`, id).Scan(&exists); err != nil {
		if errors.Is(mapSQLiteError(err), repository.ErrNotFound) {
			return false, nil
		}
		return false, mapSQLiteError(err)
	}
	return true, nil
}

func scanProjectPath(row *sql.Row) (repository.ProjectPath, error) {
	var path projectPathRow
	if err := row.Scan(&path.ID, &path.ProjectID, &path.Path, &path.DeviceID, &path.CreatedAt, &path.UpdatedAt); err != nil {
		return repository.ProjectPath{}, mapSQLiteError(err)
	}
	return path.toModel()
}

func scanProjectPathRows(rows *sql.Rows) (repository.ProjectPath, error) {
	var path projectPathRow
	if err := rows.Scan(&path.ID, &path.ProjectID, &path.Path, &path.DeviceID, &path.CreatedAt, &path.UpdatedAt); err != nil {
		return repository.ProjectPath{}, mapSQLiteError(err)
	}
	return path.toModel()
}

type projectPathRow struct {
	ID        int64
	ProjectID string
	Path      string
	DeviceID  string
	CreatedAt string
	UpdatedAt string
}

func (r projectPathRow) toModel() (repository.ProjectPath, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.ProjectPath{}, err
	}
	updatedAt, err := parseTimestamp(r.UpdatedAt)
	if err != nil {
		return repository.ProjectPath{}, err
	}
	return repository.ProjectPath{ID: r.ID, ProjectID: r.ProjectID, Path: r.Path, DeviceID: r.DeviceID, CreatedAt: createdAt, UpdatedAt: updatedAt}, nil
}

type chatSessionRow struct {
	ID                    string
	Source                string
	SourceSessionID       string
	ParentSourceSessionID sql.NullString
	SourceDeviceID        string
	ProjectID             sql.NullString
	CWD                   sql.NullString
	Title                 sql.NullString
	StartedAt             string
	LastActivityAt        string
	OriginalSourcePath    sql.NullString
	RawSourceKey          sql.NullString
	CreatedAt             string
	UpdatedAt             string
	DeletedAt             sql.NullString
}

func (r chatSessionRow) toModel() (repository.ChatSession, error) {
	startedAt, err := parseTimestamp(r.StartedAt)
	if err != nil {
		return repository.ChatSession{}, err
	}
	lastActivityAt, err := parseTimestamp(r.LastActivityAt)
	if err != nil {
		return repository.ChatSession{}, err
	}
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.ChatSession{}, err
	}
	updatedAt, err := parseTimestamp(r.UpdatedAt)
	if err != nil {
		return repository.ChatSession{}, err
	}
	deletedAt, err := parseNullableTimestamp(r.DeletedAt)
	if err != nil {
		return repository.ChatSession{}, err
	}
	return repository.ChatSession{
		ID: r.ID, Source: r.Source, SourceSessionID: r.SourceSessionID,
		ParentSourceSessionID: nullableStringPtr(r.ParentSourceSessionID),
		SourceDeviceID:        r.SourceDeviceID,
		ProjectID:             nullableStringPtr(r.ProjectID), CWD: nullableStringPtr(r.CWD), Title: nullableStringPtr(r.Title),
		StartedAt: startedAt, LastActivityAt: lastActivityAt,
		OriginalSourcePath: nullableStringPtr(r.OriginalSourcePath),
		RawSourceKey:       nullableStringPtr(r.RawSourceKey),
		CreatedAt:          createdAt, UpdatedAt: updatedAt, DeletedAt: deletedAt,
	}, nil
}

func scanChatSession(row *sql.Row) (repository.ChatSession, error) {
	var scanned chatSessionRow
	if err := row.Scan(&scanned.ID, &scanned.Source, &scanned.SourceSessionID, &scanned.ParentSourceSessionID, &scanned.SourceDeviceID, &scanned.ProjectID, &scanned.CWD, &scanned.Title, &scanned.StartedAt, &scanned.LastActivityAt, &scanned.OriginalSourcePath, &scanned.RawSourceKey, &scanned.CreatedAt, &scanned.UpdatedAt, &scanned.DeletedAt); err != nil {
		return repository.ChatSession{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

func scanChatSessionRows(rows *sql.Rows) (repository.ChatSession, error) {
	var scanned chatSessionRow
	if err := rows.Scan(&scanned.ID, &scanned.Source, &scanned.SourceSessionID, &scanned.ParentSourceSessionID, &scanned.SourceDeviceID, &scanned.ProjectID, &scanned.CWD, &scanned.Title, &scanned.StartedAt, &scanned.LastActivityAt, &scanned.OriginalSourcePath, &scanned.RawSourceKey, &scanned.CreatedAt, &scanned.UpdatedAt, &scanned.DeletedAt); err != nil {
		return repository.ChatSession{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

type chatItemRow struct {
	ID         int64
	SessionID  string
	Ordinal    int
	Role       string
	ItemType   string
	Text       sql.NullString
	SearchText string
	RawJSON    sql.NullString
	CreatedAt  string
}

func (r chatItemRow) toModel() (repository.ChatItem, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.ChatItem{}, err
	}
	return repository.ChatItem{
		ID: r.ID, SessionID: r.SessionID, Ordinal: r.Ordinal, Role: r.Role, ItemType: r.ItemType,
		Text: nullableStringPtr(r.Text), SearchText: r.SearchText, RawJSON: nullableStringPtr(r.RawJSON), CreatedAt: createdAt,
	}, nil
}

func scanChatItem(row *sql.Row) (repository.ChatItem, error) {
	var scanned chatItemRow
	if err := row.Scan(&scanned.ID, &scanned.SessionID, &scanned.Ordinal, &scanned.Role, &scanned.ItemType, &scanned.Text, &scanned.SearchText, &scanned.RawJSON, &scanned.CreatedAt); err != nil {
		return repository.ChatItem{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

func scanChatItemRows(rows *sql.Rows) (repository.ChatItem, error) {
	var scanned chatItemRow
	if err := rows.Scan(&scanned.ID, &scanned.SessionID, &scanned.Ordinal, &scanned.Role, &scanned.ItemType, &scanned.Text, &scanned.SearchText, &scanned.RawJSON, &scanned.CreatedAt); err != nil {
		return repository.ChatItem{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

func scanChatSearchRows(rows *sql.Rows) ([]repository.ChatSearchResult, error) {
	var results []repository.ChatSearchResult
	for rows.Next() {
		var session chatSessionRow
		var item chatItemRow
		var snippet string
		var rank float64
		if err := rows.Scan(&session.ID, &session.Source, &session.SourceSessionID, &session.ParentSourceSessionID, &session.SourceDeviceID, &session.ProjectID, &session.CWD, &session.Title, &session.StartedAt, &session.LastActivityAt, &session.OriginalSourcePath, &session.RawSourceKey, &session.CreatedAt, &session.UpdatedAt, &session.DeletedAt, &item.ID, &item.SessionID, &item.Ordinal, &item.Role, &item.ItemType, &item.Text, &item.SearchText, &item.RawJSON, &item.CreatedAt, &snippet, &rank); err != nil {
			return nil, mapSQLiteError(err)
		}
		sessionModel, err := session.toModel()
		if err != nil {
			return nil, err
		}
		itemModel, err := item.toModel()
		if err != nil {
			return nil, err
		}
		results = append(results, repository.ChatSearchResult{Session: sessionModel, Item: itemModel, Snippet: snippet, Rank: rank})
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return results, nil
}

func scanRecordSearchRows(rows *sql.Rows) (repository.Record, float64, error) {
	var row recordRow
	var rank float64
	if err := rows.Scan(&row.ID, &row.Date, &row.DayOrder, &row.HTMLContent, &row.Notes, &row.ProjectID, &row.SourceDeviceID, &row.SourceRef, &row.GitRemoteURL, &row.GitHash, &row.CreatedAt, &row.UpdatedAt, &row.DeletedAt, &rank); err != nil {
		return repository.Record{}, 0, mapSQLiteError(err)
	}
	record, err := row.toModel()
	return record, rank, err
}

func sqliteFTSQuery(query string) string {
	fields := strings.Fields(query)
	quoted := make([]string, 0, len(fields))
	for _, field := range fields {
		escaped := strings.ReplaceAll(field, `"`, `""`)
		quoted = append(quoted, `"`+escaped+`"`)
	}
	return strings.Join(quoted, " ")
}

func domainResultID(result repository.DomainSearchResult) string {
	if result.Record != nil {
		return result.Record.ID
	}
	if result.Chat != nil {
		return fmt.Sprintf("%s/%06d", result.Chat.Session.ID, result.Chat.Item.Ordinal)
	}
	return ""
}

func prefixedChatSessionColumns(alias string) string {
	cols := strings.Split(chatSessionColumns, ", ")
	for i, col := range cols {
		cols[i] = alias + "." + col
	}
	return strings.Join(cols, ", ")
}

func prefixedChatItemColumns(alias string) string {
	cols := strings.Split(chatItemColumns, ", ")
	for i, col := range cols {
		cols[i] = alias + "." + col
	}
	return strings.Join(cols, ", ")
}

// RunChatImportBulkMode executes fn with per-row chat FTS maintenance
// suspended, then rebuilds chat_item_fts and restores its triggers.
func (r *Repository) RunChatImportBulkMode(ctx context.Context, fn func(context.Context) (bool, error)) (rerr error) {
	if fn == nil {
		return repository.ErrInvalidArgument
	}
	if err := r.dropChatItemFTSTriggers(ctx); err != nil {
		return fmt.Errorf("drop chat FTS triggers for bulk load: %w", err)
	}
	rebuildFTS := false
	defer func() {
		if rebuildFTS {
			if err := r.rebuildChatItemFTS(context.Background()); err != nil {
				err = fmt.Errorf("rebuild chat FTS after import: %w", err)
				if rerr != nil {
					rerr = errors.Join(rerr, err)
				} else {
					rerr = err
				}
			}
		}
		if err := r.createChatItemFTSTriggers(context.Background()); err != nil {
			err = fmt.Errorf("restore chat FTS triggers: %w", err)
			if rerr != nil {
				rerr = errors.Join(rerr, err)
			} else {
				rerr = err
			}
		}
	}()
	rebuildFTS, rerr = fn(ctx)
	return rerr
}

func (r *Repository) dropChatItemFTSTriggers(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `
DROP TRIGGER IF EXISTS chat_item_fts_after_insert;
DROP TRIGGER IF EXISTS chat_item_fts_after_update;
DROP TRIGGER IF EXISTS chat_item_fts_after_delete;
`); err != nil {
		return mapSQLiteError(err)
	}
	return nil
}

func (r *Repository) createChatItemFTSTriggers(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `
CREATE TRIGGER IF NOT EXISTS chat_item_fts_after_insert
AFTER INSERT ON chat_item
BEGIN
    INSERT INTO chat_item_fts(rowid, search_text) VALUES (NEW.id, NEW.search_text);
END;

CREATE TRIGGER IF NOT EXISTS chat_item_fts_after_update
AFTER UPDATE ON chat_item
FOR EACH ROW
WHEN OLD.search_text != NEW.search_text
BEGIN
    INSERT INTO chat_item_fts(chat_item_fts, rowid, search_text) VALUES('delete', OLD.id, OLD.search_text);
    INSERT INTO chat_item_fts(rowid, search_text) VALUES (NEW.id, NEW.search_text);
END;

CREATE TRIGGER IF NOT EXISTS chat_item_fts_after_delete
AFTER DELETE ON chat_item
BEGIN
    INSERT INTO chat_item_fts(chat_item_fts, rowid, search_text) VALUES('delete', OLD.id, OLD.search_text);
END;
`); err != nil {
		return mapSQLiteError(err)
	}
	return nil
}

func (r *Repository) rebuildChatItemFTS(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `INSERT INTO chat_item_fts(chat_item_fts) VALUES('rebuild');`); err != nil {
		return mapSQLiteError(err)
	}
	return nil
}
