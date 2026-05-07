import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { internalError } from "@/lib/api-error";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";
import { requireUser } from "@/lib/auth-helpers";
import type { ProjectsResponse } from "@/lib/types";
import type { ErrorResponseBody } from "@/lib/api-error";

/**
 * GET /api/projects
 *
 * Returns active project IDs from the registry.
 */
export async function GET(
  req: NextRequest
): Promise<NextResponse<ProjectsResponse | ErrorResponseBody> | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  const userOrError = await requireUser(req);
  if (userOrError instanceof NextResponse) return userOrError;
  const user = userOrError;

  try {
    const sql = getDb();
    const rows = (await sql`
      SELECT id
      FROM projects
      WHERE user_id = ${user.id} AND archived_at IS NULL
      ORDER BY id
    `) as Record<string, unknown>[];

    const projects = rows.map((row) => row.id as string);

    return NextResponse.json({ projects });
  } catch (err) {
    console.error("GET /api/projects error:", err);
    return internalError("Failed to fetch projects");
  }
}
