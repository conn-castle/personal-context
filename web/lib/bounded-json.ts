import type { NextResponse } from "next/server";
import type { NextRequest } from "next/server";
import { apiError } from "@/lib/api-error";
import type { ErrorResponseBody } from "@/lib/api-error";

export const MAX_JSON_BODY_BYTES = 4 * 1024 * 1024;

export class JsonBodyError extends Error {
  readonly status: number;
  readonly code: ErrorResponseBody["code"];

  constructor(status: number, code: ErrorResponseBody["code"], message: string) {
    super(message);
    this.name = "JsonBodyError";
    this.status = status;
    this.code = code;
  }
}

/**
 * Reads and parses a JSON request body with the same 4 MB cap used by pc serve.
 *
 * @param req - The incoming Next.js request.
 * @param maxBytes - Maximum allowed UTF-8 body size.
 * @returns Parsed JSON payload.
 */
export async function readBoundedJson(
  req: NextRequest,
  maxBytes = MAX_JSON_BODY_BYTES
): Promise<unknown> {
  const contentLength = req.headers.get("content-length");
  if (contentLength) {
    const parsedLength = Number.parseInt(contentLength, 10);
    if (Number.isFinite(parsedLength) && parsedLength > maxBytes) {
      throw new JsonBodyError(
        413,
        "REQUEST_BODY_TOO_LARGE",
        `JSON body exceeds ${maxBytes} bytes`
      );
    }
  }

  const reader = req.body?.getReader();
  if (!reader) {
    throw new JsonBodyError(400, "BAD_REQUEST", "Invalid JSON body");
  }

  const chunks: Uint8Array[] = [];
  let total = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    const chunk = value as Uint8Array;
    total += chunk.byteLength;
    if (total > maxBytes) {
      throw new JsonBodyError(
        413,
        "REQUEST_BODY_TOO_LARGE",
        `JSON body exceeds ${maxBytes} bytes`
      );
    }
    chunks.push(chunk);
  }

  const bytes = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }

  try {
    return JSON.parse(new TextDecoder().decode(bytes)) as unknown;
  } catch {
    throw new JsonBodyError(400, "BAD_REQUEST", "Invalid JSON body");
  }
}

/**
 * Converts a JsonBodyError into a JSON API error response.
 *
 * Routes through `apiError` so the typed error-response contract stays the
 * single source of truth for the response body shape.
 *
 * @param error - The bounded JSON reader error.
 * @returns A NextResponse with the encoded error body.
 */
export function jsonBodyErrorResponse(
  error: JsonBodyError
): NextResponse<ErrorResponseBody> {
  return apiError(error.status, error.code, error.message);
}
