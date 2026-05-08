import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { rmSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

test("renders the script-generated local demo summary and persisted record previews @cli-demo", async ({
  page
}) => {
  const repoRoot = path.resolve(process.cwd(), "..");
  const scriptPath = path.join(repoRoot, "cli", "scripts", "verify_local_demo.sh");
  const cliDir = path.join(repoRoot, "cli");

  let artifactsRoot = "";

  try {
    const output = execFileSync("bash", [scriptPath, "--no-open"], {
      cwd: cliDir,
      encoding: "utf8",
      env: {
        ...process.env,
        LC_ALL: "en_US.UTF-8"
      }
    });

    const artifactsMatch = output.match(/^Artifacts root: (.+)$/m);
    if (!artifactsMatch?.[1]) {
      throw new Error(`could not parse artifacts root from script output:\n${output}`);
    }
    artifactsRoot = artifactsMatch[1].trim();

    const summaryMatch = output.match(/^Summary file: (.+)$/m);
    if (!summaryMatch?.[1]) {
      throw new Error(`could not parse summary file from script output:\n${output}`);
    }

    const summaryPath = summaryMatch[1].trim();
    await page.goto(pathToFileURL(summaryPath).toString());

    await expect(page.getByRole("heading", { name: "Local Demo Summary" })).toBeVisible();
    await expect(page.getByText("create 10 records, delete records 06-10, restore Record 08, move Record 04 after Record 02.")).toBeVisible();

    const activeRows = page.locator("#active-order tbody tr");
    await expect(activeRows).toHaveCount(6);
    const activeTitles = await activeRows.evaluateAll((rows) =>
      rows.map((row) => row.querySelectorAll("td")[1]?.textContent?.trim() ?? "")
    );
    expect(activeTitles).toEqual([
      "Record 01",
      "Record 02",
      "Record 04",
      "Record 03",
      "Record 05",
      "Record 08"
    ]);

    const deletedRows = page.locator("#trash-list tbody tr");
    await expect(deletedRows).toHaveCount(4);
    const deletedTitles = await deletedRows.evaluateAll((rows) =>
      rows.map((row) => row.querySelectorAll("td")[1]?.textContent?.trim() ?? "")
    );
    expect(deletedTitles).toEqual([
      "Record 06",
      "Record 07",
      "Record 09",
      "Record 10"
    ]);

    const firstFrame = page.frameLocator("#first-record-frame");
    await expect(firstFrame.getByRole("heading", { name: "Record 01" })).toBeVisible();
    await expect(firstFrame.getByText("deleted records 06-10")).toBeVisible();

    const lastFrame = page.frameLocator("#last-record-frame");
    await expect(lastFrame.getByRole("heading", { name: "Record 08" })).toBeVisible();
    await expect(
      lastFrame.getByText("Expected final active order: 01, 02, 04, 03, 05, 08.")
    ).toBeVisible();
  } finally {
    if (artifactsRoot.length > 0) {
      rmSync(artifactsRoot, { force: true, recursive: true });
    }
  }
});
