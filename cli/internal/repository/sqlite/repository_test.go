package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/repository/repositorytest"
	sqlitebootstrap "github.com/conn-castle/personal-context/cli/internal/sqlite"
)

func testMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations", "sqlite"))
}

func newSQLiteRepo(t *testing.T) repository.Repository {
	t.Helper()
	repo, _ := newConcreteRepo(t)
	return repo
}

func newConcreteRepo(t *testing.T) (*Repository, *sql.DB) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pc.db")
	connection, err := sqlitebootstrap.Open(path)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
	})

	if err := connection.ApplyMigrations(context.Background(), testMigrationsDir(t)); err != nil {
		t.Fatalf("ApplyMigrations() error = %v", err)
	}

	repo, err := New(connection.DB())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return repo, connection.DB()
}

func mustCreateSlide(t *testing.T, repo repository.Repository, input repository.CreateSlideInput) repository.Slide {
	t.Helper()

	slide, err := repo.CreateSlide(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateSlide() error = %v", err)
	}
	return slide
}

func TestSQLiteRepositoryContractSuite(t *testing.T) {
	repositorytest.RunContractSuite(t, newSQLiteRepo)
}

func TestNewFailsForNilDB(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("expected error for nil db, got nil")
	}
}

func TestSlideValidationConflictAndNotFoundBranches(t *testing.T) {
	repo, _ := newConcreteRepo(t)
	ctx := context.Background()

	_, err := repo.GetSlideByID(ctx, "")
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty id, got %v", err)
	}

	_, err = repo.CreateSlide(ctx, repository.CreateSlideInput{})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid create input, got %v", err)
	}

	createdAt := time.Date(2026, time.March, 5, 10, 11, 12, 123000000, time.FixedZone("UTC+2", 2*60*60))
	updatedAt := createdAt.Add(2 * time.Minute)
	deletedAt := updatedAt.Add(1 * time.Minute)
	slide := mustCreateSlide(t, repo, repository.CreateSlideInput{
		ID:          "20260305-a1b2c3d4",
		Date:        "2026-03-05",
		HTMLContent: "<h1>x</h1>",
		CreatedAt:   &createdAt,
		UpdatedAt:   &updatedAt,
		DeletedAt:   &deletedAt,
	})
	if slide.DayOrder != "n" {
		t.Fatalf("expected default day_order n, got %q", slide.DayOrder)
	}
	if slide.CreatedAt.Location() != time.UTC || slide.UpdatedAt.Location() != time.UTC {
		t.Fatalf("expected timestamps normalized to UTC, got created=%v updated=%v", slide.CreatedAt, slide.UpdatedAt)
	}
	if slide.DeletedAt == nil {
		t.Fatal("expected deleted_at to be persisted")
	}

	_, err = repo.CreateSlide(ctx, repository.CreateSlideInput{
		ID:          "20260305-a1b2c3d4",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: "<h1>dup</h1>",
	})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate slide id, got %v", err)
	}

	_, err = repo.UpdateSlide(ctx, repository.UpdateSlideInput{})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid update input, got %v", err)
	}

	_, err = repo.UpdateSlide(ctx, repository.UpdateSlideInput{
		ID:          "20260305-ffffeeee",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: "<h1>missing</h1>",
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing slide update, got %v", err)
	}

	if err := repo.SoftDeleteSlide(ctx, "20260305-ffffeeee"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing soft delete, got %v", err)
	}
	if err := repo.RestoreSlide(ctx, "20260305-ffffeeee"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing restore, got %v", err)
	}
	if err := repo.DeleteSlide(ctx, "20260305-ffffeeee"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing delete, got %v", err)
	}
}

func TestAssetValidationAndNotFoundBranches(t *testing.T) {
	repo, _ := newConcreteRepo(t)
	ctx := context.Background()
	slide := mustCreateSlide(t, repo, repository.CreateSlideInput{
		ID:          "20260305-11112222",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: "<h1>assets</h1>",
	})

	_, err := repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid figure create, got %v", err)
	}
	_, err = repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
		SlideID:  "20260305-missing00",
		Filename: "x.png",
		S3Key:    "figures/20260305-missing00/x.png",
	})
	if !errors.Is(err, repository.ErrForeignKeyViolation) {
		t.Fatalf("expected ErrForeignKeyViolation for orphan figure, got %v", err)
	}

	figure, err := repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
		SlideID:  slide.ID,
		Filename: "x.png",
		S3Key:    "figures/20260305-11112222/x.png",
	})
	if err != nil {
		t.Fatalf("CreateSlideFigure() error = %v", err)
	}
	if _, err := repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{ID: figure.ID}); err != nil {
		t.Fatalf("UpdateSlideFigure() with minimal input should succeed, got %v", err)
	}
	if _, err := repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{ID: 999999, Filename: "new.png"}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing figure update, got %v", err)
	}
	if _, err := repo.ListSlideFiguresBySlideID(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty slideID list figures, got %v", err)
	}
	if err := repo.DeleteSlideFigure(ctx, 999999); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing figure delete, got %v", err)
	}

	_, err = repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid data file create, got %v", err)
	}
	_, err = repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
		SlideID:  slide.ID,
		Filename: "x.csv",
		S3Key:    "data/20260305-11112222/x.csv",
		Size:     -1,
		Hash:     strings.Repeat("a", 64),
	})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for negative file size, got %v", err)
	}
	dataFile, err := repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
		SlideID:  slide.ID,
		Filename: "x.csv",
		S3Key:    "data/20260305-11112222/x.csv",
		Size:     1,
		Hash:     strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("CreateSlideDataFile() error = %v", err)
	}
	if _, err := repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{ID: dataFile.ID}); err != nil {
		t.Fatalf("UpdateSlideDataFile() with minimal input should succeed, got %v", err)
	}
	if _, err := repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{ID: 999999, Filename: "new.csv"}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing data-file update, got %v", err)
	}
	negativeSize := int64(-1)
	if _, err := repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{
		ID:   dataFile.ID,
		Size: &negativeSize,
	}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for negative data-file update size, got %v", err)
	}
	if _, err := repo.ListSlideDataFilesBySlideID(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty slideID list data files, got %v", err)
	}
	if err := repo.DeleteSlideDataFile(ctx, 999999); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing data-file delete, got %v", err)
	}
}

func TestSchemaRejectsNegativeSlideDataFileSize(t *testing.T) {
	repo, db := newConcreteRepo(t)
	ctx := context.Background()

	slide := mustCreateSlide(t, repo, repository.CreateSlideInput{
		ID:          "20260305-dddd0001",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: "<h1>schema</h1>",
	})

	_, err := db.ExecContext(
		ctx,
		`INSERT INTO slide_data_files (slide_id, filename, s3_key, size, hash) VALUES (?, ?, ?, ?, ?);`,
		slide.ID,
		"bad.csv",
		"data/20260305-dddd0001/bad.csv",
		-1,
		strings.Repeat("a", 64),
	)
	if err == nil {
		t.Fatal("expected CHECK constraint failure for negative size")
	}
	if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("expected CHECK constraint context, got %v", err)
	}
}

func TestFigureAndDataFileNoOpUpdatesDoNotBumpSyncVersion(t *testing.T) {
	repo, db := newConcreteRepo(t)
	ctx := context.Background()

	slide := mustCreateSlide(t, repo, repository.CreateSlideInput{
		ID:          "20260305-dddd0002",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: "<h1>sync</h1>",
	})

	figure, err := repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
		SlideID:  slide.ID,
		Filename: "plot.png",
		S3Key:    "figures/20260305-dddd0002/plot.png",
	})
	if err != nil {
		t.Fatalf("CreateSlideFigure() error = %v", err)
	}

	dataFile, err := repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
		SlideID:  slide.ID,
		Filename: "data.csv",
		S3Key:    "data/20260305-dddd0002/data.csv",
		Size:     4,
		Hash:     strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatalf("CreateSlideDataFile() error = %v", err)
	}

	beforeNoOp, err := repo.GetSyncVersion(ctx)
	if err != nil {
		t.Fatalf("GetSyncVersion() before no-op updates error = %v", err)
	}

	if _, err := db.ExecContext(
		ctx,
		`UPDATE slide_figures SET filename = filename, s3_key = s3_key, alt_text = alt_text WHERE id = ?;`,
		figure.ID,
	); err != nil {
		t.Fatalf("no-op figure update failed: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`UPDATE slide_data_files SET filename = filename, s3_key = s3_key, size = size, hash = hash, description = description WHERE id = ?;`,
		dataFile.ID,
	); err != nil {
		t.Fatalf("no-op data-file update failed: %v", err)
	}

	afterNoOp, err := repo.GetSyncVersion(ctx)
	if err != nil {
		t.Fatalf("GetSyncVersion() after no-op updates error = %v", err)
	}
	if afterNoOp.Version != beforeNoOp.Version {
		t.Fatalf("expected no-op updates not to bump sync version, before=%d after=%d", beforeNoOp.Version, afterNoOp.Version)
	}

	if _, err := db.ExecContext(
		ctx,
		`UPDATE slide_figures SET filename = ? WHERE id = ?;`,
		"plot-v2.png",
		figure.ID,
	); err != nil {
		t.Fatalf("meaningful figure update failed: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`UPDATE slide_data_files SET size = ? WHERE id = ?;`,
		5,
		dataFile.ID,
	); err != nil {
		t.Fatalf("meaningful data-file update failed: %v", err)
	}

	afterMeaningful, err := repo.GetSyncVersion(ctx)
	if err != nil {
		t.Fatalf("GetSyncVersion() after meaningful updates error = %v", err)
	}
	if afterMeaningful.Version != afterNoOp.Version+2 {
		t.Fatalf(
			"expected two meaningful updates to bump sync version by 2, before=%d after=%d",
			afterNoOp.Version,
			afterMeaningful.Version,
		)
	}
}

func TestTemplateValidationConflictAndNotFoundBranches(t *testing.T) {
	repo, _ := newConcreteRepo(t)
	ctx := context.Background()

	_, err := repo.CreateTemplate(ctx, repository.CreateTemplateInput{})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid template create, got %v", err)
	}

	createdAt := time.Date(2026, time.March, 5, 1, 2, 3, 0, time.FixedZone("UTC-5", -5*60*60))
	updatedAt := createdAt.Add(time.Minute)
	tmpl, err := repo.CreateTemplate(ctx, repository.CreateTemplateInput{
		Name:        "dup-template",
		HTMLContent: "<main>1</main>",
		CreatedAt:   &createdAt,
		UpdatedAt:   &updatedAt,
	})
	if err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
	if tmpl.CreatedAt.Location() != time.UTC || tmpl.UpdatedAt.Location() != time.UTC {
		t.Fatalf("expected UTC timestamps, got created=%v updated=%v", tmpl.CreatedAt, tmpl.UpdatedAt)
	}

	_, err = repo.CreateTemplate(ctx, repository.CreateTemplateInput{
		Name:        "dup-template",
		HTMLContent: "<main>2</main>",
	})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate template name, got %v", err)
	}

	_, err = repo.UpdateTemplate(ctx, repository.UpdateTemplateInput{})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid template update, got %v", err)
	}
	_, err = repo.UpdateTemplate(ctx, repository.UpdateTemplateInput{
		Name:        "missing-template",
		HTMLContent: "<main>x</main>",
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing template update, got %v", err)
	}

	if err := repo.DeleteTemplate(ctx, "missing-template"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing template delete, got %v", err)
	}
}

func TestSyncVersionParseErrorBranch(t *testing.T) {
	repo, db := newConcreteRepo(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `UPDATE sync_version SET updated_at = 'not-a-time' WHERE id = 1;`); err != nil {
		t.Fatalf("corrupt sync_version updated_at failed: %v", err)
	}

	_, err := repo.GetSyncVersion(ctx)
	if err == nil {
		t.Fatal("expected GetSyncVersion() to fail for invalid timestamp")
	}
	if !strings.Contains(err.Error(), "parse timestamp") {
		t.Fatalf("expected parse timestamp context, got %v", err)
	}
}

func TestRowModelConversionAndScanErrorBranches(t *testing.T) {
	repo, db := newConcreteRepo(t)
	_ = repo

	if _, err := (slideRow{CreatedAt: "bad", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}).toModel(); err == nil {
		t.Fatal("expected slideRow.toModel() to fail on bad created_at")
	}
	if _, err := (figureRow{CreatedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected figureRow.toModel() to fail on bad created_at")
	}
	if _, err := (dataFileRow{CreatedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected dataFileRow.toModel() to fail on bad created_at")
	}
	if _, err := (templateRow{CreatedAt: "bad", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}).toModel(); err == nil {
		t.Fatal("expected templateRow.toModel() to fail on bad created_at")
	}

	row := db.QueryRow(`SELECT 1;`)
	if _, err := scanSlide(row); err == nil {
		t.Fatal("expected scanSlide() to fail for wrong column count")
	}
	row = db.QueryRow(`SELECT 1;`)
	if _, err := scanFigure(row); err == nil {
		t.Fatal("expected scanFigure() to fail for wrong column count")
	}
	row = db.QueryRow(`SELECT 1;`)
	if _, err := scanDataFile(row); err == nil {
		t.Fatal("expected scanDataFile() to fail for wrong column count")
	}
	row = db.QueryRow(`SELECT 1;`)
	if _, err := scanTemplate(row); err == nil {
		t.Fatal("expected scanTemplate() to fail for wrong column count")
	}

	rows, err := db.Query(`SELECT 1;`)
	if err != nil {
		t.Fatalf("query rows failed: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected rows.Next() to be true")
	}
	if _, err := scanSlideRows(rows); err == nil {
		t.Fatal("expected scanSlideRows() to fail for wrong column count")
	}

	rows, err = db.Query(`SELECT 1;`)
	if err != nil {
		t.Fatalf("query rows failed: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected rows.Next() to be true")
	}
	if _, err := scanFigureRows(rows); err == nil {
		t.Fatal("expected scanFigureRows() to fail for wrong column count")
	}

	rows, err = db.Query(`SELECT 1;`)
	if err != nil {
		t.Fatalf("query rows failed: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected rows.Next() to be true")
	}
	if _, err := scanDataFileRows(rows); err == nil {
		t.Fatal("expected scanDataFileRows() to fail for wrong column count")
	}

	rows, err = db.Query(`SELECT 1;`)
	if err != nil {
		t.Fatalf("query rows failed: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected rows.Next() to be true")
	}
	if _, err := scanTemplateRows(rows); err == nil {
		t.Fatal("expected scanTemplateRows() to fail for wrong column count")
	}
}

type stubResult struct {
	affected int64
	err      error
}

func (s stubResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (s stubResult) RowsAffected() (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.affected, nil
}

func TestUtilityAndErrorMappingBranches(t *testing.T) {
	if nullableString(nil) != nil {
		t.Fatal("expected nullableString(nil) to be nil")
	}
	text := "x"
	if got := nullableString(&text); got != "x" {
		t.Fatalf("expected nullableString pointer to unwrap, got %v", got)
	}

	if nullableStringPtr(sql.NullString{}) != nil {
		t.Fatal("expected nullableStringPtr invalid to be nil")
	}
	ptr := nullableStringPtr(sql.NullString{Valid: true, String: "x"})
	if ptr == nil || *ptr != "x" {
		t.Fatalf("expected nullableStringPtr valid to return value, got %v", ptr)
	}

	if _, err := parseTimestamp("not-a-time"); err == nil {
		t.Fatal("expected parseTimestamp() to fail")
	}
	if _, err := parseNullableTimestamp(sql.NullString{Valid: true, String: "not-a-time"}); err == nil {
		t.Fatal("expected parseNullableTimestamp() to fail")
	}

	when := time.Date(2026, time.March, 5, 12, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	nullable := nullableTime(&when)
	formatted, ok := nullable.(string)
	if !ok || !strings.HasSuffix(formatted, "Z") {
		t.Fatalf("expected nullableTime() to format UTC RFC3339, got %v", nullable)
	}
	if nullableTime(nil) != nil {
		t.Fatal("expected nullableTime(nil) to be nil")
	}

	if err := ensureRowsAffected(stubResult{err: errors.New("boom")}); err == nil {
		t.Fatal("expected ensureRowsAffected() to propagate rows affected error")
	}
	if err := ensureRowsAffected(stubResult{affected: 0}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for zero affected rows, got %v", err)
	}
	if err := ensureRowsAffected(stubResult{affected: 1}); err != nil {
		t.Fatalf("expected ensureRowsAffected() success, got %v", err)
	}

	if mapSQLiteError(nil) != nil {
		t.Fatal("expected nil mapSQLiteError(nil)")
	}
	if !errors.Is(mapSQLiteError(sql.ErrNoRows), repository.ErrNotFound) {
		t.Fatal("expected sql.ErrNoRows to map to ErrNotFound")
	}
	if !errors.Is(mapSQLiteError(errors.New("UNIQUE constraint failed: slides.id")), repository.ErrConflict) {
		t.Fatal("expected unique violation to map to ErrConflict")
	}
	if !errors.Is(mapSQLiteError(errors.New("FOREIGN KEY constraint failed")), repository.ErrForeignKeyViolation) {
		t.Fatal("expected foreign key violation to map to ErrForeignKeyViolation")
	}

	other := errors.New("other")
	if !errors.Is(mapSQLiteError(other), other) {
		t.Fatal("expected non-sqlite errors to pass through")
	}
}

func TestAdditionalModelTimestampFailureBranches(t *testing.T) {
	if _, err := (slideRow{CreatedAt: "2026-03-05T00:00:00Z", UpdatedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected slideRow.toModel() to fail on bad updated_at")
	}
	if _, err := (slideRow{
		CreatedAt: "2026-03-05T00:00:00Z",
		UpdatedAt: "2026-03-05T00:00:00Z",
		DeletedAt: sql.NullString{Valid: true, String: "bad"},
	}).toModel(); err == nil {
		t.Fatal("expected slideRow.toModel() to fail on bad deleted_at")
	}
	if _, err := (templateRow{CreatedAt: "2026-03-05T00:00:00Z", UpdatedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected templateRow.toModel() to fail on bad updated_at")
	}
}

func TestMethodsFailLoudlyWhenDBIsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close() error = %v", err)
	}

	repo, err := New(db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	projectID := "org/p"

	methods := []struct {
		name string
		run  func() error
	}{
		{name: "CreateSlide", run: func() error {
			_, err := repo.CreateSlide(ctx, repository.CreateSlideInput{
				ID:          "20260320-abcd1234",
				Date:        "2026-03-20",
				DayOrder:    "a",
				HTMLContent: "<h1>x</h1>",
				ProjectID:   &projectID,
			})
			return err
		}},
		{name: "GetSlideByID", run: func() error {
			_, err := repo.GetSlideByID(ctx, "20260320-abcd1234")
			return err
		}},
		{name: "UpdateSlide", run: func() error {
			_, err := repo.UpdateSlide(ctx, repository.UpdateSlideInput{
				ID:          "20260320-abcd1234",
				Date:        "2026-03-20",
				DayOrder:    "a",
				HTMLContent: "<h1>x</h1>",
			})
			return err
		}},
		{name: "ListSlides", run: func() error { _, err := repo.ListSlides(ctx, repository.ListSlidesFilter{}); return err }},
		{name: "SoftDeleteSlide", run: func() error { return repo.SoftDeleteSlide(ctx, "20260320-abcd1234") }},
		{name: "RestoreSlide", run: func() error { return repo.RestoreSlide(ctx, "20260320-abcd1234") }},
		{name: "DeleteSlide", run: func() error { return repo.DeleteSlide(ctx, "20260320-abcd1234") }},
		{name: "CreateSlideFigure", run: func() error {
			_, err := repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
				SlideID:  "20260320-abcd1234",
				Filename: "plot.png",
				S3Key:    "figures/20260320-abcd1234/plot.png",
			})
			return err
		}},
		{name: "GetSlideFigureByID", run: func() error {
			_, err := repo.GetSlideFigureByID(ctx, 1)
			return err
		}},
		{name: "UpdateSlideFigure", run: func() error {
			_, err := repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{ID: 1, Filename: "plot2.png"})
			return err
		}},
		{name: "ListSlideFiguresBySlideID", run: func() error { _, err := repo.ListSlideFiguresBySlideID(ctx, "20260320-abcd1234"); return err }},
		{name: "DeleteSlideFigure", run: func() error { return repo.DeleteSlideFigure(ctx, 1) }},
		{name: "CreateSlideDataFile", run: func() error {
			_, err := repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
				SlideID:  "20260320-abcd1234",
				Filename: "data.csv",
				S3Key:    "data/20260320-abcd1234/data.csv",
				Size:     1,
				Hash:     strings.Repeat("a", 64),
			})
			return err
		}},
		{name: "GetSlideDataFileByID", run: func() error {
			_, err := repo.GetSlideDataFileByID(ctx, 1)
			return err
		}},
		{name: "UpdateSlideDataFile", run: func() error {
			_, err := repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{ID: 1, Filename: "data2.csv"})
			return err
		}},
		{name: "ListSlideDataFilesBySlideID", run: func() error { _, err := repo.ListSlideDataFilesBySlideID(ctx, "20260320-abcd1234"); return err }},
		{name: "DeleteSlideDataFile", run: func() error { return repo.DeleteSlideDataFile(ctx, 1) }},
		{name: "CreateTemplate", run: func() error {
			_, err := repo.CreateTemplate(ctx, repository.CreateTemplateInput{
				Name:        "tmpl",
				HTMLContent: "<main>x</main>",
			})
			return err
		}},
		{name: "GetTemplateByName", run: func() error {
			_, err := repo.GetTemplateByName(ctx, "tmpl")
			return err
		}},
		{name: "UpdateTemplate", run: func() error {
			_, err := repo.UpdateTemplate(ctx, repository.UpdateTemplateInput{
				Name:        "tmpl",
				HTMLContent: "<main>y</main>",
			})
			return err
		}},
		{name: "ListTemplates", run: func() error { _, err := repo.ListTemplates(ctx); return err }},
		{name: "DeleteTemplate", run: func() error { return repo.DeleteTemplate(ctx, "tmpl") }},
		{name: "GetSyncVersion", run: func() error { _, err := repo.GetSyncVersion(ctx); return err }},
	}

	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			if err := method.run(); err == nil {
				t.Fatalf("expected %s to fail on closed db", method.name)
			}
		})
	}
}
