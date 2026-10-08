# Integration, Automation, and AI Contracts

**Status:** Integration, automation, and provider-neutral AI source runtimes composed; second operational AI wave published and accepted on the constrained demo; remaining configured providers pending

## Integration identity and intake

Service API keys are opaque `rsk_` bearer credentials. Rarity returns the secret once and persists only its SHA-256 digest and non-secret lookup prefix. Each key has an MSP, optional Client, capability allowlist, data-scope allowlist, expiry, attribution, and auditable rotation/revocation history. Missing grants are denied by default.

The public lifecycle is restricted to `service_key.manage`: `GET /api/v1/admin/service-keys` lists at most 250 keys for the active Client using non-secret metadata, `POST /api/v1/admin/service-keys` issues a key, and `POST /api/v1/admin/service-keys/{id}/rotate` and `/revoke` address the existing credential by non-secret key ID and require an audit reason. Existing bearer secrets are never accepted by lifecycle requests. Issue and rotation responses return the new secret once; list and subsequent API representations exclude both the bearer token and stored digest. The permission-aware GUI makes this lifecycle discoverable while preserving that one-time reveal contract.

Inbound and outbound webhooks use an exact-body HMAC-SHA-256 signature over timestamp, event identity, and body. Verification requires an allowed clock window and an atomic replay claim. Outbound connections deliver only explicitly allowlisted event types within their MSP/optional Client scope. Each event is planned once into durable per-connection work, claimed with a visibility lease, and retried with bounded backoff inside the configured maximum 24-hour window. GUI-managed signing secrets are purpose-encrypted and write-only; existing `env://RARITY_WEBHOOK_SECRET_*` references remain supported for deployment compatibility. Both formats resolve only at inbound verification or outbound delivery time. Destinations require HTTPS, disable redirects, and reject local, private, link-local, user-info, malformed, or private-DNS/rebinding targets. Exact signed envelopes contain canonical event and subject metadata plus the event's safe `data` object; delivery attempts retain stable error codes and immutable visible history.

Authenticated operators with `integration.read` use `GET /api/v1/integrations/webhooks/deliveries` to inspect the newest 200 durable deliveries in the active Client, including retry state, attempt count, terminal failure code, and latest safe HTTP result. Administrators with `integration.manage` use `GET|POST /api/v1/integrations/webhooks/connections`, `PATCH /api/v1/integrations/webhooks/connections/{id}`, and `POST /api/v1/integrations/webhooks/connections/{id}/credential` for scoped creation, metadata/enablement changes, and reason-required credential replacement with optimistic versions. A terminal delivery can be explicitly granted a fresh bounded retry window through `/connections/{id}/deliveries/{event_id}/retry`; the reset and its audit/outbox evidence commit atomically and enumeration-safe scope applies. The Webhooks GUI separates read and management capabilities and never receives a signing-secret reference, ciphertext, or plaintext after submission.

Microsoft Graph notifications contain no trusted message content. Rarity validates the subscription client state and dedicated mailbox resource, then retrieves the message server-side. Graph conversation identity is the first threading key, followed by internet message headers and an explicit Rarity reply token. The reply token combines the visible `RTY-<number>` display ID with a 26-character opaque secret; a predictable display ID alone never selects an existing thread. Subject-only matching is prohibited. Message persistence and each folder’s next delta cursor commit atomically, and reconciliation is due every five minutes.

Administrators with `integration.manage` use `GET|POST /api/v1/integrations/graph/mailboxes`, `PATCH /api/v1/integrations/graph/mailboxes/{id}`, and `/credential` to provision and govern MSP mailbox intake. Entra tenant/application metadata is visible, but the client secret and server-generated notification client state are purpose-encrypted and write-only. Existing dedicated `env://RARITY_GRAPH_CREDENTIAL_*` and `env://RARITY_GRAPH_CLIENT_STATE_*` references remain runtime-compatible. Metadata, enablement, and credential replacement are reason-required and optimistically versioned; a change fences older claimed work and marks the subscription for recreation.

Forwarding intake uses an authenticated MSP-scoped relay handoff and requires the protected recipient, an allowlisted sender domain, aligned envelope/header sender domains, DMARC plus SPF or DKIM, size limits, and durable connection-global/sender rate limits. Exact MIME is stored outside PostgreSQL. Rejected input remains visible in quarantine with a stable reason and connection-scoped identity; the upstream mailbox protection remains responsible for malware screening. Direct API and inbound webhook sources require an authenticated scoped identity and a durable raw-payload reference.

Administrators with `integration.manage` use `GET|POST /api/v1/integrations/forwarding/connections` and `PATCH /api/v1/integrations/forwarding/connections/{id}` to manage the MSP-global forwarding boundary. Intake addresses, normalized sender-domain allowlists, 1–50 MiB message ceilings, bounded per-minute limits, and enablement are reason-required and optimistically versioned; each change commits its audit and outbox facts atomically. The Forwarding intake GUI exposes current health and receipt freshness without exposing raw messages.

Datto is read/ingest-only. A new connection performs a full inventory snapshot; later runs are incremental every 15 minutes by default or manually requested. Stable Datto identifiers are authoritative matches. Hostname, serial, and MAC evidence creates review candidates only. Missing assets become stale, never deleted. Cleared alerts append recovery evidence without resolving the linked Incident.

The permission-aware Datto reconciliation GUI loads at most 100 pending candidates for the active Client through `integration.datto.reconcile`, presents the stored match evidence, and offers only the typed `link`, `choose_rarity`, `choose_datto`, or `keep_separate` decisions. Every submission requires an operator reason and uses the existing atomic decision, asset, audit, and outbox boundary.

Teams resolves an Incoming Webhook connection at delivery time and sends only the approved compact card. Administrators manage named MSP/optional Client destinations through the GUI; webhook URLs are write-only, purpose-encrypted, replaceable, and never returned by the API. Existing `env://RARITY_TEAMS_WEBHOOK_*` connections remain readable for migration compatibility. Create, metadata change, credential replacement, enable/disable, and test actions require `integration.manage`, an audit reason, scoped authorization, and optimistic version checks where state changes. Remote tests and delivery require public HTTPS, DNS-pinned dialing, proxy bypass, and redirect refusal. Failures retry for at most 24 hours and then remain visible; immutable attempts preserve the claimed connection version while stale attempts cannot overwrite newer connection health. Unified health evaluates freshness, queue delay, credential expiry, consecutive failures, and pending delivery failures for every connection.

## Typed automation

Published automation versions are immutable. Definitions contain an event trigger, nested typed conditions/branches, waits, and these V1 actions only:

- update a supported field;
- add a comment through the application service;
- assign supported ownership;
- perform a governed workflow transition;
- call an approved external HTTP Connection by reference.

Definitions cannot contain credentials or actions for shell/code execution, direct database access, local files, or remote sessions. Runs use an automation principal limited to the published capability and Client allowlists. The connection’s scope is checked separately for external calls.

Each run uses a unique idempotency key and records trigger event, immutable version, sanitized input field set, causation, depth, attempt, step outcomes, changed object identities, duration, retry, and safe error codes. Depth is capped at eight. Waits are durable. Exhausted runs enter a scoped dead-letter queue with reason-required, audited retry, replay, or dismissal.

The authenticated active-Client management boundary requires `automation.manage`. `POST /api/v1/automation/definitions` creates a Client-scoped draft; `POST /api/v1/automation/definitions/{id}/versions/{version}/revisions` creates the next draft from a published version; and `POST /api/v1/automation/definitions/{id}/versions/{version}/publish` performs the only permitted immutable-version state transition. Revision and publication require the definition record's optimistic `expected_version`. `POST /api/v1/automation/connections` creates an enabled Client-scoped `external_http` Connection with only `automation.call_http` and safe automation-input access. Destinations must pass the public HTTPS check, signing secrets must use the restricted `env://RARITY_AUTOMATION_HTTP_SECRET_*` namespace, and responses never disclose the secret reference.

`GET /api/v1/automation/definitions` returns the latest version visible in the
active Client scope. The Automation GUI supports typed draft creation,
publication, and revision of a published version without exposing Connection
credentials.

`POST /api/v1/automation/dead-letters/{id}/actions` exposes `retry`, `replay`, and `dismiss` through the authenticated active-Client boundary. Lookup is enumeration-safe, requires `automation.dead_letter.manage` and a non-empty reason, and atomically writes the state transition, action evidence, audit record, outbox event, and any resulting schedule. Retry grants the failed run one additional attempt from its failed step. Replay creates a new execution-job generation with a distinct idempotency key while retaining the original failed run. The asynchronous workers execute either schedule.

`GET /api/v1/automation/dead-letters` returns the 100 newest scoped failures
with open items first. The same permission-aware GUI makes open failures
discoverable and requires an explicit reason before retry, replay, or dismissal.

## Constrained AI assistance

AI is disabled until the MSP opts in and accepts the named provider disclosure. V1 supports only technician-requested summaries, reply drafts, and similar-ticket/knowledge suggestions. The trusted principal must have `ai.assist` in the target Client.

Only standard, relevant context fields cross the provider boundary. Attachments, credentials, secrets, and sensitive fields are excluded by default. Similarity output is constrained to candidate identities already retrieved within the caller’s permissions.

Every recommendation records provider, model, prompt version, relevant input field names, confidence, candidate identities, generated time, and usage/cost. Output remains `pending_human` until accepted or rejected. A decision records actor, time, and reason; the decision contract enforces `applied: false` and `sent: false`.

Provider connections use the code-owned `ollama` or `openai_compatible` adapter. The GUI is the credential configuration boundary: plaintext exists only in request memory and protected-secret sealing/opening calls, PostgreSQL stores purpose-encrypted ciphertext, and every API representation exposes only `credential_configured` metadata. Ollama and other self-hosted endpoints may use explicitly acknowledged local mode. Remote mode requires public HTTPS; both modes refuse redirects and revalidate destinations against DNS rebinding.

Remote timeout defaults to five minutes, local timeout defaults to 15 minutes, and the hard maximum is 60 minutes. The serialized request default is 1 MiB with a 5 MiB maximum. The raw structured response default is 5 MiB with a 10 MiB maximum; 5 MiB is a transport-byte ceiling, not a model-token limit.

Provider management uses `GET|POST /api/v1/ai/providers`, `PATCH /api/v1/ai/providers/{id}`, `POST /api/v1/ai/providers/{id}/credential`, `/test`, and `/discover-models`, `GET|PATCH /api/v1/ai/providers/{id}/models`, and `GET|PATCH /api/v1/ai/policy`. Management is MSP-scoped and requires `ai.manage`; optimistic versions protect updates.

`POST /api/v1/work-records/{id}/ai/jobs` submits a Client-scoped durable job with an idempotency key. `GET /api/v1/ai/jobs/{id}`, `POST /api/v1/ai/jobs/{id}/cancel`, and `POST /api/v1/ai/jobs/{id}/retry` expose enumeration-safe progress and reasoned lifecycle actions. Leases, concurrency limits, cancellation, request/response ceilings, and provider timeouts remain durable across API restarts. Context and similarity candidates are loaded server-side in the active Client.

`GET /api/v1/ai/recommendations/{id}` returns only the scoped review projection and relevant input field names. `POST /api/v1/ai/recommendations/{id}/decide` accepts an authenticated Client-scoped `accepted` or `rejected` decision with a required reason. Lookup is enumeration-safe within the principal's active Client. The recommendation state, immutable decision evidence, audit record, and outbox event commit atomically; the response explicitly reports `applied: false` and `sent: false`.

### Second operational workspace wave

The MSP-wide workspace now composes these closed tools and exact capabilities:

- `ticket.create` requires `work_record.create`;
- `ticket.assign` requires `work_record.assign`;
- `opportunity.list` and `opportunity.get` require `opportunity.read`;
- `opportunity.transition` requires `opportunity.transition`;
- `opportunity.activity.create` requires `opportunity.activity.create`;
- `proposal.list` and `proposal.get` require `proposal.read`;
- `proposal.create` requires `proposal.create`; and
- `knowledge.publish` requires `knowledge.publish`.

Every Client-bound request names one explicit authorized active Client. Exact
resolvers are Client-scoped, normalized, deterministic, and bounded to two
candidates. Opportunity references prefer an exact display ID over an exact
name. `proposal.get` accepts only an exact Proposal display ID;
`proposal.list` may use the ordinary canonical `opportunity_id` filter; and
`proposal.create` resolves one exact Opportunity display ID or name before
creating only an empty version-1 draft container with the supplied Proposal
display ID. Proposal lines, pricing, issue, approval, acceptance, and
conversion are not part of that reference or mutation contract.

The user supplies every business value: Client and object references, Ticket
display ID/type/title/description/status/priority and optional Service or
Contract, assignment target/version/reason, Opportunity destination or
activity content, Proposal display ID, and Knowledge Article version and
publication reason. Rarity generates only trusted internal IDs, timestamps,
actor, correlation, and causation metadata. Missing values cause
clarification or rejection; the planner and tools do not infer defaults,
owners, stages, content, prices, or approval/publication decisions.

`ticket.create` uses the ordinary read-only Work Record preflight for the same
routing, workflow, initial-state, SLA, Service, and Contract selection used by
execution. The stored fence includes routing rule-set, Queue, Workflow, SLA
policy, and calendar identities/versions. Confirmation reloads the Client and
optional references, reruns preflight, and compares the exact preview.
Execution additionally carries the confirmed Client, Service, and Contract
versions into the ordinary transaction so a confirm-to-commit change fails
closed.

Assignment, Opportunity transition, and internal Knowledge publication require
non-empty reasons on both AI and ordinary callers. The trimmed reason is stored
on `work_record.owner.changed`, `opportunity.stage.changed`, and
`knowledge.published` audit facts. All writes call ordinary services with
source `ai_workspace`; rejection, expiry, ambiguity, authorization loss,
version/configuration drift, or a failed writer leaves no partial domain,
audit, or outbox set.

Proposal issue/approval/acceptance/conversion and line pricing remain absent,
as do Opportunity creation/conversion or arbitrary field replacement, Project
financials and Change Orders, time/billing authority, configuration and role
administration, automation publication/execution/replay, integrations,
network calls, credentials/secrets, and client-visible or external Knowledge
delivery.

The isolated PostgreSQL proof applied migrations 1–81 and passed 29 leaf cases:
17 deterministic concurrency cases and 12 forced audit/outbox rollback cases
covering Ticket create/assignment, Opportunity transition/activity, Proposal
draft creation, and internal Knowledge publication.

The exact pushed revision
`963f4759effe28e34eed6033230f0439ea7e890f` then passed constrained-demo
deployment and authenticated 1440-by-900 acceptance. The all-authorized-Clients
drawer exposed the closed catalog; exact Ticket create/assignment, Sales
reads, rejected Opportunity/Proposal writes, and rejected-then-confirmed
internal Knowledge publication behaved as designed. Matching Ticket and
Knowledge audit/outbox facts were present, the Knowledge fixture remained
`client_visible = false`, outbound webhook and external notification
deliveries plus automation jobs were zero, and the browser console was empty.
The single-Client/global-admin fixture cannot supply genuine cross-Client,
restricted-principal, inactive-technician, or ambiguous-technician browser
denials; deterministic automated coverage remains the evidence for those
cases.

## Runtime boundary

Service API keys now use the runnable API's PostgreSQL repository and bearer-principal boundary, including atomic issue, rotation, revocation, audit, and outbox persistence. `POST /api/v1/intake/direct` uses that trusted Client scope and `intake.write`, preserves an exact valid-JSON body in object storage, and atomically records a Client-scoped idempotent external identity with inbound-event, audit, and outbox evidence. `POST /api/v1/intake/forwarding/{connection_id}` provides the authenticated mail-relay boundary, persists exact MIME plus accepted/quarantined connection-scoped evidence, enforces durable rate windows, and updates authoritative forwarding health in the event transaction. `POST /api/v1/webhooks/inbound/{connection_id}` resolves only configured Client-scoped inbound connections, authenticates the exact body with a dedicated environment secret reference and five-minute timestamp window, then commits its replay claim and connection-scoped external identity with the inbound event and mutation evidence in one transaction. Authentication errors do not reveal connection existence, independent connections cannot collide on a provider event ID, and API responses never expose raw object references. The API process also plans and delivers durable outbound webhook jobs every five seconds using exact event allowlists, current enabled connection scope, delivery-time environment secrets, attempt-guarded state transitions, bounded retry, redirect refusal, and DNS-aware public-address enforcement.

`POST /api/v1/graph/notifications` now provides the public Microsoft Graph validation and basic/lifecycle notification boundary. It validates every notification's subscription, mailbox resource, and separately referenced client state before atomically queuing message hints or lifecycle recovery. Leased workers use separately referenced Entra application credentials, refuse redirects and off-origin delta cursors, retrieve exact MIME server-side, and store purpose-encrypted delta cursors in the same PostgreSQL transaction as normalized message, audit, outbox, and connection-health state. Notification hints run every five seconds, while independently leased delta reconciliation remains due at least every five minutes and clears missed-notification recovery only after a successful cursor commit.

`GET /api/v1/integrations/health` composes the MSP-scoped durable health view and requires `integration.read`; the view itself retains MSP scope before the repository builds the worst-state snapshot. Automation definition/Connection management, dead-letter management, and AI recommendation accept/reject decisions use the same runnable principal boundary and PostgreSQL transaction contracts.

The API process now plans Client-scoped outbox events exactly once against each enabled current published Automation Version, leases execution jobs, and runs them under the version's capability allowlist. Application actions retain their normal authorization, optimistic-version, audit, and outbox boundaries. External HTTP actions re-authorize the current Connection scope, resolve a dedicated environment signing-secret reference at call time, refuse unsafe destinations and redirects, and emit a signed safe-input envelope. Successful action output advances the in-run object version; durable waits and bounded retries resume from the recorded step with attempt-specific history, while terminal failures create the run failure and dead letter atomically. Causation identifies the parent automation run so recursive events remain subject to the depth-eight ceiling.

The API process now composes protected provider management, adapter discovery/testing, durable AI job execution, and safe shutdown. Live Graph, Datto, Teams, external-automation, and AI acceptance still requires configured encrypted non-production connections. The isolated PostgreSQL migration and repository gate is recorded as passed in [Integration, Automation, and AI Acceptance](../../docs/06-development/integration-automation-ai-acceptance.md). Deployed external-provider acceptance is not claimed by this document.
