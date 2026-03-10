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

    // Fetch server_now first and use it as the cutoff for the items query
    // to avoid a race condition where items could be modified between the
    // two queries, causing missed or duplicated changes.
    const serverNowResult = (await sql`SELECT NOW() as server_now`) as Record<string, unknown>[];
    const serverNow = serverNowResult[0].server_now as string;

    const items = (await sql.query(
      `SELECT s.id, s.date, s.day_order, s.html_content, s.project_id, s.updated_at, s.deleted_at,
           COALESCE(fc.figure_count, 0) AS figure_count,
           COALESCE(dc.data_file_count, 0) AS data_file_count
         FROM slides s
         LEFT JOIN (
           SELECT slide_id, COUNT(*)::int AS figure_count
           FROM slide_figures
           GROUP BY slide_id
         ) fc ON fc.slide_id = s.id
         LEFT JOIN (
           SELECT slide_id, COUNT(*)::int AS data_file_count
           FROM slide_data_files
           GROUP BY slide_id
         ) dc ON dc.slide_id = s.id
         WHERE s.updated_at >= $1 AND s.updated_at <= $2
         ORDER BY s.date DESC, s.day_order ASC, s.id ASC`,
      [since, serverNow]
    )) as Record<string, unknown>[];

    const slideSummaries: SlideSummary[] = items.map((row) => ({
      id: row.id as string,
      date: row.date as string,
      day_order: row.day_order as string,
      html_content: row.html_content as string,
      project_id: (row.project_id as string | null) ?? null,
      updated_at: row.updated_at as string,
      deleted_at: (row.deleted_at as string | null) ?? null,
      figure_count: Number(row.figure_count),
      data_file_count: Number(row.data_file_count),
    }));

    return NextResponse.json({ items: slideSummaries, server_now: serverNow });
  } catch (err) {
    console.error("GET /api/sync/changes error:", err);
    return internalError("Failed to fetch sync changes");
  }
}
