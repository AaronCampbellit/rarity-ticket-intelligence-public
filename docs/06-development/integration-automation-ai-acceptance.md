# Integration, Automation, and AI Acceptance

**Status:** Portable, PostgreSQL, core Ollama, signed-webhook,
external-automation, and full-desktop GUI acceptance complete; expanded AI
object catalog and second operational AI wave published and accepted on the
constrained demo; remaining configured-provider scenarios pending

## Portable gate

Run from the repository root:

```bash
go mod verify
go test -race ./backend/internal/aiassist ./backend/internal/httpapi ./backend/internal/store/psa
go test ./backend/... ./tests/...
go vet ./backend/... ./tests/...
npm --prefix frontend test -- --run
npm --prefix frontend run build
node scripts/validate-integration-automation-docs.mjs
git diff --check
```

The gate proves hash-only service keys, scoped bearer identity, webhook signatures and replay controls, Graph reliability/threading/cursor rules, protected forwarding, normalized system intake, Datto read-only reconciliation and alert behavior, Teams content/retry boundaries, integration health, typed automation, scoped draft/revise/publish management, safe external-Connection creation, leased event execution, least-privilege application actions, separately scoped and signed external calls, step-resumable waits/retries, enumeration-safe atomic dead-letter actions, and provider-neutral AI composition. AI coverage includes protected write-only credentials, local/remote transport boundaries, Ollama and OpenAI-compatible adapters, request/response/timeout/cancellation limits, durable jobs and leases, Client-scoped context/candidates, and atomic human decisions that cannot apply records, send messages, or start automation.

## Environment gate

When `TEST_DATABASE_URL` points to an isolated PostgreSQL database, run
`go test ./tests/integration -count=1` to prove migrations and transaction
contracts. On 2026-07-31, revision
`3338d3c3430025c5766ac21cf9b7842bd7889b6f` passed that suite plus the complete
version-66 migration chain and the provider-runtime migration up/down contract
against an isolated database on the constrained ADR-0033 demo host. The
synthetic acceptance database was removed afterward. This evidence does not
qualify the host as the supported Ubuntu profile.

On 2026-07-31, working-tree snapshot `475aa5c5a69c` based on
`dc5d6b177e193ff0373ad129fe842cbc09185d7b` passed the full portable gate and
constrained demo deployment checks. The live Compose stack recovered from a
simultaneous service restart, retained its PostgreSQL and MinIO volumes and 126
database tables, returned migration version 66, and served the exact snapshot
revision. The API worker loops also exercised PostgreSQL timestamp cutoffs
without the prior parameter-type error. A pinned Playwright 1.62.0 smoke check
rendered the exact build and navigated from the shell to Projects without
relevant browser errors.

Provider acceptance additionally requires synthetic, non-production Graph, Datto, Teams, webhook, external-automation, and AI connections. For Teams, configure a write-only Incoming Webhook through the GUI and verify create, replace, test, enable/disable, Client isolation, redirect refusal, public-address enforcement, retry expiry, and that stale delivery attempts cannot overwrite newer health. For AI, configure the connection through the GUI and verify credential replacement, discovery, explicit model enablement, disclosure opt-in, slow generation, cancellation, retry, nullable usage/cost evidence, and Client isolation.

On August 2 2026, demo revision `d4b3dda` used an authenticated local Platform
Administrator session to configure Ollama 0.14.2 at the approved local-network
boundary, test the connection, discover `nomic-embed-text:latest` and
`qwen3:14b`, explicitly enable `qwen3:14b`, map the enabled summary policy, and
submit a real Client-scoped Work Record summary. The first successful cold run
completed once on attempt 1 after about two minutes. A second warm run completed
once on attempt 1 in about 18 seconds and stored a pending-human recommendation
derived only from `title` and `description`, with the corrected actual completion
timestamp. This proves the core Ollama connection, discovery, selection, policy,
slow polling, generation, and persistence path.

A subsequent live job moved from `queued` to `cancelled`, stayed cancelled after
the worker cycle, and produced no recommendation. A second valid synthetic
Client received the same enumeration-safe 404 when requesting that Acme job,
proving cross-Client lookup isolation. A guarded endpoint-change probe exposed
and fixed two provider-management defects: nullable acknowledgement timestamps
now have an explicit PostgreSQL type, and an enabled-policy/active-job reference
constraint now returns actionable `409 provider_in_use` instead of a generic
500. The protected provider remained enabled, healthy, and unchanged throughout
the failed probes.

On August 3 2026, the authenticated HTTPS demo completed the GUI review at a
1440 by 900 desktop viewport across the core user journeys. That review exposed
and repaired a nullable empty AI-policy feature list that prevented AI Settings
from loading and unstyled Sales detail metadata/forms. AI Settings then rendered
ready without an error, Sales detail controls remained contained and readable,
and the desktop shell retained its full navigation.

The same acceptance run created synthetic inbound and outbound webhook
connections through the GUI. Exact-body signatures returned `202`; a replay
returned `409 webhook_replay`; bad and stale signatures returned the same
enumeration-safe `401 webhook_authentication_failed`; rotating the credential
invalidated the old secret; disablement rejected delivery; and re-enablement
restored acceptance. An `intake.webhook.received` event was durably delivered
to a synthetic public HTTPS endpoint on attempt 1 with HTTP 204. No production
data or credential was used.

The external-automation continuation on revision
`f83d85d43d6197615160c3a28666138fc9ccea5d` exposed and repaired an ambiguous
PostgreSQL parameter type in external Connection persistence. The authenticated
GUI then created a Client-scoped `external_http` Connection using the restricted
environment-secret namespace, created and published a typed
`intake.webhook.received` automation with one `call_http` step, and triggered it
with synthetic signed intake. The run and step both completed on attempt 1
against a public HTTPS 204 endpoint, and no dead letter was created.

On August 2 2026, the same local provider completed the live failed-job retry
gate: after connectivity recovery, job
`d00126c7-41a6-413c-baa8-72f7857ab2dc` moved from `failed` to `queued` and
completed on attempt 2 with a pending-human recommendation; a second retry was
correctly rejected with `409 job_not_retryable`. Demo revision `229756e25558`
then exercised a broader synthetic workload across two isolated Clients:
ten client resources, eight Work Records spanning Incident, Request, Change,
and Problem, two fallback workflows, mixed lifecycle states, internal and
client-visible comments, Tasks, a time entry, two knowledge articles, three
Opportunities, and a Proposal. A new `qwen3:14b` summary completed on attempt 1
in about 49 seconds using only title and description. Cross-Client Work Record,
Opportunity, knowledge, and AI lookups returned enumeration-safe 404s. An API
restart retained the dataset and completed job, readiness reported the exact
revision, all Compose services remained healthy, and no new API 5xx or error
log occurred after the final fixes.

That live expansion also found and repaired three implementation gaps with
regression coverage: the Global Administrator role now receives asset and
internal/client-comment creation capabilities on both new and existing
installations; malformed optional location IDs are rejected as 422 before
persistence; and Task parent IDs are explicitly typed as UUIDs in PostgreSQL.

Demo revision `e3574bf` additionally applied migration 68 and proved the
Entra-optional identity lifecycle: local Platform Administrator login, optimistic
password reset, allowed-network replacement, session revocation after both
security changes, disablement, disabled-login rejection, and 409 classification
for stale mutations. The existing administrator remained enabled. The Entra
candidate endpoint reported `not_connected` with no credential configured;
credential-backed verification and activation remain pending by design.

On August 4 2026, demo revision
`97b705784f301dbfaddc575064421a208108f375` completed authenticated
natural-language Project proposal acceptance at a 1440 by 900 desktop
viewport. With Northwind Legal selected as the active Client, the message
“Create a project titled Onboarding for Northwind Legal with the three setup
tasks, A, B, C.” produced the existing exact-preview `project.create` action
with `planned` lifecycle, `open` task status, and only the supplied name and
task titles. The proposal was explicitly rejected. The Northwind Project
selector remained unchanged with only the pre-existing modernization Project,
proving no synthetic Project was created.

On August 4 2026, demo revision
`da48a4ca8040ae845e6a0b22d33ba21016ce621f` completed authenticated
standalone Task proposal acceptance at a 1440 by 900 desktop viewport. With
Northwind Legal selected as the active Client, the message “Add a task titled
Validate standalone AI task acceptance to project PRJ-PROP-NW-2042 for
Northwind Legal.” resolved the existing modernization Project and produced the
exact-preview `task.create` action. The preview contained the resolved Project,
ordinary `open` status, and only the supplied Task title; it did not add an
owner, estimate, description, or dates. The proposal was explicitly rejected.
The Project task region then reported “No project-level tasks are pending,” and
the synthetic title was absent, proving no Task was created.

## Expanded AI object and Client business-data catalog

On 2026-08-06, the expanded MSP-wide action catalog passed the Task 12
portable gates. It adds closed, capability-gated Client-resource
read/create/update/lifecycle actions across Locations, Contacts, Assets,
Services, and Contracts; minimized Project, Knowledge, and Prospect reads; and
confirmed Knowledge draft, Prospect creation, and Ticket routing proposals.
The user supplies every business value, Client-bound actions require an
explicit authorized active Client, and confirmation reauthorizes and
re-previews the live target before calling the ordinary service.

The environment-gated PostgreSQL suite applied migrations 1–81 to the isolated
`rti_ai_catalog_test_20260805` database on the constrained demo host. It proved
resource creation versus Client deactivation serialization, one successful and
one conflicting same-version update, Location deactivation versus dependent
Contact/Asset mutation locking, atomic resource/audit/outbox rollback, and the
ordinary-create versus Prospect-conversion Client identity race. Fixture
checks found no residue, the exact scratch database was force-dropped and
verified absent, and public health/readiness plus the RTI, Hank, and RTM
Compose project counts remained healthy. The running RTI deployment was not
changed, so this evidence is not publication, deployment, production, HA, or
live browser acceptance.

Publication, exact-revision demo deployment, and the approved 1440×900 browser
journeys completed on 2026-08-06. The live catalog listed all five resource
kinds; prepared and rejected exact creates for all five; completed selected
create, update, deactivate, reactivate, ambiguity, stale-version, inactive,
and in-use checks; searched Projects and Knowledge; rejected a Prospect
proposal; created and revised a Knowledge draft; and routed synthetic incident
`INC-AI-ACCEPT-20260806` from `Global Triage` to
`Northwind Escalations` at version 1 to version 2. The route has matching audit
and outbox correlation `f192bef1-f6f9-48f9-89e6-2b10457e5ece`, zero delivery
attempts, no delivery error, and an empty browser console.

Acceptance also exposed and permanently repaired the ordinary all-Clients
Service Desk setup path for global routing, calendars, and SLA fallback plus
the `/api/v1/me` routing capability allowlist. Revision
`347addfdf5780d6a4507e822f0c352625a9c6027` was the accepted service-desk
runtime. Only one active demo Client and an MSP-global administrator were
available, so cross-Client isolation and a live restricted-principal denial
remain environment-bounded rather than fabricated. Automated contracts cover
those denials. Second-wave and high-impact financial, credential, integration,
and contractual actions remain excluded from the closed catalog.

The subsequent final whole-branch review found six material issues. The source
fix wave now uses two-row-bounded display-ID-first SQL resolution for Client
resources and Client-scoped, two-row-bounded SQL candidates plus the shared
Unicode whitespace and case-normalization contract for Projects and Knowledge;
removes lifecycle/authority field shadowing; locks the active Client before
Knowledge draft and Ticket route writes; preserves typed Client/resource
ambiguity, missing-target, lifecycle, authority, and version errors through
Registry and safe HTTP mapping; and embeds canonical Location identity in
Contact/Asset proposals.
Refreshed Go, vet, build, documentation, diff, and focused frontend gates pass.
A fresh isolated PostgreSQL run applied
migrations 1–81 and additionally proved Knowledge drafting and Ticket routing
serialize against Client deactivation. The exact scratch database was removed,
fixture residue was zero, and the live demo remained unchanged at revision
`f29d1204bdefdd2be01681e81c3b129e4cc0a0a4`. Exact re-review found no remaining
Critical or Important issues. Publication, exact-revision deployment, and
full-page live browser acceptance are complete with the evidence above.

## Second operational AI wave

The approved second operational wave adds exactly `ticket.create`
(`work_record.create`), `ticket.assign` (`work_record.assign`),
`opportunity.list` and `opportunity.get` (`opportunity.read`),
`opportunity.transition` (`opportunity.transition`),
`opportunity.activity.create` (`opportunity.activity.create`),
`proposal.list` and `proposal.get` (`proposal.read`), `proposal.create`
(`proposal.create`), and `knowledge.publish` (`knowledge.publish`).

All Client-bound actions require one explicit authorized active Client and
bounded exact resolution. The user supplies every business value. The source
generates only trusted identity and mutation metadata, rejects unknown fields
and invalid enums, and does not infer Ticket defaults, technicians, Sales
stages or activities, Proposal content/pricing, or Knowledge publication
authority. Proposal get uses an exact display ID, list retains its ordinary
canonical Opportunity-ID filter, and create resolves an exact Opportunity
reference only to create an empty draft container.

Ticket creation shares the ordinary routing/workflow/SLA selection path between
read-only preflight and execution. Confirmation rechecks the stored selection
fence plus Client, Service, and Contract identity/version, and the ordinary
transaction now fences those reference versions as well. Ticket assignment,
Opportunity transition, and Knowledge publication carry required trimmed
reasons into their ordinary audit facts. The closed catalog contains no
Proposal issue/approval/acceptance/conversion or lines/pricing, Opportunity
conversion, Project financial/Change Order authority, time/billing action,
configuration/automation/integration administration, credential/secret
operation, network call, or client-visible/external Knowledge delivery.

Task 11 used only `TEST_DATABASE_URL` inside isolated PostgreSQL processes,
applied all 77 migration files through schema version 81, and passed 29 leaf
cases: 17 deterministic concurrency cases and 12 forced audit/outbox rollback
cases. It proved lock ordering and atomicity for Ticket create/assignment,
Opportunity transition/activity, Proposal draft creation, and internal
Knowledge publication. Fixture residue and other scratch-database connections
were zero, all exact scratch databases and temporary infrastructure were
removed, and RTI/Hank/RTM shared-host inventory was unchanged.

The exact `42ef4dd..51f187c` source review found one Important
confirm-to-commit gap in Ticket creation: Client, Service, and Contract
versions were rechecked before execution but not all were carried into the
ordinary transaction. Task 12 added the narrow version fence and focused
RED/GREEN regression coverage. No other Critical or Important authorization,
enumeration, preflight/execute, transaction-ordering, audit/outbox,
no-invention, high-impact-catalog, or frontend stale-state issue remained.

The reviewed branch was fast-forwarded to `main`, and exact local, pushed, and
remote `main` parity was established at
`963f4759effe28e34eed6033230f0439ea7e890f`. The constrained demo served that
same revision from `/readyz`, `/v1/system/build`, and the rendered shell after
migration 81; all six RTI containers were healthy with zero restarts, and the
shared Hank and RTM projects were unchanged.

At 1440 by 900, an authenticated global administrator used the
`All authorized clients` drawer to reject and then confirm a synthetic Ticket
create, confirm an exact active-technician assignment, list/get Opportunity
and Proposal records, reject an Opportunity transition, activity, and Proposal
draft creation, and reject then confirm publication of a disposable internal
Knowledge draft. Ticket create previewed the ordinary Queue, Workflow, SLA
policy/calendar, and initial-state selection. Stale Ticket version and unknown
technician probes failed safely. The browser console contained no warnings or
errors.

PostgreSQL showed the Ticket at version 2 with matching create/assignment audit
and outbox facts and the supplied assignment reason. The Knowledge Article was
published at version 2 with `client_visible = false`, matching audit/outbox
facts, and the supplied publication reason. Those events produced zero
outbound webhook deliveries, zero external notification deliveries, and zero
automation jobs. The one-Client/two-active-technician global-admin demo cannot
truthfully demonstrate cross-Client, restricted-principal,
inactive-technician, or ambiguous-technician browser denials; deterministic
coverage remains authoritative for those cases.

Detailed evidence is retained in
`.superpowers/sdd/2026-08-06-ai-second-operational-wave/task-13-live-acceptance-report.md`.

Do not place production customer data, credentials, mailbox content, Datto payloads, or provider secrets in fixtures or logs.
