// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";

const mermaidMock = vi.hoisted(() => ({
  initialize: vi.fn(),
  render: vi.fn().mockResolvedValue({ svg: "<svg></svg>" }),
}));

vi.mock("mermaid", () => ({
  default: mermaidMock,
}));

async function importMarkdownRenderer() {
  return import("@/components/markdown-renderer");
}

describe("MarkdownRenderer", () => {
  const originalClipboard = Object.getOwnPropertyDescriptor(
    window.navigator,
    "clipboard"
  );

  beforeEach(() => {
    vi.resetModules();
    mermaidMock.initialize.mockClear();
    mermaidMock.render.mockClear();
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.useRealTimers();
    if (originalClipboard) {
      Object.defineProperty(window.navigator, "clipboard", originalClipboard);
      return;
    }
    Reflect.deleteProperty(window.navigator, "clipboard");
  });

  it("initializes mermaid with strict security", async () => {
    await importMarkdownRenderer();

    expect(mermaidMock.initialize).toHaveBeenCalledWith(
      expect.objectContaining({ securityLevel: "strict" })
    );
  });

  it("does not throw when clipboard access is unavailable", async () => {
    const { MarkdownRenderer } = await importMarkdownRenderer();
    Object.defineProperty(window.navigator, "clipboard", {
      configurable: true,
      value: undefined,
    });

    render(<MarkdownRenderer content={"```js\nconst answer = 42;\n```"} />);

    const copyButton = screen.getByRole("button", { name: "Copy code" });
    expect(() => fireEvent.click(copyButton)).not.toThrow();
    expect(copyButton.textContent).toBe("Copy");
  });

  it("copies code and shows temporary feedback when clipboard access succeeds", async () => {
    const { MarkdownRenderer } = await importMarkdownRenderer();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(window.navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    render(<MarkdownRenderer content={"```ts\nconst total = 3;\n```"} />);

    const copyButton = screen.getByRole("button", { name: "Copy code" });
    fireEvent.click(copyButton);

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("const total = 3;");
    });
    await waitFor(() => {
      expect(copyButton.textContent).toBe("Copied!");
    });
  });
});
