# AI Platform

**Status:** Provider-neutral runtime and permission-aware workspace composed; live workspace acceptance pending

AI is an MSP-opt-in, permission-aware platform service. V1 supports technician-controlled summaries, reply drafts, similar-ticket/knowledge suggestions, governed classification suggestions, cited RTI product guidance, and a closed set of explicitly confirmed ticket actions. Semantic search, correlation, attachment/log analysis, and AI participants are deferred.

AI context is client-isolated, minimized, traceable, and governed by provider/data-retention policy. Provider data sharing is disclosed before enablement. Attachments, credentials, secrets, and sensitive fields are excluded by default, and Rarity never trains its models on customer data. Recommendations identify model/provider, relevant inputs, confidence or limitations, and whether a human accepted them. AI never silently changes material customer data or sends messages/runs automation autonomously.

Provider abstraction is code-owned: V1 ships only the `ollama` and `openai_compatible` adapters. Administrators configure either adapter, its endpoint, optional write-only credential, discovered models, and feature policy in the GUI; credentials are purpose-encrypted in PostgreSQL and never returned by the API. Arbitrary administrator-defined HTTP templates are excluded.

Remote connections require public HTTPS, refuse redirects, and pin validated public DNS addresses. An acknowledged local mode permits HTTP only to loopback/private/link-local destinations for local runtimes such as Ollama. Either adapter may use either network mode after validation.

Remote requests default to five minutes and local requests to 15 minutes; the hard timeout maximum is 60 minutes. Serialized requests default to 1 MiB and cannot exceed 5 MiB. Raw structured responses default to 5 MiB and cannot exceed 10 MiB. The 5 MiB response ceiling limits transport bytes, not model tokens or recommendation text.

The global workspace persists personal MSP/Client-scoped conversations and
messages and retrieves from a curated full-text RTI corpus that rejects
recovery, credential, token, and environment sources. Administrator guidance
is returned only to an administrator-capable principal. The top-bar workspace
remains mounted across navigation and clears Client-bound context when the
active Client changes.

Workspace tools form a closed server registry. Read tools can return product
guidance, authorized navigation, safe integration health, and Client-scoped
ticket reads/search. Ticket actions and direct Project-with-Tasks creation
persist an exact expiring preview, then re-load the principal, reauthorize the
ordinary application capabilities, and re-resolve Client scope before
execution. Versioned targets must also match the previewed version.

Write inputs have a strict provenance boundary: names, titles, descriptions,
dates, owners, estimates, budgets, and other business values must be supplied
by the user. A tool may resolve a user-supplied name to an authorized internal
identifier and may generate identifiers, timestamps, correlation IDs, and
other system metadata. Missing optional business values remain unset; a
missing required or ambiguous value must be returned for clarification rather
than invented. Direct Project creation starts in `planned`, creates only the
explicit task titles in `open`, and records no Proposal lineage.

Arbitrary SQL, HTTP, shell, routes, or administrator-defined tools are
prohibited. A client-visible reply is not represented as sent email; outbound
email remains unavailable until a dedicated delivery service can provide an
equivalent transaction and evidence boundary.

The composed provider runtime continues to enforce MSP opt-in, provider
disclosure, `ai.assist`, Client scope, context minimization, default
sensitive-data exclusion, authorized candidate sets, model/prompt
traceability, nullable usage/cost records, durable cancellation/retry, and
reasoned human decisions.

Classification suggestions use the active MSP-global catalog as their closed
candidate set and require `classification.apply` for the target object. A
pending suggestion is not an assignment. A technician may accept or dismiss
it, and the decision records model, prompt/version, confidence, rationale, and
actor evidence. `classification.ai.manage` separately controls the MSP policy.

Automatic application is disabled by default and must be explicitly enabled
with a bounded confidence threshold. It is additive only: it may add an active
meaningful tag at or above the configured threshold, but it cannot remove a
human-applied tag, replace the final meaningful effective tag, cross Client
scope, or create a catalog value. Failures leave the object unchanged and
available for human review. AI context classification used to minimize or
route model context remains an internal AI safety concept; it is not the
generic Category field removed by the governed-classification cutover.

See [Integration, Automation, and AI Contracts](../04-api/integration-automation-ai-contracts.md).

## Mention boundary

AI is not a mention target and `@AI` is not a command surface. The mention
picker accepts only active internal staff and organizational teams; token APIs
reject an `ai` target type. Mention text does not invoke summarization,
investigation, drafting, classification, actions, approvals, or any other AI
workflow. This exclusion does not reduce the unrelated, explicitly invoked AI
capabilities documented above.
