import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import RootLayout, { metadata } from "../../app/layout";

describe("root layout", () => {
  it("exports metadata with title and description", () => {
    expect(metadata.title).toBe("Personal Context");
    expect(metadata.description).toBe("Personal Context web workspace");
  });

  it("renders children inside html/body shell", () => {
    const markup = renderToStaticMarkup(
      createElement(RootLayout, null, createElement("p", null, "child"))
    );

    expect(markup).toContain("<html");
    expect(markup).toContain("lang=\"en\"");
    expect(markup).toContain("<body>");
    expect(markup).toContain("<p>child</p>");
  });
});
