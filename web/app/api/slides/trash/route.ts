import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { internalError } from "@/lib/api-error";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";
import { bumpS3Version, deleteS3Objects } from "@/lib/s3";
import type { PurgeTrashResponse } from "@/lib/types";
import type { ErrorResponseBody } from "@/lib/api-error";

/**
 * DELETE /api/slides/trash
 *
 * Permanently deletes all soft-deleted (trashed) slides and their child rows.
 * S3 object cleanup is best-effort — files are orphaned on failure but DB is consistent.
 */
export async function DELETE(
  req: NextRequest
): Promise<NextResponse<PurgeTrashResponse | ErrorResponseBody> | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  try {
    const sql = getDb();

    // Count trashed slides before deleting
    const countResult = (await sql`
      SELECT COUNT(*)::int AS count FROM slides WHERE deleted_at IS NOT NULL
    `) as Record<string, unknown>[];
    const purgedCount = (countResult[0]?.count as number) ?? 0;

    if (purgedCount > 0) {
      // Collect S3 keys for cleanup before deleting DB rows
      const figureKeys = (await sql`
        SELECT sf.s3_key FROM slide_figures sf
        JOIN slides s ON s.id = sf.slide_id
        WHERE s.deleted_at IS NOT NULL
      `) as Record<string, unknown>[];
      const dataFileKeys = (await sql`
        SELECT sdf.s3_key FROM slide_data_files sdf
        JOIN slides s ON s.id = sdf.slide_id
        WHERE s.deleted_at IS NOT NULL
      `) as Record<string, unknown>[];

      // Hard-delete from DB (CASCADE handles child rows)
      await sql`DELETE FROM slides WHERE deleted_at IS NOT NULL`;

      // Best-effort S3 cleanup
      const allKeys = [
        ...figureKeys.map((r) => r.s3_key as string),
        ...dataFileKeys.map((r) => r.s3_key as string),
      ].filter(Boolean);

      if (allKeys.length > 0) {
        try {
          await deleteS3Objects(allKeys);
        } catch (s3Err) {
          // Log but don't fail — DB is already consistent
          console.warn("S3 cleanup after purge failed:", s3Err);
        }
      }
    }

    // Get current sync version
    const versionResult = (await sql`
      SELECT version, updated_at FROM sync_version LIMIT 1
    `) as { version: number; updated_at: string }[];
    const syncVersion = (versionResult[0]?.version as number) ?? 0;
    const syncUpdatedAt =
      versionResult[0]?.updated_at ?? new Date().toISOString();

    if (purgedCount > 0) {
      try {
        await bumpS3Version(syncVersion, syncUpdatedAt);
      } catch (error) {
        console.error(
          "DELETE /api/slides/trash S3 version bump failed after Postgres commit:",
          error
        );
      }
    }

    return NextResponse.json({
      purged_count: purgedCount,
      sync_version: syncVersion,
    });
  } catch (err) {
    console.error("DELETE /api/slides/trash error:", err);
    return internalError("Failed to purge trash");
  }
}
