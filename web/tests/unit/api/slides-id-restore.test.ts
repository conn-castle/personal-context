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

import { POST } from "@/app/api/slides/[id]/restore/route";

type RouteContext = { params: Promise<{ id: string }> };

function makeContext(id: string): RouteContext {
  return { params: Promise.resolve({ id }) };
}

describe("POST /api/slides/[id]/restore", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockBumpS3Version.mockReset();
    mockBumpS3Version.mockResolvedValue(undefined);
  });

  const slideId = "20250304-a3f2b7e1";

  it("restores a deleted slide", async () => {
    // UPDATE RETURNING
    mockSql.mockResolvedValueOnce([
      {
        id: slideId,
        deleted_at: null,
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version
    mockSql.mockResolvedValueOnce([{ version: 8, updated_at: "2025-03-04T12:00:00.000Z" }]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/restore`,
      { method: "POST" }
    );
    const res = await POST(req, makeContext(slideId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.id).toBe(slideId);
    expect(body.deleted_at).toBeNull();
    expect(body.updated_at).toBe("2025-03-04T12:00:00.000Z");
    expect(body.sync_version).toBe(8);
  });

  it("returns 404 for a slide that is not deleted", async () => {
    // UPDATE returns no rows (slide exists but deleted_at IS NULL)
    mockSql.mockResolvedValueOnce([]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/restore`,
      { method: "POST" }
    );
    const res = await POST(req, makeContext(slideId));

    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
  });

  it("returns 404 for nonexistent slide", async () => {
    mockSql.mockResolvedValueOnce([]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/restore`,
      { method: "POST" }
    );
    const res = await POST(req, makeContext(slideId));

    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
  });

  it("returns 400 for invalid slide ID", async () => {
    const req = new NextRequest(
      "http://localhost/api/slides/bad-id/restore",
      { method: "POST" }
    );
    const res = await POST(req, makeContext("bad-id"));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("INVALID_ID");
  });

  it("bumps S3 version after successful restore", async () => {
    mockSql.mockResolvedValueOnce([
      {
        id: slideId,
        deleted_at: null,
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    mockSql.mockResolvedValueOnce([{ version: 12, updated_at: "2025-03-04T12:00:00.000Z" }]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/restore`,
      { method: "POST" }
    );
    await POST(req, makeContext(slideId));

    expect(mockBumpS3Version).toHaveBeenCalledWith(12, "2025-03-04T12:00:00.000Z");
  });

  it("returns 200 when S3 version bump fails after restore commits", async () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    mockSql.mockResolvedValueOnce([
      {
        id: slideId,
        deleted_at: null,
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    mockSql.mockResolvedValueOnce([{ version: 13, updated_at: "2025-03-04T12:00:00.000Z" }]);
    mockBumpS3Version.mockRejectedValueOnce(new Error("S3 unavailable"));

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/restore`,
      { method: "POST" }
    );
    const res = await POST(req, makeContext(slideId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.sync_version).toBe(13);

    errorSpy.mockRestore();
  });

  it("defaults to sync_version 0 when no version rows", async () => {
    mockSql.mockResolvedValueOnce([
      {
        id: slideId,
        deleted_at: null,
        updated_at: "2025-03-04T12:00:00.000Z",
      },
    ]);
    // sync_version query returns empty
    mockSql.mockResolvedValueOnce([]);

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/restore`,
      { method: "POST" }
    );
    const res = await POST(req, makeContext(slideId));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.sync_version).toBe(0);
    expect(mockBumpS3Version).toHaveBeenCalledWith(0, expect.any(String));
  });

  it("returns 500 on database error", async () => {
    mockSql.mockRejectedValueOnce(new Error("connection refused"));

    const req = new NextRequest(
      `http://localhost/api/slides/${slideId}/restore`,
      { method: "POST" }
    );
    const res = await POST(req, makeContext(slideId));

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });

});
