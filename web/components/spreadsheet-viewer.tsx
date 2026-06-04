"use client";

import {
  useState,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
} from "react";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@/components/ui/resizable";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { RecordNavigation } from "@/components/record-navigation";
import { RecordViewer } from "@/components/record-viewer";
import { RecordDetails } from "@/components/record-details";
import { CollapsedDetailsStrip } from "@/components/collapsed-details-strip";
import { RecordMetadataBar } from "@/components/record-metadata-bar";
import { ProjectPicker } from "@/components/project-picker";
import { RecordDatePicker } from "@/components/record-date-picker";
import { SettingsOverlay } from "@/components/settings-overlay";
import { useRecords } from "@/hooks/use-records";
import { useSyncManager } from "@/hooks/use-sync-manager";
import { useLocalStorage } from "@/hooks/use-local-storage";
import type { ViewMode, PanelVisibility, RecordSummary } from "@/lib/types";
import { cn } from "@/lib/utils";
import {
  PanelLeftClose,
  PanelLeftOpen,
  PanelRightClose,
  PanelRightOpen,
  PanelTopClose,
  PanelTopOpen,
  ChevronLeft,
  ChevronRight,
  Keyboard,
  Moon,
  Sun,
  Settings,
} from "lucide-react";
import { useTheme } from "next-themes";

/**
 * Returns the selected record index or -1 when the selection is absent.
 *
 * @param records - The current filtered record list.
 * @param selectedRecordId - The currently selected record ID.
 * @returns The zero-based index in `records`, or -1 when not found.
 */
function getSelectedRecordIndex(
  records: RecordSummary[],
  selectedRecordId: string | undefined
): number {
  if (!selectedRecordId) {
    return -1;
  }
  return records.findIndex((record) => record.id === selectedRecordId);
}

/**
 * Finds the exact-date record or the nearest record by date when no exact match exists.
 *
 * @param records - The current filtered record list.
 * @param targetDate - The requested calendar date.
 * @returns The best record to jump to, if one exists.
 */
function findNearestRecordByDate(
  records: RecordSummary[],
  targetDate: Date
): RecordSummary | undefined {
  const targetDateStr = targetDate.toISOString().split("T")[0];
  const exactMatch = records.find((record) => record.date === targetDateStr);
  if (exactMatch) {
    return exactMatch;
  }

  const [nearestRecord] = [...records].sort((left, right) => {
    const leftDiff = Math.abs(new Date(left.date).getTime() - targetDate.getTime());
    const rightDiff = Math.abs(new Date(right.date).getTime() - targetDate.getTime());
    return leftDiff - rightDiff;
  });
  return nearestRecord;
}

export function SpreadsheetViewer() {
  const {
    records,
    selectedRecord,
    projects,
    error,
    hasMore,
    fetchRecords,
    fetchMore,
    selectRecord,
    updateRecord,
    deleteRecord,
    restoreRecord,
    fetchProjects,
    refreshRecords,
  } = useRecords();

  const handleSyncData = useCallback(() => {
    void refreshRecords();
    void fetchProjects();
  }, [refreshRecords, fetchProjects]);

  const { markMutation, version, lastSyncAt, syncError } =
    useSyncManager({
      onSyncData: handleSyncData,
    });

  // Persisted UI state via localStorage
  const [selectedProjects, setSelectedProjects, selectedProjectsLoaded] =
    useLocalStorage<string[]>("selectedProjects", []);
  const [viewMode, setViewMode] = useLocalStorage<ViewMode>("viewMode", "strip");
  const [panelVisibility, setPanelVisibility] =
    useLocalStorage<PanelVisibility>("panelVisibility", {
      navigation: true,
      details: true,
      metadata: true,
    });
  const [lastSelectedRecordId, setLastSelectedRecordId, lastSelectedRecordLoaded] =
    useLocalStorage<string | null>("lastSelectedRecordId", null);
  const [detailsActiveTab, setDetailsActiveTab] = useState("notes");
  const [settingsOpen, setSettingsOpen] = useState(false);

  const { theme, setTheme } = useTheme();
  const isDarkMode = theme === "dark";

  const togglePanel = useCallback(
    (panel: keyof PanelVisibility) => {
      setPanelVisibility((value) => ({ ...value, [panel]: !value[panel] }));
    },
    [setPanelVisibility]
  );

  // Fetch projects on mount
  useEffect(() => {
    void fetchProjects();
  }, [fetchProjects]);

  // Fetch records on mount and when project filter changes
  useEffect(() => {
    if (!selectedProjectsLoaded) {
      return;
    }

    const projectFilter =
      selectedProjects.length === 1 ? selectedProjects[0] : undefined;
    void fetchRecords({ project: projectFilter });
  }, [selectedProjects, selectedProjectsLoaded, fetchRecords]);

  // Client-side multi-project filtering (for multi-select beyond API support)
  const filteredRecords = useMemo(() => {
    if (
      selectedProjects.length === 0 ||
      selectedProjects.length === projects.length
    ) {
      return records;
    }
    return records.filter(
      (record) =>
        record.project_id && selectedProjects.includes(record.project_id)
    );
  }, [records, selectedProjects, projects.length]);

  const filteredRecordsRef = useRef<RecordSummary[]>(filteredRecords);
  const selectedRecordIdRef = useRef<string | undefined>(selectedRecord?.id);

  useLayoutEffect(() => {
    filteredRecordsRef.current = filteredRecords;
    selectedRecordIdRef.current = selectedRecord?.id;
  }, [filteredRecords, selectedRecord?.id]);

  const selectAndRememberRecord = useCallback(
    (id: string) => {
      setLastSelectedRecordId(id);
      return selectRecord(id);
    },
    [selectRecord, setLastSelectedRecordId]
  );

  const selectRelativeRecord = useCallback(
    (offset: -1 | 1) => {
      const currentRecords = filteredRecordsRef.current;
      const currentIndex = getSelectedRecordIndex(
        currentRecords,
        selectedRecordIdRef.current
      );
      const target = currentRecords[currentIndex + offset];
      if (target) {
        void selectAndRememberRecord(target.id);
      }
    },
    [selectAndRememberRecord]
  );

  // Auto-select: prefer last-selected record, fall back to most recent
  useEffect(() => {
    if (!selectedProjectsLoaded || !lastSelectedRecordLoaded) {
      return;
    }

    if (filteredRecords.length > 0 && !selectedRecord) {
      const target =
        lastSelectedRecordId &&
        filteredRecords.find((s) => s.id === lastSelectedRecordId)
          ? lastSelectedRecordId
          : filteredRecords[0].id;
      void selectAndRememberRecord(target);
    }
  }, [
    filteredRecords,
    lastSelectedRecordId,
    lastSelectedRecordLoaded,
    selectedProjectsLoaded,
    selectedRecord,
    selectAndRememberRecord,
  ]);

  // Keyboard navigation
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLTextAreaElement
      ) {
        return;
      }

      switch (e.key) {
        case "ArrowLeft":
        case "ArrowUp":
          e.preventDefault();
          selectRelativeRecord(-1);
          break;
        case "ArrowRight":
        case "ArrowDown":
          e.preventDefault();
          selectRelativeRecord(1);
          break;
        case "[":
          e.preventDefault();
          togglePanel("navigation");
          break;
        case "]":
          e.preventDefault();
          togglePanel("details");
          break;
        case "\\":
          e.preventDefault();
          togglePanel("metadata");
          break;
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [selectRelativeRecord, togglePanel]);

  const toggleNavigation = useCallback(() => {
    togglePanel("navigation");
  }, [togglePanel]);

  const toggleDetails = useCallback(() => {
    togglePanel("details");
  }, [togglePanel]);

  const openDetailsToTab = useCallback((tab: string) => {
    setDetailsActiveTab(tab);
    setPanelVisibility((v) => ({ ...v, details: true }));
  }, [setPanelVisibility]);

  const toggleMetadata = useCallback(() => {
    togglePanel("metadata");
  }, [togglePanel]);

  const goToPrevious = useCallback(() => {
    selectRelativeRecord(-1);
  }, [selectRelativeRecord]);

  const goToNext = useCallback(() => {
    selectRelativeRecord(1);
  }, [selectRelativeRecord]);

  const goToDate = useCallback(
    (date: Date) => {
      const target = findNearestRecordByDate(filteredRecords, date);
      if (target) {
        void selectAndRememberRecord(target.id);
      }
    },
    [filteredRecords, selectAndRememberRecord]
  );

  const handleSelectRecord = useCallback(
    (record: RecordSummary) => {
      void selectAndRememberRecord(record.id);
    },
    [selectAndRememberRecord]
  );

  const handleUpdateRecord = useCallback(
    async (id: string, body: Record<string, unknown>) => {
      const didUpdate = await updateRecord(id, body);
      if (didUpdate) {
        markMutation();
      }
      return didUpdate;
    },
    [updateRecord, markMutation]
  );

  const handleDeleteRecord = useCallback(
    async (id: string) => {
      const didDelete = await deleteRecord(id);
      if (didDelete) {
        markMutation();
      }
    },
    [deleteRecord, markMutation]
  );

  const handleRestoreRecord = useCallback(
    async (id: string) => {
      const didRestore = await restoreRecord(id);
      if (didRestore) {
        markMutation();
      }
    },
    [restoreRecord, markMutation]
  );

  const currentIndex = getSelectedRecordIndex(filteredRecords, selectedRecord?.id);
  const hasPrevious = currentIndex > 0;
  const hasNext = currentIndex < filteredRecords.length - 1;

  return (
    <>
      <SettingsOverlay
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        syncVersion={version}
        lastSyncAt={lastSyncAt}
        syncError={syncError}
        onDataChanged={handleSyncData}
      />
      <div className="h-screen flex flex-col bg-background">
        {/* Error banner */}
        {error && (
          <div className="flex-shrink-0 bg-destructive px-4 py-2 text-center text-sm text-white">
            {error}
          </div>
        )}

        {/* Header */}
        <header className="flex-shrink-0 h-12 border-b border-border bg-card flex items-center justify-between px-4">
          <div className="flex items-center gap-3">
            <h1 className="text-sm font-semibold text-foreground">
              Personal Context
            </h1>

            <div className="w-px h-5 bg-border" />

            <ProjectPicker
              projects={projects}
              selectedProjects={selectedProjects}
              onSelectionChange={setSelectedProjects}
            />

            <span className="text-xs text-muted-foreground">
              {filteredRecords.length === records.length
                ? `${filteredRecords.length} records`
                : `${filteredRecords.length} of ${records.length} records`}
            </span>
          </div>

          <div className="flex items-center gap-1">
            {/* Navigation controls */}
            <div className="flex items-center gap-1 mr-2">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={goToPrevious}
                    disabled={!hasPrevious}
                  >
                    <ChevronLeft className="w-4 h-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Previous record (&larr;)</TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={goToNext}
                    disabled={!hasNext}
                  >
                    <ChevronRight className="w-4 h-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Next record (&rarr;)</TooltipContent>
              </Tooltip>

              <RecordDatePicker
                records={filteredRecords}
                onSelectDate={goToDate}
              />
            </div>

            <div className="w-px h-5 bg-border mx-1" />

            {/* Panel toggles */}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={toggleNavigation}
                  className={cn(
                    panelVisibility.navigation && "text-primary"
                  )}
                >
                  {panelVisibility.navigation ? (
                    <PanelLeftClose className="w-4 h-4" />
                  ) : (
                    <PanelLeftOpen className="w-4 h-4" />
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {panelVisibility.navigation
                  ? "Hide navigation"
                  : "Show navigation"}{" "}
                ([)
              </TooltipContent>
            </Tooltip>

            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={toggleMetadata}
                  className={cn(
                    panelVisibility.metadata && "text-primary"
                  )}
                >
                  {panelVisibility.metadata ? (
                    <PanelTopClose className="w-4 h-4" />
                  ) : (
                    <PanelTopOpen className="w-4 h-4" />
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {panelVisibility.metadata
                  ? "Hide metadata"
                  : "Show metadata"}{" "}
                (\)
              </TooltipContent>
            </Tooltip>

            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={toggleDetails}
                  className={cn(
                    panelVisibility.details && "text-primary"
                  )}
                >
                  {panelVisibility.details ? (
                    <PanelRightClose className="w-4 h-4" />
                  ) : (
                    <PanelRightOpen className="w-4 h-4" />
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {panelVisibility.details
                  ? "Hide details"
                  : "Show details"}{" "}
                (])
              </TooltipContent>
            </Tooltip>

            <div className="w-px h-5 bg-border mx-1" />

            {/* Theme toggle */}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => setTheme(isDarkMode ? "light" : "dark")}
                >
                  {isDarkMode ? (
                    <Sun className="w-4 h-4" />
                  ) : (
                    <Moon className="w-4 h-4" />
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {isDarkMode ? "Light mode" : "Dark mode"}
              </TooltipContent>
            </Tooltip>

            {/* Keyboard shortcuts hint */}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon-sm">
                  <Keyboard className="w-4 h-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent className="p-3">
                <div className="space-y-2 text-xs">
                  <div className="flex items-center gap-2">
                    <div className="flex gap-1">
                      <kbd className="inline-flex items-center justify-center min-w-[20px] h-5 px-1.5 bg-background text-foreground border border-background/20 rounded text-[10px] font-mono font-medium shadow-sm">
                        &larr;
                      </kbd>
                      <kbd className="inline-flex items-center justify-center min-w-[20px] h-5 px-1.5 bg-background text-foreground border border-background/20 rounded text-[10px] font-mono font-medium shadow-sm">
                        &rarr;
                      </kbd>
                    </div>
                    <span>Navigate records</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <kbd className="inline-flex items-center justify-center min-w-[20px] h-5 px-1.5 bg-background text-foreground border border-background/20 rounded text-[10px] font-mono font-medium shadow-sm">
                      [
                    </kbd>
                    <span>Toggle navigation panel</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <kbd className="inline-flex items-center justify-center min-w-[20px] h-5 px-1.5 bg-background text-foreground border border-background/20 rounded text-[10px] font-mono font-medium shadow-sm">
                      \
                    </kbd>
                    <span>Toggle metadata bar</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <kbd className="inline-flex items-center justify-center min-w-[20px] h-5 px-1.5 bg-background text-foreground border border-background/20 rounded text-[10px] font-mono font-medium shadow-sm">
                      ]
                    </kbd>
                    <span>Toggle details panel</span>
                  </div>
                </div>
              </TooltipContent>
            </Tooltip>

            {/* Settings */}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => setSettingsOpen(true)}
                >
                  <Settings className="w-4 h-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Settings</TooltipContent>
            </Tooltip>
          </div>
        </header>

        {/* Main content area with resizable panels */}
        <div className="flex-1 min-h-0">
          <ResizablePanelGroup
            id="main-panel-group"
            key={`panels-${panelVisibility.navigation}-${panelVisibility.details}`}
            direction="horizontal"
            className="h-full"
          >
            {/* Navigation panel */}
            {panelVisibility.navigation && (
              <>
                <ResizablePanel
                  id="nav-panel"
                  defaultSize="18%"
                  minSize="12%"
                  maxSize="50%"
                  className="min-w-0"
                >
                  <RecordNavigation
                    records={filteredRecords}
                    selectedRecordId={selectedRecord?.id ?? null}
                    onSelectRecord={handleSelectRecord}
                    viewMode={viewMode}
                    onViewModeChange={setViewMode}
                    hasMore={hasMore}
                    onLoadMore={fetchMore}
                  />
                </ResizablePanel>
                <ResizableHandle withHandle />
              </>
            )}

            {/* Main record viewer with metadata bar */}
            <ResizablePanel
              id="viewer-panel"
              defaultSize={
                panelVisibility.navigation && panelVisibility.details
                  ? "54%"
                  : panelVisibility.navigation
                    ? "82%"
                    : panelVisibility.details
                      ? "72%"
                      : "100%"
              }
              minSize="30%"
            >
              <div className="h-full flex flex-col">
                {panelVisibility.metadata && (
                  <RecordMetadataBar
                    record={selectedRecord}
                    onDelete={handleDeleteRecord}
                    onRestore={handleRestoreRecord}
                    isEmpty={filteredRecords.length === 0}
                  />
                )}
                <div className="flex-1 min-h-0">
                  <RecordViewer record={selectedRecord} isEmpty={filteredRecords.length === 0} />
                </div>
              </div>
            </ResizablePanel>

            {/* Details panel */}
            {panelVisibility.details ? (
              <>
                <ResizableHandle withHandle />
                <ResizablePanel
                  id="details-panel"
                  defaultSize="28%"
                  minSize="20%"
                  maxSize="45%"
                >
                  <RecordDetails
                    record={selectedRecord}
                    activeTab={detailsActiveTab}
                    onTabChange={setDetailsActiveTab}
                    onUpdateRecord={handleUpdateRecord}
                    isEmpty={filteredRecords.length === 0}
                  />
                </ResizablePanel>
              </>
            ) : (
              <ResizablePanel
                id="collapsed-details-panel"
                defaultSize={48}
                minSize={48}
                maxSize={48}
                groupResizeBehavior="preserve-pixel-size"
                disabled
                className="min-w-12 max-w-12"
              >
                <CollapsedDetailsStrip
                  record={selectedRecord}
                  onOpenTab={openDetailsToTab}
                />
              </ResizablePanel>
            )}
          </ResizablePanelGroup>
        </div>
      </div>
    </>
  );
}
