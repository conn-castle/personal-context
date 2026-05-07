import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

const mockSql = vi.fn();
vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

const mockGetPresignedUrl = vi.fn();
const { mockIsLocalMode, mockProxyToLocal } = vi.hoisted(() => ({
  mockIsLocalMode: vi.fn(),
  mockProxyToLocal: vi.fn(),
}));
vi.mock("@/lib/s3", () => ({
  getPresignedUrl: (...args: unknown[]) => mockGetPresignedUrl(...args),
  getS3Version: vi.fn(),
}));

vi.mock("@/lib/auth-helpers", () => ({
  requireUser: vi.fn().mockResolvedValue({ id: "test-user-id", email: "test@test.com" }),
}));

vi.mock("@/lib/local-proxy", () => ({
  isLocalMode: mockIsLocalMode,
  proxyToLocal: mockProxyToLocal,
}));

import { GET } from "@/app/api/files/[slideId]/[...path]/route";

type RouteContext = {
  params: Promise<{ slideId: string; path: string[] }>;
};

function makeContext(slideId: string, path: string[]): RouteContext {
  return { params: Promise.resolve({ slideId, path }) };
}

describe("GET /api/files/[slideId]/[...path]", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockGetPresignedUrl.mockReset();
    mockIsLocalMode.mockReset();
    mockIsLocalMode.mockReturnValue(false);
    mockProxyToLocal.mockReset();
  });

  const slideId = "20250304-a3f2b7e1";

  it("proxies to the local backend in local mode", async () => {
    const proxied = new Response("proxied", { status: 202 });
    mockIsLocalMode.mockReturnValueOnce(true);
    mockProxyToLocal.mockResolvedValueOnce(proxied);

    const req = new NextRequest(
      `http://localhost/api/files/${slideId}/figures/chart.png`
    );
    const res = await GET(req, makeContext(slideId, ["figures", "chart.png"]));

    expect(res).toBe(proxied);
    expect(mockProxyToLocal).toHaveBeenCalledWith(req);
    expect(mockSql).not.toHaveBeenCalled();
  });

  it("returns presigned URL for a figure file", async () => {
    mockSql.mockResolvedValueOnce([
      { s3_key: `figures/${slideId}/chart-v2.png` },
    ]);
    mockGetPresignedUrl.mockResolvedValueOnce({
      url: "https://s3.example.com/signed-url",
      expires_at: "2025-03-04T13:00:00.000Z",
    });

    const req = new NextRequest(
      `http://localhost/api/files/${slideId}/figures/chart.png`
    );
    const res = await GET(req, makeContext(slideId, ["figures", "chart.png"]));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.url).toBe("https://s3.example.com/signed-url");
    expect(body.expires_at).toBe("2025-03-04T13:00:00.000Z");
    expect(mockGetPresignedUrl).toHaveBeenCalledWith(
      `figures/${slideId}/chart-v2.png`,
      "test-user-id"
    );
  });

  it("returns presigned URL for a data file", async () => {
    mockSql.mockResolvedValueOnce([
      { s3_key: `data/${slideId}/results-2025.csv` },
    ]);
    mockGetPresignedUrl.mockResolvedValueOnce({
      url: "https://s3.example.com/data-url",
      expires_at: "2025-03-04T13:00:00.000Z",
    });

    const req = new NextRequest(
      `http://localhost/api/files/${slideId}/data/results.csv`
    );
    const res = await GET(req, makeContext(slideId, ["data", "results.csv"]));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.url).toBe("https://s3.example.com/data-url");
    expect(mockGetPresignedUrl).toHaveBeenCalledWith(
      `data/${slideId}/results-2025.csv`,
      "test-user-id"
    );
  });

  it("returns 404 for unknown file", async () => {
    mockSql.mockResolvedValueOnce([]);

    const req = new NextRequest(
      `http://localhost/api/files/${slideId}/figures/unknown.png`
    );
    const res = await GET(
      req,
      makeContext(slideId, ["figures", "unknown.png"])
    );

    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
  });

  it("returns 400 for invalid slide ID", async () => {
    const req = new NextRequest(
      "http://localhost/api/files/bad-id/figures/chart.png"
    );
    const res = await GET(
      req,
      makeContext("bad-id", ["figures", "chart.png"])
    );

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("INVALID_ID");
  });

  it("returns 400 for invalid path type", async () => {
    const req = new NextRequest(
      `http://localhost/api/files/${slideId}/invalid/chart.png`
    );
    const res = await GET(
      req,
      makeContext(slideId, ["invalid", "chart.png"])
    );

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 400 for path traversal attempt", async () => {
    const req = new NextRequest(
      `http://localhost/api/files/${slideId}/figures/../../../etc/passwd`
    );
    const res = await GET(
      req,
      makeContext(slideId, ["figures", "../../../etc/passwd"])
    );

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 400 for missing filename in path", async () => {
    const req = new NextRequest(
      `http://localhost/api/files/${slideId}/figures`
    );
    const res = await GET(req, makeContext(slideId, ["figures"]));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 400 for extra path segments", async () => {
    const req = new NextRequest(
      `http://localhost/api/files/${slideId}/figures/chart.png/extra`
    );
    const res = await GET(
      req,
      makeContext(slideId, ["figures", "chart.png", "extra"])
    );

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("returns 500 on database error", async () => {
    mockSql.mockRejectedValueOnce(new Error("connection refused"));

    const req = new NextRequest(
      `http://localhost/api/files/${slideId}/figures/chart.png`
    );
    const res = await GET(
      req,
      makeContext(slideId, ["figures", "chart.png"])
    );

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });

  it("returns 500 on S3 presigning error", async () => {
    mockSql.mockResolvedValueOnce([
      { s3_key: `figures/${slideId}/chart-v2.png` },
    ]);
    mockGetPresignedUrl.mockRejectedValueOnce(
      new Error("S3 access denied")
    );

    const req = new NextRequest(
      `http://localhost/api/files/${slideId}/figures/chart.png`
    );
    const res = await GET(
      req,
      makeContext(slideId, ["figures", "chart.png"])
    );

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });
});
