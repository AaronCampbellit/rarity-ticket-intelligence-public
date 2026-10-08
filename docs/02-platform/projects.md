# Projects

**Status:** Implemented source contract; constrained demo PostgreSQL acceptance complete

Projects are first-class native PSA objects, not collections of disguised tickets or mutable Opportunities. They are created directly or through the audited conversion of an accepted Proposal.

## Structure

A Project contains ordered Phases. Rarity does not introduce a separate Milestone concept. Phases carry owners, participating teams, lifecycle state, planned and actual dates, budgets, planned hours, deliverables, and completion criteria.

Project tasks belong to a Phase and may contain subtasks. Task dependencies are not supported. Work records may relate to Projects without becoming Project tasks.

## Commercial baseline and conversion

An editable conversion preview maps the accepted Proposal Version into Client, Project, Phase, scope, budget, planned-hour, resource, and task records. Conversion is atomic and idempotent. A failed conversion creates no partial records.

The accepted Proposal Version becomes the immutable original commercial baseline. The Opportunity remains as read-only sales history linked to the resulting Project.

## Resources and financials

Resource planning supports Phase-level role/team hours, task-level named assignments, availability, scheduled work, actual time, and overbooking.

The Project workspace derives capacity for its named technicians across the
Project's planned date window. Available minutes come from MSP-wide exact
technician availability settings; scheduled minutes aggregate dated
Project/Phase task assignments across the MSP; actual minutes use clipped
delivery time entries. Only aggregate capacity is returned under
`project.resource.plan`, so another Client's work details are never disclosed,
and the calculation never mutates plans automatically.

Project and Phase tasks accept direct time entries through the shared native
time model. Captured duration is visible as task and Phase actual minutes.
Billable classification remains evidence only; it does not manufacture a labor
rate, recognized revenue, or an invoice.

Authorized users record exact non-labor costs against the Project or one of its
Phases. Each entry identifies its type, description, amount, currency, date,
and whether it is an incurred actual or a committed cost. These source-backed
inputs are audited and visible only with Project financial-read access.

Authorized Project users append recognized billable-work evidence against the
Project or an optional Phase. Recognition records an exact amount, currency,
description, date, actor, audit fact, and outbox event; it does not create an
invoice or imply payment. Actual labor cost is rebuilt from delivery Time
Entries and immutable effective-dated technician cost rates.

Initial Project financials include original and current budget, planned and
actual labor, cost actuals, committed cost, recognized billable work, and
profitability. Project and Phase summaries use persisted inputs and reject
mixed currencies. Phase summaries include only evidence explicitly attributed
to that Phase; Project-wide entries are not allocated heuristically. Missing
labor rates make actual labor and recognized profit visibly unavailable.
Native invoices, payments, and accounting synchronization follow in a later
PSA phase.

The Project read model calculates the available financial summary on the
server from the immutable original/current baselines and persisted Project
costs, recognized work, rated Time Entries, and Phase attribution. It returns
Project and Phase summaries only under `project.financial.read`; the browser
does not reconstruct financial values from independently loaded records.

## Change Orders

Versioned Change Orders alter current scope, price, or planned hours without rewriting the original baseline. Normal internal and customer approvals are available. A user whose normal Project access permits Change Order updates may override approval without a distinct override permission; the override requires a reason and preserves actor, timestamp, exact version, previous state, and immutable audit history.

## Implementation evidence

The current source includes atomic and idempotent conversion, ordered Phases,
Project task lineage, role/team capacity plans, audited Project/Phase cost
inputs, financial calculations over authoritative inputs, immutable Change
Order Versions, append-only decision/application evidence, and apply-once
baseline updates. Component and browser acceptance verifies the Project
delivery, capacity, financial, and reasoned override workflows with synthetic
fixtures.

See [Native PSA Opportunities and Projects API](../04-api/psa-opportunities-projects.md) and [Native PSA Acceptance](../06-development/psa-acceptance.md). Native invoicing, payments, accounting synchronization, and PostgreSQL-backed acceptance are not complete.

## Calendar workspace

See the [unified calendar contract](unified-calendar.md) for shared scheduling, typed source forms, date semantics, capacity, and current implementation evidence.
