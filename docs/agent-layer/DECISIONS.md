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
- If a decision is superseded, replace the old entry with the new one. Fold the old entry's tradeoff context into the new entry's `Reason` field when it is still valuable, then remove the old entry.
- Periodically consolidate: remove entries that are now self-evident from the codebase (the decision is embodied in code, tests, or docs and a reader would learn it without the log). When removing, verify the tradeoff information is not uniquely preserved in the log.

### Entry template
```text
- Decision YYYY-MM-DD short-slug: Short title
    Decision: <what was chosen>
    Reason: <why it was chosen>
    Tradeoffs: <what is gained and what is lost>
```

## Decision Log

<!-- ENTRIES START -->

- Decision 2026-05-16 c4d5e6: Raw chat sources are Personal Context-owned with record-style canonical keys
    Decision: `pc chat import` copies the raw transcript into `<PC_HOME>/personal-context/chats/raw/{chat_session_id}/source.{json|jsonl|ndjson}`. `chat_session.original_source_path` retains the original imported path as provenance; `chat_session.raw_source_key` stores the canonical relative key for both local lookup and S3 object lookup (under `users/{user_id}/` in cloud mode). Key shape is enforced by CHECK constraint in canonical/SQLite/Postgres schemas and by the `filesystem.ValidateChatSourceKey` helper at every code boundary.
    Reason: Mirrors the record figure/data file model (one canonical relative key, no machine-specific absolute paths in storage references). Avoids split-brain identity between the managed file and provenance metadata, and lets `pc chat import --delete-source` safely remove the agent-owned original after Personal Context takes ownership.
    Tradeoffs: One managed raw file per chat session — multiple raw files per chat would require a separate `chat_source_files` table later. Changed imports write session/items in one SQLite batch, then promote staged raw files and only then delete originals. Multi-session batches can leave a tail of DB-committed sessions whose raw promotion never happened if the post-commit promote step fails mid-batch; the next `pc chat import` self-heals each affected session (append-only re-imports compare DB items and promote raw without duplicating suffix items; full re-imports fall back to ReplaceMode and churn `sync_version`). Unchanged re-imports are skipped entirely and do not bump sync state; skipping bypasses upsert (and its `ClearDeleted: true`), so an identical re-import no longer un-deletes a soft-deleted chat — use `pc chat restore` to revive one.

- Decision 2026-03-05 b3c4d5: Figma's fractional indexing for day_order
    Decision: Use Figma's fractional indexing algorithm (Go port) for `day_order` strings.
    Reason: Industry standard for collaborative reordering. Lexicographic sort, only moved item updated.
    Tradeoffs: String length grows with repeated insertions between same positions; acceptable for 1-2K records/year.

- Decision 2026-03-05 c5d6e7: s3_key stores canonical relative path regardless of mode
    Decision: `s3_key` always stores relative path (e.g., `figures/20250304-a3f2b7e1/loss-curve.png`) whether or not S3 is configured.
    Reason: Same value works as S3 key and local filesystem relative path. Enables seamless mode transitions.
    Tradeoffs: Column name `s3_key` slightly misleading in local-only mode; clarity in cloud mode wins.

- Decision 2026-03-05 f1a2b3: last_sync_at captured at sync start
    Decision: Record `last_sync_at` at the beginning of sync (before push), not at the end.
    Reason: Changes made to cloud during sync window would be missed otherwise.
    Tradeoffs: May re-process some records on next sync; UPSERT makes this safe.

- Decision 2026-03-05 e1f2a3: Sandboxed iframes for record HTML rendering
    Decision: Render record HTML in sandboxed iframes with `transform: scale()` for 16:9 containers.
    Reason: Arbitrary HTML in main document risks XSS and style leakage, even single-user (agents generate HTML).
    Tradeoffs: Performance cost of iframes; mitigated by virtualization.

- Decision 2026-03-05 b7c8d9: Auto-sync failure is non-fatal warning
    Decision: When auto-sync after CLI write fails, command succeeds (exit 0) and prints warning.
    Reason: Local-first design. Change will sync on next successful pc sync or auto-sync.
    Tradeoffs: User may not notice sync failure; warning message mitigates.

- Decision 2026-03-05 d1e2f3: pc gc resurrection documented, tombstones deferred
    Decision: Document that pc gc should be followed by pc sync on all machines. Tombstone mechanism deferred.
    Reason: Machine A hard-deletes, Machine B could re-push. Tombstones add complexity for an edge case.
    Tradeoffs: Deleted records can reappear; documented mitigation sufficient for 1-2 machines.

- Decision 2026-03-05 f5a6b7: Timestamp tie — edit wins over delete
    Decision: When updated_at equals deleted_at to microsecond, edit wins and record is active.
    Reason: Preserving data is safer than losing it. Deterministic tiebreaker.
    Tradeoffs: Simultaneous delete may not take effect; user can re-delete.

- Decision 2026-03-05 h3i4j5: No JSONB column — data files for arbitrary metadata
    Decision: Do not add a JSONB/JSON column for arbitrary unstructured metadata. Use data files (e.g., `context.json` in `data/`) instead.
    Reason: SQLite cannot query JSONB. Explicit columns are better for longevity and type safety. Data files already serve the purpose of attaching arbitrary structured data.
    Tradeoffs: Adding new structured metadata requires a schema change; acceptable for a 5-table system.

- Decision 2026-03-05 i5j6k7: Git fields — CLI via metadata.json only, web UI via PATCH
    Decision: CLI (`pc records add`, `pc records edit`): `git_remote_url` and `git_hash` come exclusively from `metadata.json` in the input folder. No `--git-remote-url` or `--git-hash` CLI flags. Web UI: edits git fields via `PATCH /api/records/[id]` — a different interface, not contradictory.
    Reason: CLI uses folder-based input (single source of truth per operation). Web UI uses field-level mutation (different interface contract).
    Tradeoffs: Manual CLI users must create `metadata.json` to set git fields; acceptable since these fields are primarily for agent use.

- Decision 2026-03-05 j7k8l9: DB-managed timestamps via trigger
    Decision: `created_at` and `updated_at` are fully DB-managed. `created_at` via DEFAULT on INSERT. `updated_at` via BEFORE UPDATE trigger that auto-bumps to NOW() unless the value was explicitly changed. Sync/import bypasses the trigger by setting explicit timestamps.
    Reason: Prevents bugs like restore not bumping `updated_at`. Application code cannot forget to set timestamps. Simpler code.
    Tradeoffs: Different trigger syntax per dialect (Postgres BEFORE UPDATE, SQLite AFTER UPDATE). SQLite gets millisecond precision vs microsecond in Postgres; sufficient for single-user.

- Decision 2026-03-05 k9l0m1: Record ID uses 8 hex chars (not 4)
    Decision: Record ID format is `{YYYYMMDD}-{8-random-hex}` (e.g., `20250304-a3f2b7e1`). Changed from 4 hex chars before any data was persisted.
    Reason: 4 hex chars (65,536 per day) has ~50% collision probability over 10 years at 5 records/day (birthday paradox). 8 hex chars (4.3 billion per day) makes collisions effectively impossible. ID is the primary sync/import key — collision is catastrophic.
    Tradeoffs: IDs are 4 chars longer; still human-readable and filesystem-safe.

- Decision 2026-03-05 l1m2n3: AWS credentials in ~/.aws/credentials [personal-context] profile
    Decision: `pc setup` collects AWS keys and writes them to `~/.aws/credentials` under a `[personal-context]` profile. `config.json` stores the profile name, bucket, region, and Neon URL — never AWS keys. Go SDK loads via `WithSharedConfigProfile("personal-context")`. `pc setup --remove-cloud` removes the profile.
    Reason: Standard AWS credential location. No new dependency (Go SDK reads it natively). Other profiles untouched. Clean interactive UX — user enters keys in `pc setup`, no manual file editing.
    Tradeoffs: Writing to `~/.aws/credentials` is outside the repo; standard practice for AWS tooling.

- Decision 2026-03-05 m3n4o5: Two-tier data integrity guarantee
    Decision: Full lossless guarantee for Local ↔ Cloud sync (all fields, all figures, all data files). Narrowed guarantee for git export paths: all database fields and figures lossless, data file *references* preserved but binary content requires S3.
    Reason: Data files stay in S3 only (not in git export). Git export is for record content backup. Full recovery = git clone + S3.
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
    Reason: Strict `>` can miss boundary updates when `last_sync_at` equals a record's `updated_at`. Precision mismatch between Postgres (microsecond) and SQLite (millisecond) causes comparison drift. `>=` with idempotent UPSERT is safe — at most one redundant re-process per sync.
    Tradeoffs: Marginal re-processing cost (one record per sync cycle); negligible for single-user.

- Decision 2026-03-05 u9v0w1: Figure references in HTML use relative paths
    Decision: `html_content` references figures as `figures/{filename}` (relative, no record_id). Each rendering context resolves: web UI rewrites to presigned URLs; git export matches naturally (`./figures/{filename}` relative to record folder). `pc records add`/`pc records edit` validate that every `figures/` src has a matching file.
    Reason: Standard HTML with relative paths. No custom protocol for agents to learn. Natural fit for git export structure.
    Tradeoffs: Requires per-context resolution logic; trivial (URL rewriting in iframe, validation in CLI).

- Decision 2026-03-05 w4x5y6: Custom migration runner with consolidated embedded schema
    Decision: Build a custom migration runner in `cli/internal/sqlite/` with a single canonical schema file (`sqlite_schema.sql`) embedded via `//go:embed`. The separate `cli/migrations/` package was removed in Phase 4.
    Reason: Simpler dependency graph, full control over SQLite-specific PRAGMA and connection hooks, no CGO requirement. Single schema file because there are no deployed users requiring multi-file migration history.
    Tradeoffs: Must maintain custom runner; complexity is low (single SQL file, idempotent schema_migrations table). Multi-file migrations can be reintroduced if needed for Postgres (Phase 5).

- Decision 2026-03-06 z6a7b8: Package-level function variable for test-only dependency injection
    Decision: Use `var resolveHomeDirFn = defaultResolveHomeDir` pattern in cli package to allow test-only injection of errors for otherwise-untestable paths (e.g., os.UserHomeDir failure).
    Reason: Covers 7 error paths (one per command) that cannot be reached via environment manipulation. Alternative (interface-based DI) would require threading a dependency through every command function for a single test concern.
    Tradeoffs: Unsafe with t.Parallel (tests must restore original via t.Cleanup). Acceptable since cli package tests are not parallel. See ISSUES.md b2c3d4.

- Decision 2026-03-05 y5z6a7: Playwright browser e2e runs in local mode with page.route mocks
    Decision: Playwright browser verification starts Next.js with `LOCAL_BACKEND_URL=http://127.0.0.1:9876` and uses `page.route()` interception instead of MSW or a real backend.
    Reason: Browser e2e should verify UI/browser wiring without cloud credentials or auth sessions. Browser-level route interception keeps tests close to real rendering without adding MSW while cloud behavior remains covered by API/unit tests and CLI cloud integration tests.
    Tradeoffs: Mocked browser e2e does not prove the hosted cloud auth flow end to end, and mocks remain per-test boilerplate; cloud data behavior is covered below the browser layer.

- Decision 2026-03-07 a1b2c3: S3 client constructor accepts pre-configured *s3.Client
    Decision: `s3client.New()` accepts a pre-configured `*s3.Client` and bucket name (same DI pattern as Postgres repo accepting `*pgxpool.Pool`).
    Reason: Keeps credential resolution, endpoint override, and region config out of the s3client package. Callers own their AWS configuration. Enables MinIO injection for integration tests without conditional logic.
    Tradeoffs: Caller must construct the `*s3.Client` themselves; acceptable since credential setup is a one-time concern in `pc setup` / test harness.

- Decision 2026-03-07 b3c4d5p: HeadVersion returns 0 for missing _version key
    Decision: `HeadVersion` returns version `0` (not an error) when the `_version` key does not exist in S3.
    Reason: Simplifies sync bootstrap: version 0 means "never synced." Callers do not need to distinguish "key missing" from "version is zero." First `UpdateVersion` creates the key.
    Tradeoffs: Cannot distinguish "bucket exists but no _version" from "version is literally 0"; not meaningful in practice since UpdateVersion always increments.

- Decision 2026-03-07 c5d6e7p: Schema equivalence guard compares structure not dialect
    Decision: `scripts/check_schema_equivalence.sh` compares tables, columns, indexes, UNIQUE constraints, and search-index structures between Postgres and SQLite schemas, but does NOT compare types, CHECK expressions, or non-search triggers.
    Reason: Type names (`TIMESTAMPTZ` vs `TEXT`), CHECK syntax, and most trigger syntax are intentionally different between dialects. Search structures are pinned because both backends depend on them for user-visible search correctness.
    Tradeoffs: A column type mismatch (e.g., wrong SQLite type) would not be caught; mitigated by integration tests exercising both backends against the same contract suite.

- Decision 2026-03-08 d7e8f9: S3Endpoint and S3ForcePathStyle config fields for S3-compatible services
    Decision: Added `s3_endpoint` and `s3_force_path_style` to Config. These are optional — empty/false means standard AWS S3. `Mode()` does not require them for cloud mode. Exposed as `--s3-endpoint` and `--s3-force-path-style` CLI flags (non-interactive only). Threaded through `openCloudStack`, `validateS3AccessFn`, and `runSetupCloud`.
    Reason: Enables MinIO and other S3-compatible services for integration testing without env var hacks. Also supports users who self-host S3-compatible storage.
    Tradeoffs: Two new config fields that most users won't need; `omitempty` keeps them invisible in default configs.

- Decision 2026-03-09 p7q8r9: Cloud-rooted exports use local seeded templates
    Decision: `pc export --from-cloud` and `pc verify --from-cloud` read records/figures from Postgres + S3, but template files from the local seeded template set.
    Reason: Templates are not part of the current cloud sync contract, yet cloud-rooted exports and round-trip verification still need deterministic `templates/*.html` output for the git snapshot format.
    Tradeoffs: Cloud-rooted export assumes the local setup has the canonical builtin templates. If templates become user-editable later, template sync/export semantics must be revisited.

- Decision 2026-03-09 r1s2t3: Metadata-only cloud sync for git-restored data files
    Decision: When a local record has a data-file row but no local binary, `pc sync` still creates or updates the cloud metadata row if the S3 key is unchanged (or the row is new). Changing the S3 key without a local binary still fails.
    Reason: `pc restore-db` and `pc import` intentionally preserve data-file references without binaries. The Tier 2 restore paths need a later sync to recreate database state in a fresh cloud without inventing fake file content.
    Tradeoffs: Cloud metadata can temporarily reference an object that is absent from the bucket until the binary is restored from the original S3 source or re-uploaded from disk. Sync remains strict for S3 key changes because there is no safe object to point at.

- Decision 2026-03-09 s3t4u5: Presentation components excluded from Vitest coverage thresholds
    Decision: `components/*.tsx` and `app/page.tsx` are excluded from Vitest coverage thresholds; primary behavioral coverage comes from Playwright e2e, with targeted component unit tests allowed when they capture high-value state transitions.
    Reason: These are mostly thin wrappers over UI libraries. Blanket unit coverage produces brittle prop-assertion tests, but a few local state paths are still worth pinning with focused component tests.
    Tradeoffs: Coverage numbers appear lower than route/hook code; contributors must use judgment to add only high-signal component tests.

- Decision 2026-03-09 u7v8w9: useSyncManager 4-layer polling instead of WebSocket
    Decision: `useSyncManager` uses 4-layer polling (manual, interaction, tab visibility, idle) instead of WebSocket for change detection.
    Reason: S3 `_version` file doesn't support push notifications. Polling is simpler and sufficient for low-frequency CLI-driven mutations.
    Tradeoffs: Changes are not instant (seconds of latency); acceptable for single-user CLI-to-web workflow.

- Decision 2026-05-07 v9w0x1: sync_version is per-user with tenant-scoped S3 version objects
    Decision: In cloud mode, `sync_version` is keyed by `user_id` and S3 version objects live under `users/{user_id}/_version`; SQLite remains a local singleton. Shared template changes create or bump every existing user's `sync_version` row.
    Reason: Multi-user cloud mode needs each user's polling cursor to advance only for that user's record mutations while preserving the simple single-version polling model within each tenant. Templates are shared, so every existing user's cursor must advance when templates change, including users who have not yet made record mutations.
    Tradeoffs: Fresh cloud bootstrap must create users/auth tables before API keys can be generated, and all cloud callers must carry user scope into Postgres and S3. Template changes touch all users; a separate shared-resource cursor would scale better for frequent template edits but would require widening the client polling contract.

- Decision 2026-03-09 a3b4c5: Visual regression baselines committed as darwin, Linux CI deferred
    Decision: Playwright visual regression baselines generated on macOS (darwin) and committed to git. `snapshotPathTemplate` removes platform from paths. Linux-based CI baselines deferred.
    Reason: Docker-based Linux baseline generation hit pnpm + radix-ui hoisting issues. darwin baselines are sufficient for local development; CI visual tests will need Linux baselines when set up.
    Tradeoffs: Visual tests will fail in Linux CI until Linux baselines are generated. `maxDiffPixelRatio: 0.02` tolerates minor rendering differences.

- Decision 2026-03-09 w1x2y3: v0.dev UI adopted as canonical frontend, adapted for real types
    Decision: Replaced hand-built UI components with v0.dev reference design components. Adapted `Record` type to `RecordSummary`/`RecordDetail` split. All hook orchestration lives in `SpreadsheetViewer`, not `page.tsx`. `resizable.tsx` wraps react-resizable-panels v4 API with v2-style `direction` prop for shadcn compatibility.
    Reason: User required visual parity with v0.dev reference. v0.dev used a single `Record` type with mock data; our real backend uses `RecordSummary` (list) and `RecordDetail` (selected). The v4 API renames `PanelGroup`→`Group`, `PanelResizeHandle`→`Separator`, `direction`→`orientation`.
    Tradeoffs: v0.dev ScaledRecordFrame lost figure URL resolution (moved to iframe rendering context). NotesEditor doesn't key on recordId (editing state may persist across record changes — tracked in ISSUES.md).

- Decision 2026-03-10 x3y4z5a: html_content included in RecordSummary for thumbnail rendering
    Decision: Keep nullable `html_content` on `RecordSummary` for `GET /api/records` and `GET /api/sync/changes`; thumbnails render HTML in `ScaledRecordFrame` only when non-null and use a notes/data fallback when null.
    Reason: v0.dev reference renders thumbnails from actual HTML, while Castle Vault records may legitimately omit `record.html`. With 20 records per page, the additional payload remains manageable.
    Tradeoffs: Larger API responses. If record HTML grows very large (100KB+), consider a separate thumbnail content endpoint or server-side thumbnail generation.

- Decision 2026-03-10 y5z6a7a: react-markdown + remark-gfm + mermaid for notes rendering
    Decision: Replaced custom inline MarkdownRenderer with `react-markdown` + `remark-gfm` + `mermaid` library. Mermaid code blocks are detected and rendered as SVG diagrams client-side.
    Reason: Custom renderer only supported basic markdown (headings, bold, code, lists, tables). Full GFM support (strikethrough, task lists, autolinks) and mermaid diagrams were requested.
    Tradeoffs: Three new dependencies (~220 packages total from mermaid). Mermaid is a large library; consider lazy loading if bundle size becomes a concern.

- Decision 2026-03-10 a8b9c0d: `pc serve` as Go REST server for local dev mode (dual-backend architecture)
    Decision: Local dev mode uses a Go HTTP server (`pc serve`) that implements the same REST API shape as the Next.js API routes, backed by the existing Go SQLite repository and local filesystem. Next.js API routes detect `LOCAL_BACKEND_URL` and proxy to the Go server. Cloud mode (Amplify) is unchanged — Node.js handles Neon + S3 directly.
    Reason: Three alternatives were evaluated: (1) Go reverse proxy replacing Node entirely — precludes cloud-deployed frontend on Amplify; (2) SQLite-in-Node via env var — adds SQLite dependency to Node, requires SQL dialect translation layer, pollutes the production web app; (3) Go REST server — reuses existing Go repository layer (24 methods, already tested), no new Node dependencies, and keeps the web-side changes isolated to a small proxy helper plus the affected API route handlers. The Go CLI already has full SQLite access, filesystem code, and contract tests for both SQLite and Postgres implementations.
    Tradeoffs: Two implementations of the same API logic (Go for local, Node for cloud). Mitigated by contract tests that call both backends with identical inputs and assert response parity + correctness. Both must stay in sync when API changes are made. Cloud `_version` now uses JSON `{version, updated_at}` as the canonical payload, while readers retain compatibility with legacy bare-integer objects.

- Decision 2026-03-13 d3e4f5: Auth.js credentials provider for web authentication (two-mode architecture)
    Decision: Use Auth.js (NextAuth v5) with Credentials provider and Neon Postgres adapter. Two modes: (1) local-only — CLI + SQLite, no auth; (2) web — Auth.js credentials, email/password. Self-hosted and managed are the same codebase; OAuth providers (Google, GitHub) can be added later via env vars with zero code changes. CLI authenticates via user-generated API keys (hashed, stored in `api_keys` table). Local dev mode (`pc serve`) bypasses auth entirely.
    Reason: Auth.js is open source (no vendor lock-in), has native Next.js App Router support, uses the existing Neon Postgres for session/user storage (no new infrastructure), and supports the 10+ year project lifespan. Credentials provider works for both self-hosted and managed without external OAuth app registrations. API keys are simpler than OAuth device flow for CLI auth.
    Tradeoffs: Credentials provider requires password hashing and storage (security surface). Auth tables are Postgres-only (no SQLite equivalent needed — local mode has no users). S3 keys gain a `users/{user_id}/` prefix, requiring migration logic if data exists pre-auth.

- Decision 2026-05-07 b4c5d6: Records require registry provenance and optional HTML
    Decision: Records may omit `record.html` (`html_content = NULL`), but every record must carry explicit registered `project_id` and `source_device_id`; `active_project` config is ignored by write paths.
    Reason: Notes/data-first vault records need to round-trip without fabricated HTML while retaining auditable provenance across SQLite, Postgres, sync, and git export/import.
    Tradeoffs: Existing folders and scripts must register/pass project and device values explicitly; this removes convenient hidden assignment but prevents silent misclassification.

- Decision 2026-05-07 c6d7e8: Source-available noncommercial release license
    Decision: License Personal Context under PolyForm Noncommercial 1.0.0 (`PolyForm-Noncommercial-1.0.0`) and describe the Homebrew formula as `Personal structured vault for searchable knowledge, data, files, and records`.
    Reason: The repo is public for Homebrew releases, but the owner wants to retain commercial rights and prevent third-party commercial use without separate permission.
    Tradeoffs: This is source-available, not open source by OSI criteria; some package ecosystems and commercial users may reject or require legal review before use.

- Decision 2026-05-08 e8f9g0: List/search JSON uses paginated envelope
    Decision: Domain-specific list/search JSON responses use `cli/internal/listpage.Response` with `{items,total,next_cursor}`; top-level `pc search --json`/`--format json` is the cross-domain exception and returns a flat array with `domain` on each item.
    Reason: Domain lists need totals/cursors, while cross-domain search has heterogeneous record/chat result shapes and is optimized for direct piping/export.
    Tradeoffs: Consumers must distinguish domain-specific search/list responses from top-level cross-domain search.

- Decision 2026-05-14 n2p3q4: Chat feature is a pre-release clean-cut schema update
    Decision: Add `project_paths`, chat tables, and search structures directly to fresh SQLite/Postgres bootstrap schemas; do not add forward migrations or database rewrite/delete behavior for this feature.
    Reason: The maintainer confirmed there are no prior users or prior data to preserve for this slice.
    Tradeoffs: Existing developer databases created before this feature must be recreated manually; future post-release schema changes still need a migration strategy.

- Decision 2026-05-14 r5s6t7: Chat import uses explicit device provenance and nullable project assignment
    Decision: `pc chat import` requires `--device`; project assignment is derived from registered `project_paths`, and unmatched sessions remain `project_id = NULL` until `pc project register <id> [path] --device <id>` backfills them.
    Reason: This mirrors record provenance rules and avoids hidden current-device/current-project defaults.
    Tradeoffs: First-time setup requires registering devices and paths before imports classify cleanly, but unassigned sessions stay visible for later review.

- Decision 2026-05-23 p8q6r2: SQLite local connections use WAL synchronous NORMAL
    Decision: Local SQLite connections set `PRAGMA synchronous = NORMAL` after enabling WAL.
    Reason: Large chat imports are dominated by many small WAL commit syncs; SQLite documents WAL+NORMAL as consistent and durable across application crashes while allowing the last transactions to roll back after OS crash or power loss.
    Tradeoffs: Re-runnable local imports gain much faster commit behavior, but a system crash can lose the most recent committed local transactions. Use higher-level re-import/sync repair rather than assuming power-loss durability for every import batch.

- Decision 2026-05-28 r6x7y8: Round-6 chat-import data safety — source identity, parent metadata, honest summary
    Decision: Claude Task-tool subagent transcripts (files under `subagents/`, or rows with `isSidechain`) get a file-unique `source_session_id` of `<parent_sid>:<subagent_basename>` plus a nullable indexed `parent_source_session_id`; Gemini transcripts (no usable internal id) derive `source_session_id` from the file path (project-key directory plus basename), with exact byte-identical duplicate files collapsed (`duplicates_skipped`); a scanned file that collides with a different file's existing `(source, source_session_id)` and diverges is warn-and-skipped (`collisions_skipped`), never overwritten; Gemini `gemini`/`info`/`error` rows normalize to `message`/`event` item types; empty or metadata-only transcripts create no session; the import summary reports work performed (`items_imported`) separately from authoritative stored state (`items_delta`, `items_after_import` via the repository `CountChatItems` source of truth), and `raw_sources_copied` counts distinct retained sessions.
    Reason: Every subagent file carries the PARENT `sessionId`, so basename-derived identity collided and silently overwrote siblings (data loss); path-derived Gemini identity stops project-name vs project-hash copies overwriting each other; `parent_source_session_id` is canonical source metadata that Claude provides before the parent row necessarily exists, so it is nullable and deliberately not a FK. The old `items_created` counter mixed work with net change and could not reconcile with the database.
    Tradeoffs: `parent_source_session_id` is unenforced by a FK (parent row may be absent or imported later); divergent collisions are surfaced but not auto-resolved (user investigates); the summary JSON field set changed (`items_created` → `items_imported`, added `items_delta`/`items_after_import`/`duplicates_skipped`/`collisions_skipped`) — acceptable pre-release and noted in CHANGELOG. Follows n2p3q4: the column was added to the fresh SQLite/Postgres schemas with no migration.

- Decision 2026-06-01 g7h8i9: gc retention is a nullable config field defaulting to 30 days
    Decision: `pc gc` retention is configured via `gc_retention_days` (a `*int` in `Config`). Unset (nil) means the product default of 30 days; an explicit value must be a positive integer or `Read`/`Write` fail. One window applies to both record and chat trash.
    Reason: A pointer distinguishes "legacy/unset config" from an explicit value so absent config can never be read as a silent zero-day (immediate-purge) retention, which would be a data-loss footgun.
    Tradeoffs: Set only by editing `config.json` (no dedicated `pc config` command yet, matching `active_project`); a future generic config-setter command would supersede that.

- Decision 2026-06-02 e4t6y1: web/lib/api-error.ts is the single source of truth for API error responses
    Decision: Every JSON error response from a web API route, lib helper, or `middleware.ts` must be built by an `api-error.ts` helper (`badRequest`, `unauthorized`, `conflict`, `invalidConfig`, `localBackendUnavailable`, etc.); no code emits an inline `NextResponse.json({ error, code })`. `middleware.ts` runs in the Edge runtime, but `api-error.ts` only depends on `NextResponse`/web-standard APIs (no Node-only imports), so it is Edge-safe to use there. The `ErrorCode` union enumerates exactly the codes the API can return, so a typo or unlisted code fails typecheck. The middleware's page-route fallback (`new NextResponse("Invalid LOCAL_BACKEND_URL configuration", { status: 500 })`) is a plain-text page response, not a JSON error body, and is intentionally outside this contract.
    Reason: Inline error responses had drifted from the `ErrorCode` union (the type omitted `UNAUTHORIZED`, `CONFLICT`, `INVALID_CONFIG`, `LOCAL_BACKEND_UNAVAILABLE`, `LOCAL_MODE_AUTH_DISABLED`, `REGISTRATION_DISABLED` while still declaring the unused `METHOD_NOT_ALLOWED`), so the wire contract was unenforced and partly inaccurate.
    Tradeoffs: New error kinds require adding both a union member and a helper, but that is the point — the compiler now guards the contract. Status/body shapes are unchanged from the previous inline responses (purely a centralization, no behavior change).

- Decision 2026-06-04 g7h2k4: Gemini project attribution derives cwd from the source; no project_hash column
    Decision: `pc chat import` attributes Gemini sessions by resolving the repo root from the source on disk — the sibling `.project_root` literal path (preferred), else the registered project path whose sha256 equals the session JSON's `projectHash` — and persisting it as the session `cwd`. The existing `MatchProjectPath` (at import) and `BackfillChatProjects` (on `pc project register`, matching `project_id IS NULL AND cwd IS NOT NULL`) then attribute Gemini exactly like codex/claude. The byte-unchanged ("exact match") import path also resolves and repairs unattributed Gemini rows, so a plain re-import self-heals an existing store without rewriting items.
    Reason: Persisting cwd recovers the only advantage of a dedicated `project_hash` column (re-attribution on a later register) without a schema change. PC has no incremental migration system (single embedded canonical schema + equivalence guard), so a new column would force a full rebuild and ongoing SQLite/Postgres maintenance. Keeping the signal derivable from the source respects single-source-of-truth.
    Tradeoffs: projectHash is one-way, so a hash-only session whose project is unregistered at import time gets no cwd and is NOT caught by a later `pc project register` (only by a re-import, which is idempotent). projectHash matching is exact-root only; sub-directory launches attribute only when a `.project_root` literal path is present. Gemini sessions outside any registered project stay unassigned (reported via `pc chat list --unassigned`, never guessed).

- Decision 2026-06-04 k8m5v2: Codex fork lineage reuses parent_source_session_id; cwd/title lock to the fork header
    Decision: Codex rollouts keep the first `session_meta.id` as `source_session_id`; later replayed parent headers cannot overwrite it. The first non-null `forked_from_id` is stored in `parent_source_session_id`. `cwd`/`title` are likewise locked to the fork's own header: once a fork (`parent_source_session_id != nil`) has set them, replayed parent `session_meta`/`turn_context` lines cannot overwrite them. Non-fork sessions keep last-wins (preserving mid-session `cd` and the old-rollout `turn_context` cwd backfill).
    Reason: A Codex fork is source-local lineage, the same relationship shape already modeled for Claude subagents, and the existing column/filter/index can represent it without a schema change or migration. A fork rollout replays the parent's metadata after the fork's own header, so unconditional last-wins attributed forks to the parent's cwd/project; the fork header is the authoritative fork identity, and Codex re-stamps replayed rows with fork-creation time so a timestamp-based discriminator is impossible — locking on first-set is the only reliable signal.
    Tradeoffs: `parent_source_session_id` now covers Claude subagent parents and Codex fork ancestors; future lineage queries should keep using the source filter when the distinction matters. A fork that `cd`s to a third directory after forking pins to the fork-creation dir rather than the latest — accepted, as the fork-creation dir is the defensible attribution and the case is an edge of an edge.

- Decision 2026-06-06 m9n4p2: SQLite chat search uses FTS5 rank-only page ordering
    Decision: Local SQLite `SearchChatItems` orders pages by `chat_item_fts.rank` and exposes `-chat_item_fts.rank` as score, without secondary SQL tie-breakers for equal-rank rows. Postgres keeps rank, recency, ordinal ordering.
    Reason: On the large local store, adding any secondary SQLite sort key (`last_activity_at`, ordinal, rowid, or id) reintroduced `USE TEMP B-TREE FOR ORDER BY` and full-match ranking before `LIMIT`; the FTS5 hidden-rank path is the bounded top-k performance fix.
    Tradeoffs: SQLite and Postgres can order equal-rank chat hits differently, so offset page boundaries for ties are not a cross-backend parity contract. SQLite preserves relevance ordering and pagination integrity, but not documented recency tie-breaking.
