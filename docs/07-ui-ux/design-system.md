# Rarity Product Design System

**Status:** Implemented; rendered and assistive-technology acceptance pending

The package foundations, public component API, Rarity workflow patterns,
authenticated shell, development catalog, all 37 authenticated feature
surfaces, and source-boundary enforcement are implemented. The acceptance worksheet at
`design-system-acceptance.md` is authoritative for browser, zoom, touch,
VoiceOver, NVDA, and human visual evidence that cannot be claimed from source
or component tests.

## Purpose

Rarity uses one accessible product design system across its authenticated PSA
application. The system gives technicians, sales staff, project teams, and
administrators a predictable interaction language while preserving the
authority, auditability, and client isolation of server-side workflows.

The first release covers the authenticated product application. Documentation,
marketing pages, and external customer portals are outside this initial scope.

## Current-product audit

The August 1, 2026 source and rendered-product audit found:

| Surface | Current count |
| --- | ---: |
| Buttons | 124 |
| Forms | 66 |
| Inputs | 253 |
| Selects | 39 |
| Text areas | 31 |
| Expandable panes | 15 |
| Tables | 7 |
| Alert announcements | 38 |
| Status announcements | 56 |
| Disabled-control instances | 50 |
| Main product surfaces | 37 |

The product already has a recognizable visual language, semantic landmarks,
native controls, focus treatment, status announcements, and responsive layouts.
The principal weakness is duplication: actions, forms, panels, state messages,
tables, and responsive rules are implemented independently by feature.

The system will consolidate those patterns incrementally. Existing workflows
remain available while each feature family migrates.

## Design principles

1. **The next action is obvious.** Each page has at most one visually dominant
   primary action for the current task.
2. **State is never color-only.** Text, icons, and semantics carry meaning in
   addition to color.
3. **Consequences are visible before commitment.** Sensitive, destructive,
   irreversible, cross-client, or immutable actions show scope and impact.
4. **Complexity follows the work.** Common technician actions remain immediate;
   advanced configuration uses panes, tabs, and progressive disclosure.
5. **Server authority remains visible.** Validation, permissions, versions,
   scope, health, and retry rules are presented as product behavior rather than
   hidden implementation details.
6. **Keyboard and assistive-technology operation are first-class.** Every
   pointer interaction has a semantic, focusable alternative.
7. **Dense does not mean cramped.** Operational screens may be information
   dense, but spacing, grouping, typography, and alignment preserve scanning.
8. **The system is adoptable.** Components can coexist with current CSS while
   a feature migrates; a full application rewrite is not required.

## Architecture

The system has four layers:

```text
Foundations
  -> Primitives
    -> Product patterns
      -> Page templates
```

- **Foundations** define tokens, typography, iconography, motion, density,
  breakpoints, focus, and content rules.
- **Primitives** implement the smallest reusable controls and containers.
- **Product patterns** combine primitives into recurring Rarity workflows.
- **Page templates** provide consistent shells for worklists, records,
  settings, dashboards, and system states.

Feature code owns business data and workflow commands. Design-system components
own presentation, interaction behavior, accessibility semantics, and visual
states. Components do not fetch feature data or infer authorization.

## Foundations

### Semantic tokens

Components consume semantic tokens rather than feature-specific color names.
The initial token groups are:

- `surface`: canvas, raised, inset, selected, scrim
- `text`: primary, secondary, subtle, inverse, disabled, link
- `border`: default, strong, focused, disabled
- `action`: primary, primary-hover, secondary, subtle, danger
- `status`: neutral, info, success, warning, danger
- `focus`: ring, ring-offset
- `shadow`: raised, overlay

The Rarity red remains the brand and primary-action color. Red is not used for
routine status decoration when it could be confused with danger or failure.

Tokens must support light mode, future dark mode, forced colors, and print
without changing component APIs.

### Typography

The type system defines:

- display and page titles
- section and panel headings
- body and compact body
- labels and helper text
- record identifiers and metadata
- tabular numeric values
- code, JSON, and immutable evidence

Record identifiers, dates, money, duration, and versions receive stable
tabular treatment for fast comparison.

### Spacing and density

Use a shared spacing scale for control height, panel padding, grid gaps, and
page rhythm. Components support:

- `comfortable` density for forms, settings, and infrequent workflows
- `compact` density for worklists, tables, audit evidence, and operations

Density changes spacing, not target size or legibility.

### Icons

Use one maintained icon library. Icons supplement visible labels by default.
Icon-only actions require an accessible name and tooltip and are limited to
well-understood repeated actions such as close, reveal, overflow, and row
expansion.

Do not use text symbols, emoji, or handcrafted SVG approximations as product
icons.

### Motion

Motion communicates state change and spatial continuity. It never delays work.
All non-essential motion respects `prefers-reduced-motion`. Progress indicators
do not imply a completion estimate unless the server provides one.

### Responsive behavior

Shared breakpoints replace feature-local breakpoint drift. The system defines:

- wide desktop: persistent grouped navigation and multi-pane workspaces
- desktop: persistent navigation with adaptive content grids
- tablet: collapsible navigation and stacked secondary panes
- narrow: single-column task flow with drawers or full-screen detail panes

Tables provide horizontal containment or a purpose-built list alternative.
Important actions never disappear solely because the viewport is narrow.

## Action taxonomy

Every action uses an intent, emphasis, size, state, and optional icon.

### Action intents

| Intent | Use | Current Rarity examples |
| --- | --- | --- |
| Primary | Completes the page's current task | Save provider, Create project, Record time |
| Secondary | Useful alternative without primary emphasis | Test connection, Preview conversion, Refresh |
| Tertiary | Low-emphasis local action | Cancel, Close, Edit, View details |
| Danger | Destructive or access-removing action | Revoke key, Revoke session, Disable recovery account |
| Link | Navigates to another location or record | Open opportunity, View audit evidence |

### Action states

All action components support:

- default
- hover
- active
- focus-visible
- disabled with a discoverable reason when practical
- pending with stable width and progress wording
- success acknowledgement
- failed without losing entered values

### Action groups

The source inventory maps current actions into these reusable groups:

- **Create and add:** work, prospect, opportunity, proposal, commercial line,
  project, phase resource plan, task or subtask, Change Order, connection,
  pipeline, stage, automation, article, service key, recovery account, client,
  availability window, and labor rate.
- **Save and edit:** provider, credential, models, policy, phase, participant,
  custom field, workflow, scheduling, name, view, and configuration.
- **Transition:** move stage, claim work, update status or priority, issue,
  publish, accept, reject, approve, override, apply, enable, disable, and
  recognize billable work.
- **Record evidence:** note, message, activity, time, cost, decision, customer
  acceptance, audit reason, and reconciliation outcome.
- **Security:** sign in, sign out, replace secret, rotate key, revoke key,
  revoke session, and manage recovery access.
- **Recovery:** refresh, retry generation, retry delivery, retry models,
  cancel generation, and resolve dead letters.
- **Data:** search, filter, sort, select, upload, download, export, expand,
  collapse, paginate, and open details.

Button text begins with a specific verb. Generic `Submit`, `OK`, and `Yes`
labels are not used where the consequence can be named.

## Primitive component catalog

### Actions and selection

- `Button`
- `IconButton`
- `ButtonGroup`
- `OverflowMenu`
- `Checkbox`
- `RadioGroup`
- `Switch`
- `SegmentedControl`
- `SelectionCard`
- `Tag` and removable filter chip

Checkboxes select multiple values. Radios select one value from a visible set.
Switches immediately change a reversible boolean setting. Boolean changes that
require a reason, version, or server confirmation use an explicit action form,
not a switch.

### Fields

- `Field`
- `TextInput`
- `PasswordInput`
- `WriteOnlySecretField`
- `NumberInput`
- `MoneyInput`
- `DurationInput`
- `DateInput`
- `DateRange`
- `Select`
- `Combobox`
- `SearchInput`
- `Textarea`
- `CodeEditor`
- `FileUpload`
- `FieldGroup`
- `FormActions`
- `ValidationSummary`

Every field supports label, description, required/optional indication, value,
disabled and read-only states, validation, and error association. A field error
is specific and actionable. A form-level summary links to invalid fields.

`WriteOnlySecretField` never displays an existing secret. It shows only
configured/not-configured state, replacement, reason, and last-test evidence.

JSON and policy configuration use a code-aware editor with syntax feedback,
formatting, and a plain-text fallback rather than an unassisted text area.

### Navigation

- `AppShell`
- `GroupedSidebar`
- `TopBar`
- `GlobalSearch`
- `GlobalCreate`
- `Breadcrumbs`
- `PageTabs`
- `LocalNav`
- `Pagination`
- `Stepper`
- `SkipLink`

The primary sidebar is grouped by work domain and capability. The current flat
list of all available routes does not scale. Groups are:

- Work
- Sales
- Projects
- Knowledge and billing
- Integrations and automation
- Administration

The active page and active group are exposed semantically. Narrow layouts use
the same information architecture in a disclosed navigation panel.

### Containers and panes

- `Page`
- `PageHeader`
- `ActionBar`
- `Section`
- `Panel`
- `Card`
- `Inset`
- `SplitPane`
- `ListDetail`
- `Drawer`
- `Dialog`
- `ConfirmationDialog`
- `Disclosure`
- `Accordion`
- `StickyActions`

Use a dialog for a short, blocking decision. Use a drawer for supporting work
that should retain page context. Use a dedicated page for long, high-risk, or
multi-section tasks. Native disclosure remains appropriate for optional,
non-blocking detail; it is not the default container for primary creation
workflows.

Dialogs trap focus, have a visible title, close with Escape when safe, restore
focus to the opener, and never close from an accidental backdrop click while
the form is dirty.

### Data display

- `StatusBadge`
- `RecordID`
- `MetadataList`
- `Metric`
- `KeyValueList`
- `Avatar` and `Identity`
- `Timeline`
- `Progress`
- `Table`
- `Worklist`
- `TreeList`
- `PipelineBoard`
- `DefinitionList`
- `AuditEvidence`

Tables support stable headers, sorting, selection, pagination, row actions,
loading, empty, and error states. Bulk actions preview scope and report partial
failure. Worklists preserve focus and selection when data refreshes.

## Feedback and state catalog

### Local feedback

- field help
- field error
- form validation summary
- inline status
- status badge
- progress text
- tooltip
- toast
- page or section banner

Toasts confirm non-critical completion. They never contain the only copy of an
error or required next step. Persistent consequences use an inline notice or
banner.

### Shared section states

Every data-bearing section supports:

- initial
- loading
- refreshing
- populated
- filtered with results
- filtered with no results
- empty with a relevant primary action
- unavailable
- permission denied
- stale or version conflict
- partial failure
- offline or disconnected

Loading preserves the final layout where practical through skeletons. Empty
states explain why the area is empty and what authorized action can change it.
Errors explain what happened, what remains safe, and whether the user should
retry, refresh, change input, sign in, select a client, or contact an
administrator.

### Full-page system states

- `NotFoundPage`
- `PermissionDeniedPage`
- `AuthenticationRequiredPage`
- `EnvironmentUnavailablePage`
- `MaintenancePage`
- `SetupRequiredPage`
- `ClientSelectionRequiredPage`
- `UnexpectedErrorPage`

Each page retains product identity, a plain-language explanation, a safe
primary route, correlation/support evidence when available, and keyboard focus
on the page title. Raw stack traces and provider bodies are never displayed.

## Product workflow patterns

### Scoped action

Shows the active MSP or Client, affected record, required capability, and action
reason. It is used for sensitive integrations, service keys, billing,
reconciliation, and administrative changes.

### Reason-required confirmation

Combines consequence copy, immutable reason input, expected version, and
confirm/cancel actions. It is used for approval overrides, rejections, revokes,
dead-letter actions, key rotation, and protected configuration changes.

### Versioned editor

Shows current immutable or editable version, history, compare metadata,
conflict recovery, and issue/publish actions. It is used by Proposals, Change
Orders, pipelines, service-desk workflows, policies, and articles.

### Conflict recovery

Preserves the user's entered values, explains that the record changed, offers a
readable summary of newer evidence, and supports refresh/reapply or cancel.
Silent overwrites are not allowed.

### Durable job progress

Supports queued, running, cancellation requested, completed, failed,
retry-eligible, retry-delayed, cancelled, and pending-human states. Progress
survives refresh and exposes manual refresh when polling is unavailable.

### Connection management

Combines safe identity, enabled state, credential status, network mode,
test/discovery actions, last success, safe failure code, version, and scoped
reason. Credentials remain write-only.

### Approval and human decision

Presents evidence, decision options, required reason, override status, and
explicit acknowledgement of what will and will not be applied or sent.

### Record workspace

Combines page identity, status, metadata, primary workflow action, activity,
tasks, related records, and context panes. The same hierarchy applies to work,
Opportunity, Proposal, Project, Change Order, Client, and asset records.

### Worklist and saved view

Combines search, structured filters, saved views, results, selection, bulk
actions, refresh state, and list/detail behavior. Filters are URL-addressable
when they represent useful working context.

### Audit evidence

Uses append-only chronology, actor, capability, scope, correlation, before/after
metadata, and export. Evidence is dense but readable and never reduced to a
generic activity feed.

## Page templates

### Worklist page

Page header, scope, saved views and filters, result count, list or table,
selection/bulk actions, pagination, and optional detail pane.

### Record page

Breadcrumbs, record ID and title, status, metadata, primary transition,
activity, related sections, contextual actions, and version/conflict evidence.

### Settings page

Grouped local navigation or list/detail navigation, configuration form,
write-only secret treatment, health evidence, sticky save actions, and
version/conflict recovery.

### Dashboard page

Scope, date range, health/freshness, metrics, actionable exceptions, trends,
and links to source worklists. Dashboard cards do not become dead-end
decoration.

### Wizard page

Step indicator, scoped inputs, validation, reversible navigation, review, final
consequence, idempotent completion, and recovery after interruption. Setup and
Proposal-to-Project conversion use this template.

## Accessibility contract

Rarity targets WCAG 2.2 AA. Each component documents:

- semantic role and accessible name
- keyboard operation
- focus entry, movement, trapping, and restoration
- label, description, error, and status relationships
- disabled versus read-only behavior
- live-region behavior
- target size
- contrast and forced-color behavior
- zoom, reflow, and narrow-layout behavior
- reduced-motion behavior

Common rules:

- focus is visible and never hidden behind sticky content
- focus order follows the visual and task order
- validation does not rely on placeholder text
- errors remain present until resolved or dismissed deliberately
- asynchronous state changes use restrained live announcements
- tables retain header relationships and offer an accessible narrow-layout
  alternative
- drag or reordering always has move-up/down or direct-position controls
- tooltips are not required to understand or complete a task
- disabled actions expose prerequisite guidance nearby

## Content rules

- Use sentence case.
- Name the action and object: `Issue proposal version`, not `Continue`.
- Say why an action is unavailable.
- Separate an error from recovery guidance.
- Identify scope using the product's exact `MSP`, `Client`, and record language.
- Use `delete`, `revoke`, `disable`, `remove`, and `cancel` precisely; they are
  not interchangeable.
- Do not expose raw provider, SQL, network, or stack errors.
- Use stable safe error codes only when they help support or retry decisions.

## Component package boundaries

The frontend will introduce:

```text
frontend/src/design-system/
  foundations/
  components/
  patterns/
  templates/
  testing/
```

Public imports come from one design-system entry point. Feature modules do not
import private component internals or foundation files directly.

The initial package remains inside the Rarity frontend repository. Publishing a
separate npm package is deferred until another real consumer exists.

## Catalog and documentation

A development-only component catalog renders every component and product
pattern with realistic Rarity examples. It includes:

- default and interaction states
- compact and comfortable density
- light, future dark, and forced-color readiness
- desktop and narrow examples
- keyboard notes
- accessible-name and announcement behavior
- long text, empty, loading, error, conflict, and permission examples

The catalog is not reachable in production builds unless explicitly enabled for
a non-production environment.

## Testing and acceptance

### Automated

- unit tests for behavior and state mapping
- component accessibility checks with axe
- keyboard and focus tests
- visual snapshots at shared breakpoints
- contrast and forced-color checks where tooling supports them
- reduced-motion checks
- TypeScript public-API checks
- representative Playwright workflows using migrated components

### Manual

- keyboard-only traversal
- VoiceOver and NVDA screen-reader paths
- 200% and 400% zoom/reflow
- touch target and tablet use
- forced colors and high contrast
- light/dark visual review when dark mode is introduced

Passing a build or screenshot alone does not complete component acceptance.

## Adoption sequence

1. **Foundations and catalog:** semantic tokens, typography, spacing, focus,
   breakpoints, icons, component test utilities, and catalog shell.
2. **Core actions and fields:** buttons, field primitives, validation, form
   actions, status badges, and notices.
3. **Application shell:** grouped navigation, top bar, global search/create,
   page header, and responsive navigation.
4. **Feedback states:** loading, empty, error, permission, conflict, toast,
   banner, and full-page system states.
5. **Containers and data:** panels, disclosures, dialogs, drawers, tables,
   worklists, filters, and pagination.
6. **Rarity patterns:** reason-required actions, versioned editors, scoped
   actions, durable jobs, connection management, and audit evidence.
7. **Feature migration:** Work first, then Sales and Projects, followed by
   integrations/automation and administration.
8. **Legacy removal:** remove feature-local duplicates only after their
   migrated workflows pass rendered and accessibility acceptance.

## Explicit non-goals for the first release

- redesigning the Rarity brand
- creating a public multi-product component package
- replacing native controls without a demonstrated product need
- migrating every screen in one change
- adding documentation or marketing-site components
- claiming manual WCAG acceptance from automated tests
