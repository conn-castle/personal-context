import { describe, expect, it } from "vitest";

import {
  formatDate,
  formatRelativeDate,
  formatFileSize,
  groupRecordsByDate,
  groupRecordsByDateDesc,
  getFigureFilenames,
  rewriteFigureSources,
} from "@/lib/record-utils";
import type { RecordSummary } from "@/lib/types";

/** Helper to build a minimal RecordSummary for testing. */
function makeSummary(overrides: Partial<RecordSummary> & { id: string; date: string }): RecordSummary {
  return {
    day_order: "a0",
    html_content: "<p>Test content</p>",
    project_id: "org/default",
    source_device_id: "device-a",
    source_ref: null,
    updated_at: "2025-03-04T00:00:00Z",
    deleted_at: null,
    figure_count: 0,
    data_file_count: 0,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// formatDate
// ---------------------------------------------------------------------------
describe("formatDate", () => {
  it("formats a date string with weekday, month, day, and year", () => {
    const result = formatDate("2025-03-04");
    expect(result).toContain("Mar");
    expect(result).toContain("4");
    expect(result).toContain("2025");
  });

  it("produces stable output regardless of timezone (noon UTC)", () => {
    const result = formatDate("2025-12-31");
    expect(result).toContain("Dec");
    expect(result).toContain("31");
    expect(result).toContain("2025");
  });
});

// ---------------------------------------------------------------------------
// formatRelativeDate
// ---------------------------------------------------------------------------
describe("formatRelativeDate", () => {
  it("returns 'Today' for the current date", () => {
    const today = new Date();
    const yyyy = today.getFullYear();
    const mm = String(today.getMonth() + 1).padStart(2, "0");
    const dd = String(today.getDate()).padStart(2, "0");
    expect(formatRelativeDate(`${yyyy}-${mm}-${dd}`)).toBe("Today");
  });

  it("returns 'Yesterday' for the previous date", () => {
    const yesterday = new Date();
    yesterday.setDate(yesterday.getDate() - 1);
    const yyyy = yesterday.getFullYear();
    const mm = String(yesterday.getMonth() + 1).padStart(2, "0");
    const dd = String(yesterday.getDate()).padStart(2, "0");
    expect(formatRelativeDate(`${yyyy}-${mm}-${dd}`)).toBe("Yesterday");
  });

  it("returns a formatted date for older dates", () => {
    const result = formatRelativeDate("2020-01-01");
    expect(result).toContain("Jan");
    expect(result).toContain("1");
  });
});

// ---------------------------------------------------------------------------
// formatFileSize
// ---------------------------------------------------------------------------
describe("formatFileSize", () => {
  it("formats bytes", () => {
    expect(formatFileSize(0)).toBe("0 B");
    expect(formatFileSize(512)).toBe("512 B");
    expect(formatFileSize(1023)).toBe("1023 B");
  });

  it("formats kilobytes", () => {
    expect(formatFileSize(1024)).toBe("1.0 KB");
    expect(formatFileSize(45056)).toBe("44.0 KB");
  });

  it("formats megabytes", () => {
    expect(formatFileSize(1048576)).toBe("1.0 MB");
    expect(formatFileSize(2621440)).toBe("2.5 MB");
  });
});

// ---------------------------------------------------------------------------
// groupRecordsByDateDesc
// ---------------------------------------------------------------------------
describe("groupRecordsByDateDesc", () => {
  it("groups and sorts by date DESC, day_order ASC", () => {
    const records = [
      { date: "2025-03-03", day_order: "a1" },
      { date: "2025-03-04", day_order: "a0" },
      { date: "2025-03-03", day_order: "a0" },
      { date: "2025-03-04", day_order: "a1" },
    ];
    const groups = groupRecordsByDateDesc(records);
    expect(groups).toHaveLength(2);
    expect(groups[0].date).toBe("2025-03-04");
    expect(groups[0].records.map((s) => s.day_order)).toEqual(["a0", "a1"]);
    expect(groups[1].date).toBe("2025-03-03");
    expect(groups[1].records.map((s) => s.day_order)).toEqual(["a0", "a1"]);
  });

  it("returns empty array for empty input", () => {
    expect(groupRecordsByDateDesc([])).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// groupRecordsByDate
// ---------------------------------------------------------------------------
describe("groupRecordsByDate", () => {
  it("groups records by date correctly", () => {
    const records: RecordSummary[] = [
      makeSummary({ id: "s1", date: "2025-03-04", day_order: "a0" }),
      makeSummary({ id: "s2", date: "2025-03-04", day_order: "a1" }),
      makeSummary({ id: "s3", date: "2025-03-03", day_order: "a0" }),
      makeSummary({ id: "s4", date: "2025-03-03", day_order: "a1" }),
    ];

    const groups = groupRecordsByDate(records);

    expect(groups).toHaveLength(2);
    expect(groups[0].date).toBe("2025-03-04");
    expect(groups[0].records).toHaveLength(2);
    expect(groups[0].records[0].id).toBe("s1");
    expect(groups[0].records[1].id).toBe("s2");
    expect(groups[1].date).toBe("2025-03-03");
    expect(groups[1].records).toHaveLength(2);
    expect(groups[1].records[0].id).toBe("s3");
    expect(groups[1].records[1].id).toBe("s4");
  });

  it("returns empty array for empty input", () => {
    expect(groupRecordsByDate([])).toEqual([]);
  });

  it("single record returns single group", () => {
    const records = [makeSummary({ id: "s1", date: "2025-01-01" })];
    const groups = groupRecordsByDate(records);
    expect(groups).toHaveLength(1);
    expect(groups[0].date).toBe("2025-01-01");
    expect(groups[0].records).toHaveLength(1);
  });

  it("preserves order within groups", () => {
    const records: RecordSummary[] = [
      makeSummary({ id: "s1", date: "2025-03-04", day_order: "a0" }),
      makeSummary({ id: "s2", date: "2025-03-04", day_order: "a1" }),
      makeSummary({ id: "s3", date: "2025-03-04", day_order: "a2" }),
    ];

    const groups = groupRecordsByDate(records);
    expect(groups).toHaveLength(1);
    expect(groups[0].records.map((s) => s.id)).toEqual(["s1", "s2", "s3"]);
  });
});

// ---------------------------------------------------------------------------
// getFigureFilenames
// ---------------------------------------------------------------------------
describe("getFigureFilenames", () => {
  it("collects unique figure filenames and ignores query/hash suffixes", () => {
    const html = [
      '<img src="figures/plot.png?raw=1#preview">',
      "<img src='figures/plot.png?raw=2'>",
      '<img src="figures/other.png#hero">',
    ].join("");

    expect(getFigureFilenames(html)).toEqual(["plot.png", "other.png"]);
  });
});

// ---------------------------------------------------------------------------
// rewriteFigureSources
// ---------------------------------------------------------------------------
describe("rewriteFigureSources", () => {
  it("rewrites known figure sources", () => {
    const html =
      '<img src="figures/plot.png"><img src="https://example.com/external.png">';
    const result = rewriteFigureSources(html, {
      "plot.png": "https://signed.example.com/plot.png",
    });

    expect(result).toBe(
      '<img src="https://signed.example.com/plot.png"><img src="https://example.com/external.png">'
    );
  });

  it("leaves unknown figure sources untouched", () => {
    const html = '<img src="figures/missing.png">';
    expect(rewriteFigureSources(html, {})).toBe(html);
  });

  it("supports single-quoted figure sources", () => {
    const html = "<img src='figures/plot.png'>";
    const result = rewriteFigureSources(html, {
      "plot.png": "https://signed.example.com/plot.png",
    });

    expect(result).toBe("<img src='https://signed.example.com/plot.png'>");
  });

  it("rewrites figure sources with whitespace around '='", () => {
    const html = '<img src = "figures/plot.png">';
    const result = rewriteFigureSources(html, {
      "plot.png": "https://signed.example.com/plot.png",
    });

    expect(result).toBe('<img src="https://signed.example.com/plot.png">');
  });

  it("rewrites figure sources when the original src has query or hash suffixes", () => {
    const html = '<img src="figures/plot.png?raw=1#preview">';
    const result = rewriteFigureSources(html, {
      "plot.png": "https://signed.example.com/plot.png",
    });

    expect(result).toBe('<img src="https://signed.example.com/plot.png">');
  });
});
