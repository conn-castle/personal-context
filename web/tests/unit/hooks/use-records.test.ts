// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useRecords } from "@/hooks/use-records";
import type { RecordSummary, RecordDetail } from "@/lib/types";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const mockRecord: RecordSummary = {
  id: "20260309-aabbccdd",
  date: "2026-03-09",
  day_order: "a0",
  html_content: "<p>Test content</p>",
  project_id: "org/proj",
  source_device_id: "device-a",
  source_ref: null,
  updated_at: "2026-03-09T10:00:00Z",
  deleted_at: null,
  figure_count: 1,
  data_file_count: 0,
};

const mockRecordDetail: RecordDetail = {
  id: "20260309-aabbccdd",
  date: "2026-03-09",
  day_order: "a0",
  html_content: "<p>hello</p>",
  notes: "Some notes",
  project_id: "org/proj",
  source_device_id: "device-a",
  source_ref: null,
  git_remote_url: null,
  git_hash: null,
  created_at: "2026-03-09T08:00:00Z",
  updated_at: "2026-03-09T10:00:00Z",
  deleted_at: null,
  figures: [],
  data_files: [],
};

function mockFetch(responses: Record<string, unknown>) {
  return vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET";
    const key = `${method} ${url}`;

    // Find matching key by prefix
    for (const [pattern, body] of Object.entries(responses)) {
      if (key.startsWith(pattern) || url.startsWith(pattern.replace(/^GET /, ""))) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve(body),
        });
      }
    }

    return Promise.resolve({
      ok: false,
      status: 404,
      json: () => Promise.resolve({ error: "Not found" }),
    });
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("useRecords", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  describe("buildQuery (via fetchRecords)", () => {
    it("passes deleted filter param", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ items: [], total: 0, next_cursor: null }),
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords({ deleted: true });
      });

      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("deleted=true")
      );
    });

    it("sends no query string with no params", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ items: [], total: 0, next_cursor: null }),
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      expect(fetchMock).toHaveBeenCalledWith("/api/records");
    });
  });

  describe("fetchRecords", () => {
    it("fetches records and sets state", async () => {
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      expect(result.current.records).toHaveLength(1);
      expect(result.current.records[0].id).toBe("20260309-aabbccdd");
      expect(result.current.hasMore).toBe(false);
      expect(result.current.isLoading).toBe(false);
    });

    it("sets hasMore when next_cursor is present", async () => {
      globalThis.fetch = mockFetch({
        "/api/records": {
          items: [mockRecord],
          total: 2,
          next_cursor: "abc123",
        },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      expect(result.current.hasMore).toBe(true);
    });

    it("passes project filter param", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ items: [], total: 0, next_cursor: null }),
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords({ project: "org/alpha" });
      });

      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("project=org")
      );
    });

    it("sets error on fetch failure", async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      expect(result.current.error).toContain("Failed to fetch records");
      expect(result.current.isLoading).toBe(false);
    });

    it("sets error on network error", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue(new Error("Network error"));

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      expect(result.current.error).toBe("Network error");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      expect(result.current.error).toBe("Failed to fetch records");
    });

    it("does not let a stale first-page response overwrite a newer one", async () => {
      const slowRecord: RecordSummary = {
        ...mockRecord,
        id: "20260309-11111111",
        project_id: "org/slow",
      };
      const fastRecord: RecordSummary = {
        ...mockRecord,
        id: "20260309-22222222",
        project_id: "org/fast",
      };
      let resolveSlow:
        | ((value: {
            ok: boolean;
            json: () => Promise<{
              items: RecordSummary[];
              total: number;
              next_cursor: string | null;
            }>;
          }) => void)
        | null = null;
      let resolveFast:
        | ((value: {
            ok: boolean;
            json: () => Promise<{
              items: RecordSummary[];
              total: number;
              next_cursor: string | null;
            }>;
          }) => void)
        | null = null;
      const fetchMock = vi
        .fn()
        .mockImplementationOnce(
          () =>
            new Promise((resolve) => {
              resolveSlow = resolve as typeof resolveSlow;
            })
        )
        .mockImplementationOnce(
          () =>
            new Promise((resolve) => {
              resolveFast = resolve as typeof resolveFast;
            })
        );
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useRecords());

      let slowRequest: Promise<void>;
      await act(async () => {
        slowRequest = result.current.fetchRecords({ project: "org/slow" });
        await Promise.resolve();
      });

      let fastRequest: Promise<void>;
      await act(async () => {
        fastRequest = result.current.fetchRecords({ project: "org/fast" });
        await Promise.resolve();
      });

      await act(async () => {
        resolveFast?.({
          ok: true,
          json: () =>
            Promise.resolve({
              items: [fastRecord],
              total: 1,
              next_cursor: null,
            }),
        });
        await fastRequest!;
      });

      expect(result.current.records).toEqual([fastRecord]);
      expect(result.current.hasMore).toBe(false);

      await act(async () => {
        resolveSlow?.({
          ok: true,
          json: () =>
            Promise.resolve({
              items: [slowRecord],
              total: 2,
              next_cursor: "stale-cursor",
            }),
        });
        await slowRequest!;
      });

      expect(result.current.records).toEqual([fastRecord]);
      expect(result.current.hasMore).toBe(false);
      expect(result.current.isLoading).toBe(false);
      expect(result.current.error).toBeNull();
    });

    it("clears loading when a silent refresh supersedes an older first-page request", async () => {
      const refreshedRecord: RecordSummary = {
        ...mockRecord,
        id: "20260310-22222222",
      };
      let resolveInitial:
        | ((value: {
            ok: boolean;
            json: () => Promise<{
              items: RecordSummary[];
              total: number;
              next_cursor: string | null;
            }>;
          }) => void)
        | null = null;
      let resolveRefresh:
        | ((value: {
            ok: boolean;
            json: () => Promise<{
              items: RecordSummary[];
              total: number;
              next_cursor: string | null;
            }>;
          }) => void)
        | null = null;
      const fetchMock = vi
        .fn()
        .mockImplementationOnce(
          () =>
            new Promise((resolve) => {
              resolveInitial = resolve as typeof resolveInitial;
            })
        )
        .mockImplementationOnce(
          () =>
            new Promise((resolve) => {
              resolveRefresh = resolve as typeof resolveRefresh;
            })
        );
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useRecords());

      let initialRequest: Promise<void>;
      await act(async () => {
        initialRequest = result.current.fetchRecords();
        await Promise.resolve();
      });
      expect(result.current.isLoading).toBe(true);

      let refreshRequest: Promise<void>;
      await act(async () => {
        refreshRequest = result.current.refreshRecords();
        await Promise.resolve();
      });

      await act(async () => {
        resolveRefresh?.({
          ok: true,
          json: () =>
            Promise.resolve({
              items: [refreshedRecord],
              total: 1,
              next_cursor: null,
            }),
        });
        await refreshRequest!;
      });

      await act(async () => {
        resolveInitial?.({
          ok: true,
          json: () =>
            Promise.resolve({
              items: [mockRecord],
              total: 1,
              next_cursor: "stale-cursor",
            }),
        });
        await initialRequest!;
      });

      expect(result.current.records).toEqual([refreshedRecord]);
      expect(result.current.isLoading).toBe(false);
    });
  });

  describe("fetchMore", () => {
    it("does nothing when no cursor is set", async () => {
      const fetchMock = vi.fn();
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchMore();
      });

      expect(fetchMock).not.toHaveBeenCalled();
    });

    it("appends next page to existing records", async () => {
      const record2: RecordSummary = { ...mockRecord, id: "20260308-11111111" };

      // First fetch returns one record with cursor
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 2, next_cursor: "cursor1" },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      // Set up second page
      globalThis.fetch = mockFetch({
        "/api/records": { items: [record2], total: 2, next_cursor: null },
      });

      await act(async () => {
        await result.current.fetchMore();
      });

      expect(result.current.records).toHaveLength(2);
      expect(result.current.hasMore).toBe(false);
    });

    it("sets error on fetch failure", async () => {
      // First fetch to set cursor
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 2, next_cursor: "cursor1" },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      // Now fail the fetchMore
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
      });

      await act(async () => {
        await result.current.fetchMore();
      });

      expect(result.current.error).toContain("Failed to fetch more records");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 2, next_cursor: "cursor1" },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      await act(async () => {
        await result.current.fetchMore();
      });

      expect(result.current.error).toBe("Failed to fetch more records");
    });

    it("ignores duplicate fetchMore calls while a page request is already in flight", async () => {
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 2, next_cursor: "cursor1" },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      let resolvePage:
        | ((value: {
            ok: boolean;
            json: () => Promise<{
              items: RecordSummary[];
              total: number;
              next_cursor: null;
            }>;
          }) => void)
        | null = null;
      const fetchMock = vi.fn().mockImplementation(() => {
        return new Promise((resolve) => {
          resolvePage = resolve as typeof resolvePage;
        });
      });
      globalThis.fetch = fetchMock;

      await act(async () => {
        void result.current.fetchMore();
        void result.current.fetchMore();
        await Promise.resolve();
      });

      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(result.current.isFetchingMore).toBe(true);

      await act(async () => {
        resolvePage?.({
          ok: true,
          json: () =>
            Promise.resolve({
              items: [{ ...mockRecord, id: "20260308-11111111" }],
              total: 2,
              next_cursor: null,
            }),
        });
        await Promise.resolve();
      });

      expect(result.current.records).toHaveLength(2);
      expect(result.current.isFetchingMore).toBe(false);
    });

    it("does not append a stale next page after a newer first-page request starts", async () => {
      const nextPageRecord: RecordSummary = {
        ...mockRecord,
        id: "20260308-11111111",
      };
      const refreshedRecord: RecordSummary = {
        ...mockRecord,
        id: "20260310-22222222",
      };

      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 2, next_cursor: "cursor1" },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      let resolveNextPage:
        | ((value: {
            ok: boolean;
            json: () => Promise<{
              items: RecordSummary[];
              total: number;
              next_cursor: string | null;
            }>;
          }) => void)
        | null = null;
      let resolveReplacement:
        | ((value: {
            ok: boolean;
            json: () => Promise<{
              items: RecordSummary[];
              total: number;
              next_cursor: string | null;
            }>;
          }) => void)
        | null = null;
      const fetchMock = vi
        .fn()
        .mockImplementationOnce(
          () =>
            new Promise((resolve) => {
              resolveNextPage = resolve as typeof resolveNextPage;
            })
        )
        .mockImplementationOnce(
          () =>
            new Promise((resolve) => {
              resolveReplacement = resolve as typeof resolveReplacement;
            })
        );
      globalThis.fetch = fetchMock;

      let nextPageRequest: Promise<void>;
      await act(async () => {
        nextPageRequest = result.current.fetchMore();
        await Promise.resolve();
      });

      let replacementRequest: Promise<void>;
      await act(async () => {
        replacementRequest = result.current.fetchRecords();
        await Promise.resolve();
      });

      await act(async () => {
        resolveReplacement?.({
          ok: true,
          json: () =>
            Promise.resolve({
              items: [refreshedRecord],
              total: 1,
              next_cursor: null,
            }),
        });
        await replacementRequest!;
      });

      await act(async () => {
        resolveNextPage?.({
          ok: true,
          json: () =>
            Promise.resolve({
              items: [nextPageRecord],
              total: 2,
              next_cursor: "stale-cursor",
            }),
        });
        await nextPageRequest!;
      });

      expect(result.current.records).toEqual([refreshedRecord]);
      expect(result.current.hasMore).toBe(false);
      expect(result.current.isFetchingMore).toBe(false);
      expect(result.current.isLoading).toBe(false);
    });
  });

  describe("selectRecord", () => {
    it("fetches and sets selected record detail", async () => {
      globalThis.fetch = mockFetch({
        "/api/records/20260309-aabbccdd": { record: mockRecordDetail },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.selectRecord("20260309-aabbccdd");
      });

      expect(result.current.selectedRecord?.id).toBe("20260309-aabbccdd");
      expect(result.current.selectedRecord?.html_content).toBe("<p>hello</p>");
    });

    it("sets error on failure", async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.selectRecord("20260309-nonexist");
      });

      expect(result.current.error).toContain("Failed to fetch record");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.selectRecord("20260309-nonexist");
      });

      expect(result.current.error).toBe("Failed to fetch record detail");
    });
  });

  describe("updateRecord", () => {
    it("updates selected record and list entry", async () => {
      const updatedDetail: RecordDetail = {
        ...mockRecordDetail,
        notes: "Updated notes",
        project_id: "org/beta",
      };

      // First load records + select
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      // Set selected record
      globalThis.fetch = mockFetch({
        "/api/records/20260309-aabbccdd": { record: mockRecordDetail },
      });

      await act(async () => {
        await result.current.selectRecord("20260309-aabbccdd");
      });

      // Now update
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            record: updatedDetail,
            sync_version: 5,
          }),
      });

      await act(async () => {
        await result.current.updateRecord("20260309-aabbccdd", {
          notes: "Updated notes",
        });
      });

      expect(result.current.selectedRecord?.notes).toBe("Updated notes");
      expect(result.current.records[0].project_id).toBe("org/beta");
    });

    it("does not change selectedRecord when updating a different record", async () => {
      const otherDetail: RecordDetail = {
        ...mockRecordDetail,
        id: "20260308-11111111",
        notes: "Other notes",
      };

      // Load records and select a different record
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      globalThis.fetch = mockFetch({
        "/api/records/20260308-11111111": { record: otherDetail },
      });

      await act(async () => {
        await result.current.selectRecord("20260308-11111111");
      });

      // Update a record that is not currently selected
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            record: { ...mockRecordDetail, notes: "Changed" },
            sync_version: 5,
          }),
      });

      await act(async () => {
        await result.current.updateRecord("20260309-aabbccdd", {
          notes: "Changed",
        });
      });

      // selectedRecord should not change (it's a different record)
      expect(result.current.selectedRecord?.id).toBe("20260308-11111111");
      expect(result.current.selectedRecord?.notes).toBe("Other notes");
    });

    it("sets error on failure", async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.updateRecord("20260309-aabbccdd", {
          notes: "test",
        });
      });

      expect(result.current.error).toContain("Failed to update record");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.updateRecord("20260309-aabbccdd", {
          notes: "test",
        });
      });

      expect(result.current.error).toBe("Failed to update record");
    });
  });

  describe("deleteRecord", () => {
    it("optimistically removes record from list", async () => {
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      expect(result.current.records).toHaveLength(1);

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            id: "20260309-aabbccdd",
            deleted_at: "2026-03-09T12:00:00Z",
            updated_at: "2026-03-09T12:00:00Z",
            sync_version: 6,
          }),
      });

      await act(async () => {
        await result.current.deleteRecord("20260309-aabbccdd");
      });

      expect(result.current.records).toHaveLength(0);
    });

    it("does not clear selectedRecord when deleting a different record", async () => {
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      // Select a record
      globalThis.fetch = mockFetch({
        "/api/records/20260309-aabbccdd": { record: mockRecordDetail },
      });

      await act(async () => {
        await result.current.selectRecord("20260309-aabbccdd");
      });

      // Delete a different record
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            id: "20260308-other",
            deleted_at: "2026-03-09T12:00:00Z",
            updated_at: "2026-03-09T12:00:00Z",
            sync_version: 6,
          }),
      });

      await act(async () => {
        await result.current.deleteRecord("20260308-other");
      });

      // selectedRecord should still be set
      expect(result.current.selectedRecord?.id).toBe("20260309-aabbccdd");
    });

    it("re-fetches on failure to restore accurate state", async () => {
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      expect(result.current.records).toHaveLength(1);

      // Delete fails — mock fetch to fail DELETE but succeed on re-fetch
      let callCount = 0;
      globalThis.fetch = vi.fn().mockImplementation(() => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve({ ok: false, status: 500 });
        }
        // Re-fetch after failure
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({ items: [mockRecord], total: 1, next_cursor: null }),
        });
      });

      await act(async () => {
        await result.current.deleteRecord("20260309-aabbccdd");
      });

      // After rollback re-fetch, records should be restored
      expect(result.current.records).toHaveLength(1);
      expect(result.current.error).toContain("Failed to delete record");
    });
  });

  describe("restoreRecord", () => {
    it("optimistically removes record from list", async () => {
      const deletedRecord: RecordSummary = {
        ...mockRecord,
        deleted_at: "2026-03-09T11:00:00Z",
      };

      globalThis.fetch = mockFetch({
        "/api/records": { items: [deletedRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords({ deleted: true });
      });

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            id: "20260309-aabbccdd",
            deleted_at: null,
            updated_at: "2026-03-09T12:00:00Z",
            sync_version: 7,
          }),
      });

      await act(async () => {
        await result.current.restoreRecord("20260309-aabbccdd");
      });

      expect(result.current.records).toHaveLength(0);
    });

    it("does not clear selectedRecord when restoring a different record", async () => {
      const deletedRecord: RecordSummary = {
        ...mockRecord,
        deleted_at: "2026-03-09T11:00:00Z",
      };

      globalThis.fetch = mockFetch({
        "/api/records": { items: [deletedRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords({ deleted: true });
      });

      // Select the record
      globalThis.fetch = mockFetch({
        "/api/records/20260309-aabbccdd": { record: mockRecordDetail },
      });

      await act(async () => {
        await result.current.selectRecord("20260309-aabbccdd");
      });

      // Restore a different record
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            id: "20260308-other",
            deleted_at: null,
            updated_at: "2026-03-09T12:00:00Z",
            sync_version: 7,
          }),
      });

      await act(async () => {
        await result.current.restoreRecord("20260308-other");
      });

      // selectedRecord should still be set
      expect(result.current.selectedRecord?.id).toBe("20260309-aabbccdd");
    });

    it("re-fetches on failure to restore accurate state", async () => {
      const deletedRecord: RecordSummary = {
        ...mockRecord,
        deleted_at: "2026-03-09T11:00:00Z",
      };

      globalThis.fetch = mockFetch({
        "/api/records": { items: [deletedRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords({ deleted: true });
      });

      // Restore fails — mock fetch to fail POST but succeed on re-fetch
      let callCount = 0;
      globalThis.fetch = vi.fn().mockImplementation(() => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve({ ok: false, status: 500 });
        }
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({ items: [deletedRecord], total: 1, next_cursor: null }),
        });
      });

      await act(async () => {
        await result.current.restoreRecord("20260309-aabbccdd");
      });

      // After rollback re-fetch, records should be restored
      expect(result.current.records).toHaveLength(1);
      expect(result.current.error).toContain("Failed to restore record");
    });
  });

  describe("reorderRecord", () => {
    it("updates record date and day_order in list", async () => {
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            id: "20260309-aabbccdd",
            date: "2026-03-10",
            day_order: "a1",
            updated_at: "2026-03-09T12:00:00Z",
            sync_version: 8,
          }),
      });

      await act(async () => {
        await result.current.reorderRecord("20260309-aabbccdd", {
          date: "2026-03-10",
          position: { kind: "last" },
        });
      });

      expect(result.current.records[0].date).toBe("2026-03-10");
      expect(result.current.records[0].day_order).toBe("a1");
    });

    it("re-sorts records after reorder to maintain date DESC, day_order ASC, id ASC", async () => {
      const recordA = { ...mockRecord, id: "20260309-aaaaaaaa", date: "2026-03-09", day_order: "a0" };
      const recordB = { ...mockRecord, id: "20260309-bbbbbbbb", date: "2026-03-09", day_order: "a1" };
      const recordC = { ...mockRecord, id: "20260308-cccccccc", date: "2026-03-08", day_order: "a0" };

      globalThis.fetch = mockFetch({
        "/api/records": { items: [recordA, recordB, recordC], total: 3, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());
      await act(async () => { await result.current.fetchRecords(); });

      // Reorder recordC to 2026-03-09 with day_order between A and B
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          id: "20260308-cccccccc",
          date: "2026-03-09",
          day_order: "a0V",
          updated_at: "2026-03-09T12:00:00Z",
          sync_version: 8,
        }),
      });

      await act(async () => {
        await result.current.reorderRecord("20260308-cccccccc", {
          date: "2026-03-09",
          position: { kind: "after", reference_id: "20260309-aaaaaaaa" },
        });
      });

      // All three records should now be on 2026-03-09, sorted by day_order
      expect(result.current.records).toHaveLength(3);
      expect(result.current.records[0].id).toBe("20260309-aaaaaaaa"); // a0
      expect(result.current.records[1].id).toBe("20260308-cccccccc"); // a0V
      expect(result.current.records[2].id).toBe("20260309-bbbbbbbb"); // a1
    });

    it("sets error on failure", async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.reorderRecord("20260309-aabbccdd", {
          position: { kind: "first" },
        });
      });

      expect(result.current.error).toContain("Failed to reorder record");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.reorderRecord("20260309-aabbccdd", {
          position: { kind: "first" },
        });
      });

      expect(result.current.error).toBe("Failed to reorder record");
    });
  });

  describe("fetchProjects", () => {
    it("fetches and stores project list", async () => {
      globalThis.fetch = mockFetch({
        "/api/projects": { projects: ["org/alpha", "org/beta"] },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchProjects();
      });

      expect(result.current.projects).toEqual(["org/alpha", "org/beta"]);
    });

    it("sets error on failure", async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchProjects();
      });

      expect(result.current.error).toContain("Failed to fetch projects");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchProjects();
      });

      expect(result.current.error).toBe("Failed to fetch projects");
    });
  });

  describe("refreshRecords", () => {
    it("re-fetches with the same params", async () => {
      // Initial fetch with project filter
      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords({ project: "org/alpha" });
      });

      // Refresh should use the same params
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({ items: [mockRecord], total: 1, next_cursor: null }),
      });
      globalThis.fetch = fetchMock;

      await act(async () => {
        await result.current.refreshRecords();
      });

      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("project=org")
      );
    });

    it("clears selectedRecord when the refreshed page no longer contains it", async () => {
      const otherRecord: RecordSummary = {
        ...mockRecord,
        id: "20260308-11111111",
      };

      globalThis.fetch = mockFetch({
        "/api/records": { items: [mockRecord], total: 1, next_cursor: null },
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.fetchRecords();
      });

      globalThis.fetch = mockFetch({
        "/api/records/20260309-aabbccdd": { record: mockRecordDetail },
      });

      await act(async () => {
        await result.current.selectRecord("20260309-aabbccdd");
      });

      expect(result.current.selectedRecord?.id).toBe("20260309-aabbccdd");

      globalThis.fetch = mockFetch({
        "/api/records": { items: [otherRecord], total: 1, next_cursor: null },
      });

      await act(async () => {
        await result.current.refreshRecords();
      });

      expect(result.current.records).toEqual([otherRecord]);
      expect(result.current.selectedRecord).toBeNull();
    });

    it("sets error on failure", async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
      });

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.refreshRecords();
      });

      expect(result.current.error).toContain("Failed to refresh records");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useRecords());

      await act(async () => {
        await result.current.refreshRecords();
      });

      expect(result.current.error).toBe("Failed to refresh records");
    });
  });
});
