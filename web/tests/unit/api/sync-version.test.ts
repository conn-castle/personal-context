import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

vi.mock("@/lib/db", () => ({
  getDb: () => vi.fn(),
}));

const mockGetS3Version = vi.fn();
vi.mock("@/lib/s3", () => ({
  getS3Version: (...args: unknown[]) => mockGetS3Version(...args),
}));

import { GET } from "@/app/api/sync/version/route";

describe("GET /api/sync/version", () => {
  beforeEach(() => {
    mockGetS3Version.mockReset();
  });

  it("returns version from S3", async () => {
    mockGetS3Version.mockResolvedValue({
      version: 42,
      updated_at: "2026-03-01T12:00:00.000Z",
    });

    const req = new NextRequest("http://localhost/api/sync/version");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.version).toBe(42);
    expect(body.updated_at).toBe("2026-03-01T12:00:00.000Z");
  });

  it("returns version 0 when no key exists in S3", async () => {
    mockGetS3Version.mockResolvedValue({
      version: 0,
      updated_at: "",
    });

    const req = new NextRequest("http://localhost/api/sync/version");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.version).toBe(0);
    expect(body.updated_at).toBe("");
  });

  it("returns 500 on S3 error", async () => {
    mockGetS3Version.mockRejectedValue(new Error("S3 access denied"));

    const req = new NextRequest("http://localhost/api/sync/version");
    const res = await GET(req);

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });
});
