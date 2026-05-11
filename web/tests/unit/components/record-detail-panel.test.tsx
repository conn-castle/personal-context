// @vitest-environment jsdom
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";

vi.mock("@/components/ui/tabs", () => ({
  Tabs: ({
    children,
    ...props
  }: { children?: ReactNode } & Record<string, unknown>) => (
    <div {...props}>{children}</div>
  ),
  TabsList: ({
    children,
    ...props
  }: { children?: ReactNode } & Record<string, unknown>) => (
    <div {...props}>{children}</div>
  ),
  TabsTrigger: ({
    children,
    ...props
  }: { children?: ReactNode } & Record<string, unknown>) => (
    <button type="button" {...props}>
      {children}
    </button>
  ),
  TabsContent: ({
    children,
    ...props
  }: { children?: ReactNode } & Record<string, unknown>) => (
    <div {...props}>{children}</div>
  ),
}));

vi.mock("@/components/ui/scroll-area", () => ({
  ScrollArea: ({
    children,
    ...props
  }: { children?: ReactNode } & Record<string, unknown>) => (
    <div {...props}>{children}</div>
  ),
}));

vi.mock("@/components/markdown-renderer", () => ({
  MarkdownRenderer: ({ content }: { content: string }) => (
    <span>{content}</span>
  ),
}));

vi.mock("@/components/asset-card", () => ({
  AssetCard: ({
    filename,
    type,
    description,
    size,
  }: {
    filename: string;
    type: string;
    description?: string | null;
    size?: string;
  }) => (
    <div data-testid={`asset-card-${type}-${filename}`}>
      <span>{filename}</span>
      {description && <span>{description}</span>}
      {size && <span>{size}</span>}
    </div>
  ),
}));

import { RecordDetails } from "@/components/record-details";
import type { RecordDetail } from "@/lib/types";

const record: RecordDetail = {
  id: "20260309-aabbccdd",
  date: "2026-03-09",
  day_order: "a0",
  html_content: "<p>Record</p>",
  notes: "Some notes here",
  project_id: "org/proj",
  source_device_id: "device-a",
  source_ref: null,
  git_remote_url: null,
  git_hash: null,
  created_at: "2026-03-09T08:00:00Z",
  updated_at: "2026-03-09T10:00:00Z",
  deleted_at: null,
  figures: [
    {
      filename: "chart.png",
      s3_key: "figures/20260309-aabbccdd/chart-v2.png",
      size: 1024,
      alt_text: null,
      description: null,
    },
  ],
  data_files: [
    {
      filename: "results.csv",
      s3_key: "data/20260309-aabbccdd/results-v2.csv",
      size: 2048,
      description: "Results",
    },
  ],
};

describe("RecordDetails", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("shows empty state when no record is selected and project is empty", () => {
    render(<RecordDetails record={null} isEmpty={true} />);
    expect(screen.getByText("No records in this project")).toBeTruthy();
  });

  it("shows loading state when no record is selected and project is not empty", () => {
    render(<RecordDetails record={null} isEmpty={false} />);
    expect(screen.getByText("Loading record details...")).toBeTruthy();
  });

  it("renders notes content for a selected record", () => {
    render(<RecordDetails record={record} />);
    expect(screen.getByText("Some notes here")).toBeTruthy();
  });

  it("renders asset cards for figures and data files", () => {
    render(<RecordDetails record={record} />);
    expect(
      screen.getByTestId("asset-card-figure-chart.png")
    ).toBeTruthy();
    expect(
      screen.getByTestId("asset-card-file-results.csv")
    ).toBeTruthy();
  });

  it("allows editing and saving notes via onUpdateRecord", async () => {
    const onUpdateRecord = vi.fn().mockResolvedValue(true);
    render(
      <RecordDetails record={record} onUpdateRecord={onUpdateRecord} />
    );

    // Click the Edit pencil button (has title="Edit notes")
    fireEvent.click(screen.getByTitle("Edit notes"));

    // The textarea should now be visible with the current notes
    const textarea = screen.getByRole("textbox");
    expect(textarea).toBeTruthy();

    // Change the notes
    fireEvent.change(textarea, { target: { value: "Updated notes" } });

    // Click Save
    fireEvent.click(screen.getByText("Save"));

    expect(onUpdateRecord).toHaveBeenCalledWith("20260309-aabbccdd", {
      notes: "Updated notes",
    });
    await waitFor(() => {
      expect(screen.queryByRole("textbox")).toBeNull();
    });
  });

  it("keeps draft notes visible when save fails", async () => {
    const onUpdateRecord = vi.fn().mockResolvedValue(false);
    render(
      <RecordDetails record={record} onUpdateRecord={onUpdateRecord} />
    );

    fireEvent.click(screen.getByTitle("Edit notes"));
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "Unsaved draft" },
    });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => {
      expect(onUpdateRecord).toHaveBeenCalledWith("20260309-aabbccdd", {
        notes: "Unsaved draft",
      });
    });
    expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe(
      "Unsaved draft"
    );
  });

  it("shows Add notes button when notes are empty", () => {
    const emptyNotesRecord: RecordDetail = { ...record, notes: null };
    render(<RecordDetails record={emptyNotesRecord} />);
    expect(screen.getByText("Add notes")).toBeTruthy();
    expect(screen.getByText("No notes for this record")).toBeTruthy();
  });

  it("cancels editing and restores the original notes", () => {
    render(<RecordDetails record={record} />);

    fireEvent.click(screen.getByTitle("Edit notes"));
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "Draft edit" },
    });

    // Cancel editing
    fireEvent.click(screen.getByText("Cancel"));

    // Should be back to viewing mode with original notes
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("Some notes here")).toBeTruthy();
  });
});
