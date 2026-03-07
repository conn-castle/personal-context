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
