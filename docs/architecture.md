# Personal Context — Architecture (Redesign Target)

> _The target architecture for the storage redesign. Companion to [VISION.md](VISION.md)
> (why Personal Context exists) and [access-scenarios.md](access-scenarios.md) (the
> access, data-locality, and ownership requirements). This is a settled-statement doc,
> not a notebook: it records decisions that are **locked**; open design questions live in
> project working memory. The README's current "Architecture" section describes the
> **existing** implementation and will be updated when this redesign lands._

## Founding decision (locked 2026-06-06)

Personal Context is **one tool**, built as a **shared substrate** carrying **two
differentiated domains**, exposing **one unified corpus**, with the rich web UI as a
**review layer** over an agent-driven, headless-everywhere write path.

One product and one mental model for the user — **not** one undifferentiated data model,
and **not** several tools.

## Shape

```
                  one tool  (one CLI · install · config · access)
                                       │
                       ┌───────────────┴───────────────┐
                       │       shared substrate         │
                       │  canonical store · single-     │
                       │  source-of-truth · access ·    │
                       │  sync · retrieval              │
                       └───────────────┬───────────────┘
                 ┌─────────────────────┴─────────────────────┐
            Records domain                              Archive domain
       curated · mutable · small                    raw · immutable · bulk
       agent-produced deliverable               append-only · content-addressed
                                                     (chats + artifacts)
                 └──────────────── unified retrieval ─────────┘
                                       │
                            review UI (humans look here)
```

## The two domains

- **Records** — the agent's deliberate, curated **output**: a mutable, structured,
  visual artifact produced (often unattended) for a human to review. Small volume.
  This is the agent-authored lab notebook.
- **Archive** — the raw **byproduct** of work, kept for retrieval: immutable,
  append-only, content-addressed, bulk. Two sibling item kinds under one domain:
  **chats** (imported agent transcripts) and **artifacts** (the files agents generate as
  they work).

Records are curated and mutable; the archive is accumulated and immutable. Each is
stored according to its nature rather than forced into a single model.

## Settled principles

| Decision | Choice | Why |
|---|---|---|
| Packaging | **One tool** | One UX / CLI / config; cross-domain retrieval and single-source-of-truth are cleanest with one ownership boundary; the vision is one frictionless corpus. |
| Data model | **Differentiated domains** | Records (mutable / curated / small) and archive (immutable / bulk / content-addressed) have genuinely different storage and lifecycle needs; one model compromises one side (the lesson of the current design). |
| Substrate | **Shared** | The canonical store, single-source-of-truth, access scenarios, sync, and retrieval are identical across domains — built once, not per domain. |
| Corpus | **Unified** | "Interact with every piece of knowledge with zero friction" is cross-domain retrieval: one queryable corpus over both domains. |
| UI role | **Review layer** | Agents author headless, everywhere; the rich web UI is where a human is present to look — layered over the write path, not the authoring mechanism. |

## The discipline this requires

One tool with differentiated domains beats today's one-model design **only if the
substrate↔domain boundary is held with discipline.** The archive must stay append-only
and content-addressed; the substrate must not leak domain specifics; the records domain
must stay the small mutable thing it already is. A sloppy layering yields a monolith that
is *also* internally fragmented — the worst of both.

## Still in design

These build on the founding shape above and are tracked in project working memory, not
here: the durable canonical-copy location (local / git / cloud object store), the blob
and retention tiering for bulk archive bytes, and the per-domain storage specifics.
