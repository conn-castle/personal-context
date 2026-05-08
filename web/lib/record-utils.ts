import type { RecordSummary, RecordGroup, VirtualDateRecord } from "@/lib/types";

// ---------- UI helper functions (moved from v0.dev mock-data.ts) ----------

/**
 * Formats a date string for display. Returns a consistent format that
 * avoids hydration mismatches (no relative dates).
 *
 * @param dateStr - A YYYY-MM-DD date string.
 * @returns A formatted date string (e.g. "Sat, Mar 7, 2026").
 */
export function formatDate(dateStr: string): string {
  const date = new Date(dateStr + "T12:00:00Z");
  return date.toLocaleDateString("en-US", {
    weekday: "short",
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

/**
 * Formats a date string with relative labels (Today, Yesterday).
 * Only safe for use in client-side effects (not during SSR).
 *
 * @param dateStr - A YYYY-MM-DD date string.
 * @returns "Today", "Yesterday", or a formatted date string.
 */
export function formatRelativeDate(dateStr: string): string {
  const date = new Date(dateStr + "T00:00:00");
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const yesterday = new Date(today);
  yesterday.setDate(yesterday.getDate() - 1);

  if (date.toDateString() === today.toDateString()) return "Today";
  if (date.toDateString() === yesterday.toDateString()) return "Yesterday";

  return date.toLocaleDateString("en-US", {
    weekday: "short",
    month: "short",
    day: "numeric",
  });
}

/**
 * Formats a byte count as a human-readable file size string.
 *
 * @param bytes - File size in bytes.
 * @returns Formatted size string (e.g. "1.2 KB", "3.4 MB").
 */
export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * Extracts unique project IDs from an array of records.
 *
 * @param records - Array of records with optional project_id.
 * @returns Sorted array of unique, non-null project ID strings.
 */
export function getUniqueProjects(
  records: { project_id: string | null }[]
): string[] {
  const projects = new Set<string>();
  for (const record of records) {
    if (record.project_id) {
      projects.add(record.project_id);
    }
  }
  return Array.from(projects).sort();
}

/**
 * Groups records by date in descending date order, ascending day_order within
 * each date. Works with any object that has `date` and `day_order` fields.
 *
 * @param records - Array of records to group.
 * @returns An array of { date, records } groups.
 */
export function groupRecordsByDateDesc<
  T extends { date: string; day_order: string }
>(records: T[]): { date: string; records: T[] }[] {
  const sorted = [...records].sort((a, b) => {
    const dateCompare = b.date.localeCompare(a.date);
    if (dateCompare !== 0) return dateCompare;
    return a.day_order.localeCompare(b.day_order);
  });

  const groups = new Map<string, T[]>();
  for (const record of sorted) {
    const existing = groups.get(record.date) || [];
    existing.push(record);
    groups.set(record.date, existing);
  }

  return Array.from(groups.entries()).map(([date, records]) => ({
    date,
    records,
  }));
}

/**
 * Groups an array of record summaries by date.
 *
 * Assumes records are already sorted by date DESC, day_order ASC.
 * Adjacent records with the same date are grouped together.
 *
 * @param records - Sorted array of record summaries.
 * @returns An array of RecordGroup objects, one per unique date.
 */
export function groupRecordsByDate(records: RecordSummary[]): RecordGroup[] {
  if (records.length === 0) return [];

  const groups: RecordGroup[] = [];
  let current: RecordGroup | null = null;

  for (const record of records) {
    if (current === null || current.date !== record.date) {
      current = { date: record.date, records: [record] };
      groups.push(current);
    } else {
      current.records.push(record);
    }
  }

  return groups;
}

/**
 * Inserts virtual date-marker elements before each record group.
 *
 * Every group receives a date marker immediately before it.
 *
 * @param groups - Record groups (typically from groupRecordsByDate).
 * @returns Alternating VirtualDateRecord and RecordGroup elements.
 */
export function injectVirtualDateRecords(
  groups: RecordGroup[]
): (RecordGroup | VirtualDateRecord)[] {
  const result: (RecordGroup | VirtualDateRecord)[] = [];

  for (const group of groups) {
    result.push({ type: "date-marker", date: group.date });
    result.push(group);
  }

  return result;
}

/**
 * Builds the regex used to discover and rewrite `figures/{filename}` src
 * attributes.
 *
 * Query strings and hash fragments are treated as part of the source attribute
 * but ignored for filename lookup because the resolved URL replaces the entire
 * value.
 *
 * @returns A fresh global regex for figure src attributes.
 */
function createFigureSrcAttributePattern(): RegExp {
  return /src\s*=\s*(["'])figures\/([^"'?#]+)(?:[?#][^"']*)?\1/gi;
}

/**
 * Collects unique figure filenames referenced by `figures/{filename}` sources.
 *
 * @param html - The raw record HTML.
 * @returns Unique figure filenames in first-seen order.
 */
export function getFigureFilenames(html: string): string[] {
  return Array.from(
    new Set(
      Array.from(
        html.matchAll(createFigureSrcAttributePattern()),
        (match) => match[2]
      )
    )
  );
}

/**
 * Rewrites `figures/{filename}` image sources in record HTML to resolved URLs.
 *
 * External URLs and non-figure relative sources are left untouched.
 *
 * @param html - The raw record HTML.
 * @param urlByFilename - Mapping of figure filename to resolved URL.
 * @returns HTML with known figure sources rewritten.
 */
export function rewriteFigureSources(
  html: string,
  urlByFilename: Record<string, string>
): string {
  return html.replace(
    createFigureSrcAttributePattern(),
    (match, quote: string, filename: string) => {
      const resolvedUrl = urlByFilename[filename];
      if (!resolvedUrl) {
        return match;
      }
      return `src=${quote}${resolvedUrl}${quote}`;
    }
  );
}

/**
 * Converts a simple subset of Markdown to HTML.
 *
 * Supported syntax:
 * - `# heading` through `###### heading` (h1-h6)
 * - `**bold**` inline
 * - `` `code` `` inline
 * - `- list item` (consecutive items grouped into `<ul>`)
 * - Empty lines create paragraph breaks
 * - Plain text wrapped in `<p>`
 *
 * @param markdown - The markdown source string.
 * @returns An HTML string.
 */
export function renderMarkdownToHtml(markdown: string): string {
  if (markdown === "") return "";

  const lines = markdown.split("\n");
  const blocks: string[] = [];
  let listBuffer: string[] = [];

  function flushList(): void {
    if (listBuffer.length > 0) {
      blocks.push(
        "<ul>" + listBuffer.map((item) => `<li>${item}</li>`).join("") + "</ul>"
      );
      listBuffer = [];
    }
  }

  for (const line of lines) {
    // Empty line — flush list and act as paragraph separator
    if (line.trim() === "") {
      flushList();
      continue;
    }

    // Heading: # through ######
    const headingMatch = line.match(/^(#{1,6})\s+(.+)$/);
    if (headingMatch) {
      flushList();
      const level = headingMatch[1].length;
      const text = applyInline(headingMatch[2]);
      blocks.push(`<h${level}>${text}</h${level}>`);
      continue;
    }

    // List item: - text
    const listMatch = line.match(/^-\s+(.+)$/);
    if (listMatch) {
      listBuffer.push(applyInline(listMatch[1]));
      continue;
    }

    // Plain text → paragraph
    flushList();
    blocks.push(`<p>${applyInline(line)}</p>`);
  }

  flushList();

  return blocks.join("");
}

/**
 * Applies inline markdown formatting (bold, code) to a text string.
 *
 * @param text - The raw text to process.
 * @returns The text with inline HTML substitutions.
 */
function applyInline(text: string): string {
  let result = escapeHtml(text);
  // Bold: **text**
  result = result.replace(/\*\*(.+?)\*\*/g, "<strong>$1</strong>");
  // Inline code: `text`
  result = result.replace(/`(.+?)`/g, "<code>$1</code>");
  return result;
}

/**
 * Escapes raw HTML characters before markdown tags are applied.
 *
 * @param text - Untrusted markdown text.
 * @returns HTML-escaped text.
 */
function escapeHtml(text: string): string {
  return text
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}
