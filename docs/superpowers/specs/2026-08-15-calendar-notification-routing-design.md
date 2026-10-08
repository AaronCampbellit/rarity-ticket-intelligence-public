# Calendar Notification Routing Design

**Date:** 2026-08-15

**Status:** Approved design direction; implementation pending

**Scope:** Organization-controlled routing of calendar changes to the affected technician by in-app notification and email.

## Outcome

Accepted calendar changes and due reminders produce durable, privacy-safe notifications only when an organization's published notification policy enables them. Recipient preferences may narrow those configured channels. Notification planning and delivery are asynchronous and never change the success or failure of the calendar operation that caused them.

This slice completes the routing and delivery boundary for calendar notifications. It does not add Teams delivery, a new policy subsystem, or AI-generated notification copy.

## Decisions

- Reuse the existing versioned organization notification policies and delivery jobs.
- Use the event type `calendar.schedule_changed` for routable calendar changes.
- Route calendar policy destinations through the dynamic recipient reference `calendar.assignee`.
- Permit only `in_app` and `email` calendar destinations. Reject Teams and webhook destinations at policy publication.
- Treat missing organization policy as explicit opt-out: the event is consumed with zero deliveries.
- Treat recipient preferences only as a channel disablement layer. A preference cannot enable a channel absent from organization policy.
- Persist a general recipient notification inbox so an `in_app` delivery has a durable, queryable result.
- Reauthorize and re-render source details at delivery time instead of trusting stale, pre-rendered event text.
- Group changes sharing a calendar correlation ID into one recipient digest where possible, including cascade changes from a single accepted proposal.

## Alternatives Considered

### Separate calendar routing settings

Rejected because it would duplicate policy lifecycle, audit, priority, publication, and delivery semantics. Calendar-specific conditions fit within the existing policy model.

### Shared Teams webhook delivery

Deferred. The current Teams connection is organization-wide and cannot guarantee a private message to the affected technician. Enabling it would violate recipient-specific delivery expectations.

### Default delivery when no policy matches

Rejected. Organization notification policy is the authoritative delivery gate, so silence is the safe and predictable default.

## Policy Contract

The existing versioned policy schema remains authoritative. `PolicyConditions` gains optional calendar-specific fields:

- `calendar_change_classes`: any of `schedule`, `pto`, `conflict`, `cancellation`, or `reminder`.
- `calendar_urgencies`: any of `routine`, `important`, or `urgent`.

For event type `calendar.schedule_changed`:

- Every destination channel must be `in_app` or `email`.
- Every destination must use recipient reference `calendar.assignee`.
- Existing organization/client scope, priority, effective period, publication state, quiet period, stable ordering, and content-classification rules continue to apply.
- An empty calendar-specific condition matches all values for that condition.
- Unknown change classes, urgencies, channels, or recipient references make policy publication fail validation.
- Duplicate destinations within a policy are rejected after normalization.

Policy evaluation uses the immutable published policy version captured during planning. Editing a policy creates a new version and does not rewrite already planned deliveries. Delivery still applies current recipient eligibility and preferences as a safety layer.

## Canonical Calendar Event

All routable sources normalize to a canonical event with event type `calendar.schedule_changed`. The envelope contains:

- stable event ID;
- organization ID;
- correlation ID;
- occurred-at timestamp;
- change class and urgency;
- affected technician ID;
- stable source references and source revisions;
- action link parameters;
- non-sensitive structural facts needed for digesting, such as change count and time range.

The event does not contain authoritative rendered titles, client names, ticket subjects, PTO reasons, or other sensitive display strings. Those values are resolved through authorized repositories at delivery time.

The canonical sources are:

- an accepted schedule proposal, including all direct and cascade changes in one correlated event per affected technician;
- an approved or rejected PTO request when it changes the technician's calendar state;
- a newly detected actionable conflict;
- a cancellation that removes or invalidates scheduled work;
- a due `calendar.reminder` occurrence.

Event creation is transactionally coupled to the successful domain mutation when the source command owns the transaction. When a fact can only be recognized after projection, the projection transaction inserts the canonical event alongside its durable read-model update. A failed or rolled-back schedule mutation cannot emit a canonical event.

Event IDs are deterministic from organization, source kind, source identifier, source revision, affected technician, and normalized change class. Replaying a command or projection therefore converges on the same event. Correlation IDs group all changes produced by one accepted user or system action.

## Planning Flow

The calendar planning worker claims unplanned canonical events and performs these steps:

1. Load and validate the affected technician and event references in the event's organization.
2. Convert the event to the existing `AppliedScheduleEvent`/calendar planning model.
3. Match published `calendar.schedule_changed` policies using existing precedence plus the new calendar conditions.
4. Resolve `calendar.assignee` to the affected technician.
5. Intersect policy destinations with current recipient channel preferences.
6. Create ordinary notification delivery jobs and their typed calendar payload metadata in the same transaction that records the event plan.
7. Mark a valid event planned even when no policy matches or all channels are disabled.

The typed calendar delivery metadata is stored in a child table keyed one-to-one by notification delivery ID. It contains only stable source references, source revisions, change class, urgency, correlation ID, and action-link parameters. It does not duplicate mutable technician email addresses or sensitive rendered copy.

The delivery deduplication key contains organization, canonical correlation, affected technician, normalized change class, channel, policy ID, and policy version. Reclaiming or replaying an event cannot create a second equivalent job. Separate published policy destinations may still intentionally produce separate jobs.

Planning uses the existing claim timeout and compare-and-set behavior. A worker crash before the atomic planning transaction leaves the event reclaimable; a crash after commit sees the completed plan and cannot duplicate it.

## Delivery-Time Authorization and Rendering

Before sending either channel, the delivery worker:

- verifies the recipient technician is active and belongs to the organization;
- verifies the recipient is still relevant to the calendar event;
- reapplies the recipient's current preference for the channel;
- loads every referenced source through the source-specific authorization boundary;
- renders display text from only the sources the recipient may currently view;
- uses the recipient's current email address for email delivery.

If the recipient remains eligible but a referenced source is no longer visible, that item is rendered generically as a `calendar item`; inaccessible titles, client names, subjects, and reasons are omitted. If the recipient is inactive, is no longer the affected assignee, or cannot receive any meaningful part of the event, the delivery is suppressed with an auditable terminal reason.

Quiet-period and retry behavior remains channel-specific and asynchronous. A missing or invalid technician email is a terminal `channel_unavailable` suppression, not a retryable transport failure. Transport and transient provider failures use the existing bounded retry mechanism.

No planning, rendering, inbox, or transport failure rolls back or alters the originating calendar mutation.

## In-App Inbox

An `in_app` delivery atomically creates a durable `recipient_notifications` record and marks the delivery delivered. The record includes:

- notification ID, organization ID, and recipient technician ID;
- source event ID and delivery deduplication key;
- rendered title, body, action link, and content classification;
- creation timestamp;
- nullable read timestamp;
- row version for optimistic updates.

The inbox record is immutable except for read state. A unique constraint on organization, recipient, and delivery deduplication key prevents duplicate visible notifications if delivery is retried around a worker failure.

Authenticated API routes provide:

- a cursor-paginated list scoped to the current principal;
- unread count;
- an idempotent mark-read operation protected by organization and recipient ownership.

This implementation slice includes the backend inbox contract and APIs. A dedicated frontend notification-center experience is a follow-up; existing product surfaces may consume the API without changing the routing contract.

## Email Contract

Email delivery uses the existing provider abstraction and retry lifecycle with a calendar-specific message renderer. The subject identifies the kind of calendar change without exposing restricted source data. The body contains the authorized digest, time range, urgency where relevant, and a product link.

Email address resolution occurs immediately before the provider call. Provider idempotency uses the notification delivery deduplication key so an ambiguous retry does not intentionally create a second message where the provider supports idempotency.

## Persistence and Migration

The migration adds:

- calendar-specific policy condition fields using the existing serialized/versioned condition representation;
- the canonical calendar event data needed by the planning worker, reusing the existing notification event/outbox boundary where practical;
- `notification_calendar_delivery_payloads`, keyed by notification delivery ID;
- `recipient_notifications` with ownership, deduplication, pagination, unread, and optimistic-read indexes.

Backfill is not required for historical calendar changes. Existing published policies remain valid and do not begin sending calendar notifications unless they explicitly target `calendar.schedule_changed` with valid calendar destinations.

Migration constraints enforce organization ownership and one-to-one delivery payload cardinality. Repository transactions enforce that the canonical event, plan marker, delivery, payload, and inbox transitions cannot become partially visible within their respective stages.

## Failure and Audit Semantics

- Invalid calendar policies cannot be published and return field-specific validation errors.
- No matching policy, preference-disabled channels, unavailable channels, lost eligibility, and lost authorization are recorded as distinct outcomes.
- Planning failures remain retryable without consuming the event.
- Terminal suppression consumes only the affected delivery.
- Delivery attempts retain the policy ID/version and canonical event/correlation IDs for audit.
- Logs and audit records use stable identifiers and reason codes; they do not log sensitive rendered notification bodies.

## Verification

Unit tests cover:

- calendar policy condition and destination validation;
- policy precedence and matching by change class and urgency;
- organization-policy and recipient-preference intersection;
- no-policy and all-channels-disabled zero-delivery behavior;
- canonical event ID and delivery deduplication stability;
- cascade grouping by correlation;
- generic rendering for sources whose visibility was revoked;
- suppression for inactive or no-longer-relevant recipients;
- current email resolution, unavailable email, retryable provider failures, and idempotency keys;
- in-app inbox deduplication, pagination, unread count, mark-read idempotency, and ownership enforcement.

PostgreSQL integration tests cover:

- accepted schedule mutation and canonical event atomicity;
- projection-derived event atomicity;
- claim recovery and concurrent planning without duplicate deliveries;
- policy-version capture and delivery payload persistence;
- atomic inbox insertion and delivery completion;
- cross-organization isolation for events, deliveries, and inbox records;
- migration upgrade from the current schema.

HTTP tests cover authenticated inbox listing, unread count, mark-read ownership, malformed cursors, and cross-organization denial. The full backend suite, frontend suite, production build, migration contract tests, and PostgreSQL integration suite must pass before completion.

## Acceptance Criteria

- An accepted eligible calendar change creates exactly one canonical event per affected technician and correlation group.
- With no matching published organization policy, it creates no delivery.
- A matching policy creates only its configured `in_app` and/or `email` deliveries, further narrowed by recipient preferences.
- Teams and webhook calendar destinations cannot be published.
- Delivery never exposes source data the recipient cannot view at send time.
- In-app delivery yields one durable principal-owned inbox item.
- Email uses the recipient's current valid address and the normal retry lifecycle.
- Replays, worker crashes, and concurrent claims do not create duplicate visible notifications.
- Notification failures never undo or corrupt calendar state.
