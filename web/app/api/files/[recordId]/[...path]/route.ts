import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { getPresignedUrl } from "@/lib/s3";
import { invalidId, notFound, badRequest, internalError } from "@/lib/api-error";
import { isValidRecordId, isValidFilename } from "@/lib/validation";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";
import { requireUser } from "@/lib/auth-helpers";
import type { FileUrlResponse } from "@/lib/types";

// Neon driver supports both tagged-template and function call forms.
// TypeScript only sees the tagged-template signature, so we cast for dynamic queries.
type SqlFn = ReturnType<typeof getDb> &
  ((query: string, params: unknown[]) => Promise<Record<string, unknown>[]>);

type RouteContext = {
  params: Promise<{ recordId: string; path: string[] }>;
};

const VALID_FILE_TYPES = ["figures", "data"] as const;
type FileType = (typeof VALID_FILE_TYPES)[number];

/** Maps file type to its database table name. */
const FILE_TYPE_TABLE: Record<FileType, string> = {
  figures: "record_figures",
  data: "record_data_files",
};

/**
 * GET /api/files/[recordId]/[...path] — returns a presigned URL for a record file.
 *
 * URL pattern: /api/files/{recordId}/figures/{filename} or /api/files/{recordId}/data/{filename}
 *
 * @param _req - The incoming request (unused).
 * @param context - Route context containing recordId and path segments.
 * @returns Presigned URL and expiration, or an error.
 */
export async function GET(
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
    const { recordId, path } = await context.params;

    if (!isValidRecordId(recordId)) {
      return invalidId(recordId);
    }

    // Validate path structure: must be exactly [type, filename]
    if (!path || path.length !== 2) {
      return badRequest("Path must include file type and filename");
    }

    const fileType = path[0];
    const filename = path[1];

    // Validate file type
    if (!VALID_FILE_TYPES.includes(fileType as FileType)) {
      return badRequest(
        `Invalid file type: ${fileType}. Must be "figures" or "data"`
      );
    }

    // Validate filename (prevents path traversal)
    if (!isValidFilename(filename)) {
      return badRequest(`Invalid filename: ${filename}`);
    }

    const sql = getDb() as SqlFn;
    const table = FILE_TYPE_TABLE[fileType as FileType];

    // Defense-in-depth: FILE_TYPE_TABLE is already a closed map keyed by
    // the FileType union, so `table` can only be one of the known values.
    // This runtime check guards against future regressions if the map is
    // ever widened, preventing arbitrary table names from reaching the query.
    // Intentionally untested: this branch is unreachable under normal
    // operation because VALID_FILE_TYPES constrains the input before
    // FILE_TYPE_TABLE is accessed. It exists as defense-in-depth only.
    /* c8 ignore next 3 */
    const VALID_TABLES = new Set(["record_figures", "record_data_files"]);
    if (!VALID_TABLES.has(table)) {
      throw new Error(`Invalid table: ${table}`);
    }

    // Resolve the canonical storage key from the DB row.
    // Join with records to verify ownership.
    const rows = await sql(
      `SELECT f.s3_key FROM ${table} f JOIN records s ON s.id = f.record_id WHERE f.record_id = $1 AND f.filename = $2 AND s.user_id = $3`,
      [recordId, filename, user.id]
    );

    if (rows.length === 0) {
      return notFound(`File not found: ${fileType}/${filename}`);
    }

    // Generate presigned URL
    const s3Key = rows[0].s3_key as string;
    const { url, expires_at } = await getPresignedUrl(s3Key, user.id);

    const response: FileUrlResponse = { url, expires_at };
    return NextResponse.json(response);
  } catch (error) {
    console.error("GET /api/files/[recordId]/[...path] error:", error);
    return internalError("Failed to generate file URL");
  }
}
