# Unified Calendar Task 10 Persistence Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist calendar notification preferences and reminder facts, then compose the Task 10 notification, reminder, and recommendation services into the API runtime.

**Architecture:** Add a versioned recipient-owned preference set with normalized rule rows so replacements are atomic, auditable, and preserve the event/change/urgency/channel dimensions. Extend reminder facts with source-revision and threshold identity, implement both repositories in the PSA store, and inject concrete services through narrow HTTP/runtime interfaces. AI provider generation remains behind the existing provider boundary and every returned candidate still passes `calendar.ProposalService.Preview`; no recommendation path receives an apply dependency.

**Tech Stack:** Go 1.24, PostgreSQL migrations, pgx transactions, existing audit/event-outbox envelopes, `net/http`.

## Global Constraints

- Preferences are owned by the trusted principal; request bodies never select a recipient.
- Preference replacement uses optimistic concurrency and atomically writes rules, audit evidence, and an outbox event.
- Reminder deduplication identity is projection, occurrence, threshold, and source revision.
- Reminder evaluation is bounded and skips terminal, inaccessible, or preference-disabled recipients.
- AI recommendations can preview but can never apply a schedule.
- Existing deployed migration files remain immutable. The duplicate local-only
  client-resource versions 80/81 were renumbered to 97/98 after confirming on
  2026-08-15 that no external database had applied that unpublished history.
- The synchronous calendar recommendation path is zero-cost-only until it is
  moved onto the reserved/actual usage-accounting job runtime.
- Recommendation responses expose deterministic preview findings and a fresh
  preview request, never the proposal ID/version accepted by Apply.

## Remaining runtime boundary

The reminder evaluator is composed and emits asynchronous outbox facts. The
current safe evaluator scope is the standard 24-hour threshold for upcoming,
timed, non-recurring reminder-bearing roles. All-day organization timezone,
recurrence occurrence expansion, and administrator-configured thresholds need
a persisted configuration contract before they are enabled.

The general calendar change planner is deliberately not connected to the existing
notification-delivery tables yet: those tables require an organization policy
and, for Teams, a concrete connection destination, while recipient calendar
preferences only express channel enablement. The next slice must define that
organization-routing contract and persist calendar plans into the normal
delivery lifecycle; silently choosing a Teams connection or discarding plans
would violate the notification governance model.

---

### Task 1: Persist Calendar Preference Sets and Reminder Revision Identity

**Files:**
- Create: `backend/migrations/000095_calendar_notifications.sql`
- Create: `backend/migrations/calendar_notifications_contract_test.go`

**Interfaces:**
- Produces: `calendar_notification_preference_sets`, `calendar_notification_preference_rules`, and revision-aware uniqueness for `calendar_reminder_facts`.

- [ ] Write a migration contract test requiring tenant/technician foreign keys, rule enum checks, optimistic versioning, source revision, threshold, and the exact reminder deduplication key.
- [ ] Run `go test ./backend/migrations -run CalendarNotifications -count=1` and verify it fails because migration 95 is absent.
- [ ] Add migration 95. Use a versioned parent row keyed by `(msp_id, technician_id)` and child rules keyed by `(msp_id, technician_id, event_class, change_class, urgency, channel)`. Add `source_revision bigint` and `threshold text` to reminder facts and replace the old reminder uniqueness constraint with `(msp_id, projection_id, occurrence_key, threshold, source_revision)`.
- [ ] Run the migration contract and full migration package; both must pass.

### Task 2: Implement Atomic PostgreSQL Preference and Reminder Repositories

**Files:**
- Create: `backend/internal/store/psa/calendar_notification_repository.go`
- Create: `backend/internal/store/psa/calendar_notification_repository_test.go`
- Modify: `backend/internal/notifications/calendar.go`
- Modify: `backend/internal/calendar/reminders.go`

**Interfaces:**
- Implements: `notifications.CalendarPreferenceRepository`, `notifications.CalendarPreferenceReader`, and `calendar.ReminderRepository`.
- Consumes: the existing PSA transaction helper, audit ledger schema, event outbox schema, and calendar projection/source authorization data.

- [ ] Write failing repository tests proving default reads, trusted-recipient atomic replacement, version conflict, rule round-trip, audit/outbox rollback, reminder paging, reauthorization, preference suppression, and revision-aware claim deduplication.
- [ ] Run `go test ./backend/internal/store/psa -run 'CalendarPreference|CalendarReminder' -count=1` and verify the new tests fail for missing methods.
- [ ] Implement preference reads and atomic replacement. Lock the parent row, compare the expected version, replace child rules, append an audit record, and append `calendar.notification_preferences.replaced` in the same transaction.
- [ ] Implement bounded reminder candidate loading from active projections. Resolve only active assignees with current source access and an enabled matching preference rule.
- [ ] Implement `ClaimReminder` as one transaction that inserts the revision-aware fact and emits `calendar.reminder.due`; a uniqueness loss returns `false` without emitting.
- [ ] Run the focused repository tests and full PSA store suite.

### Task 3: Expose Typed Preferences and Compose Runtime Services

**Files:**
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/internal/httpapi/calendar_routes.go`
- Modify: `backend/internal/httpapi/calendar_routes_test.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/cmd/rarity-api/main_test.go`

**Interfaces:**
- Produces: typed `GET/PUT /api/v1/calendar/preferences`, concrete `CalendarPreferences`, and concrete reminder/recommendation runtime dependencies.

- [ ] Write failing HTTP tests for typed preference round-trip, unknown-field rejection, principal ownership, and optimistic version propagation.
- [ ] Replace the `any`/`map[string]any` calendar preference seam with `Get` and `Replace` typed methods and strict DTO decoding.
- [ ] Compose the PostgreSQL calendar notification repository and preference service in `rarity-api`.
- [ ] Compose reminder evaluation through the existing bounded worker lifecycle. Keep notification delivery asynchronous.
- [ ] Compose the calendar recommendation service with the permission-safe context loader, configured provider adapter, and proposal preview service; do not inject `Apply`.
- [ ] Run `go test ./backend/internal/calendar ./backend/internal/notifications ./backend/internal/aiassist ./backend/internal/httpapi ./backend/internal/store/psa ./backend/cmd/rarity-api -run 'Reminder|Calendar|Schedule|Recommendation' -count=1`.
- [ ] Run `go test ./backend/...`, `npm --prefix frontend test -- --run`, `npm --prefix frontend run build`, `node scripts/validate-docs.mjs`, and `git diff --check` sequentially.
