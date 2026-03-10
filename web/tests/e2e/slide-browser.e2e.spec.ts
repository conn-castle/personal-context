import { expect, test, type Page, type Route } from "@playwright/test";

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const SLIDE_A = {
  id: "20260309-aabbccdd",
  date: "2026-03-09",
  day_order: "a0",
  project_id: "org/alpha",
  updated_at: "2026-03-09T10:00:00Z",
  deleted_at: null,
  figure_count: 2,
  data_file_count: 1,
};

const SLIDE_B = {
  id: "20260309-11223344",
  date: "2026-03-09",
  day_order: "a1",
  project_id: null,
  updated_at: "2026-03-09T09:00:00Z",
  deleted_at: null,
  figure_count: 0,
  data_file_count: 0,
};

const SLIDE_C = {
  id: "20260308-55667788",
  date: "2026-03-08",
  day_order: "a0",
  project_id: "org/beta",
  updated_at: "2026-03-08T12:00:00Z",
  deleted_at: null,
  figure_count: 1,
  data_file_count: 0,
};

const SLIDE_A_DETAIL = {
  id: "20260309-aabbccdd",
  date: "2026-03-09",
  day_order: "a0",
  html_content: "<h1>Slide A</h1><p>Some content here</p>",
  notes: "# Important\n\nThese are **bold** notes.",
  project_id: "org/alpha",
  git_remote_url: "https://github.com/org/repo",
  git_hash: "abc1234def5678",
  created_at: "2026-03-09T08:00:00Z",
  updated_at: "2026-03-09T10:00:00Z",
  deleted_at: null,
  figures: [
    { filename: "plot1.png", s3_key: "slides/20260309-aabbccdd/figures/plot1.png", size: 45056, alt_text: "Plot 1" },
    { filename: "chart.svg", s3_key: "slides/20260309-aabbccdd/figures/chart.svg", size: 12288 },
  ],
  data_files: [
    { filename: "results.csv", s3_key: "slides/20260309-aabbccdd/data/results.csv", size: 2048, description: "Experiment results" },
  ],
};

const DELETED_SLIDE = {
  id: "20260307-deadbeef",
  date: "2026-03-07",
  day_order: "a0",
  project_id: null,
  updated_at: "2026-03-07T12:00:00Z",
  deleted_at: "2026-03-07T14:00:00Z",
  figure_count: 0,
  data_file_count: 0,
};

const DELETED_SLIDE_DETAIL = {
  id: "20260307-deadbeef",
  date: "2026-03-07",
  day_order: "a0",
  html_content: "<p>Deleted slide content</p>",
  notes: null,
  project_id: null,
  git_remote_url: null,
  git_hash: null,
  created_at: "2026-03-07T08:00:00Z",
  updated_at: "2026-03-07T12:00:00Z",
  deleted_at: "2026-03-07T14:00:00Z",
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
  await page.route("**/api/slides/20260309-aabbccdd", async (route: Route) => {
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
          id: "20260309-aabbccdd",
          deleted_at: "2026-03-09T12:00:00Z",
          updated_at: "2026-03-09T12:00:00Z",
          sync_version: 3,
        }),
      });
    }
  });

  // Mock /api/slides/[id] for deleted slide
  await page.route("**/api/slides/20260307-deadbeef", async (route: Route) => {
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
  await page.route("**/api/slides/20260307-deadbeef/restore", async (route: Route) => {
    calls.push({ method: "POST", url: route.request().url() });
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        id: "20260307-deadbeef",
        deleted_at: null,
        updated_at: "2026-03-09T12:00:00Z",
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
          updated_at: "2026-03-09T10:00:00Z",
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
        server_now: "2026-03-09T10:00:00Z",
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

    // Date headers visible (formatRelativeDate output depends on current date)
    await expect(page.getByText(/Today|Yesterday|Mar 9/)).toBeVisible();
    await expect(page.getByText(/Today|Yesterday|Mar 8/)).toBeVisible();

    // Slide buttons visible (last 8 chars of IDs)
    await expect(page.getByText("aabbccdd")).toBeVisible();
    await expect(page.getByText("11223344")).toBeVisible();
    await expect(page.getByText("55667788")).toBeVisible();

    // Slide count badge
    await expect(page.getByText("3 slides")).toBeVisible();

    // Placeholder text when no slide selected
    await expect(page.getByText("Select a slide", { exact: true })).toBeVisible();
    await expect(page.getByText("Choose a slide from the navigation panel")).toBeVisible();
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

    // Wait for filtered results
    await expect(page.getByText("1 slides")).toBeVisible();

    // Verify only the alpha project slide is shown
    await expect(page.getByText("aabbccdd")).toBeVisible();
    await expect(page.getByText("11223344")).not.toBeVisible();
    await expect(page.getByText("55667788")).not.toBeVisible();

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

    // Click slide A
    await page.getByText("aabbccdd").click();
    const previewFrame = page.frameLocator('iframe[title="Slide content"]');

    // Center panel: HTML content rendered
    await expect(previewFrame.getByRole("heading", { name: "Slide A" })).toBeVisible();
    await expect(previewFrame.getByText("Some content here")).toBeVisible();

    // Detail panel: metadata (date format depends on current date)
    await expect(page.getByText(/Today|Yesterday|Mar 9/).last()).toBeVisible();
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

  test("edit slide: edit notes and save persists via PATCH @e2e", async ({
    page,
  }) => {
    const { calls } = await setupMockApi(page);
    await page.goto("/");
    await expect(page.getByText("3 slides")).toBeVisible();

    // Select slide A
    await page.getByText("aabbccdd").click();
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

    // Select slide A and delete it
    await page.getByText("aabbccdd").click();
    await expect(
      page
        .frameLocator('iframe[title="Slide content"]')
        .getByRole("heading", { name: "Slide A" })
    ).toBeVisible();

    // Click delete button (in detail panel footer, not "Show deleted" toggle)
    await page.getByRole("button", { name: /^Delete$/i }).click();

    // Slide should be optimistically removed (2 slides remain)
    await expect(page.getByText("2 slides")).toBeVisible();
    await expect(page.getByText("aabbccdd")).not.toBeVisible();
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
    await expect(page.getByText("deadbeef")).toBeVisible();

    // Select the deleted slide
    await page.getByText("deadbeef").click();

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

    // Placeholder text visible
    await expect(page.getByText("Select a slide", { exact: true })).toBeVisible();
  });

  test.skip("sync version display: version badge visible after interaction triggers sync @e2e", async ({
    page,
  }) => {
    // TODO: unskip when version badge UI is implemented
    await setupMockApi(page, {
      syncVersion: { version: 99, updated_at: "2026-03-09T12:00:00Z" },
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

    await page.goto("/");

    // Initial 2 slides
    await expect(page.getByText("2 slides")).toBeVisible();

    // "Load more" button should be visible
    const loadMore = page.getByRole("button", { name: /Load more/i });
    await expect(loadMore).toBeVisible();

    await loadMore.click();

    // Now 3 slides should be visible
    await expect(page.getByText("3 slides")).toBeVisible();
    await expect(page.getByText("55667788")).toBeVisible();

    // "Load more" should disappear (no more cursor)
    await expect(loadMore).not.toBeVisible();
  });
});
