import { beforeEach, describe, expect, it, vi } from "vitest";

const { capturedAuthorize, capturedAuthConfig, mockQuery, mockVerifyPassword } = vi.hoisted(() => ({
  capturedAuthorize: { fn: undefined as ((credentials?: Record<string, unknown>) => Promise<unknown>) | undefined },
  capturedAuthConfig: { value: undefined as {
    callbacks?: {
      jwt?: (args: { token: Record<string, unknown>; user?: { id?: string } }) => Record<string, unknown>;
      session?: (args: {
        session: { user: Record<string, unknown> };
        token: Record<string, unknown>;
      }) => { user: Record<string, unknown> };
    };
  } | undefined },
  mockQuery: vi.fn(),
  mockVerifyPassword: vi.fn(),
}));

vi.mock("next-auth/providers/credentials", () => ({
  __esModule: true,
  default: (config: { authorize: (credentials?: Record<string, unknown>) => Promise<unknown> }) => {
    capturedAuthorize.fn = config.authorize;
    return config;
  },
}));

vi.mock("next-auth", () => ({
  __esModule: true,
  default: vi.fn((config) => {
    capturedAuthConfig.value = config;
    return {
      handlers: { GET: vi.fn(), POST: vi.fn() },
      auth: vi.fn(),
      signIn: vi.fn(),
      signOut: vi.fn(),
    };
  }),
}));

vi.mock("@/lib/db-pool", () => ({
  getPool: () => ({ query: mockQuery }),
}));

vi.mock("@/lib/password", () => ({
  verifyPassword: mockVerifyPassword,
}));

import "@/lib/auth";

describe("auth credentials provider", () => {
  beforeEach(() => {
    mockQuery.mockReset();
    mockVerifyPassword.mockReset();
  });

  it("canonicalizes credentials email before querying", async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [
        {
          id: "user-1",
          email: "user@example.com",
          name: "User",
          password_hash: "stored-hash",
        },
      ],
    });
    mockVerifyPassword.mockResolvedValueOnce(true);

    if (!capturedAuthorize.fn) {
      throw new Error("credentials authorize callback was not captured");
    }

    const result = await capturedAuthorize.fn({
      email: "  USER@Example.com ",
      password: "secret-password",
    });

    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining("WHERE email = $1"),
      ["user@example.com"],
    );
    expect(result).toEqual({
      id: "user-1",
      email: "user@example.com",
      name: "User",
    });
  });

  it("rejects invalid canonicalized email before database query", async () => {
    if (!capturedAuthorize.fn) {
      throw new Error("credentials authorize callback was not captured");
    }

    const result = await capturedAuthorize.fn({
      email: "   ",
      password: "secret-password",
    });

    expect(result).toBeNull();
    expect(mockQuery).not.toHaveBeenCalled();
  });

  it("returns null when user does not exist in database", async () => {
    mockQuery.mockResolvedValueOnce({ rows: [] });

    if (!capturedAuthorize.fn) {
      throw new Error("credentials authorize callback was not captured");
    }

    const result = await capturedAuthorize.fn({
      email: "nobody@example.com",
      password: "any-password",
    });

    expect(result).toBeNull();
    expect(mockQuery).toHaveBeenCalledWith(
      expect.stringContaining("WHERE email = $1"),
      ["nobody@example.com"],
    );
    expect(mockVerifyPassword).not.toHaveBeenCalled();
  });

  it("returns null when password is incorrect", async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [
        {
          id: "user-1",
          email: "user@example.com",
          name: "User",
          password_hash: "stored-hash",
        },
      ],
    });
    mockVerifyPassword.mockResolvedValueOnce(false);

    if (!capturedAuthorize.fn) {
      throw new Error("credentials authorize callback was not captured");
    }

    const result = await capturedAuthorize.fn({
      email: "user@example.com",
      password: "wrong-password",
    });

    expect(result).toBeNull();
    expect(mockVerifyPassword).toHaveBeenCalledWith("wrong-password", "stored-hash");
  });

  it("returns null name when database name is null", async () => {
    mockQuery.mockResolvedValueOnce({
      rows: [
        {
          id: "user-1",
          email: "user@example.com",
          name: null,
          password_hash: "stored-hash",
        },
      ],
    });
    mockVerifyPassword.mockResolvedValueOnce(true);

    if (!capturedAuthorize.fn) {
      throw new Error("credentials authorize callback was not captured");
    }

    const result = await capturedAuthorize.fn({
      email: "user@example.com",
      password: "secret-password",
    });

    expect(result).toEqual({
      id: "user-1",
      email: "user@example.com",
      name: null,
    });
  });

  it("rejects email values without @ after canonicalization", async () => {
    if (!capturedAuthorize.fn) {
      throw new Error("credentials authorize callback was not captured");
    }

    const result = await capturedAuthorize.fn({
      email: "not-an-email",
      password: "secret-password",
    });

    expect(result).toBeNull();
    expect(mockQuery).not.toHaveBeenCalled();
  });

  it("returns null when credential types are invalid", async () => {
    if (!capturedAuthorize.fn) {
      throw new Error("credentials authorize callback was not captured");
    }

    await expect(capturedAuthorize.fn()).resolves.toBeNull();
    await expect(
      capturedAuthorize.fn({ email: "user@example.com", password: 123 }),
    ).resolves.toBeNull();
  });

  it("sets userId in JWT and session callbacks", () => {
    const callbacks = capturedAuthConfig.value?.callbacks;
    if (!callbacks?.jwt || !callbacks.session) {
      throw new Error("auth callbacks were not captured");
    }

    const token = callbacks.jwt({ token: {}, user: { id: "user-1" } });
    expect(token.userId).toBe("user-1");
    expect(callbacks.jwt({ token })).toBe(token);

    const session = callbacks.session({
      session: { user: {} },
      token: { userId: "user-1" },
    });
    expect(session.user.id).toBe("user-1");
  });
});
