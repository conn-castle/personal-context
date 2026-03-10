import { describe, expect, it, vi, beforeEach } from "vitest";

const mockSql = vi.fn();
vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

const mockBumpS3Version = vi.fn();
vi.mock("@/lib/s3", () => ({
  bumpS3Version: (...args: unknown[]) => mockBumpS3Version(...args),
  getS3Version: vi.fn(),
}));

import { handlePatchSlide } from "@/lib/slides-handlers";

describe("PATCH /api/slides/[id] (handlePatchSlide)", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockBumpS3Version.mockReset();
    mockBumpS3Version.mockResolvedValue(undefined);
  });

  const slideId = "20250304-a3f2b7e1";

  const makeSlideRow = (overrides: Record<string, unknown> = {}) => ({
    id: slideId,
    date: "2025-03-04",
    day_order: "a0",
    html_content: "<p>Hello</p>",
    notes: null,
    project_id: null,
    git_remote_url: null,
    git_hash: null,
    created_at: "2025-03-04T08:00:00.000Z",
    updated_at: "2025-03-04T10:00:00.000Z",
    deleted_at: null,
    ...overrides,
  });

  const makeFigures = () => [
    {
      filename: "fig1.png",
      s3_key: "figures/20250304-a3f2b7e1/fig1.png",
      size: 1234,
      hash: "abc123",
      alt_text: null,
      description: null,
    },
  ];

  const makeDataFiles = () => [
    {
      filename: "data.csv",
      s3_key: "data/20250304-a3f2b7e1/data.csv",
      size: 5678,
      hash: "def456",
      description: null,
    },
  ];

  it("updates project_id", async () => {
    // Dynamic UPDATE query
    mockSql.mockResolvedValueOnce([
      makeSlideRow({ project_id: "org/alpha" }),
    ]);
    // sync_version query
    mockSql.mockResolvedValueOnce([{ version: 5, updated_at: "2025-03-04T10:00:00.000Z" }]);
    // figures
    mockSql.mockResolvedValueOnce(makeFigures());
    // data_files
    mockSql.mockResolvedValueOnce(makeDataFiles());

    const res = await handlePatchSlide(slideId, { project_id: "org/alpha" });

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.slide.project_id).toBe("org/alpha");
    expect(body.sync_version).toBe(5);
    expect(mockBumpS3Version).toHaveBeenCalledWith(5, "2025-03-04T10:00:00.000Z");
  });

  it("updates notes and normalizes empty string to null", async () => {
    // Dynamic UPDATE query - empty notes normalized to null
    mockSql.mockResolvedValueOnce([makeSlideRow({ notes: null })]);
    // sync_version query
    mockSql.mockResolvedValueOnce([{ version: 6, updated_at: "2025-03-04T10:00:00.000Z" }]);
    // figures
    mockSql.mockResolvedValueOnce([]);
    // data_files
    mockSql.mockResolvedValueOnce([]);

    const res = await handlePatchSlide(slideId, { notes: "" });

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.slide.notes).toBeNull();
  });

  it("updates git fields", async () => {
    const gitHash = "a".repeat(40);
    const gitUrl = "https://github.com/example/repo.git";

    mockSql.mockResolvedValueOnce([
      makeSlideRow({ git_hash: gitHash, git_remote_url: gitUrl }),
    ]);
    mockSql.mockResolvedValueOnce([{ version: 7, updated_at: "2025-03-04T10:00:00.000Z" }]);
    mockSql.mockResolvedValueOnce([]);
    mockSql.mockResolvedValueOnce([]);

    const res = await handlePatchSlide(slideId, {
      git_hash: gitHash,
      git_remote_url: gitUrl,
    });

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.slide.git_hash).toBe(gitHash);
    expect(body.slide.git_remote_url).toBe(gitUrl);
  });

  it("validates git_hash format", async () => {
    const res = await handlePatchSlide(slideId, {
      git_hash: "not-a-valid-hash",
    });

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("rejects empty body", async () => {
    const res = await handlePatchSlide(slideId, {});

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
  });

  it("rejects unknown fields", async () => {
    const res = await handlePatchSlide(slideId, {
      unknown_field: "value",
    } as Record<string, unknown>);

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toContain("Unknown fields");
  });

  it("returns 404 for nonexistent slide", async () => {
    // Dynamic UPDATE returns no rows
    mockSql.mockResolvedValueOnce([]);

    const res = await handlePatchSlide(slideId, {
      project_id: "org/alpha",
    });

    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
  });

  it("returns 400 for invalid slide ID", async () => {
    const res = await handlePatchSlide("bad-id", {
      project_id: "org/alpha",
    });

    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("INVALID_ID");
  });

  it("bumps S3 version after successful update", async () => {
    mockSql.mockResolvedValueOnce([makeSlideRow()]);
    mockSql.mockResolvedValueOnce([{ version: 10, updated_at: "2025-03-04T10:00:00.000Z" }]);
    mockSql.mockResolvedValueOnce([]);
    mockSql.mockResolvedValueOnce([]);

    const res = await handlePatchSlide(slideId, { notes: "updated" });

    expect(res.status).toBe(200);
    expect(mockBumpS3Version).toHaveBeenCalledWith(10, "2025-03-04T10:00:00.000Z");
  });

  it("returns 200 when S3 version bump fails after the update commits", async () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    mockSql.mockResolvedValueOnce([makeSlideRow({ notes: "updated" })]);
    mockSql.mockResolvedValueOnce([{ version: 11, updated_at: "2025-03-04T10:00:00.000Z" }]);
    mockSql.mockResolvedValueOnce([]);
    mockSql.mockResolvedValueOnce([]);
    mockBumpS3Version.mockRejectedValueOnce(new Error("S3 unavailable"));

    const res = await handlePatchSlide(slideId, { notes: "updated" });

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.slide.notes).toBe("updated");
    expect(body.sync_version).toBe(11);

    errorSpy.mockRestore();
  });

  it("defaults sync_version to 0 when sync_version table is empty", async () => {
    mockSql.mockResolvedValueOnce([makeSlideRow({ notes: "updated" })]);
    // sync_version query returns empty
    mockSql.mockResolvedValueOnce([]);
    mockSql.mockResolvedValueOnce([]);
    mockSql.mockResolvedValueOnce([]);

    const res = await handlePatchSlide(slideId, { notes: "updated" });

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.sync_version).toBe(0);
    expect(mockBumpS3Version).toHaveBeenCalledWith(0, expect.any(String));
  });

  it("returns 500 on database error", async () => {
    mockSql.mockRejectedValueOnce(new Error("connection refused"));

    const res = await handlePatchSlide(slideId, { notes: "updated" });

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });

});
