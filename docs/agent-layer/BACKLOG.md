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

- Backlog 2026-03-05 a1b1c1: Cross-date drag-and-drop in web UI
    Priority: High. Area: web
    Description: Allow users to drag slides across date boundaries in the web UI, changing both `date` and `day_order`.
    Acceptance criteria: Slide can be dragged from one date group to another; date and day_order update correctly via API.
    Notes: MVP restricts to intra-day. Web UI is independent of CLI so cross-date moves will be needed.

- Backlog 2026-03-05 b2c2d2: Google Slides migration tool
    Priority: Medium. Area: cli
    Description: Convert existing Google Slides decks into git export folder format for import via `pc import`.
    Acceptance criteria: Speaker notes extracted to notes field. Slides exported as HTML. Chronological ordering preserved.
    Notes: Spec Section 7. Entry point is `pc import`.

- Backlog 2026-03-05 c3d3e3: Agent skill interface for CLI
    Priority: Medium. Area: cli
    Description: Dedicated skill/template system for CLI-based slide creation by agents.
    Acceptance criteria: Agents can discover available templates and create slides using structured skill invocations.

- Backlog 2026-03-05 d4e4f4: Agentic chat panel in web UI
    Priority: Low. Area: web
    Description: Embedded chat panel in the web UI for agent-assisted slide creation and editing.
    Acceptance criteria: User can converse with an agent to create/modify slides.

- Backlog 2026-03-05 f6a6b6: Multi-user auth with per-user S3 namespace
    Priority: Low. Area: infra
    Description: Authentication system with per-user data isolation (`s3://bucket/users/{user_id}/...`).
    Acceptance criteria: Multiple users can use the same deployment with isolated data.

- Backlog 2026-03-05 a7b7c7: Mobile-optimized web UI
    Priority: Low. Area: web
    Description: Responsive design optimized for mobile viewing and basic editing.
    Acceptance criteria: Slide viewer, notes, and basic editing work well on mobile.
