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

- Decision 2026-03-05 v2w3x4: Web workspace uses Vitest for unit coverage gates
    Decision: Use Vitest (not Jest) as the canonical `web/` unit test runner and coverage gate (`pnpm test`, `pnpm test:coverage`).
    Reason: Roadmap allowed Vitest/Jest. Vitest offered a lean scaffold path with fast run times and simple 95% threshold enforcement.
    Tradeoffs: Next.js examples more commonly show Jest; contributors must follow the Vitest-specific config and scripts in this repo.

- Decision 2026-03-05 w4x5y6: Custom migration runner with consolidated embedded schema
    Decision: Build a custom migration runner in `cli/internal/sqlite/` with a single canonical schema file (`sqlite_schema.sql`) embedded via `//go:embed`. The separate `cli/migrations/` package was removed in Phase 4.
    Reason: Simpler dependency graph, full control over SQLite-specific PRAGMA and connection hooks, no CGO requirement. Single schema file because there are no deployed users requiring multi-file migration history.
    Tradeoffs: Must maintain custom runner; complexity is low (single SQL file, idempotent schema_migrations table). Multi-file migrations can be reintroduced if needed for Postgres (Phase 5).

- Decision 2026-03-06 z6a7b8: Package-level function variable for test-only dependency injection
    Decision: Use `var resolveHomeDirFn = defaultResolveHomeDir` pattern in cli package to allow test-only injection of errors for otherwise-untestable paths (e.g., os.UserHomeDir failure).
    Reason: Covers 7 error paths (one per command) that cannot be reached via environment manipulation. Alternative (interface-based DI) would require threading a dependency through every command function for a single test concern.
    Tradeoffs: Unsafe with t.Parallel (tests must restore original via t.Cleanup). Acceptable since cli package tests are not parallel. See ISSUES.md b2c3d4.

- Decision 2026-03-05 y5z6a7: Phase 1 Playwright gate is DB-free smoke only
    Decision: Phase 1 Playwright verification runs a DB-free smoke test (`pnpm test:e2e:smoke`) against a static app route via `playwright.config` `webServer`.
    Reason: Full DB-backed e2e setup belongs to later phases; Phase 1 needs reproducible e2e wiring without introducing fake backend logic.
    Tradeoffs: Phase 1 e2e only proves browser/server wiring, not data workflows; richer e2e scenarios remain required in later roadmap phases.

- Decision 2026-03-07 a1b2c3: S3 client constructor accepts pre-configured *s3.Client
    Decision: `s3client.New()` accepts a pre-configured `*s3.Client` and bucket name (same DI pattern as Postgres repo accepting `*pgxpool.Pool`).
    Reason: Keeps credential resolution, endpoint override, and region config out of the s3client package. Callers own their AWS configuration. Enables MinIO injection for integration tests without conditional logic.
    Tradeoffs: Caller must construct the `*s3.Client` themselves; acceptable since credential setup is a one-time concern in `pc setup` / test harness.

- Decision 2026-03-07 b3c4d5p: HeadVersion returns 0 for missing _version key
    Decision: `HeadVersion` returns version `0` (not an error) when the `_version` key does not exist in S3.
    Reason: Simplifies sync bootstrap: version 0 means "never synced." Callers do not need to distinguish "key missing" from "version is zero." First `UpdateVersion` creates the key.
    Tradeoffs: Cannot distinguish "bucket exists but no _version" from "version is literally 0"; not meaningful in practice since UpdateVersion always increments.

- Decision 2026-03-07 c5d6e7p: Schema equivalence guard compares structure not dialect
    Decision: `scripts/check_schema_equivalence.sh` compares tables, columns, indexes, and UNIQUE constraints between Postgres and SQLite schemas but does NOT compare types, CHECK expressions, or triggers.
    Reason: Type names (`TIMESTAMPTZ` vs `TEXT`), CHECK syntax, and trigger syntax are intentionally different between dialects. Structural equivalence (same tables with same columns and same indexes) is the meaningful invariant.
    Tradeoffs: A column type mismatch (e.g., wrong SQLite type) would not be caught; mitigated by integration tests exercising both backends against the same contract suite.

- Decision 2026-03-08 d7e8f9: S3Endpoint and S3ForcePathStyle config fields for S3-compatible services
    Decision: Added `s3_endpoint` and `s3_force_path_style` to Config. These are optional — empty/false means standard AWS S3. `Mode()` does not require them for cloud mode. Exposed as `--s3-endpoint` and `--s3-force-path-style` CLI flags (non-interactive only). Threaded through `openCloudStack`, `validateS3AccessFn`, and `runSetupCloud`.
    Reason: Enables MinIO and other S3-compatible services for integration testing without env var hacks. Also supports users who self-host S3-compatible storage.
    Tradeoffs: Two new config fields that most users won't need; `omitempty` keeps them invisible in default configs.

- Decision 2026-03-09 p7q8r9: Cloud-rooted exports use local seeded templates
    Decision: `pc export --from-cloud` and `pc verify --from-cloud` read slides/figures from Postgres + S3, but template files from the local seeded template set.
    Reason: Templates are not part of the current cloud sync contract, yet cloud-rooted exports and round-trip verification still need deterministic `templates/*.html` output for the git snapshot format.
    Tradeoffs: Cloud-rooted export assumes the local setup has the canonical builtin templates. If templates become user-editable later, template sync/export semantics must be revisited.

- Decision 2026-03-09 r1s2t3: Metadata-only cloud sync for git-restored data files
    Decision: When a local slide has a data-file row but no local binary, `pc sync` still creates or updates the cloud metadata row if the S3 key is unchanged (or the row is new). Changing the S3 key without a local binary still fails.
    Reason: `pc restore-db` and `pc import` intentionally preserve data-file references without binaries. The Tier 2 restore paths need a later sync to recreate database state in a fresh cloud without inventing fake file content.
    Tradeoffs: Cloud metadata can temporarily reference an object that is absent from the bucket until the binary is restored from the original S3 source or re-uploaded from disk. Sync remains strict for S3 key changes because there is no safe object to point at.

- Decision 2026-03-09 s3t4u5: Presentation components excluded from Vitest coverage thresholds
    Decision: `components/*.tsx` and `app/page.tsx` are excluded from Vitest coverage thresholds; primary behavioral coverage comes from Playwright e2e, with targeted component unit tests allowed when they capture high-value state transitions.
    Reason: These are mostly thin wrappers over UI libraries. Blanket unit coverage produces brittle prop-assertion tests, but a few local state paths are still worth pinning with focused component tests.
    Tradeoffs: Coverage numbers appear lower than route/hook code; contributors must use judgment to add only high-signal component tests.

- Decision 2026-03-09 t5u6v7: Playwright e2e uses page.route() API interception, not MSW
    Decision: Playwright e2e tests mock the backend via `page.route()` API interception instead of MSW or a real backend.
    Reason: Simplest approach — no extra dependencies, tests real frontend rendering while mocking network at the browser level.
    Tradeoffs: Mocks are per-test boilerplate; MSW would centralize them but adds a dependency for little gain at current test count.

- Decision 2026-03-09 u7v8w9: useSyncManager 4-layer polling instead of WebSocket
    Decision: `useSyncManager` uses 4-layer polling (manual, interaction, tab visibility, idle) instead of WebSocket for change detection.
    Reason: S3 `_version` file doesn't support push notifications. Polling is simpler and sufficient for low-frequency CLI-driven mutations.
    Tradeoffs: Changes are not instant (seconds of latency); acceptable for single-user CLI-to-web workflow.

- Decision 2026-03-09 v9w0x1: sync_version uses single-row singleton with trigger-driven auto-increment
    Decision: `sync_version` table uses a single-row singleton pattern with a trigger that auto-increments on any slide mutation, not per-slide versioning.
    Reason: Simpler client polling — one value to check. S3 `_version` mirrors this single integer.
    Tradeoffs: Any mutation bumps the global version even if the web client already has the change; redundant refetch is cheap (small dataset).

- Decision 2026-03-09 x9y0z1: react-resizable-panels v4 sizes must use string percentages
    Decision: All `defaultSize`, `minSize`, `maxSize` props in `spreadsheet-viewer.tsx` use string percentages (e.g., `"18%"`) not bare numbers.
    Reason: react-resizable-panels v4 treats bare numbers as **pixels**, not percentages. `defaultSize={18}` = 18px, which renders panels as tiny slivers.
    Tradeoffs: None — this is a correctness fix. v2 used numbers-as-percentages; v4 changed the API.

- Decision 2026-03-09 z1a2b3: pnpm .npmrc hoists @radix-ui/* for unified radix-ui package
    Decision: `web/.npmrc` contains `public-hoist-pattern[]=@radix-ui/*` to hoist transitive `@radix-ui/react-*` sub-packages from the unified `radix-ui` meta-package.
    Reason: shadcn/ui components import from `@radix-ui/react-*` (individual packages), but only the unified `radix-ui` is in `package.json`. pnpm's strict mode doesn't hoist transitive deps by default.
    Tradeoffs: Slightly looser isolation for @radix-ui packages; required for the app to build.

- Decision 2026-03-09 a3b4c5: Visual regression baselines committed as darwin, Linux CI deferred
    Decision: Playwright visual regression baselines generated on macOS (darwin) and committed to git. `snapshotPathTemplate` removes platform from paths. Linux-based CI baselines deferred.
    Reason: Docker-based Linux baseline generation hit pnpm + radix-ui hoisting issues. darwin baselines are sufficient for local development; CI visual tests will need Linux baselines when set up.
    Tradeoffs: Visual tests will fail in Linux CI until Linux baselines are generated. `maxDiffPixelRatio: 0.02` tolerates minor rendering differences.

- Decision 2026-03-09 w1x2y3: v0.dev UI adopted as canonical frontend, adapted for real types
    Decision: Replaced hand-built UI components with v0.dev reference design components. Adapted `Slide` type to `SlideSummary`/`SlideDetail` split. All hook orchestration lives in `SpreadsheetViewer`, not `page.tsx`. `resizable.tsx` wraps react-resizable-panels v4 API with v2-style `direction` prop for shadcn compatibility.
    Reason: User required visual parity with v0.dev reference. v0.dev used a single `Slide` type with mock data; our real backend uses `SlideSummary` (list) and `SlideDetail` (selected). The v4 API renames `PanelGroup`→`Group`, `PanelResizeHandle`→`Separator`, `direction`→`orientation`.
    Tradeoffs: v0.dev ScaledSlideFrame lost figure URL resolution (moved to iframe rendering context). NotesEditor doesn't key on slideId (editing state may persist across slide changes — tracked in ISSUES.md).
