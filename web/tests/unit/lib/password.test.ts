import { describe, expect, it } from "vitest";
import { hashPassword, verifyPassword } from "@/lib/password";

describe("password helpers", () => {
  it("hashes and verifies a password correctly", async () => {
    const password = "secure-password-123";
    const hash = await hashPassword(password);

    expect(hash).not.toBe(password);
    expect(hash).toMatch(/^\$2[aby]?\$/);

    const valid = await verifyPassword(password, hash);
    expect(valid).toBe(true);
  });

  it("rejects an incorrect password", async () => {
    const hash = await hashPassword("correct-password");

    const valid = await verifyPassword("wrong-password", hash);
    expect(valid).toBe(false);
  });

  it("produces different hashes for the same password", async () => {
    const password = "same-password";
    const hash1 = await hashPassword(password);
    const hash2 = await hashPassword(password);

    expect(hash1).not.toBe(hash2);
    // Both should still verify
    expect(await verifyPassword(password, hash1)).toBe(true);
    expect(await verifyPassword(password, hash2)).toBe(true);
  });
});
