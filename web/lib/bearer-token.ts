/**
 * Extracts a bearer token from Authorization header value.
 *
 * Accepts scheme matching case-insensitively and tolerates surrounding
 * whitespace. Returns null when no valid bearer token is present.
 *
 * @param authorizationHeader - Raw Authorization header value.
 * @returns Bearer token or null when header is absent/invalid.
 */
export function extractBearerToken(authorizationHeader: string | null): string | null {
  if (authorizationHeader === null) {
    return null;
  }

  const trimmed = authorizationHeader.trim();
  if (trimmed.length === 0) {
    return null;
  }

  const match = /^bearer\s+(.+)$/i.exec(trimmed);
  if (!match) {
    return null;
  }

  const token = match[1].trim();
  return token.length > 0 ? token : null;
}
