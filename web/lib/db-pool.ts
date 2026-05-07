import { Pool } from "pg";

let pool: Pool | null = null;

/**
 * Returns a standard Postgres connection pool (module-level singleton).
 *
 * Uses the standard `pg` package — NOT `@neondatabase/serverless` — so that
 * auth operations have zero Neon-specific dependency. Any Postgres provider
 * will work.
 *
 * @returns A `pg.Pool` instance connected via DATABASE_URL.
 * @throws If DATABASE_URL is not set.
 */
export function getPool(): Pool {
  if (pool) return pool;

  const connectionString = process.env.DATABASE_URL;
  if (!connectionString) {
    throw new Error("DATABASE_URL environment variable is required");
  }

  pool = new Pool({ connectionString, max: 5 });
  return pool;
}

/**
 * Resets the cached pool connection. Test-only.
 */
export function resetPool(): void {
  if (pool) {
    pool.end().catch(() => {});
    pool = null;
  }
}
