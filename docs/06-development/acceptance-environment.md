# Acceptance Environment and Fixtures

**Status:** Synthetic acceptance contract; local and CI harnesses implemented

See [local development](local-development.md) for the executable PostgreSQL/browser
harness. The inventory below is the required acceptance model, not a claim that
every recovery or provider scenario has run. Supported-profile and manual results
remain in the [release gate](release-readiness.md).

## Environment profile

The acceptance environment is a Docker Compose-based, non-production installation with synthetic tenants, a PostgreSQL test topology, temporary cache/queue, S3-compatible test storage, fake/controlled Entra and Microsoft Graph boundaries, webhook receiver, Datto API simulator, and fault injection controls.

## Standard fixtures

- One MSP; Alpha and Bravo clients with intentionally colliding human identifiers.
- `tests/fixtures/foundation.json` is the initial safe seed: `msp-demo`, `client-alpha`, and `client-bravo` share the collision value `SITE-100` while retaining distinct durable IDs. It contains no integration credentials.
- Technicians spanning queues, teams, departments, and roles.
- Incidents, requests, changes, problems, tasks, contracts, assets, services, graph edges, attachments, internal notes, public replies, time entries, and knowledge.
- Scoped service API keys (technician PATs are excluded by ADR-0020), automation identities, disabled users, break-glass user, stale sessions, deleted/archived objects, and retention-eligible data.
- Graph messages, forwarded email, webhooks, Datto device/alert snapshots, deduplication/recovery events, and integration failures.

## Required scenarios

The baseline suite exercises client isolation, RBAC, workflow selection, SLA calendar math, ownership, duplicate merge redirects, public/internal note separation, attachment authorization, Graph/threaded intake, signed webhooks, event idempotency, retry/dead-letter flow, Datto reconciliation, backup restore, and health telemetry.

## Data rule

Fixtures are synthetic and safe to inspect. No production customer data, customer credentials, or real integration secrets are permitted.
