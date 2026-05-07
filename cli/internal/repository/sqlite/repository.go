package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
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

// CreateSlide inserts a slide row.
func (r *Repository) CreateSlide(ctx context.Context, input repository.CreateSlideInput) (repository.Slide, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Date) == "" ||
		strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.SourceDeviceID) == "" {
		return repository.Slide{}, repository.ErrInvalidArgument
	}
	if strings.TrimSpace(input.DayOrder) == "" {
		input.DayOrder = "n"
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO slides (id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at)
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
		return repository.Slide{}, mapSQLiteError(err)
	}

	return r.GetSlideByID(ctx, input.ID)
}

// GetSlideByID fetches one slide by ID.
func (r *Repository) GetSlideByID(ctx context.Context, id string) (repository.Slide, error) {
	if strings.TrimSpace(id) == "" {
		return repository.Slide{}, repository.ErrInvalidArgument
	}

	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at
         FROM slides WHERE id = ?;`,
		id,
	)
	return scanSlide(row)
}

// UpdateSlide updates mutable slide fields.
func (r *Repository) UpdateSlide(ctx context.Context, input repository.UpdateSlideInput) (repository.Slide, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Date) == "" || strings.TrimSpace(input.DayOrder) == "" ||
		strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.SourceDeviceID) == "" {
		return repository.Slide{}, repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(
		ctx,
		`UPDATE slides
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
		return repository.Slide{}, mapSQLiteError(err)
	}
	if err := ensureRowsAffected(result); err != nil {
		return repository.Slide{}, err
	}

	return r.GetSlideByID(ctx, input.ID)
}

// ListSlides returns slides sorted by (date, day_order, id).
func (r *Repository) ListSlides(ctx context.Context, filter repository.ListSlidesFilter) ([]repository.Slide, error) {
	if filter.Limit < 0 {
		return nil, repository.ErrInvalidArgument
	}

	trimmedQuery := ""
	if filter.Query != nil {
		trimmedQuery = strings.TrimSpace(*filter.Query)
		if trimmedQuery == "" {
			return nil, repository.ErrInvalidArgument
		}
	}

	builder := strings.Builder{}
	builder.WriteString(`SELECT id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at FROM slides WHERE 1=1`)
	args := make([]any, 0, 4)

	if filter.OnlyDeleted {
		builder.WriteString(` AND deleted_at IS NOT NULL`)
	} else if !filter.IncludeDeleted {
		builder.WriteString(` AND deleted_at IS NULL`)
	}
	if filter.ProjectID != nil {
		builder.WriteString(` AND project_id = ?`)
		args = append(args, *filter.ProjectID)
	}
	if filter.DateFrom != nil {
		builder.WriteString(` AND date >= ?`)
		args = append(args, *filter.DateFrom)
	}
	if filter.DateTo != nil {
		builder.WriteString(` AND date <= ?`)
		args = append(args, *filter.DateTo)
	}
	if filter.UpdatedAfter != nil {
		builder.WriteString(` AND updated_at >= ?`)
		args = append(args, filter.UpdatedAfter.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	if filter.UpdatedBefore != nil {
		builder.WriteString(` AND updated_at <= ?`)
		args = append(args, filter.UpdatedBefore.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	if filter.Query != nil {
		escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(trimmedQuery)
		q := "%" + escaped + "%"
		builder.WriteString(` AND (html_content LIKE ? ESCAPE '\' OR notes LIKE ? ESCAPE '\' OR project_id LIKE ? ESCAPE '\' OR source_device_id LIKE ? ESCAPE '\' OR source_ref LIKE ? ESCAPE '\')`)
		args = append(args, q, q, q, q, q)
	}
	builder.WriteString(` ORDER BY date, day_order, id`)
	if filter.Limit > 0 {
		builder.WriteString(` LIMIT ?`)
		args = append(args, filter.Limit)
	}

	rows, err := r.db.QueryContext(ctx, builder.String(), args...)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()

	slides := make([]repository.Slide, 0)
	for rows.Next() {
		slide, err := scanSlideRows(rows)
		if err != nil {
			return nil, err
		}
		slides = append(slides, slide)
	}
	if err := rows.Err(); err != nil {
		return nil, mapSQLiteError(err)
	}
	return slides, nil
}

// SoftDeleteSlide sets deleted_at when not already set.
func (r *Repository) SoftDeleteSlide(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(
		ctx,
		`UPDATE slides
         SET deleted_at = COALESCE(deleted_at, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now'))
         WHERE id = ?;`,
		id,
	)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// RestoreSlide clears deleted_at.
func (r *Repository) RestoreSlide(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(ctx, `UPDATE slides SET deleted_at = NULL WHERE id = ?;`, id)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// DeleteSlide hard-deletes a slide row.
func (r *Repository) DeleteSlide(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(ctx, `DELETE FROM slides WHERE id = ?;`, id)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// CreateSlideFigure inserts a figure row.
func (r *Repository) CreateSlideFigure(ctx context.Context, input repository.CreateSlideFigureInput) (repository.SlideFigure, error) {
	if strings.TrimSpace(input.SlideID) == "" || strings.TrimSpace(input.Filename) == "" || strings.TrimSpace(input.S3Key) == "" {
		return repository.SlideFigure{}, repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO slide_figures(slide_id, filename, s3_key, alt_text) VALUES(?, ?, ?, ?);`,
		input.SlideID,
		input.Filename,
		input.S3Key,
		nullableString(input.AltText),
	)
	if err != nil {
		return repository.SlideFigure{}, mapSQLiteError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return repository.SlideFigure{}, fmt.Errorf("last insert id: %w", err)
	}

	return r.GetSlideFigureByID(ctx, id)
}

// GetSlideFigureByID fetches a figure by id.
func (r *Repository) GetSlideFigureByID(ctx context.Context, id int64) (repository.SlideFigure, error) {
	if id <= 0 {
		return repository.SlideFigure{}, repository.ErrInvalidArgument
	}

	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, slide_id, filename, s3_key, alt_text, created_at FROM slide_figures WHERE id = ?;`,
		id,
	)
	return scanFigure(row)
}

// UpdateSlideFigure updates mutable figure fields.
// Patch semantics: empty Filename/S3Key preserve existing values; nil AltText preserves existing.
func (r *Repository) UpdateSlideFigure(ctx context.Context, input repository.UpdateSlideFigureInput) (repository.SlideFigure, error) {
	if input.ID <= 0 {
		return repository.SlideFigure{}, repository.ErrInvalidArgument
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
	query := fmt.Sprintf(`UPDATE slide_figures SET %s WHERE id = ?;`, strings.Join(setClauses, ", "))

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return repository.SlideFigure{}, mapSQLiteError(err)
	}
	if err := ensureRowsAffected(result); err != nil {
		return repository.SlideFigure{}, err
	}

	return r.GetSlideFigureByID(ctx, input.ID)
}

// ListSlideFiguresBySlideID lists figures for a slide.
func (r *Repository) ListSlideFiguresBySlideID(ctx context.Context, slideID string) ([]repository.SlideFigure, error) {
	if strings.TrimSpace(slideID) == "" {
		return nil, repository.ErrInvalidArgument
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, slide_id, filename, s3_key, alt_text, created_at
         FROM slide_figures
         WHERE slide_id = ?
         ORDER BY id;`,
		slideID,
	)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()

	figures := make([]repository.SlideFigure, 0)
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

// DeleteSlideFigure deletes a figure row.
func (r *Repository) DeleteSlideFigure(ctx context.Context, id int64) error {
	if id <= 0 {
		return repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(ctx, `DELETE FROM slide_figures WHERE id = ?;`, id)
	if err != nil {
		return mapSQLiteError(err)
	}
	return ensureRowsAffected(result)
}

// CreateSlideDataFile inserts a data-file row.
func (r *Repository) CreateSlideDataFile(ctx context.Context, input repository.CreateSlideDataFileInput) (repository.SlideDataFile, error) {
	if strings.TrimSpace(input.SlideID) == "" || strings.TrimSpace(input.Filename) == "" || strings.TrimSpace(input.S3Key) == "" || strings.TrimSpace(input.Hash) == "" {
		return repository.SlideDataFile{}, repository.ErrInvalidArgument
	}
	if input.Size < 0 {
		return repository.SlideDataFile{}, repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO slide_data_files(slide_id, filename, s3_key, size, hash, description)
         VALUES(?, ?, ?, ?, ?, ?);`,
		input.SlideID,
		input.Filename,
		input.S3Key,
		input.Size,
		input.Hash,
		nullableString(input.Description),
	)
	if err != nil {
		return repository.SlideDataFile{}, mapSQLiteError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return repository.SlideDataFile{}, fmt.Errorf("last insert id: %w", err)
	}

	return r.GetSlideDataFileByID(ctx, id)
}

// GetSlideDataFileByID fetches one data-file row.
func (r *Repository) GetSlideDataFileByID(ctx context.Context, id int64) (repository.SlideDataFile, error) {
	if id <= 0 {
		return repository.SlideDataFile{}, repository.ErrInvalidArgument
	}

	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, slide_id, filename, s3_key, size, hash, description, created_at FROM slide_data_files WHERE id = ?;`,
		id,
	)
	return scanDataFile(row)
}

// UpdateSlideDataFile updates mutable data-file fields.
func (r *Repository) UpdateSlideDataFile(ctx context.Context, input repository.UpdateSlideDataFileInput) (repository.SlideDataFile, error) {
	if input.ID <= 0 {
		return repository.SlideDataFile{}, repository.ErrInvalidArgument
	}
	if input.Size != nil && *input.Size < 0 {
		return repository.SlideDataFile{}, repository.ErrInvalidArgument
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
	query := fmt.Sprintf(`UPDATE slide_data_files SET %s WHERE id = ?;`, strings.Join(setClauses, ", "))

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return repository.SlideDataFile{}, mapSQLiteError(err)
	}
	if err := ensureRowsAffected(result); err != nil {
		return repository.SlideDataFile{}, err
	}

	return r.GetSlideDataFileByID(ctx, input.ID)
}

// ListSlideDataFilesBySlideID lists data files for a slide.
func (r *Repository) ListSlideDataFilesBySlideID(ctx context.Context, slideID string) ([]repository.SlideDataFile, error) {
	if strings.TrimSpace(slideID) == "" {
		return nil, repository.ErrInvalidArgument
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, slide_id, filename, s3_key, size, hash, description, created_at
         FROM slide_data_files
         WHERE slide_id = ?
         ORDER BY id;`,
		slideID,
	)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer func() { _ = rows.Close() }()

	files := make([]repository.SlideDataFile, 0)
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

// DeleteSlideDataFile deletes a data-file row.
func (r *Repository) DeleteSlideDataFile(ctx context.Context, id int64) error {
	if id <= 0 {
		return repository.ErrInvalidArgument
	}

	result, err := r.db.ExecContext(ctx, `DELETE FROM slide_data_files WHERE id = ?;`, id)
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

// CountActiveSlides returns the number of non-deleted slides.
func (r *Repository) CountActiveSlides(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM slides WHERE deleted_at IS NULL`).Scan(&count)
	if err != nil {
		return 0, mapSQLiteError(err)
	}
	return count, nil
}

// CountTrashedSlides returns the number of soft-deleted slides.
func (r *Repository) CountTrashedSlides(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM slides WHERE deleted_at IS NOT NULL`).Scan(&count)
	if err != nil {
		return 0, mapSQLiteError(err)
	}
	return count, nil
}

// PurgeDeletedSlides hard-deletes all soft-deleted slides and returns their IDs.
func (r *Repository) PurgeDeletedSlides(ctx context.Context) ([]string, error) {
	// Collect IDs first (needed by callers for filesystem cleanup).
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM slides WHERE deleted_at IS NOT NULL`)
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
	_, err = r.db.ExecContext(ctx, `DELETE FROM slides WHERE deleted_at IS NOT NULL`)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	return ids, nil
}

type slideRow struct {
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

func (r slideRow) toModel() (repository.Slide, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.Slide{}, err
	}
	updatedAt, err := parseTimestamp(r.UpdatedAt)
	if err != nil {
		return repository.Slide{}, err
	}
	deletedAt, err := parseNullableTimestamp(r.DeletedAt)
	if err != nil {
		return repository.Slide{}, err
	}

	return repository.Slide{
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

func scanSlide(row *sql.Row) (repository.Slide, error) {
	var scanned slideRow
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
		return repository.Slide{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

func scanSlideRows(rows *sql.Rows) (repository.Slide, error) {
	var scanned slideRow
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
		return repository.Slide{}, mapSQLiteError(err)
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
	SlideID   string
	Filename  string
	S3Key     string
	AltText   sql.NullString
	CreatedAt string
}

func (r figureRow) toModel() (repository.SlideFigure, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.SlideFigure{}, err
	}
	return repository.SlideFigure{
		ID:        r.ID,
		SlideID:   r.SlideID,
		Filename:  r.Filename,
		S3Key:     r.S3Key,
		AltText:   nullableStringPtr(r.AltText),
		CreatedAt: createdAt,
	}, nil
}

func scanFigure(row *sql.Row) (repository.SlideFigure, error) {
	var scanned figureRow
	if err := row.Scan(&scanned.ID, &scanned.SlideID, &scanned.Filename, &scanned.S3Key, &scanned.AltText, &scanned.CreatedAt); err != nil {
		return repository.SlideFigure{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

func scanFigureRows(rows *sql.Rows) (repository.SlideFigure, error) {
	var scanned figureRow
	if err := rows.Scan(&scanned.ID, &scanned.SlideID, &scanned.Filename, &scanned.S3Key, &scanned.AltText, &scanned.CreatedAt); err != nil {
		return repository.SlideFigure{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

type dataFileRow struct {
	ID          int64
	SlideID     string
	Filename    string
	S3Key       string
	Size        int64
	Hash        string
	Description sql.NullString
	CreatedAt   string
}

func (r dataFileRow) toModel() (repository.SlideDataFile, error) {
	createdAt, err := parseTimestamp(r.CreatedAt)
	if err != nil {
		return repository.SlideDataFile{}, err
	}
	return repository.SlideDataFile{
		ID:          r.ID,
		SlideID:     r.SlideID,
		Filename:    r.Filename,
		S3Key:       r.S3Key,
		Size:        r.Size,
		Hash:        r.Hash,
		Description: nullableStringPtr(r.Description),
		CreatedAt:   createdAt,
	}, nil
}

func scanDataFile(row *sql.Row) (repository.SlideDataFile, error) {
	var scanned dataFileRow
	if err := row.Scan(&scanned.ID, &scanned.SlideID, &scanned.Filename, &scanned.S3Key, &scanned.Size, &scanned.Hash, &scanned.Description, &scanned.CreatedAt); err != nil {
		return repository.SlideDataFile{}, mapSQLiteError(err)
	}
	return scanned.toModel()
}

func scanDataFileRows(rows *sql.Rows) (repository.SlideDataFile, error) {
	var scanned dataFileRow
	if err := rows.Scan(&scanned.ID, &scanned.SlideID, &scanned.Filename, &scanned.S3Key, &scanned.Size, &scanned.Hash, &scanned.Description, &scanned.CreatedAt); err != nil {
		return repository.SlideDataFile{}, mapSQLiteError(err)
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

// sqliteTimestampFormat matches SQLite's strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
// output: always 3 fractional-second digits (millisecond precision).
const sqliteTimestampFormat = "2006-01-02T15:04:05.000Z"

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(sqliteTimestampFormat)
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
