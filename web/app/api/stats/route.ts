import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { internalError } from "@/lib/api-error";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";
import { requireUser } from "@/lib/auth-helpers";
import type { StatsResponse } from "@/lib/types";
import type { ErrorResponseBody } from "@/lib/api-error";

/**
 * GET /api/stats
 *
 * Returns total slide count, active registry project count, and trashed slide count.
 */
export async function GET(
  req: NextRequest
): Promise<NextResponse<StatsResponse | ErrorResponseBody> | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  const userOrError = await requireUser(req);
  if (userOrError instanceof NextResponse) return userOrError;
  const user = userOrError;

  try {
    const sql = getDb();

    const [totalResult, projectResult, trashedResult] = await Promise.all([
      sql`SELECT COUNT(*)::int AS count FROM slides WHERE user_id = ${user.id} AND deleted_at IS NULL`,
      sql`SELECT COUNT(*)::int AS count FROM projects WHERE user_id = ${user.id} AND archived_at IS NULL`,
      sql`SELECT COUNT(*)::int AS count FROM slides WHERE user_id = ${user.id} AND deleted_at IS NOT NULL`,
    ]);

    const totalSlides =
      ((totalResult as Record<string, unknown>[])[0]?.count as number) ?? 0;
    const totalProjects =
      ((projectResult as Record<string, unknown>[])[0]?.count as number) ?? 0;
    const trashedSlides =
      ((trashedResult as Record<string, unknown>[])[0]?.count as number) ?? 0;

    return NextResponse.json({
      total_slides: totalSlides,
      total_projects: totalProjects,
      trashed_slides: trashedSlides,
    });
  } catch (err) {
    console.error("GET /api/stats error:", err);
    return internalError("Failed to fetch stats");
  }
}
