# Issues

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Deferred defects, maintainability refactors, technical debt, risks, and engineering concerns. Add an entry only when you are not fixing it now.

## Format
- Insert new entries immediately below `<!-- ENTRIES START -->` (most recent first).
- Keep each entry **3–5 lines**.
- Line 1 starts with `- Issue YYYY-MM-DD <id>:` and a short title.
- Lines 2–5 are indented by **4 spaces** and use `Key: Value`.
- Keep **exactly one blank line** between entries.
- Prevent duplicates: search the file and merge/rewrite instead of adding near-duplicates.
- When fixed, remove the entry from this file.

### Entry template
```text
- Issue YYYY-MM-DD short-slug: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies/constraints>
```

## Open issues

<!-- ENTRIES START -->

- Issue 2026-06-22 j4m8q2: Gemini source identity differs between JSON and JSONL transcript paths
    Priority: Low. Area: cli/internal/chatimport/chatimport.go
    Description: For source=gemini, the JSONL path forces sessionMeta=nil in unwrapChatLine so the path-derived geminiSourceSessionID always wins, but the JSON path's jsonTranscriptSessionFields.applyTo honors top-level session_id/conversation_id/chat_id/id for all sources including gemini. A real Gemini JSON carrying a top-level id identical across the project-name and project-hash copies of the same session would collapse both back to one identity — exactly the collision geminiSourceSessionID was built to prevent. Practical risk is low (per q2v9m6 real Gemini files carry no usable in-file id); the existing TestGeminiDuplicatePathsGetDistinctIdentities fixture has no id field so it does not cover this.
    Next step: Decide whether path-derived gemini identity is canonical for both formats (then skip applyTo's id override when source==gemini, matching JSONL) — this changes behavior for any Gemini JSON that does carry an in-file id and would touch the gemini-fixture test. Capture the decision before implementing. Related: q2v9m6.

- Issue 2026-06-22 k7p3r5: JSON (non-JSONL) transcript path has no per-element memory bound
    Priority: Low. Area: cli/internal/chatimport/chatimport.go
    Description: decodeJSONTranscriptItems calls decoder.Decode(&raw) per array element with no size cap, fully materializing each element into map[string]any. The JSONL path deliberately caps each row at 256 MiB (maxJSONLLineBytes) and surfaces bufio.ErrTooLong; the JSON path has no equivalent guard, so a single pathological array element (e.g. a huge embedded tool output) is materialized whole. Asymmetric DoS surface against read-only on-disk artifacts (low real-world risk). readGeminiProjectHash is fine — Decode into a 1-field struct streams without retaining other fields.
    Next step: Decide whether to add an io.LimitReader cap on the JSON decode path mirroring maxJSONLLineBytes (rejects very large but valid JSON transcripts — a behavior tradeoff) or to accept the asymmetry and document it. Capture the choice before implementing.

- Issue 2026-06-07 h3v6n2: chat import completeness check re-walks and re-hashes every transcript
    Priority: Low. Area: cli/internal/cli/chat.go
    Description: scanChatImportCompleteness does a full pre-pass that re-walks, re-parses, and re-hashes every transcript the main import loop then processes again, roughly doubling parse+hash I/O on large stores (the very high-file-count w8k3r1 scenario the check targets). Correct, just not free.
    Next step: Fold the unique-session set into the main scan (record (source, source_session_id) and content hashes during the existing per-file loop) instead of a separate pre-pass, then compare against the store once at the end.
    Notes: Surfaced reviewing the w8k3r1 import-completeness PR. Deferred as out-of-scope for the tight two-issue fix; it touches the correctness-critical import loop and warrants its own change.

- Issue 2026-06-06 m2k7r9: fetchMore is outside the monotonic page-request sequence guard
    Priority: Low. Area: web/hooks/use-records.ts
    Description: `loadRecordsPage` (fetch/refresh paths) increments `recordsPageRequestSeqRef` and discards stale responses, but `fetchMore` reads/writes `cursorRef.current` and calls `updatePaginationState`/`mergeUniqueRecords` with no sequence check. A `fetchMore` in flight when a `fetchRecords`/`refreshRecords` resets page 1 can land its old page and clobber the cursor the newer load just set. Narrow window, pre-existing (original sequence-guard fix scoped only fetch/refresh).
    Next step: Capture the request sequence at the start of `fetchMore` and discard its result if `recordsPageRequestSeqRef.current` advanced before it resolved (mirror `loadRecordsPage`).

- Issue 2026-06-06 s5n3w8: selectRecord and refreshRecords reconcile race on selectedRecord
    Priority: Low. Area: web/hooks/use-records.ts
    Description: `selectRecord` writes `selectedRecord` with no shared ordering token, while the `replaceRecordsPage` reconcile (background `refreshRecords`) independently clears it. The two async writers can interleave: a reconcile may null a just-clicked record, or a slow `selectRecord` may resurrect a record the reconcile cleared. The functional `setState` updater narrows but does not close the window. Self-corrects on the next user action, so Low.
    Next step: If addressed, gate detail-state writes on a shared monotonic sequence or abort token so the reconcile and selectRecord cannot interleave inconsistently.

- Issue 2026-06-06 v6n8r4: GitHub reports default-branch Dependabot vulnerabilities
    Priority: High. Area: dependency security
    Description: GitHub reported 58 vulnerabilities on the default branch during PR #40 remote-branch cleanup (3 critical, 21 high, 30 moderate, 4 low). Details are in the repository Dependabot alerts.
    Next step: Inspect the GitHub Dependabot alert list, upgrade affected dependencies where compatible, and record any accepted risk explicitly.

- Issue 2026-06-06 t4p8m1: FTS chat search --offset is scan-and-discard (linear deep-pagination cost)
    Priority: Low. Area: cli/internal/repository (sqlite+postgres) chat search
    Description: `pc chat search`/`pc search` paginate the FTS path with SQL OFFSET, so deep pages re-scan and discard all skipped rows. Measured ~0.19 ms/row (`review` offset 0/500/1000/2000 = 0.19/0.36/0.56/0.75 s; `hydroponics` offset 4000 = 1.59 s vs 46 ms at 0); non-FTS `pc chat list` is constant-time by contrast. The JSON envelope already returns `next_cursor`.
    Next step: Replace offset paging with keyset/cursor pagination on (rank, id) for the chat FTS path; reuse the existing next_cursor surface.
    Notes: Deferred from the 2026-06 search-quality slice (that slice fixed top-k LIMIT latency, not offset cost). Lower priority than the resolved limit-latency bug.

- Issue 2026-06-06 q9r2k7: Codex fork sessions replay full parent history (heavy content duplication at scale)
    Priority: Medium. Area: cli chat import / storage / data model
    Description: Codex forks import as distinct sessions that each carry a near-complete copy of the parent's items by design (1,111 forks from 220 parents on the reference store; codex raw dominates ~12 GB of 13 GB chats/). This inflates DB/raw storage and makes any shared-history term match the parent plus every fork. Intentional behavior per the v0.1.5 CHANGELOG — changing it needs a product/data-model decision, not a silent fix.
    Next step: Decide whether to store only a fork's divergent tail with a pointer to the parent prefix and/or de-duplicate replayed items at the item layer; capture the decision in DECISIONS.md before implementing.
    Notes: Surfaced in the v0.1.5 search-quality handoff §4. Related search-side symptom (one conversation returned many times) overlaps backlog fork-family grouping.

- Issue 2026-06-04 q2v9m6: Gemini chat session identity is path-derived, not the in-file sessionId
    Priority: Low. Area: cli/internal/chatimport/chatimport.go
    Description: geminiSourceSessionID derives source_session_id from the file path (grandparent/parent/basename) with a stale comment claiming Gemini carries no usable session id; current Gemini session JSON does carry a stable top-level `sessionId`. Path-based identity works but is brittle (a moved/renamed tmp dir changes identity) and differs from codex/claude which key on the native id.
    Next step: Switch Gemini identity to the in-file `sessionId` only during a fresh store rebuild — it rewrites every Gemini source_session_id, so doing it incrementally would churn/duplicate existing rows. Defer until such a rebuild.

- Issue 2026-06-04 h5j6k7: Harden schema-level record asset key canonicality
    Priority: Low. Area: schema
    Description: Repository adapters now reject non-canonical record child asset keys, but canonical SQLite/Postgres schema artifacts still enforce only `figures/` and `data/` prefixes. Direct database writes can still persist keys outside `{kind}/{record_id}/{filename}` until schema constraints are tightened through the migration system.
    Next step: Add dialect-appropriate schema constraints or triggers for exact child asset key canonicality when schema migrations are next extended; keep repository validation as the runtime guard meanwhile.

- Issue 2026-06-04 w3h7p1: Finish CI/release workflow supply-chain hardening beyond SHA pinning
    Priority: Low. Area: .github/workflows
    Description: PR #34 pinned actions to SHAs and added `persist-credentials: false` to the read-only `ci.yml` checkouts. Two reviewer-suggested hardenings remain: `cache: false` on `setup-go` (a CI-speed vs cache-poisoning tradeoff) in `ci.yml` and `release.yml` build-release, and `persist-credentials: false` on `release.yml` checkouts. The release-pipeline change is risky because the homebrew-tap checkout (release.yml:116) feeds `peter-evans/create-pull-request`, which pushes; `release.yml` is tag-triggered and not exercised by PR CI, so the credential change can't be verified here.
    Next step: Decide the cache tradeoff explicitly; if applying `persist-credentials: false` to release.yml, verify `create-github-app-token` + `create-pull-request` still authenticate (token is passed explicitly) on a real tag run before relying on it.

- Issue 2026-05-22 h9k2m4: Replace per-import hash of managed raw with schema-backed fingerprint
    Priority: Low. Area: cli/internal/cli/chat.go, schema
    Description: Exact unchanged chat imports still pay source + managed file hashing cost, plus an O(N) `ListChatSessions(IncludeDeleted: true)` load per source per import to populate the lookup index. Append-only JSONL/NDJSON now uses a suffix import path, so this is a remaining optimization rather than the active-session bottleneck.
    Next step: If large-history profiling shows exact-match hashing or the per-source list load remains expensive, design a schema-backed source fingerprint with explicit migration handling and consider a narrower index-load filter.

- Issue 2026-05-17 a3i6f8: Snapshot replacement is rollback-safe but not crash-safe atomic
    Priority: Medium. Area: cli/internal/gitsnapshot/snapshot.go
    Description: `replaceSnapshotContents` moves each managed entry independently (backup-then-promote per entry). If the process is killed between the backup loop and the promotion loop, the export root is left with a partial snapshot plus a backup dir, violating the atomic-replacement contract.
    Next step: Refactor to swap the entire snapshot root via a single rename (write to `<root>.new`, rename old root to backup, rename new in, then delete backup). Make sure restore-on-failure handles the case where the original root was already renamed away.

- Issue 2026-05-17 r6e0k3: Cross-table ID uniqueness for records vs chat_session is not atomically enforced
    Priority: Medium. Area: cli/internal/repository/{sqlite,postgres}/repository.go, schema
    Description: `CreateRecord` and `UpsertChatSession` each do a read-then-insert preflight to check that the ID isn't already used by the other table. Two concurrent writers can pass both probes and commit conflicting IDs. The races are unlikely in practice (chat IDs include random hex), but the invariant is real if the design relies on a shared ID namespace.
    Next step: Introduce a dedicated `id_registry` table with a unique constraint and reserve the ID transactionally on creation, or run both inserts inside the same transaction with a shared advisory lock. Update both backends; document the contract in DECISIONS.md.

- Issue 2026-05-17 p2r3s4: Project-paths registry sync is insert-only and grows monotonically
    Priority: Medium. Area: cli/internal/sync/service.go cli/internal/repository
    Description: `syncRegistries` re-uploads every project path on every sync (no `since` filter and `UpsertProjectPath` uses `INSERT OR IGNORE`). Project paths therefore cannot be removed via sync — a path deleted on device A still influences chat project-id matching on device B forever — and cost is O(paths × sync-passes).
    Next step: Decide between (a) adding `deleted_at` to `project_paths` with tombstone propagation, or (b) documenting one-way insert-only semantics and exposing `pc project paths prune`. Add a `since` filter on `ListProjectPaths` for incremental sync regardless.

- Issue 2026-05-11 q8r9s0: Snapshot import and restore-db replacement paths are not atomic
    Priority: High. Area: cli/internal/cli/snapshot_support.go
    Description: `pc import` and `pc restore-db` can still mutate earlier database/file sections before a later record or filesystem failure occurs, so a mid-operation error can leave users with a partial restore despite chat raw-source rollback and upfront chat source-identity validation.
    Next step: Design a staged or transactional replacement path for the full local SQLite database plus managed file payloads, then add failure tests proving the original state remains recoverable after post-backup errors.

- Issue 2026-05-11 n6p7q8: Multi-project web filter paginates over an incomplete client-side result set
    Priority: Medium. Area: web/components/spreadsheet-viewer.tsx
    Description: Selecting multiple projects fetches an unfiltered page from the API and filters it client-side, so matching records beyond the current unfiltered page can be hidden and pagination counts/cursors do not represent the selected projects.
    Next step: Decide whether the API should support multi-project filters or whether the UI should constrain project filtering to the server-supported single-project/all modes; then add component and hook tests for the chosen behavior.

- Issue 2026-05-10 f1a2l3p: `pc fetch --all` is sequential; large datasets pay full round-trip per file
    Priority: Low. Area: cli/internal/cli/fetch.go
    Description: `fetchAllDataFiles` iterates records and data files one at a time, so each S3 download blocks the next and each on-disk hash blocks subsequent work. For users with thousands of records, wall-clock time is dominated by sequential network latency and hashing even though the operations are independent. Implementing a bounded worker pool would require: thread-safe stats updates (currently plain ints/int64), deterministic-enough stderr/failure ordering for tests, a concurrency cap (flag, env, or CPU-based default), and a decision on whether cancellation drains or aborts in-flight downloads.
    Next step: Prototype an `errgroup.Group` with a semaphore-bounded worker pool, add a `--concurrency` flag (default e.g. min(8, runtime.NumCPU())), and convert `fetchAllStats` to atomic counters. Verify with a benchmark and an updated cloud E2E that exercises >50 files.
    Notes: Raised by gemini-code-assist on PR #20; deferred from that PR because parallelization is a perf optimization beyond the initial feature scope.

- Issue 2026-03-11 t1u2v3a: Seed idempotency is fragile when user edits tutorial record HTML
    Priority: Low. Area: cli/internal/cli/seed.go
    Description: `runSeed` uses HTML content as the identity key for existing tutorial records (`existingByHTML`). If a user edits the HTML of a seeded record, `runSeed` will not recognise it as existing and will create a duplicate on the next run. Stable IDs would require schema changes (a `seed_key` column or similar) and migration support.
    Next step: When the schema is next extended, consider adding a `seed_key` or `origin` column to records so seed idempotency is content-independent. Until then, the backfill repair logic handles partial deletion correctly.

- Issue 2026-03-10 k1l2m3a: handleSyncData discards incremental sync payload
    Priority: Medium. Area: web/components/spreadsheet-viewer.tsx
    Description: `handleSyncData` callback ignores the `SyncChangesResponse` data from `useSyncManager` and does a full page-1 refetch via `refreshRecords()`. This wastes the incremental `GET /api/sync/changes` API call and resets pagination on every sync.
    Next step: Either use the incremental items to merge into the local record list, or remove the `GET /api/sync/changes` fetch from `useSyncManager` and use version-triggered full refetch only.

- Issue 2026-03-10 a1b2c3a: refreshRecords does not set isLoading during background sync
    Priority: Low. Area: web/hooks/use-records.ts
    Description: `refreshRecords` (called by sync manager on version change) never sets `isLoading`. Consumers see `false` throughout the fetch, so no loading indicator is shown during background refreshes.
    Next step: Decide if this is intentional (silent refresh) or if a separate `isRefreshing` state should be exposed.

- Issue 2026-03-10 g3h4i5a: AssetCard download/delete handlers are no-ops
    Priority: Medium. Area: web/components/record-details.tsx
    Description: The `onDownload` and `onDelete` callbacks passed to `AssetCard` for both figures and data files are empty arrow functions. The delete confirmation dialog appears but the confirmed action is a no-op.
    Next step: Implement download via the `/api/files` presigned URL endpoint. Decide on figure/data-file deletion UX (CLI-only or web-editable).

- Issue 2026-03-09 f7g8h9: Linux CI visual regression baselines not yet generated
    Priority: Medium. Area: web/tests/e2e
    Description: Visual regression baselines were generated on macOS. CI runs on Linux where font rendering differs, so visual tests will fail. Docker-based generation hit pnpm + radix-ui hoisting issues (`.npmrc` `public-hoist-pattern` not taking effect inside the Playwright Docker image).
    Next step: When CI is set up, generate Linux baselines via Docker with the Playwright image. May need to copy `.npmrc` into the Docker context or use `shamefully-hoist=true`.

- Issue 2026-03-09 u1v2w3: Drag-and-drop reorder not implemented in web UI
    Priority: Medium. Area: web/components
    Description: `useRecords.reorderRecord` exists and API route works, but `RecordNavigation` has no drag handlers. Users cannot reorder records via drag-and-drop in the browser.
    Next step: Add `@dnd-kit/core` or HTML5 drag-and-drop to `RecordNavigation`, call `reorderRecord` on drop with the order route's `computeFractionalIndex`.

- Issue 2026-03-08 k4l5m6: Repository adapters and sync suites are oversized
    Priority: Low. Area: cli/internal/repository, cli/internal/sync
    Description: `repository.go` adapters, `service.go`, and their largest companion test files now concentrate CRUD/reconciliation helpers and long scenario suites in 600-3900 LOC files, which raises review and change risk.
    Next step: When these areas are next touched for feature work, split scan/helper logic and test scenarios into themed files without changing repository contracts.

- Issue 2026-03-06 e5f6a7: edit/add commands use manual rollback instead of DB transactions
    Priority: Medium. Area: cli/internal/cli
    Description: `runEdit` and `runAdd` track mutation state with boolean flags and deferred rollback closures instead of wrapping multi-step DB operations in a transaction. This is fragile — a crash between DB writes and file operations leaves inconsistent state. The repository interface lacks transaction support by design (Phase 2 decision).
    Next step: When transaction support is added to the repository interface (Phase 6 sync or earlier), refactor edit/add to use proper DB transactions for the multi-step write sequences.

- Issue 2026-03-06 c3d4e5: Coverage scripts run all tests twice in CI
    Priority: Low. Area: cli/scripts
    Description: `check_coverage.sh` and `check_coverage_per_package.sh` both run `go test` independently. Every test runs at least twice per CI job, doubling test execution time as the package count grows.
    Next step: Merge the two scripts or have the per-package script reuse the aggregate profile.

- Issue 2026-03-06 b2c3d4: CLI package-level test function variables unsafe with t.Parallel
    Priority: Low. Area: cli/internal/cli
    Description: CLI tests still mutate package-level stubs such as `resolveHomeDirFn`, `screenshotWithChromeFn`, and cloud setup hooks, then restore via `t.Cleanup`. Safe today because cli package tests are not parallel, but adding parallel tests would cause data races.
    Next step: If CLI tests need parallelism, refactor the remaining CLI command stubs into command-scoped dependencies or instance hooks.
