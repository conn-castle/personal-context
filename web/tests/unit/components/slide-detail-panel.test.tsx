// @vitest-environment jsdom
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
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

import { SlideDetails } from "@/components/slide-details";
import type { SlideDetail } from "@/lib/types";

const slide: SlideDetail = {
  id: "20260309-aabbccdd",
  date: "2026-03-09",
  day_order: "a0",
  html_content: "<p>Slide</p>",
  notes: "Some notes here",
  project_id: null,
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

describe("SlideDetails", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("shows empty state when no slide is selected", () => {
    render(<SlideDetails slide={null} />);
    expect(screen.getByText("No slide selected")).toBeTruthy();
  });

  it("renders notes content for a selected slide", () => {
    render(<SlideDetails slide={slide} />);
    expect(screen.getByText("Some notes here")).toBeTruthy();
  });

  it("renders asset cards for figures and data files", () => {
    render(<SlideDetails slide={slide} />);
    expect(
      screen.getByTestId("asset-card-figure-chart.png")
    ).toBeTruthy();
    expect(
      screen.getByTestId("asset-card-file-results.csv")
    ).toBeTruthy();
  });

  it("allows editing and saving notes via onUpdateSlide", () => {
    const onUpdateSlide = vi.fn();
    render(
      <SlideDetails slide={slide} onUpdateSlide={onUpdateSlide} />
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

    expect(onUpdateSlide).toHaveBeenCalledWith("20260309-aabbccdd", {
      notes: "Updated notes",
    });
  });

  it("shows Add notes button when notes are empty", () => {
    const emptyNotesSlide: SlideDetail = { ...slide, notes: null };
    render(<SlideDetails slide={emptyNotesSlide} />);
    expect(screen.getByText("Add notes")).toBeTruthy();
    expect(screen.getByText("No notes for this slide")).toBeTruthy();
  });

  it("cancels editing and restores the original notes", () => {
    render(<SlideDetails slide={slide} />);

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
