# Native PSA Opportunities and Projects API

**Status:** PostgreSQL PSA actions and validated active-Client identity composed

The native PSA API exposes scoped sales-to-delivery mutations under `/api/v1`. The application resolves the authenticated principal at the trusted HTTP boundary; callers cannot select an MSP or Client scope in request bodies.

Browser session requests for Client-scoped actions send `X-Rarity-Client-ID`. The server trims and validates the selected Client against the authenticated session's MSP, requires the Client to be active, and loads only unexpired MSP-global plus matching Client role capabilities. Missing, malformed, inactive, cross-MSP, or unauthorized contexts fail closed. MSP-global Prospect and Pipeline actions omit the header. Service API keys retain their configured Client scope and reject a conflicting Client header.

All request bodies are JSON, size-limited to 1 MiB, reject unknown fields, and must contain exactly one JSON value. Versioned mutations return an `ETag`. Requests that support `expected_version` use that value as their optimistic concurrency condition; Opportunity transitions may instead supply `If-Match`.

## Routes

| Action | Endpoint | Success | Concurrency and replay |
|---|---|---:|---|
| Create Prospect | `POST /api/v1/prospects` | `201` | New record |
| Create Pipeline | `POST /api/v1/pipelines` | `201` | New record |
| Transition Opportunity | `PATCH /api/v1/opportunities/{id}` | `200` | `expected_version` or `If-Match` |
| Replace Opportunity custom fields | `PUT /api/v1/opportunities/{id}/custom-fields` | `200` | `expected_version` or `If-Match` |
| Replace Opportunity participants | `PUT /api/v1/opportunities/{id}/participants` | `200` | `expected_version` or `If-Match` |
| List Opportunity attachments | `GET /api/v1/opportunities/{id}/attachments` | `200` | Bounded scoped metadata |
| Upload Opportunity attachment | `POST /api/v1/opportunities/{id}/attachments` | `201` | New immutable metadata and object |
| Issue Proposal Version | `POST /api/v1/proposals/{id}/versions` | `201` | Proposal `expected_version` |
| Accept Proposal Version | `POST /api/v1/proposal-versions/{id}/accept` | `201` | Exact immutable Proposal Version |
| Preview conversion | `POST /api/v1/opportunities/{id}/conversion-preview` | `200` | Opportunity `expected_version`; returns deterministic preview hash |
| Convert Opportunity | `POST /api/v1/opportunities/{id}/convert` | `201` or replay `200` | Exact Opportunity version, preview hash, and idempotency key |
| Create Project | `POST /api/v1/projects` | `201` | New record linked to original Proposal Version |
| Create Change Order draft | `POST /api/v1/projects/{id}/change-orders` | `201` | New scoped draft with version `1` |
| Issue Change Order Version | `POST /api/v1/change-orders/{id}/versions` | `201` | Change Order `expected_version` |
| Approve Change Order Version | `POST /api/v1/change-order-versions/{id}/approve` | `200` | Change Order `expected_version` |
| Override Change Order approval | `POST /api/v1/change-order-versions/{id}/override-approval` | `200` | Change Order `expected_version`; nonblank reason required |
| Apply Change Order Version | `POST /api/v1/change-order-versions/{id}/apply` | `200` | Change Order `expected_version`; approved version applies once |

Electronic Proposal acceptance requires an opaque acceptance grant bound to the exact Proposal Version. A trusted verifier derives signer identity, acceptance time, evidence, and grant identity from that grant; request-body signer fields cannot establish customer identity. Missing, invalid, expired, replayed, or mismatched grants fail closed. Offline acceptance records the acting authenticated principal as the recorder. Conversion requires one explicit Client choice: match an existing Client or create one from the Prospect. Every Proposal Line must map exactly once, and only selected incomplete Opportunity Tasks move.

The conversion response contains `project_id`, `client_id`, `conversion_id`, and `already_exists`. A replay with the same idempotency contract returns the original identifiers with `already_exists: true`; conflicting or stale inputs do not create partial records.

The authenticated AI workspace has a separate direct-Project application
service rather than relaxing the public Project route. After an exact preview
and explicit confirmation, it atomically creates one `planned` Project and the
explicitly supplied `open` task titles. It requires both `project.create` and
`task.create`, records AI source and correlation evidence, and stores no
Proposal Version lineage. All identifiers are server-generated; omitted
business fields remain unset.

The AI workspace message route recognizes the explicit form “Create a project
titled NAME for CLIENT with tasks A, B, C.” `NAME`, `CLIENT`, and every task
title come from the message. The named Client must match the authenticated
active Client; the route does not search or switch Client scope. Missing
business data returns a clarification and creates no proposal. A complete
request returns the normal assistant messages plus an optional `proposal`
object backed by the same `project.create` tool and confirmation boundary.
Other messages retain the curated product-help path.

The same message route recognizes the bounded standalone-Task form “Add a task
titled TITLE to project PROJECT for CLIENT.” The named Client must be the
active Client. `PROJECT` is resolved server-side within that Client by an exact
normalized Project name or display ID; no match or more than one match returns
a clarification and creates no proposal. The resulting `task.create` preview
contains only the resolved Project, the supplied title, and the ordinary
`open` status. Owner, estimate, description, due date, and other optional
business values remain unset. Confirmation invokes the ordinary Task
application service with the authenticated principal and rechecks the Project
scope and version. Phase tasks, subtasks, assignments, estimates, status
changes, and Task updates remain outside this command until ordinary
application-service contracts support them.

## Errors

Errors use a stable JSON envelope:

```json
{
  "error": {
    "code": "version_conflict",
    "message": "resource changed; refresh and retry"
  }
}
```

| HTTP | Code | Meaning |
|---:|---|---|
| `401` | `unauthenticated` | No trusted principal resolved |
| `403` | `forbidden` | Principal lacks the required capability |
| `404` | `not_found` | Missing or out-of-scope resource; scope failures do not enumerate another Client |
| `409` | `version_conflict` | Stale object version or conversion preview |
| `409` | `already_converted` | Opportunity was converted under a different request contract |
| `422` | `validation_failed` | Invalid shape, mapping, state, acceptance evidence, or approval reason |
| `500` | `internal_error` | Request could not be completed |
| `501` | `not_implemented` | Route is present but its application service is not composed |

## Durable event families

Successful mutations write their audit and canonical outbox facts with the durable state change. The implemented schema-version-1 families are:

- `prospect.created`
- `pipeline.created`
- `opportunity.stage.changed`
- `opportunity.custom_fields.replaced`
- `opportunity.participants.replaced`
- `proposal_version.issued`
- `proposal.accepted`
- `opportunity.converted`
- `project.created`
- `phase.updated`
- `resource_plan.created`
- `change_order.version.issued`
- `change_order.approval.decided`
- `change_order.applied`

Every canonical event carries event identity, schema version, occurrence time, MSP and optional Client scope, actor, subject identity/version, source, and correlation identity. Public API mutations derive `source: api` at the server boundary; callers cannot supply an audit-source label. Consumers must treat delivery as at least once and deduplicate by event identity.

## Runtime composition boundary

The runnable process now composes PostgreSQL-backed Prospect and Pipeline creation, optimistic Opportunity stage transition, Proposal issuance and acceptance, immutable Proposal snapshots, exact-bound single-use electronic acceptance grants, Project/Phase creation, atomic Opportunity conversion, and Change Order issue/decision/application. Durable mutations write audit and outbox facts in the same transaction. Prospect conversion preserves lineage and contact data, and project-level original/current budgets are uniquely constrained. Opaque sessions resolve MSP-global or explicitly selected Client role capabilities, and scoped `rsk_` service keys retain their configured Client and data-scope boundaries. Constrained demo PostgreSQL acceptance is recorded in [Native PSA Acceptance](../../docs/06-development/psa-acceptance.md). This document does not claim native invoicing, payments, accounting synchronization, supported-profile qualification, or production acceptance.
