"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { SyncVersionResponse, SyncChangesResponse } from "@/lib/types";

/** Configuration options for the sync manager hook. */
export interface UseSyncManagerOptions {
  /** Whether the sync manager is active. Defaults to true. */
  enabled?: boolean;
  /** Callback invoked when new sync data arrives (version changed). */
  onSyncData?: (data: SyncChangesResponse) => void;
  /** Cooldown between version checks (ms). Layers 2–4 respect this. Default 30000. */
  cooldownMs?: number;
  /** Idle poll interval when idle < deepIdleThresholdMs (ms). Default 60000. */
  idlePollMs?: number;
  /** Idle poll interval when idle > deepIdleThresholdMs (ms). Default 300000. */
  deepIdlePollMs?: number;
  /** Time (ms) after which the user is considered deeply idle. Default 600000 (10 min). */
  deepIdleThresholdMs?: number;
}

/** Return value of useSyncManager. */
export interface UseSyncManagerReturn {
  /** Layer 1: Trigger a manual sync. Always fires, ignores cooldown. */
  syncNow: () => Promise<void>;
  /** Mark that the current client just performed a mutation. Prevents the next version-change from triggering a data fetch (self-inflicted sync prevention). */
  markMutation: () => void;
  /** The last known sync version from the server. */
  version: number;
  /** Whether a sync check is currently in flight. */
  isSyncing: boolean;
  /** ISO timestamp of the last successful data sync, or null if never synced. */
  lastSyncAt: string | null;
}

const COOLDOWN_MS = 30_000;
const IDLE_POLL_MS = 60_000;
const DEEP_IDLE_POLL_MS = 300_000;
const DEEP_IDLE_THRESHOLD_MS = 600_000;

function isSyncVersionResponse(value: unknown): value is SyncVersionResponse {
  if (!value || typeof value !== "object") {
    return false;
  }

  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.version === "number" &&
    typeof candidate.updated_at === "string"
  );
}

function isSyncChangesResponse(value: unknown): value is SyncChangesResponse {
  if (!value || typeof value !== "object") {
    return false;
  }

  const candidate = value as Record<string, unknown>;
  return (
    Array.isArray(candidate.items) &&
    typeof candidate.server_now === "string"
  );
}

async function fetchJsonOrThrow<T>(
  input: RequestInfo | URL,
  isValid: (value: unknown) => value is T
): Promise<T> {
  const res = await fetch(input);
  if (!res.ok) {
    throw new Error(`Sync request failed: ${res.status}`);
  }

  const payload = await res.json();
  if (!isValid(payload)) {
    throw new Error("Sync response payload was invalid");
  }

  return payload;
}

/**
 * Smart layered sync polling hook.
 *
 * Four layers trigger version checks via GET /api/sync/version:
 * 1. Manual (syncNow) — always fires, ignores cooldown
 * 2. Interaction (click) — respects cooldown
 * 3. Tab visibility (visibilitychange → visible) — respects cooldown
 * 4. Idle polling — 60s when idle < 10min, 5min when idle > 10min, stops when tab hidden
 *
 * On version change: fetches changes via GET /api/sync/changes?since=<timestamp>,
 * then passes data to onSyncData callback.
 *
 * Self-inflicted sync prevention: calling markMutation() before a version bump
 * prevents the next version-change from triggering an unnecessary data fetch.
 *
 * @param options - Configuration options.
 * @returns Sync control functions and state.
 */
export function useSyncManager(
  options: UseSyncManagerOptions = {}
): UseSyncManagerReturn {
  const {
    enabled = true,
    onSyncData,
    cooldownMs = COOLDOWN_MS,
    idlePollMs = IDLE_POLL_MS,
    deepIdlePollMs = DEEP_IDLE_POLL_MS,
    deepIdleThresholdMs = DEEP_IDLE_THRESHOLD_MS,
  } = options;

  const [version, setVersion] = useState(0);
  const [isSyncing, setIsSyncing] = useState(false);
  const [lastSyncAt, setLastSyncAt] = useState<string | null>(null);

  // Mutable refs to avoid re-render dependency loops
  const lastCheckRef = useRef(0);
  const lastActivityRef = useRef(Date.now());
  const selfMutationRef = useRef(false);
  const versionRef = useRef(0);
  const lastSyncAtRef = useRef<string | null>(null);
  const isSyncingRef = useRef(false);
  const onSyncDataRef = useRef(onSyncData);
  onSyncDataRef.current = onSyncData;

  /**
   * Core sync: check version, optionally fetch changes.
   *
   * @param ignoreCooldown - If true, skip cooldown check (Layer 1).
   */
  const doSync = useCallback(
    async (ignoreCooldown: boolean) => {
      if (!enabled) return;
      if (isSyncingRef.current) return;

      const now = Date.now();
      if (!ignoreCooldown && now - lastCheckRef.current < cooldownMs) return;
      lastCheckRef.current = now;

      isSyncingRef.current = true;
      setIsSyncing(true);
      try {
        const versionData = await fetchJsonOrThrow<SyncVersionResponse>(
          "/api/sync/version",
          isSyncVersionResponse
        );

        if (versionData.version !== versionRef.current) {
          // Self-inflicted mutation: update version but skip data fetch
          if (selfMutationRef.current) {
            selfMutationRef.current = false;
            if (!lastSyncAtRef.current) {
              lastSyncAtRef.current = versionData.updated_at;
              setLastSyncAt(versionData.updated_at);
            }
            versionRef.current = versionData.version;
            setVersion(versionData.version);
            return;
          }

          // Fetch incremental changes if we have a previous sync timestamp
          if (lastSyncAtRef.current) {
            const changesData = await fetchJsonOrThrow<SyncChangesResponse>(
              `/api/sync/changes?since=${encodeURIComponent(lastSyncAtRef.current)}`,
              isSyncChangesResponse
            );
            onSyncDataRef.current?.(changesData);
            lastSyncAtRef.current = changesData.server_now;
            setLastSyncAt(changesData.server_now);
          } else {
            // First observed version change — trigger a refresh path before
            // advancing the cursor so the initial post-load delta is not lost.
            onSyncDataRef.current?.({
              items: [],
              server_now: versionData.updated_at,
            });
            lastSyncAtRef.current = versionData.updated_at;
            setLastSyncAt(versionData.updated_at);
          }

          versionRef.current = versionData.version;
          setVersion(versionData.version);
        }
      } catch {
        // Swallow errors — sync failures are non-fatal
      } finally {
        isSyncingRef.current = false;
        setIsSyncing(false);
      }
    },
    [enabled, cooldownMs]
  );

  // Layer 1: Manual refresh (always fires, ignores cooldown)
  const syncNow = useCallback(async () => {
    await doSync(true);
  }, [doSync]);

  // Self-inflicted mutation flag
  const markMutation = useCallback(() => {
    selfMutationRef.current = true;
  }, []);

  // Layer 2: Interaction (click) — respects cooldown
  useEffect(() => {
    if (!enabled) return;
    const handler = () => {
      lastActivityRef.current = Date.now();
      void doSync(false);
    };
    document.addEventListener("click", handler);
    return () => {
      document.removeEventListener("click", handler);
    };
  }, [enabled, doSync]);

  // Layer 3: Tab visibility — respects cooldown
  useEffect(() => {
    if (!enabled) return;
    const handler = () => {
      if (document.visibilityState === "visible") {
        void doSync(false);
      }
    };
    document.addEventListener("visibilitychange", handler);
    return () => {
      document.removeEventListener("visibilitychange", handler);
    };
  }, [enabled, doSync]);

  // Layer 4: Idle polling — adaptive interval, stops when tab hidden
  useEffect(() => {
    if (!enabled) return;

    const tick = () => {
      if (document.visibilityState === "hidden") return;
      void doSync(false);
    };

    // Use the shorter interval and check idle duration inside the callback
    // to switch between normal idle (60s) and deep idle (5min) adaptively
    let timerId: ReturnType<typeof setTimeout>;

    const schedule = () => {
      const idle = Date.now() - lastActivityRef.current;
      const interval =
        idle > deepIdleThresholdMs ? deepIdlePollMs : idlePollMs;
      timerId = setTimeout(() => {
        tick();
        schedule();
      }, interval);
    };

    schedule();

    return () => {
      clearTimeout(timerId);
    };
  }, [enabled, doSync, idlePollMs, deepIdlePollMs, deepIdleThresholdMs]);

  return { syncNow, markMutation, version, isSyncing, lastSyncAt };
}
