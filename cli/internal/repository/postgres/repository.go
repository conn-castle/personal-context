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
	pool   *pgxpool.Pool
	userID string // scopes all slide/sync_version queries to this user
}

var _ repository.Repository = (*Repository)(nil)

// rowScanner is satisfied by both pgx.Row and *pgx.Rows, allowing scan
// helpers to be used for single-row and multi-row queries alike.
type rowScanner interface {
	Scan(dest ...any) error
}

// New constructs a Postgres repository implementation scoped to a specific user.
// Args: pool is an initialized pgx connection pool; userID scopes all queries.
// Returns: repository implementation or an error when pool or userID is invalid.
func New(pool *pgxpool.Pool, userID string) (*Repository, error) {
	if pool == nil {
		return nil, repository.ErrInvalidArgument
	}
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: userID is required for Postgres repository", repository.ErrInvalidArgument)
	}
	return &Repository{pool: pool, userID: userID}, nil
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

	row := r.pool.QueryRow(
		ctx,
		`INSERT INTO slides (id, user_id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, COALESCE($12, NOW()), COALESCE($13, NOW()), $14)
         RETURNING id, user_id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at`,
		input.ID,
		r.userID,
		input.Date,
		input.DayOrder,
		input.HTMLContent,
		input.Notes,
		input.ProjectID,
		input.SourceDeviceID,
		input.SourceRef,
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
		`SELECT id, user_id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at
         FROM slides WHERE id = $1 AND user_id = $2`,
		id,
		r.userID,
	)
	return scanSlide(row)
}

// UpdateSlide updates mutable slide fields.
func (r *Repository) UpdateSlide(ctx context.Context, input repository.UpdateSlideInput) (repository.Slide, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Date) == "" || strings.TrimSpace(input.DayOrder) == "" ||
		strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.SourceDeviceID) == "" {
		return repository.Slide{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`UPDATE slides
         SET date = $1, day_order = $2, html_content = $3, notes = $4, project_id = $5, source_device_id = $6, source_ref = $7, git_remote_url = $8, git_hash = $9,
             updated_at = COALESCE($10, updated_at),
             deleted_at = $11
         WHERE id = $12 AND user_id = $13
         RETURNING id, user_id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at`,
		input.Date,
		input.DayOrder,
		input.HTMLContent,
		input.Notes,
		input.ProjectID,
		input.SourceDeviceID,
		input.SourceRef,
		input.GitRemoteURL,
		input.GitHash,
		input.UpdatedAt,
		input.DeletedAt,
		input.ID,
		r.userID,
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
	builder.WriteString(`SELECT id, user_id, date, day_order, html_content, notes, project_id, source_device_id, source_ref, git_remote_url, git_hash, created_at, updated_at, deleted_at FROM slides WHERE user_id = $1`)
	args := []any{r.userID}
	paramIdx := 2

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
	if filter.UpdatedAfter != nil {
		fmt.Fprintf(&builder, ` AND updated_at >= $%d`, paramIdx)
		args = append(args, filter.UpdatedAfter.UTC())
		paramIdx++
	}
	if filter.UpdatedBefore != nil {
		fmt.Fprintf(&builder, ` AND updated_at <= $%d`, paramIdx)
		args = append(args, filter.UpdatedBefore.UTC())
		paramIdx++
	}
	if filter.Query != nil {
		escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(trimmedQuery)
		q := "%" + escaped + "%"
		fmt.Fprintf(&builder, ` AND (html_content ILIKE $%d ESCAPE '\' OR notes ILIKE $%d ESCAPE '\' OR project_id ILIKE $%d ESCAPE '\' OR source_device_id ILIKE $%d ESCAPE '\' OR source_ref ILIKE $%d ESCAPE '\')`, paramIdx, paramIdx+1, paramIdx+2, paramIdx+3, paramIdx+4)
		args = append(args, q, q, q, q, q)
		paramIdx += 5
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
		slide, err := scanSlide(rows)
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
		`UPDATE slides SET deleted_at = COALESCE(deleted_at, NOW()) WHERE id = $1 AND user_id = $2`,
		id,
		r.userID,
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

	tag, err := r.pool.Exec(ctx, `UPDATE slides SET deleted_at = NULL WHERE id = $1 AND user_id = $2`, id, r.userID)
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

	tag, err := r.pool.Exec(ctx, `DELETE FROM slides WHERE id = $1 AND user_id = $2`, id, r.userID)
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
		`WITH scoped_slide AS (
             SELECT id
             FROM slides
             WHERE id = $1 AND user_id = $2
         )
         INSERT INTO slide_figures(slide_id, filename, s3_key, alt_text)
         SELECT id, $3, $4, $5
         FROM scoped_slide
         RETURNING id, slide_id, filename, s3_key, alt_text, created_at`,
		input.SlideID,
		r.userID,
		input.Filename,
		input.S3Key,
		input.AltText,
	)
	figure, err := scanFigure(row)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Preserve contract behavior: missing parent slide surfaces as FK violation.
			// This also keeps cross-user and missing-slide outcomes indistinguishable.
			return repository.SlideFigure{}, repository.ErrForeignKeyViolation
		}
		return repository.SlideFigure{}, err
	}
	return figure, nil
}

// GetSlideFigureByID fetches a figure by id.
func (r *Repository) GetSlideFigureByID(ctx context.Context, id int64) (repository.SlideFigure, error) {
	if id <= 0 {
		return repository.SlideFigure{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`SELECT f.id, f.slide_id, f.filename, f.s3_key, f.alt_text, f.created_at
         FROM slide_figures AS f
         INNER JOIN slides AS s ON s.id = f.slide_id
         WHERE f.id = $1 AND s.user_id = $2`,
		id,
		r.userID,
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

	args = append(args, input.ID, r.userID)
	query := fmt.Sprintf(
		`UPDATE slide_figures AS f
         SET %s
         FROM slides AS s
         WHERE f.id = $%d
           AND s.id = f.slide_id
           AND s.user_id = $%d
         RETURNING f.id, f.slide_id, f.filename, f.s3_key, f.alt_text, f.created_at`,
		strings.Join(setClauses, ", "),
		nextParam,
		nextParam+1,
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
		`SELECT f.id, f.slide_id, f.filename, f.s3_key, f.alt_text, f.created_at
         FROM slide_figures AS f
         INNER JOIN slides AS s ON s.id = f.slide_id
         WHERE f.slide_id = $1 AND s.user_id = $2
         ORDER BY f.id`,
		slideID,
		r.userID,
	)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	figures := make([]repository.SlideFigure, 0)
	for rows.Next() {
		figure, err := scanFigure(rows)
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

	tag, err := r.pool.Exec(
		ctx,
		`DELETE FROM slide_figures AS f
         USING slides AS s
         WHERE f.id = $1
           AND s.id = f.slide_id
           AND s.user_id = $2`,
		id,
		r.userID,
	)
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
		`WITH scoped_slide AS (
             SELECT id
             FROM slides
             WHERE id = $1 AND user_id = $2
         )
         INSERT INTO slide_data_files(slide_id, filename, s3_key, size, hash, description)
         SELECT id, $3, $4, $5, $6, $7
         FROM scoped_slide
         RETURNING id, slide_id, filename, s3_key, size, hash, description, created_at`,
		input.SlideID,
		r.userID,
		input.Filename,
		input.S3Key,
		input.Size,
		input.Hash,
		input.Description,
	)
	file, err := scanDataFile(row)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Preserve contract behavior and avoid leaking tenant existence via error shape.
			return repository.SlideDataFile{}, repository.ErrForeignKeyViolation
		}
		return repository.SlideDataFile{}, err
	}
	return file, nil
}

// GetSlideDataFileByID fetches one data-file row.
func (r *Repository) GetSlideDataFileByID(ctx context.Context, id int64) (repository.SlideDataFile, error) {
	if id <= 0 {
		return repository.SlideDataFile{}, repository.ErrInvalidArgument
	}

	row := r.pool.QueryRow(
		ctx,
		`SELECT d.id, d.slide_id, d.filename, d.s3_key, d.size, d.hash, d.description, d.created_at
         FROM slide_data_files AS d
         INNER JOIN slides AS s ON s.id = d.slide_id
         WHERE d.id = $1 AND s.user_id = $2`,
		id,
		r.userID,
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

	args = append(args, input.ID, r.userID)
	query := fmt.Sprintf(
		`UPDATE slide_data_files AS d
         SET %s
         FROM slides AS s
         WHERE d.id = $%d
           AND s.id = d.slide_id
           AND s.user_id = $%d
         RETURNING d.id, d.slide_id, d.filename, d.s3_key, d.size, d.hash, d.description, d.created_at`,
		strings.Join(setClauses, ", "),
		nextParam,
		nextParam+1,
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
		`SELECT d.id, d.slide_id, d.filename, d.s3_key, d.size, d.hash, d.description, d.created_at
         FROM slide_data_files AS d
         INNER JOIN slides AS s ON s.id = d.slide_id
         WHERE d.slide_id = $1 AND s.user_id = $2
         ORDER BY d.id`,
		slideID,
		r.userID,
	)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	files := make([]repository.SlideDataFile, 0)
	for rows.Next() {
		file, err := scanDataFile(rows)
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

	tag, err := r.pool.Exec(
		ctx,
		`DELETE FROM slide_data_files AS d
         USING slides AS s
         WHERE d.id = $1
           AND s.id = d.slide_id
           AND s.user_id = $2`,
		id,
		r.userID,
	)
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
		template, err := scanTemplate(rows)
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

// GetSyncVersion returns the per-user sync_version row, creating it if absent.
func (r *Repository) GetSyncVersion(ctx context.Context) (repository.SyncVersion, error) {
	var sv repository.SyncVersion
	err := r.pool.QueryRow(ctx,
		`INSERT INTO sync_version (user_id, version, updated_at)
		 VALUES ($1, 0, NOW())
		 ON CONFLICT (user_id) DO NOTHING
		 RETURNING user_id, version, updated_at`, r.userID).
		Scan(&sv.UserID, &sv.Version, &sv.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return repository.SyncVersion{}, mapPgError(err)
	}
	// Row already existed (ON CONFLICT DO NOTHING returns no rows) — fetch it.
	if errors.Is(err, pgx.ErrNoRows) {
		err = r.pool.QueryRow(ctx,
			`SELECT user_id, version, updated_at FROM sync_version WHERE user_id = $1`, r.userID).
			Scan(&sv.UserID, &sv.Version, &sv.UpdatedAt)
		if err != nil {
			return repository.SyncVersion{}, mapPgError(err)
		}
	}
	sv.UpdatedAt = sv.UpdatedAt.UTC()
	return sv, nil
}

// CreateProject inserts a project registry row.
func (r *Repository) CreateProject(ctx context.Context, input repository.CreateRegistryInput) (repository.Project, error) {
	if strings.TrimSpace(input.ID) == "" {
		return repository.Project{}, repository.ErrInvalidArgument
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO projects (user_id, id, created_at, updated_at, archived_at)
         VALUES ($1, $2, COALESCE($3, NOW()), COALESCE($4, NOW()), $5)
         RETURNING id, created_at, updated_at, archived_at`,
		r.userID, input.ID, input.CreatedAt, input.UpdatedAt, input.ArchivedAt)
	return scanProject(row)
}

// GetProjectByID fetches a project registry row.
func (r *Repository) GetProjectByID(ctx context.Context, id string) (repository.Project, error) {
	if strings.TrimSpace(id) == "" {
		return repository.Project{}, repository.ErrInvalidArgument
	}
	row := r.pool.QueryRow(ctx,
		`SELECT id, created_at, updated_at, archived_at FROM projects WHERE user_id = $1 AND id = $2`,
		r.userID, id)
	return scanProject(row)
}

// ListProjects returns project registry rows sorted by id.
func (r *Repository) ListProjects(ctx context.Context, includeArchived bool) ([]repository.Project, error) {
	query := `SELECT id, created_at, updated_at, archived_at FROM projects WHERE user_id = $1`
	if !includeArchived {
		query += ` AND archived_at IS NULL`
	}
	query += ` ORDER BY id`
	rows, err := r.pool.Query(
		ctx,
		query,
		r.userID,
	)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	projects := make([]repository.Project, 0)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return projects, nil
}

// ArchiveProject marks a project as archived.
func (r *Repository) ArchiveProject(ctx context.Context, id string) (repository.Project, error) {
	return r.setProjectArchivedAt(ctx, id, "NOW()")
}

// RestoreProject clears a project's archive timestamp.
func (r *Repository) RestoreProject(ctx context.Context, id string) (repository.Project, error) {
	return r.setProjectArchivedAt(ctx, id, "NULL")
}

func (r *Repository) setProjectArchivedAt(ctx context.Context, id string, valueSQL string) (repository.Project, error) {
	if strings.TrimSpace(id) == "" {
		return repository.Project{}, repository.ErrInvalidArgument
	}
	row := r.pool.QueryRow(ctx,
		fmt.Sprintf(`UPDATE projects SET archived_at = %s WHERE user_id = $1 AND id = $2 RETURNING id, created_at, updated_at, archived_at`, valueSQL),
		r.userID, id)
	return scanProject(row)
}

// UpsertProjectForImport creates or replaces an imported project when newer.
func (r *Repository) UpsertProjectForImport(ctx context.Context, project repository.Project) (bool, error) {
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
	tag, err := r.pool.Exec(ctx,
		`UPDATE projects SET created_at = $3, updated_at = $4, archived_at = $5 WHERE user_id = $1 AND id = $2`,
		r.userID, project.ID, project.CreatedAt, project.UpdatedAt, project.ArchivedAt)
	if err != nil {
		return false, mapPgError(err)
	}
	return true, ensureRowsAffected(tag)
}

// CreateDevice inserts a device registry row.
func (r *Repository) CreateDevice(ctx context.Context, input repository.CreateRegistryInput) (repository.Device, error) {
	if strings.TrimSpace(input.ID) == "" {
		return repository.Device{}, repository.ErrInvalidArgument
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO devices (user_id, id, created_at, updated_at, archived_at)
         VALUES ($1, $2, COALESCE($3, NOW()), COALESCE($4, NOW()), $5)
         RETURNING id, created_at, updated_at, archived_at`,
		r.userID, input.ID, input.CreatedAt, input.UpdatedAt, input.ArchivedAt)
	return scanDevice(row)
}

// GetDeviceByID fetches a device registry row.
func (r *Repository) GetDeviceByID(ctx context.Context, id string) (repository.Device, error) {
	if strings.TrimSpace(id) == "" {
		return repository.Device{}, repository.ErrInvalidArgument
	}
	row := r.pool.QueryRow(ctx,
		`SELECT id, created_at, updated_at, archived_at FROM devices WHERE user_id = $1 AND id = $2`,
		r.userID, id)
	return scanDevice(row)
}

// ListDevices returns device registry rows sorted by id.
func (r *Repository) ListDevices(ctx context.Context, includeArchived bool) ([]repository.Device, error) {
	query := `SELECT id, created_at, updated_at, archived_at FROM devices WHERE user_id = $1`
	if !includeArchived {
		query += ` AND archived_at IS NULL`
	}
	query += ` ORDER BY id`
	rows, err := r.pool.Query(ctx, query, r.userID)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	devices := make([]repository.Device, 0)
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return devices, nil
}

// ArchiveDevice marks a device as archived.
func (r *Repository) ArchiveDevice(ctx context.Context, id string) (repository.Device, error) {
	return r.setDeviceArchivedAt(ctx, id, "NOW()")
}

// RestoreDevice clears a device archive timestamp.
func (r *Repository) RestoreDevice(ctx context.Context, id string) (repository.Device, error) {
	return r.setDeviceArchivedAt(ctx, id, "NULL")
}

func (r *Repository) setDeviceArchivedAt(ctx context.Context, id string, valueSQL string) (repository.Device, error) {
	if strings.TrimSpace(id) == "" {
		return repository.Device{}, repository.ErrInvalidArgument
	}
	row := r.pool.QueryRow(ctx,
		fmt.Sprintf(`UPDATE devices SET archived_at = %s WHERE user_id = $1 AND id = $2 RETURNING id, created_at, updated_at, archived_at`, valueSQL),
		r.userID, id)
	return scanDevice(row)
}

// UpsertDeviceForImport creates or replaces an imported device when newer.
func (r *Repository) UpsertDeviceForImport(ctx context.Context, device repository.Device) (bool, error) {
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
	tag, err := r.pool.Exec(ctx,
		`UPDATE devices SET created_at = $3, updated_at = $4, archived_at = $5 WHERE user_id = $1 AND id = $2`,
		r.userID, device.ID, device.CreatedAt, device.UpdatedAt, device.ArchivedAt)
	if err != nil {
		return false, mapPgError(err)
	}
	return true, ensureRowsAffected(tag)
}

// CountActiveSlides returns the number of non-deleted slides.
func (r *Repository) CountActiveSlides(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM slides WHERE user_id = $1 AND deleted_at IS NULL`, r.userID).Scan(&count)
	if err != nil {
		return 0, mapPgError(err)
	}
	return count, nil
}

// CountTrashedSlides returns the number of soft-deleted slides.
func (r *Repository) CountTrashedSlides(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM slides WHERE user_id = $1 AND deleted_at IS NOT NULL`, r.userID).Scan(&count)
	if err != nil {
		return 0, mapPgError(err)
	}
	return count, nil
}

// PurgeDeletedSlides hard-deletes all soft-deleted slides and returns their IDs.
func (r *Repository) PurgeDeletedSlides(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `DELETE FROM slides WHERE user_id = $1 AND deleted_at IS NOT NULL RETURNING id`, r.userID)
	if err != nil {
		return nil, mapPgError(err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, mapPgError(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPgError(err)
	}
	return ids, nil
}

// scanSlide scans a single slide from any row scanner (pgx.Row or pgx.Rows).
func scanSlide(rs rowScanner) (repository.Slide, error) {
	var s repository.Slide
	var date time.Time
	err := rs.Scan(
		&s.ID,
		&s.UserID,
		&date,
		&s.DayOrder,
		&s.HTMLContent,
		&s.Notes,
		&s.ProjectID,
		&s.SourceDeviceID,
		&s.SourceRef,
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

// scanProject scans a single project registry row.
func scanProject(rs rowScanner) (repository.Project, error) {
	var p repository.Project
	err := rs.Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt, &p.ArchivedAt)
	if err != nil {
		return repository.Project{}, mapPgError(err)
	}
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	if p.ArchivedAt != nil {
		utc := p.ArchivedAt.UTC()
		p.ArchivedAt = &utc
	}
	return p, nil
}

// scanDevice scans a single device registry row.
func scanDevice(rs rowScanner) (repository.Device, error) {
	var d repository.Device
	err := rs.Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt, &d.ArchivedAt)
	if err != nil {
		return repository.Device{}, mapPgError(err)
	}
	d.CreatedAt = d.CreatedAt.UTC()
	d.UpdatedAt = d.UpdatedAt.UTC()
	if d.ArchivedAt != nil {
		utc := d.ArchivedAt.UTC()
		d.ArchivedAt = &utc
	}
	return d, nil
}

// scanFigure scans a single figure from any row scanner (pgx.Row or pgx.Rows).
func scanFigure(rs rowScanner) (repository.SlideFigure, error) {
	var f repository.SlideFigure
	err := rs.Scan(&f.ID, &f.SlideID, &f.Filename, &f.S3Key, &f.AltText, &f.CreatedAt)
	if err != nil {
		return repository.SlideFigure{}, mapPgError(err)
	}
	f.CreatedAt = f.CreatedAt.UTC()
	return f, nil
}

// scanDataFile scans a single data file from any row scanner (pgx.Row or pgx.Rows).
func scanDataFile(rs rowScanner) (repository.SlideDataFile, error) {
	var d repository.SlideDataFile
	err := rs.Scan(&d.ID, &d.SlideID, &d.Filename, &d.S3Key, &d.Size, &d.Hash, &d.Description, &d.CreatedAt)
	if err != nil {
		return repository.SlideDataFile{}, mapPgError(err)
	}
	d.CreatedAt = d.CreatedAt.UTC()
	return d, nil
}

// scanTemplate scans a single template from any row scanner (pgx.Row or pgx.Rows).
func scanTemplate(rs rowScanner) (repository.Template, error) {
	var t repository.Template
	err := rs.Scan(&t.Name, &t.HTMLContent, &t.Description, &t.CreatedAt, &t.UpdatedAt)
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
