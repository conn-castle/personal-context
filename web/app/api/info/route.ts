import { NextRequest, NextResponse } from "next/server";
import { isLocalMode, proxyToLocal } from "@/lib/local-proxy";
import type { AppInfoResponse } from "@/lib/types";
import type { ErrorResponseBody } from "@/lib/api-error";
import packageMetadata from "../../../package.json";

/**
 * GET /api/info
 *
 * Returns the application mode (local or cloud) and version.
 */
export async function GET(
  req: NextRequest
): Promise<NextResponse<AppInfoResponse | ErrorResponseBody> | Response> {
  if (isLocalMode()) {
    return proxyToLocal(req);
  }

  return NextResponse.json({
    mode: "cloud",
    version: packageMetadata.version,
  });
}
