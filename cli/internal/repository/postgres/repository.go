package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

// Repository implements repository.Repository using Postgres via pgx.
type Repository struct {
	pool *pgxpool.Pool
}

var _ repository.Repository = (*Repository)(nil)

// New constructs a Postgres repository implementation.
// Args: pool is an initialized pgx connection pool.
// Returns: repository implementation or an error when pool is nil.
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, repository.ErrInvalidArgument
	}
	return &Repository{pool: pool}, nil
}

// CreateSlide inserts a slide row.
func (r *Repository) CreateSlide(ctx context.Context, input repository.CreateSlideInput) (repository.Slide, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Date) == "" || strings.TrimSpace(input.HTMLContent) == "" {
		return repository.Slide{}, repository.ErrInvalidArgument
	}
	if strings.TrimSpace(input.DayOrder) == "" {
		input.DayOrder = "n"
	}

	row := r.pool.QueryRow(
		ctx,
		`INSERT INTO slides (id, date, day_order, html_content, notes, project_id, git_remote_url, git_hash, created_at, updated_at, deleted_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE($9, NOW()), COALESCE($10, NOW()), $11)
         RETURNING id, date, day_order, html_content, notes, project_id, git_remote_url, git_hash, created_at, updated_at, deleted_at`,
		input.ID,
		input.Date,
		input.DayOrder,
		input.HTMLContent,
		input.Notes,
		input.ProjectID,
		input.GitRemoteURL,
		input.GitHash,
		input.CreatedAt,
		input.UpdatedAt,
		input.DeletedAt,
	)
	return scanSlide(row)
}

// GetSlideByID fetches one slide by ID.
func (r *Repository) GetSlideByID(ctx context.Context, id string) (repository.Slide, error) {
	if strings.TrimSpace(id) == "" {
		return repository.Slide{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`SELECT id, date, day_order, html_content, notes, project_id, git_remote_url, git_hash, created_at, updated_at, deleted_at
         FROM slides WHERE id = $1`,
		id,
	)
	return scanSlide(row)
}

// UpdateSlide updates mutable slide fields.
func (r *Repository) UpdateSlide(ctx context.Context, input repository.UpdateSlideInput) (repository.Slide, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Date) == "" || strings.TrimSpace(input.DayOrder) == "" || strings.TrimSpace(input.HTMLContent) == "" {
		return repository.Slide{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`UPDATE slides
         SET date = $1, day_order = $2, html_content = $3, notes = $4, project_id = $5, git_remote_url = $6, git_hash = $7,
             updated_at = COALESCE($8, updated_at),
             deleted_at = $9
         WHERE id = $10
         RETURNING id, date, day_order, html_content, notes, project_id, git_remote_url, git_hash, created_at, updated_at, deleted_at`,
		input.Date,
		input.DayOrder,
		input.HTMLContent,
		input.Notes,
		input.ProjectID,
		input.GitRemoteURL,
		input.GitHash,
		input.UpdatedAt,
		input.DeletedAt,
		input.ID,
	)
	return scanSlide(row)
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
	builder.WriteString(`SELECT id, date, day_order, html_content, notes, project_id, git_remote_url, git_hash, created_at, updated_at, deleted_at FROM slides WHERE 1=1`)
	args := make([]any, 0, 8)
	paramIdx := 1

	if filter.OnlyDeleted {
		builder.WriteString(` AND deleted_at IS NOT NULL`)
	} else if !filter.IncludeDeleted {
		builder.WriteString(` AND deleted_at IS NULL`)
	}
	if filter.ProjectID != nil {
		fmt.Fprintf(&builder, ` AND project_id = $%d`, paramIdx)
		args = append(args, *filter.ProjectID)
		paramIdx++
	}
	if filter.DateFrom != nil {
		fmt.Fprintf(&builder, ` AND date >= $%d`, paramIdx)
		args = append(args, *filter.DateFrom)
		paramIdx++
	}
	if filter.DateTo != nil {
		fmt.Fprintf(&builder, ` AND date <= $%d`, paramIdx)
		args = append(args, *filter.DateTo)
		paramIdx++
	}
	if filter.Query != nil {
		escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(trimmedQuery)
		q := "%" + escaped + "%"
		fmt.Fprintf(&builder, ` AND (html_content ILIKE $%d ESCAPE '\' OR notes ILIKE $%d ESCAPE '\' OR project_id ILIKE $%d ESCAPE '\')`, paramIdx, paramIdx+1, paramIdx+2)
		args = append(args, q, q, q)
		paramIdx += 3
	}
	builder.WriteString(` ORDER BY date, day_order, id`)
	if filter.Limit > 0 {
		fmt.Fprintf(&builder, ` LIMIT $%d`, paramIdx)
		args = append(args, filter.Limit)
	}

	rows, err := r.pool.Query(ctx, builder.String(), args...)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	slides := make([]repository.Slide, 0)
	for rows.Next() {
		slide, err := scanSlideRows(rows)
		if err != nil {
			return nil, err
		}
		slides = append(slides, slide)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return slides, nil
}

// SoftDeleteSlide sets deleted_at when not already set.
func (r *Repository) SoftDeleteSlide(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return repository.ErrInvalidArgument
	}

	tag, err := r.pool.Exec(
		ctx,
		`UPDATE slides SET deleted_at = COALESCE(deleted_at, NOW()) WHERE id = $1`,
		id,
	)
	if err != nil {
		return mapPgError(err)
	}
	return ensureRowsAffected(tag)
}

// RestoreSlide clears deleted_at.
func (r *Repository) RestoreSlide(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return repository.ErrInvalidArgument
	}

	tag, err := r.pool.Exec(ctx, `UPDATE slides SET deleted_at = NULL WHERE id = $1`, id)
	if err != nil {
		return mapPgError(err)
	}
	return ensureRowsAffected(tag)
}

// DeleteSlide hard-deletes a slide row.
func (r *Repository) DeleteSlide(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return repository.ErrInvalidArgument
	}

	tag, err := r.pool.Exec(ctx, `DELETE FROM slides WHERE id = $1`, id)
	if err != nil {
		return mapPgError(err)
	}
	return ensureRowsAffected(tag)
}

// CreateSlideFigure inserts a figure row.
func (r *Repository) CreateSlideFigure(ctx context.Context, input repository.CreateSlideFigureInput) (repository.SlideFigure, error) {
	if strings.TrimSpace(input.SlideID) == "" || strings.TrimSpace(input.Filename) == "" || strings.TrimSpace(input.S3Key) == "" {
		return repository.SlideFigure{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`INSERT INTO slide_figures(slide_id, filename, s3_key, alt_text) VALUES($1, $2, $3, $4)
         RETURNING id, slide_id, filename, s3_key, alt_text, created_at`,
		input.SlideID,
		input.Filename,
		input.S3Key,
		input.AltText,
	)
	return scanFigure(row)
}

// GetSlideFigureByID fetches a figure by id.
func (r *Repository) GetSlideFigureByID(ctx context.Context, id int64) (repository.SlideFigure, error) {
	if id <= 0 {
		return repository.SlideFigure{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`SELECT id, slide_id, filename, s3_key, alt_text, created_at FROM slide_figures WHERE id = $1`,
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
		"filename = COALESCE(NULLIF($1, ''), filename)",
		"s3_key = COALESCE(NULLIF($2, ''), s3_key)",
	}
	args := []any{input.Filename, input.S3Key}
	nextParam := 3

	if input.AltText != nil {
		setClauses = append(setClauses, fmt.Sprintf("alt_text = $%d", nextParam))
		args = append(args, input.AltText)
		nextParam++
	}

	args = append(args, input.ID)
	query := fmt.Sprintf(
		`UPDATE slide_figures SET %s WHERE id = $%d RETURNING id, slide_id, filename, s3_key, alt_text, created_at`,
		strings.Join(setClauses, ", "),
		nextParam,
	)

	row := r.pool.QueryRow(ctx, query, args...)
	slide, err := scanFigure(row)
	if err != nil {
		return repository.SlideFigure{}, err
	}
	return slide, nil
}

// ListSlideFiguresBySlideID lists figures for a slide.
func (r *Repository) ListSlideFiguresBySlideID(ctx context.Context, slideID string) ([]repository.SlideFigure, error) {
	if strings.TrimSpace(slideID) == "" {
		return nil, repository.ErrInvalidArgument
	}

	rows, err := r.pool.Query(
		ctx,
		`SELECT id, slide_id, filename, s3_key, alt_text, created_at
         FROM slide_figures
         WHERE slide_id = $1
         ORDER BY id`,
		slideID,
	)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	figures := make([]repository.SlideFigure, 0)
	for rows.Next() {
		figure, err := scanFigureRows(rows)
		if err != nil {
			return nil, err
		}
		figures = append(figures, figure)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}

	return figures, nil
}

// DeleteSlideFigure deletes a figure row.
func (r *Repository) DeleteSlideFigure(ctx context.Context, id int64) error {
	if id <= 0 {
		return repository.ErrInvalidArgument
	}

	tag, err := r.pool.Exec(ctx, `DELETE FROM slide_figures WHERE id = $1`, id)
	if err != nil {
		return mapPgError(err)
	}
	return ensureRowsAffected(tag)
}

// CreateSlideDataFile inserts a data-file row.
func (r *Repository) CreateSlideDataFile(ctx context.Context, input repository.CreateSlideDataFileInput) (repository.SlideDataFile, error) {
	if strings.TrimSpace(input.SlideID) == "" || strings.TrimSpace(input.Filename) == "" || strings.TrimSpace(input.S3Key) == "" || strings.TrimSpace(input.Hash) == "" {
		return repository.SlideDataFile{}, repository.ErrInvalidArgument
	}
	if input.Size < 0 {
		return repository.SlideDataFile{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`INSERT INTO slide_data_files(slide_id, filename, s3_key, size, hash, description)
         VALUES($1, $2, $3, $4, $5, $6)
         RETURNING id, slide_id, filename, s3_key, size, hash, description, created_at`,
		input.SlideID,
		input.Filename,
		input.S3Key,
		input.Size,
		input.Hash,
		input.Description,
	)
	return scanDataFile(row)
}

// GetSlideDataFileByID fetches one data-file row.
func (r *Repository) GetSlideDataFileByID(ctx context.Context, id int64) (repository.SlideDataFile, error) {
	if id <= 0 {
		return repository.SlideDataFile{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`SELECT id, slide_id, filename, s3_key, size, hash, description, created_at FROM slide_data_files WHERE id = $1`,
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
		"filename = COALESCE(NULLIF($1, ''), filename)",
		"s3_key = COALESCE(NULLIF($2, ''), s3_key)",
		"size = COALESCE($3, size)",
		"hash = COALESCE(NULLIF($4, ''), hash)",
	}
	args := []any{input.Filename, input.S3Key, input.Size, input.Hash}
	nextParam := 5

	if input.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", nextParam))
		args = append(args, input.Description)
		nextParam++
	}

	args = append(args, input.ID)
	query := fmt.Sprintf(
		`UPDATE slide_data_files SET %s WHERE id = $%d RETURNING id, slide_id, filename, s3_key, size, hash, description, created_at`,
		strings.Join(setClauses, ", "),
		nextParam,
	)

	row := r.pool.QueryRow(ctx, query, args...)
	file, err := scanDataFile(row)
	if err != nil {
		return repository.SlideDataFile{}, err
	}
	return file, nil
}

// ListSlideDataFilesBySlideID lists data files for a slide.
func (r *Repository) ListSlideDataFilesBySlideID(ctx context.Context, slideID string) ([]repository.SlideDataFile, error) {
	if strings.TrimSpace(slideID) == "" {
		return nil, repository.ErrInvalidArgument
	}

	rows, err := r.pool.Query(
		ctx,
		`SELECT id, slide_id, filename, s3_key, size, hash, description, created_at
         FROM slide_data_files
         WHERE slide_id = $1
         ORDER BY id`,
		slideID,
	)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	files := make([]repository.SlideDataFile, 0)
	for rows.Next() {
		file, err := scanDataFileRows(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}

	return files, nil
}

// DeleteSlideDataFile deletes a data-file row.
func (r *Repository) DeleteSlideDataFile(ctx context.Context, id int64) error {
	if id <= 0 {
		return repository.ErrInvalidArgument
	}

	tag, err := r.pool.Exec(ctx, `DELETE FROM slide_data_files WHERE id = $1`, id)
	if err != nil {
		return mapPgError(err)
	}
	return ensureRowsAffected(tag)
}

// CreateTemplate inserts a template row.
func (r *Repository) CreateTemplate(ctx context.Context, input repository.CreateTemplateInput) (repository.Template, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.HTMLContent) == "" {
		return repository.Template{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`INSERT INTO templates(name, html_content, description, created_at, updated_at)
         VALUES($1, $2, $3, COALESCE($4, NOW()), COALESCE($5, NOW()))
         RETURNING name, html_content, description, created_at, updated_at`,
		input.Name,
		input.HTMLContent,
		input.Description,
		input.CreatedAt,
		input.UpdatedAt,
	)
	return scanTemplate(row)
}

// GetTemplateByName fetches one template by name.
func (r *Repository) GetTemplateByName(ctx context.Context, name string) (repository.Template, error) {
	if strings.TrimSpace(name) == "" {
		return repository.Template{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`SELECT name, html_content, description, created_at, updated_at FROM templates WHERE name = $1`,
		name,
	)
	return scanTemplate(row)
}

// UpdateTemplate updates mutable template fields.
func (r *Repository) UpdateTemplate(ctx context.Context, input repository.UpdateTemplateInput) (repository.Template, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.HTMLContent) == "" {
		return repository.Template{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`UPDATE templates
         SET html_content = $1, description = $2, updated_at = COALESCE($3, updated_at)
         WHERE name = $4
         RETURNING name, html_content, description, created_at, updated_at`,
		input.HTMLContent,
		input.Description,
		input.UpdatedAt,
		input.Name,
	)
	return scanTemplate(row)
}

// ListTemplates returns templates sorted by name.
func (r *Repository) ListTemplates(ctx context.Context) ([]repository.Template, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT name, html_content, description, created_at, updated_at FROM templates ORDER BY name`,
	)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	templates := make([]repository.Template, 0)
	for rows.Next() {
		template, err := scanTemplateRows(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}

	return templates, nil
}

// DeleteTemplate deletes a template row.
func (r *Repository) DeleteTemplate(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return repository.ErrInvalidArgument
	}

	tag, err := r.pool.Exec(ctx, `DELETE FROM templates WHERE name = $1`, name)
	if err != nil {
		return mapPgError(err)
	}
	return ensureRowsAffected(tag)
}

// GetSyncVersion returns the singleton sync_version row.
func (r *Repository) GetSyncVersion(ctx context.Context) (repository.SyncVersion, error) {
	var sv repository.SyncVersion
	err := r.pool.QueryRow(ctx, `SELECT id, version, updated_at FROM sync_version WHERE id = 1`).
		Scan(&sv.ID, &sv.Version, &sv.UpdatedAt)
	if err != nil {
		return repository.SyncVersion{}, mapPgError(err)
	}
	sv.UpdatedAt = sv.UpdatedAt.UTC()
	return sv, nil
}

// ListDistinctProjectIDs returns sorted distinct non-NULL project_id values from active slides.
func (r *Repository) ListDistinctProjectIDs(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT DISTINCT project_id FROM slides WHERE project_id IS NOT NULL AND deleted_at IS NULL ORDER BY project_id`,
	)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	projects := make([]string, 0)
	for rows.Next() {
		var pid string
		if err := rows.Scan(&pid); err != nil {
			return nil, mapPgError(err)
		}
		projects = append(projects, pid)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return projects, nil
}

// scanSlide scans a single slide row from pgx.Row.
func scanSlide(row pgx.Row) (repository.Slide, error) {
	var s repository.Slide
	var date time.Time
	err := row.Scan(
		&s.ID,
		&date,
		&s.DayOrder,
		&s.HTMLContent,
		&s.Notes,
		&s.ProjectID,
		&s.GitRemoteURL,
		&s.GitHash,
		&s.CreatedAt,
		&s.UpdatedAt,
		&s.DeletedAt,
	)
	if err != nil {
		return repository.Slide{}, mapPgError(err)
	}
	s.Date = date.Format("2006-01-02")
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()
	if s.DeletedAt != nil {
		utc := s.DeletedAt.UTC()
		s.DeletedAt = &utc
	}
	return s, nil
}

// scanSlideRows scans a single slide from pgx.Rows.
func scanSlideRows(rows pgx.Rows) (repository.Slide, error) {
	var s repository.Slide
	var date time.Time
	err := rows.Scan(
		&s.ID,
		&date,
		&s.DayOrder,
		&s.HTMLContent,
		&s.Notes,
		&s.ProjectID,
		&s.GitRemoteURL,
		&s.GitHash,
		&s.CreatedAt,
		&s.UpdatedAt,
		&s.DeletedAt,
	)
	if err != nil {
		return repository.Slide{}, mapPgError(err)
	}
	s.Date = date.Format("2006-01-02")
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()
	if s.DeletedAt != nil {
		utc := s.DeletedAt.UTC()
		s.DeletedAt = &utc
	}
	return s, nil
}

// scanFigure scans a single figure from pgx.Row.
func scanFigure(row pgx.Row) (repository.SlideFigure, error) {
	var f repository.SlideFigure
	err := row.Scan(&f.ID, &f.SlideID, &f.Filename, &f.S3Key, &f.AltText, &f.CreatedAt)
	if err != nil {
		return repository.SlideFigure{}, mapPgError(err)
	}
	f.CreatedAt = f.CreatedAt.UTC()
	return f, nil
}

// scanFigureRows scans a single figure from pgx.Rows.
func scanFigureRows(rows pgx.Rows) (repository.SlideFigure, error) {
	var f repository.SlideFigure
	err := rows.Scan(&f.ID, &f.SlideID, &f.Filename, &f.S3Key, &f.AltText, &f.CreatedAt)
	if err != nil {
		return repository.SlideFigure{}, mapPgError(err)
	}
	f.CreatedAt = f.CreatedAt.UTC()
	return f, nil
}

// scanDataFile scans a single data file from pgx.Row.
func scanDataFile(row pgx.Row) (repository.SlideDataFile, error) {
	var d repository.SlideDataFile
	err := row.Scan(&d.ID, &d.SlideID, &d.Filename, &d.S3Key, &d.Size, &d.Hash, &d.Description, &d.CreatedAt)
	if err != nil {
		return repository.SlideDataFile{}, mapPgError(err)
	}
	d.CreatedAt = d.CreatedAt.UTC()
	return d, nil
}

// scanDataFileRows scans a single data file from pgx.Rows.
func scanDataFileRows(rows pgx.Rows) (repository.SlideDataFile, error) {
	var d repository.SlideDataFile
	err := rows.Scan(&d.ID, &d.SlideID, &d.Filename, &d.S3Key, &d.Size, &d.Hash, &d.Description, &d.CreatedAt)
	if err != nil {
		return repository.SlideDataFile{}, mapPgError(err)
	}
	d.CreatedAt = d.CreatedAt.UTC()
	return d, nil
}

// scanTemplate scans a single template from pgx.Row.
func scanTemplate(row pgx.Row) (repository.Template, error) {
	var t repository.Template
	err := row.Scan(&t.Name, &t.HTMLContent, &t.Description, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return repository.Template{}, mapPgError(err)
	}
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	return t, nil
}

// scanTemplateRows scans a single template from pgx.Rows.
func scanTemplateRows(rows pgx.Rows) (repository.Template, error) {
	var t repository.Template
	err := rows.Scan(&t.Name, &t.HTMLContent, &t.Description, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return repository.Template{}, mapPgError(err)
	}
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	return t, nil
}

// mapPgError converts pgx/pgconn errors to repository sentinel errors.
func mapPgError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", repository.ErrConflict, err)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: %s", repository.ErrForeignKeyViolation, err)
		}
	}
	return err
}

// ensureRowsAffected returns ErrNotFound when no rows were affected.
func ensureRowsAffected(tag pgconn.CommandTag) error {
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}
