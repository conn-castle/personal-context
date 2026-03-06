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

## Phase 2 — Core Data Layer (Local)

### Goal
- SQLite database fully operational with all 5 tables.
- Local filesystem operations for figures and data files.
- Foundation libraries (config, slide ID generator, fractional indexing, timezone utilities).
- All code test-first with >95% coverage.

### Tasks
- [ ] **Tests first**: Write test cases for slide ID generation (format, uniqueness, date extraction, crypto/rand usage) before implementation
- [ ] **Tests first**: Write test cases for fractional indexing (insert-at-start, insert-at-end, insert-between, lexicographic ordering, repeated insertions between same positions) before implementation
- [ ] **Tests first**: Write test cases for timezone utilities (local-to-UTC, UTC-to-local, "today" in various timezones, microsecond precision round-trip) before implementation
- [ ] **Tests first**: Write test cases for config module (read/write, cloud vs local-only detection, missing file, corrupt file) before implementation
- [ ] **Tests first**: Write test cases for notes normalization (NULL, empty string, whitespace-only, valid markdown) before implementation
- [ ] Re-enable `go mod tidy -diff` in `.github/workflows/ci.yml` and restore it in `COMMANDS.md` after predeclared CLI dependencies become actively imported in this phase
- [ ] Implement config module: read/write `~/personal-context/.pc/config.json` (0600 permissions), detect cloud vs local-only
- [ ] Implement SQLite connection factory: `PRAGMA foreign_keys = ON`, WAL mode, connection wrapping
- [ ] Create SQLite migrations in `cli/migrations/sqlite/` (all 5 tables + sync_version triggers + auto_update_updated_at trigger + UNIQUE constraints on (slide_id, filename))
- [ ] Define `Repository` interface: CRUD for all 5 tables, query methods, soft delete
- [ ] Implement slide ID generator, fractional indexing, timezone utilities, notes normalization
- [ ] **Tests first**: Write SQLite repository integration tests before implementation — CRUD for all 5 tables, soft delete, cascading delete, sort order `(date, day_order, id)`, UNIQUE constraint violations on `(slide_id, filename)`, foreign key rejection
- [ ] Implement SQLite repository (all `Repository` interface methods)
- [ ] **Tests first**: Write filesystem client tests (copy figures, copy data files, delete, path resolution, special characters in filenames, missing directories) before implementation
- [ ] Implement local filesystem client

### Exit criteria
- All foundation libraries pass their test-first test suites.
- SQLite repository passes full integration test suite: CRUD, soft deletes, cascading deletes, sort order, UNIQUE constraints, foreign key enforcement.
- Filesystem client handles happy paths and error cases.
- `go test -cover` reports >95% for all packages in this phase.

## Phase 3 — Local CLI Foundation (Setup + CRUD)

### Goal
- Usable local-only CLI: user can set up, create, view, edit, delete, restore, and reorder slides.
- CLI e2e tests run the actual `pc` binary as a subprocess.

### Tasks
- [ ] **Tests first**: Write e2e test for `pc setup` (local-only) before implementation — verify directory creation, SQLite initialized, templates seeded, idempotent re-run
- [ ] Implement `pc setup` (local-only path)
- [ ] **Tests first**: Write e2e tests for `pc add` before implementation — valid folder, missing slide.html, metadata.json merge with flags, figure/data file copy verified on disk, SHA-256 hash verified, day_order generated correctly, --project flag, --date flag, --position flag
- [ ] Implement `pc add <path>`
- [ ] **Tests first**: Write e2e tests for `pc show` before implementation — text format output, json format output, nonexistent ID error
- [ ] Implement `pc show <id>`
- [ ] **Tests first**: Write e2e tests for `pc edit` before implementation — full replacement verified (html_content, notes, figures, data files all replaced), old figures removed from disk, id/date/day_order/created_at preserved, updated_at changed
- [ ] Implement `pc edit <id> <path>`
- [ ] **Tests first**: Write e2e tests for `pc delete` / `pc restore` before implementation — deleted_at set/cleared, updated_at auto-bumped by trigger on both operations, slide excluded from/included in normal queries
- [ ] Implement `pc delete <id>` and `pc restore <id>`
- [ ] **Tests first**: Write e2e tests for `pc move` before implementation — date change resets day_order, position flags (after:ID, before:ID, first, last) produce correct ordering, only moved slide updated
- [ ] Implement `pc move <id>`
- [ ] Write e2e edge case tests: minimal slide (no figures/notes/data/project), slide with 10+ figures, special characters in filenames, unicode in HTML and notes

### Exit criteria
- All CLI commands pass e2e tests (binary run as subprocess, stdout/stderr/exit code verified, DB and filesystem state verified).
- Edge cases covered.
- `go test -cover` reports >95% for all packages.

## Phase 4 — Local CLI Features (Search, Trash, Projects)

### Goal
- Complete local CLI feature set. Full local-only workflow e2e tested.

### Tasks
- [ ] **Tests first**: Write e2e tests for `pc search` before implementation — matches in html_content, notes, project_id; --format table/ids/json output verified; --limit; --project filter; --deleted flag includes soft-deleted; no results returns empty; case-insensitive matching
- [ ] Implement `pc search <query>`
- [ ] **Tests first**: Write e2e tests for `pc trash` before implementation — lists only soft-deleted slides, shows id/date/deleted_at, empty trash returns clean message
- [ ] Implement `pc trash`
- [ ] **Tests first**: Write e2e tests for `pc gc` before implementation — only deletes trash older than 30 days, leaves younger trash alone, cascades child rows, removes figure/data files from disk, verify with `pc trash` after gc
- [ ] Implement `pc gc`
- [ ] **Tests first**: Write e2e tests for `pc project` before implementation — set stores in config, clear removes, list returns distinct project_ids from DB, active project used by `pc add` when --project not specified
- [ ] Implement `pc project set/clear/list`
- [ ] **Tests first**: Write e2e tests for `pc doctor` before implementation — healthy system passes, missing DB detected, orphaned figures detected
- [ ] Implement `pc doctor` (local checks)
- [ ] Write full-workflow e2e test: `pc setup` -> `pc project set` -> `pc add` (x3 different dates/projects) -> `pc search` -> `pc edit` -> `pc move` -> `pc delete` -> `pc trash` -> wait simulation -> `pc gc` -> `pc doctor`

### Exit criteria
- All local CLI commands pass e2e tests.
- Full local-only workflow e2e test passes.
- `go test -cover` reports >95% for all packages.

## Phase 5 — Cloud Data Layer

### Goal
- Postgres repository and S3 client operational, tested to same standard as local layer.

### Tasks
- [ ] Create Postgres migrations in `cli/migrations/postgres/` (all 5 tables + sync_version triggers + auto_update_updated_at trigger + UNIQUE constraints)
- [ ] **Tests first**: Run the same Repository interface test suite against Postgres implementation (test suite written in Phase 2 should be backend-agnostic)
- [ ] Implement Postgres repository using pgx
- [ ] **Tests first**: Write S3 client integration tests before implementation — upload then download round-trip (byte-for-byte), delete then verify gone, Exists returns correct bool, HeadVersion/UpdateVersion round-trip, upload to nonexistent bucket fails clearly
- [ ] Implement S3 client wrapper
- [ ] **Tests first**: Write cloud config validation tests — valid Neon URL succeeds, invalid URL fails with clear error, valid S3 bucket succeeds, nonexistent bucket fails with clear error
- [ ] Implement cloud config validation

### Exit criteria
- Postgres repository passes identical test suite as SQLite repository.
- S3 client passes all integration tests.
- Cloud config validation correctly distinguishes valid/invalid configurations.
- `go test -cover` reports >95% for all packages.

## Phase 6 — Sync Engine + Cloud CLI

### Goal
- Bidirectional sync working. Auto-sync after CLI writes. All cloud CLI commands operational.
- Sync correctness proven by comprehensive test suite covering all conflict scenarios.

### Tasks
- [ ] **Tests first**: Write sync engine unit tests before implementation — define test cases for every conflict scenario from the spec:
    - Push: new slide inserts into Neon
    - Pull: new Neon slide inserts into local
    - Edit on one side only: other side updated
    - Edit same slide on both: later `updated_at` wins
    - Delete vs edit: compare `deleted_at` vs `updated_at`, most recent wins
    - Timestamp tie: edit wins over delete
    - Resurrection: edit wins over delete -> `deleted_at` cleared to NULL
    - Restore visibility: `pc restore` bumps `updated_at` via trigger, visible to sync predicate `updated_at >= last_sync_at`
    - Child row matching by `(slide_id, filename)` — verify no duplicates after round-trip
    - `last_sync_at` captured at start: changes during sync window are not missed
- [ ] **Tests first**: Write sync e2e tests before implementation — two SQLite databases + one Postgres, simulate:
    - Clean push/pull with figures
    - Concurrent edits on different slides (no conflict)
    - Concurrent edits on same slide (timestamp wins)
    - Delete on one machine, edit on another
    - Auto-sync after `pc add` with cloud configured
    - Auto-sync failure (unreachable Neon): command succeeds with warning
    - `pc sync` without cloud configured: error
- [ ] Implement sync engine core: push/pull, conflict resolution, child row matching
- [ ] Implement `last_sync_at` capture at sync start
- [ ] Implement file lock (`.pc/sync.lock`) with test for concurrent sync prevention
- [ ] Implement auto-sync wrapper with non-fatal failure handling
- [ ] Implement `pc sync`, `pc fetch`
- [ ] Implement `pc setup` (cloud path): Neon URL + S3 bucket/region + AWS key prompts, write credentials to `~/.aws/credentials` [personal-context] profile, merge preview, table creation, non-interactive mode. `config.json` stores profile name + Neon URL + bucket/region (no AWS keys).
- [ ] Implement `pc setup --remove-cloud` (removes config + [personal-context] profile from ~/.aws/credentials)
- [ ] Implement `pc doctor` (cloud checks), `pc gc` (cloud-aware)
- [ ] Update all mutation commands to call auto-sync
- [ ] Write e2e tests for `pc setup` cloud path (interactive and non-interactive), `pc fetch`, `pc doctor` cloud checks, `pc gc` cloud-aware behavior

### Exit criteria
- All sync conflict scenarios pass test suite.
- Two-database sync e2e tests pass (push, pull, conflicts, resurrection, partial failure).
- Auto-sync triggers correctly, fails gracefully.
- File lock prevents concurrent sync.
- `go test -cover` reports >95% for all packages.

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

### Exit criteria
- All 5 conversion paths pass round-trip tests with full field verification.
- All edge cases pass.
- Export is deterministic (identical output for identical data).
- LFS pointers detected and rejected.
- `pc restore-db` creates backup before wipe.
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
