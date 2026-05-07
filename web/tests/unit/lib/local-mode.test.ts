import { afterEach, describe, expect, it } from "vitest";
import {
  getLocalBackendURL,
  getLocalModeState,
  isLocalModeEnabled,
} from "@/lib/local-mode";

const originalLocalBackendURL = process.env.LOCAL_BACKEND_URL;

describe("local-mode", () => {
  afterEach(() => {
    if (originalLocalBackendURL === undefined) {
      delete process.env.LOCAL_BACKEND_URL;
      return;
    }
    process.env.LOCAL_BACKEND_URL = originalLocalBackendURL;
  });

  it("returns null when local backend URL is unset or blank", () => {
    delete process.env.LOCAL_BACKEND_URL;
    expect(getLocalBackendURL()).toBeNull();
    expect(isLocalModeEnabled()).toBe(false);

    process.env.LOCAL_BACKEND_URL = "   ";
    expect(getLocalBackendURL()).toBeNull();
    expect(isLocalModeEnabled()).toBe(false);
  });

  it("accepts IPv4 and IPv6 loopback URLs", () => {
    process.env.LOCAL_BACKEND_URL = "http://127.42.0.1:9876";
    expect(getLocalBackendURL()?.href).toBe("http://127.42.0.1:9876/");

    process.env.LOCAL_BACKEND_URL = "http://[::1]:9876";
    expect(getLocalBackendURL()?.href).toBe("http://[::1]:9876/");
  });

  it("rejects malformed URLs, unsupported protocols, and non-loopback hosts", () => {
    process.env.LOCAL_BACKEND_URL = "not a url";
    expect(() => getLocalBackendURL()).toThrow("valid absolute URL");

    process.env.LOCAL_BACKEND_URL = "ftp://127.0.0.1";
    expect(() => getLocalBackendURL()).toThrow("http:// or https://");

    process.env.LOCAL_BACKEND_URL = "https://example.com";
    expect(() => getLocalBackendURL()).toThrow("loopback host");
  });

  it("rejects hostnames that only prefix-match IPv4 loopback", () => {
    process.env.LOCAL_BACKEND_URL = "http://127.0.0.1foo:9876";
    expect(() => getLocalBackendURL()).toThrow("loopback host");

    process.env.LOCAL_BACKEND_URL = "http://127.0.0.999:9876";
    expect(() => getLocalBackendURL()).toThrow("valid absolute URL");
  });

  it("reports invalid local-mode config without throwing", () => {
    process.env.LOCAL_BACKEND_URL = "https://example.com";
    expect(getLocalModeState()).toEqual({ enabled: false, hasConfigError: true });
  });
});
