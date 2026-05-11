import { describe, expect, it } from "vitest";
import { NextRequest } from "next/server";
import {
  JsonBodyError,
  readBoundedJson,
} from "@/lib/bounded-json";

describe("readBoundedJson", () => {
  it("parses valid JSON within the byte limit", async () => {
    const req = new NextRequest("http://localhost/api/test", {
      method: "POST",
      body: JSON.stringify({ ok: true }),
    });

    await expect(readBoundedJson(req, 1024)).resolves.toEqual({ ok: true });
  });

  it("ignores malformed content-length values and reads the body", async () => {
    const req = new NextRequest("http://localhost/api/test", {
      method: "POST",
      body: JSON.stringify({ ok: true }),
      headers: { "content-length": "unknown" },
    });

    await expect(readBoundedJson(req, 1024)).resolves.toEqual({ ok: true });
  });

  it("rejects requests without a body stream", async () => {
    const req = new NextRequest("http://localhost/api/test", {
      method: "POST",
    });

    await expect(readBoundedJson(req, 1024)).rejects.toMatchObject({
      status: 400,
      code: "BAD_REQUEST",
    } satisfies Partial<JsonBodyError>);
  });

  it("rejects bodies over the content-length limit before reading", async () => {
    const req = new NextRequest("http://localhost/api/test", {
      method: "POST",
      body: "{}",
      headers: { "content-length": "5" },
    });

    await expect(readBoundedJson(req, 4)).rejects.toMatchObject({
      status: 413,
      code: "REQUEST_BODY_TOO_LARGE",
    } satisfies Partial<JsonBodyError>);
  });

  it("rejects streamed bodies that exceed the byte limit", async () => {
    const req = new NextRequest("http://localhost/api/test", {
      method: "POST",
      body: JSON.stringify({ value: "too long" }),
    });

    await expect(readBoundedJson(req, 8)).rejects.toMatchObject({
      status: 413,
      code: "REQUEST_BODY_TOO_LARGE",
    } satisfies Partial<JsonBodyError>);
  });

  it("rejects malformed JSON bodies", async () => {
    const req = new NextRequest("http://localhost/api/test", {
      method: "POST",
      body: "{",
    });

    await expect(readBoundedJson(req, 1024)).rejects.toMatchObject({
      status: 400,
      code: "BAD_REQUEST",
    } satisfies Partial<JsonBodyError>);
  });
});
