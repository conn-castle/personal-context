import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest, NextResponse } from "next/server";

const {
  mockIsLocalMode,
  mockProxyToLocal,
  mockQuery,
  mockRequireSessionUser,
} = vi.hoisted(() => ({
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

import { DELETE } from "@/app/api/api-keys/[id]/route";

describe("DELETE /api/api-keys/[id]", () => {
  beforeEach(() => {
    mockQuery.mockReset();
    mockRequireSessionUser.mockReset();
    mockRequireSessionUser.mockResolvedValue({
      id: "test-user-id",
      email: "test@test.com",
    });
    mockIsLocalMode.mockReset();
    mockIsLocalMode.mockReturnValue(false);
    mockProxyToLocal.mockReset();
  });

  function makeContext(id: string) {
    return { params: Promise.resolve({ id }) };
  }

  it("proxies to the local backend in local mode", async () => {
    const proxied = new Response("proxied", { status: 202 });
    mockIsLocalMode.mockReturnValueOnce(true);
    mockProxyToLocal.mockResolvedValueOnce(proxied);

    const req = new NextRequest(
      "http://localhost/api/api-keys/key-id-123",
      { method: "DELETE" },
    );
    const res = await DELETE(
      req,
      makeContext("a1b2c3d4-e5f6-7890-abcd-ef1234567890"),
    );

    expect(res).toBe(proxied);
    expect(mockProxyToLocal).toHaveBeenCalledWith(req);
    expect(mockRequireSessionUser).not.toHaveBeenCalled();
    expect(mockQuery).not.toHaveBeenCalled();
  });

  it("revokes an API key", async () => {
    const revokedAt = new Date("2026-03-01");
    mockQuery.mockResolvedValueOnce({
      rows: [{ id: "key-id-123", revoked_at: revokedAt }],
    });

    const req = new NextRequest(
      "http://localhost/api/api-keys/key-id-123",
      { method: "DELETE" },
    );
    const res = await DELETE(
      req,
      makeContext("a1b2c3d4-e5f6-7890-abcd-ef1234567890"),
    );

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.id).toBe("key-id-123");
    expect(body.revoked_at).toBe("2026-03-01T00:00:00.000Z");
    // Verify UPDATE is scoped to the authenticated user
    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining("user_id"),
      expect.arrayContaining(["test-user-id"]),
    );
  });

  it("returns 404 when key not found or already revoked", async () => {
    mockQuery.mockResolvedValueOnce({ rows: [] });

    const req = new NextRequest(
      "http://localhost/api/api-keys/key-id-123",
      { method: "DELETE" },
    );
    const res = await DELETE(
      req,
      makeContext("a1b2c3d4-e5f6-7890-abcd-ef1234567890"),
    );

    expect(res.status).toBe(404);
  });

  it("returns 400 for invalid UUID", async () => {
    const req = new NextRequest(
      "http://localhost/api/api-keys/not-a-uuid",
      { method: "DELETE" },
    );
    const res = await DELETE(req, makeContext("not-a-uuid"));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error).toContain("Invalid API key ID");
  });

  it("returns 500 on database error", async () => {
    mockQuery.mockRejectedValueOnce(new Error("connection refused"));

    const req = new NextRequest(
      "http://localhost/api/api-keys/key-id-123",
      { method: "DELETE" },
    );
    const res = await DELETE(
      req,
      makeContext("a1b2c3d4-e5f6-7890-abcd-ef1234567890"),
    );

    expect(res.status).toBe(500);
  });

  it("returns auth error when no session is present", async () => {
    mockRequireSessionUser.mockResolvedValueOnce(
      NextResponse.json(
        { error: "Unauthorized", code: "UNAUTHORIZED" },
        { status: 401 },
      ),
    );

    const req = new NextRequest(
      "http://localhost/api/api-keys/key-id-123",
      { method: "DELETE" },
    );
    const res = await DELETE(
      req,
      makeContext("a1b2c3d4-e5f6-7890-abcd-ef1234567890"),
    );

    expect(res.status).toBe(401);
    expect(mockQuery).not.toHaveBeenCalled();
  });
});
