import { NextRequest, NextResponse } from "next/server";
import { getPool } from "@/lib/db-pool";
import { hashPassword } from "@/lib/password";
import { canonicalizeEmailIdentity } from "@/lib/email-identity";
import { getLocalModeState } from "@/lib/local-mode";
import {
  badRequest,
  conflict,
  internalError,
  invalidConfig,
  localModeAuthDisabled,
  registrationDisabled,
} from "@/lib/api-error";
import {
  JsonBodyError,
  jsonBodyErrorResponse,
  readBoundedJson,
} from "@/lib/bounded-json";

interface PgErrorLike {
  code?: string;
  constraint?: string;
}

function isUniqueEmailViolation(error: unknown): boolean {
  if (!error || typeof error !== "object") {
    return false;
  }

  const pgError = error as PgErrorLike;
  if (pgError.code !== "23505") {
    return false;
  }

  return pgError.constraint === undefined || pgError.constraint === "users_email_key";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/**
 * POST /api/register — Creates a new user account.
 *
 * Validates inputs, checks REGISTRATION_ENABLED env var, hashes the password,
 * and inserts into the users table. Returns the created user (without password_hash).
 */
export async function POST(req: NextRequest): Promise<NextResponse> {
  const localMode = getLocalModeState();
  if (localMode.hasConfigError) {
    return invalidConfig();
  }

  if (localMode.enabled) {
    return localModeAuthDisabled("Registration is unavailable in local mode.");
  }

  // Check if registration is enabled.
  const enabled = process.env.REGISTRATION_ENABLED === "true";
  if (!enabled) {
    return registrationDisabled();
  }

  let body: unknown;
  try {
    body = await readBoundedJson(req);
  } catch (error) {
    if (error instanceof JsonBodyError) {
      return jsonBodyErrorResponse(error);
    }
    return badRequest("Invalid JSON body.");
  }
  if (!isRecord(body)) {
    return badRequest("JSON body must be an object.");
  }

  const { email, name, password } = body;
  const canonicalEmail = typeof email === "string" ? canonicalizeEmailIdentity(email) : "";

  if (!canonicalEmail.includes("@")) {
    return badRequest("A valid email is required.");
  }

  if (!password || typeof password !== "string" || password.length < 8) {
    return badRequest("Password must be at least 8 characters.");
  }

  if (name !== undefined && name !== null && typeof name !== "string") {
    return badRequest("Name must be a string or null.");
  }

  const pool = getPool();
  const passwordHash = await hashPassword(password);
  try {
    const { rows } = await pool.query(
      `INSERT INTO users (id, email, name, password_hash)
       VALUES (gen_random_uuid()::TEXT, $1, $2, $3)
       RETURNING id, email, name, created_at`,
      [canonicalEmail, name ?? null, passwordHash],
    );

    return NextResponse.json(rows[0], { status: 201 });
  } catch (error) {
    if (isUniqueEmailViolation(error)) {
      return conflict("An account with this email already exists.");
    }

    console.error("POST /api/register error:", error);
    return internalError("Registration failed.");
  }
}
