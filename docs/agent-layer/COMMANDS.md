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
Notes: Enforces the hard 95% threshold and fails loudly below target. Excludes `internal/repository/repositorytest` (contract helper, no `_test.go` files), `internal/e2e`, and `migrations`. CLI e2e tests run separately in CI via `go test ./internal/e2e`.

- Run per-package Go coverage gate
```bash
./scripts/check_coverage_per_package.sh 95
```
Run from: `cli/`
Notes: Fails when any tested package drops below 95%. Excludes `internal/repository/repositorytest` (contract helper, no `_test.go` files), `internal/e2e`, and `migrations`. CLI e2e tests run separately in CI via `go test ./internal/e2e`.

- Run full Phase 3 manual verification flow (opens slide preview in browser)
```bash
./scripts/verify_phase3_manual.sh
```
Run from: `cli/`
Notes: Use `--no-open` in non-interactive environments. Prints artifact paths and cleanup command.

- Run CLI e2e tests
```bash
go test ./internal/e2e
```
Run from: `cli/`
Notes: Executed explicitly in CI because coverage scripts intentionally exclude `internal/e2e`.

- Run Go linter
```bash
golangci-lint run ./...
```
Run from: `cli/`
Prerequisites: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@v2.10` and ensure `$(go env GOPATH)/bin` is on `PATH`.

## Next.js Web UI (`web/`)

- Install dependencies
```bash
npm install
```
Run from: `web/`

- Run dev server
```bash
npm run dev
```
Run from: `web/`

- Build for production
```bash
npm run build
```
Run from: `web/`

- Run tests
```bash
npm test
```
Run from: `web/`

- Run tests with coverage
```bash
npm run test:coverage
```
Run from: `web/`
Notes: CI enforces >95% coverage.

- Run linter
```bash
npm run lint
```
Run from: `web/`

- Run typecheck
```bash
npm run typecheck
```
Run from: `web/`

- Run Playwright smoke e2e tests
```bash
npm run test:e2e:smoke
```
Run from: `web/`
Prerequisites: `npx playwright install` for browser binaries.

- Run Playwright e2e for standalone CLI slide preview flow
```bash
npm run test:e2e:cli-slide
```
Run from: `web/`
Prerequisites: Go toolchain and `npx playwright install` for browser binaries.
Notes: Executes `cli/scripts/verify_phase3_manual.sh --no-open`, then loads the generated `slide.html` in Chromium.

## Repository root

- Verify canonical schema contract for both workspaces
```bash
./scripts/check_schema_contract.sh
```
Run from: repo root
Notes: Fails when canonical `schema/` files are missing, when `cli/` or `web/` stop referencing them in executable/config files, or when workspace-local schema duplicates are introduced.
