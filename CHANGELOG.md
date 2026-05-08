# Changelog

All notable changes to Personal Context are documented here.

Release entries must use this format so the release workflow can extract notes:

```markdown
## vX.Y.Z - YYYY-MM-DD
```

## Unreleased

- Renamed the data model from "slides" to "records" across the CLI, web API, web UI, database schema, and git export format. Breaking change: database tables (`slides`/`slide_figures`/`slide_data_files` → `records`/`record_figures`/`record_data_files`), API URL paths (`/api/slides/*` → `/api/records/*`), JSON shapes (`SlideSummary`/`SlideDetail` → `RecordSummary`/`RecordDetail`), and git export folder layout (`slides/{slide_id}/slide.html` → `records/{record_id}/record.html`) are not backward-compatible.

## v0.1.0 - 2026-05-07

- Added release automation for GitHub Releases and the Conn Castle Homebrew tap.
- Licensed the project under PolyForm Noncommercial 1.0.0.
