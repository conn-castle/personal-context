# Personal Context (`pc`)

Personal engineering notebook system that stores work as individual HTML slides — images, text, tables, code, and data — organized chronologically and by project.

## Status

**Phase 1 scaffolding in progress.** `cli/`, `web/`, and `schema/` directories are now present, and schema/type files are canonicalized under `schema/`. Remaining design documents in `docs/requirements/` are temporary and will be removed once migration tasks in Phase 1 are complete. See the [Roadmap](docs/agent-layer/ROADMAP.md) for current progress.

## Repository Structure

```
personal-context/              # code repo (this repo)
├── cli/                       # Go CLI workspace (scaffolded)
├── web/                       # Next.js workspace (scaffolded)
├── schema/                    # Canonical schema.sql and schema-types.ts
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
- [Schema](schema/schema.sql) — canonical database DDL source
- [Types](schema/schema-types.ts) — canonical TypeScript schema interfaces
- [Roadmap](docs/agent-layer/ROADMAP.md) — phased implementation plan
- [Decisions](docs/agent-layer/DECISIONS.md) — architectural decision log
- [Context](docs/agent-layer/CONTEXT.md) — project context and invariants
