# Context

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Persistent project-specific knowledge that does not belong in ISSUES, BACKLOG, ROADMAP, DECISIONS, or COMMANDS. Read this file before starting work on a task.

Record three categories of information here:
1. **Project context** — domain concepts, architectural invariants, naming conventions, external dependencies, environment setup notes, team norms, and any other stable facts an agent needs to work effectively in this repository.
2. **Project-specific nuances** — non-obvious behaviors, implicit conventions, or user-provided clarifications that an agent would not discover from reading the code alone. When a user corrects a misunderstanding or explains how something actually works in this project, record it here.
3. **Lessons learned** — repeated mistakes, surprising behaviors, non-obvious gotchas, and corrective patterns discovered during development. When an error recurs or a workaround is needed more than once, record it here so future agents avoid the same mistake.

Do not duplicate information that belongs in other memory files:
- Deferred bugs or tech debt → ISSUES.md
- Planned features → BACKLOG.md
- Workflow commands → COMMANDS.md
- Non-obvious decisions → DECISIONS.md
- Phased plans → ROADMAP.md

## Format
- Organize by topic using headings (`##`, `###`).
- Prefer concise bullet points. State facts directly; omit hedging language.
- Before adding an entry, search this file for existing coverage. Merge into or update an existing section instead of creating a near-duplicate.
- Remove or update entries when the underlying facts change.
- Insert all content below `<!-- ENTRIES START -->`.

<!-- ENTRIES START -->

> **Status:** Phase 6 (Sync Engine & Cloud CLI) is complete. Bidirectional sync engine, cloud setup wizard, all 15 CLI commands are implemented, auto-sync in mutation commands is operational, and cloud-aware gc/doctor are in place. `pc sync` and `pc fetch` require cloud configuration. See ROADMAP.md for phase-by-phase implementation progress.

## Project Overview

Personal Context (`pc`) is a personal engineering notebook system that stores work as individual HTML slides — images, text, tables, code, and data — organized chronologically and by project. Designed for 10+ year lifespan, accumulating 1,000-2,000 slides per year.

Replaces yearly Google Slides decks with a database-backed, agent-friendly, multi-device, browsable system.

## Monorepo Structure

```
personal-context/          # code repo
├── cli/                   # Go CLI
├── web/                   # Next.js app (App Router)
├── schema/                # schema.sql (DDL source of truth), schema-types.ts
└── docs/                  # README, example GitHub Action workflow
```

Separate **data repo**: nightly git export of slide data (metadata.json, slide.html, notes.md, figures via Git LFS). GitHub Action for nightly export lives there.
- Scheduled nightly at `0 4 * * *` UTC.
- Workflow downloads the `pc` binary from code-repo releases, then runs `pc export --from-cloud --path . --github-remote origin`.
- The code repository stores an example workflow under `docs/` for users to copy into their data repo.

## Architecture: Three Data States

```
Local SQLite + local files      <-- CLI writes here (always, every machine)
     | pc sync (bidirectional, if cloud configured)
Neon Postgres + S3              <-- cloud source of truth (web UI reads/writes)
     | nightly export (cloud -> GitHub, via GitHub Action on data repo)
GitHub + S3                     <-- portable backup (git clone + S3 = full restore)
```

Any state can be reconstructed from any other (subject to two-tier guarantee):
- Postgres <-- git export via `pc restore-db` then `pc sync` (Tier 2: data file binaries require S3; soft-deleted slides not in export)
- Local SQLite <-- Postgres via `pc sync` (Tier 1: fully lossless)
- Git export <-- Postgres via `pc export` (Tier 2: data file binaries stay in S3; soft-deleted excluded)
- Postgres <-- local SQLite via `pc sync` push phase (Tier 1: fully lossless)

### Source of Truth
- **Cloud configured**: Neon Postgres + S3 is cloud source of truth. Web UI reads/writes here.
- **Local-only mode**: Local SQLite + local files only. No web UI.

## Data Model (5 Tables)

| Table | PK | Purpose |
|-------|-----|---------|
| `slides` | `id` TEXT (`YYYYMMDD-8hex`) | HTML content, notes, project_id, git_remote_url, git_hash, date, day_order, soft delete |
| `slide_figures` | `id` auto-increment | Image refs: filename, s3_key, alt_text. FK -> slides CASCADE |
| `slide_data_files` | `id` auto-increment | Data file refs: filename, s3_key, size, SHA-256 hash, description. FK -> slides CASCADE |
| `templates` | `name` TEXT | HTML templates for slide creation. Hardcoded, seeded by `pc setup` |
| `sync_version` | `id` (always 1) | Single-row version counter, auto-incremented by triggers |

### Key Fields and Invariants
- **Slide ID**: `{YYYYMMDD}-{8-random-hex}` from `crypto/rand` (e.g., `20250304-a3f2b7e1`). Date prefix matches the slide's `date` field (UTC-normalized).
- **Sort key**: `ORDER BY (date, day_order, id)` — always deterministic.
- **day_order**: Fractional index string (Figma's algorithm, safe characters only). Lexicographic sort. Reordering updates only the moved slide.
- **project_id**: Slash-convention string (e.g., `"happy-ai/sleep-staging"`). No project table in MVP.
- **s3_key**: Canonical relative path (e.g., `figures/20250304-a3f2b7e1/loss-curve.png`). Same value for both S3 and local filesystem, regardless of mode.
- **git_remote_url**: Optional. Git remote URL (e.g., `https://github.com/org/repo`). Set via `metadata.json` only — no CLI flags. Displayed as clickable link in web UI.
- **git_hash**: Optional. Full 40-character SHA-1 commit hash. Set via `metadata.json` only. In web UI, linkable to `{git_remote_url}/commit/{git_hash}` when both present.
- **deleted_at**: NULL = active, non-NULL = soft-deleted. `WHERE deleted_at IS NULL` on all normal queries.
- **notes**: Full markdown. Empty string normalized to NULL at write time. `has_notes` = `notes IS NOT NULL`.
- **No title. No tags.** Organization is by project and date only.

### Figure References in HTML
- `html_content` references figures as `figures/{filename}` (relative path, no slide_id — implicit from context).
- Each rendering context resolves: web UI iframe rewrites to presigned URLs via `GET /api/files/{slide_id}/figures/{filename}`; git export matches naturally (`./figures/{filename}` relative to slide folder).
- `pc add`/`pc edit` validate that every `figures/` src in HTML has a matching file in the input folder.
- External URLs (`https://...`) pass through unchanged. Data files are attachments, not referenced in HTML.

### Schema Portability (Postgres / SQLite)
- `schema/schema.sql` is the design-level source of truth (Postgres dialect). The canonical SQLite schema is embedded in `cli/internal/sqlite/sqlite_schema.sql` and applied via `Connection.ApplySchema()`.
- Postgres schema embedded in `cli/internal/repository/postgres/postgres_schema.sql` and applied via `ApplySchema()`. Migration file: `cli/migrations/postgres/001_initial_schema.sql`.
- CI schema equivalence guard (`scripts/check_schema_equivalence.sh`) prevents structural drift between the two schemas.
- `created_at` and `updated_at` are DB-managed via defaults and triggers. `deleted_at` set by application code. See "DB-Managed Timestamps" below.
- `PRAGMA foreign_keys = ON` required on every SQLite connection (otherwise `ON DELETE CASCADE` silently ignored).
- SQLite WAL mode enabled for concurrent reads.
- Triggers reimplemented in SQLite syntax (per-row instead of per-statement).
- `TIMESTAMPTZ` stored as ISO 8601 text with `Z` suffix in SQLite.
- Child rows (`slide_figures`, `slide_data_files`) matched by `(slide_id, filename)` during sync, NOT by auto-increment `id`.

## Timezone Rules

- All timestamps (`created_at`, `updated_at`, `deleted_at`) stored as UTC. `TIMESTAMPTZ` in Postgres, ISO 8601 `Z` suffix in SQLite/JSON.
- `date` field is a local calendar date (`YYYY-MM-DD`) with no time component. "Today" = user's local timezone at creation time.

### DB-Managed Timestamps
- `created_at`: `DEFAULT NOW()` on INSERT. Never modified by triggers.
- `updated_at`: `DEFAULT NOW()` on INSERT. BEFORE UPDATE trigger auto-bumps to `NOW()` when value not explicitly changed (`NEW.updated_at = OLD.updated_at`). Any normal UPDATE (edit, delete, restore) automatically bumps `updated_at`.
- `deleted_at`: Set explicitly by application code. The `updated_at` trigger auto-fires on the same UPDATE.
- **Sync/import bypass**: Set explicit `updated_at` in the statement → trigger detects `NEW.updated_at != OLD.updated_at` → skips auto-bump, preserving original timestamp.
- SQLite uses AFTER UPDATE trigger (different syntax, same semantics). SQLite gets millisecond precision; Postgres gets microsecond.
- **Precision:** Millisecond is the effective minimum. Postgres timestamps are truncated to millisecond when syncing to SQLite. All sync and polling cursors use `>=` (not `>`) to prevent boundary misses. UPSERT makes re-processing safe.

## Sync Mechanism

### CLI (Local-First)
- CLI always writes to local SQLite + local files.
- If cloud configured: auto-sync after each mutation (`pc add`, `pc edit`, `pc delete`, `pc restore`, `pc move`). Failure is non-fatal (prints warning, exit code 0). Exception: `pc gc` orchestrates its own cloud-first-then-local deletion directly, not via auto-sync.
- If no cloud: writes succeed locally, sync silently skipped.
- `pc sync` (explicit): full bidirectional push-then-pull. Errors if no cloud configured.

### Bidirectional Sync Protocol
1. Acquire file lock (`.pc/sync.lock`) to prevent concurrent sync.
2. Capture `last_sync_at` at sync **start** (not end).
3. **Push**: local slides where `updated_at >= last_sync_at` -> UPSERT into Neon + upload figures to S3. (No `deleted_at` check needed — trigger auto-bumps `updated_at`.)
4. **Pull**: Neon slides where `updated_at >= last_sync_at` -> UPSERT into local + download figures.
5. Update `last_sync_at` only after both phases complete fully.
6. Release lock.

### Conflict Resolution
- Most-recent-action-wins using wall-clock timestamps.
- Delete vs edit: compare `deleted_at` vs `updated_at`. Most recent wins. On resurrection (edit wins), clear `deleted_at` to NULL.
- Timestamp tie: edit wins over delete (deterministic tiebreaker).

### Child Row Sync (Critical Invariants)
- `slide_figures` and `slide_data_files` auto-increment PKs diverge between Postgres and SQLite. **Sync must match child rows by `(slide_id, filename)`, NOT by `id`.**
- Child rows are **only modified as part of a parent slide operation** (`pc add`, `pc edit`, sync). Never independently. The parent slide's `updated_at` is the authoritative change signal. The `sync_version` triggers on child tables may cause harmless false positives during sync. If independent child modification commands are ever added, a cross-table trigger to bump parent `updated_at` must be added.

### Web UI Sync (Smart Layered Polling)
Four layers, 30-second global cooldown. All version checks go through `GET /api/sync/version` (Next.js reads S3 `_version` server-side, NOT Postgres — keeps Neon asleep on free tier).
- Layer 1: Manual refresh (ignores cooldown)
- Layer 2: Interaction-driven (clicks/navigation, subject to cooldown)
- Layer 3: Tab visibility (subject to cooldown)
- Layer 4: Idle polling (idle < 10 min: every 60s, idle > 10 min: every 5 min, tab hidden: nothing)

S3 `_version` is bumped write-after (only after Postgres commit succeeds, retry up to 3 times). Never write-ahead — prevents race where a client polls, sees the bump, queries Postgres, and gets stale data.

On version change: query Neon for slides with `updated_at >= last_known_timestamp`. If a version bump produces no incremental changes, perform full reconciliation (hard-deleted rows are invisible to incremental queries).

## Directory Structures

### Local
```
~/personal-context/
├── .pc/
│   ├── config.json           # Neon URL, S3 bucket/region, aws_profile name, active project, optional s3_endpoint/s3_force_path_style (0600, no AWS keys). Cloud mode detected by presence of neon_url + aws_profile.
│   ├── pc.db                 # local SQLite
│   ├── last_sync             # timestamp of last cloud sync
│   └── sync.lock             # file lock for concurrent sync prevention
├── figures/{slide_id}/{filename}
└── data/{slide_id}/{filename}  # sparse, on demand
```

### S3
```
s3://personal-context-prod/
├── figures/{slide_id}/{filename}
├── data/{slide_id}/{filename}
└── _version                    # sync heartbeat
```

### Git Export
```
personal-context-data/
├── templates/*.html
└── slides/{slide_id}/
    ├── metadata.json           # SlideExport (no HTML, no notes text)
    ├── slide.html              # html_content
    ├── notes.md                # only if has_notes
    └── figures/                # Git LFS
```
Data files stay in S3 only; `metadata.json` lists what exists. Soft-deleted slides excluded from export. Export must be deterministic (consistent JSON key order, sorted arrays) for clean git diffs.

## Technology Stack

| Component | Technology |
|-----------|-----------|
| CLI | Go: cobra, modernc.org/sqlite (pure Go), fracdex (fractional indexing), pgx (direct, not database/sql), aws-sdk-go-v2 (+credentials, +service/s3, +smithy-go), testcontainers-go (integration tests). Custom migration runner in `cli/internal/sqlite/` (no golang-migrate). |
| Web UI | Next.js App Router, React, sandboxed iframes for slide HTML rendering |
| Web hosting | AWS Amplify (SSR via Lambda, us-east-1) |
| DB (cloud) | Neon Postgres (provider-portable). @neondatabase/serverless HTTP driver for Lambda |
| DB (local) | SQLite via modernc.org/sqlite (pure Go, no CGO) |
| Storage (cloud) | AWS S3 (Intelligent-Tiering, us-east-1). Presigned URLs for web downloads |
| Storage (local) | Local filesystem |
| Backup | GitHub nightly export + Git LFS |

### Go Repository Pattern
- `Repository` interface with separate SQLite and Postgres implementations.
- SQL dialects diverge enough that sharing query code via `database/sql` is a false economy.
- **SQLite** (`cli/internal/sqlite/`): modernc.org/sqlite (pure Go), `?` positional params, `LIKE` for search, text timestamps. Custom migration runner with single embedded `sqlite_schema.sql`.
- **Postgres** (`cli/internal/repository/postgres/`): pgx (direct, not database/sql), `$N` positional params, `ILIKE` for case-insensitive search, `RETURNING` clauses, native `time.Time`. Embedded DDL via `postgres_schema.sql` + `ApplySchema()`. Migration file in `cli/migrations/postgres/001_initial_schema.sql`.
- **Contract tests** (`cli/internal/repository/repositorytest/`): backend-agnostic test suite run against both SQLite and Postgres implementations.
- **Integration tests**: testcontainers-go for both backends — Postgres uses schema-per-test isolation, SQLite uses temp files.

### S3 Client
- `cli/internal/s3client/` — thin wrapper over AWS SDK v2 `*s3.Client`.
- Methods: `Upload`, `Download`, `Delete`, `Exists`, `HeadVersion`, `UpdateVersion`.
- Constructor accepts pre-configured `*s3.Client` + bucket (DI pattern — no credential logic in the package).
- `HeadVersion` returns 0 for missing `_version` key (simplifies sync bootstrap).
- `mapS3Error` and `isNotFoundError` helpers for consistent error handling.
- Integration tests use testcontainers-go with MinIO container (bucket-per-test isolation).

### Cloud Config Validation
- `cli/internal/config/validate.go` — `ValidateNeonURL` (postgres:// scheme + host), `ValidateS3Bucket` (S3 naming rules), `ValidateS3Region` (AWS region format), `ValidateCloudConfig` (composite).

### Schema Equivalence Guard
- `scripts/check_schema_equivalence.sh` — CI script comparing Postgres (`schema/schema.sql`) and SQLite (`cli/internal/sqlite/sqlite_schema.sql`) schemas for structural equivalence: tables, columns, indexes, UNIQUE constraints. Does not compare types, CHECK expressions, or triggers (intentionally dialect-specific).
- `scripts/verify_phase5_demo.sh` — repository-level Phase 5 demo runner: executes schema contract/equivalence checks, cloud config validation tests, and Docker-backed Postgres/S3 integration suites.

## CLI Commands

### Setup & Health
- `pc setup` — first-time or reconfigure (idempotent, interactive/non-interactive, `--remove-cloud`)
- `pc doctor` — health checks (DB readability, orphaned figure/data directories, missing local figure/data files; cloud connectivity WARN if configured but unreachable)

### Slide CRUD
- `pc add <path>` — create slide from folder (`slide.html` required, `metadata.json` for project_id/git_remote_url/git_hash)
- `pc edit <id> <path>` — full replacement of content, notes, figures, data files, git fields (`updated_at` auto-bumped by trigger)
- `pc delete <id>` — soft-delete
- `pc restore <id>` — un-delete
- `pc move <id>` — change date or position
- `pc show <id>` — display metadata (including git fields), notes, figures, data files (`--format text|json`)

### Trash
- `pc trash` — list soft-deleted slides
- `pc gc` — hard-delete trash > 30 days (cloud-first if configured: deletes from Neon before local to prevent sync re-creation, warns if cloud unreachable, removes local figure/data files, runs auto-sync)

### Search & Projects
- `pc search <query>` — LIKE/ILIKE on html_content, notes, project_id (not git fields)
- `pc project set|clear|list` — manage active project

### Sync & Data
- `pc sync` — bidirectional cloud sync (errors if no cloud)
- `pc fetch` — download data files from S3
- `pc export` — DB to git folder format
- `pc import <path>` — merge from git export (update if newer, else skip; full replacement of child rows)
- `pc restore-db <path>` — rebuild DB from git export (destructive, auto-backup before wipe)
- `pc verify` — full round-trip data integrity tests

### `pc fetch` modes and flags
- Slide mode:
  - `pc fetch <slide_id>`
  - Downloads all data files for one slide into `~/personal-context/data/{slide_id}/` by default.
- Project mode:
  - `pc fetch --project "org/project"`
  - Downloads data files for all slides in a project.
- Recent-window mode:
  - `pc fetch --recent 3m`
  - Downloads data files for slides inside a relative time window (`d`, `w`, `m`, `y` suffixes).
- Output override:
  - `--output "./target-dir"`
  - Writes downloads under the provided directory instead of the default data path.
- Preconditions:
  - Cloud must be configured. If cloud is not configured, `pc fetch` fails loudly (no silent local fallback).

### `pc export` nightly cloud backup usage
- Manual/local usage:
  - `pc export --path ./pc-export --github-remote origin`
- Nightly data-repo workflow usage:
  - `pc export --from-cloud --path . --github-remote origin`
  - Reads slide rows from Neon and file blobs from S3 using repository secrets.

### `pc setup` flow details
- Non-interactive mode:
  - `pc setup --neon-url=... --s3-bucket=... --s3-region=... --aws-key=... --aws-secret=...`
  - Requires all cloud flags together; missing values fail with an explicit missing-flag list.
  - No merge-preview prompts in non-interactive mode.
- Remove-cloud mode:
  - `pc setup --remove-cloud`
  - Removes cloud config from `~/personal-context/.pc/config.json` and removes `[personal-context]` from `~/.aws/credentials`.
  - Does not delete local/cloud data.
- Interactive mode sequence:
  - Local checks and creation (SQLite + template seeding).
  - Prompt to configure cloud.
  - Prompt for Neon URL, S3 bucket, S3 region, AWS access key, AWS secret key.
  - Validate Neon connectivity and S3 bucket write access in memory first.
  - Show merge preview (local count, cloud count, post-sync count).
  - On confirmation: ensure Neon tables exist, write `[personal-context]` AWS profile, write `config.json` with mode metadata (no AWS secret material).
- No partial-success writes:
  - If Neon/S3 validation fails, `pc setup` writes nothing and exits with a clear error.

## Web UI (MVP)

- 16:9 slide viewer with sandboxed iframes, `transform: scale()`, white background
- Virtual date slides injected at render time
- Filter by project
- Intra-day drag-and-drop reorder (cross-date deferred to post-MVP)
- View notes (markdown), figures, data files with sizes and download
- Edit project_id, notes, git_remote_url, git_hash
- Soft delete + trash view with restore
- 4-layer sync polling via `useSyncManager()` hook

### API Routes
- `GET /api/slides` — list (paginated, filtered)
- `GET /api/slides/[id]` — single slide with figures and data files
- `PATCH /api/slides/[id]` — edit project_id, notes, git_remote_url, git_hash
- `PATCH /api/slides/[id]/order` — reorder (fractional index)
- `DELETE /api/slides/[id]` — soft delete
- `POST /api/slides/[id]/restore` — restore
- `GET /api/sync/version` — reads S3 `_version` (not Postgres)
- `GET /api/sync/changes?since=<ISO>` — changed slides
- `GET /api/files/[slide_id]/data/[filename]` — presigned download URL
- `GET /api/files/[slide_id]/figures/[filename]` — presigned figure URL
- `GET /api/projects` — distinct project_ids
- No slide creation endpoint (CLI only)

### API payload shapes

```ts
type ErrorResponse = {
  error: string;
  code: string;
};

type SlideSummary = {
  id: string;
  date: string; // YYYY-MM-DD
  day_order: string;
  project_id: string | null;
  updated_at: string; // ISO 8601 UTC
  deleted_at: string | null;
  figure_count: number;
  data_file_count: number;
};

type SlideFile = {
  filename: string;
  s3_key: string;
  size?: number;
  hash?: string;
  alt_text?: string | null;
  description?: string | null;
};

type SlideDetail = {
  id: string;
  date: string;
  day_order: string;
  html_content: string;
  notes: string | null;
  project_id: string | null;
  git_remote_url: string | null;
  git_hash: string | null;
  created_at: string;
  updated_at: string;
  deleted_at: string | null;
  figures: SlideFile[];
  data_files: SlideFile[];
};
```

- `GET /api/slides`
  - Query params:
    - `limit` (number, default 20)
    - `cursor` (opaque string for forward pagination)
    - `project` (string filter)
    - `deleted` (`true|false`, default `false`)
    - `updated_after` (ISO 8601 UTC cursor)
  - Response:
    ```ts
    {
      items: SlideSummary[];
      next_cursor: string | null;
    }
    ```

- `GET /api/slides/[id]`
  - Response:
    ```ts
    {
      slide: SlideDetail;
    }
    ```
  - `404` when slide is absent.

- `PATCH /api/slides/[id]`
  - Request:
    ```ts
    {
      project_id?: string | null;
      notes?: string | null;
      git_remote_url?: string | null;
      git_hash?: string | null;
    }
    ```
  - Response:
    ```ts
    {
      slide: SlideDetail;
      sync_version: number;
    }
    ```

- `PATCH /api/slides/[id]/order`
  - Request:
    ```ts
    {
      date?: string; // YYYY-MM-DD
      position:
        | { kind: "first" | "last" }
        | { kind: "before" | "after"; reference_id: string };
    }
    ```
  - Response:
    ```ts
    {
      id: string;
      date: string;
      day_order: string;
      updated_at: string;
      sync_version: number;
    }
    ```

- `DELETE /api/slides/[id]`
  - Response:
    ```ts
    {
      id: string;
      deleted_at: string;
      updated_at: string;
      sync_version: number;
    }
    ```

- `POST /api/slides/[id]/restore`
  - Response:
    ```ts
    {
      id: string;
      deleted_at: null;
      updated_at: string;
      sync_version: number;
    }
    ```

- `GET /api/sync/version`
  - Response:
    ```ts
    {
      version: number;
      updated_at: string;
    }
    ```
  - Source is S3 `_version`, not Postgres.

- `GET /api/sync/changes?since=<ISO>`
  - Response:
    ```ts
    {
      items: SlideSummary[];
      server_now: string;
    }
    ```
  - Includes soft-deleted slides so delete propagation stays lossless.

- `GET /api/files/[slide_id]/data/[filename]`
- `GET /api/files/[slide_id]/figures/[filename]`
  - Response:
    ```ts
    {
      url: string;
      expires_at: string;
    }
    ```
  - `404` for unknown file/slide pair.

- `GET /api/projects`
  - Response:
    ```ts
    {
      projects: string[];
    }
    ```

## Soft Deletes

- Soft-deleted slides sync bidirectionally, excluded from git export.
- `ON DELETE CASCADE` handles child rows on hard delete.
- `pc gc` deletes from cloud first (Neon), then local, to prevent sync re-creation. If cloud is unreachable, warns on stderr and proceeds with local-only deletion. If cloud `DeleteSlide` fails (non-ErrNotFound), skips that slide with a warning. Runs auto-sync afterward. Edge case: another machine that hasn't synced could re-push; run `pc sync` on all machines before/after gc. Tombstones deferred.

## Data Integrity: Two-Tier Guarantee

### Tier 1 — Full Lossless (Local ↔ Cloud Sync)
All database fields, all figures, all data files byte-for-byte identical.
- Path A: Local + files -> sync -> Neon + S3 -> sync -> Local + files

### Tier 2 — Narrowed (Git Export Paths)
All database fields of active (non-deleted) slides and figures lossless. Data file **references** preserved; binary content requires S3. Soft-deleted slides and `deleted_at` excluded from export. Full recovery = git clone + S3.
- Path B: Neon + S3 -> export -> git -> restore-db -> Neon
- Path C: Local + files -> export -> git -> restore-db -> Local
- Path D: Neon -> sync -> Local -> export -> git
- Path E: git -> import -> Local

## Testing

### Philosophy
- **Test-first**: Write tests before implementation when possible. Define what correct behavior looks like, then build to pass.
- **Tests verify correctness, not existence**: Tests must assert meaningful outcomes (data integrity, correct state transitions, proper error handling), not merely confirm that code runs without crashing.
- **>95% code coverage on all code**: CLI (Go) and web UI (Next.js) both. Enforced in CI — builds fail below threshold.

### Test Layers

**Unit tests** — Pure functions, business logic, data transformations. Fast, no I/O.
- Go: `go test` with table-driven tests. Foundation libraries (fractional indexing, slide ID, timezone, config).
- Next.js: Vitest or Jest. Utility functions, data transformation, sync state logic.

**Integration tests** — Real databases, real filesystem, real S3 (or mocked S3 for CI).
- Go: SQLite repository CRUD, Postgres repository CRUD, S3 client operations, migration runner.
- Next.js: API route handlers against test Neon database.

**CLI e2e tests** — Run the actual `pc` binary as a subprocess.
- Set up temp directories, run commands, verify stdout, exit codes, DB state, and filesystem state.
- Cover full workflows: setup -> add -> show -> edit -> search -> delete -> trash -> gc.
- Sync e2e: two SQLite databases + one Postgres, simulate multi-machine sync with conflict scenarios.
- Export/import e2e: all 5 conversion paths with real data, round-trip verification.

**Web UI e2e tests** — Playwright against real Next.js app + test database.
- Full user workflows: browse slides, filter by project, view details, edit, delete, restore, reorder.
- Sync detection: CLI creates slide -> web UI detects via sync manager -> slide appears.
- Error states: network failures, invalid data, empty states.

**Full system e2e** — CLI + cloud + web UI together.
- CLI creates slide -> syncs to Neon -> web UI sees it -> web UI edits -> CLI syncs and sees the edit.
- Multi-machine simulation: two local databases syncing through cloud with conflicts.

### Coverage Enforcement
- Go: `go test -coverprofile` with threshold check in CI. Minimum 95%.
- Next.js: Coverage reporter (vitest/jest `--coverage`) with threshold in config. Minimum 95%.
- CI fails if any package drops below 95%.

### Test Data
- Fixtures for edge cases: minimal slide (no figures/notes/data/project), large slide (20+ figures, 100KB+ HTML), unicode content, special characters in filenames, slides across multiple dates and projects.
- Test database seeding utilities shared across test suites.

## Cost (Single User, 2 Years)
Neon free tier $0 + S3 ~$0.15 + Amplify ~$0-$1 = **~$0.16-$1.16/mo**. Local-only: $0.
