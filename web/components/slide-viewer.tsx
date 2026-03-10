"use client";

import type { SlideDetail } from "@/lib/types";
import { ScaledSlideFrame } from "@/components/scaled-slide-frame";
import { FolderOpen } from "lucide-react";

interface SlideViewerProps {
  slide: SlideDetail | null;
  /** Whether the slide list is empty (no slides in the project). */
  isEmpty?: boolean;
}

export function SlideViewer({ slide, isEmpty }: SlideViewerProps) {
  if (!slide) {
    return (
      <div className="h-full flex items-center justify-center bg-background">
        <div className="text-center text-muted-foreground max-w-md px-6">
          <FolderOpen className="w-16 h-16 mx-auto mb-4 opacity-50" />
          {isEmpty ? (
            <>
              <p className="text-lg font-medium">Empty project</p>
              <p className="text-sm mt-1">
                No slides have been created yet. Use the CLI to add slides
                ({" "}
                <code className="bg-muted px-1.5 py-0.5 rounded text-xs font-mono">
                  pc add
                </code>
                {" "}) or have your agent create one to get started.
              </p>
            </>
          ) : (
            <>
              <p className="text-lg font-medium">Loading...</p>
              <p className="text-sm mt-1">Fetching slide content</p>
            </>
          )}
        </div>
      </div>
    );
  }

  return (
    <div data-testid="slide-viewer" className="h-full w-full bg-muted/30 p-4">
      <ScaledSlideFrame htmlContent={slide.html_content} showBorder align="top" />
    </div>
  );
}
