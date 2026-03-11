import { NextRequest, NextResponse } from "next/server";
import { getS3Version } from "@/lib/s3";
import { internalError } from "@/lib/api-error";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";
import type { SyncVersionResponse } from "@/lib/types";
import type { ErrorResponseBody } from "@/lib/api-error";

/**
 * GET /api/sync/version
 *
 * Returns the current sync version from S3 `_version` key.
 */
export async function GET(
  _req: NextRequest
): Promise<NextResponse<SyncVersionResponse | ErrorResponseBody> | Response> {
  if (isLocalMode()) {
    return proxyToLocal(_req);
  }

  try {
    void _req;
    const { version, updated_at } = await getS3Version();
    return NextResponse.json({ version, updated_at });
  } catch (err) {
    console.error("GET /api/sync/version error:", err);
    return internalError("Failed to fetch sync version");
  }
}
