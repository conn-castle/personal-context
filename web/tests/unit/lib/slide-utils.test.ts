import { describe, expect, it } from "vitest";

import {
  formatDate,
  formatRelativeDate,
  formatFileSize,
  getUniqueProjects,
  groupSlidesByDate,
  groupSlidesByDateDesc,
  injectVirtualDateSlides,
  getFigureFilenames,
  renderMarkdownToHtml,
  rewriteFigureSources,
} from "@/lib/slide-utils";
import type { SlideSummary, SlideGroup } from "@/lib/types";

/** Helper to build a minimal SlideSummary for testing. */
function makeSummary(overrides: Partial<SlideSummary> & { id: string; date: string }): SlideSummary {
  return {
    day_order: "a0",
    project_id: null,
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
// getUniqueProjects
// ---------------------------------------------------------------------------
describe("getUniqueProjects", () => {
  it("extracts unique non-null project IDs sorted", () => {
    const slides = [
      { project_id: "org/beta" },
      { project_id: null },
      { project_id: "org/alpha" },
      { project_id: "org/beta" },
    ];
    expect(getUniqueProjects(slides)).toEqual(["org/alpha", "org/beta"]);
  });

  it("returns empty array when all null", () => {
    expect(getUniqueProjects([{ project_id: null }])).toEqual([]);
  });

  it("returns empty array for empty input", () => {
    expect(getUniqueProjects([])).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// groupSlidesByDateDesc
// ---------------------------------------------------------------------------
describe("groupSlidesByDateDesc", () => {
  it("groups and sorts by date DESC, day_order ASC", () => {
    const slides = [
      { date: "2025-03-03", day_order: "a1" },
      { date: "2025-03-04", day_order: "a0" },
      { date: "2025-03-03", day_order: "a0" },
      { date: "2025-03-04", day_order: "a1" },
    ];
    const groups = groupSlidesByDateDesc(slides);
    expect(groups).toHaveLength(2);
    expect(groups[0].date).toBe("2025-03-04");
    expect(groups[0].slides.map((s) => s.day_order)).toEqual(["a0", "a1"]);
    expect(groups[1].date).toBe("2025-03-03");
    expect(groups[1].slides.map((s) => s.day_order)).toEqual(["a0", "a1"]);
  });

  it("returns empty array for empty input", () => {
    expect(groupSlidesByDateDesc([])).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// groupSlidesByDate
// ---------------------------------------------------------------------------
describe("groupSlidesByDate", () => {
  it("groups slides by date correctly", () => {
    const slides: SlideSummary[] = [
      makeSummary({ id: "s1", date: "2025-03-04", day_order: "a0" }),
      makeSummary({ id: "s2", date: "2025-03-04", day_order: "a1" }),
      makeSummary({ id: "s3", date: "2025-03-03", day_order: "a0" }),
      makeSummary({ id: "s4", date: "2025-03-03", day_order: "a1" }),
    ];

    const groups = groupSlidesByDate(slides);

    expect(groups).toHaveLength(2);
    expect(groups[0].date).toBe("2025-03-04");
    expect(groups[0].slides).toHaveLength(2);
    expect(groups[0].slides[0].id).toBe("s1");
    expect(groups[0].slides[1].id).toBe("s2");
    expect(groups[1].date).toBe("2025-03-03");
    expect(groups[1].slides).toHaveLength(2);
    expect(groups[1].slides[0].id).toBe("s3");
    expect(groups[1].slides[1].id).toBe("s4");
  });

  it("returns empty array for empty input", () => {
    expect(groupSlidesByDate([])).toEqual([]);
  });

  it("single slide returns single group", () => {
    const slides = [makeSummary({ id: "s1", date: "2025-01-01" })];
    const groups = groupSlidesByDate(slides);
    expect(groups).toHaveLength(1);
    expect(groups[0].date).toBe("2025-01-01");
    expect(groups[0].slides).toHaveLength(1);
  });

  it("preserves order within groups", () => {
    const slides: SlideSummary[] = [
      makeSummary({ id: "s1", date: "2025-03-04", day_order: "a0" }),
      makeSummary({ id: "s2", date: "2025-03-04", day_order: "a1" }),
      makeSummary({ id: "s3", date: "2025-03-04", day_order: "a2" }),
    ];

    const groups = groupSlidesByDate(slides);
    expect(groups).toHaveLength(1);
    expect(groups[0].slides.map((s) => s.id)).toEqual(["s1", "s2", "s3"]);
  });
});

// ---------------------------------------------------------------------------
// injectVirtualDateSlides
// ---------------------------------------------------------------------------
describe("injectVirtualDateSlides", () => {
  it("inserts date markers between groups", () => {
    const groups: SlideGroup[] = [
      { date: "2025-03-04", slides: [makeSummary({ id: "s1", date: "2025-03-04" })] },
      { date: "2025-03-03", slides: [makeSummary({ id: "s2", date: "2025-03-03" })] },
    ];

    const result = injectVirtualDateSlides(groups);

    expect(result).toHaveLength(4);
    expect(result[0]).toEqual({ type: "date-marker", date: "2025-03-04" });
    expect(result[1]).toEqual(groups[0]);
    expect(result[2]).toEqual({ type: "date-marker", date: "2025-03-03" });
    expect(result[3]).toEqual(groups[1]);
  });

  it("empty groups returns empty", () => {
    expect(injectVirtualDateSlides([])).toEqual([]);
  });

  it("single group gets one marker", () => {
    const groups: SlideGroup[] = [
      { date: "2025-03-04", slides: [makeSummary({ id: "s1", date: "2025-03-04" })] },
    ];

    const result = injectVirtualDateSlides(groups);
    expect(result).toHaveLength(2);
    expect(result[0]).toEqual({ type: "date-marker", date: "2025-03-04" });
    expect(result[1]).toEqual(groups[0]);
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

// ---------------------------------------------------------------------------
// renderMarkdownToHtml
// ---------------------------------------------------------------------------
describe("renderMarkdownToHtml", () => {
  it("renders headings (h1-h3)", () => {
    expect(renderMarkdownToHtml("# Title")).toBe("<h1>Title</h1>");
    expect(renderMarkdownToHtml("## Subtitle")).toBe("<h2>Subtitle</h2>");
    expect(renderMarkdownToHtml("### Section")).toBe("<h3>Section</h3>");
  });

  it("renders h4-h6", () => {
    expect(renderMarkdownToHtml("#### H4")).toBe("<h4>H4</h4>");
    expect(renderMarkdownToHtml("##### H5")).toBe("<h5>H5</h5>");
    expect(renderMarkdownToHtml("###### H6")).toBe("<h6>H6</h6>");
  });

  it("renders bold", () => {
    expect(renderMarkdownToHtml("**bold**")).toBe("<p><strong>bold</strong></p>");
  });

  it("renders inline code", () => {
    expect(renderMarkdownToHtml("`code`")).toBe("<p><code>code</code></p>");
  });

  it("renders list items as ul", () => {
    const md = "- item one\n- item two\n- item three";
    expect(renderMarkdownToHtml(md)).toBe(
      "<ul><li>item one</li><li>item two</li><li>item three</li></ul>"
    );
  });

  it("groups consecutive list items into one ul", () => {
    const md = "- a\n- b\n\n- c";
    const html = renderMarkdownToHtml(md);
    // First group becomes one <ul>, paragraph break, then second group
    expect(html).toContain("<ul><li>a</li><li>b</li></ul>");
    expect(html).toContain("<ul><li>c</li></ul>");
  });

  it("renders paragraphs from plain text", () => {
    expect(renderMarkdownToHtml("Hello world")).toBe("<p>Hello world</p>");
  });

  it("handles multiple paragraphs separated by empty lines", () => {
    const md = "First paragraph\n\nSecond paragraph";
    const html = renderMarkdownToHtml(md);
    expect(html).toBe("<p>First paragraph</p><p>Second paragraph</p>");
  });

  it("empty string returns empty", () => {
    expect(renderMarkdownToHtml("")).toBe("");
  });

  it("escapes raw HTML before rendering markdown", () => {
    const html = renderMarkdownToHtml(
      `<script>alert("x")</script> **bold**`
    );
    expect(html).toBe(
      "<p>&lt;script&gt;alert(&quot;x&quot;)&lt;/script&gt; <strong>bold</strong></p>"
    );
  });

  it("escapes raw HTML inside headings and list items", () => {
    const html = renderMarkdownToHtml("# <b>Title</b>\n- <img src=x>");
    expect(html).toBe(
      "<h1>&lt;b&gt;Title&lt;/b&gt;</h1><ul><li>&lt;img src=x&gt;</li></ul>"
    );
  });
});
