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

	// Create a test user required by the user_id FK on slides.
	const testUserID = "test-user-contract"
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)`,
		testUserID, schemaName+"@test.com", "hash-placeholder",
	); err != nil {
		pool.Close()
		t.Fatalf("create test user: %v", err)
	}

	repo, err := New(pool, testUserID)
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

	// Create a test user required by the user_id FK on slides.
	const testUserID = "test-user-concrete"
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)`,
		testUserID, schemaName+"@test.com", "hash-placeholder",
	); err != nil {
		pool.Close()
		t.Fatalf("create test user: %v", err)
	}

	repo, err := New(pool, testUserID)
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

func mustCreateUser(t *testing.T, pool *pgxpool.Pool, userID string, email string) {
	t.Helper()

	if _, err := pool.Exec(
		context.Background(),
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)`,
		userID,
		email,
		"hash-placeholder",
	); err != nil {
		t.Fatalf("create test user %q: %v", userID, err)
	}
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
		t.Fatalf("expected ErrForeignKeyViolation for orphan figure create, got %v", err)
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
		SlideID:  "20260305-missing00",
		Filename: "missing.csv",
		S3Key:    "data/20260305-missing00/missing.csv",
		Size:     1,
		Hash:     strings.Repeat("a", 64),
	})
	if !errors.Is(err, repository.ErrForeignKeyViolation) {
		t.Fatalf("expected ErrForeignKeyViolation for orphan data-file create, got %v", err)
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

func TestAssetQueriesAreUserScoped(t *testing.T) {
	repoA, pool := newConcreteRepo(t)
	ctx := context.Background()

	const userB = "test-user-secondary"
	mustCreateUser(t, pool, userB, "secondary@test.local")
	repoB, err := New(pool, userB)
	if err != nil {
		t.Fatalf("New() for secondary user error = %v", err)
	}

	slideA := mustCreateSlide(t, repoA, repository.CreateSlideInput{
		ID:          "20260305-abcdd001",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: "<h1>user-a</h1>",
	})
	slideB := mustCreateSlide(t, repoB, repository.CreateSlideInput{
		ID:          "20260305-abcdd002",
		Date:        "2026-03-05",
		DayOrder:    "b",
		HTMLContent: "<h1>user-b</h1>",
	})

	figureA, err := repoA.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
		SlideID:  slideA.ID,
		Filename: "a.png",
		S3Key:    "figures/20260305-abcdd001/a.png",
	})
	if err != nil {
		t.Fatalf("CreateSlideFigure() for user A error = %v", err)
	}
	if _, err := repoB.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
		SlideID:  slideA.ID,
		Filename: "cross.png",
		S3Key:    "figures/20260305-abcdd001/cross.png",
	}); !errors.Is(err, repository.ErrForeignKeyViolation) {
		t.Fatalf("expected ErrForeignKeyViolation for cross-user figure create, got %v", err)
	}
	if _, err := repoB.GetSlideFigureByID(ctx, figureA.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user figure get, got %v", err)
	}
	if _, err := repoB.UpdateSlideFigure(ctx, repository.UpdateSlideFigureInput{
		ID:       figureA.ID,
		Filename: "steal.png",
	}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user figure update, got %v", err)
	}
	figuresForAFromB, err := repoB.ListSlideFiguresBySlideID(ctx, slideA.ID)
	if err != nil {
		t.Fatalf("ListSlideFiguresBySlideID() for cross-user slide error = %v", err)
	}
	if len(figuresForAFromB) != 0 {
		t.Fatalf("expected no cross-user figures, got %d", len(figuresForAFromB))
	}
	if err := repoB.DeleteSlideFigure(ctx, figureA.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user figure delete, got %v", err)
	}
	if _, err := repoB.CreateSlideFigure(ctx, repository.CreateSlideFigureInput{
		SlideID:  slideB.ID,
		Filename: "b.png",
		S3Key:    "figures/20260305-abcdd002/b.png",
	}); err != nil {
		t.Fatalf("expected same-user figure create to succeed, got %v", err)
	}

	dataFileA, err := repoA.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
		SlideID:  slideA.ID,
		Filename: "a.csv",
		S3Key:    "data/20260305-abcdd001/a.csv",
		Size:     16,
		Hash:     strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("CreateSlideDataFile() for user A error = %v", err)
	}
	if _, err := repoB.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
		SlideID:  slideA.ID,
		Filename: "cross.csv",
		S3Key:    "data/20260305-abcdd001/cross.csv",
		Size:     1,
		Hash:     strings.Repeat("b", 64),
	}); !errors.Is(err, repository.ErrForeignKeyViolation) {
		t.Fatalf("expected ErrForeignKeyViolation for cross-user data-file create, got %v", err)
	}
	if _, err := repoB.GetSlideDataFileByID(ctx, dataFileA.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user data-file get, got %v", err)
	}
	if _, err := repoB.UpdateSlideDataFile(ctx, repository.UpdateSlideDataFileInput{
		ID:       dataFileA.ID,
		Filename: "steal.csv",
	}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user data-file update, got %v", err)
	}
	filesForAFromB, err := repoB.ListSlideDataFilesBySlideID(ctx, slideA.ID)
	if err != nil {
		t.Fatalf("ListSlideDataFilesBySlideID() for cross-user slide error = %v", err)
	}
	if len(filesForAFromB) != 0 {
		t.Fatalf("expected no cross-user data files, got %d", len(filesForAFromB))
	}
	if err := repoB.DeleteSlideDataFile(ctx, dataFileA.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user data-file delete, got %v", err)
	}
	if _, err := repoB.CreateSlideDataFile(ctx, repository.CreateSlideDataFileInput{
		SlideID:  slideB.ID,
		Filename: "b.csv",
		S3Key:    "data/20260305-abcdd002/b.csv",
		Size:     8,
		Hash:     strings.Repeat("c", 64),
	}); err != nil {
		t.Fatalf("expected same-user data-file create to succeed, got %v", err)
	}
}

func TestApplySchemaRejectsLegacyPreAuthCloudSchema(t *testing.T) {
	ctx := context.Background()
	schemaCounter++
	schemaName := fmt.Sprintf("legacy_%d_%d", time.Now().UnixNano(), schemaCounter)

	adminPool, err := pgxpool.New(ctx, sharedContainer.connStr)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		adminPool.Close()
		t.Fatalf("CREATE SCHEMA error = %v", err)
	}
	adminPool.Close()

	connStr := sharedContainer.connStr + fmt.Sprintf("&search_path=%s", schemaName)
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("pgxpool.New(search_path) error = %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
		CREATE TABLE slides (
			id TEXT PRIMARY KEY,
			date DATE NOT NULL,
			day_order TEXT NOT NULL DEFAULT 'n',
			html_content TEXT NOT NULL,
			notes TEXT,
			project_id TEXT,
			git_remote_url TEXT,
			git_hash TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMPTZ
		);
		CREATE TABLE sync_version (
			id INTEGER PRIMARY KEY,
			version BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`); err != nil {
		t.Fatalf("create legacy schema fixtures error = %v", err)
	}

	err = ApplySchema(ctx, pool)
	if err == nil {
		t.Fatal("expected ApplySchema to reject legacy pre-auth schema")
	}
	if !strings.Contains(err.Error(), "legacy pre-auth cloud schema detected") {
		t.Fatalf("expected explicit legacy schema guard error, got %v", err)
	}
}

func TestApplySchemaIsIdempotent(t *testing.T) {
	ctx := context.Background()
	schemaCounter++
	schemaName := fmt.Sprintf("idempotent_%d_%d", time.Now().UnixNano(), schemaCounter)

	adminPool, err := pgxpool.New(ctx, sharedContainer.connStr)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		adminPool.Close()
		t.Fatalf("CREATE SCHEMA error = %v", err)
	}
	adminPool.Close()

	connStr := sharedContainer.connStr + fmt.Sprintf("&search_path=%s", schemaName)
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("pgxpool.New(search_path) error = %v", err)
	}
	defer pool.Close()

	if err := ApplySchema(ctx, pool); err != nil {
		t.Fatalf("first ApplySchema() error = %v", err)
	}
	if err := ApplySchema(ctx, pool); err != nil {
		t.Fatalf("second ApplySchema() should be safe after bootstrap, got %v", err)
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

func TestTemplateChangesCreateSyncRowsForAllUsers(t *testing.T) {
	_, pool := newConcreteRepo(t)
	ctx := context.Background()

	mustCreateUser(t, pool, "template-sync-user", "template-sync-user@test.com")

	if _, err := pool.Exec(ctx,
		`INSERT INTO templates (name, html_content, description) VALUES ($1, $2, $3)`,
		"template-sync",
		"<section>v1</section>",
		"shared template",
	); err != nil {
		t.Fatalf("insert template: %v", err)
	}

	rows, err := pool.Query(ctx, `SELECT user_id, version FROM sync_version ORDER BY user_id`)
	if err != nil {
		t.Fatalf("query sync_version after template insert: %v", err)
	}
	versions := map[string]int64{}
	for rows.Next() {
		var userID string
		var version int64
		if err := rows.Scan(&userID, &version); err != nil {
			rows.Close()
			t.Fatalf("scan sync_version after template insert: %v", err)
		}
		versions[userID] = version
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sync_version after template insert: %v", err)
	}
	rows.Close()

	wantAfterInsert := map[string]int64{
		"template-sync-user": 1,
		"test-user-concrete": 1,
	}
	if len(versions) != len(wantAfterInsert) {
		t.Fatalf("sync_version after template insert = %v, want %v", versions, wantAfterInsert)
	}
	for userID, want := range wantAfterInsert {
		if got := versions[userID]; got != want {
			t.Fatalf("sync_version[%q] after template insert = %d, want %d; all versions = %v", userID, got, want, versions)
		}
	}

	if _, err := pool.Exec(ctx,
		`UPDATE templates SET html_content = $1 WHERE name = $2`,
		"<section>v2</section>",
		"template-sync",
	); err != nil {
		t.Fatalf("update template: %v", err)
	}

	rows, err = pool.Query(ctx, `SELECT user_id, version FROM sync_version ORDER BY user_id`)
	if err != nil {
		t.Fatalf("query sync_version after template update: %v", err)
	}
	versions = map[string]int64{}
	for rows.Next() {
		var userID string
		var version int64
		if err := rows.Scan(&userID, &version); err != nil {
			rows.Close()
			t.Fatalf("scan sync_version after template update: %v", err)
		}
		versions[userID] = version
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sync_version after template update: %v", err)
	}
	rows.Close()

	wantAfterUpdate := map[string]int64{
		"template-sync-user": 2,
		"test-user-concrete": 2,
	}
	if len(versions) != len(wantAfterUpdate) {
		t.Fatalf("sync_version after template update = %v, want %v", versions, wantAfterUpdate)
	}
	for userID, want := range wantAfterUpdate {
		if got := versions[userID]; got != want {
			t.Fatalf("sync_version[%q] after template update = %d, want %d; all versions = %v", userID, got, want, versions)
		}
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
