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

> **Note:** `cli/` and `web/` directories now exist as Phase 1 scaffolds, but most commands below require the pending Phase 1 initialization tasks (Go module setup in `cli/` and Next.js app initialization in `web/`).

## Go CLI (`cli/`)

- Initialize Go module
```bash
go mod init github.com/conn-castle/personal-context/cli
```
Run from: `cli/`
Notes: To be run during Phase 1 scaffolding.

- Run Go tests
```bash
go test ./...
```
Run from: `cli/`

- Build CLI binary
```bash
go build -o pc ./cmd/pc
```
Run from: `cli/`
Notes: Binary name is `pc`.

- Run Go tests with coverage
```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
```
Run from: `cli/`
Notes: CI enforces >95% coverage. Use `go tool cover -html=coverage.out` to inspect uncovered lines.

- Run Go linter
```bash
golangci-lint run ./...
```
Run from: `cli/`
Prerequisites: `golangci-lint` installed

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
npm test -- --coverage
```
Run from: `web/`
Notes: CI enforces >95% coverage.

- Run Playwright e2e tests
```bash
npx playwright test
```
Run from: `web/`
Prerequisites: `npx playwright install` for browser binaries. Test database configured.
