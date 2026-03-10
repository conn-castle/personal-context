import { describe, expect, it, beforeEach, afterEach } from "vitest";
import { getDb, resetDb } from "@/lib/db";

describe("getDb", () => {
  const originalEnv = process.env;

  beforeEach(() => {
    process.env = { ...originalEnv };
    resetDb();
  });

  afterEach(() => {
    process.env = originalEnv;
    resetDb();
  });

  it("throws when DATABASE_URL is not set", () => {
    delete process.env.DATABASE_URL;
    expect(() => getDb()).toThrow(
      "DATABASE_URL environment variable is required"
    );
  });

  it("returns a query function when DATABASE_URL is set", () => {
    process.env.DATABASE_URL = "postgres://user:pass@host/db";
    const sql = getDb();
    expect(typeof sql).toBe("function");
  });

  it("returns the same instance on subsequent calls (singleton)", () => {
    process.env.DATABASE_URL = "postgres://user:pass@host/db";
    const sql1 = getDb();
    const sql2 = getDb();
    expect(sql1).toBe(sql2);
  });

  it("returns a fresh instance after resetDb()", () => {
    process.env.DATABASE_URL = "postgres://user:pass@host/db";
    const sql1 = getDb();
    resetDb();
    const sql2 = getDb();
    expect(sql1).not.toBe(sql2);
  });
});
