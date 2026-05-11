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
import type { RecordDetail } from "@/lib/types";
import { formatDate, formatRelativeDate } from "@/lib/record-utils";
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

interface RecordMetadataBarProps {
  record: RecordDetail | null;
  onDelete?: (id: string) => void;
  onRestore?: (id: string) => void;
  /** Whether the record list is empty (no records in the project). */
  isEmpty?: boolean;
}

function DateDisplay({ date }: { date: string }) {
  const [label, setLabel] = useState(() => formatDate(date));

  useEffect(() => {
    setLabel(formatRelativeDate(date));
  }, [date]);

  return <>{label}</>;
}

export function RecordMetadataBar({ record, onDelete, onRestore, isEmpty }: RecordMetadataBarProps) {
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
  }, [checkScroll, record]);

  if (!record) {
    return (
      <div className="h-10 border-b border-border bg-card flex items-center justify-center">
        <span className="text-sm text-muted-foreground">
          {isEmpty ? "No records in this project" : "Loading..."}
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
                <DateDisplay date={record.date} />
              </span>
            </div>

            {/* Project */}
            {record.project_id && (
              <>
                <div className="w-px h-4 bg-border flex-shrink-0" />
                <div className="flex items-center gap-2 whitespace-nowrap">
                  <FolderOpen className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                  <Badge variant="secondary" className="font-mono text-xs">
                    {record.project_id}
                  </Badge>
                </div>
              </>
            )}

            {/* Git info */}
            {record.git_remote_url && (
              <>
                <div className="w-px h-4 bg-border flex-shrink-0" />
                <a
                  href={record.git_remote_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-1.5 text-xs text-primary hover:underline whitespace-nowrap"
                >
                  <LinkIcon className="w-3 h-3 flex-shrink-0" />
                  <span className="max-w-[200px] truncate">
                    {record.git_remote_url.replace(
                      "https://github.com/",
                      ""
                    )}
                  </span>
                  <ExternalLink className="w-3 h-3 flex-shrink-0" />
                </a>
              </>
            )}

            {record.git_hash && (
              <>
                <div className="w-px h-4 bg-border flex-shrink-0" />
                <div className="flex items-center gap-2 text-xs text-muted-foreground whitespace-nowrap">
                  <GitBranch className="w-3 h-3 flex-shrink-0" />
                  <code className="font-mono bg-muted px-1.5 py-0.5 rounded">
                    {record.git_hash.slice(0, 8)}
                  </code>
                  {record.git_remote_url && (
                    <a
                      href={`${record.git_remote_url}/commit/${record.git_hash}`}
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
      {record.deleted_at && (
        <div className="flex-shrink-0 px-2">
          <Button
            variant="outline"
            size="sm"
            className="h-7 text-xs"
            onClick={() => onRestore?.(record.id)}
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
            {/* Share/Download/Copy/Print are not yet implemented. Render as
                disabled so users see them as in-progress rather than as
                broken interactive items. Re-enable when handlers are wired. */}
            <DropdownMenuItem disabled aria-disabled="true">
              <Share2 className="w-4 h-4" />
              Share record
            </DropdownMenuItem>
            <DropdownMenuItem disabled aria-disabled="true">
              <Download className="w-4 h-4" />
              Download record
            </DropdownMenuItem>
            <DropdownMenuItem disabled aria-disabled="true">
              <Copy className="w-4 h-4" />
              Copy link
            </DropdownMenuItem>
            <DropdownMenuItem disabled aria-disabled="true">
              <Printer className="w-4 h-4" />
              Print
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={() => onDelete?.(record.id)}>
              <Trash2 className="w-4 h-4" />
              Delete record
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
