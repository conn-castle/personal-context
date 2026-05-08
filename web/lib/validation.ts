const RECORD_ID_REGEX = /^\d{8}-[0-9a-f]{8}$/;
const GIT_HASH_REGEX = /^[0-9a-f]{40}$/;
const ISO_DATE_REGEX = /^\d{4}-\d{2}-\d{2}$/;
const ISO_TIMESTAMP_REGEX =
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

/**
 * Validates a record ID matches the `{YYYYMMDD}-{8hex}` format.
 *
 * @param id - The record ID to validate.
 * @returns True if the ID is valid.
 */
export function isValidRecordId(id: string): boolean {
  return RECORD_ID_REGEX.test(id);
}

/**
 * Validates a git hash is a 40-character lowercase hex string.
 *
 * @param hash - The git hash to validate.
 * @returns True if valid.
 */
export function isValidGitHash(hash: string): boolean {
  return GIT_HASH_REGEX.test(hash);
}

/**
 * Validates a date string is YYYY-MM-DD and represents a real calendar date.
 *
 * @param date - The date string to validate.
 * @returns True if valid.
 */
export function isValidDate(date: string): boolean {
  if (!ISO_DATE_REGEX.test(date)) return false;
  const parsed = new Date(date + "T00:00:00Z");
  if (isNaN(parsed.getTime())) return false;
  // Verify no date rollover (e.g., Feb 30 -> Mar 2)
  const [year, month, day] = date.split("-").map(Number);
  return (
    parsed.getUTCFullYear() === year &&
    parsed.getUTCMonth() + 1 === month &&
    parsed.getUTCDate() === day
  );
}

/**
 * Validates an ISO 8601 timestamp string.
 *
 * @param ts - The timestamp string to validate.
 * @returns True if parseable as a valid date.
 */
export function isValidISOTimestamp(ts: string): boolean {
  if (!ISO_TIMESTAMP_REGEX.test(ts)) return false;
  const parsed = new Date(ts);
  return !isNaN(parsed.getTime());
}

/**
 * Parses a query string value as an integer with bounds clamping.
 *
 * @param value - The raw query string value (null or string).
 * @param defaultValue - Returned when value is null, empty, or NaN.
 * @param min - Optional lower bound (clamped).
 * @param max - Optional upper bound (clamped).
 * @returns The parsed and clamped integer.
 */
export function parseQueryInt(
  value: string | null,
  defaultValue: number,
  min?: number,
  max?: number
): number {
  if (value === null || value === "") return defaultValue;
  const parsed = parseInt(value, 10);
  if (isNaN(parsed)) return defaultValue;
  let result = parsed;
  if (min !== undefined && result < min) result = min;
  if (max !== undefined && result > max) result = max;
  return result;
}

/**
 * Validates a filename is safe (non-empty, no path separators, no traversal).
 *
 * @param filename - The filename to validate.
 * @returns True if safe.
 */
export function isValidFilename(filename: string): boolean {
  if (filename.length === 0) return false;
  if (
    filename.includes("/") ||
    filename.includes("\\") ||
    filename.includes("\0")
  )
    return false;
  if (filename === "." || filename === "..") return false;
  return true;
}

/**
 * Normalizes notes: empty string and undefined become null (per CONTEXT.md).
 *
 * @param notes - The notes value.
 * @returns The normalized notes (null if empty).
 */
export function normalizeNotes(
  notes: string | null | undefined
): string | null {
  if (notes === undefined || notes === null || notes === "") return null;
  return notes;
}

/**
 * Validates a PATCH /api/records/[id] request body.
 * Checks for allowed fields, type correctness, and format constraints.
 *
 * @param body - The parsed request body.
 * @returns Validation result with data or error message.
 */
export function validateRecordUpdateInput(
  body: Record<string, unknown>
):
  | { valid: true; data: Record<string, unknown> }
  | { valid: false; error: string } {
  const allowed = ["project_id", "notes", "git_remote_url", "git_hash"];
  const keys = Object.keys(body);

  const unknownKeys = keys.filter((k) => !allowed.includes(k));
  if (unknownKeys.length > 0) {
    return { valid: false, error: `Unknown fields: ${unknownKeys.join(", ")}` };
  }

  if (keys.length === 0) {
    return {
      valid: false,
      error: "Request body must include at least one field to update",
    };
  }

  if ("git_hash" in body && body.git_hash !== null) {
    if (typeof body.git_hash !== "string" || !isValidGitHash(body.git_hash)) {
      return {
        valid: false,
        error: "git_hash must be a 40-character hex string or null",
      };
    }
  }

  if ("git_remote_url" in body && body.git_remote_url !== null) {
    if (
      typeof body.git_remote_url !== "string" ||
      body.git_remote_url.length === 0
    ) {
      return {
        valid: false,
        error: "git_remote_url must be a non-empty string or null",
      };
    }
    const allowedSchemes = ["https://", "http://", "git://", "ssh://"];
    const url = body.git_remote_url as string;
    if (!allowedSchemes.some((s) => url.startsWith(s))) {
      return {
        valid: false,
        error:
          "git_remote_url must start with https://, http://, git://, or ssh://",
      };
    }
  }

  if ("notes" in body) {
    if (body.notes !== null && typeof body.notes !== "string") {
      return { valid: false, error: "notes must be a string or null" };
    }
  }

  if ("project_id" in body) {
    if (
      typeof body.project_id !== "string" ||
      body.project_id.trim() === "" ||
      body.project_id !== body.project_id.trim()
    ) {
      return {
        valid: false,
        error: "project_id must be a non-empty string with no leading or trailing whitespace",
      };
    }
  }

  // Build normalized data without mutating the input
  const data: Record<string, unknown> = {};
  for (const key of keys) {
    if (key === "project_id") {
      data.project_id = body.project_id;
    } else if (key === "notes") {
      data.notes = normalizeNotes(body.notes as string | null | undefined);
    } else {
      data[key] = body[key];
    }
  }

  return { valid: true, data };
}
