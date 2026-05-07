import { afterEach, describe, expect, it, vi } from "vitest";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

vi.mock("@/app/register/register-form", () => ({
  __esModule: true,
  default: () => createElement("div", { "data-testid": "register-form" }),
}));

import RegisterPage from "@/app/register/page";

const originalRegistrationEnabled = process.env.REGISTRATION_ENABLED;
const originalLocalBackendURL = process.env.LOCAL_BACKEND_URL;

describe("register page", () => {
  afterEach(() => {
    if (originalRegistrationEnabled === undefined) {
      delete process.env.REGISTRATION_ENABLED;
    } else {
      process.env.REGISTRATION_ENABLED = originalRegistrationEnabled;
    }

    if (originalLocalBackendURL === undefined) {
      delete process.env.LOCAL_BACKEND_URL;
      return;
    }
    process.env.LOCAL_BACKEND_URL = originalLocalBackendURL;
  });

  it("renders disabled state when registration is turned off", () => {
    process.env.REGISTRATION_ENABLED = "false";

    const markup = renderToStaticMarkup(createElement(RegisterPage));

    expect(markup).toContain("Registration is disabled");
    expect(markup).not.toContain('data-testid="register-form"');
  });

  it("renders disabled state when registration is unset", () => {
    delete process.env.REGISTRATION_ENABLED;

    const markup = renderToStaticMarkup(createElement(RegisterPage));

    expect(markup).toContain("Registration is disabled");
    expect(markup).not.toContain('data-testid="register-form"');
  });

  it("renders local-mode state when LOCAL_BACKEND_URL is set", () => {
    process.env.REGISTRATION_ENABLED = "true";
    process.env.LOCAL_BACKEND_URL = "http://127.0.0.1:9876";

    const markup = renderToStaticMarkup(createElement(RegisterPage));

    expect(markup).toContain("Registration is unavailable in local mode");
    expect(markup).not.toContain('data-testid="register-form"');
  });

  it("renders invalid-config state for malformed LOCAL_BACKEND_URL", () => {
    process.env.REGISTRATION_ENABLED = "true";
    process.env.LOCAL_BACKEND_URL = "https://example.com";

    const markup = renderToStaticMarkup(createElement(RegisterPage));

    expect(markup).toContain("Invalid local backend configuration");
    expect(markup).not.toContain('data-testid="register-form"');
  });

  it("renders the active registration form when enabled", () => {
    process.env.REGISTRATION_ENABLED = "true";

    const markup = renderToStaticMarkup(createElement(RegisterPage));

    expect(markup).toContain('data-testid="register-form"');
  });
});
