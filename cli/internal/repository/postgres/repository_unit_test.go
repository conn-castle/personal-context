package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/conn-castle/personal-context/cli/internal/repository"
)

func TestNewFailsForNilPool(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("expected error for nil pool, got nil")
	}
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestMapPgErrorNil(t *testing.T) {
	if mapPgError(nil) != nil {
		t.Fatal("expected nil for nil error")
	}
}

func TestMapPgErrorNoRows(t *testing.T) {
	err := mapPgError(pgx.ErrNoRows)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for ErrNoRows, got %v", err)
	}
}

func TestMapPgErrorUniqueViolation(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", Message: "duplicate key"}
	err := mapPgError(pgErr)
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected ErrConflict for unique violation, got %v", err)
	}
}

func TestMapPgErrorForeignKeyViolation(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23503", Message: "foreign key violation"}
	err := mapPgError(pgErr)
	if !errors.Is(err, repository.ErrForeignKeyViolation) {
		t.Fatalf("expected ErrForeignKeyViolation for FK violation, got %v", err)
	}
}

func TestMapPgErrorPassthrough(t *testing.T) {
	orig := errors.New("other error")
	err := mapPgError(orig)
	if !errors.Is(err, orig) {
		t.Fatalf("expected passthrough for unknown error, got %v", err)
	}
}

func TestEnsureRowsAffectedZero(t *testing.T) {
	tag := pgconn.NewCommandTag("UPDATE 0")
	err := ensureRowsAffected(tag)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for 0 rows affected, got %v", err)
	}
}

func TestEnsureRowsAffectedOne(t *testing.T) {
	tag := pgconn.NewCommandTag("UPDATE 1")
	err := ensureRowsAffected(tag)
	if err != nil {
		t.Fatalf("expected nil for 1 row affected, got %v", err)
	}
}

func TestApplySchemaFailsForNilPool(t *testing.T) {
	err := ApplySchema(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}
