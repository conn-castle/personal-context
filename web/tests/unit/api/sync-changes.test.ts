import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

const mockQuery = vi.fn();
const mockSql = Object.assign(vi.fn(), { query: mockQuery });
vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

import { GET } from "@/app/api/sync/changes/route";

describe("GET /api/sync/changes", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockQuery.mockReset();
  });

  it("returns changed slides since timestamp", async () => {
    const slideRow = {
      id: "20260301-aabbccdd",
      date: "2026-03-01",
      day_order: "a0",
      project_id: "org/proj",
      updated_at: "2026-03-01T14:00:00.000Z",
      deleted_at: null,
      figure_count: 2,
      data_file_count: 1,
    };

    // sql`` for server_now (called first), then sql.query() for slides
    mockSql.mockResolvedValueOnce([
      { server_now: "2026-03-09T10:00:00.000Z" },
    ]);
    mockQuery.mockResolvedValueOnce([slideRow]);

    const req = new NextRequest(
      "http://localhost/api/sync/changes?since=2026-03-01T00:00:00.000Z"
    );
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(1);
    expect(body.items[0].id).toBe("20260301-aabbccdd");
    expect(body.server_now).toBe("2026-03-09T10:00:00.000Z");
  });

  it("includes soft-deleted slides in results", async () => {
    const deletedSlide = {
      id: "20260215-11223344",
      date: "2026-02-15",
      day_order: "a0",
      project_id: null,
      updated_at: "2026-03-01T10:00:00.000Z",
      deleted_at: "2026-03-01T10:00:00.000Z",
      figure_count: 0,
      data_file_count: 0,
    };

    mockSql.mockResolvedValueOnce([
      { server_now: "2026-03-09T10:00:00.000Z" },
    ]);
    mockQuery.mockResolvedValueOnce([deletedSlide]);

    const req = new NextRequest(
      "http://localhost/api/sync/changes?since=2026-02-01T00:00:00.000Z"
    );
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(1);
    expect(body.items[0].deleted_at).toBe("2026-03-01T10:00:00.000Z");
  });

  it("returns empty items when no changes exist", async () => {
    mockSql.mockResolvedValueOnce([
      { server_now: "2026-03-09T10:00:00.000Z" },
    ]);
    mockQuery.mockResolvedValueOnce([]);

    const req = new NextRequest(
      "http://localhost/api/sync/changes?since=2026-03-09T00:00:00.000Z"
    );
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toEqual([]);
    expect(body.server_now).toBe("2026-03-09T10:00:00.000Z");
  });

  it("rejects request when since param is missing", async () => {
    const req = new NextRequest("http://localhost/api/sync/changes");
    const res = await GET(req);

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("rejects request when since param is invalid", async () => {
    const req = new NextRequest(
      "http://localhost/api/sync/changes?since=not-a-date"
    );
    const res = await GET(req);

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns server_now from Postgres", async () => {
    mockSql.mockResolvedValueOnce([
      { server_now: "2026-03-09T15:30:00.000Z" },
    ]);
    mockQuery.mockResolvedValueOnce([]);

    const req = new NextRequest(
      "http://localhost/api/sync/changes?since=2026-03-09T00:00:00.000Z"
    );
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.server_now).toBe("2026-03-09T15:30:00.000Z");
  });

  it("uses >= for timestamp comparison (Decision t7u8v9)", async () => {
    mockSql.mockResolvedValueOnce([
      { server_now: "2026-03-09T10:00:00.000Z" },
    ]);
    mockQuery.mockResolvedValueOnce([]);

    const req = new NextRequest(
      "http://localhost/api/sync/changes?since=2026-03-01T00:00:00.000Z"
    );
    await GET(req);

    // The slides query uses sql.query(queryString, params) — verify it uses >=
    const firstCall = mockQuery.mock.calls[0];
    expect(firstCall[0]).toContain(">=");
  });

  it("uses server_now as upper bound in items query", async () => {
    mockSql.mockResolvedValueOnce([
      { server_now: "2026-03-09T10:00:00.000Z" },
    ]);
    mockQuery.mockResolvedValueOnce([]);

    const req = new NextRequest(
      "http://localhost/api/sync/changes?since=2026-03-01T00:00:00.000Z"
    );
    await GET(req);

    // Verify the items query uses server_now as the upper bound ($2)
    const firstCall = mockQuery.mock.calls[0];
    expect(firstCall[0]).toContain("<=");
    expect(firstCall[1]).toEqual([
      "2026-03-01T00:00:00.000Z",
      "2026-03-09T10:00:00.000Z",
    ]);
  });

  it("returns 500 on unexpected database error", async () => {
    mockSql.mockRejectedValue(new Error("connection refused"));

    const req = new NextRequest(
      "http://localhost/api/sync/changes?since=2026-03-01T00:00:00.000Z"
    );
    const res = await GET(req);

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });
});
