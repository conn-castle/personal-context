"use client";

import type { SlideDetail } from "@/lib/types";
import { ScaledSlideFrame } from "@/components/scaled-slide-frame";
import { FileDown, FileText, FolderOpen, Image as ImageIcon } from "lucide-react";

interface SlideViewerProps {
  slide: SlideDetail | null;
  /** Whether the slide list is empty (no slides in the project). */
  isEmpty?: boolean;
}

function pluralize(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
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

  if (slide.html_content === null) {
    const notePreview = slide.notes?.trim() || "No notes";
    return (
      <div
        data-testid="slide-viewer"
        className="h-full w-full bg-muted/30 p-6"
      >
        <div className="flex h-full w-full items-center justify-center">
          <div className="w-full max-w-3xl">
            <div className="mb-5 flex items-center gap-3 text-muted-foreground">
              <FileText className="h-6 w-6" />
              <h2 className="text-xl font-semibold text-foreground">
                Notes/data-only record
              </h2>
            </div>
            <p className="max-h-48 overflow-hidden whitespace-pre-wrap text-sm leading-6 text-foreground">
              {notePreview}
            </p>
            <div className="mt-6 flex flex-wrap gap-3 text-sm text-muted-foreground">
              <span className="inline-flex items-center gap-2">
                <ImageIcon className="h-4 w-4" />
                {pluralize(slide.figures.length, "figure", "figures")}
              </span>
              <span className="inline-flex items-center gap-2">
                <FileDown className="h-4 w-4" />
                {pluralize(slide.data_files.length, "data file", "data files")}
              </span>
            </div>
          </div>
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
