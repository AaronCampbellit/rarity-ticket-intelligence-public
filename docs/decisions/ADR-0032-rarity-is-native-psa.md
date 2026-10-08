# ADR-0032: Rarity is the native PSA system of record

**Status:** Accepted\
**Date:** 2026-07-28

## Decision

Rarity is the PSA system of record, not an integration overlay for another PSA. Opportunities, Proposal Versions, approvals, Projects, Phases, resource plans, Project financials, and Change Orders are native first-class domains.

The commercial lifecycle is Opportunity to Proposal/Approval to Project. Conversion is previewable, atomic, idempotent, and preserves the accepted Proposal Version as the immutable original Project baseline. Selected incomplete Opportunity tasks retain identity and history when moved to the Project.

Projects use ordered Phases and subtasks. They do not introduce Milestones or task dependencies. Initial Project financials include budget, labor/cost actuals, committed cost, billable work, and profitability. Invoicing, payments, and accounting synchronization are deferred.

Versioned Change Orders support normal approvals and a reason-required audited approval override. The override uses normal Project/Change Order update authorization and does not introduce a dedicated override permission.

## Consequences

- Sales and Project delivery are separate native domains joined through explicit relationships and durable events.
- Existing Ticket Intelligence remains a PSA delivery module rather than the whole product identity.
- Architecture, permissions, APIs, search, reporting, navigation, and roadmap must treat Opportunities and Projects as core platform capabilities.
- External CRM, accounting, and PSA integrations may exchange data but do not own Rarity's authoritative Opportunity or Project state.
- Native invoicing and accounting work must extend the accepted commercial and Project financial records rather than create a parallel financial model.
