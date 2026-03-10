import { describe, expect, it, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";

const mockSql = vi.fn();
vi.mock("@/lib/db", () => ({
  getDb: () => mockSql,
}));

import { GET } from "@/app/api/projects/route";

describe("GET /api/projects", () => {
  beforeEach(() => {
    mockSql.mockReset();
  });

  it("returns distinct project IDs", async () => {
    mockSql.mockResolvedValue([
      { project_id: "org/alpha" },
      { project_id: "org/beta" },
    ]);

    const req = new NextRequest("http://localhost/api/projects");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.projects).toEqual(["org/alpha", "org/beta"]);
  });

  it("excludes deleted slides from project list", async () => {
    mockSql.mockResolvedValue([{ project_id: "org/active" }]);

    const req = new NextRequest("http://localhost/api/projects");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.projects).toEqual(["org/active"]);

    // Verify the SQL query filters deleted_at IS NULL
    const callArgs = mockSql.mock.calls[0];
    // Tagged template: first arg is TemplateStringsArray
    const queryParts = callArgs[0] as TemplateStringsArray;
    const fullQuery = queryParts.join("");
    expect(fullQuery).toContain("deleted_at IS NULL");
  });

  it("returns empty array when no projects exist", async () => {
    mockSql.mockResolvedValue([]);

    const req = new NextRequest("http://localhost/api/projects");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.projects).toEqual([]);
  });

  it("returns 500 on unexpected database error", async () => {
    mockSql.mockRejectedValue(new Error("connection refused"));

    const req = new NextRequest("http://localhost/api/projects");
    const res = await GET(req);

    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
  });
});
