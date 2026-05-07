/**
 * Canonicalizes a user-supplied email identity for account lookups and writes.
 *
 * Args: email is raw user input from forms/API payloads.
 * Returns: trimmed, lowercased email identity.
 */
export function canonicalizeEmailIdentity(email: string): string {
  return email.trim().toLowerCase();
}
