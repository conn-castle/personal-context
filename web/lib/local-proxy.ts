/**
 * Proxy helper for local development mode.
 *
 * When LOCAL_BACKEND_URL is set, API route handlers call proxyToLocal()
 * to forward the request to the Go server (pc serve) instead of using
 * Neon/S3 directly. When not set, behavior is unchanged.
 */
import { getLocalBackendURL, getLocalModeState } from "@/lib/local-mode";
import { localBackendUnavailable } from "@/lib/api-error";

/** Headers that must not be forwarded between hops (RFC 7230 §6.1). */
const HOP_BY_HOP = new Set([
  "connection",
  "keep-alive",
  "transfer-encoding",
  "te",
  "trailer",
  "upgrade",
]);

/** Sensitive end-user auth headers should not be forwarded to local proxy targets. */
const SENSITIVE_REQUEST_HEADERS = new Set(["authorization", "cookie"]);
const SENSITIVE_RESPONSE_HEADERS = new Set(["set-cookie"]);

/**
 * Copies request headers from `source`, skipping hop-by-hop and sensitive headers.
 *
 * @param source - The original Headers to copy from.
 * @returns A new Headers object with hop-by-hop headers removed.
 */
function forwardRequestHeaders(source: Headers): Headers {
  const headers = new Headers();
  source.forEach((value, key) => {
    const normalized = key.toLowerCase();
    if (!HOP_BY_HOP.has(normalized) && !SENSITIVE_REQUEST_HEADERS.has(normalized)) {
      headers.set(key, value);
    }
  });
  return headers;
}

/**
 * Copies response headers from `source`, skipping hop-by-hop and sensitive headers.
 *
 * @param source - The original response headers to copy from.
 * @returns A new Headers object safe to return from proxy.
 */
function forwardResponseHeaders(source: Headers): Headers {
  const headers = new Headers();
  source.forEach((value, key) => {
    const normalized = key.toLowerCase();
    if (!HOP_BY_HOP.has(normalized) && !SENSITIVE_RESPONSE_HEADERS.has(normalized)) {
      headers.set(key, value);
    }
  });
  return headers;
}

/**
 * Returns true if local backend mode is active.
 *
 * Returns false when LOCAL_BACKEND_URL is unset or misconfigured.
 * Config errors are surfaced by the middleware (getLocalModeState);
 * route handlers use this as a safe boolean check.
 *
 * @returns Whether LOCAL_BACKEND_URL is set and valid.
 */
export function isLocalMode(): boolean {
  return getLocalModeState().enabled;
}

/**
 * Proxies a Next.js API request to the local Go server.
 *
 * Forwards the HTTP method, headers, body, and query params to the Go
 * server at LOCAL_BACKEND_URL and returns the response as a Next.js Response.
 *
 * @param request - The incoming Next.js request.
 * @returns The proxied response from the Go server.
 */
export async function proxyToLocal(request: Request): Promise<Response> {
  const backendURL = getLocalBackendURL();
  if (!backendURL) {
    throw new Error("LOCAL_BACKEND_URL is not set");
  }

  const url = new URL(request.url);
  const targetUrl = new URL(
    `${url.pathname}${url.search}`,
    backendURL,
  ).toString();

  const headers = forwardRequestHeaders(request.headers);

  const init: RequestInit = {
    method: request.method,
    headers,
  };

  // Forward body for non-GET/HEAD requests
  if (request.method !== "GET" && request.method !== "HEAD") {
    try {
      const body = await request.text();
      if (body) {
        init.body = body;
      }
    } catch {
      // No body to forward
    }
  }

  let response: Response;
  try {
    response = await fetch(targetUrl, init);
  } catch {
    return localBackendUnavailable();
  }

  // Return the Go server's response directly (arrayBuffer is encoding-safe
  // for both text and binary payloads such as screenshots).
  const responseBody = await response.arrayBuffer();
  return new Response(responseBody, {
    status: response.status,
    headers: forwardResponseHeaders(response.headers),
  });
}
