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

## Filters

- `--agent codex|claude|gemini` restricts results to one source.
- `--project <id>` restricts to one project.
- `--parent-source-session-id <sid>` restricts results to the subagent sessions
  of one parent transcript, so you can search within a parent's subagents.
- `--limit` / `--offset` paginate; `--format json` emits a structured envelope.

## Tips

- Results are ranked by relevance, then recency.
- To read a full transcript after finding a hit, run `pc chat show <chat-id>`.
