# Personal Context (`pc`)

Personal Context is a local-first engineering notebook system that stores work as HTML slides, attached figures, and data files, organized by date and project.

## Status

Phase 10 (Authentication & Multi-User) is complete. Phase 11 (Deployment, CI/CD, and Integration Testing) is next — see [Roadmap](docs/agent-layer/ROADMAP.md).

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
│   ├── check_schema_contract.sh    # Canonical schema guard
│   └── check_schema_equivalence.sh # Postgres/SQLite schema parity guard
└── .github/workflows/ci.yml
```

## Setup

### Local Development Prerequisites

- Go toolchain compatible with `cli/go.mod`
- Node.js 22+
- pnpm (`npm i -g pnpm`)
- Docker running for the Postgres and S3 integration suites (`testcontainers-go`)

### Development Verification

This sequence covers both local development checks and the Docker-backed cloud/integration suites used for full repo verification.

```bash
# CLI
cd cli
go mod tidy -diff
go test ./...
go build ./...
go build -o pc ./cmd/pc
./scripts/check_coverage.sh 95
./scripts/check_coverage_per_package.sh 95
go test -tags integration ./internal/repository/postgres/ -v -timeout 180s
go test -tags integration ./internal/s3client/ -v -timeout 60s
go test -tags integration ./internal/cloude2e/ -v -timeout 420s
./scripts/verify_phase3_manual.sh
./scripts/verify_local_demo.sh

# Web
cd ../web
pnpm install
pnpm lint
pnpm typecheck
pnpm test
pnpm test:coverage
pnpm build
pnpm exec playwright install
pnpm test:e2e:smoke
pnpm test:e2e:slide-browser
pnpm test:e2e:cli-slide
pnpm test:e2e:markdown
pnpm test:e2e:visual
pnpm test:e2e:cli-demo

# Or from the repo root (recommended):
make check          # everything
make test           # just tests
make web-e2e        # just Playwright

# Repo-level schema contract
cd ..
./scripts/check_schema_contract.sh
./scripts/check_schema_equivalence.sh
```

### Cloud Setup

```bash
# Interactive (prompts whether to configure cloud, then asks for each value):
pc setup

# Non-interactive:
pc setup \
  --neon-url="..." \
  --s3-bucket="..." \
  --s3-region="..." \
  --aws-key="..." \
  --aws-secret="..." \
  --api-key="pc_key_..."

# Remove cloud configuration:
pc setup --remove-cloud
```

- First cloud bootstrap for a fresh database:

```bash
pc setup --init-cloud-schema --neon-url="..."
```

Run this once before the first web sign-in flow so the `users` and `api_keys`
tables exist. Then enable registration, create a user in the web app, create an
API key from that authenticated session, and run the full `pc setup` command
above with `--api-key`.

- AWS credentials are written to `~/.aws/credentials` under `[personal-context]`.
- Cloud metadata is stored in `~/personal-context/.pc/config.json` and includes the CLI API key (treat this file as sensitive).
- Validates Neon connectivity and S3 access before persisting configuration.
- Creates Postgres schema tables on first cloud setup.
- `--api-key` stores a CLI authentication key (generate it from an authenticated web session via `/api/api-keys`). Required for multi-user cloud sync.

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
- `pc gc` — hard-delete trash older than 30 days (cascades child rows, removes files; cloud-aware: deletes from cloud first to prevent sync re-creation)
- `pc project set|clear|list` — manage active project (active project used by `pc add` when no `--project` flag)
- `pc doctor` — check system health (DB, orphans, missing files; cloud connectivity if configured)
- `pc sync` — bidirectional sync between local SQLite and cloud Postgres/S3 (requires cloud configuration)
- `pc fetch <slide_id>` — download data files from cloud S3 (`--project`, `--recent 3d/2w/1m/1y`, `--output`)
- `pc export --path <dir>` — write deterministic git snapshot folders (`templates/`, `slides/`); `--from-cloud` reads slides/assets from Postgres/S3
- `pc import <path>` — merge a git snapshot into local SQLite using `updated_at` rules (`same/older -> skip`, `newer -> replace`)
- `pc restore-db <path>` — replace local SQLite state from a git snapshot and create an auto-backup snapshot first under `~/personal-context/.pc/backups/`
- `pc verify` — run a local Tier 2 round-trip verification; `pc verify --from-cloud` verifies the cloud-rooted round-trip path
- `pc serve` — start Go HTTP server on `127.0.0.1:9876` (default) implementing the web API against local SQLite + filesystem (`--port` to override)
- `pc screenshot <id>` — capture a 1920x1080 PNG of a slide using headless Chrome (`--output` to set path; requires Chrome/Chromium or `PC_CHROME_PATH`)
- `pc seed` — create 6 tutorial slides under `personal-context/tutorial` project (idempotent; auto-run by `make dev-local`)

## Web UI Overview

The web UI provides:

- App Router + TypeScript
- Real slide/project/sync/file API routes backed by Neon + S3 helpers
- Three-panel Slide Browser with date-grouped navigation, 16:9 sandboxed preview, notes editing, attachment actions, slide delete controls, and trash counts/purge controls in Settings
- `useSyncManager` 4-layer polling and cursor-based pagination
- Vitest coverage thresholds (95%) plus Playwright smoke and Slide Browser e2e coverage
- Schema-contract module referencing canonical `schema/` artifacts
- Local dev mode: Next.js proxies to `pc serve` when `LOCAL_BACKEND_URL` is set (no cloud credentials needed)

### Authentication

The web UI uses Auth.js (NextAuth v5) with email/password credentials. Two operational modes:

- **Local mode** (`pc serve`): No authentication. Single-user, localhost only.
- **Cloud mode** (Amplify/Neon): Auth.js credentials with JWT sessions (90-day expiry). Per-user data isolation (slides, S3 files, sync version).

CLI authentication uses API keys generated from an authenticated web session. Store the key via `pc setup --api-key=<key>`.
Create a key from an authenticated web session:

```js
await fetch("/api/api-keys", {
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ label: "CLI" }),
}).then((r) => r.json());
```

Copy the returned `raw_key` value once and set it with `pc setup --api-key=<key>`.

Set `AUTH_TRUST_HOST=true` in cloud/proxy deployments (Amplify, ingress, reverse proxy) so Auth.js trusts forwarded host/proto headers.

Registration is disabled by default. Set `REGISTRATION_ENABLED=true` only while onboarding users.

Production deployments must enforce upstream rate limiting (WAF/reverse proxy) for `/api/auth/*`, `/api/register`, and repeated invalid API-key attempts. The app does not include built-in auth throttling.

### Legacy Pre-Auth Cloud Upgrade (Manual Path)

If your cloud database was created before multi-user auth (legacy pre-auth schema), automated in-place migration is not supported. The Postgres schema bootstrap intentionally fails with:

`legacy pre-auth cloud schema detected (...): in-place migration is required before applying auth-aware schema`

Operator playbook for this case:

1. Freeze writes to the legacy deployment and take a backup/export snapshot.
2. Provision a fresh cloud environment with the auth-aware schema (new Neon DB + web auth config).
3. Migrate cloud object keys to user-scoped prefixes (`users/{user_id}/...`) before cutover.
4. Import/restore data into the new environment and validate with `pc sync`, `pc doctor`, and web sign-in before reopening writes.

This migration path is intentionally manual today; treat it as a planned maintenance cutover.

### Local Dev Mode (`pc serve`)

Run the full web UI against your local SQLite database and filesystem — no Neon or S3 required.

```bash
# Automatic: if DATABASE_URL and S3_BUCKET are set in the environment or web/.env.local,
# starts cloud mode; otherwise starts local mode
make dev

# Explicit local mode
make dev-local

# Explicit cloud mode (fails fast if DATABASE_URL or S3_BUCKET is missing)
make dev-cloud

# Manual (two terminals):
# Terminal 1:
cd cli && go build -o pc ./cmd/pc && ./pc serve --port 9876
# Terminal 2:
cd web && LOCAL_BACKEND_URL=http://127.0.0.1:9876 pnpm dev
```

In local mode, `/login`, `/register`, and `/api/auth/*` are intentionally disabled.
`LOCAL_BACKEND_URL` must use a loopback host (`localhost`, `127.0.0.1`, or `::1`).

Prerequisites: `pc setup` must have been run (creates `~/personal-context/` and the local SQLite database). `make dev-cloud` requires non-empty `DATABASE_URL` and `S3_BUCKET` values in the shell environment or `web/.env.local`.

## Development Workflow

### CLI (`cli/`)

```bash
go mod tidy -diff
go test ./...
go build ./...
go build -o pc ./cmd/pc
./scripts/check_coverage.sh 95
./scripts/check_coverage_per_package.sh 95
go test -tags integration ./internal/repository/postgres/ -v -timeout 180s
go test -tags integration ./internal/s3client/ -v -timeout 60s
go test -tags integration ./internal/cloude2e/ -v -timeout 420s
./scripts/verify_phase3_manual.sh
./scripts/verify_local_demo.sh
golangci-lint run ./...
```

The three `-tags integration` commands require Docker because testcontainers-go starts Postgres and MinIO containers for the cloud data-layer packages.

`go test -tags integration ./internal/cloude2e/ -v -timeout 420s` is the self-contained cloud CLI e2e suite: it drives the compiled `pc` binary through cloud onboarding, first sync plus doctor, two-home auto-sync conflict resolution, and `fetch --project --output` using testcontainers-backed Postgres and MinIO plus temp homes for isolated AWS credentials.

`./scripts/verify_phase3_manual.sh` runs the full Phase 3 local flow (`setup/add/show/edit/move/delete/restore`), creates a standalone slide preview, and opens it in your default browser. Use `--no-open` for headless runs.

`./scripts/verify_local_demo.sh` runs a generalized local demo flow: it creates 10 numbered slides, deletes 5, restores 1, moves 1, verifies the final state through the real CLI, and opens a generated summary page with persisted previews for the first and last active slides. Use `--no-open` for headless runs.

### Web (`web/`)

```bash
pnpm install
pnpm lint
pnpm typecheck
pnpm test
pnpm test:coverage
pnpm build
pnpm exec playwright install
pnpm test:e2e:smoke
pnpm test:e2e:slide-browser
pnpm test:e2e:markdown
pnpm test:e2e:visual
pnpm test:e2e:cli-slide
pnpm test:e2e:cli-demo
```

Or via Makefile: `make web-all` runs lint + typecheck + coverage + build + e2e.

`pnpm test:e2e:slide-browser` exercises the mocked Slide Browser workflow (browse, filter, notes edit, delete/restore, sync badge, error states, pagination) via `page.route()` interception, so no real backend is required.

`pnpm test:e2e:cli-slide` requires Go on `PATH` because it executes `cli/scripts/verify_phase3_manual.sh --no-open`.

`pnpm test:e2e:cli-demo` requires Go on `PATH` because it executes `cli/scripts/verify_local_demo.sh --no-open`.

### Repository (`repo root`)

```bash
./scripts/check_schema_contract.sh
./scripts/check_schema_equivalence.sh
```

## CI/CD

GitHub Actions workflow: `.github/workflows/ci.yml`

- Enforces schema contract and schema-equivalence checks.
- Runs CLI `go mod tidy -diff`, build/test/lint, aggregate coverage gate (`>=95%`), per-package coverage gate (`>=95%` for each tested package), CLI e2e, and the Docker-backed Postgres/S3 integration suites.
- Runs web lint/test/coverage/build, Playwright smoke e2e, Playwright Slide Browser e2e, and the standalone CLI-slide/local-demo Playwright flows.
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
