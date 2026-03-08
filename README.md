# Personal Context (`pc`)

Personal Context is a local-first engineering notebook system that stores work as HTML slides, attached figures, and data files, organized by date and project.

## Status

Roadmap Phase 4 (Local CLI Features) is implemented: all local commands are operational including CRUD (`pc setup`, `pc add`, `pc show`, `pc edit`, `pc delete`, `pc restore`, `pc move`) and search/management (`pc search`, `pc trash`, `pc gc`, `pc project`, `pc doctor`). 103 e2e tests and per-package >=95% coverage. Cloud/web product features remain in later roadmap phases.

## Architecture

```
Local SQLite + local files      <- CLI writes here (always)
     | pc sync (optional, cloud configured)
Neon Postgres + S3              <- cloud source of truth for web UI
     | nightly export
GitHub + S3                     <- portable backup
```

- Local-only mode runs without Neon/S3.
- Cloud mode enables bidirectional sync and web UI data access.
- Canonical schema artifacts are stored in:
  - `schema/schema.sql`
  - `schema/schema-types.ts`

## Monorepo Layout

```
personal-context/
├── cli/                         # Go CLI workspace
├── web/                         # Next.js App Router workspace
├── schema/                      # Canonical schema artifacts
├── docs/
│   ├── agent-layer/             # Project memory files
│   └── nightly-export-workflow.example.yml
├── scripts/
│   └── check_schema_contract.sh # Canonical schema guard
└── .github/workflows/ci.yml
```

## Setup

### Local Development Prerequisites

- Go toolchain compatible with `cli/go.mod`
- Node.js 22+
- npm

### Local-Only Setup (Development Verification)

```bash
# CLI
cd cli
go mod tidy -diff
go test ./...
go build ./...
go build -o pc ./cmd/pc
./scripts/check_coverage.sh 95
./scripts/check_coverage_per_package.sh 95
./scripts/verify_phase3_manual.sh
./scripts/verify_local_demo.sh

# Web
cd ../web
npm install
npm run lint
npm run typecheck
npm test
npm run test:coverage
npm run build
npx playwright install
npm run test:e2e:smoke
npm run test:e2e:cli-slide
npm run test:e2e:cli-demo

# Repo-level schema contract
cd ..
./scripts/check_schema_contract.sh
```

### Cloud Setup (Planned Runtime Path)

Cloud runtime features are implemented in later roadmap phases. The intended CLI setup path is:

```bash
pc setup \
  --neon-url="..." \
  --s3-bucket="..." \
  --s3-region="..." \
  --aws-key="..." \
  --aws-secret="..."
```

- AWS credentials are written to `~/.aws/credentials` under `[personal-context]`.
- Cloud metadata is stored in `~/personal-context/.pc/config.json` (no secret keys in config).

## CLI Command Reference

### Currently Implemented CLI Surface

- `pc --help`, `pc --version`
- `pc setup` — initialize local environment (directories, SQLite, templates)
- `pc add <path>` — create slide from folder (slide.html required, metadata.json for project/git fields)
- `pc show <id>` — display slide metadata, notes, figures, data files (`--format text|json`)
- `pc edit <id> <path>` — full replacement of content, notes, figures, data files
- `pc delete <id>` — soft-delete a slide
- `pc restore <id>` — un-delete a slide
- `pc move <id>` — change date and/or position (`--date`, `--first`, `--last`, `--after`, `--before`)
- `pc search <query>` — search slides by content, notes, or project (`--format table|ids|json`, `--limit`, `--project`, `--deleted`)
- `pc trash` — list soft-deleted slides
- `pc gc` — hard-delete trash older than 30 days (cascades child rows, removes files)
- `pc project set|clear|list` — manage active project (active project used by `pc add` when no `--project` flag)
- `pc doctor` — check local system health (DB, orphans, missing files)

### Planned Command Surface (Roadmap)

- Sync/data: `pc sync`, `pc fetch`, `pc export`, `pc import`, `pc restore-db`, `pc verify`

## Web UI Overview

Phase 1 provides a Next.js scaffold with:

- App Router + TypeScript
- Lint, unit tests, coverage thresholds (95%)
- Playwright DB-free smoke test
- Schema-contract module referencing canonical `schema/` artifacts

Full production UI behavior (slides, filters, edit/delete/restore, sync manager) is implemented in later roadmap phases.

## Development Workflow

### CLI (`cli/`)

```bash
go mod tidy -diff
go test ./...
go build ./...
go build -o pc ./cmd/pc
./scripts/check_coverage.sh 95
./scripts/check_coverage_per_package.sh 95
./scripts/verify_phase3_manual.sh
./scripts/verify_local_demo.sh
golangci-lint run ./...
```

`./scripts/verify_phase3_manual.sh` runs the full Phase 3 local flow (`setup/add/show/edit/move/delete/restore`), creates a standalone slide preview, and opens it in your default browser. Use `--no-open` for headless runs.

`./scripts/verify_local_demo.sh` runs a generalized local demo flow: it creates 10 numbered slides, deletes 5, restores 1, moves 1, verifies the final state through the real CLI, and opens a generated summary page with persisted previews for the first and last active slides. Use `--no-open` for headless runs.

### Web (`web/`)

```bash
npm install
npm run lint
npm run typecheck
npm test
npm run test:coverage
npm run build
npx playwright install
npm run test:e2e:smoke
npm run test:e2e:cli-slide
npm run test:e2e:cli-demo
```

`npm run test:e2e:cli-slide` requires Go on `PATH` because it executes `cli/scripts/verify_phase3_manual.sh --no-open`.

`npm run test:e2e:cli-demo` requires Go on `PATH` because it executes `cli/scripts/verify_local_demo.sh --no-open`.

### Repository (`repo root`)

```bash
./scripts/check_schema_contract.sh
```

## CI/CD

GitHub Actions workflow: `.github/workflows/ci.yml`

- Enforces schema contract checks.
- Runs CLI `go mod tidy -diff`, build/test/lint, aggregate coverage gate (`>=95%`), and per-package coverage gate (`>=95%` for each tested package).
- Runs web lint/test/coverage/build, Playwright smoke e2e, and Playwright standalone CLI-slide e2e.
- Uses pinned `golangci-lint` version via `GOLANGCI_LINT_VERSION`.

## Nightly Export Example

A copy-ready data-repo workflow template is provided at:

- `docs/nightly-export-workflow.example.yml`

It is scheduled for `0 4 * * *` UTC and uses:

- `pc export --from-cloud --path . --github-remote origin`

## Contributing

1. Keep changes scoped to the requested roadmap phase.
2. Add or update tests with code changes; do not lower coverage bars.
3. Update docs and memory files when behavior/commands/contracts change.
4. Run local checks before opening a PR.
5. Do not commit secrets; use placeholders and local env configuration.

## Documentation

- [Roadmap](docs/agent-layer/ROADMAP.md)
- [Decisions](docs/agent-layer/DECISIONS.md)
- [Context](docs/agent-layer/CONTEXT.md)
- [Commands](docs/agent-layer/COMMANDS.md)
- [Schema SQL](schema/schema.sql)
- [Schema Types](schema/schema-types.ts)
