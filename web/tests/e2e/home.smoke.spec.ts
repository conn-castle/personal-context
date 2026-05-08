import { expect, test, type Route } from "@playwright/test";

test("renders the home page with Personal Context heading @smoke", async ({
  page,
}) => {
  // Mock all API routes so the page loads without a real backend
  await page.route("**/api/records", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ items: [], next_cursor: null }),
    });
  });
  await page.route("**/api/records?*", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ items: [], next_cursor: null }),
    });
  });
  await page.route("**/api/projects", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ projects: [] }),
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

  await expect(
    page.getByRole("heading", { name: "Personal Context" })
  ).toBeVisible();
});
