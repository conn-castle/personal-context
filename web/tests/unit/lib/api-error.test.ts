import { describe, expect, it } from "vitest";
import {
  apiError,
  notFound,
  badRequest,
  invalidId,
  internalError,
  unauthorized,
  conflict,
  invalidConfig,
  localBackendUnavailable,
  localModeAuthDisabled,
  registrationDisabled,
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

  it("unauthorized returns 401 with UNAUTHORIZED code and default message", async () => {
    const res = unauthorized();
    expect(res.status).toBe(401);
    const body = await res.json();
    expect(body).toEqual({ error: "Unauthorized", code: "UNAUTHORIZED" });
  });

  it("unauthorized accepts a custom message", async () => {
    const res = unauthorized("Invalid or revoked API key");
    expect(res.status).toBe(401);
    const body = await res.json();
    expect(body.code).toBe("UNAUTHORIZED");
    expect(body.error).toBe("Invalid or revoked API key");
  });

  it("conflict returns 409 with CONFLICT code", async () => {
    const res = conflict("already exists");
    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body.code).toBe("CONFLICT");
    expect(body.error).toBe("already exists");
  });

  it("invalidConfig returns 500 with INVALID_CONFIG code and default message", async () => {
    const res = invalidConfig();
    expect(res.status).toBe(500);
    const body = await res.json();
    expect(body).toEqual({
      error: "Invalid LOCAL_BACKEND_URL configuration",
      code: "INVALID_CONFIG",
    });
  });

  it("localBackendUnavailable returns 502 with LOCAL_BACKEND_UNAVAILABLE code", async () => {
    const res = localBackendUnavailable();
    expect(res.status).toBe(502);
    const body = await res.json();
    expect(body).toEqual({
      error: "Local backend unavailable",
      code: "LOCAL_BACKEND_UNAVAILABLE",
    });
  });

  it("localModeAuthDisabled returns 403 with LOCAL_MODE_AUTH_DISABLED code", async () => {
    const res = localModeAuthDisabled("Authentication is unavailable in local mode.");
    expect(res.status).toBe(403);
    const body = await res.json();
    expect(body.code).toBe("LOCAL_MODE_AUTH_DISABLED");
    expect(body.error).toBe("Authentication is unavailable in local mode.");
  });

  it("registrationDisabled returns 403 with REGISTRATION_DISABLED code and default message", async () => {
    const res = registrationDisabled();
    expect(res.status).toBe(403);
    const body = await res.json();
    expect(body).toEqual({
      error: "Registration is disabled.",
      code: "REGISTRATION_DISABLED",
    });
  });

  it("response has application/json content type", () => {
    const res = notFound("test");
    expect(res.headers.get("content-type")).toContain("application/json");
  });
});
