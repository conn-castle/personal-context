import { NextRequest, NextResponse } from "next/server";
import crypto from "crypto";
import { getPool } from "@/lib/db-pool";
import { badRequest, internalError } from "@/lib/api-error";
import type { ErrorResponseBody } from "@/lib/api-error";
import { requireSessionUser } from "@/lib/auth-helpers";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";

interface ApiKeyListItem {
  id: string;
  label: string;
  created_at: string;
  last_used_at: string | null;
  revoked_at: string | null;
}

interface CreateApiKeyResponse {
  id: string;
  label: string;
  raw_key: string;
  created_at: string;
}

/**
 * GET /api/api-keys
 *
 * Lists all API keys for the authenticated user (never exposes the raw key).
 */
export async function GET(
  req: NextRequest
): Promise<NextResponse<{ keys: ApiKeyListItem[] } | ErrorResponseBody> | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  const userOrError = await requireSessionUser();
  if (userOrError instanceof NextResponse) return userOrError;
  const user = userOrError;

  try {
    const pool = getPool();
    const { rows } = await pool.query(
      `SELECT id, label, created_at, last_used_at, revoked_at
       FROM api_keys
       WHERE user_id = $1
       ORDER BY created_at DESC`,
      [user.id]
    );

    const keys: ApiKeyListItem[] = rows.map((row) => ({
      id: row.id as string,
      label: row.label as string,
      created_at: (row.created_at as Date).toISOString(),
      last_used_at: row.last_used_at
        ? (row.last_used_at as Date).toISOString()
        : null,
      revoked_at: row.revoked_at
        ? (row.revoked_at as Date).toISOString()
        : null,
    }));

    return NextResponse.json({ keys });
  } catch (err) {
    console.error("GET /api/api-keys error:", err);
    return internalError("Failed to list API keys");
  }
}

/**
 * POST /api/api-keys
 *
 * Creates a new API key. Returns the raw key exactly once — it cannot be
 * retrieved again after this response.
 *
 * Body: { "label": "My laptop" }
 */
export async function POST(
  req: NextRequest
): Promise<NextResponse<CreateApiKeyResponse | ErrorResponseBody> | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  const userOrError = await requireSessionUser();
  if (userOrError instanceof NextResponse) return userOrError;
  const user = userOrError;

  try {
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

    const label = body.label;
    if (typeof label !== "string" || label.trim().length === 0) {
      return badRequest("label is required and must be a non-empty string");
    }

    // Generate raw key with recognizable prefix.
    const rawKey = `pc_key_${crypto.randomUUID()}`;
    const keyHash = crypto.createHash("sha256").update(rawKey).digest("hex");

    const pool = getPool();
    const { rows } = await pool.query(
      `INSERT INTO api_keys (user_id, key_hash, label)
       VALUES ($1, $2, $3)
       RETURNING id, label, created_at`,
      [user.id, keyHash, label.trim()]
    );

    const row = rows[0];
    const response: CreateApiKeyResponse = {
      id: row.id as string,
      label: row.label as string,
      raw_key: rawKey,
      created_at: (row.created_at as Date).toISOString(),
    };

    return NextResponse.json(response, { status: 201 });
  } catch (err) {
    console.error("POST /api/api-keys error:", err);
    return internalError("Failed to create API key");
  }
}
