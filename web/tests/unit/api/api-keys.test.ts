import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest, NextResponse } from "next/server";

const { mockIsLocalMode, mockProxyToLocal, mockQuery, mockRequireSessionUser } = vi.hoisted(() => ({
  mockIsLocalMode: vi.fn(),
  mockProxyToLocal: vi.fn(),
  mockQuery: vi.fn(),
  mockRequireSessionUser: vi.fn(),
}));

vi.mock("@/lib/db-pool", () => ({
  getPool: () => ({ query: mockQuery }),
}));

vi.mock("@/lib/auth-helpers", () => ({
  requireSessionUser: mockRequireSessionUser,
}));

vi.mock("@/lib/local-proxy", () => ({
  isLocalMode: mockIsLocalMode,
  proxyToLocal: mockProxyToLocal,
}));

import { GET, POST } from "@/app/api/api-keys/route";

describe("GET /api/api-keys", () => {
  beforeEach(() => {
    mockQuery.mockReset();
    mockIsLocalMode.mockReset();
    mockProxyToLocal.mockReset();
    mockIsLocalMode.mockReturnValue(false);
    mockRequireSessionUser.mockReset();
    mockRequireSessionUser.mockResolvedValue({
      id: "test-user-id",
      email: "test@test.com",
    });
  });

  it("lists API keys for the authenticated user", async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [
        {
          id: "key-1",
          label: "My laptop",
          created_at: new Date("2026-01-01"),
          last_used_at: new Date("2026-01-15"),
          revoked_at: null,
        },
        {
          id: "key-2",
          label: "CI server",
          created_at: new Date("2026-02-01"),
          last_used_at: null,
          revoked_at: new Date("2026-02-10"),
        },
      ],
    });

    const req = new NextRequest("http://localhost/api/api-keys");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.keys).toHaveLength(2);
    expect(body.keys[0].label).toBe("My laptop");
    expect(body.keys[0].last_used_at).toBe("2026-01-15T00:00:00.000Z");
    expect(body.keys[1].revoked_at).toBe("2026-02-10T00:00:00.000Z");
    // Never exposes the raw key
    expect(body.keys[0]).not.toHaveProperty("key_hash");
    expect(body.keys[0]).not.toHaveProperty("raw_key");
    // Verify query is scoped to the authenticated user
    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining("user_id"),
      ["test-user-id"],
    );
  });

  it("proxies GET in local mode", async () => {
    const proxied = new Response("proxied", { status: 202 });
    mockIsLocalMode.mockReturnValueOnce(true);
    mockProxyToLocal.mockResolvedValueOnce(proxied);

    const req = new NextRequest("http://localhost/api/api-keys");
    const res = await GET(req);

    expect(res).toBe(proxied);
    expect(mockRequireSessionUser).not.toHaveBeenCalled();
    expect(mockQuery).not.toHaveBeenCalled();
  });

  it("returns empty array when no keys exist", async () => {
    mockQuery.mockResolvedValueOnce({ rows: [] });

    const req = new NextRequest("http://localhost/api/api-keys");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.keys).toHaveLength(0);
  });

  it("returns 500 on database error", async () => {
    mockQuery.mockRejectedValueOnce(new Error("connection refused"));

    const req = new NextRequest("http://localhost/api/api-keys");
    const res = await GET(req);

    expect(res.status).toBe(500);
  });

  it("returns auth error when no session is present", async () => {
    mockRequireSessionUser.mockResolvedValueOnce(
      NextResponse.json(
        { error: "Unauthorized", code: "UNAUTHORIZED" },
        { status: 401 },
      ),
    );

    const req = new NextRequest("http://localhost/api/api-keys");
    const res = await GET(req);

    expect(res.status).toBe(401);
    expect(mockQuery).not.toHaveBeenCalled();
  });
});

describe("POST /api/api-keys", () => {
  beforeEach(() => {
    mockQuery.mockReset();
    mockIsLocalMode.mockReset();
    mockProxyToLocal.mockReset();
    mockIsLocalMode.mockReturnValue(false);
    mockRequireSessionUser.mockReset();
    mockRequireSessionUser.mockResolvedValue({
      id: "test-user-id",
      email: "test@test.com",
    });
  });

  it("creates a new API key and returns the raw key once", async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [
        {
          id: "new-key-id",
          label: "My laptop",
          created_at: new Date("2026-01-01"),
        },
      ],
    });

    const req = new NextRequest("http://localhost/api/api-keys", {
      method: "POST",
      body: JSON.stringify({ label: "My laptop" }),
      headers: { "content-type": "application/json" },
    });

    const res = await POST(req);

    expect(res.status).toBe(201);
    const body = await res.json();
    expect(body.id).toBe("new-key-id");
    expect(body.label).toBe("My laptop");
    expect(body.raw_key).toMatch(/^pc_key_/);
    expect(body.created_at).toBe("2026-01-01T00:00:00.000Z");
    // Verify INSERT includes user_id
    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining("INSERT"),
      expect.arrayContaining(["test-user-id"]),
    );
  });

  it("proxies POST in local mode", async () => {
    const proxied = new Response("proxied", { status: 202 });
    mockIsLocalMode.mockReturnValueOnce(true);
    mockProxyToLocal.mockResolvedValueOnce(proxied);

    const req = new NextRequest("http://localhost/api/api-keys", {
      method: "POST",
      body: JSON.stringify({ label: "test" }),
      headers: { "content-type": "application/json" },
    });
    const res = await POST(req);

    expect(res).toBe(proxied);
    expect(mockRequireSessionUser).not.toHaveBeenCalled();
    expect(mockQuery).not.toHaveBeenCalled();
  });

  it("returns 400 for missing label", async () => {
    const req = new NextRequest("http://localhost/api/api-keys", {
      method: "POST",
      body: JSON.stringify({}),
      headers: { "content-type": "application/json" },
    });

    const res = await POST(req);

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error).toContain("label");
  });

  it("returns 400 for empty label", async () => {
    const req = new NextRequest("http://localhost/api/api-keys", {
      method: "POST",
      body: JSON.stringify({ label: "   " }),
      headers: { "content-type": "application/json" },
    });

    const res = await POST(req);

    expect(res.status).toBe(400);
  });

  it("returns 400 for invalid JSON body", async () => {
    const req = new NextRequest("http://localhost/api/api-keys", {
      method: "POST",
      body: "not json",
      headers: { "content-type": "application/json" },
    });

    const res = await POST(req);

    expect(res.status).toBe(400);
  });

  it("returns 400 for non-object body", async () => {
    const req = new NextRequest("http://localhost/api/api-keys", {
      method: "POST",
      body: JSON.stringify([1, 2, 3]),
      headers: { "content-type": "application/json" },
    });

    const res = await POST(req);

    expect(res.status).toBe(400);
  });

  it("returns 500 on database error", async () => {
    mockQuery.mockRejectedValueOnce(new Error("connection refused"));

    const req = new NextRequest("http://localhost/api/api-keys", {
      method: "POST",
      body: JSON.stringify({ label: "test" }),
      headers: { "content-type": "application/json" },
    });

    const res = await POST(req);

    expect(res.status).toBe(500);
  });

  it("returns auth error when no session is present", async () => {
    mockRequireSessionUser.mockResolvedValueOnce(
      NextResponse.json(
        { error: "Unauthorized", code: "UNAUTHORIZED" },
        { status: 401 },
      ),
    );

    const req = new NextRequest("http://localhost/api/api-keys", {
      method: "POST",
      body: JSON.stringify({ label: "test" }),
      headers: { "content-type": "application/json" },
    });
    const res = await POST(req);

    expect(res.status).toBe(401);
    expect(mockQuery).not.toHaveBeenCalled();
  });
});
