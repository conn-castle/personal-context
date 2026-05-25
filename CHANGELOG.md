# Changelog

All notable changes to Personal Context are documented here.

Release entries must use this format so the release workflow can extract notes:

```markdown
## vX.Y.Z - YYYY-MM-DD
```

## Unreleased

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
