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
  isValidSlideId,
  validateSlideUpdateInput,
} from "@/lib/validation";
import type { SlideDetail, SlideFile } from "@/lib/types";

// Neon driver supports both tagged-template and function call forms.
// TypeScript only sees the tagged-template signature, so we cast for dynamic queries.
type SqlFn = ReturnType<typeof getDb> &
  ((query: string, params: unknown[]) => Promise<Record<string, unknown>[]>);

/**
 * Handles the PATCH /api/slides/[id] logic.
 * Updates a slide with the provided fields and returns the updated slide detail.
 *
 * @param id - The slide ID from the URL path.
 * @param body - The parsed request body with fields to update.
 * @returns A NextResponse with the updated slide or an error.
 */
export async function handlePatchSlide(
  id: string,
  body: Record<string, unknown>
): Promise<NextResponse> {
  try {
    // Validate ID
    if (!isValidSlideId(id)) {
      return invalidId(id);
    }

    // Validate body
    const validation = validateSlideUpdateInput(body);
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
    const queryText = `UPDATE slides SET ${setClauses.join(", ")} WHERE id = $${paramIndex} AND deleted_at IS NULL RETURNING *`;

    const rows = (await sql(queryText, values)) as Record<string, unknown>[];

    if (rows.length === 0) {
      return notFound(`Slide not found: ${id}`);
    }

    const row = rows[0];

    // Read sync_version (trigger already fired from the UPDATE above)
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
        "PATCH /api/slides/[id] S3 version bump failed after Postgres commit:",
        error
      );
    }

    // Fetch child rows for full detail
    const figures = (await sql`SELECT filename, s3_key, size, hash, alt_text, description FROM slide_figures WHERE slide_id = ${id}`) as SlideFile[];
    const dataFiles = (await sql`SELECT filename, s3_key, size, hash, description FROM slide_data_files WHERE slide_id = ${id}`) as SlideFile[];

    const slide: SlideDetail = {
      id: row.id as string,
      date: row.date as string,
      day_order: row.day_order as string,
      html_content: row.html_content as string,
      notes: row.notes as string | null,
      project_id: row.project_id as string | null,
      git_remote_url: row.git_remote_url as string | null,
      git_hash: row.git_hash as string | null,
      created_at: row.created_at as string,
      updated_at: row.updated_at as string,
      deleted_at: row.deleted_at as string | null,
      figures,
      data_files: dataFiles,
    };

    return NextResponse.json({ slide, sync_version: syncVersion });
  } catch (error) {
    console.error("PATCH /api/slides/[id] error:", error);
    return internalError("Failed to update slide");
  }
}
