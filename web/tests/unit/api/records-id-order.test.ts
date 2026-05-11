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

const { mockIsLocalMode, mockProxyToLocal } = vi.hoisted(() => ({
  mockIsLocalMode: vi.fn(),
  mockProxyToLocal: vi.fn(),
}));
vi.mock("@/lib/local-proxy", () => ({
  isLocalMode: mockIsLocalMode,
  proxyToLocal: mockProxyToLocal,
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

vi.mock("@/lib/auth-helpers", () => ({
  requireUser: vi.fn().mockResolvedValue({ id: "test-user-id", email: "test@test.com" }),
}));

import { PATCH } from "@/app/api/records/[id]/order/route";

type RouteContext = { params: Promise<{ id: string }> };

function makeContext(id: string): RouteContext {
  return { params: Promise.resolve({ id }) };
}

describe("PATCH /api/records/[id]/order", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockBumpS3Version.mockReset();
    mockBumpS3Version.mockResolvedValue(undefined);
    mockIsLocalMode.mockReset();
    mockIsLocalMode.mockReturnValue(false);
    mockProxyToLocal.mockReset();
  });

  const recordId = "20250304-a3f2b7e1";

  it("proxies PATCH in local mode", async () => {
    const proxied = new Response("proxied", { status: 202 });
    mockIsLocalMode.mockReturnValueOnce(true);
    mockProxyToLocal.mockResolvedValueOnce(proxied);

    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res).toBe(proxied);
    expect(mockSql).not.toHaveBeenCalled();
  });

  it("reorders to first position", async () => {
    // Read current record to get date
    mockSql.mockResolvedValueOnce([
      { id: recordId, date: "2025-03-04", day_order: "a1" },
    ]);
    // Read siblings for that date (excludes the moving record)
    mockSql.mockResolvedValueOnce([
      { id: "20250304-11111111", day_order: "a0" },
    ]);
    // UPDATE
    mockSql.mockResolvedValueOnce([
      {
        id: recordId,
        date: "2025-03-04",
        day_order: "a/",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version
    mockSql.mockResolvedValueOnce([{ version: 9, updated_at: "2025-03-04T12:00:00.000Z" }]);

    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.id).toBe(recordId);
    expect(body.sync_version).toBe(9);
  });

  it("reorders to last position", async () => {
    // Read current record
    mockSql.mockResolvedValueOnce([
      { id: recordId, date: "2025-03-04", day_order: "a0" },
    ]);
    // Read siblings (excludes the moving record)
    mockSql.mockResolvedValueOnce([
      { id: "20250304-22222222", day_order: "a1" },
    ]);
    // UPDATE
    mockSql.mockResolvedValueOnce([
      {
        id: recordId,
        date: "2025-03-04",
        day_order: "a1V",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version
    mockSql.mockResolvedValueOnce([{ version: 10, updated_at: "2025-03-04T12:00:00.000Z" }]);

    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "last" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.id).toBe(recordId);
  });

  it("reorders before a reference record", async () => {
    const refId = "20250304-22222222";
    // Read current record
    mockSql.mockResolvedValueOnce([
      { id: recordId, date: "2025-03-04", day_order: "a2" },
    ]);
    // Read siblings (excludes the moving record)
    mockSql.mockResolvedValueOnce([
      { id: "20250304-11111111", day_order: "a0" },
      { id: refId, day_order: "a1" },
    ]);
    // UPDATE
    mockSql.mockResolvedValueOnce([
      {
        id: recordId,
        date: "2025-03-04",
        day_order: "a0V",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version
    mockSql.mockResolvedValueOnce([{ version: 11, updated_at: "2025-03-04T12:00:00.000Z" }]);

    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "before", reference_id: refId },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.id).toBe(recordId);
  });

  it("reorders after a reference record", async () => {
    const refId = "20250304-11111111";
    // Read current record
    mockSql.mockResolvedValueOnce([
      { id: recordId, date: "2025-03-04", day_order: "a0" },
    ]);
    // Read siblings (excludes the moving record)
    mockSql.mockResolvedValueOnce([
      { id: refId, day_order: "a1" },
      { id: "20250304-33333333", day_order: "a2" },
    ]);
    // UPDATE
    mockSql.mockResolvedValueOnce([
      {
        id: recordId,
        date: "2025-03-04",
        day_order: "a1V",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version
    mockSql.mockResolvedValueOnce([{ version: 12, updated_at: "2025-03-04T12:00:00.000Z" }]);

    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "after", reference_id: refId },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.id).toBe(recordId);
  });

  it("changes date while reordering", async () => {
    // Read current record (different date)
    mockSql.mockResolvedValueOnce([
      { id: recordId, date: "2025-03-03", day_order: "a0" },
    ]);
    // Read siblings for the NEW date
    mockSql.mockResolvedValueOnce([
      { id: "20250304-11111111", day_order: "a0" },
    ]);
    // UPDATE
    mockSql.mockResolvedValueOnce([
      {
        id: recordId,
        date: "2025-03-04",
        day_order: "a0V",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version
    mockSql.mockResolvedValueOnce([{ version: 13, updated_at: "2025-03-04T12:00:00.000Z" }]);

    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          date: "2025-03-04",
          position: { kind: "last" },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.date).toBe("2025-03-04");
  });

  it("returns 400 for invalid position kind", async () => {
    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "invalid" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toContain("position.kind");
  });

  it("returns 400 for before without reference_id", async () => {
    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "before" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toContain("reference_id");
  });

  it("returns 400 for invalid reference_id", async () => {
    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "before", reference_id: "bad-ref" },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 400 for malformed JSON bodies", async () => {
    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: "{bad-json",
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toBe("Invalid JSON body");
  });

  it("returns 413 for over-limit JSON bodies", async () => {
    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: {
          "Content-Type": "application/json",
          "content-length": String(4 * 1024 * 1024 + 1),
        },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(413);
    const body = await res.json();
    expect(body.code).toBe("REQUEST_BODY_TOO_LARGE");
    expect(mockSql).not.toHaveBeenCalled();
  });

  it("returns 400 when the JSON body is not an object", async () => {
    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify([]),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toBe("Request body must be a JSON object");
  });

  it("returns 400 for non-string reference_id", async () => {
    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "before", reference_id: 12345 },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toContain("reference_id");
  });

  it("returns 400 when reference_id equals the record id", async () => {
    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "before", reference_id: recordId },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toContain("relative to itself");
  });

  it("returns 400 for invalid record ID", async () => {
    const req = new NextRequest(
      "http://localhost/api/records/bad-id/order",
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

  it("returns 404 for nonexistent record", async () => {
    mockSql.mockResolvedValueOnce([]);

    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
  });

  it("returns 400 for invalid date format", async () => {
    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          date: "not-a-date",
          position: { kind: "first" },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 400 for missing position", async () => {
    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({}),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 404 when reference record not found in siblings", async () => {
    const refId = "20250304-99999999";
    // Read current record
    mockSql.mockResolvedValueOnce([
      { id: recordId, date: "2025-03-04", day_order: "a0" },
    ]);
    // Read siblings (excludes the moving record) — reference not present
    mockSql.mockResolvedValueOnce([
      { id: "20250304-11111111", day_order: "a1" },
    ]);

    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({
          position: { kind: "before", reference_id: refId },
        }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
  });

  it("returns 200 when S3 version bump fails after reorder commits", async () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    mockSql.mockResolvedValueOnce([
      { id: recordId, date: "2025-03-04", day_order: "a1" },
    ]);
    // Read siblings (excludes the moving record)
    mockSql.mockResolvedValueOnce([
      { id: "20250304-11111111", day_order: "a0" },
    ]);
    mockSql.mockResolvedValueOnce([
      {
        id: recordId,
        date: "2025-03-04",
        day_order: "a/",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    mockSql.mockResolvedValueOnce([{ version: 14, updated_at: "2025-03-04T12:00:00.000Z" }]);
    mockBumpS3Version.mockRejectedValueOnce(new Error("S3 unavailable"));

    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.sync_version).toBe(14);

    errorSpy.mockRestore();
  });

  it("defaults sync_version to 0 when sync_version table is empty", async () => {
    // Read current record
    mockSql.mockResolvedValueOnce([
      { id: recordId, date: "2025-03-04", day_order: "a1" },
    ]);
    // Read siblings
    mockSql.mockResolvedValueOnce([
      { id: "20250304-11111111", day_order: "a0" },
    ]);
    // UPDATE
    mockSql.mockResolvedValueOnce([
      {
        id: recordId,
        date: "2025-03-04",
        day_order: "a/",
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version returns empty
    mockSql.mockResolvedValueOnce([]);

    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.sync_version).toBe(0);
    expect(mockBumpS3Version).toHaveBeenCalledWith(0, expect.any(String), "test-user-id");
  });

  it("returns 500 on database error", async () => {
    mockSql.mockRejectedValueOnce(new Error("connection refused"));

    const req = new NextRequest(
      `http://localhost/api/records/${recordId}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "first" } }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext(recordId));

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });

});
