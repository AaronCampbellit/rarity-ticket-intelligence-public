# Calendar Notification Routing Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route canonical calendar changes through published organization policies to privacy-safe in-app and email deliveries for the affected technician.

**Architecture:** Extend the existing versioned notification policy, planner, and delivery pipeline. Calendar facts carry stable references only; planning intersects organization policy with recipient preferences, while delivery reauthorizes and renders current details before atomically writing an inbox item or sending email.

**Tech Stack:** Go 1.24, PostgreSQL/pgx, Goose SQL migrations, `net/http`, existing notification workers and SMTP abstraction.

## Global Constraints

- The routable event type is exactly `calendar.schedule_changed`.
- The dynamic recipient reference is exactly `calendar.assignee`.
- Calendar policy destinations permit only `in_app` and `email`; Teams and webhook destinations are rejected.
- No matching organization policy means zero deliveries.
- Recipient preferences may disable an organization-configured channel but may not enable an unconfigured channel.
- Canonical events and delivery payloads persist stable references, not authoritative sensitive display text.
- Delivery reauthorizes the current recipient and every source before rendering.
- Notification failure never rolls back or mutates the originating calendar operation.
- A dedicated frontend notification-center UI is out of scope; authenticated backend inbox APIs are in scope.
- Every production behavior follows an observed red-green-refactor test cycle.

## File Structure

- `backend/internal/notifications/calendar.go`: canonical types, preference rules, and calendar matching helpers.
- `backend/internal/notifications/management.go`: calendar policy publication validation.
- `backend/internal/notifications/planner.go`: policy/preference intersection and typed calendar deliveries.
- `backend/internal/notifications/delivery.go`: delivery authorization, inbox completion, email, and retry outcomes.
- `backend/internal/notifications/inbox.go`: inbox domain model and principal-owned service.
- `backend/internal/store/psa/calendar_repository.go`: transactional canonical facts for accepted scheduling changes.
- `backend/internal/store/psa/calendar_notification_repository.go`: canonical reminder facts.
- `backend/internal/store/psa/notification_repository.go`: event claims, atomic plans/payloads, reauthorization, and inbox persistence.
- `backend/internal/httpapi/notification_inbox_routes.go`: list, unread-count, and mark-read handlers.
- `backend/migrations/000099_calendar_notification_routing.sql`: payload and inbox schema.

---

### Task 1: Calendar policy and preference contract

**Files:**
- Modify: `backend/internal/notifications/calendar.go`
- Modify: `backend/internal/notifications/calendar_test.go`
- Modify: `backend/internal/notifications/management.go`
- Modify: `backend/internal/notifications/management_test.go`
- Modify: `backend/migrations/000095_calendar_notifications.sql`
- Modify: `backend/migrations/calendar_notifications_contract_test.go`

**Interfaces:**
- Produces: `CalendarNotificationEventType`, `CalendarAssigneeRecipientRef`, normalized calendar conditions, and `CalendarPreference.ChannelEnabled`.
- Consumes: existing `Channel`, `PolicyConditions`, `Destination`, and versioned policy publication.

- [ ] **Step 1: Write failing policy and preference tests**

Add table-driven service tests with literal cases for accepted in-app/email destinations and rejected Teams/webhook/static recipients. Add behavior tests for valid condition values, invalid values, duplicate normalized values, explicit preference disablement, and the safe absent-rule default.

```go
func TestCalendarPolicyDestinationsArePrivateAndDynamic(t *testing.T) {
    cases := []struct{name string; channel Channel; ref string; wantErr bool}{
        {"in app", InApp, CalendarAssigneeRecipientRef, false},
        {"email", Email, CalendarAssigneeRecipientRef, false},
        {"teams", Teams, CalendarAssigneeRecipientRef, true},
        {"webhook", Webhook, CalendarAssigneeRecipientRef, true},
        {"static", Email, "technician:fixed", true},
    }
    // Publish each case through PolicyManagementService and compare
    // errors.Is(err, ErrInvalidPolicy) with wantErr.
}
```

- [ ] **Step 2: Run tests and observe the expected failure**

```bash
cd backend
go test ./internal/notifications -run 'TestCalendarPolicy|TestCalendarPreference' -count=1
```

Expected: FAIL because the canonical constants, condition fields, and policy validation do not exist or still accept Teams.

- [ ] **Step 3: Implement the minimal contract**

```go
const (
    CalendarNotificationEventType = "calendar.schedule_changed"
    CalendarAssigneeRecipientRef   = "calendar.assignee"
    CalendarSchedule     CalendarChangeClass = "schedule"
    CalendarPTO          CalendarChangeClass = "pto"
    CalendarConflict     CalendarChangeClass = "conflict"
    CalendarCancellation CalendarChangeClass = "cancellation"
    CalendarReminder     CalendarChangeClass = "reminder"
    CalendarRoutine   CalendarUrgency = "routine"
    CalendarImportant CalendarUrgency = "important"
    CalendarUrgent    CalendarUrgency = "urgent"
)

func (p CalendarPreference) ChannelEnabled(change CalendarChangeClass, urgency CalendarUrgency, channel Channel) bool
```

Add `CalendarChangeClasses []CalendarChangeClass` and `CalendarUrgencies []CalendarUrgency` to `PolicyConditions` with snake-case JSON names. Trim, sort, and deduplicate condition slices. For the canonical event type, require only the dynamic assignee and in-app/email. Align migration 95's preference checks to the five change classes, three urgencies, and two channels.

- [ ] **Step 4: Run focused tests**

```bash
cd backend
go test ./internal/notifications ./migrations -run 'Calendar|Policy' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit only the task files**

```bash
git add backend/internal/notifications/calendar.go backend/internal/notifications/calendar_test.go backend/internal/notifications/management.go backend/internal/notifications/management_test.go backend/migrations/000095_calendar_notifications.sql backend/migrations/calendar_notifications_contract_test.go
git commit -m "feat: validate calendar notification policies"
```

### Task 2: Canonical event and persistence schema

**Files:**
- Create: `backend/migrations/000099_calendar_notification_routing.sql`
- Create: `backend/migrations/calendar_notification_routing_contract_test.go`
- Modify: `backend/internal/store/psa/calendar_repository.go`
- Modify: `backend/internal/store/psa/calendar_repository_test.go`
- Modify: `backend/internal/store/psa/calendar_notification_repository.go`
- Modify: `backend/internal/store/psa/calendar_notification_repository_test.go`
- Modify: `backend/internal/store/psa/calendar_notification_postgres_integration_test.go`

**Interfaces:**
- Produces: canonical outbox rows, `notification_calendar_delivery_payloads`, and `recipient_notifications`.
- Consumes: `schedulingFactID`, scheduling proposal correlation, `event_outbox`, reminder facts, and projection source metadata.

- [ ] **Step 1: Write failing migration and repository tests**

Exercise the migrated schema through PostgreSQL catalogs and real inserts. Require the payload table to be one-to-one with `notification_deliveries`, and the inbox table to be unique by organization/recipient/deduplication key with technician ownership, unread, pagination, and version indexes. Repository tests prove accepted proposal atomicity, reminder normalization, PTO decision projection, newly actionable conflict detection, cancellation/removal detection, and replay suppression.

```sql
CREATE TABLE notification_calendar_delivery_payloads (
  delivery_id uuid PRIMARY KEY REFERENCES notification_deliveries(id) ON DELETE CASCADE,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  recipient_technician_id uuid NOT NULL,
  correlation_id uuid NOT NULL,
  change_class text NOT NULL,
  urgency text NOT NULL,
  source_refs jsonb NOT NULL CHECK (jsonb_typeof(source_refs)='array'),
  action_path text NOT NULL CHECK (action_path LIKE '/%'),
  FOREIGN KEY (recipient_technician_id,msp_id) REFERENCES technicians(id,msp_id)
);
```

- [ ] **Step 2: Run tests and observe the expected failure**

```bash
cd backend
go test ./migrations ./internal/store/psa -run 'CalendarNotificationRouting|CanonicalCalendar|Reminder' -count=1
```

Expected: FAIL because migration 99 and canonical scheduling facts do not exist.

- [ ] **Step 3: Implement schema and canonical facts**

For accepted scheduling changes, insert one `calendar.schedule_changed` outbox fact per affected technician and correlation after all typed mutations succeed but before commit. Its deterministic ID derives from proposal ID, technician ID, and event type. Group every accepted direct/cascade source assigned to that technician. Use `schedule`; map hard/overrideable conflicts to `urgent`, warning conflicts to `important`, otherwise `routine`.

Within `ApplyProjectionBatchAtomic`, compare locked prior projections with the accepted new batch and emit canonical facts in the same projection transaction: `pto` when a PTO source revision changes the technician's projected state, `conflict` only when a projection newly enters an actionable conflict state, and `cancellation` when an active assigned projection becomes terminal or is removed. Use `batch.Cursor.EventID` as the correlation for projection-derived facts and deterministic IDs from source, revision, recipient, and change class. Reconciliation batches without a source event cursor update projections but do not synthesize user-facing change notifications.

Both schedule and reminder facts use this exact structural data shape and never include titles/names/reasons:

```json
{"recipient_id":"uuid","change_class":"schedule","urgency":"routine","source_refs":[{"type":"work_record","id":"uuid","client_id":"uuid-or-empty","event_role":"scheduled","source_revision":4}],"action_path":"/calendar"}
```

Change reminder emission to the canonical event type with `change_class=reminder` and `urgency=routine`.

- [ ] **Step 4: Run focused and PostgreSQL tests**

```bash
cd backend
go test ./migrations ./internal/store/psa -run 'CalendarNotificationRouting|CanonicalCalendar|Reminder' -count=1
```

Expected: PASS, including rollback and replay cases.

- [ ] **Step 5: Commit only the task files**

```bash
git add backend/migrations/000099_calendar_notification_routing.sql backend/migrations/calendar_notification_routing_contract_test.go backend/internal/store/psa/calendar_repository.go backend/internal/store/psa/calendar_repository_test.go backend/internal/store/psa/calendar_notification_repository.go backend/internal/store/psa/calendar_notification_repository_test.go backend/internal/store/psa/calendar_notification_postgres_integration_test.go
git commit -m "feat: emit canonical calendar notification events"
```

### Task 3: Policy-gated calendar planning

**Files:**
- Modify: `backend/internal/notifications/planner.go`
- Modify: `backend/internal/notifications/planner_test.go`
- Modify: `backend/internal/notifications/calendar.go`
- Modify: `backend/internal/notifications/calendar_test.go`
- Modify: `backend/internal/store/psa/notification_repository.go`
- Modify: `backend/internal/store/psa/notification_repository_test.go`
- Modify: `backend/internal/store/psa/calendar_notification_postgres_integration_test.go`

**Interfaces:**
- Consumes: Task 1 policy contract and Task 2 canonical event/schema.
- Produces: calendar fields on `PlanningEvent`, `CalendarDeliveryPayload` on `PlannedDelivery`, and atomic payload persistence.

- [ ] **Step 1: Write failing planner tests**

Test no-policy consumption with zero delivery, calendar condition matching, preference intersection, policy-version deduplication, duplicate claims, malformed canonical data remaining retryable, and one digest for sources sharing recipient/correlation/change/urgency.

```go
type CalendarSourceReference struct {
    Type string `json:"type"`
    ID string `json:"id"`
    ClientID string `json:"client_id,omitempty"`
    EventRole string `json:"event_role"`
    SourceRevision int64 `json:"source_revision"`
}
```

- [ ] **Step 2: Run tests and observe the expected failure**

```bash
cd backend
go test ./internal/notifications ./internal/store/psa -run 'Planner.*Calendar|Calendar.*PlanAtomic' -count=1
```

Expected: FAIL because canonical data is not decoded and typed payloads are not persisted.

- [ ] **Step 3: Implement calendar planning**

```go
type CalendarDeliveryPayload struct {
    CorrelationID string
    ChangeClass CalendarChangeClass
    Urgency CalendarUrgency
    Sources []CalendarSourceReference
    ActionPath string
}
```

Decode only canonical structural fields from `event_outbox.data`. Invalid calendar rows return an error before `notification_event_plans` is inserted. In `Planner.RunOnce`, resolve only `calendar.assignee`, match calendar conditions, intersect policy destinations with `CalendarPreference.ChannelEnabled`, and deduplicate using correlation, recipient, change class, channel, policy ID, and policy version. Persist delivery and typed payload in one `PlanAtomic` transaction. Valid zero-delivery plans still consume the event.

- [ ] **Step 4: Run planner and repository tests**

```bash
cd backend
go test ./internal/notifications ./internal/store/psa -run 'Planner|Calendar.*PlanAtomic|CanonicalCalendar' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit only the task files**

```bash
git add backend/internal/notifications/planner.go backend/internal/notifications/planner_test.go backend/internal/notifications/calendar.go backend/internal/notifications/calendar_test.go backend/internal/store/psa/notification_repository.go backend/internal/store/psa/notification_repository_test.go backend/internal/store/psa/calendar_notification_postgres_integration_test.go
git commit -m "feat: plan policy-gated calendar deliveries"
```

### Task 4: Delivery-time authorization, inbox, and email

**Files:**
- Create: `backend/internal/notifications/inbox.go`
- Create: `backend/internal/notifications/inbox_test.go`
- Modify: `backend/internal/notifications/delivery.go`
- Modify: `backend/internal/notifications/delivery_test.go`
- Modify: `backend/internal/notifications/email.go`
- Modify: `backend/internal/notifications/email_test.go`
- Modify: `backend/internal/store/psa/notification_repository.go`
- Modify: `backend/internal/store/psa/notification_repository_test.go`
- Modify: `backend/internal/store/psa/calendar_notification_postgres_integration_test.go`

**Interfaces:**
- Consumes: Task 3 calendar payload jobs and existing retry lifecycle.
- Produces: delivery-time calendar authorization, atomic in-app completion, calendar email rendering, and inbox service methods.

- [ ] **Step 1: Write failing delivery and inbox tests**

Use real stateful repository doubles and mock only external email transport. Prove current authorized labels, generic `calendar item` rendering after source visibility revocation, inactive/no-longer-relevant suppression, preference disablement, unavailable email, transient retry, 24-hour expiry, atomic inbox completion, and retry convergence.

- [ ] **Step 2: Run tests and observe the expected failure**

```bash
cd backend
go test ./internal/notifications ./internal/store/psa -run 'CalendarDelivery|RecipientInbox|CalendarEmail' -count=1
```

Expected: FAIL because calendar reauthorization and durable inbox completion do not exist.

- [ ] **Step 3: Implement delivery contracts**

```go
type CalendarRenderedDelivery struct {
    Authorized bool
    SuppressionReason string
    RecipientEmail string
    Title string
    Body string
    ActionPath string
}

type RecipientNotification struct {
    ID, RecipientID, DeliveryID, DeduplicationKey string
    Title, Body, ActionPath, ContentClassification string
    CreatedAt time.Time
    ReadAt *time.Time
    Version int64
}

func (s *EmailService) DeliverCalendar(context.Context, CalendarEmail) error
```

Extend `DeliveryJob` with its calendar payload and deduplication key. Calendar jobs use `ReauthorizeCalendarDelivery`; mention jobs retain their existing path. In-app delivery performs one transaction that convergently inserts the inbox row and compare-and-set marks delivered. Email resolves the current address and builds an HTTPS URL from configured public URL plus authorized relative action path. Never persist rendered copy into canonical facts or delivery payloads.

- [ ] **Step 4: Run focused and PostgreSQL tests**

```bash
cd backend
go test ./internal/notifications ./internal/store/psa -run 'CalendarDelivery|RecipientInbox|CalendarEmail' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit only the task files**

```bash
git add backend/internal/notifications/inbox.go backend/internal/notifications/inbox_test.go backend/internal/notifications/delivery.go backend/internal/notifications/delivery_test.go backend/internal/notifications/email.go backend/internal/notifications/email_test.go backend/internal/store/psa/notification_repository.go backend/internal/store/psa/notification_repository_test.go backend/internal/store/psa/calendar_notification_postgres_integration_test.go
git commit -m "feat: deliver calendar notifications privately"
```

### Task 5: Authenticated inbox API and runtime integration

**Files:**
- Create: `backend/internal/httpapi/notification_inbox_routes.go`
- Create: `backend/internal/httpapi/notification_inbox_routes_test.go`
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/cmd/rarity-api/main_test.go`
- Modify: `backend/internal/store/psa/calendar_notification_postgres_integration_test.go`
- Modify: `docs-site/app.js`

**Interfaces:**
- Consumes: Task 4 inbox service/repository and existing authenticated principal resolution.
- Produces: `GET /api/v1/notifications`, `GET /api/v1/notifications/unread-count`, and `PATCH /api/v1/notifications/{id}/read`.

- [ ] **Step 1: Write failing HTTP and bootstrap tests**

Exercise the real router. Test principal-owned list, literal `{"count":2}` unread response, idempotent own-item mark-read, cross-recipient 404, unauthenticated 401, malformed cursor/body 400, and production dependency wiring. Assert that the existing planner/delivery loops are reused rather than adding a calendar-only worker.

- [ ] **Step 2: Run tests and observe the expected failure**

```bash
cd backend
go test ./internal/httpapi ./cmd/rarity-api -run 'NotificationInbox|CalendarNotificationRuntime' -count=1
```

Expected: FAIL because routes and dependency wiring are absent.

- [ ] **Step 3: Implement the service, handlers, and wiring**

```go
type InboxActions interface {
    List(context.Context, authorization.Principal, string, int) (notifications.InboxPage, error)
    UnreadCount(context.Context, authorization.Principal) (int, error)
    MarkRead(context.Context, authorization.Principal, string, int64) (notifications.RecipientNotification, error)
}
```

Clamp list limits to 1..100 with default 50. Use an opaque cursor containing last creation time and ID. Malformed cursors return validation error. Mark-read is idempotent when already read at the supplied version; stale versions use the existing version-conflict response. Cross-organization or cross-recipient access returns `scope.ErrNotFound`. Wire the service through the existing `NotificationRepository`, register routes, and document policy opt-in in `docs-site/app.js`.

- [ ] **Step 4: Run focused end-to-end tests**

```bash
cd backend
go test ./internal/httpapi ./cmd/rarity-api ./internal/store/psa -run 'NotificationInbox|CalendarNotificationRuntime|CalendarNotificationEndToEnd' -count=1
```

Expected: PASS from accepted schedule fact through planning, delivery, and inbox evidence.

- [ ] **Step 5: Run the complete verification matrix**

```bash
cd backend
go test ./... -count=1
cd ../frontend
npm test -- --run
npm run build
cd ..
git diff --check
git status --short
```

Expected: all commands pass and `git diff --check` prints nothing.

- [ ] **Step 6: Commit only the task files**

```bash
git add backend/internal/httpapi/notification_inbox_routes.go backend/internal/httpapi/notification_inbox_routes_test.go backend/internal/httpapi/router.go backend/cmd/rarity-api/main.go backend/cmd/rarity-api/main_test.go backend/internal/store/psa/calendar_notification_postgres_integration_test.go docs-site/app.js
git commit -m "feat: expose calendar notification inbox"
```
