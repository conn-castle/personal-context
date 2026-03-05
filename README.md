# Personal Context (`pc`)

Personal engineering notebook system that stores work as individual HTML slides — images, text, tables, code, and data — organized chronologically and by project.

## Status

**Design phase complete, pre-implementation.** Requirements docs in `docs/requirements/` are the current design specification. During Phase 1 (scaffolding), all design information will be consolidated into `docs/agent-layer/` memory files and `schema/`, and the requirements folder will be deleted. See the [Roadmap](docs/agent-layer/ROADMAP.md) for implementation progress.

## Repository Structure

```
personal-context/              # code repo (this repo)
├── cli/                       # Go CLI (planned)
├── web/                       # Next.js app (planned)
├── schema/                    # schema.sql, schema-types.ts (planned)
├── docs/
│   ├── requirements/          # Design specification (temporary, deleted after Phase 1)
│   └── agent-layer/           # Agent memory files (roadmap, decisions, context)
└── README.md
```

A separate **data repo** will hold nightly git exports of slide data.

## Architecture

```
Local SQLite + local files      <- CLI writes here (always)
     | pc sync (bidirectional, if cloud configured)
Neon Postgres + S3              <- cloud source of truth (web UI)
     | nightly export
GitHub + S3                     <- portable backup
```

## Documentation

- [Product Spec](docs/requirements/product-spec.md) — design specification (temporary)
- [Schema](docs/requirements/schema.sql) — database DDL (Postgres dialect, design-level)
- [Types](docs/requirements/schema-types.ts) — TypeScript interfaces
- [Roadmap](docs/agent-layer/ROADMAP.md) — phased implementation plan
- [Decisions](docs/agent-layer/DECISIONS.md) — architectural decision log
- [Context](docs/agent-layer/CONTEXT.md) — project context and invariants
