//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	pg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/repository/repositorytest"
)

// sharedContainer holds a single Postgres container reused across tests.
// Each test gets an isolated schema within the same container.
var sharedContainer struct {
	connStr string
}

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, err := pg.Run(ctx,
		"postgres:16-alpine",
		pg.WithDatabase("testdb"),
		pg.WithUsername("test"),
		pg.WithPassword("test"),
		pg.WithSQLDriver("pgx"),
		pg.BasicWaitStrategies(),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres container: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = container.Terminate(ctx) }()

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get connection string: %v\n", err)
		os.Exit(1)
	}
	sharedContainer.connStr = connStr

	os.Exit(m.Run())
}

// schemaCounter provides unique schema names across tests.
var schemaCounter int

func newPostgresRepo(t *testing.T) repository.Repository {
	t.Helper()

	ctx := context.Background()
	schemaCounter++
	schemaName := fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), schemaCounter)

	pool, err := pgxpool.New(ctx, sharedContainer.connStr)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}

	// Create an isolated schema for this test.
	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		pool.Close()
		t.Fatalf("CREATE SCHEMA error = %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf("SET search_path TO %s", schemaName)); err != nil {
		pool.Close()
		t.Fatalf("SET search_path error = %v", err)
	}
	pool.Close()

	// Re-create pool with search_path set via connection string.
	connStr := sharedContainer.connStr + fmt.Sprintf("&search_path=%s", schemaName)
	pool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("pgxpool.New(search_path) error = %v", err)
	}

	if err := ApplySchema(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("ApplySchema() error = %v", err)
	}

	repo, err := New(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("New() error = %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return repo
}

func newConcreteRepo(t *testing.T) (*Repository, *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	schemaCounter++
	schemaName := fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), schemaCounter)

	pool, err := pgxpool.New(ctx, sharedContainer.connStr)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}

	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		pool.Close()
		t.Fatalf("CREATE SCHEMA error = %v", err)
	}
	pool.Close()

	connStr := sharedContainer.connStr + fmt.Sprintf("&search_path=%s", schemaName)
	pool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("pgxpool.New(search_path) error = %v", err)
	}

	if err := ApplySchema(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("ApplySchema() error = %v", err)
	}

	repo, err := New(pool)
	if err != nil {
		pool.Close()
		t.Fatalf("New() error = %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return repo, pool
}

func mustCreateSlide(t *testing.T, repo repository.Repository, input repository.CreateSlideInput) repository.Slide {
	t.Helper()

	slide, err := repo.CreateSlide(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateSlide() error = %v", err)
	}
	return slide
}

func TestPostgresRepositoryContractSuite(t *testing.T) {
	repositorytest.RunContractSuite(t, newPostgresRepo)
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
	repo, pool := newConcreteRepo(t)
	ctx := context.Background()

	slide := mustCreateSlide(t, repo, repository.CreateSlideInput{
		ID:          "20260305-dddd0001",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: "<h1>schema</h1>",
	})

	_, err := pool.Exec(
		ctx,
		`INSERT INTO slide_data_files (slide_id, filename, s3_key, size, hash) VALUES ($1, $2, $3, $4, $5)`,
		slide.ID,
		"bad.csv",
		"data/20260305-dddd0001/bad.csv",
		-1,
		strings.Repeat("a", 64),
	)
	if err == nil {
		t.Fatal("expected CHECK constraint failure for negative size")
	}
	if !strings.Contains(err.Error(), "check") && !strings.Contains(err.Error(), "violates") {
		t.Fatalf("expected CHECK constraint context, got %v", err)
	}
}

func TestFigureAndDataFileNoOpUpdatesDoNotBumpSyncVersion(t *testing.T) {
	repo, _ := newConcreteRepo(t)
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

	if _, err := repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{ID: figure.ID}); err != nil {
		t.Fatalf("UpdateSlideFigure() no-op error = %v", err)
	}
	if _, err := repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{ID: dataFile.ID}); err != nil {
		t.Fatalf("UpdateSlideDataFile() no-op error = %v", err)
	}

	afterNoOp, err := repo.GetSyncVersion(ctx)
	if err != nil {
		t.Fatalf("GetSyncVersion() after no-op updates error = %v", err)
	}
	if afterNoOp.Version != beforeNoOp.Version {
		t.Fatalf("expected no-op updates not to bump sync version, before=%d after=%d", beforeNoOp.Version, afterNoOp.Version)
	}

	if _, err := repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{
		ID:       figure.ID,
		Filename: "plot-v2.png",
	}); err != nil {
		t.Fatalf("UpdateSlideFigure() meaningful update error = %v", err)
	}

	newSize := int64(5)
	if _, err := repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{
		ID:   dataFile.ID,
		Size: &newSize,
	}); err != nil {
		t.Fatalf("UpdateSlideDataFile() meaningful update error = %v", err)
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

func TestErrorPathsWithClosedPool(t *testing.T) {
	repo, pool := newConcreteRepo(t)
	ctx := context.Background()

	// Create a slide so we have a valid ID for methods that need it.
	slide := mustCreateSlide(t, repo, repository.CreateSlideInput{
		ID:          "20260305-aaaa0001",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: "<h1>pool-close</h1>",
	})

	// Close the pool to force all subsequent operations to fail.
	pool.Close()

	if _, err := repo.ListSlides(ctx, repository.ListSlidesFilter{}); err == nil {
		t.Fatal("expected error for ListSlides on closed pool")
	}
	if err := repo.SoftDeleteSlide(ctx, slide.ID); err == nil {
		t.Fatal("expected error for SoftDeleteSlide on closed pool")
	}
	if err := repo.RestoreSlide(ctx, slide.ID); err == nil {
		t.Fatal("expected error for RestoreSlide on closed pool")
	}
	if err := repo.DeleteSlide(ctx, slide.ID); err == nil {
		t.Fatal("expected error for DeleteSlide on closed pool")
	}
	if _, err := repo.ListSlideFiguresBySlideID(ctx, slide.ID); err == nil {
		t.Fatal("expected error for ListSlideFiguresBySlideID on closed pool")
	}
	if err := repo.DeleteSlideFigure(ctx, 1); err == nil {
		t.Fatal("expected error for DeleteSlideFigure on closed pool")
	}
	if _, err := repo.ListSlideDataFilesBySlideID(ctx, slide.ID); err == nil {
		t.Fatal("expected error for ListSlideDataFilesBySlideID on closed pool")
	}
	if err := repo.DeleteSlideDataFile(ctx, 1); err == nil {
		t.Fatal("expected error for DeleteSlideDataFile on closed pool")
	}
	if _, err := repo.ListTemplates(ctx); err == nil {
		t.Fatal("expected error for ListTemplates on closed pool")
	}
	if err := repo.DeleteTemplate(ctx, "any"); err == nil {
		t.Fatal("expected error for DeleteTemplate on closed pool")
	}
	if _, err := repo.GetSyncVersion(ctx); err == nil {
		t.Fatal("expected error for GetSyncVersion on closed pool")
	}
	if _, err := repo.ListDistinctProjectIDs(ctx); err == nil {
		t.Fatal("expected error for ListDistinctProjectIDs on closed pool")
	}
	if _, err := repo.GetSlideByID(ctx, slide.ID); err == nil {
		t.Fatal("expected error for GetSlideByID on closed pool")
	}
	if _, err := repo.CreateSlide(ctx, repository.CreateSlideInput{
		ID: "20260305-aaaa0002", Date: "2026-03-05", HTMLContent: "<h1>x</h1>",
	}); err == nil {
		t.Fatal("expected error for CreateSlide on closed pool")
	}
	if _, err := repo.UpdateSlide(ctx, repository.UpdateSlideInput{
		ID: slide.ID, Date: "2026-03-05", DayOrder: "a", HTMLContent: "<h1>x</h1>",
	}); err == nil {
		t.Fatal("expected error for UpdateSlide on closed pool")
	}
	if _, err := repo.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
		SlideID: slide.ID, Filename: "x.png", S3Key: "figures/x.png",
	}); err == nil {
		t.Fatal("expected error for CreateSlideFigure on closed pool")
	}
	if _, err := repo.GetSlideFigureByID(ctx, 1); err == nil {
		t.Fatal("expected error for GetSlideFigureByID on closed pool")
	}
	if _, err := repo.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{
		ID: 1, Filename: "y.png",
	}); err == nil {
		t.Fatal("expected error for UpdateSlideFigure on closed pool")
	}
	if _, err := repo.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
		SlideID: slide.ID, Filename: "x.csv", S3Key: "data/x.csv", Size: 1, Hash: strings.Repeat("a", 64),
	}); err == nil {
		t.Fatal("expected error for CreateSlideDataFile on closed pool")
	}
	if _, err := repo.GetSlideDataFileByID(ctx, 1); err == nil {
		t.Fatal("expected error for GetSlideDataFileByID on closed pool")
	}
	if _, err := repo.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{
		ID: 1, Filename: "y.csv",
	}); err == nil {
		t.Fatal("expected error for UpdateSlideDataFile on closed pool")
	}
	if _, err := repo.CreateTemplate(ctx, repository.CreateTemplateInput{
		Name: "t", HTMLContent: "<main>x</main>",
	}); err == nil {
		t.Fatal("expected error for CreateTemplate on closed pool")
	}
	if _, err := repo.GetTemplateByName(ctx, "t"); err == nil {
		t.Fatal("expected error for GetTemplateByName on closed pool")
	}
	if _, err := repo.UpdateTemplate(ctx, repository.UpdateTemplateInput{
		Name: "t", HTMLContent: "<main>x</main>",
	}); err == nil {
		t.Fatal("expected error for UpdateTemplate on closed pool")
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
