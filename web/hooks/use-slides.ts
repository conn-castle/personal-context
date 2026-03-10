"use client";

import { useState, useCallback, useRef } from "react";
import type {
  SlideSummary,
  SlideDetail,
  PaginatedResponse,
  ProjectsResponse,
  DeleteResponse,
  RestoreResponse,
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

  const fetchSlides = useCallback(async (params?: SlideFilterParams) => {
    filterParamsRef.current = params;
    cursorRef.current = null;
    setIsLoading(true);
    setError(null);

    try {
      const res = await fetch(`/api/slides${buildQuery(params)}`);
      if (!res.ok) {
        throw new Error(`Failed to fetch slides: ${res.status}`);
      }
      const data = (await res.json()) as PaginatedResponse<SlideSummary>;
      setSlides(data.items);
      cursorRef.current = data.next_cursor;
      setHasMore(data.next_cursor !== null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch slides");
    } finally {
      setIsLoading(false);
    }
  }, []);

  const fetchMore = useCallback(async () => {
    if (!cursorRef.current || isFetchingMoreRef.current) return;

    isFetchingMoreRef.current = true;
    setIsFetchingMore(true);
    setIsLoading(true);
    setError(null);

    try {
      const res = await fetch(
        `/api/slides${buildQuery(filterParamsRef.current, cursorRef.current)}`
      );
      if (!res.ok) {
        throw new Error(`Failed to fetch more slides: ${res.status}`);
      }
      const data = (await res.json()) as PaginatedResponse<SlideSummary>;
      setSlides((prev) => [...prev, ...data.items]);
      cursorRef.current = data.next_cursor;
      setHasMore(data.next_cursor !== null);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to fetch more slides"
      );
    } finally {
      isFetchingMoreRef.current = false;
      setIsFetchingMore(false);
      setIsLoading(false);
    }
  }, []);

  const selectSlide = useCallback(async (id: string) => {
    setIsLoading(true);
    setError(null);

    try {
      const res = await fetch(`/api/slides/${encodeURIComponent(id)}`);
      if (!res.ok) {
        throw new Error(`Failed to fetch slide: ${res.status}`);
      }
      const data = (await res.json()) as { slide: SlideDetail };
      setSelectedSlide(data.slide);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to fetch slide detail"
      );
    } finally {
      setIsLoading(false);
    }
  }, []);

  const updateSlide = useCallback(
    async (id: string, body: Record<string, unknown>) => {
      setError(null);

      try {
        const res = await fetch(`/api/slides/${encodeURIComponent(id)}`, {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
        if (!res.ok) {
          throw new Error(`Failed to update slide: ${res.status}`);
        }
        const data = (await res.json()) as {
          slide: SlideDetail;
          sync_version: number;
        };

        // Update selectedSlide if it matches the updated slide
        setSelectedSlide((prev) =>
          prev?.id === id ? data.slide : prev
        );

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
        setError(
          err instanceof Error ? err.message : "Failed to update slide"
        );
        return false;
      }
    },
    []
  );

  const deleteSlide = useCallback(async (id: string) => {
    setError(null);

    setSlides((prev) => prev.filter((s) => s.id !== id));
    setSelectedSlide((prev) => (prev?.id === id ? null : prev));

    try {
      const res = await fetch(`/api/slides/${encodeURIComponent(id)}`, {
        method: "DELETE",
      });
      if (!res.ok) {
        throw new Error(`Failed to delete slide: ${res.status}`);
      }
      (await res.json()) as DeleteResponse;
      return true;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete slide");
      await fetchSlides(filterParamsRef.current);
      return false;
    }
  }, [fetchSlides]);

  const restoreSlide = useCallback(async (id: string) => {
    setError(null);

    setSlides((prev) => prev.filter((s) => s.id !== id));
    setSelectedSlide((prev) => (prev?.id === id ? null : prev));

    try {
      const res = await fetch(
        `/api/slides/${encodeURIComponent(id)}/restore`,
        { method: "POST" }
      );
      if (!res.ok) {
        throw new Error(`Failed to restore slide: ${res.status}`);
      }
      (await res.json()) as RestoreResponse;
      return true;
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to restore slide"
      );
      await fetchSlides(filterParamsRef.current);
      return false;
    }
  }, [fetchSlides]);

  const reorderSlide = useCallback(
    async (id: string, body: Record<string, unknown>) => {
      setError(null);

      try {
        const res = await fetch(
          `/api/slides/${encodeURIComponent(id)}/order`,
          {
            method: "PATCH",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body),
          }
        );
        if (!res.ok) {
          throw new Error(`Failed to reorder slide: ${res.status}`);
        }
        const data = (await res.json()) as {
          id: string;
          date: string;
          day_order: string;
          updated_at: string;
          sync_version: number;
        };

        setSlides((prev) =>
          prev
            .map((s) =>
              s.id === id
                ? {
                    ...s,
                    date: data.date,
                    day_order: data.day_order,
                    updated_at: data.updated_at,
                  }
                : s
            )
            .sort((a, b) => {
              const dateCmp = b.date.localeCompare(a.date);
              if (dateCmp !== 0) return dateCmp;
              const orderCmp = a.day_order.localeCompare(b.day_order);
              if (orderCmp !== 0) return orderCmp;
              return a.id.localeCompare(b.id);
            })
        );
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "Failed to reorder slide"
        );
      }
    },
    []
  );

  const fetchProjects = useCallback(async () => {
    setError(null);

    try {
      const res = await fetch("/api/projects");
      if (!res.ok) {
        throw new Error(`Failed to fetch projects: ${res.status}`);
      }
      const data = (await res.json()) as ProjectsResponse;
      setProjects(data.projects);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to fetch projects"
      );
    }
  }, []);

  const refreshSlides = useCallback(async () => {
    cursorRef.current = null;
    setHasMore(false);
    setError(null);

    try {
      const res = await fetch(
        `/api/slides${buildQuery(filterParamsRef.current)}`
      );
      if (!res.ok) {
        throw new Error(`Failed to refresh slides: ${res.status}`);
      }
      const data = (await res.json()) as PaginatedResponse<SlideSummary>;
      setSlides(data.items);
      cursorRef.current = data.next_cursor;
      setHasMore(data.next_cursor !== null);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to refresh slides"
      );
    }
  }, []);

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
