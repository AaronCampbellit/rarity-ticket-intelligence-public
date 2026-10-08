# Product Experience Overhaul Design

**Status:** Approved for implementation on 2026-08-05

## Goal

Turn Rarity into an all-day operational workspace for technicians and
administrators. The product must feel focused, responsive, layered, and
pleasant without sacrificing the density required for service delivery.

The implementation provides two complete dark-mode visual directions in one
build. An admin-only preference switches between them without changing routes,
data, permissions, or component APIs.

## Approved Product Decisions

- Both directions use the same shell, route manifest, components, state, and
  workflows.
- No user-facing surface exposes raw JSON. APIs and storage may continue to use
  JSON internally.
- AI assist is a persistent, collapsible right rail that follows the user and
  can reference the active client, record, or workspace.
- Open records use resumable workspace tabs. Quick previews can be promoted
  into tabs without losing work.
- Density defaults adapt by workspace and can be overridden globally.
- Technicians and administrators use one role-aware shell.
- A role-aware Home workspace becomes the default authenticated landing page.
- Phones support essential field work: triage, ticket updates, notes, time
  entry, approvals, search, and AI chat. Full functionality begins at tablet
  and laptop widths.
- Rarity red remains the primary brand and action color in both directions.
- Navigation groups, labels, and technical setup flows may be improved while
  preserving capabilities, permission boundaries, and recognized deep links.

## Design Directions

### Signal Console

Signal Console is the recommended eventual default. It is a modern
service-management console with compact geometry, crisp separation, restrained
elevation, and fast-scanning work views.

- Layered graphite surfaces with subtle neutral and red tints.
- A persistent text-and-icon navigation rail with compact grouped navigation.
- Table-first operational pages with Kanban, list, calendar, or timeline view
  switches where the underlying work benefits from them.
- Precise dividers, compact control heights, tabular metadata, and restrained
  shadows.
- Side previews feel attached to the selected row and keep the worklist
  visible.
- Motion is short and functional: row selection, drawer entry, menu expansion,
  status transition, and tab restoration.

### Atlas Workspace

Atlas Workspace is a more spatial command environment. It keeps the same
capabilities but uses stronger canvas depth, softer separation, and more
prominent multi-pane work.

- A slimmer collapsible navigation rail and more open work canvas.
- Charcoal surfaces with cool supporting tints and soft red illumination for
  focus and priority.
- Kanban, timelines, and split panes receive more visual prominence while dense
  tables remain available.
- Floating overlays, preview panes, and menus use stronger elevation and
  smoother spatial motion.
- Workspace tabs read as resumable tasks rather than browser chrome.

Both directions use the same Rarity-red action hierarchy, semantic statuses,
typography scale, accessibility rules, and interaction anatomy. Differences
are expressed through semantic direction tokens and component variants, never
feature-local forks.

## Application Architecture

### Route manifest

One typed route manifest replaces route knowledge spread across `App.tsx`,
`navigation.ts`, and feature conditionals. Each entry declares:

- stable route ID and recognized legacy hash
- human label, navigation group, and icon
- required capability and intended roles
- page family and default density
- supported views
- preview and workspace-tab behavior
- mobile availability

The manifest covers Home plus all current destinations:

- Work
- Sales
- Proposals
- Conversion
- Projects
- AI assist and AI settings
- Teams settings
- Sessions
- Local administrators
- Roles
- Audit
- Service desk settings
- Setup center
- Operations
- Automation
- Service API keys
- Webhooks
- Datto reconciliation
- Datto connections
- Forwarding intake
- Graph mailboxes
- Knowledge
- Billing review
- Clients and directory
- Client resources
- Sales pipelines
- Prospects
- Design system
- Login, recovery access, loading, setup-required, unauthorized, and global
  error states

Existing hashes keep resolving. Updated grouping and labels do not break saved
links.

### Shell regions

The authenticated shell has six stable regions:

1. **Navigation rail** — role-aware product areas, favorites, recent records,
   and a consolidated administration tree.
2. **Command bar** — global search, create, client scope, notifications, user
   menu, density, and direction preferences.
3. **Workspace tabs** — open records and tasks with dirty, pending, failed, and
   completed indicators.
4. **Page canvas** — the active Home, worklist, record, dashboard, settings, or
   system-state template.
5. **Preview layer** — a non-destructive side drawer for selected rows, cards,
   events, resources, and audit entries.
6. **AI rail** — persistent assistant history and composer with explicit
   current-context attachments.

The shell owns layout, persistence, keyboard behavior, responsive collapse,
and focus restoration. Feature pages own business data and commands.

### State and persistence

- Direction, density, rail sizes, navigation disclosure state, and non-sensitive
  workspace tabs persist locally per user.
- Unsaved values remain in component state and receive a close warning.
- Sensitive values, issued secrets, generated tokens, and server data are not
  persisted in browser storage.
- The active client and record are explicit context objects shared with search,
  previews, tabs, and AI.
- Browser history represents navigation; opening or closing previews does not
  destroy the underlying page state.

## Information Architecture

Top-level product areas are:

- Home
- Work
- Sales
- Delivery
- Clients
- Knowledge
- Operations
- Admin

Admin is subdivided into Access, Platform, Service configuration, Integrations,
Automation, and AI. Permission filtering removes unavailable destinations while
keeping group structure predictable.

Global search returns people, clients, tickets, opportunities, proposals,
projects, knowledge, and settings. Results support keyboard selection, quick
preview, and open-in-tab.

Home is role-aware:

- Technicians see assigned work, SLA risk, schedule, queue health, approvals,
  recent records, and resumable tasks.
- Administrators see platform health, integration incidents, failed jobs,
  approvals, security events, and configuration tasks.
- Mixed-role users see a unified prioritized feed with saved views instead of a
  different shell.

## Page Families

### Worklists

Work, Sales, Proposals, Projects, Prospects, Knowledge, Clients, Audit,
Sessions, keys, and reconciliation use a shared worklist template:

- title, scope, live result count, saved view, and one primary action
- compact filter and sort controls with removable filter chips
- table, list, Kanban, calendar, or timeline switch when meaningful
- resizable columns where appropriate
- bulk selection with a consequence preview
- row/card selection opening the side preview
- promotion from preview to a resumable workspace tab
- useful loading skeleton, empty guidance, stale-data notice, and retry state

Kanban is a real view over the same records and filters. Dragging between
columns previews the state transition, permission result, and any required
reason before commit.

### Record workspaces

Tickets, opportunities, proposals, projects, change orders, knowledge articles,
clients, and resources use a common record template:

- compact identity header and state controls
- primary activity or work surface
- contextual facts, relationships, SLA, commercial data, and audit evidence in
  a resizable secondary pane
- anchored composer for replies, notes, time, decisions, or edits
- visible save/pending/conflict status
- related records open in previews or tabs

### Settings and setup

Settings use a local navigation rail, readable form sections, sticky save
actions, inline validation, and clear connection/test evidence. Advanced
options use disclosures or a guided builder rather than raw payload fields.

Setup center becomes a guided checklist with readiness, dependencies, test
results, and recovery guidance. It uses structured fields for intake, storage,
backup, networks, providers, capabilities, scopes, and policies.

### Dashboards and operations

Home, Operations, billing, capacity, forecast, and integration health use open
bands, tables, timelines, and charts. Cards are reserved for independently
actionable summaries. Dashboards always provide a route into the underlying
records and preserve active filters.

## No-Raw-JSON Contract

Raw JSON must never be displayed, requested, edited, copied, or used as helper
copy in the product UI.

Known current exposures are replaced as follows:

- Automation steps become a drag-and-drop workflow builder with triggers,
  conditions, branches, actions, retry policy, validation, and a readable
  Mermaid-style flow preview.
- Setup intake, object-storage, backup, and recovery configuration become typed
  field groups and connection wizards.
- Service-key capabilities and data scopes become searchable multi-select
  permission builders with grouped plain-language descriptions.
- Datto reconciliation values become labeled key/value facts, differences, and
  human-readable mapping tables.
- Policy, webhook, routing, and provider configuration use structured builders,
  condition rows, tables, and plain text.
- Knowledge and audit evidence render as formatted prose, facts, and timelines.

Developer tools and API transport may use JSON internally. User-visible labels
must say configuration, steps, fields, mapping, request details, or evidence as
appropriate rather than JSON.

## Design-System Expansion

### Foundations

Add semantic tokens for:

- direction-aware canvas, navigation, panel, raised, inset, overlay, selected,
  hover, and scrim surfaces
- red brand/action states and semantic info/success/warning/danger states
- inner, raised, floating, and modal elevation
- compact, standard, and touch density
- navigation, content, preview, and AI rail widths
- typography, tabular metadata, focus, motion, blur, and z-index layers

No feature CSS may choose direction-specific colors or shadows directly.

### Components

Complete or add shared implementations for:

- AppShell, CommandBar, ProductNav, WorkspaceTabs, ResizablePane, PreviewDrawer,
  AIRail, CommandPalette, GlobalSearch, GlobalCreate
- PageHeader, Breadcrumbs, LocalNav, PageTabs, ViewSwitcher, SavedViews
- Button, IconButton, SplitButton, Menu, Combobox, MultiSelect, DatePicker,
  DateRangePicker, TimeInput, SearchInput, TextInput, Textarea, RichComposer,
  FileUpload, Switch, Checkbox, RadioGroup, SegmentedControl
- DataTable, Worklist, KanbanBoard, Timeline, ActivityFeed, KeyValueList,
  DiffView, Pagination, FilterBuilder
- Dialog, Popover, Tooltip, Drawer, Toast, InlineNotice, Skeleton, StatePanel,
  ValidationSummary
- WorkflowBuilder, ConditionBuilder, ScopeBuilder, ConnectionWizard, Mermaid
  Preview

All components include keyboard, focus, hover, active, selected, pending,
disabled, validation, error, and reduced-motion behavior in the design-system
catalog.

## Interaction Rules

- Menus and dropdowns use deliberate placement, selection markers, search when
  lists are long, keyboard navigation, and focus return.
- Date controls support typing, calendar selection, ranges, clear affordances,
  locale-aware display, and useful validation.
- Text inputs use visible labels, quiet filled surfaces, strong focus, inline
  help, error association, and appropriate autocomplete.
- Side previews preserve work context and can be pinned, resized, closed, or
  promoted to a tab.
- AI never silently changes a record. Proposed changes show scope and require
  an explicit user action.
- Drag-and-drop always has keyboard controls and a non-drag alternative.
- Motion communicates spatial continuity and never blocks rapid work.

## Error and Conflict Handling

- Loading, empty, permission-denied, offline, stale, partial, and server-error
  states are first-class page or region variants.
- Failed inline actions preserve user input and appear next to the initiating
  control.
- Version conflicts compare current and submitted values in readable form and
  offer reload, copy, or reasoned override where permitted.
- Long-running work remains visible in the relevant tab and a global jobs
  surface.
- Toasts acknowledge completion but never contain the only evidence or recovery
  action.
- The application boundary retains a branded recovery screen and safe retry.

## Responsive Behavior

- Wide desktop supports navigation, page, preview, and AI simultaneously.
- Laptop collapses either preview or AI on demand and preserves both states.
- Tablet uses overlay navigation and one secondary rail at a time.
- Phone provides Home, search, queue triage, ticket detail, notes, time entry,
  approvals, and AI. Secondary context becomes a full-height sheet.
- Touch targets remain at least 44 CSS pixels even when the surrounding
  workspace uses compact density.
- Tables use purpose-built mobile lists rather than squeezed columns.

## Accessibility

- Meet WCAG 2.2 AA contrast, focus, target-size, naming, status, and error
  requirements.
- Every pointer workflow has a keyboard equivalent.
- Focus order follows visible pane order, and closing overlays restores focus.
- Status and priority never rely on color alone.
- Screen-reader announcements cover row selection, drag movement, tab state,
  async completion, validation, and AI generation.
- Forced-colors, 200% zoom, reduced motion, and browser text scaling are
  supported.

## Verification and Migration Contract

Migration is complete only when every route and nested state uses the new
system in both directions. A generated route/component coverage ledger records:

- route and page family
- Signal Console desktop screenshot
- Atlas Workspace desktop screenshot
- applicable tablet or phone screenshot
- loading, empty, error, permission, and populated states
- open menus, dialogs, date pickers, previews, workspace tabs, and AI rail
- keyboard path, focus behavior, and automated accessibility result
- confirmation that no raw JSON is visible

Automated checks include:

- design-token and public-component boundary tests
- route-manifest completeness against the `Page` union
- a source and rendered-text ban on user-visible raw JSON
- component interaction and accessibility tests
- page smoke tests for both directions
- build, unit, and browser tests
- horizontal-overflow and responsive viewport checks

Rendered QA covers at least 1440×1024, 1280×720, 834×1194, 390×844, 200% zoom,
reduced motion, and forced colors. Every current page, dialog, drawer, popover,
dropdown, picker, form, table, board, and state must appear in the ledger.

## Delivery Strategy

1. Establish the typed route manifest and coverage ledger.
2. Expand foundations and the catalog with both directions and interaction
   states.
3. Build the shell, Home, workspace tabs, previews, and persistent AI rail.
4. Replace all raw-JSON UI with structured builders.
5. Migrate page families, then every feature route.
6. Complete responsive and accessibility passes.
7. Run the route/state ledger in both directions and repair every uncovered or
   legacy surface before handoff.

The two directions ship together behind the admin-only switch. Choosing the
eventual default is a token preference change, not a migration project.
