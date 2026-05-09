/**
 * UI visual regression and console health tests.
 *
 * These tests load the UI, interact with buttons and panels, verify there are
 * no console errors or unhandled exceptions, and capture visual snapshots at
 * key states.
 *
 * First run: execute with `--update-snapshots` to generate baseline images.
 *   pnpm test:e2e:visual --update-snapshots
 *
 * Subsequent runs compare against baselines and fail on visual regressions.
 *   pnpm test:e2e:visual
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
// Mock data
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Presentation-style HTML templates — styled to fill the 1920×1080 iframe
// like a real record deck, not bare unstyled text.
// ---------------------------------------------------------------------------

const RECORD_A_HTML = [
  '<div style="display:flex;flex-direction:column;justify-content:center;height:100%;padding:80px 120px;font-family:system-ui,sans-serif">',
  '<h1 style="font-size:72px;font-weight:700;color:#1a1a2e;margin-bottom:32px">Experiment Results</h1>',
  '<p style="font-size:36px;color:#4a4a6a;line-height:1.5;margin-bottom:24px">Analysis of the Q1 dataset reveals a 23% improvement in convergence rate across all test conditions.</p>',
  '<p style="font-size:28px;color:#7a7a9a;line-height:1.6">Additional observations confirm the hypothesis that batch normalization significantly reduces training variance in the lower layers.</p>',
  "</div>",
].join("");

const RECORD_B_HTML = [
  '<div style="display:flex;flex-direction:column;justify-content:center;height:100%;padding:80px 120px;font-family:system-ui,sans-serif">',
  '<h1 style="font-size:72px;font-weight:700;color:#1a1a2e;margin-bottom:48px">Methodology</h1>',
  '<ul style="font-size:32px;color:#4a4a6a;line-height:2;list-style:disc;padding-left:48px">',
  "<li>Baseline comparison using standard SGD optimizer</li>",
  "<li>Test group with adaptive learning rate schedule</li>",
  "<li>Control for hardware variance across GPU clusters</li>",
  "<li>Statistical significance threshold p &lt; 0.01</li>",
  "</ul>",
  "</div>",
].join("");

const RECORD_C_HTML = [
  '<div style="display:flex;flex-direction:column;justify-content:center;align-items:center;height:100%;padding:80px 120px;font-family:system-ui,sans-serif;text-align:center">',
  '<div style="font-size:20px;font-weight:600;color:#6366f1;text-transform:uppercase;letter-spacing:4px;margin-bottom:24px">org/beta</div>',
  '<h1 style="font-size:64px;font-weight:700;color:#1a1a2e;margin-bottom:32px">Infrastructure Migration Plan</h1>',
  '<p style="font-size:32px;color:#4a4a6a;line-height:1.6;max-width:1200px">Phase 2 rollout targets completion by end of Q2 with zero-downtime deployment strategy.</p>',
  "</div>",
].join("");

const RECORD_A = {
  id: "20260309-aabbccdd",
  date: "2026-03-09",
  day_order: "a0",
  html_content: RECORD_A_HTML,
  project_id: "org/alpha",
  source_device_id: "device-a",
  source_ref: null,
  updated_at: "2026-03-09T10:00:00Z",
  deleted_at: null,
  figure_count: 2,
  data_file_count: 1,
};

const RECORD_B = {
  id: "20260309-11223344",
  date: "2026-03-09",
  day_order: "a1",
  html_content: RECORD_B_HTML,
  project_id: "org/beta",
  source_device_id: "device-a",
  source_ref: null,
  updated_at: "2026-03-09T09:00:00Z",
  deleted_at: null,
  figure_count: 0,
  data_file_count: 0,
};

const RECORD_C = {
  id: "20260308-55667788",
  date: "2026-03-08",
  day_order: "a0",
  html_content: RECORD_C_HTML,
  project_id: "org/beta",
  source_device_id: "device-b",
  source_ref: null,
  updated_at: "2026-03-08T12:00:00Z",
  deleted_at: null,
  figure_count: 1,
  data_file_count: 0,
};

const RECORD_A_DETAIL = {
  id: "20260309-aabbccdd",
  date: "2026-03-09",
  day_order: "a0",
  html_content: RECORD_A_HTML,
  notes: "# Important\n\nThese are **bold** notes.\n\n- Item one\n- Item two",
  project_id: "org/alpha",
  source_device_id: "device-a",
  source_ref: null,
  git_remote_url: "https://github.com/org/repo",
  git_hash: "abc1234def5678",
  created_at: "2026-03-09T08:00:00Z",
  updated_at: "2026-03-09T10:00:00Z",
  deleted_at: null,
  figures: [
    {
      filename: "plot1.png",
      s3_key: "records/20260309-aabbccdd/figures/plot1.png",
      size: 45056,
      alt_text: "Plot 1",
    },
    {
      filename: "chart.svg",
      s3_key: "records/20260309-aabbccdd/figures/chart.svg",
      size: 12288,
    },
  ],
  data_files: [
    {
      filename: "results.csv",
      s3_key: "records/20260309-aabbccdd/data/results.csv",
      size: 2048,
      description: "Experiment results",
    },
  ],
};

const PROJECTS = ["org/alpha", "org/beta"];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Returns true for console messages that are expected dev-mode noise
 * (React hydration mismatches from next-themes, etc.) and should not
 * fail the "no console errors" assertion.
 *
 * NOTE: Keep this list tight. Only suppress messages that are known
 * framework noise in dev mode, never real application errors.
 */
function isExpectedDevWarning(msg: string): boolean {
  return (
    msg.includes("hydrat") ||
    msg.includes("Hydrat") ||
    msg.includes("A tree hydrated")
  );
}

/** Sets up API route interception with default mock responses. */
async function setupMockApi(
  page: Page,
  overrides?: {
    records?: unknown;
    projects?: unknown;
  }
) {
  await page.route("**/api/records?*", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        overrides?.records ?? {
          items: [RECORD_A, RECORD_B, RECORD_C],
          total: 3,
          next_cursor: null,
        }
      ),
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
      body: JSON.stringify(
        overrides?.records ?? {
          items: [RECORD_A, RECORD_B, RECORD_C],
          total: 3,
          next_cursor: null,
        }
      ),
    });
  });

  await page.route(
    "**/api/records/20260309-aabbccdd",
    async (route: Route) => {
      const method = route.request().method();
      if (method === "GET") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ record: RECORD_A_DETAIL }),
        });
      } else if (method === "PATCH") {
        const body = route.request().postDataJSON();
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            record: {
              ...RECORD_A_DETAIL,
              ...(body as Record<string, unknown>),
            },
            sync_version: 2,
          }),
        });
      }
    }
  );

  await page.route("**/api/projects", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        overrides?.projects ?? { projects: PROJECTS }
      ),
    });
  });

  await page.route("**/api/sync/version", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        version: 0,
        updated_at: "2026-03-09T10:00:00Z",
      }),
    });
  });

  await page.route("**/api/sync/changes*", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: [],
        server_now: "2026-03-09T10:00:00Z",
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
 * Asserts that no unexpected console errors, unhandled page errors,
 * failed network requests, or Next.js dev indicator issues occurred.
 *
 * Filters out known dev-mode noise (e.g. React hydration mismatches from
 * next-themes setting the class attribute during SSR).
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

test.describe("UI snapshots and console health @visual", () => {
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
      // Ignore requests intercepted by route mocks (e.g. aborted by test teardown)
      if (request.failure()?.errorText === "net::ERR_ABORTED") return;
      failedRequests.push(
        `${request.method()} ${request.url()} — ${request.failure()?.errorText ?? "unknown"}`
      );
    });
  });

  test("initial load: three-panel layout with auto-selected record @visual", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");

    // Wait for the UI to fully render
    await expect(page.getByText("3 records")).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Personal Context" })
    ).toBeVisible();

    // Navigation panel has record thumbnails (rendered as ScaledRecordFrame iframes)
    // Verify thumbnails are present by checking the thumbnail buttons
    const thumbnailButtons = page.locator(
      ".aspect-video"
    );
    await expect(thumbnailButtons.first()).toBeVisible();

    // Most recent record (RECORD_A) is auto-selected — iframe renders the HTML content
    const iframe = page.locator('[data-testid="record-viewer"]').frameLocator('iframe[title="Record content"]');
    await expect(iframe.locator("h1")).toBeVisible();

    // Notes should be visible in detail panel (auto-selected record's notes)
    await expect(page.getByText("Important")).toBeVisible();

    await assertNoErrors(page, consoleErrors, pageErrors, failedRequests);
    await expect(page).toHaveScreenshot("01-initial-load.png", SNAPSHOT_OPTS);
  });

  test("record selected: content and details visible @visual", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 records")).toBeVisible();

    // RECORD_A is auto-selected on load. Wait for detail to load.
    const iframe = page.locator('[data-testid="record-viewer"]').frameLocator('iframe[title="Record content"]');
    await expect(iframe.locator("h1")).toBeVisible();

    // Notes should be visible in detail panel (default tab)
    await expect(page.getByText("Important")).toBeVisible();

    // Metadata bar shows project and git info
    await expect(page.getByText("org/alpha").last()).toBeVisible();
    await expect(page.getByText("abc1234d")).toBeVisible();

    await assertNoErrors(page, consoleErrors, pageErrors, failedRequests);
    await expect(page).toHaveScreenshot("02-record-selected.png", SNAPSHOT_OPTS);
  });

  test("panel toggles: keyboard shortcuts hide and show panels @visual", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 records")).toBeVisible();

    // RECORD_A is auto-selected — wait for content to load
    await expect(
      page.locator('[data-testid="record-viewer"]').frameLocator('iframe[title="Record content"]').locator("h1")
    ).toBeVisible();

    // Hide navigation panel with [ key
    await page.keyboard.press("[");
    // Navigation panel heading should disappear
    await expect(
      page.getByRole("heading", { name: "Records" })
    ).not.toBeVisible();

    await expect(page).toHaveScreenshot("03-nav-hidden.png", SNAPSHOT_OPTS);

    // Hide details panel with ] key
    await page.keyboard.press("]");
    // Notes content should disappear
    await expect(page.getByText("Important")).not.toBeVisible();

    await expect(page).toHaveScreenshot("04-viewer-only.png", SNAPSHOT_OPTS);

    // Restore both panels
    await page.keyboard.press("[");
    await page.keyboard.press("]");
    await expect(
      page.getByRole("heading", { name: "Records" })
    ).toBeVisible();
    await expect(page.getByText("Important")).toBeVisible();

    // Hide metadata bar with \ key
    await page.keyboard.press("\\");
    // Git hash should disappear from metadata bar
    await expect(page.getByText("abc1234d")).not.toBeVisible();

    await expect(page).toHaveScreenshot("05-metadata-hidden.png", SNAPSHOT_OPTS);

    // Restore metadata
    await page.keyboard.press("\\");

    await assertNoErrors(page, consoleErrors, pageErrors, failedRequests);
  });

  test("detail tabs: switching between Notes, Figures, Files @visual", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 records")).toBeVisible();

    // RECORD_A is auto-selected — wait for notes to render
    await expect(page.getByText("Important")).toBeVisible();

    // Switch to Figures tab using role locator — tab text is hidden at narrow
    // panel widths via container query, but role="tab" is always present.
    // Tab order: 0=Notes, 1=Figures, 2=Files.
    // Use force:true because the resizable panel separator can overlap the
    // narrow detail panel tabs at the default viewport width.
    await page.getByRole("tab").nth(1).click({ force: true });
    await expect(page.getByText("plot1.png")).toBeVisible();
    await expect(page.getByText("chart.svg")).toBeVisible();

    await expect(page).toHaveScreenshot("06-figures-tab.png", SNAPSHOT_OPTS);

    // Switch to Files tab
    await page.getByRole("tab").nth(2).click({ force: true });
    await expect(page.getByText("results.csv")).toBeVisible();
    await expect(page.getByText("Experiment results")).toBeVisible();

    await expect(page).toHaveScreenshot("07-files-tab.png", SNAPSHOT_OPTS);

    await assertNoErrors(page, consoleErrors, pageErrors, failedRequests);
  });

  test("dark mode: theme toggle switches appearance @visual", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 records")).toBeVisible();

    // Find and click the dark mode toggle (Moon icon button in header)
    const themeButton = page.locator(
      "header button:has(svg.lucide-moon)"
    );
    await themeButton.click();

    // The <html> element should now have class="dark"
    await expect(page.locator("html")).toHaveClass(/dark/);

    await expect(page).toHaveScreenshot("08-dark-mode.png", SNAPSHOT_OPTS);

    // Click again to go back to light (now Sun icon)
    const lightButton = page.locator(
      "header button:has(svg.lucide-sun)"
    );
    await lightButton.click();
    await expect(page.locator("html")).not.toHaveClass(/dark/);

    await assertNoErrors(page, consoleErrors, pageErrors, failedRequests);
  });

  test("settings overlay: opens and renders all sections @visual", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 records")).toBeVisible();

    // Open settings (gear icon button in header)
    const settingsButton = page.locator(
      "header button:has(svg.lucide-settings)"
    );
    await settingsButton.click();

    // Settings overlay should be visible
    await expect(
      page.getByRole("heading", { name: "Settings" })
    ).toBeVisible();
    await expect(page.getByText("Project Info")).toBeVisible();
    await expect(page.getByText("General")).toBeVisible();

    await expect(page).toHaveScreenshot("09-settings-overlay.png", SNAPSHOT_OPTS);

    // Close settings
    await page
      .locator(".fixed button:has(svg.lucide-x)")
      .click();
    await expect(
      page.getByRole("heading", { name: "Settings" })
    ).not.toBeVisible();

    await assertNoErrors(page, consoleErrors, pageErrors, failedRequests);
  });

  test("empty state: placeholder when no records exist @visual", async ({
    page,
  }) => {
    await setupMockApi(page, {
      records: { items: [], total: 0, next_cursor: null },
      projects: { projects: [] },
    });
    await page.goto("/");

    await expect(page.getByText("0 records")).toBeVisible();
    await expect(
      page.getByText("Empty project", { exact: true })
    ).toBeVisible();

    await assertNoErrors(page, consoleErrors, pageErrors, failedRequests);
    await expect(page).toHaveScreenshot("10-empty-state.png", SNAPSHOT_OPTS);
  });

  test("grid view mode: navigation switches to grid layout @visual", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 records")).toBeVisible();

    // Click the Grid view button (title="Grid view")
    await page.getByTitle("Grid view").click();

    // Verify grid class is applied to the record container (multiple date
    // groups may each have a grid, so use .first())
    await expect(
      page.locator(".grid.grid-cols-2").first()
    ).toBeVisible();

    await expect(page).toHaveScreenshot("11-grid-view.png", SNAPSHOT_OPTS);

    // Switch back to strip view
    await page.getByTitle("Strip view").click();

    await assertNoErrors(page, consoleErrors, pageErrors, failedRequests);
  });
});
