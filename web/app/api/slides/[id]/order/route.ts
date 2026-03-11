import { NextRequest, NextResponse } from "next/server";
import { getDb } from "@/lib/db";
import { bumpS3Version } from "@/lib/s3";
import { invalidId, notFound, badRequest, internalError } from "@/lib/api-error";
import { isValidSlideId, isValidDate } from "@/lib/validation";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";
import { generateKeyBetween } from "fractional-indexing";
import type { ReorderInput, ReorderResponse } from "@/lib/types";

type RouteContext = { params: Promise<{ id: string }> };

type SiblingRow = {
  id: string;
  day_order: string;
};

/**
 * Validates the request body for the reorder endpoint.
 *
 * @param body - The parsed JSON body.
 * @returns Validation result with parsed data or error message.
 */
function validateReorderInput(
  body: Record<string, unknown>
): { valid: true; data: ReorderInput } | { valid: false; error: string } {
  if (!body.position || typeof body.position !== "object") {
    return { valid: false, error: "position is required" };
  }

  const position = body.position as Record<string, unknown>;
  const kind = position.kind;

  if (
    kind !== "first" &&
    kind !== "last" &&
    kind !== "before" &&
    kind !== "after"
  ) {
    return {
      valid: false,
      error: 'position.kind must be "first", "last", "before", or "after"',
    };
  }

  if ((kind === "before" || kind === "after") && !position.reference_id) {
    return {
      valid: false,
      error: `position.reference_id is required for kind "${kind}"`,
    };
  }

  if (
    (kind === "before" || kind === "after") &&
    typeof position.reference_id !== "string"
  ) {
    return {
      valid: false,
      error: "position.reference_id must be a string",
    };
  }

  if (
    (kind === "before" || kind === "after") &&
    !isValidSlideId(position.reference_id as string)
  ) {
    return { valid: false, error: "Invalid reference_id format" };
  }

  if (body.date !== undefined) {
    if (typeof body.date !== "string" || !isValidDate(body.date)) {
      return { valid: false, error: "Invalid date format" };
    }
  }

  const result: ReorderInput = {
    position:
      kind === "before" || kind === "after"
        ? {
            kind,
            reference_id: position.reference_id as string,
          }
        : { kind },
  };

  if (body.date !== undefined) {
    result.date = body.date as string;
  }

  return { valid: true, data: result };
}

/**
 * Computes the fractional index for the target position among siblings.
 *
 * @param siblings - All sibling slides for the target date, sorted by day_order ASC.
 * @param position - The desired position.
 * @returns The computed fractional index, or null if reference not found.
 */
function computeFractionalIndex(
  siblings: SiblingRow[],
  position: ReorderInput["position"]
): string | null {
  if (position.kind === "first") {
    const firstOrder = siblings.length > 0 ? siblings[0].day_order : null;
    return generateKeyBetween(null, firstOrder);
  }

  if (position.kind === "last") {
    const lastOrder =
      siblings.length > 0 ? siblings[siblings.length - 1].day_order : null;
    return generateKeyBetween(lastOrder, null);
  }

  // before or after — type narrowed: kind is "before" | "after" with reference_id
  const refPosition = position as {
    kind: "before" | "after";
    reference_id: string;
  };
  const refIndex = siblings.findIndex(
    (s) => s.id === refPosition.reference_id
  );
  if (refIndex === -1) {
    return null; // reference not found
  }

  if (position.kind === "before") {
    const prevOrder = refIndex > 0 ? siblings[refIndex - 1].day_order : null;
    const refOrder = siblings[refIndex].day_order;
    return generateKeyBetween(prevOrder, refOrder);
  }

  // "after"
  const refOrder = siblings[refIndex].day_order;
  const nextOrder =
    refIndex < siblings.length - 1
      ? siblings[refIndex + 1].day_order
      : null;
  return generateKeyBetween(refOrder, nextOrder);
}

/**
 * PATCH /api/slides/[id]/order — reorders a slide within its date or moves to a new date.
 *
 * @param req - The incoming request with JSON body.
 * @param context - Route context containing the slide ID.
 * @returns The updated slide order info with sync version.
 */
export async function PATCH(
  req: NextRequest,
  context: RouteContext
): Promise<NextResponse | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  try {
    const { id } = await context.params;

    if (!isValidSlideId(id)) {
      return invalidId(id);
    }

    let body: Record<string, unknown>;
    try {
      const parsed = await req.json();
      if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
        return badRequest("Request body must be a JSON object");
      }
      body = parsed as Record<string, unknown>;
    } catch {
      return badRequest("Invalid JSON body");
    }

    const validation = validateReorderInput(body);
    if (!validation.valid) {
      return badRequest(validation.error);
    }

    const input = validation.data;

    if (
      (input.position.kind === "before" || input.position.kind === "after") &&
      (input.position as { reference_id?: string }).reference_id === id
    ) {
      return badRequest("Cannot reorder a slide relative to itself");
    }

    const sql = getDb();

    // Read current slide to get its date
    const slideRows = (await sql`SELECT id, date, day_order FROM slides WHERE id = ${id} AND deleted_at IS NULL`) as {
      id: string;
      date: string;
      day_order: string;
    }[];

    if (slideRows.length === 0) {
      return notFound(`Slide not found: ${id}`);
    }

    const targetDate = input.date ?? slideRows[0].date;

    // Read siblings for the target date (exclude the moving slide)
    const siblings = (await sql`SELECT id, day_order FROM slides WHERE date = ${targetDate} AND deleted_at IS NULL AND id != ${id} ORDER BY day_order ASC, id ASC`) as SiblingRow[];

    // Compute new fractional index
    const newOrder = computeFractionalIndex(siblings, input.position);
    if (newOrder === null) {
      const pos = input.position as { reference_id?: string };
      return notFound(
        `Reference slide not found: ${pos.reference_id ?? ""}`
      );
    }

    // Update the slide
    const updateRows = (await sql`UPDATE slides SET date = ${targetDate}, day_order = ${newOrder} WHERE id = ${id} RETURNING id, date, day_order, updated_at`) as {
      id: string;
      date: string;
      day_order: string;
      updated_at: string;
    }[];

    const updated = updateRows[0];

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
        "PATCH /api/slides/[id]/order S3 version bump failed after Postgres commit:",
        error
      );
    }

    const response: ReorderResponse = {
      id: updated.id,
      date: updated.date,
      day_order: updated.day_order,
      updated_at: updated.updated_at,
      sync_version: syncVersion,
    };

    return NextResponse.json(response);
  } catch (error) {
    console.error("PATCH /api/slides/[id]/order error:", error);
    return internalError("Failed to reorder slide");
  }
}
