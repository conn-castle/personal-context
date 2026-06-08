# Search syntax

`pc chat search <query>` and `pc search <query>` query the full-text index over
chat item text.

## Matching

- The query is split on whitespace into terms, and every term must match
  (logical AND). For example, `chat import` matches items containing both
  `chat` and `import`.
- Matching is token-based and case-insensitive.
- Tool outputs are excluded by default. Pass `--include-tool-outputs` to include
  `item_type=tool_output` items.

## Boolean operators are not supported

Personal Context search is implicit-AND only; it has no Boolean query parser.
To avoid silently distorting results, a query that contains a standalone
all-uppercase operator keyword — `OR`, `AND`, `NOT`, or `NEAR` — is rejected
with an explicit error instead of treating the keyword as a literal required
term. For example, `monarch OR zzznomatch` errors rather than quietly returning
fewer hits than `monarch` alone.

Lowercase and mixed-case words such as `and`, `or`, `not`, and `near` are
ordinary search terms, so natural-language queries like
`research and development`, `not found`, and `near miss` work normally.

## Filters

- Limit results to a single agent with `--agent codex|claude|gemini`.
- Search within a specific project using `--project <id>`.
- Show only subagent sessions from a given parent transcript with
  `--parent-source-session-id <sid>`.
- `--limit` / `--offset` paginate; `--format json` emits a structured envelope.

## JSON output semantics

`--format json` emits `{ "items": [...], "total": N, "next_cursor": "..." }`:

- `total` is the full number of matching items under the same filters, not the
  size of the returned page. Use it for "N results found" displays.
- `next_cursor` is a string offset for the next page, or `null` when the current
  page is the last one. It is the reliable "has more" signal.
- Each `pc chat search` item includes a `score` field: the FTS relevance rank of
  that hit, where a higher score is a better match. Items also carry
  backward-compatible top-level `id`, `source`, `project_id`, and `date` fields
  alongside the nested `session` object.

## Ranking

- Results are ranked by full-text relevance.
- In local SQLite mode, `pc chat search` uses SQLite FTS5's native rank order so
  small pages can return without sorting every match. Equal-rank chat hits do
  not have a documented secondary order in this mode.
- In cloud Postgres mode, chat hits with equal rank are secondarily ordered by
  session recency, then item ordinal.

## Tips

- To read a full transcript after finding a hit, run `pc chat show <chat-id>`.
