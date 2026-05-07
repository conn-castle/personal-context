import { afterEach, describe, expect, it, vi } from "vitest";

describe("db-pool", () => {
  const originalDatabaseURL = process.env.DATABASE_URL;

  afterEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    if (originalDatabaseURL === undefined) {
      delete process.env.DATABASE_URL;
      return;
    }
    process.env.DATABASE_URL = originalDatabaseURL;
  });

  it("throws when DATABASE_URL is missing", async () => {
    delete process.env.DATABASE_URL;

    const { getPool } = await import("@/lib/db-pool");

    expect(() => getPool()).toThrow("DATABASE_URL environment variable is required");
  });

  it("creates and reuses a pg pool", async () => {
    const end = vi.fn().mockResolvedValue(undefined);
    const Pool = vi.fn(function Pool(this: { end: typeof end }) {
      this.end = end;
    });
    vi.doMock("pg", () => ({ Pool }));
    process.env.DATABASE_URL = "postgres://user:pass@host/db";

    const { getPool, resetPool } = await import("@/lib/db-pool");
    const first = getPool();
    const second = getPool();

    expect(first).toBe(second);
    expect(Pool).toHaveBeenCalledTimes(1);
    expect(Pool).toHaveBeenCalledWith({
      connectionString: "postgres://user:pass@host/db",
      max: 5,
    });

    resetPool();
    expect(end).toHaveBeenCalledTimes(1);
  });

  it("resetPool is a no-op before a pool is created", async () => {
    const { resetPool } = await import("@/lib/db-pool");

    expect(() => resetPool()).not.toThrow();
  });
});
