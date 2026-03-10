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
    it("generates a presigned URL with default expiry", async () => {
      mockGetSignedUrl.mockResolvedValue("https://signed.example.com/key");
      const result = await getPresignedUrl("figures/slide-1/image.png");
      expect(result.url).toBe("https://signed.example.com/key");
      expect(result.expires_at).toBeDefined();
      const expiresAt = new Date(result.expires_at).getTime();
      expect(expiresAt).toBeGreaterThan(Date.now());
    });

    it("uses custom expiry when provided", async () => {
      mockGetSignedUrl.mockResolvedValue("https://signed.example.com/key");
      await getPresignedUrl("key", 600);
      expect(mockGetSignedUrl).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({ Bucket: "test-bucket", Key: "key" }),
        { expiresIn: 600 }
      );
    });

    it("uses PRESIGNED_URL_EXPIRY_SECONDS when no explicit expiry is provided", async () => {
      process.env.PRESIGNED_URL_EXPIRY_SECONDS = "900";
      mockGetSignedUrl.mockResolvedValue("https://signed.example.com/key");

      await getPresignedUrl("key");

      expect(mockGetSignedUrl).toHaveBeenCalledWith(
        expect.anything(),
        expect.objectContaining({ Bucket: "test-bucket", Key: "key" }),
        { expiresIn: 900 }
      );
    });

    it("throws when PRESIGNED_URL_EXPIRY_SECONDS is invalid", async () => {
      process.env.PRESIGNED_URL_EXPIRY_SECONDS = "abc";
      mockGetSignedUrl.mockResolvedValue("https://signed.example.com/key");

      await expect(getPresignedUrl("key")).rejects.toThrow(
        "PRESIGNED_URL_EXPIRY_SECONDS must be a positive integer"
      );
    });
  });

  describe("getS3Version", () => {
    it("returns version 0 when NoSuchKey", async () => {
      mockSend.mockRejectedValue({ name: "NoSuchKey" });
      const result = await getS3Version();
      expect(result).toEqual({ version: 0, updated_at: "" });
    });

    it("returns version 0 when NotFound", async () => {
      mockSend.mockRejectedValue({ name: "NotFound" });
      const result = await getS3Version();
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
      const result = await getS3Version();
      expect(result.version).toBe(42);
      expect(result.updated_at).toBe("2026-03-09T00:00:00Z");
    });

    it("returns version 0 for empty body", async () => {
      mockSend.mockResolvedValue({
        Body: { transformToString: () => Promise.resolve("") },
      });
      const result = await getS3Version();
      expect(result).toEqual({ version: 0, updated_at: "" });
    });

    it("returns version 0 for null body", async () => {
      mockSend.mockResolvedValue({ Body: null });
      const result = await getS3Version();
      expect(result).toEqual({ version: 0, updated_at: "" });
    });

    it("returns version 0 for malformed JSON body", async () => {
      mockSend.mockResolvedValue({
        Body: {
          transformToString: () =>
            Promise.resolve(JSON.stringify({ foo: "bar" })),
        },
      });
      const result = await getS3Version();
      expect(result).toEqual({ version: 0, updated_at: "" });
    });

    it("rethrows non-NotFound errors", async () => {
      mockSend.mockRejectedValue(new Error("NetworkError"));
      await expect(getS3Version()).rejects.toThrow("NetworkError");
    });

    it("rethrows errors without a name property", async () => {
      const err = { code: "AccessDenied", message: "Forbidden" };
      mockSend.mockRejectedValue(err);
      await expect(getS3Version()).rejects.toBe(err);
    });
  });

  describe("bumpS3Version", () => {
    it("writes version to S3 with provided updatedAt", async () => {
      mockSend.mockResolvedValue({});
      await bumpS3Version(5, "2026-03-09T10:00:00.000Z");
      expect(mockSend).toHaveBeenCalledTimes(1);
      const callArg = mockSend.mock.calls[0][0] as Record<string, unknown>;
      expect(callArg.Bucket).toBe("test-bucket");
      expect(callArg.Key).toBe("_version");
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
      await bumpS3Version(5, "2026-03-09T10:00:00.000Z");
      expect(mockSend).toHaveBeenCalledTimes(2);
    });

    it("throws after max retries with exponential backoff", async () => {
      mockSend
        .mockRejectedValueOnce(new Error("Persistent"))
        .mockRejectedValueOnce(new Error("Persistent"))
        .mockRejectedValueOnce(new Error("Persistent"));
      await expect(
        bumpS3Version(5, "2026-03-09T10:00:00.000Z")
      ).rejects.toThrow("Persistent");
      expect(mockSend).toHaveBeenCalledTimes(3);
    });
  });
});
