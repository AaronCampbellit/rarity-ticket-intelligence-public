# Build-Readiness Decision Register

**Status:** Complete; all eleven foundation decisions resolved

The local [build-readiness worksheet](../../docs-site/build-readiness.html) is retained as the historical decision record. Durable constraints are captured in ADRs and detailed implementation specifications.

## Required decisions

1. Application stack: **resolved** — Go, React/TypeScript/Vite, pgx/sqlc, Goose, Go tests, and Playwright.
2. PostgreSQL HA manager, routing, fencing, and failure-domain guidance: **resolved** — Patroni, three-member etcd, HAProxy, strict sync, required watchdog; one VM for demo/test and three separate production nodes.
3. Temporary cache/queue technology and operating contract: **resolved** — Valkey for temporary acceleration, PostgreSQL as durable record, one container for demo/test and primary/replica/three Sentinels for production.
4. Snapshot/WAL cadence, separate backup destination, recovery-key procedure, and alerts: **resolved** — pgBackRest, continuous encrypted WAL archive with 60-second archive timeout, six-hour incrementals/nightly differentials/weekly full backups, separate object storage, protected key recovery, monthly restore verification, and quarterly DR exercise.
5. CI, artifact registry, scanning/provenance, review, and Compose acceptance-environment baseline: **resolved** — local Git and Docker Compose first; remote Git, CI, registry, SBOM, and artifact provenance deferred until the first runnable product milestone.
6. Microsoft Graph subscription, forwarding-domain, and mail-retention operations: **resolved** — dedicated shared support mailbox, mailbox-scoped Entra application, basic notification then secure retrieval, lifecycle handling, five-minute delta reconciliation, seven-year encrypted mail retention, and protected forwarding fallback.
7. Datto API/authentication, sync, reconciliation, and deduplication defaults: **resolved** — named encrypted read/ingest-only connection, initial full sync then configurable 15-minute incremental sync, audited reconciliation/provenance, stale-not-deleted assets, and 24-hour default alert deduplication.
8. Teams delivery/authentication, notification templates, and escalation defaults: **resolved** — named encrypted Incoming Webhook channels, compact safe Adaptive Cards, 24-hour retry, quiet-period deduplication, and in-app/email/Teams default escalation policies.
9. Dashboard visual system and default worklist behavior: **resolved** — the shared Rarity design system, capability-aware home, technician worklist, grouped product navigation, workspace history, and private notification center are the implemented V1 defaults.
10. Measurable latency, throughput, storage, queue-lag, sync-freshness, and recovery budgets: **resolved** — accepted 50-technician/1,000-client/5,000-ticket peak workload with measured p95/p99 service budgets, 1,000-event five-minute burst tolerance, search/Datto/Graph freshness budgets, and seven-year storage forecasting with headroom.
11. First supported Ubuntu/Docker deployment profile: **resolved** — Ubuntu Server 24.04 LTS x86-64, Docker Engine/Compose v2, documented development/demo and single-node pilot tiers, Caddy ingress, local MinIO for pilot payloads, and remote object storage for backups/WAL.

The first five establish the platform-delivery foundation. All eleven decisions are resolved. Further visual and workflow refinement is normal product iteration and is not a build-readiness gate.
