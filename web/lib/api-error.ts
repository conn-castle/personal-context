import { NextResponse } from "next/server";

export type ErrorCode =
  | "NOT_FOUND"
  | "BAD_REQUEST"
  | "REQUEST_BODY_TOO_LARGE"
  | "INTERNAL_ERROR"
  | "INVALID_ID"
  | "METHOD_NOT_ALLOWED";

export type ErrorResponseBody = {
  error: string;
  code: ErrorCode;
};

/**
 * Creates a JSON error response with the given status, code, and message.
 *
 * @param status - HTTP status code.
 * @param code - Machine-readable error code.
 * @param message - Human-readable error message.
 * @returns A NextResponse with JSON error body.
 */
export function apiError(
  status: number,
  code: ErrorCode,
  message: string
): NextResponse<ErrorResponseBody> {
  return NextResponse.json({ error: message, code }, { status });
}

/**
 * Returns a 404 NOT_FOUND response.
 *
 * @param message - Error message.
 */
export function notFound(message: string): NextResponse<ErrorResponseBody> {
  return apiError(404, "NOT_FOUND", message);
}

/**
 * Returns a 400 BAD_REQUEST response.
 *
 * @param message - Error message.
 */
export function badRequest(message: string): NextResponse<ErrorResponseBody> {
  return apiError(400, "BAD_REQUEST", message);
}

/**
 * Returns a 400 INVALID_ID response for malformed record IDs.
 *
 * @param id - The invalid record ID.
 */
export function invalidId(id: string): NextResponse<ErrorResponseBody> {
  return apiError(400, "INVALID_ID", `Invalid record ID format: ${id}`);
}

/**
 * Returns a 500 INTERNAL_ERROR response.
 *
 * @param message - Error message.
 */
export function internalError(
  message: string
): NextResponse<ErrorResponseBody> {
  return apiError(500, "INTERNAL_ERROR", message);
}
