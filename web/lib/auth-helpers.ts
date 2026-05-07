import { NextResponse } from "next/server";
import crypto from "crypto";
import { auth } from "@/lib/auth";
import { getPool } from "@/lib/db-pool";
import { extractBearerToken } from "@/lib/bearer-token";

interface AuthenticatedUser {
  id: string;
  email: string;
}

/**
 * Resolves the authenticated user from the request.
 *
 * Checks for a Bearer token (API key) first, then falls back to Auth.js
 * session (web UI). Returns the authenticated user or a 401 response.
 *
 * @param req - The incoming request (used for Authorization header).
 * @returns The authenticated user, or a NextResponse with 401 status.
 */
export async function requireUser(
  req: Request,
): Promise<AuthenticatedUser | NextResponse> {
  // Check for Bearer token first (API key auth for CLI).
  const rawKey = extractBearerToken(req.headers.get("authorization"));
  if (rawKey !== null) {
    return validateApiKey(rawKey);
  }

  return requireSessionUser();
}

/**
 * Resolves the authenticated user from an Auth.js session only.
 *
 * Used by endpoints that must not accept bearer API keys.
 *
 * @returns The authenticated user from session, or a 401 response.
 */
export async function requireSessionUser(): Promise<AuthenticatedUser | NextResponse> {
  const session = await auth();
  if (!session?.user?.id || typeof session.user.email !== "string") {
    return NextResponse.json(
      { error: "Unauthorized", code: "UNAUTHORIZED" },
      { status: 401 },
    );
  }

  return { id: session.user.id, email: session.user.email };
}

/**
 * Validates an API key by hashing it and looking up the hash in the database.
 * Updates last_used_at on successful validation.
 *
 * @param rawKey - The raw API key string.
 * @returns The user associated with the key, or a 401 response.
 */
async function validateApiKey(
  rawKey: string,
): Promise<AuthenticatedUser | NextResponse> {
  const keyHash = crypto.createHash("sha256").update(rawKey).digest("hex");
  const pool = getPool();

  const { rows } = await pool.query(
    `SELECT ak.user_id, u.email
     FROM api_keys ak
     JOIN users u ON u.id = ak.user_id
     WHERE ak.key_hash = $1 AND ak.revoked_at IS NULL`,
    [keyHash],
  );

  if (rows.length === 0) {
    return NextResponse.json(
      { error: "Invalid or revoked API key", code: "UNAUTHORIZED" },
      { status: 401 },
    );
  }

  // Update last_used_at (fire-and-forget; don't block the response).
  pool
    .query(
      "UPDATE api_keys SET last_used_at = NOW() WHERE key_hash = $1",
      [keyHash],
    )
    .catch((err: unknown) => {
      console.warn("Failed to update api_key last_used_at:", err);
    });

  return { id: rows[0].user_id, email: rows[0].email };
}
