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
	_, err := New(nil, "user-id")
	if err == nil {
		t.Fatal("expected error for nil pool, got nil")
	}
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestNewFailsForEmptyUserID(t *testing.T) {
	// Cannot construct a real pool without a Postgres server, but the userID
	// validation fires before pool is used.
	_, err := New(nil, "")
	if err == nil {
		t.Fatal("expected error for empty userID, got nil")
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

func TestLessSameDomainTiebreak(t *testing.T) {
	chatResult := func(sessionID string, ordinal int) repository.DomainSearchResult {
		return repository.DomainSearchResult{
			Domain: "chats",
			Chat: &repository.ChatSearchResult{
				Session: repository.ChatSession{ID: sessionID},
				Item:    repository.ChatItem{Ordinal: ordinal},
			},
		}
	}
	recordResult := func(id string) repository.DomainSearchResult {
		return repository.DomainSearchResult{Domain: "records", Record: &repository.Record{ID: id}}
	}

	// Records tiebreak by id, mirroring `ORDER BY rank DESC, id`.
	if !lessSameDomainTiebreak(recordResult("20260305-aaaaaaaa"), recordResult("20260305-bbbbbbbb")) {
		t.Fatal("record tiebreak: lower id must sort first")
	}
	if lessSameDomainTiebreak(recordResult("20260305-bbbbbbbb"), recordResult("20260305-aaaaaaaa")) {
		t.Fatal("record tiebreak: higher id must not sort first")
	}

	// Chats tiebreak by session id first, then ordinal.
	if !lessSameDomainTiebreak(chatResult("20260305-aaaaaaaa", 99), chatResult("20260305-bbbbbbbb", 1)) {
		t.Fatal("chat tiebreak: lower session id must sort first regardless of ordinal")
	}

	// Ordinals compare NUMERICALLY, not lexicographically, mirroring the SQL
	// `ci.ordinal` column. Lexicographically "1000000" < "999999" ('1' < '9'),
	// but numerically 999999 < 1000000. The merge must match SQL numeric order so
	// the bounded per-domain LIMIT keeps the correct pagination boundary element.
	if !lessSameDomainTiebreak(chatResult("20260305-deadbeef", 999_999), chatResult("20260305-deadbeef", 1_000_000)) {
		t.Fatal("chat tiebreak: ordinal 999999 must sort before 1000000 (numeric order)")
	}
	if lessSameDomainTiebreak(chatResult("20260305-deadbeef", 1_000_000), chatResult("20260305-deadbeef", 999_999)) {
		t.Fatal("chat tiebreak: ordinal 1000000 must not sort before 999999")
	}

	// Equal results are not strictly less than each other (irreflexive).
	if lessSameDomainTiebreak(chatResult("20260305-deadbeef", 7), chatResult("20260305-deadbeef", 7)) {
		t.Fatal("chat tiebreak: equal results must not report less-than")
	}
}
