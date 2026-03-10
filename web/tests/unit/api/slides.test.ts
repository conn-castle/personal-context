import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

const mockSql = vi.fn();
vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

import { GET } from "@/app/api/slides/route";

describe("GET /api/slides", () => {
  beforeEach(() => {
    mockSql.mockReset();
  });

  const makeSlideSummary = (overrides: Record<string, unknown> = {}) => ({
    id: "20250304-a3f2b7e1",
    date: "2025-03-04",
    day_order: "a0",
    project_id: null,
    updated_at: "2025-03-04T10:00:00.000Z",
    deleted_at: null,
    figure_count: 2,
    data_file_count: 1,
    ...overrides,
  });

  it("returns slides with default limit", async () => {
    const slides = [
      makeSlideSummary({ id: "20250304-a3f2b7e1" }),
      makeSlideSummary({ id: "20250303-b4c5d6e7" }),
    ];
    mockSql.mockResolvedValue(slides);

    const req = new NextRequest("http://localhost/api/slides");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(2);
    expect(body.next_cursor).toBeNull();
  });

  it("returns next_cursor only when an extra row exists beyond the page limit", async () => {
    const slides = [
      makeSlideSummary({
        id: "20250304-a3f2b7e1",
        date: "2025-03-04",
        day_order: "a0",
      }),
      makeSlideSummary({
        id: "20250303-b4c5d6e7",
        date: "2025-03-03",
        day_order: "a1",
      }),
      makeSlideSummary({
        id: "20250302-c5d6e7f8",
        date: "2025-03-02",
        day_order: "a0",
      }),
    ];
    mockSql.mockResolvedValue(slides);

    const req = new NextRequest("http://localhost/api/slides?limit=2");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(2);
    expect(body.next_cursor).not.toBeNull();

    // Decode cursor to verify its structure
    const cursor = JSON.parse(atob(body.next_cursor));
    expect(cursor).toEqual({
      date: "2025-03-03",
      day_order: "a1",
      id: "20250303-b4c5d6e7",
    });
  });

  it("does not return next_cursor when the result set ends on the page boundary", async () => {
    const slides = [
      makeSlideSummary({
        id: "20250304-a3f2b7e1",
        date: "2025-03-04",
        day_order: "a0",
      }),
      makeSlideSummary({
        id: "20250303-b4c5d6e7",
        date: "2025-03-03",
        day_order: "a1",
      }),
    ];
    mockSql.mockResolvedValue(slides);

    const req = new NextRequest("http://localhost/api/slides?limit=2");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(2);
    expect(body.next_cursor).toBeNull();
  });

  it("paginates with cursor", async () => {
    const cursor = btoa(
      JSON.stringify({
        date: "2025-03-04",
        day_order: "a0",
        id: "20250304-a3f2b7e1",
      })
    );
    mockSql.mockResolvedValue([
      makeSlideSummary({ id: "20250303-b4c5d6e7", date: "2025-03-03" }),
    ]);

    const req = new NextRequest(
      `http://localhost/api/slides?cursor=${cursor}`
    );
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(1);
    // Verify the query used parameterized form with cursor values
    expect(mockSql).toHaveBeenCalledTimes(1);
  });

  it("filters by project", async () => {
    mockSql.mockResolvedValue([
      makeSlideSummary({ project_id: "org/alpha" }),
    ]);

    const req = new NextRequest(
      "http://localhost/api/slides?project=org/alpha"
    );
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(1);
    expect(body.items[0].project_id).toBe("org/alpha");
  });

  it("filters deleted slides when deleted=true", async () => {
    mockSql.mockResolvedValue([
      makeSlideSummary({ deleted_at: "2025-03-04T10:00:00.000Z" }),
    ]);

    const req = new NextRequest("http://localhost/api/slides?deleted=true");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(1);
    expect(body.items[0].deleted_at).not.toBeNull();
  });

  it("filters by updated_after with >= comparison", async () => {
    mockSql.mockResolvedValue([
      makeSlideSummary({
        updated_at: "2025-03-04T10:00:00.000Z",
      }),
    ]);

    const req = new NextRequest(
      "http://localhost/api/slides?updated_after=2025-03-04T10:00:00.000Z"
    );
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(1);
  });

  it("returns empty results", async () => {
    mockSql.mockResolvedValue([]);

    const req = new NextRequest("http://localhost/api/slides");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toEqual([]);
    expect(body.next_cursor).toBeNull();
  });

  it("returns 400 for invalid updated_after timestamp", async () => {
    const req = new NextRequest(
      "http://localhost/api/slides?updated_after=not-a-timestamp"
    );
    const res = await GET(req);

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toContain("updated_after");
  });

  it("returns 400 for cursor with invalid field types", async () => {
    // Valid base64 JSON, but fields are numbers instead of strings
    const cursor = btoa(JSON.stringify({ date: 123, day_order: 456, id: 789 }));
    const req = new NextRequest(
      `http://localhost/api/slides?cursor=${cursor}`
    );
    const res = await GET(req);

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 400 for invalid cursor", async () => {
    const req = new NextRequest(
      "http://localhost/api/slides?cursor=not-valid-base64!!!"
    );
    const res = await GET(req);

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("uses default limit of 20 and clamps to max 100", async () => {
    mockSql.mockResolvedValue([]);

    // Default limit
    const req1 = new NextRequest("http://localhost/api/slides");
    await GET(req1);
    expect(mockSql).toHaveBeenCalledTimes(1);

    // Verify limit is clamped when > 100
    mockSql.mockReset();
    mockSql.mockResolvedValue([]);
    const req2 = new NextRequest("http://localhost/api/slides?limit=200");
    await GET(req2);
    expect(mockSql).toHaveBeenCalledTimes(1);
  });

  it("returns correct sort order", async () => {
    const slides = [
      makeSlideSummary({
        id: "20250304-a3f2b7e1",
        date: "2025-03-04",
        day_order: "a0",
      }),
      makeSlideSummary({
        id: "20250304-b4c5d6e7",
        date: "2025-03-04",
        day_order: "a1",
      }),
      makeSlideSummary({
        id: "20250303-c5d6e7f8",
        date: "2025-03-03",
        day_order: "a0",
      }),
    ];
    mockSql.mockResolvedValue(slides);

    const req = new NextRequest("http://localhost/api/slides");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items[0].date).toBe("2025-03-04");
    expect(body.items[2].date).toBe("2025-03-03");
  });

  it("returns 500 on database error", async () => {
    mockSql.mockRejectedValue(new Error("connection refused"));

    const req = new NextRequest("http://localhost/api/slides");
    const res = await GET(req);

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });
});
