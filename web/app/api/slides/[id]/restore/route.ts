import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { bumpS3Version } from "@/lib/s3";
import { invalidId, notFound, internalError } from "@/lib/api-error";
import { isValidSlideId } from "@/lib/validation";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";
import type { RestoreResponse } from "@/lib/types";

type RouteContext = { params: Promise<{ id: string }> };

/**
 * POST /api/slides/[id]/restore — restores a soft-deleted slide.
 *
 * @param _req - The incoming request (unused).
 * @param context - Route context containing the slide ID.
 * @returns The restored slide info with sync version.
 */
export async function POST(
  _req: NextRequest,
  context: RouteContext
): Promise<NextResponse | Response> {
  if (isLocalMode()) {
    return proxyToLocal(_req);
  }

  try {
    const { id } = await context.params;

    if (!isValidSlideId(id)) {
      return invalidId(id);
    }

    const sql = getDb();

    // Only restore slides that are currently deleted
    const rows = (await sql`UPDATE slides SET deleted_at = NULL WHERE id = ${id} AND deleted_at IS NOT NULL RETURNING id, deleted_at, updated_at`) as {
      id: string;
      deleted_at: null;
      updated_at: string;
    }[];

    if (rows.length === 0) {
      return notFound(`Slide not found or not deleted: ${id}`);
    }

    const row = rows[0];

    // Read sync_version and bump S3
    const versionRows = (await sql`SELECT version, updated_at FROM sync_version LIMIT 1`) as {
      version: number;
      updated_at: string;
    }[];
    const syncVersion = versionRows[0]?.version ?? 0;
    const syncUpdatedAt = versionRows[0]?.updated_at ?? new Date().toISOString();

    try {
      await bumpS3Version(syncVersion, syncUpdatedAt);
    } catch (error) {
      console.error(
        "POST /api/slides/[id]/restore S3 version bump failed after Postgres commit:",
        error
      );
    }

    const response: RestoreResponse = {
      id: row.id,
      deleted_at: null,
      updated_at: row.updated_at,
      sync_version: syncVersion,
    };

    return NextResponse.json(response);
  } catch (error) {
    console.error("POST /api/slides/[id]/restore error:", error);
    return internalError("Failed to restore slide");
  }
}
