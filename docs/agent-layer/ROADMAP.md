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
- Implemented Postgres repository (`cli/internal/repository/postgres/`) — full 24-method pgx-based Repository with positional params, ILIKE, RETURNING, native `time.Time`, `mapPgError`, `ensureRowsAffected`. Embedded DDL via `schema.go` + `ApplySchema()`. Migration file in `cli/migrations/postgres/001_initial_schema.sql`. 17 contract suite + 5 Postgres-specific integration tests (testcontainers-go, schema-per-test isolation) + 9 unit tests. 95.2% coverage.
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

## Phase 7 — Export/Import System

### Goal
- Full data portability. Two-tier integrity: Tier 1 (sync) fully lossless, Tier 2 (git export) lossless for DB fields + figures with data file references preserved.

### Tasks
- [ ] **Tests first**: Write conversion path tests before implementation — Tier 1 (full lossless) and Tier 2 (narrowed) guarantees:
    - **Tier 1** — Path A: Local + files -> sync -> Neon + S3 -> sync -> Local + files (all fields, all figures, all data files byte-for-byte)
    - **Tier 2** — Path B: Neon + S3 -> export -> git -> restore-db -> Neon (all fields + figures lossless; data file references preserved, binaries not in git)
    - **Tier 2** — Path C: Local + files -> export -> git -> restore-db -> Local (same as B from local)
    - **Tier 2** — Path D: Neon -> sync -> Local -> export -> git (full chain, narrowed at git boundary)
    - **Tier 2** — Path E: git -> import -> Local SQLite (merge import)
- [ ] **Tests first**: Write edge case tests covering: minimal slide, large slide (20+ figures, 100KB+ HTML, 50KB+ notes), unicode in HTML and notes, special characters in filenames, multiple slides same date with different day_order, slide date differs from created_at, soft-deleted slides excluded from export, empty database, `pc import` with overlapping IDs (same updated_at -> skip, older -> skip, newer -> update with full child row replacement)
- [ ] **Tests first**: Write determinism test — export same data twice, verify byte-for-byte identical output (JSON key order, array sorting)
- [ ] **Tests first**: Write LFS pointer detection test — create a file with LFS pointer format, verify import rejects it with clear error
- [ ] **Tests first**: Write `pc restore-db` safety test — verify auto-backup created before wipe, verify restore from backup possible
- [ ] Implement export engine, deterministic JSON serialization
- [ ] Implement `pc export`, `pc import`, `pc restore-db`, `pc verify`
- [ ] Implement LFS pointer detection

- [ ] **Consolidate schema**: Fold all SQLite and Postgres migration files into canonical schema files per backend and remove individual migrations (no deployed users, so no migration history to preserve)

### Exit criteria
- All 5 conversion paths pass round-trip tests with full field verification.
- All edge cases pass.
- Export is deterministic (identical output for identical data).
- LFS pointers detected and rejected.
- `pc restore-db` creates backup before wipe.
- Single canonical schema file per backend, no separate migration files (SQLite + Postgres).
- `go test -cover` reports >95% for all packages.

## Phase 8 — v0.dev UI Design

### Goal
- Complete front-end UI designed by v0.dev with mocked API calls. All UI work happens in this phase.
- Produce an API spec document that gives v0.dev full context for every UI interaction.

### Tasks
- [ ] Write API spec document for v0.dev: all endpoints, request/response TypeScript types, mock data examples, behavioral descriptions for every UI interaction
- [ ] Include in spec: slide viewer (16:9, sandboxed iframes, scaling), virtual date slides, project filter, slide detail panel (notes markdown, figures, data files with download, git_remote_url as clickable link, git_hash linkable to commit), editing (project_id, notes, git_remote_url, git_hash), intra-day drag-and-drop reorder, soft delete, trash view with restore, sync indicator/manual refresh, loading states, error handling, responsive layout
- [ ] Include in spec: mock data covering edge cases (slides with/without figures, notes, data files, multiple projects, different dates)
- [ ] Provide spec to v0.dev and iterate on the design
- [ ] Review v0.dev output for completeness against all UI requirements
- [ ] Receive final zip file from v0.dev

### Exit criteria
- API spec document covers every endpoint and UI interaction.
- v0.dev has produced a complete Next.js reference project with 100% of UI features.
- All UI components render correctly with mocked data.
- Reference project received as zip file.

## Phase 9 — Web UI Implementation (from v0.dev reference)

### Goal
- Production web UI rebuilt from v0.dev reference, wired to real backend, with >95% coverage and Playwright e2e tests.

### Tasks
- [ ] Set up Neon serverless driver in Next.js
- [ ] Set up S3 client for presigned URL generation (Amplify IAM role or env vars)
- [ ] **Tests first**: Write API route tests before implementation — for each route, test:
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
- [ ] Implement all API routes
- [ ] **Tests first**: Write `useSyncManager` unit tests before implementation — test each layer's trigger and cooldown behavior:
    - Layer 1 (manual): always fires, ignores cooldown
    - Layer 2 (interaction): respects 30s cooldown
    - Layer 3 (tab visibility): respects 30s cooldown
    - Layer 4 (idle polling): 60s when idle < 10 min, 5 min when idle > 10 min, stops when tab hidden
    - Version change triggers data fetch
    - Self-inflicted sync prevention: own mutations don't trigger unnecessary fetch
- [ ] Implement `useSyncManager()` hook
- [ ] Rebuild UI components from v0.dev reference (visual/interaction reference, not verbatim code)
- [ ] Wire UI components to real API routes
- [ ] **Tests first**: Write component unit tests — virtual date slide injection logic (on date change, after 10+ consecutive), fractional index computation for drag-and-drop, markdown rendering output
- [ ] Write Playwright e2e tests:
    - Browse slides: slides render in 16:9 containers, virtual date slides appear, scroll through list
    - Filter by project: select project, only matching slides shown, clear filter shows all
    - Slide details: click slide, notes rendered as markdown, figures listed, data files listed with sizes, download link works
    - Edit slide: change project_id, change notes, verify changes persist after page reload
    - Delete and restore: soft delete slide, verify it disappears from main view, open trash, verify it appears, restore, verify it returns
    - Drag-and-drop reorder: drag slide within same date, verify new order persists after reload
    - Sync detection: use API to simulate CLI adding a slide (insert directly into test DB + bump version), verify sync manager detects and slide appears without manual refresh
    - Error states: API unavailable, empty database (no slides)
- [ ] Deploy to AWS Amplify

### Exit criteria
- All API routes pass test suite.
- `useSyncManager` passes unit tests for all 4 layers.
- All Playwright e2e tests pass.
- `npm test -- --coverage` reports >95%.
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
