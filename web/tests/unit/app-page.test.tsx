import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import HomePage from "../../app/page";

describe("home page", () => {
  it("renders scaffold copy for Phase 1 smoke checks", () => {
    const markup = renderToStaticMarkup(createElement(HomePage));

    expect(markup).toContain("Personal Context Web");
    expect(markup).toContain("Phase 1 scaffold is active");
  });
});
