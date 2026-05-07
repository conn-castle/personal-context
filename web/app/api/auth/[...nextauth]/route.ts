import { NextRequest, NextResponse } from "next/server";
import { handlers } from "@/lib/auth";
import { getLocalModeState } from "@/lib/local-mode";

function localModeDisabledResponse(): NextResponse {
  return NextResponse.json(
    { error: "Authentication is unavailable in local mode.", code: "LOCAL_MODE_AUTH_DISABLED" },
    { status: 403 },
  );
}

function invalidConfigResponse(): NextResponse {
  return NextResponse.json(
    { error: "Invalid LOCAL_BACKEND_URL configuration", code: "INVALID_CONFIG" },
    { status: 500 },
  );
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
