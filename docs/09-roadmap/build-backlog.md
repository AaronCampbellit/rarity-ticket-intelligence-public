# Build Backlog

**Status:** Historical P0–P6 delivery map; current execution is tracked separately

Use the [current execution backlog](current-execution.md) for ordered corrective work,
calendar frontend delivery and qualification. The groups below describe the original
delivery scope; they are not an instruction to rebuild implemented features.

## P0 — Required before feature implementation

1. Define database HA implementation and test harness for primary/synchronous/asynchronous topology.
2. Define migration, outbox, event versioning, and audit-ledger schemas.
3. Build client-isolation fixtures and authorization contract suite.
4. Establish encrypted configuration, secret references, local development safety, CI, artifact provenance, and documentation checks.
5. Define installation wizard UX and setup/help-center content for organization, Entra, intake, object storage, backups, and initial admin.

## P1 — Platform kernel

1. Organization/client/technician/role domain services and global role editor.
2. Optional Entra JIT, local Platform Administrator access, session controls, and audit views.
3. Object/relationship/history/soft-delete APIs and event outbox.
4. PostgreSQL durable records, temporary cache/queue adapter, and S3-compatible/managed storage adapter.

## P2 — Service desk

1. Work-record types, default states/priorities, configurable workflows, and lifecycle actions.
2. Queue triage, routing rules, owner/collaborator semantics, scheduled work/calendar, and notifications.
3. Contracts/SLAs/calendars/time entries/billing CSV export.
4. Public replies versus internal notes, attachment handling, duplicate merge/redirects, record links, and internal knowledge.
5. Technician dashboards, worklists, saved/shared filters, and permission-aware global search.

## P3 — Native PSA sales — source slice implemented

1. Prospects, Prospect-to-Client lineage, duplicate Client matching, and sales Contacts.
2. Multiple administrator-configurable Opportunity pipelines, stage gates, aging, and weighted/committed forecasting.
3. Opportunity activities, attachments, custom fields, and first-class tasks.
4. Immutable Proposal Versions with fixed-fee, time-and-materials, product/license, and recurring-service lines.
5. Rule-driven internal approvals, electronic acceptance, offline acceptance, and immutable accepted PDF snapshots.

## P4 — Native PSA Project delivery — source slice implemented

1. Editable, atomic, idempotent Opportunity conversion with Proposal Line, Phase, budget, hours, Client, and task mappings.
2. Ordered Project Phases, tasks/subtasks, owners, dates, deliverables, and completion criteria without Milestones or dependencies.
3. Phase role/team plans, named task assignments, availability, scheduled work, actual time, and overbooking.
4. Original/current budgets, labor and cost actuals, committed cost, billable work, and Phase/Project profitability.
5. Versioned Change Orders with normal approvals and reason-required audited override under normal update access.

## P5 — Intake, Datto, automation, and AI — source slice implemented

1. Graph notification/retrieval, lifecycle handling, five-minute delta reconciliation, and forwarding intake with secure threading.
2. `/v1`, scoped service keys, inbound/outbound webhooks, and delivery retry.
3. Datto full/incremental asset sync, reconciliation, stale review, alert dedupe/recovery.
4. Typed automation with run history/dead letters.
5. MSP opt-in summaries, reply drafts, and similar-record/knowledge assistance.

## P6 — Release readiness

1. HA failover, backup/PITR, monthly restore verification, quarterly DR exercise.
2. Rolling/maintenance upgrade flows, observability dashboard, and operational runbooks.
3. Scale, security, accessibility, migration, upgrade, and recovery acceptance tests.
