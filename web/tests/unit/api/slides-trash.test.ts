import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

const mockTransaction = vi.fn();
const mockSql = Object.assign(vi.fn(), { transaction: mockTransaction });
const mockBumpS3Version = vi.fn();
const mockDeleteS3Objects = vi.fn();
const { mockIsLocalMode, mockProxyToLocal } = vi.hoisted(() => ({
  mockIsLocalMode: vi.fn(),
  mockProxyToLocal: vi.fn(),
}));
vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

// Mock S3 cleanup/version helpers used by the route.
vi.mock("@/lib/s3", () => ({
  bumpS3Version: (...args: unknown[]) => mockBumpS3Version(...args),
  deleteS3Objects: (...args: unknown[]) => mockDeleteS3Objects(...args),
}));

vi.mock("@/lib/auth-helpers", () => ({
  requireUser: vi.fn().mockResolvedValue({ id: "test-user-id", email: "test@test.com" }),
}));

vi.mock("@/lib/local-proxy", () => ({
  isLocalMode: mockIsLocalMode,
  proxyToLocal: mockProxyToLocal,
}));

import { DELETE } from "@/app/api/slides/trash/route";

/**
 * Helper: sets up mockSql for a trash DELETE request.
 *
 * The route calls sql`` 4 times inside sql.transaction([...]) to create query
 * objects, then calls sql`` once more for the sync version query. This helper
 * configures mock returns for all calls in the correct order.
 */
function setupTrashMocks(
  transactionResult: unknown[],
  syncVersionRow: Record<string, unknown>[] | undefined
) {
  // 4 tagged-template calls inside the transaction array (return dummy query objects)
  for (let i = 0; i < 4; i++) {
    mockSql.mockReturnValueOnce({});
  }
  mockTransaction.mockResolvedValueOnce(transactionResult);

  if (syncVersionRow !== undefined) {
    // Sync version query (outside transaction)
    mockSql.mockResolvedValueOnce(syncVersionRow);
  }
}

describe("DELETE /api/slides/trash", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockTransaction.mockReset();
    mockBumpS3Version.mockReset();
    mockDeleteS3Objects.mockReset();
    mockBumpS3Version.mockResolvedValue(undefined);
    mockDeleteS3Objects.mockResolvedValue(undefined);
    mockIsLocalMode.mockReset();
    mockIsLocalMode.mockReturnValue(false);
    mockProxyToLocal.mockReset();
  });

  it("proxies to the local backend in local mode", async () => {
    const proxied = new Response("proxied", { status: 202 });
    mockIsLocalMode.mockReturnValueOnce(true);
    mockProxyToLocal.mockResolvedValueOnce(proxied);

    const req = new NextRequest("http://localhost/api/slides/trash", {
      method: "DELETE",
    });
    const res = await DELETE(req);

    expect(res).toBe(proxied);
    expect(mockProxyToLocal).toHaveBeenCalledWith(req);
    expect(mockSql).not.toHaveBeenCalled();
  });

  it("purges trashed slides and returns count", async () => {
    setupTrashMocks(
      [[{ count: 2 }], [{ s3_key: "fig1" }], [{ s3_key: "data1" }], []],
      [{ version: 5, updated_at: "2026-03-11T12:00:00.000Z" }]
    );

    const req = new NextRequest("http://localhost/api/slides/trash", {
      method: "DELETE",
    });
    const res = await DELETE(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.purged_count).toBe(2);
    expect(body.sync_version).toBe(5);
    expect(mockDeleteS3Objects).toHaveBeenCalledWith(["fig1", "data1"], "test-user-id");
    expect(mockBumpS3Version).toHaveBeenCalledWith(
      5,
      "2026-03-11T12:00:00.000Z",
      "test-user-id"
    );
  });

  it("returns zero when no trashed slides exist", async () => {
    setupTrashMocks(
      [[{ count: 0 }], [], [], []],
      [{ version: 3, updated_at: "2026-03-11T12:05:00.000Z" }]
    );

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
    // Transaction inner calls still consume sql`` calls
    for (let i = 0; i < 4; i++) {
      mockSql.mockReturnValueOnce({});
    }
    mockTransaction.mockRejectedValueOnce(new Error("connection refused"));

    const req = new NextRequest("http://localhost/api/slides/trash", {
      method: "DELETE",
    });
    const res = await DELETE(req);

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });

  it("purges trashed slides with no S3 keys to clean", async () => {
    setupTrashMocks(
      [[{ count: 1 }], [], [], []],
      [{ version: 7, updated_at: "2026-03-11T12:10:00.000Z" }]
    );

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
      "2026-03-11T12:10:00.000Z",
      "test-user-id"
    );
  });

  it("falls back to zero for empty sync version result", async () => {
    setupTrashMocks(
      [[{ count: 0 }], [], [], []],
      [] // empty sync version
    );

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
    setupTrashMocks(
      [[{ count: 1 }], [{ s3_key: "fig1" }], [], []],
      [{ version: 9, updated_at: "2026-03-11T12:15:00.000Z" }]
    );
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
