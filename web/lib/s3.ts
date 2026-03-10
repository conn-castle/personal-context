import {
  S3Client,
  GetObjectCommand,
  PutObjectCommand,
} from "@aws-sdk/client-s3";
import { getSignedUrl } from "@aws-sdk/s3-request-presigner";

const VERSION_KEY = "_version";
const DEFAULT_PRESIGN_EXPIRY_SECONDS = 3600;
const S3_VERSION_RETRY_COUNT = 3;

let s3Client: S3Client | null = null;

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

  const parsed = Number.parseInt(raw, 10);
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
 * @param s3Key - The S3 object key.
 * @param expirySeconds - URL expiration in seconds (default 3600).
 * @returns Presigned URL and expiration timestamp.
 */
export async function getPresignedUrl(
  s3Key: string,
  expirySeconds?: number
): Promise<{ url: string; expires_at: string }> {
  const client = getS3Client();
  const bucket = getS3Bucket();
  const resolvedExpirySeconds =
    expirySeconds ?? getConfiguredPresignExpirySeconds();

  const command = new GetObjectCommand({ Bucket: bucket, Key: s3Key });
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
 * @returns The current version and its updated_at timestamp.
 */
export async function getS3Version(): Promise<{
  version: number;
  updated_at: string;
}> {
  const client = getS3Client();
  const bucket = getS3Bucket();

  try {
    const command = new GetObjectCommand({ Bucket: bucket, Key: VERSION_KEY });
    const response = await client.send(command);
    const body = await response.Body?.transformToString();
    if (!body) return { version: 0, updated_at: "" };

    const parsed = JSON.parse(body) as Record<string, unknown>;
    return {
      version: typeof parsed.version === "number" ? parsed.version : 0,
      updated_at:
        typeof parsed.updated_at === "string" ? parsed.updated_at : "",
    };
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
 * @throws After all retry attempts fail.
 */
export async function bumpS3Version(
  version: number,
  updatedAt: string
): Promise<void> {
  const client = getS3Client();
  const bucket = getS3Bucket();
  const body = JSON.stringify({
    version,
    updated_at: updatedAt,
  });

  let lastError: unknown;
  for (let attempt = 0; attempt < S3_VERSION_RETRY_COUNT; attempt++) {
    try {
      const command = new PutObjectCommand({
        Bucket: bucket,
        Key: VERSION_KEY,
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
