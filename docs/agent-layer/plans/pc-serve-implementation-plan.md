# `pc serve` Implementation Plan

> **Phase 9 completed (2026-03).** This plan is retained as architectural rationale. For current state, see CONTEXT.md.

## Overview

Add a `pc serve` command to the Go CLI that starts an HTTP server implementing the same REST API as the Next.js API routes, backed by local SQLite + filesystem. This enables `make dev` without cloud credentials (Neon/S3).

**Architecture**:
- Cloud (Amplify): `Browser → Next.js API routes → Neon Postgres + S3`
- Local (`pc serve`): `Browser → Next.js API routes → Go HTTP server → SQLite + local files`

The Next.js API routes detect `LOCAL_BACKEND_URL` and proxy to the Go server when set. When not set, behavior is unchanged.

## Prerequisites

Before starting implementation, resolve:

- **S3 `_version` format discrepancy** (ISSUES.md p4q5r6a): Go writes plain text int64, web writes JSON `{version, updated_at}`. Must converge on JSON format in both Go and web S3 clients first, since both CLI sync and web UI mutations will coexist in production.

## Task List

### Phase A: Foundation (Go HTTP server scaffold)

**A1. Converge S3 `_version` format (Go + web)**

*Go side:*
- Update `cli/internal/s3client/` to read/write `_version` as JSON `{"version": N, "updated_at": "ISO"}` instead of plain text int64.
- Update `HeadVersion` to return `(int64, string, error)` or a struct with both version and updated_at.
- Update `UpdateVersion` to accept both version and updated_at.
- Add backward-compatible read: if JSON parse fails, try plain text int64 (migration path for existing buckets).
- Update all callers in the sync engine.
- Update integration tests.

*Web side:*
- Update `web/lib/s3.ts` `getS3Version()` to add a backward-compatible read fallback: if `JSON.parse(body)` fails, try parsing `body` as a plain-text integer and return `{ version: parseInt(body), updated_at: "" }`. Currently `getS3Version()` only handles JSON — if a Go-written plain text `_version` exists, `JSON.parse` will throw and propagate as an unhandled error (not a `NoSuchKey`, so the catch block won't intercept it).
- This ensures both Go and web can read each other's `_version` writes during the migration window where both formats may coexist.

**Exit**: All existing Go tests pass with the new format. Web `getS3Version` tests cover both JSON and plain-text inputs. Existing S3 buckets with plain text format are still readable by both Go and web.

**A2. Repository interface extensions for HTTP API**

The Go `Repository` interface was designed for CLI use and has two gaps that must be resolved before implementing HTTP handlers:

*Gap 1: `UpdateSlideInput` is full-replacement, not patch-shaped.*
- `UpdateSlideInput` requires all fields (ID, Date, DayOrder, HTMLContent). The web `PATCH /api/slides/:id` accepts partial updates (only `project_id`, `notes`, `git_remote_url`, `git_hash`).
- **Decision**: Do NOT change the Repository interface or `UpdateSlideInput` struct. Instead, implement a read-then-merge adapter in the `serve` package: the handler fetches the existing slide via `GetSlideByID`, merges the PATCH body fields onto it, and calls `UpdateSlide` with the complete input. This keeps the Repository contract stable and matches how the web API routes work (they also read-then-write against Postgres).
- Document this pattern in the serve package.

*Gap 2: `ListSlidesFilter` lacks `UpdatedAfter`.*
- The `GET /api/sync/changes?since=<ISO>` endpoint needs to filter slides by `updated_at >= since AND updated_at <= server_now`.
- **Decision**: Add `UpdatedAfter *time.Time` and `UpdatedBefore *time.Time` fields to `ListSlidesFilter` in `cli/internal/repository/types.go`.
- Update both SQLite and Postgres repository implementations to apply the new filter fields in their `ListSlides` queries.
- Add contract test cases for the new filter fields.

**Exit**: `ListSlidesFilter` supports `UpdatedAfter` and `UpdatedBefore`. Both repository implementations pass contract tests. The serve package has a documented read-then-merge adapter pattern for PATCH.

**A3. Create `pc serve` command scaffold**
- New package: `cli/internal/serve/`
- New command: `cli/internal/cli/serve.go` — registers `pc serve` with cobra.
- Flags: `--port` (default 9876), `--web-dir` (optional path to `web/` for auto-launching `next dev`).
- On startup: read CLI config, open SQLite DB, resolve local data directory (`~/personal-context/`), start HTTP server on `127.0.0.1:<port>`.
- Log: print `Local API server listening on http://127.0.0.1:<port>` and `Set LOCAL_BACKEND_URL=http://127.0.0.1:<port> when running next dev`.
- Graceful shutdown on SIGINT/SIGTERM.
- **Exit**: `pc serve` starts and responds to `GET /health` with `{"status": "ok"}`.

**A4. Request routing and error handling**
- Use `net/http` ServeMux (no external router dependency needed — Go 1.22+ supports method+path patterns).
- JSON request parsing helpers: `decodeJSON`, `writeJSON`, `writeError`.
- Error mapper: `repository.ErrNotFound` → 404, `repository.ErrConflict` → 409, `repository.ErrInvalidArgument` → 400, `repository.ErrForeignKeyViolation` → 409, anything else → 500.
- Error response shape matches web API: `{"error": "message", "code": "ERROR_CODE"}`.
- CORS middleware: allow `http://localhost:3000` (Next.js dev server).
- **Exit**: Shared helpers tested. Error mapping covered.

### Phase B: API endpoint implementation

Implement each endpoint in `cli/internal/serve/`. Each handler calls the existing `Repository` interface methods and formats responses to match the web API JSON shapes exactly (field names, types, null handling, pagination format).

**B1. `GET /api/projects`**
- Calls `repo.ListDistinctProjectIDs()`.
- Response: `{"projects": ["a", "b"]}`.

**B2. `GET /api/slides`**
- Parse query params: `limit`, `cursor`, `project`, `deleted`, `updated_after`.
- Calls `repo.ListSlides()` with appropriate `ListSlidesFilter`.
- Implement cursor-based pagination (same cursor format as web API — encode `(date, day_order, id)` tuple).
- Response: `{"items": [...SlideSummary], "next_cursor": "..." | null}`.
- **Note**: Must include `html_content`, `figure_count`, `data_file_count` in each SlideSummary. `figure_count` and `data_file_count` require additional queries or a list+count helper.

**B3. `GET /api/slides/:id`**
- Calls `repo.GetSlideByID()`, `repo.ListSlideFiguresBySlideID()`, `repo.ListSlideDataFilesBySlideID()`.
- Response: `{"slide": SlideDetail}` with figures and data_files arrays.

**B4. `PATCH /api/slides/:id`**
- Parse JSON body: `project_id`, `notes`, `git_remote_url`, `git_hash` (all optional).
- Uses the read-then-merge adapter (A2): fetch existing slide, merge PATCH fields, call `repo.UpdateSlide()` with the complete input.
- Re-fetch full slide detail for response.
- Include `sync_version` from `repo.GetSyncVersion()`.
- Response: `{"slide": SlideDetail, "sync_version": N}`.

**B5. `PATCH /api/slides/:id/order`**
- Parse JSON body: `date`, `position` (first/last/before/after with reference_id).
- Compute new fractional index using existing `fracdex` library.
- Uses the read-then-merge adapter: fetch existing slide, update date + day_order, call `repo.UpdateSlide()`.
- Response: `{"id", "date", "day_order", "updated_at", "sync_version"}`.

**B6. `DELETE /api/slides/:id`**
- Calls `repo.SoftDeleteSlide()`.
- Re-fetch slide for response timestamps.
- Response: `{"id", "deleted_at", "updated_at", "sync_version"}`.

**B7. `POST /api/slides/:id/restore`**
- Calls `repo.RestoreSlide()`.
- Re-fetch slide for response.
- Response: `{"id", "deleted_at": null, "updated_at", "sync_version"}`.

**B8. `GET /api/sync/version`**
- Calls `repo.GetSyncVersion()`.
- Response: `{"version": N, "updated_at": "ISO"}`.

**B9. `GET /api/sync/changes?since=<ISO>`**
- **Snapshot window pattern** (matches web route behavior): Capture `server_now = time.Now().UTC()` *before* querying the database. Use `server_now` as the upper bound for the `updated_at` range (`updated_at >= since AND updated_at <= server_now`). Return `server_now` in the response.
- This prevents the race where a slide is modified between capturing the timestamp and querying, which would cause the change to be missed by the next sync (since the client uses `server_now` as its next `since` value).
- Calls `repo.ListSlides()` with `UpdatedAfter` and `UpdatedBefore` filters (from A2) plus `IncludeDeleted: true`.
- Response: `{"items": [...SlideSummary], "server_now": "ISO"}`.

**B10. `GET /api/files/:slide_id/figures/:filename` and `GET /api/files/:slide_id/data/:filename`**
- **Contract**: Returns JSON `{"url": "<direct_file_url>", "expires_at": "<far_future_ISO>"}` — identical response shape to the cloud route's `FileUrlResponse`.
- The `url` value points to a separate direct-file-serving endpoint on the Go server: `http://localhost:<port>/local-files/<slide_id>/figures/<filename>` (or `/data/`).
- `expires_at` is a fixed far-future timestamp (e.g., `2099-01-01T00:00:00Z`) since local files don't expire.
- The Go server also registers a `/local-files/` handler that resolves the local file path (`<data_dir>/figures/<slide_id>/<filename>` or `<data_dir>/data/<slide_id>/<filename>`), validates against directory traversal (`..` in path components), and serves the file with `http.ServeFile`.
- Validate that the file record exists in the DB (slide_figures / slide_data_files table) before generating the URL — return 404 if not found.
- **Rationale**: Keeping the JSON `{url, expires_at}` shape means the frontend `FileUrlResponse` type, presigned URL caching, and `<img src={url}>` rendering all work unchanged in local mode. No conditional branching in the frontend.

### Phase C: Web-side proxy integration

**C1. Add proxy helper**
- New file: `web/lib/local-proxy.ts` — helper that checks `process.env.LOCAL_BACKEND_URL` and, when set, proxies a `Request` to the Go server and returns the response.
- Handles: forwarding method, headers, body, query params. Returns the Go server's response directly.

**C2. Update API routes to proxy when local**
- Each API route file (`web/app/api/*/route.ts`) gets a guard at the top of each handler:
  ```ts
  if (process.env.LOCAL_BACKEND_URL) {
    return proxyToLocal(request);
  }
  ```
- The rest of the handler (Neon/S3 logic) is unchanged.
- Files to update: `slides/route.ts`, `slides/[id]/route.ts`, `slides/[id]/order/route.ts`, `slides/[id]/restore/route.ts`, `sync/version/route.ts`, `sync/changes/route.ts`, `files/[slideId]/[...path]/route.ts`, `projects/route.ts`.

**C3. `make dev` integration**
- **Decision**: Single canonical command `make dev` that auto-detects the mode:
  - If `DATABASE_URL` and `S3_BUCKET` are set in `web/.env.local`, run `next dev` directly (cloud mode, existing behavior).
  - If not, run `pc serve` (Go server on port 9876) and `LOCAL_BACKEND_URL=http://127.0.0.1:9876 next dev` concurrently (local mode).
- Also provide explicit targets for when auto-detection is not desired:
  - `make dev-local` — always starts `pc serve` + proxied `next dev`, regardless of env vars.
  - `make dev-cloud` — always starts `next dev` without proxy, fails fast if cloud env vars are missing.
  - `make serve` — starts `pc serve` only (Go server), for users who run `next dev` separately.
- Update `web/.env.example` with `LOCAL_BACKEND_URL` documentation.
- **Rationale**: `make dev` should "just work" for the common case. Explicit targets exist for CI and advanced use.

### Phase D: Contract tests

**D1. Define shared test fixtures**
- JSON fixture files in a shared location (e.g., `tests/contract/fixtures/`).
- Each fixture defines: HTTP method, path, query params, request body, expected status code, expected response body.
- Cover every endpoint with multiple cases: happy path, edge cases, error cases.

*Timestamp normalization rules* (applied by both test runners):
- Fixture `expected_response` uses placeholder `"<<TIMESTAMP>>"` for any timestamp field whose exact value depends on DB clock/trigger timing.
- Test runners validate `"<<TIMESTAMP>>"` fields using a structural matcher: value must be a valid ISO 8601 string in UTC (ends with `Z` or `+00:00`), non-empty.
- For **ordering assertions** (e.g., "updated_at of slide A > updated_at of slide B"), fixtures include an optional `"timestamp_ordering"` array: `[["$.items[0].updated_at", ">", "$.items[1].updated_at"]]`. Test runners parse these as JSONPath comparisons on the actual response.
- For **precision normalization**: both test runners truncate all timestamp values to millisecond precision before comparison (SQLite stores ms, Postgres stores µs). This means `2026-03-10T10:00:00.123456Z` and `2026-03-10T10:00:00.123Z` are treated as equal.
- `null` timestamp fields (e.g., `deleted_at: null`) are compared exactly — no normalization.

**D2. Go contract test runner**
- Test file in `cli/internal/serve/` that:
  - Seeds a test SQLite DB with fixture data.
  - Starts the Go HTTP server on a random port.
  - For each fixture: sends the HTTP request, applies timestamp normalization, asserts response matches expected.
  - Runs as part of `go test ./internal/serve/...`.

**D3. Node contract test runner**
- Test file in `web/tests/contract/` that:
  - Seeds a test Postgres DB (via testcontainers or Neon test instance) with the same fixture data.
  - Starts the Next.js dev server.
  - For each fixture: sends the HTTP request, applies timestamp normalization, asserts response matches expected.
  - Runs as part of `pnpm test:contract`.

**D4. Parity assertion test**
- A CI job (or test script) that:
  - Runs both servers (Go + Next.js) with identical seed data.
  - For each fixture: sends the same request to both, applies timestamp normalization (truncate to ms, replace `<<TIMESTAMP>>` placeholders), diffs the normalized responses.
  - Fails if any response differs.
  - Uses deep-equal on parsed JSON objects, not string comparison.

### Phase E: Polish and documentation

**E1. `pc serve` help text and documentation**
- Cobra help text with usage examples.
- Update `README.md` with local dev setup instructions.
- Update `COMMANDS.md` with `pc serve`, `make dev`, `make dev-local`, `make dev-cloud`, `make serve`.

**E2. Update CONTEXT.md**
- Document the dual-backend architecture diagram.
- Document the `LOCAL_BACKEND_URL` env var.

**E3. Coverage and CI**
- Ensure `pc serve` handlers have >95% Go test coverage.
- Add contract tests to CI pipeline.
- Verify `pnpm test:coverage` still >95% with the proxy additions.

## Implementation Order

1. **A1** — Converge S3 version format (Go + web fallback)
2. **A2** — Repository interface extensions (`UpdatedAfter`/`UpdatedBefore` in `ListSlidesFilter`, read-then-merge adapter pattern)
3. **A3, A4** — Go server scaffold + routing
4. **B1–B10** — All API endpoints (can be done incrementally, testing each)
5. **C1–C2** — Web proxy integration
6. **C3** — Makefile integration
7. **D1–D4** — Contract tests
8. **E1–E3** — Documentation and polish

## Key Risks

- **Cursor pagination parity**: The web API uses an opaque cursor encoding. The Go server must produce identical cursor format so the frontend pagination works seamlessly.
- **Timestamp precision**: SQLite stores millisecond precision, Postgres stores microsecond. Contract tests truncate to millisecond for comparison, and use `<<TIMESTAMP>>` placeholders for DB-generated values.
- **Response field ordering**: JSON field order doesn't matter semantically, but contract test assertions should use deep-equal on parsed objects, not string comparison.
- **Read-then-merge atomicity**: The Go PATCH handler reads the slide, merges fields, and writes back. Without transactions, a concurrent mutation between read and write could lose data. Acceptable for single-user local mode; document the limitation.

## Revision History

- **v2 (2026-03-10)**: Addressed 6 review findings:
  1. A1 now includes web-side `_version` fallback in `getS3Version()`.
  2. Added A2 (repository extensions): `UpdatedAfter`/`UpdatedBefore` in `ListSlidesFilter`, read-then-merge adapter for PATCH.
  3. B10 locked to JSON `{url, expires_at}` contract with Go serving files at `/local-files/` endpoint.
  4. B9 specifies snapshot-window pattern (`server_now` captured before query, used as upper bound).
  5. C4 replaced with C3: `make dev` auto-detects mode, with explicit `make dev-local`/`make dev-cloud` overrides.
  6. D1 includes timestamp normalization rules: `<<TIMESTAMP>>` placeholders, ms truncation, ordering assertions.
