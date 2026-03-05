# Personal Context (`pc`) — Product Spec

> **Status:** COMPLETE — ready for implementation
> **Last updated:** 2026-03-05

---

## Overview

**Personal Context** is a personal engineering notebook system that stores work as individual HTML slides — images, text, tables, code, and data — organized chronologically and by project. Designed for a 10+ year lifespan, accumulating 1,000–2,000 slides per year across all projects.

Replaces yearly Google Slides decks with a database-backed, agent-friendly, multi-device, browsable system.

**Repositories:** Two separate repos:
- **Code repo:** Monorepo: `cli/` (Go CLI), `web/` (Next.js app), `schema/` (schema.sql, schema-types.ts), `docs/` (README, example GitHub Action workflow).
- **Data repo:** Nightly git export of slide data (metadata.json, slide.html, notes.md, figures via LFS). GitHub Action for nightly export lives here.

---

## Design Principles

- **Simplicity**: Slides are the core primitive. A slide is its HTML content — no title, no tags. Organization is by project and date.
- **Local-first**: The CLI always writes locally. Cloud sync is optional. Works fully offline.
- **Agent-first**: Claude (and other agents) create slides via the CLI. The CLI is the primary agent interface.
- **Longevity**: Must work reliably for 10+ years. No exotic dependencies.
- **Multi-device**: Two Macs stay in sync via cloud. Web UI for viewing and editing.
- **Portable**: All data exportable to a self-contained git repo at any time. No vendor lock-in.
- **Provider-agnostic Postgres**: Cloud database uses standard Postgres. No Neon-specific features. Changing providers = changing one connection string.
- **Open-source path**: Self-hosted first, with a clear path to a hosted multi-tenant model.

---

## Timezone Rules

All timestamps (`created_at`, `updated_at`, `deleted_at`) are stored as **UTC** — `TIMESTAMPTZ` in Postgres, ISO 8601 with `Z` suffix in SQLite and JSON.

The `date` field on slides is a **local calendar date** (`YYYY-MM-DD`) with no time component. "Today" is determined by the user's local timezone at creation time.

All reads convert UTC timestamps to local timezone for display.

**Timestamp management is DB-driven:**
- `created_at` — set via `DEFAULT NOW()` on INSERT. Never modified.
- `updated_at` — set via `DEFAULT NOW()` on INSERT. Auto-bumped by a BEFORE UPDATE trigger when the value is not explicitly changed (i.e., `NEW.updated_at = OLD.updated_at`). This means any normal UPDATE (edit, delete, restore) automatically bumps `updated_at`.
- `deleted_at` — set explicitly by application code (`NOW()` for delete, `NULL` for restore). The `updated_at` trigger auto-fires on the same UPDATE.
- **Sync/import bypass:** When sync or import sets explicit timestamps in the UPDATE/INSERT statement, the trigger detects `NEW.updated_at != OLD.updated_at` and skips the auto-bump, preserving the original timestamp.
- **Precision:** Millisecond is the effective minimum across all contexts. Postgres stores microsecond precision; SQLite stores millisecond. Postgres timestamps are truncated to millisecond when syncing to SQLite. All sync and polling cursors use `>=` (not strict `>`) to prevent boundary misses. UPSERT makes re-processing safe.

---

## 1. Data Architecture (LOCKED)

### Three Data States

```
Local SQLite + local files      ← CLI writes here (always, every machine)
     ↕ pc sync (bidirectional, if cloud configured)
Neon Postgres + S3              ← cloud source of truth (web UI reads/writes here)
     ↕ nightly export (cloud → GitHub, via GitHub Action on data repo)
GitHub + S3                     ← portable backup (git clone + S3 = full restore)
```

Any state can be reconstructed from any other (subject to the two-tier guarantee — see Data Integrity section):
- Postgres ← git export via `pc restore-db` then `pc sync` (Tier 2: data file binaries require S3; soft-deleted slides not in export)
- Local SQLite ← Postgres via `pc sync` (Tier 1: fully lossless)
- Git export ← Postgres via `pc export` (Tier 2: data file binaries stay in S3; soft-deleted slides excluded)
- Postgres ← local SQLite via `pc sync` (push phase, Tier 1: fully lossless)

### Source of Truth

**Cloud configured:** Neon Postgres + S3 is the cloud source of truth. Web UI reads/writes here. CLI writes locally and syncs bidirectionally.

**Local-only mode:** Local SQLite + local files is the only copy. No web UI. CLI works standalone.

### Slide ID Format

`{YYYYMMDD}-{8-char-hash}` — e.g., `20250304-a3f2b7e1`

Date prefix from local timezone. The 8-char hash is **random hex** (from `crypto/rand`), ensuring uniqueness even when multiple machines create slides on the same day simultaneously. Collision probability effectively zero (4.3 billion combinations per day).

Human-readable, greppable, filesystem-safe.

### Slide Ordering

Sort key: `(date, day_order, id)`

- `date` — primary sort. Can differ from `created_at`.
- `day_order` — fractional index string for intra-day ordering (Figma's algorithm). Lexicographic sort. Reordering updates only the moved slide.
- `id` — universal tiebreaker. Always deterministic.

### What Lives Where

**In Postgres / Local SQLite (same schema, 5 tables):**

| Table | Contents |
|-------|----------|
| `slides` | HTML content, notes, project_id, git_remote_url, git_hash, date, day_order, deleted_at |
| `slide_figures` | Image references (filename, S3/local path, alt text) |
| `slide_data_files` | Data file references (filename, S3/local path, size, hash, description) |
| `templates` | Slide HTML templates for agents and manual use |
| `sync_version` | Single-row version counter, auto-incremented by triggers |

**No title column. No tags column.** Organization is by project and date. `project_id` uses slash convention for hierarchy (`"happy-ai/sleep-staging"` → org `happy-ai`, project `sleep-staging`). No project table in MVP — just a string.

**Binary files — S3 (cloud) / local filesystem:**

Cloud:
```
s3://personal-context-prod/
├── figures/{slide_id}/{filename}
├── data/{slide_id}/{filename}
└── _version                          ← sync heartbeat for web UI
```

Local:
```
~/personal-context/
├── figures/{slide_id}/{filename}
├── data/{slide_id}/{filename}        ← sparse, on demand
```

**S3 region:** `us-east-1` (matches Neon, cheapest, Amplify default).

**S3 bucket naming:** `personal-context-prod` (single user). For multi-user future: `personal-context-prod/users/{user_id}/...` — `user_id` from auth system, injected by API routes. Don't design this now.

S3 Intelligent-Tiering:
- < 30 days: Frequent Access (~$0.023/GB)
- 30+ days untouched: Infrequent Access (~40% cheaper)
- 90+ days untouched: Archive Instant Access (~68% cheaper, millisecond retrieval)

### Nightly GitHub Snapshot

A **GitHub Action on the data repo** runs nightly (`cron: '0 4 * * *'` UTC):

1. Downloads the `pc` binary from the code repo's GitHub Releases
2. Runs `pc export --from-cloud --path . --github-remote origin` (connects to Neon via connection string in repo secrets, reads figures from S3 via AWS credentials in repo secrets)
3. Figures tracked via Git LFS
4. Export commits and pushes to the data repo

Local-only users run `pc export --github-remote origin` manually or on their own cron. The code repo contains an example Action workflow that users copy into their data repo.

**Exported structure (no manifest.json):**

```
personal-context-data/
├── templates/
│   ├── text-only.html
│   └── single-image.html
└── slides/
    ├── 20250304-a3f2b7e1/
    │   ├── metadata.json      ← slide metadata (no HTML, no notes text)
    │   ├── slide.html         ← html_content
    │   ├── notes.md           ← notes (only if has_notes)
    │   └── figures/           ← Git LFS
    │       ├── loss-curve.png
    │       └── confusion-matrix.png
    └── 20250304-b7e1c9d3/
        ├── metadata.json
        └── slide.html
```

`data/` files (CSVs, models) stay in S3 only. `metadata.json` lists what exists.

**Rollback:** Git checkout any nightly snapshot, then `pc restore-db`.

**The git export folder does NOT persist locally during normal operation.** Generated on demand by `pc export`, consumed by `pc import` / `pc restore-db`.

### Local Directory Structure

```
~/personal-context/
├── .pc/
│   ├── config.json           # cloud config (optional), active project
│   ├── pc.db                 # local SQLite (same schema as Postgres)
│   └── last_sync             # timestamp of last cloud sync
├── figures/                  # locally stored/cached figures
│   └── 20250304-a3f2b7e1/
│       └── loss-curve.png
└── data/                     # sparse — on demand only
    └── 20250304-a3f2b7e1/
        └── training-log.csv
```

**Data files sync rules (MVP):** on-demand only via `pc fetch`. No automatic data file sync. Project-based and time-window sync deferred to post-MVP.

### Soft Deletes

`deleted_at` timestamp on slides table. `WHERE deleted_at IS NULL` on all normal queries.

- **Delete:** `UPDATE slides SET deleted_at = NOW() WHERE id = ?` (trigger auto-bumps `updated_at`)
- **Restore:** `UPDATE slides SET deleted_at = NULL WHERE id = ?` (trigger auto-bumps `updated_at`, making the change visible to sync)
- **List trash:** `SELECT id, date, deleted_at FROM slides WHERE deleted_at IS NOT NULL`
- **Hard delete:** `DELETE FROM slides WHERE deleted_at < NOW() - INTERVAL '30 days'`

Soft-deleted slides sync bidirectionally. Excluded from git export. Hard delete via `pc gc` on demand (not automatic). `pc gc` hard-deletes from both local SQLite and Neon (if cloud configured), removes associated figures and data files from local filesystem and S3. `ON DELETE CASCADE` handles the database child rows (slide_figures, slide_data_files) automatically.

**Edge case — multi-machine:** If another machine has a soft-deleted copy and hasn't synced before `pc gc` runs, that machine could re-push the slide on next sync. Mitigation: run `pc sync` on all machines before and after `pc gc`. Tombstone/durable-delete mechanism deferred to post-MVP.

**Edge case — web UI:** `pc gc` bumps S3 `_version`. The web UI detects the version change but incremental polling (`updated_at >= last_known`) returns no results (hard-deleted rows are gone). The web UI must perform a full reconciliation: fetch the complete slide list and remove any locally-cached slides no longer present.

### Date Slides (Virtual)

Not stored. Injected at render time:
- When date changes between consecutive slides.
- After 10+ consecutive slides without a date change.
- Display: centered `YYYY-MM-DD`, minimal styling.

### Notes Convention

Full markdown stored in Postgres/SQLite `notes` column. In git export: content in `notes.md` sibling file, `has_notes` boolean in `metadata.json`. The JSON does NOT contain notes text.

### Slide Rendering Container

**16:9 aspect ratio.** Scales to fill available width in the viewer. No enforced padding, margins, or fonts — the HTML content controls all styling. The viewer provides a white background behind the container by default and nothing else.

### Templates

Templates are **hardcoded and seeded** by `pc setup`. No CLI commands for template management in MVP. Templates are rows in the database — if the user or a future version wants to manage them, a CLI command set can be added later.

---

## 2. Sync Mechanism (LOCKED)

### CLI: Local-First with Optional Cloud Sync

The CLI always writes to local SQLite + local files. If cloud is configured, it syncs automatically after each write operation. If cloud is not configured, writes succeed locally and sync is silently skipped.

### Write Flow

**CLI writes:**
1. Write to local SQLite + save figures/data locally.
2. If cloud configured: automatically push to Neon + upload to S3.
3. After successful Neon write: update `s3://_version` (retry up to 3 times on failure).
4. If cloud NOT configured: silently skip sync. The write already succeeded locally.

**Web UI writes (via Next.js API routes):**
1. Write to Neon Postgres. Sync version auto-increments via trigger.
2. Upload figures to S3 via presigned URL (if applicable).
3. After successful Postgres commit: update `s3://_version` (retry up to 3 times on failure).

**S3 `_version` update ordering:** Always write-after — `s3://_version` is bumped only after the Postgres write succeeds. This prevents a race where another client polls S3, sees the version change, queries Postgres, and gets stale data because the write hasn't committed yet. If the S3 update fails after all retries, the change is invisible to polling clients until the next successful write bumps `_version`.

### Bidirectional Sync (`pc sync`)

Explicit manual sync. **Errors if cloud is not configured** ("no cloud configured — run `pc setup` to configure cloud sync").

**Protocol:**
1. Acquire file lock (`.pc/sync.lock`) to prevent concurrent sync.
2. Capture `last_sync_at` at sync **start** (not end) — changes made during the sync window are caught on the next sync.
3. **Push:** Find local slides where `updated_at >= last_sync_at`. UPSERT into Neon. Upload changed figures to S3. Match child rows (`slide_figures`, `slide_data_files`) by `(slide_id, filename)`, never by auto-increment `id`. (No `deleted_at` check needed — the DB trigger auto-bumps `updated_at` on soft-delete/restore.)
4. **Pull:** Find Neon slides where `updated_at >= last_sync_at`. UPSERT into local SQLite. Download changed figures. Match child rows by `(slide_id, filename)`.
5. Update `last_sync_at` only after both phases complete fully.
6. Release lock.

### Automatic vs Manual Sync

| Context | Behavior |
|---------|----------|
| `pc add`, `pc edit`, `pc delete`, `pc restore`, `pc move` with cloud configured | Automatic sync after the local write succeeds |
| `pc gc` with cloud configured | Cloud-first-then-local: deletes from Neon + S3 first, then local SQLite + filesystem. Not the auto-sync pattern — gc orchestrates its own cloud operations directly. |
| `pc add`, `pc edit`, `pc delete`, `pc restore`, `pc move`, `pc gc` without cloud | Local write only, sync silently skipped |
| `pc sync` with cloud configured | Full bidirectional push+pull |
| `pc sync` without cloud | Error: "no cloud configured — run `pc setup` to configure cloud sync" |

### Conflict Resolution

Most-recent-action-wins using timestamps.

| Scenario | Resolution |
|----------|-----------|
| New slide, no conflict | Push inserts into Neon |
| New slide, same day_order | Both exist, `id` tiebreaker in sort |
| Edit on one side only | Push or pull updates the other |
| Edit same slide on both | Later `updated_at` wins |
| Delete vs edit conflict | Compare `deleted_at` vs `updated_at`. Most recent wins. |
| Timestamp tie (edit vs delete) | Edit wins — preserving data is safer. Deterministic tiebreaker. |
| Resurrection (edit wins over delete) | Clear `deleted_at` to NULL. `updated_at` is the winning timestamp. |
| Restore (un-delete) | `updated_at` auto-bumped by trigger, making the change visible to sync via `updated_at >= last_sync_at`. |

### Web UI Sync (Smart Layered Polling)

Four layers, 30-second global cooldown:

**Layer 1 — Manual refresh:** Always works, ignores cooldown.
**Layer 2 — Interaction-driven:** Check on clicks/navigation, subject to cooldown.
**Layer 3 — Tab visibility:** Check when tab becomes visible, subject to cooldown.
**Layer 4 — Idle polling:** Idle < 10 min: every 60s. Idle > 10 min: every 5 min. Tab hidden: nothing.

All version checks (Layers 2–4) go through `GET /api/sync/version`, which reads S3 `_version` server-side (not Postgres). This keeps Neon asleep on the free tier (scales to zero after 5 min inactivity). Neon only wakes when a version change is detected and the UI fetches slide data. Cost: ~$0.006/month for Lambda invocations.

On change detected: query Neon for slides with `updated_at >= last_known_timestamp`. If a version bump produces no incremental changes, perform a full reconciliation (hard-deleted rows are invisible to incremental queries — see `pc gc` edge case).

### Sync Cost

| Scenario | Monthly cost |
|----------|-------------|
| CLI (on-demand sync) | $0 |
| Web UI (layered polling) | ~$0.01 |

### Future Upgrade Path

Real-time push if needed: AWS IoT Core MQTT (~$0.05/year) or Pusher/Ably free tier.

---

## 3. Tools & Technologies (LOCKED)

| Component | Decision |
|-----------|---------|
| Database (cloud) | Neon Postgres (provider-portable) |
| Database (local) | SQLite (`modernc.org/sqlite`, pure Go) |
| Binary storage (cloud) | AWS S3 (Intelligent-Tiering, `us-east-1`) |
| Binary storage (local) | Local filesystem |
| Version history | GitHub (nightly export via Action + Git LFS) |
| CLI language | Go (`cobra`, `pgx`, `aws-sdk-go-v2`) |
| UI framework | Next.js (React, API routes) |
| UI hosting | AWS Amplify (native Next.js, SSR via Lambda) |

### Shared Schema Strategy

`schema.sql` is the **design-level** source of truth (Postgres dialect). The **executable** sources of truth are the migration files: `cli/migrations/postgres/` and `cli/migrations/sqlite/` (separate DDL due to dialect differences). Go structs and TypeScript interfaces (`schema-types.ts`) are maintained manually in the same code repo. At 5 tables, automated code generation is not justified. Update all four (schema.sql, both migration directories, schema-types.ts) together when the schema changes. Use `golang-migrate` for Go and raw SQL for Next.js migrations.

---

## 4. JSON Schema & Data Model (LOCKED)

Full DDL: `schema.sql` | Full types: `schema-types.ts`

### Tables

**slides:** `id, date, day_order, html_content, notes, project_id, git_remote_url, git_hash, created_at, updated_at, deleted_at`
**slide_figures:** `id, slide_id (FK CASCADE), filename, s3_key, alt_text, created_at`
**slide_data_files:** `id, slide_id (FK CASCADE), filename, s3_key, size, hash, description, created_at`
**templates:** `name (PK), html_content, description, created_at, updated_at`
**sync_version:** `id (always 1), version, updated_at`

### Key Design Decisions

- **No title. No tags.** Organization by project and date.
- **Same schema for Postgres and SQLite.** Dialect differences in code.
- **Dedicated tables for figures and data files** (not JSONB) — type safety for agent writers.
- **No project table in MVP** — `project_id` is a slash-convention string.
- **Fractional index strings** for `day_order` — reorder one slide without touching others.
- **Sort key `(date, day_order, id)`** — deterministic always.
- **Soft deletes via `deleted_at`** — sync, trash/restore, 30-day cleanup.
- **All timestamps UTC** — `created_at` and `updated_at` are DB-managed via defaults and triggers. Sync/import bypasses triggers with explicit values.
- **Full-text search V1** — `ILIKE` on `html_content`, `notes`, `project_id`.
- **`s3_key` is a canonical relative path** — e.g., `figures/20250304-a3f2b7e1/loss-curve.png`. Same value used for S3 key and local filesystem relative path, regardless of mode. Column name kept as `s3_key` for clarity in cloud mode.
- **Empty notes normalized to NULL** — at write time. `has_notes` = `notes IS NOT NULL`. Eliminates ambiguity between empty string and NULL.
- **`git_remote_url` and `git_hash`** — optional nullable TEXT columns on slides for linking slides to source code commits. CLI input: set via `metadata.json` in the input folder only (no CLI flags). Web UI: editable via `PATCH /api/slides/[id]`. No JSONB column — data files (e.g., `context.json` in `data/`) serve the purpose of arbitrary unstructured metadata.

### Figure References in HTML

`html_content` references figures using relative paths: `figures/{filename}` (e.g., `<img src="figures/loss-curve.png">`). The slide's `id` is implicit — determined by the rendering context.

| Context | Resolution |
|---------|-----------|
| Web UI (iframe) | Rewrite `figures/{filename}` → presigned URL via `GET /api/files/{slide_id}/figures/{filename}` |
| Git export | `./figures/{filename}` relative to slide folder — natural match |
| `pc add` / `pc edit` | Validate every `figures/` src in HTML has a matching file in the input folder |

**Rules:**
- Only `figures/{filename}` references are rewritten. External URLs (`https://...`) pass through unchanged.
- Data files are attachments, not inline content — not referenced in HTML.
- Agents and manual users produce standard HTML with relative figure paths.

### JSON Export (metadata.json)

Not a 1:1 database dump:
- `html_content` → `slide.html` sibling file
- `notes` → `notes.md` sibling file; `has_notes` boolean in JSON
- Figures/data_files inlined as arrays (no auto-increment IDs)
- `deleted_at` never exported

```json
{
  "format_version": 1,
  "id": "20250304-a3f2b7e1",
  "date": "2025-03-04",
  "day_order": "a",
  "project_id": "happy-ai/sleep-staging",
  "git_remote_url": "https://github.com/happy-ai/sleep-staging",
  "git_hash": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
  "has_notes": true,
  "figures": [
    {
      "filename": "loss-curve.png",
      "s3_key": "figures/20250304-a3f2b7e1/loss-curve.png",
      "alt_text": "Training loss over 50 epochs"
    }
  ],
  "data_files": [
    {
      "filename": "training-log.csv",
      "s3_key": "data/20250304-a3f2b7e1/training-log.csv",
      "size": 2048000,
      "hash": "ab3f2c8d...",
      "description": "Epoch-level training metrics"
    }
  ],
  "created_at": "2025-03-04T14:32:00Z",
  "updated_at": "2025-03-04T16:10:00Z"
}
```

---

## Data Integrity & Conversion Testing (REQUIRED)

Dedicated test suite. Two guarantee tiers depending on the conversion path.

### Tier 1 — Full Lossless (Local ↔ Cloud Sync)

All database fields, all figures, and all data files are byte-for-byte identical after round-trip.

```
Path A: Local SQLite + files → pc sync → Neon + S3 → pc sync → Local SQLite + files
```

### Tier 2 — Narrowed (Git Export Paths)

All database fields of active (non-deleted) slides and all figures are lossless. Data file **references** (metadata in `slide_data_files`) are preserved, but data file **binary content** is not included in git export — it remains in S3. Soft-deleted slides and `deleted_at` are excluded from export. Full recovery = git clone + S3.

```
Path B: Neon + S3 → pc export → git → pc restore-db → Neon         (git round-trip)
Path C: Local + files → pc export → git → pc restore-db → Local    (git round-trip from local)
Path D: Neon → pc sync → Local → pc export → git                   (full chain)
Path E: git → pc import → Local SQLite                              (merge import)
```

### Verification

**Tier 1:** All fields identical before and after: id, date, day_order, html_content, notes, project_id, git_remote_url, git_hash, created_at, updated_at. All figure and data file references intact. All binary content byte-for-byte (SHA-256).

**Tier 2:** All fields of active (non-deleted) slides identical. All figure binary content byte-for-byte. Data file references intact (filename, s3_key, size, hash, description). Data file binary content not verified (not in git export). Soft-deleted slides and `deleted_at` excluded from export.

### Edge Cases

- Minimal slide (no figures, no data, no notes, no project)
- Large slide (20+ figures, 100KB+ HTML, 50KB+ notes)
- Unicode in HTML and notes
- Special characters in filenames
- Multiple slides same date with different day_order
- Slide date differs from created_at
- Soft-deleted slides (sync correctly, exclude from export)
- Empty database
- `pc import` with overlapping IDs into non-empty database

### Implementation

`pc verify` runs full round-trip suite. `pc export --verify` exports then restores to temp schema and diffs. CI/CD on every schema or sync code change.

---

## 5. CLI Architecture & Commands (LOCKED)

### Architecture

Go CLI always writes to local SQLite + local files. Cloud sync is automatic after writes when configured, silently skipped when not. Explicit `pc sync` errors without cloud config.

**Local-only:** `brew install pc && pc setup` (answer "n" to cloud config)
**Cloud:** `brew install pc && pc setup` (answer "y" to cloud config, or provide all flags)

### Commands

```
SETUP & HEALTH
  pc setup                    — first-time setup or reconfigure (idempotent, --remove-cloud to disable)
  pc doctor                   — check health

SLIDE CRUD
  pc add <path>               — create slide from folder
  pc edit <id> <path>         — replace slide content from folder
  pc delete <id>              — soft-delete
  pc restore <id>             — un-delete
  pc move <id>                — change date or position
  pc show <id>                — display slide details

TRASH
  pc trash                    — list soft-deleted slides
  pc gc                       — hard-delete trash older than 30 days

SEARCH
  pc search <query>           — text search across slides

PROJECTS
  pc project set <project_id> — set active project
  pc project clear            — clear active project
  pc project list             — list all known projects

SYNC
  pc sync                     — bidirectional cloud sync (errors if no cloud)
  pc fetch                    — download data files on demand

DATA MANAGEMENT
  pc export                   — export to git folder format
  pc import <path>            — merge from git export (non-destructive)
  pc restore-db <path>        — rebuild from git export (destructive)
  pc verify                   — data integrity tests
```

### `pc doctor`

**Checks:**
- Local database exists and is readable
- Schema version is current
- Local figures directory is accessible
- If cloud configured: Neon connection works, S3 bucket is accessible
- Reports any orphaned figure references (figure in DB but file missing locally)

### `pc add <path>`

Input folder:
```
my-slide/
├── slide.html         ← required
├── notes.md           ← optional
├── metadata.json      ← optional (project_id, git_remote_url, git_hash)
├── figures/           ← optional
│   └── loss-curve.png
└── data/              ← optional (data files to attach)
    └── training-log.csv
```

Flags:
```
--project "happy-ai/sleep-staging"   # defaults to active project
--date "2025-03-04"                  # defaults to today (local timezone)
--position "after:20250304-a3f2b7e1"     # defaults to end of day
```

Behavior:
1. Validate folder structure (`slide.html` must exist, validate HTML is parseable)
2. Read `metadata.json` if present (project_id, git_remote_url, git_hash), merge with flags (flags override for project_id)
3. If `--project` not set and active project exists, use active project
4. Generate slide ID: `{YYYYMMDD}-{8-random-hex}` (date from local timezone)
5. Generate `day_order` (end of target date by default, or relative to `--position`)
6. Insert into local SQLite
7. Copy figures to `~/personal-context/figures/{slide_id}/`
8. Copy data files to `~/personal-context/data/{slide_id}/` and register in `slide_data_files` (with SHA-256 hash and file size)
9. If cloud configured, automatically sync (push slide + upload figures/data to S3)
10. Print: slide ID

### `pc edit <id> <path>`

Same input folder as `pc add` (including optional `data/`). Full replacement of html_content, notes, figures, and data files. Git fields (`git_remote_url`, `git_hash`) come from `metadata.json` in the input folder — same contract as `pc add`, no CLI flags for these fields.

Preserves: id, date, day_order, created_at.
Updates: html_content, notes, project_id, git_remote_url, git_hash, figures, data_files. `updated_at` auto-bumped by trigger.

**Important:** Any change to a slide's figures or data files must also bump the parent slide's `updated_at`. This ensures sync detects the change via the slide's timestamp. The `pc edit` command handles this automatically since it UPDATEs the parent slide row, triggering the `auto_update_updated_at` trigger.

Old figures and data files removed from local filesystem. New ones copied in. If cloud configured, automatically syncs.

### `pc show <id>`

Displays full slide details:
- Metadata: id, date, day_order, project_id, git_remote_url, git_hash, created_at, updated_at
- Notes: full markdown content (or "no notes")
- Figures: list of filenames
- Data files: list of filenames with sizes

Does NOT dump raw HTML content. The HTML lives in the input folder or can be viewed in the web UI.

Flags:
```
--format text        # default: human-readable
--format json        # machine-readable
```

### `pc search <query>`

Flags:
```
--format table|ids|json    # default: table
--limit 20                 # default: 20
--project "..."            # filter
--deleted                  # include soft-deleted
```

Table output: `ID | DATE | PROJECT`. IDs output: one per line (for piping/agent use).

### `pc move <id>`

Flags (at least one required):
```
--date "2025-03-05"
--position after:ID | before:ID | first | last
```

If date changes, day_order resets to end of new date (unless --position also set). Only the moved slide is updated. If cloud configured, automatically syncs.

### `pc fetch`

Downloads data files from S3. By default, files go to `~/personal-context/data/{slide_id}/`. Optionally, specify a different output directory (useful for agents that want their own working copy).

```
pc fetch <slide_id>                           # all data files for one slide
pc fetch --project "happy-ai/sleep-staging"   # all data files for a project
pc fetch --recent 3m                          # all data files from last 3 months
```

Flags:
```
--output "./my-dir"    # optional: download to a specific directory instead of the default data path
```

**Errors if cloud is not configured** (data files only exist in S3 for cloud users; local-only users already have all their data locally).

### `pc export`

Flags:
```
--path "./output"              # where to write the export (default: ./pc-export/)
--github-remote "origin"       # optional: commit and push to this git remote after export
--from-cloud                   # export from Neon instead of local SQLite (for GitHub Action use)
--verify                       # round-trip verify after export
```

With `--github-remote`: after generating the export, runs `git add . && git commit && git push {remote}`. This is how local-only users can manually push snapshots to GitHub.

### Export, Import, Restore-DB

**`pc export`** — generates git folder structure from the current database. Works from local SQLite (default) or Neon (if cloud configured and `--from-cloud` flag is added, for the GitHub Action use case). Does NOT require cloud to be configured — local-only users can export at any time.

**`pc import <path>`** — merges slides from git export into existing database. Matching IDs: skip or update by `updated_at`. New IDs: insert. Nothing deleted. Copies figures from the export folder into `~/personal-context/figures/`. Entry point for Google Slides migration.

**`pc restore-db <path>`** — wipes local database, rebuilds entirely from git export. Copies all figures from the export folder into `~/personal-context/figures/`. Disaster recovery. To restore to Neon: `pc restore-db <path>` then `pc sync` (push phase populates Neon from the restored local DB). Data file binaries are not in the git export — they remain in S3.

Both share code. Difference is whether database is wiped first.

### `pc setup`

Idempotent. Run anytime. No `--cloud` flag — setup detects what's needed and asks.

**Non-interactive mode (all flags provided):**
```
pc setup --neon-url="..." --s3-bucket="..." --s3-region="..." --aws-key="..." --aws-secret="..."
```
All required flags must be provided. AWS credentials are written to `~/.aws/credentials` under a `[personal-context]` profile (never stored in `config.json`). If any are missing, error: "Cloud setup requires all credentials. Missing: --s3-bucket, --aws-key. Provide all flags or run `pc setup` interactively." No merge preview or confirmation prompts in non-interactive mode.

**Removing cloud config:**
```
pc setup --remove-cloud
```
Removes cloud configuration from `config.json` and the `[personal-context]` profile from `~/.aws/credentials`. Does NOT delete cloud data (Neon, S3) or local data. CLI reverts to local-only mode. Reversible by running `pc setup` again.

**Interactive flow:**

```
$ pc setup

── Local Setup ──────────────────────────────────────────────────

Local database: ~/personal-context/.pc/pc.db
Status: ✓ Already exists (3,247 slides, 12 projects)
         (or: Creating... ✓ Done)

Templates: ✓ Up to date
           (or: Seeding... ✓ Done)

── Cloud Setup ──────────────────────────────────────────────────

Cloud sync is not configured.

Cloud sync enables:
  • Bidirectional sync between multiple machines
  • Web UI access via AWS Amplify
  • Nightly GitHub backups

See https://github.com/conn-castle/personal-context/blob/main/README.md
for detailed setup instructions (Neon account, S3 bucket, AWS credentials).

Would you like to configure cloud sync? (y/n): y

Neon Postgres connection string
  Example: postgresql://user:password@ep-cool-name-123.us-east-2.aws.neon.tech/personal_context
  > postgresql://nick:abc123@ep-wild-fog-456.us-east-2.aws.neon.tech/pc_prod

S3 bucket name
  Example: personal-context-prod
  > personal-context-prod

S3 region
  Default: us-east-1
  >

AWS Access Key ID
  Example: AKIAIOSFODNN7EXAMPLE
  > AKIA...

AWS Secret Access Key
  Example: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
  > ****

── Validating (using provided credentials in memory) ───────────

Neon connection... ✓ Connected
S3 bucket access... ✓ Bucket exists and is writable

── Merge Preview ────────────────────────────────────────────────

Local:     3,247 slides across 12 projects
Cloud:     1,892 slides across 8 projects
Post Sync: 4,134 slides across 13 projects

Proceed with setup? (y/n): y

── Finalizing ───────────────────────────────────────────────────

Creating Neon tables... ✓ Done (or: ✓ Tables already exist)
Saving AWS credentials to ~/.aws/credentials [personal-context]... ✓ Done
Saving config to ~/personal-context/.pc/config.json (0600)... ✓ Done

Setup complete. Run `pc sync` to synchronize.
```

**Idempotency behavior:**
- Local exists? Skip creation, show current state.
- Cloud already configured? Show current config, ask if user wants to reconfigure.
- Templates outdated? Re-seed.
- Tables exist in Neon? Skip creation.

**Credential storage:** `config.json` (0600 permissions) stores Neon connection URL, S3 bucket name, S3 region, and AWS profile name (`"personal-context"`). Cloud mode is detected by the presence of `neon_url` and `aws_profile` — no separate cloud-enabled flag. **AWS credentials are never stored in `config.json`** — `pc setup` writes them to `~/.aws/credentials` under a `[personal-context]` profile. The Go SDK loads this profile via `config.WithSharedConfigProfile("personal-context")`. If `~/.aws/credentials` already exists, only the `[personal-context]` section is added or updated — other profiles are untouched.

**No partial success:** All input gathered and validated (Neon connection + S3 access) before anything is written. If either validation fails, nothing is saved. User fixes the issue and runs `pc setup` again.

**Merge preview:** Queries both databases, diffs slide IDs to compute accurate post-sync counts (unique to local + unique to cloud + shared). This is a real set comparison, not a simple sum. Helps the user catch "wrong database" mistakes before committing.

---

## 6. UI/UX Design

> **Status:** Left to implementer. Key constraints and required features below.

### Slide Viewer
- 16:9 slide container, scales to fill width. No enforced padding/margins/fonts. White background default.
- Virtual date slides injected at render time.
- Filter by project.
- Reorder slides via intra-day drag-and-drop (updates day_order via API route). Cross-date moves require the CLI (`pc move --date`).

### Slide Details
- View rendered notes (markdown) for any slide.
- View list of figures with filenames.
- View list of data files with filenames and sizes.
- Download individual data files from S3.
- View/render any markdown files in data attachments.

### Editing
- Basic slide editing from UI (project, notes, git_remote_url, git_hash).
- Soft delete from UI.
- Trash view with restore.

### Sync
- Smart layered sync with 30-second global cooldown (Layers 1–4 as defined in Section 2).

---

## 7. Migration (Future Phase)

Convert existing Google Slides decks into git export folder format. Use `pc import` to merge into database. Preserve chronological ordering. Extract speaker notes → notes field.

---

## Future Features (Not in MVP)

- **Agent skill interface:** Dedicated skill/template system for CLI-based slide creation.
- **Agentic chat panel in web UI.**
- **Real-time push sync** (IoT Core / Pusher / Ably).
- **Multi-user auth** with per-user S3 namespace.
- **Mobile-optimized UI.**

---

## Cost Summary (Approximate)

> **Note:** These are approximate estimates as of early 2026. Verify current pricing for Neon, AWS S3, AWS Amplify, and GitHub LFS before production deployment.

**1 user after 2 years:**

| Component | Cost |
|-----------|------|
| Neon Postgres (free tier) | $0 |
| S3 (~15GB) | ~$0.15 |
| Amplify | ~$0–$1 |
| Sync | ~$0.01 |
| GitHub + LFS | $0 |
| **Total** | **~$0.16–$1.16/mo** |

**Local-only user:** $0/mo.

| | 1 user | 10 users | 100 users |
|---|---|---|---|
| Neon | $0 | $0–$19 | ~$19 |
| S3 | $0.15 | $1.50 | $15 |
| Amplify | $0–$1 | ~$2 | ~$10 |
| Sync | $0.01 | $0.10 | $1 |
| **Total** | **~$0.16–$1.16** | **~$4–$23** | **~$45** |

---

## Resolved Questions

- **Next.js API route design** — endpoints and payload shapes are defined in CONTEXT.md (API Routes section) and ROADMAP.md (Phase 9).
- **Data file sync** — MVP: data files are on-demand only via `pc fetch`. No automatic data file sync. Project-based and time-window sync deferred to post-MVP.
- **Git LFS storage limits** — monitor during use; not a blocking design question.
