import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { NextRequest } from "next/server";

const mockQuery = vi.fn();
vi.mock("@/lib/db-pool", () => ({
  getPool: () => ({ query: mockQuery }),
}));

const { mockHashPassword } = vi.hoisted(() => ({
  mockHashPassword: vi.fn().mockResolvedValue("hashed-password"),
}));

vi.mock("@/lib/password", () => ({
  hashPassword: mockHashPassword,
}));

import { POST } from "@/app/api/register/route";

describe("POST /api/register", () => {
  const originalEnv = process.env.REGISTRATION_ENABLED;
  const originalLocalBackendURL = process.env.LOCAL_BACKEND_URL;

  beforeEach(() => {
    mockQuery.mockReset();
    process.env.REGISTRATION_ENABLED = "true";
    delete process.env.LOCAL_BACKEND_URL;
  });

  afterEach(() => {
    process.env.REGISTRATION_ENABLED = originalEnv;
    process.env.LOCAL_BACKEND_URL = originalLocalBackendURL;
  });

  function makeReq(body: unknown): NextRequest {
    return new NextRequest("http://localhost/api/register", {
      method: "POST",
      body: JSON.stringify(body),
      headers: { "content-type": "application/json" },
    });
  }

  it("creates a user with valid inputs", async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [
        {
          id: "new-user-id",
          email: "new@example.com",
          name: "Test User",
          created_at: new Date("2026-01-01"),
        },
      ],
    });

    const res = await POST(
      makeReq({
        email: "  NEW@Example.com ",
        name: "Test User",
        password: "secure-password",
      }),
    );

    expect(res.status).toBe(201);
    const body = await res.json();
    expect(body.email).toBe("new@example.com");
    expect(body.name).toBe("Test User");
    expect(body).not.toHaveProperty("password_hash");
    // Verify password was hashed before insertion
    expect(mockHashPassword).toHaveBeenCalledWith("secure-password");
    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining("INSERT INTO users"),
      ["new@example.com", "Test User", "hashed-password"],
    );
  });

  it("returns 403 when registration is disabled", async () => {
    process.env.REGISTRATION_ENABLED = "false";

    const res = await POST(
      makeReq({ email: "a@b.com", password: "12345678" }),
    );

    expect(res.status).toBe(403);
    const body = await res.json();
    expect(body.code).toBe("REGISTRATION_DISABLED");
  });

  it("returns 403 when registration is not explicitly enabled", async () => {
    delete process.env.REGISTRATION_ENABLED;

    const res = await POST(
      makeReq({ email: "a@b.com", password: "12345678" }),
    );

    expect(res.status).toBe(403);
    const body = await res.json();
    expect(body.code).toBe("REGISTRATION_DISABLED");
  });

  it("returns 403 in local mode", async () => {
    process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";

    const res = await POST(
      makeReq({ email: "a@b.com", password: "12345678" }),
    );

    expect(res.status).toBe(403);
    const body = await res.json();
    expect(body.code).toBe("LOCAL_MODE_AUTH_DISABLED");
  });

  it("returns 500 for invalid LOCAL_BACKEND_URL configuration", async () => {
    process.env.LOCAL_BACKEND_URL = "https://example.com";

    const res = await POST(
      makeReq({ email: "a@b.com", password: "12345678" }),
    );

    expect(res.status).toBe(500);
    await expect(res.json()).resolves.toEqual({
      error: "Invalid LOCAL_BACKEND_URL configuration",
      code: "INVALID_CONFIG",
    });
  });

  it("returns 400 for invalid JSON body", async () => {
    const req = new NextRequest("http://localhost/api/register", {
      method: "POST",
      body: "not json",
      headers: { "content-type": "application/json" },
    });

    const res = await POST(req);

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 413 for over-limit JSON bodies", async () => {
    const req = new NextRequest("http://localhost/api/register", {
      method: "POST",
      body: JSON.stringify({ email: "a@b.com", password: "12345678" }),
      headers: {
        "content-type": "application/json",
        "content-length": String(4 * 1024 * 1024 + 1),
      },
    });

    const res = await POST(req);

    expect(res.status).toBe(413);
    const body = await res.json();
    expect(body.code).toBe("REQUEST_BODY_TOO_LARGE");
    expect(mockQuery).not.toHaveBeenCalled();
  });

  it.each([null, [], "string", 42])(
    "returns 400 when JSON body is not an object: %s",
    async (bodyValue) => {
      const res = await POST(makeReq(bodyValue));

      expect(res.status).toBe(400);
      const body = await res.json();
      expect(body.code).toBe("BAD_REQUEST");
      expect(mockQuery).not.toHaveBeenCalled();
    },
  );

  it("returns 400 for missing email", async () => {
    const res = await POST(makeReq({ password: "12345678" }));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error).toContain("email");
  });

  it("returns 400 for email without @", async () => {
    const res = await POST(
      makeReq({ email: "not-an-email", password: "12345678" }),
    );

    expect(res.status).toBe(400);
  });

  it("returns 400 for short password", async () => {
    const res = await POST(
      makeReq({ email: "a@b.com", password: "short" }),
    );

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error).toContain("8 characters");
  });

  it("returns 400 for missing password", async () => {
    const res = await POST(makeReq({ email: "a@b.com" }));

    expect(res.status).toBe(400);
  });

  it("returns 400 for non-string name", async () => {
    const res = await POST(
      makeReq({ email: "a@b.com", name: 123, password: "12345678" }),
    );

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error).toContain("Name");
    expect(mockQuery).not.toHaveBeenCalled();
  });

  it("returns 409 for duplicate email", async () => {
    mockQuery.mockRejectedValueOnce({
      code: "23505",
      constraint: "users_email_key",
    });

    const res = await POST(
      makeReq({ email: " Existing@Example.com ", password: "12345678" }),
    );

    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body.code).toBe("CONFLICT");
  });

  it("returns structured 500 for unexpected errors", async () => {
    mockQuery.mockRejectedValueOnce(new Error("db down"));

    const res = await POST(
      makeReq({ email: "x@example.com", password: "12345678" }),
    );

    expect(res.status).toBe(500);
    await expect(res.json()).resolves.toEqual({
      error: "Registration failed.",
      code: "INTERNAL_ERROR",
    });
  });

  it("returns structured 500 for non-object database rejections", async () => {
    mockQuery.mockRejectedValueOnce("db down");

    const res = await POST(
      makeReq({ email: "x@example.com", password: "12345678" }),
    );

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });
});
