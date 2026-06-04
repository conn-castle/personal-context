"use client";

import { useState, useCallback, useRef } from "react";
import type {
  RecordSummary,
  RecordDetail,
  PaginatedResponse,
  ProjectsResponse,
  ReorderResponse,
} from "@/lib/types";

/** Filter parameters persisted across refreshes. */
interface RecordFilterParams {
  project?: string;
  deleted?: boolean;
}

/** Return value of useRecords. */
export interface UseRecordsReturn {
  /** The current list of record summaries. */
  records: RecordSummary[];
  /** The currently selected record detail, or null. */
  selectedRecord: RecordDetail | null;
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
  /** Fetch the first page of records, optionally with new filter params. */
  fetchRecords: (params?: RecordFilterParams) => Promise<void>;
  /** Load the next page and append results to the records list. */
  fetchMore: () => Promise<void>;
  /** Fetch full detail for a single record and set it as selectedRecord. */
  selectRecord: (id: string) => Promise<void>;
  /** Update editable fields on a record (project_id, notes, git fields). */
  updateRecord: (
    id: string,
    body: Record<string, unknown>
  ) => Promise<boolean>;
  /** Soft-delete a record and remove it from the local list. */
  deleteRecord: (id: string) => Promise<boolean>;
  /** Restore a soft-deleted record and remove it from the local list. */
  restoreRecord: (id: string) => Promise<boolean>;
  /** Reorder a record (change date/position). */
  reorderRecord: (id: string, body: Record<string, unknown>) => Promise<void>;
  /** Fetch distinct project IDs. */
  fetchProjects: () => Promise<void>;
  /** Re-fetch records with the same filter params (called by sync manager). */
  refreshRecords: () => Promise<void>;
}

/**
 * Builds a query string from record list filter params.
 *
 * @param params - Optional filter params (project, deleted).
 * @param cursor - Optional pagination cursor.
 * @returns A URL query string including a leading '?'.
 */
function buildQuery(params?: RecordFilterParams, cursor?: string): string {
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

interface RecordDetailResponse {
  record: RecordDetail;
}

interface FetchRecordsOptions {
  preserveExistingError?: boolean;
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
 * Appends only records whose ids are not already present in the list.
 *
 * @param existing - Current ordered record list.
 * @param incoming - Newly fetched records.
 * @returns Existing records followed by new unique records.
 */
function mergeUniqueRecords(
  existing: RecordSummary[],
  incoming: RecordSummary[]
): RecordSummary[] {
  const existingIDs = new Set(existing.map((record) => record.id));
  const newRecords = incoming.filter((record) => !existingIDs.has(record.id));
  return [...existing, ...newRecords];
}

/**
 * Sorts records using the API display order contract.
 *
 * @param records - Records to sort.
 * @returns A new array sorted by date DESC, day_order ASC, id ASC.
 */
function sortRecords(records: RecordSummary[]): RecordSummary[] {
  return [...records].sort((a, b) => {
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
 * Data-fetching hook for managing record state in the web UI.
 *
 * Provides CRUD operations, pagination, project filtering, and local state
 * management for records. All fetch calls are wrapped in try/catch with error
 * state propagation.
 *
 * @returns Record state and mutation functions.
 */
export function useRecords(): UseRecordsReturn {
  const [records, setRecords] = useState<RecordSummary[]>([]);
  const [selectedRecord, setSelectedRecord] = useState<RecordDetail | null>(null);
  const [projects, setProjects] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [isFetchingMore, setIsFetchingMore] = useState(false);

  const cursorRef = useRef<string | null>(null);
  const filterParamsRef = useRef<RecordFilterParams | undefined>(undefined);
  const isFetchingMoreRef = useRef(false);
  const replacementRequestSeqRef = useRef(0);
  const loadingRequestSeqRef = useRef(0);

  const beginLoading = useCallback(() => {
    const loadingSeq = loadingRequestSeqRef.current + 1;
    loadingRequestSeqRef.current = loadingSeq;
    setIsLoading(true);
    return loadingSeq;
  }, []);

  const finishLoading = useCallback((loadingSeq: number) => {
    if (loadingRequestSeqRef.current === loadingSeq) {
      setIsLoading(false);
    }
  }, []);

  const updatePaginationState = useCallback((nextCursor: string | null) => {
    cursorRef.current = nextCursor;
    setHasMore(nextCursor !== null);
  }, []);

  const replaceRecordsPage = useCallback(
    (data: PaginatedResponse<RecordSummary>) => {
      setRecords(data.items);
      setSelectedRecord((prev) =>
        prev && data.items.some((record) => record.id === prev.id) ? prev : null
      );
      updatePaginationState(data.next_cursor);
    },
    [updatePaginationState]
  );

  const removeRecordLocally = useCallback((id: string) => {
    setRecords((prev) => prev.filter((record) => record.id !== id));
    setSelectedRecord((prev) => (prev?.id === id ? null : prev));
  }, []);

  const fetchRecords = useCallback(
    async (
      params?: RecordFilterParams,
      options: FetchRecordsOptions = {}
    ) => {
      const requestSeq = replacementRequestSeqRef.current + 1;
      replacementRequestSeqRef.current = requestSeq;
      const loadingSeq = beginLoading();
      filterParamsRef.current = params;
      cursorRef.current = null;
      if (!options.preserveExistingError) {
        setError(null);
      }

      try {
        const data = await fetchJsonOrThrow<PaginatedResponse<RecordSummary>>(
          `/api/records${buildQuery(params)}`,
          "Failed to fetch records"
        );
        if (replacementRequestSeqRef.current !== requestSeq) {
          return;
        }
        replaceRecordsPage(data);
      } catch (err) {
        if (replacementRequestSeqRef.current !== requestSeq) {
          return;
        }
        setError(getErrorMessage(err, "Failed to fetch records"));
      } finally {
        finishLoading(loadingSeq);
      }
    },
    [beginLoading, finishLoading, replaceRecordsPage]
  );

  const fetchMore = useCallback(async () => {
    const currentCursor = cursorRef.current;
    if (!currentCursor || isFetchingMoreRef.current) {
      return;
    }
    const requestSeq = replacementRequestSeqRef.current;
    const loadingSeq = beginLoading();

    isFetchingMoreRef.current = true;
    setIsFetchingMore(true);
    setError(null);

    try {
      const data = await fetchJsonOrThrow<PaginatedResponse<RecordSummary>>(
        `/api/records${buildQuery(filterParamsRef.current, currentCursor)}`,
        "Failed to fetch more records"
      );
      if (replacementRequestSeqRef.current !== requestSeq) {
        return;
      }
      setRecords((prev) => mergeUniqueRecords(prev, data.items));
      updatePaginationState(data.next_cursor);
    } catch (err) {
      if (replacementRequestSeqRef.current !== requestSeq) {
        return;
      }
      setError(getErrorMessage(err, "Failed to fetch more records"));
    } finally {
      isFetchingMoreRef.current = false;
      setIsFetchingMore(false);
      finishLoading(loadingSeq);
    }
  }, [beginLoading, finishLoading, updatePaginationState]);

  const selectRecord = useCallback(async (id: string) => {
    const loadingSeq = beginLoading();
    setError(null);

    try {
      const data = await fetchJsonOrThrow<RecordDetailResponse>(
        `/api/records/${encodeURIComponent(id)}`,
        "Failed to fetch record"
      );
      setSelectedRecord(data.record);
    } catch (err) {
      setError(getErrorMessage(err, "Failed to fetch record detail"));
    } finally {
      finishLoading(loadingSeq);
    }
  }, [beginLoading, finishLoading]);

  const updateRecord = useCallback(
    async (id: string, body: Record<string, unknown>) => {
      setError(null);

      try {
        const data = await patchJsonOrThrow<RecordDetailResponse>(
          `/api/records/${encodeURIComponent(id)}`,
          "Failed to update record",
          body
        );

        // Update selectedRecord if it matches the updated record
        setSelectedRecord((prev) => (prev?.id === id ? data.record : prev));

        // Update the summary in the records list
        setRecords((prev) =>
          prev.map((s) =>
            s.id === id
              ? {
                  ...s,
                  project_id: data.record.project_id,
                  updated_at: data.record.updated_at,
                  deleted_at: data.record.deleted_at,
                }
              : s
          )
        );
        return true;
      } catch (err) {
        setError(getErrorMessage(err, "Failed to update record"));
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
      removeRecordLocally(id);

      try {
        await fetchJsonOrThrow<unknown>(path, failureMessage, { method });
        return true;
      } catch (err) {
        setError(getErrorMessage(err, failureMessage));
        await fetchRecords(filterParamsRef.current, {
          preserveExistingError: true,
        });
        return false;
      }
    },
    [fetchRecords, removeRecordLocally]
  );

  const deleteRecord = useCallback(async (id: string) => {
    return performOptimisticRemoval(
      id,
      `/api/records/${encodeURIComponent(id)}`,
      "DELETE",
      "Failed to delete record"
    );
  }, [performOptimisticRemoval]);

  const restoreRecord = useCallback(async (id: string) => {
    return performOptimisticRemoval(
      id,
      `/api/records/${encodeURIComponent(id)}/restore`,
      "POST",
      "Failed to restore record"
    );
  }, [performOptimisticRemoval]);

  const reorderRecord = useCallback(
    async (id: string, body: Record<string, unknown>) => {
      setError(null);

      try {
        const data = await patchJsonOrThrow<ReorderResponse>(
          `/api/records/${encodeURIComponent(id)}/order`,
          "Failed to reorder record",
          body
        );

        setRecords((prev) =>
          sortRecords(
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
        setError(getErrorMessage(err, "Failed to reorder record"));
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

  const refreshRecords = useCallback(async () => {
    const requestSeq = replacementRequestSeqRef.current + 1;
    replacementRequestSeqRef.current = requestSeq;
    updatePaginationState(null);
    setError(null);

    try {
      const data = await fetchJsonOrThrow<PaginatedResponse<RecordSummary>>(
        `/api/records${buildQuery(filterParamsRef.current)}`,
        "Failed to refresh records"
      );
      if (replacementRequestSeqRef.current !== requestSeq) {
        return;
      }
      replaceRecordsPage(data);
    } catch (err) {
      if (replacementRequestSeqRef.current !== requestSeq) {
        return;
      }
      setError(getErrorMessage(err, "Failed to refresh records"));
    }
  }, [replaceRecordsPage, updatePaginationState]);

  return {
    records,
    selectedRecord,
    projects,
    isLoading,
    error,
    hasMore,
    isFetchingMore,
    fetchRecords,
    fetchMore,
    selectRecord,
    updateRecord,
    deleteRecord,
    restoreRecord,
    reorderRecord,
    fetchProjects,
    refreshRecords,
  };
}
