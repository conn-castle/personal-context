import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { rmSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

test("renders script-generated standalone record HTML with bounds frame @cli-record", async ({
  page
}) => {
  const repoRoot = path.resolve(process.cwd(), "..");
  const scriptPath = path.join(repoRoot, "cli", "scripts", "verify_phase3_manual.sh");
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

    const previewMatch = output.match(/^Preview file: (.+)$/m);
    if (!previewMatch?.[1]) {
      throw new Error(`could not parse preview file from script output:\n${output}`);
    }

    const previewPath = previewMatch[1].trim();
    await page.goto(pathToFileURL(previewPath).toString());

    const boundary = page.locator("#record-boundary");
    await expect(boundary).toBeVisible();

    const borderTopWidth = await boundary.evaluate((element) => {
      return getComputedStyle(element).borderTopWidth;
    });
    expect(borderTopWidth).toBe("3px");

    const frame = page.frameLocator("#record-frame");
    await expect(frame.getByRole("heading", { name: "Record A edited" })).toBeVisible();

    const image = frame.getByRole("img", { name: "Plot 2" });
    await expect(image).toBeVisible();

    const naturalWidth = await image.evaluate((element) => {
      return (element as HTMLImageElement).naturalWidth;
    });
    expect(naturalWidth).toBeGreaterThan(0);
  } finally {
    if (artifactsRoot.length > 0) {
      rmSync(artifactsRoot, { force: true, recursive: true });
    }
  }
});
