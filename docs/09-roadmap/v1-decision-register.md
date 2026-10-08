# V1 Technician Platform Decision Register

**Status:** V1 baseline approved

The interactive [V1 decision worksheet](../../docs-site/v1-decisions.html) is the fastest way to answer these questions locally and export the result. This Markdown record anchors the work in the project source of truth.

## Decision groups

- Infrastructure: PostgreSQL HA implementation, fencing, WAL/snapshot cadence, temporary queue/cache, object storage, scale budgets, single-node profile, and upgrade rules.
- Security: key management, break-glass controls, sessions, destructive confirmations, and diagnostic-data access.
- Intake and API: Graph permissions/subscriptions, forwarding anti-spoofing, threading, service keys, PATs, webhooks, and public API limits.
- Datto RMM: API/authentication, sync intervals, reconciliation, discovery provenance, deduplication, stale assets, and error handling.
- Technician experience: SLAs, templates, Teams, worklists, notifications, closure, duplicates, attachment previews, dashboard sharing, and calendars.
- Delivery: CI/artifact tooling, scanning, acceptance-environment lifecycle, and release policy.

## Approved baseline

The recommended defaults are approved for 40 of 42 worksheet items. This includes the recommended defaults shown in the local worksheet for HA implementation selection, recovery cadence, cache/queue boundary, scale budgets, key/session/break-glass controls, Graph/forwarding/API/webhook operation, Datto sync/reconciliation/deduplication, technician notification/worklist/closure/attachment/calendar behavior, and delivery governance.

## INF-05 — Object storage support matrix — resolved

MinIO is the first supported self-hosted S3-compatible object-storage option. The initial single-server profile uses Docker Compose on Ubuntu with MinIO on dedicated persistent storage. HA production uses storage outside the single Rarity application host or an independently durable/HA MinIO deployment. PostgreSQL snapshots and WAL archives still reach a separate failure domain. See [ADR-0021](../decisions/ADR-0021-minio-first-self-hosted-object-storage.md).

### API-02 — Technician personal access tokens — resolved

PATs are not required for normal technician work. The V1 decision is **do not issue technician PATs** and use scoped service API keys only for named integrations. PATs can be introduced later if a real technician-script use case appears. See [ADR-0020](../decisions/ADR-0020-no-technician-pats-in-v1.md).

Each answer should become an ADR when it makes a durable platform constraint, then update its owning specification and Phase 0 backlog item.
