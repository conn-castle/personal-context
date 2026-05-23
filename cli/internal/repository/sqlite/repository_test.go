package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/personal-context/cli/internal/repository"
	"github.com/conn-castle/personal-context/cli/internal/repository/repositorytest"
	sqlitebootstrap "github.com/conn-castle/personal-context/cli/internal/sqlite"
)

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

	if err := connection.ApplySchema(context.Background()); err != nil {
		t.Fatalf("ApplySchema() error = %v", err)
	}

	repo, err := New(connection.DB())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return repo, connection.DB()
}

func mustCreateRecord(t *testing.T, repo repository.Repository, input repository.CreateRecordInput) repository.Record {
	t.Helper()

	ctx := context.Background()
	if input.ProjectID == "" {
		input.ProjectID = "sqlite/default-project"
	}
	if input.SourceDeviceID == "" {
		input.SourceDeviceID = "sqlite-device"
	}
	_, err := repo.GetProjectByID(ctx, input.ProjectID)
	if errors.Is(err, repository.ErrNotFound) {
		_, err = repo.CreateProject(ctx, repository.CreateRegistryInput{ID: input.ProjectID})
	}
	if err != nil {
		t.Fatalf("ensure project registry row failed: %v", err)
	}
	_, err = repo.GetDeviceByID(ctx, input.SourceDeviceID)
	if errors.Is(err, repository.ErrNotFound) {
		_, err = repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: input.SourceDeviceID})
	}
	if err != nil {
		t.Fatalf("ensure device registry row failed: %v", err)
	}

	record, err := repo.CreateRecord(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateRecord() error = %v", err)
	}
	return record
}

func strPtr(value string) *string {
	return &value
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
		HTMLContent: strPtr("<h1>x</h1>"),
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
		HTMLContent:    strPtr("<h1>dup</h1>"),
		ProjectID:      record.ProjectID,
		SourceDeviceID: record.SourceDeviceID,
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
		HTMLContent:    strPtr("<h1>missing</h1>"),
		ProjectID:      record.ProjectID,
		SourceDeviceID: record.SourceDeviceID,
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
		HTMLContent: strPtr("<h1>assets</h1>"),
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
		t.Fatalf("expected ErrForeignKeyViolation for orphan figure, got %v", err)
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

func TestSchemaRejectsNegativeRecordDataFileSize(t *testing.T) {
	repo, db := newConcreteRepo(t)
	ctx := context.Background()

	record := mustCreateRecord(t, repo, repository.CreateRecordInput{
		ID:          "20260305-dddd0001",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: strPtr("<h1>schema</h1>"),
	})

	_, err := db.ExecContext(
		ctx,
		`INSERT INTO record_data_files (record_id, filename, s3_key, size, hash) VALUES (?, ?, ?, ?, ?);`,
		record.ID,
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

	record := mustCreateRecord(t, repo, repository.CreateRecordInput{
		ID:          "20260305-dddd0002",
		Date:        "2026-03-05",
		DayOrder:    "a",
		HTMLContent: strPtr("<h1>sync</h1>"),
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

	if _, err := db.ExecContext(
		ctx,
		`UPDATE record_figures SET filename = filename, s3_key = s3_key, alt_text = alt_text WHERE id = ?;`,
		figure.ID,
	); err != nil {
		t.Fatalf("no-op figure update failed: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`UPDATE record_data_files SET filename = filename, s3_key = s3_key, size = size, hash = hash, description = description WHERE id = ?;`,
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
		`UPDATE record_figures SET filename = ? WHERE id = ?;`,
		"plot-v2.png",
		figure.ID,
	); err != nil {
		t.Fatalf("meaningful figure update failed: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`UPDATE record_data_files SET size = ? WHERE id = ?;`,
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

	if _, err := (recordRow{CreatedAt: "bad", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}).toModel(); err == nil {
		t.Fatal("expected recordRow.toModel() to fail on bad created_at")
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
	if _, err := (projectPathRow{CreatedAt: "bad", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}).toModel(); err == nil {
		t.Fatal("expected projectPathRow.toModel() to fail on bad created_at")
	}
	if _, err := (chatSessionRow{StartedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected chatSessionRow.toModel() to fail on bad started_at")
	}
	if _, err := (chatSessionRow{StartedAt: time.Now().UTC().Format(time.RFC3339Nano), LastActivityAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected chatSessionRow.toModel() to fail on bad last_activity_at")
	}
	if _, err := (chatSessionRow{StartedAt: time.Now().UTC().Format(time.RFC3339Nano), LastActivityAt: time.Now().UTC().Format(time.RFC3339Nano), CreatedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected chatSessionRow.toModel() to fail on bad created_at")
	}
	if _, err := (chatSessionRow{StartedAt: time.Now().UTC().Format(time.RFC3339Nano), LastActivityAt: time.Now().UTC().Format(time.RFC3339Nano), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected chatSessionRow.toModel() to fail on bad updated_at")
	}
	if _, err := (chatItemRow{CreatedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected chatItemRow.toModel() to fail on bad created_at")
	}

	row := db.QueryRow(`SELECT 1;`)
	if _, err := scanRecord(row); err == nil {
		t.Fatal("expected scanRecord() to fail for wrong column count")
	}
	row = db.QueryRow(`SELECT 1;`)
	if _, err := scanProjectPath(row); err == nil {
		t.Fatal("expected scanProjectPath() to fail for wrong column count")
	}
	row = db.QueryRow(`SELECT 1;`)
	if _, err := scanChatSession(row); err == nil {
		t.Fatal("expected scanChatSession() to fail for wrong column count")
	}
	row = db.QueryRow(`SELECT 1;`)
	if _, err := scanChatItem(row); err == nil {
		t.Fatal("expected scanChatItem() to fail for wrong column count")
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
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatal("expected rows.Next() to be true")
	}
	if _, err := scanRecordRows(rows); err == nil {
		t.Fatal("expected scanRecordRows() to fail for wrong column count")
	}

	rows, err = db.Query(`SELECT 1;`)
	if err != nil {
		t.Fatalf("query rows failed: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatal("expected rows.Next() to be true")
	}
	if _, err := scanProjectPathRows(rows); err == nil {
		t.Fatal("expected scanProjectPathRows() to fail for wrong column count")
	}

	rows, err = db.Query(`SELECT 1;`)
	if err != nil {
		t.Fatalf("query rows failed: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatal("expected rows.Next() to be true")
	}
	if _, err := scanChatSessionRows(rows); err == nil {
		t.Fatal("expected scanChatSessionRows() to fail for wrong column count")
	}

	rows, err = db.Query(`SELECT 1;`)
	if err != nil {
		t.Fatalf("query rows failed: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatal("expected rows.Next() to be true")
	}
	if _, err := scanChatItemRows(rows); err == nil {
		t.Fatal("expected scanChatItemRows() to fail for wrong column count")
	}

	rows, err = db.Query(`SELECT 1;`)
	if err != nil {
		t.Fatalf("query rows failed: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatal("expected rows.Next() to be true")
	}
	if _, _, err := scanRecordSearchRows(rows); err == nil {
		t.Fatal("expected scanRecordSearchRows() to fail for wrong column count")
	}

	rows, err = db.Query(`SELECT 1 WHERE 0;`)
	if err != nil {
		t.Fatalf("query rows failed: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if results, err := scanChatSearchRows(rows); err != nil || len(results) != 0 {
		t.Fatalf("expected empty chat search rows to succeed, got results=%+v err=%v", results, err)
	}

	rows, err = db.Query(`SELECT 1;`)
	if err != nil {
		t.Fatalf("query rows failed: %v", err)
	}
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	if !errors.Is(mapSQLiteError(errors.New("UNIQUE constraint failed: records.id")), repository.ErrConflict) {
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
	if _, err := (recordRow{CreatedAt: "2026-03-05T00:00:00Z", UpdatedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected recordRow.toModel() to fail on bad updated_at")
	}
	if _, err := (recordRow{
		CreatedAt: "2026-03-05T00:00:00Z",
		UpdatedAt: "2026-03-05T00:00:00Z",
		DeletedAt: sql.NullString{Valid: true, String: "bad"},
	}).toModel(); err == nil {
		t.Fatal("expected recordRow.toModel() to fail on bad deleted_at")
	}
	if _, err := (templateRow{CreatedAt: "2026-03-05T00:00:00Z", UpdatedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected templateRow.toModel() to fail on bad updated_at")
	}
	if _, err := (registryRow{CreatedAt: "bad", UpdatedAt: "2026-03-05T00:00:00Z"}).toProject(); err == nil {
		t.Fatal("expected project row to fail on bad created_at")
	}
	if _, err := (registryRow{CreatedAt: "2026-03-05T00:00:00Z", UpdatedAt: "bad"}).toDevice(); err == nil {
		t.Fatal("expected device row to fail on bad updated_at")
	}
	if _, err := (registryRow{
		CreatedAt:  "2026-03-05T00:00:00Z",
		UpdatedAt:  "2026-03-05T00:00:00Z",
		ArchivedAt: sql.NullString{Valid: true, String: "bad"},
	}).toProject(); err == nil {
		t.Fatal("expected project row to fail on bad archived_at")
	}
}

func TestChatRowModelTimestampFailureBranches(t *testing.T) {
	if _, err := (projectPathRow{CreatedAt: "bad", UpdatedAt: "2026-05-14T12:00:00.000Z"}).toModel(); err == nil {
		t.Fatal("expected project path created_at parse error")
	}
	if _, err := (projectPathRow{CreatedAt: "2026-05-14T12:00:00.000Z", UpdatedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected project path updated_at parse error")
	}

	validTime := "2026-05-14T12:00:00.000Z"
	baseSession := chatSessionRow{
		ID:             "20260514-aabbccdd",
		StartedAt:      validTime,
		LastActivityAt: validTime,
		CreatedAt:      validTime,
		UpdatedAt:      validTime,
	}
	sessionCases := []chatSessionRow{
		func() chatSessionRow { row := baseSession; row.StartedAt = "bad"; return row }(),
		func() chatSessionRow { row := baseSession; row.LastActivityAt = "bad"; return row }(),
		func() chatSessionRow { row := baseSession; row.CreatedAt = "bad"; return row }(),
		func() chatSessionRow { row := baseSession; row.UpdatedAt = "bad"; return row }(),
		func() chatSessionRow {
			row := baseSession
			row.DeletedAt = sql.NullString{String: "bad", Valid: true}
			return row
		}(),
	}
	for _, row := range sessionCases {
		if _, err := row.toModel(); err == nil {
			t.Fatalf("expected chat session timestamp error for %+v", row)
		}
	}

	if _, err := (chatItemRow{CreatedAt: "bad"}).toModel(); err == nil {
		t.Fatal("expected chat item created_at parse error")
	}
}

func TestRegistryMethodsSQLiteBranches(t *testing.T) {
	repo, _ := newConcreteRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	later := time.Now().UTC().Add(time.Hour)
	archived := later.Add(time.Hour)

	if _, err := repo.CreateProject(ctx, repository.CreateRegistryInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("CreateProject(empty) error = %v", err)
	}
	if _, err := repo.GetProjectByID(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("GetProjectByID(empty) error = %v", err)
	}
	project, err := repo.CreateProject(ctx, repository.CreateRegistryInput{ID: "registry/project", CreatedAt: &now, UpdatedAt: &now})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if project.ID != "registry/project" {
		t.Fatalf("project ID = %q", project.ID)
	}
	if _, err := repo.CreateProject(ctx, repository.CreateRegistryInput{ID: "registry/project"}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("CreateProject(duplicate) error = %v", err)
	}
	if _, err := repo.ArchiveProject(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("ArchiveProject(empty) error = %v", err)
	}
	if _, err := repo.ArchiveProject(ctx, "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("ArchiveProject(missing) error = %v", err)
	}
	if _, err := repo.ArchiveProject(ctx, "registry/project"); err != nil {
		t.Fatalf("ArchiveProject() error = %v", err)
	}
	activeProjects, err := repo.ListProjects(ctx, false)
	if err != nil {
		t.Fatalf("ListProjects(active) error = %v", err)
	}
	if len(activeProjects) != 0 {
		t.Fatalf("expected archived project hidden, got %#v", activeProjects)
	}
	allProjects, err := repo.ListProjects(ctx, true)
	if err != nil {
		t.Fatalf("ListProjects(all) error = %v", err)
	}
	if len(allProjects) != 1 || allProjects[0].ArchivedAt == nil {
		t.Fatalf("expected archived project in all list, got %#v", allProjects)
	}
	if _, err := repo.RestoreProject(ctx, "registry/project"); err != nil {
		t.Fatalf("RestoreProject() error = %v", err)
	}
	changed, err := repo.UpsertProjectForImport(ctx, repository.Project{ID: "registry/project", CreatedAt: now, UpdatedAt: now})
	if err != nil || changed {
		t.Fatalf("older UpsertProjectForImport changed=%v error=%v", changed, err)
	}
	changed, err = repo.UpsertProjectForImport(ctx, repository.Project{ID: "registry/project", CreatedAt: now, UpdatedAt: later, ArchivedAt: &archived})
	if err != nil || !changed {
		t.Fatalf("newer UpsertProjectForImport changed=%v error=%v", changed, err)
	}
	if changed, err := repo.UpsertProjectForImport(ctx, repository.Project{}); !errors.Is(err, repository.ErrInvalidArgument) || changed {
		t.Fatalf("invalid UpsertProjectForImport changed=%v error=%v", changed, err)
	}
	changed, err = repo.UpsertProjectForImport(ctx, repository.Project{ID: "new-project", CreatedAt: now, UpdatedAt: later})
	if err != nil || !changed {
		t.Fatalf("new UpsertProjectForImport changed=%v error=%v", changed, err)
	}

	if _, err := repo.CreateDevice(ctx, repository.CreateRegistryInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("CreateDevice(empty) error = %v", err)
	}
	if _, err := repo.GetDeviceByID(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("GetDeviceByID(empty) error = %v", err)
	}
	device, err := repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: "registry-device", CreatedAt: &now, UpdatedAt: &now})
	if err != nil {
		t.Fatalf("CreateDevice() error = %v", err)
	}
	if device.ID != "registry-device" {
		t.Fatalf("device ID = %q", device.ID)
	}
	if _, err := repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: "registry-device"}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("CreateDevice(duplicate) error = %v", err)
	}
	if _, err := repo.ArchiveDevice(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("ArchiveDevice(empty) error = %v", err)
	}
	if _, err := repo.ArchiveDevice(ctx, "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("ArchiveDevice(missing) error = %v", err)
	}
	if _, err := repo.ArchiveDevice(ctx, "registry-device"); err != nil {
		t.Fatalf("ArchiveDevice() error = %v", err)
	}
	activeDevices, err := repo.ListDevices(ctx, false)
	if err != nil {
		t.Fatalf("ListDevices(active) error = %v", err)
	}
	if len(activeDevices) != 0 {
		t.Fatalf("expected archived device hidden, got %#v", activeDevices)
	}
	allDevices, err := repo.ListDevices(ctx, true)
	if err != nil {
		t.Fatalf("ListDevices(all) error = %v", err)
	}
	if len(allDevices) != 1 || allDevices[0].ArchivedAt == nil {
		t.Fatalf("expected archived device in all list, got %#v", allDevices)
	}
	if _, err := repo.RestoreDevice(ctx, "registry-device"); err != nil {
		t.Fatalf("RestoreDevice() error = %v", err)
	}
	if changed, err := repo.UpsertDeviceForImport(ctx, repository.Device{}); !errors.Is(err, repository.ErrInvalidArgument) || changed {
		t.Fatalf("invalid UpsertDeviceForImport changed=%v error=%v", changed, err)
	}
	changed, err = repo.UpsertDeviceForImport(ctx, repository.Device{ID: "registry-device", CreatedAt: now, UpdatedAt: now})
	if err != nil || changed {
		t.Fatalf("older UpsertDeviceForImport changed=%v error=%v", changed, err)
	}
	changed, err = repo.UpsertDeviceForImport(ctx, repository.Device{ID: "registry-device", CreatedAt: now, UpdatedAt: later, ArchivedAt: &archived})
	if err != nil || !changed {
		t.Fatalf("newer UpsertDeviceForImport changed=%v error=%v", changed, err)
	}
	changed, err = repo.UpsertDeviceForImport(ctx, repository.Device{ID: "new-device", CreatedAt: now, UpdatedAt: later})
	if err != nil || !changed {
		t.Fatalf("new UpsertDeviceForImport changed=%v error=%v", changed, err)
	}
}

func TestSQLiteCountsAndPurgeBranches(t *testing.T) {
	repo, db := newConcreteRepo(t)
	ctx := context.Background()
	active := mustCreateRecord(t, repo, repository.CreateRecordInput{
		ID:          "20260305-aaabbb01",
		Date:        "2026-03-05",
		HTMLContent: strPtr("<h1>active</h1>"),
	})
	trashed := mustCreateRecord(t, repo, repository.CreateRecordInput{
		ID:          "20260305-aaabbb02",
		Date:        "2026-03-05",
		HTMLContent: strPtr("<h1>trashed</h1>"),
	})
	if err := repo.SoftDeleteRecord(ctx, trashed.ID); err != nil {
		t.Fatalf("SoftDeleteRecord() error = %v", err)
	}
	activeCount, err := repo.CountActiveRecords(ctx)
	if err != nil {
		t.Fatalf("CountActiveRecords() error = %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active count = %d", activeCount)
	}
	trashedCount, err := repo.CountTrashedRecords(ctx)
	if err != nil {
		t.Fatalf("CountTrashedRecords() error = %v", err)
	}
	if trashedCount != 1 {
		t.Fatalf("trashed count = %d", trashedCount)
	}
	blankQuery := " "
	if _, err := repo.CountRecords(ctx, repository.ListRecordsFilter{Query: &blankQuery}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid count query error, got %v", err)
	}
	deletedAt := time.Now().UTC().Add(-48 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	if _, err := db.ExecContext(ctx, `UPDATE records SET deleted_at = ? WHERE id = ?`, deletedAt, trashed.ID); err != nil {
		t.Fatalf("backdate deleted_at: %v", err)
	}
	purged, err := repo.PurgeDeletedRecords(ctx)
	if err != nil {
		t.Fatalf("PurgeDeletedRecords() error = %v", err)
	}
	if len(purged) != 1 || purged[0] != trashed.ID {
		t.Fatalf("purged = %#v", purged)
	}
	if _, err := repo.GetRecordByID(ctx, active.ID); err != nil {
		t.Fatalf("active record should remain: %v", err)
	}
	if counts, err := repo.CountRecordChildren(ctx, nil); err != nil || len(counts) != 0 {
		t.Fatalf("CountRecordChildren(empty) = %+v, %v; want empty nil", counts, err)
	}
	if _, err := repo.CountRecordChildren(ctx, []string{" "}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid child-count id error, got %v", err)
	}
	updatedBefore := time.Now().UTC().Add(time.Hour)
	withHTML, err := repo.ListRecords(ctx, repository.ListRecordsFilter{HasHTML: true, UpdatedBefore: &updatedBefore, IncludeDeleted: true})
	if err != nil {
		t.Fatalf("ListRecords(HasHTML UpdatedBefore) error = %v", err)
	}
	if len(withHTML) != 1 || withHTML[0].ID != active.ID {
		t.Fatalf("expected active HTML record after purge, got %+v", withHTML)
	}
	purged, err = repo.PurgeDeletedRecords(ctx)
	if err != nil {
		t.Fatalf("PurgeDeletedRecords(empty) error = %v", err)
	}
	if len(purged) != 0 {
		t.Fatalf("expected no second purge IDs, got %#v", purged)
	}
}

func TestSQLiteListChildAndTemplateBranches(t *testing.T) {
	repo, _ := newConcreteRepo(t)
	ctx := context.Background()
	record := mustCreateRecord(t, repo, repository.CreateRecordInput{
		ID:          "20260305-aaabbb03",
		Date:        "2026-03-05",
		HTMLContent: strPtr("<h1>children</h1>"),
	})
	alt := "Alt"
	if _, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
		RecordID: record.ID,
		Filename: "plot.png",
		S3Key:    "figures/20260305-aaabbb03/plot.png",
		AltText:  &alt,
	}); err != nil {
		t.Fatalf("CreateRecordFigure() error = %v", err)
	}
	figures, err := repo.ListRecordFiguresByRecordID(ctx, record.ID)
	if err != nil {
		t.Fatalf("ListRecordFiguresByRecordID() error = %v", err)
	}
	if len(figures) != 1 || figures[0].AltText == nil {
		t.Fatalf("figures = %#v", figures)
	}
	description := "Data description"
	if _, err := repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
		RecordID:    record.ID,
		Filename:    "data.csv",
		S3Key:       "data/20260305-aaabbb03/data.csv",
		Size:        12,
		Hash:        strings.Repeat("b", 64),
		Description: &description,
	}); err != nil {
		t.Fatalf("CreateRecordDataFile() error = %v", err)
	}
	dataFiles, err := repo.ListRecordDataFilesByRecordID(ctx, record.ID)
	if err != nil {
		t.Fatalf("ListRecordDataFilesByRecordID() error = %v", err)
	}
	if len(dataFiles) != 1 || dataFiles[0].Description == nil {
		t.Fatalf("data files = %#v", dataFiles)
	}
	if _, err := repo.CreateTemplate(ctx, repository.CreateTemplateInput{Name: "extra", HTMLContent: "<main>extra</main>"}); err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
	templates, err := repo.ListTemplates(ctx)
	if err != nil {
		t.Fatalf("ListTemplates() error = %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("templates = %#v", templates)
	}
}

func TestSQLiteProjectPathChatAndUnifiedSearchBranches(t *testing.T) {
	repo, _ := newConcreteRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	projectID := "sqlite/chat-project"
	deviceID := "sqlite-chat-device"
	if _, err := repo.CreateProject(ctx, repository.CreateRegistryInput{ID: projectID, CreatedAt: &now, UpdatedAt: &now}); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if _, err := repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: deviceID, CreatedAt: &now, UpdatedAt: &now}); err != nil {
		t.Fatalf("CreateDevice() error = %v", err)
	}
	if _, _, err := repo.UpsertProjectPath(ctx, repository.CreateProjectPathInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid project path input error, got %v", err)
	}
	path, created, err := repo.UpsertProjectPath(ctx, repository.CreateProjectPathInput{ProjectID: projectID, Path: "/tmp/sqlite-chat", DeviceID: deviceID, CreatedAt: &now, UpdatedAt: &now})
	if err != nil {
		t.Fatalf("UpsertProjectPath() error = %v", err)
	}
	if !created || path.ProjectID != projectID {
		t.Fatalf("unexpected project path result: created=%v path=%+v", created, path)
	}
	if _, created, err = repo.UpsertProjectPath(ctx, repository.CreateProjectPathInput{ProjectID: projectID, Path: "/tmp/sqlite-chat", DeviceID: deviceID}); err != nil || created {
		t.Fatalf("expected idempotent path upsert, created=%v err=%v", created, err)
	}
	paths, err := repo.ListProjectPaths(ctx, &projectID)
	if err != nil {
		t.Fatalf("ListProjectPaths() error = %v", err)
	}
	if len(paths) != 1 || paths[0].Path != "/tmp/sqlite-chat" {
		t.Fatalf("unexpected project paths: %+v", paths)
	}
	allPaths, err := repo.ListProjectPaths(ctx, nil)
	if err != nil {
		t.Fatalf("ListProjectPaths(all) error = %v", err)
	}
	if len(allPaths) != 1 {
		t.Fatalf("unexpected all project paths: %+v", allPaths)
	}

	cwd := "/tmp/sqlite-chat/nested"
	title := "SQLite chat"
	sourcePath := "/tmp/sqlite-chat/session.json"
	session, created, err := repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
		CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:                 "20260514-c0ffee00",
			Source:             "codex",
			SourceSessionID:    "source-1",
			SourceDeviceID:     deviceID,
			CWD:                &cwd,
			Title:              &title,
			StartedAt:          now,
			LastActivityAt:     now.Add(time.Minute),
			OriginalSourcePath: &sourcePath,
			CreatedAt:          &now,
			UpdatedAt:          &now,
		},
		ClearDeleted: true,
	})
	if err != nil {
		t.Fatalf("UpsertChatSession() error = %v", err)
	}
	if !created {
		t.Fatal("expected new chat session")
	}
	updatedSession, created, err := repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
		CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:              "20260514-c0ffee99",
			Source:          "codex",
			SourceSessionID: "source-1",
			SourceDeviceID:  deviceID,
			CWD:             &cwd,
			StartedAt:       now.Add(-time.Minute),
			LastActivityAt:  now.Add(2 * time.Minute),
			CreatedAt:       &now,
			UpdatedAt:       &now,
		},
	})
	if err != nil {
		t.Fatalf("UpsertChatSession(update) error = %v", err)
	}
	if created || updatedSession.ID != session.ID {
		t.Fatalf("expected existing chat update, created=%v session=%+v", created, updatedSession)
	}
	if _, _, err := repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid chat session input error, got %v", err)
	}
	if _, err := repo.GetChatSessionByID(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid chat id lookup error, got %v", err)
	}
	if assigned, err := repo.BackfillChatProjects(ctx); err != nil || assigned != 1 {
		t.Fatalf("BackfillChatProjects() assigned=%d err=%v", assigned, err)
	}
	if assigned, err := repo.BackfillChatProjects(ctx); err != nil || assigned != 0 {
		t.Fatalf("second BackfillChatProjects() assigned=%d err=%v", assigned, err)
	}
	session, err = repo.GetChatSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetChatSessionByID() error = %v", err)
	}
	if session.ProjectID == nil || *session.ProjectID != projectID {
		t.Fatalf("expected backfilled project id, got %+v", session.ProjectID)
	}
	if _, err := repo.GetChatSessionBySource(ctx, "", "source-1"); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid source lookup error, got %v", err)
	}
	bySource, err := repo.GetChatSessionBySource(ctx, "codex", "source-1")
	if err != nil {
		t.Fatalf("GetChatSessionBySource() error = %v", err)
	}
	if bySource.ID != session.ID {
		t.Fatalf("GetChatSessionBySource() id = %q, want %q", bySource.ID, session.ID)
	}
	count, err := repo.CountChatSessions(ctx, repository.ListChatSessionsFilter{ProjectID: &projectID})
	if err != nil {
		t.Fatalf("CountChatSessions() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("chat count = %d, want 1", count)
	}
	if _, err := repo.CountChatSessions(ctx, repository.ListChatSessionsFilter{Offset: -1}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid count filter error, got %v", err)
	}
	if _, err := repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{Limit: -1}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid list filter error, got %v", err)
	}
	source := "codex"
	sessions, err := repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{ProjectID: &projectID, Source: &source, DeviceID: &deviceID})
	if err != nil {
		t.Fatalf("ListChatSessions() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one filtered chat, got %+v", sessions)
	}
	dateFrom := now.Add(-time.Hour)
	dateTo := now.Add(2 * time.Hour)
	updatedAfter := now.Add(-time.Minute)
	sessions, err = repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{
		DateFrom:     &dateFrom,
		DateTo:       &dateTo,
		UpdatedAfter: &updatedAfter,
		Limit:        1,
		Offset:       0,
	})
	if err != nil {
		t.Fatalf("ListChatSessions(date filters) error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one date-filtered chat, got %+v", sessions)
	}
	sessionTwo, created, err := repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{
		CreateChatSessionInput: repository.CreateChatSessionInput{
			ID:              "20260514-c0ffee01",
			Source:          "codex",
			SourceSessionID: "source-2",
			SourceDeviceID:  deviceID,
			ProjectID:       &projectID,
			StartedAt:       now.Add(2 * time.Minute),
			LastActivityAt:  now.Add(2 * time.Minute),
			CreatedAt:       &now,
			UpdatedAt:       &now,
		},
	})
	if err != nil {
		t.Fatalf("UpsertChatSession(second) error = %v", err)
	}
	if !created {
		t.Fatal("expected second chat session")
	}
	sessions, err = repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{ProjectID: &projectID, Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListChatSessions(offset) error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != session.ID {
		t.Fatalf("expected second page to contain first session, got %+v", sessions)
	}
	sessions, err = repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{Unassigned: true})
	if err != nil {
		t.Fatalf("ListChatSessions(unassigned) error = %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected no unassigned chats after backfill, got %+v", sessions)
	}
	if maxOrdinal, err := repo.MaxChatItemOrdinal(ctx, session.ID); err != nil || maxOrdinal != -1 {
		t.Fatalf("initial max ordinal = %d err=%v, want -1", maxOrdinal, err)
	}
	if _, err := repo.ListChatItems(ctx, ""); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid chat item list error, got %v", err)
	}
	if missingItems, err := repo.ListChatItems(ctx, "20260514-deadbeef"); err != nil || len(missingItems) != 0 {
		t.Fatalf("expected missing chat item list to be empty, got %+v err=%v", missingItems, err)
	}
	userText := "sqlite chat needle"
	if _, err := repo.CreateChatItem(ctx, repository.CreateChatItemInput{}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid chat item error, got %v", err)
	}
	if _, err := repo.CreateChatItem(ctx, repository.CreateChatItemInput{SessionID: session.ID, Ordinal: 0, Role: "user", ItemType: "message", Text: &userText, CreatedAt: &now}); err != nil {
		t.Fatalf("CreateChatItem(user) error = %v", err)
	}
	if _, err := repo.CreateChatItem(ctx, repository.CreateChatItemInput{SessionID: session.ID, Ordinal: 0, Role: "user", ItemType: "message", Text: &userText, CreatedAt: &now}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected duplicate chat item conflict, got %v", err)
	}
	toolText := "sqlite chat hidden tool needle"
	if _, err := repo.CreateChatItem(ctx, repository.CreateChatItemInput{SessionID: session.ID, Ordinal: 1, Role: "tool", ItemType: "tool_output", Text: &toolText, SearchText: toolText, CreatedAt: &now}); err != nil {
		t.Fatalf("CreateChatItem(tool) error = %v", err)
	}
	items, err := repo.ListChatItems(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListChatItems() error = %v", err)
	}
	if len(items) != 2 || items[0].SearchText != userText {
		t.Fatalf("unexpected chat items: %+v", items)
	}
	emptyItems, err := repo.ListChatItems(ctx, sessionTwo.ID)
	if err != nil {
		t.Fatalf("ListChatItems(empty session) error = %v", err)
	}
	if len(emptyItems) != 0 {
		t.Fatalf("expected no items for second session, got %+v", emptyItems)
	}
	if err := repo.ReplaceChatItems(ctx, "", nil); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid replace chat items session error, got %v", err)
	}
	if err := repo.ReplaceChatItems(ctx, "20260514-deadbeef", nil); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected missing replace chat items session error, got %v", err)
	}
	if err := repo.ReplaceChatItems(ctx, sessionTwo.ID, []repository.CreateChatItemInput{{Ordinal: -1, Role: "user", ItemType: "message"}}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid replace chat item error, got %v", err)
	}
	replacementText := "replacement text fallback"
	if err := repo.ReplaceChatItems(ctx, sessionTwo.ID, []repository.CreateChatItemInput{
		{Ordinal: 0, Role: "assistant", ItemType: "message", Text: &replacementText, CreatedAt: &now},
		{Ordinal: 0, Role: "assistant", ItemType: "message", Text: &replacementText, CreatedAt: &now},
	}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected duplicate replace chat item conflict, got %v", err)
	}
	if err := repo.ReplaceChatItems(ctx, sessionTwo.ID, []repository.CreateChatItemInput{{Ordinal: 0, Role: "assistant", ItemType: "message", Text: &replacementText, CreatedAt: &now}}); err != nil {
		t.Fatalf("ReplaceChatItems() error = %v", err)
	}
	replacedItems, err := repo.ListChatItems(ctx, sessionTwo.ID)
	if err != nil {
		t.Fatalf("ListChatItems(replaced) error = %v", err)
	}
	if len(replacedItems) != 1 || replacedItems[0].SearchText != replacementText {
		t.Fatalf("unexpected replaced items: %+v", replacedItems)
	}
	if err := repo.AppendChatItems(ctx, "", nil); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid append chat items session error, got %v", err)
	}
	if err := repo.AppendChatItems(ctx, "20260514-deadbeef", nil); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected missing append chat items session error, got %v", err)
	}
	appendedText := "appended text fallback"
	if err := repo.AppendChatItems(ctx, sessionTwo.ID, []repository.CreateChatItemInput{{Ordinal: -1, Role: "user", ItemType: "message", Text: &appendedText, CreatedAt: &now}}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid append item ordinal error, got %v", err)
	}
	if err := repo.AppendChatItems(ctx, sessionTwo.ID, []repository.CreateChatItemInput{{Ordinal: 0, Role: "user", ItemType: "message", Text: &appendedText, CreatedAt: &now}}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected duplicate-ordinal append item conflict, got %v", err)
	}
	if err := repo.AppendChatItems(ctx, sessionTwo.ID, []repository.CreateChatItemInput{{Ordinal: 1, Role: "user", ItemType: "message", Text: &appendedText, CreatedAt: &now}}); err != nil {
		t.Fatalf("AppendChatItems() error = %v", err)
	}
	appendedItems, err := repo.ListChatItems(ctx, sessionTwo.ID)
	if err != nil {
		t.Fatalf("ListChatItems(appended) error = %v", err)
	}
	if len(appendedItems) != 2 || appendedItems[1].SearchText != appendedText {
		t.Fatalf("unexpected appended items: %+v", appendedItems)
	}
	if maxOrdinal, err := repo.MaxChatItemOrdinal(ctx, session.ID); err != nil || maxOrdinal != 1 {
		t.Fatalf("max ordinal after inserts = %d err=%v, want 1", maxOrdinal, err)
	}
	results, err := repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "needle"})
	if err != nil {
		t.Fatalf("SearchChatItems() error = %v", err)
	}
	if len(results) != 1 || results[0].Item.ItemType == "tool_output" {
		t.Fatalf("expected default search to exclude tool output, got %+v", results)
	}
	results, err = repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "needle", IncludeToolOutputs: true, ProjectID: &projectID, Source: &source})
	if err != nil {
		t.Fatalf("SearchChatItems(include tools) error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected two chat search results with tool outputs, got %+v", results)
	}
	results, err = repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "needle", IncludeToolOutputs: true, DateFrom: &dateFrom, DateTo: &dateTo, Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("SearchChatItems(date page) error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one paged chat search result, got %+v", results)
	}
	if _, err := repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "needle", Offset: -1}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid chat search offset error, got %v", err)
	}
	if _, err := repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: " "}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid chat search error, got %v", err)
	}

	notes := "record sqlite needle"
	mustCreateRecord(t, repo, repository.CreateRecordInput{
		ID:             "20260514-aabbccdd",
		Date:           "2026-05-14",
		DayOrder:       "a0",
		HTMLContent:    strPtr("<p>record</p>"),
		Notes:          &notes,
		ProjectID:      projectID,
		SourceDeviceID: deviceID,
		CreatedAt:      &now,
		UpdatedAt:      &now,
	})
	if _, _, err := repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{CreateChatSessionInput: repository.CreateChatSessionInput{
		ID:              "20260514-aabbccdd",
		Source:          "codex",
		SourceSessionID: "record-id-conflict",
		SourceDeviceID:  deviceID,
		StartedAt:       now,
		LastActivityAt:  now,
	}}); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected chat id collision with record to conflict, got %v", err)
	}
	all, err := repo.SearchAll(ctx, repository.UnifiedSearchFilter{Query: "needle", IncludeToolOutputs: true, ProjectID: &projectID})
	if err != nil {
		t.Fatalf("SearchAll() error = %v", err)
	}
	seenDomains := map[string]bool{}
	for _, result := range all {
		seenDomains[result.Domain] = true
	}
	if !seenDomains["records"] || !seenDomains["chats"] {
		t.Fatalf("expected record and chat search domains, got %+v", all)
	}
	chatDomain := "chats"
	chatOnly, err := repo.SearchAll(ctx, repository.UnifiedSearchFilter{Query: "needle", Domain: &chatDomain, Limit: 1, Offset: 1, IncludeToolOutputs: true})
	if err != nil {
		t.Fatalf("SearchAll(chat domain) error = %v", err)
	}
	if len(chatOnly) != 1 || chatOnly[0].Domain != "chats" {
		t.Fatalf("unexpected chat-only search page: %+v", chatOnly)
	}
	firstChatOnly, err := repo.SearchAll(ctx, repository.UnifiedSearchFilter{Query: "needle", Domain: &chatDomain, Limit: 1, IncludeToolOutputs: true})
	if err != nil {
		t.Fatalf("SearchAll(chat domain first page) error = %v", err)
	}
	if len(firstChatOnly) != 1 || firstChatOnly[0].Domain != "chats" {
		t.Fatalf("unexpected first chat-only search page: %+v", firstChatOnly)
	}
	if emptyPage, err := repo.SearchAll(ctx, repository.UnifiedSearchFilter{Query: "needle", Offset: 99}); err != nil || len(emptyPage) != 0 {
		t.Fatalf("expected empty high-offset search page, got %+v err=%v", emptyPage, err)
	}
	if _, err := repo.SearchAll(ctx, repository.UnifiedSearchFilter{Query: ""}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid unified search error, got %v", err)
	}
	recordDomain := "records"
	recordOnly, err := repo.SearchAll(ctx, repository.UnifiedSearchFilter{Query: "needle", Domain: &recordDomain, Limit: 1})
	if err != nil {
		t.Fatalf("SearchAll(record domain) error = %v", err)
	}
	if len(recordOnly) != 1 || recordOnly[0].Domain != "records" {
		t.Fatalf("unexpected record-only search: %+v", recordOnly)
	}
	if _, err := repo.SearchAll(ctx, repository.UnifiedSearchFilter{Query: "needle", Limit: -1}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected invalid unified search limit error, got %v", err)
	}

	// Unified date filters must trim record hits to the window. The test
	// fixture has at least one record whose date falls outside a far-past
	// window, so a unified search restricted to that window must drop it.
	farPast := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	farPastEnd := time.Date(2020, 12, 31, 0, 0, 0, 0, time.UTC)
	pastOnly, err := repo.SearchAll(ctx, repository.UnifiedSearchFilter{
		Query:    "needle",
		DateFrom: &farPast,
		DateTo:   &farPastEnd,
	})
	if err != nil {
		t.Fatalf("SearchAll(past window) error = %v", err)
	}
	for _, r := range pastOnly {
		if r.Domain == "records" {
			t.Fatalf("expected record hits to be excluded by far-past date window, got %+v", r)
		}
	}

	// Offset-only filter on chat list (no Limit) must not fail — the
	// SQLite query builder emits a LIMIT -1 sentinel for this case.
	if _, err := repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{Offset: 0}); err != nil {
		t.Fatalf("ListChatSessions(Offset:0) error = %v", err)
	}
	if _, err := repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{Offset: 1}); err != nil {
		t.Fatalf("ListChatSessions(Offset:1, no Limit) error = %v", err)
	}
	// Same LIMIT -1 sentinel must work for offset-only chat-item search.
	if _, err := repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "needle", Offset: 1, IncludeToolOutputs: true}); err != nil {
		t.Fatalf("SearchChatItems(Offset:1, no Limit) error = %v", err)
	}
	unknownDomain := "unknown"
	unknownResults, err := repo.SearchAll(ctx, repository.UnifiedSearchFilter{Query: "needle", Domain: &unknownDomain})
	if err != nil {
		t.Fatalf("SearchAll(unknown domain) error = %v", err)
	}
	if len(unknownResults) != 0 {
		t.Fatalf("expected no unknown-domain search results, got %+v", unknownResults)
	}
	if err := repo.SoftDeleteChatSession(ctx, "20260514-deadbeef"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected missing soft-delete error, got %v", err)
	}
	if err := repo.DeleteChatSession(ctx, "20260514-deadbeef"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected missing delete error, got %v", err)
	}
	if err := repo.SoftDeleteChatSession(ctx, session.ID); err != nil {
		t.Fatalf("SoftDeleteChatSession() error = %v", err)
	}
	sessions, err = repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{IncludeDeleted: true, Limit: 2})
	if err != nil {
		t.Fatalf("ListChatSessions(IncludeDeleted) error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected active and deleted chats, got %+v", sessions)
	}
	deleted, err := repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{OnlyDeleted: true})
	if err != nil {
		t.Fatalf("ListChatSessions(OnlyDeleted) error = %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("expected one deleted chat, got %+v", deleted)
	}
	deletedResults, err := repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "needle", IncludeDeleted: true, IncludeToolOutputs: true})
	if err != nil {
		t.Fatalf("SearchChatItems(IncludeDeleted) error = %v", err)
	}
	if len(deletedResults) != 2 {
		t.Fatalf("expected deleted chat search results when IncludeDeleted=true, got %+v", deletedResults)
	}
	deletedAll, err := repo.SearchAll(ctx, repository.UnifiedSearchFilter{Query: "needle", Domain: &chatDomain, IncludeDeleted: true, IncludeToolOutputs: true})
	if err != nil {
		t.Fatalf("SearchAll(chats IncludeDeleted) error = %v", err)
	}
	if len(deletedAll) != 2 {
		t.Fatalf("expected deleted chats in unified search, got %+v", deletedAll)
	}
	if err := repo.RestoreChatSession(ctx, session.ID); err != nil {
		t.Fatalf("RestoreChatSession() error = %v", err)
	}
	restored, err := repo.GetChatSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetChatSessionByID(restored) error = %v", err)
	}
	if restored.DeletedAt != nil {
		t.Fatalf("expected restored chat deleted_at nil, got %+v", restored.DeletedAt)
	}
	if err := repo.RestoreChatSession(ctx, "20260514-deadbeef"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected missing restore chat error, got %v", err)
	}
	if err := repo.RestoreChatSession(ctx, ""); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected empty restore chat id to miss, got %v", err)
	}
	if err := repo.SoftDeleteChatSession(ctx, session.ID); err != nil {
		t.Fatalf("SoftDeleteChatSession(second) error = %v", err)
	}
	if err := repo.DeleteChatSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteChatSession() error = %v", err)
	}
	if err := repo.DeleteChatSession(ctx, sessionTwo.ID); err != nil {
		t.Fatalf("DeleteChatSession(second) error = %v", err)
	}
	if _, err := repo.GetChatSessionByID(ctx, session.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected deleted chat to be missing, got %v", err)
	}
}

func TestWriteChatImportBatchReplaceAppendSearchAndSyncVersion(t *testing.T) {
	ctx := context.Background()
	repo, _ := newConcreteRepo(t)
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	if _, err := repo.CreateProject(ctx, repository.CreateRegistryInput{ID: "batch-project"}); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if _, err := repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: "batch-device"}); err != nil {
		t.Fatalf("CreateDevice() error = %v", err)
	}
	before, err := repo.GetSyncVersion(ctx)
	if err != nil {
		t.Fatalf("GetSyncVersion(before) error = %v", err)
	}

	alpha := "batch alpha needle"
	results, err := repo.WriteChatImportBatch(ctx, []repository.ChatImportOp{{
		Session: repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              "20260514-11111111",
				Source:          "codex",
				SourceSessionID: "batch-one",
				SourceDeviceID:  "batch-device",
				ProjectID:       strPtr("batch-project"),
				StartedAt:       now,
				LastActivityAt:  now,
			},
			ClearDeleted: true,
		},
		ItemMode: repository.ChatImportItemModeReplace,
		Items: []repository.CreateChatItemInput{{
			Ordinal:   0,
			Role:      "user",
			ItemType:  "message",
			Text:      &alpha,
			CreatedAt: &now,
		}},
	}})
	if err != nil {
		t.Fatalf("WriteChatImportBatch(replace) error = %v", err)
	}
	if len(results) != 1 || !results[0].Created || results[0].Session.ID != "20260514-11111111" {
		t.Fatalf("unexpected replace results: %+v", results)
	}
	search, err := repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "alpha"})
	if err != nil {
		t.Fatalf("SearchChatItems(alpha) error = %v", err)
	}
	if len(search) != 1 || search[0].Item.SearchText != alpha {
		t.Fatalf("expected batch replace item to be searchable, got %+v", search)
	}
	afterReplace, err := repo.GetSyncVersion(ctx)
	if err != nil {
		t.Fatalf("GetSyncVersion(after replace) error = %v", err)
	}
	if afterReplace.Version <= before.Version {
		t.Fatalf("sync_version did not increase after batch replace: before=%d after=%d", before.Version, afterReplace.Version)
	}

	appended := "batch appended needle"
	results, err = repo.WriteChatImportBatch(ctx, []repository.ChatImportOp{{
		Session: repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              "20260514-11111111",
				Source:          "codex",
				SourceSessionID: "batch-one",
				SourceDeviceID:  "batch-device",
				StartedAt:       now,
				LastActivityAt:  now.Add(time.Minute),
			},
			ClearDeleted: true,
		},
		ItemMode: repository.ChatImportItemModeAppend,
		Items: []repository.CreateChatItemInput{{
			Ordinal:   1,
			Role:      "assistant",
			ItemType:  "message",
			Text:      &appended,
			CreatedAt: &now,
		}},
	}})
	if err != nil {
		t.Fatalf("WriteChatImportBatch(append) error = %v", err)
	}
	if len(results) != 1 || results[0].Created {
		t.Fatalf("expected append to update existing session, got %+v", results)
	}
	items, err := repo.ListChatItems(ctx, "20260514-11111111")
	if err != nil {
		t.Fatalf("ListChatItems() error = %v", err)
	}
	if len(items) != 2 || items[1].SearchText != appended {
		t.Fatalf("unexpected appended items: %+v", items)
	}
	search, err = repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "appended"})
	if err != nil {
		t.Fatalf("SearchChatItems(appended) error = %v", err)
	}
	if len(search) != 1 || search[0].Item.Ordinal != 1 {
		t.Fatalf("expected appended batch item to be searchable, got %+v", search)
	}
	afterAppend, err := repo.GetSyncVersion(ctx)
	if err != nil {
		t.Fatalf("GetSyncVersion(after append) error = %v", err)
	}
	if afterAppend.Version <= afterReplace.Version {
		t.Fatalf("sync_version did not increase after batch append: before=%d after=%d", afterReplace.Version, afterAppend.Version)
	}
}

func TestWriteChatImportBatchRollsBackOnItemConflict(t *testing.T) {
	ctx := context.Background()
	repo, _ := newConcreteRepo(t)
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	if _, err := repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: "batch-device"}); err != nil {
		t.Fatalf("CreateDevice() error = %v", err)
	}
	first := "first rollback item"
	if _, err := repo.WriteChatImportBatch(ctx, []repository.ChatImportOp{{
		Session: repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              "20260514-22222222",
				Source:          "codex",
				SourceSessionID: "batch-rollback",
				SourceDeviceID:  "batch-device",
				StartedAt:       now,
				LastActivityAt:  now,
			},
			ClearDeleted: true,
		},
		ItemMode: repository.ChatImportItemModeReplace,
		Items: []repository.CreateChatItemInput{{
			Ordinal:   0,
			Role:      "user",
			ItemType:  "message",
			Text:      &first,
			CreatedAt: &now,
		}},
	}}); err != nil {
		t.Fatalf("seed WriteChatImportBatch() error = %v", err)
	}

	shouldRollback := "should rollback"
	_, err := repo.WriteChatImportBatch(ctx, []repository.ChatImportOp{{
		Session: repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              "20260514-22222222",
				Source:          "codex",
				SourceSessionID: "batch-rollback",
				SourceDeviceID:  "batch-device",
				StartedAt:       now,
				LastActivityAt:  now.Add(time.Minute),
			},
			ClearDeleted: true,
		},
		ItemMode: repository.ChatImportItemModeAppend,
		Items: []repository.CreateChatItemInput{{
			Ordinal:   1,
			Role:      "assistant",
			ItemType:  "message",
			Text:      &shouldRollback,
			CreatedAt: &now,
		}},
	}, {
		Session: repository.UpsertChatSessionInput{
			CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              "20260514-22222222",
				Source:          "codex",
				SourceSessionID: "batch-rollback",
				SourceDeviceID:  "batch-device",
				StartedAt:       now,
				LastActivityAt:  now.Add(time.Minute),
			},
			ClearDeleted: true,
		},
		ItemMode: repository.ChatImportItemModeAppend,
		Items: []repository.CreateChatItemInput{{
			Ordinal:   0,
			Role:      "assistant",
			ItemType:  "message",
			Text:      &shouldRollback,
			CreatedAt: &now,
		}},
	}})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected conflict from duplicate ordinal, got %v", err)
	}
	items, err := repo.ListChatItems(ctx, "20260514-22222222")
	if err != nil {
		t.Fatalf("ListChatItems() error = %v", err)
	}
	if len(items) != 1 || items[0].SearchText != first {
		t.Fatalf("batch conflict did not roll back all item writes: %+v", items)
	}
	search, err := repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "rollback"})
	if err != nil {
		t.Fatalf("SearchChatItems(rollback) error = %v", err)
	}
	if len(search) != 1 || search[0].Item.SearchText != first {
		t.Fatalf("FTS changed despite rollback: %+v", search)
	}
}

func TestSQLiteChatSmallHelperBranches(t *testing.T) {
	if got := sqliteFTSQuery(`quoted "needle"`); got != `"quoted" """needle"""` {
		t.Fatalf("sqliteFTSQuery() = %q", got)
	}
	recordID := domainResultID(repository.DomainSearchResult{Record: &repository.Record{ID: "record-id"}})
	if recordID != "record-id" {
		t.Fatalf("domainResultID(record) = %q", recordID)
	}
	chatID := domainResultID(repository.DomainSearchResult{Chat: &repository.ChatSearchResult{
		Session: repository.ChatSession{ID: "chat-id"},
		Item:    repository.ChatItem{Ordinal: 7},
	}})
	if chatID != "chat-id/000007" {
		t.Fatalf("domainResultID(chat) = %q", chatID)
	}
	if empty := domainResultID(repository.DomainSearchResult{}); empty != "" {
		t.Fatalf("domainResultID(empty) = %q", empty)
	}
	where, args, err := listRecordsPredicateSQL(repository.ListRecordsFilter{Query: strPtr("needle")}, "", true)
	if err != nil {
		t.Fatalf("listRecordsPredicateSQL(include query) error = %v", err)
	}
	if !strings.Contains(where, "html_content LIKE") || len(args) != 2 {
		t.Fatalf("unexpected LIKE predicate branch where=%q args=%+v", where, args)
	}
	validTime := "2026-05-14T12:00:00.000Z"
	if project, err := (registryRow{ID: "p", CreatedAt: validTime, UpdatedAt: validTime}).toProject(); err != nil || project.ID != "p" {
		t.Fatalf("registryRow.toProject() = %+v, %v", project, err)
	}
	if _, err := (registryRow{CreatedAt: "bad", UpdatedAt: validTime}).toDevice(); err == nil {
		t.Fatal("expected device row to fail on bad created_at")
	}
	if _, err := (registryRow{CreatedAt: validTime, UpdatedAt: validTime, ArchivedAt: sql.NullString{String: "bad", Valid: true}}).toDevice(); err == nil {
		t.Fatal("expected device row to fail on bad archived_at")
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
	deviceID := "device/p"

	methods := []struct {
		name string
		run  func() error
	}{
		{name: "CreateRecord", run: func() error {
			_, err := repo.CreateRecord(ctx, repository.CreateRecordInput{
				ID:             "20260320-abcd1234",
				Date:           "2026-03-20",
				DayOrder:       "a",
				HTMLContent:    strPtr("<h1>x</h1>"),
				ProjectID:      projectID,
				SourceDeviceID: deviceID,
			})
			return err
		}},
		{name: "GetRecordByID", run: func() error {
			_, err := repo.GetRecordByID(ctx, "20260320-abcd1234")
			return err
		}},
		{name: "UpdateRecord", run: func() error {
			_, err := repo.UpdateRecord(ctx, repository.UpdateRecordInput{
				ID:             "20260320-abcd1234",
				Date:           "2026-03-20",
				DayOrder:       "a",
				HTMLContent:    strPtr("<h1>x</h1>"),
				ProjectID:      projectID,
				SourceDeviceID: deviceID,
			})
			return err
		}},
		{name: "ListRecords", run: func() error { _, err := repo.ListRecords(ctx, repository.ListRecordsFilter{}); return err }},
		{name: "CountRecords", run: func() error { _, err := repo.CountRecords(ctx, repository.ListRecordsFilter{}); return err }},
		{name: "CountRecordChildren", run: func() error {
			_, err := repo.CountRecordChildren(ctx, []string{"20260320-abcd1234"})
			return err
		}},
		{name: "SoftDeleteRecord", run: func() error { return repo.SoftDeleteRecord(ctx, "20260320-abcd1234") }},
		{name: "RestoreRecord", run: func() error { return repo.RestoreRecord(ctx, "20260320-abcd1234") }},
		{name: "DeleteRecord", run: func() error { return repo.DeleteRecord(ctx, "20260320-abcd1234") }},
		{name: "CreateRecordFigure", run: func() error {
			_, err := repo.CreateRecordFigure(ctx, repository.CreateRecordFigureInput{
				RecordID: "20260320-abcd1234",
				Filename: "plot.png",
				S3Key:    "figures/20260320-abcd1234/plot.png",
			})
			return err
		}},
		{name: "GetRecordFigureByID", run: func() error {
			_, err := repo.GetRecordFigureByID(ctx, 1)
			return err
		}},
		{name: "UpdateRecordFigure", run: func() error {
			_, err := repo.UpdateRecordFigure(ctx, repository.UpdateRecordFigureInput{ID: 1, Filename: "plot2.png"})
			return err
		}},
		{name: "ListRecordFiguresByRecordID", run: func() error { _, err := repo.ListRecordFiguresByRecordID(ctx, "20260320-abcd1234"); return err }},
		{name: "DeleteRecordFigure", run: func() error { return repo.DeleteRecordFigure(ctx, 1) }},
		{name: "CreateRecordDataFile", run: func() error {
			_, err := repo.CreateRecordDataFile(ctx, repository.CreateRecordDataFileInput{
				RecordID: "20260320-abcd1234",
				Filename: "data.csv",
				S3Key:    "data/20260320-abcd1234/data.csv",
				Size:     1,
				Hash:     strings.Repeat("a", 64),
			})
			return err
		}},
		{name: "GetRecordDataFileByID", run: func() error {
			_, err := repo.GetRecordDataFileByID(ctx, 1)
			return err
		}},
		{name: "UpdateRecordDataFile", run: func() error {
			_, err := repo.UpdateRecordDataFile(ctx, repository.UpdateRecordDataFileInput{ID: 1, Filename: "data2.csv"})
			return err
		}},
		{name: "ListRecordDataFilesByRecordID", run: func() error { _, err := repo.ListRecordDataFilesByRecordID(ctx, "20260320-abcd1234"); return err }},
		{name: "DeleteRecordDataFile", run: func() error { return repo.DeleteRecordDataFile(ctx, 1) }},
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
		{name: "ListProjects", run: func() error { _, err := repo.ListProjects(ctx, true); return err }},
		{name: "CreateProject", run: func() error { _, err := repo.CreateProject(ctx, repository.CreateRegistryInput{ID: "p"}); return err }},
		{name: "GetProjectByID", run: func() error { _, err := repo.GetProjectByID(ctx, "p"); return err }},
		{name: "ArchiveProject", run: func() error { _, err := repo.ArchiveProject(ctx, "p"); return err }},
		{name: "RestoreProject", run: func() error { _, err := repo.RestoreProject(ctx, "p"); return err }},
		{name: "ListDevices", run: func() error { _, err := repo.ListDevices(ctx, true); return err }},
		{name: "CreateDevice", run: func() error { _, err := repo.CreateDevice(ctx, repository.CreateRegistryInput{ID: "d"}); return err }},
		{name: "GetDeviceByID", run: func() error { _, err := repo.GetDeviceByID(ctx, "d"); return err }},
		{name: "ArchiveDevice", run: func() error { _, err := repo.ArchiveDevice(ctx, "d"); return err }},
		{name: "RestoreDevice", run: func() error { _, err := repo.RestoreDevice(ctx, "d"); return err }},
		{name: "UpsertProjectForImport", run: func() error {
			_, err := repo.UpsertProjectForImport(ctx, repository.Project{ID: "p", CreatedAt: time.Now(), UpdatedAt: time.Now()})
			return err
		}},
		{name: "UpsertDeviceForImport", run: func() error {
			_, err := repo.UpsertDeviceForImport(ctx, repository.Device{ID: "d", CreatedAt: time.Now(), UpdatedAt: time.Now()})
			return err
		}},
		{name: "UpsertProjectPath", run: func() error {
			_, _, err := repo.UpsertProjectPath(ctx, repository.CreateProjectPathInput{ProjectID: projectID, Path: "/tmp/project", DeviceID: deviceID})
			return err
		}},
		{name: "ListProjectPaths", run: func() error { _, err := repo.ListProjectPaths(ctx, nil); return err }},
		{name: "BackfillChatProjects", run: func() error { _, err := repo.BackfillChatProjects(ctx); return err }},
		{name: "UpsertChatSession", run: func() error {
			_, _, err := repo.UpsertChatSession(ctx, repository.UpsertChatSessionInput{CreateChatSessionInput: repository.CreateChatSessionInput{
				ID:              "20260320-abcd5678",
				Source:          "codex",
				SourceSessionID: "closed-db-session",
				SourceDeviceID:  deviceID,
				StartedAt:       time.Now().UTC(),
				LastActivityAt:  time.Now().UTC(),
			}})
			return err
		}},
		{name: "GetChatSessionByID", run: func() error { _, err := repo.GetChatSessionByID(ctx, "20260320-abcd5678"); return err }},
		{name: "GetChatSessionBySource", run: func() error { _, err := repo.GetChatSessionBySource(ctx, "codex", "closed-db-session"); return err }},
		{name: "ListChatSessions", run: func() error { _, err := repo.ListChatSessions(ctx, repository.ListChatSessionsFilter{}); return err }},
		{name: "CountChatSessions", run: func() error { _, err := repo.CountChatSessions(ctx, repository.ListChatSessionsFilter{}); return err }},
		{name: "SoftDeleteChatSession", run: func() error { return repo.SoftDeleteChatSession(ctx, "20260320-abcd5678") }},
		{name: "RestoreChatSession", run: func() error { return repo.RestoreChatSession(ctx, "20260320-abcd5678") }},
		{name: "DeleteChatSession", run: func() error { return repo.DeleteChatSession(ctx, "20260320-abcd5678") }},
		{name: "MaxChatItemOrdinal", run: func() error { _, err := repo.MaxChatItemOrdinal(ctx, "20260320-abcd5678"); return err }},
		{name: "CreateChatItem", run: func() error {
			_, err := repo.CreateChatItem(ctx, repository.CreateChatItemInput{SessionID: "20260320-abcd5678", Ordinal: 0, Role: "user", ItemType: "message"})
			return err
		}},
		{name: "ReplaceChatItems", run: func() error {
			return repo.ReplaceChatItems(ctx, "20260320-abcd5678", []repository.CreateChatItemInput{{Ordinal: 0, Role: "user", ItemType: "message"}})
		}},
		{name: "AppendChatItems", run: func() error {
			return repo.AppendChatItems(ctx, "20260320-abcd5678", []repository.CreateChatItemInput{{Ordinal: 0, Role: "user", ItemType: "message"}})
		}},
		{name: "ListChatItems", run: func() error { _, err := repo.ListChatItems(ctx, "20260320-abcd5678"); return err }},
		{name: "SearchChatItems", run: func() error {
			_, err := repo.SearchChatItems(ctx, repository.SearchChatItemsFilter{Query: "x"})
			return err
		}},
		{name: "SearchAll", run: func() error { _, err := repo.SearchAll(ctx, repository.UnifiedSearchFilter{Query: "x"}); return err }},
		{name: "CountActiveRecords", run: func() error { _, err := repo.CountActiveRecords(ctx); return err }},
		{name: "CountTrashedRecords", run: func() error { _, err := repo.CountTrashedRecords(ctx); return err }},
		{name: "PurgeDeletedRecords", run: func() error { _, err := repo.PurgeDeletedRecords(ctx); return err }},
	}

	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			if err := method.run(); err == nil {
				t.Fatalf("expected %s to fail on closed db", method.name)
			}
		})
	}
}
