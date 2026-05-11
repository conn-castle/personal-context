"use client";

import { cn } from "@/lib/utils";
import type { RecordSummary } from "@/lib/types";
import { ScaledRecordFrame } from "@/components/scaled-record-frame";
import { FileDown, FileText, Image as ImageIcon } from "lucide-react";

interface RecordThumbnailProps {
  record: RecordSummary;
  isSelected: boolean;
  onClick: () => void;
}

export function RecordThumbnail({
  record,
  isSelected,
  onClick,
}: RecordThumbnailProps) {
  const ariaLabel = `Record ${record.id} from ${record.date}, project ${record.project_id}`;

  return (
    <button
      onClick={onClick}
      aria-label={ariaLabel}
      aria-current={isSelected ? "true" : undefined}
      className={cn(
        "group relative w-full rounded-lg overflow-hidden transition-all duration-200",
        "border-2 hover:border-primary/50",
        "focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:ring-offset-sidebar",
        isSelected
          ? "border-primary shadow-lg shadow-primary/20"
          : "border-transparent"
      )}
    >
      {/* 16:9 aspect ratio container */}
      <div className="relative aspect-video bg-record-bg rounded-md overflow-hidden">
        {record.html_content === null ? (
          <div className="flex h-full w-full flex-col items-center justify-center gap-1 bg-muted/40 text-muted-foreground">
            <FileText className="h-5 w-5" />
            <span className="text-[11px] font-medium">Notes/data</span>
          </div>
        ) : (
          <ScaledRecordFrame
            htmlContent={record.html_content}
            className="pointer-events-none"
          />
        )}

        {/* Indicators */}
        <div className="absolute bottom-1 right-1 flex gap-1">
          {record.figure_count > 0 && (
            <div
              className="w-4 h-4 rounded bg-sidebar-accent/80 flex items-center justify-center"
              title={`${record.figure_count} figure(s)`}
            >
              <ImageIcon className="w-2.5 h-2.5 text-sidebar-foreground" />
            </div>
          )}
          {record.data_file_count > 0 && (
            <div
              className="w-4 h-4 rounded bg-sidebar-accent/80 flex items-center justify-center"
              title={`${record.data_file_count} file(s)`}
            >
              <FileDown className="w-2.5 h-2.5 text-sidebar-foreground" />
            </div>
          )}
        </div>
      </div>
    </button>
  );
}
