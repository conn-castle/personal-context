# Changelog

All notable changes to Personal Context are documented here.

Release entries must use this format so the release workflow can extract notes:

```markdown
## vX.Y.Z - YYYY-MM-DD
```

## Unreleased

- Optimized SQLite chat imports by suspending per-row chat FTS trigger maintenance during the import, rebuilding the chat search index once afterward, and serializing imports with the local sync lock.
- Made local setup and CLI startup fail loudly when a pre-release SQLite store has the old standalone `chat_item_fts` table shape or missing chat FTS triggers.
- Changed the pre-release project registry command from `pc project add` to `pc project register` for consistency with `pc device register`; no compatibility alias is exposed.
- Narrowed default project-path chat import roots to Claude and Gemini transcript directories so config/cache JSON under `.claude/` and `.gemini/` is not parsed as chat data.
- Documented the pre-release `pc chat list --format json` envelope rename from `sessions` to the canonical `{items,total,next_cursor}` shape used by domain list/search commands.
- Clarified that `pc chat search --format json` reports `total` as the current page size and uses `next_cursor` for pagination.

## v0.1.1 - 2026-05-09

- Renamed the data model from "slides" to "records" across the CLI, web API, web UI, database schema, and git export format. Breaking change: database tables (`slides`/`slide_figures`/`slide_data_files` → `records`/`record_figures`/`record_data_files`), API URL paths (`/api/slides/*` → `/api/records/*`), JSON shapes (`SlideSummary`/`SlideDetail` → `RecordSummary`/`RecordDetail`), and git export folder layout (`slides/{slide_id}/slide.html` → `records/{record_id}/record.html`) are not backward-compatible.
- Added paginated `{items, total, next_cursor}` envelope to `pc list --format json`, `pc search --format json`, and `GET /api/records`. Breaking change: `pc search --format json` previously returned a bare array; consumers using `jq '.[]'` should switch to `jq '.items[]'`. `pc search` also gains a default `--limit` of 50 (pass `--limit 0` for unlimited) and surfaces `Showing X of Y` truncation footers for table/ids output.

## v0.1.0 - 2026-05-07

- Added release automation for GitHub Releases and the Conn Castle Homebrew tap.
- Licensed the project under PolyForm Noncommercial 1.0.0.
