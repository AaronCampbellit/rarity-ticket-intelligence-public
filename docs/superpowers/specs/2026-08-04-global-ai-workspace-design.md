# Global AI Workspace Design

**Date:** 2026-08-04

**Status:** Approved design

**Scope:** Global AI chat, RTI knowledge, permission-filtered operational context, and confirmed typed actions

## Goal

Rarity will provide an AI workspace from the authenticated top bar on every page. It can explain RTI, retrieve authorized operational context, and propose typed actions that execute only with the logged-in user's live permissions and explicit confirmation for every write.

## Decisions

- The AI workspace is a top-bar popdown/drawer available throughout the authenticated application.
- Read-only tools may execute immediately.
- Every write shows an exact preview and requires explicit user confirmation.
- The model never receives arbitrary database or HTTP access.
- Tools call existing application services and inherit their authorization, scope, validation, audit, and outbox boundaries.
- The server reauthorizes the logged-in principal immediately before tool execution.
- RTI product knowledge includes safe configuration, health, status, and documentation content, but excludes credentials, secrets, tokens, and recovery data.
- Operational retrieval is MSP- and Client-isolated and filtered to records the principal may read.
- Existing provider policy, durable jobs, protected credentials, response limits, and safe logging remain in force.

## Architecture

### Global conversation shell

`AppShell` owns a global AI trigger and drawer. Conversation sessions are server-side and scoped to MSP, principal, and active Client. The browser may send the current route and object identity as an untrusted navigation hint; the server reloads and authorizes all context.

The existing standalone AI page remains only as a deep-link compatibility route during migration. Primary navigation no longer treats AI as a separate destination.

### Knowledge planes

The product-help plane ingests an explicitly curated, versioned corpus built from approved RTI documentation. Entries contain source identity, version, section, audience, safe classification, and text. Build-time ingestion rejects secret-like paths and protected operational data.

The operational plane queries existing ticket, project, organization, contact, asset, knowledge, and configuration read services. Each query declares its required capability and scope. Results are minimized before provider disclosure.

PostgreSQL full-text search is the initial retrieval implementation. Semantic/vector indexing is deferred until it demonstrates material retrieval benefit and has a separately approved data-governance design.

### Typed tool registry

Each tool declares:

- stable name and version;
- JSON input schema;
- required capability;
- scope resolver;
- read or write classification;
- preview builder;
- application-service executor;
- output minimizer.

The model emits a tool proposal. The server validates the schema, resolves scope, and authorizes the current principal. Read tools may execute and return minimized results. Write tools create an expiring proposal containing normalized input, target identity/version, preview, and required capability.

Confirmation reloads the principal, target, and current version; reauthorizes; verifies the proposal has not expired or changed; and invokes the application service. Audit records identify the user as actor and include AI source, conversation ID, proposal ID, correlation ID, and tool version.

### Tool delivery order

1. Product-help, navigation, status, and authorized read/search tools.
2. Ticket and related-object creation proposals.
3. Ticket field, assignment, status, note, and email proposals.
4. Other object update proposals using existing services.
5. Automation-run proposals after policy, preview, and high-impact confirmation controls are proven.

Tools are added individually with contract tests. There is no generic model-generated route, SQL, or arbitrary automation input.

## API shape

Versioned `/api/v1` routes cover:

- conversation create, list, get, and archive;
- message submission and durable response state;
- proposal read, confirm, and reject;
- curated product-corpus status and administrator rebuild;
- tool catalog metadata safe for the current principal.

Conversation messages store role, safe text, referenced object identities, provider job identity, and timestamps. They do not duplicate protected provider credentials or unrestricted record snapshots.

## GUI

The top bar exposes a consistent AI trigger with unread/running state. The drawer preserves its conversation while the user navigates. It shows active Client, cited RTI documentation, referenced records, durable job progress, and safe errors.

Write proposals render the exact target, changed fields, expected version, and effect. Confirm and reject are explicit actions. The UI never represents a proposal as completed until the server reports the trusted mutation result.

## Failure handling

- Client switching closes or rebinds the visible conversation only after explicit confirmation; context never leaks across Clients.
- Lost capabilities, stale target versions, expired proposals, or deleted targets prevent execution.
- Provider failures preserve the conversation and expose only safe error codes.
- Tool output and conversation context remain subject to existing provider request/response ceilings.
- A disabled AI policy or model mapping blocks new messages and proposal execution.
- High-impact tools can be disabled independently without disabling read-only chat.

## Verification

- Registry tests prove schema validation, capability declaration, scope resolution, preview generation, and absence of arbitrary execution.
- Authorization tests prove live reauthorization and Client isolation at proposal and confirmation time.
- Corpus tests prove versioning, safe-source allowlisting, secret exclusion, and useful product-help retrieval.
- Durable-job tests cover navigation away, retry, cancellation, and conversation resumption.
- Frontend tests cover global availability, Client switching, citations, proposal previews, confirmation, rejection, stale proposals, and safe failures.
- Live acceptance uses synthetic non-production records and confirms that a principal cannot read or mutate anything unavailable through the normal UI/API.

## Explicit exclusions

- Silent or autonomous material writes.
- Arbitrary SQL, HTTP requests, shell commands, or unregistered API calls.
- Secrets, credentials, tokens, recovery material, or raw protected configuration in AI context.
- Cross-Client conversational memory.
- Vector search in the initial delivery.

