import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

const mockSql = vi.fn();
vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

const mockBumpS3Version = vi.fn();
vi.mock("@/lib/s3", () => ({
  bumpS3Version: (...args: unknown[]) => mockBumpS3Version(...args),
  getS3Version: vi.fn(),
}));

vi.mock("fractional-indexing", () => ({
  generateKeyBetween: vi.fn(
    (a: string | null | undefined, b: string | null | undefined) => {
      // Simple deterministic mock: produce a value "between" a and b
      if (a === null || a === undefined) {
        if (b === null || b === undefined) return "a0";
        return "a" + String.fromCharCode(b.charCodeAt(1) - 1 || 0);
      }
      if (b === null || b === undefined) {
        return a + "V";
      }
      return a + "V";
    }
  ),
}));

import { PATCH } from "@/app/api/slides/[id]/order/route";

type RouteContext = { params: Promise<{ id: string }> };

function makeContext(id: string): RouteContext {
  return { params: Promise.resolve({ id }) };
}

describe("PATCH /api/slides/[id]/order", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockBumpS3Version.mockReset();
    mockBumpS3Version.mockResolvedValue(undefined);
  });

  const slideId = "20250304-a3f2b7e1";

  it("reorders to first position", async () => {
    // Read current slide to get date
    mockSql.mockResolvedValueOnce([
      { id: slideId, date: "2025-03-04", day_order: "a1" },
    ]);
    // Read siblings for that date (excludes the moving slide)
    mockSql.mockResolvedValueOnce([
      { id: "20250304-11111111", day_order: "a0" },
    ]);
    // UPDATE
    mockSql.mockResolvedValueOnce([
      {
        id: slideId,
        date: "2025-03-04",
        day_order: "a/",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version
    mockSql.mockResolvedValueOnce([{ version: 9 }]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.id).toBe(slideId);
    expect(body.sync_version).toBe(9);
  });

  it("reorders to last position", async () => {
    // Read current slide
    mockSql.mockResolvedValueOnce([
      { id: slideId, date: "2025-03-04", day_order: "a0" },
    ]);
    // Read siblings (excludes the moving slide)
    mockSql.mockResolvedValueOnce([
      { id: "20250304-22222222", day_order: "a1" },
    ]);
    // UPDATE
    mockSql.mockResolvedValueOnce([
      {
        id: slideId,
        date: "2025-03-04",
        day_order: "a1V",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version
    mockSql.mockResolvedValueOnce([{ version: 10 }]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "last" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.id).toBe(slideId);
  });

  it("reorders before a reference slide", async () => {
    const refId = "20250304-22222222";
    // Read current slide
    mockSql.mockResolvedValueOnce([
      { id: slideId, date: "2025-03-04", day_order: "a2" },
    ]);
    // Read siblings (excludes the moving slide)
    mockSql.mockResolvedValueOnce([
      { id: "20250304-11111111", day_order: "a0" },
      { id: refId, day_order: "a1" },
    ]);
    // UPDATE
    mockSql.mockResolvedValueOnce([
      {
        id: slideId,
        date: "2025-03-04",
        day_order: "a0V",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version
    mockSql.mockResolvedValueOnce([{ version: 11 }]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "before", reference_id: refId },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.id).toBe(slideId);
  });

  it("reorders after a reference slide", async () => {
    const refId = "20250304-11111111";
    // Read current slide
    mockSql.mockResolvedValueOnce([
      { id: slideId, date: "2025-03-04", day_order: "a0" },
    ]);
    // Read siblings (excludes the moving slide)
    mockSql.mockResolvedValueOnce([
      { id: refId, day_order: "a1" },
      { id: "20250304-33333333", day_order: "a2" },
    ]);
    // UPDATE
    mockSql.mockResolvedValueOnce([
      {
        id: slideId,
        date: "2025-03-04",
        day_order: "a1V",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version
    mockSql.mockResolvedValueOnce([{ version: 12 }]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "after", reference_id: refId },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.id).toBe(slideId);
  });

  it("changes date while reordering", async () => {
    // Read current slide (different date)
    mockSql.mockResolvedValueOnce([
      { id: slideId, date: "2025-03-03", day_order: "a0" },
    ]);
    // Read siblings for the NEW date
    mockSql.mockResolvedValueOnce([
      { id: "20250304-11111111", day_order: "a0" },
    ]);
    // UPDATE
    mockSql.mockResolvedValueOnce([
      {
        id: slideId,
        date: "2025-03-04",
        day_order: "a0V",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version
    mockSql.mockResolvedValueOnce([{ version: 13 }]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          date: "2025-03-04",
          position: { kind: "last" },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.date).toBe("2025-03-04");
  });

  it("returns 400 for invalid position kind", async () => {
    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "invalid" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toContain("position.kind");
  });

  it("returns 400 for before without reference_id", async () => {
    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "before" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toContain("reference_id");
  });

  it("returns 400 for invalid reference_id", async () => {
    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "before", reference_id: "bad-ref" },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 400 for malformed JSON bodies", async () => {
    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: "{bad-json",
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toBe("Invalid JSON body");
  });

  it("returns 400 when the JSON body is not an object", async () => {
    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify([]),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toBe("Request body must be a JSON object");
  });

  it("returns 400 for non-string reference_id", async () => {
    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "before", reference_id: 12345 },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toContain("reference_id");
  });

  it("returns 400 when reference_id equals the slide id", async () => {
    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "before", reference_id: slideId },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toContain("relative to itself");
  });

  it("returns 400 for invalid slide ID", async () => {
    const req = new NextRequest(
      "http://localhost/api/slides/bad-id/order",
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext("bad-id"));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("INVALID_ID");
  });

  it("returns 404 for nonexistent slide", async () => {
    mockSql.mockResolvedValueOnce([]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
  });

  it("returns 400 for invalid date format", async () => {
    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          date: "not-a-date",
          position: { kind: "first" },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 400 for missing position", async () => {
    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({}),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 404 when reference slide not found in siblings", async () => {
    const refId = "20250304-99999999";
    // Read current slide
    mockSql.mockResolvedValueOnce([
      { id: slideId, date: "2025-03-04", day_order: "a0" },
    ]);
    // Read siblings (excludes the moving slide) — reference not present
    mockSql.mockResolvedValueOnce([
      { id: "20250304-11111111", day_order: "a1" },
    ]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "before", reference_id: refId },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
  });

  it("returns 200 when S3 version bump fails after reorder commits", async () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    mockSql.mockResolvedValueOnce([
      { id: slideId, date: "2025-03-04", day_order: "a1" },
    ]);
    // Read siblings (excludes the moving slide)
    mockSql.mockResolvedValueOnce([
      { id: "20250304-11111111", day_order: "a0" },
    ]);
    mockSql.mockResolvedValueOnce([
      {
        id: slideId,
        date: "2025-03-04",
        day_order: "a/",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    mockSql.mockResolvedValueOnce([{ version: 14 }]);
    mockBumpS3Version.mockRejectedValueOnce(new Error("S3 unavailable"));

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.sync_version).toBe(14);

    errorSpy.mockRestore();
  });

  it("returns 500 on database error", async () => {
    mockSql.mockRejectedValueOnce(new Error("connection refused"));

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(slideId));

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });

});
