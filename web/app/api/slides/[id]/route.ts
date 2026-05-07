import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { bumpS3Version } from "@/lib/s3";
import {
  badRequest,
  invalidId,
  notFound,
  internalError,
} from "@/lib/api-error";
import type { ErrorResponseBody } from "@/lib/api-error";
import { isValidSlideId } from "@/lib/validation";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";
import { requireUser } from "@/lib/auth-helpers";
import type { SlideDetail, SlideFile, DeleteResponse } from "@/lib/types";
import { handlePatchSlide } from "@/lib/slides-handlers";

type RouteContext = { params: Promise<{ id: string }> };

/**
 * GET /api/slides/[id]
 *
 * Returns a single slide with its figures and data files.
 */
export async function GET(
  req: NextRequest,
  context: RouteContext
): Promise<NextResponse<{ slide: SlideDetail } | ErrorResponseBody> | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  const userOrError = await requireUser(req);
  if (userOrError instanceof NextResponse) return userOrError;
  const user = userOrError;

  try {
    const { id } = await context.params;

    if (!isValidSlideId(id)) {
      return invalidId(id);
    }

    const sql = getDb();

    const slideRows = (await sql`
      SELECT id, date, day_order, html_content, notes, project_id,
             git_remote_url, git_hash, created_at, updated_at, deleted_at
      FROM slides
      WHERE id = ${id} AND user_id = ${user.id}
    `) as Record<string, unknown>[];

    if (slideRows.length === 0) {
      return notFound(`Slide not found: ${id}`);
    }

    const [figureRows, dataFileRows] = await Promise.all([
      sql`
        SELECT filename, s3_key, size, hash, alt_text, description
        FROM slide_figures
        WHERE slide_id = ${id}
      ` as Promise<Record<string, unknown>[]>,
      sql`
        SELECT filename, s3_key, size, hash, description
        FROM slide_data_files
        WHERE slide_id = ${id}
      ` as Promise<Record<string, unknown>[]>,
    ]);

    const row = slideRows[0];

    const figures: SlideFile[] = figureRows.map((r) => ({
      filename: r.filename as string,
      s3_key: r.s3_key as string,
      size: r.size != null ? Number(r.size) : undefined,
      hash: r.hash != null ? (r.hash as string) : undefined,
      alt_text: (r.alt_text as string | null) ?? null,
      description: (r.description as string | null) ?? null,
    }));

    const dataFiles: SlideFile[] = dataFileRows.map((r) => ({
      filename: r.filename as string,
      s3_key: r.s3_key as string,
      size: r.size != null ? Number(r.size) : undefined,
      hash: r.hash != null ? (r.hash as string) : undefined,
      description: (r.description as string | null) ?? null,
    }));

    const slide: SlideDetail = {
      id: row.id as string,
      date: row.date as string,
      day_order: row.day_order as string,
      html_content: row.html_content as string,
      notes: (row.notes as string | null) ?? null,
      project_id: (row.project_id as string | null) ?? null,
      git_remote_url: (row.git_remote_url as string | null) ?? null,
      git_hash: (row.git_hash as string | null) ?? null,
      created_at: row.created_at as string,
      updated_at: row.updated_at as string,
      deleted_at: (row.deleted_at as string | null) ?? null,
      figures,
      data_files: dataFiles,
    };

    return NextResponse.json({ slide });
  } catch (err) {
    console.error("GET /api/slides/[id] error:", err);
    return internalError("Failed to fetch slide");
  }
}

/**
 * PATCH /api/slides/[id]
 *
 * Updates a slide with the provided fields (project_id, notes, git_remote_url, git_hash).
 * Bumps the S3 sync version after the Postgres write succeeds.
 */
export async function PATCH(
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

    if (!isValidSlideId(id)) {
      return invalidId(id);
    }

    let body: Record<string, unknown>;

    try {
      const parsed = (await req.json()) as unknown;
      if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
        return badRequest("Request body must be a JSON object");
      }
      body = parsed as Record<string, unknown>;
    } catch {
      return badRequest("Invalid JSON body");
    }

    return handlePatchSlide(id, body, user.id);
  } catch (err) {
    console.error("PATCH /api/slides/[id] error:", err);
    return internalError("Failed to update slide");
  }
}

/**
 * DELETE /api/slides/[id]
 *
 * Soft-deletes a slide by setting deleted_at = NOW().
 * Bumps the S3 sync version after the Postgres write succeeds.
 */
export async function DELETE(
  req: NextRequest,
  context: RouteContext
): Promise<NextResponse<DeleteResponse | ErrorResponseBody> | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  const userOrError = await requireUser(req);
  if (userOrError instanceof NextResponse) return userOrError;
  const user = userOrError;

  try {
    const { id } = await context.params;

    if (!isValidSlideId(id)) {
      return invalidId(id);
    }

    const sql = getDb();

    const rows = (await sql`
      UPDATE slides
      SET deleted_at = NOW()
      WHERE id = ${id} AND user_id = ${user.id} AND deleted_at IS NULL
      RETURNING id, deleted_at, updated_at
    `) as Record<string, unknown>[];

    if (rows.length === 0) {
      return notFound(`Slide not found or already deleted: ${id}`);
    }

    const row = rows[0];

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
        "DELETE /api/slides/[id] S3 version bump failed after Postgres commit:",
        error
      );
    }

    return NextResponse.json({
      id: row.id as string,
      deleted_at: row.deleted_at as string,
      updated_at: row.updated_at as string,
      sync_version: syncVersion,
    });
  } catch (err) {
    console.error("DELETE /api/slides/[id] error:", err);
    return internalError("Failed to delete slide");
  }
}
