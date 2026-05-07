package postgres

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed postgres_schema.sql
var schemaSQL string

// ApplySchema executes the embedded Postgres DDL against the pool.
// Args: ctx controls cancellation; pool is the target connection pool.
// Returns: nil on success or an error describing the schema application failure.
func ApplySchema(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("pool is required")
	}

	legacy, reason, err := detectLegacyPreAuthSchema(ctx, pool)
	if err != nil {
		return fmt.Errorf("detect legacy schema: %w", err)
	}
	if legacy {
		return fmt.Errorf(
			"legacy pre-auth cloud schema detected (%s): in-place migration is required before applying auth-aware schema",
			reason,
		)
	}

	_, err = pool.Exec(ctx, schemaSQL)
	if err != nil {
		return fmt.Errorf("apply postgres schema: %w", err)
	}
	return nil
}

func detectLegacyPreAuthSchema(ctx context.Context, pool *pgxpool.Pool) (bool, string, error) {
	slidesExists, err := tableExists(ctx, pool, "slides")
	if err != nil {
		return false, "", err
	}
	usersExists, err := tableExists(ctx, pool, "users")
	if err != nil {
		return false, "", err
	}
	if slidesExists && !usersExists {
		return true, "slides table exists but users table is missing", nil
	}
	if slidesExists {
		hasSlideUserID, err := columnExists(ctx, pool, "slides", "user_id")
		if err != nil {
			return false, "", err
		}
		if !hasSlideUserID {
			return true, "slides.user_id is missing", nil
		}
	}

	syncVersionExists, err := tableExists(ctx, pool, "sync_version")
	if err != nil {
		return false, "", err
	}
	if syncVersionExists {
		hasSyncUserID, err := columnExists(ctx, pool, "sync_version", "user_id")
		if err != nil {
			return false, "", err
		}
		hasLegacyID, err := columnExists(ctx, pool, "sync_version", "id")
		if err != nil {
			return false, "", err
		}
		if !hasSyncUserID && hasLegacyID {
			return true, "sync_version.id exists and sync_version.user_id is missing", nil
		}
	}

	return false, "", nil
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, tableName string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (
            SELECT 1
            FROM information_schema.tables
            WHERE table_schema = current_schema()
              AND table_name = $1
        )`,
		tableName,
	).Scan(&exists)
	return exists, err
}

func columnExists(ctx context.Context, pool *pgxpool.Pool, tableName string, columnName string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = current_schema()
              AND table_name = $1
              AND column_name = $2
        )`,
		tableName,
		columnName,
	).Scan(&exists)
	return exists, err
}
