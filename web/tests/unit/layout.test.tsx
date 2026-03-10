import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

// Mock next/font/google since font functions aren't available in vitest
vi.mock("next/font/google", () => ({
  Geist: () => ({ className: "geist-mock" }),
  Geist_Mono: () => ({ className: "geist-mono-mock" }),
}));

// Mock TooltipProvider since it requires client-side context
vi.mock("@/components/ui/tooltip", () => ({
  TooltipProvider: ({ children }: { children: React.ReactNode }) =>
    createElement("div", { "data-testid": "tooltip-provider" }, children),
}));

// Mock ThemeProvider since next-themes isn't available in SSR test
vi.mock("@/components/theme-provider", () => ({
  ThemeProvider: ({ children }: { children: React.ReactNode }) =>
    createElement("div", { "data-testid": "theme-provider" }, children),
}));

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
    expect(markup).toContain('lang="en"');
    expect(markup).toContain("<body");
    expect(markup).toContain("<p>child</p>");
  });

  it("wraps children with ThemeProvider and TooltipProvider", () => {
    const markup = renderToStaticMarkup(
      createElement(RootLayout, null, createElement("p", null, "child"))
    );

    expect(markup).toContain("theme-provider");
    expect(markup).toContain("tooltip-provider");
  });
});
