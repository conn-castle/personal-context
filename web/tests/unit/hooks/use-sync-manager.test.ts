// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useSyncManager } from "@/hooks/use-sync-manager";
import type { SyncVersionResponse, SyncChangesResponse } from "@/lib/types";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function mockFetchResponses(
  versionRes: SyncVersionResponse,
  changesRes?: SyncChangesResponse
) {
  const fetchMock = vi.fn();
  fetchMock.mockImplementation((url: string) => {
    if (url.includes("/api/sync/version")) {
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve(versionRes),
      });
    }
    if (url.includes("/api/sync/changes")) {
      return Promise.resolve({
        ok: true,
        json: () =>
          Promise.resolve(
            changesRes ?? { items: [], server_now: "2026-03-09T12:00:00Z" }
          ),
      });
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`));
  });
  return fetchMock;
}

// ---------------------------------------------------------------------------
// Test suite
// ---------------------------------------------------------------------------

describe("useSyncManager", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    vi.useFakeTimers();
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    vi.useRealTimers();
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  // -------------------------------------------------------------------------
  // Layer 1: Manual refresh — always fires, ignores cooldown
  // -------------------------------------------------------------------------
  describe("Layer 1: Manual refresh", () => {
    it("fires immediately and fetches version", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useSyncManager());

      await act(async () => {
        await result.current.syncNow();
      });

      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/api/sync/version")
      );
    });

    it("ignores cooldown when called multiple times", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useSyncManager());

      await act(async () => {
        await result.current.syncNow();
      });
      await act(async () => {
        await result.current.syncNow();
      });

      // Should have called /api/sync/version twice (no cooldown blocking)
      const versionCalls = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      );
      expect(versionCalls).toHaveLength(2);
    });

    it("calls onSyncData when version changes", async () => {
      const changesData: SyncChangesResponse = {
        items: [
          {
            id: "20260309-aabbccdd",
            date: "2026-03-09",
            day_order: "a0",
            html_content: "<p>Test content</p>",
            project_id: "org/proj",
            source_device_id: "device-a",
            source_ref: null,
            updated_at: "2026-03-09T10:00:00Z",
            deleted_at: null,
            figure_count: 0,
            data_file_count: 0,
          },
        ],
        server_now: "2026-03-09T12:00:00Z",
      };
      const fetchMock = mockFetchResponses(
        { version: 1, updated_at: "2026-03-09T10:00:00Z" },
        changesData
      );
      globalThis.fetch = fetchMock;

      const onSyncData = vi.fn();
      const { result } = renderHook(() =>
        useSyncManager({ onSyncData })
      );

      // First call: establishes baseline version
      await act(async () => {
        await result.current.syncNow();
      });
      expect(onSyncData).toHaveBeenCalledWith({
        items: [],
        server_now: "2026-03-09T10:00:00Z",
      });
      onSyncData.mockClear();

      // Update mock to return a new version
      fetchMock.mockImplementation((url: string) => {
        if (url.includes("/api/sync/version")) {
          return Promise.resolve({
            ok: true,
            json: () =>
              Promise.resolve({ version: 2, updated_at: "2026-03-09T11:00:00Z" }),
          });
        }
        if (url.includes("/api/sync/changes")) {
          return Promise.resolve({
            ok: true,
            json: () => Promise.resolve(changesData),
          });
        }
        return Promise.reject(new Error(`Unexpected fetch: ${url}`));
      });

      // Second call: version changed, should fetch changes
      await act(async () => {
        await result.current.syncNow();
      });

      expect(onSyncData).toHaveBeenCalledTimes(1);
      expect(onSyncData).toHaveBeenCalledWith(changesData);
    });

    it("updates version state after sync", async () => {
      const fetchMock = mockFetchResponses({
        version: 42,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useSyncManager());

      await act(async () => {
        await result.current.syncNow();
      });

      expect(result.current.version).toBe(42);
    });

    it("sets isSyncing during sync", async () => {
      let resolveVersion: (v: unknown) => void;
      const fetchMock = vi.fn().mockImplementation((url: string) => {
        if (url.includes("/api/sync/version")) {
          return new Promise((resolve) => {
            resolveVersion = resolve;
          });
        }
        return Promise.reject(new Error("Unexpected"));
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useSyncManager());

      expect(result.current.isSyncing).toBe(false);

      let syncPromise: Promise<void>;
      act(() => {
        syncPromise = result.current.syncNow();
      });

      expect(result.current.isSyncing).toBe(true);

      await act(async () => {
        resolveVersion!({
          ok: true,
          json: () =>
            Promise.resolve({ version: 1, updated_at: "2026-03-09T10:00:00Z" }),
        });
        await syncPromise!;
      });

      expect(result.current.isSyncing).toBe(false);
    });
  });

  // -------------------------------------------------------------------------
  // Layer 2: Interaction-driven — respects 30s cooldown
  // -------------------------------------------------------------------------
  describe("Layer 2: Interaction-driven", () => {
    it("triggers sync on click", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      renderHook(() => useSyncManager());

      await act(async () => {
        document.dispatchEvent(new MouseEvent("click", { bubbles: true }));
        // Let microtasks resolve
        await vi.advanceTimersByTimeAsync(0);
      });

      const versionCalls = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      );
      expect(versionCalls.length).toBeGreaterThanOrEqual(1);
    });

    it("refreshes on the first observed version change from passive sync layers", async () => {
      const onSyncData = vi.fn();
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      renderHook(() => useSyncManager({ onSyncData }));

      await act(async () => {
        document.dispatchEvent(new MouseEvent("click", { bubbles: true }));
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(onSyncData).toHaveBeenCalledWith({
        items: [],
        server_now: "2026-03-09T10:00:00Z",
      });
    });

    it("respects cooldown — second click within cooldown is blocked", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      renderHook(() => useSyncManager({ cooldownMs: 30_000 }));

      // First click triggers a version check
      await act(async () => {
        document.dispatchEvent(new MouseEvent("click", { bubbles: true }));
        await vi.advanceTimersByTimeAsync(0);
      });

      const callsAfterFirst = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      ).length;
      expect(callsAfterFirst).toBeGreaterThanOrEqual(1);

      // Second click immediately — should be blocked by cooldown
      await act(async () => {
        document.dispatchEvent(new MouseEvent("click", { bubbles: true }));
        await vi.advanceTimersByTimeAsync(0);
      });

      const callsAfterSecond = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      ).length;
      // No additional version calls should have been made
      expect(callsAfterSecond).toBe(callsAfterFirst);
    });

    it("fires again after cooldown expires", async () => {
      // Use a very short cooldown so we don't trigger many idle polls
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      renderHook(() => useSyncManager({ cooldownMs: 100 }));

      // First click
      await act(async () => {
        document.dispatchEvent(new MouseEvent("click", { bubbles: true }));
        await vi.advanceTimersByTimeAsync(0);
      });

      const callsAfterFirst = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      ).length;

      // Advance past 100ms cooldown (short enough to not trigger idle poll)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(150);
      });

      // Second click — should fire because cooldown expired
      await act(async () => {
        document.dispatchEvent(new MouseEvent("click", { bubbles: true }));
        await vi.advanceTimersByTimeAsync(0);
      });

      const callsAfterSecond = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      ).length;
      expect(callsAfterSecond).toBeGreaterThan(callsAfterFirst);
    });
  });

  // -------------------------------------------------------------------------
  // Layer 3: Tab visibility — respects 30s cooldown
  // -------------------------------------------------------------------------
  describe("Layer 3: Tab visibility", () => {
    it("triggers sync when tab becomes visible", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      renderHook(() => useSyncManager());

      // Advance past cooldown so the visibility trigger fires
      await act(async () => {
        await vi.advanceTimersByTimeAsync(31_000);
      });

      // Simulate tab becoming visible
      Object.defineProperty(document, "visibilityState", {
        value: "visible",
        writable: true,
        configurable: true,
      });

      await act(async () => {
        document.dispatchEvent(new Event("visibilitychange"));
        await vi.advanceTimersByTimeAsync(0);
      });

      const versionCalls = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      );
      expect(versionCalls.length).toBeGreaterThanOrEqual(1);
    });

    it("respects 30s cooldown on tab visibility", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      renderHook(() => useSyncManager());

      // Trigger a click first to set the cooldown
      await act(async () => {
        document.dispatchEvent(new MouseEvent("click", { bubbles: true }));
        await vi.advanceTimersByTimeAsync(0);
      });

      const callsAfterClick = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      ).length;

      // Immediately trigger visibility — should be blocked
      Object.defineProperty(document, "visibilityState", {
        value: "visible",
        writable: true,
        configurable: true,
      });

      await act(async () => {
        document.dispatchEvent(new Event("visibilitychange"));
        await vi.advanceTimersByTimeAsync(0);
      });

      const callsAfterVisibility = fetchMock.mock.calls.filter(
        (c: unknown[]) => (c[0] as string).includes("/api/sync/version")
      ).length;
      expect(callsAfterVisibility).toBe(callsAfterClick);
    });

    it("does not trigger sync when tab becomes hidden", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      renderHook(() => useSyncManager());

      // Advance past cooldown
      await act(async () => {
        await vi.advanceTimersByTimeAsync(31_000);
      });

      // Make tab hidden
      Object.defineProperty(document, "visibilityState", {
        value: "hidden",
        writable: true,
        configurable: true,
      });

      const callsBefore = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      ).length;

      await act(async () => {
        document.dispatchEvent(new Event("visibilitychange"));
        await vi.advanceTimersByTimeAsync(0);
      });

      const callsAfter = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      ).length;
      expect(callsAfter).toBe(callsBefore);
    });
  });

  // -------------------------------------------------------------------------
  // Layer 4: Idle polling — adaptive timing, stops when tab hidden
  // -------------------------------------------------------------------------
  describe("Layer 4: Idle polling", () => {
    it("polls every 60s when idle < 10 minutes", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      // Set tab visible
      Object.defineProperty(document, "visibilityState", {
        value: "visible",
        writable: true,
        configurable: true,
      });

      renderHook(() => useSyncManager());

      // Wait for the first idle poll (60s)
      await act(async () => {
        await vi.advanceTimersByTimeAsync(60_000);
      });

      const versionCalls = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      );
      expect(versionCalls.length).toBeGreaterThanOrEqual(1);
    });

    it("polls every 5 minutes when idle > 10 minutes", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      Object.defineProperty(document, "visibilityState", {
        value: "visible",
        writable: true,
        configurable: true,
      });

      renderHook(() => useSyncManager());

      // Advance 11 minutes (past deep idle threshold) without any user activity
      await act(async () => {
        await vi.advanceTimersByTimeAsync(11 * 60_000);
      });

      // Reset call count
      fetchMock.mockClear();

      // Wait another 5 minutes
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5 * 60_000);
      });

      const versionCalls = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      );
      expect(versionCalls.length).toBeGreaterThanOrEqual(1);
    });

    it("stops polling when tab is hidden", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      // Start with visible
      Object.defineProperty(document, "visibilityState", {
        value: "visible",
        writable: true,
        configurable: true,
      });

      renderHook(() => useSyncManager());

      // Let first poll fire
      await act(async () => {
        await vi.advanceTimersByTimeAsync(60_000);
      });

      // Hide tab
      Object.defineProperty(document, "visibilityState", {
        value: "hidden",
        writable: true,
        configurable: true,
      });
      await act(async () => {
        document.dispatchEvent(new Event("visibilitychange"));
      });

      const callsBefore = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      ).length;

      // Advance another 60s — no poll should fire
      await act(async () => {
        await vi.advanceTimersByTimeAsync(60_000);
      });

      const callsAfter = fetchMock.mock.calls.filter((c: unknown[]) =>
        (c[0] as string).includes("/api/sync/version")
      ).length;
      expect(callsAfter).toBe(callsBefore);
    });
  });

  // -------------------------------------------------------------------------
  // Version change triggers data fetch
  // -------------------------------------------------------------------------
  describe("Version change triggers data fetch", () => {
    it("does not fetch changes when version is unchanged", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      const onSyncData = vi.fn();
      const { result } = renderHook(() =>
        useSyncManager({ onSyncData })
      );

      // First sync — establishes version
      await act(async () => {
        await result.current.syncNow();
      });
      expect(onSyncData).toHaveBeenCalledWith({
        items: [],
        server_now: "2026-03-09T10:00:00Z",
      });
      onSyncData.mockClear();

      // Second sync — same version
      await act(async () => {
        await result.current.syncNow();
      });

      // onSyncData should NOT be called (no version change)
      expect(onSyncData).not.toHaveBeenCalled();
    });
  });

  describe("HTTP error responses", () => {
    it("preserves sync state when /api/sync/version returns a non-OK response", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: () =>
          Promise.resolve({
            error: "Failed to fetch sync version",
            code: "INTERNAL_ERROR",
          }),
      });
      globalThis.fetch = fetchMock;

      const onSyncData = vi.fn();
      const { result } = renderHook(() => useSyncManager({ onSyncData }));

      await act(async () => {
        await result.current.syncNow();
      });

      expect(result.current.version).toBe(0);
      expect(result.current.lastSyncAt).toBeNull();
      expect(onSyncData).not.toHaveBeenCalled();
    });

    it("preserves the previous cursor when /api/sync/changes returns a non-OK response", async () => {
      let currentVersion = 1;
      const fetchMock = vi.fn().mockImplementation((url: string) => {
        if (url.includes("/api/sync/version")) {
          return Promise.resolve({
            ok: true,
            json: () =>
              Promise.resolve({
                version: currentVersion,
                updated_at: "2026-03-09T10:00:00Z",
              }),
          });
        }

        if (url.includes("/api/sync/changes")) {
          return Promise.resolve({
            ok: false,
            status: 500,
            json: () =>
              Promise.resolve({
                error: "Failed to fetch sync changes",
                code: "INTERNAL_ERROR",
              }),
          });
        }

        return Promise.reject(new Error(`Unexpected fetch: ${url}`));
      });
      globalThis.fetch = fetchMock;

      const onSyncData = vi.fn();
      const { result } = renderHook(() => useSyncManager({ onSyncData }));

      await act(async () => {
        await result.current.syncNow();
      });
      expect(result.current.version).toBe(1);
      expect(result.current.lastSyncAt).toBe("2026-03-09T10:00:00Z");
      onSyncData.mockClear();

      currentVersion = 2;

      await act(async () => {
        await result.current.syncNow();
      });

      expect(result.current.version).toBe(1);
      expect(result.current.lastSyncAt).toBe("2026-03-09T10:00:00Z");
      expect(onSyncData).not.toHaveBeenCalled();
    });

    it("ignores invalid 200 payloads from /api/sync/version", async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ version: "bad", updated_at: 123 }),
      });

      const onSyncData = vi.fn();
      const { result } = renderHook(() => useSyncManager({ onSyncData }));

      await act(async () => {
        await result.current.syncNow();
      });

      expect(result.current.version).toBe(0);
      expect(result.current.lastSyncAt).toBeNull();
      expect(onSyncData).not.toHaveBeenCalled();
    });
  });

  // -------------------------------------------------------------------------
  // Self-inflicted sync prevention
  // -------------------------------------------------------------------------
  describe("Self-inflicted sync prevention", () => {
    it("skips data fetch after markMutation when version changes", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      const onSyncData = vi.fn();
      const { result } = renderHook(() =>
        useSyncManager({ onSyncData })
      );

      // Establish baseline version
      await act(async () => {
        await result.current.syncNow();
      });
      expect(onSyncData).toHaveBeenCalledWith({
        items: [],
        server_now: "2026-03-09T10:00:00Z",
      });
      onSyncData.mockClear();

      // Mark a local mutation
      act(() => {
        result.current.markMutation();
      });

      // Update mock to return new version
      fetchMock.mockImplementation((url: string) => {
        if (url.includes("/api/sync/version")) {
          return Promise.resolve({
            ok: true,
            json: () =>
              Promise.resolve({
                version: 2,
                updated_at: "2026-03-09T11:00:00Z",
              }),
          });
        }
        if (url.includes("/api/sync/changes")) {
          return Promise.resolve({
            ok: true,
            json: () =>
              Promise.resolve({
                items: [],
                server_now: "2026-03-09T12:00:00Z",
              }),
          });
        }
        return Promise.reject(new Error("Unexpected"));
      });

      // Sync — version changed but self-inflicted, should skip data fetch
      await act(async () => {
        await result.current.syncNow();
      });

      expect(onSyncData).not.toHaveBeenCalled();
      // But version should still update
      expect(result.current.version).toBe(2);
    });

    it("fetches data normally after self-inflicted flag is consumed", async () => {
      const changesData: SyncChangesResponse = {
        items: [],
        server_now: "2026-03-09T13:00:00Z",
      };

      let currentVersion = 1;
      const fetchMock = vi.fn().mockImplementation((url: string) => {
        if (url.includes("/api/sync/version")) {
          return Promise.resolve({
            ok: true,
            json: () =>
              Promise.resolve({
                version: currentVersion,
                updated_at: "2026-03-09T10:00:00Z",
              }),
          });
        }
        if (url.includes("/api/sync/changes")) {
          return Promise.resolve({
            ok: true,
            json: () => Promise.resolve(changesData),
          });
        }
        return Promise.reject(new Error("Unexpected"));
      });
      globalThis.fetch = fetchMock;

      const onSyncData = vi.fn();
      const { result } = renderHook(() =>
        useSyncManager({ onSyncData })
      );

      // Establish baseline
      await act(async () => {
        await result.current.syncNow();
      });
      expect(onSyncData).toHaveBeenCalledWith({
        items: [],
        server_now: "2026-03-09T10:00:00Z",
      });
      onSyncData.mockClear();

      // Mark mutation and sync (consumes the flag)
      act(() => {
        result.current.markMutation();
      });
      currentVersion = 2;
      await act(async () => {
        await result.current.syncNow();
      });
      expect(onSyncData).not.toHaveBeenCalled();

      // Another version change — should fetch normally
      currentVersion = 3;
      await act(async () => {
        await result.current.syncNow();
      });
      expect(onSyncData).toHaveBeenCalledTimes(1);
    });
  });

  // -------------------------------------------------------------------------
  // Disabled state
  // -------------------------------------------------------------------------
  describe("Disabled state", () => {
    it("does not sync when enabled is false", async () => {
      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() =>
        useSyncManager({ enabled: false })
      );

      await act(async () => {
        await result.current.syncNow();
      });

      expect(fetchMock).not.toHaveBeenCalled();
    });
  });

  // -------------------------------------------------------------------------
  // Error handling
  // -------------------------------------------------------------------------
  describe("Error handling", () => {
    it("resets isSyncing after fetch error", async () => {
      const fetchMock = vi
        .fn()
        .mockRejectedValue(new Error("Network error"));
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useSyncManager());

      await act(async () => {
        await result.current.syncNow();
      });

      expect(result.current.isSyncing).toBe(false);
    });

    it("exposes the latest sync error to consumers", async () => {
      vi.spyOn(console, "warn").mockImplementation(() => undefined);
      globalThis.fetch = vi
        .fn()
        .mockRejectedValue(new Error("Network error"));

      const { result } = renderHook(() => useSyncManager());

      await act(async () => {
        await result.current.syncNow();
      });

      expect(result.current.syncError).toBe("Network error");
    });

    it("falls back to a generic message when the rejection is not an Error", async () => {
      vi.spyOn(console, "warn").mockImplementation(() => undefined);
      // A non-Error rejection (e.g. a thrown string) must surface the
      // user-facing fallback literal rather than leaking the raw value.
      globalThis.fetch = vi.fn().mockRejectedValue("boom");

      const { result } = renderHook(() => useSyncManager());

      await act(async () => {
        await result.current.syncNow();
      });

      expect(result.current.syncError).toBe("Sync failed");
    });

    it("falls back to a generic message when the Error has an empty message", async () => {
      vi.spyOn(console, "warn").mockImplementation(() => undefined);
      // An Error with no message must also surface the fallback literal
      // instead of an empty string.
      globalThis.fetch = vi.fn().mockRejectedValue(new Error(""));

      const { result } = renderHook(() => useSyncManager());

      await act(async () => {
        await result.current.syncNow();
      });

      expect(result.current.syncError).toBe("Sync failed");
    });

    it("clears syncError after a later successful sync", async () => {
      vi.spyOn(console, "warn").mockImplementation(() => undefined);
      let shouldFail = true;
      const fetchMock = vi.fn().mockImplementation((url: string) => {
        if (shouldFail) {
          return Promise.reject(new Error("Network error"));
        }
        if (url.includes("/api/sync/version")) {
          return Promise.resolve({
            ok: true,
            json: () =>
              Promise.resolve({
                version: 1,
                updated_at: "2026-03-09T10:00:00Z",
              }),
          });
        }
        return Promise.reject(new Error(`Unexpected fetch: ${url}`));
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useSyncManager());

      await act(async () => {
        await result.current.syncNow();
      });
      expect(result.current.syncError).toBe("Network error");

      shouldFail = false;
      await act(async () => {
        await result.current.syncNow();
      });

      expect(result.current.syncError).toBeNull();
      expect(result.current.version).toBe(1);
    });

    it("ignores non-OK sync version responses without mutating sync state", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: () => Promise.resolve({ error: "boom" }),
      });
      globalThis.fetch = fetchMock as typeof fetch;

      const onSyncData = vi.fn();
      const { result } = renderHook(() => useSyncManager({ onSyncData }));

      await act(async () => {
        await result.current.syncNow();
      });

      expect(result.current.version).toBe(0);
      expect(result.current.lastSyncAt).toBeNull();
      expect(onSyncData).not.toHaveBeenCalled();
    });

    it("preserves the previous cursor when /api/sync/changes returns non-OK", async () => {
      let currentVersion = 1;
      const fetchMock = vi.fn().mockImplementation((url: string) => {
        if (url.includes("/api/sync/version")) {
          return Promise.resolve({
            ok: true,
            json: () =>
              Promise.resolve({
                version: currentVersion,
                updated_at: "2026-03-09T10:00:00Z",
              }),
          });
        }

        return Promise.resolve({
          ok: false,
          status: 500,
          json: () => Promise.resolve({ error: "boom" }),
        });
      });
      globalThis.fetch = fetchMock as typeof fetch;

      const onSyncData = vi.fn();
      const { result } = renderHook(() => useSyncManager({ onSyncData }));

      await act(async () => {
        await result.current.syncNow();
      });
      expect(result.current.lastSyncAt).toBe("2026-03-09T10:00:00Z");
      onSyncData.mockClear();

      currentVersion = 2;
      await act(async () => {
        await result.current.syncNow();
      });

      expect(result.current.version).toBe(1);
      expect(result.current.lastSyncAt).toBe("2026-03-09T10:00:00Z");
      expect(onSyncData).not.toHaveBeenCalled();
    });
  });

  // -------------------------------------------------------------------------
  // Cleanup
  // -------------------------------------------------------------------------
  describe("Cleanup", () => {
    it("removes event listeners on unmount", async () => {
      const removeEventListenerSpy = vi.spyOn(
        document,
        "removeEventListener"
      );

      const fetchMock = mockFetchResponses({
        version: 1,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      const { unmount } = renderHook(() => useSyncManager());

      unmount();

      // Should have removed click and visibilitychange listeners
      const removedEvents = removeEventListenerSpy.mock.calls.map(
        (c) => c[0]
      );
      expect(removedEvents).toContain("click");
      expect(removedEvents).toContain("visibilitychange");

      removeEventListenerSpy.mockRestore();
    });
  });

  // -------------------------------------------------------------------------
  // Type guard edge cases (malformed API responses)
  // -------------------------------------------------------------------------
  describe("malformed API responses", () => {
    it("handles non-object version response gracefully", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(null),
      });
      globalThis.fetch = fetchMock;

      const onSyncData = vi.fn();
      const { result } = renderHook(() =>
        useSyncManager({ onSyncData })
      );

      await act(async () => {
        result.current.syncNow();
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(onSyncData).not.toHaveBeenCalled();
    });

    it("handles version response with wrong field types gracefully", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ version: "not-a-number", updated_at: 123 }),
      });
      globalThis.fetch = fetchMock;

      const onSyncData = vi.fn();
      const { result } = renderHook(() =>
        useSyncManager({ onSyncData })
      );

      await act(async () => {
        result.current.syncNow();
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(onSyncData).not.toHaveBeenCalled();
    });
  });

  // -------------------------------------------------------------------------
  // Self-mutation with no prior sync
  // -------------------------------------------------------------------------
  describe("self-mutation before initial sync", () => {
    it("sets lastSyncAt from version response on first self-mutation sync", async () => {
      const fetchMock = mockFetchResponses({
        version: 5,
        updated_at: "2026-03-09T10:00:00Z",
      });
      globalThis.fetch = fetchMock;

      const onSyncData = vi.fn();
      const { result } = renderHook(() =>
        useSyncManager({ onSyncData })
      );

      // Mark a self-mutation before any sync has happened
      act(() => {
        result.current.markMutation();
      });

      // Trigger sync — should detect version change but skip data fetch
      await act(async () => {
        result.current.syncNow();
        await vi.advanceTimersByTimeAsync(0);
      });

      expect(onSyncData).not.toHaveBeenCalled();
      expect(result.current.lastSyncAt).toBe("2026-03-09T10:00:00Z");
      expect(result.current.version).toBe(5);
    });
  });
});
