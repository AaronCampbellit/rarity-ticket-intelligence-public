# Signal Console Consolidation

**Status:** Approved design, implementation specification
**Date:** 2026-08-05

## Goal

Consolidate Rarity on the Signal Console operating model without carrying
forward the prototype's navigation, tab, preview, or demo-data defects. The
result must preserve work in progress across normal product pages, reserve the
top tab bar for individual tickets, tasks, and projects, and verify every
protected route before the prototype-only Atlas and synthetic-preview artifacts
are removed.

Rarity remains one dark, role-aware application. Changing responsibilities
changes available content and actions, never the shell or product identity.

## Chosen architecture

Evolve the existing workspace reducer into two explicit state domains:

1. **Page history** retains mounted route surfaces such as Home, Work, Sales,
   Proposals, settings, and administration. These pages are activated through
   navigation and never appear in the top record-tab bar.
2. **Record workspace** owns previews and full record tabs for tickets, tasks,
   and projects. It remains responsible for authorization-safe restoration,
   dirty-state protection, activation, closing, and AI context.

This fixes the current modeling defect at its source: route navigation and
record multitasking are different interactions and must not share one tab
collection.

### Alternatives rejected

1. **Hide route tabs with CSS.** This would preserve the reducer coupling and
   continue producing incorrect activation, restoration, and close behavior.
2. **Replace the router and workspace stack.** A full rewrite would increase
   regression risk across the protected route manifest without improving the
   requested interaction model.

## Signal shell

Signal is the only production design direction after this migration.

### Left navigation

- Keep the grouped, role-aware Signal navigation and current Admin disclosure.
- Remove the Console Queue Context sidebar and its hard-coded counts, recent
  records, links, markup, tests, and styles.
- Add one desktop collapse control to the grouped navigation.
- Expanded mode shows the full brand, group labels, link labels, and build
  label. Collapsed mode shows the compact brand and route icons with accessible
  names and native tooltips.
- Persist the expanded or collapsed state per principal in local storage.
- Desktop collapse must not alter the mobile behavior. On narrow screens the
  navigation remains an overlay with focus restoration and Escape dismissal.

### Top bar

- Keep Create as the primary global action.
- Place Ask Rarity directly beside Create in the top command bar.
- Ask Rarity opens a non-blocking dock without navigating away, changing the
  active page, closing a preview, or losing a draft.
- Search, client selection, presentation density, and sign-out remain in the
  command bar according to permission and screen size.
- Remove the design-direction selector. Density remains a supported preference.

## Page history

Normal product routes retain their local UI state when the user navigates away
and returns during the same authenticated session.

- Page history is internal and has no visible top tabs.
- Returning through the left navigation, browser history, or command palette
  restores the mounted page, including form drafts, filters, selected view,
  pagination, scroll ownership, and expanded sections.
- Cache at most eight clean route surfaces. Evict the least recently activated
  clean route when the limit is exceeded.
- A route with registered dirty state is never evicted. Closing the session
  retains the existing sign-out cleanup behavior.
- A route that becomes unauthorized is removed immediately.
- Page history is in-memory only. Record tabs retain their current
  authorization-filtered session restoration.

The cache limit prevents a technician or administrator who works all day from
mounting every route indefinitely while preserving the workspaces they revisit
most often.

## Record previews and full tabs

The top tab bar is exclusively for individual records with one of these entity
types:

- `ticket`
- `task`
- `project`

Route-level items are invalid record tabs and are rejected by reducer,
serialization, restoration, and rendering boundaries.

### Preview behavior

- A single click on a ticket, task, or project opens or updates the right-side
  preview.
- The preview header has one primary **Open** action and one **Close preview**
  action.
- Remove Pin, pin state, pin icons, and duplicate promotion behavior.
- Open closes the preview, opens or activates the corresponding full record tab,
  navigates to its owning route, restores its client scope when authorized, and
  renders the larger record form.
- Opening a record that already has a tab activates the existing tab without
  duplication.

### Direct-open behavior

- Double-clicking a ticket from Home or Work opens it directly in the full
  record workspace.
- Double-clicking a ticket card in the Work Kanban does the same.
- Project and task rows or cards use the same single-click preview and
  double-click full-open contract.
- Keyboard users receive an explicit **Open full record** action; double-click
  is never the only route to full record access.
- A double-click may momentarily select the record but must finish with the
  preview closed and the full tab active.

### Record tabs

- Tabs show the record identifier or concise project/task label, dirty
  indicator, and close action.
- Tabs can be activated, keyboard-reordered, and closed with the existing
  unsaved-change confirmation.
- Closing the active tab returns to the owning route surface, preserving that
  route's page history.
- AI context follows the preview when present, otherwise the active record tab,
  otherwise the active route and client.

## Full record surfaces

Ticket, task, and project tabs render the larger form factor inside their owning
route:

- Ticket: full triage, workflow, priority, assignment, notes, time, attachments,
  and AI-aware context.
- Task: full task status, ownership, schedule, notes, time, and parent project
  context.
- Project: project summary, phases, capacity, financial authorization, tasks,
  change orders, and activity.

Opening a full record uses the existing route and data APIs. It does not create
parallel record routes or duplicate business logic.

## Optional Kanban views

Signal keeps its efficient list defaults and adopts the useful Atlas boards as
optional views.

| Workspace | Default | Optional board grouping |
| --- | --- | --- |
| Work | List | Ticket workflow status |
| Sales | List | Opportunity pipeline stage |
| Projects | List | Project delivery state |

- Use the shared `ViewSwitcher` and `KanbanBoard` contracts.
- Persist the selected view per principal and route in local storage.
- List and Kanban consume the same filtered record collection and selection
  state.
- Single-click preview, double-click full-open, keyboard full-open, loading,
  empty, permission, and error behavior are identical in both views.
- Board movement is enabled only where an existing authorized mutation supports
  the target transition. A failed move restores the prior lane and presents a
  user-facing error.
- Proposals remain a stateful list/editor workflow. They are normal page history,
  not record tabs and not a Kanban surface in this migration.

## Demo and prototype artifact removal

After the live Signal behavior and route verification are green, remove:

- the Atlas shell, Atlas page composition, Atlas-only CSS, and Atlas tests;
- the design-direction preference and selector;
- the synthetic preview provider, catalog, indicator, styles, and tests;
- the `VITE_RARITY_SYNTHETIC_PREVIEW` Docker build argument, Compose argument,
  local environment setting, and configuration test;
- direction-specific conditionals that no longer have a production branch;
- hard-coded queue context and other prototype-only copy or counts.

Ordinary deterministic fixtures inside automated tests remain. Historical
design documents remain as decision records and the prior two-direction
specification is marked superseded by this specification.

Production and normal development builds must contain no demo tenant, fake
customer, fake ticket, or synthetic-preview feature switch.

## Data flow and state boundaries

1. Authentication resolves the principal, capabilities, authorized routes, and
   authorized clients.
2. Navigation activates one route surface in page history.
3. A supported record selection dispatches `openPreview` with its entity type,
   route, record ID, client ID, and label.
4. Open or direct-open dispatches `openRecord`, which deduplicates the tab,
   closes the preview, and requests navigation to the owning route and record.
5. The owning feature loads the full record through its existing authorized
   API and renders its full form.
6. Dirty sources register against either a route-history owner or record-tab
   owner and protect that owner from destructive close or eviction.
7. Ask Rarity derives context from preview, active record, or active route in
   that order without owning navigation state.

The reducer remains pure. Browser storage, navigation, focus restoration, and
API requests remain in controller and feature layers.

## Error and edge handling

- Failed full-record loads keep the record tab open and show a retryable error
  in the record surface.
- Unauthorized or removed clients purge their restored record tabs and
  previews.
- Restored data from an older workspace schema is rejected rather than
  guessing an entity type.
- Duplicate Open actions activate the existing tab.
- Closing a preview restores focus to the record that opened it when that
  trigger remains mounted.
- Closing a dirty record requires confirmation. Page-history navigation does
  not prompt because it does not unmount or discard the page.
- Empty and error states remain authored product UI; raw API responses and raw
  JSON never appear.

## Route-wide migration and verification

Every protected route in `routeManifest` must pass the Signal shell contract.
Verification is manifest-driven so newly added routes cannot silently miss the
migration.

For each authorized protected route, verify:

- exactly one active main landmark and one route surface;
- grouped navigation, active route, top bar, and responsive navigation;
- no Console Queue Context, Atlas rail, design-direction control, demo
  indicator, synthetic customer data, or user-facing raw JSON;
- route navigation does not create a top record tab;
- route state survives navigation away and back;
- loading, empty, populated, validation, permission, and error states use the
  design-system contracts applicable to that route family;
- keyboard access and automated WCAG A/AA checks remain green.

Record interaction suites separately verify ticket, task, and project preview,
Open, direct-open, deduplication, dirty close, client authorization, restoration,
and AI context.

The final gate is:

1. focused reducer, shell, Work, Home, Sales, and Project tests;
2. manifest-wide route rendering and old-artifact assertions;
3. complete frontend test suite;
4. production TypeScript and Vite build;
5. Docker frontend build with no demo argument;
6. live and ready health endpoints;
7. visual inspection in a user-selected browser before claiming visual QA.

## Completion criteria

- Signal is the only shipped product direction.
- The left navigation is collapsible and the queue-context sidebar is gone.
- Top tabs contain only individual tickets, tasks, and projects.
- Normal pages preserve useful session history without visible route tabs.
- Preview Open and record double-click produce one deduplicated full record tab.
- Ask Rarity sits beside Create and never interrupts active work.
- Work, Sales, and Projects offer consistent optional Kanban views.
- Every protected route passes the Signal shell, interaction, accessibility,
  and no-old-artifact contracts.
- No demo workspace code, build flag, fake tenant data, Atlas implementation,
  or user-facing raw JSON remains.
