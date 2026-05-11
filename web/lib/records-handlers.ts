import { NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { bumpS3Version } from "@/lib/s3";
import {
  invalidId,
  notFound,
  badRequest,
  internalError,
} from "@/lib/api-error";
import {
  isValidRecordId,
  validateRecordUpdateInput,
} from "@/lib/validation";
import type { RecordDetail, RecordFile } from "@/lib/types";

// Neon driver supports both tagged-template and function call forms.
// TypeScript only sees the tagged-template signature, so we cast for dynamic queries.
type SqlFn = ReturnType<typeof getDb> &
  ((query: string, params: unknown[]) => Promise<Record<string, unknown>[]>);

/**
 * Handles the PATCH /api/records/[id] logic.
 * Updates a record with the provided fields and returns the updated record detail.
 *
 * @param id - The record ID from the URL path.
 * @param body - The parsed request body with fields to update.
 * @param userId - The authenticated user's ID for query scoping.
 * @returns A NextResponse with the updated record or an error.
 */
export async function handlePatchRecord(
  id: string,
  body: Record<string, unknown>,
  userId: string,
): Promise<NextResponse> {
  try {
    // Validate ID
    if (!isValidRecordId(id)) {
      return invalidId(id);
    }

    // Validate body
    const validation = validateRecordUpdateInput(body);
    if (!validation.valid) {
      return badRequest(validation.error);
    }

    const { data } = validation;
    const sql = getDb() as SqlFn;

    // Build dynamic UPDATE from the validated & normalized data
    const setClauses: string[] = [];
    const values: unknown[] = [];
    let paramIndex = 1;

    if ("project_id" in data) {
      setClauses.push(`project_id = $${paramIndex++}`);
      values.push(data.project_id);
    }

    if ("notes" in data) {
      setClauses.push(`notes = $${paramIndex++}`);
      values.push(data.notes);
    }

    if ("git_remote_url" in data) {
      setClauses.push(`git_remote_url = $${paramIndex++}`);
      values.push(data.git_remote_url);
    }

    if ("git_hash" in data) {
      setClauses.push(`git_hash = $${paramIndex++}`);
      values.push(data.git_hash);
    }

    values.push(id);
    values.push(userId);
    const queryText = `UPDATE records SET ${setClauses.join(", ")} WHERE id = $${paramIndex} AND user_id = $${paramIndex + 1} AND deleted_at IS NULL RETURNING *`;

    const rows = (await sql(queryText, values)) as Record<string, unknown>[];

    if (rows.length === 0) {
      return notFound(`Record not found: ${id}`);
    }

    const row = rows[0];

    // Read sync_version (trigger already fired from the UPDATE above)
    const versionRows = (await sql`SELECT version, updated_at FROM sync_version WHERE user_id = ${userId}`) as {
      version: number;
      updated_at: string;
    }[];
    const syncVersion = versionRows[0]?.version ?? 0;
    const syncUpdatedAt = versionRows[0]?.updated_at ?? new Date().toISOString();

    try {
      await bumpS3Version(syncVersion, syncUpdatedAt, userId);
    } catch (error) {
      console.error(
        "PATCH /api/records/[id] S3 version bump failed after Postgres commit:",
        error
      );
    }

    // Fetch child rows for full detail. Defense-in-depth INNER JOIN on
    // records so cross-tenant access stays impossible even if a refactor
    // reorders these queries relative to the UPDATE ownership check above.
    const figureRows = (await sql`
      SELECT f.filename, f.s3_key, f.alt_text
      FROM record_figures f
      INNER JOIN records r ON r.id = f.record_id
      WHERE f.record_id = ${id} AND r.user_id = ${userId}
    `) as Record<string, unknown>[];
    const dataFiles = (await sql`
      SELECT d.filename, d.s3_key, d.size, d.hash, d.description
      FROM record_data_files d
      INNER JOIN records r ON r.id = d.record_id
      WHERE d.record_id = ${id} AND r.user_id = ${userId}
    `) as RecordFile[];

    const figures: RecordFile[] = figureRows.map((r) => ({
      filename: r.filename as string,
      s3_key: r.s3_key as string,
      alt_text: (r.alt_text as string | null) ?? null,
    }));

    const record: RecordDetail = {
      id: row.id as string,
      date: row.date as string,
      day_order: row.day_order as string,
      html_content: (row.html_content as string | null) ?? null,
      notes: row.notes as string | null,
      project_id: row.project_id as string,
      source_device_id: row.source_device_id as string,
      source_ref: row.source_ref as string | null,
      git_remote_url: row.git_remote_url as string | null,
      git_hash: row.git_hash as string | null,
      created_at: row.created_at as string,
      updated_at: row.updated_at as string,
      deleted_at: row.deleted_at as string | null,
      figures,
      data_files: dataFiles,
    };

    return NextResponse.json({ record, sync_version: syncVersion });
  } catch (error) {
    console.error("PATCH /api/records/[id] error:", error);
    return internalError("Failed to update record");
  }
}
