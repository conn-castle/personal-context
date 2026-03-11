/**
 * Proxy helper for local development mode.
 *
 * When LOCAL_BACKEND_URL is set, API route handlers call proxyToLocal()
 * to forward the request to the Go server (pc serve) instead of using
 * Neon/S3 directly. When not set, behavior is unchanged.
 */

/** Headers that must not be forwarded between hops (RFC 7230 §6.1). */
const HOP_BY_HOP = new Set([
  "connection",
  "keep-alive",
  "transfer-encoding",
  "te",
  "trailer",
  "upgrade",
]);

/**
 * Copies headers from `source`, skipping hop-by-hop headers.
 *
 * @param source - The original Headers to copy from.
 * @returns A new Headers object with hop-by-hop headers removed.
 */
function forwardHeaders(source: Headers): Headers {
  const headers = new Headers();
  source.forEach((value, key) => {
    if (!HOP_BY_HOP.has(key.toLowerCase())) {
      headers.set(key, value);
    }
  });
  return headers;
}

/**
 * Returns true if local backend mode is active.
 *
 * @returns Whether LOCAL_BACKEND_URL is set.
 */
export function isLocalMode(): boolean {
  return !!process.env.LOCAL_BACKEND_URL;
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
  const backendUrl = process.env.LOCAL_BACKEND_URL;
  if (!backendUrl) {
    throw new Error("LOCAL_BACKEND_URL is not set");
  }

  const url = new URL(request.url);
  const targetUrl = `${backendUrl}${url.pathname}${url.search}`;

  const headers = forwardHeaders(request.headers);

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
    return new Response(
      JSON.stringify({
        error: "Local backend unavailable",
        code: "LOCAL_BACKEND_UNAVAILABLE",
      }),
      {
        status: 502,
        headers: { "content-type": "application/json" },
      }
    );
  }

  // Return the Go server's response directly (arrayBuffer is encoding-safe
  // for both text and binary payloads such as screenshots).
  const responseBody = await response.arrayBuffer();
  return new Response(responseBody, {
    status: response.status,
    headers: forwardHeaders(response.headers),
  });
}
