"use client";

import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { RecordDetail } from "@/lib/types";
import { formatFileSize } from "@/lib/record-utils";
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
import { MarkdownRenderer } from "@/components/markdown-renderer";

interface RecordDetailsProps {
  record: RecordDetail | null;
  activeTab?: string;
  onTabChange?: (tab: string) => void;
  onUpdateRecord?: (id: string, body: Record<string, unknown>) => void;
  /** Whether the record list is empty (no records in the project). */
  isEmpty?: boolean;
}

export function RecordDetails(props: RecordDetailsProps) {
  const { record, activeTab, onTabChange, onUpdateRecord, isEmpty } = props;
  if (!record) {
    return (
      <div className="h-full flex items-center justify-center bg-card text-card-foreground p-4">
        <div className="text-center text-muted-foreground">
          <FileText className="w-12 h-12 mx-auto mb-3 opacity-50" />
          <p className="text-sm">
            {isEmpty
              ? "No records in this project"
              : "Loading record details..."}
          </p>
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
            {record.figures.length > 0 && (
              <Badge
                variant="secondary"
                className="ml-1 h-4 px-1 text-[10px] flex-shrink-0"
              >
                {record.figures.length}
              </Badge>
            )}
          </TabsTrigger>
          <TabsTrigger
            value="files"
            className="flex-1 flex items-center justify-center gap-1.5 text-xs"
          >
            <FileDown className="w-3.5 h-3.5 flex-shrink-0" />
            <span className="hidden @[280px]:inline">Files</span>
            {record.data_files.length > 0 && (
              <Badge
                variant="secondary"
                className="ml-1 h-4 px-1 text-[10px] flex-shrink-0"
              >
                {record.data_files.length}
              </Badge>
            )}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="notes" className="flex-1 min-h-0 mt-0 p-0">
          <NotesEditor
            key={record.id}
            recordId={record.id}
            notes={record.notes}
            onSave={onUpdateRecord}
          />
        </TabsContent>

        <TabsContent value="figures" className="flex-1 min-h-0 mt-0 p-0">
          <ScrollArea className="h-full">
            <div className="p-4">
              {record.figures.length > 0 ? (
                <div className="space-y-2">
                  {record.figures.map((figure) => (
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
              {record.data_files.length > 0 ? (
                <div className="space-y-2">
                  {record.data_files.map((file) => (
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
  recordId,
  notes,
  onSave,
}: {
  recordId: string;
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
    onSave?.(recordId, { notes: editedNotes || null });
    setIsEditing(false);
  };

  if (!notes && !isEditing) {
    return (
      <div className="h-full flex flex-col items-center justify-center text-muted-foreground p-4">
        <FileText className="w-10 h-10 mb-2 opacity-50" />
        <p className="text-sm mb-3">No notes for this record</p>
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

