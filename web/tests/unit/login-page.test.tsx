import { afterEach, describe, expect, it, vi } from "vitest";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

vi.mock("@/app/login/login-form", () => ({
  __esModule: true,
  default: () => createElement("div", { "data-testid": "login-form" }),
}));

import LoginPage from "@/app/login/page";

const originalLocalBackendURL = process.env.LOCAL_BACKEND_URL;

describe("login page", () => {
  afterEach(() => {
    if (originalLocalBackendURL === undefined) {
      delete process.env.LOCAL_BACKEND_URL;
      return;
    }
    process.env.LOCAL_BACKEND_URL = originalLocalBackendURL;
  });

  it("renders local-mode state when LOCAL_BACKEND_URL is set", () => {
    process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";

    const markup = renderToStaticMarkup(createElement(LoginPage));

    expect(markup).toContain("Sign in is unavailable in local mode");
    expect(markup).not.toContain('data-testid="login-form"');
  });

  it("renders invalid-config state for malformed LOCAL_BACKEND_URL", () => {
    process.env.LOCAL_BACKEND_URL = "https://example.com";

    const markup = renderToStaticMarkup(createElement(LoginPage));

    expect(markup).toContain("Invalid local backend configuration");
    expect(markup).not.toContain('data-testid="login-form"');
  });

  it("renders sign-in form when local mode is disabled", () => {
    delete process.env.LOCAL_BACKEND_URL;

    const markup = renderToStaticMarkup(createElement(LoginPage));

    expect(markup).toContain('data-testid="login-form"');
  });
});
