import { expect, test, type Page, type Route } from "@playwright/test";

// ---------------------------------------------------------------------------
// Dynamic date helpers — ensures tests are date-independent
// ---------------------------------------------------------------------------

function padTwo(n: number): string {
  return String(n).padStart(2, "0");
}

/** Returns "YYYYMMDD" for use in slide IDs. */
function dateId(d: Date): string {
  return `${d.getFullYear()}${padTwo(d.getMonth() + 1)}${padTwo(d.getDate())}`;
}

/** Returns "YYYY-MM-DD" for use in date fields. */
function dateField(d: Date): string {
  return `${d.getFullYear()}-${padTwo(d.getMonth() + 1)}-${padTwo(d.getDate())}`;
}

/** Returns an ISO timestamp string with the given time suffix. */
function dateIso(d: Date, time: string): string {
  return `${dateField(d)}T${time}`;
}

const _now = new Date();
_now.setHours(12, 0, 0, 0);
const _yesterday = new Date(_now);
_yesterday.setDate(_yesterday.getDate() - 1);
const _twoDaysAgo = new Date(_now);
_twoDaysAgo.setDate(_twoDaysAgo.getDate() - 2);

const TODAY_ID = dateId(_now);
const TODAY_DATE = dateField(_now);
const YESTERDAY_ID = dateId(_yesterday);
const YESTERDAY_DATE = dateField(_yesterday);
const TWO_DAYS_AGO_ID = dateId(_twoDaysAgo);
const TWO_DAYS_AGO_DATE = dateField(_twoDaysAgo);

// ---------------------------------------------------------------------------
// Mock data (dates computed relative to today)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Presentation-style HTML templates — styled to fill the 1920×1080 iframe
// like a real slide deck, not bare unstyled text.
// ---------------------------------------------------------------------------

const SLIDE_A_HTML = [
  '<div style="display:flex;flex-direction:column;justify-content:center;height:100%;padding:80px 120px;font-family:system-ui,sans-serif">',
  '<h1 style="font-size:72px;font-weight:700;color:#1a1a2e;margin-bottom:32px">Experiment Results</h1>',
  '<p style="font-size:36px;color:#4a4a6a;line-height:1.5;margin-bottom:24px">Analysis of the Q1 dataset reveals a 23% improvement in convergence rate across all test conditions.</p>',
  '<p style="font-size:28px;color:#7a7a9a;line-height:1.6">Additional observations confirm the hypothesis.</p>',
  "</div>",
].join("");

const SLIDE_C_HTML = [
  '<div style="display:flex;flex-direction:column;justify-content:center;align-items:center;height:100%;padding:80px 120px;font-family:system-ui,sans-serif;text-align:center">',
  '<div style="font-size:20px;font-weight:600;color:#6366f1;text-transform:uppercase;letter-spacing:4px;margin-bottom:24px">org/beta</div>',
  '<h1 style="font-size:64px;font-weight:700;color:#1a1a2e;margin-bottom:32px">Infrastructure Migration</h1>',
  '<p style="font-size:32px;color:#4a4a6a;line-height:1.6;max-width:1200px">Phase 2 rollout targets completion by end of Q2.</p>',
  "</div>",
].join("");

const DELETED_SLIDE_HTML = [
  '<div style="display:flex;flex-direction:column;justify-content:center;align-items:center;height:100%;padding:80px 120px;font-family:system-ui,sans-serif;text-align:center">',
  '<h1 style="font-size:64px;font-weight:700;color:#1a1a2e;margin-bottom:32px">Archived Analysis</h1>',
  '<p style="font-size:32px;color:#4a4a6a">This slide has been removed from the active deck.</p>',
  "</div>",
].join("");

const SLIDE_A = {
  id: `${TODAY_ID}-aabbccdd`,
  date: TODAY_DATE,
  day_order: "a0",
  html_content: SLIDE_A_HTML,
  project_id: "org/alpha",
  source_device_id: "device-a",
  source_ref: "vault://experiment",
  updated_at: dateIso(_now, "10:00:00Z"),
  deleted_at: null,
  figure_count: 2,
  data_file_count: 1,
};

const SLIDE_B = {
  id: `${TODAY_ID}-11223344`,
  date: TODAY_DATE,
  day_order: "a1",
  html_content: null,
  project_id: "org/beta",
  source_device_id: "device-a",
  source_ref: "obsidian://methodology",
  updated_at: dateIso(_now, "09:00:00Z"),
  deleted_at: null,
  figure_count: 0,
  data_file_count: 0,
};

const SLIDE_C = {
  id: `${YESTERDAY_ID}-55667788`,
  date: YESTERDAY_DATE,
  day_order: "a0",
  html_content: SLIDE_C_HTML,
  project_id: "org/beta",
  source_device_id: "device-b",
  source_ref: null,
  updated_at: dateIso(_yesterday, "12:00:00Z"),
  deleted_at: null,
  figure_count: 1,
  data_file_count: 0,
};

const SLIDE_A_DETAIL = {
  id: `${TODAY_ID}-aabbccdd`,
  date: TODAY_DATE,
  day_order: "a0",
  html_content: SLIDE_A_HTML,
  notes: "# Important\n\nThese are **bold** notes.",
  project_id: "org/alpha",
  source_device_id: "device-a",
  source_ref: "vault://experiment",
  git_remote_url: "https://github.com/org/repo",
  git_hash: "abc1234def5678",
  created_at: dateIso(_now, "08:00:00Z"),
  updated_at: dateIso(_now, "10:00:00Z"),
  deleted_at: null,
  figures: [
    { filename: "plot1.png", s3_key: `slides/${TODAY_ID}-aabbccdd/figures/plot1.png`, size: 45056, alt_text: "Plot 1" },
    { filename: "chart.svg", s3_key: `slides/${TODAY_ID}-aabbccdd/figures/chart.svg`, size: 12288 },
  ],
  data_files: [
    { filename: "results.csv", s3_key: `slides/${TODAY_ID}-aabbccdd/data/results.csv`, size: 2048, description: "Experiment results" },
  ],
};

const SLIDE_B_DETAIL = {
  id: `${TODAY_ID}-11223344`,
  date: TODAY_DATE,
  day_order: "a1",
  html_content: null,
  notes: "# Methodology\n\nNotes-first record with supporting data.",
  project_id: "org/beta",
  source_device_id: "device-a",
  source_ref: "obsidian://methodology",
  git_remote_url: null,
  git_hash: null,
  created_at: dateIso(_now, "08:30:00Z"),
  updated_at: dateIso(_now, "09:00:00Z"),
  deleted_at: null,
  figures: [],
  data_files: [
    { filename: "methodology.json", s3_key: `slides/${TODAY_ID}-11223344/data/methodology.json`, size: 1024, description: "Method notes" },
  ],
};

const DELETED_SLIDE = {
  id: `${TWO_DAYS_AGO_ID}-deadbeef`,
  date: TWO_DAYS_AGO_DATE,
  day_order: "a0",
  html_content: DELETED_SLIDE_HTML,
  project_id: "org/alpha",
  source_device_id: "device-a",
  source_ref: null,
  updated_at: dateIso(_twoDaysAgo, "12:00:00Z"),
  deleted_at: dateIso(_twoDaysAgo, "14:00:00Z"),
  figure_count: 0,
  data_file_count: 0,
};

const DELETED_SLIDE_DETAIL = {
  id: `${TWO_DAYS_AGO_ID}-deadbeef`,
  date: TWO_DAYS_AGO_DATE,
  day_order: "a0",
  html_content: DELETED_SLIDE_HTML,
  notes: null,
  project_id: "org/alpha",
  source_device_id: "device-a",
  source_ref: null,
  git_remote_url: null,
  git_hash: null,
  created_at: dateIso(_twoDaysAgo, "08:00:00Z"),
  updated_at: dateIso(_twoDaysAgo, "12:00:00Z"),
  deleted_at: dateIso(_twoDaysAgo, "14:00:00Z"),
  figures: [],
  data_files: [],
};

const PROJECTS = ["org/alpha", "org/beta"];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Sets up API route interception with default mock responses.
 * Returns an object to track API calls.
 */
async function setupMockApi(page: Page, overrides?: {
  slides?: unknown;
  projects?: unknown;
  syncVersion?: unknown;
}) {
  const calls: { method: string; url: string; body?: unknown }[] = [];

  // Mock /api/slides (list)
  await page.route("**/api/slides?*", async (route: Route) => {
    const url = route.request().url();
    calls.push({ method: "GET", url });

    // Check if requesting deleted slides
    if (url.includes("deleted=true")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(
          overrides?.slides ?? { items: [DELETED_SLIDE], next_cursor: null }
        ),
      });
      return;
    }

    // Check for project filter
    if (url.includes("project=")) {
      const projectMatch = url.match(/project=([^&]+)/);
      const projectId = projectMatch ? decodeURIComponent(projectMatch[1]) : null;
      const filtered = [SLIDE_A, SLIDE_B, SLIDE_C].filter(
        (s) => s.project_id === projectId
      );
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ items: filtered, next_cursor: null }),
      });
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        overrides?.slides ?? { items: [SLIDE_A, SLIDE_B, SLIDE_C], next_cursor: null }
      ),
    });
  });

  // Mock /api/slides (list, no query params)
  await page.route("**/api/slides", async (route: Route) => {
    if (route.request().method() !== "GET") {
      await route.continue();
      return;
    }
    calls.push({ method: "GET", url: route.request().url() });
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        overrides?.slides ?? { items: [SLIDE_A, SLIDE_B, SLIDE_C], next_cursor: null }
      ),
    });
  });

  // Mock /api/slides/[id] (detail)
  await page.route(`**/api/slides/${TODAY_ID}-aabbccdd`, async (route: Route) => {
    const method = route.request().method();
    calls.push({ method, url: route.request().url() });

    if (method === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ slide: SLIDE_A_DETAIL }),
      });
    } else if (method === "PATCH") {
      const body = route.request().postDataJSON();
      calls.push({ method: "PATCH-body", url: route.request().url(), body });
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          slide: { ...SLIDE_A_DETAIL, ...(body as Record<string, unknown>) },
          sync_version: 2,
        }),
      });
    } else if (method === "DELETE") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          id: `${TODAY_ID}-aabbccdd`,
          deleted_at: dateIso(_now, "12:00:00Z"),
          updated_at: dateIso(_now, "12:00:00Z"),
          sync_version: 3,
        }),
      });
    }
  });

  await page.route(`**/api/slides/${TODAY_ID}-11223344`, async (route: Route) => {
    calls.push({ method: route.request().method(), url: route.request().url() });
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ slide: SLIDE_B_DETAIL }),
    });
  });

  // Mock /api/slides/[id] for deleted slide
  await page.route(`**/api/slides/${TWO_DAYS_AGO_ID}-deadbeef`, async (route: Route) => {
    const method = route.request().method();
    calls.push({ method, url: route.request().url() });

    if (method === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ slide: DELETED_SLIDE_DETAIL }),
      });
    }
  });

  // Mock /api/slides/[id]/restore
  await page.route(`**/api/slides/${TWO_DAYS_AGO_ID}-deadbeef/restore`, async (route: Route) => {
    calls.push({ method: "POST", url: route.request().url() });
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        id: `${TWO_DAYS_AGO_ID}-deadbeef`,
        deleted_at: null,
        updated_at: dateIso(_now, "12:00:00Z"),
        sync_version: 4,
      }),
    });
  });

  // Mock /api/projects
  await page.route("**/api/projects", async (route: Route) => {
    calls.push({ method: "GET", url: route.request().url() });
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        overrides?.projects ?? { projects: PROJECTS }
      ),
    });
  });

  // Mock /api/sync/version
  await page.route("**/api/sync/version", async (route: Route) => {
    calls.push({ method: "GET", url: route.request().url() });
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        overrides?.syncVersion ?? {
          version: 0,
          updated_at: dateIso(_now, "10:00:00Z"),
        }
      ),
    });
  });

  // Mock /api/sync/changes
  await page.route("**/api/sync/changes*", async (route: Route) => {
    calls.push({ method: "GET", url: route.request().url() });
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: [],
        server_now: dateIso(_now, "10:00:00Z"),
      }),
    });
  });

  return { calls };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe("Slide Browser", () => {
  test("browse slides: renders slides with date headers and slide count @e2e", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");

    // App title visible
    await expect(page.getByRole("heading", { name: "Personal Context" })).toBeVisible();

    // Date headers visible (mock dates are today/yesterday, so labels are stable).
    // "Today" also appears in the metadata bar for the auto-selected slide, so
    // scope to the first match (navigation date header).
    await expect(page.getByText("Today").first()).toBeVisible();
    await expect(page.getByText("Yesterday").first()).toBeVisible();

    // Slide count badge
    await expect(page.getByText("3 slides")).toBeVisible();

    // Auto-selection: first slide (SLIDE_A) is auto-selected, so the viewer
    // shows an iframe with slide content instead of a placeholder.
    const previewFrame = page.locator('[data-testid="slide-viewer"]').frameLocator('iframe[title="Slide content"]');
    await expect(previewFrame.getByRole("heading", { name: "Experiment Results" })).toBeVisible();
  });

  test("filter by project: selecting a project re-fetches slides @e2e", async ({
    page,
  }) => {
    const { calls } = await setupMockApi(page);
    await page.goto("/");

    // Wait for initial load
    await expect(page.getByText("3 slides")).toBeVisible();

    // Open project picker
    await page.getByRole("button", { name: /All Projects/i }).click();

    // Select org/alpha (scope to cmdk items to avoid matching slide thumbnails)
    await page.locator("[cmdk-item]", { hasText: "org/alpha" }).click();

    // Wait for filtered results — only 1 slide remains (SLIDE_A in org/alpha)
    await expect(page.getByText("1 slides")).toBeVisible();

    // Verify a project-filtered call was made
    const projectCalls = calls.filter(
      (c) => c.method === "GET" && c.url.includes("project=")
    );
    expect(projectCalls.length).toBeGreaterThan(0);
  });

  test("slide details: click slide shows detail panel with notes, figures, files @e2e", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 slides")).toBeVisible();

    // SLIDE_A is auto-selected (first in sort order: date DESC, day_order ASC)
    const previewFrame = page.locator('[data-testid="slide-viewer"]').frameLocator('iframe[title="Slide content"]');

    // Center panel: HTML content rendered
    await expect(previewFrame.getByRole("heading", { name: "Experiment Results" })).toBeVisible();
    await expect(previewFrame.getByText("23% improvement")).toBeVisible();

    // Detail panel: metadata (slide A is today)
    await expect(page.getByText("Today").last()).toBeVisible();
    await expect(page.getByText("org/alpha").last()).toBeVisible();
    // Git hash (first 7 chars)
    await expect(page.getByText("abc1234")).toBeVisible();

    // Notes tab (default) - markdown rendered
    await expect(page.getByText("Important")).toBeVisible();
    await expect(page.locator("strong", { hasText: "bold" })).toBeVisible();

    // Figures tab
    await page.getByRole("tab", { name: /Figures/i }).click();
    await expect(page.getByText("plot1.png")).toBeVisible();
    await expect(page.getByText("chart.svg")).toBeVisible();
    await expect(page.getByText("44.0 KB")).toBeVisible();
    await expect(page.getByText("12.0 KB")).toBeVisible();

    // Files tab
    await page.getByRole("tab", { name: /Files/i }).click();
    await expect(page.getByText("results.csv")).toBeVisible();
    await expect(page.getByText("2.0 KB")).toBeVisible();
    await expect(page.getByText("Experiment results")).toBeVisible();
  });

  test("notes/data-only records render fallback instead of an iframe @e2e", async ({
    page,
  }) => {
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 slides")).toBeVisible();

    await page.getByText("Notes/data").first().click();

    const viewer = page.getByTestId("slide-viewer");
    await expect(viewer.getByText("Notes/data-only record")).toBeVisible();
    await expect(viewer.getByText("Notes-first record with supporting data.")).toBeVisible();
    await expect(viewer.getByText("1 data file")).toBeVisible();
    await expect(
      page.locator('[data-testid="slide-viewer"] iframe[title="Slide content"]')
    ).toHaveCount(0);
  });

  test("edit slide: edit notes and save persists via PATCH @e2e", async ({
    page,
  }) => {
    const { calls } = await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 slides")).toBeVisible();

    // SLIDE_A is auto-selected (first in sort order)
    await expect(page.getByText("Important")).toBeVisible();

    // Click Edit button
    await page.getByRole("button", { name: /Edit/i }).click();

    // Textarea should appear with existing notes
    const textarea = page.locator("textarea");
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue("# Important\n\nThese are **bold** notes.");

    // Clear and type new notes
    await textarea.fill("Updated notes content");

    // Click Save
    await page.getByRole("button", { name: /Save/i }).click();

    // Verify PATCH call was made
    const patchCalls = calls.filter((c) => c.method === "PATCH-body");
    expect(patchCalls.length).toBeGreaterThan(0);
    expect(patchCalls[0].body).toEqual({ notes: "Updated notes content" });
  });

  test.skip("delete and restore: delete removes slide, show deleted reveals it, restore brings it back @e2e", async ({
    page,
  }) => {
    // TODO: unskip when Delete/Restore UI is implemented in SlideDetails
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 slides")).toBeVisible();

    // SLIDE_A is auto-selected (first in sort order)
    await expect(
      page.locator('[data-testid="slide-viewer"]')
        .frameLocator('iframe[title="Slide content"]')
        .getByRole("heading", { name: "Slide A" })
    ).toBeVisible();

    // Click delete button (in detail panel footer, not "Show deleted" toggle)
    await page.getByRole("button", { name: /^Delete$/i }).click();

    // Slide should be optimistically removed (2 slides remain)
    await expect(page.getByText("2 slides")).toBeVisible();
  });

  test.skip("show deleted: toggle shows deleted slides with restore button @e2e", async ({
    page,
  }) => {
    // TODO: unskip when "Show deleted" toggle UI is implemented
    await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 slides")).toBeVisible();

    // Toggle to show deleted
    await page.getByRole("button", { name: /Show deleted/i }).click();

    // Should fetch deleted slides
    await expect(page.getByText("1 slides")).toBeVisible();

    // Should show "Deleted" badge (the destructive badge in detail panel)
    await expect(page.locator("[data-variant='destructive']", { hasText: "Deleted" })).toBeVisible();
    // Restore button should be visible (not Delete)
    await expect(page.getByRole("button", { name: /Restore/i })).toBeVisible();
  });

  test("error states: API failure shows error banner @e2e", async ({
    page,
  }) => {
    // Mock slides API to fail
    await page.route("**/api/slides", async (route: Route) => {
      await route.fulfill({ status: 500 });
    });
    await page.route("**/api/slides?*", async (route: Route) => {
      await route.fulfill({ status: 500 });
    });
    await page.route("**/api/projects", async (route: Route) => {
      await route.fulfill({ status: 500 });
    });
    await page.route("**/api/sync/version", async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          version: 0,
          updated_at: dateIso(_now, "10:00:00Z"),
        }),
      });
    });
    await page.route("**/api/sync/changes*", async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          items: [],
          server_now: dateIso(_now, "10:00:00Z"),
        }),
      });
    });

    await page.goto("/");

    // Error banner should appear
    await expect(page.getByText(/Failed to fetch/)).toBeVisible();
  });

  test("empty database: shows placeholder when no slides @e2e", async ({
    page,
  }) => {
    await setupMockApi(page, {
      slides: { items: [], next_cursor: null },
      projects: { projects: [] },
    });
    await page.goto("/");

    // Should show 0 slides
    await expect(page.getByText("0 slides")).toBeVisible();

    // Empty state text visible
    await expect(page.getByText("Empty project")).toBeVisible();
  });

  test.skip("sync version display: version badge visible after interaction triggers sync @e2e", async ({
    page,
  }) => {
    // TODO: unskip when version badge UI is implemented
    await setupMockApi(page, {
      syncVersion: { version: 99, updated_at: dateIso(_now, "12:00:00Z") },
    });
    await page.goto("/");

    // Wait for slides to load
    await expect(page.getByText("3 slides")).toBeVisible();

    // Click anywhere on the page to trigger Layer 2 sync check
    await page.getByText("3 slides").click();

    // Version badge should show after sync check completes
    await expect(page.getByText("v99")).toBeVisible({ timeout: 5000 });
  });

  test("load more pagination: Load more button fetches next page @e2e", async ({
    page,
  }) => {
    await page.route(/\/api\/slides(?:\?.*)?$/, async (route: Route) => {
      if (route.request().method() !== "GET") {
        await route.continue();
        return;
      }

      const url = new URL(route.request().url());
      const cursor = url.searchParams.get("cursor");

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(
          cursor === "cursor1"
            ? {
                items: [SLIDE_C],
                next_cursor: null,
              }
            : {
                items: [SLIDE_A, SLIDE_B],
                next_cursor: "cursor1",
              }
        ),
      });
    });
    // Mock detail endpoint for auto-selected SLIDE_A
    await page.route(`**/api/slides/${TODAY_ID}-aabbccdd`, async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ slide: SLIDE_A_DETAIL }),
      });
    });
    await page.route("**/api/projects", async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ projects: PROJECTS }),
      });
    });
    await page.route("**/api/sync/version", async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          version: 0,
          updated_at: dateIso(_now, "10:00:00Z"),
        }),
      });
    });
    await page.route("**/api/sync/changes*", async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          items: [],
          server_now: dateIso(_now, "10:00:00Z"),
        }),
      });
    });

    await page.goto("/");

    // Initial 2 slides
    await expect(page.getByText("2 slides")).toBeVisible();

    // "Load more" button should be visible
    const loadMore = page.getByRole("button", { name: /Load more/i });
    await expect(loadMore).toBeVisible();

    await loadMore.click();

    // Now 3 slides should be visible, and "Yesterday" date header appears for SLIDE_C
    await expect(page.getByText("3 slides")).toBeVisible();
    await expect(page.getByText("Yesterday")).toBeVisible();

    // "Load more" should disappear (no more cursor)
    await expect(loadMore).not.toBeVisible();
  });
});
