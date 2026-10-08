# REST API

## Governed classification tags

The classification catalog is MSP-global. It is never filtered or created by
Client, team, object type, or technician. `GET /api/v1/tag-groups` and
`GET /api/v1/tags` return the separately grouped catalog to an authorized
`classification.apply` technician; catalog maintenance requires
`classification.manage`:

- `POST /api/v1/tag-groups`, `PATCH /api/v1/tag-groups/{id}`
- `POST /api/v1/tags`, `PATCH /api/v1/tags/{id}`
- `GET /api/v1/tags/{id}/impact?operation=rename|move|merge|archive&replacement_tag_id={id}`
- `POST /api/v1/tags/{id}/merge` and `POST /api/v1/tags/{id}/archive`
- `GET /api/v1/classification/health` and
  `GET /api/v1/classification/migration-runs`

Catalog creates return an `ETag`; catalog updates and lifecycle actions require
one strong `If-Match: "{version}"` header. A request may repeat
`expected_version` only when it matches the header. Tag merge/archive bodies
require a non-empty `reason`; merge supplies `survivor_tag_id`, and archive may
supply `replacement_tag_id`. The system `Unclassified` tag and its System group
are installed during first-run bootstrap and cannot be maintained through these
routes.

Direct associations use the supported object types `work_record`, `task`,
`project`, `asset`, `knowledge_article`, and `time_entry`:

- `GET /api/v1/objects/{object_type}/{id}/tags`
- `GET /api/v1/objects/{object_type}/{id}/tag-history`
- `PUT /api/v1/objects/{object_type}/{id}/tags`
- `POST /api/v1/tag-bulk-actions`

An association read or change requires `classification.apply` and access to the
exact resolved Client object. The server obtains MSP, Client, actor, source,
correlation, and object scope from authentication, path, and trusted request
metadata; request bodies cannot override them. Cross-Client and missing objects
both return `not_found`. `PUT` replaces only direct tag IDs and requires
`If-Match`, `tag_ids`, a reason, and an idempotency key. It returns the object
with its new `ETag`. Task inheritance remains derived from the current Project:
responses expose `direct`, `inherited`, and de-duplicated active `effective`
assignments, but an inherited assignment is never copied or removable by a Task
write. Bulk items are independently atomic and use the same direct-replacement
rules.

Assignment source values are `human`, `ai_confirmed`, `ai_automatic`,
`automation`, `integration`, `migration`, and `system_fallback`. Public direct
association writes are server-classified as `human`; automatic and integration
sources are reserved for trusted application paths. Archived tags cannot be
assigned, merged identities resolve to their active survivor, and the last
meaningful effective tag cannot be removed. Generic Category APIs and storage
are absent; `forecast_category`, `content_classification`, and billing
classification remain separate concepts.

All tagging mutation bodies are a single strict `application/json` object with
a 1 MiB limit and reject unknown fields. Stable error codes include
`classification_required`, `tag_archived`, `tag_ambiguous`,
`version_conflict`, `not_found`, and `forbidden`; malformed requests use
`validation_failed`.

`POST /api/v1/tag-bulk-actions` accepts `{"items":[...]}` with 1–500 items.
Each item contains `object_type`, `object_id`, `tag_ids`, `reason`,
`idempotency_key`, and its positive `expected_version`; MSP, Client, actor,
source, correlation, and causation are always server-derived. It returns HTTP
200 with `{"results":[{"target":...,"object":...},
{"target":...,"error":{"code":"version_conflict","message":"..."}}]}`.
Items are independently atomic. Per-item errors use the same stable codes as
single routes, including `version_conflict`, `not_found`, `forbidden`,
`classification_required`, `tag_archived`, and `tag_ambiguous`.

Protected System/Unclassified catalog mutations return `409 system_managed`.

Classification suggestions and policy use dedicated routes and capabilities:

- `POST /api/v1/objects/{object_type}/{id}/classification-suggestions`
- `GET /api/v1/classification-suggestions/{id}`
- `POST /api/v1/classification-suggestions/{id}/decide`
- `GET /api/v1/classification/ai-policy`
- `PATCH /api/v1/classification/ai-policy`

Suggestion requests and decisions require `classification.apply` on the exact
Client object. Policy updates require `classification.ai.manage`. Automatic
application is opt-in, threshold-gated, and additive: it never removes a
human assignment or bypasses the final-meaningful-tag guard.

`classification.report` protects `GET /api/v1/tag-reports/{kind}` for
`usage`, `combinations`, `trends`, `classification-health`, and
`recurring-issues`. Supporting routes are
`GET /api/v1/tag-reports/recurring-issues/{tag_id}/evidence` and
`GET /api/v1/tag-reports/options/technicians`. Filters are server-resolved to
the authorized MSP and active Client. An active Client is required for every
report request. Reports use effective tags by default and preserve merged and
archived historical identities. The report response exposes only
`projection_as_of` for projection freshness.

Before a classification cutover, an operator runs the non-interactive command
`rarity-admin classification-preflight --msp DISPLAY_ID` with `DATABASE_URL`
set. Its JSON report contains exact per-object and aggregate totals,
meaningful/unclassified/invalid counts, unresolved retired references,
projection lag and pending events, AI policy, persisted generic Category
database-column detection, and Task 1 backfill evidence. It fails closed when
an active supported object has no effective classification, on an unresolved
merged/archived reference, when a generic Category database column remains, or
on missing backfill evidence or repository error. Only a fully passing run
records a `verified_no_op` migration-run fact. The CLI detects persisted generic
Category database columns; it does not scan application source. The mandatory
separate source gate `node scripts/validate-classification-cutover.mjs`
structurally scans production Go, TypeScript, and SQL contracts.

The removed generic Category contract must not be reintroduced on any of the
six supported objects. Domain-specific `forecast_category`,
`content_classification`, AI context classification, and billable/billing
classification remain valid because they do not compete with governed Tags.

## Sales pipeline configuration

`GET /api/v1/pipelines` lists enabled MSP-wide pipelines and their ordered
stages. `POST /api/v1/pipelines` creates a new immutable pipeline definition.
The list route requires `opportunity.read`; creation requires `pipeline.create`.
Both operate at MSP scope. Stage definitions include forecast
probability/category, required fields, allowed next stages, and proposal or
approval gates.

`GET /api/v1/prospects` returns a bounded, active-only MSP sales list under
`opportunity.read`; it does not accept Client context. `POST /api/v1/prospects`
remains separately gated by `prospect.create`. A linked Opportunity supplies
exactly `prospect_id` (not `client_id`) and preserves that lineage through
Proposal acceptance and atomic conversion.

`GET /api/v1/opportunities/{id}/tasks` lists the first-class, Client-scoped
Opportunity task history under `opportunity.read`; `POST` appends a task under
`task.create`. Task identity and version are retained so conversion can move
selected incomplete tasks without copying or losing Opportunity history.

`PUT /api/v1/opportunities/{id}/custom-fields` replaces the bounded custom
string-field map under `opportunity.update`. It requires the exact Opportunity
version through `expected_version` or `If-Match`, rejects reserved core names,
and atomically writes `opportunity.custom_fields.replaced` audit/outbox facts.

`PUT /api/v1/opportunities/{id}/participants` replaces the optional
participating `team_id` and ordered `contact_ids` under `opportunity.update`.
It requires the exact Opportunity version; the team must belong to the MSP and
every active Contact must belong to the selected Client. The version change
and `opportunity.participants.replaced` facts commit atomically.

`GET /api/v1/opportunities/{id}/attachments` returns at most 100 scoped
attachment metadata records under `opportunity.read`. `POST` requires
`attachment.create` and streams one raw, length-delimited allowlisted file of
at most 250 MiB to object storage. The request supplies `Content-Type` and
`X-Rarity-Filename`; PostgreSQL stores the scoped parent, checksum, uploader,
and correlated `attachment.created` facts without storing file bytes.

`POST /api/v1/projects/{id}/tasks` uses that same Client-scoped Task model
after conversion. It creates either an ordered top-level Project task or a
subtask through `parent_task_id`, requires `task.create`, validates the Project
and optional parent within the trusted scope, and writes the task with audit
and outbox facts atomically.

`POST /api/v1/phases/{id}/tasks` applies the same contract to a Phase. The
Project read model returns top-level Phase tasks with subtask counts so the
delivery plan and task-placement controls remain server-backed rather than
maintaining a separate browser checklist.

All Work Record, Opportunity, Project, and Phase task-create routes accept an
optional scoped `owner_id` plus nonnegative `estimate_minutes`. The shared
service stores those named-assignment and planned-work inputs on the same Task;
PostgreSQL rejects inactive or cross-MSP technicians before audit or outbox
facts are written.

`POST /api/v1/tasks/{id}/time-entries` records actual time directly against a
Client-scoped Project or Phase task. It uses the existing technician,
interval, billable classification, approval, audit, and outbox contracts while
leaving service-desk billing export limited to Work Record time. Project read
models aggregate exact captured duration into task and Phase actual minutes;
no labor rate or recognized revenue is inferred.

`POST /api/v1/projects/{id}/resource-plans` creates a Phase-level role or team
allocation with an inclusive date window and planned minutes. Exactly one of
`role_id` or `team_id` is required. The mutation requires
`project.resource.plan` and atomically stores the plan, audit record, and
outbox event.

`POST /api/v1/admin/technicians/{id}/availability` creates an exact MSP-wide
availability window under `organization.manage`. The request supplies
`starts_at`, `ends_at`, and `available_minutes`; the available amount cannot
exceed the window, and an active technician is locked while overlapping
windows are rejected. The window, audit record, and
`technician.availability.created` outbox event commit atomically. Client-scoped
sessions cannot mutate this MSP-global setting.

Project reads include aggregate `capacity` only for
`project.resource.plan`. The server derives it for named Project technicians
over the Project schedule from exact availability, all dated Project/Phase
assignments in the MSP, and clipped delivery time entries. The response
contains available, scheduled, actual, remaining, and overbooked minutes
without disclosing another Client's records.

`POST /api/v1/admin/technicians/{id}/labor-cost-rates` appends an immutable
effective-dated internal hourly rate under MSP-wide `organization.manage`.
The active technician, currency, nonnegative minor-unit rate, actor, and scope
are validated before the rate, audit record, and
`technician.labor_cost_rate.created` event commit atomically. Client-scoped
sessions cannot write MSP-global rates.

`POST /api/v1/projects/{id}/cost-actuals` records a Client-scoped non-labor
Project cost under `project.edit`. The request accepts an optional Phase,
cost type, description, exact minor-unit amount and three-letter currency,
incurred date, and whether the amount is committed rather than incurred.
The Project and optional Phase are validated inside the trusted scope before
the cost, audit record, and outbox event commit atomically. Project reads
return these inputs only under `project.financial.read`; the GUI separates
incurred cost actuals from committed costs without inventing labor rates or
recognized billable revenue.

`POST /api/v1/projects/{id}/billable-work` appends audited recognized-work
evidence under `project.edit`. It accepts an optional Phase, description,
exact amount/currency, and recognition timestamp, validates the Project/Phase
inside trusted Client scope, and atomically emits
`project.billable_work.recognized`. This input does not create an invoice,
payment, or accounting transaction.

`GET /api/v1/projects/{id}` includes a server-calculated `financials` summary
and per-Phase summaries only under `project.financial.read`. The calculation
uses original/current baselines, incurred/committed costs, recognized work,
and delivery Time Entries valued at the latest technician rate effective when
work began. Mixed currencies are rejected. Missing rates mark actual labor
incomplete and recognized profit unavailable rather than manufacturing zero.
Financially unauthorized reads omit the summaries and redact their supporting
records.

`PATCH /api/v1/phases/{id}` updates the Phase name, owner, participating teams,
planning dates/minutes, budget, deliverables, and completion criteria. It
requires `project.edit`, Client scope, and the exact current version via
`If-Match`; stale edits fail before audit or outbox facts are written.

`POST /api/v1/projects/{id}/change-orders` creates a Client-scoped draft under
`change_order.update`. Draft creation verifies the Project is in scope and
atomically writes the Change Order, audit record, and outbox event. The
returned version is then supplied to
`POST /api/v1/change-orders/{id}/versions`; immutable issued Versions continue
through the existing approval, reasoned override, and apply-once actions.

`GET /api/v1/proposal-versions/{id}/internal-approval` returns the scoped,
versioned internal-approval evidence needed for safe approve/reject actions.
The response carries an `ETag`; decisions submit that exact version with a
required reason. Offline customer acceptance is separately capability-gated
and records signer identity and acceptance time against the immutable version.

**Status:** Core conventions and initial runtime routes implemented

Resources expose create, retrieve, list, update, lifecycle actions, relationships, and history according to capability and policy. Domain transitions use explicit action endpoints when a generic field patch would bypass rules.

Examples include `/api/v1/work-records`, `/api/v1/incidents`, `/api/v1/alerts`, `/api/v1/clients`, and `/api/v1/assets`. Specialized types share representations and semantics while exposing their additional fields.

## Initial Client-resource runtime

`POST /api/v1/admin/clients` creates a Client organization from an MSP-global authenticated principal with `client.create`. `POST /api/v1/admin/departments`, `/api/v1/admin/teams`, and `/api/v1/admin/queues` require MSP-global `organization.manage`, validate hierarchy and optional active Client scope inside the transaction, and atomically commit the directory record with a reasoned audit entry and canonical outbox fact. `GET /api/v1/directory` requires `organization.read`; MSP-global callers receive active Clients and the global hierarchy, while an active-Client caller receives only that Client plus MSP-global queues.

`POST /api/v1/locations` creates a Location in the authenticated session's explicitly selected active Client. The request accepts `display_id` and `name`, rejects unknown fields and oversized/multiple JSON values, requires `location.create`, and never accepts MSP or Client scope from the body. The Location, audit record, and canonical outbox event commit atomically. The response returns `201` with an `ETag`.

`POST /api/v1/contracts` creates an effective-dated Contract in the same trusted Client boundary. It accepts `display_id`, `name`, an RFC 3339 `starts_on`, and an optional RFC 3339 `ends_on`; the application rejects a missing start or an end before the start, requires `contract.create`, and atomically commits the Contract with audit/outbox evidence.

`POST /api/v1/contacts` creates a Client Contact with `display_id`, `display_name`, optional `email`, and optional `location_id`. When present, the Location is validated by MSP and Client inside the Contact transaction; missing and cross-Client references return the same not-found result before audit or event facts are written. The action requires `contact.create`.

`POST /api/v1/services` creates an operational Service with `display_id`, `name`, and optional `criticality`. Migration 27 preserves criticality in PostgreSQL, and the action requires `service.create` with the same atomic audit/outbox contract.

`POST /api/v1/assets` creates an Asset with type, optional scoped Location, and explicit provenance (`source_system`, `external_id`, and `authority`). Migration 27 constrains authority to `discovered` or `technician_confirmed` and uniquely identifies non-empty external identities within the Client/source boundary. Location validation is enumeration-safe and occurs before dependent facts; the action requires `asset.create`.

`POST /api/v1/work-records` creates the shared Incident, Service Request, Change, or Problem record with `display_id`, `type`, `title`, optional `description`, `status`, `priority`, and optional `service_id`/`contract_id`. Optional context references must be active, effective, and belong to the authenticated Client before they can influence selection. The server derives the technician actor and active Client, requires `work_record.create`, applies the current versioned routing rule set, evaluates the scoped published workflow set using the selected queue, and then selects the SLA using Client/type/priority/queue/Service/Contract conditions. The requested status must exist in the selected workflow. The record, exact routing and workflow decisions, SLA policy/calendar versions, business-time warning/deadline timestamps, full evaluation traces, audit entry, and `work_record.created` outbox event persist atomically.

`GET /api/v1/work-records/{id}` and `GET /api/v1/work-records` require `work_record.read` in the authenticated active Client. The list supports status, queue, and owner filters, a limit of at most 100, and stable `(updated_at, id)` keyset pagination through paired `before_updated_at` and `before_id` values. Both paths enforce MSP and Client scope in their PostgreSQL predicates and hide missing or cross-Client records identically.

`POST /api/v1/work-records/{id}/assign` requires `expected_version` and an active same-MSP `owner_id`. It requires `work_record.assign`; stale versions and invalid owners fail before owner-change evidence is written.

`POST /api/v1/work-records/{id}/transition` requires `expected_version` and `to_status`, with an optional human reason. It requires `work_record.transition`, loads the exact immutable workflow version selected when the Work Record was created, validates the configured edge and destination requirements such as mandatory ownership, and atomically updates the versioned status with correlated audit/outbox evidence. Workflow states may declare `sla_behavior` as `active`, `resolved`, or `cancelled`; policy `pause_states` govern pauses. The same transaction pauses/resumes exact-calendar timers, excludes paused business time, records resolution/cancellation/reopen outcomes, and emits distinct SLA facts. Later workflow, policy, or calendar publications never rewrite an existing Work Record's rules.

`POST /api/v1/work-records/{id}/priority` requires the Work Record version, new priority, and a reason under `work_record.edit`. V1 deliberately retains the already-bound SLA policy/calendar and deadlines instead of silently restarting or reselecting them; correlated `work_record.priority.changed` and `sla.policy.retained` evidence makes that policy explicit.

`POST /api/v1/work-records/{id}/sla/override` is separately protected by `sla.override` and requires Work Record and SLA versions plus a reason. An authorized administrator may move either unfinished response or resolution due timestamp to a future instant; its warning timestamp shifts by the same delta. The Work Record and SLA versions, timer state, immutable before/after override row, audit record, and outbox event commit atomically. Completed targets cannot be rewritten.

`POST /api/v1/work-records/{id}/route` requires `expected_version`, `queue_id`, and a non-empty explanation. It requires `work_record.route`; the queue must be MSP-global or belong to the active Client. The versioned route change, reasoned audit record, and outbox event commit atomically.

`POST /api/v1/work-records/{id}/merge` treats `{id}` as the canonical winner and requires both record versions, a distinct `duplicate_id`, and a reason. It requires `work_record.merge`; both records must be in the active Client. The duplicate becomes a redirect tombstone, tasks/comments/attachments/time entries are reparented without losing their identities, and both version changes plus child movement and evidence commit in one transaction.

`POST /api/v1/work-records/{id}/participants` adds an active technician as a `collaborator`, `reviewer`, `escalation`, or `watcher`; `POST /api/v1/work-records/{id}/participants/{participant_id}/remove` closes that participation record with a required reason. Both operations require `work_record.assign`, enforce Work Record and participant versions, preserve historical participation, and atomically version the Work Record with audit/outbox evidence. Primary ownership remains a separate action.

`POST /api/v1/work-records/{id}/comments` requires an explicit `visibility` of `internal` or `client` and a non-empty body. Internal notes require `comment.internal.create`; client-visible replies require the distinct `comment.public.create` capability. The active Work Record and technician author are validated before the comment and its audit/outbox evidence commit. The first client-visible reply atomically records response attainment against the bound SLA; a reply during a pause receives the elapsed exact-calendar pause extension before on-time or breached status is determined. Internal notes never affect response SLA.

`POST /api/v1/work-records/{id}/time-entries` records exact `started_at`/`ended_at` timestamps, derived duration, billable classification, optional note, technician, and optional Task. It requires `time_entry.create`; the technician must be active, and an optional Task must belong to the same active Work Record and Client. Capture never applies billing rounding.

`POST /api/v1/work-records/{id}/attachments` streams a raw request body with required `Content-Type`, `Content-Length`, and `X-Rarity-Filename` headers. It requires `attachment.create`, enforces the configured 250 MB default and content-type allowlist before storage, strips path components from filenames, writes to an opaque Client-scoped MinIO key, verifies the streamed byte count and SHA-256 digest, and then atomically records metadata with audit/outbox evidence. Database rejection triggers compensating object deletion. The response exposes the digest and metadata, not the storage key.

`POST /api/v1/relationships` creates a registered same-Client relationship using explicit source/target types and IDs. It requires `relationship.create`; both endpoints are resolved inside the authenticated MSP and Client, inactive, deleted, expired, or inaccessible endpoints are hidden as not found, symmetric `related_to` edges are stored canonically, and arbitrary type combinations are rejected. The current runtime registry covers Work Record links to Assets, Services, Contracts, and other Work Records plus registered Asset/Service dependency relationships.

`POST /api/v1/work-records/{id}/tasks` appends an ordered open Task or subtask. It requires `task.create`; a supplied `parent_task_id` must identify a Task on the same active Work Record in the authenticated Client. The model intentionally does not infer or expose a dependency graph.

`GET /api/v1/search?q={text}&limit={1..100}` performs a bounded literal search inside the authenticated Client. It requires `search.read` and currently projects active Work Records, Contacts, Locations, Assets, Services, and effective Contracts. Internal comments and restricted attachment metadata are deliberately excluded until result-level visibility enforcement is composed.

`POST /api/v1/views` saves either a `saved_search` query or `dashboard` layout for the authenticated technician. It requires `view.save`; non-private audiences additionally require `view.share`. Caller-supplied MSP and Client filters are removed before persistence. `GET /api/v1/views/{id}/resolve` enforces private/team/department/queue/MSP audience membership, requires `search.read` or `dashboard.read` in the active Client, and injects the authenticated MSP and Client scope into the resolved query. Saved definitions can therefore be shared without carrying reusable cross-Client scope.

Platform administrators with `service_key.manage` use `GET /api/v1/admin/service-keys` to list Client-scoped non-secret key metadata, `POST /api/v1/admin/service-keys` to issue, and `/api/v1/admin/service-keys/{id}/rotate|revoke` to manage scoped integration credentials. Rotation and revocation require reasons and identify the credential by key ID, never by resubmitting its bearer secret. New secrets are returned exactly once. The Service API keys GUI uses the same active-Client and CSRF boundaries and never receives a stored token or digest.

`POST /api/v1/workflows` publishes an initial immutable workflow version; `POST /api/v1/workflows/{id}/versions` publishes a replacement using `expected_version`. Both require `workflow.publish`. Every effective MSP/Client configuration must contain exactly one enabled, unconditional, non-expiring fallback and unambiguous enabled priority/stable-order pairs. State keys and transitions are structurally validated, publication versions are immutable, and the mutable selector plus audit/outbox evidence update atomically.

`POST /api/v1/routing-rules/versions` publishes the MSP's initial or replacement ordered routing rule set using `expected_version` and `routing.manage`. Positions and rule IDs must be unique and the final rule must be unconditional. Global rules may target only MSP-global queues; Client-conditioned rules may target either an MSP-global queue or that same Client's queue. Published versions are immutable, replacement is optimistic, and rule-set/rule rows plus audit/outbox evidence commit atomically.

`POST /api/v1/business-calendars` and `POST /api/v1/business-calendars/{id}/versions` publish validated IANA-timezone calendars with non-overlapping weekly windows and normalized holiday dates. `POST /api/v1/sla-policies` and `POST /api/v1/sla-policies/{id}/versions` publish ordered response/resolution policies bound to an exact accessible calendar version. Both require `sla.manage`; replacements require `expected_version`. The effective MSP/Client policy set has one unconditional global fallback and unambiguous enabled priority/stable-order pairs. Calendar and policy versions are immutable and publication evidence is atomic.

`POST /api/v1/notification-policies` and `POST /api/v1/notification-policies/{id}/versions` publish ordered notification policy versions using `notification.manage`. Policies match durable Work Record events by MSP/Client, contract, workflow, queue, team, department, record type, and priority, and target named channel destinations rather than raw addresses. Replacements require `expected_version`; exact policy versions and their destinations are immutable, and publication evidence commits atomically. The runtime plans each event/policy/channel/recipient combination once, persists quiet-period suppression, permits explicit critical bypass, and uses bounded visibility leases for delivery. In-app rows become durable inbox records immediately. Teams destinations resolve only named, scoped, enabled connections; GUI-managed credentials are purpose-encrypted and legacy `env://RARITY_TEAMS_WEBHOOK_*` references remain supported without persisting or logging the webhook value. Delivery uses compact safe cards and retains versioned retry/failure history for 24 hours.

The client-scoped Knowledge workspace uses `GET /api/v1/knowledge/articles` for a bounded ID/title search, `POST /api/v1/knowledge/articles` for draft creation, `GET /api/v1/knowledge/articles/{id}` for current-version reading, `/versions` for immutable draft revision, and `/publish` for publication. Read, edit, and publish controls are independently capability-gated. Internal-only scope is enforced in both service and repository queries; client-visible publication remains deferred.

The client-scoped Billing review workspace loads the bounded approval queue from `GET /api/v1/time-entries/approvals`, records reason-required approve/reject decisions through `/time-entries/{id}/approval`, and downloads an audited CSV through `POST /api/v1/billing-exports`. Approval and export capabilities are independent. Export records the exact entry versions and CSV digest and fails when the selected period contains unapproved billable time.

The MSP-scoped Clients and directory workspace reads `/api/v1/directory` and exposes capability-gated creation for Clients, Departments, Teams, and global or Client-scoped Queues through `/api/v1/admin/*`. Department, Team, and Queue creation requires a reason and commits audit/outbox evidence; the resulting hierarchy is the named configuration boundary consumed by routing, assignment, shared views, and notifications.

The active-client Resources workspace reads a bounded, metadata-only catalog from `GET /api/v1/client-resources` under `search.read` and uses the existing independently capability-gated create routes for Locations, Contacts, Assets, Services, and Contracts. Every catalog branch retains explicit MSP/Client and active-lifecycle predicates. Manually entered assets are marked technician-confirmed; discovered provenance remains separately attributable.

`GET|POST /api/v1/integrations/teams/connections`, `PATCH /api/v1/integrations/teams/connections/{id}`, and `POST /api/v1/integrations/teams/connections/{id}/credential|test` provide `integration.manage` administration. Responses contain credential presence only and never return webhook URLs. Create, rename, enable/disable, credential replacement, and test require an audit reason; mutations use `expected_version` or `If-Match` where applicable. Tests and delivery accept public HTTPS only, pin validated DNS addresses, bypass ambient proxies, and refuse redirects.

`POST /api/v1/knowledge/articles`, `POST /api/v1/knowledge/articles/{id}/versions`, `POST /api/v1/knowledge/articles/{id}/publish`, and `GET /api/v1/knowledge/articles/{id}` compose the internal-only knowledge lifecycle. Draft creation and immutable draft revisions require `knowledge.edit`; one-way publication requires `knowledge.publish`; reads require `knowledge.read`. Every operation derives the active Client from the trusted principal, replacement and publication use `expected_version`, and accepted mutations emit audit/outbox evidence atomically. Published bodies cannot be changed or deleted, and the database rejects client-visible knowledge in V1.

`POST /api/v1/time-entries/{id}/approval` records a reason-required `approved` or `rejected` decision using `time_entry.approve` and optimistic `expected_version`. Decisions are append-only and the current Time Entry approval state changes with the decision in the same transaction. `POST /api/v1/billing-exports` requires `time_entry.export`, accepts an exclusive RFC 3339 `from`/`through` range of at most 366 days, and refuses the complete export when any selected billable entry is not approved. Successful exports are limited to 10,000 entries and return CSV with an export ID and SHA-256 headers; immutable evidence stores the exact Time Entry versions and exported field snapshots plus audit/outbox facts. This is an export boundary only and does not introduce invoices, payments, or accounting synchronization.

`GET /api/v1/integrations/health` requires MSP-scoped `integration.read` and returns the authoritative worst-state snapshot plus per-connection evidence for Graph, Datto, forwarding, and Teams. The unified database view retains `msp_id` and the repository filters before returning connection IDs, freshness, durable provider state, safe error codes, and pending delivery failures; another MSP's connections cannot enter the snapshot.

## Internal collaboration and mentions

These routes accept authenticated internal technicians only. Supported parent
types are exactly `work_record`, `task`, and `project`; supported source kinds
are exactly `details`, `comment`, and `note`.

- `GET /api/v1/work-records/{id}/internal-content`
- `PUT /api/v1/work-records/{id}/internal-details`
- `POST /api/v1/work-records/{id}/internal-comments`
- `POST /api/v1/work-records/{id}/notes`
- `GET /api/v1/tasks/{id}/internal-content`
- `PUT /api/v1/tasks/{id}/internal-details`
- `POST /api/v1/tasks/{id}/internal-comments`
- `POST /api/v1/tasks/{id}/notes`
- `GET /api/v1/projects/{id}/internal-content`
- `PUT /api/v1/projects/{id}/internal-details`
- `POST /api/v1/projects/{id}/internal-comments`
- `POST /api/v1/projects/{id}/notes`
- `PATCH /api/v1/internal-content/{source_id}`
- `POST /api/v1/internal-content/{source_id}/redact`
- `GET /api/v1/mentions/candidates`
- `GET /api/v1/mentions/widget`
- `PATCH /api/v1/mentions/items/{id}`
- `POST /api/v1/mentions/occurrences/{id}/resolve`
- `GET /api/v1/notification-preferences/mentions`
- `PATCH /api/v1/notification-preferences/mentions`
- `PUT /api/v1/admin/teams/{id}/members`

An internal content create/update body is the exact object below. `tokens` and
`confirmed_team_snapshots` are required even when empty. Details use
`expected_version`; new comments and notes require zero. Edit also requires
`parent_type`, `parent_id`, and `source_kind`.

```json
{
  "body": "Please review @Taylor",
  "tokens": [{
    "id": "stable-token-id",
    "target_type": "staff | team",
    "target_id": "uuid",
    "label": "@Taylor",
    "start": 14,
    "end": 21
  }],
  "confirmed_team_snapshots": {
    "team-uuid": {
      "team_version": 3,
      "eligible_member_ids": ["technician-uuid"]
    }
  },
  "expected_version": 2,
  "idempotency_key": "caller-stable-key"
}
```

Internal-content responses contain exactly `id`, `parent_type`, `parent_id`,
`source_kind`, `body`, `tokens`, `author_id`, `lifecycle_state`, `version`,
`created_at`, `updated_at`, optional `redacted_at`, `read_only`, and `legacy`.
History requires current read or edit access to the parent; it does not require
the widget-only `mention.read` capability. Creating, editing, or redacting
still requires both `mention.create` and effective parent edit access.
The stable edit DTO adds `parent_type`, `parent_id`, and `source_kind` to the
create/update fields. Redaction accepts exactly `parent_type`, `parent_id`,
`source_kind`, `expected_version`, and `idempotency_key`.

Candidate query fields are `parent_type`, `parent_id`, `source_kind`, optional
`q`, and the paired exact-confirmation fields `target_type=team` and
`target_id`. Each response element is exactly `target_type`, `id`, `label`,
optional `eligible_count`, optional `excluded_count`, optional
`eligible_member_ids`, and `version`. Direct ineligible targets fail; a partial
team returns its exact eligible snapshot and must be reconfirmed at save.

The widget query requires `state=unread|read|archived`, `limit=1..50`, and an
optional signed cursor. Its response is `counts {unread,read,archived}`, `items
[]`, and optional `next_cursor`. Each item contains `id`, `parent_type`,
`parent_id`, `parent_display_id`, `parent_subject`, `latest_occurrence_id`,
`author_label`, `origin`, optional sanitized `preview`, `state`,
`last_mentioned_at`, and `version`. State PATCH is exactly
`{"state":"unread|read|archived","expected_version":n}`.

Resolve POST is exactly `{"item_id":"uuid","expected_version":n}`. It
reauthorizes and atomically marks read, returning `href`, authenticated
`client_id`, `parent_type`, `parent_id`, optional `source_id`, optional
`token_id`, `source_available`, and `item_version`. The hash route is the
canonical exact-source contract; callers must not manufacture source anchors.
All mention, collaboration, and team-member identifier boundaries reject
non-UUID path, token, target, item, occurrence, team, and technician values
before repository access.

Occurrence rows store only opaque identifiers, including `token_id`, target,
source revision, parent, author, and time. They do not copy token JSON,
rendered labels, or offsets. Preview and exact-token focus join `token_id` to
the current source document at read time, so edited token positions are never
read from a stale occurrence snapshot.

Team membership PUT is exactly
`{"technician_ids":["uuid"],"expected_version":n,"reason":"..."}` and
requires organization administration. Mention preferences expose
`technician_id`, fixed `event_type=mention.occurred`, `email_enabled`,
`teams_enabled`, `time_zone`, optional quiet-hour bounds, `version`, and
channel availability. In-app delivery is always the Dashboard widget; policy
planning deliberately ignores `in_app` mention destinations. Email and Teams
contain only author, safe object identity/subject, and an authenticated link.
They never contain internal body or preview text and reauthorize immediately
before send.

The preference PATCH body is exactly:

```json
{
  "email_enabled": true,
  "teams_enabled": true,
  "time_zone": "America/New_York",
  "quiet_start": "22:00",
  "quiet_end": "06:00",
  "expected_version": 2
}
```

`quiet_start` and `quiet_end` must either both be `null` or both be valid
24-hour `HH:MM` values. The authenticated technician owns the preference;
no technician identifier is accepted in the PATCH body.

Mention invalidations use five-minute visibility leases and batches of at most
500. Every widget/preview/state/link read remains a synchronous authorization
backstop. Authorization writers increment a per-MSP revision before changing
role facts; mention writers lock that revision before resolving recipients and
store it with the occurrence/item snapshot. A loss event writes at most one
compact marker and never fans out mention items in the request transaction.
Applicable pending markers fail closed. The worker pages markers and items,
evaluates role and capability history exactly at the revision boundary, and
stores only confirmed snapshot losses. A confirmed loss remains effective if
access is regranted before the worker runs. Loss retains immutable occurrence,
resolution, user state, and audit history; it sets access suppression and
cancels pending delivery as `access_revoked`. A newer authorized mention may
supersede an older decision and returns the collapsed recipient/object item to
unread; a regrant alone cannot do so.

Role-assignment expiry is an explicit authorization mutation rather than an
implicit wall-clock side effect. A bounded worker transaction increments and
locks the MSP revision before deleting expired assignments, writes immutable
`role.unassigned` audit/outbox evidence, and evaluates temporal history at the
timestamp recorded for that revision. Role, capability, parent, source, and
mention mutation paths all acquire the revision lock before their domain rows.

Operational counters cover occurrence target type, eligible/excluded
resolution, deduplication, item state transition, re-mention, suppression,
preview outcome, deep-link outcome, and notification outcome. Their structured
evidence may contain MSP, Client, object type/ID, occurrence ID, channel, and a
safe outcome/error code, but never source body, preview, label, offsets, or
token-adjacent text. Mutation counters are emitted only after the owning source
transaction commits. Notification planning uses one atomic idempotency owner;
replays and concurrent losing planners return the existing plan without
emitting another planning outcome.

Datto administrators use the authenticated Datto connections GUI or `GET|POST /api/v1/integrations/datto/connections`, `PATCH /api/v1/integrations/datto/connections/{id}`, and `POST /api/v1/integrations/datto/connections/{id}/credential`. The API URL is safe metadata; API keys and secrets are purpose-encrypted and write-only. Create, scheduling changes, enable/disable, and credential replacement require MSP-scoped `integration.manage`, a reason, and optimistic versions. Runtime credential references include an independent configuration generation, so disabling, reconfiguring, or rotating a connection invalidates already-claimed credential access while legacy `env://RARITY_DATTO_CREDENTIAL_*` deployments remain supported.

## Provider-neutral AI runtime

`GET|POST /api/v1/ai/providers`, `PATCH /api/v1/ai/providers/{id}`, and the provider `/credential`, `/test`, `/discover-models`, and `/models` routes require MSP-scoped `ai.manage`. `GET|PATCH /api/v1/ai/policy` controls the opt-in disclosure and enabled model per supported feature. V1 adapters are `ollama` and `openai_compatible`; credentials are write-only API inputs backed by purpose-encrypted storage. Remote endpoints require public HTTPS, while acknowledged local mode permits local model servers such as Ollama. Both modes refuse redirects.

Provider connections default to a five-minute remote or 15-minute local timeout and reject values above 60 minutes. Requests default to 1 MiB and responses to 5 MiB; hard maxima are 5 MiB and 10 MiB respectively. The raw 5 MiB response default is a transport ceiling, not a token limit.

`POST /api/v1/work-records/{id}/ai/jobs` requires active-Client `ai.assist`, an enabled opt-in policy/model, a supported feature, and an idempotency key. It returns a durable queued job. `GET /api/v1/ai/jobs/{id}` returns scoped progress; `/cancel` and `/retry` require an optimistic version and human reason. `GET /api/v1/ai/recommendations/{id}` returns the safe review projection. `/decide` records a reasoned accepted/rejected decision and always reports `applied: false` and `sent: false`.

`POST /api/v1/intake/direct` accepts an authenticated service principal with `intake.write` and an active Client scope. It requires `application/json`, a non-empty `Idempotency-Key` of at most 512 bytes, valid JSON, and a raw body no larger than 1 MiB. The API derives MSP, Client, and actor from the trusted principal; it never accepts scope from the payload. The exact body is stored in object storage under an opaque deterministic reference, while the Client-scoped external identity, inbound event, audit record, and outbox event commit atomically. Repeating the same key for the same Client returns the original event without storing another payload; the same external identity may be used independently by another Client. Database failure removes the newly stored object, including the losing side of an idempotency race. Accepted requests return `202`.

`POST /api/v1/webhooks/inbound/{connection_id}` authenticates a configured, enabled, Client-scoped inbound or bidirectional webhook connection without requiring a user session. Send valid JSON of at most 1 MiB with `X-Rarity-Event-ID`, `X-Rarity-Timestamp`, and `X-Rarity-Signature`; the V1 HMAC covers the canonical timestamp, event ID, and exact body, permits at most five minutes of clock skew, and resolves either a purpose-encrypted GUI-managed secret or a legacy `env://RARITY_WEBHOOK_SECRET_*` reference so plaintext secrets are not stored in PostgreSQL. The replay claim, connection-scoped external identity, inbound event, audit record, and outbox event commit atomically after the exact body is written to object storage, so independent connections may safely use the same provider event ID. A changed/disabled connection cannot win a race after authentication, and any failed transaction deletes the object. Missing connections and invalid or stale signatures share a non-enumerating `401`; replay returns `409`; accepted requests return a minimal `202` response that does not reveal scope, authentication evidence, or the internal raw-payload reference.

Webhook administrators can instead provision a signing secret through the authenticated Webhooks GUI. Rarity purpose-encrypts the write-only value, clears request buffers, and returns only `credential_configured`; inbound and outbound runtimes resolve that sealed value without exposing it. Legacy environment references continue to work.

Graph mailbox administrators use the authenticated Graph mailboxes GUI or `/api/v1/integrations/graph/mailboxes`. Entra tenant and application IDs are visible metadata; client secrets and server-generated notification client state are purpose-encrypted and write-only. Create, mailbox/enablement changes, and credential replacement require `integration.manage`, a reason, and optimistic versions. Configuration generations fence runtime credential lookup independently from routine health-version updates.

`POST /api/v1/intake/forwarding/{connection_id}` is the protected handoff from an authenticated MSP-scoped mail relay service key with `intake.forwarding.write`; it is not an anonymous SMTP endpoint. Send an exact `message/rfc822` body of at most 50 MiB plus trusted relay metadata in `X-Rarity-Envelope-From`, `X-Rarity-Header-From`, `X-Rarity-Recipient`, `X-Rarity-Message-ID`, and `X-Rarity-SPF|DKIM|DMARC` (`pass` when verified). Rarity revalidates the enabled connection, protected recipient, aligned envelope/header sender domain, domain allowlist, DMARC plus SPF or DKIM, configured message-size limit, and durable connection-global/sender per-minute limits. The exact MIME is stored in object storage; accepted and rejected messages both receive connection-scoped idempotent inbound records. Rejected input is persisted as quarantined with a stable reason, while the event, connection health, audit, and outbox evidence commit atomically. Missing or oversized message IDs receive a content-derived identity and quarantine rather than being silently dropped. Database failure removes the new object, duplicate relay attempts return the original result, and `202` covers both received and quarantined outcomes.

The API process evaluates unpaused active SLA timers every 30 seconds in bounded optimistic batches. Crossing response or resolution warning/breach thresholds versions the timer and atomically emits dedicated audit/outbox facts; concurrent technician changes win through optimistic conflict handling and are reconsidered on the next pass.

Lifecycle-action routes not documented above remain source contracts until their persistence and public runtime slices are composed.

## Unified calendar

The [calendar HTTP route catalog](../02-platform/unified-calendar.md#http-route-catalog) is the authoritative browser/source contract for calendar events, capacity, live updates, scheduling proposals, dependencies, typed commitments, workforce schedules/PTO, custom dates, and saved lenses. Calendar pages derive all-client scope from authorization; source mutations preserve their ordinary permissions and versions.
