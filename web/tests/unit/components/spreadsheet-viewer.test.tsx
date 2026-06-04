// @vitest-environment jsdom
import { fireEvent, render } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { SpreadsheetViewer } from "@/components/spreadsheet-viewer";

const selectRecordMock = vi.fn();
const fetchRecordsMock = vi.fn();
const fetchProjectsMock = vi.fn();
const refreshRecordsMock = vi.fn();

function createStorageMock(): Storage {
  const store = new Map<string, string>();

  return {
    get length() {
      return store.size;
    },
    clear() {
      store.clear();
    },
    getItem(key: string) {
      return store.get(key) ?? null;
    },
    key(index: number) {
      return Array.from(store.keys())[index] ?? null;
    },
    removeItem(key: string) {
      store.delete(key);
    },
    setItem(key: string, value: string) {
      store.set(key, value);
    },
  };
}

vi.mock("@/hooks/use-records", () => ({
  useRecords: () => ({
    records: [
      {
        id: "20260311-record-1",
        date: "2026-03-11",
        day_order: "a0",
        html_content: "<p>Record 1</p>",
        project_id: "alpha",
        source_device_id: "device-a",
        source_ref: null,
        updated_at: "2026-03-11T10:00:00.000Z",
        deleted_at: null,
        figure_count: 0,
        data_file_count: 0,
      },
      {
        id: "20260311-record-2",
        date: "2026-03-11",
        day_order: "a1",
        html_content: "<p>Record 2</p>",
        project_id: "alpha",
        source_device_id: "device-a",
        source_ref: null,
        updated_at: "2026-03-11T11:00:00.000Z",
        deleted_at: null,
        figure_count: 0,
        data_file_count: 0,
      },
    ],
    selectedRecord: {
      id: "20260311-record-1",
      date: "2026-03-11",
      day_order: "a0",
      html_content: "<p>Record 1</p>",
      notes: "",
      figures: [],
      data_files: [],
      project_id: "alpha",
      source_device_id: "device-a",
      source_ref: null,
      git_remote_url: null,
      git_hash: null,
      updated_at: "2026-03-11T10:00:00.000Z",
      created_at: "2026-03-11T09:00:00.000Z",
      deleted_at: null,
    },
    projects: [],
    error: null,
    hasMore: false,
    fetchRecords: fetchRecordsMock,
    fetchMore: vi.fn(),
    selectRecord: selectRecordMock,
    updateRecord: vi.fn(),
    deleteRecord: vi.fn(),
    restoreRecord: vi.fn(),
    fetchProjects: fetchProjectsMock,
    refreshRecords: refreshRecordsMock,
    isLoading: false,
    isFetchingMore: false,
    reorderRecord: vi.fn(),
  }),
}));

vi.mock("@/hooks/use-sync-manager", () => ({
  useSyncManager: () => ({
    syncNow: vi.fn(),
    markMutation: vi.fn(),
    version: 1,
    isSyncing: false,
    lastSyncAt: null,
  }),
}));

vi.mock("next-themes", () => ({
  useTheme: () => ({
    theme: "light",
    setTheme: vi.fn(),
  }),
}));

vi.mock("@/components/settings-overlay", () => ({
  SettingsOverlay: () => null,
}));

vi.mock("@/components/record-navigation", () => ({
  RecordNavigation: () => <div>navigation</div>,
}));

vi.mock("@/components/record-viewer", () => ({
  RecordViewer: () => <div>viewer</div>,
}));

vi.mock("@/components/record-details", () => ({
  RecordDetails: () => <div>details</div>,
}));

vi.mock("@/components/collapsed-details-strip", () => ({
  CollapsedDetailsStrip: () => <div>collapsed-details</div>,
}));

vi.mock("@/components/record-metadata-bar", () => ({
  RecordMetadataBar: () => <div>metadata</div>,
}));

vi.mock("@/components/project-picker", () => ({
  ProjectPicker: () => <div>project-picker</div>,
}));

vi.mock("@/components/record-date-picker", () => ({
  RecordDatePicker: () => <div>date-picker</div>,
}));

vi.mock("@/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  ResizablePanel: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  ResizableHandle: () => <div />,
}));

vi.mock("@/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipContent: () => null,
}));

describe("SpreadsheetViewer", () => {
  beforeEach(() => {
    const storage = createStorageMock();
    Object.defineProperty(window, "localStorage", {
      value: storage,
      configurable: true,
    });
    Object.defineProperty(globalThis, "localStorage", {
      value: storage,
      configurable: true,
    });
    selectRecordMock.mockReset();
    fetchRecordsMock.mockReset();
    fetchProjectsMock.mockReset();
    refreshRecordsMock.mockReset();
  });

  it("persists the newly selected record when navigating with the keyboard", () => {
    render(<SpreadsheetViewer />);

    fireEvent.keyDown(window, { key: "ArrowRight" });

    expect(selectRecordMock).toHaveBeenCalledWith("20260311-record-2");
    expect(window.localStorage.getItem("pc:lastSelectedRecordId")).toBe(
      JSON.stringify("20260311-record-2")
    );
  });
});
