# AI Object Catalog and Client Business Data Design

**Status:** Approved; final whole-branch review clean with refreshed isolated
PostgreSQL acceptance complete; publication/deployment/live acceptance pending

**Date:** 2026-08-05

## Goal

Extend the MSP-wide AI workspace from Tickets, Projects, Tasks, and minimal
Client creation into a closed catalog of Client business-data and operational
object actions. Every action must use ordinary Rarity application services,
accept only business values supplied by the user, expose an exact preview, and
retain the existing authorization, active-target, version, audit, outbox,
expiry, rejection, and confirmation boundaries.

This design deliberately rejects a generic CRUD, SQL, HTTP, shell, or
model-selected route tool.

## Delivery strategy

The work is split into three isolated implementation streams:

1. Client-resource foundation plus read/create actions.
2. Client-resource update and lifecycle actions.
3. Broader operational object/action catalog.

The streams may develop in parallel in isolated worktrees, but they merge in
dependency order. The Client-resource foundation merges before lifecycle AI
tools so both streams share one resolver, validation, identity, scope, and
transaction contract.

## Global safety contract

- New conversations remain MSP-global and never use the page Client as an
  authorization boundary.
- Every Client-targeted action explicitly names or selects one authorized
  active Client.
- Names and display IDs resolve server-side. Missing or ambiguous references
  produce clarification and no proposal.
- Model/browser input never supplies trusted object IDs, actor IDs,
  correlation IDs, scope, lifecycle metadata, versions, or provenance
  authority.
- The user supplies every business value. Optional values remain unset unless
  explicitly supplied; the planner never invents defaults.
- Unknown JSON fields fail validation.
- Every write proposal expires, displays canonical targets and exact
  before/after values, and requires explicit confirmation.
- Confirmation reloads the principal, reauthorizes the target, resolves live
  references, reloads the current version, rebuilds the preview, and rejects
  drift.
- Execution invokes the ordinary application service with source
  `ai_workspace` and the proposal correlation identity.
- Rejection and expiry never invoke a domain writer.
- Cross-Client, inactive, missing, and wrong-kind targets fail without
  revealing whether an inaccessible object exists.

## Stream 1: Client-resource foundation and creation

### Shared resource boundary

Add one reusable Client-resource query and resolution boundary:

- active Client authorization by exact MSP and Client;
- kind-scoped get by trusted ID;
- exact display-ID lookup first;
- normalized exact-name lookup second;
- zero or multiple matches return a non-enumerating not-found/ambiguous result;
- bounded active-resource listing by Client, kind, query, and limit;
- minimized summaries containing only ID, kind, display ID, name, safe detail,
  Location ID, lifecycle, and version.

The current mixed newest-500 catalog remains compatible for the existing page,
but AI resolution uses the exact query boundary and never depends on a
truncated list. Creation/reference resolution is active-only. Lifecycle Get
may load an exact active or inactive target so Reactivate can operate without
making inactive records broadly searchable.

### Transaction hardening

All five Client-resource create repositories must:

1. begin a transaction;
2. lock the active Client row with the shared `FOR SHARE` guard;
3. validate active dependent references;
4. write the resource;
5. write audit and outbox facts;
6. commit atomically.

Contact and Asset Location references require an active Location in the same
MSP and Client. Unique resource display-ID and Asset external-identity
violations map to a typed `resource_identity_conflict` and safe HTTP `409`
instead of a generic `500`.

Existing per-kind display-ID semantics remain unchanged for this slice:
trimmed, case-sensitive, unique per MSP, Client, and resource kind across all
lifecycle states. Names may repeat, so ambiguous name references require the
display ID.

### Trusted identity and attribution

Resource create commands accept optional trusted prepared resource and
correlation IDs:

- ordinary HTTP generates them inside the service;
- AI preparation generates a stable future resource ID;
- AI execution passes the proposal correlation ID;
- callers and models cannot submit either field through public request
  schemas.

### Closed read tool

`client_resource.list`, capability `search.read`:

```json
{
  "client_id": "trusted resolved Client ID",
  "kind": "location|contact|asset|service|contract",
  "query": "optional literal query",
  "limit": 25
}
```

`limit` is 1–50. Results contain minimized active summaries only.

### Closed create tools

The public request portion contains references and business values. Preparation
adds trusted IDs.

- `location.create`, capability `location.create`
  - required: Client reference, display ID, name
- `contact.create`, capability `contact.create`
  - required: Client reference, display ID, display name
  - optional: email, phone, Location reference
- `asset.create`, capability `asset.create`
  - required: Client reference, display ID, name, asset type
  - optional: Location reference
  - optional external identity requires both source system and external ID
  - authority is system-owned `technician_confirmed`
- `service.create`, capability `service.create`
  - required: Client reference, display ID, name
  - optional criticality: `low`, `normal`, `high`, or `critical`
- `contract.create`, capability `contract.create`
  - required: Client reference, display ID, name, start date
  - optional end date, which cannot precede the start date

Dates use exact `YYYY-MM-DD` input. No date, criticality, email, phone,
Location, external identity, or other business value is inferred.

### Preview and frontend

Create previews target the prepared resource ID at version zero and show:

- canonical Client;
- `New <resource kind>`;
- display ID and name;
- every explicitly supplied optional field;
- system lifecycle `active`;
- Asset authority `technician_confirmed`.

Contact and Asset previews reload and display the canonical active Location.

The AI drawer adds:

- `Add client resource` structured mode;
- resource-kind selector;
- explicit Target Client selector;
- capability-filtered dynamic fields;
- optional Location selector for Contacts and Assets;
- `Look up client resources` read mode.

Chat supports rigid list/create commands. Missing required values or ambiguous
Client/Location references produce clarification, not a partial proposal.

## Stream 2: Client-resource update and lifecycle

### Ordinary domain services

Add typed `Get`, `Update`, `Deactivate`, and `Reactivate` contracts for
Locations, Contacts, Assets, Services, and Contracts. Archive, delete, restore,
bulk mutation, scope changes, and display-ID renames are excluded.

Every mutation requires:

- exact Client and resource target;
- expected version;
- reason;
- actor and source;
- at least one explicit changed field for Update.

Mutable fields:

| Resource | Mutable fields |
| --- | --- |
| Location | name |
| Contact | display name, email, phone, active Location or explicit Location clear |
| Asset | name, asset type, active Location or explicit Location clear |
| Service | name, criticality |
| Contract | name, start date, end date or explicit end-date clear |

IDs, MSP/Client scope, display IDs, creation metadata, and Asset provenance are
immutable. Generic Asset update/lifecycle actions reject
integration/discovered Assets with `resource_authority_conflict`; their source
integration owns reconciliation and lifecycle.

### Lifecycle state machine

- `active -> inactive` through Deactivate.
- `inactive -> active` through Reactivate.
- Update requires an active Client and active resource.
- Reactivate requires an active Client and valid active dependencies.
- Location deactivation is blocked while active Contacts or Assets reference
  it.
- Historical Work Record references do not block deactivation.

Add database lifecycle checks for the five resource tables and supporting
indexes for active Location dependency checks.

### Concurrency and persistence

Each resource transaction:

1. locks the active Client;
2. loads the exact scoped resource and dependency state;
3. applies one typed version/lifecycle-qualified update;
4. requires exactly one affected row;
5. increments version and actor/time metadata;
6. appends audit and outbox facts;
7. commits atomically.

Stale expected versions return `version_conflict`. Invalid state transitions
return `lifecycle_conflict`.

### Safe audit representation

Extend mutation audit support with a redaction-aware safe diff. Audit may
record that email or phone changed, but general audit/event payloads must not
copy raw contact details. Events remain minimized domain facts.

### API and AI tools

Ordinary routes:

- `GET /api/v1/{kind}s/{id}`
- `PATCH /api/v1/{kind}s/{id}`
- `POST /api/v1/{kind}s/{id}/deactivate`
- `POST /api/v1/{kind}s/{id}/reactivate`

Updates require matching quoted `If-Match` and body expected version.

Capabilities:

- `<kind>.update`
- `<kind>.lifecycle`

AI registers closed per-kind tool names generated by shared constructors:

- `<kind>.update`
- `<kind>.deactivate`
- `<kind>.reactivate`

Preparation resolves the canonical resource and current version. Preview shows
the exact before/after fields, lifecycle, target, reason, and version.
Confirmation reloads all of them.

Errors:

- `404 not_found`: missing, wrong-kind, inaccessible target, or inactive
  dependent reference; an exact inactive resource target remains available to
  authorized Get/Reactivate flows
- `409 version_conflict`: stale version
- `409 lifecycle_conflict`: invalid current state
- `409 resource_in_use`: active Location dependencies
- `409 resource_authority_conflict`: integration-owned Asset
- `422 validation_failed`: missing reason, no-op or malformed patch

### Client Resources page

Replace the stacked operational presentation with a dense RTM-style resource
table:

- kind, display ID, name, safe detail, lifecycle, version;
- active/inactive filter;
- capability-gated edit and lifecycle controls;
- exact confirmation with reason;
- responsive full-width layout.

Creation remains available in a compact action surface.

## Stream 3: broader object/action catalog

### First operational wave

Add closed service-backed tools:

- `project.search`
- `project.get`
- `knowledge.search`
- `knowledge.get`
- `knowledge.draft.create`
- `knowledge.draft.revise`
- `prospect.list`
- `prospect.create`
- `ticket.route`

Also expose the already-registered tools coherently in chat and structured
mode:

- `ticket.get`
- `ticket.search`
- `ticket.transition`
- `ticket.priority`
- `ticket.note`
- `ticket.reply`
- `project.create`
- `task.create`
- `client.create`

The UI calls `ticket.reply` a `client-visible reply`; it does not claim email
delivery.

Project and Knowledge reads are explicit-Client, capability-filtered, bounded,
and minimized. Prospect list/create remains MSP-global. Prospect creation
requires user-supplied display ID and name, with optional email and phone only
when supplied. Knowledge draft creation requires display ID, title, and body.
Revision requires the exact Article, expected version, title, and body. Ticket
routing requires Client, Ticket reference, Queue reference, expected version,
and reason.

Every referenced Project, Knowledge Article, Prospect, Ticket, and Queue uses a
bounded exact resolver. Ambiguity creates no proposal.

### Second operational wave

Implement only after the named prerequisite is complete:

- `ticket.create` after trusted ID preparation and an ordinary preflight that
  returns exact routing, workflow, SLA, Service, and Contract effects;
- `ticket.assign` after an authorized technician directory/resolver exists;
- Opportunity list/get/transition/activity after Sales queries accept explicit
  MSP-wide drawer targets;
- Proposal list/get/create after the same target-aware Sales query foundation;
- Knowledge publish after explicit publication-impact review.

### High-impact actions deferred

The following require a separate approved design because they affect money,
contractual authority, external systems, credentials, or broad configuration:

- Proposal issue, approval, acceptance grant, acceptance, and conversion;
- Project financial recognition, costs, resource plans, Change Order
  approval/override/apply;
- time amendments/reversals, timer control, approval, and billing export;
- department/team/queue administration;
- automation publication/replay;
- integration configuration, external calls, credentials, and secret
  rotation.

## Verification

### Portable gates

- strict schema and unknown-field rejection;
- missing-business-data clarification;
- exact, missing, ambiguous, cross-Client, and inactive reference resolution;
- capability denial and enumeration safety;
- active-Client lock-first transaction ordering;
- active dependent-reference checks;
- expected-version and lifecycle races;
- exact preview and confirmation-time drift rejection;
- rejection/expiry no-write proof;
- audit/outbox atomicity and correlation;
- redacted safe diffs;
- planner tests proving no invented values;
- frontend capability, interaction, stale-state, responsive, and accessibility
  tests;
- full Go tests/vet, frontend tests/build, documentation validators, and diff
  checks.

### Isolated PostgreSQL acceptance

The demo host may provide Docker/PostgreSQL capacity, but concurrency tests use
a separately named disposable scratch database and `TEST_DATABASE_URL`.
They never run destructive cleanup against the live demo application database.

Required proofs:

- resource creation versus Client deactivation serialization;
- two same-version resource writers yield one success and one conflict;
- Location deactivation versus dependent Contact/Asset mutation;
- atomic resource/audit/outbox rollback;
- existing Client identity cross-path concurrency test.

The scratch database name is resolved exactly before creation and cleanup.
Only that database may be dropped after successful or failed verification.

### Full-page demo acceptance

At 1440 by 900:

1. list each resource kind without changing the page Client;
2. prepare and reject one exact create proposal per resource kind;
3. confirm selected synthetic creates/updates, then verify exact audit/outbox
   and version changes;
4. prove stale confirmation, inactive Client, ambiguous Location, Location in
   use, and discovered Asset edit failures;
5. deactivate/reactivate an allowed synthetic resource;
6. search/read Projects and Knowledge across authorized Clients;
7. prepare/reject a Prospect;
8. create/revise a synthetic Knowledge draft from supplied values only;
9. route a synthetic Ticket with exact Client/Ticket/Queue/reason preview;
10. verify no framework overlay or browser console errors and capture visible
    proposal/result evidence.

Synthetic accepted records are explicitly labeled for demo acceptance and
cleaned through ordinary product lifecycle actions when the scenario permits.

## Documentation and completion

Update the API, AI contract, roadmap, rendered tracker, acceptance evidence,
and active RTI Kanban card. Publication, deployment, and live acceptance remain
separate gates. A Git push is not deployment, and healthy endpoints without
exact build identity and affected-path proof are not acceptance.
