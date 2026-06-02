import { NextResponse } from "next/server";

// The full set of machine-readable error codes the API can return. This union
// is the single source of truth for the error-response contract: every error
// body emitted by a route handler or lib helper must use one of these codes.
// Keep it in sync with the helpers below — there should be no inline
// `NextResponse.json({ error, code })` error responses that bypass this type.
export type ErrorCode =
  | "NOT_FOUND"
  | "BAD_REQUEST"
  | "REQUEST_BODY_TOO_LARGE"
  | "INTERNAL_ERROR"
  | "INVALID_ID"
  | "UNAUTHORIZED"
  | "CONFLICT"
  | "INVALID_CONFIG"
  | "LOCAL_BACKEND_UNAVAILABLE"
  | "LOCAL_MODE_AUTH_DISABLED"
  | "REGISTRATION_DISABLED";

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

/**
 * Returns a 401 UNAUTHORIZED response for missing or invalid authentication.
 *
 * @param message - Error message (default "Unauthorized").
 */
export function unauthorized(
  message = "Unauthorized"
): NextResponse<ErrorResponseBody> {
  return apiError(401, "UNAUTHORIZED", message);
}

/**
 * Returns a 409 CONFLICT response for a resource-state conflict.
 *
 * @param message - Error message.
 */
export function conflict(message: string): NextResponse<ErrorResponseBody> {
  return apiError(409, "CONFLICT", message);
}

/**
 * Returns a 500 INVALID_CONFIG response for a server misconfiguration that
 * prevents the request from being served (e.g. a malformed LOCAL_BACKEND_URL).
 *
 * @param message - Error message (default describes the local backend config).
 */
export function invalidConfig(
  message = "Invalid LOCAL_BACKEND_URL configuration"
): NextResponse<ErrorResponseBody> {
  return apiError(500, "INVALID_CONFIG", message);
}

/**
 * Returns a 502 LOCAL_BACKEND_UNAVAILABLE response when the local Go backend
 * cannot be reached while proxying in local mode.
 *
 * @param message - Error message (default "Local backend unavailable").
 */
export function localBackendUnavailable(
  message = "Local backend unavailable"
): NextResponse<ErrorResponseBody> {
  return apiError(502, "LOCAL_BACKEND_UNAVAILABLE", message);
}

/**
 * Returns a 403 LOCAL_MODE_AUTH_DISABLED response when an auth-only endpoint is
 * hit while the server runs in local mode (auth is intentionally disabled).
 *
 * @param message - Error message.
 */
export function localModeAuthDisabled(
  message: string
): NextResponse<ErrorResponseBody> {
  return apiError(403, "LOCAL_MODE_AUTH_DISABLED", message);
}

/**
 * Returns a 403 REGISTRATION_DISABLED response when registration is turned off.
 *
 * @param message - Error message (default "Registration is disabled.").
 */
export function registrationDisabled(
  message = "Registration is disabled."
): NextResponse<ErrorResponseBody> {
  return apiError(403, "REGISTRATION_DISABLED", message);
}
