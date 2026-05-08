import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

const mockSql = vi.fn();
vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

vi.mock("@/lib/auth-helpers", () => ({
  requireUser: vi.fn().mockResolvedValue({ id: "test-user-id", email: "test@test.com" }),
}));

import { GET } from "@/app/api/stats/route";

describe("GET /api/stats", () => {
  beforeEach(() => {
    mockSql.mockReset();
  });

  it("returns counts for records, projects, and trashed", async () => {
    mockSql
      .mockResolvedValueOnce([{ count: 42 }])
      .mockResolvedValueOnce([{ count: 5 }])
      .mockResolvedValueOnce([{ count: 3 }]);

    const req = new NextRequest("http://localhost/api/stats");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.total_records).toBe(42);
    expect(body.total_projects).toBe(5);
    expect(body.trashed_records).toBe(3);
  });

  it("returns zeros when database is empty", async () => {
    mockSql
      .mockResolvedValueOnce([{ count: 0 }])
      .mockResolvedValueOnce([{ count: 0 }])
      .mockResolvedValueOnce([{ count: 0 }]);

    const req = new NextRequest("http://localhost/api/stats");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.total_records).toBe(0);
    expect(body.total_projects).toBe(0);
    expect(body.trashed_records).toBe(0);
  });

  it("returns 500 on database error", async () => {
    mockSql.mockRejectedValueOnce(new Error("connection refused"));

    const req = new NextRequest("http://localhost/api/stats");
    const res = await GET(req);

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });

  it("falls back to zero when query returns empty rows", async () => {
    mockSql
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([]);

    const req = new NextRequest("http://localhost/api/stats");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.total_records).toBe(0);
    expect(body.total_projects).toBe(0);
    expect(body.trashed_records).toBe(0);
  });
});
