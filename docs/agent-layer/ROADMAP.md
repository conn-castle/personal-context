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
  - replace ALL phase content (Goal, Tasks, Task details, Exit criteria) with a concise bullet summary of what was accomplished (no checkbox list).
- **Archival:** When more than 5 completed phases exist, consolidate the oldest completed phases into a single `## Archived phases` summary. Keep the 5 most recently completed phases as individual entries. The archive section uses one line per phase.

### Phase templates

Archived (compact):
```markdown
## Archived phases (1–N)
- Phase 1 — <name>: <one-line summary>
- Phase 2 — <name>: <one-line summary>
```

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
- Implemented foundation libraries with test-first coverage: record ID generation, fractional indexing, timezone utilities, config read/write + mode detection, and notes normalization.
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
- Extended `ListRecordsFilter` with `OnlyDeleted` and `Query` fields; added `ListDistinctProjectIDs` to Repository interface; extended Config with `ActiveProject`.
- Added filesystem methods: `BasePath`, `ListRecordIDsOnDisk`, `DeleteRecordDir` for gc and doctor operations.
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
- Implemented bidirectional sync engine (`internal/sync/`) with push-then-pull conflict resolution (last-writer-wins, edit-wins-on-tie), child row matching by `(record_id, filename)`, and file-based sync lock (`.pc/sync.lock`).
- Implemented sync session management (`internal/syncengine/`) with `last_sync_at` cursor captured at sync start, file lock for concurrent sync prevention, and cursor persistence.
- Implemented `pc sync`, `pc fetch` (record ID / `--project` / `--recent` modes with `--output`), `pc setup` cloud path (interactive + non-interactive Neon/S3/AWS credential wizard), `pc setup --remove-cloud`.
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

## Phase 9 ✅ — Web UI Integration (API, Logic, and v0.dev UI Adoption)
- Production web UI with all API routes (records CRUD, sync, projects, files, info, stats, purge trash), `useSyncManager` 4-layer polling, Playwright e2e tests, and >95% coverage (99.16% statements, 95.28% branches).
- `pc serve` Go HTTP server implementing all REST API endpoints backed by local SQLite + filesystem. Next.js API routes detect `LOCAL_BACKEND_URL` and proxy to Go via `local-proxy.ts`. `make dev-local` orchestrates both servers.
- Contract parity tests (`contract-parity.test.ts`) verify all 13 shared endpoint response shapes match between Go and Node implementations.
- SettingsOverlay with sync status, data management (purge trash with AlertDialog confirmation), about info. `useLocalStorage` hook persists UI state (view mode, project filter, panel visibility, last selected record).
- v0.dev reference used solely for visual/UI parity; all backend, logic, and data layer implemented from scratch.

## Phase 10 ✅ — Authentication & Multi-User
- Auth.js v5 with Credentials provider and JWT sessions (90-day maxAge). Login and registration pages with shadcn/ui.
- Per-user data isolation: `users` and `api_keys` tables (Postgres only), `records.user_id` FK, per-user `sync_version`, S3 `users/{user_id}/` key prefix.
- All API routes protected via `requireUser()`: Bearer token (API key) or Auth.js session. Local mode (`pc serve`) bypasses auth.
- CLI API key system: `pc setup --init-cloud-schema` bootstraps a fresh Postgres database, `pc setup --api-key` stores the user API key, and `resolveUserIDFromAPIKey` validates against `api_keys` table. `openCloudStack` auto-resolves userID from config.
- API key CRUD: `POST /api/api-keys` (create), `GET /api/api-keys` (list), `DELETE /api/api-keys/[id]` (revoke). Registration gating via `REGISTRATION_ENABLED` env var.
- Tests: auth-helpers (7), password (3), register (8), api-keys CRUD (13), setup API key validation (2), resolveUserID (3). All Go and web tests pass. Aggregate Go coverage 95.5%.

## Phase 11 — Deployment, CI/CD, and Integration Testing

### Goal
- Production-ready deployment. Full CI/CD. Full system e2e tests. Complete documentation.

### Tasks
- [ ] Deploy to AWS Amplify
- [ ] Configure Amplify env vars and IAM role for S3
- [ ] Set up full CI pipeline: Go test + coverage + lint, Next.js test + coverage + build + lint, Playwright e2e, coverage gates (>95% both)
- [x] Add GitHub Release and Homebrew tap automation for the `pc` CLI
- [ ] Create nightly export GitHub Action (example in docs/, real in data repo)
- [ ] Write full system e2e tests (CLI + cloud + web UI together):
    - CLI creates record with figures -> `pc sync` -> web UI Playwright test verifies record appears with figures
    - Web UI edits record notes -> CLI runs `pc sync` -> CLI `pc show` verifies updated notes
    - CLI deletes record -> sync -> web UI verifies record gone from main view, appears in trash
    - Web UI restores record from trash -> CLI syncs -> CLI verifies record is active
    - Conflict: CLI edits record offline, web UI edits same record, CLI syncs -> later timestamp wins on both sides
- [ ] Test multi-machine sync simulation: two local databases syncing through Neon with conflict scenarios
- [ ] Test local-only mode e2e: full workflow without cloud configuration
- [ ] Verify all 5 data conversion paths with production-like data volume (500+ records)
- [ ] Write comprehensive README (setup guide, CLI usage, web UI, architecture)
- [ ] Final review and update of all memory files

### Exit criteria
- Deployed and accessible on Amplify.
- Full system e2e tests pass (CLI -> cloud -> web UI round-trips).
- CI pipeline enforces >95% coverage on all code, fails build on regression.
- Nightly export Action runs successfully.
- Multi-machine sync tested with conflicts.
- Local-only mode fully functional.
- README complete and accurate.
