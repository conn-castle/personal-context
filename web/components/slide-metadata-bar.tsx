"use client";

import { useEffect, useState, useRef, useCallback } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { SlideDetail } from "@/lib/types";
import { formatDate, formatRelativeDate } from "@/lib/slide-utils";
import { cn } from "@/lib/utils";
import {
  GitBranch,
  Link as LinkIcon,
  Calendar,
  FolderOpen,
  ExternalLink,
  MoreVertical,
  Share2,
  Download,
  Copy,
  Printer,
  Trash2,
  RotateCcw,
} from "lucide-react";

interface SlideMetadataBarProps {
  slide: SlideDetail | null;
  onDelete?: (id: string) => void;
  onRestore?: (id: string) => void;
}

function DateDisplay({ date }: { date: string }) {
  const [label, setLabel] = useState(() => formatDate(date));

  useEffect(() => {
    setLabel(formatRelativeDate(date));
  }, [date]);

  return <>{label}</>;
}

export function SlideMetadataBar({ slide, onDelete, onRestore }: SlideMetadataBarProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [canScrollLeft, setCanScrollLeft] = useState(false);
  const [canScrollRight, setCanScrollRight] = useState(false);

  const checkScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;

    setCanScrollLeft(el.scrollLeft > 0);
    setCanScrollRight(el.scrollLeft < el.scrollWidth - el.clientWidth - 1);
  }, []);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;

    checkScroll();
    el.addEventListener("scroll", checkScroll);

    const resizeObserver = new ResizeObserver(checkScroll);
    resizeObserver.observe(el);

    return () => {
      el.removeEventListener("scroll", checkScroll);
      resizeObserver.disconnect();
    };
  }, [checkScroll, slide]);

  if (!slide) {
    return (
      <div className="h-10 border-b border-border bg-card flex items-center justify-center">
        <span className="text-sm text-muted-foreground">
          No slide selected
        </span>
      </div>
    );
  }

  return (
    <div className="h-9 border-b border-border bg-card flex items-center overflow-hidden">
      {/* Scrollable content area with fade indicators */}
      <div className="flex-1 relative overflow-hidden h-full">
        {/* Left fade gradient */}
        <div
          className={cn(
            "absolute left-0 top-0 bottom-0.5 w-6 bg-gradient-to-r from-card to-transparent z-10 pointer-events-none transition-opacity duration-200",
            canScrollLeft ? "opacity-100" : "opacity-0"
          )}
        />

        {/* Right fade gradient */}
        <div
          className={cn(
            "absolute right-0 top-0 bottom-0.5 w-6 bg-gradient-to-l from-card to-transparent z-10 pointer-events-none transition-opacity duration-200",
            canScrollRight ? "opacity-100" : "opacity-0"
          )}
        />

        {/* Scrollable content */}
        <div
          ref={scrollRef}
          className="h-full overflow-x-auto overflow-y-hidden [&::-webkit-scrollbar]:h-0.5 [&::-webkit-scrollbar-track]:bg-transparent [&::-webkit-scrollbar-thumb]:bg-border [&::-webkit-scrollbar-thumb]:rounded-full hover:[&::-webkit-scrollbar-thumb]:bg-muted-foreground/30"
        >
          <div className="flex items-center gap-4 px-4 min-w-max h-full pb-0.5">
            {/* Date */}
            <div className="flex items-center gap-2 text-sm text-muted-foreground whitespace-nowrap">
              <Calendar className="w-3.5 h-3.5 flex-shrink-0" />
              <span>
                <DateDisplay date={slide.date} />
              </span>
            </div>

            {/* Project */}
            {slide.project_id && (
              <>
                <div className="w-px h-4 bg-border flex-shrink-0" />
                <div className="flex items-center gap-2 whitespace-nowrap">
                  <FolderOpen className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                  <Badge variant="secondary" className="font-mono text-xs">
                    {slide.project_id}
                  </Badge>
                </div>
              </>
            )}

            {/* Git info */}
            {slide.git_remote_url && (
              <>
                <div className="w-px h-4 bg-border flex-shrink-0" />
                <a
                  href={slide.git_remote_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-1.5 text-xs text-primary hover:underline whitespace-nowrap"
                >
                  <LinkIcon className="w-3 h-3 flex-shrink-0" />
                  <span className="max-w-[200px] truncate">
                    {slide.git_remote_url.replace(
                      "https://github.com/",
                      ""
                    )}
                  </span>
                  <ExternalLink className="w-3 h-3 flex-shrink-0" />
                </a>
              </>
            )}

            {slide.git_hash && (
              <>
                <div className="w-px h-4 bg-border flex-shrink-0" />
                <div className="flex items-center gap-2 text-xs text-muted-foreground whitespace-nowrap">
                  <GitBranch className="w-3 h-3 flex-shrink-0" />
                  <code className="font-mono bg-muted px-1.5 py-0.5 rounded">
                    {slide.git_hash.slice(0, 8)}
                  </code>
                  {slide.git_remote_url && (
                    <a
                      href={`${slide.git_remote_url}/commit/${slide.git_hash}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-primary hover:underline"
                    >
                      <ExternalLink className="w-3 h-3" />
                    </a>
                  )}
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Restore button when soft-deleted */}
      {slide.deleted_at && (
        <div className="flex-shrink-0 px-2">
          <Button
            variant="outline"
            size="sm"
            className="h-7 text-xs"
            onClick={() => onRestore?.(slide.id)}
          >
            <RotateCcw className="w-3.5 h-3.5 mr-1" />
            Restore
          </Button>
        </div>
      )}

      {/* Separator */}
      <div className="flex-shrink-0 w-px h-5 bg-border" />

      {/* More menu - fixed on the right */}
      <div className="flex-shrink-0 px-2">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon-sm" className="h-7 w-7">
              <MoreVertical className="w-4 h-4" />
              <span className="sr-only">More options</span>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-48">
            <DropdownMenuItem>
              <Share2 className="w-4 h-4" />
              Share slide
            </DropdownMenuItem>
            <DropdownMenuItem>
              <Download className="w-4 h-4" />
              Download slide
            </DropdownMenuItem>
            <DropdownMenuItem>
              <Copy className="w-4 h-4" />
              Copy link
            </DropdownMenuItem>
            <DropdownMenuItem>
              <Printer className="w-4 h-4" />
              Print
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={() => onDelete?.(slide.id)}>
              <Trash2 className="w-4 h-4" />
              Delete slide
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
