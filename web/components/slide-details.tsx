"use client";

import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { SlideDetail } from "@/lib/types";
import { formatFileSize } from "@/lib/slide-utils";
import {
  FileText,
  Image as ImageIcon,
  FileDown,
  Copy,
  Check,
  Pencil,
  X,
} from "lucide-react";
import { useState, useRef, useEffect } from "react";
import { Textarea } from "@/components/ui/textarea";
import { AssetCard } from "@/components/asset-card";

interface SlideDetailsProps {
  slide: SlideDetail | null;
  activeTab?: string;
  onTabChange?: (tab: string) => void;
  onUpdateSlide?: (id: string, body: Record<string, unknown>) => void;
}

export function SlideDetails(props: SlideDetailsProps) {
  const { slide, activeTab, onTabChange, onUpdateSlide } = props;
  if (!slide) {
    return (
      <div className="h-full flex items-center justify-center bg-card text-card-foreground p-4">
        <div className="text-center text-muted-foreground">
          <FileText className="w-12 h-12 mx-auto mb-3 opacity-50" />
          <p className="text-sm">No slide selected</p>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col bg-card text-card-foreground @container">
      {/* Tabs for Notes, Figures, Files */}
      <Tabs
        value={activeTab || "notes"}
        onValueChange={onTabChange}
        className="flex-1 flex flex-col min-h-0"
      >
        <TabsList className="flex-shrink-0 mx-4 mt-3 bg-muted w-[calc(100%-2rem)]">
          <TabsTrigger
            value="notes"
            className="flex-1 flex items-center justify-center gap-1.5 text-xs"
          >
            <FileText className="w-3.5 h-3.5 flex-shrink-0" />
            <span className="hidden @[280px]:inline">Notes</span>
          </TabsTrigger>
          <TabsTrigger
            value="figures"
            className="flex-1 flex items-center justify-center gap-1.5 text-xs"
          >
            <ImageIcon className="w-3.5 h-3.5 flex-shrink-0" />
            <span className="hidden @[280px]:inline">Figures</span>
            {slide.figures.length > 0 && (
              <Badge
                variant="secondary"
                className="ml-1 h-4 px-1 text-[10px] flex-shrink-0"
              >
                {slide.figures.length}
              </Badge>
            )}
          </TabsTrigger>
          <TabsTrigger
            value="files"
            className="flex-1 flex items-center justify-center gap-1.5 text-xs"
          >
            <FileDown className="w-3.5 h-3.5 flex-shrink-0" />
            <span className="hidden @[280px]:inline">Files</span>
            {slide.data_files.length > 0 && (
              <Badge
                variant="secondary"
                className="ml-1 h-4 px-1 text-[10px] flex-shrink-0"
              >
                {slide.data_files.length}
              </Badge>
            )}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="notes" className="flex-1 min-h-0 mt-0 p-0">
          <NotesEditor
            key={slide.id}
            slideId={slide.id}
            notes={slide.notes}
            onSave={onUpdateSlide}
          />
        </TabsContent>

        <TabsContent value="figures" className="flex-1 min-h-0 mt-0 p-0">
          <ScrollArea className="h-full">
            <div className="p-4">
              {slide.figures.length > 0 ? (
                <div className="space-y-2">
                  {slide.figures.map((figure) => (
                    <AssetCard
                      key={figure.filename}
                      type="figure"
                      filename={figure.filename}
                      description={figure.alt_text}
                      size={
                        figure.size != null
                          ? formatFileSize(figure.size)
                          : undefined
                      }
                      onDownload={() => {
                        /* Download figure */
                      }}
                      onDelete={() => {
                        /* Delete figure */
                      }}
                    />
                  ))}
                </div>
              ) : (
                <div className="text-center text-muted-foreground py-8">
                  <ImageIcon className="w-10 h-10 mx-auto mb-2 opacity-50" />
                  <p className="text-sm">No figures attached</p>
                </div>
              )}
            </div>
          </ScrollArea>
        </TabsContent>

        <TabsContent value="files" className="flex-1 min-h-0 mt-0 p-0">
          <ScrollArea className="h-full">
            <div className="p-4">
              {slide.data_files.length > 0 ? (
                <div className="space-y-2">
                  {slide.data_files.map((file) => (
                    <AssetCard
                      key={file.filename}
                      type="file"
                      filename={file.filename}
                      description={file.description}
                      size={
                        file.size != null
                          ? formatFileSize(file.size)
                          : undefined
                      }
                      onDownload={() => {
                        /* Download file */
                      }}
                      onDelete={() => {
                        /* Delete file */
                      }}
                    />
                  ))}
                </div>
              ) : (
                <div className="text-center text-muted-foreground py-8">
                  <FileDown className="w-10 h-10 mx-auto mb-2 opacity-50" />
                  <p className="text-sm">No data files attached</p>
                </div>
              )}
            </div>
          </ScrollArea>
        </TabsContent>
      </Tabs>
    </div>
  );
}

/** Notes editor with view/edit modes. */
function NotesEditor({
  slideId,
  notes,
  onSave,
}: {
  slideId: string;
  notes: string | null;
  onSave?: (id: string, body: Record<string, unknown>) => void;
}) {
  const [isEditing, setIsEditing] = useState(false);
  const [editedNotes, setEditedNotes] = useState(notes || "");
  const [copied, setCopied] = useState(false);
  const copyTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (copyTimeoutRef.current) {
        clearTimeout(copyTimeoutRef.current);
      }
    };
  }, []);

  const handleCopy = async () => {
    if (!notes) return;
    try {
      await navigator.clipboard.writeText(notes);
      setCopied(true);
      if (copyTimeoutRef.current) {
        clearTimeout(copyTimeoutRef.current);
      }
      copyTimeoutRef.current = setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard write failed — do not show "Copied!" feedback
    }
  };

  const handleEdit = () => {
    setEditedNotes(notes || "");
    setIsEditing(true);
  };

  const handleCancel = () => {
    setEditedNotes(notes || "");
    setIsEditing(false);
  };

  const handleSave = () => {
    onSave?.(slideId, { notes: editedNotes || null });
    setIsEditing(false);
  };

  if (!notes && !isEditing) {
    return (
      <div className="h-full flex flex-col items-center justify-center text-muted-foreground p-4">
        <FileText className="w-10 h-10 mb-2 opacity-50" />
        <p className="text-sm mb-3">No notes for this slide</p>
        <Button variant="outline" size="sm" onClick={() => setIsEditing(true)}>
          <Pencil className="w-3.5 h-3.5 mr-1.5" />
          Add notes
        </Button>
      </div>
    );
  }

  if (isEditing) {
    return (
      <div className="h-full flex flex-col p-4">
        <div className="flex items-center justify-between mb-2">
          <span className="text-xs text-muted-foreground">
            Editing markdown
          </span>
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="sm" onClick={handleCancel}>
              <X className="w-3.5 h-3.5 mr-1" />
              Cancel
            </Button>
            <Button size="sm" onClick={handleSave}>
              <Check className="w-3.5 h-3.5 mr-1" />
              Save
            </Button>
          </div>
        </div>
        <Textarea
          value={editedNotes}
          onChange={(e) => setEditedNotes(e.target.value)}
          className="flex-1 font-mono text-sm resize-none"
          placeholder="Write your notes in markdown..."
        />
      </div>
    );
  }

  return (
    <ScrollArea className="h-full">
      <div className="p-4">
        <div className="relative">
          <div className="absolute top-0 right-0 flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon-sm"
              className="h-7 w-7 text-muted-foreground hover:text-foreground"
              onClick={handleCopy}
              title={copied ? "Copied!" : "Copy notes"}
            >
              {copied ? (
                <Check className="w-3.5 h-3.5" />
              ) : (
                <Copy className="w-3.5 h-3.5" />
              )}
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              className="h-7 w-7 text-muted-foreground hover:text-foreground"
              onClick={handleEdit}
              title="Edit notes"
            >
              <Pencil className="w-3.5 h-3.5" />
            </Button>
          </div>
          <div className="prose prose-sm dark:prose-invert max-w-none pr-16">
            <MarkdownRenderer content={notes!} />
          </div>
        </div>
      </div>
    </ScrollArea>
  );
}

/** Simple markdown renderer for notes. */
function MarkdownRenderer({ content }: { content: string }) {
  const lines = content.split("\n");
  const elements: React.ReactNode[] = [];
  let inCodeBlock = false;
  let codeContent = "";
  let inTable = false;
  let tableRows: string[][] = [];
  let listItems: string[] = [];
  let inList = false;
  let isOrderedList = false;

  const flushList = () => {
    if (listItems.length > 0) {
      const Tag = isOrderedList ? "ol" : "ul";
      const listClass = isOrderedList ? "list-decimal" : "list-disc";
      elements.push(
        <Tag key={elements.length} className={`${listClass} list-inside space-y-1 my-2`}>
          {listItems.map((item, i) => (
            <li
              key={i}
              className="text-sm"
              dangerouslySetInnerHTML={{ __html: parseInline(item) }}
            />
          ))}
        </Tag>
      );
      listItems = [];
    }
    inList = false;
  };

  const flushTable = () => {
    if (tableRows.length > 0) {
      elements.push(
        <div key={elements.length} className="overflow-x-auto my-3">
          <table className="w-full text-sm border-collapse">
            <thead>
              <tr>
                {tableRows[0]?.map((cell, i) => (
                  <th
                    key={i}
                    className="border border-border px-3 py-1.5 bg-muted text-left font-medium"
                  >
                    {cell.trim()}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {tableRows.slice(2).map((row, i) => (
                <tr key={i}>
                  {row.map((cell, j) => (
                    <td key={j} className="border border-border px-3 py-1.5">
                      {cell.trim()}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
      tableRows = [];
    }
    inTable = false;
  };

  const escapeHtml = (text: string): string => {
    return text
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;");
  };

  const parseInline = (text: string): string => {
    return escapeHtml(text)
      .replace(/\*\*(.+?)\*\*/g, "<strong>$1</strong>")
      .replace(
        /`(.+?)`/g,
        '<code class="bg-muted px-1 py-0.5 rounded text-xs font-mono">$1</code>'
      );
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // Code blocks
    if (line.startsWith("```")) {
      if (!inCodeBlock) {
        flushList();
        flushTable();
        inCodeBlock = true;
        codeContent = "";
      } else {
        elements.push(
          <pre
            key={elements.length}
            className="bg-muted rounded-md p-3 overflow-x-auto my-3"
          >
            <code className="text-xs font-mono">{codeContent.trim()}</code>
          </pre>
        );
        inCodeBlock = false;
      }
      continue;
    }

    if (inCodeBlock) {
      codeContent += line + "\n";
      continue;
    }

    // Tables
    if (line.includes("|") && line.trim().startsWith("|")) {
      flushList();
      inTable = true;
      const cells = line.split("|").filter((c) => c.trim() !== "");
      tableRows.push(cells);
      continue;
    } else if (inTable && !line.includes("|")) {
      flushTable();
    }

    // Empty line
    if (line.trim() === "") {
      flushList();
      continue;
    }

    // Headings
    if (line.startsWith("### ")) {
      flushList();
      flushTable();
      elements.push(
        <h3 key={elements.length} className="font-semibold text-base mt-4 mb-2">
          {line.slice(4)}
        </h3>
      );
      continue;
    }

    if (line.startsWith("## ")) {
      flushList();
      flushTable();
      elements.push(
        <h2 key={elements.length} className="font-semibold text-lg mt-4 mb-2">
          {line.slice(3)}
        </h2>
      );
      continue;
    }

    // List items
    if (line.match(/^[-*]\s/)) {
      if (inList && isOrderedList) flushList();
      inList = true;
      isOrderedList = false;
      listItems.push(line.slice(2));
      continue;
    }

    if (line.match(/^\d+\.\s/)) {
      if (inList && !isOrderedList) flushList();
      inList = true;
      isOrderedList = true;
      listItems.push(line.replace(/^\d+\.\s/, ""));
      continue;
    }

    // Regular paragraph
    flushList();
    flushTable();
    elements.push(
      <p
        key={elements.length}
        className="text-sm my-2"
        dangerouslySetInnerHTML={{ __html: parseInline(line) }}
      />
    );
  }

  flushList();
  flushTable();

  return <>{elements}</>;
}
