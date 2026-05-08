import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

const { mockGetToken } = vi.hoisted(() => ({
  mockGetToken: vi.fn(),
}));

vi.mock("next-auth/jwt", () => ({
  getToken: mockGetToken,
}));

import middleware from "@/middleware";

const originalLocalBackendURL = process.env.LOCAL_BACKEND_URL;

function makeRequest(
  url: string,
  init?: ConstructorParameters<typeof NextRequest>[1],
): NextRequest {
  return new NextRequest(url, init);
}

describe("middleware auth gate", () => {
  beforeEach(() => {
    delete process.env.LOCAL_BACKEND_URL;
    mockGetToken.mockReset();
    mockGetToken.mockResolvedValue(null);
  });

  afterAll(() => {
    if (originalLocalBackendURL === undefined) {
      delete process.env.LOCAL_BACKEND_URL;
      return;
    }
    process.env.LOCAL_BACKEND_URL = originalLocalBackendURL;
  });

  it("allows unauthenticated POST /api/register", async () => {
    const req = makeRequest("http://localhost/api/register", {
      method: "POST",
    });

    const res = await middleware(req);
    expect(res.status).toBe(200);
  });

  it("allows unauthenticated GET /api/info", async () => {
    const req = makeRequest("http://localhost/api/info");

    const res = await middleware(req);
    expect(res.status).toBe(200);
    expect(mockGetToken).not.toHaveBeenCalled();
  });

  it("allows unauthenticated API request with bearer token", async () => {
    const req = makeRequest("http://localhost/api/records", {
      headers: { authorization: "Bearer test-key" },
    });

    const res = await middleware(req);
    expect(res.status).toBe(200);
  });

  it("allows lowercase/padded bearer tokens", async () => {
    const req = makeRequest("http://localhost/api/records", {
      headers: { authorization: "   bearer   test-key   " },
    });

    const res = await middleware(req);
    expect(res.status).toBe(200);
  });

  it("requires session auth for API-key management endpoints", async () => {
    const req = makeRequest("http://localhost/api/api-keys", {
      headers: { authorization: "Bearer test-key" },
    });

    const res = await middleware(req);
    expect(res.status).toBe(401);
  });

  it("returns 401 JSON for unauthenticated protected API route", async () => {
    const req = makeRequest("http://localhost/api/records");

    const res = await middleware(req);
    expect(res.status).toBe(401);
    await expect(res.json()).resolves.toEqual({
      error: "Unauthorized",
      code: "UNAUTHORIZED",
    });
  });

  it("redirects unauthenticated protected page route to /login", async () => {
    const req = makeRequest("http://localhost/dashboard");

    const res = await middleware(req);
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("http://localhost/login");
  });

  it("allows authenticated protected routes", async () => {
    mockGetToken.mockResolvedValueOnce({ userId: "user-123" });
    const req = makeRequest("http://localhost/dashboard");

    const res = await middleware(req);
    expect(res.status).toBe(200);
  });

  it("skips auth in local mode", async () => {
    process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";
    const req = makeRequest("http://localhost/api/records");

    const res = await middleware(req);
    expect(res.status).toBe(200);
  });

  it("returns 500 for invalid local backend URL", async () => {
    process.env.LOCAL_BACKEND_URL = "https://example.com";
    const req = makeRequest("http://localhost/api/records");

    const res = await middleware(req);
    expect(res.status).toBe(500);
    await expect(res.json()).resolves.toEqual({
      error: "Invalid LOCAL_BACKEND_URL configuration",
      code: "INVALID_CONFIG",
    });
  });
});
