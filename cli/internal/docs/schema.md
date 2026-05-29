# Chat schema

Chat data lives in three structures. SQLite (local mode) and Postgres (cloud
mode) keep the same shared columns; Postgres adds a `user_id` column for
multi-user scoping and a generated `search_vector` column for full-text search.

## chat_session

One row per imported transcript.

- `id` — Personal Context chat id (`YYYYMMDD-<8 hex>`), unique across records.
- `source` — `codex`, `claude_code`, or `gemini`.
- `source_session_id` — the agent's session identity (see `pc docs chat-import`).
- `parent_source_session_id` — for a Claude subagent session, the parent
  transcript's source session id; `NULL` for ordinary sessions. It is nullable
  source metadata, not a foreign key, so the parent row may be absent or
  imported later. Indexed (`idx_chat_session_parent`).
- `source_device_id` — registered device the transcript was imported from.
- `project_id` — assigned project, or `NULL` when unassigned.
- `cwd`, `title` — optional working directory and title.
- `started_at`, `last_activity_at` — UTC timestamps.
- `original_source_path` — provenance path of the imported transcript.
- `raw_source_key` — relative key of the managed raw copy
  (`chats/raw/<id>/source.{json,jsonl,ndjson}`).
- `created_at`, `updated_at`, `deleted_at` — UTC timestamps; `deleted_at` marks a
  soft delete.

`UNIQUE (source, source_session_id)` enforces one session per source identity.

## chat_item

Normalized messages/events inside a session.

- `id`, `session_id`, `ordinal` (0-based, unique per session), `role`,
  `item_type`, `text`, `search_text`, `raw_json`, `created_at`.

Deleting a session cascades to its items.

## chat_item_fts

A full-text index over `chat_item.search_text` (SQLite FTS5; Postgres uses a
generated `search_vector` with a GIN index). It backs `pc chat search`. The
canonical text lives in `chat_item`; the FTS structure only indexes
`search_text`.
