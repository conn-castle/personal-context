// @vitest-environment jsdom
import { afterEach, beforeAll, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import { RecordMetadataBar } from "@/components/record-metadata-bar";
import type { RecordDetail } from "@/lib/types";

beforeAll(() => {
  // RecordMetadataBar observes its scroll container; jsdom lacks ResizeObserver.
  if (!globalThis.ResizeObserver) {
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver;
  }
});

function makeRecord(overrides: Partial<RecordDetail> = {}): RecordDetail {
  return {
    id: "20260307-abcdef12",
    date: "2026-03-07",
    day_order: "a0",
    html_content: null,
    notes: null,
    project_id: "proj",
    source_device_id: "dev",
    source_ref: null,
    git_remote_url: "https://github.com/acme/repo",
    git_hash: "0123456789abcdef",
    created_at: "2026-03-07T00:00:00.000Z",
    updated_at: "2026-03-07T00:00:00.000Z",
    deleted_at: null,
    figures: [],
    data_files: [],
    ...overrides,
  };
}

describe("RecordMetadataBar git commit link", () => {
  afterEach(() => {
    cleanup();
  });

  it("exposes an accessible name on the icon-only commit link", () => {
    render(<RecordMetadataBar record={makeRecord()} />);

    // The icon-only commit link must be reachable by accessible name so screen
    // readers do not announce it as an unlabeled "link". Without the sr-only
    // label this query returns nothing and the test fails.
    const commitLink = screen.getByRole("link", { name: "View commit" });
    expect(commitLink.getAttribute("href")).toBe(
      "https://github.com/acme/repo/commit/0123456789abcdef"
    );
  });
});
