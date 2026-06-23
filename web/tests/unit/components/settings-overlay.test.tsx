// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SettingsOverlay } from "@/components/settings-overlay";

describe("SettingsOverlay", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/info")) {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({
              mode: "cloud",
              version: "test",
            }),
        });
      }
      if (url.includes("/api/stats")) {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({
              total_records: 1,
              total_projects: 1,
              trashed_records: 0,
            }),
        });
      }
      return Promise.resolve({
        ok: false,
        json: () => Promise.resolve({ error: "not found" }),
      });
    });
  });

  afterEach(() => {
    cleanup();
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("shows the current sync error in the sync status section", () => {
    render(
      <SettingsOverlay
        open
        onClose={() => undefined}
        syncVersion={7}
        lastSyncAt={null}
        syncError="Sync request failed: 500"
        onDataChanged={() => undefined}
      />
    );

    expect(screen.getByText("Sync error")).toBeTruthy();
    expect(screen.getByText("Sync request failed: 500")).toBeTruthy();
  });

  it("omits the sync error row when there is no error", () => {
    render(
      <SettingsOverlay
        open
        onClose={() => undefined}
        syncVersion={7}
        lastSyncAt={null}
        syncError={null}
        onDataChanged={() => undefined}
      />
    );

    expect(screen.queryByText("Sync error")).toBeNull();
  });

  it("falls back to the raw string for an unparseable lastSyncAt", () => {
    render(
      <SettingsOverlay
        open
        onClose={() => undefined}
        syncVersion={7}
        lastSyncAt="not-a-timestamp"
        syncError={null}
        onDataChanged={() => undefined}
      />
    );

    // A malformed timestamp produces an Invalid Date whose toLocaleString()
    // yields the literal "Invalid Date" (it does not throw, so the old
    // try/catch never fired). The guard must surface the raw value instead.
    expect(screen.getByText("not-a-timestamp")).toBeTruthy();
    expect(screen.queryByText("Invalid Date")).toBeNull();
  });
});
