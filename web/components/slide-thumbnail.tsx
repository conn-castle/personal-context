"use client";

import { cn } from "@/lib/utils";
import type { SlideSummary } from "@/lib/types";
import { Image as ImageIcon, FileDown } from "lucide-react";

interface SlideThumbnailProps {
  slide: SlideSummary;
  isSelected: boolean;
  onClick: () => void;
}

export function SlideThumbnail({
  slide,
  isSelected,
  onClick,
}: SlideThumbnailProps) {
  return (
    <button
      onClick={onClick}
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
      <div className="relative aspect-video bg-slide-bg rounded-md overflow-hidden">
        {/* Slide ID and date placeholder (summary doesn't include html_content) */}
        <div className="absolute inset-0 flex flex-col items-center justify-center p-2 pointer-events-none">
          <span className="text-[10px] font-mono text-muted-foreground/70 truncate max-w-full">
            {slide.id}
          </span>
          {slide.project_id && (
            <span className="text-[9px] text-primary/60 truncate max-w-full mt-0.5">
              {slide.project_id}
            </span>
          )}
        </div>

        {/* Indicators */}
        <div className="absolute bottom-1 right-1 flex gap-1">
          {slide.figure_count > 0 && (
            <div
              className="w-4 h-4 rounded bg-sidebar-accent/80 flex items-center justify-center"
              title={`${slide.figure_count} figure(s)`}
            >
              <ImageIcon className="w-2.5 h-2.5 text-sidebar-foreground" />
            </div>
          )}
          {slide.data_file_count > 0 && (
            <div
              className="w-4 h-4 rounded bg-sidebar-accent/80 flex items-center justify-center"
              title={`${slide.data_file_count} file(s)`}
            >
              <FileDown className="w-2.5 h-2.5 text-sidebar-foreground" />
            </div>
          )}
        </div>
      </div>
    </button>
  );
}
