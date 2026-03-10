import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { fetchSlideFileUrl } from "@/lib/file-url";

describe("fetchSlideFileUrl", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it("returns the resolved presigned URL", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          url: "https://signed.example.com/figure.png",
          expires_at: "2026-03-09T15:00:00Z",
        }),
    }) as typeof globalThis.fetch;

    await expect(
      fetchSlideFileUrl("20260309-aabbccdd", "figures", "figure.png")
    ).resolves.toBe("https://signed.example.com/figure.png");

    expect(globalThis.fetch).toHaveBeenCalledWith(
      "/api/files/20260309-aabbccdd/figures/figure.png"
    );
  });

  it("throws on non-OK responses", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      json: () => Promise.resolve({}),
    }) as typeof globalThis.fetch;

    await expect(
      fetchSlideFileUrl("20260309-aabbccdd", "data", "results.csv")
    ).rejects.toThrow("Failed to resolve data file: 404");
  });

  it("throws when the API payload is missing the URL", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ expires_at: "2026-03-09T15:00:00Z" }),
    }) as typeof globalThis.fetch;

    await expect(
      fetchSlideFileUrl("20260309-aabbccdd", "figures", "figure.png")
    ).rejects.toThrow("Invalid figures file URL response");
  });
});
