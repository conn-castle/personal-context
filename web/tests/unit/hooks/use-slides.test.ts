// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useSlides } from "@/hooks/use-slides";
import type { SlideSummary, SlideDetail } from "@/lib/types";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const mockSlide: SlideSummary = {
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

const mockSlideDetail: SlideDetail = {
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

describe("useSlides", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  describe("buildQuery (via fetchSlides)", () => {
    it("passes deleted filter param", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ items: [], next_cursor: null }),
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides({ deleted: true });
      });

      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("deleted=true")
      );
    });

    it("sends no query string with no params", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ items: [], next_cursor: null }),
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      expect(fetchMock).toHaveBeenCalledWith("/api/slides");
    });
  });

  describe("fetchSlides", () => {
    it("fetches slides and sets state", async () => {
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      expect(result.current.slides).toHaveLength(1);
      expect(result.current.slides[0].id).toBe("20260309-aabbccdd");
      expect(result.current.hasMore).toBe(false);
      expect(result.current.isLoading).toBe(false);
    });

    it("sets hasMore when next_cursor is present", async () => {
      globalThis.fetch = mockFetch({
        "/api/slides": {
          items: [mockSlide],
          next_cursor: "abc123",
        },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      expect(result.current.hasMore).toBe(true);
    });

    it("passes project filter param", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ items: [], next_cursor: null }),
      });
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides({ project: "org/alpha" });
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

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      expect(result.current.error).toContain("Failed to fetch slides");
      expect(result.current.isLoading).toBe(false);
    });

    it("sets error on network error", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue(new Error("Network error"));

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      expect(result.current.error).toBe("Network error");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      expect(result.current.error).toBe("Failed to fetch slides");
    });
  });

  describe("fetchMore", () => {
    it("does nothing when no cursor is set", async () => {
      const fetchMock = vi.fn();
      globalThis.fetch = fetchMock;

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchMore();
      });

      expect(fetchMock).not.toHaveBeenCalled();
    });

    it("appends next page to existing slides", async () => {
      const slide2: SlideSummary = { ...mockSlide, id: "20260308-11111111" };

      // First fetch returns one slide with cursor
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: "cursor1" },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      // Set up second page
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [slide2], next_cursor: null },
      });

      await act(async () => {
        await result.current.fetchMore();
      });

      expect(result.current.slides).toHaveLength(2);
      expect(result.current.hasMore).toBe(false);
    });

    it("sets error on fetch failure", async () => {
      // First fetch to set cursor
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: "cursor1" },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      // Now fail the fetchMore
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
      });

      await act(async () => {
        await result.current.fetchMore();
      });

      expect(result.current.error).toContain("Failed to fetch more slides");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: "cursor1" },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      await act(async () => {
        await result.current.fetchMore();
      });

      expect(result.current.error).toBe("Failed to fetch more slides");
    });

    it("ignores duplicate fetchMore calls while a page request is already in flight", async () => {
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: "cursor1" },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      let resolvePage:
        | ((value: {
            ok: boolean;
            json: () => Promise<{
              items: SlideSummary[];
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
              items: [{ ...mockSlide, id: "20260308-11111111" }],
              next_cursor: null,
            }),
        });
        await Promise.resolve();
      });

      expect(result.current.slides).toHaveLength(2);
      expect(result.current.isFetchingMore).toBe(false);
    });
  });

  describe("selectSlide", () => {
    it("fetches and sets selected slide detail", async () => {
      globalThis.fetch = mockFetch({
        "/api/slides/20260309-aabbccdd": { slide: mockSlideDetail },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.selectSlide("20260309-aabbccdd");
      });

      expect(result.current.selectedSlide?.id).toBe("20260309-aabbccdd");
      expect(result.current.selectedSlide?.html_content).toBe("<p>hello</p>");
    });

    it("sets error on failure", async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.selectSlide("20260309-nonexist");
      });

      expect(result.current.error).toContain("Failed to fetch slide");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.selectSlide("20260309-nonexist");
      });

      expect(result.current.error).toBe("Failed to fetch slide detail");
    });
  });

  describe("updateSlide", () => {
    it("updates selected slide and list entry", async () => {
      const updatedDetail: SlideDetail = {
        ...mockSlideDetail,
        notes: "Updated notes",
        project_id: "org/beta",
      };

      // First load slides + select
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      // Set selected slide
      globalThis.fetch = mockFetch({
        "/api/slides/20260309-aabbccdd": { slide: mockSlideDetail },
      });

      await act(async () => {
        await result.current.selectSlide("20260309-aabbccdd");
      });

      // Now update
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            slide: updatedDetail,
            sync_version: 5,
          }),
      });

      await act(async () => {
        await result.current.updateSlide("20260309-aabbccdd", {
          notes: "Updated notes",
        });
      });

      expect(result.current.selectedSlide?.notes).toBe("Updated notes");
      expect(result.current.slides[0].project_id).toBe("org/beta");
    });

    it("does not change selectedSlide when updating a different slide", async () => {
      const otherDetail: SlideDetail = {
        ...mockSlideDetail,
        id: "20260308-11111111",
        notes: "Other notes",
      };

      // Load slides and select a different slide
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      globalThis.fetch = mockFetch({
        "/api/slides/20260308-11111111": { slide: otherDetail },
      });

      await act(async () => {
        await result.current.selectSlide("20260308-11111111");
      });

      // Update a slide that is not currently selected
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            slide: { ...mockSlideDetail, notes: "Changed" },
            sync_version: 5,
          }),
      });

      await act(async () => {
        await result.current.updateSlide("20260309-aabbccdd", {
          notes: "Changed",
        });
      });

      // selectedSlide should not change (it's a different slide)
      expect(result.current.selectedSlide?.id).toBe("20260308-11111111");
      expect(result.current.selectedSlide?.notes).toBe("Other notes");
    });

    it("sets error on failure", async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.updateSlide("20260309-aabbccdd", {
          notes: "test",
        });
      });

      expect(result.current.error).toContain("Failed to update slide");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.updateSlide("20260309-aabbccdd", {
          notes: "test",
        });
      });

      expect(result.current.error).toBe("Failed to update slide");
    });
  });

  describe("deleteSlide", () => {
    it("optimistically removes slide from list", async () => {
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      expect(result.current.slides).toHaveLength(1);

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
        await result.current.deleteSlide("20260309-aabbccdd");
      });

      expect(result.current.slides).toHaveLength(0);
    });

    it("does not clear selectedSlide when deleting a different slide", async () => {
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      // Select a slide
      globalThis.fetch = mockFetch({
        "/api/slides/20260309-aabbccdd": { slide: mockSlideDetail },
      });

      await act(async () => {
        await result.current.selectSlide("20260309-aabbccdd");
      });

      // Delete a different slide
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
        await result.current.deleteSlide("20260308-other");
      });

      // selectedSlide should still be set
      expect(result.current.selectedSlide?.id).toBe("20260309-aabbccdd");
    });

    it("re-fetches on failure to restore accurate state", async () => {
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
      });

      expect(result.current.slides).toHaveLength(1);

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
            Promise.resolve({ items: [mockSlide], next_cursor: null }),
        });
      });

      await act(async () => {
        await result.current.deleteSlide("20260309-aabbccdd");
      });

      // After rollback re-fetch, slides should be restored
      // Note: fetchSlides clears error, so error is null after successful re-fetch
      expect(result.current.slides).toHaveLength(1);
    });
  });

  describe("restoreSlide", () => {
    it("optimistically removes slide from list", async () => {
      const deletedSlide: SlideSummary = {
        ...mockSlide,
        deleted_at: "2026-03-09T11:00:00Z",
      };

      globalThis.fetch = mockFetch({
        "/api/slides": { items: [deletedSlide], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides({ deleted: true });
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
        await result.current.restoreSlide("20260309-aabbccdd");
      });

      expect(result.current.slides).toHaveLength(0);
    });

    it("does not clear selectedSlide when restoring a different slide", async () => {
      const deletedSlide: SlideSummary = {
        ...mockSlide,
        deleted_at: "2026-03-09T11:00:00Z",
      };

      globalThis.fetch = mockFetch({
        "/api/slides": { items: [deletedSlide], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides({ deleted: true });
      });

      // Select the slide
      globalThis.fetch = mockFetch({
        "/api/slides/20260309-aabbccdd": { slide: mockSlideDetail },
      });

      await act(async () => {
        await result.current.selectSlide("20260309-aabbccdd");
      });

      // Restore a different slide
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
        await result.current.restoreSlide("20260308-other");
      });

      // selectedSlide should still be set
      expect(result.current.selectedSlide?.id).toBe("20260309-aabbccdd");
    });

    it("re-fetches on failure to restore accurate state", async () => {
      const deletedSlide: SlideSummary = {
        ...mockSlide,
        deleted_at: "2026-03-09T11:00:00Z",
      };

      globalThis.fetch = mockFetch({
        "/api/slides": { items: [deletedSlide], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides({ deleted: true });
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
            Promise.resolve({ items: [deletedSlide], next_cursor: null }),
        });
      });

      await act(async () => {
        await result.current.restoreSlide("20260309-aabbccdd");
      });

      // After rollback re-fetch, slides should be restored
      expect(result.current.slides).toHaveLength(1);
    });
  });

  describe("reorderSlide", () => {
    it("updates slide date and day_order in list", async () => {
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides();
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
        await result.current.reorderSlide("20260309-aabbccdd", {
          date: "2026-03-10",
          position: { kind: "last" },
        });
      });

      expect(result.current.slides[0].date).toBe("2026-03-10");
      expect(result.current.slides[0].day_order).toBe("a1");
    });

    it("re-sorts slides after reorder to maintain date DESC, day_order ASC, id ASC", async () => {
      const slideA = { ...mockSlide, id: "20260309-aaaaaaaa", date: "2026-03-09", day_order: "a0" };
      const slideB = { ...mockSlide, id: "20260309-bbbbbbbb", date: "2026-03-09", day_order: "a1" };
      const slideC = { ...mockSlide, id: "20260308-cccccccc", date: "2026-03-08", day_order: "a0" };

      globalThis.fetch = mockFetch({
        "/api/slides": { items: [slideA, slideB, slideC], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());
      await act(async () => { await result.current.fetchSlides(); });

      // Reorder slideC to 2026-03-09 with day_order between A and B
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
        await result.current.reorderSlide("20260308-cccccccc", {
          date: "2026-03-09",
          position: { kind: "after", reference_id: "20260309-aaaaaaaa" },
        });
      });

      // All three slides should now be on 2026-03-09, sorted by day_order
      expect(result.current.slides).toHaveLength(3);
      expect(result.current.slides[0].id).toBe("20260309-aaaaaaaa"); // a0
      expect(result.current.slides[1].id).toBe("20260308-cccccccc"); // a0V
      expect(result.current.slides[2].id).toBe("20260309-bbbbbbbb"); // a1
    });

    it("sets error on failure", async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.reorderSlide("20260309-aabbccdd", {
          position: { kind: "first" },
        });
      });

      expect(result.current.error).toContain("Failed to reorder slide");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.reorderSlide("20260309-aabbccdd", {
          position: { kind: "first" },
        });
      });

      expect(result.current.error).toBe("Failed to reorder slide");
    });
  });

  describe("fetchProjects", () => {
    it("fetches and stores project list", async () => {
      globalThis.fetch = mockFetch({
        "/api/projects": { projects: ["org/alpha", "org/beta"] },
      });

      const { result } = renderHook(() => useSlides());

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

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchProjects();
      });

      expect(result.current.error).toContain("Failed to fetch projects");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchProjects();
      });

      expect(result.current.error).toBe("Failed to fetch projects");
    });
  });

  describe("refreshSlides", () => {
    it("re-fetches with the same params", async () => {
      // Initial fetch with project filter
      globalThis.fetch = mockFetch({
        "/api/slides": { items: [mockSlide], next_cursor: null },
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.fetchSlides({ project: "org/alpha" });
      });

      // Refresh should use the same params
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({ items: [mockSlide], next_cursor: null }),
      });
      globalThis.fetch = fetchMock;

      await act(async () => {
        await result.current.refreshSlides();
      });

      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("project=org")
      );
    });

    it("sets error on failure", async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
      });

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.refreshSlides();
      });

      expect(result.current.error).toContain("Failed to refresh slides");
    });

    it("uses fallback message for non-Error thrown value", async () => {
      globalThis.fetch = vi.fn().mockRejectedValue("string error");

      const { result } = renderHook(() => useSlides());

      await act(async () => {
        await result.current.refreshSlides();
      });

      expect(result.current.error).toBe("Failed to refresh slides");
    });
  });
});
