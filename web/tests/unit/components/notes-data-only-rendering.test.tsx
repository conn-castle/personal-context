// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { SlideDetail, SlideSummary } from "@/lib/types";

const scaledSlideFrameMock = vi.fn();

vi.mock("@/components/scaled-slide-frame", () => ({
  ScaledSlideFrame: (props: { htmlContent: string }) => {
    scaledSlideFrameMock(props);
    return <div data-testid="scaled-slide-frame" />;
  },
}));

import { SlideThumbnail } from "@/components/slide-thumbnail";
import { SlideViewer } from "@/components/slide-viewer";

const summary: SlideSummary = {
  id: "20260507-aabbccdd",
  date: "2026-05-07",
  day_order: "a0",
  html_content: null,
  project_id: "vault/project",
  source_device_id: "workstation",
  source_ref: "obsidian://daily",
  updated_at: "2026-05-07T12:00:00.000Z",
  deleted_at: null,
  figure_count: 1,
  data_file_count: 2,
};

const detail: SlideDetail = {
  ...summary,
  notes: "Daily notes and observations",
  git_remote_url: null,
  git_hash: null,
  created_at: "2026-05-07T11:00:00.000Z",
  figures: [
    {
      filename: "chart.png",
      s3_key: "figures/20260507-aabbccdd/chart.png",
    },
  ],
  data_files: [
    {
      filename: "context.json",
      s3_key: "data/20260507-aabbccdd/context.json",
    },
    {
      filename: "metrics.csv",
      s3_key: "data/20260507-aabbccdd/metrics.csv",
    },
  ],
};

describe("notes/data-only rendering", () => {
  afterEach(() => {
    cleanup();
    scaledSlideFrameMock.mockClear();
  });

  it("renders a deliberate main viewer state without passing null HTML to the iframe", () => {
    render(<SlideViewer slide={detail} />);

    expect(screen.getByText("Notes/data-only record")).toBeTruthy();
    expect(screen.getByText("Daily notes and observations")).toBeTruthy();
    expect(screen.getByText("1 figure")).toBeTruthy();
    expect(screen.getByText("2 data files")).toBeTruthy();
    expect(screen.queryByTestId("scaled-slide-frame")).toBeNull();
    expect(scaledSlideFrameMock).not.toHaveBeenCalled();
  });

  it("renders a thumbnail fallback without passing null HTML to the iframe", () => {
    render(
      <SlideThumbnail
        slide={summary}
        isSelected={false}
        onClick={vi.fn()}
      />
    );

    expect(screen.getByText("Notes/data")).toBeTruthy();
    expect(screen.queryByTestId("scaled-slide-frame")).toBeNull();
    expect(scaledSlideFrameMock).not.toHaveBeenCalled();
  });
});
