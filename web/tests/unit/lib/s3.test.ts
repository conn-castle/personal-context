import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

const mockSend = vi.fn();
const mockGetSignedUrl = vi.fn();

vi.mock("@aws-sdk/client-s3", () => {
  return {
    S3Client: class MockS3Client {
      send = mockSend;
    },
    GetObjectCommand: class MockGetObjectCommand {
      [key: string]: unknown;
      constructor(input: Record<string, unknown>) {
        Object.assign(this, input);
      }
    },
    PutObjectCommand: class MockPutObjectCommand {
      [key: string]: unknown;
      constructor(input: Record<string, unknown>) {
        Object.assign(this, input);
      }
    },
    DeleteObjectsCommand: class MockDeleteObjectsCommand {
      [key: string]: unknown;
      constructor(input: Record<string, unknown>) {
        Object.assign(this, input);
      }
    },
  };
});

vi.mock("@aws-sdk/s3-request-presigner", () => ({
  getSignedUrl: (...args: unknown[]) => mockGetSignedUrl(...args),
}));

import {
  getS3Bucket,
  getS3Client,
  getPresignedUrl,
  getS3Version,
  bumpS3Version,
  deleteS3Objects,
  resetS3Client,
} from "@/lib/s3";

describe("S3 utilities", () => {
  const originalEnv = process.env;

  beforeEach(() => {
    process.env = {
      ...originalEnv,
      S3_BUCKET: "test-bucket",
      AWS_REGION: "us-east-1",
    };
    mockSend.mockReset();
    mockGetSignedUrl.mockReset();
    resetS3Client();
  });

  afterEach(() => {
    process.env = originalEnv;
    resetS3Client();
  });

  describe("getS3Bucket", () => {
    it("throws when S3_BUCKET is not set", () => {
      delete process.env.S3_BUCKET;
      expect(() => getS3Bucket()).toThrow(
        "S3_BUCKET environment variable is required"
      );
    });

    it("returns bucket name when set", () => {
      expect(getS3Bucket()).toBe("test-bucket");
    });
  });

  describe("getS3Client", () => {
    it("returns a client instance", () => {
      const client = getS3Client();
      expect(client).toBeDefined();
      expect(client.send).toBeDefined();
    });

    it("returns the same instance on subsequent calls", () => {
      expect(getS3Client()).toBe(getS3Client());
    });

    it("returns a new instance after reset", () => {
      const first = getS3Client();
      resetS3Client();
      const second = getS3Client();
      expect(first).not.toBe(second);
    });
  });

  describe("getPresignedUrl", () => {
    const userId = "test-user-id";

    it("generates a presigned URL with default expiry and user prefix", async () => {
      mockGetSignedUrl.mockResolvedValue("https://signed.example.com/key");
      const result = await getPresignedUrl("figures/slide-1/image.png", userId);
      expect(result.url).toBe("https://signed.example.com/key");
      expect(result.expires_at).toBeDefined();
      const expiresAt = new Date(result.expires_at).getTime();
      expect(expiresAt).toBeGreaterThan(Date.now());
      expect(mockGetSignedUrl).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({ Key: "users/test-user-id/figures/slide-1/image.png" }),
        expect.anything()
      );
    });

    it("uses custom expiry when provided", async () => {
      mockGetSignedUrl.mockResolvedValue("https://signed.example.com/key");
      await getPresignedUrl("key", userId, 600);
      expect(mockGetSignedUrl).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({ Bucket: "test-bucket", Key: "users/test-user-id/key" }),
        { expiresIn: 600 }
      );
    });

    it("uses PRESIGNED_URL_EXPIRY_SECONDS when no explicit expiry is provided", async () => {
      process.env.PRESIGNED_URL_EXPIRY_SECONDS = "900";
      mockGetSignedUrl.mockResolvedValue("https://signed.example.com/key");

      await getPresignedUrl("key", userId);

      expect(mockGetSignedUrl).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({ Bucket: "test-bucket", Key: "users/test-user-id/key" }),
        { expiresIn: 900 }
      );
    });

    it("throws when PRESIGNED_URL_EXPIRY_SECONDS is invalid", async () => {
      process.env.PRESIGNED_URL_EXPIRY_SECONDS = "abc";
      mockGetSignedUrl.mockResolvedValue("https://signed.example.com/key");

      await expect(getPresignedUrl("key", userId)).rejects.toThrow(
        "PRESIGNED_URL_EXPIRY_SECONDS must be a positive integer"
      );
    });
  });

  describe("getS3Version", () => {
    const userId = "test-user-id";

    it("returns version 0 when NoSuchKey", async () => {
      mockSend.mockRejectedValue({ name: "NoSuchKey" });
      const result = await getS3Version(userId);
      expect(result).toEqual({ version: 0, updated_at: "" });
    });

    it("returns version 0 when NotFound", async () => {
      mockSend.mockRejectedValue({ name: "NotFound" });
      const result = await getS3Version(userId);
      expect(result).toEqual({ version: 0, updated_at: "" });
    });

    it("parses version from S3 body", async () => {
      mockSend.mockResolvedValue({
        Body: {
          transformToString: () =>
            Promise.resolve(
              JSON.stringify({
                version: 42,
                updated_at: "2026-03-09T00:00:00Z",
              })
            ),
        },
      });
      const result = await getS3Version(userId);
      expect(result.version).toBe(42);
      expect(result.updated_at).toBe("2026-03-09T00:00:00Z");
    });

    it("uses per-user key prefix", async () => {
      mockSend.mockRejectedValue({ name: "NoSuchKey" });
      await getS3Version(userId);
      const callArg = mockSend.mock.calls[0][0] as Record<string, unknown>;
      expect(callArg.Key).toBe("users/test-user-id/_version");
    });

    it("returns version 0 for empty body", async () => {
      mockSend.mockResolvedValue({
        Body: { transformToString: () => Promise.resolve("") },
      });
      const result = await getS3Version(userId);
      expect(result).toEqual({ version: 0, updated_at: "" });
    });

    it("returns version 0 for null body", async () => {
      mockSend.mockResolvedValue({ Body: null });
      const result = await getS3Version(userId);
      expect(result).toEqual({ version: 0, updated_at: "" });
    });

    it("throws for malformed JSON object body", async () => {
      mockSend.mockResolvedValue({
        Body: {
          transformToString: () =>
            Promise.resolve(JSON.stringify({ foo: "bar" })),
        },
      });
      await expect(getS3Version(userId)).rejects.toThrow(
        "JSON object must include numeric version and string updated_at"
      );
    });

    it("falls back to plain text int64 (Go CLI legacy format)", async () => {
      mockSend.mockResolvedValue({
        Body: { transformToString: () => Promise.resolve("42") },
      });
      const result = await getS3Version(userId);
      expect(result).toEqual({ version: 42, updated_at: "" });
    });

    it("falls back to plain text int64 with whitespace", async () => {
      mockSend.mockResolvedValue({
        Body: { transformToString: () => Promise.resolve("  7  \n") },
      });
      const result = await getS3Version(userId);
      expect(result).toEqual({ version: 7, updated_at: "" });
    });

    it("throws for unparseable non-JSON, non-integer content", async () => {
      mockSend.mockResolvedValue({
        Body: { transformToString: () => Promise.resolve("not-a-number") },
      });
      await expect(getS3Version(userId)).rejects.toThrow(
        "Failed to parse _version content"
      );
    });

    it("rethrows non-NotFound errors", async () => {
      mockSend.mockRejectedValue(new Error("NetworkError"));
      await expect(getS3Version(userId)).rejects.toThrow("NetworkError");
    });

    it("rethrows errors without a name property", async () => {
      const err = { code: "AccessDenied", message: "Forbidden" };
      mockSend.mockRejectedValue(err);
      await expect(getS3Version(userId)).rejects.toBe(err);
    });
  });

  describe("bumpS3Version", () => {
    const userId = "test-user-id";

    it("writes version to S3 with user-prefixed key", async () => {
      mockSend.mockResolvedValue({});
      await bumpS3Version(5, "2026-03-09T10:00:00.000Z", userId);
      expect(mockSend).toHaveBeenCalledTimes(1);
      const callArg = mockSend.mock.calls[0][0] as Record<string, unknown>;
      expect(callArg.Bucket).toBe("test-bucket");
      expect(callArg.Key).toBe("users/test-user-id/_version");
      expect(callArg.ContentType).toBe("application/json");
      const body = JSON.parse(callArg.Body as string) as {
        version: number;
        updated_at: string;
      };
      expect(body.version).toBe(5);
      expect(body.updated_at).toBe("2026-03-09T10:00:00.000Z");
    });

    it("retries on failure with exponential backoff and succeeds", async () => {
      mockSend
        .mockRejectedValueOnce(new Error("Temporary"))
        .mockResolvedValue({});
      await bumpS3Version(5, "2026-03-09T10:00:00.000Z", userId);
      expect(mockSend).toHaveBeenCalledTimes(2);
    });

    it("throws after max retries with exponential backoff", async () => {
      mockSend
        .mockRejectedValueOnce(new Error("Persistent"))
        .mockRejectedValueOnce(new Error("Persistent"))
        .mockRejectedValueOnce(new Error("Persistent"));
      await expect(
        bumpS3Version(5, "2026-03-09T10:00:00.000Z", userId)
      ).rejects.toThrow("Persistent");
      expect(mockSend).toHaveBeenCalledTimes(3);
    });
  });

  describe("deleteS3Objects", () => {
    const userId = "test-user-id";

    it("returns without calling S3 when the key list is empty", async () => {
      await deleteS3Objects([], userId);

      expect(mockSend).not.toHaveBeenCalled();
    });

    it("deletes all provided keys with user prefix", async () => {
      mockSend.mockResolvedValue({});

      await deleteS3Objects(["figures/a.png", "data/b.csv"], userId);

      expect(mockSend).toHaveBeenCalledTimes(1);
      const callArg = mockSend.mock.calls[0][0] as Record<string, unknown>;
      expect(callArg.Bucket).toBe("test-bucket");
      expect(callArg.Delete).toEqual({
        Objects: [
          { Key: "users/test-user-id/figures/a.png" },
          { Key: "users/test-user-id/data/b.csv" },
        ],
        Quiet: true,
      });
    });

    it("throws when S3 reports per-object delete failures", async () => {
      mockSend.mockResolvedValue({
        Errors: [{ Key: "users/test-user-id/figures/a.png", Code: "AccessDenied" }],
      });

      await expect(deleteS3Objects(["figures/a.png"], userId)).rejects.toThrow(
        "S3 delete failed for:"
      );
    });

    it("splits more than 1000 keys into multiple batch requests", async () => {
      mockSend.mockResolvedValue({});

      await deleteS3Objects(
        Array.from({ length: 1001 }, (_, index) => `figures/key-${index}.png`),
        userId
      );

      expect(mockSend).toHaveBeenCalledTimes(2);
      const firstBatch = mockSend.mock.calls[0][0] as {
        Delete: { Objects: Array<{ Key: string }> };
      };
      const secondBatch = mockSend.mock.calls[1][0] as {
        Delete: { Objects: Array<{ Key: string }> };
      };
      expect(firstBatch.Delete.Objects).toHaveLength(1000);
      expect(secondBatch.Delete.Objects).toEqual([
        { Key: "users/test-user-id/figures/key-1000.png" },
      ]);
    });
  });
});
