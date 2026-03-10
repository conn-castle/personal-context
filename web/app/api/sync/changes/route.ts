import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { badRequest, internalError } from "@/lib/api-error";
import type { ErrorResponseBody } from "@/lib/api-error";
import { isValidISOTimestamp } from "@/lib/validation";
import type { SyncChangesResponse, SlideSummary } from "@/lib/types";

/**
 * GET /api/sync/changes?since={ISO 8601 UTC}
 *
 * Returns slides modified at or after the given timestamp (>= comparison,
 * per Decision t7u8v9). Includes soft-deleted slides so clients can
 * synchronize deletions.
 */
export async function GET(
  req: NextRequest
): Promise<NextResponse<SyncChangesResponse | ErrorResponseBody>> {
  try {
    const since = req.nextUrl.searchParams.get("since");

    if (!since) {
      return badRequest("Missing required query parameter: since");
    }

    if (!isValidISOTimestamp(since)) {
      return badRequest(
        "Invalid since parameter: must be a valid ISO 8601 timestamp"
      );
    }

    const sql = getDb();

    const [items, serverNowResult] = await Promise.all([
      sql.query(
        `SELECT id, date, day_order, project_id, updated_at, deleted_at,
           (SELECT COUNT(*)::int FROM slide_figures WHERE slide_id = s.id) as figure_count,
           (SELECT COUNT(*)::int FROM slide_data_files WHERE slide_id = s.id) as data_file_count
         FROM slides s
         WHERE updated_at >= $1
         ORDER BY date DESC, day_order ASC, id ASC`,
        [since]
      ) as Promise<Record<string, unknown>[]>,
      sql`SELECT NOW() as server_now` as Promise<Record<string, unknown>[]>,
    ]);

    const slideSummaries: SlideSummary[] = items.map((row) => ({
      id: row.id as string,
      date: row.date as string,
      day_order: row.day_order as string,
      project_id: (row.project_id as string | null) ?? null,
      updated_at: row.updated_at as string,
      deleted_at: (row.deleted_at as string | null) ?? null,
      figure_count: Number(row.figure_count),
      data_file_count: Number(row.data_file_count),
    }));

    const serverNow = serverNowResult[0].server_now as string;

    return NextResponse.json({ items: slideSummaries, server_now: serverNow });
  } catch (err) {
    console.error("GET /api/sync/changes error:", err);
    return internalError("Failed to fetch sync changes");
  }
}
