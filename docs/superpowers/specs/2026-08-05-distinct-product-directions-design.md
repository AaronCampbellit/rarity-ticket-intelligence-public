# Distinct Product Directions and Preview Dataset

**Status:** Superseded by `2026-08-05-signal-console-consolidation-design.md`
**Date:** 2026-08-05

## Goal

Make Signal Console and Atlas Workspace meaningfully different operating
models, not two color treatments on the same shell. Both directions must
support the complete protected route manifest, the same permissions, the same
record actions, the same client scope, and the same synthetic preview workload.
Choosing either direction must not leave a second migration pass.

The product remains one role-aware application. Switching responsibilities
changes available work and navigation, not the product identity.

## Chosen architecture

Use a shared domain and divergent composition architecture.

- Shared: API clients, permissions, route manifest, data models, form controls,
  status language, accessibility behavior, responsive breakpoints, workspace
  state, command registry, and design tokens.
- Direction-specific: shell geometry, navigation disclosure, page templates,
  worklist presentation, record inspection, AI placement, preview behavior,
  elevation, motion, and density defaults.
- Page features declare their family and capabilities. Direction-aware
  templates decide how those features are composed without duplicating business
  logic.

This provides stronger differentiation than CSS-only variants without creating
two independent products or duplicated route implementations.

### Alternatives rejected

1. **CSS-only divergence.** Fastest, but it preserves the same information
   hierarchy and recreates the current “just colors” problem.
2. **Separate page implementations for every direction.** Maximum visual
   freedom, but doubles maintenance, raises parity risk, and makes complete
   route migration harder to verify.

## Signal Console

Signal is a keyboard-first operational console for people processing high
volumes of work.

### Shell geometry

- A persistent, compact navigation column groups role-aware modules.
- A narrow context rail presents saved queues, views, filters, and recent
  records when the active route supports them.
- The center work surface favors dense tables and compact Kanban lanes.
- A persistent inspector occupies the right side when a record is selected.
  Selection does not navigate away from the queue.
- Workspace tabs remain compact and subordinate to the active queue.
- AI opens as a narrow dock tied to the inspector and does not cover the active
  work surface.

### Interaction model

- Single-click selects and previews; explicit open promotes a record to a tab.
- Sticky action strips keep triage, assignment, status, time, notes, and
  approvals available.
- Keyboard shortcuts cover global search, command palette, next/previous
  record, assign, status, note, and time entry.
- Tables use 36–40 pixel rows by default with resizable columns and visible
  selected-row state.
- Motion is restrained and functional. State changes use short fades, selection
  bars, and inline expansion rather than floating movement.

### Visual character

Graphite surfaces, crisp separators, low-radius controls, compact typography,
and shallow shadows. Depth communicates pinned regions and active inspection,
not decoration.

## Atlas Workspace

Atlas is a spatial workspace for people coordinating several streams of work
and maintaining context across them.

### Shell geometry

- A compact icon rail expands on demand to expose role-aware module names and
  subnavigation.
- Prominent horizontal workspace tabs organize open tasks and records.
- The center uses a larger, modular canvas with boards, grouped cards, timelines,
  and comfortable tables.
- Record previews open in floating side drawers over the canvas and can be
  pinned as full workspace tabs.
- AI is a full-height sidecar that can remain open beside the canvas.

### Interaction model

- Boards and grouped cards are primary on worklist-capable pages; tables remain
  one click away.
- Drag and drop changes supported workflow state with an undo affordance and
  explicit failure recovery.
- Drawers preserve the canvas position, filters, and selected view.
- Workspace tabs are a primary multitasking affordance with stronger dirty,
  pinned, and active states.
- Motion reinforces spatial relationships: drawers slide from their owning
  edge, cards settle into lanes, and tab activation uses a subtle depth change.

### Visual character

Cool charcoal surfaces, more breathing room, stronger surface separation,
larger radii, and deeper but controlled shadows. Depth distinguishes canvas,
cards, drawers, and the AI sidecar.

## Page-family migration

Every protected route is rendered through one of the following shared contracts.

| Page family | Signal Console | Atlas Workspace |
| --- | --- | --- |
| Home | Live queue plus inspector and compact operational summaries | Modular operational canvas with movable emphasis and board summaries |
| Worklist | Dense table or compact lanes with persistent selection inspector | Board/grouped cards by default with table view available |
| Record | Split editor with sticky actions and contextual history | Full workspace tab with section canvas and supporting drawers |
| Settings | Compact two-column navigation and form workspace | Section cards with progressive disclosure and guided flows |
| Operations | High-density event table, health strip, and investigation inspector | Status canvas, incident groupings, and drill-in drawers |
| Catalog | Direction-aware examples for every primitive and template | Direction-aware examples for every primitive and template |

Public login and recovery routes share the brand and component foundations but
do not expose authenticated shell chrome.

## Preview dataset

The local preview uses one clearly labeled, deterministic synthetic tenant:
**Northstar Managed Services**. Production and normal development behavior never
fall back to this dataset.

### Data boundary

- Preview data is enabled only by an explicit local/demo environment setting.
- A visible “Synthetic preview data” indicator appears in the shell.
- Failed live requests continue to display error states. They never substitute
  fixtures.
- The dataset uses stable IDs and dates relative to one fixed preview clock so
  screenshots and tests remain repeatable.
- No real customer names, domains, credentials, messages, or identifiers appear.

### Dataset coverage

- Four clients spanning legal, manufacturing, healthcare, and nonprofit work.
- Twelve technicians and administrators with varied roles, skills, capacity,
  and presence.
- At least thirty work records across priority, SLA, status, assignment, source,
  and age.
- Notes, time entries, approval requests, attachments, related records, and
  knowledge suggestions.
- Sales prospects, opportunities, proposals, projects, phases, tasks, capacity,
  change orders, and billing review.
- Integration health, reconciliation candidates, automation runs, service keys,
  webhooks, audit events, sessions, directory records, client resources, and
  platform settings.
- AI examples remain review-only and visibly synthetic.

The same records and counts are shown in both directions. Any visible difference
therefore comes from workflow and composition rather than different content.

## Interaction and data flow

Feature pages continue to own loading and mutation behavior. They expose
direction-neutral view models and commands to page-family templates.

1. Route and permissions resolve the available module and actions.
2. The active design direction selects the shell and page-family template.
3. Live or explicitly enabled preview repositories produce the same typed view
   models.
4. User actions invoke the same command handlers in either direction.
5. Workspace state retains tabs, previews, dirty state, active client, filters,
   and AI context while the user multitasks.

Direction changes preserve the active route, open workspace items, client
scope, and unsaved-change protection.

## Error handling

- Loading, empty, permission-denied, validation, conflict, offline, and server
  error states use shared semantics but direction-specific placement.
- Drag-and-drop mutations are optimistic only when the command supports a safe
  rollback. Failure restores the item and explains the reason.
- Preview mode is explicit. Missing fixtures fail visibly during development
  instead of silently showing partial data.
- Unsupported mobile routes retain the existing handoff screen.

## Mobile

Mobile remains intentionally limited to field essentials: triage, ticket
updates, notes, time entry, approvals, search, and AI chat.

- Signal mobile becomes a compact queue-to-record flow with a bottom action bar.
- Atlas mobile becomes a card stack with full-screen record sheets.
- Administration, complex settings, sales conversion, and delivery planning
  remain desktop-only.

## Verification

- Contract tests enumerate every route and render it in both directions.
- Shell tests verify different landmark geometry, navigation disclosure,
  preview placement, AI placement, and density behavior.
- Preview-data tests prove explicit opt-in, stable fixtures, complete route
  coverage, and no fixture fallback after API failure.
- Interaction tests cover table/board switching, selection, drawer promotion,
  workspace tabs, dirty close protection, drag-and-drop rollback, and AI context.
- Accessibility checks cover keyboard access, focus return, landmark uniqueness,
  dialog semantics, status text, and non-color state indicators.
- Production build and the complete frontend test suite must pass.

## Completion criteria

- The two directions are recognizable from grayscale screenshots based on
  layout alone.
- Every protected route uses a migrated direction-aware shell and page-family
  template.
- No user-facing raw JSON editor or raw JSON response is present.
- The synthetic workload makes primary, empty, warning, error, and overloaded
  states inspectable without connecting real systems.
- Switching direction never loses route, client, tabs, preview context, or dirty
  state.
- Production behavior contains no implicit demo-data fallback.
