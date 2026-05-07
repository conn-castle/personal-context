import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

const { mockAuthGet, mockAuthPost } = vi.hoisted(() => ({
  mockAuthGet: vi.fn(),
  mockAuthPost: vi.fn(),
}));

vi.mock("@/lib/auth", () => ({
  handlers: {
    GET: mockAuthGet,
    POST: mockAuthPost,
  },
}));

import { GET, POST } from "@/app/api/auth/[...nextauth]/route";

describe("api auth route", () => {
  const originalLocalBackendURL = process.env.LOCAL_BACKEND_URL;

  beforeEach(() => {
    delete process.env.LOCAL_BACKEND_URL;
    mockAuthGet.mockReset();
    mockAuthPost.mockReset();
    mockAuthGet.mockResolvedValue(new Response("ok", { status: 200 }));
    mockAuthPost.mockResolvedValue(new Response("ok", { status: 200 }));
  });

  afterAll(() => {
    if (originalLocalBackendURL === undefined) {
      delete process.env.LOCAL_BACKEND_URL;
      return;
    }
    process.env.LOCAL_BACKEND_URL = originalLocalBackendURL;
  });

  it("returns 403 in local mode", async () => {
    process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";

    const req = new NextRequest("http://localhost/api/auth/signin");
    const res = await GET(req);

    expect(res.status).toBe(403);
    expect(mockAuthGet).not.toHaveBeenCalled();
    await expect(res.json()).resolves.toEqual({
      error: "Authentication is unavailable in local mode.",
      code: "LOCAL_MODE_AUTH_DISABLED",
    });
  });

  it("returns 403 for POST in local mode", async () => {
    process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";

    const req = new NextRequest("http://localhost/api/auth/callback/credentials", {
      method: "POST",
    });
    const res = await POST(req);

    expect(res.status).toBe(403);
    expect(mockAuthPost).not.toHaveBeenCalled();
  });

  it("returns 500 for invalid LOCAL_BACKEND_URL", async () => {
    process.env.LOCAL_BACKEND_URL = "https://example.com";

    const req = new NextRequest("http://localhost/api/auth/signin");
    const res = await GET(req);

    expect(res.status).toBe(500);
    await expect(res.json()).resolves.toEqual({
      error: "Invalid LOCAL_BACKEND_URL configuration",
      code: "INVALID_CONFIG",
    });
    expect(mockAuthGet).not.toHaveBeenCalled();
  });

  it("returns 500 for POST with invalid LOCAL_BACKEND_URL", async () => {
    process.env.LOCAL_BACKEND_URL = "https://example.com";

    const req = new NextRequest("http://localhost/api/auth/callback/credentials", {
      method: "POST",
    });
    const res = await POST(req);

    expect(res.status).toBe(500);
    expect(mockAuthPost).not.toHaveBeenCalled();
  });

  it("delegates GET when local mode is disabled", async () => {
    const req = new NextRequest("http://localhost/api/auth/signin");
    const res = await GET(req);

    expect(res.status).toBe(200);
    expect(mockAuthGet).toHaveBeenCalledTimes(1);
  });

  it("delegates POST when local mode is disabled", async () => {
    const req = new NextRequest("http://localhost/api/auth/callback/credentials", {
      method: "POST",
    });
    const res = await POST(req);

    expect(res.status).toBe(200);
    expect(mockAuthPost).toHaveBeenCalledTimes(1);
  });
});
