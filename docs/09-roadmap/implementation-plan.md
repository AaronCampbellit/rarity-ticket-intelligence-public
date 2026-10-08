# Phased Implementation Plan

**Status:** Historical Phase 0-8 implementation record; corrective work and calendar frontend implementation recorded in the current backlog; constrained demo Compose, PostgreSQL, core Ollama runtime, expanded AI catalog, second operational AI wave, and exact published-artifact acceptance complete; remaining configured-provider scenarios and supported-profile infrastructure acceptance remain active.

## Current work and evidence boundaries

The [current execution backlog](current-execution.md) is authoritative for next work.
This document preserves delivery history; past “complete” or “review-clean” statements
apply to their named slice, revision and environment. They are not claims about
every subsequent working tree. The unified calendar browser/source implementation is now present; the current
backlog records local verification and outstanding external acceptance.

## Phase 0 — Foundation approval and delivery baseline

Establish repository conventions, CI, documentation checks, threat model, deployment test environment, and acceptance fixtures. Exit: tracked risks and a reproducible non-production platform baseline. Product implementation remains separately authorized.

## Phase 1 — Secure platform kernel

Implement organization/client scope, optional Entra JIT and local Platform Administrator identity, global RBAC, sessions, audit ledger, encryption/secret abstraction, PostgreSQL schema/migrations, object envelope, event outbox, and client-isolation tests. Exit: all mutations use authorized application services and emit audited durable events.

Acceptance evidence: demo revision `e3574bf` applied migration 68 and proved
Entra-free local Platform Administrator login, password reset, allowed-network
replacement, mandatory session revocation after both security changes,
disablement, disabled-login rejection, and optimistic-conflict classification.
The Entra candidate remains `not_connected` with no credential configured, so
credential-backed verification and later activation remain pending by design.

The packaged `rarity-admin local-admin reset-password` operator command now
provides the documented production lockout path without an HTTP bypass. It
accepts passwords only through a no-echo interactive TTY and atomically updates
the bcrypt hash, revokes active sessions, and writes correlated system-actor
audit/outbox facts. The one-shot Compose `operator` profile is excluded from
normal startup and shares the verified API image.

## Phase 2 — Work management

Implement clients, contacts, locations, teams/departments/queues, assets/services/contracts, Incident/Request/Change/Problem records, child tasks, routing, assignment, collaboration, comments/public replies, attachments, time entries, links, duplicate merging, and core search. Exit: technicians can operate a secure queue-to-resolution workflow.

Constrained demo revision `229756e25558` now carries two isolated Client
catalogs with locations, contacts, assets, services, and contracts plus eight
synthetic Work Records spanning every V1 record type. The live workflows cover
new, in-progress, and resolved states with internal/client comments, Tasks, and
billable time. Cross-Client Work Record, sales, knowledge, and AI reads return
enumeration-safe 404s, and an API restart retained the full PostgreSQL dataset.
Live acceptance also repaired the Global Administrator asset/comment grants,
malformed optional-location validation, and PostgreSQL Task parent typing.

The workforce-time source slice now provides independent durable timers per
ticket, server-owned stopped captures, one-time capture consumption into
immutable labor-role rate snapshots, and Monday-based weekly timesheets.
Technicians can review their own daily and weekly totals, while client-scoped
Time Reviewers can amend pending entries with before/after evidence, approve or
reject changes, and reverse-and-replace approved entries. MSP administrators
can create reasoned authorization roles, assign them without widening Client
scope, and append effective-dated labor-role versions from the directory GUI.
Stopped captures can be composed atomically with internal notes or public
client replies. Outbound email composition remains a separate task until an
outbound delivery service provides the same transaction boundary.

## Phase 3 — Workflow, SLA, and technician experience

Implement configurable workflow/statuses, schedules, response/resolution SLAs, calendars, notifications, approval-before-export, dashboard/worklists, saved searches, sharing, and internal knowledge. Exit: a technician and manager can configure and observe normal service-desk operation.

## Phase 4 — Native PSA sales

Implement Prospects, multiple configurable Opportunity pipelines, stage gates, activities/tasks, forecasting, versioned Proposals with four line types, internal approvals, electronic/offline customer acceptance, and immutable accepted PDF snapshots. Exit: sales can take a Prospect or existing Client from Opportunity through an accepted commercial baseline without an external PSA.

Authenticated Opportunity operations now resolve configured pipeline and stage
names, create Client-scoped opportunities at the configured initial stage,
record immutable activity history, and expose only configured next-stage
transitions. Mutation controls remain capability-gated, while pipeline discovery
uses ordinary Opportunity read access rather than granting pipeline
administration.

The authenticated Proposal workspace creates drafts from scoped Opportunities
and issues immutable versions containing one or more fixed-fee,
time-and-materials, product/license, or recurring-service lines. It captures
price, cost, discount, tax, recurrence, planned labor, expiry, and explicit
amount/margin approval rules while the server remains authoritative for totals,
approval requirements, and immutable PDF snapshot generation.

Required internal approvals are loaded as versioned evidence before a
capability-gated, reasoned approve/reject decision; the GUI sends the exact
approval ETag version. Once approval is satisfied, authorized staff may record
offline signer identity and acceptance time against the immutable Proposal
Version. Electronic acceptance remains token-bound to the external signer
flow rather than exposing its one-time grant in the internal workspace.

The MSP-wide Prospect workspace lists active pre-client organizations, creates
new Prospects under separate authority, and starts linked Opportunities in a
configured pipeline without manufacturing Client scope. The existing atomic
conversion preview remains responsible for duplicate Client matching and for
turning an accepted Prospect commercial baseline into a Client and Project.
When conversion creates that Client, its larger transaction now enters the
same MSP-scoped atomic identity boundary as ordinary and AI Client creation:
one shared deterministic normalization, all-lifecycle cross-field recheck,
advisory transaction lock, and safe typed conflict precede every conversion
write. Existing-Client conversion retains its prior behavior without taking
that lock.

The authenticated Opportunity workspace also presents the Client-scoped
server aggregation by stage: pipeline amount, probability-weighted amount,
forecast category, and opportunity count. Stage transitions reload this
authoritative forecast rather than calculating totals from the bounded browser
worklist.

Opportunity detail now lists and creates first-class scoped Tasks alongside
immutable activities. These are the same versioned Task records selected by
conversion preview and moved atomically into the resulting Project, rather
than browser-only checklist items.

Opportunity detail now also replaces bounded custom string fields with exact
version checks and uploads content-type allowlisted attachments through the
shared object-storage boundary. PostgreSQL retains Client-scoped metadata and
checksums with audit/outbox facts; the GUI lists that authoritative metadata
without placing file bytes in the sales record.

The same versioned Opportunity workspace now replaces one participating MSP
team and an ordered Client Contact set. Repository validation rejects
cross-MSP teams and cross-Client Contacts before the scoped participant rows,
Opportunity version, audit record, and outbox event commit together.

After conversion, the Project workspace now continues that shared Task
lifecycle: authorized users append ordered top-level Project tasks or subtasks
of existing Project tasks. PostgreSQL validates both the Project parent and
optional parent task in Client scope before atomically storing task, audit, and
outbox facts.

The same Task lifecycle is available inside ordered Phases. The Project read
model loads scoped Phase tasks and subtask counts, and the GUI can place new
top-level work or subtasks in the Project backlog or a specific Phase without
creating a second task representation.

Shared Task creation now also persists an optional named technician assignment
and planned minutes for Opportunity, Project, Phase, and Work Record tasks.
The repository validates the assigned technician as active in the same MSP
before writing the Task, audit record, and outbox event. Project and Phase
read models return the persisted planned minutes, and the delivery workspace
renders them rather than reverting to browser-only estimates.

Project and Phase tasks now accept scoped actual-time entries through the
existing time-entry service. The compatibility migration keeps Work Record
time unchanged while permitting task-only delivery time, PostgreSQL validates
the Project/Phase task and active technician, and Project reads aggregate
captured duration into task and Phase actual minutes. Service-desk billing
exports intentionally exclude task-only Project time until recognized
billable-work and labor-rate semantics are defined.

Authorized Project users now record exact non-labor cost actuals and committed
costs against the Project or an optional Phase. The runtime validates scope,
persists the cost with correlated audit and outbox facts, redacts the collection
without financial-read access, and renders incurred and committed totals from
those persisted inputs. Labor-cost and recognized-revenue calculations remain
source-backed rather than inferred from time or billing classifications.

The runtime now composes the existing financial service into authenticated
Project reads. PostgreSQL supplies immutable baseline amounts and persisted
incurred/committed costs, the service enforces one currency and calculates
available profit projections, and the GUI renders that server summary rather
than rebuilding it in browser state.

Actual labor and recognized billable work now have authoritative contracts.
MSP-wide administrators append immutable effective-dated technician cost rates
from the directory GUI; Project users append audited recognized-work evidence
at Project or Phase scope. PostgreSQL values each delivery Time Entry at the
latest rate effective when work began and returns Project and Phase
profitability from attributed baselines, costs, labor, and recognized work.
Mixed currencies fail closed, Project-wide entries are not invented into Phase
allocations, and missing rates make actual labor and recognized profit visibly
unavailable.

Phase-level role/team resource planning is now runtime-composed: authorized
users create dated allocations with planned minutes from the authenticated
Project workspace, and PostgreSQL stores the plan with audit and outbox facts
in one transaction. The refreshed Project read model displays the persisted
allocation.

MSP-wide organization administrators now configure exact dated technician
availability windows from the directory GUI. PostgreSQL rejects inactive
technicians and overlapping windows before atomically storing availability,
audit, and outbox facts. Authorized Project resource planners receive a
server-derived capacity view for named Project technicians: availability,
scheduled assignments across Clients, clipped actual delivery time, remaining
capacity, and overbooking are aggregated without exposing another Client's
records or mutating the plan.

The live Project workspace now edits the complete settled Phase planning
contract with optimistic concurrency: ownership/team participation, dates,
planned labor, budget, deliverables, and completion criteria. The read model
returns the persisted Phase dates, and every save carries the exact Phase
version through `If-Match`.

The Change Order lifecycle is now reachable without seeded database records:
authorized Project users create scoped, audited drafts, issue their first or
replacement immutable Version from the Project workspace using the exact
Change Order version, and then use the existing approval, reasoned override,
and apply-once controls.

## Phase 5 — Native PSA Project delivery

Implement atomic conversion preview, Prospect-to-Client matching, selective Opportunity task movement, Projects, ordered Phases, subtasks, resource planning/capacity, original/current budget, labor and cost actuals, committed cost, billable work, profitability, and versioned Change Orders with reason-required audited approval override. Exit: an accepted Proposal can become a staffed, financially tracked Project without duplicate or partial records.

Active slice: deterministic editable conversion previews with exact accepted-Proposal/snapshot validation, explicit Prospect-to-Client matching, complete Proposal Line/financial/task validation, stale-preview rejection, retry-safe idempotency, and a single atomic mutation contract covering Client creation, Project/Phase creation, locked original/current baselines, selected task movement/history, won Opportunity state, conversion record, audit, and outbox; scoped/versioned Projects linked to their immutable Proposal baseline; ordered Phases without a separate Milestone or task-dependency model; Phase ownership/team participation and planning fields; optimistic Phase updates; Phase-level role/team resource plans; read-only capacity views; currency-safe actual and projected profitability from planned labor, actual labor, actual/committed costs, and recognized billable work; immutable Change Order Versions with append-only approval/override evidence, normal-update authorization for reason-required overrides, optimistic concurrency, and apply-once current-baseline updates that preserve the original baseline; and thin `/api/v1` PSA mutation/action routes with trusted principal injection, strict size-limited DTO decoding, required conversion version/idempotency/preview controls, stable error codes, enumeration-safe scope failures, ETags, canonical outbox envelopes, and preserved platform health routes.

Acceptance evidence: the shared responsive workspace, component suite, production
frontend build, and four Playwright journeys verify the synthetic
Opportunity-to-Project, selective task movement, capacity/overbooking,
financial baseline, profitability, and reasoned Change Order
override/application paths. Portable Go and infrastructure contract packages
pass. On 2026-07-31, revision `3338d3c3430025c5766ac21cf9b7842bd7889b6f`
passed the PostgreSQL migration chain through version 66 plus conversion,
Opportunity-extension, Project-isolation, proposal-immutability, task-history,
and Change Order integration tests on the constrained ADR-0033 demo host.
The same revision passed Compose health, readiness, build-identity, rendered
shell, and pinned Playwright browser smoke checks. This is constrained demo
evidence, not supported Ubuntu-profile, production, HA, or capacity acceptance.

## Phase 6 — Intake and integration

Implement Microsoft Graph intake, forwarding intake, lifecycle handling, five-minute delta reconciliation, direct API/webhooks, thread correlation, signed outbound webhooks, scoped service API keys, Datto asset sync/reconciliation, alert deduplication/recovery events, and integration health. Exit: supported sources reliably create/update work without manual data repair.

Implemented source contracts: hash-only scoped service keys; exact-body signed and replay-protected inbound/outbound webhooks; Graph subscription/lifecycle recovery, server-side message retrieval, secure threading precedence, five-minute reconciliation, and atomic cursor advancement; protected forwarding quarantine plus authenticated direct intake normalization; read-only Datto full/incremental reconciliation, review-only match candidates, stale-not-delete behavior, and independent alert recovery evidence; encrypted safe Teams delivery with bounded retry; and authoritative connection health. PostgreSQL migration and repository acceptance is complete on the constrained demo environment. The constrained HTTPS demo now also proves GUI-created inbound and outbound webhook connections, exact-body signature acceptance, replay and stale/bad-signature rejection, credential rotation, disable/re-enable behavior, and one-attempt durable delivery to a synthetic public HTTPS endpoint. It additionally proves a GUI-created Client-scoped external automation connection, publication of a typed `call_http` definition, and a successful one-attempt signed HTTPS callback without a dead letter. Configured non-production Graph, Datto, and Teams acceptance remains pending.

## Phase 7 — Automation and intelligence

Implement typed V1 automation, connection objects, retries/dead letters, and MSP-opt-in constrained AI assistance. Exit: no automation or AI path bypasses permissions, audit, or client isolation.

Implemented source contracts: immutable typed automation versions with bounded nesting and prohibited privileged actions; capability/Client-scoped execution principals; separately authorized Connection use; durable waits; idempotent causation-aware runs; sanitized step history; bounded retries and reasoned dead-letter operations; MSP-opt-in, provider-disclosed AI summaries, reply drafts, and suggestions with minimized context, authorized candidate sets, model/prompt/cost traceability, and explicit human decisions that cannot directly apply records or send messages; and public runtime composition for protected provider management, durable generation jobs, Ollama, and OpenAI-compatible adapters. The constrained demo now proves local Ollama connection testing, discovery, explicit model selection, policy opt-in, slow durable generation, pending-human recommendation persistence against a real `qwen3:14b` model, reasoned queued-job cancellation without a recommendation, successful failed-job retry after connectivity recovery, terminal-job retry rejection, enumeration-safe cross-Client job lookup, and a GUI-configured signed external automation callback that completed on attempt 1. A live protected endpoint-change probe also proves actionable `409 provider_in_use` behavior without changing the healthy provider. Full-desktop GUI review at 1440 by 900 pixels now covers the core authenticated journeys and repaired the AI settings bootstrap plus Sales detail presentation defects it exposed. Other non-production AI provider families remain pending.

The global AI workspace source slice adds personal conversations, a curated
versioned RTI knowledge corpus with safe citations, and a persistent top-bar
drawer available across authenticated routes. New drawer conversations and
proposals are now MSP- and principal-scoped rather than bound to the page's
Client filter; the drawer says `All authorized clients`. Structured ticket and
Project forms require an explicit target from the authorized Client directory,
while Project and standalone Task chat commands resolve the user-supplied
Client name or display ID server-side. Project resolution for Task commands
remains inside that resolved Client, and missing or ambiguous references create
no proposal.

The closed typed registry also exposes MSP-global `client.create`. It accepts
only the user-supplied Client name and display ID, and blocks a duplicate name
or display ID across all lifecycle states, including inactive, archived, and
deleted. The exact preview contains only name, display ID, and active lifecycle.
Every in-product Client writer, including Prospect conversion, shares the same
commit-time atomic identity boundary; proposal-time checks remain fast feedback
rather than the final race-safety boundary.
After confirmed AI Client creation, the browser refreshes the authorized
directory so the new Client is immediately available to selectors and later
proposal previews. Target-bearing previews show the canonical Client identity
and fail closed when it cannot be resolved; the creation preview retains its
explicit `New client` identity.
Every write expires, requires explicit confirmation, reloads the MSP-global
principal, rechecks the ordinary action capability and exact Client or MSP
target, rebuilds the preview, and invokes the ordinary application service with
correlated audit and outbox evidence. Tool preparation generates only system
identifiers and metadata; it rejects caller-supplied identities and never
invents names, descriptions, owners, dates, estimates, budgets, contacts,
domains, locations, contracts, billing, services, or onboarding data.

The MSP-wide AI Client slice has completed portable focused/full verification,
whole-diff review, publication, exact-revision demo deployment, and full-page
live acceptance. Demo revision
`690b1e9dc1338a1731ef7be0d4a69c3400514fec` applied migration 79 and passed
public health/readiness/build identity plus fresh-asset verification. At 1440
by 900, the drawer and structured selector exposed the authorized Client
directory, the Northwind Project command resolved the canonical Client and
tasks A, B, C, and the exact synthetic Client preview was rejected; a fresh
directory reload proved no Client write. Silent or automatic confirmation,
automatic page-filter switching, broader Client business-data automation,
Client updates/lifecycle actions, and generic SQL/HTTP/shell execution remain
excluded. Outbound email is intentionally not claimed because the current
public reply service does not provide a dedicated email-delivery evidence
boundary.

The expanded AI object and Client business-data catalog is source-complete and
review-clean; its final whole-branch hold findings have a source fix wave with
refreshed PostgreSQL proof and exact re-review complete. It is a separate
acceptance slice from the already deployed minimal Client action above. The
drawer now provides closed, capability-gated
reads for Client resources, Projects, Knowledge, and Prospects; exact
create/update/deactivate/reactivate proposals for Locations, Contacts, Assets,
Services, and Contracts; and closed Knowledge draft, Prospect creation, and
Ticket routing proposals. The existing Ticket, Project, Task, and Client tools
remain in the unified MSP-wide catalog. Every Client-bound action resolves an
explicit authorized active Client, and write confirmation reloads authority,
references, state, and version before invoking the ordinary service.

Migrations 80–81 add the five resource lifecycle constraints, active Location
dependency indexes, and Global Administrator lifecycle capabilities. On
2026-08-06, the Task 12 Go, vet, frontend test/build, documentation, and diff
gates passed. The isolated demo PostgreSQL proof applied migrations 1–81
and passed deterministic Client/resource serialization, same-version conflict,
Location/dependent mutation, rollback atomicity, and cross-path Client identity
races. The exact scratch database was removed and the live demo stayed on its
prior revision throughout. That evidence predates the final resolver,
transaction, typed-error, summary, and canonical-Location fix wave. The
Project/Knowledge resolver follow-up retains a Client-scoped SQL limit of two
while matching case-insensitive display IDs and names with the shared Unicode
whitespace normalization contract; typed Client/resource resolution and
lifecycle errors now survive proposal preparation into safe HTTP responses.
Refreshed isolated PostgreSQL evidence additionally covers Knowledge-draft and
Ticket-route races against Client deactivation; fixture residue was zero and
the scratch database was removed. Exact re-review found no remaining Critical
or Important issues. Publication, exact-revision deployment, and 1440×900
live acceptance completed on 2026-08-06. The accepted service-desk runtime was
`347addfdf5780d6a4507e822f0c352625a9c6027`: all five Client-resource
journeys, Project and Knowledge reads, rejected Prospect creation, Knowledge
draft/revision, and exact Ticket routing passed with matching audit/outbox
evidence and an empty browser console. Acceptance permanently repaired the
all-Clients routing/calendar/SLA setup path and routing capability exposure.
Cross-Client and restricted-principal browser proofs remain bounded by the
single-Client/global-admin demo fixture and are covered by automated contracts.
The second operational wave is source-complete, whole-branch reviewed,
published, deployed, and accepted on the constrained demo.
It adds closed Ticket create/assign, Opportunity list/get/transition/activity,
Proposal list/get/empty-draft-create, and internal Knowledge publication tools
under their ordinary capabilities. The user supplies every business value;
exact Client/object resolution, confirmation-time reload and readable preview,
Ticket selection/reference fences, required assignment/transition/publication
reasons, and ordinary atomic audit/outbox writers remain mandatory.

Its isolated PostgreSQL proof applied migrations through version 81 and passed
17 deterministic concurrency cases plus 12 forced audit/outbox rollback cases.
The Task 12 review repaired one Important Ticket-create confirm-to-commit gap
by carrying confirmed Client, Service, and Contract versions into the ordinary
transaction; focused RED/GREEN coverage and exact re-review found no remaining
Critical or Important issue.

The branch was fast-forwarded and pushed to `main`; revision
`963f4759effe28e34eed6033230f0439ea7e890f` then passed exact-build
constrained-demo deployment and authenticated full-page acceptance at 1440 by
900. Ticket create/assignment, Sales reads and rejected writes, and
rejected-then-confirmed internal Knowledge publication produced the expected
versions and matching audit/outbox evidence. The Knowledge fixture remained
internal, external delivery/automation counts were zero, and the browser
console was empty. Cross-Client, restricted-principal, inactive-technician,
and ambiguous-technician live denials remain bounded by the
single-Client/global-admin fixture and are covered deterministically.

High-impact financial, credential, integration, configuration, automation,
contractual approval/acceptance, Proposal issue/pricing/conversion, Project
financial/Change Order, time/billing, and external/client-visible Knowledge
actions remain intentionally absent rather than implied generic automation.

Acceptance evidence: the integration, automation, and AI package tests, targeted vet gate, and documentation validator pass portably. See [Integration, Automation, and AI Contracts](../04-api/integration-automation-ai-contracts.md) and [Integration, Automation, and AI Acceptance](../06-development/integration-automation-ai-acceptance.md).

## Phase 8 — Production operations and release readiness

Implement Compose deployment, three-node PostgreSQL HA, object-storage choices, WAL/snapshot backup, restore/DR verification, rolling upgrade path, health/metrics/traces/logs, capacity tests at 50 concurrently active technicians/1,000 clients/5,000 tickets per peak day plus a 1,000-event five-minute burst, security testing, and release runbooks. Exit: documented evidence of the agreed recovery, availability, latency, and scale targets.

Implemented source contracts: a hardened single-node pilot profile; separate three-node Patroni/etcd/HAProxy contracts; encrypted pgBackRest backup/WAL and clean-room PITR verification; upgrade evidence gating; correlated route-level logs, traces, internal metrics, and capacity evaluation; a sealed repository security scan with remediated intake, acceptance, and audit-provenance findings; automated WCAG A/AA workflow checks and keyboard skip navigation; public `/api/v1` ingress; read-only pull-request CI with PostgreSQL and browser jobs; digest-pinned container inputs; reviewed-main GHCR publication with vulnerability scanning, BuildKit SBOM/provenance, keyless exact-digest signing, verification-before-promotion, and release Compose overrides; and a fail-closed revision-bound release evidence gate with pilot, promotion, rollback, recovery, HA, capacity, monitoring, security, accessibility, and supply-chain runbooks. Constrained single-node Compose, PostgreSQL, core Ollama, local administrator lifecycle, pre-candidate automated accessibility, and exact published-artifact evidence are complete under ADR-0033. Supported Ubuntu-profile requalification, remaining configured-provider scenarios, true browser zoom, physical touch, VoiceOver/NVDA, HA failover, restore/DR, OTLP/collector, and realistic capacity evidence remain live acceptance gates.

On August 18 2026, reviewed revision
`098e2a66a29424225911018ef5db0762f136af9a` passed all three trusted-branch
CI jobs and the chained publication workflow. Both final GHCR digests passed
the publish scan, exact-identity keyless signing, signature verification, and
promotion. The repository-native verifier then retained both signature
results, both SPDX SBOMs, both production BuildKit provenance documents, and
the revision-bound artifact manifest as exactly seven JSON files. The
mandatory release-gate consumer accepted both real artifacts in its complete
contract fixture. This closes the published-artifact gate only; it does not
claim that the other thirteen pilot operations were executed.

Current verification-host inventory on August 18 2026 is Ubuntu 24.04.4 LTS
with 8 vCPU, 16,353,916 KiB memory, a 102,128,648,192-byte root filesystem,
Docker Engine 29.7.1, and Compose 5.4.0. The operating system and CPU count now
match the pilot baseline, but memory and storage remain below the
8-vCPU/32-GB/1-TB qualification target. It remains constrained evidence, not
supported-profile capacity evidence.
Live logs also exposed an SLA evaluator system-actor UUID defect. Revision
`64b6f5bfa93e47ebec1ed92bf640f5d09b8ea918` deployed the source fix using the
existing zero-UUID system identity convention; health and readiness remained
200 and no prior UUID error recurred across a full evaluator interval.
