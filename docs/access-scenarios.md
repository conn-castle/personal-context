# Personal Context — Access & Data Requirements

> _A companion to [VISION.md](VISION.md). VISION covers **why** Personal Context exists;
> this covers **where, how, and under what rules** its data must be usable — the access,
> data-locality, persistence, and ownership requirements the user has. It is a settled
> statement of needs, not a working notebook: open questions and design notes belong in
> the project's working memory, not here._

## Core requirement

The data must be **easily accessible from any repository the user starts working in** —
regardless of the device or environment.

The hard part is that "accessible" has to mean different things in different
environments, because each one imposes different constraints on what may persist
locally, what may cross the network, and how much storage is available. The scenarios
below are the big ones. They are **not exhaustive** — there are surely others the user
hasn't thought of yet — but they bound the space.

## Single source of truth

Personal Context is the **sole source of truth for all the data it contains.** When data
is imported — chat histories from the agent folders, and the artifacts agents generate —
Personal Context takes ownership, and the **original copy is removed from its source
location** so the data lives in exactly one place.

This is configurable: a user may disable deletion and keep both copies. But the
single-source-of-truth behavior — import, then delete the original — is the intended
default and is the owner's own setting. **One canonical copy, no duplicate storage.**

## Scenarios

### 1. Personal device — persistence OK, but storage-aware

A device the user owns, where it is fine for data to persist locally. But the user still
cares about managing storage, because many devices don't have much of it. So they want
**local copies of the things agents may want to peruse**, while being able to
**offload, not sync, or download data on demand** when it isn't currently useful. The
defining need is *selective local materialization* — keep what's useful nearby, evict
the rest, and re-fetch when needed.

### 2. Work / managed computer — no data at rest

A work computer where the user wants an agent (or themselves) to have full
**programmatic access to all of the information**, but does **not** want any of it to
**live on the device at all**. Access is required; local persistence is not.

### 3. Ephemeral cloud / sandbox instance

A temporary instance the user spins up to build something. From here they may want their
**new chats to flow into Personal Context**, and they will **definitely** want
**access to their full historical body of knowledge**. Nothing necessarily survives
teardown, so the instance acts as a transient read/write client against a durable store
that lives elsewhere.

### 4. Fully local / privacy-first — no external communication

A privacy-minded user who wants **100% of everything on their own device** — literally
**no external communication or network activity** involving their chats or data. Here
cloud and remote services are not merely optional; they are forbidden.

## What the scenarios vary along

The same product has to satisfy all of these, so it helps to name the dimensions they
pull on:

| Scenario | Local persistence | Local footprint | Network / cloud |
|---|---|---|---|
| 1. Personal, storage-aware | Allowed | **Selective** — offload + fetch on demand | Allowed |
| 2. Work, no data at rest | **Not allowed** | None | Required (remote access) |
| 3. Ephemeral sandbox | Transient only | None after teardown | Required |
| 4. Fully local / private | **Required** | **Full** (everything) | **Forbidden** |

**Reading the full historical body of knowledge is needed in every scenario** —
perusing locally (#1), all information programmatically (#2), the historical body (#3),
and everything (#4). That capability is constant.

**Capturing new data into Personal Context is a core behavior** wherever the user is
working, because Personal Context is the single source of truth for what it contains.
What varies is only how each environment persists that capture — locally, remotely, or
transiently.

## The core tension

Two things fall out of the table, and together they are the constraint the architecture
has to resolve:

- **Constant:** every environment must be able to read the full historical body of
  knowledge (and at least some must capture new data).
- **Variable:** what is allowed to persist locally and what is allowed to cross the
  network is exactly what changes between environments — and scenarios #2/#3 (nothing
  may live locally) are the direct opposite of scenario #4 (nothing may leave the
  device).

A single design has to span "nothing local, all remote" and "everything local, nothing
remote" without becoming two different products.
