import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { internalError } from "@/lib/api-error";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";
import { requireUser } from "@/lib/auth-helpers";
import { bumpS3Version, deleteS3Objects } from "@/lib/s3";
import type { PurgeTrashResponse } from "@/lib/types";
import type { ErrorResponseBody } from "@/lib/api-error";

/**
 * DELETE /api/records/trash
 *
 * Permanently deletes all soft-deleted (trashed) records and their child rows.
 * S3 object cleanup is best-effort — files are orphaned on failure but DB is consistent.
 */
export async function DELETE(
  req: NextRequest
): Promise<NextResponse<PurgeTrashResponse | ErrorResponseBody> | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  const userOrError = await requireUser(req);
  if (userOrError instanceof NextResponse) return userOrError;
  const user = userOrError;

  try {
    const sql = getDb();

    // Run count, key collection, and delete in a single transaction so all
    // statements see the same snapshot — prevents a concurrent restore from
    // causing S3 key divergence.
    const [countResult, figureKeys, dataFileKeys] = (await sql.transaction([
      sql`SELECT COUNT(*)::int AS count FROM records WHERE user_id = ${user.id} AND deleted_at IS NOT NULL`,
      sql`
        SELECT sf.s3_key FROM record_figures sf
        JOIN records s ON s.id = sf.record_id
        WHERE s.user_id = ${user.id} AND s.deleted_at IS NOT NULL
      `,
      sql`
        SELECT sdf.s3_key FROM record_data_files sdf
        JOIN records s ON s.id = sdf.record_id
        WHERE s.user_id = ${user.id} AND s.deleted_at IS NOT NULL
      `,
      sql`DELETE FROM records WHERE user_id = ${user.id} AND deleted_at IS NOT NULL`,
    ])) as [
      Record<string, unknown>[],
      Record<string, unknown>[],
      Record<string, unknown>[],
    ];
    const purgedCount = (countResult[0]?.count as number) ?? 0;

    if (purgedCount > 0) {
      // Best-effort S3 cleanup
      const allKeys = [
        ...figureKeys.map((r) => r.s3_key as string),
        ...dataFileKeys.map((r) => r.s3_key as string),
      ].filter(Boolean);

      if (allKeys.length > 0) {
        try {
          await deleteS3Objects(allKeys, user.id);
        } catch (s3Err) {
          // Log but don't fail — DB is already consistent
          console.warn("S3 cleanup after purge failed:", s3Err);
        }
      }
    }

    // Get current sync version
    const versionResult = (await sql`
      SELECT version, updated_at FROM sync_version WHERE user_id = ${user.id}
    `) as { version: number; updated_at: string }[];
    const syncVersion = (versionResult[0]?.version as number) ?? 0;
    const syncUpdatedAt =
      versionResult[0]?.updated_at ?? new Date().toISOString();

    if (purgedCount > 0) {
      try {
        await bumpS3Version(syncVersion, syncUpdatedAt, user.id);
      } catch (error) {
        console.error(
          "DELETE /api/records/trash S3 version bump failed after Postgres commit:",
          error
        );
      }
    }

    return NextResponse.json({
      purged_count: purgedCount,
      sync_version: syncVersion,
    });
  } catch (err) {
    console.error("DELETE /api/records/trash error:", err);
    return internalError("Failed to purge trash");
  }
}
