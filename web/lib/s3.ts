import {
  S3Client,
  GetObjectCommand,
  PutObjectCommand,
  DeleteObjectsCommand,
} from "@aws-sdk/client-s3";
import { getSignedUrl } from "@aws-sdk/s3-request-presigner";

/**
 * Returns the per-user S3 key prefix.
 *
 * @param userId - The authenticated user's ID.
 * @returns The prefix string (e.g. "users/{userId}/").
 */
function userPrefix(userId: string): string {
  return `users/${userId}/`;
}

const VERSION_KEY = "_version";
const DEFAULT_PRESIGN_EXPIRY_SECONDS = 3600;
const S3_VERSION_RETRY_COUNT = 3;

let s3Client: S3Client | null = null;

/**
 * Parses the canonical JSON `_version` payload.
 *
 * @param body - Raw `_version` object body.
 * @returns Parsed version metadata.
 * @throws If required fields are missing or have the wrong types.
 */
function parseVersionObject(body: string): { version: number; updated_at: string } {
  const parsed: unknown = JSON.parse(body);
  if (typeof parsed !== "object" || parsed === null) {
    throw new Error(`Failed to parse _version content: ${body}`);
  }

  const obj = parsed as Record<string, unknown>;
  if (typeof obj.version !== "number" || typeof obj.updated_at !== "string") {
    throw new Error(
      "Failed to parse _version content: JSON object must include numeric version and string updated_at"
    );
  }

  // The sync version is an int64 counter; reject non-integer, negative, NaN, or
  // Infinity values (all of which pass `typeof === "number"`) so corrupt
  // payloads fail loud here instead of flowing into sync comparison logic.
  if (!Number.isInteger(obj.version) || obj.version < 0) {
    throw new Error(
      "Failed to parse _version content: version must be a non-negative integer"
    );
  }

  return {
    version: obj.version,
    updated_at: obj.updated_at,
  };
}

/**
 * Returns a lazily-initialized S3 client singleton.
 *
 * @returns The S3 client.
 */
export function getS3Client(): S3Client {
  if (s3Client) return s3Client;
  s3Client = new S3Client({ region: process.env.AWS_REGION ?? "us-east-1" });
  return s3Client;
}

/**
 * Returns the configured S3 bucket name.
 *
 * @returns The bucket name.
 * @throws If S3_BUCKET is not set.
 */
export function getS3Bucket(): string {
  const bucket = process.env.S3_BUCKET;
  if (!bucket) {
    throw new Error("S3_BUCKET environment variable is required");
  }
  return bucket;
}

/**
 * Returns the configured default presigned URL expiry.
 *
 * @returns Expiry in seconds.
 * @throws If PRESIGNED_URL_EXPIRY_SECONDS is present but invalid.
 */
function getConfiguredPresignExpirySeconds(): number {
  const raw = process.env.PRESIGNED_URL_EXPIRY_SECONDS;
  if (!raw) {
    return DEFAULT_PRESIGN_EXPIRY_SECONDS;
  }

  // Use Number() rather than parseInt() so non-integer values fail loud. With
  // parseInt(), "3600.9" silently truncated to 3600 and "12abc" to 12, and the
  // Number.isInteger guard below could never fire (parseInt always yields an
  // integer or NaN).
  const parsed = Number(raw.trim());
  if (!Number.isInteger(parsed) || parsed <= 0) {
    throw new Error(
      "PRESIGNED_URL_EXPIRY_SECONDS must be a positive integer"
    );
  }

  return parsed;
}

/**
 * Generates a presigned GET URL for an S3 object.
 *
 * @param s3Key - The S3 object key (without user prefix).
 * @param userId - The user ID for per-user namespacing.
 * @param expirySeconds - URL expiration in seconds (default 3600).
 * @returns Presigned URL and expiration timestamp.
 */
export async function getPresignedUrl(
  s3Key: string,
  userId: string,
  expirySeconds?: number
): Promise<{ url: string; expires_at: string }> {
  const client = getS3Client();
  const bucket = getS3Bucket();
  const resolvedExpirySeconds =
    expirySeconds ?? getConfiguredPresignExpirySeconds();

  const fullKey = `${userPrefix(userId)}${s3Key}`;
  const command = new GetObjectCommand({ Bucket: bucket, Key: fullKey });
  const url = await getSignedUrl(client, command, {
    expiresIn: resolvedExpirySeconds,
  });
  const expiresAt = new Date(
    Date.now() + resolvedExpirySeconds * 1000
  ).toISOString();

  return { url, expires_at: expiresAt };
}

/**
 * Reads the sync version from S3 `_version` key.
 * Returns version 0 if the key does not exist (Decision b3c4d5p).
 *
 * @param userId - The user ID for per-user namespacing.
 * @returns The current version and its updated_at timestamp.
 */
export async function getS3Version(userId: string): Promise<{
  version: number;
  updated_at: string;
}> {
  const client = getS3Client();
  const bucket = getS3Bucket();
  const fullKey = `${userPrefix(userId)}${VERSION_KEY}`;

  try {
    const command = new GetObjectCommand({ Bucket: bucket, Key: fullKey });
    const response = await client.send(command);
    const body = await response.Body?.transformToString();
    if (!body) return { version: 0, updated_at: "" };

    const trimmedBody = body.trim();
    if (trimmedBody.startsWith("{")) {
      return parseVersionObject(trimmedBody);
    }

    // Backward-compatible fallback: plain text int64 (Go CLI legacy format).
    // Require a bare non-negative decimal integer; `parseInt` would otherwise
    // silently accept corrupt prefixes ("12abc" -> 12), signs ("+5", "-7"), and
    // scientific notation ("1e3" -> 1).
    if (/^\d+$/.test(trimmedBody)) {
      return { version: Number(trimmedBody), updated_at: "" };
    }
    throw new Error(`Failed to parse _version content: ${body}`);
  } catch (err: unknown) {
    if (isNotFoundError(err)) {
      return { version: 0, updated_at: "" };
    }
    throw err;
  }
}

/**
 * Writes a new sync version to S3 `_version` key.
 * Retries up to 3 times on failure with exponential backoff
 * (Decision o7p8q9: write-after with retry).
 *
 * @param version - The version number to write.
 * @param updatedAt - The DB timestamp to use as updated_at (avoids clock divergence with Postgres).
 * @param userId - The user ID for per-user namespacing.
 * @throws After all retry attempts fail.
 */
export async function bumpS3Version(
  version: number,
  updatedAt: string,
  userId: string
): Promise<void> {
  const client = getS3Client();
  const bucket = getS3Bucket();
  const fullKey = `${userPrefix(userId)}${VERSION_KEY}`;
  const body = JSON.stringify({
    version,
    updated_at: updatedAt,
  });

  let lastError: unknown;
  for (let attempt = 0; attempt < S3_VERSION_RETRY_COUNT; attempt++) {
    try {
      const command = new PutObjectCommand({
        Bucket: bucket,
        Key: fullKey,
        Body: body,
        ContentType: "application/json",
      });
      await client.send(command);
      return;
    } catch (err) {
      lastError = err;
      if (attempt < S3_VERSION_RETRY_COUNT - 1) {
        await new Promise((resolve) =>
          setTimeout(resolve, Math.pow(2, attempt) * 100)
        );
      }
    }
  }
  throw lastError;
}

/**
 * Deletes multiple S3 objects by key in a single batch request.
 *
 * Uses the DeleteObjects API which supports up to 1000 keys per call.
 * Keys are batched automatically if the count exceeds the API limit.
 *
 * @param keys - The S3 object keys to delete (without user prefix).
 * @param userId - The user ID for per-user namespacing.
 * @throws If the S3 DeleteObjects call fails.
 */
export async function deleteS3Objects(keys: string[], userId: string): Promise<void> {
  if (keys.length === 0) return;

  const client = getS3Client();
  const bucket = getS3Bucket();
  const BATCH_SIZE = 1000;
  const prefix = userPrefix(userId);

  for (let i = 0; i < keys.length; i += BATCH_SIZE) {
    const batch = keys.slice(i, i + BATCH_SIZE);
    const command = new DeleteObjectsCommand({
      Bucket: bucket,
      Delete: {
        Objects: batch.map((key) => ({ Key: `${prefix}${key}` })),
        Quiet: true,
      },
    });
    const result = await client.send(command);
    if (result.Errors && result.Errors.length > 0) {
      const details = result.Errors.map(
        (error) => `${error.Key ?? "<unknown>"} (${error.Code ?? "Unknown"})`
      ).join(", ");
      throw new Error(`S3 delete failed for: ${details}`);
    }
  }
}

/**
 * Resets the cached S3 client. Test-only.
 */
export function resetS3Client(): void {
  s3Client = null;
}

function isNotFoundError(err: unknown): boolean {
  if (err && typeof err === "object" && "name" in err) {
    const name = (err as { name: string }).name;
    return name === "NoSuchKey" || name === "NotFound";
  }
  return false;
}
