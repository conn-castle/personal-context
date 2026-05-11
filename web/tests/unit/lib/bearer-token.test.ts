import { describe, expect, it } from "vitest";
import { extractBearerToken } from "@/lib/bearer-token";

describe("extractBearerToken", () => {
  it("returns null for absent, blank, or non-bearer headers", () => {
    expect(extractBearerToken(null)).toBeNull();
    expect(extractBearerToken("   ")).toBeNull();
    expect(extractBearerToken("Basic abc")).toBeNull();
    expect(extractBearerToken("Bearer    ")).toBeNull();
  });

  it("extracts bearer tokens case-insensitively", () => {
    expect(extractBearerToken("Bearer token-123")).toBe("token-123");
    expect(extractBearerToken("  bearer   token-456  ")).toBe("token-456");
  });
});
