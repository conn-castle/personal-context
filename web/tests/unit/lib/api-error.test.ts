import { describe, expect, it } from "vitest";
import {
  apiError,
  notFound,
  badRequest,
  invalidId,
  internalError,
} from "@/lib/api-error";

describe("API error helpers", () => {
  it("apiError returns response with correct status and body", async () => {
    const res = apiError(418, "BAD_REQUEST", "teapot");
    expect(res.status).toBe(418);
    const body = await res.json();
    expect(body).toEqual({ error: "teapot", code: "BAD_REQUEST" });
  });

  it("notFound returns 404 with NOT_FOUND code", async () => {
    const res = notFound("record not found");
    expect(res.status).toBe(404);
    const body = await res.json();
    expect(body.code).toBe("NOT_FOUND");
    expect(body.error).toBe("record not found");
  });

  it("badRequest returns 400 with BAD_REQUEST code", async () => {
    const res = badRequest("missing field");
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("BAD_REQUEST");
    expect(body.error).toBe("missing field");
  });

  it("invalidId returns 400 with INVALID_ID code containing the ID", async () => {
    const res = invalidId("bad-id");
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.code).toBe("INVALID_ID");
    expect(body.error).toContain("bad-id");
  });

  it("internalError returns 500 with INTERNAL_ERROR code", async () => {
    const res = internalError("unexpected");
    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body.code).toBe("INTERNAL_ERROR");
    expect(body.error).toBe("unexpected");
  });

  it("response has application/json content type", () => {
    const res = notFound("test");
    expect(res.headers.get("content-type")).toContain("application/json");
  });
});
