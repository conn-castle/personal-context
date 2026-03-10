import type { FileUrlResponse } from "@/lib/types";

export type SlideFileKind = "figures" | "data";

/**
 * Resolves a slide attachment to a presigned URL via the web file API.
 *
 * @param slideId - The slide identifier.
 * @param kind - The attachment collection (`figures` or `data`).
 * @param filename - The attachment filename.
 * @returns The resolved presigned URL.
 * @throws If the API response is not successful or does not contain a URL.
 */
export async function fetchSlideFileUrl(
  slideId: string,
  kind: SlideFileKind,
  filename: string
): Promise<string> {
  const res = await fetch(
    `/api/files/${encodeURIComponent(slideId)}/${kind}/${encodeURIComponent(filename)}`
  );

  if (!res.ok) {
    throw new Error(`Failed to resolve ${kind} file: ${res.status}`);
  }

  const data = (await res.json()) as FileUrlResponse;
  if (typeof data.url !== "string" || data.url.length === 0) {
    throw new Error(`Invalid ${kind} file URL response`);
  }

  return data.url;
}
