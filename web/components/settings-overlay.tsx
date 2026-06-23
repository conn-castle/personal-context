"use client";

import { useState, useEffect, useCallback } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import {
  Trash2,
  Github,
  Loader2,
} from "lucide-react";
import type { AppInfoResponse, StatsResponse } from "@/lib/types";

interface SettingsOverlayProps {
  open: boolean;
  onClose: () => void;
  syncVersion: number;
  lastSyncAt: string | null;
  syncError: string | null;
  onDataChanged: () => void;
}

/**
 * Settings dialog with Sync & Connection, Data Management, and About sections.
 *
 * Uses shadcn Dialog for accessibility (focus trapping, Escape key, ARIA role).
 * Fetches /api/info and /api/stats on open.
 */
export function SettingsOverlay({
  open,
  onClose,
  syncVersion,
  lastSyncAt,
  syncError,
  onDataChanged,
}: SettingsOverlayProps) {
  const [info, setInfo] = useState<AppInfoResponse | null>(null);
  const [stats, setStats] = useState<StatsResponse | null>(null);
  const [isPurging, setIsPurging] = useState(false);
  const [purgeError, setPurgeError] = useState<string | null>(null);

  // Fetch info and stats when dialog opens
  useEffect(() => {
    if (!open) return;

    setInfo(null);
    setStats(null);
    setPurgeError(null);

    const controller = new AbortController();

    void Promise.all([
      fetch("/api/info", { signal: controller.signal })
        .then((res) => (res.ok ? res.json() : null))
        .then((data: AppInfoResponse | null) => {
          if (data) setInfo(data);
        })
        .catch(() => {
          /* network error — info stays null */
        }),
      fetch("/api/stats", { signal: controller.signal })
        .then((res) => (res.ok ? res.json() : null))
        .then((data: StatsResponse | null) => {
          if (data) setStats(data);
        })
        .catch(() => {
          /* network error — stats stays null */
        }),
    ]);

    return () => controller.abort();
  }, [open]);

  const handlePurgeTrash = useCallback(async () => {
    setIsPurging(true);
    setPurgeError(null);
    try {
      const res = await fetch("/api/records/trash", { method: "DELETE" });
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw new Error(
          (body as Record<string, string> | null)?.error ?? "Purge failed"
        );
      }

      // Refresh stats
      const statsRes = await fetch("/api/stats");
      if (statsRes.ok) {
        const newStats = (await statsRes.json()) as StatsResponse;
        setStats(newStats);
      }

      onDataChanged();
    } catch (err) {
      setPurgeError(err instanceof Error ? err.message : "Purge failed");
    } finally {
      setIsPurging(false);
    }
  }, [onDataChanged]);

  /**
   * Formats an ISO timestamp for display.
   *
   * @param iso - ISO 8601 timestamp string.
   * @returns Human-readable date/time string.
   */
  function formatSyncTime(iso: string): string {
    // `new Date(iso)` returns an Invalid Date (it does not throw) for a
    // malformed string, and `toLocaleString()` on an Invalid Date yields the
    // literal "Invalid Date" rather than throwing — so a try/catch never fires.
    // Guard explicitly and fall back to the raw value for unparseable input.
    const parsed = new Date(iso);
    if (Number.isNaN(parsed.getTime())) {
      return iso;
    }
    return parsed.toLocaleString();
  }

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="sm:max-w-lg max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Settings</DialogTitle>
          <DialogDescription>
            View sync status, manage data, and app information.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6 py-2">
          {/* Section 1: Sync & Connection */}
          <section>
            <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
              Sync & Connection
            </h3>
            <div className="space-y-3">
              {/* Mode badge */}
              <div className="flex items-center justify-between">
                <span className="text-sm">Mode</span>
                {info ? (
                  <Badge
                    variant={info.mode === "local" ? "secondary" : "default"}
                    className={
                      info.mode === "local"
                        ? "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200"
                        : "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200"
                    }
                  >
                    {info.mode === "local" ? "Local" : "Cloud"}
                  </Badge>
                ) : (
                  <span className="text-xs text-muted-foreground">
                    Loading...
                  </span>
                )}
              </div>

              {/* Last sync time */}
              <div className="flex items-center justify-between">
                <span className="text-sm">Last synced</span>
                <span className="text-sm text-muted-foreground">
                  {lastSyncAt ? formatSyncTime(lastSyncAt) : "Never"}
                </span>
              </div>

              {syncError && (
                <div className="flex items-start justify-between gap-4">
                  <span className="text-sm">Sync error</span>
                  <span className="max-w-[65%] break-words text-right text-sm text-destructive">
                    {syncError}
                  </span>
                </div>
              )}

              {/* Sync version */}
              <div className="flex items-center justify-between">
                <span className="text-sm">Sync version</span>
                <span className="text-sm text-muted-foreground font-mono">
                  {syncVersion || "—"}
                </span>
              </div>
            </div>
          </section>

          <Separator />

          {/* Section 2: Data Management */}
          <section>
            <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
              Data Management
            </h3>
            <div className="space-y-3">
              {stats ? (
                <>
                  <div className="flex items-center justify-between">
                    <span className="text-sm">Total records</span>
                    <span className="text-sm text-muted-foreground font-mono">
                      {stats.total_records}
                    </span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-sm">Projects</span>
                    <span className="text-sm text-muted-foreground font-mono">
                      {stats.total_projects}
                    </span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-sm">Records in trash</span>
                    <span className="text-sm text-muted-foreground font-mono">
                      {stats.trashed_records}
                    </span>
                  </div>
                </>
              ) : (
                <p className="text-sm text-muted-foreground">Loading stats...</p>
              )}

              {purgeError && (
                <p className="text-sm text-destructive">{purgeError}</p>
              )}

              {/* Purge Trash with confirmation */}
              <AlertDialog>
                <AlertDialogTrigger asChild>
                  <Button
                    variant="destructive"
                    size="sm"
                    className="w-full"
                    disabled={
                      isPurging || !stats || stats.trashed_records === 0
                    }
                  >
                    {isPurging ? (
                      <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    ) : (
                      <Trash2 className="w-4 h-4 mr-2" />
                    )}
                    {isPurging
                      ? "Purging..."
                      : `Purge Trash${stats && stats.trashed_records > 0 ? ` (${stats.trashed_records})` : ""}`}
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>Permanently delete all trashed records?</AlertDialogTitle>
                    <AlertDialogDescription>
                      This will permanently delete{" "}
                      {stats?.trashed_records ?? 0} trashed record
                      {stats?.trashed_records === 1 ? "" : "s"} and all
                      associated figures and data files. This action cannot be
                      undone.
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>Cancel</AlertDialogCancel>
                    <AlertDialogAction
                      onClick={() => void handlePurgeTrash()}
                      className="bg-destructive text-white hover:bg-destructive/90"
                    >
                      Purge All
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </div>
          </section>

          <Separator />

          {/* Section 3: About */}
          <section>
            <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
              About
            </h3>
            <div className="space-y-2 text-sm text-muted-foreground">
              <div className="flex items-center justify-between">
                <span>Version</span>
                <span className="font-mono">
                  {info?.version ?? "—"}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span>Mode</span>
                <span>{info?.mode ?? "—"}</span>
              </div>
              <a
                href="https://github.com/conn-castle/personal-context"
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors pt-1"
              >
                <Github className="w-4 h-4" />
                <span>GitHub Repository</span>
              </a>
            </div>
          </section>
        </div>
      </DialogContent>
    </Dialog>
  );
}
