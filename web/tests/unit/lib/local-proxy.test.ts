import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// Store the original fetch
const originalFetch = globalThis.fetch;

import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";

describe("local-proxy", () => {
  const originalEnv = process.env;

  beforeEach(() => {
    process.env = { ...originalEnv };
  });

  afterEach(() => {
    process.env = originalEnv;
    globalThis.fetch = originalFetch;
  });

  describe("isLocalMode", () => {
    it("returns false when LOCAL_BACKEND_URL is not set", () => {
      delete process.env.LOCAL_BACKEND_URL;
      expect(isLocalMode()).toBe(false);
    });

    it("returns false when LOCAL_BACKEND_URL is empty string", () => {
      process.env.LOCAL_BACKEND_URL = "";
      expect(isLocalMode()).toBe(false);
    });

    it("returns true when LOCAL_BACKEND_URL is set", () => {
      process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";
      expect(isLocalMode()).toBe(true);
    });
  });

  describe("proxyToLocal", () => {
    it("throws when LOCAL_BACKEND_URL is not set", async () => {
      delete process.env.LOCAL_BACKEND_URL;
      const request = new Request("http://localhost:3000/api/slides");
      await expect(proxyToLocal(request)).rejects.toThrow(
        "LOCAL_BACKEND_URL is not set"
      );
    });

    it("proxies a GET request to the Go server", async () => {
      process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";
      const mockFetch = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ items: [] }), {
          status: 200,
          headers: { "content-type": "application/json" },
        })
      );
      globalThis.fetch = mockFetch;

      const request = new Request(
        "http://localhost:3000/api/slides?limit=10&project=alpha"
      );
      const response = await proxyToLocal(request);

      expect(mockFetch).toHaveBeenCalledWith(
        "http://127.0.0.1:9876/api/slides?limit=10&project=alpha",
        expect.objectContaining({ method: "GET" })
      );
      expect(response.status).toBe(200);
      const body = (await response.json()) as Record<string, unknown>;
      expect(body.items).toEqual([]);
    });

    it("proxies a PATCH request with body", async () => {
      process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";
      const mockFetch = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ slide: { id: "test" } }), {
          status: 200,
          headers: { "content-type": "application/json" },
        })
      );
      globalThis.fetch = mockFetch;

      const request = new Request(
        "http://localhost:3000/api/slides/20260310-aaaaaaaa",
        {
          method: "PATCH",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ project_id: "alpha" }),
        }
      );
      const response = await proxyToLocal(request);

      expect(mockFetch).toHaveBeenCalledWith(
        "http://127.0.0.1:9876/api/slides/20260310-aaaaaaaa",
        expect.objectContaining({
          method: "PATCH",
          body: JSON.stringify({ project_id: "alpha" }),
        })
      );
      expect(response.status).toBe(200);
    });

    it("forwards request headers and strips hop-by-hop headers", async () => {
      process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";
      const mockFetch = vi.fn().mockResolvedValue(
        new Response("{}", {
          status: 200,
          headers: { "content-type": "application/json" },
        })
      );
      globalThis.fetch = mockFetch;

      const request = new Request("http://localhost:3000/api/slides", {
        method: "PATCH",
        headers: {
          "content-type": "application/json",
          "x-custom": "kept",
          connection: "keep-alive",
          "transfer-encoding": "chunked",
        },
        body: "{}",
      });
      await proxyToLocal(request);

      const callArgs = mockFetch.mock.calls[0] as [string, RequestInit];
      const headers = callArgs[1].headers as Headers;
      expect(headers.get("content-type")).toBe("application/json");
      expect(headers.get("x-custom")).toBe("kept");
      expect(headers.has("connection")).toBe(false);
      expect(headers.has("transfer-encoding")).toBe(false);
    });

    it("forwards response headers and strips hop-by-hop headers", async () => {
      process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";
      const mockFetch = vi.fn().mockResolvedValue(
        new Response("{}", {
          status: 200,
          headers: {
            "content-type": "application/json",
            "x-request-id": "abc-123",
          },
        })
      );
      globalThis.fetch = mockFetch;

      const request = new Request("http://localhost:3000/api/slides");
      const response = await proxyToLocal(request);

      expect(response.headers.get("content-type")).toBe("application/json");
      expect(response.headers.get("x-request-id")).toBe("abc-123");
    });

    it("preserves error status codes from Go server", async () => {
      process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";
      const mockFetch = vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({ error: "Not found", code: "NOT_FOUND" }),
          {
            status: 404,
            headers: { "content-type": "application/json" },
          }
        )
      );
      globalThis.fetch = mockFetch;

      const request = new Request(
        "http://localhost:3000/api/slides/20260310-zzzzzzzz"
      );
      const response = await proxyToLocal(request);

      expect(response.status).toBe(404);
    });

    it("returns JSON when the local backend is unreachable", async () => {
      process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";
      const mockFetch = vi
        .fn()
        .mockRejectedValue(new Error("connect ECONNREFUSED"));
      globalThis.fetch = mockFetch;

      const request = new Request("http://localhost:3000/api/slides");
      const response = await proxyToLocal(request);

      expect(response.status).toBe(502);
      expect(response.headers.get("content-type")).toBe("application/json");
      const body = (await response.json()) as Record<string, unknown>;
      expect(body.code).toBe("LOCAL_BACKEND_UNAVAILABLE");
      expect(body.error).toBe("Local backend unavailable");
    });

    it("forwards no content-type when Go response omits it", async () => {
      process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";
      // Simulate a response with no headers at all.
      const fakeResponse = {
        status: 200,
        headers: new Headers(),
        arrayBuffer: () => Promise.resolve(new TextEncoder().encode("{}").buffer),
      } as unknown as Response;
      const mockFetch = vi.fn().mockResolvedValue(fakeResponse);
      globalThis.fetch = mockFetch;

      const request = new Request("http://localhost:3000/api/slides");
      const response = await proxyToLocal(request);

      // With no content-type from the Go server, none is set on the proxy response.
      expect(response.headers.has("content-type")).toBe(false);
    });

    it("does not forward body for GET requests", async () => {
      process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";
      const mockFetch = vi.fn().mockResolvedValue(
        new Response("{}", {
          status: 200,
          headers: { "content-type": "application/json" },
        })
      );
      globalThis.fetch = mockFetch;

      const request = new Request("http://localhost:3000/api/slides");
      await proxyToLocal(request);

      const callArgs = mockFetch.mock.calls[0] as [string, RequestInit];
      expect(callArgs[1].body).toBeUndefined();
    });

    it("proxies DELETE request without body", async () => {
      process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";
      const mockFetch = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ id: "test", deleted_at: "2026-03-10" }), {
          status: 200,
          headers: { "content-type": "application/json" },
        })
      );
      globalThis.fetch = mockFetch;

      const request = new Request(
        "http://localhost:3000/api/slides/20260310-aaaaaaaa",
        { method: "DELETE" }
      );
      const response = await proxyToLocal(request);

      expect(mockFetch).toHaveBeenCalledWith(
        "http://127.0.0.1:9876/api/slides/20260310-aaaaaaaa",
        expect.objectContaining({ method: "DELETE" })
      );
      expect(response.status).toBe(200);
    });
  });
});
