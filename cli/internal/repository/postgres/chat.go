package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

const chatSessionColumns = `id, user_id, source, source_session_id, source_device_id, project_id, cwd, title, started_at, last_activity_at, original_source_path, raw_source_key, created_at, updated_at, deleted_at`
const chatItemColumns = `id, session_id, ordinal, role, item_type, text, search_text, raw_json, created_at`

// UpsertProjectPath registers a project path and reports whether a row was inserted.
func (r *Repository) UpsertProjectPath(ctx context.Context, input repository.CreateProjectPathInput) (repository.ProjectPath, bool, error) {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.Path) == "" || strings.TrimSpace(input.DeviceID) == "" {
		return repository.ProjectPath{}, false, repository.ErrInvalidArgument
	}
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO project_paths (user_id, project_id, path, device_id, created_at, updated_at)
         VALUES ($1, $2, $3, $4, COALESCE($5, NOW()), COALESCE($6, NOW()))
         ON CONFLICT (user_id, project_id, path, device_id) DO NOTHING`,
		r.userID, input.ProjectID, input.Path, input.DeviceID, input.CreatedAt, input.UpdatedAt)
	if err != nil {
		return repository.ProjectPath{}, false, mapPgError(err)
	}
	path, err := r.getProjectPath(ctx, input.ProjectID, input.Path, input.DeviceID)
	return path, tag.RowsAffected() > 0, err
}

// ListProjectPaths returns registered project paths sorted by path.
func (r *Repository) ListProjectPaths(ctx context.Context, projectID *string) ([]repository.ProjectPath, error) {
	query := `SELECT id, project_id, path, device_id, created_at, updated_at FROM project_paths WHERE user_id = $1`
	args := []any{r.userID}
	if projectID != nil {
		query += ` AND project_id = $2`
		args = append(args, *projectID)
	}
	query += ` ORDER BY path, project_id, device_id`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()
	var paths []repository.ProjectPath
	for rows.Next() {
		path, err := scanProjectPath(rows)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return paths, nil
}

// BackfillChatProjects assigns unassigned chat sessions by longest registered cwd prefix.
func (r *Repository) BackfillChatProjects(ctx context.Context) (int, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE chat_session AS cs
         SET project_id = (
             SELECT pp.project_id
             FROM project_paths AS pp
             WHERE pp.user_id = cs.user_id
               AND pp.device_id = cs.source_device_id
               AND (
                 pp.path = cs.cwd
                 OR (
                   left(cs.cwd, length(pp.path)) = pp.path
                   AND substring(cs.cwd from length(pp.path) + 1 for 1) IN ('/', chr(92))
                 )
               )
             ORDER BY length(pp.path) DESC, pp.project_id
             LIMIT 1
         )
         WHERE cs.user_id = $1
           AND cs.project_id IS NULL
           AND cs.cwd IS NOT NULL
           AND EXISTS (
             SELECT 1
             FROM project_paths AS pp
             WHERE pp.user_id = cs.user_id
               AND pp.device_id = cs.source_device_id
               AND (
                 pp.path = cs.cwd
                 OR (
                   left(cs.cwd, length(pp.path)) = pp.path
                   AND substring(cs.cwd from length(pp.path) + 1 for 1) IN ('/', chr(92))
                 )
               )
           )`,
		r.userID)
	if err != nil {
		return 0, mapPgError(err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repository) getProjectPath(ctx context.Context, projectID string, path string, deviceID string) (repository.ProjectPath, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, project_id, path, device_id, created_at, updated_at
         FROM project_paths WHERE user_id = $1 AND project_id = $2 AND path = $3 AND device_id = $4`,
		r.userID, projectID, path, deviceID)
	return scanProjectPath(row)
}

// UpsertChatSession creates or updates a chat session by source identity.
func (r *Repository) UpsertChatSession(ctx context.Context, input repository.UpsertChatSessionInput) (repository.ChatSession, bool, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Source) == "" ||
		strings.TrimSpace(input.SourceSessionID) == "" || strings.TrimSpace(input.SourceDeviceID) == "" ||
		input.StartedAt.IsZero() || input.LastActivityAt.IsZero() {
		return repository.ChatSession{}, false, repository.ErrInvalidArgument
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
	_, err := r.pool.Exec(ctx,
		`INSERT INTO chat_session (id, user_id, source, source_session_id, source_device_id, project_id, cwd, title, started_at, last_activity_at, original_source_path, raw_source_key, created_at, updated_at, deleted_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, COALESCE($13, NOW()), COALESCE($14, NOW()), $15)
         ON CONFLICT (user_id, source, source_session_id) DO UPDATE SET
             source_device_id = EXCLUDED.source_device_id,
             project_id = COALESCE(EXCLUDED.project_id, chat_session.project_id),
             cwd = COALESCE(EXCLUDED.cwd, chat_session.cwd),
             title = COALESCE(EXCLUDED.title, chat_session.title),
             started_at = LEAST(chat_session.started_at, EXCLUDED.started_at),
             last_activity_at = GREATEST(chat_session.last_activity_at, EXCLUDED.last_activity_at),
             original_source_path = EXCLUDED.original_source_path,
             raw_source_key = COALESCE(EXCLUDED.raw_source_key, chat_session.raw_source_key),
             updated_at = COALESCE(EXCLUDED.updated_at, NOW()),
             deleted_at = CASE WHEN $16 THEN NULL ELSE EXCLUDED.deleted_at END`,
		input.ID, r.userID, input.Source, input.SourceSessionID, input.SourceDeviceID, input.ProjectID, input.CWD,
		input.Title, input.StartedAt.UTC(), input.LastActivityAt.UTC(), input.OriginalSourcePath, input.RawSourceKey, input.CreatedAt, input.UpdatedAt, input.DeletedAt, input.ClearDeleted)
	if err != nil {
		return repository.ChatSession{}, false, mapPgError(err)
	}
	session, err := r.GetChatSessionBySource(ctx, input.Source, input.SourceSessionID)
	return session, created, err
}

// GetChatSessionByID fetches one chat session by PC chat ID.
func (r *Repository) GetChatSessionByID(ctx context.Context, id string) (repository.ChatSession, error) {
	if strings.TrimSpace(id) == "" {
		return repository.ChatSession{}, repository.ErrInvalidArgument
	}
	row := r.pool.QueryRow(ctx, `SELECT `+chatSessionColumns+` FROM chat_session WHERE user_id = $1 AND id = $2`, r.userID, id)
	return scanChatSession(row)
}

// GetChatSessionBySource fetches one chat session by source identity.
func (r *Repository) GetChatSessionBySource(ctx context.Context, source string, sourceSessionID string) (repository.ChatSession, error) {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(sourceSessionID) == "" {
		return repository.ChatSession{}, repository.ErrInvalidArgument
	}
	row := r.pool.QueryRow(ctx, `SELECT `+chatSessionColumns+` FROM chat_session WHERE user_id = $1 AND source = $2 AND source_session_id = $3`, r.userID, source, sourceSessionID)
	return scanChatSession(row)
}

// ListChatSessions returns chat sessions by last activity descending.
func (r *Repository) ListChatSessions(ctx context.Context, filter repository.ListChatSessionsFilter) ([]repository.ChatSession, error) {
	where, args, next, err := r.chatSessionPredicate(filter)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + chatSessionColumns + ` FROM chat_session ` + where + ` ORDER BY last_activity_at DESC, id DESC`
	if filter.Limit > 0 {
		query += fmt.Sprintf(` LIMIT $%d`, next)
		args = append(args, filter.Limit)
		next++
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(` OFFSET $%d`, next)
		args = append(args, filter.Offset)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()
	var sessions []repository.ChatSession
	for rows.Next() {
		session, err := scanChatSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return sessions, nil
}

// CountChatSessions returns the number of chat sessions matching filters.
func (r *Repository) CountChatSessions(ctx context.Context, filter repository.ListChatSessionsFilter) (int, error) {
	where, args, _, err := r.chatSessionPredicate(filter)
	if err != nil {
		return 0, err
	}
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM chat_session `+where, args...).Scan(&count); err != nil {
		return 0, mapPgError(err)
	}
	return count, nil
}

func (r *Repository) chatSessionPredicate(filter repository.ListChatSessionsFilter) (string, []any, int, error) {
	if filter.Limit < 0 || filter.Offset < 0 {
		return "", nil, 0, repository.ErrInvalidArgument
	}
	builder := strings.Builder{}
	builder.WriteString(`WHERE user_id = $1`)
	args := []any{r.userID}
	next := 2
	if filter.OnlyDeleted {
		builder.WriteString(` AND deleted_at IS NOT NULL`)
	} else if !filter.IncludeDeleted {
		builder.WriteString(` AND deleted_at IS NULL`)
	}
	addString := func(sql string, value string) {
		fmt.Fprintf(&builder, sql, next)
		args = append(args, value)
		next++
	}
	if filter.ProjectID != nil {
		addString(` AND project_id = $%d`, *filter.ProjectID)
	}
	if filter.Unassigned {
		builder.WriteString(` AND project_id IS NULL`)
	}
	if filter.Source != nil {
		addString(` AND source = $%d`, *filter.Source)
	}
	if filter.DeviceID != nil {
		addString(` AND source_device_id = $%d`, *filter.DeviceID)
	}
	if filter.DateFrom != nil {
		fmt.Fprintf(&builder, ` AND last_activity_at >= $%d`, next)
		args = append(args, filter.DateFrom.UTC())
		next++
	}
	if filter.DateTo != nil {
		fmt.Fprintf(&builder, ` AND last_activity_at <= $%d`, next)
		args = append(args, filter.DateTo.UTC())
		next++
	}
	if filter.UpdatedAfter != nil {
		fmt.Fprintf(&builder, ` AND updated_at >= $%d`, next)
		args = append(args, filter.UpdatedAfter.UTC())
		next++
	}
	return builder.String(), args, next, nil
}

// SoftDeleteChatSession marks a chat session deleted. Re-deleting an
// already-soft-deleted chat is a no-op for the tombstone timestamp so the
// original deletion time survives — matches SoftDeleteRecord and prevents
// trash-age / gc / sync drift on repeated deletes.
func (r *Repository) SoftDeleteChatSession(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE chat_session SET deleted_at = COALESCE(deleted_at, NOW()) WHERE user_id = $1 AND id = $2`, r.userID, id)
	if err != nil {
		return mapPgError(err)
	}
	return ensureRowsAffected(tag)
}

// RestoreChatSession clears the deleted_at marker on a chat session.
func (r *Repository) RestoreChatSession(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE chat_session SET deleted_at = NULL WHERE user_id = $1 AND id = $2 AND deleted_at IS NOT NULL`, r.userID, id)
	if err != nil {
		return mapPgError(err)
	}
	return ensureRowsAffected(tag)
}

// DeleteChatSession hard-deletes a chat session and its items.
func (r *Repository) DeleteChatSession(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM chat_session WHERE user_id = $1 AND id = $2`, r.userID, id)
	if err != nil {
		return mapPgError(err)
	}
	return ensureRowsAffected(tag)
}

// MaxChatItemOrdinal returns the highest stored ordinal for a session.
func (r *Repository) MaxChatItemOrdinal(ctx context.Context, sessionID string) (int, error) {
	var ordinal *int
	if err := r.pool.QueryRow(ctx, `SELECT MAX(ordinal) FROM chat_item AS ci INNER JOIN chat_session AS cs ON cs.id = ci.session_id WHERE cs.user_id = $1 AND ci.session_id = $2`, r.userID, sessionID).Scan(&ordinal); err != nil {
		return 0, mapPgError(err)
	}
	if ordinal == nil {
		return -1, nil
	}
	return *ordinal, nil
}

// CreateChatItem inserts one chat item.
func (r *Repository) CreateChatItem(ctx context.Context, input repository.CreateChatItemInput) (repository.ChatItem, error) {
	if strings.TrimSpace(input.SessionID) == "" || input.Ordinal < 0 || strings.TrimSpace(input.Role) == "" || strings.TrimSpace(input.ItemType) == "" {
		return repository.ChatItem{}, repository.ErrInvalidArgument
	}
	searchText := input.SearchText
	if searchText == "" && input.Text != nil {
		searchText = *input.Text
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO chat_item (session_id, ordinal, role, item_type, text, search_text, raw_json, created_at)
         SELECT $1, $2, $3, $4, $5, $6, $7, COALESCE($8, NOW())
         WHERE EXISTS (SELECT 1 FROM chat_session WHERE user_id = $9 AND id = $1)
         RETURNING `+chatItemColumns,
		input.SessionID, input.Ordinal, input.Role, input.ItemType, input.Text, searchText, input.RawJSON, input.CreatedAt, r.userID)
	return scanChatItem(row)
}

// AppendChatItems inserts normalized items for an existing chat session atomically.
func (r *Repository) AppendChatItems(ctx context.Context, sessionID string, items []repository.CreateChatItemInput) error {
	if strings.TrimSpace(sessionID) == "" {
		return repository.ErrInvalidArgument
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mapPgError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM chat_session WHERE user_id = $1 AND id = $2`, r.userID, sessionID).Scan(&exists); err != nil {
		return mapPgError(err)
	}
	for _, item := range items {
		if item.Ordinal < 0 || strings.TrimSpace(item.Role) == "" || strings.TrimSpace(item.ItemType) == "" {
			return repository.ErrInvalidArgument
		}
		searchText := item.SearchText
		if searchText == "" && item.Text != nil {
			searchText = *item.Text
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO chat_item (session_id, ordinal, role, item_type, text, search_text, raw_json, created_at)
             VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, NOW()))`,
			sessionID, item.Ordinal, item.Role, item.ItemType, item.Text, searchText, item.RawJSON, item.CreatedAt); err != nil {
			return mapPgError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return mapPgError(err)
	}
	return nil
}

// ReplaceChatItems replaces all normalized items for a chat session atomically.
func (r *Repository) ReplaceChatItems(ctx context.Context, sessionID string, items []repository.CreateChatItemInput) error {
	if strings.TrimSpace(sessionID) == "" {
		return repository.ErrInvalidArgument
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mapPgError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM chat_session WHERE user_id = $1 AND id = $2`, r.userID, sessionID).Scan(&exists); err != nil {
		return mapPgError(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chat_item WHERE session_id = $1`, sessionID); err != nil {
		return mapPgError(err)
	}
	for _, item := range items {
		if item.Ordinal < 0 || strings.TrimSpace(item.Role) == "" || strings.TrimSpace(item.ItemType) == "" {
			return repository.ErrInvalidArgument
		}
		searchText := item.SearchText
		if searchText == "" && item.Text != nil {
			searchText = *item.Text
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO chat_item (session_id, ordinal, role, item_type, text, search_text, raw_json, created_at)
             VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, NOW()))`,
			sessionID, item.Ordinal, item.Role, item.ItemType, item.Text, searchText, item.RawJSON, item.CreatedAt); err != nil {
			return mapPgError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return mapPgError(err)
	}
	return nil
}

// ListChatItems returns all items in ordinal order for a session.
func (r *Repository) ListChatItems(ctx context.Context, sessionID string) ([]repository.ChatItem, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, repository.ErrInvalidArgument
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+prefixedChatItemColumns("ci")+`
         FROM chat_item AS ci INNER JOIN chat_session AS cs ON cs.id = ci.session_id
         WHERE cs.user_id = $1 AND ci.session_id = $2
         ORDER BY ci.ordinal`,
		r.userID, sessionID)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()
	var items []repository.ChatItem
	for rows.Next() {
		item, err := scanChatItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return items, nil
}

// SearchChatItems searches chat item text.
func (r *Repository) SearchChatItems(ctx context.Context, filter repository.SearchChatItemsFilter) ([]repository.ChatSearchResult, error) {
	query := strings.TrimSpace(filter.Query)
	if query == "" || filter.Limit < 0 || filter.Offset < 0 {
		return nil, repository.ErrInvalidArgument
	}
	args := []any{r.userID, query}
	next := 3
	where := strings.Builder{}
	where.WriteString(`WHERE cs.user_id = $1 AND ci.search_vector @@ plainto_tsquery('pg_catalog.simple'::regconfig, $2)`)
	if !filter.IncludeDeleted {
		where.WriteString(` AND cs.deleted_at IS NULL`)
	}
	if !filter.IncludeToolOutputs {
		where.WriteString(` AND ci.item_type != 'tool_output'`)
	}
	if filter.ProjectID != nil {
		fmt.Fprintf(&where, ` AND cs.project_id = $%d`, next)
		args = append(args, *filter.ProjectID)
		next++
	}
	if filter.Source != nil {
		fmt.Fprintf(&where, ` AND cs.source = $%d`, next)
		args = append(args, *filter.Source)
		next++
	}
	if filter.DateFrom != nil {
		fmt.Fprintf(&where, ` AND cs.last_activity_at >= $%d`, next)
		args = append(args, filter.DateFrom.UTC())
		next++
	}
	if filter.DateTo != nil {
		fmt.Fprintf(&where, ` AND cs.last_activity_at <= $%d`, next)
		args = append(args, filter.DateTo.UTC())
		next++
	}
	sqlQuery := `SELECT ` + prefixedChatSessionColumns("cs") + `, ` + prefixedChatItemColumns("ci") + `,
            ts_headline('pg_catalog.simple'::regconfig, ci.search_text, plainto_tsquery('pg_catalog.simple'::regconfig, $2)) AS snippet,
            ts_rank(ci.search_vector, plainto_tsquery('pg_catalog.simple'::regconfig, $2)) AS rank
        FROM chat_item AS ci INNER JOIN chat_session AS cs ON cs.id = ci.session_id ` + where.String() + `
        ORDER BY rank DESC, cs.last_activity_at DESC, ci.ordinal`
	if filter.Limit > 0 {
		sqlQuery += fmt.Sprintf(` LIMIT $%d`, next)
		args = append(args, filter.Limit)
		next++
	}
	if filter.Offset > 0 {
		sqlQuery += fmt.Sprintf(` OFFSET $%d`, next)
		args = append(args, filter.Offset)
	}
	rows, err := r.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()
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
	query := strings.TrimSpace(filter.Query)
	args := []any{r.userID, query}
	next := 3
	where := strings.Builder{}
	where.WriteString(`WHERE user_id = $1 AND search_vector @@ plainto_tsquery('pg_catalog.simple'::regconfig, $2)`)
	if !filter.IncludeDeleted {
		where.WriteString(` AND deleted_at IS NULL`)
	}
	if filter.ProjectID != nil {
		fmt.Fprintf(&where, ` AND project_id = $%d`, next)
		args = append(args, *filter.ProjectID)
		next++
	}
	// Forward DateFrom/DateTo so unified search trims record hits the same
	// way it trims chat hits.
	if filter.DateFrom != nil {
		fmt.Fprintf(&where, ` AND date >= $%d`, next)
		args = append(args, filter.DateFrom.UTC().Format("2006-01-02"))
		next++
	}
	if filter.DateTo != nil {
		fmt.Fprintf(&where, ` AND date <= $%d`, next)
		args = append(args, filter.DateTo.UTC().Format("2006-01-02"))
		next++
	}
	_ = next
	sqlQuery := `SELECT id, user_id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at,
            ts_rank(search_vector, plainto_tsquery('pg_catalog.simple'::regconfig, $2)) AS rank
        FROM records ` + where.String() + `
        ORDER BY rank DESC, date, day_order, id`
	rows, err := r.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()
	results := []repository.DomainSearchResult{}
	for rows.Next() {
		record, rank, err := scanRecordSearchRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, repository.DomainSearchResult{Domain: "records", Record: &record, Rank: rank})
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return results, nil
}

func (r *Repository) recordIDExists(ctx context.Context, id string) (bool, error) {
	var exists int
	if err := r.pool.QueryRow(ctx, `SELECT 1 FROM records WHERE user_id = $1 AND id = $2 LIMIT 1`, r.userID, id).Scan(&exists); err != nil {
		if errors.Is(mapPgError(err), repository.ErrNotFound) {
			return false, nil
		}
		return false, mapPgError(err)
	}
	return true, nil
}

func (r *Repository) chatSessionIDExists(ctx context.Context, id string) (bool, error) {
	var exists int
	if err := r.pool.QueryRow(ctx, `SELECT 1 FROM chat_session WHERE user_id = $1 AND id = $2 LIMIT 1`, r.userID, id).Scan(&exists); err != nil {
		if errors.Is(mapPgError(err), repository.ErrNotFound) {
			return false, nil
		}
		return false, mapPgError(err)
	}
	return true, nil
}

func scanProjectPath(rs rowScanner) (repository.ProjectPath, error) {
	var path repository.ProjectPath
	if err := rs.Scan(&path.ID, &path.ProjectID, &path.Path, &path.DeviceID, &path.CreatedAt, &path.UpdatedAt); err != nil {
		return repository.ProjectPath{}, mapPgError(err)
	}
	path.CreatedAt = path.CreatedAt.UTC()
	path.UpdatedAt = path.UpdatedAt.UTC()
	return path, nil
}

func scanChatSession(rs rowScanner) (repository.ChatSession, error) {
	var s repository.ChatSession
	if err := rs.Scan(&s.ID, &s.UserID, &s.Source, &s.SourceSessionID, &s.SourceDeviceID, &s.ProjectID, &s.CWD, &s.Title, &s.StartedAt, &s.LastActivityAt, &s.OriginalSourcePath, &s.RawSourceKey, &s.CreatedAt, &s.UpdatedAt, &s.DeletedAt); err != nil {
		return repository.ChatSession{}, mapPgError(err)
	}
	s.StartedAt = s.StartedAt.UTC()
	s.LastActivityAt = s.LastActivityAt.UTC()
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()
	if s.DeletedAt != nil {
		utc := s.DeletedAt.UTC()
		s.DeletedAt = &utc
	}
	return s, nil
}

func scanChatItem(rs rowScanner) (repository.ChatItem, error) {
	var item repository.ChatItem
	if err := rs.Scan(&item.ID, &item.SessionID, &item.Ordinal, &item.Role, &item.ItemType, &item.Text, &item.SearchText, &item.RawJSON, &item.CreatedAt); err != nil {
		return repository.ChatItem{}, mapPgError(err)
	}
	item.CreatedAt = item.CreatedAt.UTC()
	return item, nil
}

func scanChatSearchRows(rows pgx.Rows) ([]repository.ChatSearchResult, error) {
	var results []repository.ChatSearchResult
	for rows.Next() {
		session, item, snippet, rank, err := scanChatSearchRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, repository.ChatSearchResult{Session: session, Item: item, Snippet: snippet, Rank: rank})
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return results, nil
}

func scanChatSearchRow(rs rowScanner) (repository.ChatSession, repository.ChatItem, string, float64, error) {
	var session repository.ChatSession
	var item repository.ChatItem
	var snippet string
	var rank float64
	if err := rs.Scan(&session.ID, &session.UserID, &session.Source, &session.SourceSessionID, &session.SourceDeviceID, &session.ProjectID, &session.CWD, &session.Title, &session.StartedAt, &session.LastActivityAt, &session.OriginalSourcePath, &session.RawSourceKey, &session.CreatedAt, &session.UpdatedAt, &session.DeletedAt, &item.ID, &item.SessionID, &item.Ordinal, &item.Role, &item.ItemType, &item.Text, &item.SearchText, &item.RawJSON, &item.CreatedAt, &snippet, &rank); err != nil {
		return repository.ChatSession{}, repository.ChatItem{}, "", 0, mapPgError(err)
	}
	session.StartedAt = session.StartedAt.UTC()
	session.LastActivityAt = session.LastActivityAt.UTC()
	session.CreatedAt = session.CreatedAt.UTC()
	session.UpdatedAt = session.UpdatedAt.UTC()
	if session.DeletedAt != nil {
		utc := session.DeletedAt.UTC()
		session.DeletedAt = &utc
	}
	item.CreatedAt = item.CreatedAt.UTC()
	return session, item, snippet, rank, nil
}

func scanRecordSearchRow(rs rowScanner) (repository.Record, float64, error) {
	var record repository.Record
	var rank float64
	if err := rs.Scan(&record.ID, &record.UserID, &record.Date, &record.DayOrder, &record.HTMLContent, &record.Notes, &record.ProjectID, &record.SourceDeviceID, &record.SourceRef, &record.GitRemoteURL, &record.GitHash, &record.CreatedAt, &record.UpdatedAt, &record.DeletedAt, &rank); err != nil {
		return repository.Record{}, 0, mapPgError(err)
	}
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	if record.DeletedAt != nil {
		utc := record.DeletedAt.UTC()
		record.DeletedAt = &utc
	}
	return record, rank, nil
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
