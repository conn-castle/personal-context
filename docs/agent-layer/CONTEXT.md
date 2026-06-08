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

> **Status:** Phase 10 (Authentication & Multi-User) is complete. See ROADMAP.md.

## Project Overview

Personal Context (`pc`) is a personal engineering notebook system that stores work as individual HTML records and imported agent chats — images, text, tables, code, transcripts, and data — organized chronologically and by project. Designed for 10+ year lifespan, accumulating 1,000-2,000 records per year.

Replaces yearly Google Slides decks with a database-backed, agent-friendly, multi-device, browsable system.

## Monorepo Structure

```
personal-context/          # code repo
├── cli/                   # Go CLI
├── web/                   # Next.js app (App Router)
├── schema/                # schema.sql (DDL source of truth), schema-types.ts
└── docs/                  # README, example GitHub Action workflow
```

Separate **data repo**: nightly git export of active record data and chat sessions (record metadata/content/notes, figures via Git LFS, chat metadata/items/raw source copies). GitHub Action for nightly export lives there.
- Scheduled nightly at `0 4 * * *` UTC.
- Workflow downloads the `pc` binary from code-repo releases, then runs `pc export --from-cloud --path . --github-remote origin`.
- The code repository stores an example workflow under `docs/` for users to copy into their data repo.

CLI releases are published from stable `vX.Y.Z` tags. The release workflow builds macOS/Linux `pc` artifacts, publishes a GitHub Release, and opens a PR against `conn-castle/homebrew-tap` for `Formula/personal-context.rb`. The Homebrew formula name is `personal-context`; the installed binary remains `pc`; the formula description is `Personal structured vault for searchable knowledge, data, files, and records`.

The project license is PolyForm Noncommercial 1.0.0 (`PolyForm-Noncommercial-1.0.0`). Commercial use requires separate permission from the licensor.

## Architecture: Three Data States

```
Local SQLite + local files      <-- CLI writes here (always, every machine)
     | pc sync (bidirectional, if cloud configured)
Neon Postgres + S3              <-- cloud source of truth (web UI reads/writes)
     | nightly export (cloud -> GitHub, via GitHub Action on data repo)
GitHub + S3                     <-- portable backup (git clone + S3 = full restore)
```

Any state can be reconstructed from any other (subject to two-tier guarantee):
- Postgres <-- git export via `pc restore-db` then `pc sync` (Tier 2: data file rows and active chat rows/items/raw sources are recreated, data file object content still requires S3/original files, and soft-deleted records/chats stay excluded)
- Local SQLite <-- Postgres via `pc sync` (Tier 1: fully lossless)
- Git export <-- Postgres via `pc export` (Tier 2: active records and chats are exported; data file binaries stay in S3; soft-deleted records/chats are excluded)
- Postgres <-- local SQLite via `pc sync` push phase (Tier 1: fully lossless)

### Source of Truth
- **Cloud configured**: Neon Postgres + S3 is cloud source of truth. Web UI reads/writes here via Next.js API routes.
- **Local dev mode** (`pc serve`): Go HTTP server implements the same REST API using local SQLite + filesystem. Next.js API routes proxy to Go when `LOCAL_BACKEND_URL` is set. Local mode is single-user and intentionally disables `/login`, `/register`, and `/api/auth/*`.
- **Local-only mode** (CLI only): Local SQLite + local files. No web UI.

## Data Model (12 Tables)

| Table | PK | Purpose |
|-------|-----|---------|
| `users` | `id` TEXT (UUID) | User accounts: email, name, password_hash. Postgres only. |
| `api_keys` | `id` TEXT (UUID) | CLI auth keys: user_id FK, key_hash (SHA-256), label, last_used_at, revoked_at. Postgres only. |
| `projects` | `id` TEXT (`user_id`, `id` in Postgres) | Project registry with archived state. |
| `devices` | `id` TEXT (`user_id`, `id` in Postgres) | Source-device registry with archived state. |
| `project_paths` | `id` auto-increment | Absolute normalized project paths per source device; used to assign imported chats without prompting. |
| `records` | `id` TEXT (`YYYYMMDD-8hex`) | Optional HTML content, notes, project_id, source_device_id, source_ref, git fields, date, day_order, user_id (Postgres), soft delete |
| `chat_session` | `id` TEXT (`YYYYMMDD-8hex`) | Imported agent chat session keyed idempotently by `(source, source_session_id)`; nullable indexed `parent_source_session_id` links a subagent transcript to its parent; nullable project assignment, cwd/title/source path, source device, soft delete. |
| `chat_item` | `id` auto-increment | Normalized chat messages/tool events with ordinal, role, item_type, text/search_text, raw_json. |
| `record_figures` | `id` auto-increment | Image refs: filename, s3_key, alt_text. FK -> records CASCADE |
| `record_data_files` | `id` auto-increment | Data file refs: filename, s3_key, size, SHA-256 hash, description. FK -> records CASCADE |
| `templates` | `name` TEXT | HTML templates for record creation. Hardcoded, seeded by `pc setup` |
| `sync_version` | `user_id` TEXT (Postgres) / `id` (SQLite) | Per-user version counter (Postgres), singleton (SQLite). Auto-incremented by triggers |

### Key Fields and Invariants
- **Record/chat ID**: `{YYYYMMDD}-{8-random-hex}` from `crypto/rand` (e.g., `20250304-a3f2b7e1`). Chat creation checks both records and chats to avoid cross-domain collisions.
- **Sort key**: `ORDER BY (date, day_order, id)` — always deterministic.
- **day_order**: Fractional index string (Figma's algorithm, safe characters only). Lexicographic sort. Reordering updates only the moved record.
- **project_id**: Required slash-convention string (e.g., `"happy-ai/sleep-staging"`) that references a non-archived project registry row for new writes.
- **source_device_id**: Required source-device registry ID for record provenance; new writes reject archived or missing devices.
- **source_ref**: Optional opaque provenance string. Do not URI-validate it in the first pass.
- **html_content**: Optional. `NULL` means `record.html` was absent and the web UI renders a notes/data-only state instead of an iframe. Empty or whitespace-only `record.html` remains a non-null string.
- **s3_key**: Canonical relative path (e.g., `figures/20250304-a3f2b7e1/loss-curve.png`). Same value for both S3 and local filesystem, regardless of mode.
- **git_remote_url**: Optional. Git remote URL (e.g., `https://github.com/org/repo`). Set via `metadata.json` only — no CLI flags. Displayed as clickable link in web UI.
- **git_hash**: Optional. Full 40-character SHA-1 commit hash. Set via `metadata.json` only. In web UI, linkable to `{git_remote_url}/commit/{git_hash}` when both present.
- **deleted_at**: NULL = active, non-NULL = soft-deleted. `WHERE deleted_at IS NULL` on all normal queries.
- **notes**: Full markdown. Empty string normalized to NULL at write time. `has_notes` = `notes IS NOT NULL`.
- **No title. No tags.** Organization is by project and date only.
- **Chat source values**: use unambiguous product identifiers in storage (`codex`, `claude_code`, `gemini`). CLI `--agent claude` maps to `claude_code`.
- **Chat project assignment**: import is non-interactive. Sessions whose `cwd` does not match a registered `project_paths` row are stored with `project_id = NULL`; registering a path via `pc project register <id> [path] --device <id>` backfills matching NULL sessions.
- **Chat source identity & lineage**:
  - `source_session_id` derivation: internal session id for Codex/Claude; file path (project-key dir + basename) for Gemini (no internal id).
  - Claude Task-tool subagent transcripts (files under `subagents/`, or `isSidechain` rows): `source_session_id = <parent_sid>:<subagent_basename>` plus nullable `parent_source_session_id` linking to the parent (metadata, never a FK — the parent row may be absent).
  - Codex fork rollouts: keep their own first `session_meta.id` and store `forked_from_id` in the same nullable `parent_source_session_id` lineage field. `cwd`/`title` are locked to the fork's own header once set (a fork's rollout replays the parent's `session_meta`/`turn_context` carrying the parent cwd, which must not reattribute the fork to the parent's project). Non-fork sessions keep last-wins so a mid-session `cd` and the old-rollout `turn_context` cwd backfill still work.
  - Duplicates: exact byte-identical files are collapsed (`duplicates_skipped`).
  - Collisions: a file colliding with a different file's existing identity and diverging is warn-and-skipped (`collisions_skipped`), never overwritten.
  - Parse errors: unparseable transcript files are warn-and-skipped (`files_skipped`) without aborting the full import.
  - Gemini normalization: `gemini`/`info`/`error` rows normalize to `message`/`event` item types; empty/metadata-only transcripts create no session.
  - Import summary: separates work performed (`items_imported`) from authoritative stored state (`items_delta`, `items_after_import` via repository `CountChatItems`); `raw_sources_copied` counts distinct retained sessions.

### Figure References in HTML
- `html_content` references figures as `figures/{filename}` (relative path, no record_id — implicit from context).
- Each rendering context resolves: web UI iframe rewrites to presigned URLs via `GET /api/files/{record_id}/figures/{filename}`; git export matches naturally (`./figures/{filename}` relative to record folder).
- `pc records add`/`pc records edit` validate that every `figures/` src in HTML has a matching file in the input folder.
- External URLs (`https://...`) pass through unchanged. Data files are attachments, not referenced in HTML.

### Schema Portability (Postgres / SQLite)
- `schema/schema.sql` is the design-level source of truth (Postgres dialect). The canonical SQLite schema is embedded in `cli/internal/sqlite/sqlite_schema.sql` and applied via `Connection.ApplySchema()`.
- Postgres schema embedded in `cli/internal/repository/postgres/postgres_schema.sql` and applied via `ApplySchema()`. No separate migration history is kept under `cli/`.
- CI schema equivalence guard (`scripts/check_schema_equivalence.sh`) prevents structural drift between the two schemas.
- `created_at` and `updated_at` are DB-managed via defaults and triggers. `deleted_at` set by application code. See "DB-Managed Timestamps" below.
- `PRAGMA foreign_keys = ON` required on every SQLite connection (otherwise `ON DELETE CASCADE` silently ignored).
- SQLite WAL mode is enabled for concurrent reads; local connections use `synchronous=NORMAL`.
- Triggers reimplemented in SQLite syntax (per-row instead of per-statement).
- `TIMESTAMPTZ` stored as ISO 8601 text with `Z` suffix in SQLite.
- Child rows (`record_figures`, `record_data_files`) matched by `(record_id, filename)` during sync, NOT by auto-increment `id`.

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
- If cloud configured: auto-sync after each mutation (`pc records add`, `pc records edit`, `pc records delete`, `pc records restore`, `pc records move`, chat import/delete/restore). Failure is non-fatal (prints warning, exit code 0). `pc gc` orchestrates cloud-first-then-local hard deletion, then runs auto-sync.
- If no cloud: writes succeed locally, sync silently skipped.
- `pc sync` (explicit): full bidirectional push-then-pull. Errors if no cloud configured.

### Bidirectional Sync Protocol
1. Acquire file lock (`.pc/sync.lock`) to prevent concurrent sync. Acquisition and stale recovery are serialized by an advisory guard at `.pc/sync.lock.guard`. The lock stores JSON metadata (`pid`, `hostname`, `started_at`); if a same-host lock points to a PID that no longer exists, the next sync replaces it while holding the guard. Unparseable or different-host locks remain blocking.
2. Capture `last_sync_at` at sync **start** (not end).
3. **Push**: local records/chats/project paths where `updated_at >= last_sync_at` -> UPSERT into Neon; record figures upload to S3. (No `deleted_at` check needed — trigger auto-bumps `updated_at`.)
4. **Pull**: Neon records/chats/project paths where `updated_at >= last_sync_at` -> UPSERT into local; record figures download from S3.
5. Update `last_sync_at` only after both phases complete fully.
6. Release lock.

### Conflict Resolution
- Most-recent-action-wins using wall-clock timestamps.
- Delete vs edit: compare `deleted_at` vs `updated_at`. Most recent wins. On resurrection (edit wins), clear `deleted_at` to NULL.
- Timestamp tie: edit wins over delete (deterministic tiebreaker).

### Child Row Sync (Critical Invariants)
- `record_figures` and `record_data_files` auto-increment PKs diverge between Postgres and SQLite. **Sync must match child rows by `(record_id, filename)`, NOT by `id`.**
- Child rows are **only modified as part of a parent record operation** (`pc records add`, `pc records edit`, sync). Never independently. The parent record's `updated_at` is the authoritative change signal. The `sync_version` triggers on child tables may cause harmless false positives during sync. If independent child modification commands are ever added, a cross-table trigger to bump parent `updated_at` must be added.

### Web UI Sync (Smart Layered Polling)
Four layers, 30-second global cooldown. All version checks go through `GET /api/sync/version` (Next.js reads S3 `_version` server-side, NOT Postgres — keeps Neon asleep on free tier).
- Layer 1: Manual refresh (ignores cooldown)
- Layer 2: Interaction-driven (clicks/navigation, subject to cooldown)
- Layer 3: Tab visibility (subject to cooldown)
- Layer 4: Idle polling (idle < 10 min: every 60s, idle > 10 min: every 5 min, tab hidden: nothing)

S3 `_version` is bumped write-after (only after Postgres commit succeeds, retry up to 3 times). Never write-ahead — prevents race where a client polls, sees the bump, queries Postgres, and gets stale data.

On version change: query Neon for records with `updated_at >= last_known_timestamp`. If a version bump produces no incremental changes, perform full reconciliation (hard-deleted rows are invisible to incremental queries).

## Directory Structures

### Local
```
~/personal-context/
├── .pc/
│   ├── config.json           # Neon URL, S3 bucket/region, aws_profile name, api_key, optional s3_endpoint/s3_force_path_style (0600, no AWS keys). Stale active_project may exist but is ignored by write paths. Cloud mode detected by presence of neon_url + aws_profile.
│   ├── pc.db                 # local SQLite
│   ├── last_sync             # timestamp of last cloud sync
│   ├── sync.lock             # JSON metadata file lock for concurrent sync prevention and stale same-host recovery
│   └── sync.lock.guard       # advisory guard file used during lock acquisition/recovery
├── figures/{record_id}/{filename}
├── data/{record_id}/{filename}  # sparse, on demand
└── chats/raw/{chat_session_id}/source.{json|jsonl|ndjson}  # PC-owned raw transcript copy; original imported path retained as chat_session.original_source_path
```

### S3
```
s3://personal-context-prod/
└── users/{user_id}/
    ├── figures/{record_id}/{filename}
    ├── data/{record_id}/{filename}
    ├── chats/raw/{chat_session_id}/source.{json|jsonl|ndjson}
    └── _version                # per-user sync heartbeat
```

### Git Export
```
personal-context-data/
├── projects.json
├── devices.json
├── templates/*.html
├── chats/{session_id}/
│   ├── metadata.json           # ChatExport
│   ├── items.jsonl             # one ChatItemExport per line
│   └── source.{json|jsonl|ndjson}
└── records/{record_id}/
    ├── metadata.json           # RecordExport (no HTML, no notes text)
    ├── record.html              # optional; absent means html_content is NULL
    ├── notes.md                # only if has_notes
    └── figures/                # Git LFS
```
Data files stay in S3 only; `metadata.json` lists what exists. Current chat export includes normalized metadata/items plus the Personal Context-owned raw source copy. Soft-deleted records and chats are excluded from export. Export must be deterministic (consistent JSON key order, sorted arrays) for clean git diffs. Long-term chat export size/privacy policy remains tracked in BACKLOG.md.

## Technology Stack

| Component | Technology |
|-----------|-----------|
| CLI | Go: cobra, modernc.org/sqlite (pure Go), fracdex (fractional indexing), pgx (direct, not database/sql), aws-sdk-go-v2 (+credentials, +service/s3, +smithy-go), testcontainers-go (integration tests). Custom migration runner in `cli/internal/sqlite/` (no golang-migrate). |
| Web UI | Next.js App Router, React, react-resizable-panels, shadcn/ui (New York), lucide-react, date-fns, react-markdown + remark-gfm + mermaid (notes rendering), sandboxed iframes for record HTML rendering |
| Web hosting | AWS Amplify (SSR via Lambda, us-east-1) |
| DB (cloud) | Neon Postgres (provider-portable). @neondatabase/serverless HTTP driver for Lambda |
| DB (local) | SQLite via modernc.org/sqlite (pure Go, no CGO) |
| Storage (cloud) | AWS S3 (Intelligent-Tiering, us-east-1). Presigned URLs for web downloads |
| Storage (local) | Local filesystem |
| Backup | GitHub nightly export + Git LFS |

## Authentication & Multi-User

### Two Operational Modes
1. **Local only** — CLI + SQLite, no auth, no `user_id`. `pc serve` + Next.js with `LOCAL_BACKEND_URL`.
2. **Web (cloud)** — Auth.js v5 (Credentials provider), per-user data isolation. JWT sessions (90-day maxAge, no DB hit per request).

### Auth Flow
- **Web UI**: Auth.js Credentials provider → email/password → JWT session.
- **CLI**: API keys (`pc_key_<uuid>`) stored in `config.json`. SHA-256 hash stored in `api_keys` table. CLI resolves `user_id` via `resolveUserIDFromAPIKey` → queries `api_keys WHERE key_hash = $1 AND revoked_at IS NULL`.
- **API routes**: `requireUser(req)` checks Bearer token first (API key auth for CLI), then falls back to Auth.js session (web UI). Returns `{ id, email }` or 401.
- **Local mode**: `isLocalMode()` returns true → proxy to Go server → no auth check.

### Per-User Data Isolation
- `records.user_id` FK → `users.id` (Postgres only). SQLite has no `user_id` column.
- `sync_version` scoped per-user (PK = `user_id`).
- S3 keys prefixed with `users/{user_id}/` at call site. `s3_key` column unchanged in DB.
- All API routes and repository queries filter by `user_id`.

### Auth Database
- `users` table: `id`, `email`, `name`, `password_hash`, `created_at`, `updated_at`.
- `api_keys` table: `id`, `user_id`, `key_hash` (SHA-256), `label`, `created_at`, `last_used_at`, `revoked_at`.
- Auth queries use standard `pg` Pool (`web/lib/db-pool.ts`), NOT `@neondatabase/serverless`. Zero Neon lock-in for auth.
- Fresh cloud databases are bootstrapped with `pc setup --init-cloud-schema --neon-url=...` before the first web registration/API-key flow.

### Auth Endpoints
- `POST /api/register` — creates user (gated by `REGISTRATION_ENABLED` env var).
- `GET/POST /api/auth/[...nextauth]` — Auth.js route handler.
- `GET /api/api-keys` — list keys for authenticated user.
- `POST /api/api-keys` — create key (returns raw key once).
- `DELETE /api/api-keys/[id]` — revoke key.

### Go Repository Pattern
- `Repository` interface with separate SQLite and Postgres implementations.
- SQL dialects diverge enough that sharing query code via `database/sql` is a false economy.
- **SQLite** (`cli/internal/sqlite/`): modernc.org/sqlite (pure Go), `?` positional params, FTS5 virtual tables for record/chat search, text timestamps. Custom migration runner with single embedded `sqlite_schema.sql`.
- **Postgres** (`cli/internal/repository/postgres/`): pgx (direct, not database/sql), `$N` positional params, generated `tsvector` columns plus GIN indexes for record/chat search, `RETURNING` clauses, native `time.Time`. Embedded DDL via `postgres_schema.sql` + `ApplySchema()`. No separate migration history under `cli/`.
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
- `scripts/check_schema_equivalence.sh` — CI script comparing Postgres (`schema/schema.sql`) and SQLite (`cli/internal/sqlite/sqlite_schema.sql`) schemas for structural equivalence: tables, columns, indexes, UNIQUE constraints, and search-index structures (SQLite FTS tables/triggers plus Postgres TSVECTOR/GIN indexes). Does not compare types, CHECK expressions, or non-search triggers (intentionally dialect-specific).
- `scripts/verify_phase5_demo.sh` — repository-level Phase 5 demo runner: executes schema contract/equivalence checks, cloud config validation tests, and Docker-backed Postgres/S3 integration suites.

## CLI Commands

### Setup & Health
- `pc setup` — first-time or reconfigure (idempotent, interactive/non-interactive, `--remove-cloud`)
- `pc doctor` — health checks (DB readability, orphaned figure/data directories, missing local figure/data/chat raw-source files; cloud connectivity and chat raw cloud checks WARN if configured but unreachable)

### Record CRUD
- `pc records add <path>` — create record from folder (`record.html` optional; project/device provenance required through flags or metadata)
- `pc records edit <id> <path>` — full replacement of content, notes, figures, data files, git fields (`updated_at` auto-bumped by trigger)
- `pc records delete <id>` — soft-delete
- `pc records restore <id>` — un-delete
- `pc records move <id>` — change date or position
- `pc records show <id>` — display record metadata (including git fields), notes, figures, data files (`--format text|json`)
- `pc show <id>` — cross-domain record/chat display.

### Trash
- `pc trash` — list soft-deleted records and chats
- `pc gc` — hard-delete trash older than the configured retention window (default 30 days; `gc_retention_days` in `.pc/config.json` overrides it for records and chats alike; cloud-first if configured: deletes from Neon/S3 before local to prevent sync re-creation, warns if cloud unreachable, removes local figure/data/chat raw-source files, runs auto-sync)

### Search & Registries
- `pc search <query>` — cross-domain records/chats search; `--json`/`--format json` emits a flat array with `domain` on every item.
- `pc records list` — bounded newest-first record summaries with cursor pagination, date/project/deleted filters, `--query`, `--has-html`, `--has-data`, `--all`, and `--format table|ids|json`; JSON returns `{items,total,next_cursor}`.
- `pc records stats` — local record statistics with active/deleted counts, content/attachment counts, oldest/newest dates, and explicit size fields (`recorded_data_file_bytes`, `local_attachment_bytes`, `store_file_bytes`, `local_total_bytes`)
- `pc records files list` — local record attachment inventory with figure/data rows, recorded data-file size, local file size/path, and present/missing status
- `pc chat import --device <id>` — full-scan import for Codex, Claude Code, and Gemini transcripts; uses the local sync lock and bulk chat FTS rebuild for mutating imports; `--agent` narrows and `--root` overrides default roots while requiring `--agent`.
- `pc chat list|search|show|delete|restore` — chat browsing, item search, transcript rendering, soft deletion, and restore. `pc chat show` uses `$PAGER` only when stdout is a TTY, and shows a subagent list/count for a parent plus the parent for a subagent (text + JSON). `pc chat list`/`pc chat search` accept `--parent-source-session-id <sid>` to scope to one parent's subagents.
- `pc docs [topic]` / `pc docs search <query>` — print embedded concept reference (chat-import, item-types, schema, search-syntax, project-device-registry) that matches the installed binary; markdown lives in `cli/internal/docs/*.md`, embedded via `//go:embed`.
- `pc project list|register|archive|restore` — manage registered projects; optional `pc project register <id> [path] --device <id>` registers a project path and backfills matching unassigned chats.
- `pc device list|register|archive|restore` — manage registered source devices

### Local Dev Server
- `pc serve` — start Go HTTP server on `127.0.0.1:<port>` implementing the web API against local SQLite + filesystem. Used with `next dev` for local web UI development without cloud credentials.

### Screenshot
- `pc screenshot <id>` — renders record HTML at 1920x1080 using headless Chrome and saves as PNG. Requires Chrome/Chromium on PATH or `PC_CHROME_PATH` env var. `--output` / `-o` to set output path (default: `<id>.png` in current directory).

### Dev Tools
- `pc seed` — creates 6 tutorial records under the `personal-context/tutorial` project. Idempotent — backfills any missing built-in tutorial records and skips only when all 6 already exist. Run automatically by `make dev-local`.

### Sync & Data
- `pc sync` — bidirectional cloud sync (errors if no cloud)
- `pc fetch` — download data files from S3
- `pc export` — DB to git folder format
- `pc import <path>` — merge from git export (update if newer, else skip; full replacement of child rows)
- `pc restore-db <path>` — rebuild DB from git export (destructive, writes a backup snapshot first under `~/personal-context/.pc/backups/`)
- `pc verify` — full round-trip data integrity tests (`pc verify` for local, `pc verify --from-cloud` for cloud-rooted verification)

### `pc fetch` modes and flags
- All mode:
  - `pc fetch --all`
  - Scans every non-deleted cloud record and ensures each cloud-backed data file exists at `~/personal-context/data/{record_id}/`.
  - Skips files whose local copy already matches the recorded size and SHA-256 hash; otherwise downloads from S3 and verifies the downloaded bytes against the same size and hash. If verification fails after a download, the unverified bytes are removed from the canonical path so callers never observe a known-bad file.
  - Aborts promptly on a cancelled context (Ctrl+C) — partial summary still printed.
  - Reports records scanned, already-present files, downloads, bytes downloaded, and missing/failed files. Per-failure detail is written to stderr; on failure, exits non-zero with a bounded preview of the first few errors.
  - Cannot be combined with `--output`.
- Record mode:
  - `pc fetch <record_id>`
  - Downloads all data files for one record into `~/personal-context/data/{record_id}/` by default.
- Project mode:
  - `pc fetch --project "org/project"`
  - Downloads data files for all records in a project.
- Recent-window mode:
  - `pc fetch --recent 3m`
  - Downloads data files for records inside a relative time window (`d`, `w`, `m`, `y` suffixes).
- Output override:
  - `--output "./target-dir"`
  - Writes record/project/recent downloads under the provided directory instead of the default data path.
- Preconditions:
  - Cloud must be configured. If cloud is not configured, `pc fetch` fails loudly (no silent local fallback).

### `pc export` nightly cloud backup usage
- Manual/local usage:
  - `pc export --path ./pc-export --github-remote origin`
  - Use `--project`, `--from YYYY-MM-DD`, and `--to YYYY-MM-DD` to export an active-record subset.
- Nightly data-repo workflow usage:
  - `pc export --from-cloud --path . --github-remote origin`
  - Reads record rows from Neon and figure blobs from S3 using repository secrets; template exports still come from the seeded local template set because templates are not cloud-synced.

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

- 16:9 record viewer with sandboxed iframes, `transform: scale()`, white background
- Virtual date records injected at render time
- Filter by project
- Auto-selects the most recent record on initial load (never shows an empty "select a record" state when records exist)
- Read-only chronological navigation (drag-and-drop reorder is still deferred)
- View notes (full markdown via react-markdown + remark-gfm + mermaid), figures, and data files with sizes plus open/download actions
- Edit notes from the detail panel
- Soft delete, restore handling for selected deleted records, and Settings trash counts/purge controls. A browseable trash/deleted-records view is still deferred in BACKLOG.md.
- 4-layer sync polling via `useSyncManager()` hook

### Web UI Architecture

3-panel resizable layout using `react-resizable-panels` (v4 API, wrapped with v2-style `direction` prop in `resizable.tsx`):
- `spreadsheet-viewer.tsx` — shell/layout (top-level orchestrator, owns all hook calls and UI state)
- `record-navigation.tsx` — left panel (record list grouped by date, strip/grid views)
- `record-viewer.tsx` — center panel (scaled iframe preview of selected record)
- `record-details.tsx` — right panel (tabbed: notes editor, figures via AssetCard, data files via AssetCard)
- `record-metadata-bar.tsx` — metadata strip above viewer (date, project, git info, more menu)
- `collapsed-details-strip.tsx` — icon strip when details panel is hidden
- `project-picker.tsx` — project filter popover (uses cmdk for search/multi-select)
- `record-date-picker.tsx` — calendar date picker (react-day-picker v9)
- `record-thumbnail.tsx` — thumbnail card in navigation panel (uses `ScaledRecordFrame` for HTML preview, identical rendering to main viewer)
- `asset-card.tsx` + `asset-preview-dialog.tsx` — file/figure cards with preview dialog
- `settings-overlay.tsx` — settings dialog with sync/status details, data management, trash purge controls, and external links
- `theme-provider.tsx` — wraps next-themes ThemeProvider
- `markdown-renderer.tsx` — full markdown rendering via react-markdown + remark-gfm + mermaid diagram support
- `scaled-record-frame.tsx` — 16:9 sandboxed iframe with `transform: scale()` (no figure URL resolution — renders htmlContent directly)

### Web UI Hooks

- `useRecords` (`hooks/use-records.ts`) — data fetching, CRUD mutations, cursor-based pagination, optimistic updates
- `useSyncManager` (`hooks/use-sync-manager.ts`) — 4-layer smart polling (manual, interaction, visibility, idle)

### Web UI Libraries

- **shadcn/ui** (New York style) — UI primitives in `components/ui/`. OKLCH color system, Tailwind v4.
- **radix-ui** (unified package) — requires `web/.npmrc` with `public-hoist-pattern[]=@radix-ui/*` for pnpm to hoist sub-packages that shadcn/ui components import directly.
- **react-resizable-panels** v4 — 3-panel layout. v4 API uses `Group`/`Panel`/`Separator` (not v2's `PanelGroup`/`Panel`/`PanelResizeHandle`). `resizable.tsx` provides v2-compatible `direction` prop wrapper. **Size props must use string percentages** (e.g., `"18%"`), not bare numbers (which are pixels in v4).
- **next-themes** — dark mode support via `ThemeProvider` + `useTheme()`
- **cmdk** — command palette for ProjectPicker search/multi-select
- **react-day-picker** v9 — calendar in RecordDatePicker
- **react-markdown** + **remark-gfm** — full markdown rendering for notes (GFM tables, strikethrough, task lists, autolinks)
- **mermaid** — renders `mermaid` code blocks as diagrams in the notes panel
- **lucide-react** — icons
- **date-fns** — date formatting

### Web UI Test Strategy

- Presentation components (`components/*.tsx`, `app/page.tsx`) and shadcn primitives (`components/ui/**`) are excluded from Vitest unit coverage thresholds.
- These components are primarily validated via **Playwright e2e** tests (`tests/e2e/`), with focused component unit tests added for high-value state transitions.
- Playwright e2e tests use `page.route()` interception for API mocking — no real backend or database needed.

### Visual Regression Tests

- `tests/e2e/ui-visual.e2e.spec.ts` — 8 tests covering: initial load, record selection, panel toggles, tab switching, dark mode, settings overlay, empty state, grid view. Each test also asserts zero unexpected console errors.
- Baselines stored in `tests/e2e/__screenshots__/ui-visual.e2e.spec.ts/*.png` (11 PNGs, committed to git).
- `snapshotPathTemplate` in `playwright.config.ts` removes platform from paths: `{testDir}/__screenshots__/{testFilePath}/{arg}{ext}`.
- `maxDiffPixelRatio: 0.02` on all `toHaveScreenshot()` calls to tolerate Next.js dev badge.
- Run: `pnpm test:e2e:visual` (compare), `pnpm test:e2e:visual -- --update-snapshots` (regenerate).
- Current baselines are macOS/darwin. Linux CI baselines deferred (see ISSUES.md f7g8h9).

### Deployment

- `amplify.yml` in `web/` configures AWS Amplify build/deploy (SSR via Lambda, us-east-1).

### API Routes
- `GET /api/records` — list (paginated, filtered)
- `GET /api/records/[id]` — single record with figures and data files
- `PATCH /api/records/[id]` — edit project_id, notes, git_remote_url, git_hash
- `PATCH /api/records/[id]/order` — reorder (fractional index)
- `DELETE /api/records/[id]` — soft delete
- `POST /api/records/[id]/restore` — restore
- `GET /api/sync/version` — reads S3 `_version` (not Postgres)
- `GET /api/sync/changes?since=<ISO>` — changed records
- `GET /api/files/[record_id]/data/[filename]` — presigned download URL
- `GET /api/files/[record_id]/figures/[filename]` — presigned figure URL
- `GET /api/projects` — active registry project IDs
- `GET /api/info` — application mode and version
- `GET /api/stats` — total active records, active registry projects, trashed record count
- `DELETE /api/records/trash` — bulk purge all soft-deleted records
- `POST /api/register` — create user account (gated by `REGISTRATION_ENABLED`)
- `GET/POST /api/auth/[...nextauth]` — Auth.js route handler
- `GET /api/api-keys` — list API keys for authenticated user
- `POST /api/api-keys` — create API key
- `DELETE /api/api-keys/[id]` — revoke API key
- No record creation endpoint (CLI only)
- All data routes require authentication (Bearer token or session). Local mode bypasses auth.

### API payload shapes

```ts
type ErrorResponse = {
  error: string;
  code: string;
};

type RecordSummary = {
  id: string;
  date: string; // YYYY-MM-DD
  day_order: string;
  html_content: string | null;
  project_id: string;
  source_device_id: string;
  source_ref: string | null;
  updated_at: string; // ISO 8601 UTC
  deleted_at: string | null;
  figure_count: number;
  data_file_count: number;
};

type RecordFile = {
  filename: string;
  s3_key: string;
  size?: number;
  hash?: string;
  alt_text?: string | null;
  description?: string | null;
};

type RecordDetail = {
  id: string;
  date: string;
  day_order: string;
  html_content: string | null;
  notes: string | null;
  project_id: string;
  source_device_id: string;
  source_ref: string | null;
  git_remote_url: string | null;
  git_hash: string | null;
  created_at: string;
  updated_at: string;
  deleted_at: string | null;
  figures: RecordFile[];
  data_files: RecordFile[];
};
```

- `GET /api/records`
  - Query params:
    - `limit` (number, default 20)
    - `cursor` (opaque string for forward pagination)
    - `project` (string filter)
    - `deleted` (`true|false`, default `false`)
    - `updated_after` (ISO 8601 UTC cursor)
  - Response:
    ```ts
    {
      items: RecordSummary[];
      total: number;
      next_cursor: string | null;
    }
    ```

- `GET /api/records/[id]`
  - Response:
    ```ts
    {
      record: RecordDetail;
    }
    ```
  - `404` when record is absent.

- `PATCH /api/records/[id]`
  - Request:
    ```ts
    {
      project_id?: string;
      notes?: string | null;
      git_remote_url?: string | null;
      git_hash?: string | null;
    }
    ```
  - Response:
    ```ts
    {
      record: RecordDetail;
      sync_version: number;
    }
    ```

- `PATCH /api/records/[id]/order`
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

- `DELETE /api/records/[id]`
  - Response:
    ```ts
    {
      id: string;
      deleted_at: string;
      updated_at: string;
      sync_version: number;
    }
    ```

- `POST /api/records/[id]/restore`
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
      items: RecordSummary[];
      server_now: string;
    }
    ```
  - Includes soft-deleted records so delete propagation stays lossless.

- `GET /api/files/[record_id]/data/[filename]`
- `GET /api/files/[record_id]/figures/[filename]`
  - Response:
    ```ts
    {
      url: string;
      expires_at: string;
    }
    ```
  - `404` for unknown file/record pair.

- `GET /api/projects`
  - Response:
    ```ts
    {
      projects: string[];
    }
    ```

- `GET /api/info`
  - Response:
    ```ts
    {
      mode: "local" | "cloud";
      version: string;
    }
    ```

- `GET /api/stats`
  - Response:
    ```ts
    {
      total_records: number;
      total_projects: number;
      trashed_records: number;
    }
    ```
  - `500` on database query failure.

- `DELETE /api/records/trash`
  - Bulk hard-deletes all soft-deleted records. Best-effort cleanup of associated figures/data files.
  - Response:
    ```ts
    {
      purged_count: number;
      sync_version: number;
    }
    ```
  - `500` on database operation failure.

## Soft Deletes

- Soft-deleted records sync bidirectionally, excluded from git export.
- `ON DELETE CASCADE` handles child rows on hard delete.
- `pc gc` deletes from cloud first (Neon), then local, to prevent sync re-creation. If cloud is unreachable, warns on stderr and proceeds with local-only deletion. If cloud `DeleteRecord` fails (non-ErrNotFound), skips that record with a warning. Runs auto-sync afterward. Edge case: another machine that hasn't synced could re-push; run `pc sync` on all machines before/after gc. Tombstones deferred.

## Data Integrity: Two-Tier Guarantee

### Tier 1 — Full Lossless (Local ↔ Cloud Sync)
All database fields, all figures, all data files byte-for-byte identical.
- Path A: Local + files -> sync -> Neon + S3 -> sync -> Local + files

### Tier 2 — Narrowed (Git Export Paths)
All database fields of active (non-deleted) records and figures lossless. Data file **references** preserved; binary content requires S3. Soft-deleted records and `deleted_at` excluded from export. Full recovery = git clone + S3.
- Path B: Neon + S3 -> export -> git -> restore-db -> Neon
- Path C: Local + files -> export -> git -> restore-db -> Local
- Path D: Neon -> sync -> Local -> export -> git
- Path E: git -> import -> Local
- After `pc restore-db`, a later `pc sync` recreates data-file rows in cloud metadata even when the local binary is absent. The referenced object still has to exist in S3 or be restored separately.

## Testing

### Philosophy
- **Test-first**: Write tests before implementation when possible. Define what correct behavior looks like, then build to pass.
- **Tests verify correctness, not existence**: Tests must assert meaningful outcomes (data integrity, correct state transitions, proper error handling), not merely confirm that code runs without crashing.
- **>95% code coverage on all code**: CLI (Go) and web UI (Next.js) both. Enforced in CI — builds fail below threshold.

### Test Layers

**Unit tests** — Pure functions, business logic, data transformations. Fast, no I/O.
- Go: `go test` with table-driven tests. Foundation libraries (fractional indexing, record ID, timezone, config).
- Next.js: Vitest. Utility functions, data transformation, sync state logic.

**Integration tests** — Real databases, real filesystem, real S3 (or mocked S3 for CI).
- Go: SQLite repository CRUD, Postgres repository CRUD, S3 client operations, migration runner.
- Next.js: API route handlers with mocked Neon/S3/local proxy dependencies.

**CLI e2e tests** — Run the actual `pc` binary as a subprocess.
- Set up temp directories, run commands, verify stdout, exit codes, DB state, and filesystem state.
- Cover full workflows: setup -> add -> show -> edit -> search -> delete -> trash -> gc.
- Sync e2e: two SQLite databases + one Postgres, simulate multi-machine sync with conflict scenarios.
- Export/import e2e: all 5 conversion paths with real data, round-trip verification.

**Web UI e2e tests** — Playwright against a real Next.js app with `page.route()` API interception.
- Covered workflows: browse records, filter by project, view details, edit notes, notes/data-only rendering, sync badge, pagination, markdown rendering, visual states, and error states.
- Deferred UI workflows: browseable trash/restore flow and drag-and-drop reorder remain in BACKLOG.md/ISSUES.md.
- Full CLI + cloud + web round-trips remain Phase 11 work.

**Full system e2e** — CLI + cloud + web UI together.
- CLI creates record -> syncs to Neon -> web UI sees it -> web UI edits -> CLI syncs and sees the edit.
- Multi-machine simulation: two local databases syncing through cloud with conflicts.

### Coverage Enforcement
- Go: `go test -coverprofile` with threshold check in CI. Minimum 95%.
- Next.js: Coverage reporter (vitest/jest `--coverage`) with threshold in config. Minimum 95%.
- CI fails if any package drops below 95%.

### Test Data
- Fixtures for edge cases: minimal record (no figures/notes/data/project), large record (20+ figures, 100KB+ HTML), unicode content, special characters in filenames, records across multiple dates and projects.
- Test database seeding utilities shared across test suites.

## Cost (Single User, 2 Years)
Neon free tier $0 + S3 ~$0.15 + Amplify ~$0-$1 = **~$0.16-$1.16/mo**. Local-only: $0.
