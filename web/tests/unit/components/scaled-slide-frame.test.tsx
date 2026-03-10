// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import { ScaledSlideFrame } from "@/components/scaled-slide-frame";

class ResizeObserverMock {
  constructor(callback: ResizeObserverCallback) {
    void callback;
  }

  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

describe("ScaledSlideFrame", () => {
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    globalThis.ResizeObserver =
      ResizeObserverMock as unknown as typeof ResizeObserver;
  });

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver;
    vi.restoreAllMocks();
  });

  it("renders an iframe with the provided HTML content", () => {
    const { container } = render(
      <ScaledSlideFrame htmlContent="<p>Hello World</p>" />
    );

    const iframe = container.querySelector("iframe");
    expect(iframe).not.toBeNull();

    const srcDoc = iframe?.getAttribute("srcdoc") ?? "";
    expect(srcDoc).toContain("<p>Hello World</p>");
  });

  it("sets iframe dimensions to the base width and height", () => {
    const { container } = render(
      <ScaledSlideFrame htmlContent="<p>Test</p>" baseWidth={800} baseHeight={600} />
    );

    const iframe = container.querySelector("iframe");
    expect(iframe?.style.width).toBe("800px");
    expect(iframe?.style.height).toBe("600px");
  });

  it("uses default 1920x1080 dimensions when none specified", () => {
    const { container } = render(
      <ScaledSlideFrame htmlContent="<p>Default</p>" />
    );

    const iframe = container.querySelector("iframe");
    expect(iframe?.style.width).toBe("1920px");
    expect(iframe?.style.height).toBe("1080px");
  });

  it("sandboxes the iframe with allow-same-origin only", () => {
    const { container } = render(
      <ScaledSlideFrame htmlContent="<p>Sandboxed</p>" />
    );

    const iframe = container.querySelector("iframe");
    expect(iframe?.getAttribute("sandbox")).toBe("allow-same-origin");
  });

  it("preserves external URLs in the HTML content without modification", () => {
    const { container } = render(
      <ScaledSlideFrame
        htmlContent='<img src="https://example.com/image.png">'
      />
    );

    const srcDoc =
      container.querySelector("iframe")?.getAttribute("srcdoc") ?? "";
    expect(srcDoc).toContain('src="https://example.com/image.png"');
  });
});
