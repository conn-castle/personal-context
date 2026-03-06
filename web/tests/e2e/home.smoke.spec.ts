import { expect, test } from "@playwright/test";

test("renders the scaffolded home page @smoke", async ({ page }) => {
  await page.goto("/");

  await expect(
    page.getByRole("heading", { name: "Personal Context Web" })
  ).toBeVisible();
});
