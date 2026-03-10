import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { internalError } from "@/lib/api-error";
import type { ProjectsResponse } from "@/lib/types";
import type { ErrorResponseBody } from "@/lib/api-error";

/**
 * GET /api/projects
 *
 * Returns distinct project IDs from non-deleted slides.
 */
export async function GET(
  _req: NextRequest
): Promise<NextResponse<ProjectsResponse | ErrorResponseBody>> {
  try {
    void _req;
    const sql = getDb();
    const rows = (await sql`
      SELECT DISTINCT project_id
      FROM slides
      WHERE deleted_at IS NULL AND project_id IS NOT NULL
      ORDER BY project_id
    `) as Record<string, unknown>[];

    const projects = rows.map((row) => row.project_id as string);

    return NextResponse.json({ projects });
  } catch (err) {
    console.error("GET /api/projects error:", err);
    return internalError("Failed to fetch projects");
  }
}
