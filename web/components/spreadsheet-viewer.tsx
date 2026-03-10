"use client";

import { useState, useCallback, useEffect, useMemo } from "react";
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
import { SlideNavigation } from "@/components/slide-navigation";
import { SlideViewer } from "@/components/slide-viewer";
import { SlideDetails } from "@/components/slide-details";
import { CollapsedDetailsStrip } from "@/components/collapsed-details-strip";
import { SlideMetadataBar } from "@/components/slide-metadata-bar";
import { ProjectPicker } from "@/components/project-picker";
import { SlideDatePicker } from "@/components/slide-date-picker";
import { SettingsOverlay } from "@/components/settings-overlay";
import { useSlides } from "@/hooks/use-slides";
import { useSyncManager } from "@/hooks/use-sync-manager";
import type { ViewMode, PanelVisibility, SlideSummary } from "@/lib/types";
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

export function SpreadsheetViewer() {
  const {
    slides,
    selectedSlide,
    projects,
    error,
    hasMore,
    fetchSlides,
    fetchMore,
    selectSlide,
    updateSlide,
    deleteSlide,
    restoreSlide,
    fetchProjects,
    refreshSlides,
  } = useSlides();

  const handleSyncData = useCallback(() => {
    void refreshSlides();
    void fetchProjects();
  }, [refreshSlides, fetchProjects]);

  const { markMutation } = useSyncManager({
    onSyncData: handleSyncData,
  });

  // Multi-select project filter (empty = all projects)
  const [selectedProjects, setSelectedProjects] = useState<string[]>([]);
  const [viewMode, setViewMode] = useState<ViewMode>("strip");
  const [panelVisibility, setPanelVisibility] = useState<PanelVisibility>({
    navigation: true,
    details: true,
    metadata: true,
  });
  const [detailsActiveTab, setDetailsActiveTab] = useState("notes");
  const [settingsOpen, setSettingsOpen] = useState(false);

  const { theme, setTheme } = useTheme();
  const isDarkMode = theme === "dark";

  // Fetch projects on mount
  useEffect(() => {
    void fetchProjects();
  }, [fetchProjects]);

  // Fetch slides on mount and when project filter changes
  useEffect(() => {
    const projectFilter =
      selectedProjects.length === 1 ? selectedProjects[0] : undefined;
    void fetchSlides({ project: projectFilter });
  }, [selectedProjects, fetchSlides]);

  // Client-side multi-project filtering (for multi-select beyond API support)
  const filteredSlides = useMemo(() => {
    if (
      selectedProjects.length === 0 ||
      selectedProjects.length === projects.length
    ) {
      return slides;
    }
    return slides.filter(
      (slide) =>
        slide.project_id && selectedProjects.includes(slide.project_id)
    );
  }, [slides, selectedProjects, projects.length]);

  // Auto-select the most recent slide when slides load and none is selected
  useEffect(() => {
    if (filteredSlides.length > 0 && !selectedSlide) {
      void selectSlide(filteredSlides[0].id);
    }
  }, [filteredSlides, selectedSlide, selectSlide]);

  // Keyboard navigation
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLTextAreaElement
      ) {
        return;
      }

      const currentIndex = filteredSlides.findIndex(
        (s) => s.id === selectedSlide?.id
      );

      switch (e.key) {
        case "ArrowLeft":
        case "ArrowUp":
          e.preventDefault();
          if (currentIndex > 0) {
            void selectSlide(filteredSlides[currentIndex - 1].id);
          }
          break;
        case "ArrowRight":
        case "ArrowDown":
          e.preventDefault();
          if (currentIndex < filteredSlides.length - 1) {
            void selectSlide(filteredSlides[currentIndex + 1].id);
          }
          break;
        case "[":
          e.preventDefault();
          setPanelVisibility((v) => ({ ...v, navigation: !v.navigation }));
          break;
        case "]":
          e.preventDefault();
          setPanelVisibility((v) => ({ ...v, details: !v.details }));
          break;
        case "\\":
          e.preventDefault();
          setPanelVisibility((v) => ({ ...v, metadata: !v.metadata }));
          break;
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [selectedSlide, filteredSlides, selectSlide]);

  const toggleNavigation = useCallback(() => {
    setPanelVisibility((v) => ({ ...v, navigation: !v.navigation }));
  }, []);

  const toggleDetails = useCallback(() => {
    setPanelVisibility((v) => ({ ...v, details: !v.details }));
  }, []);

  const openDetailsToTab = useCallback((tab: string) => {
    setDetailsActiveTab(tab);
    setPanelVisibility((v) => ({ ...v, details: true }));
  }, []);

  const toggleMetadata = useCallback(() => {
    setPanelVisibility((v) => ({ ...v, metadata: !v.metadata }));
  }, []);

  const goToPrevious = useCallback(() => {
    const currentIndex = filteredSlides.findIndex(
      (s) => s.id === selectedSlide?.id
    );
    if (currentIndex > 0) {
      void selectSlide(filteredSlides[currentIndex - 1].id);
    }
  }, [selectedSlide, filteredSlides, selectSlide]);

  const goToNext = useCallback(() => {
    const currentIndex = filteredSlides.findIndex(
      (s) => s.id === selectedSlide?.id
    );
    if (currentIndex < filteredSlides.length - 1) {
      void selectSlide(filteredSlides[currentIndex + 1].id);
    }
  }, [selectedSlide, filteredSlides, selectSlide]);

  const goToDate = useCallback(
    (date: Date) => {
      const dateStr = date.toISOString().split("T")[0];
      const slideOnDate = filteredSlides.find((s) => s.date === dateStr);
      if (slideOnDate) {
        void selectSlide(slideOnDate.id);
        return;
      }
      // Find nearest slide
      const sorted = [...filteredSlides].sort((a, b) => {
        const diffA = Math.abs(
          new Date(a.date).getTime() - date.getTime()
        );
        const diffB = Math.abs(
          new Date(b.date).getTime() - date.getTime()
        );
        return diffA - diffB;
      });
      if (sorted.length > 0) {
        void selectSlide(sorted[0].id);
      }
    },
    [filteredSlides, selectSlide]
  );

  const handleSelectSlide = useCallback(
    (slide: SlideSummary) => {
      void selectSlide(slide.id);
    },
    [selectSlide]
  );

  const handleUpdateSlide = useCallback(
    async (id: string, body: Record<string, unknown>) => {
      const didUpdate = await updateSlide(id, body);
      if (didUpdate) {
        markMutation();
      }
    },
    [updateSlide, markMutation]
  );

  const handleDeleteSlide = useCallback(
    async (id: string) => {
      const didDelete = await deleteSlide(id);
      if (didDelete) {
        markMutation();
      }
    },
    [deleteSlide, markMutation]
  );

  const handleRestoreSlide = useCallback(
    async (id: string) => {
      const didRestore = await restoreSlide(id);
      if (didRestore) {
        markMutation();
      }
    },
    [restoreSlide, markMutation]
  );

  const currentIndex = filteredSlides.findIndex(
    (s) => s.id === selectedSlide?.id
  );
  const hasPrevious = currentIndex > 0;
  const hasNext = currentIndex < filteredSlides.length - 1;

  return (
    <>
      <SettingsOverlay
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
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
              {filteredSlides.length === slides.length
                ? `${filteredSlides.length} slides`
                : `${filteredSlides.length} of ${slides.length} slides`}
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
                <TooltipContent>Previous slide (&larr;)</TooltipContent>
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
                <TooltipContent>Next slide (&rarr;)</TooltipContent>
              </Tooltip>

              <SlideDatePicker
                slides={filteredSlides}
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
                    <span>Navigate slides</span>
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
                  <SlideNavigation
                    slides={filteredSlides}
                    selectedSlideId={selectedSlide?.id ?? null}
                    onSelectSlide={handleSelectSlide}
                    viewMode={viewMode}
                    onViewModeChange={setViewMode}
                    hasMore={hasMore}
                    onLoadMore={fetchMore}
                  />
                </ResizablePanel>
                <ResizableHandle withHandle />
              </>
            )}

            {/* Main slide viewer with metadata bar */}
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
                  <SlideMetadataBar
                    slide={selectedSlide}
                    onDelete={handleDeleteSlide}
                    onRestore={handleRestoreSlide}
                    isEmpty={slides.length === 0}
                  />
                )}
                <div className="flex-1 min-h-0">
                  <SlideViewer slide={selectedSlide} isEmpty={slides.length === 0} />
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
                  <SlideDetails
                    slide={selectedSlide}
                    activeTab={detailsActiveTab}
                    onTabChange={setDetailsActiveTab}
                    onUpdateSlide={handleUpdateSlide}
                    isEmpty={slides.length === 0}
                  />
                </ResizablePanel>
              </>
            ) : (
              <CollapsedDetailsStrip
                slide={selectedSlide}
                onOpenTab={openDetailsToTab}
              />
            )}
          </ResizablePanelGroup>
        </div>
      </div>
    </>
  );
}
