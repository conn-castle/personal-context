# Roadmap

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
A phased plan of work that guides architecture decisions and sequencing. The roadmap is the “what next” reference; the backlog holds unscheduled items.

## Format
- The roadmap is a single list of numbered phases under `<!-- PHASES START -->`.
- Do not renumber completed phases (phases marked with ✅).
- You may renumber incomplete phases when updating the roadmap (e.g., to insert a new phase).
- Incomplete phases include **Goal**, **Tasks** (checkbox list), and **Exit criteria** sections.
- When a phase is complete:
  - update the heading to: `## Phase N ✅ — <phase name>`
  - replace the phase content with a short bullet summary of what was accomplished (no checkbox list).

### Phase templates

Completed:
```markdown
## Phase N ✅ — <phase name>
- <Accomplishment summary bullet>
- <Accomplishment summary bullet>
```

Incomplete:
```markdown
## Phase N — <phase name>

### Goal
- <What success looks like for this phase, in 1–3 bullet points.>

### Tasks
- [ ] <Concrete deliverable-oriented task>
- [ ] <Concrete deliverable-oriented task>

### Exit criteria
- <Objective condition that must be true to call the phase complete.>
- <Prefer testable statements: “X exists”, “Y passes”, “Z is documented”.>
```

## Phases

<!-- PHASES START -->

## Phase 1 ✅ — Project Scaffolding
- Initialized runnable Go CLI scaffold in `cli/` with required dependency set, root command, schema contract checks, lint, and hard 95% coverage enforcement.
- Initialized Next.js App Router + TypeScript scaffold in `web/` with `@neondatabase/serverless`, lint/test/coverage gates (95%), and DB-free Playwright smoke e2e configuration.
- Added CI workflow (`.github/workflows/ci.yml`) covering schema contract checks, Go build/test/lint/coverage gates, and web lint/test/coverage/build/e2e checks.
- Added repository-level schema contract guard script (`scripts/check_schema_contract.sh`) enforcing canonical `schema/` usage and preventing workspace-local schema duplicates.
- Migrated `docs/requirements/` content into canonical sources (`schema/`, `docs/agent-layer/*`, `README.md`), documented migration evidence in `.agent-layer/tmp/phase1-requirements-migration-checklist.md`, and removed `docs/requirements/`.
- Added nightly export workflow example (`docs/nightly-export-workflow.example.yml`) and rewrote root `README.md` with architecture, setup, command reference, web overview, and contributor workflow.

## Phase 2 ✅ — Core Data Layer (Local)
- Implemented foundation libraries with test-first coverage: slide ID generation, fractional indexing, timezone utilities, config read/write + mode detection, and notes normalization.
- Added SQLite local data layer end-to-end: connection factory (`foreign_keys=ON`, WAL), executable migrations for all 5 tables with sync/timestamp triggers, backend-agnostic repository contract, SQLite repository implementation, and integration tests.
- Implemented local filesystem client for figures/data with path validation, copy/delete behavior, and error-case coverage.
- Restored `go mod tidy -diff` in CI and added per-package coverage enforcement (`>=95%`) in CI and CLI workflow commands.

## Phase 3 ✅ — Local CLI Foundation (Setup + CRUD)
- Implemented all 7 local CLI commands: `pc setup`, `pc add`, `pc show`, `pc edit`, `pc delete`, `pc restore`, `pc move` — registered in root.go with Cobra.
- E2e test suite (57 tests) runs compiled `pc` binary as subprocess: setup (4), add (11), show (5), edit (10), delete/restore (9), move (12), edge cases (6).
- Extensive in-process unit tests for Go coverage: add_test.go, commands_test.go, coverage_test.go, error_paths_test.go — covering happy paths, error injection via `resolveHomeDirFn`, DB corruption, permission failures, and trigger errors.
- Per-package coverage >=95% enforced (`internal/cli` at 95.3%). All tests pass, go vet clean.

## Phase 4 ✅ — Local CLI Features (Search, Trash, Projects)
- Implemented 5 new CLI commands: `pc search`, `pc trash`, `pc gc`, `pc project set/clear/list`, `pc doctor` — all registered in root.go.
- Extended `ListSlidesFilter` with `OnlyDeleted` and `Query` fields; added `ListDistinctProjectIDs` to Repository interface; extended Config with `ActiveProject`.
- Added filesystem methods: `BasePath`, `ListSlideIDsOnDisk`, `DeleteSlideDir` for gc and doctor operations.
- Active project integration: `pc add` uses active project when no `--project` flag or metadata.json project_id.
- E2e test suite expanded to 103 tests (from 57): search (14), trash (5), gc (9), project (12), doctor (5), workflow (1), plus existing tests.
- Full local-only workflow e2e test (`TestFullLocalWorkflow`) exercises all commands end-to-end.
- Consolidated SQLite schema: removed `cli/migrations/` package, embedded single canonical schema in `cli/internal/sqlite/sqlite_schema.sql`.
- Per-package coverage >=95% enforced. All packages pass. Linter clean.

## Phase 5 ✅ — Cloud Data Layer
- Implemented Postgres repository (`cli/internal/repository/postgres/`) — full 24-method pgx-based Repository with positional params, ILIKE, RETURNING, native `time.Time`, `mapPgError`, `ensureRowsAffected`. Embedded DDL via `schema.go` + `ApplySchema()`. 17 contract suite + 5 Postgres-specific integration tests (testcontainers-go, schema-per-test isolation) + 9 unit tests. 95.2% coverage.
- Implemented S3 client (`cli/internal/s3client/`) — `Upload`, `Download`, `Delete`, `Exists`, `HeadVersion`, `UpdateVersion` methods. DI constructor accepts pre-configured `*s3.Client` + bucket. Integration tests via testcontainers-go MinIO container (bucket-per-test isolation) + unit tests for error mapping. 98.6% coverage.
- Implemented cloud config validation (`cli/internal/config/validate.go`) — `ValidateNeonURL`, `ValidateS3Bucket`, `ValidateS3Region`, `ValidateCloudConfig`. 48 table-driven test cases. 95.1% coverage.
- Added CI schema equivalence guard (`scripts/check_schema_equivalence.sh`) — parses both schema files, compares tables, columns, indexes, and UNIQUE constraints. Added to CI workflow.
- Dependencies added: `aws-sdk-go-v2` + credentials + service/s3 + smithy-go, `testcontainers-go/modules/minio`.

## Phase 6 ✅ — Sync Engine + Cloud CLI
- Implemented bidirectional sync engine (`internal/sync/`) with push-then-pull conflict resolution (last-writer-wins, edit-wins-on-tie), child row matching by `(slide_id, filename)`, and file-based sync lock (`.pc/sync.lock`).
- Implemented sync session management (`internal/syncengine/`) with `last_sync_at` cursor captured at sync start, file lock for concurrent sync prevention, and cursor persistence.
- Implemented `pc sync`, `pc fetch` (slide ID / `--project` / `--recent` modes with `--output`), `pc setup` cloud path (interactive + non-interactive Neon/S3/AWS credential wizard), `pc setup --remove-cloud`.
- Implemented `pc doctor` cloud connectivity checks (WARN if unreachable, OK if reachable, skipped if local-only), `pc gc` cloud-aware (hard-deletes from cloud first to prevent sync re-creation, warns if cloud unreachable).
- Auto-sync (`runAutoSyncFn`) integrated into all 6 mutation commands: add, edit, delete, restore, move, gc. Failures are non-fatal (stderr warnings).
- 160+ sync/conflict unit tests, integration tests (testcontainers Postgres+MinIO), and e2e coverage for cloud-command validation plus local-only/no-op entrypoints. Per-package coverage >=95%.

## Phase 7 ✅ — Export/Import System
- Implemented deterministic git snapshot export/import support across local and cloud flows: `pc export`, `pc import`, `pc restore-db`, and `pc verify`, including LFS pointer rejection, import merge rules, and automatic restore backups.
- Added local and cloud verification coverage for all five conversion paths plus the Phase 7 edge matrix: deterministic exports, large/unicode payloads, soft-delete exclusion, overlap semantics, cloud-rooted round trips, and restore-db -> sync into a fresh cloud while preserving data-file metadata.
- Consolidated Postgres schema delivery into the embedded canonical schema file, removed the standalone Postgres migration artifact, and kept coverage gates above 95% for all tracked CLI packages.

## Phase 8 ✅ — v0.dev UI Design
- Designed full front-end UI with v0.dev, iterating on the design until all UI features were covered.
- Received final v0.dev reference project as zip file, extracted into the repository.

## Phase 9 — Web UI Integration (API, Logic, and v0.dev UI Adoption)

### Goal
- Production web UI with real API routes and business logic, wired to Neon and S3, with >95% coverage and Playwright e2e tests.
- **v0.dev constraint:** The v0.dev reference project is used *solely* as a visual/UI reference. Copy specific UI elements (components, layouts, styles) for visual parity. Do NOT copy or replicate any backend code, API routes, business logic, data fetching, or state management from v0.dev. All backend and logic are designed and implemented from scratch in this phase.

### Tasks
- [x] Deep-dive the v0.dev reference project (`tmp/v0-personal-context-design/`) — catalog its file structure, components, pages, data flow, and styling approach to inform which UI elements to adopt
- [x] Set up Neon serverless driver in Next.js
- [x] Set up S3 client for presigned URL generation (Amplify IAM role or env vars)
- [x] **Tests first**: Write API route tests before implementation — for each route, test:
    - GET /api/slides: pagination, project filter, deleted filter, updated_after filter, sort order, empty results
    - GET /api/slides/[id]: returns slide with figures and data files, 404 for nonexistent ID
    - PATCH /api/slides/[id]: updates project_id, notes, git_remote_url, git_hash; updated_at auto-bumped by trigger; updates sync_version; S3 _version bumped write-after with retry; rejects invalid ID
    - PATCH /api/slides/[id]/order: computes correct fractional index, updates updated_at and sync_version
    - DELETE /api/slides/[id]: sets deleted_at, updates sync_version
    - POST /api/slides/[id]/restore: clears deleted_at, updates sync_version
    - GET /api/sync/version: returns version from S3 _version (not Postgres)
    - GET /api/sync/changes: returns slides changed since timestamp, includes soft-deleted
    - GET /api/files presigned URLs: returns valid presigned URL, 404 for nonexistent file
    - GET /api/projects: returns distinct project_ids, excludes deleted slides
- [x] Implement all API routes
- [x] **Tests first**: Write `useSyncManager` unit tests before implementation — test each layer's trigger and cooldown behavior:
    - Layer 1 (manual): always fires, ignores cooldown
    - Layer 2 (interaction): respects 30s cooldown
    - Layer 3 (tab visibility): respects 30s cooldown
    - Layer 4 (idle polling): 60s when idle < 10 min, 5 min when idle > 10 min, stops when tab hidden
    - Version change triggers data fetch
    - Self-inflicted sync prevention: own mutations don't trigger unnecessary fetch
- [x] Implement `useSyncManager()` hook
- [x] Adopt UI components from v0.dev reference (visual/interaction parity only — no v0.dev backend, logic, or data layer code)
- [x] Wire UI components to real API routes
- [x] **Tests first**: Write component/unit utility tests for virtual date slide grouping, fractional index computation, and markdown rendering output
- [x] Write Playwright e2e tests:
    - Browse slides: slides render with date headers, slide count badge, navigation buttons
    - Filter by project: select project, only matching slides shown
    - Slide details: click slide, notes rendered as markdown, figures listed, data files listed with sizes
    - Edit slide: change notes, verify PATCH persists
    - Delete and restore: soft delete slide, verify optimistic removal, show deleted view with restore
    - Sync version display: sync version badge visible after interaction
    - Error states: API unavailable shows error banner, empty database shows placeholder
    - Load more pagination: cursor-based next-page loading
- [ ] **Local dev mode (`pc serve`)** — Go HTTP server that implements the same REST API as the Next.js routes, backed by local SQLite + filesystem. Next.js API routes detect `LOCAL_BACKEND_URL` and proxy to the Go server. Details:
    - **Architecture**: `pc serve` starts a Go HTTP server on `127.0.0.1:<port>`. It implements every `/api/*` endpoint using the existing Go `Repository` interface (SQLite) and local filesystem for figures/data files. Next.js API route handlers detect `LOCAL_BACKEND_URL` and delegate to `web/lib/local-proxy.ts`, which forwards requests to the Go server. When `LOCAL_BACKEND_URL` is unset, the existing Neon + S3 route logic stays active.
    - **Go server endpoints** (mirror web API 1:1):
        - `GET /api/slides` — paginated list (cursor, project filter, deleted filter, updated_after)
        - `GET /api/slides/:id` — single slide with figures and data files
        - `PATCH /api/slides/:id` — update project_id, notes, git fields
        - `PATCH /api/slides/:id/order` — reorder (fractional index)
        - `DELETE /api/slides/:id` — soft delete
        - `POST /api/slides/:id/restore` — restore
        - `GET /api/sync/version` — return sync_version from SQLite (static in local mode, no S3)
        - `GET /api/sync/changes?since=<ISO>` — changed slides since timestamp
        - `GET /local-files/:slide_id/figures/:filename` — serve local figure file
        - `GET /local-files/:slide_id/data/:filename` — serve local data file
        - `GET /api/files/:slideId/:fileType/:filename` — returns `{url, expires_at}` JSON (matching cloud presigned-URL shape)
        - `GET /api/projects` — distinct project_ids
    - **File serving**: Go serves figures/data files directly from the local filesystem (`~/personal-context/figures/` and `~/personal-context/data/`). The `/api/files` response returns JSON with a direct Go-server URL under `/local-files/...` instead of an S3 presigned URL.
    - **Sync behavior in local mode**: `GET /api/sync/version` returns the SQLite `sync_version` table value. The web UI's `useSyncManager` still polls, but version only changes when `pc` CLI mutates locally. No S3 involved.
    - **S3 `_version` format alignment**: JSON `{version, updated_at}` is now the canonical `_version` payload in cloud mode, with legacy bare-integer reads retained for compatibility.
    - **Startup**: `pc serve` reads the CLI config (`~/personal-context/.pc/config.json`), opens the SQLite DB, resolves the local data directory, and starts listening. `make dev` / `make dev-local` are responsible for launching Next.js alongside it.
    - **Security**: Bind to `127.0.0.1` only (never `0.0.0.0`). Validate file paths to prevent directory traversal. No authentication (local-only).
    - **Contract tests**: Shared test fixtures define inputs + expected outputs. Each test calls both the Next.js API route (against Neon/test DB) and the Go HTTP endpoint (against SQLite) with the same input, then asserts: (1) both responses are identical (parity), and (2) both match the expected output (correctness). Run in CI with testcontainers for Postgres.
    - **Web-side changes**: `web/lib/local-proxy.ts` plus the affected route handlers under `web/app/api/**` implement local-mode proxying. When `LOCAL_BACKEND_URL` is unset, the routes continue using Neon/S3 helpers directly.
- [ ] Build out real SettingsOverlay (theme, sync config, keyboard shortcuts, data management)
- [ ] Deploy to AWS Amplify

### Exit criteria
- All API routes pass test suite.
- `useSyncManager` passes unit tests for all 4 layers.
- All Playwright e2e tests pass.
- `pnpm test:coverage` reports >95%.
- `pc serve` starts Go HTTP server and `make dev` works without cloud credentials.
- Contract tests verify parity between Go and Node API implementations.
- Deployed and accessible on Amplify.

## Phase 10 — Deployment, CI/CD, and Integration Testing

### Goal
- Production-ready deployment. Full CI/CD. Full system e2e tests. Complete documentation.

### Tasks
- [ ] Configure Amplify env vars and IAM role for S3
- [ ] Set up full CI pipeline: Go test + coverage + lint, Next.js test + coverage + build + lint, Playwright e2e, coverage gates (>95% both)
- [ ] Create nightly export GitHub Action (example in docs/, real in data repo)
- [ ] Write full system e2e tests (CLI + cloud + web UI together):
    - CLI creates slide with figures -> `pc sync` -> web UI Playwright test verifies slide appears with figures
    - Web UI edits slide notes -> CLI runs `pc sync` -> CLI `pc show` verifies updated notes
    - CLI deletes slide -> sync -> web UI verifies slide gone from main view, appears in trash
    - Web UI restores slide from trash -> CLI syncs -> CLI verifies slide is active
    - Conflict: CLI edits slide offline, web UI edits same slide, CLI syncs -> later timestamp wins on both sides
- [ ] Test multi-machine sync simulation: two local databases syncing through Neon with conflict scenarios
- [ ] Test local-only mode e2e: full workflow without cloud configuration
- [ ] Verify all 5 data conversion paths with production-like data volume (500+ slides)
- [ ] Write comprehensive README (setup guide, CLI usage, web UI, architecture)
- [ ] Final review and update of all memory files

### Exit criteria
- Full system e2e tests pass (CLI -> cloud -> web UI round-trips).
- CI pipeline enforces >95% coverage on all code, fails build on regression.
- Nightly export Action runs successfully.
- Multi-machine sync tested with conflicts.
- Local-only mode fully functional.
- README complete and accurate.
