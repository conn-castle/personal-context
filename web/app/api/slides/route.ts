import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { badRequest, internalError } from "@/lib/api-error";
import { parseQueryInt, isValidISOTimestamp } from "@/lib/validation";
import type { SlideSummary } from "@/lib/types";

// Neon driver supports both tagged-template and function call forms.
// TypeScript only sees the tagged-template signature, so we cast for dynamic queries.
type SqlFn = ReturnType<typeof getDb> &
  ((query: string, params: unknown[]) => Promise<Record<string, unknown>[]>);

type CursorPayload = {
  date: string;
  day_order: string;
  id: string;
};

/**
 * Decodes and validates a pagination cursor.
 *
 * @param raw - The base64-encoded cursor string.
 * @returns The decoded cursor payload, or null if invalid.
 */
function decodeCursor(raw: string): CursorPayload | null {
  try {
    const json = atob(raw);
    const parsed = JSON.parse(json) as Record<string, unknown>;
    if (
      typeof parsed.date !== "string" ||
      typeof parsed.day_order !== "string" ||
      typeof parsed.id !== "string"
    ) {
      return null;
    }
    return parsed as unknown as CursorPayload;
  } catch {
    return null;
  }
}

/**
 * Encodes a cursor payload as a base64 string.
 *
 * @param payload - The cursor data.
 * @returns The base64-encoded cursor.
 */
function encodeCursor(payload: CursorPayload): string {
  return btoa(JSON.stringify(payload));
}

/**
 * GET /api/slides — lists slides with pagination, filtering, and sorting.
 *
 * @param req - The incoming request.
 * @returns Paginated list of slide summaries.
 */
export async function GET(
  req: NextRequest
): Promise<NextResponse> {
  try {
    const sql = getDb() as SqlFn;
    const url = req.nextUrl;

    const limit = parseQueryInt(url.searchParams.get("limit"), 20, 1, 100);
    const cursorRaw = url.searchParams.get("cursor");
    const project = url.searchParams.get("project");
    const deletedParam = url.searchParams.get("deleted");
    const updatedAfter = url.searchParams.get("updated_after");

    // Validate updated_after if provided
    if (updatedAfter !== null && !isValidISOTimestamp(updatedAfter)) {
      return badRequest("Invalid updated_after timestamp");
    }

    // Decode cursor if provided
    let cursor: CursorPayload | null = null;
    if (cursorRaw !== null) {
      cursor = decodeCursor(cursorRaw);
      if (cursor === null) {
        return badRequest("Invalid cursor format");
      }
    }

    // Build dynamic query
    const conditions: string[] = [];
    const params: unknown[] = [];
    let paramIndex = 1;

    // deleted filter
    const showDeleted = deletedParam === "true";
    if (showDeleted) {
      conditions.push("s.deleted_at IS NOT NULL");
    } else {
      conditions.push("s.deleted_at IS NULL");
    }

    // project filter
    if (project !== null) {
      conditions.push(`s.project_id = $${paramIndex++}`);
      params.push(project);
    }

    // updated_after filter (>= per Decision t7u8v9)
    if (updatedAfter !== null) {
      conditions.push(`s.updated_at >= $${paramIndex++}`);
      params.push(updatedAfter);
    }

    // cursor-based pagination (row-value comparison)
    // Sort is: date DESC, day_order ASC, id ASC
    // So "next page" means: rows where (date, day_order, id) comes after the cursor in sort order
    // Which translates to: (date < cursor_date) OR (date = cursor_date AND day_order > cursor_day_order) OR (date = cursor_date AND day_order = cursor_day_order AND id > cursor_id)
    if (cursor !== null) {
      const d = paramIndex++;
      const o = paramIndex++;
      const i = paramIndex++;
      conditions.push(
        `(s.date < $${d} OR (s.date = $${d} AND s.day_order > $${o}) OR (s.date = $${d} AND s.day_order = $${o} AND s.id > $${i}))`
      );
      params.push(cursor.date, cursor.day_order, cursor.id);
    }

    const whereClause =
      conditions.length > 0 ? `WHERE ${conditions.join(" AND ")}` : "";

    const pageSize = limit + 1;

    const queryText = `
      SELECT
        s.id,
        s.date,
        s.day_order,
        s.project_id,
        s.updated_at,
        s.deleted_at,
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
      ${whereClause}
      ORDER BY s.date DESC, s.day_order ASC, s.id ASC
      LIMIT $${paramIndex}
    `;
    params.push(pageSize);

    const rows = (await sql(queryText, params)) as unknown as SlideSummary[];

    const hasNextPage = rows.length > limit;
    const items = hasNextPage ? rows.slice(0, limit) : rows;

    // Build next_cursor
    let nextCursor: string | null = null;
    if (hasNextPage) {
      const last = items[items.length - 1];
      nextCursor = encodeCursor({
        date: last.date,
        day_order: last.day_order,
        id: last.id,
      });
    }

    return NextResponse.json({ items, next_cursor: nextCursor });
  } catch (error) {
    console.error("GET /api/slides error:", error);
    return internalError("Failed to list slides");
  }
}
