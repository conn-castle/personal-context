import { describe, expect, it } from "vitest";
import { NextRequest } from "next/server";

import { GET } from "@/app/api/info/route";
import packageMetadata from "../../../package.json";

describe("GET /api/info", () => {
  it("returns cloud mode and version", async () => {
    const req = new NextRequest("http://localhost/api/info");
    const res = await GET(req);

    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.mode).toBe("cloud");
    expect(body.version).toBe(packageMetadata.version);
  });
});
