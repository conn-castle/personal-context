// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup } from "@testing-library/react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import LoginForm from "@/app/login/login-form";

const { mockPush, mockRefresh, mockSignIn } = vi.hoisted(() => ({
  mockPush: vi.fn(),
  mockRefresh: vi.fn(),
  mockSignIn: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({
    push: mockPush,
    refresh: mockRefresh,
  }),
}));

vi.mock("next-auth/react", () => ({
  signIn: mockSignIn,
}));

describe("login form", () => {
  beforeEach(() => {
    cleanup();
    mockPush.mockReset();
    mockRefresh.mockReset();
    mockSignIn.mockReset();
  });

  it("submits credentials and navigates home on success", async () => {
    mockSignIn.mockResolvedValueOnce({ ok: true });
    render(<LoginForm />);

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "secret-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/");
    });
    expect(mockRefresh).toHaveBeenCalledOnce();
    expect(mockSignIn).toHaveBeenCalledWith("credentials", {
      email: "user@example.com",
      password: "secret-password",
      redirect: false,
    });
  });

  it("shows an error when credentials are rejected", async () => {
    mockSignIn.mockResolvedValueOnce({ error: "CredentialsSignin" });
    render(<LoginForm />);

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "wrong-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe(
        "Invalid email or password.",
      );
    });
    expect(mockPush).not.toHaveBeenCalled();
  });

  it("recovers when sign-in throws", async () => {
    mockSignIn.mockRejectedValueOnce(new Error("network unavailable"));
    render(<LoginForm />);

    fireEvent.change(screen.getByLabelText("Email"), {
      target: { value: "user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "secret-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe(
        "Unable to sign in. Please try again.",
      );
    });
    expect(
      (screen.getByRole("button", { name: "Sign in" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    expect(mockPush).not.toHaveBeenCalled();
  });
});
