# Work Records and Tickets

**Status:** Draft

One shared work-record foundation supports Incident, Service Request, Problem, and Change in V1. Alerts are Incidents with alert-source metadata; maintenance, security, onboarding, and procurement use classifications, templates, and workflows.

The runnable API now composes Client-scoped shared Work Record creation, versioned deterministic automatic routing, immutable workflow selection and governed status transitions, optimistic primary ownership, versioned technician participation for collaborators, reviewers, escalation engineers, and watchers, reasoned manual transfer to MSP-global or matching Client queues, reason-required duplicate merge with redirect tombstones and operational child-history preservation, explicitly separated internal/client-visible comments, streamed checksum-verified attachments, exact-duration billable/non-billable time capture, registry-governed same-Client relationships, and ordered Tasks/subtasks. Each action uses capability authorization, PostgreSQL persistence, and atomic audit/outbox evidence.

All types share IDs, client context, routing, primary owner, collaborators, watchers, status, priority, impact, urgency, SLA, comments, notes, attachments, time, tags, custom fields, relationships, audit, versioning, permissions, APIs, events, search, reporting, and AI assistance.

The composed relationship mutation accepts only registry-approved same-Client edges. Both endpoints are existence- and lifecycle-checked in the mutation transaction; symmetric Work Record `related_to` edges are canonicalized, and Incident-to-Problem and Change implementation links enforce their Work Record subtypes.

Core search is composed as an explicitly Client-scoped, bounded projection over active Work Records and operational Client resources. Comments and attachment metadata remain excluded until their separate visibility capabilities can be enforced per result.

Types add fields and workflow behavior: incidents track outage and impact; requests use catalog forms and fulfillment; problems track root cause and known errors; changes include risk, implementation, rollback, approvals, and maintenance windows; alerts include source IDs, occurrence counts, raw payload references, and correlation.

Workflow configuration now has a public publish boundary for initial and replacement immutable versions. Publication rejects missing/disabled/conditional fallbacks, ambiguous enabled ordering, cross-Client conditions, invalid state graphs, and stale replacement versions before durable audit/outbox evidence is written.

Built-in types are protected definitions. MSPs may clone or create types from shared fields, forms, workflow, permissions, SLA, and automation. See the [V1 Service Desk Operating Model](../01-architecture/v1-service-desk-operating-model.md).

## Calendar workspace

See the [unified calendar contract](unified-calendar.md) for shared scheduling, typed source forms, date semantics, capacity, and current implementation evidence.
