import { describe, expect, it, vi } from "vitest";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

vi.mock("@/components/spreadsheet-viewer", () => ({
  SpreadsheetViewer: () =>
    createElement("div", { "data-testid": "spreadsheet-viewer" }),
}));

import HomePage from "@/app/page";

describe("home page", () => {
  it("renders the SpreadsheetViewer component", () => {
    const markup = renderToStaticMarkup(createElement(HomePage));
    expect(markup).toContain('data-testid="spreadsheet-viewer"');
  });
});
