import type { FileUrlResponse } from "@/lib/types";

export type RecordFileKind = "figures" | "data";

/**
 * Resolves a record attachment to a presigned URL via the web file API.
 *
 * @param recordId - The record identifier.
 * @param kind - The attachment collection (`figures` or `data`).
 * @param filename - The attachment filename.
 * @returns The resolved presigned URL.
 * @throws If the API response is not successful or does not contain a URL.
 */
export async function fetchRecordFileUrl(
  recordId: string,
  kind: RecordFileKind,
  filename: string
): Promise<string> {
  const res = await fetch(
    `/api/files/${encodeURIComponent(recordId)}/${kind}/${encodeURIComponent(filename)}`
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
