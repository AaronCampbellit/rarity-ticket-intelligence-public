# Unified calendar

**Status:** Source implementation and local checks complete; external acceptance remains. See the
[current execution record](../09-roadmap/current-execution.md) for verified results
and external release gates. The historical task plan is not the current status.

## Workspace and ownership

The protected `#/calendar` workspace offers Day, Week, Month, Timeline, Capacity,
and Agenda views. It combines authorized Rarity-native sources across clients;
the navigation bar's active client does not silently narrow the calendar. Explicit
client, technician, owner, team, project, phase, SLA, technology, tag, ticket type,
priority, event role, health, source type, scheduling mode, and terminal-state
filters are visible. Filters and lenses can be saved privately or shared under
ordinary view permissions. Only the viewer timezone is stored as a per-principal
browser preference. Busy source identities are never stored in preferences.

The Home schedule feed uses the same calendar API. Calendar editing requires a
desktop viewport; the phone route provides the existing desktop handoff. The
calendar is not an Outlook/Google calendar synchronization implementation.

Calendar projections are disposable read models. Work Records, tasks, projects,
phases, milestones, resource plans, technician schedules, PTO, maintenance windows,
commercial commitments, and custom date values remain authoritative. Edits go to
typed source services or the scheduling proposal transaction. There is no generic
mutable calendar event. Source changes retain their audit and outbox behavior.

## Dates, visibility, and volume

- Date-only values remain ISO dates. An interval's end date is exclusive. Creation
  forms ask for the last included day and convert it to that boundary.
- Timed values carry RFC3339 instants and an IANA timezone. Display uses the viewer's
  timezone. Nonexistent and repeated daylight-saving clock times require correction
  or an explicit UTC instant; they are not silently shifted during form submission.
- Recurrence is structured: daily, weekly with selected weekdays, monthly, or yearly,
  with an interval and count/end bound. Rescheduling a series requires an explicit
  occurrence scope. The server validates expansion and occurrence identities.
- Full events expose authorized source links, role, health and reason codes,
  recurrence, scheduling metadata, and conflicts. A Busy block exposes only the
  permitted time/technician context. It has no source link, projection identity,
  recurrence, health, dependency membership, or scheduling action in the browser.
- Event requests use bounded pages and opaque cursors. Every lens exposes **Load more
  events** and warns while a view is incomplete. Very dense requests fail explicitly
  with the server result-size error; narrow the period or filters before retrying.
- Live changes trigger an authorized refetch. Window/client changes abort earlier
  requests, discard stale results, and reset the visible page. Refresh remains
  available if the live feed is interrupted.

## Scheduling and capacity

Drag a schedulable card to another day or choose **Reschedule** in its details.
Both paths open the same keyboard-accessible preview. No source date changes on
an initial drag. The server reloads source revisions, scope, workforce authority,
dependencies, conflicts, capacity and health, then issues an expiring proposal.

The preview lists required and optional changes, blocked sources, conflict
severity, capacity changes, health, and notification effects. Confirmation requires
a reason; optional changes are explicitly selected. Hard conflicts and expired
proposals cannot be confirmed. Version conflicts require a fresh preview. Apply
passes the exact proposal version and runs the source mutations atomically.

Dependencies support finish-to-start, start-to-start, and finish-to-finish with
bounded lead/lag. The event details panel lists only edges whose **both endpoints**
remain readable. Editors can preview/create an edge using same-client events in
the loaded window, or remove an edge using its current version. Changing an edge
does not silently move its sources; rescheduling still uses a proposal.

Capacity is an authoritative server result: available, fixed, allocated, committed,
remaining, overbooked, tentative unavailable, unscheduled, and dependency-blocked
minutes. A failed capacity request is not displayed as zero availability. Requested
PTO is tentative; approved PTO affects committed capacity. Health follows the
versioned blocked, overdue, at-risk, on-track precedence and terminal handling.

## Typed administration

**Create and manage calendar records** opens permission-aware source forms:

| Surface | Behavior |
| --- | --- |
| Milestone | Select client/project/phase, owner, due date, all-day or timed interval; edit and transition with a source version |
| PTO | Request for the signed-in technician; current manager/workforce admin decides; authorized owner/admin cancels |
| Maintenance | Typed client/service/asset scopes, recurrence, protection and conflict policy |
| Renewal/license | Client, vendor, owner, effective/notice/renewal/expiration dates, exact quantity and minor-unit cost, optional resource relationships |
| Working schedules | Effective dates, IANA timezone, weekly windows, capacity percentages and dated availability exceptions |
| Conflict policy | MSP/team/technician scope; warning, overrideable block or hard block per known conflict kind |
| Custom dates | Active typed field definitions and date/datetime semantics, optional task/Work Record capacity with an effort source |
| Notifications | Versioned recipient rules for schedule, PTO, conflict, cancellation and reminder, urgency, in-app/email; organization policy and quiet periods still apply |

Project details and Directory Settings reuse these forms. Work Record/task,
project, asset, knowledge article, and time-entry surfaces expose **Custom calendar
dates**. Their definitions are read through a source-authorized endpoint; calendar
policy administration is not required to read an ordinary record's date fields.
Saving still requires that source's edit permission. Definition changes are loaded
by custom-date projection adapters without restarting the application.

Forms preserve server versions. Ambiguous network retries reuse the same
idempotency key for the same payload. An explicit conflict requires reloading;
the browser does not invent a newer version. Source services enforce allowed
status transitions and all relationship ownership constraints.

## HTTP route catalog

All routes below have the `/api/v1` prefix. Browser mutations include the normal
session CSRF headers. Calendar reads derive scope from the principal; explicit
source client context is supplied for project/resource editor reads.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/calendar/events` | Bounded authorized event page |
| GET | `/calendar/filter-options` | Privacy-safe filter dimensions and counts |
| GET | `/calendar/capacity` | Authoritative capacity summaries |
| GET | `/calendar/live` | Cursor/refetch event stream |
| POST | `/calendar/proposals` | Preview a typed scheduling change |
| POST | `/calendar/proposals/{id}/apply` | Confirm exact proposal version and reason |
| GET | `/calendar/dependencies` | Source-authorized incident edges; `projection_id` required |
| POST | `/calendar/dependencies/preview` | Validate an edge and show cascade effects |
| POST | `/calendar/dependencies` | Create a validated edge |
| DELETE | `/calendar/dependencies/{id}` | Remove with `expected_version` |
| GET, PUT | `/calendar/conflict-policies` | Read/replace scoped policy |
| GET, PUT | `/calendar/custom-date-fields` | Administer active date definitions |
| GET, PUT | `/calendar/notification-preferences` | Recipient notification rules |
| GET, PUT | `/calendar/preferences` | Compatibility alias for notification preferences |
| GET | `/objects/{type}/{id}/custom-date-fields` | Read definitions under ordinary source authority |
| GET, PUT | `/objects/{type}/{id}/custom-date-values` | Read/set a typed source date value |
| GET, POST | `/workforce/schedules` | Read/publish effective working schedules |
| POST | `/workforce/schedules/{id}/exceptions` | Versioned dated exception |
| GET, POST | `/workforce/pto` | Read/request PTO |
| POST | `/workforce/pto/{id}/decision` | Current-manager/admin decision |
| POST | `/workforce/pto/{id}/cancel` | Authorized cancellation |
| GET, POST | `/projects/{id}/milestones` | Read/create project milestones |
| PATCH | `/project-milestones/{id}` | Versioned milestone edit/transition |
| GET, POST | `/maintenance-windows` | Read/create maintenance |
| PATCH | `/maintenance-windows/{id}` | Versioned maintenance edit/transition |
| GET, POST | `/commercial-commitments` | Read/create renewals and licenses |
| PATCH | `/commercial-commitments/{id}` | Versioned commercial edit/transition |
| GET, POST | `/views` | Saved `calendar_lens` views |

Calendar capabilities are `calendar.read`, `calendar.schedule`,
`calendar.commitment.manage`, `calendar.policy.manage`, and
`calendar.workforce.manage`. Saved lenses use `view.save`/`view.share`; source
reads/edits retain ordinary project, Work Record, knowledge, asset, and time-entry
authority. UI capabilities are convenience controls, never an authorization bypass.
Stable errors are mapped by the HTTP domain-error boundary; invalid intervals,
read-only roles, stale versions, missing recurrence scope, forbidden changes and
result-size limits are surfaced rather than replaced with empty success responses.

## Operations and development evidence

Use the disposable PostgreSQL workflow in [local development](../06-development/local-development.md).
The Playwright acceptance server activates real calendar services only with
`CALENDAR_E2E_SERVE=1`; it is a test fixture, not a production authentication mode.
The browser tests create typed records through those services and verify database
reads and projected calendar results. Mocked tests separately cover adversarial
Busy payloads and proposal UI states.

`rarity-admin calendar-reconcile --msp-id <uuid> --limit 500` emits a read-only
report; add `--repair` to repair projections from their authoritative sources.
`DATABASE_URL` must identify the intended environment. Limits are 1–5000. This is
a bounded scan, not proof that a larger tenant has been fully reconciled.

`rarity-admin calendar-demo-seed --msp-id <uuid> --client-ids <uuid>,<uuid>
--technician-ids <uuid>,<uuid> --at <RFC3339>` exports a deterministic **fixture
manifest**. It performs no database writes. The manifest describes typed source
scenarios, two timezones, recurrence, dependencies, PTO states, conflicts and health;
a test harness must materialize sources through their ordinary domain services.
For a runnable database demo, use the isolated browser acceptance fixture. This
replaces the old plan's implicit direct database demo seeding assumption.

Run `CALENDAR_PERFORMANCE=1 go test ./backend/internal/calendar -run
'^TestCalendarInteractivePerformanceFixture$' -count=1 -v` separately from other
heavy checks. Its indexed in-memory fixture holds 100,000 projections for 1,000
clients and measures 50 concurrent technician queries across 90 days plus a
100-node cascade. Its 500 ms p95 assertion is a service-fixture check. It does not
establish PostgreSQL, production hardware, HA, provider delivery, or release load
qualification. Those retained measurements and manual accessibility evidence
remain external gates.
