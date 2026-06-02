import { NextRequest } from "next/server";
import type { NextResponse } from "next/server";
import { handlers } from "@/lib/auth";
import { getLocalModeState } from "@/lib/local-mode";
import { invalidConfig, localModeAuthDisabled } from "@/lib/api-error";

function localModeDisabledResponse(): NextResponse {
  return localModeAuthDisabled("Authentication is unavailable in local mode.");
}

function invalidConfigResponse(): NextResponse {
  return invalidConfig();
}

export async function GET(req: NextRequest): Promise<Response> {
  const localMode = getLocalModeState();
  if (localMode.hasConfigError) {
    return invalidConfigResponse();
  }
  if (localMode.enabled) {
    return localModeDisabledResponse();
  }

  return handlers.GET(req);
}

export async function POST(req: NextRequest): Promise<Response> {
  const localMode = getLocalModeState();
  if (localMode.hasConfigError) {
    return invalidConfigResponse();
  }
  if (localMode.enabled) {
    return localModeDisabledResponse();
  }

  return handlers.POST(req);
}
