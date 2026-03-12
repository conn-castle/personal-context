/**
 * Tests that each API route correctly proxies to the Go server
 * when LOCAL_BACKEND_URL is set (local dev mode).
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

// Mock local-proxy to simulate local mode
vi.mock("@/lib/local-proxy", () => ({
  isLocalMode: () => true,
  proxyToLocal: vi.fn().mockResolvedValue(
    new Response(JSON.stringify({ proxied: true }), {
      status: 200,
      headers: { "content-type": "application/json" },
    })
  ),
}));

// Mock db and s3 so they don't fail when imported (even though they won't be called)
vi.mock("@/lib/db", () => ({
  getDb: () => vi.fn(),
}));
vi.mock("@/lib/s3", () => ({
  getPresignedUrl: vi.fn(),
  getS3Version: vi.fn(),
  bumpS3Version: vi.fn(),
  deleteS3Objects: vi.fn(),
}));

import { proxyToLocal } from "@/lib/local-proxy";
import { GET as projectsGET } from "@/app/api/projects/route";
import { GET as slidesGET } from "@/app/api/slides/route";
import { GET as slideGET, PATCH as slidePATCH, DELETE as slideDELETE } from "@/app/api/slides/[id]/route";
import { PATCH as orderPATCH } from "@/app/api/slides/[id]/order/route";
import { POST as restorePOST } from "@/app/api/slides/[id]/restore/route";
import { GET as syncVersionGET } from "@/app/api/sync/version/route";
import { GET as syncChangesGET } from "@/app/api/sync/changes/route";
import { GET as filesGET } from "@/app/api/files/[slideId]/[...path]/route";
import { GET as infoGET } from "@/app/api/info/route";
import { GET as statsGET } from "@/app/api/stats/route";
import { DELETE as trashDELETE } from "@/app/api/slides/trash/route";

const mockProxy = proxyToLocal as ReturnType<typeof vi.fn>;

describe("API routes — local proxy guard", () => {
  beforeEach(() => {
    mockProxy.mockClear();
  });

  it("GET /api/projects proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/projects");
    const res = await projectsGET(req);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("GET /api/slides proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/slides");
    const res = await slidesGET(req);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("GET /api/slides/[id] proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/slides/20260310-aaaaaaaa");
    const ctx = { params: Promise.resolve({ id: "20260310-aaaaaaaa" }) };
    const res = await slideGET(req, ctx);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("PATCH /api/slides/[id] proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/slides/20260310-aaaaaaaa", {
      method: "PATCH",
      body: JSON.stringify({ project_id: "test" }),
    });
    const ctx = { params: Promise.resolve({ id: "20260310-aaaaaaaa" }) };
    const res = await slidePATCH(req, ctx);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("DELETE /api/slides/[id] proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/slides/20260310-aaaaaaaa", {
      method: "DELETE",
    });
    const ctx = { params: Promise.resolve({ id: "20260310-aaaaaaaa" }) };
    const res = await slideDELETE(req, ctx);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("PATCH /api/slides/[id]/order proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/slides/20260310-aaaaaaaa/order", {
      method: "PATCH",
      body: JSON.stringify({ position: { kind: "last" } }),
    });
    const ctx = { params: Promise.resolve({ id: "20260310-aaaaaaaa" }) };
    const res = await orderPATCH(req, ctx);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("POST /api/slides/[id]/restore proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/slides/20260310-aaaaaaaa/restore", {
      method: "POST",
    });
    const ctx = { params: Promise.resolve({ id: "20260310-aaaaaaaa" }) };
    const res = await restorePOST(req, ctx);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("GET /api/sync/version proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/sync/version");
    const res = await syncVersionGET(req);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("GET /api/sync/changes proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/sync/changes?since=2026-03-10T00:00:00Z");
    const res = await syncChangesGET(req);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("GET /api/files/[slideId]/[...path] proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/files/20260310-aaaaaaaa/figures/fig.png");
    const ctx = {
      params: Promise.resolve({
        slideId: "20260310-aaaaaaaa",
        path: ["figures", "fig.png"],
      }),
    };
    const res = await filesGET(req, ctx);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("GET /api/info proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/info");
    const res = await infoGET(req);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("GET /api/stats proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/stats");
    const res = await statsGET(req);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });

  it("DELETE /api/slides/trash proxies in local mode", async () => {
    const req = new NextRequest("http://localhost/api/slides/trash", {
      method: "DELETE",
    });
    const res = await trashDELETE(req);
    expect(mockProxy).toHaveBeenCalledWith(req);
    expect(res.status).toBe(200);
  });
});
