// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import RegisterForm from "@/app/register/register-form";

const { mockSignIn, mockPush, mockRefresh } = vi.hoisted(() => ({
  mockSignIn: vi.fn(),
  mockPush: vi.fn(),
  mockRefresh: vi.fn(),
}));

vi.mock("next-auth/react", () => ({
  signIn: mockSignIn,
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({
    push: mockPush,
    refresh: mockRefresh,
  }),
}));

describe("register form", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    cleanup();
    mockSignIn.mockReset();
    mockPush.mockReset();
    mockRefresh.mockReset();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it("surfaces a fallback error when /api/register returns non-JSON failure", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      new Response("server unavailable", {
        status: 500,
        headers: { "content-type": "text/plain" },
      }),
    );

    render(<RegisterForm />);

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: "very-secure-password" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain("Registration failed.");
    });
    expect(mockSignIn).not.toHaveBeenCalled();
    expect(mockPush).not.toHaveBeenCalled();
  });

  it("surfaces a structured API error message", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: "Registration is disabled." }), {
        status: 403,
        headers: { "content-type": "application/json" },
      }),
    );

    render(<RegisterForm />);

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe(
        "Registration is disabled.",
      );
    });
    expect(mockSignIn).not.toHaveBeenCalled();
  });

  it("falls back when API error JSON has no string error", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: 123 }), {
        status: 400,
        headers: { "content-type": "application/json" },
      }),
    );

    render(<RegisterForm />);

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe("Registration failed.");
    });
    expect(mockSignIn).not.toHaveBeenCalled();
  });

  it("validates password length before calling the API", async () => {
    globalThis.fetch = vi.fn();

    render(<RegisterForm />);

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "short" },
    });
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: "short" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    expect(screen.getByRole("alert").textContent).toContain("8 characters");
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("validates matching passwords before calling the API", async () => {
    globalThis.fetch = vi.fn();

    render(<RegisterForm />);

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: "different-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    expect(screen.getByRole("alert").textContent).toBe("Passwords do not match.");
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("registers, signs in, and navigates home", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ id: "user-1" }), {
        status: 201,
        headers: { "content-type": "application/json" },
      }),
    );
    mockSignIn.mockResolvedValueOnce({ ok: true });

    render(<RegisterForm />);

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/");
    });
    expect(mockRefresh).toHaveBeenCalledOnce();
    expect(globalThis.fetch).toHaveBeenCalledWith("/api/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        email: "user@example.com",
        name: null,
        password: "very-secure-password",
      }),
    });
  });

  it("surfaces sign-in failure after account creation", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ id: "user-1" }), {
        status: 201,
        headers: { "content-type": "application/json" },
      }),
    );
    mockSignIn.mockResolvedValueOnce({ error: "CredentialsSignin" });

    render(<RegisterForm />);

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain(
        "Account created but sign-in failed",
      );
    });
    expect(mockPush).not.toHaveBeenCalled();
  });

  it("surfaces network failures", async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new Error("network down"));

    render(<RegisterForm />);

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.change(screen.getByLabelText("Confirm password"), {
      target: { value: "very-secure-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Register" }));

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe("Registration failed.");
    });
    expect(mockSignIn).not.toHaveBeenCalled();
  });
});
