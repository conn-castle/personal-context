# Backlog

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Unscheduled user-visible features and tasks (distinct from issues; not refactors). Maintainability refactors belong in ISSUES.md.

## Format
- Insert new entries immediately below `<!-- ENTRIES START -->` (most recent first).
- Keep each entry **3–5 lines**.
- Line 1 starts with `- Backlog YYYY-MM-DD <id>:` and a short title.
- Lines 2–5 are indented by **4 spaces** and use `Key: Value`.
- Keep **exactly one blank line** between entries.
- Prevent duplicates: search the file and merge/rewrite instead of adding near-duplicates.
- When scheduled into ROADMAP.md, move the work into ROADMAP.md and remove it from this file.
- When implemented, remove the entry from this file.

### Entry template
```text
- Backlog YYYY-MM-DD abcdef: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <what the user should be able to do>
    Acceptance criteria: <clear condition to consider it done>
    Notes: <optional dependencies/constraints>
```

## Features and tasks (not scheduled)

<!-- ENTRIES START -->

- Backlog 2026-03-12 r6s6t6: Web UI trash view and restore flow
    Priority: High. Area: web
    Description: Add a visible way in the web UI to switch into a trash/deleted-slides view, browse soft-deleted slides, inspect them, and restore a slide back to the active deck.
    Acceptance criteria: User can open a trash view from the web UI, see only soft-deleted slides, select one to review its content/metadata, and restore it so it disappears from trash and returns to the active list after refresh/sync.
    Notes: Backend support already exists for deleted-slide listing and restore; the missing piece is the user-facing navigation/toggle and end-to-end trash workflow in the current UI.

- Backlog 2026-03-11 v3w4x5: GitHub backup/sync integration and Settings link
    Priority: Medium. Area: web, infra
    Description: Implement GitHub-based backup/sync (nightly export to a private data repo). The Settings overlay "GitHub Repository" link should point to the user's backup data repo, not the source code repo. May involve configuring the target repo, auth, and scheduling.
    Acceptance criteria: Nightly export pushes to a configured GitHub repo. Settings overlay links to the user's data backup repo. Setup wizard or config allows specifying the target repo.
    Notes: Nightly export workflow example exists at `docs/nightly-export-workflow.example.yml`. The current Settings "GitHub Repository" link is a placeholder pointing to the source repo.

- Backlog 2026-03-11 s1t2u3: Rod fallback for `pc screenshot` when system Chrome is not available
    Priority: Low. Area: cli
    Description: `pc screenshot` currently requires system Chrome/Chromium on PATH. Add Rod (Go headless Chrome library) as an automatic fallback that downloads Chromium on first use (~150MB) when no system browser is detected.
    Acceptance criteria: `pc screenshot <id>` works on machines without Chrome installed, with a one-time auto-download and clear progress indicator.
    Notes: Rod downloads Chromium to a cache directory. The existing `PC_CHROME_PATH` env var override should continue to take priority.

- Backlog 2026-03-10 m2n2o2: Markdown-authored slides with reliable rendering
    Priority: Medium. Area: slide-format
    Description: Allow slides to be authored in Markdown instead of only raw HTML, or support a hybrid Markdown-plus-HTML model that still renders cleanly in previews, exports, and the web UI.
    Acceptance criteria: Users can create and edit slides using the chosen Markdown or hybrid format; rendered output remains visually correct and consistent anywhere slides are displayed.
    Notes: Decide whether Markdown is canonical source with HTML as a derived artifact, or whether a constrained hybrid format is the long-term source of truth.

- Backlog 2026-03-10 h1i1j1: Web UI slide creation via + button in header
    Priority: High. Area: web
    Description: Allow users to create a new slide directly from the web UI via a + icon in the top header bar. Requires decisions about: what template to use, whether to prompt for project/date, how to set initial html_content, and whether this needs a new POST /api/slides endpoint (currently CLI-only).
    Acceptance criteria: User can click + in the header, fill minimal fields, and a new slide appears in the navigation panel. Slide persists to Neon and syncs via existing sync mechanism.
    Notes: Currently no slide creation endpoint exists (CLI only per CONTEXT.md). Needs a new POST /api/slides route, template selection UX, and decisions around default content generation.

- Backlog 2026-03-06 g8h8i8: PDF export for slide ranges with optional title page
    Priority: Medium. Area: web
    Description: Allow exporting only a selected slide range to PDF and optionally prepend a title page during share/export flows.
    Acceptance criteria: User can choose start/end slide; PDF contains only selected slides; optional title page toggle adds a first page with presentation title and metadata.
    Notes: Clarify whether title-page fields are editable and shared between export and share workflows.

- Backlog 2026-03-06 j9k9l9: Expiring share links for slide ranges
    Priority: Medium. Area: web
    Description: Create shareable links scoped to a selected slide range with an explicit expiration date.
    Acceptance criteria: Link grants access only to the selected range until expiration; expired links return a clear access-denied response.
    Notes: Backend token payload should include range bounds and expiration timestamp in UTC.

- Backlog 2026-03-05 e5f5g5: Tombstone table for durable hard-delete propagation
    Priority: Medium. Area: sync
    Description: Add `gc_tombstones(slide_id, deleted_at)` table. `pc gc` inserts a tombstone before hard-deleting each row. Sync and web UI check tombstones to learn about hard deletes. Periodic cleanup of old tombstones (e.g., 90 days).
    Acceptance criteria: Hard-deleted slides are removed from all machines and web UI without full reconciliation. Tombstones cleaned up after retention period.
    Notes: Currently mitigated by full reconciliation on version-bump-with-no-changes. See Decision d1e2f3.

- Backlog 2026-03-05 a1b1c1: Web UI slide reordering and moves
    Priority: High. Area: web
    Description: Add a visible way in the web UI to move slides around, including reordering within a date group and moving a slide to a different date/group.
    Acceptance criteria: User can move a slide earlier/later in the current group and into another date from the web UI; `date` and `day_order` update correctly and the new order persists after refresh/sync.
    Notes: Backend reorder support already exists; the missing piece is user-facing move/reorder controls in the current UI, whether drag-and-drop, explicit move actions, or both.

- Backlog 2026-03-05 b2c2d2: Google Slides migration tool
    Priority: Medium. Area: cli
    Description: Convert existing Google Slides decks into git export folder format for import via `pc import`.
    Acceptance criteria: Speaker notes extracted to notes field. Slides exported as HTML. Chronological ordering preserved.
    Notes: Spec Section 7. Entry point is `pc import`.

- Backlog 2026-03-05 c3d3e3: Agent skill interface for CLI
    Priority: Medium. Area: cli
    Description: Dedicated skill/template system for CLI-based slide creation by agents.
    Acceptance criteria: Agents can discover available templates and create slides using structured skill invocations.

- Backlog 2026-03-05 d4e4f4: Agentic `pc` interface in web UI
    Priority: Medium. Area: web
    Description: Add an embedded agent interface in the web UI that lets users interact with their slide library the way an agent would via `pc`, including command-like creation, editing, search, movement, delete/restore, and related workflows.
    Acceptance criteria: User can open the UI agent panel, issue natural-language or command-oriented requests, and see the agent read or mutate slides through the same underlying capabilities exposed by `pc`, with visible results and clear confirmations/errors.
    Notes: Reuse the existing `pc`/agent capability surface as the source of truth rather than inventing a separate browser-only action model.

- Backlog 2026-03-05 f6a6b6: Multi-user auth with per-user S3 namespace
    Priority: Low. Area: infra
    Description: Authentication system with per-user data isolation (`s3://bucket/users/{user_id}/...`).
    Acceptance criteria: Multiple users can use the same deployment with isolated data.

- Backlog 2026-03-05 a7b7c7: Mobile-optimized web UI
    Priority: Low. Area: web
    Description: Responsive design optimized for mobile viewing and basic editing.
    Acceptance criteria: Slide viewer, notes, and basic editing work well on mobile.
