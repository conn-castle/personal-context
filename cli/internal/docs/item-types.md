# Chat item types

Every imported chat item has a normalized `item_type` and a `role`. The
`item_type` set is small and agent-agnostic; the `role` carries the speaker.

## item_type values

- `message` — ordinary chat content (user prompts and assistant replies).
- `tool_output` — tool calls and their results.
- `reasoning` — model chain-of-thought turns (e.g. Codex reasoning).
- `event` — system/status and error events (e.g. Gemini info/error lines).

Agent-specific labels are normalized at parse time so they never leak into
`item_type`. In particular, Gemini's `type:"gemini"` model turns become
`item_type=message` with `role=assistant`, and Gemini `type:"info"` /
`type:"error"` lines become `item_type=event` with `role=info` / `role=error`.

## role values

Common roles include `user`, `assistant`, `system`, `model`, `tool`, `info`,
and `error`. A role of `unknown` is used only when the source row provides no
role hint at all. Filter and group on `item_type` for structure and on `role`
for the speaker.
