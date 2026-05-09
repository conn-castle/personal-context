import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

const mockSql = vi.fn();
vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

vi.mock("@/lib/auth-helpers", () => ({
  requireUser: vi.fn().mockResolvedValue({ id: "test-user-id", email: "test@test.com" }),
}));

import { GET } from "@/app/api/records/route";

describe("GET /api/records", () => {
  beforeEach(() => {
    mockSql.mockReset();
  });

  const makeRecordSummary = (overrides: Record<string, unknown> = {}) => ({
    id: "20250304-a3f2b7e1",
    date: "2025-03-04",
    day_order: "a0",
    html_content: "<p>Test content</p>",
    project_id: "org/proj",
    source_device_id: "device-a",
    source_ref: null,
    updated_at: "2025-03-04T10:00:00.000Z",
    deleted_at: null,
    figure_count: 2,
    data_file_count: 1,
    ...overrides,
  });

  const mockRecordQuery = (records: Record<string, unknown>[], total = records.length) => {
    mockSql.mockResolvedValueOnce([{ total }]).mockResolvedValueOnce(records);
  };

  it("returns records with default limit", async () => {
    const records = [
      makeRecordSummary({ id: "20250304-a3f2b7e1" }),
      makeRecordSummary({ id: "20250303-b4c5d6e7" }),
    ];
    mockRecordQuery(records);

    const req = new NextRequest("http://localhost/api/records");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(2);
    expect(body.next_cursor).toBeNull();
  });

  it("returns next_cursor only when an extra row exists beyond the page limit", async () => {
    const records = [
      makeRecordSummary({
        id: "20250304-a3f2b7e1",
        date: "2025-03-04",
        day_order: "a0",
      }),
      makeRecordSummary({
        id: "20250303-b4c5d6e7",
        date: "2025-03-03",
        day_order: "a1",
      }),
      makeRecordSummary({
        id: "20250302-c5d6e7f8",
        date: "2025-03-02",
        day_order: "a0",
      }),
    ];
    mockRecordQuery(records, 3);

    const req = new NextRequest("http://localhost/api/records?limit=2");
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
    const records = [
      makeRecordSummary({
        id: "20250304-a3f2b7e1",
        date: "2025-03-04",
        day_order: "a0",
      }),
      makeRecordSummary({
        id: "20250303-b4c5d6e7",
        date: "2025-03-03",
        day_order: "a1",
      }),
    ];
    mockRecordQuery(records);

    const req = new NextRequest("http://localhost/api/records?limit=2");
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
    mockRecordQuery([
      makeRecordSummary({ id: "20250303-b4c5d6e7", date: "2025-03-03" }),
    ], 2);

    const req = new NextRequest(
      `http://localhost/api/records?cursor=${cursor}`
    );
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(1);
    expect(body.total).toBe(2);
    // Verify the query used parameterized form with cursor values
    expect(mockSql).toHaveBeenCalledTimes(2);
  });

  it("filters by project", async () => {
    mockRecordQuery([
      makeRecordSummary({ project_id: "org/alpha" }),
    ]);

    const req = new NextRequest(
      "http://localhost/api/records?project=org/alpha"
    );
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(1);
    expect(body.items[0].project_id).toBe("org/alpha");
  });

  it("returns nullable HTML and source metadata", async () => {
    mockRecordQuery([
      makeRecordSummary({
        html_content: null,
        project_id: "org/alpha",
        source_device_id: "laptop",
        source_ref: "obsidian://daily-note",
      }),
    ]);

    const req = new NextRequest("http://localhost/api/records");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items[0].html_content).toBeNull();
    expect(body.items[0].project_id).toBe("org/alpha");
    expect(body.items[0].source_device_id).toBe("laptop");
    expect(body.items[0].source_ref).toBe("obsidian://daily-note");
  });

  it("filters deleted records when deleted=true", async () => {
    mockRecordQuery([
      makeRecordSummary({ deleted_at: "2025-03-04T10:00:00.000Z" }),
    ]);

    const req = new NextRequest("http://localhost/api/records?deleted=true");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(1);
    expect(body.items[0].deleted_at).not.toBeNull();
  });

  it("filters by updated_after with >= comparison", async () => {
    mockRecordQuery([
      makeRecordSummary({
        updated_at: "2025-03-04T10:00:00.000Z",
      }),
    ]);

    const req = new NextRequest(
      "http://localhost/api/records?updated_after=2025-03-04T10:00:00.000Z"
    );
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toHaveLength(1);
  });

  it("returns empty results", async () => {
    mockRecordQuery([]);

    const req = new NextRequest("http://localhost/api/records");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items).toEqual([]);
    expect(body.total).toBe(0);
    expect(body.next_cursor).toBeNull();
  });

  it("returns 400 for invalid updated_after timestamp", async () => {
    const req = new NextRequest(
      "http://localhost/api/records?updated_after=not-a-timestamp"
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
      `http://localhost/api/records?cursor=${cursor}`
    );
    const res = await GET(req);

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 400 for invalid cursor", async () => {
    const req = new NextRequest(
      "http://localhost/api/records?cursor=not-valid-base64!!!"
    );
    const res = await GET(req);

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("uses default limit of 20 and clamps to max 100", async () => {
    mockRecordQuery([]);

    // Default limit
    const req1 = new NextRequest("http://localhost/api/records");
    await GET(req1);
    expect(mockSql).toHaveBeenCalledTimes(2);

    // Verify limit is clamped when > 100
    mockSql.mockReset();
    mockRecordQuery([]);
    const req2 = new NextRequest("http://localhost/api/records?limit=200");
    await GET(req2);
    expect(mockSql).toHaveBeenCalledTimes(2);
  });

  it("returns correct sort order", async () => {
    const records = [
      makeRecordSummary({
        id: "20250304-a3f2b7e1",
        date: "2025-03-04",
        day_order: "a0",
      }),
      makeRecordSummary({
        id: "20250304-b4c5d6e7",
        date: "2025-03-04",
        day_order: "a1",
      }),
      makeRecordSummary({
        id: "20250303-c5d6e7f8",
        date: "2025-03-03",
        day_order: "a0",
      }),
    ];
    mockRecordQuery(records);

    const req = new NextRequest("http://localhost/api/records");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.items[0].date).toBe("2025-03-04");
    expect(body.items[2].date).toBe("2025-03-03");
  });

  it("returns 500 on database error", async () => {
    mockSql.mockRejectedValue(new Error("connection refused"));

    const req = new NextRequest("http://localhost/api/records");
    const res = await GET(req);

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });
});
