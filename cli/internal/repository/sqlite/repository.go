package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/timeutil"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Repository implements repository.Repository using SQLite.
type Repository struct {
	db *sql.DB
}

var _ repository.Repository = (*Repository)(nil)

// New constructs a SQLite repository implementation.
// Args: db is an initialized SQLite handle.
// Returns: repository implementation or an error when db is nil.
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, repository.ErrInvalidArgument
	}
	return &Repository{db: db}, nil
}

// CreateRecord inserts a record row.
func (r *Repository) CreateRecord(ctx context.Context, input repository.CreateRecordInput) (repository.Record, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Date) == "" ||
		strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.SourceDeviceID) == "" {
		return repository.Record{}, repository.ErrInvalidArgument
	}
	if strings.TrimSpace(input.DayOrder) == "" {
		input.DayOrder = "n"
	}
	if exists, err := r.chatSessionIDExists(ctx, input.ID); err != nil {
		return repository.Record{}, err
	} else if exists {
		return repository.Record{}, fmt.Errorf("%w: id %s already exists as chat session", repository.ErrConflict, input.ID)
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO records (id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), ?);`,
		input.ID,
		input.Date,
		input.DayOrder,
		nullableString(input.HTMLContent),
		nullableString(input.Notes),
		input.ProjectID,
		input.SourceDeviceID,
		nullableString(input.SourceRef),
		nullableString(input.GitRemoteURL),
		nullableString(input.GitHash),
		nullableTime(input.CreatedAt),
		nullableTime(input.UpdatedAt),
		nullableTime(input.DeletedAt),
	)
	if err != nil {
		return repository.Record{}, mapSQLiteError(err)
	}

	return r.GetRecordByID(ctx, input.ID)
}

// GetRecordByID fetches one record by ID.
func (r *Repository) GetRecordByID(ctx context.Context, id string) (repository.Record, error) {
	if strings.TrimSpace(id) == "" {
		return repository.Record{}, repository.ErrInvalidArgument
	}

	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at
         FROM records WHERE id = ?;`,
		id,
	)
	return scanRecord(row)
}

// UpdateRecord updates mutable record fields.
func (r *Repository) UpdateRecord(ctx context.Context, input repository.UpdateRecordInput) (repository.Record, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Date) == "" || strings.TrimSpace(input.DayOrder) == "" ||
		strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.SourceDeviceID) == "" {
		return repository.Record{}, repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(
		ctx,
		`UPDATE records
         SET date = ?, day_order = ?, html_content = ?, notes = ?, project_id = ?, source_device_id = ?, source_ref = ?, git_remote_url = ?, git_hash = ?,
             updated_at = COALESCE(?, updated_at),
             deleted_at = ?
         WHERE id = ?;`,
		input.Date,
		input.DayOrder,
		nullableString(input.HTMLContent),
		nullableString(input.Notes),
		input.ProjectID,
		input.SourceDeviceID,
		nullableString(input.SourceRef),
		nullableString(input.GitRemoteURL),
		nullableString(input.GitHash),
		nullableTime(input.UpdatedAt),
		nullableTime(input.DeletedAt),
		input.ID,
	)
	if err != nil {
		return repository.Record{}, mapSQLiteError(err)
	}
	if err := ensureRowsAffected(result); err != nil {
		return repository.Record{}, err
	}

	return r.GetRecordByID(ctx, input.ID)
}

// ListRecords returns records sorted by (date, day_order, id).
func (r *Repository) ListRecords(ctx context.Context, filter repository.ListRecordsFilter) ([]repository.Record, error) {
	if filter.Limit < 0 {
		return nil, repository.ErrInvalidArgument
	}

	whereSQL, args, err := listRecordsPredicateSQL(filter, "records", false)
	if err != nil {
		return nil, err
	}

	query := `SELECT id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at FROM records ` + whereSQL + ` ORDER BY date, day_order, id`
	if filter.Query != nil {
		query = `SELECT records.id, records.date, records.day_order, records.html_content, records.notes, records.project_id, records.source_device_id, records.source_ref, records.git_remote_url, records.git_hash, records.created_at, records.updated_at, records.deleted_at
			FROM records INNER JOIN records_fts ON records_fts.id = records.id ` + whereSQL + ` AND records_fts MATCH ? ORDER BY -bm25(records_fts) DESC, records.date, records.day_order, records.id`
		args = append(args, sqliteFTSQuery(*filter.Query))
	}
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()

	records := make([]repository.Record, 0)
	for rows.Next() {
		record, err := scanRecordRows(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return records, nil
}

func listRecordsPredicateSQL(filter repository.ListRecordsFilter, qualifier string, includeQuery bool) (string, []any, error) {
	trimmedQuery := ""
	if filter.Query != nil {
		trimmedQuery = strings.TrimSpace(*filter.Query)
		if trimmedQuery == "" {
			return "", nil, repository.ErrInvalidArgument
		}
	}

	builder := strings.Builder{}
	builder.WriteString(`WHERE 1=1`)
	args := make([]any, 0, 4)
	col := func(name string) string {
		if qualifier == "" {
			return name
		}
		return qualifier + "." + name
	}

	if filter.OnlyDeleted {
		builder.WriteString(` AND ` + col("deleted_at") + ` IS NOT NULL`)
	} else if !filter.IncludeDeleted {
		builder.WriteString(` AND ` + col("deleted_at") + ` IS NULL`)
	}
	if filter.ProjectID != nil {
		builder.WriteString(` AND ` + col("project_id") + ` = ?`)
		args = append(args, *filter.ProjectID)
	}
	if filter.DateFrom != nil {
		builder.WriteString(` AND ` + col("date") + ` >= ?`)
		args = append(args, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		builder.WriteString(` AND ` + col("date") + ` <= ?`)
		args = append(args, *filter.DateTo)
	}
	if filter.HasHTML {
		builder.WriteString(` AND ` + col("html_content") + ` IS NOT NULL`)
	}
	if filter.HasData {
		builder.WriteString(` AND EXISTS (SELECT 1 FROM record_data_files AS rdf WHERE rdf.record_id = ` + col("id") + `)`)
	}
	if filter.UpdatedAfter != nil {
		builder.WriteString(` AND ` + col("updated_at") + ` >= ?`)
		args = append(args, timeutil.FormatUTCMillis(*filter.UpdatedAfter))
	}
	if filter.UpdatedBefore != nil {
		builder.WriteString(` AND ` + col("updated_at") + ` <= ?`)
		args = append(args, timeutil.FormatUTCMillis(*filter.UpdatedBefore))
	}
	if includeQuery && filter.Query != nil {
		escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(trimmedQuery)
		q := "%" + escaped + "%"
		builder.WriteString(` AND (` + col("html_content") + ` LIKE ? ESCAPE '\' OR ` + col("notes") + ` LIKE ? ESCAPE '\')`)
		args = append(args, q, q)
	}
	return builder.String(), args, nil
}

// CountRecords returns the number of records matching non-pagination filters.
func (r *Repository) CountRecords(ctx context.Context, filter repository.ListRecordsFilter) (int, error) {
	whereSQL, args, err := listRecordsPredicateSQL(filter, "records", false)
	if err != nil {
		return 0, err
	}
	var count int
	query := `SELECT COUNT(*) FROM records ` + whereSQL
	if filter.Query != nil {
		query = `SELECT COUNT(*) FROM records INNER JOIN records_fts ON records_fts.id = records.id ` + whereSQL + ` AND records_fts MATCH ?`
		args = append(args, sqliteFTSQuery(*filter.Query))
	}
	err = r.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, mapSQLiteError(err)
	}
	return count, nil
}

// sqliteCountChildrenChunkSize bounds the number of host parameters per
// CountRecordChildren batch. SQLite's default cap is 32766, so 500 leaves
// generous headroom while keeping each round-trip cheap.
const sqliteCountChildrenChunkSize = 500

// CountRecordChildren returns child-row counts keyed by record ID. Inputs
// larger than sqliteCountChildrenChunkSize are split into multiple bound
// queries so the SQLite host-parameter cap is never reached.
func (r *Repository) CountRecordChildren(ctx context.Context, recordIDs []string) (map[string]repository.ChildCounts, error) {
	counts := make(map[string]repository.ChildCounts)
	if len(recordIDs) == 0 {
		return counts, nil
	}
	ids := make([]string, 0, len(recordIDs))
	for _, id := range recordIDs {
		if strings.TrimSpace(id) == "" {
			return nil, repository.ErrInvalidArgument
		}
		ids = append(ids, id)
	}

	setFigures := func(c *repository.ChildCounts, n int) { c.Figures = n }
	setDataFiles := func(c *repository.ChildCounts, n int) { c.DataFiles = n }
	for start := 0; start < len(ids); start += sqliteCountChildrenChunkSize {
		end := start + sqliteCountChildrenChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		args := make([]any, 0, len(chunk))
		placeholders := make([]string, 0, len(chunk))
		for _, id := range chunk {
			args = append(args, id)
			placeholders = append(placeholders, "?")
		}
		inClause := strings.Join(placeholders, ",")
		if err := r.mergeChildCounts(ctx, counts, setFigures, `SELECT record_id, COUNT(*) FROM record_figures WHERE record_id IN (`+inClause+`) GROUP BY record_id`, args...); err != nil {
			return nil, err
		}
		if err := r.mergeChildCounts(ctx, counts, setDataFiles, `SELECT record_id, COUNT(*) FROM record_data_files WHERE record_id IN (`+inClause+`) GROUP BY record_id`, args...); err != nil {
			return nil, err
		}
	}
	return counts, nil
}

func (r *Repository) mergeChildCounts(ctx context.Context, counts map[string]repository.ChildCounts, set func(*repository.ChildCounts, int), query string, args ...any) error {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var recordID string
		var count int
		if err := rows.Scan(&recordID, &count); err != nil {
			return mapSQLiteError(err)
		}
		childCounts := counts[recordID]
		set(&childCounts, count)
		counts[recordID] = childCounts
	}
	if err := rows.Err(); err != nil {
		return mapSQLiteError(err)
	}
	return nil
}

// SoftDeleteRecord sets deleted_at when not already set.
func (r *Repository) SoftDeleteRecord(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(
		ctx,
		`UPDATE records
         SET deleted_at = COALESCE(deleted_at, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now'))
         WHERE id = ?;`,
		id,
	)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// RestoreRecord clears deleted_at.
func (r *Repository) RestoreRecord(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(ctx, `UPDATE records SET deleted_at = NULL WHERE id = ?;`, id)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// DeleteRecord hard-deletes a record row.
func (r *Repository) DeleteRecord(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(ctx, `DELETE FROM records WHERE id = ?;`, id)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// CreateRecordFigure inserts a figure row.
func (r *Repository) CreateRecordFigure(ctx context.Context, input repository.CreateRecordFigureInput) (repository.RecordFigure, error) {
	if strings.TrimSpace(input.RecordID) == "" || strings.TrimSpace(input.Filename) == "" || strings.TrimSpace(input.S3Key) == "" {
		return repository.RecordFigure{}, repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO record_figures(record_id, filename, s3_key, alt_text) VALUES(?, ?, ?, ?);`,
		input.RecordID,
		input.Filename,
		input.S3Key,
		nullableString(input.AltText),
	)
	if err != nil {
		return repository.RecordFigure{}, mapSQLiteError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return repository.RecordFigure{}, fmt.Errorf("last insert id: %w", err)
	}

	return r.GetRecordFigureByID(ctx, id)
}

// GetRecordFigureByID fetches a figure by id.
func (r *Repository) GetRecordFigureByID(ctx context.Context, id int64) (repository.RecordFigure, error) {
	if id <= 0 {
		return repository.RecordFigure{}, repository.ErrInvalidArgument
	}

	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, record_id, filename, s3_key, alt_text, created_at FROM record_figures WHERE id = ?;`,
		id,
	)
	return scanFigure(row)
}

// UpdateRecordFigure updates mutable figure fields.
// Patch semantics: empty Filename/S3Key preserve existing values; nil AltText preserves existing.
func (r *Repository) UpdateRecordFigure(ctx context.Context, input repository.UpdateRecordFigureInput) (repository.RecordFigure, error) {
	if input.ID <= 0 {
		return repository.RecordFigure{}, repository.ErrInvalidArgument
	}

	setClauses := []string{
		"filename = COALESCE(NULLIF(?, ''), filename)",
		"s3_key = COALESCE(NULLIF(?, ''), s3_key)",
	}
	args := []any{input.Filename, input.S3Key}

	if input.AltText != nil {
		setClauses = append(setClauses, "alt_text = ?")
		args = append(args, nullableString(input.AltText))
	}

	args = append(args, input.ID)
	query := fmt.Sprintf(`UPDATE record_figures SET %s WHERE id = ?;`, strings.Join(setClauses, ", "))

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return repository.RecordFigure{}, mapSQLiteError(err)
	}
	if err := ensureRowsAffected(result); err != nil {
		return repository.RecordFigure{}, err
	}

	return r.GetRecordFigureByID(ctx, input.ID)
}

// ListRecordFiguresByRecordID lists figures for a record.
func (r *Repository) ListRecordFiguresByRecordID(ctx context.Context, recordID string) ([]repository.RecordFigure, error) {
	if strings.TrimSpace(recordID) == "" {
		return nil, repository.ErrInvalidArgument
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, record_id, filename, s3_key, alt_text, created_at
         FROM record_figures
         WHERE record_id = ?
         ORDER BY id;`,
		recordID,
	)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()

	figures := make([]repository.RecordFigure, 0)
	for rows.Next() {
		figure, err := scanFigureRows(rows)
		if err != nil {
			return nil, err
		}
		figures = append(figures, figure)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}

	return figures, nil
}

// DeleteRecordFigure deletes a figure row.
func (r *Repository) DeleteRecordFigure(ctx context.Context, id int64) error {
	if id <= 0 {
		return repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(ctx, `DELETE FROM record_figures WHERE id = ?;`, id)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// CreateRecordDataFile inserts a data-file row.
func (r *Repository) CreateRecordDataFile(ctx context.Context, input repository.CreateRecordDataFileInput) (repository.RecordDataFile, error) {
	if strings.TrimSpace(input.RecordID) == "" || strings.TrimSpace(input.Filename) == "" || strings.TrimSpace(input.S3Key) == "" || strings.TrimSpace(input.Hash) == "" {
		return repository.RecordDataFile{}, repository.ErrInvalidArgument
	}
	if input.Size < 0 {
		return repository.RecordDataFile{}, repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO record_data_files(record_id, filename, s3_key, size, hash, description)
         VALUES(?, ?, ?, ?, ?, ?);`,
		input.RecordID,
		input.Filename,
		input.S3Key,
		input.Size,
		input.Hash,
		nullableString(input.Description),
	)
	if err != nil {
		return repository.RecordDataFile{}, mapSQLiteError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return repository.RecordDataFile{}, fmt.Errorf("last insert id: %w", err)
	}

	return r.GetRecordDataFileByID(ctx, id)
}

// GetRecordDataFileByID fetches one data-file row.
func (r *Repository) GetRecordDataFileByID(ctx context.Context, id int64) (repository.RecordDataFile, error) {
	if id <= 0 {
		return repository.RecordDataFile{}, repository.ErrInvalidArgument
	}

	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, record_id, filename, s3_key, size, hash, description, created_at FROM record_data_files WHERE id = ?;`,
		id,
	)
	return scanDataFile(row)
}

// UpdateRecordDataFile updates mutable data-file fields.
func (r *Repository) UpdateRecordDataFile(ctx context.Context, input repository.UpdateRecordDataFileInput) (repository.RecordDataFile, error) {
	if input.ID <= 0 {
		return repository.RecordDataFile{}, repository.ErrInvalidArgument
	}
	if input.Size != nil && *input.Size < 0 {
		return repository.RecordDataFile{}, repository.ErrInvalidArgument
	}

	setClauses := []string{
		"filename = COALESCE(NULLIF(?, ''), filename)",
		"s3_key = COALESCE(NULLIF(?, ''), s3_key)",
		"size = COALESCE(?, size)",
		"hash = COALESCE(NULLIF(?, ''), hash)",
	}
	args := []any{input.Filename, input.S3Key, nullableInt64(input.Size), nullableString(input.Hash)}

	if input.Description != nil {
		setClauses = append(setClauses, "description = ?")
		args = append(args, nullableString(input.Description))
	}

	args = append(args, input.ID)
	query := fmt.Sprintf(`UPDATE record_data_files SET %s WHERE id = ?;`, strings.Join(setClauses, ", "))

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return repository.RecordDataFile{}, mapSQLiteError(err)
	}
	if err := ensureRowsAffected(result); err != nil {
		return repository.RecordDataFile{}, err
	}

	return r.GetRecordDataFileByID(ctx, input.ID)
}

// ListRecordDataFilesByRecordID lists data files for a record.
func (r *Repository) ListRecordDataFilesByRecordID(ctx context.Context, recordID string) ([]repository.RecordDataFile, error) {
	if strings.TrimSpace(recordID) == "" {
		return nil, repository.ErrInvalidArgument
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, record_id, filename, s3_key, size, hash, description, created_at
         FROM record_data_files
         WHERE record_id = ?
         ORDER BY id;`,
		recordID,
	)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()

	files := make([]repository.RecordDataFile, 0)
	for rows.Next() {
		file, err := scanDataFileRows(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}

	return files, nil
}

// DeleteRecordDataFile deletes a data-file row.
func (r *Repository) DeleteRecordDataFile(ctx context.Context, id int64) error {
	if id <= 0 {
		return repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(ctx, `DELETE FROM record_data_files WHERE id = ?;`, id)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// CreateTemplate inserts a template row.
func (r *Repository) CreateTemplate(ctx context.Context, input repository.CreateTemplateInput) (repository.Template, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.HTMLContent) == "" {
		return repository.Template{}, repository.ErrInvalidArgument
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO templates(name, html_content, description, created_at, updated_at)
         VALUES(?, ?, ?, COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')));`,
		input.Name,
		input.HTMLContent,
		nullableString(input.Description),
		nullableTime(input.CreatedAt),
		nullableTime(input.UpdatedAt),
	)
	if err != nil {
		return repository.Template{}, mapSQLiteError(err)
	}

	return r.GetTemplateByName(ctx, input.Name)
}

// GetTemplateByName fetches one template by name.
func (r *Repository) GetTemplateByName(ctx context.Context, name string) (repository.Template, error) {
	if strings.TrimSpace(name) == "" {
		return repository.Template{}, repository.ErrInvalidArgument
	}

	row := r.db.QueryRowContext(
		ctx,
		`SELECT name, html_content, description, created_at, updated_at FROM templates WHERE name = ?;`,
		name,
	)
	return scanTemplate(row)
}

// UpdateTemplate updates mutable template fields.
func (r *Repository) UpdateTemplate(ctx context.Context, input repository.UpdateTemplateInput) (repository.Template, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.HTMLContent) == "" {
		return repository.Template{}, repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(
		ctx,
		`UPDATE templates
         SET html_content = ?, description = ?, updated_at = COALESCE(?, updated_at)
         WHERE name = ?;`,
		input.HTMLContent,
		nullableString(input.Description),
		nullableTime(input.UpdatedAt),
		input.Name,
	)
	if err != nil {
		return repository.Template{}, mapSQLiteError(err)
	}
	if err := ensureRowsAffected(result); err != nil {
		return repository.Template{}, err
	}

	return r.GetTemplateByName(ctx, input.Name)
}

// ListTemplates returns templates sorted by name.
func (r *Repository) ListTemplates(ctx context.Context) ([]repository.Template, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT name, html_content, description, created_at, updated_at FROM templates ORDER BY name;`,
	)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()

	templates := make([]repository.Template, 0)
	for rows.Next() {
		template, err := scanTemplateRows(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}

	return templates, nil
}

// DeleteTemplate deletes a template row.
func (r *Repository) DeleteTemplate(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(ctx, `DELETE FROM templates WHERE name = ?;`, name)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// GetSyncVersion returns the singleton sync_version row.
func (r *Repository) GetSyncVersion(ctx context.Context) (repository.SyncVersion, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, version, updated_at FROM sync_version WHERE id = 1;`)
	var version repository.SyncVersion
	var updatedAtRaw string
	if err := row.Scan(&version.ID, &version.Version, &updatedAtRaw); err != nil {
		return repository.SyncVersion{}, mapSQLiteError(err)
	}

	updatedAt, err := parseTimestamp(updatedAtRaw)
	if err != nil {
		return repository.SyncVersion{}, err
	}
	version.UpdatedAt = updatedAt
	return version, nil
}

// CreateProject inserts a project registry row.
func (r *Repository) CreateProject(ctx context.Context, input repository.CreateRegistryInput) (repository.Project, error) {
	if strings.TrimSpace(input.ID) == "" {
		return repository.Project{}, repository.ErrInvalidArgument
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO projects (id, created_at, updated_at, archived_at)
         VALUES (?, COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), ?);`,
		input.ID, nullableTime(input.CreatedAt), nullableTime(input.UpdatedAt), nullableTime(input.ArchivedAt))
	if err != nil {
		return repository.Project{}, mapSQLiteError(err)
	}
	return r.GetProjectByID(ctx, input.ID)
}

// GetProjectByID fetches a project registry row.
func (r *Repository) GetProjectByID(ctx context.Context, id string) (repository.Project, error) {
	if strings.TrimSpace(id) == "" {
		return repository.Project{}, repository.ErrInvalidArgument
	}
	row := r.db.QueryRowContext(ctx, `SELECT id, created_at, updated_at, archived_at FROM projects WHERE id = ?;`, id)
	return scanProject(row)
}

// ListProjects returns project registry rows sorted by id.
func (r *Repository) ListProjects(ctx context.Context, includeArchived bool) ([]repository.Project, error) {
	query := `SELECT id, created_at, updated_at, archived_at FROM projects`
	if !includeArchived {
		query += ` WHERE archived_at IS NULL`
	}
	query += ` ORDER BY id;`
	rows, err := r.db.QueryContext(
		ctx,
		query,
	)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()

	projects := make([]repository.Project, 0)
	for rows.Next() {
		project, err := scanProjectRows(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return projects, nil
}

// ArchiveProject marks a project as archived.
func (r *Repository) ArchiveProject(ctx context.Context, id string) (repository.Project, error) {
	return r.setProjectArchivedAt(ctx, id, `STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')`)
}

// RestoreProject clears a project's archive timestamp.
func (r *Repository) RestoreProject(ctx context.Context, id string) (repository.Project, error) {
	return r.setProjectArchivedAt(ctx, id, `NULL`)
}

func (r *Repository) setProjectArchivedAt(ctx context.Context, id string, valueSQL string) (repository.Project, error) {
	if strings.TrimSpace(id) == "" {
		return repository.Project{}, repository.ErrInvalidArgument
	}
	result, err := r.db.ExecContext(ctx, fmt.Sprintf(`UPDATE projects SET archived_at = %s WHERE id = ?;`, valueSQL), id)
	if err != nil {
		return repository.Project{}, mapSQLiteError(err)
	}
	if err := ensureRowsAffected(result); err != nil {
		return repository.Project{}, err
	}
	return r.GetProjectByID(ctx, id)
}

// UpsertProjectForImport creates or replaces an imported project when newer.
func (r *Repository) UpsertProjectForImport(ctx context.Context, project repository.Project) (bool, error) {
	return upsertProjectForImport(ctx, r, project)
}

// CreateDevice inserts a device registry row.
func (r *Repository) CreateDevice(ctx context.Context, input repository.CreateRegistryInput) (repository.Device, error) {
	if strings.TrimSpace(input.ID) == "" {
		return repository.Device{}, repository.ErrInvalidArgument
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO devices (id, created_at, updated_at, archived_at)
         VALUES (?, COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), COALESCE(?, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')), ?);`,
		input.ID, nullableTime(input.CreatedAt), nullableTime(input.UpdatedAt), nullableTime(input.ArchivedAt))
	if err != nil {
		return repository.Device{}, mapSQLiteError(err)
	}
	return r.GetDeviceByID(ctx, input.ID)
}

// GetDeviceByID fetches a device registry row.
func (r *Repository) GetDeviceByID(ctx context.Context, id string) (repository.Device, error) {
	if strings.TrimSpace(id) == "" {
		return repository.Device{}, repository.ErrInvalidArgument
	}
	row := r.db.QueryRowContext(ctx, `SELECT id, created_at, updated_at, archived_at FROM devices WHERE id = ?;`, id)
	return scanDevice(row)
}

// ListDevices returns device registry rows sorted by id.
func (r *Repository) ListDevices(ctx context.Context, includeArchived bool) ([]repository.Device, error) {
	query := `SELECT id, created_at, updated_at, archived_at FROM devices`
	if !includeArchived {
		query += ` WHERE archived_at IS NULL`
	}
	query += ` ORDER BY id;`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()

	devices := make([]repository.Device, 0)
	for rows.Next() {
		device, err := scanDeviceRows(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return devices, nil
}

// ArchiveDevice marks a device as archived.
func (r *Repository) ArchiveDevice(ctx context.Context, id string) (repository.Device, error) {
	return r.setDeviceArchivedAt(ctx, id, `STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')`)
}

// RestoreDevice clears a device archive timestamp.
func (r *Repository) RestoreDevice(ctx context.Context, id string) (repository.Device, error) {
	return r.setDeviceArchivedAt(ctx, id, `NULL`)
}

func (r *Repository) setDeviceArchivedAt(ctx context.Context, id string, valueSQL string) (repository.Device, error) {
	if strings.TrimSpace(id) == "" {
		return repository.Device{}, repository.ErrInvalidArgument
	}
	result, err := r.db.ExecContext(ctx, fmt.Sprintf(`UPDATE devices SET archived_at = %s WHERE id = ?;`, valueSQL), id)
	if err != nil {
		return repository.Device{}, mapSQLiteError(err)
	}
	if err := ensureRowsAffected(result); err != nil {
		return repository.Device{}, err
	}
	return r.GetDeviceByID(ctx, id)
}

// UpsertDeviceForImport creates or replaces an imported device when newer.
func (r *Repository) UpsertDeviceForImport(ctx context.Context, device repository.Device) (bool, error) {
	return upsertDeviceForImport(ctx, r, device)
}

// CountActiveRecords returns the number of non-deleted records.
func (r *Repository) CountActiveRecords(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM records WHERE deleted_at IS NULL`).Scan(&count)
	if err != nil {
		return 0, mapSQLiteError(err)
	}
	return count, nil
}

// CountTrashedRecords returns the number of soft-deleted records.
func (r *Repository) CountTrashedRecords(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM records WHERE deleted_at IS NOT NULL`).Scan(&count)
	if err != nil {
		return 0, mapSQLiteError(err)
	}
	return count, nil
}

// PurgeDeletedRecords hard-deletes all soft-deleted records and returns their IDs.
func (r *Repository) PurgeDeletedRecords(ctx context.Context) ([]string, error) {
	// Collect IDs first (needed by callers for filesystem cleanup).
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM records WHERE deleted_at IS NOT NULL`)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, mapSQLiteError(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	if len(ids) == 0 {
		return ids, nil
	}

	// Bulk delete (CASCADE handles child rows).
	_, err = r.db.ExecContext(ctx, `DELETE FROM records WHERE deleted_at IS NOT NULL`)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	return ids, nil
}

type recordRow struct {
	ID             string
	Date           string
	DayOrder       string
	HTMLContent    sql.NullString
	Notes          sql.NullString
	ProjectID      string
	SourceDeviceID string
	SourceRef      sql.NullString
	GitRemoteURL   sql.NullString
	GitHash        sql.NullString
	CreatedAt      string
	UpdatedAt      string
	DeletedAt      sql.NullString
}

func (r recordRow) toModel() (repository.Record, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.Record{}, err
	}
	updatedAt, err := parseTimestamp(r.UpdatedAt)
	if err != nil {
		return repository.Record{}, err
	}
	deletedAt, err := parseNullableTimestamp(r.DeletedAt)
	if err != nil {
		return repository.Record{}, err
	}

	return repository.Record{
		ID:             r.ID,
		Date:           r.Date,
		DayOrder:       r.DayOrder,
		HTMLContent:    nullableStringPtr(r.HTMLContent),
		Notes:          nullableStringPtr(r.Notes),
		ProjectID:      r.ProjectID,
		SourceDeviceID: r.SourceDeviceID,
		SourceRef:      nullableStringPtr(r.SourceRef),
		GitRemoteURL:   nullableStringPtr(r.GitRemoteURL),
		GitHash:        nullableStringPtr(r.GitHash),
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		DeletedAt:      deletedAt,
	}, nil
}

func scanRecord(row *sql.Row) (repository.Record, error) {
	var scanned recordRow
	if err := row.Scan(
		&scanned.ID,
		&scanned.Date,
		&scanned.DayOrder,
		&scanned.HTMLContent,
		&scanned.Notes,
		&scanned.ProjectID,
		&scanned.SourceDeviceID,
		&scanned.SourceRef,
		&scanned.GitRemoteURL,
		&scanned.GitHash,
		&scanned.CreatedAt,
		&scanned.UpdatedAt,
		&scanned.DeletedAt,
	); err != nil {
		return repository.Record{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

func scanRecordRows(rows *sql.Rows) (repository.Record, error) {
	var scanned recordRow
	if err := rows.Scan(
		&scanned.ID,
		&scanned.Date,
		&scanned.DayOrder,
		&scanned.HTMLContent,
		&scanned.Notes,
		&scanned.ProjectID,
		&scanned.SourceDeviceID,
		&scanned.SourceRef,
		&scanned.GitRemoteURL,
		&scanned.GitHash,
		&scanned.CreatedAt,
		&scanned.UpdatedAt,
		&scanned.DeletedAt,
	); err != nil {
		return repository.Record{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

type registryRow struct {
	ID         string
	CreatedAt  string
	UpdatedAt  string
	ArchivedAt sql.NullString
}

func (r registryRow) toProject() (repository.Project, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.Project{}, err
	}
	updatedAt, err := parseTimestamp(r.UpdatedAt)
	if err != nil {
		return repository.Project{}, err
	}
	archivedAt, err := parseNullableTimestamp(r.ArchivedAt)
	if err != nil {
		return repository.Project{}, err
	}
	return repository.Project{ID: r.ID, CreatedAt: createdAt, UpdatedAt: updatedAt, ArchivedAt: archivedAt}, nil
}

func (r registryRow) toDevice() (repository.Device, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.Device{}, err
	}
	updatedAt, err := parseTimestamp(r.UpdatedAt)
	if err != nil {
		return repository.Device{}, err
	}
	archivedAt, err := parseNullableTimestamp(r.ArchivedAt)
	if err != nil {
		return repository.Device{}, err
	}
	return repository.Device{ID: r.ID, CreatedAt: createdAt, UpdatedAt: updatedAt, ArchivedAt: archivedAt}, nil
}

func scanProject(row *sql.Row) (repository.Project, error) {
	var scanned registryRow
	if err := row.Scan(&scanned.ID, &scanned.CreatedAt, &scanned.UpdatedAt, &scanned.ArchivedAt); err != nil {
		return repository.Project{}, mapSQLiteError(err)
	}
	return scanned.toProject()
}

func scanProjectRows(rows *sql.Rows) (repository.Project, error) {
	var scanned registryRow
	if err := rows.Scan(&scanned.ID, &scanned.CreatedAt, &scanned.UpdatedAt, &scanned.ArchivedAt); err != nil {
		return repository.Project{}, mapSQLiteError(err)
	}
	return scanned.toProject()
}

func scanDevice(row *sql.Row) (repository.Device, error) {
	var scanned registryRow
	if err := row.Scan(&scanned.ID, &scanned.CreatedAt, &scanned.UpdatedAt, &scanned.ArchivedAt); err != nil {
		return repository.Device{}, mapSQLiteError(err)
	}
	return scanned.toDevice()
}

func scanDeviceRows(rows *sql.Rows) (repository.Device, error) {
	var scanned registryRow
	if err := rows.Scan(&scanned.ID, &scanned.CreatedAt, &scanned.UpdatedAt, &scanned.ArchivedAt); err != nil {
		return repository.Device{}, mapSQLiteError(err)
	}
	return scanned.toDevice()
}

func upsertProjectForImport(ctx context.Context, r *Repository, project repository.Project) (bool, error) {
	if strings.TrimSpace(project.ID) == "" || project.CreatedAt.IsZero() || project.UpdatedAt.IsZero() {
		return false, repository.ErrInvalidArgument
	}
	existing, err := r.GetProjectByID(ctx, project.ID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return false, err
	}
	if err == nil && !project.UpdatedAt.After(existing.UpdatedAt) {
		return false, nil
	}
	if errors.Is(err, repository.ErrNotFound) {
		_, err = r.CreateProject(ctx, repository.CreateRegistryInput{
			ID:         project.ID,
			CreatedAt:  &project.CreatedAt,
			UpdatedAt:  &project.UpdatedAt,
			ArchivedAt: project.ArchivedAt,
		})
		return true, err
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE projects SET created_at = ?, updated_at = ?, archived_at = ? WHERE id = ?;`,
		nullableTime(&project.CreatedAt), nullableTime(&project.UpdatedAt), nullableTime(project.ArchivedAt), project.ID)
	return true, mapSQLiteError(err)
}

func upsertDeviceForImport(ctx context.Context, r *Repository, device repository.Device) (bool, error) {
	if strings.TrimSpace(device.ID) == "" || device.CreatedAt.IsZero() || device.UpdatedAt.IsZero() {
		return false, repository.ErrInvalidArgument
	}
	existing, err := r.GetDeviceByID(ctx, device.ID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return false, err
	}
	if err == nil && !device.UpdatedAt.After(existing.UpdatedAt) {
		return false, nil
	}
	if errors.Is(err, repository.ErrNotFound) {
		_, err = r.CreateDevice(ctx, repository.CreateRegistryInput{
			ID:         device.ID,
			CreatedAt:  &device.CreatedAt,
			UpdatedAt:  &device.UpdatedAt,
			ArchivedAt: device.ArchivedAt,
		})
		return true, err
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE devices SET created_at = ?, updated_at = ?, archived_at = ? WHERE id = ?;`,
		nullableTime(&device.CreatedAt), nullableTime(&device.UpdatedAt), nullableTime(device.ArchivedAt), device.ID)
	return true, mapSQLiteError(err)
}

type figureRow struct {
	ID        int64
	RecordID  string
	Filename  string
	S3Key     string
	AltText   sql.NullString
	CreatedAt string
}

func (r figureRow) toModel() (repository.RecordFigure, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.RecordFigure{}, err
	}
	return repository.RecordFigure{
		ID:        r.ID,
		RecordID:  r.RecordID,
		Filename:  r.Filename,
		S3Key:     r.S3Key,
		AltText:   nullableStringPtr(r.AltText),
		CreatedAt: createdAt,
	}, nil
}

func scanFigure(row *sql.Row) (repository.RecordFigure, error) {
	var scanned figureRow
	if err := row.Scan(&scanned.ID, &scanned.RecordID, &scanned.Filename, &scanned.S3Key, &scanned.AltText, &scanned.CreatedAt); err != nil {
		return repository.RecordFigure{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

func scanFigureRows(rows *sql.Rows) (repository.RecordFigure, error) {
	var scanned figureRow
	if err := rows.Scan(&scanned.ID, &scanned.RecordID, &scanned.Filename, &scanned.S3Key, &scanned.AltText, &scanned.CreatedAt); err != nil {
		return repository.RecordFigure{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

type dataFileRow struct {
	ID          int64
	RecordID    string
	Filename    string
	S3Key       string
	Size        int64
	Hash        string
	Description sql.NullString
	CreatedAt   string
}

func (r dataFileRow) toModel() (repository.RecordDataFile, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.RecordDataFile{}, err
	}
	return repository.RecordDataFile{
		ID:          r.ID,
		RecordID:    r.RecordID,
		Filename:    r.Filename,
		S3Key:       r.S3Key,
		Size:        r.Size,
		Hash:        r.Hash,
		Description: nullableStringPtr(r.Description),
		CreatedAt:   createdAt,
	}, nil
}

func scanDataFile(row *sql.Row) (repository.RecordDataFile, error) {
	var scanned dataFileRow
	if err := row.Scan(&scanned.ID, &scanned.RecordID, &scanned.Filename, &scanned.S3Key, &scanned.Size, &scanned.Hash, &scanned.Description, &scanned.CreatedAt); err != nil {
		return repository.RecordDataFile{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

func scanDataFileRows(rows *sql.Rows) (repository.RecordDataFile, error) {
	var scanned dataFileRow
	if err := rows.Scan(&scanned.ID, &scanned.RecordID, &scanned.Filename, &scanned.S3Key, &scanned.Size, &scanned.Hash, &scanned.Description, &scanned.CreatedAt); err != nil {
		return repository.RecordDataFile{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

type templateRow struct {
	Name        string
	HTMLContent string
	Description sql.NullString
	CreatedAt   string
	UpdatedAt   string
}

func (r templateRow) toModel() (repository.Template, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.Template{}, err
	}
	updatedAt, err := parseTimestamp(r.UpdatedAt)
	if err != nil {
		return repository.Template{}, err
	}
	return repository.Template{
		Name:        r.Name,
		HTMLContent: r.HTMLContent,
		Description: nullableStringPtr(r.Description),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func scanTemplate(row *sql.Row) (repository.Template, error) {
	var scanned templateRow
	if err := row.Scan(&scanned.Name, &scanned.HTMLContent, &scanned.Description, &scanned.CreatedAt, &scanned.UpdatedAt); err != nil {
		return repository.Template{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

func scanTemplateRows(rows *sql.Rows) (repository.Template, error) {
	var scanned templateRow
	if err := rows.Scan(&scanned.Name, &scanned.HTMLContent, &scanned.Description, &scanned.CreatedAt, &scanned.UpdatedAt); err != nil {
		return repository.Template{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}

func parseTimestamp(raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", raw, err)
	}
	return parsed.UTC(), nil
}

func parseNullableTimestamp(raw sql.NullString) (*time.Time, error) {
	if !raw.Valid {
		return nil, nil
	}
	parsed, err := parseTimestamp(raw.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	// SQLite's strftime('%Y-%m-%dT%H:%M:%fZ', 'now') produces the same shape:
	// always 3 fractional-second digits (millisecond precision).
	return timeutil.FormatUTCMillis(*value)
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func ensureRowsAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func mapSQLiteError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return fmt.Errorf("%w: %s", repository.ErrConflict, err)
		case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
			return fmt.Errorf("%w: %s", repository.ErrForeignKeyViolation, err)
		}
	}
	// Fallback to string matching for drivers that don't expose structured errors.
	message := err.Error()
	switch {
	case strings.Contains(message, "UNIQUE constraint failed"):
		return fmt.Errorf("%w: %s", repository.ErrConflict, message)
	case strings.Contains(message, "FOREIGN KEY constraint failed"):
		return fmt.Errorf("%w: %s", repository.ErrForeignKeyViolation, message)
	default:
		return err
	}
}
