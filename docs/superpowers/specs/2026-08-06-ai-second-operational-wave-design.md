# AI Second Operational Wave Design

Date: 2026-08-06
Status: approved for implementation planning

## Purpose

Extend the MSP-wide AI workspace with the remaining prerequisite-backed,
ordinary-service actions from the approved second operational wave. The slice
adds Ticket creation and assignment, Opportunity and Proposal operations, and
internal Knowledge publication without crossing into financial authority,
contractual approval, credentials, external integrations, or broad platform
configuration.

The AI may perform an approved action, but every business value comes from the
user. Rarity may resolve named records to canonical internal identities and may
generate trusted system metadata such as IDs, timestamps, actor identity,
correlation, and causation. It never invents a title, status, priority, stage,
activity, display ID, owner, Client, or publication decision.

## Scope

### Closed tool catalog

Add these tools to the existing MSP-wide catalog:

- `ticket.create`
- `ticket.assign`
- `opportunity.list`
- `opportunity.get`
- `opportunity.transition`
- `opportunity.activity.create`
- `proposal.list`
- `proposal.get`
- `proposal.create`
- `knowledge.publish`

Every Client-bound tool requires an explicit authorized active Client even
when the current page has a Client selected. The page selection may prefill a
hint but is never trusted as action scope.

### Explicit exclusions

This design does not expose:

- Proposal issue, approval, acceptance-grant, acceptance, or conversion;
- Proposal version or line pricing authoring;
- Opportunity creation, custom-field replacement, participant replacement, or
  conversion;
- Project financial recognition, costs, resource plans, or Change Order
  approval, override, or apply;
- time amendments, reversals, timers, approval, or billing export;
- department, team, queue, routing, workflow, calendar, SLA, or role
  administration;
- automation publication, execution, or replay;
- integration configuration, network calls, credentials, tokens, secrets, or
  secret rotation;
- external Knowledge delivery or client-visible publication.

Those actions require separate product and security designs.

## Shared action contract

All new tools use the current AI proposal registry and ordinary application
services. No tool writes directly to PostgreSQL, invokes an HTTP route, or
reimplements domain mutation logic.

For every request:

1. Strict decoding rejects unknown JSON fields and invalid enum values.
2. Preparation resolves the explicit Client and every named object with a
   bounded exact resolver. Zero or multiple matches create no proposal.
3. Preparation rejects inactive Clients, inactive targets, cross-Client
   targets, and unauthorized scope without revealing whether an inaccessible
   object exists.
4. The proposal stores canonical identities, expected versions, the supplied
   business values, and trusted generated metadata. It expires through the
   existing registry contract.
5. The preview renders readable Client and object labels, current and proposed
   values, material ordinary-service effects, and any required reason. It does
   not render raw JSON or protected fields.
6. Confirmation reloads the principal, Client, target, references, versions,
   and prerequisite configuration. It rebuilds the preview and rejects drift
   before invoking the writer.
7. Execution calls the ordinary service with source `ai_workspace` and the
   proposal correlation as causation where the service accepts causation.
8. The ordinary service owns authorization, optimistic concurrency, audit,
   outbox, and transactional atomicity.
9. Rejection, expiry, invalid input, missing data, ambiguity, drift, and failed
   confirmation produce no domain mutation.

## Ticket creation prerequisite

### Ordinary preflight boundary

Refactor the Work Record application service so creation preparation and
creation use one shared selection boundary. A read-only preflight accepts the
same business inputs as ordinary creation after trusted ID preparation:

- explicit Client;
- display ID;
- record type;
- title;
- optional description;
- initial status;
- priority;
- optional Service reference; and
- optional Contract reference.

The preflight authorizes `work_record.create`, validates the active Client and
Service/Contract references, and evaluates the current routing rule set,
published workflow, and SLA policy. It returns a safe create plan containing:

- canonical Client, Service, and Contract identity;
- selected Queue and routing rule-set version;
- selected Workflow and version;
- validated initial workflow state;
- selected SLA policy and calendar versions; and
- the policy targets or durations needed to explain the expected SLA effect.

The preflight performs no write, allocates no durable business record, and
creates no audit or outbox fact. Configuration absence or invalid selection
fails closed with an actionable setup error.

Ordinary creation and preflight must call the same pure selection function.
Confirmation reruns preflight. A changed Queue, Workflow, initial-state
validity, SLA policy/calendar, Service, Contract, or Client invalidates the
proposal. Time-relative deadline timestamps are calculated by ordinary
creation at commit time; the preview identifies their authoritative policy,
calendar, targets, and durations rather than claiming a stale wall-clock
deadline.

### `ticket.create`

The user supplies every business field listed above. Rarity generates only the
record ID and mutation metadata. The exact preview shows the new Ticket,
Client, Queue, Workflow, initial status, priority, optional Service/Contract,
and selected SLA policy/calendar effect. Confirmation reruns preflight and
then calls the ordinary Work Record creation service.

Display-ID conflicts, invalid initial status, missing routing/workflow/SLA
configuration, inaccessible references, and configuration drift fail without
creating a Work Record, routing selection, workflow selection, applied SLA,
audit fact, or outbox fact.

## Ticket assignment

### Authorized technician resolver

Add a read-only directory resolver for active technicians available to the
target Client. It accepts an exact display name or workforce email supplied by
the user, applies the shared case and Unicode-whitespace normalization
contract, returns at most two candidates, and exposes only safe identity:

- technician ID for trusted internal preparation;
- display name;
- workforce email; and
- active/available scope label.

The resolver excludes inactive technicians and technicians without effective
scope for the Client. It never changes authentication identity or permits
username login.

### `ticket.assign`

The user supplies Client, Ticket reference, technician reference, exact Ticket
version, and reason. Preparation resolves the Ticket and technician and shows
the current owner and proposed owner. Confirmation rechecks Ticket version,
Client scope, technician activity/effective scope, and `work_record.assign`,
then calls the ordinary assignment service. Same-owner requests, stale
versions, ambiguity, and authorization loss fail closed.

The ordinary assignment command gains a required reason and persists it on the
assignment audit fact. Existing non-AI callers must supply the same evidence;
the AI path does not create a weaker or parallel mutation contract.

## Opportunity tools

Opportunity queries become explicit-Client adapters suitable for the global
drawer. Results are minimized to the ordinary safe Sales summary and bounded
by the existing stable cursor/limit contracts.

### Reads

- `opportunity.list` requires Client and an optional user-supplied bounded
  filter supported by the ordinary Sales query.
- `opportunity.get` requires Client and an exact display ID or name reference.

Both require `opportunity.read`. They do not return protected attachments,
proposal acceptance evidence, credentials, or Project financial data.

### `opportunity.transition`

The user supplies Client, Opportunity reference, destination pipeline stage,
expected Opportunity version, and reason. Preparation resolves the current
Opportunity and exact destination stage from the current pipeline. The preview
shows current and destination stage plus version. Confirmation rechecks the
Opportunity, stage, pipeline version/availability, authorization, and expected
version, then calls the ordinary transition service.

The ordinary transition command gains a required reason and persists it on the
stage-change audit fact. Existing non-AI callers use the same contract.

No arbitrary state string, automatic stage selection, conversion, probability
override, pricing change, or participant/custom-field mutation is permitted.

### `opportunity.activity.create`

The user supplies Client, Opportunity reference, activity kind, summary,
optional details, and optional occurrence time. An omitted occurrence time is
left unset so the ordinary service may use its documented system-time default;
the AI does not infer a customer-event time. The preview shows the exact
activity and parent Opportunity. Confirmation rechecks parent identity,
Client, lifecycle, and `opportunity.activity.create`, then calls the ordinary
service.

## Proposal tools

### Reads

- `proposal.list` requires an explicit Client and uses the ordinary bounded
  Proposal query.
- `proposal.get` requires Client and an exact Proposal display ID.

Both require `proposal.read` and return only safe draft/state/version and
canonical Opportunity linkage already available through ordinary queries.
They exclude acceptance tokens, signer evidence, and protected attachments.

### `proposal.create`

Proposal creation is draft-container creation only. The user supplies Client,
an existing Opportunity reference, and Proposal display ID. Preparation
resolves the Opportunity inside the same Client and previews a version-1 draft
linked to it. Confirmation rechecks Client activity, Opportunity identity and
eligibility, display-ID availability, and `proposal.create`, then calls the
ordinary Proposal service.

The action cannot create lines or prices, issue the Proposal, request or decide
approval, grant acceptance authority, record acceptance, or convert the
Opportunity. Those remain separate high-impact actions.

## Knowledge publication

### `knowledge.publish`

The user supplies Client, exact Article reference, expected version, and a
publication reason. Preparation resolves an internal draft, loads the exact
draft version, and previews the state change from `draft` to `published` with
title and version. The preview states that publication remains internal and
does not make the Article client-visible or send it externally.

Confirmation rechecks `knowledge.publish`, Client activity, Article identity,
draft state, non-empty body, internal visibility, and expected version, then
calls the ordinary Knowledge publication service. Published, client-visible,
empty, stale, ambiguous, or inaccessible Articles fail closed.

The ordinary publication command gains a required reason and persists it on
the publication audit fact. Existing non-AI callers use the same contract.

## Planner and user experience

Structured mode lists only tools supported by the signed-in principal. The
natural-language planner recognizes explicit requests for the closed actions
above and extracts only supplied business values. Missing required values
produce one focused clarification instead of a partial proposal.

The drawer continues to operate in all-Clients mode. Every proposal card shows
the canonical Client and target identities. Ticket preflight effects, Sales
stage changes, ownership changes, draft Proposal linkage, and Knowledge
publication impact render as plain-English label/value groups using the current
RTM-derived dense visual system. No additional page-level Client coupling is
introduced.

## Error contract

Domain errors retain their typed meaning through preparation, proposal
storage, confirmation, and HTTP mapping:

- invalid or missing business input;
- unauthorized or enumeration-safe not found;
- inactive Client or target;
- ambiguous exact reference;
- unsupported state or transition;
- stale object or prerequisite version;
- duplicate display ID;
- missing or invalid Ticket routing/workflow/SLA setup;
- technician unavailable to the Client;
- Proposal source Opportunity ineligible; and
- Knowledge publication impact not allowed.

User-facing errors are actionable and contain no internal IDs, SQL details,
authorization internals, secrets, or cross-Client existence signal.

## Concurrency and atomicity

Confirmation is not a reservation. Each ordinary writer must lock or validate
the active Client and relevant target/reference rows in deterministic order,
apply optimistic versions, and commit the domain mutation, audit record, and
outbox event atomically.

Tests must cover races between:

- Ticket creation and Client, Service, Contract, routing, workflow, or SLA
  prerequisite changes;
- Ticket assignment and Ticket version, technician activity, or scope changes;
- Opportunity transition/activity and Opportunity or pipeline changes;
- Proposal creation and Client or Opportunity changes; and
- Knowledge publication and Article revision or Client deactivation.

Exactly one valid winner is allowed where two confirmations use the same
version. Failure must leave no partial business row, selection record, audit
fact, or outbox fact.

## Verification and acceptance

### Portable gates

- strict DTO and unknown-field rejection;
- no-invented-business-data planner tests;
- exact/missing/ambiguous/inactive/cross-Client reference tests;
- capability loss and enumeration-safety tests;
- preflight/ordinary-create shared-selection tests;
- preview and confirmation-time drift tests;
- rejection and expiry no-write tests;
- expected-version and concurrency tests;
- audit/outbox atomicity, correlation, and safe-diff tests;
- plain-English preview rendering and capability filtering;
- keyboard, accessible-name, responsive, and stale-state frontend tests;
- full Go tests and vet;
- full frontend tests and production build;
- documentation validators and `git diff --check`; and
- exact whole-branch review with no unresolved Critical or Important finding.

### Demo acceptance

Deploy an exact pushed revision to the constrained non-production RTI demo and
verify migration, health, readiness, build identity, fresh assets, clean
startup, and unaffected Hank/RTM projects. At 1440 by 900 pixels:

1. prepare and reject a synthetic Ticket create with all ordinary preflight
   effects visible;
2. confirm a synthetic Ticket create and assign it to an exact active
   technician;
3. prove stale assignment and ambiguous/inactive technician failures;
4. list/get an Opportunity, prepare/reject a stage transition, and
   prepare/reject an activity;
5. list/get Proposals and prepare/reject a draft Proposal create;
6. prepare/reject Knowledge publication, then publish a synthetic internal
   draft when a disposable fixture is available;
7. verify matching versions, audit/outbox correlation, and no unintended
   external or client-visible effect; and
8. verify an empty browser console.

If the single-Client/global-administrator demo cannot provide a genuine
cross-Client or restricted-principal denial, record that environment limit and
rely on deterministic automated coverage rather than creating misleading
acceptance evidence.
