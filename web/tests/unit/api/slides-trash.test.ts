import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

const mockSql = vi.fn();
const mockBumpS3Version = vi.fn();
const mockDeleteS3Objects = vi.fn();
vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

// Mock S3 cleanup/version helpers used by the route.
vi.mock("@/lib/s3", () => ({
  bumpS3Version: (...args: unknown[]) => mockBumpS3Version(...args),
  deleteS3Objects: (...args: unknown[]) => mockDeleteS3Objects(...args),
}));

import { DELETE } from "@/app/api/slides/trash/route";

describe("DELETE /api/slides/trash", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockBumpS3Version.mockReset();
    mockDeleteS3Objects.mockReset();
    mockBumpS3Version.mockResolvedValue(undefined);
    mockDeleteS3Objects.mockResolvedValue(undefined);
  });

  it("purges trashed slides and returns count", async () => {
    mockSql
      .mockResolvedValueOnce([{ count: 2 }]) // count trashed
      .mockResolvedValueOnce([{ s3_key: "fig1" }]) // figure keys
      .mockResolvedValueOnce([{ s3_key: "data1" }]) // data file keys
      .mockResolvedValueOnce([]) // DELETE
      .mockResolvedValueOnce([
        { version: 5, updated_at: "2026-03-11T12:00:00.000Z" },
      ]); // sync version

    const req = new NextRequest("http://localhost/api/slides/trash", {
      method: "DELETE",
    });
    const res = await DELETE(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.purged_count).toBe(2);
    expect(body.sync_version).toBe(5);
    expect(mockDeleteS3Objects).toHaveBeenCalledWith(["fig1", "data1"]);
    expect(mockBumpS3Version).toHaveBeenCalledWith(
      5,
      "2026-03-11T12:00:00.000Z"
    );
  });

  it("returns zero when no trashed slides exist", async () => {
    mockSql
      .mockResolvedValueOnce([{ count: 0 }]) // count trashed
      .mockResolvedValueOnce([
        { version: 3, updated_at: "2026-03-11T12:05:00.000Z" },
      ]); // sync version

    const req = new NextRequest("http://localhost/api/slides/trash", {
      method: "DELETE",
    });
    const res = await DELETE(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.purged_count).toBe(0);
    expect(body.sync_version).toBe(3);
    expect(mockDeleteS3Objects).not.toHaveBeenCalled();
    expect(mockBumpS3Version).not.toHaveBeenCalled();
  });

  it("returns 500 on database error", async () => {
    mockSql.mockRejectedValueOnce(new Error("connection refused"));

    const req = new NextRequest("http://localhost/api/slides/trash", {
      method: "DELETE",
    });
    const res = await DELETE(req);

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });

  it("purges trashed slides with no S3 keys to clean", async () => {
    mockSql
      .mockResolvedValueOnce([{ count: 1 }]) // count trashed
      .mockResolvedValueOnce([]) // no figure keys
      .mockResolvedValueOnce([]) // no data file keys
      .mockResolvedValueOnce([]) // DELETE
      .mockResolvedValueOnce([
        { version: 7, updated_at: "2026-03-11T12:10:00.000Z" },
      ]); // sync version

    const req = new NextRequest("http://localhost/api/slides/trash", {
      method: "DELETE",
    });
    const res = await DELETE(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.purged_count).toBe(1);
    expect(body.sync_version).toBe(7);
    expect(mockDeleteS3Objects).not.toHaveBeenCalled();
    expect(mockBumpS3Version).toHaveBeenCalledWith(
      7,
      "2026-03-11T12:10:00.000Z"
    );
  });

  it("falls back to zero for empty sync version result", async () => {
    mockSql
      .mockResolvedValueOnce([{ count: 0 }]) // count trashed
      .mockResolvedValueOnce([]); // empty sync version

    const req = new NextRequest("http://localhost/api/slides/trash", {
      method: "DELETE",
    });
    const res = await DELETE(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.purged_count).toBe(0);
    expect(body.sync_version).toBe(0);
    expect(mockBumpS3Version).not.toHaveBeenCalled();
  });

  it("still succeeds when the S3 version bump fails after the delete", async () => {
    mockSql
      .mockResolvedValueOnce([{ count: 1 }])
      .mockResolvedValueOnce([{ s3_key: "fig1" }])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        { version: 9, updated_at: "2026-03-11T12:15:00.000Z" },
      ]);
    mockBumpS3Version.mockRejectedValueOnce(new Error("s3 unavailable"));

    const req = new NextRequest("http://localhost/api/slides/trash", {
      method: "DELETE",
    });
    const res = await DELETE(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.purged_count).toBe(1);
    expect(body.sync_version).toBe(9);
  });
});
