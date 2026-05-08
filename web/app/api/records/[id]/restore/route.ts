import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { bumpS3Version } from "@/lib/s3";
import { invalidId, notFound, internalError } from "@/lib/api-error";
import { isValidRecordId } from "@/lib/validation";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";
import { requireUser } from "@/lib/auth-helpers";
import type { RestoreResponse } from "@/lib/types";

type RouteContext = { params: Promise<{ id: string }> };

/**
 * POST /api/records/[id]/restore — restores a soft-deleted record.
 *
 * @param _req - The incoming request (unused).
 * @param context - Route context containing the record ID.
 * @returns The restored record info with sync version.
 */
export async function POST(
  req: NextRequest,
  context: RouteContext
): Promise<NextResponse | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  const userOrError = await requireUser(req);
  if (userOrError instanceof NextResponse) return userOrError;
  const user = userOrError;

  try {
    const { id } = await context.params;

    if (!isValidRecordId(id)) {
      return invalidId(id);
    }

    const sql = getDb();

    // Only restore records that are currently deleted
    const rows = (await sql`UPDATE records SET deleted_at = NULL WHERE id = ${id} AND user_id = ${user.id} AND deleted_at IS NOT NULL RETURNING id, deleted_at, updated_at`) as {
      id: string;
      deleted_at: null;
      updated_at: string;
    }[];

    if (rows.length === 0) {
      return notFound(`Record not found or not deleted: ${id}`);
    }

    const row = rows[0];

    // Read sync_version and bump S3
    const versionRows = (await sql`SELECT version, updated_at FROM sync_version WHERE user_id = ${user.id}`) as {
      version: number;
      updated_at: string;
    }[];
    const syncVersion = versionRows[0]?.version ?? 0;
    const syncUpdatedAt = versionRows[0]?.updated_at ?? new Date().toISOString();

    try {
      await bumpS3Version(syncVersion, syncUpdatedAt, user.id);
    } catch (error) {
      console.error(
        "POST /api/records/[id]/restore S3 version bump failed after Postgres commit:",
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
    console.error("POST /api/records/[id]/restore error:", error);
    return internalError("Failed to restore record");
  }
}
