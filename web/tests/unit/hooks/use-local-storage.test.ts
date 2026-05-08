// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement } from "react";
import { renderToString } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useLocalStorage } from "@/hooks/use-local-storage";

function createStorageMock(): Storage {
  const store = new Map<string, string>();

  return {
    get length() {
      return store.size;
    },
    clear() {
      store.clear();
    },
    getItem(key: string) {
      return store.get(key) ?? null;
    },
    key(index: number) {
      return Array.from(store.keys())[index] ?? null;
    },
    removeItem(key: string) {
      store.delete(key);
    },
    setItem(key: string, value: string) {
      store.set(key, value);
    },
  };
}

describe("useLocalStorage", () => {
  beforeEach(() => {
    const storage = createStorageMock();
    Object.defineProperty(window, "localStorage", {
      value: storage,
      configurable: true,
    });
    Object.defineProperty(globalThis, "localStorage", {
      value: storage,
      configurable: true,
    });
  });

  it("uses the default value during server render", () => {
    const originalWindow = globalThis.window;

    Reflect.deleteProperty(globalThis, "window");

    function TestComponent() {
      const [value] = useLocalStorage("testKey", "default");
      return createElement("div", null, value);
    }

    try {
      expect(renderToString(createElement(TestComponent))).toContain("default");
    } finally {
      Object.defineProperty(globalThis, "window", {
        value: originalWindow,
        configurable: true,
      });
    }
  });

  it("marks storage as loaded when nothing is stored", async () => {
    const { result } = renderHook(() =>
      useLocalStorage("testKey", "default")
    );

    await waitFor(() => {
      expect(result.current[0]).toBe("default");
      expect(result.current[2]).toBe(true);
    });
  });

  it("hydrates an existing value from localStorage after mount", async () => {
    window.localStorage.setItem("pc:testKey", JSON.stringify("stored"));

    const { result } = renderHook(() =>
      useLocalStorage("testKey", "default")
    );

    await waitFor(() => {
      expect(result.current[0]).toBe("stored");
      expect(result.current[2]).toBe(true);
    });
  });

  it("writes to localStorage when setter is called", async () => {
    const { result } = renderHook(() =>
      useLocalStorage("testKey", "default")
    );

    await waitFor(() => {
      expect(result.current[2]).toBe(true);
    });

    act(() => {
      result.current[1]("updated");
    });

    expect(result.current[0]).toBe("updated");
    expect(window.localStorage.getItem("pc:testKey")).toBe(
      JSON.stringify("updated")
    );
  });

  it("supports functional updates", async () => {
    const { result } = renderHook(() =>
      useLocalStorage<number>("counter", 0)
    );

    await waitFor(() => {
      expect(result.current[2]).toBe(true);
    });

    act(() => {
      result.current[1]((prev) => prev + 1);
    });

    expect(result.current[0]).toBe(1);
    expect(window.localStorage.getItem("pc:counter")).toBe("1");
  });

  it("falls back to default on corrupt JSON", async () => {
    window.localStorage.setItem("pc:testKey", "not{valid}json");

    const { result } = renderHook(() =>
      useLocalStorage("testKey", "fallback")
    );

    await waitFor(() => {
      expect(result.current[0]).toBe("fallback");
      expect(result.current[2]).toBe(true);
    });
  });

  it("handles complex objects", async () => {
    const defaultObj = { navigation: true, details: true, metadata: true };
    const { result } = renderHook(() =>
      useLocalStorage("panels", defaultObj)
    );

    await waitFor(() => {
      expect(result.current[2]).toBe(true);
    });

    act(() => {
      result.current[1]({ navigation: false, details: true, metadata: true });
    });

    expect(result.current[0]).toEqual({
      navigation: false,
      details: true,
      metadata: true,
    });
    expect(JSON.parse(window.localStorage.getItem("pc:panels")!)).toEqual({
      navigation: false,
      details: true,
      metadata: true,
    });
  });

  it("handles arrays", async () => {
    const { result } = renderHook(() =>
      useLocalStorage<string[]>("projects", [])
    );

    await waitFor(() => {
      expect(result.current[2]).toBe(true);
    });

    act(() => {
      result.current[1](["proj-a", "proj-b"]);
    });

    expect(result.current[0]).toEqual(["proj-a", "proj-b"]);
  });

  it("handles null values", async () => {
    const { result } = renderHook(() =>
      useLocalStorage<string | null>("recordId", null)
    );

    await waitFor(() => {
      expect(result.current[2]).toBe(true);
    });

    act(() => {
      result.current[1]("record-123");
    });
    expect(result.current[0]).toBe("record-123");

    act(() => {
      result.current[1](null);
    });
    expect(result.current[0]).toBeNull();
  });

  it("prefixes keys with pc:", async () => {
    const { result } = renderHook(() =>
      useLocalStorage("viewMode", "strip")
    );

    await waitFor(() => {
      expect(result.current[2]).toBe(true);
    });

    act(() => {
      result.current[1]("grid");
    });

    expect(window.localStorage.getItem("pc:viewMode")).toBe(
      JSON.stringify("grid")
    );
    expect(window.localStorage.getItem("viewMode")).toBeNull();
  });

  it("resets to default when key changes to one with no stored value", async () => {
    window.localStorage.setItem("pc:keyA", JSON.stringify("valueA"));

    let currentKey = "keyA";
    const { result, rerender } = renderHook(() =>
      useLocalStorage(currentKey, "default")
    );

    await waitFor(() => {
      expect(result.current[0]).toBe("valueA");
      expect(result.current[2]).toBe(true);
    });

    // Switch to a key that has no stored value
    currentKey = "keyB";
    rerender();

    await waitFor(() => {
      expect(result.current[0]).toBe("default");
    });
  });

  it("survives localStorage.setItem error", async () => {
    const setItemSpy = vi
      .spyOn(window.localStorage, "setItem")
      .mockImplementation(() => {
        throw new Error("QuotaExceededError");
      });

    const { result } = renderHook(() =>
      useLocalStorage("testKey", "default")
    );

    await waitFor(() => {
      expect(result.current[2]).toBe(true);
    });

    act(() => {
      result.current[1]("updated");
    });

    expect(result.current[0]).toBe("updated");

    setItemSpy.mockRestore();
  });
});
