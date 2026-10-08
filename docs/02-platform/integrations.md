# Integrations and Connections

**Status:** Implemented source contracts; provider acceptance pending

An Integration defines behavior and schemas; a Connection is a configured instance with encrypted credentials, permissions, endpoints, health, rate limits, and audit.

V1 covers Microsoft Graph/forwarded email, inbound/outbound webhooks, external HTTP actions, monitoring/API intake, and Datto RMM read/ingest-only asset synchronization. Connections use least privilege, destination controls, secret rotation, redacted diagnostics, and explicit client/data scope.

Microsoft Graph intake uses a dedicated Microsoft 365 shared support mailbox and an Entra application restricted to that mailbox. Basic Graph notifications initiate secure retrieval; lifecycle notifications and a per-folder delta cursor recover missed events. A reconciliation pass runs at least every five minutes. Standard forwarding is a secondary route through a dedicated Rarity intake address with sender validation, rate limits, quarantine, and visible failure health.

Datto RMM uses a dedicated encrypted connection and remains read/ingest-only in V1. It runs an initial full inventory sync then an MSP-configurable incremental sync with a 15-minute default. Stable Datto IDs are primary match keys; human/device attributes are candidate evidence. Site-to-client mapping, reconciliation choices, provenance, errors, and stale/inactive handling remain visible and audited.

Implemented source contracts also cover hash-only scoped service keys, signed/replay-protected webhooks, Graph lifecycle and atomic delta cursors, forwarding quarantine, Datto alert recovery, encrypted Teams delivery with a 24-hour retry limit, and authoritative integration health. PostgreSQL migration and repository acceptance is complete on the constrained demo environment; configured non-production provider acceptance remains pending.

See [Integration, Automation, and AI Contracts](../04-api/integration-automation-ai-contracts.md) and [Integration, Automation, and AI Acceptance](../06-development/integration-automation-ai-acceptance.md).
