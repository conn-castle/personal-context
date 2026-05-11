const LOOPBACK_HOSTS = new Set(["localhost", "::1", "[::1]"]);

function isIPv4Loopback(hostname: string): boolean {
  const parts = hostname.split(".");
  if (parts.length !== 4) {
    return false;
  }

  if (parts.some((part) => !/^\d+$/.test(part))) {
    return false;
  }

  const octets = parts.map((part) => Number.parseInt(part, 10));
  if (octets.some((octet) => Number.isNaN(octet) || octet < 0 || octet > 255)) {
    return false;
  }

  return octets[0] === 127;
}

/**
 * Returns the validated LOCAL_BACKEND_URL when local mode is enabled.
 *
 * Requires a loopback host to prevent accidentally proxying local credentials
 * to arbitrary remote endpoints.
 *
 * @returns Parsed backend URL, or null when local mode is disabled.
 * @throws If LOCAL_BACKEND_URL is malformed or not loopback-hosted.
 */
export function getLocalBackendURL(): URL | null {
  const raw = process.env.LOCAL_BACKEND_URL;
  if (raw === undefined) {
    return null;
  }

  const value = raw.trim();
  if (value.length === 0) {
    return null;
  }

  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error("LOCAL_BACKEND_URL must be a valid absolute URL");
  }

  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error("LOCAL_BACKEND_URL must use http:// or https://");
  }

  const hostname = parsed.hostname.toLowerCase();
  if (!LOOPBACK_HOSTS.has(hostname) && !isIPv4Loopback(hostname)) {
    throw new Error(
      "LOCAL_BACKEND_URL must use a loopback host (localhost, 127.0.0.1, or ::1)",
    );
  }

  return parsed;
}

/**
 * Returns true when local mode is enabled via LOCAL_BACKEND_URL.
 *
 * @throws If LOCAL_BACKEND_URL is set but invalid.
 */
export function isLocalModeEnabled(): boolean {
  return getLocalBackendURL() !== null;
}

/**
 * Safely resolves local-mode state without throwing.
 *
 * @returns Local-mode state with optional configuration error.
 */
export function getLocalModeState(): { enabled: boolean; hasConfigError: boolean } {
  try {
    return { enabled: isLocalModeEnabled(), hasConfigError: false };
  } catch {
    return { enabled: false, hasConfigError: true };
  }
}
