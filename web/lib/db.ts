import { neon } from "@neondatabase/serverless";

type NeonSql = ReturnType<typeof neon>;

let sql: NeonSql | null = null;

/**
 * Returns a Neon HTTP driver query function (module-level singleton).
 * Avoids re-parsing the connection string on every call.
 *
 * @returns The neon tagged template query function.
 * @throws If DATABASE_URL is not set.
 */
export function getDb(): NeonSql {
  if (sql) return sql;

  const connectionString = process.env.DATABASE_URL;
  if (!connectionString) {
    throw new Error("DATABASE_URL environment variable is required");
  }

  sql = neon(connectionString);
  return sql;
}

/**
 * Resets the cached database connection. Test-only.
 */
export function resetDb(): void {
  sql = null;
}
