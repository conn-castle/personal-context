/**
 * Markdown rendering e2e tests.
 *
 * Verifies that the MarkdownRenderer component correctly renders all supported
 * markdown syntax in the record detail Notes tab, including mermaid diagrams.
 *
 * Uses self-describing text (e.g. "# Header 1") so visual inspection of
 * screenshots immediately reveals rendering correctness.
 *
 * First run: generate baselines with `--update-snapshots`:
 *   pnpm exec playwright test tests/e2e/markdown-rendering.e2e.spec.ts --update-snapshots
 *
 * Subsequent runs compare against baselines:
 *   pnpm exec playwright test tests/e2e/markdown-rendering.e2e.spec.ts
 */
import { expect, test, type Page, type Route } from "@playwright/test";
import path from "path";

// Hide the Next.js dev indicator badge from screenshots via injected CSS.
// The stylePath stylesheet pierces Shadow DOM and applies only during capture.
// assertNoNextJsErrors still inspects the shadow DOM for real errors.
const SNAPSHOT_OPTS = {
  maxDiffPixelRatio: 0.02,
  stylePath: path.join(__dirname, "screenshot.css"),
};



// ---------------------------------------------------------------------------
// Comprehensive markdown content with self-describing text
// ---------------------------------------------------------------------------

const FULL_MARKDOWN_NOTES = `# Header 1

## Header 2

### Header 3

#### Header 4

##### Header 5

###### Header 6

---

**Bold Text** and *Italic Text* and ***Bold Italic Text***

~~Strikethrough Text~~

Inline \`code snippet\` within a sentence.

> Blockquote: This is a quoted paragraph.
> It can span multiple lines.

- Unordered Item 1
- Unordered Item 2
  - Nested Item 2a
  - Nested Item 2b
- Unordered Item 3

1. Ordered Item 1
2. Ordered Item 2
3. Ordered Item 3

- [x] Task Complete
- [ ] Task Incomplete

| Column A | Column B | Column C |
|----------|----------|----------|
| Row 1A   | Row 1B   | Row 1C   |
| Row 2A   | Row 2B   | Row 2C   |

\`\`\`python
def hello():
    print("Code Block")
\`\`\`

[Link Text](https://example.com)

\`\`\`mermaid
graph TD
    A[Start] --> B{Decision}
    B -->|Yes| C[Action 1]
    B -->|No| D[Action 2]
    C --> E[End]
    D --> E
\`\`\`
`;

// ---------------------------------------------------------------------------
// Minimal record data — only one record, focused on notes rendering
// ---------------------------------------------------------------------------

const RECORD_HTML = [
  '<div style="display:flex;flex-direction:column;justify-content:center;height:100%;padding:80px 120px;font-family:system-ui,sans-serif">',
  '<h1 style="font-size:72px;font-weight:700;color:#1a1a2e;margin-bottom:32px">Markdown Test Record</h1>',
  '<p style="font-size:36px;color:#4a4a6a;line-height:1.5">This record has comprehensive markdown notes.</p>',
  "</div>",
].join("");

const RECORD = {
  id: "20260310-md000001",
  date: "2026-03-10",
  day_order: "a0",
  html_content: RECORD_HTML,
  project_id: "test/markdown",
  source_device_id: "device-a",
  source_ref: null,
  updated_at: "2026-03-10T10:00:00Z",
  deleted_at: null,
  figure_count: 0,
  data_file_count: 0,
};

const RECORD_DETAIL = {
  ...RECORD,
  notes: FULL_MARKDOWN_NOTES,
  git_remote_url: null,
  git_hash: null,
  created_at: "2026-03-10T08:00:00Z",
  figures: [],
  data_files: [],
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Returns true for console messages that are expected dev-mode noise.
 * Keep this list tight — only known framework noise, never real app errors.
 */
function isExpectedDevWarning(msg: string): boolean {
  return (
    msg.includes("hydrat") ||
    msg.includes("Hydrat") ||
    msg.includes("A tree hydrated")
  );
}

/** Sets up API route interception with the markdown test record. */
async function setupMockApi(page: Page) {
  await page.route("**/api/records?*", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ items: [RECORD], next_cursor: null }),
    });
  });

  await page.route("**/api/records", async (route: Route) => {
    if (route.request().method() !== "GET") {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ items: [RECORD], next_cursor: null }),
    });
  });

  await page.route(
    `**/api/records/${RECORD.id}`,
    async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ record: RECORD_DETAIL }),
      });
    }
  );

  await page.route("**/api/projects", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ projects: ["test/markdown"] }),
    });
  });

  await page.route("**/api/sync/version", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        version: 0,
        updated_at: "2026-03-10T10:00:00Z",
      }),
    });
  });

  await page.route("**/api/sync/changes*", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: [],
        server_now: "2026-03-10T10:00:00Z",
      }),
    });
  });
}

/**
 * Asserts that the Next.js dev indicator shows zero issues.
 * Inspects the `<nextjs-portal>` shadow DOM for error indicator buttons.
 */
async function assertNoNextJsErrors(page: Page) {
  const issueText = await page.evaluate(() => {
    const portal = document.querySelector("nextjs-portal");
    if (!portal || !portal.shadowRoot) return null;
    const buttons = portal.shadowRoot.querySelectorAll("button");
    for (const btn of buttons) {
      const t = btn.textContent?.trim() || "";
      if (/^\d+\s*Issues?$/.test(t)) return t;
    }
    return null;
  });
  expect(
    issueText,
    `Next.js dev indicator shows "${issueText}". ` +
      `Run the dev server and inspect the error overlay for details.`
  ).toBeNull();
}

/**
 * Asserts no console errors, page errors, failed requests, or Next.js issues.
 */
async function assertNoErrors(
  page: Page,
  consoleErrors: string[],
  pageErrors: string[],
  failedRequests: string[]
) {
  const realConsoleErrors = consoleErrors.filter(
    (e) => !isExpectedDevWarning(e)
  );
  expect(
    realConsoleErrors,
    `Unexpected console errors:\n${realConsoleErrors.join("\n")}`
  ).toEqual([]);
  expect(
    pageErrors,
    `Unhandled page errors:\n${pageErrors.join("\n")}`
  ).toEqual([]);
  expect(
    failedRequests,
    `Failed network requests:\n${failedRequests.join("\n")}`
  ).toEqual([]);
  await assertNoNextJsErrors(page);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe("Markdown rendering @e2e @visual", () => {
  let consoleErrors: string[];
  let pageErrors: string[];
  let failedRequests: string[];

  test.beforeEach(async ({ page }) => {
    consoleErrors = [];
    pageErrors = [];
    failedRequests = [];
    page.on("console", (msg) => {
      if (msg.type() === "error") {
        consoleErrors.push(msg.text());
      }
    });
    page.on("pageerror", (error) => {
      pageErrors.push(error.message);
    });
    page.on("requestfailed", (request) => {
      if (request.failure()?.errorText === "net::ERR_ABORTED") return;
      failedRequests.push(
        `${request.method()} ${request.url()} — ${request.failure()?.errorText ?? "unknown"}`
      );
    });
  });

  test("all markdown elements render correctly in notes panel @visual", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");

    // Wait for the record to load and auto-select
    await expect(page.getByText("1 record")).toBeVisible();

    // Wait for notes to render in the detail panel
    const notesArea = page.locator(".prose");
    await expect(notesArea).toBeVisible();

    // -----------------------------------------------------------------------
    // Headings (H1–H6)
    // -----------------------------------------------------------------------
    await expect(
      notesArea.getByRole("heading", { name: "Header 1", level: 1 })
    ).toBeVisible();
    await expect(
      notesArea.getByRole("heading", { name: "Header 2", level: 2 })
    ).toBeVisible();
    await expect(
      notesArea.getByRole("heading", { name: "Header 3", level: 3 })
    ).toBeVisible();
    await expect(
      notesArea.getByRole("heading", { name: "Header 4", level: 4 })
    ).toBeVisible();
    await expect(
      notesArea.getByRole("heading", { name: "Header 5", level: 5 })
    ).toBeVisible();
    await expect(
      notesArea.getByRole("heading", { name: "Header 6", level: 6 })
    ).toBeVisible();

    // -----------------------------------------------------------------------
    // Horizontal rule
    // -----------------------------------------------------------------------
    await expect(notesArea.locator("hr")).toBeVisible();

    // -----------------------------------------------------------------------
    // Text formatting: bold, italic, bold+italic, strikethrough, inline code
    // -----------------------------------------------------------------------
    await expect(
      notesArea.locator("strong", { hasText: "Bold Text" })
    ).toBeVisible();
    await expect(
      notesArea.locator("em", { hasText: /^Italic Text$/ })
    ).toBeVisible();
    // Bold italic renders as <strong><em> or <em><strong>
    await expect(notesArea.getByText("Bold Italic Text")).toBeVisible();
    await expect(
      notesArea.locator("del", { hasText: "Strikethrough Text" })
    ).toBeVisible();
    await expect(
      notesArea.locator("code", { hasText: "code snippet" })
    ).toBeVisible();

    // -----------------------------------------------------------------------
    // Blockquote
    // -----------------------------------------------------------------------
    await expect(
      notesArea.locator("blockquote", {
        hasText: "This is a quoted paragraph",
      })
    ).toBeVisible();

    // -----------------------------------------------------------------------
    // Unordered list with nesting
    // -----------------------------------------------------------------------
    await expect(notesArea.getByText("Unordered Item 1")).toBeVisible();
    await expect(notesArea.getByText("Nested Item 2a")).toBeVisible();

    // -----------------------------------------------------------------------
    // Ordered list
    // -----------------------------------------------------------------------
    await expect(
      notesArea.getByText("Ordered Item 1", { exact: true })
    ).toBeVisible();
    await expect(
      notesArea.getByText("Ordered Item 3", { exact: true })
    ).toBeVisible();

    // -----------------------------------------------------------------------
    // Task list (GFM)
    // -----------------------------------------------------------------------
    // Task lists render as checkboxes via remark-gfm
    await expect(notesArea.getByText("Task Complete")).toBeVisible();
    await expect(notesArea.getByText("Task Incomplete")).toBeVisible();
    // Check that there's a checked checkbox
    const checkedBox = notesArea.locator('input[type="checkbox"][checked]');
    await expect(checkedBox).toBeVisible();
    // And an unchecked one
    const uncheckedBox = notesArea.locator(
      'input[type="checkbox"]:not([checked])'
    );
    await expect(uncheckedBox).toBeVisible();

    // -----------------------------------------------------------------------
    // Table (GFM)
    // -----------------------------------------------------------------------
    await expect(notesArea.locator("table")).toBeVisible();
    await expect(notesArea.getByText("Column A")).toBeVisible();
    await expect(notesArea.getByText("Row 1B")).toBeVisible();
    await expect(notesArea.getByText("Row 2C")).toBeVisible();

    // -----------------------------------------------------------------------
    // Code block
    // -----------------------------------------------------------------------
    await expect(
      notesArea.locator("code", { hasText: 'print("Code Block")' })
    ).toBeVisible();

    // -----------------------------------------------------------------------
    // Link
    // -----------------------------------------------------------------------
    const link = notesArea.getByRole("link", { name: "Link Text" });
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute("href", "https://example.com");

    // -----------------------------------------------------------------------
    // Mermaid diagram — renders as SVG inside the .prose container
    // -----------------------------------------------------------------------
    // Wait for mermaid to render (async client-side operation)
    const mermaidSvg = notesArea.locator("svg").first();
    await expect(mermaidSvg).toBeVisible({ timeout: 10_000 });

    // Verify the diagram contains expected node labels
    await expect(notesArea.locator("svg")).toContainText("Start");
    await expect(notesArea.locator("svg")).toContainText("Decision");
    await expect(notesArea.locator("svg")).toContainText("Action 1");
    await expect(notesArea.locator("svg")).toContainText("End");

    await assertNoErrors(page, consoleErrors, pageErrors, failedRequests);

    // -----------------------------------------------------------------------
    // Maximise the detail panel for screenshots:
    // 1. Hide the left nav panel with [ key
    // 2. Drag the resize handle to the far left to give details maximum width
    // -----------------------------------------------------------------------
    await page.keyboard.press("[");
    await expect(
      page.getByRole("heading", { name: "Records" })
    ).not.toBeVisible();

    // Drag the resize handle between viewer and details panels to the left
    const handle = page.locator('[role="separator"]');
    await expect(handle).toBeVisible();
    const handleBox = await handle.boundingBox();
    if (!handleBox) throw new Error("Resize handle not found");
    // Drag from handle center to 30% from the left edge of the viewport
    const viewportSize = page.viewportSize();
    if (!viewportSize) throw new Error("Viewport size not available");
    await page.mouse.move(
      handleBox.x + handleBox.width / 2,
      handleBox.y + handleBox.height / 2
    );
    await page.mouse.down();
    await page.mouse.move(viewportSize.width * 0.3, handleBox.y + handleBox.height / 2, {
      steps: 10,
    });
    await page.mouse.up();

    // Wait for layout to stabilise after resize
    await expect(notesArea).toBeVisible();

    // -----------------------------------------------------------------------
    // Visual snapshot of the full notes panel
    // -----------------------------------------------------------------------
    // Scroll notes to top to get a consistent snapshot
    await notesArea.evaluate((el) => {
      el.closest("[data-radix-scroll-area-viewport]")?.scrollTo(0, 0);
    });
    await expect(page).toHaveScreenshot(
      "01-markdown-notes-top.png",
      SNAPSHOT_OPTS
    );

    // Scroll to the table to capture table, task lists, code block, and link
    await notesArea.evaluate((el) => {
      const table = el.querySelector("table");
      if (table) {
        table.scrollIntoView({ block: "start" });
      }
    });
    await expect(notesArea.locator("table")).toBeVisible();
    await expect(page).toHaveScreenshot(
      "02-markdown-notes-middle.png",
      SNAPSHOT_OPTS
    );

    // Scroll to the bottom to capture the mermaid diagram
    await notesArea.evaluate((el) => {
      const viewport = el.closest("[data-radix-scroll-area-viewport]");
      if (viewport) viewport.scrollTo(0, viewport.scrollHeight);
    });
    // Wait a beat for mermaid SVG to be in view
    await expect(mermaidSvg).toBeVisible();
    await expect(page).toHaveScreenshot(
      "03-markdown-notes-bottom.png",
      SNAPSHOT_OPTS
    );
  });

  test("edit mode: shows raw markdown source in textarea @visual", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("1 record")).toBeVisible();

    // Wait for rendered notes to appear
    const notesArea = page.locator(".prose");
    await expect(notesArea).toBeVisible();

    // Hide nav panel and maximise details panel (same as render test)
    await page.keyboard.press("[");
    await expect(
      page.getByRole("heading", { name: "Records" })
    ).not.toBeVisible();

    const handle = page.locator('[role="separator"]');
    await expect(handle).toBeVisible();
    const handleBox = await handle.boundingBox();
    if (!handleBox) throw new Error("Resize handle not found");
    const viewportSize = page.viewportSize();
    if (!viewportSize) throw new Error("Viewport size not available");
    await page.mouse.move(
      handleBox.x + handleBox.width / 2,
      handleBox.y + handleBox.height / 2
    );
    await page.mouse.down();
    await page.mouse.move(viewportSize.width * 0.3, handleBox.y + handleBox.height / 2, {
      steps: 10,
    });
    await page.mouse.up();

    // Click the edit button (pencil icon)
    const editButton = page.locator('button[title="Edit notes"]');
    await expect(editButton).toBeVisible();
    await editButton.click();

    // The textarea should appear with the raw markdown source
    const textarea = page.locator("textarea");
    await expect(textarea).toBeVisible();
    await expect(textarea).toContainText("# Header 1");
    await expect(textarea).toContainText("```mermaid");
    await expect(textarea).toContainText("**Bold Text**");

    // "Editing markdown" label should be visible
    await expect(page.getByText("Editing markdown")).toBeVisible();

    await assertNoErrors(page, consoleErrors, pageErrors, failedRequests);

    await expect(page).toHaveScreenshot(
      "04-markdown-edit-mode.png",
      SNAPSHOT_OPTS
    );
  });

  test("mermaid diagram renders as SVG, not raw text @e2e", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("1 record")).toBeVisible();

    const notesArea = page.locator(".prose");
    await expect(notesArea).toBeVisible();

    // The mermaid code block should NOT appear as raw text
    // (if mermaid fails, it falls back to showing the raw definition)
    const rawMermaidText = notesArea.getByText("graph TD");
    await expect(rawMermaidText).not.toBeVisible({ timeout: 10_000 });

    // Instead, an SVG should be present
    const svgElement = notesArea.locator("svg").first();
    await expect(svgElement).toBeVisible({ timeout: 10_000 });

    // SVG should contain edges (path or line elements from the flowchart)
    const svgContent = await svgElement.innerHTML();
    expect(svgContent).toContain("path");

    await assertNoErrors(page, consoleErrors, pageErrors, failedRequests);
  });
});
