# Provider-Agnostic AI Runtime Design

**Date:** 2026-07-29

**Status:** Approved design

**Scope:** MSP AI provider settings, model profiles, durable generation, technician review, and administrator GUI

## Goal

Rarity will provide technician-requested summaries, reply drafts, and similar-ticket or knowledge suggestions without binding its runtime to one model vendor. Administrators configure providers, credentials, models, and feature defaults through the GUI. The initial adapters support native Ollama and OpenAI-compatible APIs; later native provider adapters use the same internal contract.

AI remains MSP opt-in, Client-isolated, permission-aware, context-minimized, and human-controlled. A recommendation cannot apply a record change, send a message, or start an automation.

## Decisions

- Provider configuration and API credentials belong in an administrator GUI, not process-specific provider environment variables.
- Provider-neutral generation uses a typed adapter registry.
- Initial adapters are `ollama` and `openai_compatible`.
- Provider connections have explicit `local` and `remote` network modes.
- Local mode may reach loopback or private-network endpoints after an administrator acknowledgment; remote mode requires public HTTPS.
- Generation is a durable background job rather than a long-lived browser request.
- Remote providers default to a five-minute timeout and local providers to fifteen minutes. Administrators may configure up to a sixty-minute hard maximum.
- Raw structured provider responses default to a 5 MiB ceiling and may be configured up to a 10 MiB hard maximum.
- V1 provider responses are structured JSON or text. Binary and future multimodal artifacts belong in encrypted object storage rather than PostgreSQL.
- Credentials are encrypted with Rarity's protected-secret provider, are write-only after submission, and are never included in API responses, logs, audit details, or provider health errors.

## Architecture

### Provider adapter registry

The AI domain depends on a provider-neutral adapter interface with operations for connection testing, model discovery, and generation. Job orchestration, policy, persistence, authorization, audit, and recommendation handling contain no vendor-specific branches.

The native Ollama adapter owns Ollama paths, request shapes, model discovery, response parsing, and usage extraction. The OpenAI-compatible adapter owns its corresponding paths and protocol. Each adapter declares whether credentials are optional or required and which feature and usage capabilities it supports.

Administrator-defined arbitrary HTTP templates are excluded. Provider-specific request paths, headers, secret placement, and response parsing remain trusted code.

### Provider connections

An MSP-scoped provider connection stores:

- stable identity, display name, adapter type, and optimistic version;
- local or remote network mode;
- normalized base URL;
- encrypted credential state without a readable credential representation;
- enabled state and health state;
- configurable execution timeout;
- configurable raw-response ceiling;
- provider-disclosure acceptance;
- local-network acknowledgment, actor, and time when local mode is selected;
- last test time, last success time, and safe last-error code.

Creating, replacing credentials, changing endpoints or network modes, enabling, and disabling are reasoned, audited actions. Moving a connection into local mode requires a fresh local-network acknowledgment.

### Model profiles

A model profile belongs to one provider connection and stores:

- provider model identifier and administrator-facing display name;
- enabled state and optimistic version;
- supported Rarity features;
- configured context and generated-output limits;
- provider capability metadata discovered by the adapter;
- last discovery time.

Multiple model profiles may use one provider connection. Model discovery does not automatically enable a model or assign it to a feature.

### MSP AI policy

The MSP policy is disabled by default. Enabling it requires accepted provider disclosure and at least one valid enabled feature mapping. Each supported feature independently selects a default model profile:

- summary;
- reply draft;
- similar-ticket or knowledge suggestions.

The policy retains a monthly cost limit and optimistic version. A zero limit means no paid usage; profiles explicitly classified as zero-cost, such as a local Ollama model, remain available. Unlimited paid operation is a separate explicit policy state rather than an overloaded zero. A provider whose cost cannot be determined is disabled under a finite limit unless an administrator explicitly enables unmetered-unknown usage. Unknown cost is never recorded as zero.

### Durable generation jobs

The submission API authenticates `ai.assist`, verifies active Client scope, loads the Work Record and authorized candidates server-side, validates the current policy and model mapping, records the approved input-field names, and creates a queued job. It returns `202 Accepted` with a job identity.

A leased worker:

1. Reloads the job, current policy, model profile, connection, and scope.
2. Reauthorizes the requester and checks that AI remains enabled.
3. Loads and minimizes authorized context server-side.
4. Resolves the encrypted credential only in memory.
5. Calls the selected adapter under the connection limits.
6. Validates text, confidence, candidates, and usage metadata.
7. Atomically records the pending-human recommendation, usage/cost evidence, job completion, audit record, and outbox event.

Jobs use queued, running, completed, failed, and cancelled states with visibility leases, bounded retry, cancellation, and lease recovery. Retries apply only to transient provider or transport failures. Invalid output, authentication failure, unsafe destinations, policy changes, response-size violations, and authorization failures do not retry automatically.

### Recommendation boundary

Successful generation creates a `pending_human` recommendation. Accepting or rejecting it records a reasoned human decision but continues to report `applied: false` and `sent: false`. Acceptance does not copy text into a Work Record or message automatically.

## Context and candidate safety

The browser supplies the feature and Work Record identity only. It cannot supply trusted context, classifications, candidate authorization, provider settings, or a model override outside the current MSP policy.

Rarity builds context from authorized server-side records. V1 permits approved standard text fields only. It excludes:

- credentials, secrets, tokens, and protected configuration;
- attachments and binary content;
- sensitive or restricted classifications not explicitly approved for the feature;
- unrelated Client or MSP records;
- internal-only fields that the requesting principal cannot read.

The existing 32,000-character minimized-context ceiling becomes the default for profiles without discovered context metadata. A model profile may define a lower or higher adapter-aware context budget. The default total serialized provider request ceiling is 1 MiB, configurable up to a 5 MiB hard maximum, with a bounded field count. Authorized candidate IDs are retrieved within the same active Client and supplied to the adapter; returned candidates must be a subset.

## Network and transport security

### Remote mode

Remote connections require public HTTPS. The transport:

- refuses redirects;
- rejects loopback, private, link-local, multicast, unspecified, and metadata addresses;
- resolves and pins public addresses at dial time to defend against DNS rebinding;
- sends credentials only to the configured origin;
- uses adapter-owned paths only;
- applies bounded request, response, header, and connection limits.

### Local mode

Local connections may use HTTP or HTTPS and may target loopback, private IPs, local DNS names, or Compose service names. This access is permitted only for connections explicitly marked local with a recorded administrator acknowledgment.

Local mode still:

- refuses redirects;
- confines requests to the configured origin and adapter-owned paths;
- applies time, size, concurrency, and cancellation limits;
- never treats “local network” as proof that the provider is on the same machine;
- never logs credentials, prompts, or raw provider bodies.

Changing a remote connection to local mode or changing a local endpoint requires a fresh acknowledgment and audit record.

## Limits

- Remote execution timeout default: 5 minutes.
- Local execution timeout default: 15 minutes.
- Configurable execution-time hard maximum: 60 minutes.
- Serialized provider request default ceiling: 1 MiB.
- Configurable serialized-request hard maximum: 5 MiB.
- Raw structured response default: 5 MiB.
- Configurable raw-response hard maximum: 10 MiB.
- Response parsing is streaming and bounded; the worker stops reading when the configured ceiling is exceeded.
- Generated recommendation text, candidate count, field count, and metadata lengths have separate practical limits.
- Provider-reported usage records input units, output units, and cost when available. Unreported cost or usage remains unknown rather than being stored as a misleading zero.

## GUI

### AI Providers settings

The administrator page lists provider name, adapter, local/remote mode, enabled state, credential-configured state, connection health, model count, last successful test, and safe last-error code.

The create/edit workflow:

1. Selects Ollama or OpenAI-compatible.
2. Selects local or remote network mode.
3. Enters the base URL and optional or required API credential.
4. Configures timeout and raw-response ceiling.
5. Records local-network acknowledgment when required.
6. Tests the connection.
7. Discovers available models.
8. Selects models and supported features.

Credentials are write-only. The GUI shows only whether a credential is configured. Credential replacement, clearing when permitted, endpoint changes, mode changes, and enable/disable actions require a reason and current object version.

### AI policy settings

The policy panel enables or disables AI for the MSP, presents provider disclosure, selects a default model profile per feature, and shows configured cost governance. Invalid or disabled connections and models cannot be selected.

### Technician workflow

The technician action enqueues a job and immediately shows its durable state. The technician may navigate away, return, cancel a queued or running job, retry an eligible failed job, and review the completed recommendation. The UI distinguishes local network from same-machine execution and shows the names of context fields approved for sharing.

## API shape

The implementation plan will preserve normal `/api/v1` conventions and define versioned routes for:

- provider connection create, list, update, credential replacement, enable/disable, test, and model discovery;
- model profile list and update;
- MSP AI policy read and update;
- Work Record generation submission;
- generation job read, cancel, and eligible retry;
- existing recommendation accept/reject decisions.

All management routes require a dedicated AI-management capability, strict size-limited decoding, optimistic versions, enumeration-safe scope handling, audit evidence, and secret-free responses. Technician generation and job routes require `ai.assist`.

## Failure handling and observability

The GUI exposes safe categories such as unavailable, authentication failed, model unavailable, timeout, cancelled, response too large, invalid response, policy changed, and unsafe endpoint. Logs and metrics use connection and job identities, durations, state transitions, unit counts, and safe codes without prompts, response bodies, Work Record contents, or credentials.

Cancellation propagates to in-flight HTTP requests. A crashed worker leaves an expiring lease so another worker may recover the job. Completion is idempotent, and a retry cannot create duplicate recommendations.

## Verification

Portable verification uses synthetic providers only.

- Domain tests cover authorization, Client isolation, opt-in policy, local-network acknowledgment, credential write-only behavior, feature routing, cancellation, retry classification, and the human-only recommendation boundary.
- Shared adapter contract tests cover model discovery, generation, usage parsing, malformed output, timeout, cancellation, 5/10 MiB limits, and secret-redacted failures for Ollama and OpenAI-compatible fixtures.
- Security tests cover remote SSRF defenses, local-origin confinement, redirect refusal, DNS rebinding, credential non-disclosure, server-owned context, and authorized candidate subsets.
- Repository tests cover encrypted credential storage, optimistic updates, leases, idempotency, and atomic job/recommendation/usage/audit/outbox persistence.
- HTTP and frontend tests cover provider setup, credential replacement, model selection, policy enablement, generation progress, cancellation, eligible retry, and recommendation review.
- Main composition tests prove that management services and workers use the same protected-secret and PostgreSQL boundaries.

Live Ollama, hosted-provider, and PostgreSQL verification remains an explicit environment gate. It must prove model discovery, slow local inference, cancellation, credential replacement, provider usage evidence, and Client isolation with synthetic non-production data.

## Explicit exclusions

- Autonomous application or sending of AI output.
- Provider-defined arbitrary HTTP templates.
- Browser-supplied trusted context or candidate authorization.
- Attachments, binary responses, and multimodal generation in V1.
- Semantic indexing, autonomous agents, classification, and attachment/log analysis.
- Claiming live provider or PostgreSQL acceptance from portable fixture tests.
