import { NextRequest, NextResponse } from "next/server";
import { getPool } from "@/lib/db-pool";
import { badRequest, notFound, internalError } from "@/lib/api-error";
import type { ErrorResponseBody } from "@/lib/api-error";
import { requireSessionUser } from "@/lib/auth-helpers";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";

type RouteContext = { params: Promise<{ id: string }> };

const UUID_REGEX = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * DELETE /api/api-keys/[id]
 *
 * Revokes an API key by setting revoked_at = NOW().
 * Only the key's owner can revoke it.
 */
export async function DELETE(
  req: NextRequest,
  context: RouteContext
): Promise<NextResponse<{ id: string; revoked_at: string } | ErrorResponseBody> | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  const userOrError = await requireSessionUser();
  if (userOrError instanceof NextResponse) return userOrError;
  const user = userOrError;

  try {
    const { id } = await context.params;

    if (!UUID_REGEX.test(id)) {
      return badRequest(`Invalid API key ID: ${id}`);
    }

    const pool = getPool();
    const { rows } = await pool.query(
      `UPDATE api_keys
       SET revoked_at = NOW()
       WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
       RETURNING id, revoked_at`,
      [id, user.id]
    );

    if (rows.length === 0) {
      return notFound(`API key not found or already revoked: ${id}`);
    }

    return NextResponse.json({
      id: rows[0].id as string,
      revoked_at: (rows[0].revoked_at as Date).toISOString(),
    });
  } catch (err) {
    console.error("DELETE /api/api-keys/[id] error:", err);
    return internalError("Failed to revoke API key");
  }
}
