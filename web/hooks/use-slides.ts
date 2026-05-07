"use client";

import { useState, useCallback, useRef } from "react";
import type {
  SlideSummary,
  SlideDetail,
  PaginatedResponse,
  ProjectsResponse,
  ReorderResponse,
} from "@/lib/types";

/** Filter parameters persisted across refreshes. */
interface SlideFilterParams {
  project?: string;
  deleted?: boolean;
}

/** Return value of useSlides. */
export interface UseSlidesReturn {
  /** The current list of slide summaries. */
  slides: SlideSummary[];
  /** The currently selected slide detail, or null. */
  selectedSlide: SlideDetail | null;
  /** Distinct project IDs from the server. */
  projects: string[];
  /** Whether any fetch operation is in progress. */
  isLoading: boolean;
  /** The most recent error message, or null. */
  error: string | null;
  /** Whether more pages are available for pagination. */
  hasMore: boolean;
  /** Whether the next page request is currently in flight. */
  isFetchingMore: boolean;
  /** Fetch the first page of slides, optionally with new filter params. */
  fetchSlides: (params?: SlideFilterParams) => Promise<void>;
  /** Load the next page and append results to the slides list. */
  fetchMore: () => Promise<void>;
  /** Fetch full detail for a single slide and set it as selectedSlide. */
  selectSlide: (id: string) => Promise<void>;
  /** Update editable fields on a slide (project_id, notes, git fields). */
  updateSlide: (
    id: string,
    body: Record<string, unknown>
  ) => Promise<boolean>;
  /** Soft-delete a slide and remove it from the local list. */
  deleteSlide: (id: string) => Promise<boolean>;
  /** Restore a soft-deleted slide and remove it from the local list. */
  restoreSlide: (id: string) => Promise<boolean>;
  /** Reorder a slide (change date/position). */
  reorderSlide: (id: string, body: Record<string, unknown>) => Promise<void>;
  /** Fetch distinct project IDs. */
  fetchProjects: () => Promise<void>;
  /** Re-fetch slides with the same filter params (called by sync manager). */
  refreshSlides: () => Promise<void>;
}

/**
 * Builds a query string from slide list filter params.
 *
 * @param params - Optional filter params (project, deleted).
 * @param cursor - Optional pagination cursor.
 * @returns A URL query string including a leading '?'.
 */
function buildQuery(params?: SlideFilterParams, cursor?: string): string {
  const searchParams = new URLSearchParams();
  if (params?.project) {
    searchParams.set("project", params.project);
  }
  if (params?.deleted !== undefined) {
    searchParams.set("deleted", String(params.deleted));
  }
  if (cursor) {
    searchParams.set("cursor", cursor);
  }
  const qs = searchParams.toString();
  return qs ? `?${qs}` : "";
}

interface SlideDetailResponse {
  slide: SlideDetail;
}

const JSON_REQUEST_HEADERS = { "Content-Type": "application/json" };

/**
 * Converts unknown thrown values into a stable user-facing error string.
 *
 * @param error - The thrown value from a request path.
 * @param fallback - Message to use when the thrown value is not an Error.
 * @returns A safe message for UI state.
 */
function getErrorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

/**
 * Fetches JSON and throws a status-based error when the request fails.
 *
 * @param input - Request URL or object.
 * @param failureMessage - Prefix for non-2xx responses.
 * @param init - Optional fetch configuration.
 * @returns Parsed JSON payload.
 */
async function fetchJsonOrThrow<T>(
  input: RequestInfo | URL,
  failureMessage: string,
  init?: RequestInit
): Promise<T> {
  const response =
    init === undefined ? await fetch(input) : await fetch(input, init);
  if (!response.ok) {
    throw new Error(`${failureMessage}: ${response.status}`);
  }
  return (await response.json()) as T;
}

/**
 * Sends a JSON PATCH request and returns the parsed JSON response.
 *
 * @param path - Request path.
 * @param failureMessage - Prefix for non-2xx responses.
 * @param body - JSON body to serialize.
 * @returns Parsed JSON payload.
 */
function patchJsonOrThrow<T>(
  path: string,
  failureMessage: string,
  body: Record<string, unknown>
): Promise<T> {
  return fetchJsonOrThrow<T>(path, failureMessage, {
    method: "PATCH",
    headers: JSON_REQUEST_HEADERS,
    body: JSON.stringify(body),
  });
}

/**
 * Appends only slides whose ids are not already present in the list.
 *
 * @param existing - Current ordered slide list.
 * @param incoming - Newly fetched slides.
 * @returns Existing slides followed by new unique slides.
 */
function mergeUniqueSlides(
  existing: SlideSummary[],
  incoming: SlideSummary[]
): SlideSummary[] {
  const existingIDs = new Set(existing.map((slide) => slide.id));
  const newSlides = incoming.filter((slide) => !existingIDs.has(slide.id));
  return [...existing, ...newSlides];
}

/**
 * Sorts slides using the API display order contract.
 *
 * @param slides - Slides to sort.
 * @returns A new array sorted by date DESC, day_order ASC, id ASC.
 */
function sortSlides(slides: SlideSummary[]): SlideSummary[] {
  return [...slides].sort((a, b) => {
    const dateCmp = b.date.localeCompare(a.date);
    if (dateCmp !== 0) {
      return dateCmp;
    }
    const orderCmp = a.day_order.localeCompare(b.day_order);
    if (orderCmp !== 0) {
      return orderCmp;
    }
    return a.id.localeCompare(b.id);
  });
}

/**
 * Data-fetching hook for managing slide state in the web UI.
 *
 * Provides CRUD operations, pagination, project filtering, and local state
 * management for slides. All fetch calls are wrapped in try/catch with error
 * state propagation.
 *
 * @returns Slide state and mutation functions.
 */
export function useSlides(): UseSlidesReturn {
  const [slides, setSlides] = useState<SlideSummary[]>([]);
  const [selectedSlide, setSelectedSlide] = useState<SlideDetail | null>(null);
  const [projects, setProjects] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [isFetchingMore, setIsFetchingMore] = useState(false);

  const cursorRef = useRef<string | null>(null);
  const filterParamsRef = useRef<SlideFilterParams | undefined>(undefined);
  const isFetchingMoreRef = useRef(false);

  const updatePaginationState = useCallback((nextCursor: string | null) => {
    cursorRef.current = nextCursor;
    setHasMore(nextCursor !== null);
  }, []);

  const replaceSlidesPage = useCallback(
    (data: PaginatedResponse<SlideSummary>) => {
      setSlides(data.items);
      updatePaginationState(data.next_cursor);
    },
    [updatePaginationState]
  );

  const removeSlideLocally = useCallback((id: string) => {
    setSlides((prev) => prev.filter((slide) => slide.id !== id));
    setSelectedSlide((prev) => (prev?.id === id ? null : prev));
  }, []);

  const fetchSlides = useCallback(async (params?: SlideFilterParams) => {
    filterParamsRef.current = params;
    cursorRef.current = null;
    setIsLoading(true);
    setError(null);

    try {
      const data = await fetchJsonOrThrow<PaginatedResponse<SlideSummary>>(
        `/api/slides${buildQuery(params)}`,
        "Failed to fetch slides"
      );
      replaceSlidesPage(data);
    } catch (err) {
      setError(getErrorMessage(err, "Failed to fetch slides"));
    } finally {
      setIsLoading(false);
    }
  }, [replaceSlidesPage]);

  const fetchMore = useCallback(async () => {
    const currentCursor = cursorRef.current;
    if (!currentCursor || isFetchingMoreRef.current) {
      return;
    }

    isFetchingMoreRef.current = true;
    setIsFetchingMore(true);
    setIsLoading(true);
    setError(null);

    try {
      const data = await fetchJsonOrThrow<PaginatedResponse<SlideSummary>>(
        `/api/slides${buildQuery(filterParamsRef.current, currentCursor)}`,
        "Failed to fetch more slides"
      );
      setSlides((prev) => mergeUniqueSlides(prev, data.items));
      updatePaginationState(data.next_cursor);
    } catch (err) {
      setError(getErrorMessage(err, "Failed to fetch more slides"));
    } finally {
      isFetchingMoreRef.current = false;
      setIsFetchingMore(false);
      setIsLoading(false);
    }
  }, [updatePaginationState]);

  const selectSlide = useCallback(async (id: string) => {
    setIsLoading(true);
    setError(null);

    try {
      const data = await fetchJsonOrThrow<SlideDetailResponse>(
        `/api/slides/${encodeURIComponent(id)}`,
        "Failed to fetch slide"
      );
      setSelectedSlide(data.slide);
    } catch (err) {
      setError(getErrorMessage(err, "Failed to fetch slide detail"));
    } finally {
      setIsLoading(false);
    }
  }, []);

  const updateSlide = useCallback(
    async (id: string, body: Record<string, unknown>) => {
      setError(null);

      try {
        const data = await patchJsonOrThrow<SlideDetailResponse>(
          `/api/slides/${encodeURIComponent(id)}`,
          "Failed to update slide",
          body
        );

        // Update selectedSlide if it matches the updated slide
        setSelectedSlide((prev) => (prev?.id === id ? data.slide : prev));

        // Update the summary in the slides list
        setSlides((prev) =>
          prev.map((s) =>
            s.id === id
              ? {
                  ...s,
                  project_id: data.slide.project_id,
                  updated_at: data.slide.updated_at,
                  deleted_at: data.slide.deleted_at,
                }
              : s
          )
        );
        return true;
      } catch (err) {
        setError(getErrorMessage(err, "Failed to update slide"));
        return false;
      }
    },
    []
  );

  const performOptimisticRemoval = useCallback(
    async (
      id: string,
      path: string,
      method: "DELETE" | "POST",
      failureMessage: string
    ) => {
      setError(null);
      removeSlideLocally(id);

      try {
        await fetchJsonOrThrow<unknown>(path, failureMessage, { method });
        return true;
      } catch (err) {
        setError(getErrorMessage(err, failureMessage));
        await fetchSlides(filterParamsRef.current);
        return false;
      }
    },
    [fetchSlides, removeSlideLocally]
  );

  const deleteSlide = useCallback(async (id: string) => {
    return performOptimisticRemoval(
      id,
      `/api/slides/${encodeURIComponent(id)}`,
      "DELETE",
      "Failed to delete slide"
    );
  }, [performOptimisticRemoval]);

  const restoreSlide = useCallback(async (id: string) => {
    return performOptimisticRemoval(
      id,
      `/api/slides/${encodeURIComponent(id)}/restore`,
      "POST",
      "Failed to restore slide"
    );
  }, [performOptimisticRemoval]);

  const reorderSlide = useCallback(
    async (id: string, body: Record<string, unknown>) => {
      setError(null);

      try {
        const data = await patchJsonOrThrow<ReorderResponse>(
          `/api/slides/${encodeURIComponent(id)}/order`,
          "Failed to reorder slide",
          body
        );

        setSlides((prev) =>
          sortSlides(
            prev.map((s) =>
              s.id === id
                ? {
                    ...s,
                    date: data.date,
                    day_order: data.day_order,
                    updated_at: data.updated_at,
                  }
                : s
            )
          )
        );
      } catch (err) {
        setError(getErrorMessage(err, "Failed to reorder slide"));
      }
    },
    []
  );

  const fetchProjects = useCallback(async () => {
    setError(null);

    try {
      const data = await fetchJsonOrThrow<ProjectsResponse>(
        "/api/projects",
        "Failed to fetch projects"
      );
      setProjects(data.projects);
    } catch (err) {
      setError(getErrorMessage(err, "Failed to fetch projects"));
    }
  }, []);

  const refreshSlides = useCallback(async () => {
    updatePaginationState(null);
    setError(null);

    try {
      const data = await fetchJsonOrThrow<PaginatedResponse<SlideSummary>>(
        `/api/slides${buildQuery(filterParamsRef.current)}`,
        "Failed to refresh slides"
      );
      replaceSlidesPage(data);
    } catch (err) {
      setError(getErrorMessage(err, "Failed to refresh slides"));
    }
  }, [replaceSlidesPage, updatePaginationState]);

  return {
    slides,
    selectedSlide,
    projects,
    isLoading,
    error,
    hasMore,
    isFetchingMore,
    fetchSlides,
    fetchMore,
    selectSlide,
    updateSlide,
    deleteSlide,
    restoreSlide,
    reorderSlide,
    fetchProjects,
    refreshSlides,
  };
}
