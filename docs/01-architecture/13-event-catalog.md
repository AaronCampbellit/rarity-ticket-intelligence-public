# Event Catalog and Envelope

**Status:** Proposed convention\
**Version:** 0.1\
**Last updated:** 2026-07-25

## Canonical envelope

```json
{
  "event_id": "019f9a10-6c52-7e4e-9d45-8d5cab7a94f3",
  "event_type": "work_record.owner.changed",
  "schema_version": 1,
  "occurred_at": "2026-07-25T19:42:16.318Z",
  "published_at": "2026-07-25T19:42:16.401Z",
  "msp_id": "019f99d1-7f09-7e59-9317-e40626f1dad7",
  "client_id": "019f99dd-10a5-755e-b4a4-7db083259778",
  "actor": {
    "type": "technician",
    "id": "019f99e3-c0ab-7a42-a820-60f4d4bc8f52"
  },
  "subject": {
    "type": "incident",
    "id": "019f99f2-ee51-74fb-8f11-29cc43b2d019",
    "version": 12
  },
  "correlation_id": "019f9a10-62b4-77df-bfba-48e8f82c45ef",
  "causation_id": "019f9a10-6211-7fd4-817b-8df5a71d8aed",
  "source": "web",
  "data": {
    "previous_owner_id": null,
    "owner_id": "019f99e3-c0ab-7a42-a820-60f4d4bc8f52"
  }
}
```

IDs above are illustrative only.

## Naming

Event types use lowercase dot-separated domain facts in past tense: `work_record.created`, `workflow.selected`, `comment.added`, `automation.run.failed`, and `integration.connection.disabled`. Commands and requests are not published as facts.

## Initial event families

- `organization.*`, `client.*`, `location.*`, `contact.*`
- `technician.*`, `membership.*`, `role.*`, `session.*`
- `work_record.*`, `comment.*`, `attachment.*`, `time_entry.*`
- `prospect.*`, `pipeline.*`, `opportunity.*`, `proposal.*`, `proposal_version.*`
- `project.*`, `phase.*`, `resource_plan.*`, `project_financial.*`, `change_order.*`
- `workflow.*`, `approval.*`, `sla.*`
- `automation.*`, `automation.run.*`
- `asset.*`, `service.*`, `relationship.*`, `impact.*`
- `integration.*`, `inbound_event.*`, `webhook.delivery.*`, `email.*`
- `backup.*`, `restore.*`, `platform.health.*`, `security.*`
- `ai.recommendation.*`
- `mention.occurred`

## `mention.occurred` version 1

`mention.occurred` is emitted once per committed internal source mutation that
adds at least one structured target. Its subject is the
`internal_collaboration_source`; the envelope is Client-scoped. `data` is:

```json
{
  "idempotency_key": "caller-stable-key",
  "source_id": "uuid",
  "source_revision": 4,
  "parent_type": "work_record | task | project",
  "parent_id": "uuid",
  "source_kind": "details | comment | note",
  "lifecycle_state": "active",
  "recipient_ids": ["technician-uuid"],
  "latest_occurrence_by_recipient": {
    "technician-uuid": "occurrence-uuid"
  }
}
```

The collaboration repository adds `idempotency_key`, `source_kind`, and the
committed `lifecycle_state` to the service-prepared event inside the same
transaction. They are routing and replay metadata only; no content is copied
into the event.

Recipient IDs are sorted. The event contains no source body, preview, rendered
label, token object, token offsets, or text surrounding a token. Consumers use
the occurrence and recipient resolution tables, then reauthorize current
state. Loss-capable role, technician-status, Client/object-transfer,
project-visibility, deletion, and redaction events create one compact scoped
mention-loss marker at the transaction's MSP authorization revision. Grant-only
role assignment and team-membership changes create no loss marker. A bounded
consumer expands markers against items and temporal role facts; events do not
directly restore or expose a projection.

Expired assignments emit `role.unassigned` with
`data.reason=assignment_expired` only after the worker opens a new MSP
authorization revision. Revision history records the exact temporal boundary;
expiry therefore follows the same fail-closed marker path as an explicit
revocation and cannot silently revive an old occurrence after regrant.

## `work_record.merged` version 1

The subject is the tombstoned duplicate work record. Its content-free `data`
contains `winner_id`, the canonical work record receiving reparented tasks and
other children. Consumers use that ID to reconsider dependent objects after
the merge transaction has already moved them; they must still reauthorize
current state before acting.

## Data policy

Event data contains the minimum useful facts, not complete object snapshots by default. Secrets, authentication material, internal notes, attachment bodies, raw email, and sensitive custom-field values never appear in general platform events. Consumers fetch authorized current state when needed.

## Delivery contract

State and outbox are committed atomically. Delivery is at least once and unordered across independent subjects. Consumers deduplicate on `event_id`, tolerate unknown additive fields, and use subject version when ordering matters. Retries are bounded and observable; poison events enter a dead-letter path.

## Compatibility

Additive optional fields may retain the same schema version. Removing, renaming, changing meaning/type, or tightening a previously valid requirement produces a new version. Producers support a declared compatibility window, and external webhook subscriptions choose supported versions.

## Approval gate

This convention remains proposed until canonical error, pagination, and object examples are reviewed together and contract tests are defined.

## Calendar and typed source facts

The [unified calendar contract](../02-platform/unified-calendar.md) describes source ownership, projections, privacy, and scheduling confirmation. Typed source changes and scheduling transactions retain correlated audit/outbox evidence. Relevant event names include:

- `calendar.conflict_policies.replaced`
- `calendar.custom_date_field.upserted`
- `calendar.notification_preferences.replaced`
- `calendar.schedule_changed`
- `calendar.dependency.created`
- `calendar.dependency.deleted`
- `commercial.commitment.created`
- `commercial.commitment.updated`
- `commercial.commitment.transitioned`
- `custom_date.set`
- `maintenance.window.created`
- `maintenance.window.transitioned`
- `maintenance.window.updated`
- `project.milestone.created`
- `project.milestone.transitioned`
- `project.milestone.updated`
- `pto.approved`
- `pto.rejected`
- `technician.schedule.published`
- `technician.schedule.exception.added`
- `pto.cancelled`
- `pto.requested`

Calendar notification payloads remain recipient- and source-authorized; they are not a general source-data broadcast.
