/**
 * Contract parity tests — shared API response shapes between Go and Next.js.
 *
 * Each test defines the exact JSON contract for an endpoint and verifies that
 * the Next.js route handler produces a response that conforms to it. Every
 * test is annotated with the corresponding Go test function(s) in server_test.go
 * so it is easy to cross-reference the two implementations.
 *
 * Rules enforced here:
 *  1. Every expected field is present.
 *  2. Every present field has the correct JS type.
 *  3. No unexpected fields appear in the response (strict shape match).
 *
 * Functional edge-cases (error paths, validation, business logic) are covered
 * in the per-endpoint unit tests; this file is intentionally focused on shape.
 */

import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

// ---------------------------------------------------------------------------
// Shared mock setup (must precede all route imports)
// ---------------------------------------------------------------------------

const mockTransaction = vi.fn();
const mockSql = Object.assign(vi.fn(), { transaction: mockTransaction });
const mockBumpS3Version = vi.fn();
const mockDeleteS3Objects = vi.fn();
const mockGetPresignedUrl = vi.fn();
const mockGetS3Version = vi.fn();

vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

vi.mock("@/lib/s3", () => ({
  bumpS3Version: (...args: unknown[]) => mockBumpS3Version(...args),
  deleteS3Objects: (...args: unknown[]) => mockDeleteS3Objects(...args),
  getPresignedUrl: (...args: unknown[]) => mockGetPresignedUrl(...args),
  getS3Version: (...args: unknown[]) => mockGetS3Version(...args),
}));

// Ensure all routes run in cloud mode (not local proxy mode).
vi.mock("@/lib/local-proxy", () => ({
  isLocalMode: () => false,
  proxyToLocal: vi.fn(),
}));

vi.mock("@/lib/auth-helpers", () => ({
  requireUser: vi.fn().mockResolvedValue({ id: "test-user-id", email: "test@test.com" }),
}));

// ---------------------------------------------------------------------------
// Route handler imports (after mocks are established)
// ---------------------------------------------------------------------------

import { GET as infoGET } from "@/app/api/info/route";
import { GET as statsGET } from "@/app/api/stats/route";
import { DELETE as trashDELETE } from "@/app/api/records/trash/route";
import { GET as recordsGET } from "@/app/api/records/route";
import {
  GET as recordGET,
  PATCH as recordPATCH,
  DELETE as recordDELETE,
} from "@/app/api/records/[id]/route";
import { POST as restorePOST } from "@/app/api/records/[id]/restore/route";
import { PATCH as orderPATCH } from "@/app/api/records/[id]/order/route";
import { GET as syncVersionGET } from "@/app/api/sync/version/route";
import { GET as syncChangesGET } from "@/app/api/sync/changes/route";
import { GET as projectsGET } from "@/app/api/projects/route";
import { GET as filesGET } from "@/app/api/files/[recordId]/[...path]/route";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type Contract = Record<string, "string" | "number" | "boolean" | "null" | "object" | "string|null" | "number|null">;

/**
 * Asserts that `body` exactly matches `contract`:
 *  - all expected fields are present with the expected type, and
 *  - no unexpected fields are present.
 *
 * The `contract` maps field name → expected JS type string (or union for nullable).
 */
function assertExactShape(body: Record<string, unknown>, contract: Contract): void {
  // Every expected field is present with correct type
  for (const [field, expectedType] of Object.entries(contract)) {
    expect(body, `field "${field}" is missing`).toHaveProperty(field);
    const value = body[field];
    if (expectedType === "string|null") {
      expect(
        typeof value === "string" || value === null,
        `field "${field}" expected string|null, got ${typeof value}`
      ).toBe(true);
    } else if (expectedType === "number|null") {
      expect(
        typeof value === "number" || value === null,
        `field "${field}" expected number|null, got ${typeof value}`
      ).toBe(true);
    } else {
      // simple type
      if (expectedType === "null") {
        expect(value, `field "${field}" expected null`).toBeNull();
      } else {
        expect(typeof value, `field "${field}" type mismatch`).toBe(expectedType);
      }
    }
  }

  // No unexpected fields are present
  const extraFields = Object.keys(body).filter((k) => !(k in contract));
  expect(extraFields, `unexpected fields in response`).toHaveLength(0);
}

/**
 * Returns route context with a single `id` param, as expected by records/[id] routes.
 */
function recordCtx(id: string) {
  return { params: Promise.resolve({ id }) };
}

const RECORD_ID = "20260310-aaaaaaaa";

// Canonical record row returned by DB mocks.
const RECORD_ROW = {
  id: RECORD_ID,
  date: "2026-03-10",
  day_order: "a0",
  html_content: null,
  notes: null,
  project_id: "org/proj",
  source_device_id: "device-a",
  source_ref: null,
  git_remote_url: null,
  git_hash: null,
  created_at: "2026-03-10T12:00:00.000Z",
  updated_at: "2026-03-10T12:00:00.000Z",
  deleted_at: null,
};

// Canonical sync_version row.
const SYNC_VERSION_ROW = { version: 5, updated_at: "2026-03-10T12:00:00.000Z" };

// ---------------------------------------------------------------------------
// Contract: GET /api/info
// Go parity: TestHandleInfo in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: GET /api/info", () => {
  it("response shape matches { mode: string, version: string }", async () => {
    const req = new NextRequest("http://localhost/api/info");
    const res = await infoGET(req);

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      mode: "string",
      version: "string",
    });

    // mode must be a recognised literal (both sides only produce "local" | "cloud")
    expect(["local", "cloud"]).toContain(body.mode);
  });
});

// ---------------------------------------------------------------------------
// Contract: GET /api/stats
// Go parity: TestHandleStats, TestHandleStats_Empty in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: GET /api/stats", () => {
  beforeEach(() => {
    mockSql.mockReset();
  });

  it("response shape matches { total_records: number, total_projects: number, trashed_records: number }", async () => {
    mockSql
      .mockResolvedValueOnce([{ count: 10 }])
      .mockResolvedValueOnce([{ count: 3 }])
      .mockResolvedValueOnce([{ count: 2 }]);

    const req = new NextRequest("http://localhost/api/stats");
    const res = await statsGET(req);

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      total_records: "number",
      total_projects: "number",
      trashed_records: "number",
    });
  });
});

// ---------------------------------------------------------------------------
// Contract: DELETE /api/records/trash
// Go parity: TestHandlePurgeTrash, TestHandlePurgeTrash_Empty in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: DELETE /api/records/trash", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockTransaction.mockReset();
    mockBumpS3Version.mockReset();
    mockDeleteS3Objects.mockReset();
    mockBumpS3Version.mockResolvedValue(undefined);
    mockDeleteS3Objects.mockResolvedValue(undefined);
  });

  it("response shape matches { purged_count: number, sync_version: number }", async () => {
    // 4 tagged-template calls inside the transaction array create query objects
    for (let i = 0; i < 4; i++) {
      mockSql.mockReturnValueOnce({});
    }
    // Transaction resolves with: [countResult, figureKeys, dataFileKeys, deleteResult]
    mockTransaction.mockResolvedValueOnce([
      [{ count: 1 }],
      [],
      [],
      [],
    ]);
    // Sync version query (outside transaction)
    mockSql.mockResolvedValueOnce([SYNC_VERSION_ROW]);

    const req = new NextRequest("http://localhost/api/records/trash", {
      method: "DELETE",
    });
    const res = await trashDELETE(req);

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      purged_count: "number",
      sync_version: "number",
    });
  });
});

// ---------------------------------------------------------------------------
// Contract: GET /api/records
// Go parity: TestListRecords_Empty, TestListRecords_SortOrder, TestListRecords_Pagination
//            in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: GET /api/records", () => {
  beforeEach(() => {
    mockSql.mockReset();
  });

  it("response shape matches { items: RecordSummary[], next_cursor: string|null }", async () => {
    const summaryRow = {
      id: RECORD_ID,
      date: "2026-03-10",
      day_order: "a0",
      html_content: "<p>test</p>",
      project_id: "org/proj",
      source_device_id: "device-a",
      source_ref: null,
      updated_at: "2026-03-10T12:00:00.000Z",
      deleted_at: null,
      figure_count: 0,
      data_file_count: 0,
    };
    // mockSql is used with a dynamic call signature; return one row
    mockSql.mockResolvedValueOnce([summaryRow]);

    const req = new NextRequest("http://localhost/api/records");
    const res = await recordsGET(req);

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    // Top-level shape
    assertExactShape(body, {
      items: "object", // array is typeof "object"
      next_cursor: "string|null",
    });

    expect(Array.isArray(body.items)).toBe(true);

    // Verify each item in the array conforms to the RecordSummary contract
    const items = body.items as Record<string, unknown>[];
    for (const item of items) {
      assertExactShape(item, {
        id: "string",
        date: "string",
        day_order: "string",
        html_content: "string|null",
        project_id: "string",
        source_device_id: "string",
        source_ref: "string|null",
        updated_at: "string",
        deleted_at: "string|null",
        figure_count: "number",
        data_file_count: "number",
      });
    }
  });
});

// ---------------------------------------------------------------------------
// Contract: GET /api/records/[id]
// Go parity: TestGetRecord_Valid in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: GET /api/records/[id]", () => {
  beforeEach(() => {
    mockSql.mockReset();
  });

  it("response shape matches { record: RecordDetail }", async () => {
    mockSql
      .mockResolvedValueOnce([RECORD_ROW]) // record row
      .mockResolvedValueOnce([]) // figures
      .mockResolvedValueOnce([]); // data files

    const req = new NextRequest(`http://localhost/api/records/${RECORD_ID}`);
    const res = await recordGET(req, recordCtx(RECORD_ID));

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      record: "object",
    });

    const record = body.record as Record<string, unknown>;
    assertExactShape(record, {
      id: "string",
      date: "string",
      day_order: "string",
      html_content: "string|null",
      notes: "string|null",
      project_id: "string",
      source_device_id: "string",
      source_ref: "string|null",
      git_remote_url: "string|null",
      git_hash: "string|null",
      created_at: "string",
      updated_at: "string",
      deleted_at: "string|null",
      figures: "object",   // array
      data_files: "object", // array
    });

    expect(Array.isArray(record.figures)).toBe(true);
    expect(Array.isArray(record.data_files)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Contract: PATCH /api/records/[id]
// Go parity: TestPatchRecord_UpdateProjectID in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: PATCH /api/records/[id]", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockBumpS3Version.mockReset();
    mockBumpS3Version.mockResolvedValue(undefined);
  });

  it("response shape matches { record: RecordDetail, sync_version: number }", async () => {
    mockSql
      .mockResolvedValueOnce([{ ...RECORD_ROW, project_id: "proj-a" }]) // UPDATE RETURNING *
      .mockResolvedValueOnce([SYNC_VERSION_ROW]) // SELECT version
      .mockResolvedValueOnce([]) // figures
      .mockResolvedValueOnce([]); // data_files

    const req = new NextRequest(
      `http://localhost/api/records/${RECORD_ID}`,
      {
        method: "PATCH",
        body: JSON.stringify({ project_id: "proj-a" }),
        headers: { "content-type": "application/json" },
      }
    );
    const res = await recordPATCH(req, recordCtx(RECORD_ID));

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      record: "object",
      sync_version: "number",
    });

    const record = body.record as Record<string, unknown>;
    assertExactShape(record, {
      id: "string",
      date: "string",
      day_order: "string",
      html_content: "string|null",
      notes: "string|null",
      project_id: "string",
      source_device_id: "string",
      source_ref: "string|null",
      git_remote_url: "string|null",
      git_hash: "string|null",
      created_at: "string",
      updated_at: "string",
      deleted_at: "string|null",
      figures: "object",
      data_files: "object",
    });
  });
});

// ---------------------------------------------------------------------------
// Contract: DELETE /api/records/[id]  (soft delete)
// Go parity: TestDeleteRecord_Success in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: DELETE /api/records/[id]", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockBumpS3Version.mockReset();
    mockBumpS3Version.mockResolvedValue(undefined);
  });

  it("response shape matches { id: string, deleted_at: string, updated_at: string, sync_version: number }", async () => {
    mockSql
      .mockResolvedValueOnce([
        {
          id: RECORD_ID,
          deleted_at: "2026-03-10T13:00:00.000Z",
          updated_at: "2026-03-10T13:00:00.000Z",
        },
      ]) // UPDATE RETURNING
      .mockResolvedValueOnce([SYNC_VERSION_ROW]); // sync version

    const req = new NextRequest(
      `http://localhost/api/records/${RECORD_ID}`,
      { method: "DELETE" }
    );
    const res = await recordDELETE(req, recordCtx(RECORD_ID));

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      id: "string",
      deleted_at: "string",
      updated_at: "string",
      sync_version: "number",
    });
  });
});

// ---------------------------------------------------------------------------
// Contract: POST /api/records/[id]/restore
// Go parity: TestRestoreRecord_Success in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: POST /api/records/[id]/restore", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockBumpS3Version.mockReset();
    mockBumpS3Version.mockResolvedValue(undefined);
  });

  it("response shape matches { id: string, deleted_at: null, updated_at: string, sync_version: number }", async () => {
    mockSql
      .mockResolvedValueOnce([
        {
          id: RECORD_ID,
          deleted_at: null,
          updated_at: "2026-03-10T14:00:00.000Z",
        },
      ]) // UPDATE RETURNING
      .mockResolvedValueOnce([SYNC_VERSION_ROW]); // sync version

    const req = new NextRequest(
      `http://localhost/api/records/${RECORD_ID}/restore`,
      { method: "POST" }
    );
    const res = await restorePOST(req, recordCtx(RECORD_ID));

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      id: "string",
      deleted_at: "null",
      updated_at: "string",
      sync_version: "number",
    });
  });
});

// ---------------------------------------------------------------------------
// Contract: PATCH /api/records/[id]/order
// Go parity: TestReorderRecord_Last in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: PATCH /api/records/[id]/order", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockBumpS3Version.mockReset();
    mockBumpS3Version.mockResolvedValue(undefined);
  });

  it("response shape matches { id: string, date: string, day_order: string, updated_at: string, sync_version: number }", async () => {
    mockSql
      .mockResolvedValueOnce([
        { id: RECORD_ID, date: "2026-03-10", day_order: "a0" },
      ]) // SELECT current record
      .mockResolvedValueOnce([]) // SELECT siblings
      .mockResolvedValueOnce([
        {
          id: RECORD_ID,
          date: "2026-03-10",
          day_order: "a1",
          updated_at: "2026-03-10T15:00:00.000Z",
        },
      ]) // UPDATE RETURNING
      .mockResolvedValueOnce([SYNC_VERSION_ROW]); // sync version

    const req = new NextRequest(
      `http://localhost/api/records/${RECORD_ID}/order`,
      {
        method: "PATCH",
        body: JSON.stringify({ position: { kind: "last" } }),
        headers: { "content-type": "application/json" },
      }
    );
    const res = await orderPATCH(req, recordCtx(RECORD_ID));

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      id: "string",
      date: "string",
      day_order: "string",
      updated_at: "string",
      sync_version: "number",
    });
  });
});

// ---------------------------------------------------------------------------
// Contract: GET /api/sync/version
// Go parity: TestSyncVersion in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: GET /api/sync/version", () => {
  beforeEach(() => {
    mockGetS3Version.mockReset();
  });

  it("response shape matches { version: number, updated_at: string }", async () => {
    mockGetS3Version.mockResolvedValueOnce({
      version: 42,
      updated_at: "2026-03-10T12:00:00.000Z",
    });

    const req = new NextRequest("http://localhost/api/sync/version");
    const res = await syncVersionGET(req);

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      version: "number",
      updated_at: "string",
    });
  });
});

// ---------------------------------------------------------------------------
// Contract: GET /api/sync/changes
// Go parity: TestSyncChanges_ReturnsChangedRecords in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: GET /api/sync/changes", () => {
  beforeEach(() => {
    mockSql.mockReset();
  });

  it("response shape matches { items: RecordSummary[], server_now: string }", async () => {
    const summaryRow = {
      id: RECORD_ID,
      date: "2026-03-10",
      day_order: "a0",
      html_content: "<p>test</p>",
      project_id: "org/proj",
      source_device_id: "device-a",
      source_ref: "vault://source",
      updated_at: "2026-03-10T12:00:00.000Z",
      deleted_at: null,
      figure_count: 0,
      data_file_count: 0,
    };

    // sync/changes route calls sql`SELECT NOW()` then sql.query(...)
    mockSql
      .mockResolvedValueOnce([{ server_now: "2026-03-10T13:00:00.000Z" }]); // SELECT NOW()

    // The second call uses sql.query (not tagged template), so attach it as .query
    (mockSql as unknown as Record<string, unknown>).query = vi.fn().mockResolvedValueOnce([summaryRow]);

    const req = new NextRequest(
      "http://localhost/api/sync/changes?since=2026-03-10T00:00:00Z"
    );
    const res = await syncChangesGET(req);

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      items: "object",
      server_now: "string",
    });

    expect(Array.isArray(body.items)).toBe(true);

    const items = body.items as Record<string, unknown>[];
    for (const item of items) {
      assertExactShape(item, {
        id: "string",
        date: "string",
        day_order: "string",
        html_content: "string|null",
        project_id: "string",
        source_device_id: "string",
        source_ref: "string|null",
        updated_at: "string",
        deleted_at: "string|null",
        figure_count: "number",
        data_file_count: "number",
      });
    }
  });
});

// ---------------------------------------------------------------------------
// Contract: GET /api/projects
// Go parity: TestListProjects_WithProjects, TestListProjects_Empty in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: GET /api/projects", () => {
  beforeEach(() => {
    mockSql.mockReset();
  });

  it("response shape matches { projects: string[] }", async () => {
    mockSql.mockResolvedValueOnce([
      { id: "alpha" },
      { id: "beta" },
    ]);

    const req = new NextRequest("http://localhost/api/projects");
    const res = await projectsGET(req);

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      projects: "object", // array
    });

    expect(Array.isArray(body.projects)).toBe(true);
    const projects = body.projects as unknown[];
    for (const p of projects) {
      expect(typeof p).toBe("string");
    }
  });
});

// ---------------------------------------------------------------------------
// Contract: GET /api/files/[recordId]/[...path]
// Go parity: TestGetFile_FigureFound, TestGetFile_DataFileFound in server_test.go
// ---------------------------------------------------------------------------

describe("Contract: GET /api/files/[recordId]/[...path]", () => {
  beforeEach(() => {
    mockSql.mockReset();
    mockGetPresignedUrl.mockReset();
  });

  it("response shape matches { url: string, expires_at: string }", async () => {
    // mockSql is used with a dynamic function call (sql(query, params))
    mockSql.mockResolvedValueOnce([{ s3_key: "figures/20260310-aaaaaaaa/fig.png" }]);
    mockGetPresignedUrl.mockResolvedValueOnce({
      url: "https://bucket.s3.example.com/figures/20260310-aaaaaaaa/fig.png?sig=abc",
      expires_at: "2026-03-10T13:00:00.000Z",
    });

    const req = new NextRequest(
      `http://localhost/api/files/${RECORD_ID}/figures/fig.png`
    );
    const ctx = {
      params: Promise.resolve({
        recordId: RECORD_ID,
        path: ["figures", "fig.png"],
      }),
    };
    const res = await filesGET(req, ctx);

    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;

    assertExactShape(body, {
      url: "string",
      expires_at: "string",
    });
  });
});
