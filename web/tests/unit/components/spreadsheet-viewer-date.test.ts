// Pin a positive-UTC-offset zone (east of UTC) BEFORE importing the module.
// Asia/Tokyo (+9) is where the previous `toISOString()` implementation rolled a
// local-midnight Date back to the previous calendar day, breaking date jumps.
process.env.TZ = "Asia/Tokyo";

import { describe, expect, it } from "vitest";

import {
  findNearestRecordByDate,
  toLocalDateKey,
} from "@/components/spreadsheet-viewer";
import type { RecordSummary } from "@/lib/types";

function makeRecord(id: string, date: string): RecordSummary {
  return {
    id,
    date,
    day_order: "a0",
    html_content: null,
    project_id: "proj",
    source_device_id: "dev",
    source_ref: null,
    updated_at: "2026-03-07T00:00:00.000Z",
    deleted_at: null,
    figure_count: 0,
    data_file_count: 0,
  };
}

describe("toLocalDateKey", () => {
  it("formats a local-midnight Date as its local calendar day, not the UTC day", () => {
    // In a +UTC zone the UTC calendar day of local midnight is the PREVIOUS
    // day. A correct local-calendar key must still be the selected day.
    const localMidnight = new Date("2026-03-07T00:00:00");
    expect(localMidnight.toISOString().split("T")[0]).toBe("2026-03-06");
    expect(toLocalDateKey(localMidnight)).toBe("2026-03-07");
  });
});

describe("findNearestRecordByDate", () => {
  it("matches the exact-date record for a calendar selection in an east-of-UTC zone", () => {
    const records = [
      makeRecord("r-06", "2026-03-06"),
      makeRecord("r-07", "2026-03-07"),
      makeRecord("r-08", "2026-03-08"),
    ];
    // RecordDatePicker builds the target Date from local midnight.
    const target = new Date("2026-03-07T00:00:00");

    const result = findNearestRecordByDate(records, target);

    // The previous UTC-based key would have matched "2026-03-06" (r-06).
    expect(result?.id).toBe("r-07");
  });

  it("picks the calendar-adjacent record by local date when no exact match exists", () => {
    const records = [
      makeRecord("r-05", "2026-03-05"),
      makeRecord("r-09", "2026-03-09"),
    ];
    // Target 2026-03-06 (local midnight) has no exact record; the nearest by
    // local calendar distance is 2026-03-05 (1 day) over 2026-03-09 (3 days).
    const target = new Date("2026-03-06T00:00:00");

    const result = findNearestRecordByDate(records, target);

    expect(result?.id).toBe("r-05");
  });
});
