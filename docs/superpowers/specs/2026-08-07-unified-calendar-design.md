# Unified Calendar Design

**Status:** Approved in design review on 2026-08-07; awaiting written-spec review

## Goal

Create one authoritative, Rarity-native calendar experience for all dated work
and commitments across every client a staff member is authorized to access.
The primary outcome is faster, more accurate scheduling and capacity planning
without creating a second system of record for tickets, tasks, projects, or
other business objects.

The calendar must make it easy to answer:

- What work and commitments are scheduled?
- Who has capacity, across all authorized clients?
- Which items are blocked, overdue, or at risk?
- What downstream work would move if a schedule changes?
- Which scheduling recommendation is safest, and why?

Every calendar event is backed by a typed domain object. The owning object
remains authoritative for its dates, permissions, status, and business rules.
The calendar provides a normalized read model, scheduling workflow, and
filtered lenses over that data.

## Approved Product Decisions

- One event projection engine powers every calendar view.
- Every supported authoritative date or date-time field projects a typed event.
- All built-in typed date fields participate automatically; they do not require
  a separate per-field calendar opt-in.
- Generic free-form calendar events are not supported.
- The calendar defaults to all clients the current staff member is authorized
  to access. Client is a filter, not the calendar's primary boundary.
- Built-in lenses are Day, Week, Month, Timeline, Capacity, and Agenda.
- Users may save private or shared named lenses using the existing team,
  department, queue, and MSP saved-view permissions.
- The source object remains authoritative. Dragging or resizing an event writes
  back through the owning domain's authorized scheduling command.
- Date-only values remain all-day events. The system does not invent a time.
- Timed events display in the viewer's selected timezone while preserving and
  identifying the original timezone when it differs.
- A single source object may project multiple typed event roles, such as a
  ticket work block and a separate SLA deadline.
- Capacity is consumed only by assigned, explicitly scheduled work with planned
  effort.
- Capacity-bearing work supports fixed blocks and effort allocations.
- Technician availability uses a recurring base schedule plus dated
  exceptions.
- Requested PTO is tentative. Approved PTO reduces capacity.
- Dependencies support finish-to-start, start-to-start, and finish-to-finish
  relationships with lead or lag. Start-to-finish is excluded.
- Direct dependencies must remain within one client.
- MSP-wide events affect availability as shared constraints; they do not create
  cross-client dependency chains.
- Moving dependent work produces an impact preview and proposed cascade.
  Cascades never apply silently.
- Ordinary overbooking is overrideable after a warning and required reason.
- Administrators may configure selected conflicts as hard blocks.
- Event health is deterministic and explainable. AI is not used to assign
  health.
- AI scheduling is recommendation-only. Every AI-proposed move requires
  authorized human approval and deterministic revalidation.
- Scheduling authorization combines source-object edit permission with
  workforce scope: technicians schedule their own work, team managers schedule
  their teams, and workforce administrators schedule across authorized
  clients.
- Milestones are first-class project records.
- PTO, maintenance windows, renewals, and licenses are first-class typed
  records rather than generic event labels.
- Maintenance windows may affect clients, services, and assets and may be
  configured as scheduling constraints.
- Renewals and licenses retain commercial and relationship context in their
  owning records.
- Admin-configured custom date and date-time fields project automatically.
- Recurrence supports daily, weekly, monthly, yearly, selected weekdays, an end
  count or date, and exceptions.
- Recurring edits support this occurrence, this and future occurrences, and the
  entire series.
- Derived commitments such as SLA deadlines, license expirations, and contract
  renewals are read-only in the calendar.
- Meaningful schedule changes and approaching commitments may notify affected
  technicians and owners according to their preferences.
- This is a complete release, not a staged calendar rollout.
- The demo contains no important historical records, so the release does not
  require a legacy event backfill.
- External Microsoft 365, Google, or other calendar synchronization is outside
  this scope.

## Scope

The release includes:

- a normalized, cross-client calendar projection
- source adapters for every supported typed event role
- Day, Week, Month, Timeline, Capacity, and Agenda lenses
- broad filters and permission-aware saved lenses
- first-class milestone, PTO, maintenance-window, renewal, and license records
- recurring technician schedules and dated availability exceptions
- structured recurrence and occurrence-level exceptions
- drag-and-drop and resize scheduling with source-domain write-back
- fixed-block and effort-allocation scheduling
- dependency definition, visualization, validation, and cascade preview
- deterministic event health with reason codes
- capacity planning across all authorized clients
- conflict policies, warnings, hard blocks, and audited overrides
- recommendation-only AI scheduling
- privacy-safe conflicts and utilization
- notifications, audit, live updates, observability, and reconciliation
- representative demo data covering all event types and states

## Non-Goals

The release intentionally excludes:

- generic or free-form calendar events
- a separate calendar-owned copy of authoritative domain dates
- customer or portal scheduling access
- external calendar synchronization or two-way calendar feeds
- start-to-finish dependencies
- direct dependency chains across clients
- AI-applied schedule changes
- silent dependency cascades
- implicit default times for date-only fields
- capacity consumption from informational deadlines by default
- historical projection backfill for existing demo records
- a staged or partial rollout of individual calendar lenses

## Core Terminology

### Source object

A typed business record that owns dates and business rules, such as a ticket,
task, project milestone, PTO request, or maintenance window.

### Event role

The semantic meaning of one projected date or interval. A ticket may project
separate `scheduled_work`, `due`, and `sla_resolution_deadline` roles. Event
roles determine editability, capacity behavior, health inputs, presentation,
and eligible dependency relationships.

### Event projection

A normalized, searchable representation of a source object's event role. It is
not authoritative and can be rebuilt from the source.

### Occurrence

One calendar instance of an event role. A non-recurring event has one
occurrence. A recurring event has occurrences derived from its series rule and
persisted exceptions.

### Lens

A named presentation and filter definition over the common event read model.
Built-in views and user-saved views are both lenses.

### Fixed block

Capacity-bearing work scheduled for an exact start and end interval.

### Effort allocation

Capacity-bearing planned minutes distributed across the assignee's available
working intervals inside an authoritative date range.

### Scheduling proposal

A short-lived, revision-bound preview of one or more source-domain changes,
including impacts, conflicts, and required overrides. Applying a proposal is
the explicit human approval step.

## Architecture

### Recommended approach

The calendar uses a dedicated normalized event projection.

Query-time federation across every source table was rejected because it would
duplicate filter, recurrence, authorization, health, and capacity logic and
would make broad cross-client views progressively harder to operate. A
calendar-owned event entity was rejected because it would duplicate
authoritative dates and create conflicting edit paths.

The projection approach preserves strong domain ownership:

1. A source-domain command validates and commits an authoritative change.
2. The same transaction records audit evidence and a versioned outbox event.
3. A source adapter projects affected event roles into the calendar read model.
4. Capacity, dependency, health, notification, and live-update services consume
   the normalized projection.
5. Calendar scheduling commands return to the source adapter for validation
   and write-back.

The backend should introduce a focused calendar package with independently
testable services for projection, recurrence, queries, scheduling proposals,
dependencies, capacity, and health. It should reuse the repository's existing
HTTP, saved-view, audit, outbox, identity, authorization, and notification
boundaries rather than creating calendar-specific substitutes.

The frontend should introduce one calendar feature routed through the existing
route manifest. All six lenses consume one calendar data client and shared
event, filter, selection, proposal, and live-update state.

### Isolation boundaries

The following units have narrow responsibilities:

- **Source adapters** translate authoritative records to event roles and own
  schedule write-back for their domain.
- **Projection service** applies versioned, idempotent event-role changes.
- **Recurrence service** validates series and expands requested occurrence
  windows.
- **Query service** returns permission-filtered events, groups, and summaries.
- **Dependency service** validates relationships and computes schedule
  constraints.
- **Capacity service** calculates availability, consumption, and conflicts.
- **Health service** assigns deterministic state and reason codes.
- **Proposal service** previews and atomically applies approved changes.
- **Notification planner** emits permission-safe delivery plans after commit.
- **Reconciliation service** detects and repairs projection drift from source
  records.

No consumer other than the relevant source adapter may update a source
domain's scheduling fields.

## Event Projection Model

### Event projection identity

An event projection is uniquely identified by:

- MSP ID
- source object type
- source object ID
- event role
- source event-role key when one object has repeated roles

The projection stores:

- immutable projection ID
- current source revision
- client scope when applicable
- source route and anchor metadata
- event type and role
- title and permission-safe presentation metadata
- owner, assignee, team, project, technology, ticket type, tag, priority, and
  SLA filter dimensions
- authoritative date, start, end, all-day, and timezone values
- recurrence series metadata
- editability and scheduling mode
- planned effort and capacity behavior
- dependency eligibility
- source status and health inputs
- cancellation or terminal state
- projection revision and timestamps

The projection contains only fields needed for calendar queries and safe event
presentation. It does not become a general copy of source content.

### One object, multiple event roles

One source object may emit several independent projections. For example:

- a ticket may emit a scheduled-work interval, due date, response deadline, and
  resolution deadline
- a project may emit planned start and completion boundaries
- a license may emit effective, notice, and expiration dates

Each role has its own identity, health rules, read/write behavior, and
occurrences. Selecting any role deep-links to the exact relevant source
context.

### Projection lifecycle

Projection writes are:

- idempotent by source identity, event role, and source revision
- ordered so an older source revision cannot replace a newer one
- retryable after transient failure
- removable or suppressible when the source is deleted, cancelled, or no
  longer projects that event role
- reconstructable from the authoritative source

A transactional outbox prevents a committed source update from losing its
projection request. Projection lag never makes the read model authoritative.

### Recurrence storage and expansion

A recurring series stores:

- local start time and duration or all-day span
- IANA timezone
- frequency
- interval
- selected weekdays when applicable
- day-of-month or equivalent monthly/yearly pattern
- optional end count or inclusive end date
- series revision

Occurrence exceptions store:

- original occurrence identity
- cancelled, rescheduled, or overridden state
- replacement date or interval
- edited fields allowed for that event role
- source revision and audit evidence

Series are stored once and expanded only for bounded query and planning
windows. Exceptions are persisted. The API rejects unbounded occurrence
queries.

## Typed Source Domains

### Tickets and tasks

Tickets and tasks may project:

- scheduled fixed work
- scheduled effort allocations
- due dates
- follow-up dates
- SLA response and resolution commitments
- supported administrator-configured custom dates

Only explicit scheduled work with an assignee and planned effort consumes
capacity. SLA and due events remain informational.

### Projects

Projects may project:

- planned start and completion boundaries
- phase boundaries
- scheduled project work
- milestones
- task schedules
- supported custom dates

### Milestones

A milestone is a first-class project record with:

- immutable ID, MSP, client, project, and optional phase
- name and description
- owner
- authoritative date or interval
- status
- priority
- dependency relationships
- audit version and timestamps

Milestones are valid dependency sources and targets but do not consume capacity
unless they represent separately scheduled assigned work.

### Technician schedules

A technician schedule contains:

- technician and workforce scope
- IANA timezone
- recurring weekly base availability
- effective date range and version
- dated exceptions

Exceptions include holidays, approved PTO, training, on-call adjustments, and
manual availability overrides. The capacity engine resolves the applicable
base revision and exceptions for each requested interval.

### PTO

A PTO request contains:

- requester
- start and end, all-day or timed
- timezone
- type and privacy-safe label
- state: requested, approved, rejected, or cancelled
- configured team manager approver
- approval or rejection evidence
- optional workforce-administrator override evidence

Requested PTO is tentative. Approved PTO reduces capacity. Rejected and
cancelled PTO do not reduce capacity. Approval routes to the configured team
manager, with authorized workforce administrators providing fallback and
override.

### Maintenance windows

A maintenance window contains:

- owner
- title and internal details
- start and end
- timezone
- status
- recurrence
- affected clients, services, and assets
- conflict policy
- audit version

Its conflict policy may be informational, warning, overrideable block, or
administrator-protected hard block. A maintenance window is an MSP-wide shared
constraint when its scope spans clients; it does not create direct
cross-client dependencies.

### Renewals and licenses

Renewal and license records contain:

- owner
- vendor
- quantity and cost where applicable
- effective, notice, renewal, and expiration dates as applicable
- recurrence
- status
- links to client, service, asset, and contract records
- versioned audit history

Their derived calendar commitments are read-only. Users open the owning record
to alter commercial dates or apply an authorized override.

### Administrator-configured custom dates

An administrator may enable a supported custom date or date-time field for
projection and configure:

- calendar label
- event category and role
- default color category
- all-day or timed interpretation based on field type
- read-only or schedulable behavior
- optional capacity-bearing behavior
- supported source-object types

Enabling a field affects records created or updated after configuration in this
demo release. The system does not reinterpret a plain date as an invented
timestamp.

## Calendar Workspace

### Global scope

The Unified Calendar opens across all clients the current staff member is
authorized to see. It does not inherit the active client selector as a hidden
restriction. Client selection is an explicit, visible filter.

This is essential for technicians and dispatchers working across several
clients in one day. Capacity and conflicts must not fragment by client.

### Built-in lenses

- **Day** presents timed and all-day work for one day with resource grouping.
- **Week** presents the primary operational scheduling grid.
- **Month** emphasizes commitments, deadlines, health, and density.
- **Timeline** presents date ranges, milestones, and dependency connectors.
- **Capacity** presents availability, committed work, remaining capacity,
  overbooking, and unscheduled effort.
- **Agenda** presents a dense chronological, paginated list suitable for broad
  date windows and keyboard workflows.

These lenses share selection, filters, saved-view definitions, event health,
and source navigation. Switching lenses does not query a separate event model.

### Filters

The common filter model includes:

- technician and owner
- team
- client
- technology
- project and phase
- SLA
- ticket type
- tags
- priority
- event type and role
- health
- capacity conflict
- date window

Filters use immutable identities rather than labels. Filter options and result
counts are permission-filtered.

### Saved lenses

A saved lens records:

- owner
- name and description
- base lens
- date-window behavior
- filters
- grouping and sorting
- visible event categories
- display density and optional capacity overlays
- private or shared scope
- version

Shared scopes reuse existing private, team, department, queue, and MSP saved
view authorization. Sharing never grants source-object access; every viewer
receives their own permission-filtered result.

### Navigation and creation

Selecting an event opens its source object and exact role context. The source
workspace handles detailed edits that do not belong in scheduling.

A typed Create menu routes users into the appropriate domain workflow for a
task, milestone, PTO request, maintenance window, renewal, license, or other
supported record. The calendar never offers a generic event form.

### Presentation

Event category determines the stable base presentation. Health and conflicts
are separate indicators rather than competing color meanings. Every state has
a text label and accessible non-color treatment.

Date-only commitments appear in all-day regions. Timed events display in the
viewer's selected timezone. When the source timezone differs, event details
show both values clearly.

### Approved visual direction

The approved visual companion establishes a dark, information-dense operations
workspace consistent with the existing Rarity product direction:

- a clear calendar title and global date navigator
- an immediately visible Day, Week, Month, Timeline, Capacity, and Agenda
  switcher
- a persistent filter and saved-lens bar
- resource-oriented rows for technicians and teams where the lens supports it
- typed event cards with compact source, client, time, and health cues
- dependency connectors in Timeline rather than decorative lines in every view
- capacity bars and conflict markers adjacent to the schedule they explain
- a contextual detail and impact-preview panel for selection, scheduling, and
  cascade confirmation

The implementation should preserve this hierarchy and interaction model while
using the repository's shared design-system primitives and accessibility
contracts. Health, event category, selection, and conflict state must remain
distinguishable without relying on color alone.

## Scheduling and Write-Back

### Scheduling modes

A schedulable source event declares one of:

- `fixed_block`, with authoritative start and end
- `effort_allocation`, with a date range and planned minutes
- `informational`, which is not directly schedulable

Dragging a fixed block changes its exact interval. Resizing changes its
duration if the source adapter permits it. Moving an effort allocation changes
its eligible range; the capacity service recalculates its distribution.

Informational events cannot be dragged. Their details identify the owning field
and link to the source workflow.

### Authorization

Scheduling requires both:

- permission to edit the source object's scheduling fields
- workforce authority over every affected assignee

The workforce model is:

- technicians may schedule their own authorized work
- team managers may schedule members of their configured teams
- workforce administrators may schedule across all clients they are authorized
  to access

A cascade is authorized per affected source and assignee. Authority over one
item never implies authority over downstream items.

### Preview and apply

Scheduling uses a two-step flow:

1. The user proposes a move, resize, allocation, recurrence change, dependency
   edit, or AI recommendation.
2. The backend validates the request and returns a scheduling proposal.
3. The proposal describes direct changes, proposed dependent changes,
   conflicts, capacity impact, health impact, notifications, and required
   overrides.
4. The user may adjust optional cascade moves, provide required reasons, and
   approve or cancel.
5. Approval revalidates the proposal and applies all required changes
   atomically.

An isolated, valid drag may present a compact confirmation, but the drag itself
is still the authorized human decision. AI never skips the proposal step.

### Conflict policy

Conflicts are classified as:

- informational
- warning
- overrideable block
- hard block

Ordinary overbooking is an overrideable block. Proceeding requires explicit
confirmation and a reason. Administrators may make approved PTO, non-working
time, or protected maintenance blackouts hard blocks.

The preview must identify which policy produced each conflict. Hard blocks
cannot be bypassed through the calendar command.

### Concurrency and audit

Proposals capture source revisions, relevant schedule revisions, dependency
versions, and conflict-policy versions. Apply fails as stale if a material
input changes.

Every applied decision records:

- actor
- proposal and correlation IDs
- before and after values
- reason
- conflict overrides
- dependency and capacity impacts
- recurrence scope
- affected source revisions
- time and timezone context

## Dependencies

### Relationship model

A dependency contains:

- predecessor source and event role
- successor source and event role
- relationship type
- lead or lag duration
- client
- version and audit metadata

Supported types are:

- finish-to-start
- start-to-start
- finish-to-finish

Positive offsets are lag. Negative offsets are lead.

### Validation

The dependency service enforces:

- both objects belong to the same client
- both event roles permit dependencies
- the actor can view and edit the relevant relationship context
- the relationship does not create a cycle
- the relationship is not a duplicate
- lead or lag values are within configured safe bounds

Informational SLA, renewal, and expiration commitments are not dependency
participants. PTO and MSP-wide operational constraints affect availability
instead of forming dependency edges.

### Visualization and cascades

The Timeline lens shows dependency connectors, violated constraints, and
downstream impact. Other lenses expose dependency badges and reason details.

Moving a constrained event computes:

- required shifts needed to preserve constraints
- optional downstream shifts
- blocked items
- new or resolved capacity conflicts
- commitments that become overdue or at risk
- sources or assignees the actor cannot change

The user may approve the valid cascade, adjust optional moves, or cancel. Every
committed cascade shares one audit correlation ID.

## Availability and Capacity

### Availability

Availability is calculated in working minutes using:

1. the technician's applicable recurring base schedule
2. dated schedule exceptions
3. approved PTO and holidays
4. training, on-call, and manual availability adjustments
5. configured maintenance or protected constraints

The service uses the authoritative timezone for each schedule and resolves
daylight-saving transitions without changing the intended local working hours.

### Consumption

Only work that is:

- assigned
- explicitly scheduled
- capacity-bearing
- associated with planned effort

consumes capacity.

Fixed blocks consume their exact overlapping working intervals. Effort
allocations distribute planned minutes into available intervals inside their
range. Informational events do not consume capacity unless an administrator
has explicitly configured that event role as capacity-bearing.

### Capacity lens

The Capacity lens presents, by technician and aggregate team:

- total available minutes
- fixed committed minutes
- allocated effort minutes
- remaining minutes
- overbooked minutes
- tentative unavailability
- unscheduled effort shown separately

Values aggregate across all authorized clients. Unscheduled work does not
inflate utilization, but remains visible as planning demand.

### Capacity conflicts

The service detects:

- overlapping fixed blocks
- work outside availability
- approved unavailability
- insufficient room for an effort allocation
- protected maintenance constraints
- team or technician overbooking

The same server-side calculation powers query summaries, scheduling previews,
automation inputs, and AI recommendations.

## Event Health

Health is deterministic, explainable, and recomputed when relevant source,
schedule, dependency, capacity, or SLA inputs change.

Active states, in precedence order, are:

1. **Blocked** — source status, an unresolved dependency, or a hard constraint
   prevents progress.
2. **Overdue** — an incomplete authoritative commitment has passed its due or
   end time.
3. **At risk** — capacity shortage, dependency delay, schedule variance, or SLA
   margin indicates likely failure before the commitment is overdue.
4. **On track** — no active rule indicates risk.

Completed and cancelled events are terminal and excluded from active-risk
totals.

Every health result includes:

- state
- evaluated time
- rule-version identity
- reason codes
- supporting source facts
- next relevant threshold where applicable

Health never relies on an opaque AI score. The interface shows the reason, such
as “blocked by milestone,” “120 minutes exceed available capacity,” or
“resolution target is inside the configured risk margin.”

## AI Scheduling Recommendations

AI may recommend:

- candidate technicians
- candidate fixed intervals
- effort-allocation windows
- dependency-safe alternatives
- cascade alternatives

Recommendations consider only data the initiating principal or authorized
system process may use, including:

- source authorization
- workforce scope
- client boundary
- required skills and technology
- availability and workload
- priority
- dependencies
- hard constraints
- deterministic event health

Each recommendation includes a concise explanation, predicted conflicts,
capacity effect, downstream tradeoffs, and the facts used. The user can send a
recommendation into the normal proposal flow.

AI cannot directly mutate a schedule. Apply-time deterministic validation
prevents a stale or incomplete recommendation from bypassing current rules.

## Permissions and Privacy

### Event visibility

Full event detail requires permission to view the source object. The calendar
does not define an independent visibility grant.

Authorization applies consistently to:

- event rows
- occurrence expansion
- filter options
- result counts
- utilization totals
- dependency overlays
- search
- saved lenses
- notifications
- deep links

Opening a deep link rechecks current source authorization.

### Privacy-safe busy blocks

A scheduler may have workforce authority over a technician without permission
to view every source record consuming that technician's time. In this case the
calendar returns a privacy-safe Busy or Unavailable block containing only the
minimum interval and conflict information required to prevent double-booking.

It must not reveal:

- client identity
- ticket or project identity
- title or description
- tags or technology
- private PTO details

PTO details follow workforce permissions. Users without detail permission see
only the availability effect.

### Cross-client isolation

Broad calendar queries span authorized clients, never all clients in the MSP by
default. Source filters and authorization predicates must be applied before
aggregation so counts and capacity cannot reveal inaccessible work.

Saved and shared lenses store definitions, not pre-authorized result sets.
Sharing a lens never expands source access.

## Notifications

The notification planner may produce events for:

- assignment to scheduled work
- meaningful rescheduling
- cancellation
- new or changed conflicts
- approaching due dates and deadlines
- PTO decisions
- approved cascades

Recipients include affected technicians, owners, and configured operational
roles. Delivery follows existing organization policy, recipient preferences,
quiet periods, and available notification channels.

Users may configure notification preferences by event type, change type,
urgency, and channel. Deduplication and aggregation prevent a cascade from
generating an unreadable burst of individual messages.

Notification payloads contain only permission-safe metadata. Access is checked
when planning delivery. Notification failure does not roll back a valid source
change.

## API and Data Flow

### Read APIs

The calendar read boundary supports:

- bounded occurrence queries by date window
- shared filters across every lens
- grouping and sorting
- Agenda pagination
- Timeline dependency overlays
- Capacity summaries
- saved-lens resolution
- event detail and source-link metadata
- live-update cursors

All read responses include stable event and occurrence identities, source
revision, health reason codes, edit capabilities, and privacy mode.

### Command APIs

The calendar command boundary supports:

- schedule preview
- proposal apply
- dependency preview and mutation
- recurrence-scope preview
- fixed-block and effort-allocation changes
- conflict override reasons

Typed source creation and non-scheduling source edits remain in their owning
domain APIs.

### Live updates

Projection changes publish a permission-filtered live update feed. Open views
may incrementally add, update, or remove occurrences and invalidate affected
capacity or dependency summaries.

The live feed is an optimization. Reconnecting with a cursor or refetching the
window must always reconstruct correct state.

## Consistency and Failure Handling

### Proposal validity

A proposal is short-lived and bound to:

- actor and authorization context
- affected source revisions
- schedule and availability revisions
- dependency revisions
- conflict-policy versions
- computed occurrence scope

Apply revalidates every binding. A stale proposal returns a refreshed impact
preview rather than partially applying old assumptions.

### Atomic changes

All required source mutations in an approved cascade, their audit evidence,
and their outbox events commit atomically. If any required mutation fails,
nothing in the cascade commits.

Optional changes removed by the user are excluded before final validation.

### Projection failure

A projection failure:

- leaves the authoritative source intact
- retries through the outbox
- records operational evidence
- contributes to projection-lag and retry metrics
- is discoverable by reconciliation

The interface distinguishes a committed source change awaiting projection from
a rejected scheduling command.

### Validation errors

The API returns actionable errors for:

- invalid date ranges
- invalid recurrence rules
- unsupported occurrence scope
- dependency cycles
- cross-client dependencies
- incompatible event roles
- hard conflicts
- missing override reasons
- stale revisions
- changed authorization

Errors preserve the user's proposed input wherever it remains safe to do so.

### Time semantics

Date-only values are stored and compared as dates. Timed values preserve their
IANA timezone and instant.

Recurring timed events preserve intended local wall-clock time across
daylight-saving changes. Ambiguous or nonexistent local times require an
explicit deterministic resolution and are displayed in the preview.

## Reconciliation and Observability

Reconciliation compares authoritative source event roles with projections and
identifies:

- missing projections
- stale source revisions
- orphaned projections
- invalid recurrence exceptions
- inconsistent terminal state
- health or capacity input lag

Repair regenerates projections from source records. It never changes
authoritative source dates.

Operational metrics include:

- projection lag
- outbox retry depth and age
- missing, stale, and orphaned projections
- calendar query latency and result volume
- recurrence expansion count and duration
- capacity-calculation duration
- proposal generation and stale-apply rates
- dependency-cycle and conflict counts
- live-feed reconnects
- notification failures
- reconciliation findings and repairs

Structured logs and traces retain correlation IDs across source command, audit,
outbox, projection, proposal, notification, and live-update work.

## Migration and Release

The release adds:

- normalized event projections and recurrence exceptions
- dependency relationships
- scheduling proposals and correlated audit evidence
- calendar lens definitions
- recurring technician schedules and dated exceptions
- first-class milestone, PTO, maintenance-window, renewal, and license records
- projection and notification outbox handlers
- reconciliation and operational-health support

The existing demo data has no material scheduling history, so no legacy event
backfill or compatibility layer is required. No legacy event backfill is part
of this release. New and updated records project after the engine is installed.
Representative seed data demonstrates:

- every typed source and event role
- all six lenses
- all-day and timed events in multiple timezones
- recurrence and exceptions
- fixed blocks and effort allocations
- requested and approved PTO
- warnings and hard conflicts
- dependencies and cascade impact
- all health states
- privacy-safe busy blocks
- AI scheduling recommendations

Every feature in this specification ships together. External calendar
integration is not a hidden prerequisite.

## Testing Strategy

### Domain and adapter tests

- every supported source and event role has an adapter contract test
- multiple event roles from one object retain independent identity
- derived read-only roles reject calendar write-back
- custom date configuration preserves date versus timestamp semantics

### Recurrence and time tests

- daily, weekly, monthly, yearly, and selected-weekday patterns
- count and date endings
- one-occurrence, future-occurrence, and whole-series edits
- cancelled and rescheduled exceptions
- month-end, leap-year, and timezone boundaries
- daylight-saving gaps and overlaps
- bounded expansion and pagination

### Authorization and privacy tests

- all-client queries include exactly authorized clients
- source detail never leaks through counts, filters, capacity, dependencies,
  saved lenses, notifications, or live updates
- privacy-safe busy blocks prevent double-booking without source disclosure
- PTO detail follows workforce permissions
- shared lenses cannot elevate access
- deep links revalidate authorization

### Scheduling and concurrency tests

- fixed-block drag and resize
- effort-allocation redistribution
- own-work, team-manager, and workforce-administrator authority
- warning and reason-required overrides
- administrator hard blocks
- stale proposal and source revisions
- recurrence-scope validation
- atomic cascade application and rollback
- complete audit correlation

### Dependency tests

- finish-to-start, start-to-start, and finish-to-finish
- lead and lag
- cycle and duplicate rejection
- same-client enforcement
- compatible event-role enforcement
- impact preview and user-adjusted optional moves
- unauthorized downstream work

### Capacity and health tests

- recurring base schedules and dated exceptions
- requested versus approved PTO
- fixed and allocated consumption
- cross-client aggregation
- unscheduled effort exclusion
- overlap, availability, allocation, and protected conflicts
- deterministic health precedence and reason codes
- health recomputation after every relevant input change

### Integration and reliability tests

- source transaction, audit, and outbox atomicity
- idempotent, revision-aware projection
- retry and reconciliation
- notification failure isolation
- live-update reconnect and refetch
- source deletion, cancellation, and permission revocation

### Frontend and accessibility tests

- Day, Week, Month, Timeline, Capacity, and Agenda behavior
- common filters and saved lenses
- keyboard scheduling and non-pointer alternatives to drag-and-drop
- accessible health and conflict states independent of color
- typed creation and source deep links
- proposal confirmation, error recovery, and stale refresh
- responsive behavior at supported desktop and compact widths

### Performance tests

Performance fixtures model realistic MSP-wide, multi-client schedules.
Benchmarks cover:

- broad authorized date-window queries
- dense Week and Month views
- Timeline dependency overlays
- team Capacity aggregation
- recurrence-heavy windows
- proposal generation with downstream cascades
- live projection updates

## Acceptance Criteria

The Unified Calendar is complete when:

1. All supported authoritative dates project through one normalized engine.
2. Every event resolves to a typed source object and exact event role.
3. Day, Week, Month, Timeline, Capacity, and Agenda use the same event and
   filter model.
4. The default view spans all and only authorized clients.
5. Date-only, timed, recurring, exception, and timezone behavior is
   deterministic.
6. Dragging and resizing use authorized source-domain preview and apply
   commands.
7. Capacity correctly combines technician schedules and assigned scheduled
   work across clients.
8. Dependencies validate client boundaries and cycles and never cascade
   silently.
9. Conflicts produce the configured warning, override, or hard-block behavior.
10. Health is deterministic, reasoned, and independent from AI.
11. AI produces explainable proposals but cannot apply schedule changes.
12. Full details, aggregate values, saved lenses, notifications, and live
    updates respect source and workforce permissions.
13. Projection failures are retryable and repairable without changing source
    records.
14. Representative demo data and automated coverage exercise the complete
    release.

## Written-Spec Review Checklist

- The calendar is a projection, not a second source of truth.
- Every event is backed by a typed domain object.
- Cross-client scope means all authorized clients, never unrestricted MSP
  access.
- Capacity is limited to assigned, explicitly scheduled work with planned
  effort.
- Cascades and AI recommendations always require authorized human approval.
- Event health is deterministic and explainable.
- Recurrence, timezones, privacy-safe conflicts, concurrency, and failure
  behavior are explicit.
- No staged rollout, external calendar integration, or legacy backfill is
  implied.
