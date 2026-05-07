import { getToken } from "next-auth/jwt";
import { NextRequest, NextResponse } from "next/server";
import { getLocalModeState } from "@/lib/local-mode";
import { extractBearerToken } from "@/lib/bearer-token";

const PUBLIC_API_ROUTES = new Set(["/api/register", "/api/info"]);

function isApiKeyManagementRoute(pathname: string): boolean {
  return pathname === "/api/api-keys" || pathname.startsWith("/api/api-keys/");
}

/**
 * Auth.js proxy that protects all routes except public ones.
 *
 * In local mode (LOCAL_BACKEND_URL set), auth is skipped entirely — the Go
 * server handles everything and there are no users.
 *
 * For API routes, returns 401 JSON instead of redirecting (for CLI/fetch).
 * For page routes, redirects unauthenticated users to /login.
 */
export default async function middleware(req: NextRequest) {
  const localMode = getLocalModeState();
  if (localMode.hasConfigError) {
    const { pathname } = req.nextUrl;
    if (pathname.startsWith("/api/")) {
      return NextResponse.json(
        { error: "Invalid LOCAL_BACKEND_URL configuration", code: "INVALID_CONFIG" },
        { status: 500 },
      );
    }

    return new NextResponse("Invalid LOCAL_BACKEND_URL configuration", {
      status: 500,
    });
  }

  if (localMode.enabled) {
    return NextResponse.next();
  }

  const { pathname } = req.nextUrl;
  const isAPI = pathname.startsWith("/api/");

  if (PUBLIC_API_ROUTES.has(pathname)) {
    return NextResponse.next();
  }

  const hasBearerToken = extractBearerToken(req.headers.get("authorization")) !== null;

  if (isAPI && hasBearerToken && !isApiKeyManagementRoute(pathname)) {
    return NextResponse.next();
  }

  const token = await getToken({ req, secret: process.env.AUTH_SECRET });
  const isLoggedIn =
    (typeof token?.userId === "string" && token.userId.length > 0) ||
    (typeof token?.sub === "string" && token.sub.length > 0);

  // API routes return 401 JSON for unauthenticated requests.
  if (isAPI && !isLoggedIn) {
    return NextResponse.json(
      { error: "Unauthorized", code: "UNAUTHORIZED" },
      { status: 401 },
    );
  }

  // Page routes redirect to /login.
  if (!isLoggedIn) {
    return NextResponse.redirect(new URL("/login", req.nextUrl));
  }

  return NextResponse.next();
}

export const config = {
  matcher: [
    /*
     * Match all routes except:
     * - /login, /register (auth pages)
     * - /api/auth/* (Auth.js routes — must be public for sign-in to work)
     * - /_next/* (Next.js internals)
     * - /favicon.ico, /icons/*, static assets
     */
    "/((?!login|register|api/auth|_next|favicon\\.ico|icons).*)",
  ],
};
