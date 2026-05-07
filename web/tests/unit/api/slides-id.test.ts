import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

const mockSql = vi.fn();
vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

const mockBumpS3Version = vi.fn();
vi.mock("@/lib/s3", () => ({
  bumpS3Version: (...args: unknown[]) => mockBumpS3Version(...args),
}));

const mockHandlePatchSlide = vi.fn();
vi.mock("@/lib/slides-handlers", () => ({
  handlePatchSlide: (...args: unknown[]) => mockHandlePatchSlide(...args),
}));

vi.mock("@/lib/auth-helpers", () => ({
  requireUser: vi.fn().mockResolvedValue({ id: "test-user-id", email: "test@test.com" }),
}));

import { GET, PATCH, DELETE } from "@/app/api/slides/[id]/route";

/** Helper to build a route context with params promise. */
function makeContext(id: string) {
  return { params: Promise.resolve({ id }) };
}

describe("GET /api/slides/[id]", () => {
  beforeEach(() => {
    mockSql.mockReset();
  });

  it("returns slide with figures and data files", async () => {
    const slideRow = {
      id: "20260301-aabbccdd",
      date: "2026-03-01",
      day_order: "a0",
      html_content: "<p>hello</p>",
      notes: "some notes",
      project_id: "org/proj",
      source_device_id: "device-a",
      source_ref: "vault://record",
      git_remote_url: "https://github.com/org/repo",
      git_hash: "a".repeat(40),
      created_at: "2026-03-01T10:00:00.000Z",
      updated_at: "2026-03-01T14:00:00.000Z",
      deleted_at: null,
    };
    const figureRows = [
      {
        filename: "chart.png",
        s3_key: "slides/20260301-aabbccdd/figures/chart.png",
        size: 1024,
        hash: "abc123",
        alt_text: "A chart",
        description: null,
      },
    ];
    const dataFileRows = [
      {
        filename: "data.csv",
        s3_key: "slides/20260301-aabbccdd/data/data.csv",
        size: 512,
        hash: "def456",
        description: "Raw data",
      },
    ];

    // First call: slide query; second: figures; third: data_files
    mockSql
      .mockResolvedValueOnce([slideRow])
      .mockResolvedValueOnce(figureRows)
      .mockResolvedValueOnce(dataFileRows);

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd"
    );
    const res = await GET(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.slide.id).toBe("20260301-aabbccdd");
    expect(body.slide.html_content).toBe("<p>hello</p>");
    expect(body.slide.source_device_id).toBe("device-a");
    expect(body.slide.source_ref).toBe("vault://record");
    expect(body.slide.figures).toHaveLength(1);
    expect(body.slide.figures[0].filename).toBe("chart.png");
    expect(body.slide.data_files).toHaveLength(1);
    expect(body.slide.data_files[0].filename).toBe("data.csv");
    expect(body.slide.data_files[0].alt_text).toBeUndefined();
    expect(mockSql.mock.calls[2]?.[0].join("")).not.toContain("alt_text");
  });

  it("handles null size, hash, and description fields in figure and data file rows", async () => {
    const slideRow = {
      id: "20260301-aabbccdd",
      date: "2026-03-01",
      day_order: "a0",
      html_content: "<p>hello</p>",
      notes: null,
      project_id: "org/proj",
      source_device_id: "device-a",
      source_ref: null,
      git_remote_url: null,
      git_hash: null,
      created_at: "2026-03-01T10:00:00.000Z",
      updated_at: "2026-03-01T14:00:00.000Z",
      deleted_at: null,
    };
    const figureRows = [
      {
        filename: "chart.png",
        s3_key: "slides/20260301-aabbccdd/figures/chart.png",
        size: null,
        hash: null,
        alt_text: null,
        description: null,
      },
    ];
    const dataFileRows = [
      {
        filename: "data.csv",
        s3_key: "slides/20260301-aabbccdd/data/data.csv",
        size: null,
        hash: null,
        description: null,
      },
    ];

    mockSql
      .mockResolvedValueOnce([slideRow])
      .mockResolvedValueOnce(figureRows)
      .mockResolvedValueOnce(dataFileRows);

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd"
    );
    const res = await GET(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.slide.figures[0].size).toBeUndefined();
    expect(body.slide.figures[0].hash).toBeUndefined();
    expect(body.slide.figures[0].alt_text).toBeNull();
    expect(body.slide.data_files[0].size).toBeUndefined();
    expect(body.slide.data_files[0].hash).toBeUndefined();
    expect(body.slide.data_files[0].alt_text).toBeUndefined();
  });

  it("returns null HTML for notes/data-only records", async () => {
    const slideRow = {
      id: "20260301-aabbccdd",
      date: "2026-03-01",
      day_order: "a0",
      html_content: null,
      notes: "notes-first record",
      project_id: "org/proj",
      source_device_id: "device-a",
      source_ref: null,
      git_remote_url: null,
      git_hash: null,
      created_at: "2026-03-01T10:00:00.000Z",
      updated_at: "2026-03-01T14:00:00.000Z",
      deleted_at: null,
    };

    mockSql
      .mockResolvedValueOnce([slideRow])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([]);

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd"
    );
    const res = await GET(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.slide.html_content).toBeNull();
    expect(body.slide.source_device_id).toBe("device-a");
  });

  it("returns 404 for nonexistent slide ID", async () => {
    mockSql.mockResolvedValueOnce([]);

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-00000000"
    );
    const res = await GET(req, makeContext("20260301-00000000"));

    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
  });

  it("returns 400 for invalid slide ID format", async () => {
    const req = new NextRequest("http://localhost/api/slides/bad-id");
    const res = await GET(req, makeContext("bad-id"));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("INVALID_ID");
  });

  it("returns 500 on unexpected database error", async () => {
    mockSql.mockRejectedValue(new Error("query timeout"));

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd"
    );
    const res = await GET(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });
});

describe("PATCH /api/slides/[id]", () => {
  beforeEach(() => {
    mockHandlePatchSlide.mockReset();
  });

  it("delegates to handlePatchSlide with id and body", async () => {
    const { NextResponse } = await import("next/server");
    mockHandlePatchSlide.mockResolvedValue(
      NextResponse.json({ slide: { id: "20260301-aabbccdd" }, sync_version: 5 })
    );

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd",
      {
        method: "PATCH",
        body: JSON.stringify({ notes: "updated" }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(200);
    expect(mockHandlePatchSlide).toHaveBeenCalledWith(
      "20260301-aabbccdd",
      { notes: "updated" },
      "test-user-id"
    );
  });

  it("returns 400 for malformed JSON bodies", async () => {
    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd",
      {
        method: "PATCH",
        body: "{bad-json",
        headers: { "Content-Type": "application/json" },
      }
    );

    const res = await PATCH(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(mockHandlePatchSlide).not.toHaveBeenCalled();
  });

  it("returns 400 for invalid slide ID", async () => {
    const req = new NextRequest(
      "http://localhost/api/slides/bad-id",
      {
        method: "PATCH",
        body: JSON.stringify({ notes: "updated" }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext("bad-id"));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("INVALID_ID");
    expect(mockHandlePatchSlide).not.toHaveBeenCalled();
  });

  it("returns 400 for non-object body (null)", async () => {
    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd",
      {
        method: "PATCH",
        body: JSON.stringify(null),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toBe("Request body must be a JSON object");
    expect(mockHandlePatchSlide).not.toHaveBeenCalled();
  });

  it("returns 400 for non-object body (array)", async () => {
    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd",
      {
        method: "PATCH",
        body: JSON.stringify([1, 2, 3]),
        headers: { "Content-Type": "application/json" },
      }
    );
    const res = await PATCH(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toBe("Request body must be a JSON object");
    expect(mockHandlePatchSlide).not.toHaveBeenCalled();
  });

  it("returns 500 when params resolution throws", async () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd",
      {
        method: "PATCH",
        body: JSON.stringify({ notes: "updated" }),
        headers: { "Content-Type": "application/json" },
      }
    );
    const brokenContext = {
      params: Promise.reject(new Error("params resolution failed")),
    };
    const res = await PATCH(req, brokenContext);

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
    expect(mockHandlePatchSlide).not.toHaveBeenCalled();

    errorSpy.mockRestore();
  });
});

describe("DELETE /api/slides/[id]", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockBumpS3Version.mockReset();
  });

  it("soft deletes a slide and bumps S3 version", async () => {
    const deletedRow = {
      id: "20260301-aabbccdd",
      deleted_at: "2026-03-09T10:00:00.000Z",
      updated_at: "2026-03-09T10:00:00.000Z",
    };

    mockSql.mockResolvedValueOnce([deletedRow]);
    mockSql.mockResolvedValueOnce([{ version: 6, updated_at: "2026-03-09T10:00:00.000Z" }]);
    mockBumpS3Version.mockResolvedValue(undefined);

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd",
      { method: "DELETE" }
    );
    const res = await DELETE(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.id).toBe("20260301-aabbccdd");
    expect(body.deleted_at).toBe("2026-03-09T10:00:00.000Z");
    expect(body.updated_at).toBe("2026-03-09T10:00:00.000Z");
    expect(body.sync_version).toBe(6);

    expect(mockBumpS3Version).toHaveBeenCalledWith(6, "2026-03-09T10:00:00.000Z", "test-user-id");
  });

  it("returns 404 for nonexistent slide", async () => {
    mockSql.mockResolvedValueOnce([]);

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-00000000",
      { method: "DELETE" }
    );
    const res = await DELETE(req, makeContext("20260301-00000000"));

    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
  });

  it("returns 404 for already-deleted slide", async () => {
    // RETURNING clause with deleted_at IS NULL filter yields empty result
    mockSql.mockResolvedValueOnce([]);

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd",
      { method: "DELETE" }
    );
    const res = await DELETE(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
  });

  it("returns 400 for invalid slide ID format", async () => {
    const req = new NextRequest("http://localhost/api/slides/invalid", {
      method: "DELETE",
    });
    const res = await DELETE(req, makeContext("invalid"));

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("INVALID_ID");
  });

  it("bumps S3 version after Postgres commit", async () => {
    const deletedRow = {
      id: "20260301-aabbccdd",
      deleted_at: "2026-03-09T10:00:00.000Z",
      updated_at: "2026-03-09T10:00:00.000Z",
    };

    mockSql.mockResolvedValueOnce([deletedRow]);
    mockSql.mockResolvedValueOnce([{ version: 11, updated_at: "2026-03-09T10:00:00.000Z" }]);
    mockBumpS3Version.mockResolvedValue(undefined);

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd",
      { method: "DELETE" }
    );
    await DELETE(req, makeContext("20260301-aabbccdd"));

    expect(mockSql).toHaveBeenCalledTimes(2);
    expect(mockBumpS3Version).toHaveBeenCalledWith(11, "2026-03-09T10:00:00.000Z", "test-user-id");
  });

  it("returns 200 when S3 version bump fails after delete commits", async () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const deletedRow = {
      id: "20260301-aabbccdd",
      deleted_at: "2026-03-09T10:00:00.000Z",
      updated_at: "2026-03-09T10:00:00.000Z",
    };

    mockSql.mockResolvedValueOnce([deletedRow]);
    mockSql.mockResolvedValueOnce([{ version: 12, updated_at: "2026-03-09T10:00:00.000Z" }]);
    mockBumpS3Version.mockRejectedValueOnce(new Error("S3 unavailable"));

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd",
      { method: "DELETE" }
    );
    const res = await DELETE(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.sync_version).toBe(12);

    errorSpy.mockRestore();
  });

  it("returns 500 on unexpected database error", async () => {
    mockSql.mockRejectedValue(new Error("connection refused"));

    const req = new NextRequest(
      "http://localhost/api/slides/20260301-aabbccdd",
      { method: "DELETE" }
    );
    const res = await DELETE(req, makeContext("20260301-aabbccdd"));

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });

});
