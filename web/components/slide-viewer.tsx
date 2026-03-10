"use client";

import type { SlideDetail } from "@/lib/types";
import { ScaledSlideFrame } from "@/components/scaled-slide-frame";

interface SlideViewerProps {
  slide: SlideDetail | null;
}

export function SlideViewer({ slide }: SlideViewerProps) {
  if (!slide) {
    return (
      <div className="h-full flex items-center justify-center bg-background">
        <div className="text-center text-muted-foreground">
          <svg
            className="w-16 h-16 mx-auto mb-4 opacity-50"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={1.5}
              d="M9 17V7m0 10a2 2 0 01-2 2H5a2 2 0 01-2-2V7a2 2 0 012-2h2a2 2 0 012 2m0 10a2 2 0 002 2h2a2 2 0 002-2M9 7a2 2 0 012-2h2a2 2 0 012 2m0 10V7m0 10a2 2 0 002 2h2a2 2 0 002-2V7a2 2 0 00-2-2h-2a2 2 0 00-2 2"
            />
          </svg>
          <p className="text-lg font-medium">Select a slide</p>
          <p className="text-sm mt-1">
            Choose a slide from the navigation panel
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full w-full bg-muted/30 p-4">
      <ScaledSlideFrame htmlContent={slide.html_content} showBorder align="top" />
    </div>
  );
}
