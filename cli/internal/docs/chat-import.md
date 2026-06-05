# Chat import

`pc chat import` scans agent transcript roots, normalizes each transcript into a
chat session plus chat items, and copies a managed raw source that Personal
Context owns. `--device <id>` is required and must name a registered, active
device. Use `--agent codex|claude|gemini` to limit the scan, and `--root <dir>`
(requires `--agent`) to override the default roots. For `--agent claude` the
root is treated as a Claude `projects/` directory: only
`<project-dir>/<session>.jsonl` and
`<project-dir>/<session>/subagents/agent-*.jsonl` are discovered, so point
`--root` at a `projects`-shaped parent rather than a flat directory of
transcript files.

## Summary fields

The command prints a JSON summary. The fields separate **work performed** from
the **resulting stored state** so the numbers reconcile with the database.

Work performed:

- `sessions_created` — new chat sessions inserted this run.
- `sessions_updated` — existing sessions whose content was replaced or appended.
- `sessions_skipped` — files whose content already matched the stored session
  (unchanged re-imports, including a session re-recognized by its source id).
- `duplicates_skipped` — files collapsed as exact byte-identical duplicates that
  were scanned under a different path (e.g. Gemini's project-name and
  project-hash copies of one session).
- `collisions_skipped` — files refused because they collided on an existing
  `(source, source_session_id)` owned by a different source file and diverged
  from it. The run never overwrites the unrelated source; a warning naming both
  paths and the colliding source id is printed to stderr.
- `files_skipped` — files that could not be parsed as transcripts. Each skipped
  path is printed to stderr and the import continues with other files.
- `items_imported` — chat item rows written this run. A replaced session counts
  every re-inserted row, so this is work performed, not net growth.
- `files_scanned` — transcript files examined (including empty ones that create
  no session).
- `raw_sources_copied` — distinct chat sessions whose managed raw source was
  written this run (retained state, not per-file copy operations).
- `sources_deleted` / `source_delete_warnings` — only with `--delete-source`.

Resulting state (authoritative, derived from the repository item count):

- `items_after_import` — absolute number of stored chat items (in non-deleted
  sessions) after the run.
- `items_delta` — signed net change in stored items (`after - before`). A replace
  that swaps one transcript for a larger one can show `items_imported` greater
  than `items_delta`.

## Source identity and data safety

Each session is keyed by `(source, source_session_id)`. Identity is derived so
that distinct transcripts never collide and overwrite one another:

- Codex and Claude Code take `source_session_id` from the transcript's internal
  session id. Codex fork rollouts keep their own first `session_meta.id` even
  when a later replayed parent header appears, and store `forked_from_id` in
  `parent_source_session_id`.
- Claude Code **subagent** (Task-tool sidechain) transcripts under a
  `subagents/` directory carry the parent transcript's session id on every row.
  They get a file-unique `source_session_id` of `<parent_sid>:<subagent_file>`
  and record `parent_source_session_id = <parent_sid>` so each subagent is its
  own session and stays navigable from the parent.
- Gemini transcripts carry no usable internal id, so `source_session_id` is
  derived from the file path (the project-key directory plus basename). The same
  session stored under both a project-name and a project-hash path therefore
  gets distinct identities; byte-identical copies are collapsed via
  `duplicates_skipped`.

Empty or metadata-only transcript files (0-byte files, Claude cursor markers,
Gemini session-header-only files) are counted in `files_scanned` but never
create a chat session.

## Recovery

Source transcript files are the canonical recovery source. If a store predates
these fixes and is missing subagent content, back up `~/personal-context`, wipe
`.pc/pc.db`, re-run `pc setup`, re-register devices and projects, and re-import
**without** `--delete-source`.
