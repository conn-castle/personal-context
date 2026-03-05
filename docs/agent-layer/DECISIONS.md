# Decisions

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
A rolling log of important, non-obvious decisions that materially affect future work (constraints, deferrals, irreversible tradeoffs). Only record decisions that future developers/agents would not learn just by reading the code. Do not log routine choices or standard best-practice decisions; if it is obvious from the code, leave it out.

## Format
- Keep entries brief and durable (avoid restating obvious defaults).
- Keep the oldest decisions near the top and add new entries at the bottom.
- Insert entries under `<!-- ENTRIES START -->`.
- Line 1 starts with `- Decision YYYY-MM-DD <id>:` and a short title.
- Lines 2–4 are indented by **4 spaces** and use `Key: Value`.
- Keep **exactly one blank line** between entries.
- If a decision is superseded, add a new entry describing the change (do not delete history unless explicitly asked).

### Entry template
```text
- Decision YYYY-MM-DD abcdef: Short title
    Decision: <what was chosen>
    Reason: <why it was chosen>
    Tradeoffs: <what is gained and what is lost>
```

## Decision Log

<!-- ENTRIES START -->

- Decision 2026-03-05 b3c4d5: Figma's fractional indexing for day_order
    Decision: Use Figma's fractional indexing algorithm (Go port) for `day_order` strings.
    Reason: Industry standard for collaborative reordering. Lexicographic sort, only moved item updated.
    Tradeoffs: String length grows with repeated insertions between same positions; acceptable for 1-2K slides/year.

- Decision 2026-03-05 c5d6e7: s3_key stores canonical relative path regardless of mode
    Decision: `s3_key` always stores relative path (e.g., `figures/20250304-a3f2b7e1/loss-curve.png`) whether or not S3 is configured.
    Reason: Same value works as S3 key and local filesystem relative path. Enables seamless mode transitions.
    Tradeoffs: Column name `s3_key` slightly misleading in local-only mode; clarity in cloud mode wins.

- Decision 2026-03-05 f1a2b3: last_sync_at captured at sync start
    Decision: Record `last_sync_at` at the beginning of sync (before push), not at the end.
    Reason: Changes made to cloud during sync window would be missed otherwise.
    Tradeoffs: May re-process some slides on next sync; UPSERT makes this safe.

- Decision 2026-03-05 e1f2a3: Sandboxed iframes for slide HTML rendering
    Decision: Render slide HTML in sandboxed iframes with `transform: scale()` for 16:9 containers.
    Reason: Arbitrary HTML in main document risks XSS and style leakage, even single-user (agents generate HTML).
    Tradeoffs: Performance cost of iframes; mitigated by virtualization.

- Decision 2026-03-05 b7c8d9: Auto-sync failure is non-fatal warning
    Decision: When auto-sync after CLI write fails, command succeeds (exit 0) and prints warning.
    Reason: Local-first design. Change will sync on next successful pc sync or auto-sync.
    Tradeoffs: User may not notice sync failure; warning message mitigates.

- Decision 2026-03-05 d1e2f3: pc gc resurrection documented, tombstones deferred
    Decision: Document that pc gc should be followed by pc sync on all machines. Tombstone mechanism deferred.
    Reason: Machine A hard-deletes, Machine B could re-push. Tombstones add complexity for an edge case.
    Tradeoffs: Deleted slides can reappear; documented mitigation sufficient for 1-2 machines.

- Decision 2026-03-05 f5a6b7: Timestamp tie — edit wins over delete
    Decision: When updated_at equals deleted_at to microsecond, edit wins and slide is active.
    Reason: Preserving data is safer than losing it. Deterministic tiebreaker.
    Tradeoffs: Simultaneous delete may not take effect; user can re-delete.

- Decision 2026-03-05 h3i4j5: No JSONB column — data files for arbitrary metadata
    Decision: Do not add a JSONB/JSON column for arbitrary unstructured metadata. Use data files (e.g., `context.json` in `data/`) instead.
    Reason: SQLite cannot query JSONB. Explicit columns are better for longevity and type safety. Data files already serve the purpose of attaching arbitrary structured data.
    Tradeoffs: Adding new structured metadata requires a schema change; acceptable for a 5-table system.

- Decision 2026-03-05 i5j6k7: Git fields — CLI via metadata.json only, web UI via PATCH
    Decision: CLI (`pc add`, `pc edit`): `git_remote_url` and `git_hash` come exclusively from `metadata.json` in the input folder. No `--git-remote-url` or `--git-hash` CLI flags. Web UI: edits git fields via `PATCH /api/slides/[id]` — a different interface, not contradictory.
    Reason: CLI uses folder-based input (single source of truth per operation). Web UI uses field-level mutation (different interface contract).
    Tradeoffs: Manual CLI users must create `metadata.json` to set git fields; acceptable since these fields are primarily for agent use.

- Decision 2026-03-05 j7k8l9: DB-managed timestamps via trigger
    Decision: `created_at` and `updated_at` are fully DB-managed. `created_at` via DEFAULT on INSERT. `updated_at` via BEFORE UPDATE trigger that auto-bumps to NOW() unless the value was explicitly changed. Sync/import bypasses the trigger by setting explicit timestamps.
    Reason: Prevents bugs like restore not bumping `updated_at`. Application code cannot forget to set timestamps. Simpler code.
    Tradeoffs: Different trigger syntax per dialect (Postgres BEFORE UPDATE, SQLite AFTER UPDATE). SQLite gets millisecond precision vs microsecond in Postgres; sufficient for single-user.

- Decision 2026-03-05 k9l0m1: Slide ID uses 8 hex chars (not 4)
    Decision: Slide ID format is `{YYYYMMDD}-{8-random-hex}` (e.g., `20250304-a3f2b7e1`). Changed from 4 hex chars before any data was persisted.
    Reason: 4 hex chars (65,536 per day) has ~50% collision probability over 10 years at 5 slides/day (birthday paradox). 8 hex chars (4.3 billion per day) makes collisions effectively impossible. ID is the primary sync/import key — collision is catastrophic.
    Tradeoffs: IDs are 4 chars longer; still human-readable and filesystem-safe.

- Decision 2026-03-05 l1m2n3: AWS credentials in ~/.aws/credentials [personal-context] profile
    Decision: `pc setup` collects AWS keys and writes them to `~/.aws/credentials` under a `[personal-context]` profile. `config.json` stores the profile name, bucket, region, and Neon URL — never AWS keys. Go SDK loads via `WithSharedConfigProfile("personal-context")`. `pc setup --remove-cloud` removes the profile.
    Reason: Standard AWS credential location. No new dependency (Go SDK reads it natively). Other profiles untouched. Clean interactive UX — user enters keys in `pc setup`, no manual file editing.
    Tradeoffs: Writing to `~/.aws/credentials` is outside the repo; standard practice for AWS tooling.

- Decision 2026-03-05 m3n4o5: Two-tier data integrity guarantee
    Decision: Full lossless guarantee for Local ↔ Cloud sync (all fields, all figures, all data files). Narrowed guarantee for git export paths: all database fields and figures lossless, data file *references* preserved but binary content requires S3.
    Reason: Data files stay in S3 only (not in git export). Git export is for slide content backup. Full recovery = git clone + S3.
    Tradeoffs: `pc restore-db` from git alone cannot recover data file binaries; documented and acceptable.

- Decision 2026-03-05 o7p8q9: S3 _version updated write-after with retry
    Decision: S3 `_version` is bumped only AFTER a successful Postgres commit, with up to 3 retries on failure. Never bumped before the DB write.
    Reason: Write-ahead causes a race condition: another client polls S3, sees the version change, queries Postgres, and gets stale data because the write hasn't committed yet. Write-after eliminates this. If all retries fail, the change is invisible until the next successful write — acceptable and self-healing.
    Tradeoffs: Brief window where a committed change is invisible if S3 update fails; extremely rare (S3 availability >99.99%).

- Decision 2026-03-05 q1r2s3: MVP data file sync is on-demand only
    Decision: Data files are fetched only via `pc fetch` (manual). No automatic data file sync in MVP.
    Reason: Simplest correct behavior. Data files can be large. Automatic sync scope (by project, time window) requires config and UX that isn't needed for a single user with `pc fetch`.
    Tradeoffs: User must manually fetch data files; acceptable for MVP. Project-based and time-window sync deferred.

- Decision 2026-03-05 t7u8v9: Sync cursors use >= with millisecond precision
    Decision: All sync and polling cursors use `>=` (not strict `>`). Effective timestamp precision is millisecond everywhere — Postgres microsecond timestamps truncated to ms when syncing to SQLite.
    Reason: Strict `>` can miss boundary updates when `last_sync_at` equals a slide's `updated_at`. Precision mismatch between Postgres (microsecond) and SQLite (millisecond) causes comparison drift. `>=` with idempotent UPSERT is safe — at most one redundant re-process per sync.
    Tradeoffs: Marginal re-processing cost (one slide per sync cycle); negligible for single-user.

- Decision 2026-03-05 u9v0w1: Figure references in HTML use relative paths
    Decision: `html_content` references figures as `figures/{filename}` (relative, no slide_id). Each rendering context resolves: web UI rewrites to presigned URLs; git export matches naturally (`./figures/{filename}` relative to slide folder). `pc add`/`pc edit` validate that every `figures/` src has a matching file.
    Reason: Standard HTML with relative paths. No custom protocol for agents to learn. Natural fit for git export structure.
    Tradeoffs: Requires per-context resolution logic; trivial (URL rewriting in iframe, validation in CLI).
