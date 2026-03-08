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
	_, err := pool.Exec(ctx, schemaSQL)
	if err != nil {
		return fmt.Errorf("apply postgres schema: %w", err)
	}
	return nil
}
