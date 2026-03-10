# Commands

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Canonical, repeatable **development workflow** commands for this repository (setup, build, run, test, coverage, lint/format, typecheck, migrations, scripts). This file is not for application/CLI usage documentation.

## Format
- Prefer commands that are stable and will be used repeatedly. Avoid one-off debugging commands.
- Organize commands using headings that fit the repo. Create headings as needed.
- If the repo is a monorepo, group commands per workspace/package/service and specify the working directory.
- When commands change, update this file and remove stale entries.
- Insert entries (and any needed headings) below `<!-- ENTRIES START -->`.

### Entry template
````text
- <Short purpose>
```bash
<command>
```
Run from: <repo root or path>
Prerequisites: <only if critical>
Notes: <optional constraints or tips>
````

<!-- ENTRIES START -->

## Go CLI (`cli/`)

- Run Go tests
```bash
go test ./...
```
Run from: `cli/`

- Verify module graph is tidy
```bash
go mod tidy -diff
```
Run from: `cli/`

- Build CLI binary
```bash
go build -o pc ./cmd/pc
```
Run from: `cli/`
Notes: Binary name is `pc`.

- Build all Go packages
```bash
go build ./...
```
Run from: `cli/`

- Run Go tests with coverage
```bash
./scripts/check_coverage.sh 95
```
Run from: `cli/`
Notes: Enforces the hard 95% threshold and fails loudly below target. Excludes `internal/repository/repositorytest` (contract helper), `internal/repository/postgres`, `internal/s3client`, `internal/cloude2e` (integration-test-only, require Docker), and `internal/e2e`. Integration tests run separately with `-tags integration`.

- Run per-package Go coverage gate
```bash
./scripts/check_coverage_per_package.sh 95
```
Run from: `cli/`
Notes: Fails when any tested package drops below 95%. Same exclusions as the aggregate coverage script. Integration-only packages (`postgres`, `s3client`, `cloude2e`) are tested with Docker via `-tags integration`.

- Run full Phase 3 manual verification flow (opens slide preview in browser)
```bash
./scripts/verify_phase3_manual.sh
```
Run from: `cli/`
Notes: Use `--no-open` in non-interactive environments. Prints artifact paths and cleanup command.

- Run generalized local demo verification flow (opens summary + persisted slide previews in browser)
```bash
./scripts/verify_local_demo.sh
```
Run from: `cli/`
Notes: Use `--no-open` in non-interactive environments. Verifies create/delete/restore/move flow and emits a summary artifact for human inspection.

- Run CLI e2e tests
```bash
go test ./internal/e2e
```
Run from: `cli/`
Notes: Executed explicitly in CI because coverage scripts intentionally exclude `internal/e2e`.

- Run cloud E2E integration tests
```bash
go test -tags integration ./internal/cloude2e/ -v -timeout 420s
```
Run from: `cli/`
Prerequisites: Docker running (testcontainers-go spins up Postgres and MinIO containers).
Notes: Uses `//go:build integration` tag. Tests cloud onboarding, first sync + doctor, two-home auto-sync conflict resolution, and `fetch --project --output` through the compiled `pc` binary. Schema-per-test and bucket-per-test isolation.

- Run Go linter
```bash
golangci-lint run ./...
```
Run from: `cli/`
Prerequisites: `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.2` and ensure `$(go env GOPATH)/bin` is on `PATH`.

- Run Postgres repository integration tests
```bash
go test -tags integration ./internal/repository/postgres/ -v -timeout 180s
```
Run from: `cli/`
Prerequisites: Docker running (testcontainers-go spins up a Postgres container).
Notes: Uses `//go:build integration` tag. Schema-per-test isolation via unique schemas.

- Run S3 client integration tests
```bash
go test -tags integration ./internal/s3client/ -v -timeout 60s
```
Run from: `cli/`
Prerequisites: Docker running (testcontainers-go spins up a MinIO container).
Notes: Uses `//go:build integration` tag. Bucket-per-test isolation.

## Next.js Web UI (`web/`)

Package manager: **pnpm** (lockfile: `web/pnpm-lock.yaml`).

- Install dependencies
```bash
pnpm install
```
Run from: `web/`

- Run dev server
```bash
pnpm dev
```
Run from: `web/`

- Build for production
```bash
pnpm build
```
Run from: `web/`

- Run tests
```bash
pnpm test
```
Run from: `web/`

- Run tests with coverage
```bash
pnpm test:coverage
```
Run from: `web/`
Notes: CI enforces >95% coverage.

- Run linter
```bash
pnpm lint
```
Run from: `web/`

- Run typecheck
```bash
pnpm typecheck
```
Run from: `web/`

- Run Playwright smoke e2e tests
```bash
pnpm test:e2e:smoke
```
Run from: `web/`
Prerequisites: `pnpm exec playwright install` for browser binaries.
Notes: Verifies the SlideBrowser "Personal Context" heading renders on the home page.

- Run Playwright Slide Browser e2e tests
```bash
pnpm test:e2e:slide-browser
```
Run from: `web/`
Prerequisites: `pnpm exec playwright install` for browser binaries.
Notes: Uses `page.route()` API interception — no real backend needed. Tests browse, filter, detail, edit, delete/restore, sync version, error states, pagination.

- Run Playwright e2e for standalone CLI slide preview flow
```bash
pnpm test:e2e:cli-slide
```
Run from: `web/`
Prerequisites: Go toolchain and `pnpm exec playwright install` for browser binaries.
Notes: Executes `cli/scripts/verify_phase3_manual.sh --no-open`, then loads the generated `slide.html` in Chromium.

- Run Playwright e2e for the generalized local demo artifact
```bash
pnpm test:e2e:cli-demo
```
Run from: `web/`
Prerequisites: Go toolchain and `pnpm exec playwright install` for browser binaries.
Notes: Executes `cli/scripts/verify_local_demo.sh --no-open`, then loads the generated summary page in Chromium.

## Makefile (repo root)

A root Makefile provides common targets for both workspaces. Run `make` or `make help` for a full list.

- Run all pre-commit checks (schema + lint + typecheck + test + build)
```bash
make check
```
Run from: repo root

- Run all tests (CLI + Web)
```bash
make test
```
Run from: repo root

- Start Next.js dev server
```bash
make dev
```
Run from: repo root

## Repository root

- Check schema equivalence between Postgres and SQLite
```bash
./scripts/check_schema_equivalence.sh
```
Run from: repo root
Notes: Compares `schema/schema.sql` (Postgres) and `cli/internal/sqlite/sqlite_schema.sql` (SQLite) for structural equivalence — tables, columns, indexes, UNIQUE constraints. Does not compare dialect-specific syntax (types, CHECK expressions, triggers). Runs in CI.

- Verify canonical schema contract for both workspaces
```bash
./scripts/check_schema_contract.sh
```
Run from: repo root
Notes: Fails when canonical `schema/` files are missing, when `cli/` or `web/` stop referencing them in executable/config files, or when workspace-local schema duplicates are introduced.
