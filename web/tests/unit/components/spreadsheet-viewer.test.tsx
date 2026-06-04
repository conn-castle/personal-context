// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import { SpreadsheetViewer } from "@/components/spreadsheet-viewer";
import type { UseRecordsReturn } from "@/hooks/use-records";
import type { RecordDetail, RecordSummary } from "@/lib/types";

const selectRecordMock = vi.fn<UseRecordsReturn["selectRecord"]>();
const fetchRecordsMock = vi.fn<UseRecordsReturn["fetchRecords"]>();
const fetchMoreMock = vi.fn<UseRecordsReturn["fetchMore"]>();
const fetchProjectsMock = vi.fn<UseRecordsReturn["fetchProjects"]>();
const refreshRecordsMock = vi.fn<UseRecordsReturn["refreshRecords"]>();
const updateRecordMock = vi.fn<UseRecordsReturn["updateRecord"]>();
const deleteRecordMock = vi.fn<UseRecordsReturn["deleteRecord"]>();
const restoreRecordMock = vi.fn<UseRecordsReturn["restoreRecord"]>();
const reorderRecordMock = vi.fn<UseRecordsReturn["reorderRecord"]>();
const markMutationMock = vi.fn();
const setThemeMock = vi.fn();
let currentSyncError: string | null = null;

const records: RecordSummary[] = [
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
];

const recordDetails: RecordDetail[] = records.map((record, index) => ({
  ...record,
  notes: "",
  figures: [],
  data_files: [],
  git_remote_url: null,
  git_hash: null,
  created_at: `2026-03-11T0${9 + index}:00:00.000Z`,
}));

let currentRecords: RecordSummary[] = records;
let currentSelectedRecord: RecordDetail | null = recordDetails[0];
let currentProjects: string[] = [];
let currentError: string | null = null;
let currentHasMore = false;

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

function getUseRecordsReturn(): UseRecordsReturn {
  return {
    records: currentRecords,
    selectedRecord: currentSelectedRecord,
    projects: currentProjects,
    error: currentError,
    hasMore: currentHasMore,
    fetchRecords: fetchRecordsMock,
    fetchMore: fetchMoreMock,
    selectRecord: selectRecordMock,
    updateRecord: updateRecordMock,
    deleteRecord: deleteRecordMock,
    restoreRecord: restoreRecordMock,
    fetchProjects: fetchProjectsMock,
    refreshRecords: refreshRecordsMock,
    isLoading: false,
    isFetchingMore: false,
    reorderRecord: reorderRecordMock,
  };
}

vi.mock("@/hooks/use-records", () => ({
  useRecords: () => getUseRecordsReturn(),
}));

vi.mock("@/hooks/use-sync-manager", () => ({
  useSyncManager: ({ onSyncData }: { onSyncData?: () => void } = {}) => ({
    syncNow: vi.fn(),
    markMutation: markMutationMock,
    version: 1,
    isSyncing: false,
    lastSyncAt: null,
    syncError: currentSyncError,
    onSyncData,
  }),
}));

vi.mock("next-themes", () => ({
  useTheme: () => ({
    theme: "light",
    setTheme: setThemeMock,
  }),
}));

vi.mock("@/components/settings-overlay", () => ({
  SettingsOverlay: ({
    onDataChanged,
    syncError,
  }: {
    onDataChanged: () => void;
    syncError: string | null;
  }) => (
    <div>
      <button data-testid="sync-data" onClick={onDataChanged}>
        sync data
      </button>
      {syncError && (
        <span data-testid="settings-sync-error">{syncError}</span>
      )}
    </div>
  ),
}));

vi.mock("@/components/record-navigation", () => ({
  RecordNavigation: () => <div>navigation</div>,
}));

vi.mock("@/components/record-viewer", () => ({
  RecordViewer: () => <div>viewer</div>,
}));

vi.mock("@/components/record-details", () => ({
  RecordDetails: ({
    onUpdateRecord,
  }: {
    onUpdateRecord: (
      id: string,
      body: Record<string, unknown>
    ) => Promise<boolean>;
  }) => (
    <button
      data-testid="update-record"
      onClick={() =>
        void onUpdateRecord("20260311-record-1", { notes: "changed" })
      }
    >
      update
    </button>
  ),
}));

vi.mock("@/components/collapsed-details-strip", () => ({
  CollapsedDetailsStrip: () => (
    <div data-testid="collapsed-details-strip">collapsed-details</div>
  ),
}));

vi.mock("@/components/record-metadata-bar", () => ({
  RecordMetadataBar: ({
    onDelete,
    onRestore,
  }: {
    onDelete: (id: string) => Promise<void>;
    onRestore: (id: string) => Promise<void>;
  }) => (
    <div>
      <button
        data-testid="delete-record"
        onClick={() => void onDelete("20260311-record-1")}
      >
        delete
      </button>
      <button
        data-testid="restore-record"
        onClick={() => void onRestore("20260311-record-1")}
      >
        restore
      </button>
    </div>
  ),
}));

vi.mock("@/components/project-picker", () => ({
  ProjectPicker: () => <div>project-picker</div>,
}));

vi.mock("@/components/record-date-picker", () => ({
  RecordDatePicker: () => <div>date-picker</div>,
}));

vi.mock("@/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: ReactNode }) => (
    <div data-testid="resizable-panel-group">{children}</div>
  ),
  ResizablePanel: ({
    children,
    id,
    defaultSize,
    minSize,
    maxSize,
    groupResizeBehavior,
    disabled,
    className,
  }: {
    children: ReactNode;
    id?: string;
    defaultSize?: string | number;
    minSize?: string | number;
    maxSize?: string | number;
    groupResizeBehavior?: string;
    disabled?: boolean;
    className?: string;
  }) => (
    <div
      data-testid="resizable-panel"
      data-panel-id={id}
      data-default-size={defaultSize}
      data-min-size={minSize}
      data-max-size={maxSize}
      data-group-resize-behavior={groupResizeBehavior}
      data-disabled={disabled ? "true" : "false"}
      data-class-name={className}
    >
      {children}
    </div>
  ),
  ResizableHandle: () => <div data-testid="resizable-handle" />,
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

    currentRecords = records;
    currentSelectedRecord = recordDetails[0];
    currentProjects = [];
    currentError = null;
    currentHasMore = false;
    currentSyncError = null;

    selectRecordMock.mockReset();
    fetchRecordsMock.mockReset();
    fetchMoreMock.mockReset();
    fetchProjectsMock.mockReset();
    refreshRecordsMock.mockReset();
    updateRecordMock.mockReset();
    deleteRecordMock.mockReset();
    restoreRecordMock.mockReset();
    reorderRecordMock.mockReset();
    markMutationMock.mockReset();
    setThemeMock.mockReset();

    selectRecordMock.mockResolvedValue(undefined);
    fetchRecordsMock.mockResolvedValue(undefined);
    fetchMoreMock.mockResolvedValue(undefined);
    fetchProjectsMock.mockResolvedValue(undefined);
    refreshRecordsMock.mockResolvedValue(undefined);
    updateRecordMock.mockResolvedValue(false);
    deleteRecordMock.mockResolvedValue(false);
    restoreRecordMock.mockResolvedValue(false);
    reorderRecordMock.mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("fetches projects and records when the viewer mounts", async () => {
    render(<SpreadsheetViewer />);

    await waitFor(() => expect(fetchProjectsMock).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(fetchRecordsMock).toHaveBeenCalledWith({ project: undefined })
    );
  });

  it("refreshes records and projects when sync data arrives", async () => {
    render(<SpreadsheetViewer />);

    await waitFor(() => expect(fetchProjectsMock).toHaveBeenCalledTimes(1));
    fetchProjectsMock.mockClear();

    fireEvent.click(screen.getByTestId("sync-data"));

    expect(refreshRecordsMock).toHaveBeenCalledTimes(1);
    expect(fetchProjectsMock).toHaveBeenCalledTimes(1);
  });

  it("passes sync errors into settings status", () => {
    currentSyncError = "Sync request failed: 500";

    render(<SpreadsheetViewer />);

    expect(screen.getByTestId("settings-sync-error").textContent).toBe(
      "Sync request failed: 500"
    );
  });

  it("marks successful record updates as local mutations", async () => {
    updateRecordMock.mockResolvedValueOnce(true);
    render(<SpreadsheetViewer />);

    fireEvent.click(screen.getByTestId("update-record"));

    await waitFor(() =>
      expect(updateRecordMock).toHaveBeenCalledWith("20260311-record-1", {
        notes: "changed",
      })
    );
    expect(markMutationMock).toHaveBeenCalledTimes(1);
  });

  it("does not mark failed record updates as local mutations", async () => {
    updateRecordMock.mockResolvedValueOnce(false);
    render(<SpreadsheetViewer />);

    fireEvent.click(screen.getByTestId("update-record"));

    await waitFor(() => expect(updateRecordMock).toHaveBeenCalledTimes(1));
    expect(markMutationMock).not.toHaveBeenCalled();
  });

  it("marks successful delete and restore actions as local mutations", async () => {
    deleteRecordMock.mockResolvedValueOnce(true);
    restoreRecordMock.mockResolvedValueOnce(true);
    render(<SpreadsheetViewer />);

    fireEvent.click(screen.getByTestId("delete-record"));
    fireEvent.click(screen.getByTestId("restore-record"));

    await waitFor(() => expect(deleteRecordMock).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(restoreRecordMock).toHaveBeenCalledTimes(1));
    expect(markMutationMock).toHaveBeenCalledTimes(2);
  });

  it("keeps collapsed details content inside a fixed-size resizable panel", () => {
    render(<SpreadsheetViewer />);

    fireEvent.keyDown(window, { key: "]" });

    const wrapper = screen
      .getByTestId("collapsed-details-strip")
      .closest("[data-panel-id='collapsed-details-panel']");

    expect(wrapper).not.toBeNull();
    expect(wrapper?.getAttribute("data-default-size")).toBe("48px");
    expect(wrapper?.getAttribute("data-min-size")).toBe("48px");
    expect(wrapper?.getAttribute("data-max-size")).toBe("48px");
    expect(wrapper?.getAttribute("data-group-resize-behavior")).toBe(
      "preserve-pixel-size"
    );
    // The collapsed strip must be non-resizable and width-locked to 48px
    // (min-w-12/max-w-12), not just pixel-sized.
    expect(wrapper?.getAttribute("data-disabled")).toBe("true");
    expect(wrapper?.getAttribute("data-class-name")).toBe("min-w-12 max-w-12");
  });

  it("persists the newly selected record when navigating with the keyboard", () => {
    render(<SpreadsheetViewer />);

    fireEvent.keyDown(window, { key: "ArrowRight" });

    expect(selectRecordMock).toHaveBeenCalledWith("20260311-record-2");
    expect(window.localStorage.getItem("pc:lastSelectedRecordId")).toBe(
      JSON.stringify("20260311-record-2")
    );
  });

  it("does not replace the keydown listener when selection changes", async () => {
    const addListenerSpy = vi.spyOn(window, "addEventListener");
    const removeListenerSpy = vi.spyOn(window, "removeEventListener");

    const { rerender } = render(<SpreadsheetViewer />);

    await waitFor(() =>
      expect(
        addListenerSpy.mock.calls.filter(([eventName]) => eventName === "keydown")
      ).toHaveLength(1)
    );

    currentSelectedRecord = recordDetails[1];
    rerender(<SpreadsheetViewer />);

    expect(
      addListenerSpy.mock.calls.filter(([eventName]) => eventName === "keydown")
    ).toHaveLength(1);
    expect(
      removeListenerSpy.mock.calls.filter(
        ([eventName]) => eventName === "keydown"
      )
    ).toHaveLength(0);

    // Toggling a panel changes panelVisibility state and re-renders. Because
    // the keyboard effect depends on togglePanel, an unstable togglePanel
    // would tear down and re-register the listener here.
    fireEvent.keyDown(window, { key: "]" });

    expect(
      addListenerSpy.mock.calls.filter(([eventName]) => eventName === "keydown")
    ).toHaveLength(1);
    expect(
      removeListenerSpy.mock.calls.filter(
        ([eventName]) => eventName === "keydown"
      )
    ).toHaveLength(0);

    fireEvent.keyDown(window, { key: "ArrowLeft" });

    expect(selectRecordMock).toHaveBeenCalledWith("20260311-record-1");
  });
});
