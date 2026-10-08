# V1 Intake and Integration Contract

**Status:** Accepted V1 direction

## Public API

The public API begins at `/v1`. Integrations use scoped service API keys. Technician personal access tokens are not issued in V1. OAuth is deferred. All keys are attributable, scoped, rotatable, auditable, and denied by default outside their permitted client/data scope.

Outbound webhooks are signed per integration secret and automatically retried with visible delivery history and failure handling.

Microsoft Teams notifications use named Incoming Webhook connections with encrypted URLs and Adaptive Card payloads. Connections are scoped to their configured destination and use a 24-hour retry window. Teams is notification-only in V1; it does not receive internal notes, attachments, credentials, email bodies, or AI context by default.

## Email and system intake

V1 supports Microsoft Graph mailbox intake, standard email forwarding, direct API intake, and inbound webhooks. A dedicated Microsoft 365 shared support mailbox creates new tickets or updates a threaded ticket. Basic Graph notifications trigger validated server-side retrieval; lifecycle events and a per-folder delta query reconcile missed notifications at least every five minutes. Rarity stores original MIME, normalized content, and authorized attachments encrypted in object storage for seven years by default. Forwarding enters a protected Rarity intake address. APIs and webhooks are system intake sources.

## Datto RMM

Datto RMM is the first full RMM integration. V1 supports read/ingest-only full asset sync plus manual/API asset creation and update. An initial full sync is followed by configurable incremental sync, defaulting to 15 minutes, with manual sync, progress, rate-limit status, and error visibility.

A reconciliation surface shows candidate matches and differences. Authorized users may link, choose Rarity, choose Datto, or keep records separate; all choices are audited. Routine configured rules may reconcile automatically; ambiguous/conflicting values require review. Retired or missing Datto assets become stale/inactive for review and are never auto-deleted.

One Datto alert identity maps to one open incident. Similar repeated alerts deduplicate or update the same open incident using a configurable fingerprint/window, defaulting to 24 hours. A cleared alert appends a recovery event but does not automatically resolve the incident. No writes are made back to Datto in V1.
