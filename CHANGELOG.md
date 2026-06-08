# Changelog

All notable changes to Personal Context are documented here.

Release entries must use this format so the release workflow can extract notes:

```markdown
## vX.Y.Z - YYYY-MM-DD
```

## Unreleased

### Added

### Changed

### Fixed

## v0.1.5 - 2026-06-05

### Fixed

- Fixed `pc chat import` aborting the whole run when one transcript file cannot be parsed. Bad files are now reported on stderr as skipped and counted in the JSON summary as `files_skipped`, while the rest of the import continues.
- Fixed Claude Code discovery so imports under `projects/` only queue top-level session JSONL files and `subagents/agent-*.jsonl` transcripts, excluding sidecars such as `tool-results/` and `memory/`.
- Fixed Codex fork imports collapsing onto their replayed parent session id. Fork rollouts now keep their own session id, store `forked_from_id` as lineage metadata, and import as distinct sessions; fork rows intentionally preserve the replayed parent history present in the source file.
- Fixed `pc chat import` attributing a Codex fork to its parent's project. A fork rollout replays the parent's metadata (carrying the parent's working directory) after the fork's own header, so the working directory and title now lock to the fork's header instead of last-wins — divergent and empty (never-continued) forks attribute to the correct project. Non-fork sessions are unaffected (a mid-session directory change still wins).

## v0.1.4 - 2026-06-04

### Added

- Made `pc gc` trash retention configurable: set `gc_retention_days` (a positive integer number of days) in `~/personal-context/.pc/config.json` to override the 30-day default. The window applies to both record and chat trash. An unset value keeps the 30-day default; invalid values (zero, negative, or above 36500) are rejected when the config is read or written, and the setting is preserved across cloud setup and removal.

### Changed

- Reduced `pc chat import` memory use on large transcripts: the JSON transcript parser now streams array elements one at a time instead of decoding the whole payload into memory, preserving the existing ordinal, last-wins, and empty/no-array semantics.
- Bounded cross-domain search pagination: each domain query now fetches only the rows needed for the requested page instead of loading the full match set into memory, with ORDER BY tiebreakers aligned across SQLite and Postgres so merged result ordering is unchanged.

### Fixed

- Fixed Gemini chat sessions importing as unassigned: Gemini transcripts carry no working directory, so they never matched a registered project. Import now resolves each session's repo root from an on-disk `.project_root` path (preferred) or by matching the session's project hash to a registered project path, and persists it as the session cwd so project attribution works. A later `pc project register` re-attributes `.project_root` sessions; hash-only sessions need a re-import.
- Fixed chat sync leaving the items table inconsistent: a chat session's row update and its item replacement are now applied in a single transaction (SQLite and Postgres), so a mid-update failure rolls back cleanly and the next sync re-syncs the chat instead of skipping it on equal timestamps.
- Fixed `pc records edit` (and other partial updates) silently restoring trashed records: updates that do not touch deletion now preserve `deleted_at` instead of clearing it, so editing a soft-deleted record no longer un-deletes it.
- Fixed legacy chats that had raw transcripts but missing or diverged item rows: re-importing a byte-identical managed transcript now re-parses and replaces the stored items when they are missing or have drifted, healing previously inconsistent imports.
- Fixed search result dates under Postgres: record dates are now scanned and formatted in UTC so they match the canonical date string instead of reflecting the server's local zone.
- Fixed five web record-viewer defects: sync failures now surface in Settings instead of only logging to the console; editing a record updates its list thumbnail without a full refresh; keyboard record navigation no longer acts on stale filter/selection state; and the collapsed details panel now satisfies the resizable-panel layout contract.

## v0.1.3 - 2026-05-29

### Added

- Added `pc docs [topic]` and `pc docs search <query>`: embedded concept reference (chat-import, item-types, schema, search-syntax, project-device-registry) that matches the installed binary and is pipeable/searchable.
- Added `parent_source_session_id` chat metadata so Claude Task-tool subagent transcripts import as distinct sessions linked to their parent, with `pc chat list --parent-source-session-id` / `pc chat search --parent-source-session-id` navigation and a parent/subagent view in `pc chat show` (text + JSON). The field round-trips through snapshot export/import and cloud sync.

### Changed

- Changed the `pc chat import` JSON summary to separate work performed from stored state: renamed `items_created` to `items_imported` (rows written this run) and added `items_delta` and `items_after_import` (authoritative, derived from the repository item count). Added `duplicates_skipped` and `collisions_skipped`, and redefined `raw_sources_copied` to count distinct retained sessions rather than per-file copy operations.
- Normalized chat item types so agent-specific labels no longer leak into `item_type`: Gemini model turns become `message`/`assistant` and Gemini info/error lines become `event` with role `info`/`error`.

### Fixed

- Fixed Claude Code subagent transcripts silently overwriting each other: each subagent file now gets a file-unique source identity instead of colliding on the shared parent session id, so re-importing preserves every subagent as its own session (run a wipe-and-reimport to recover previously lost subagent content from on-disk source files).
- Fixed Gemini project-name vs project-hash duplicate paths: divergent copies now get distinct, path-derived source identities (never overwriting each other) and exact byte-identical copies are collapsed.
- Added defense-in-depth for source-identity collisions: a scanned file that collides with a different file's existing session and diverges is reported and skipped instead of overwriting the unrelated managed source.
- Stopped empty or metadata-only transcript files from creating empty chat sessions.

## v0.1.2 - 2026-05-25

### Breaking Changes

- Moved record-specific commands under `pc records ...`: use `pc records add|list|show|edit|delete|restore|move|stats|files list` instead of the previous top-level record commands. Top-level `pc show` and `pc search` now operate across records and chats.
- Changed `pc project add` to `pc project register`; no compatibility alias is exposed. `pc project register <id> [path] --device <id>` can also associate a source path with a project for chat import attribution.
- Changed `pc search --format json` from the record-only paginated envelope to a cross-domain JSON array with a `domain` field on each result. Use `pc records list --query ... --format json` for the record-list `{items,total,next_cursor}` envelope.
- Made the chat schema a clean-cut local SQLite update. Stores created before the chat tables, external-content `chat_item_fts`, or chat FTS triggers exist now fail loudly during `pc setup` or CLI startup and must be backed up and recreated instead of silently running with a partial schema.

### Added

- Added `pc chat import --device <id>` for Codex, Claude Code, and Gemini transcript files, including managed raw transcript copies under `chats/raw/{chat_session_id}/`, project-path matching, optional `--agent` / `--root`, and safe `--delete-source` cleanup after Personal Context owns a copy.
- Added `pc chat list|search|show|delete|restore`, chat-aware `pc trash` and `pc gc`, pager support for chat display, and JSON list/search envelopes for chat browsing.
- Added chat persistence across SQLite, Postgres, cloud sync, and deterministic git export/import. Snapshots now include `chats/` metadata, normalized `items.jsonl`, and raw transcript sources.
- Added cross-domain search for records and chats, including `--domain records|chats`, `--offset`, and `--include-tool-outputs`.
- Added `pc fetch --all` to download every non-deleted cloud record data file into the canonical local data path, skip files that already match recorded size/hash metadata, and report per-file failures without stopping the full scan.

### Changed

- Optimized chat imports by skipping unchanged managed sources, appending only new JSONL/NDJSON rows when transcripts grow, batching SQLite writes, serializing imports with the local sync lock, and rebuilding chat FTS once after bulk import work.
- Narrowed default project-path chat import roots to agent transcript directories so config/cache JSON under `.claude/`, `.claude-config/`, and `.gemini/` is not parsed as chat data.
- Hardened fetch downloads by rejecting invalid record/file path metadata, enforcing canonical S3 keys, verifying downloaded size/hash, and removing unverified files after failed integrity checks.
- Hardened sync and lifecycle handling with stale sync-lock recovery, chat raw-source validation, chat raw-source cloud transfer warnings, and cloud-first cleanup for hard-deleted records and chats.
- Hardened web and local API request handling with bounded JSON request bodies, consistent 413 responses, stricter bearer token parsing, and safer local-mode/auth edge cases.
- Updated release automation so release preflight requires a matching changelog section and release artifact generation refuses stale files in `DIST_DIR`.

## v0.1.1 - 2026-05-09

- Renamed the data model from "slides" to "records" across the CLI, web API, web UI, database schema, and git export format. Breaking change: database tables (`slides`/`slide_figures`/`slide_data_files` → `records`/`record_figures`/`record_data_files`), API URL paths (`/api/slides/*` → `/api/records/*`), JSON shapes (`SlideSummary`/`SlideDetail` → `RecordSummary`/`RecordDetail`), and git export folder layout (`slides/{slide_id}/slide.html` → `records/{record_id}/record.html`) are not backward-compatible.
- Added paginated `{items, total, next_cursor}` envelope to `pc list --format json`, `pc search --format json`, and `GET /api/records`. Breaking change: `pc search --format json` previously returned a bare array; consumers using `jq '.[]'` should switch to `jq '.items[]'`. `pc search` also gains a default `--limit` of 50 (pass `--limit 0` for unlimited) and surfaces `Showing X of Y` truncation footers for table/ids output.

## v0.1.0 - 2026-05-07

- Added release automation for GitHub Releases and the Conn Castle Homebrew tap.
- Licensed the project under PolyForm Noncommercial 1.0.0.
