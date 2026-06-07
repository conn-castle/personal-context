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

	// Create a test user required by the user_id FK on records.
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

	// Create a test user required by the user_id FK on records.
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

func mustCreateRecord(t *testing.T, repo repository.Repository, input repository.CreateRecordInput) repository.Record {
	t.Helper()

	if input.ProjectID == "" {
		input.ProjectID = "test/project"
	}
	if input.SourceDeviceID == "" {
		input.SourceDeviceID = "test-device"
	}
	if _, err := repo.CreateProject(context.Background(), repository.CreateRegistryInput{ID: input.ProjectID}); err != nil && !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("CreateProject(%q) error = %v", input.ProjectID, err)
	}
	if _, err := repo.CreateDevice(context.Background(), repository.CreateRegistryInput{ID: input.SourceDeviceID}); err != nil && !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("CreateDevice(%q) error = %v", input.SourceDeviceID, err)
	}
	record, err := repo.CreateRecord(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateRecord() error = %v", err)
	}
	return record
}

func testHTML(value string) *string {
	return &value
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

func TestUpsertChatSessionWithItemsRollsBackExistingReplacementConflict(t *testing.T) {
	ctx := context.Background()
	repo, _ := newConcreteRepo(t)
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	if _, err := repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: "replacement-rollback-device"}); err != nil {
		t.Fatalf("CreateDevice() error = %v", err)
	}

	originalText := "original rollback survivor"
	createdSession, created, err := repo.UpsertChatSessionWithItems(ctx, repository.UpsertChatSessionInput{
		CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:              "20260604-11111111",
			Source:          "codex",
			SourceSessionID: "replacement-rollback",
			SourceDeviceID:  "replacement-rollback-device",
			StartedAt:       now,
			LastActivityAt:  now,
			CreatedAt:       &now,
			UpdatedAt:       &now,
		},
		ClearDeleted: true,
	}, []repository.CreateChatItemInput{{
		Ordinal:   0,
		Role:      "user",
		ItemType:  "message",
		Text:      &originalText,
		CreatedAt: &now,
	}})
	if err != nil {
		t.Fatalf("seed UpsertChatSessionWithItems() error = %v", err)
	}
	if !created {
		t.Fatal("expected seed upsert to create the chat session")
	}

	failedUpdatedAt := now.Add(time.Hour)
	replacementText := "replacement item must roll back"
	_, _, err = repo.UpsertChatSessionWithItems(ctx, repository.UpsertChatSessionInput{
		CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:              createdSession.ID,
			Source:          "codex",
			SourceSessionID: "replacement-rollback",
			SourceDeviceID:  "replacement-rollback-device",
			StartedAt:       now,
			LastActivityAt:  failedUpdatedAt,
			UpdatedAt:       &failedUpdatedAt,
		},
		ClearDeleted: true,
	}, []repository.CreateChatItemInput{{
		Ordinal:   0,
		Role:      "assistant",
		ItemType:  "message",
		Text:      &replacementText,
		CreatedAt: &failedUpdatedAt,
	}, {
		Ordinal:   0,
		Role:      "tool",
		ItemType:  "tool_output",
		Text:      &replacementText,
		CreatedAt: &failedUpdatedAt,
	}})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected replacement item conflict, got %v", err)
	}

	rolledBack, err := repo.GetChatSessionByID(ctx, createdSession.ID)
	if err != nil {
		t.Fatalf("GetChatSessionByID(after rollback) error = %v", err)
	}
	if !rolledBack.UpdatedAt.Equal(createdSession.UpdatedAt) {
		t.Fatalf("session updated_at changed after rollback: got %v want %v", rolledBack.UpdatedAt, createdSession.UpdatedAt)
	}

	items, err := repo.ListChatItems(ctx, createdSession.ID)
	if err != nil {
		t.Fatalf("ListChatItems(after rollback) error = %v", err)
	}
	if len(items) != 1 || items[0].SearchText != originalText {
		t.Fatalf("replacement conflict did not roll back chat items: %+v", items)
	}

	results, err := repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "survivor"})
	if err != nil {
		t.Fatalf("SearchChatItems(survivor) error = %v", err)
	}
	if len(results) != 1 || results[0].Session.ID != createdSession.ID || results[0].Item.SearchText != originalText {
		t.Fatalf("FTS changed despite replacement rollback: %+v", results)
	}
}

func TestRecordValidationConflictAndNotFoundBranches(t *testing.T) {
	repo, _ := newConcreteRepo(t)
	ctx := context.Background()

	_, err := repo.GetRecordByID(ctx, "")
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty id, got %v", err)
	}

	_, err = repo.CreateRecord(ctx, repository.CreateRecordInput{})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid create input, got %v", err)
	}

	createdAt := time.Date(2026, time.March, 5, 10, 11, 12, 123000000, time.FixedZone("UTC+2", 2*60*60))
	updatedAt := createdAt.Add(2 * time.Minute)
	deletedAt := updatedAt.Add(1 * time.Minute)
	record := mustCreateRecord(t, repo, repository.CreateRecordInput{
		ID:          "20260305-a1b2c3d4",
		Date:        "2026-03-05",
		HTMLContent: testHTML("<h1>x</h1>"),
		CreatedAt:   &createdAt,
		UpdatedAt:   &updatedAt,
		DeletedAt:   &deletedAt,
	})
	if record.DayOrder != "n" {
		t.Fatalf("expected default day_order n, got %q", record.DayOrder)
	}
	if record.CreatedAt.Location() != time.UTC || record.UpdatedAt.Location() != time.UTC {
		t.Fatalf("expected timestamps normalized to UTC, got created=%v updated=%v", record.CreatedAt, record.UpdatedAt)
	}
	if record.DeletedAt == nil {
		t.Fatal("expected deleted_at to be persisted")
	}

	_, err = repo.CreateRecord(ctx, repository.CreateRecordInput{
		ID:             "20260305-a1b2c3d4",
		Date:           "2026-03-05",
		DayOrder:       "a",
		HTMLContent:    testHTML("<h1>dup</h1>"),
		ProjectID:      "test/project",
		SourceDeviceID: "test-device",
	})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate record id, got %v", err)
	}

	_, err = repo.UpdateRecord(ctx, repository.UpdateRecordInput{})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid update input, got %v", err)
	}

	_, err = repo.UpdateRecord(ctx, repository.UpdateRecordInput{
		ID:             "20260305-ffffeeee",
		Date:           "2026-03-05",
		DayOrder:       "a",
		HTMLContent:    testHTML("<h1>missing</h1>"),
		ProjectID:      "test/project",
		SourceDeviceID: "test-device",
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing record update, got %v", err)
	}

	if err := repo.SoftDeleteRecord(ctx, "20260305-ffffeeee"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing soft delete, got %v", err)
	}
	if err := repo.RestoreRecord(ctx, "20260305-ffffeeee"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing restore, got %v", err)
	}
	if err := repo.DeleteRecord(ctx, "20260305-ffffeeee"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing delete, got %v", err)
	}
}

func TestAssetValidationAndNotFoundBranches(t *testing.T) {
	repo, _ := newConcreteRepo(t)
	ctx := context.Background()
	record := mustCreateRecord(t, repo, repository.CreateRecordInput{
		ID:          "20260305-11112222",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: testHTML("<h1>assets</h1>"),
	})

	_, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid figure create, got %v", err)
	}
	_, err = repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
		RecordID: "20260305-missing00",
		Filename: "x.png",
		S3Key:    "figures/20260305-missing00/x.png",
	})
	if !errors.Is(err, repository.ErrForeignKeyViolation) {
		t.Fatalf("expected ErrForeignKeyViolation for orphan figure create, got %v", err)
	}

	figure, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
		RecordID: record.ID,
		Filename: "x.png",
		S3Key:    "figures/20260305-11112222/x.png",
	})
	if err != nil {
		t.Fatalf("CreateRecordFigure() error = %v", err)
	}
	if _, err := repo.UpdateRecordFigure(ctx, repository.UpdateRecordFigureInput{ID: figure.ID}); err != nil {
		t.Fatalf("UpdateRecordFigure() with minimal input should succeed, got %v", err)
	}
	if _, err := repo.UpdateRecordFigure(ctx, repository.UpdateRecordFigureInput{ID: 999999, Filename: "new.png"}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing figure update, got %v", err)
	}
	if _, err := repo.ListRecordFiguresByRecordID(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty recordID list figures, got %v", err)
	}
	if err := repo.DeleteRecordFigure(ctx, 999999); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing figure delete, got %v", err)
	}

	_, err = repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid data file create, got %v", err)
	}
	_, err = repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
		RecordID: "20260305-missing00",
		Filename: "missing.csv",
		S3Key:    "data/20260305-missing00/missing.csv",
		Size:     1,
		Hash:     strings.Repeat("a", 64),
	})
	if !errors.Is(err, repository.ErrForeignKeyViolation) {
		t.Fatalf("expected ErrForeignKeyViolation for orphan data-file create, got %v", err)
	}

	_, err = repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
		RecordID: record.ID,
		Filename: "x.csv",
		S3Key:    "data/20260305-11112222/x.csv",
		Size:     -1,
		Hash:     strings.Repeat("a", 64),
	})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for negative file size, got %v", err)
	}
	dataFile, err := repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
		RecordID: record.ID,
		Filename: "x.csv",
		S3Key:    "data/20260305-11112222/x.csv",
		Size:     1,
		Hash:     strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("CreateRecordDataFile() error = %v", err)
	}
	if _, err := repo.UpdateRecordDataFile(ctx, repository.UpdateRecordDataFileInput{ID: dataFile.ID}); err != nil {
		t.Fatalf("UpdateRecordDataFile() with minimal input should succeed, got %v", err)
	}
	if _, err := repo.UpdateRecordDataFile(ctx, repository.UpdateRecordDataFileInput{ID: 999999, Filename: "new.csv"}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing data-file update, got %v", err)
	}
	negativeSize := int64(-1)
	if _, err := repo.UpdateRecordDataFile(ctx, repository.UpdateRecordDataFileInput{
		ID:   dataFile.ID,
		Size: &negativeSize,
	}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for negative data-file update size, got %v", err)
	}
	if _, err := repo.ListRecordDataFilesByRecordID(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty recordID list data files, got %v", err)
	}
	if err := repo.DeleteRecordDataFile(ctx, 999999); !errors.Is(err, repository.ErrNotFound) {
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

	recordA := mustCreateRecord(t, repoA, repository.CreateRecordInput{
		ID:          "20260305-abcdd001",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: testHTML("<h1>user-a</h1>"),
	})
	recordB := mustCreateRecord(t, repoB, repository.CreateRecordInput{
		ID:          "20260305-abcdd002",
		Date:        "2026-03-05",
		DayOrder:    "b",
		HTMLContent: testHTML("<h1>user-b</h1>"),
	})

	figureA, err := repoA.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
		RecordID: recordA.ID,
		Filename: "a.png",
		S3Key:    "figures/20260305-abcdd001/a.png",
	})
	if err != nil {
		t.Fatalf("CreateRecordFigure() for user A error = %v", err)
	}
	if _, err := repoB.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
		RecordID: recordA.ID,
		Filename: "cross.png",
		S3Key:    "figures/20260305-abcdd001/cross.png",
	}); !errors.Is(err, repository.ErrForeignKeyViolation) {
		t.Fatalf("expected ErrForeignKeyViolation for cross-user figure create, got %v", err)
	}
	if _, err := repoB.GetRecordFigureByID(ctx, figureA.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user figure get, got %v", err)
	}
	if _, err := repoB.UpdateRecordFigure(ctx, repository.UpdateRecordFigureInput{
		ID:       figureA.ID,
		Filename: "steal.png",
	}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user figure update, got %v", err)
	}
	figuresForAFromB, err := repoB.ListRecordFiguresByRecordID(ctx, recordA.ID)
	if err != nil {
		t.Fatalf("ListRecordFiguresByRecordID() for cross-user record error = %v", err)
	}
	if len(figuresForAFromB) != 0 {
		t.Fatalf("expected no cross-user figures, got %d", len(figuresForAFromB))
	}
	if err := repoB.DeleteRecordFigure(ctx, figureA.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user figure delete, got %v", err)
	}
	if _, err := repoB.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
		RecordID: recordB.ID,
		Filename: "b.png",
		S3Key:    "figures/20260305-abcdd002/b.png",
	}); err != nil {
		t.Fatalf("expected same-user figure create to succeed, got %v", err)
	}

	dataFileA, err := repoA.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
		RecordID: recordA.ID,
		Filename: "a.csv",
		S3Key:    "data/20260305-abcdd001/a.csv",
		Size:     16,
		Hash:     strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("CreateRecordDataFile() for user A error = %v", err)
	}
	if _, err := repoB.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
		RecordID: recordA.ID,
		Filename: "cross.csv",
		S3Key:    "data/20260305-abcdd001/cross.csv",
		Size:     1,
		Hash:     strings.Repeat("b", 64),
	}); !errors.Is(err, repository.ErrForeignKeyViolation) {
		t.Fatalf("expected ErrForeignKeyViolation for cross-user data-file create, got %v", err)
	}
	if _, err := repoB.GetRecordDataFileByID(ctx, dataFileA.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user data-file get, got %v", err)
	}
	if _, err := repoB.UpdateRecordDataFile(ctx, repository.UpdateRecordDataFileInput{
		ID:       dataFileA.ID,
		Filename: "steal.csv",
	}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user data-file update, got %v", err)
	}
	filesForAFromB, err := repoB.ListRecordDataFilesByRecordID(ctx, recordA.ID)
	if err != nil {
		t.Fatalf("ListRecordDataFilesByRecordID() for cross-user record error = %v", err)
	}
	if len(filesForAFromB) != 0 {
		t.Fatalf("expected no cross-user data files, got %d", len(filesForAFromB))
	}
	if err := repoB.DeleteRecordDataFile(ctx, dataFileA.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user data-file delete, got %v", err)
	}
	if _, err := repoB.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
		RecordID: recordB.ID,
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
		CREATE TABLE records (
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

func TestApplySchemaRejectsChatSchemaMissingParentColumn(t *testing.T) {
	ctx := context.Background()
	schemaCounter++
	schemaName := fmt.Sprintf("chat_missing_parent_%d_%d", time.Now().UnixNano(), schemaCounter)

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
		CREATE TABLE chat_session (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			source TEXT NOT NULL,
			source_session_id TEXT NOT NULL
		);
	`); err != nil {
		t.Fatalf("create old chat_session schema fixture error = %v", err)
	}

	err = ApplySchema(ctx, pool)
	if err == nil {
		t.Fatal("expected ApplySchema to reject chat_session without parent_source_session_id")
	}
	if !strings.Contains(err.Error(), "chat_session.parent_source_session_id") {
		t.Fatalf("expected explicit missing column error, got %v", err)
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

func TestSchemaRejectsNegativeRecordDataFileSize(t *testing.T) {
	repo, pool := newConcreteRepo(t)
	ctx := context.Background()

	record := mustCreateRecord(t, repo, repository.CreateRecordInput{
		ID:          "20260305-dddd0001",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: testHTML("<h1>schema</h1>"),
	})

	_, err := pool.Exec(
		ctx,
		`INSERT INTO record_data_files (record_id, filename, s3_key, size, hash) VALUES ($1, $2, $3, $4, $5)`,
		record.ID,
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

	record := mustCreateRecord(t, repo, repository.CreateRecordInput{
		ID:          "20260305-dddd0002",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: testHTML("<h1>sync</h1>"),
	})

	figure, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
		RecordID: record.ID,
		Filename: "plot.png",
		S3Key:    "figures/20260305-dddd0002/plot.png",
	})
	if err != nil {
		t.Fatalf("CreateRecordFigure() error = %v", err)
	}

	dataFile, err := repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
		RecordID: record.ID,
		Filename: "data.csv",
		S3Key:    "data/20260305-dddd0002/data.csv",
		Size:     4,
		Hash:     strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatalf("CreateRecordDataFile() error = %v", err)
	}

	beforeNoOp, err := repo.GetSyncVersion(ctx)
	if err != nil {
		t.Fatalf("GetSyncVersion() before no-op updates error = %v", err)
	}

	if _, err := repo.UpdateRecordFigure(ctx, repository.UpdateRecordFigureInput{ID: figure.ID}); err != nil {
		t.Fatalf("UpdateRecordFigure() no-op error = %v", err)
	}
	if _, err := repo.UpdateRecordDataFile(ctx, repository.UpdateRecordDataFileInput{ID: dataFile.ID}); err != nil {
		t.Fatalf("UpdateRecordDataFile() no-op error = %v", err)
	}

	afterNoOp, err := repo.GetSyncVersion(ctx)
	if err != nil {
		t.Fatalf("GetSyncVersion() after no-op updates error = %v", err)
	}
	if afterNoOp.Version != beforeNoOp.Version {
		t.Fatalf("expected no-op updates not to bump sync version, before=%d after=%d", beforeNoOp.Version, afterNoOp.Version)
	}

	if _, err := repo.UpdateRecordFigure(ctx, repository.UpdateRecordFigureInput{
		ID:       figure.ID,
		Filename: "plot-v2.png",
	}); err != nil {
		t.Fatalf("UpdateRecordFigure() meaningful update error = %v", err)
	}

	newSize := int64(5)
	if _, err := repo.UpdateRecordDataFile(ctx, repository.UpdateRecordDataFileInput{
		ID:   dataFile.ID,
		Size: &newSize,
	}); err != nil {
		t.Fatalf("UpdateRecordDataFile() meaningful update error = %v", err)
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

	// Create a record so we have a valid ID for methods that need it.
	record := mustCreateRecord(t, repo, repository.CreateRecordInput{
		ID:          "20260305-aaaa0001",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: testHTML("<h1>pool-close</h1>"),
	})

	// Close the pool to force all subsequent operations to fail.
	pool.Close()

	if _, err := repo.ListRecords(ctx, repository.ListRecordsFilter{}); err == nil {
		t.Fatal("expected error for ListRecords on closed pool")
	}
	if err := repo.SoftDeleteRecord(ctx, record.ID); err == nil {
		t.Fatal("expected error for SoftDeleteRecord on closed pool")
	}
	if err := repo.RestoreRecord(ctx, record.ID); err == nil {
		t.Fatal("expected error for RestoreRecord on closed pool")
	}
	if err := repo.DeleteRecord(ctx, record.ID); err == nil {
		t.Fatal("expected error for DeleteRecord on closed pool")
	}
	if _, err := repo.ListRecordFiguresByRecordID(ctx, record.ID); err == nil {
		t.Fatal("expected error for ListRecordFiguresByRecordID on closed pool")
	}
	if err := repo.DeleteRecordFigure(ctx, 1); err == nil {
		t.Fatal("expected error for DeleteRecordFigure on closed pool")
	}
	if _, err := repo.ListRecordDataFilesByRecordID(ctx, record.ID); err == nil {
		t.Fatal("expected error for ListRecordDataFilesByRecordID on closed pool")
	}
	if err := repo.DeleteRecordDataFile(ctx, 1); err == nil {
		t.Fatal("expected error for DeleteRecordDataFile on closed pool")
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
	if _, err := repo.ListProjects(ctx, true); err == nil {
		t.Fatal("expected error for ListProjects on closed pool")
	}
	if _, err := repo.ListDevices(ctx, true); err == nil {
		t.Fatal("expected error for ListDevices on closed pool")
	}
	if _, err := repo.GetRecordByID(ctx, record.ID); err == nil {
		t.Fatal("expected error for GetRecordByID on closed pool")
	}
	if _, err := repo.CreateRecord(ctx, repository.CreateRecordInput{
		ID: "20260305-aaaa0002", Date: "2026-03-05", HTMLContent: testHTML("<h1>x</h1>"), ProjectID: "test/project", SourceDeviceID: "test-device",
	}); err == nil {
		t.Fatal("expected error for CreateRecord on closed pool")
	}
	if _, err := repo.UpdateRecord(ctx, repository.UpdateRecordInput{
		ID: record.ID, Date: "2026-03-05", DayOrder: "a", HTMLContent: testHTML("<h1>x</h1>"), ProjectID: "test/project", SourceDeviceID: "test-device",
	}); err == nil {
		t.Fatal("expected error for UpdateRecord on closed pool")
	}
	if _, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
		RecordID: record.ID, Filename: "x.png", S3Key: "figures/x.png",
	}); err == nil {
		t.Fatal("expected error for CreateRecordFigure on closed pool")
	}
	if _, err := repo.GetRecordFigureByID(ctx, 1); err == nil {
		t.Fatal("expected error for GetRecordFigureByID on closed pool")
	}
	if _, err := repo.UpdateRecordFigure(ctx, repository.UpdateRecordFigureInput{
		ID: 1, Filename: "y.png",
	}); err == nil {
		t.Fatal("expected error for UpdateRecordFigure on closed pool")
	}
	if _, err := repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
		RecordID: record.ID, Filename: "x.csv", S3Key: "data/x.csv", Size: 1, Hash: strings.Repeat("a", 64),
	}); err == nil {
		t.Fatal("expected error for CreateRecordDataFile on closed pool")
	}
	if _, err := repo.GetRecordDataFileByID(ctx, 1); err == nil {
		t.Fatal("expected error for GetRecordDataFileByID on closed pool")
	}
	if _, err := repo.UpdateRecordDataFile(ctx, repository.UpdateRecordDataFileInput{
		ID: 1, Filename: "y.csv",
	}); err == nil {
		t.Fatal("expected error for UpdateRecordDataFile on closed pool")
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
