import { describe, expect, it, vi, beforeEach } from "vitest";
import crypto from "crypto";

const { mockQuery, mockAuth } = vi.hoisted(() => ({
  mockQuery: vi.fn(),
  mockAuth: vi.fn(),
}));

vi.mock("@/lib/db-pool", () => ({
  getPool: () => ({ query: mockQuery }),
}));

vi.mock("@/lib/auth", () => ({
  auth: mockAuth,
}));

import { requireSessionUser, requireUser } from "@/lib/auth-helpers";

describe("requireUser", () => {
  beforeEach(() => {
    mockQuery.mockReset();
    mockAuth.mockReset();
  });

  it("returns user for valid API key", async () => {
    const rawKey = "pc_key_test-key";
    const keyHash = crypto.createHash("sha256").update(rawKey).digest("hex");

    // SELECT validation (succeeds)
    mockQuery.mockResolvedValueOnce({
      rows: [{ user_id: "user-123", email: "user@example.com" }],
    });
    // UPDATE last_used_at (fire-and-forget)
    mockQuery.mockResolvedValueOnce({ rows: [] });

    const req = new Request("http://localhost/api/test", {
      headers: { authorization: `Bearer ${rawKey}` },
    });

    const result = await requireUser(req);

    expect(result).toEqual({ id: "user-123", email: "user@example.com" });
    expect(mockAuth).not.toHaveBeenCalled();
    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining("key_hash"),
      [keyHash],
    );
  });

  it("returns 401 for invalid API key", async () => {
    mockQuery.mockResolvedValueOnce({ rows: [] });

    const req = new Request("http://localhost/api/test", {
      headers: { authorization: "Bearer bad-key" },
    });

    const result = await requireUser(req);

    expect("status" in result).toBe(true);
    if ("status" in result) {
      expect(result.status).toBe(401);
      const body = await result.json();
      expect(body.code).toBe("UNAUTHORIZED");
    }
  });

  it("accepts lowercase bearer scheme", async () => {
    const rawKey = "pc_key_lowercase";
    mockQuery.mockResolvedValueOnce({
      rows: [{ user_id: "user-321", email: "user@example.com" }],
    });
    mockQuery.mockResolvedValueOnce({ rows: [] });

    const req = new Request("http://localhost/api/test", {
      headers: { authorization: `bearer ${rawKey}` },
    });

    const result = await requireUser(req);

    expect(result).toEqual({ id: "user-321", email: "user@example.com" });
  });

  it("accepts bearer headers with surrounding whitespace", async () => {
    const rawKey = "pc_key_padded";
    mockQuery.mockResolvedValueOnce({
      rows: [{ user_id: "user-654", email: "user@example.com" }],
    });
    mockQuery.mockResolvedValueOnce({ rows: [] });

    const req = new Request("http://localhost/api/test", {
      headers: { authorization: `   Bearer    ${rawKey}   ` },
    });

    const result = await requireUser(req);

    expect(result).toEqual({ id: "user-654", email: "user@example.com" });
  });

  it("returns user for valid session", async () => {
    mockAuth.mockResolvedValueOnce({
      user: { id: "session-user-id", email: "session@example.com" },
    });

    const req = new Request("http://localhost/api/test");

    const result = await requireUser(req);

    expect(result).toEqual({
      id: "session-user-id",
      email: "session@example.com",
    });
  });

  it("returns 401 when no auth is provided", async () => {
    mockAuth.mockResolvedValueOnce(null);

    const req = new Request("http://localhost/api/test");

    const result = await requireUser(req);

    expect("status" in result).toBe(true);
    if ("status" in result) {
      expect(result.status).toBe(401);
      const body = await result.json();
      expect(body.code).toBe("UNAUTHORIZED");
    }
  });

  it("returns 401 when session has no user id", async () => {
    mockAuth.mockResolvedValueOnce({ user: { email: "no-id@example.com" } });

    const req = new Request("http://localhost/api/test");

    const result = await requireUser(req);

    expect("status" in result).toBe(true);
    if ("status" in result) {
      expect(result.status).toBe(401);
    }
  });

  it("updates last_used_at on successful API key validation", async () => {
    const rawKey = "pc_key_last-used-test";
    const keyHash = crypto.createHash("sha256").update(rawKey).digest("hex");

    // First query: SELECT validation (succeeds)
    mockQuery.mockResolvedValueOnce({
      rows: [{ user_id: "user-456", email: "test@test.com" }],
    });
    // Second query: UPDATE last_used_at (fire-and-forget)
    mockQuery.mockResolvedValueOnce({ rows: [] });

    const req = new Request("http://localhost/api/test", {
      headers: { authorization: `Bearer ${rawKey}` },
    });

    await requireUser(req);

    // Wait for the fire-and-forget update
    await new Promise((resolve) => setTimeout(resolve, 10));

    expect(mockQuery).toHaveBeenCalledTimes(2);
    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining("last_used_at"),
      [keyHash],
    );
  });

  it("ignores last_used_at update failure", async () => {
    const rawKey = "pc_key_fail-update";

    mockQuery.mockResolvedValueOnce({
      rows: [{ user_id: "user-789", email: "test@test.com" }],
    });
    // Fire-and-forget update fails
    mockQuery.mockRejectedValueOnce(new Error("update failed"));

    const req = new Request("http://localhost/api/test", {
      headers: { authorization: `Bearer ${rawKey}` },
    });

    const result = await requireUser(req);

    // Should still return the user
    expect(result).toEqual({ id: "user-789", email: "test@test.com" });
  });
});

describe("requireSessionUser", () => {
  beforeEach(() => {
    mockAuth.mockReset();
  });

  it("returns user for valid session", async () => {
    mockAuth.mockResolvedValueOnce({
      user: { id: "session-user-id", email: "session@example.com" },
    });

    const result = await requireSessionUser();

    expect(result).toEqual({
      id: "session-user-id",
      email: "session@example.com",
    });
  });

  it("returns 401 when no session exists", async () => {
    mockAuth.mockResolvedValueOnce(null);

    const result = await requireSessionUser();

    expect("status" in result).toBe(true);
    if ("status" in result) {
      expect(result.status).toBe(401);
      const body = await result.json();
      expect(body.code).toBe("UNAUTHORIZED");
    }
  });

  it("returns 401 when session email is missing", async () => {
    mockAuth.mockResolvedValueOnce({ user: { id: "session-user-id" } });

    const result = await requireSessionUser();

    expect("status" in result).toBe(true);
    if ("status" in result) {
      expect(result.status).toBe(401);
    }
  });
});
