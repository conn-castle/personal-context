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

- Issue 2026-06-04 w8k3r1: pc chat import has no coverage self-check against on-disk ground truth
    Priority: Medium. Area: cli/internal/cli/chat.go, cli/internal/cli/doctor.go
    Description: The importer reports files scanned / sessions created but never compares the store against a disk scan of all discovered roots, so a silent discovery gap reads as success. This class of miss left an installed store at ~14% coverage undetected because verification compared output against the same roots the importer scans (a circular check).
    Next step: Add a coverage self-check to the import summary or `pc doctor` that counts unique sessions on disk under all discovered roots (codex rollout uuid, claude session-id + subagent, gemini basename) and reports any shortfall vs the store.
    Notes: Discovery is registry-driven and tested; this is the systemic guard so the silent-miss class cannot recur.

- Issue 2026-06-04 q2v9m6: Gemini chat session identity is path-derived, not the in-file sessionId
    Priority: Low. Area: cli/internal/chatimport/chatimport.go
    Description: geminiSourceSessionID derives source_session_id from the file path (grandparent/parent/basename) with a stale comment claiming Gemini carries no usable session id; current Gemini session JSON does carry a stable top-level `sessionId`. Path-based identity works but is brittle (a moved/renamed tmp dir changes identity) and differs from codex/claude which key on the native id.
    Next step: Switch Gemini identity to the in-file `sessionId` only during a fresh store rebuild — it rewrites every Gemini source_session_id, so doing it incrementally would churn/duplicate existing rows. Defer until such a rebuild.

- Issue 2026-06-04 t7n2v5: No backend-level test forces a real mid-transaction rollback for UpsertChatSessionWithItems
    Priority: Medium. Area: cli/internal/repository/{sqlite,postgres}, repositorytest
    Description: No test exercises a genuine mid-transaction failure (after the session UPSERT + items DELETE, before commit) for UpsertChatSessionWithItems to prove Postgres/SQLite rollback leaves session updated_at and items unchanged. The contract atomicity test's Ordinal:-1 path fails validChatItemInput pre-validation before Begin, so no real DB transaction is opened; the sync-layer self-heal test only exercises the memory repo's snapshot-restore.
    Next step: Add a backend-specific test (e.g. inject a failing exec or constraint violation between the items DELETE and commit) asserting the session row and items are unchanged after rollback. This is backend-specific (Postgres needs Docker/integration tag) and does not belong in the shared contract suite.

- Issue 2026-06-04 w3h7p1: Finish CI/release workflow supply-chain hardening beyond SHA pinning
    Priority: Low. Area: .github/workflows
    Description: PR #34 pinned actions to SHAs and added `persist-credentials: false` to the read-only `ci.yml` checkouts. Two reviewer-suggested hardenings remain: `cache: false` on `setup-go` (a CI-speed vs cache-poisoning tradeoff) in `ci.yml` and `release.yml` build-release, and `persist-credentials: false` on `release.yml` checkouts. The release-pipeline change is risky because the homebrew-tap checkout (release.yml:116) feeds `peter-evans/create-pull-request`, which pushes; `release.yml` is tag-triggered and not exercised by PR CI, so the credential change can't be verified here.
    Next step: Decide the cache tradeoff explicitly; if applying `persist-credentials: false` to release.yml, verify `create-github-app-token` + `create-pull-request` still authenticate (token is passed explicitly) on a real tag run before relying on it.

- Issue 2026-05-22 j4n7p3: Make screenshot fake-Chrome test scripts cross-platform
    Priority: Low. Area: cli/internal/cli/screenshot_test.go
    Description: `writeFakeChromeScript` and `writeFakeChromePNGScript` emit POSIX shell scripts (`#!/bin/sh`, `dd if=/dev/zero`) and are invoked by `TestScreenshotHappyPath`, `TestScreenshotDefaultOutput`, and the pre-existing `TestScreenshotPreservesRelativeFigurePaths`. CI runs Linux only (`.github/workflows/ci.yml` is ubuntu-latest), so this passes today, but a Windows test run would fail because `screenshotWithChrome` exec's the script directly.
    Next step: Replace the shell scripts with a Go-level fake (e.g., write a minimal PNG via `os.WriteFile` and return a stubbed `screenshotWithChrome` impl), or add `runtime.GOOS == "windows"` skip guards to all callers including the pre-existing test.

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

- Issue 2026-05-17 d4n6m2: Chat raw-source pull writes to disk before metadata upsert
    Priority: Medium. Area: cli/internal/sync/service.go
    Description: `transferChatRawSource` runs upload/download before `UpsertChatSession` in both directions. On push that ordering prevents advertising a key whose object is missing; on pull it inverts the rationale, so a metadata upsert failure can leave the downloaded file under `chats/raw/{id}/source.ext` with no DB row referencing it. Doctor flags it; the next successful sync self-heals; in between, lifecycle operations cannot clean it up.
    Next step: Split push and pull ordering so pull upserts metadata first, then writes the local raw file; or wrap pull metadata + raw write in a single transactional helper.

- Issue 2026-05-11 v2w3x4: Legacy sync lock files still require manual recovery
    Priority: Medium. Area: cli/internal/syncengine
    Description: New sync locks include JSON metadata and can recover stale same-host dead PIDs, but pre-metadata literal locks (`locked\n`) and other unparseable lock files remain blocking because they cannot be safely attributed to a dead process.
    Next step: Add user-facing `pc doctor` guidance or a narrowly-scoped recovery command that reports the lock path and requires explicit confirmation before removing unparseable lock files.

- Issue 2026-05-11 q8r9s0: Snapshot import and restore-db replacement paths are not atomic
    Priority: High. Area: cli/internal/cli/snapshot_support.go
    Description: `pc import` and `pc restore-db` can still mutate earlier database/file sections before a later record or filesystem failure occurs, so a mid-operation error can leave users with a partial restore despite chat raw-source rollback and upfront chat source-identity validation.
    Next step: Design a staged or transactional replacement path for the full local SQLite database plus managed file payloads, then add failure tests proving the original state remains recoverable after post-backup errors.

- Issue 2026-05-11 n6p7q8: Multi-project web filter paginates over an incomplete client-side result set
    Priority: Medium. Area: web/components/spreadsheet-viewer.tsx
    Description: Selecting multiple projects fetches an unfiltered page from the API and filters it client-side, so matching records beyond the current unfiltered page can be hidden and pagination counts/cursors do not represent the selected projects.
    Next step: Decide whether the API should support multi-project filters or whether the UI should constrain project filtering to the server-supported single-project/all modes; then add component and hook tests for the chosen behavior.

- Issue 2026-05-11 h5j6k7: Repository layer accepts non-canonical child s3_key values
    Priority: Medium. Area: cli/internal/repository, schema
    Description: Postgres schema checks only require `figures/` or `data/` prefixes, and repository adapters pass caller-provided child `s3_key` values through. Targeted fetch now rejects bad data-file keys, but central create/update paths can still persist paths that do not match the canonical `{kind}/{record_id}/{filename}` form.
    Next step: Centralize child asset key derivation/validation before repository writes for figures and data files, then strengthen schema checks through the migration system when migrations are available.

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

- Issue 2026-03-10 o9p0q1a: refreshRecords does not clear stale selectedRecord
    Priority: Low. Area: web/hooks/use-records.ts
    Description: When `refreshRecords` replaces the record list (e.g., after sync), `selectedRecord` retains stale data. If the selected record was deleted by another client, it remains visible in the detail panel while absent from navigation.
    Next step: After `refreshRecords`, check if `selectedRecord.id` is still in the new items list. If not, clear or re-fetch it.

- Issue 2026-03-10 q3r4s5a: No request cancellation between concurrent fetch/refresh operations
    Priority: Low. Area: web/hooks/use-records.ts
    Description: Both `fetchRecords` and `refreshRecords` write to the same `records` state and `cursorRef`. Concurrent in-flight requests can overwrite each other's results, leaving cursor and records in inconsistent states.
    Next step: Add an `AbortController` or monotonic request ID to discard stale responses.

- Issue 2026-03-10 a1b2c3a: refreshRecords does not set isLoading during background sync
    Priority: Low. Area: web/hooks/use-records.ts
    Description: `refreshRecords` (called by sync manager on version change) never sets `isLoading`. Consumers see `false` throughout the fetch, so no loading indicator is shown during background refreshes.
    Next step: Decide if this is intentional (silent refresh) or if a separate `isRefreshing` state should be exposed.

- Issue 2026-03-10 b3c4d5a: selfMutationRef timing window in useSyncManager
    Priority: Low. Area: web/hooks/use-sync-manager.ts
    Description: `markMutation()` sets `selfMutationRef` to `true`, but it is only consumed when a version change is detected. If the S3 `_version` bump hasn't propagated when the next sync fires, the flag stays `true` and suppresses the next legitimate external version change.
    Next step: Add a TTL or auto-clear `selfMutationRef` after ~10 seconds if no version change was observed.

- Issue 2026-03-10 c5d6e7a: deleteRecord error recovery clears the error via fetchRecords re-fetch
    Priority: Low. Area: web/hooks/use-records.ts
    Description: When `deleteRecord` fails, it sets an error and calls `fetchRecords` to roll back optimistic state. But `fetchRecords` starts with `setError(null)`, clearing the delete error. If the re-fetch succeeds, the user never sees the delete failure message.
    Next step: Use a separate error channel or toast for mutation failures, or skip `setError(null)` when `fetchRecords` is called as a rollback.

- Issue 2026-03-10 d7e8f9a: Layer 4 idle polling recursive setTimeout without cancel guard
    Priority: Low. Area: web/hooks/use-sync-manager.ts
    Description: The idle polling `schedule` function recursively calls itself via `setTimeout`. If the `useEffect` re-runs while an old `tick()` is executing `doSync`, both old and new polling chains can be active concurrently. The `isSyncingRef` guard (added in Round 1) prevents double `doSync` execution but the redundant timers remain.
    Next step: Add a `cancelled` boolean in the effect that is set `true` in the cleanup function, checked before calling `schedule()` in the recursive callback.

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

- Issue 2026-03-06 b2c3d4: Package-level test function variables unsafe with t.Parallel
    Priority: Low. Area: cli/internal/sqlite, cli/internal/config, cli/internal/filesystem
    Description: Tests in sqlite, config, and filesystem packages mutate package-level `var` stubs (syncFileFn, closeFileFn, etc.) and restore via `t.Cleanup`. Safe today (no `t.Parallel`), but adding parallel tests would cause data races.
    Next step: If the test suite grows or parallel tests are needed, refactor stubs into struct fields or interface-based dependency injection.

- Issue 2026-03-06 c3d4e5: Coverage scripts run all tests twice in CI
    Priority: Low. Area: cli/scripts
    Description: `check_coverage.sh` and `check_coverage_per_package.sh` both run `go test` independently. Every test runs at least twice per CI job, doubling test execution time as the package count grows.
    Next step: Merge the two scripts or have the per-package script reuse the aggregate profile.
