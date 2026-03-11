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
- Issue YYYY-MM-DD abcdef: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies/constraints>
```

## Open issues

<!-- ENTRIES START -->

- Issue 2026-03-11 t1u2v3a: Seed idempotency is fragile when user edits tutorial slide HTML
    Priority: Low. Area: cli/internal/cli/seed.go
    Description: `runSeed` uses HTML content as the identity key for existing tutorial slides (`existingByHTML`). If a user edits the HTML of a seeded slide, `runSeed` will not recognise it as existing and will create a duplicate on the next run. Stable IDs would require schema changes (a `seed_key` column or similar) and migration support.
    Next step: When the schema is next extended, consider adding a `seed_key` or `origin` column to slides so seed idempotency is content-independent. Until then, the backfill repair logic handles partial deletion correctly.

- Issue 2026-03-10 m3n4o5a: updateSlide does not propagate html_content back to SlideSummary list
    Priority: Low. Area: web/hooks/use-slides.ts
    Description: When a slide is updated via PATCH, `updateSlide` copies `project_id`, `updated_at`, and `deleted_at` from the response into the local SlideSummary list but not `html_content`. If PATCH is later extended to accept `html_content`, thumbnails will show stale content until a full refresh.
    Next step: When PATCH is extended to accept `html_content`, add `html_content` to the fields copied in the `setSlides` updater.

- Issue 2026-03-10 k1l2m3a: handleSyncData discards incremental sync payload
    Priority: Medium. Area: web/components/spreadsheet-viewer.tsx
    Description: `handleSyncData` callback ignores the `SyncChangesResponse` data from `useSyncManager` and does a full page-1 refetch via `refreshSlides()`. This wastes the incremental `GET /api/sync/changes` API call and resets pagination on every sync.
    Next step: Either use the incremental items to merge into the local slide list, or remove the `GET /api/sync/changes` fetch from `useSyncManager` and use version-triggered full refetch only.

- Issue 2026-03-10 l3m4n5a: Stale closure risk in keyboard navigation handler
    Priority: Low. Area: web/components/spreadsheet-viewer.tsx
    Description: The `useEffect` for keyboard navigation re-registers the event listener on every `selectedSlide` and `filteredSlides` change. Between re-registrations, a keydown could reference stale data. Additionally, `goToPrevious`/`goToNext` duplicated logic recalculates `findIndex` on each call.
    Next step: Refactor to use `useRef` for `filteredSlides` and `selectedSlide`, with a single stable event handler.

- Issue 2026-03-10 n7o8p9a: CollapsedDetailsStrip may violate ResizablePanel contract
    Priority: Medium. Area: web/components/spreadsheet-viewer.tsx
    Description: When `panelVisibility.details` is false, `CollapsedDetailsStrip` may be rendered as a direct child of `ResizablePanelGroup` without a `ResizablePanel` wrapper. `react-resizable-panels` requires all direct children to be `Panel` or `PanelResizeHandle`.
    Next step: Verify whether the component is truly a direct child of PanelGroup. If so, wrap in a fixed-size `ResizablePanel` or render outside the group.

- Issue 2026-03-10 o9p0q1a: refreshSlides does not clear stale selectedSlide
    Priority: Low. Area: web/hooks/use-slides.ts
    Description: When `refreshSlides` replaces the slide list (e.g., after sync), `selectedSlide` retains stale data. If the selected slide was deleted by another client, it remains visible in the detail panel while absent from navigation.
    Next step: After `refreshSlides`, check if `selectedSlide.id` is still in the new items list. If not, clear or re-fetch it.

- Issue 2026-03-10 p1q2r3a: fetchMore can produce duplicate slides
    Priority: Low. Area: web/hooks/use-slides.ts
    Description: `fetchMore` appends new items with `setSlides(prev => [...prev, ...data.items])`. If data changed between page fetches (slide inserted or reordered), the same slide could appear on both pages.
    Next step: Deduplicate by ID when appending: filter out items whose IDs are already in the current list.

- Issue 2026-03-10 q3r4s5a: No request cancellation between concurrent fetch/refresh operations
    Priority: Low. Area: web/hooks/use-slides.ts
    Description: Both `fetchSlides` and `refreshSlides` write to the same `slides` state and `cursorRef`. Concurrent in-flight requests can overwrite each other's results, leaving cursor and slides in inconsistent states.
    Next step: Add an `AbortController` or monotonic request ID to discard stale responses.

- Issue 2026-03-10 a1b2c3a: refreshSlides does not set isLoading during background sync
    Priority: Low. Area: web/hooks/use-slides.ts
    Description: `refreshSlides` (called by sync manager on version change) never sets `isLoading`. Consumers see `false` throughout the fetch, so no loading indicator is shown during background refreshes.
    Next step: Decide if this is intentional (silent refresh) or if a separate `isRefreshing` state should be exposed.

- Issue 2026-03-10 b3c4d5a: selfMutationRef timing window in useSyncManager
    Priority: Low. Area: web/hooks/use-sync-manager.ts
    Description: `markMutation()` sets `selfMutationRef` to `true`, but it is only consumed when a version change is detected. If the S3 `_version` bump hasn't propagated when the next sync fires, the flag stays `true` and suppresses the next legitimate external version change.
    Next step: Add a TTL or auto-clear `selfMutationRef` after ~10 seconds if no version change was observed.

- Issue 2026-03-10 c5d6e7a: deleteSlide error recovery clears the error via fetchSlides re-fetch
    Priority: Low. Area: web/hooks/use-slides.ts
    Description: When `deleteSlide` fails, it sets an error and calls `fetchSlides` to roll back optimistic state. But `fetchSlides` starts with `setError(null)`, clearing the delete error. If the re-fetch succeeds, the user never sees the delete failure message.
    Next step: Use a separate error channel or toast for mutation failures, or skip `setError(null)` when `fetchSlides` is called as a rollback.

- Issue 2026-03-10 d7e8f9a: Layer 4 idle polling recursive setTimeout without cancel guard
    Priority: Low. Area: web/hooks/use-sync-manager.ts
    Description: The idle polling `schedule` function recursively calls itself via `setTimeout`. If the `useEffect` re-runs while an old `tick()` is executing `doSync`, both old and new polling chains can be active concurrently. The `isSyncingRef` guard (added in Round 1) prevents double `doSync` execution but the redundant timers remain.
    Next step: Add a `cancelled` boolean in the effect that is set `true` in the cleanup function, checked before calling `schedule()` in the recursive callback.

- Issue 2026-03-10 e9f0g1a: useSyncManager does not expose sync errors to consumers
    Priority: Low. Area: web/hooks/use-sync-manager.ts
    Description: The `catch` block in `doSync` now logs via `console.warn`, but no error state is exposed to consumers. The UI cannot indicate that sync is failing.
    Next step: Expose a `syncError` state or `lastSyncError` ref so consumers can display a stale-data indicator.

- Issue 2026-03-10 f1g2h3a: Dropdown menu items in SlideMetadataBar have no onClick handlers
    Priority: Low. Area: web/components/slide-metadata-bar.tsx
    Description: Four action items in the "More options" dropdown (Share slide, Download slide, Copy link, Print) render as clickable items but do nothing. No `disabled` state or visual indication they are non-functional.
    Next step: Add `disabled` styling or wire handlers when the features are implemented.

- Issue 2026-03-10 g3h4i5a: AssetCard download/delete handlers are no-ops
    Priority: Medium. Area: web/components/slide-details.tsx
    Description: The `onDownload` and `onDelete` callbacks passed to `AssetCard` for both figures and data files are empty arrow functions. The delete confirmation dialog appears but the confirmed action is a no-op.
    Next step: Implement download via the `/api/files` presigned URL endpoint. Decide on figure/data-file deletion UX (CLI-only or web-editable).

- Issue 2026-03-10 h5i6j7a: Settings overlay state is local-only with no persistence
    Priority: Medium. Area: web/components/settings-overlay.tsx
    Description: All settings state (autoSave, showThumbnails, compactMode, defaultView, fontSize, etc.) is managed as local `useState` with hardcoded defaults. Closing and reopening the overlay resets everything. No persistence to localStorage or API.
    Next step: Determine which settings are real vs placeholder. Persist real settings to `localStorage` or a future API endpoint.

- Issue 2026-03-10 i7j8k9a: Accessibility gaps in web UI components
    Priority: Low. Area: web/components
    Description: Several components have accessibility gaps: `SettingsOverlay` lacks Escape key handling, focus trapping, and ARIA `role="dialog"`; `SlideThumbnail` button has no `aria-label`; navigation view-mode toggle buttons use `title` instead of `aria-label`/`aria-pressed`.
    Next step: Replace `SettingsOverlay` div with shadcn/ui `Dialog` component (provides focus trapping, Escape, ARIA). Add `aria-label` to thumbnail buttons and `aria-pressed` to toggle buttons.

- Issue 2026-03-09 f7g8h9: Linux CI visual regression baselines not yet generated
    Priority: Medium. Area: web/tests/e2e
    Description: Visual regression baselines were generated on macOS. CI runs on Linux where font rendering differs, so visual tests will fail. Docker-based generation hit pnpm + radix-ui hoisting issues (`.npmrc` `public-hoist-pattern` not taking effect inside the Playwright Docker image).
    Next step: When CI is set up, generate Linux baselines via Docker with the Playwright image. May need to copy `.npmrc` into the Docker context or use `shamefully-hoist=true`.

- Issue 2026-03-09 x3y4z5: SpreadsheetViewer missing integration tests for hook orchestration
    Priority: Medium. Area: web/tests
    Description: When page.tsx was simplified to just `<SpreadsheetViewer />`, the integration tests for initial data loading, mutation tracking (markMutation after updateSlide), and sync coordination were removed from app-page.test.tsx. These behaviors now live inside SpreadsheetViewer and are untested at the component level.
    Next step: Create `tests/unit/components/spreadsheet-viewer.test.tsx` that mocks child components and react-resizable-panels, then tests hook orchestration (fetchSlides/fetchProjects on mount, markMutation after successful mutation, no markMutation on failure).

- Issue 2026-03-09 u1v2w3: Drag-and-drop reorder not implemented in web UI
    Priority: Medium. Area: web/components
    Description: `useSlides.reorderSlide` exists and API route works, but `SlideNavigation` has no drag handlers. Users cannot reorder slides via drag-and-drop in the browser.
    Next step: Add `@dnd-kit/core` or HTML5 drag-and-drop to `SlideNavigation`, call `reorderSlide` on drop with the order route's `computeFractionalIndex`.

- Issue 2026-03-08 m7n8o9: UpdateSlide silently clears deleted_at when input.DeletedAt is nil
    Priority: Medium. Area: cli/internal/repository
    Description: `UpdateSlide` sets `deleted_at = ?` unconditionally. A caller that constructs `UpdateSlideInput` without copying the existing `DeletedAt` silently restores a soft-deleted slide. All current callers are safe (they copy `existing.DeletedAt`), but the API is a footgun for future callers.
    Next step: Add `SetDeletedAt bool` flag to `UpdateSlideInput` or change SQL to `COALESCE(?, deleted_at)` with a separate explicit-clear mechanism for sync/restore.

- Issue 2026-03-08 n9o0p1: Duplicate filenames silently overwritten in sync reconciliation plans
    Priority: Medium. Area: cli/internal/sync
    Description: `PlanFigureReconciliation` and `PlanDataFileReconciliation` build maps keyed by filename without duplicate detection. If existing or desired slices contain duplicate filenames (from corruption or bugs), one entry is silently lost. The `syncengine` package has `FigureMapByFilename`/`DataFileMapByFilename` with duplicate detection but they are not used here.
    Next step: Either use `syncengine` map helpers or add explicit duplicate detection in `conflict.go`.

- Issue 2026-03-08 o1p2q3: GitHub Actions pinned to mutable version tags
    Priority: Medium. Area: .github/workflows/ci.yml
    Description: All GitHub Actions use major version tags (`@v4`, `@v5`, `@v8`) rather than full commit SHAs. Third-party actions like `golangci/golangci-lint-action@v8` carry supply-chain risk.
    Next step: Pin all actions to full commit SHAs with version comments.

- Issue 2026-03-08 p3q4r5: CI web job runs tests twice (pnpm test then pnpm test:coverage)
    Priority: Low. Area: .github/workflows/ci.yml
    Description: The `web` CI job runs `pnpm test` (vitest run) and then `pnpm test:coverage` (vitest run --coverage), executing the identical test suite twice. The coverage run already validates test passage.
    Next step: Remove the redundant `pnpm test` step from the web CI job.

- Issue 2026-03-08 k4l5m6: Repository adapters and sync suites are oversized
    Priority: Low. Area: cli/internal/repository, cli/internal/sync
    Description: `repository.go` adapters, `service.go`, and their largest companion test files now concentrate CRUD/reconciliation helpers and long scenario suites in 600-3900 LOC files, which raises review and change risk.
    Next step: When these areas are next touched for feature work, split scan/helper logic and test scenarios into themed files without changing repository contracts.

- Issue 2026-03-08 h1i2j3: doctor missing-file checks duplicate scan logic
    Priority: Low. Area: cli/internal/cli
    Description: `checkMissingFigures` and `checkMissingDataFiles` in `doctor.go` duplicate the same traversal/stat/error plumbing with only repository and path-resolver differences.
    Next step: Introduce a shared internal helper that parameterizes list and path resolution while preserving existing error labels/output text.

- Issue 2026-03-07 f6a7b8: pc move with --date matching current date silently reorders to end
    Priority: Low. Area: cli/internal/cli
    Description: `pc move <id> --date <same-date>` with no position flags defaults to `last`, reordering the slide to the end of the day instead of preserving its position. Can cause unexpected ordering changes in scripted or repeated runs.
    Next step: When the slide's date doesn't change and no explicit position flag is given, preserve the existing day_order instead of recomputing.

- Issue 2026-03-06 e5f6a7: edit/add commands use manual rollback instead of DB transactions
    Priority: Medium. Area: cli/internal/cli
    Description: `runEdit` and `runAdd` track mutation state with boolean flags and deferred rollback closures instead of wrapping multi-step DB operations in a transaction. This is fragile — a crash between DB writes and file operations leaves inconsistent state. The repository interface lacks transaction support by design (Phase 2 decision).
    Next step: When transaction support is added to the repository interface (Phase 6 sync or earlier), refactor edit/add to use proper DB transactions for the multi-step write sequences.

- Issue 2026-03-06 a1b2c3: mapSQLiteError uses fragile string matching
    Priority: Medium. Area: cli/internal/repository/sqlite
    Description: `mapSQLiteError` classifies UNIQUE/FK constraint errors via `strings.Contains` on error messages. If `modernc.org/sqlite` changes message format, mapping silently breaks.
    Next step: Investigate `modernc.org/sqlite` structured error types (`*sqlite.Error`, error codes) and replace string matching with type assertions.

- Issue 2026-03-06 b2c3d4: Package-level test function variables unsafe with t.Parallel
    Priority: Low. Area: cli/internal/sqlite, cli/internal/config, cli/internal/filesystem
    Description: Tests in sqlite, config, and filesystem packages mutate package-level `var` stubs (syncFileFn, closeFileFn, etc.) and restore via `t.Cleanup`. Safe today (no `t.Parallel`), but adding parallel tests would cause data races.
    Next step: If the test suite grows or parallel tests are needed, refactor stubs into struct fields or interface-based dependency injection.

- Issue 2026-03-06 c3d4e5: Coverage scripts run all tests twice in CI
    Priority: Low. Area: cli/scripts
    Description: `check_coverage.sh` and `check_coverage_per_package.sh` both run `go test` independently. Every test runs at least twice per CI job, doubling test execution time as the package count grows.
    Next step: Merge the two scripts or have the per-package script reuse the aggregate profile.

- Issue 2026-03-06 d4e5f6: Coverage scripts do not use -race flag
    Priority: Low. Area: cli/scripts
    Description: Neither coverage script passes `-race` to `go test`. As concurrency-related code is added (sync engine, Phase 6), race detection becomes more important.
    Next step: Add `-race` to the coverage script test commands when sync/concurrency code lands.
