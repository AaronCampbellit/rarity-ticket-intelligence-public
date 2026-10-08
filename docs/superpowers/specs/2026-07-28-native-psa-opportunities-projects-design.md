# Native PSA Opportunities and Projects Design

**Status:** Approved design
**Date:** 2026-07-28

## Purpose

Rarity is the PSA system of record. Opportunities, proposals, approvals, Projects, delivery planning, time, costs, and profitability are native Rarity capabilities rather than an overlay on an external PSA.

This design defines the first Opportunities and Projects release. Native invoicing, payment collection, and accounting synchronization follow in a later PSA phase.

## Domain boundaries

Rarity implements the PSA as coordinated first-class domains on the shared platform:

- **Sales:** Prospect, Opportunity, Pipeline, Stage, Activity, Proposal, Proposal Version, Proposal Line, and Approval.
- **Delivery:** Project, Phase, Project Task, Subtask, Assignment, Time Entry, Change Order, Budget, and Cost Actual.
- **Shared PSA foundation:** Client, Contact, User, Team, service and product catalog, attachment, comment, audit history, custom field, workflow rule, notification, and reporting.

These entities use the common Rarity object, authorization, client-isolation, concurrency, audit, event, search, retention, and API contracts. Opportunities and Projects are not Ticket subtypes and do not share one mutable commercial record.

Tickets and time entries may relate to Opportunities or Projects through first-class relationships without becoming Opportunity or Project tasks.

## Opportunity pipelines

Administrators may create multiple Opportunity pipelines, including New Business, Existing Client Expansion, and Renewal.

Each pipeline defines:

- configurable stages and allowed transitions;
- probability and forecast classification;
- required fields;
- aging targets;
- quote requirements; and
- internal or customer approval requirements.

Opportunities support a Prospect or existing Client, Contacts, owner and team, expected close date, forecast category, expected value, activities, notes, attachments, custom fields, and tasks.

Weighted and committed revenue forecasts are derived from pipeline stage configuration and accepted commercial records. Forecast projections are rebuildable and are not authoritative financial records.

## Prospects and Clients

An Opportunity may begin with a lightweight Prospect instead of a full Client. When a Prospect Opportunity is converted, the conversion preview creates or matches the Client and carries forward Contacts, activity, attachments, and history.

The conversion must prevent accidental duplicate Clients and must preserve the Prospect-to-Client lineage.

## Proposals and approvals

A Proposal contains one or more immutable Proposal Versions. Editing an issued Proposal creates a new version; previous versions and their approval outcomes remain available.

Proposal lines support:

1. fixed-fee services;
2. time-and-materials labor;
3. products and licenses; and
4. recurring services.

Lines may include quantity, unit cost, unit price, discount, margin, tax treatment, and planned hours where applicable.

Configurable internal approval rules may evaluate value, discount, margin, or risk. Customer acceptance supports:

- built-in electronic acceptance; and
- manually recorded offline acceptance.

Both acceptance methods preserve the named signer, actor recording the acceptance when applicable, timestamp, method, exact Proposal Version, and an immutable PDF snapshot.

## Opportunity tasks

Opportunity tasks are first-class Tasks. They support ownership, estimates, attachments, comments, activity history, and other common Task behavior.

During conversion, the user selects which incomplete Opportunity tasks become Project tasks. Selected tasks retain their identity, ownership, estimates, attachments, comments, and history. Completed sales tasks remain on the Opportunity as sales history. Unselected incomplete tasks also remain on the Opportunity unless the user closes or cancels them separately.

## Conversion into a Project

An accepted Proposal may be converted through an editable preview. The preview shows:

- the Client to create or match;
- Project identity and ownership;
- proposed Phases;
- Proposal Line mappings;
- scope and deliverables;
- selected Opportunity tasks;
- resource assignments;
- planned hours;
- budgets; and
- effective dates.

The accepted Proposal Version seeds the Project's locked original commercial baseline. Proposal Lines seed scope, budget, planned hours, and Phase structure according to the reviewed mappings.

Conversion is atomic and idempotent. It validates required approvals, Client information, ownership, mappings, and financial values before writing. A failed conversion leaves the Opportunity unchanged and creates no partial Client or Project. A retry cannot create duplicate records.

After conversion, the won Opportunity remains a read-only historical sales record linked to the Project. Its retained sales history includes all Proposal Versions, approvals, activities, and tasks that were not moved.

## Projects, Phases, and Tasks

A Project contains ordered Phases. Rarity does not introduce a separate Milestone entity.

Each Phase may contain:

- owner and participating team;
- lifecycle status;
- planned and actual dates;
- budget and planned hours;
- deliverables; and
- completion criteria.

Project tasks belong to a Phase and may contain subtasks. Tasks do not support dependency relationships in this release.

## Resource planning and capacity

Resource planning supports:

- Phase-level planned hours by role or team;
- task-level assignment and planned hours by person; and
- capacity views comparing availability, scheduled work, overbooking, and actual time.

Capacity calculations are derived from authoritative assignments, schedules, time entries, and availability settings. Changes to derived capacity do not mutate Project plans automatically.

## Project financials

The initial Projects release includes:

- original and current budget;
- planned and actual labor;
- cost actuals;
- committed cost;
- recognized billable work; and
- Project and Phase profitability.

Authoritative financial inputs remain versioned or audited. Reporting and profitability projections are rebuildable.

Native invoice generation, payment collection, and accounting synchronization are explicitly deferred to the next PSA phase.

## Change Orders

The accepted Proposal Version remains the immutable original commercial baseline. Changes to scope, price, or planned hours use versioned Change Orders.

Change Orders support the normal internal and customer approval flows. They also support an approval override for any user whose normal Project access permits Change Order updates. The override does not require a distinct override permission.

Every override requires a reason and records:

- the acting user;
- timestamp;
- exact Change Order version;
- previous approval state; and
- immutable audit history.

An approved or overridden Change Order updates the current Project scope, budget, and planned hours while retaining the original baseline and every prior Change Order version.

## Authorization, concurrency, audit, and events

All mutations follow the common Rarity mutation contract:

1. establish authenticated request context;
2. enforce MSP and Client scope;
3. authorize access to the entity and action;
4. validate schema and domain invariants;
5. enforce optimistic concurrency;
6. write business state, audit, and outbox events atomically; and
7. perform notifications and derived projection updates from durable events.

Change Order override intentionally uses the existing Project and Change Order update authorization rather than a dedicated override capability, but still requires the audited reason and version record described above.

## Error handling

The UI and API return specific validation failures for missing approvals, incomplete Client data, unmapped Proposal Lines, invalid financial values, missing owners, and stale versions.

Conversion and Change Order application never expose partially applied state. Concurrency conflicts preserve the user's proposed edits and require review against the latest record version before retry.

Electronic acceptance failures never imply acceptance. Offline acceptance requires an explicit recorded action and cannot be inferred from an uploaded file or note.

## Acceptance criteria

The implementation must prove:

- MSP and Client isolation across all Sales and Delivery entities;
- multiple configurable pipelines and enforced stage rules;
- Proposal Version immutability and exact accepted-version retrieval;
- internal approval plus electronic and offline customer acceptance;
- selective movement of incomplete Opportunity tasks with retained identity and history;
- completed and unselected task retention on the Opportunity;
- atomic, idempotent conversion and safe retry behavior;
- Proposal Line mapping into Phases, scope, budget, and planned hours;
- Prospect matching or conversion without duplicate Clients;
- Phase and subtask behavior without Milestone or dependency semantics;
- resource capacity calculations from plans, assignments, and actuals;
- original baseline preservation across Change Orders;
- audited Change Order override without a dedicated permission;
- correct Project and Phase budget, actual, billable-work, and profitability calculations;
- optimistic-concurrency conflicts on material edits;
- complete audit and event records; and
- permission-aware search, export, and reporting.

## Initial-release boundary

Included:

- configurable Opportunity pipelines and forecasting;
- Prospects and Prospect-to-Client conversion;
- Proposal versions, four line types, internal approval, and customer acceptance;
- Opportunity tasks and selective task conversion;
- Projects, Phases, tasks, and subtasks;
- resource planning and capacity;
- Project financial tracking and profitability; and
- versioned Change Orders with audited approval override.

Deferred:

- native invoices;
- payment collection;
- accounting synchronization;
- Project Milestones;
- task dependencies; and
- autonomous changes to commercial or delivery records.
