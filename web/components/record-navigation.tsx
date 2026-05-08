"use client";

import { useEffect, useMemo, useState } from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { RecordThumbnail } from "@/components/record-thumbnail";
import {
  formatDate,
  formatRelativeDate,
  groupRecordsByDateDesc,
} from "@/lib/record-utils";
import type { RecordSummary, ViewMode } from "@/lib/types";
import { cn } from "@/lib/utils";
import { LayoutGrid, LayoutList } from "lucide-react";

/** Client-side date label that handles hydration properly. */
function DateLabel({ date }: { date: string }) {
  const [label, setLabel] = useState(() => formatDate(date));

  useEffect(() => {
    setLabel(formatRelativeDate(date));
  }, [date]);

  return <>{label}</>;
}

interface RecordNavigationProps {
  records: RecordSummary[];
  selectedRecordId: string | null;
  onSelectRecord: (record: RecordSummary) => void;
  viewMode: ViewMode;
  onViewModeChange: (mode: ViewMode) => void;
  hasMore?: boolean;
  onLoadMore?: () => void;
}

export function RecordNavigation({
  records,
  selectedRecordId,
  onSelectRecord,
  viewMode,
  onViewModeChange,
  hasMore,
  onLoadMore,
}: RecordNavigationProps) {
  const groupedRecords = useMemo(() => groupRecordsByDateDesc(records), [records]);

  return (
    <div className="h-full flex flex-col bg-sidebar text-sidebar-foreground">
      {/* Header */}
      <div className="flex-shrink-0 py-3 pl-4 pr-3 border-b border-sidebar-border">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-semibold">Records</h2>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => onViewModeChange("strip")}
              className={cn(
                "h-7 w-7",
                viewMode === "strip"
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent/50"
              )}
              title="Strip view"
            >
              <LayoutList className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => onViewModeChange("grid")}
              className={cn(
                "h-7 w-7",
                viewMode === "grid"
                  ? "bg-sidebar-accent text-sidebar-accent-foreground"
                  : "text-sidebar-foreground/60 hover:text-sidebar-foreground hover:bg-sidebar-accent/50"
              )}
              title="Grid view"
            >
              <LayoutGrid className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </div>

      {/* Scrollable record list */}
      <div className="flex-1 min-h-0 overflow-hidden">
        <ScrollArea className="h-full" type="always">
          <div className="p-3 space-y-2">
            {groupedRecords.map((group) => (
              <div key={group.date}>
                {/* Date marker */}
                <div className="sticky top-0 z-[1] mb-1.5 -ml-3 pl-3 pr-0 py-1 bg-sidebar">
                  <div className="flex items-center gap-2">
                    <div className="h-px flex-1 bg-date-marker/30" />
                    <span className="text-xs font-medium text-date-marker whitespace-nowrap">
                      <DateLabel date={group.date} />
                    </span>
                    <div className="h-px flex-1 bg-date-marker/30" />
                  </div>
                </div>

                {/* Records for this date */}
                <div
                  className={cn(
                    viewMode === "grid"
                      ? "grid grid-cols-2 gap-2"
                      : "flex flex-col gap-2"
                  )}
                >
                  {group.records.map((record) => (
                    <RecordThumbnail
                      key={record.id}
                      record={record}
                      isSelected={record.id === selectedRecordId}
                      onClick={() => onSelectRecord(record)}
                    />
                  ))}
                </div>
              </div>
            ))}
            {hasMore && onLoadMore && (
              <Button
                variant="ghost"
                size="sm"
                className="w-full text-xs text-muted-foreground"
                onClick={onLoadMore}
              >
                Load more...
              </Button>
            )}
          </div>
        </ScrollArea>
      </div>
    </div>
  );
}
