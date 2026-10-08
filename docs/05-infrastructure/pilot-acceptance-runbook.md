# Pilot Acceptance Runbook

**Status:** Runbook implemented; candidate artifact preflight complete; live pilot execution pending

This runbook qualifies the controlled single-node pilot and produces inputs to the release evidence gate. It never upgrades a live installation, deletes data, or substitutes the pilot for the separate three-node HA acceptance topology.

## Current candidate artifact preflight

Revision `098e2a66a29424225911018ef5db0762f136af9a` has completed the
repository-native artifact preflight for both promoted immutable GHCR digests.
Exact signatures, SPDX SBOMs, production BuildKit provenance, and the generated
artifact manifest passed. See
[Release Readiness and Evidence Gate](../06-development/release-readiness.md#published-artifact-acceptance)
for the immutable references and workflow evidence. The candidate has not yet
been deployed through this runbook, so every pilot, recovery, accessibility,
HA, telemetry, and capacity result remains pending.

## 1. Bind the candidate

Record the full Git revision, release candidate, Ubuntu host identity, Docker Engine/Compose versions, immutable image digests, configuration checksum, and test window. Confirm the host is Ubuntu Server 24.04 LTS x86-64 with the agreed 8 vCPU, 32 GB RAM, and 1 TB SSD/NVMe profile. Secrets and customer data must not enter the evidence bundle.

## 2. Preflight and deploy

Validate signed/digest-pinned images and their attestations before starting the profile:

```bash
scripts/verify-release-artifacts.sh \
  Owner/repository \
  "$RARITY_BUILD_REVISION" \
  "$RARITY_API_IMAGE" \
  "$RARITY_FRONTEND_IMAGE" \
  /absolute/path/to/existing-empty/release-evidence
docker compose --env-file .env \
  -f infrastructure/compose/compose.yaml \
  -f infrastructure/compose/compose.pilot.yaml \
  -f infrastructure/compose/compose.release.yaml \
  config --quiet
```

Use the repository's canonical GitHub `Owner/repository` spelling for the first argument; the verifier preserves it for the exact signing certificate identity and derives lowercase GHCR coordinates separately. The existing evidence directory must be empty: the verifier publishes all seven files with one atomic directory rename. It also requires the BuildKit config-source SHA, embedded build argument, OCI revision label, and workflow SHA all to equal the requested revision. Both image variables must be full `ghcr.io/...@sha256:...` references. The release override removes local application builds, ensuring the candidate runs the reviewed artifact. Confirm only TCP 443 is public and verify PostgreSQL, Valkey, MinIO, and metrics remain private. Retain the rendered configuration with secrets redacted.

Verify HTTPS, certificate trust, `/healthz`, `/readyz`, the `/api/v1` route, frontend revision, and denial of public `/metrics`. Record every container’s digest and health state.

## 3. Data and isolation

Run forward migrations and the synthetic Alpha/Bravo client-isolation matrix against the pilot PostgreSQL instance. Exercise enumeration-safe API failures and confirm audit/outbox records stay within the trusted MSP/Client scope. Run the rollback validation in an isolated database; do not reverse the live pilot database merely to create evidence.

## 4. Recovery and continuity

Run the scheduled backup tool and retain its owner-only evidence record. Perform a clean-room point-in-time restore, SQL probe, application-readiness check, object/attachment consistency check, and immutable audit/outbox check. Record observed RPO/RTO and every exception.

The separate HA topology must also prove primary loss, synchronous failover, client reconnect, acknowledged-write preservation, and recovery of the failed node. The single-node pilot cannot satisfy `ha_failover`.

Run the upgrade evidence gate, rehearse the declared rollback procedure against an isolated copy, and record compatibility and rollback limits.

## 5. Capacity and operations

Exercise 50 concurrent technicians, 1,000 Clients, 5,000 tickets over the peak-day profile, and a 1,000-event five-minute burst. Retain raw samples and the capacity evaluator result for the exact revision.

Confirm metric scraping, structured-log ingestion, correlated span export, PostgreSQL quorum/replication alerts, pgBackRest freshness, queue/integration/automation health, and warning/critical routing to named runbooks. Trigger controlled alert examples and record receipt and acknowledgement.

## 6. Security and accessibility

Retain the sealed repository security scan, dependency results, secret scan, and client-isolation evidence. Re-run the automated frontend accessibility suite against the candidate.

Manual accessibility acceptance covers keyboard-only traversal and visible focus, skip navigation, screen-reader landmarks/names/status announcements, 200% and 400% zoom/reflow, high-contrast/forced-colors behavior, reduced motion, non-color status meaning, error recovery, and touch targets on supported viewport sizes. Record browser, assistive technology, operator, results, and exceptions.

Current pre-candidate evidence:

| Check | Automated evidence | Manual result |
| --- | --- | --- |
| Effective 200% and 400% reflow | Chromium 151 at 720 px and 360 px: no horizontal overflow, action reachable, focus visible, minimum target retained | True browser zoom pending |
| Forced colors and reduced motion | Chromium emulation: named status/error text, disabled state, and interaction remain available | Physical high-contrast review pending |
| Touch targets and actions | Chromium mobile/touch emulation: 44 px navigation target and dialog actions pass | Physical touch/tablet pending |
| VoiceOver | Exact landmarks, names, announcements, setup, login, provider, model, and request-result checklist retained | Pending connected macOS operator run |
| NVDA | Same checklist and browser/version/result fields retained | Pending connected Windows NVDA run |

This table is pre-candidate evidence only. Re-run it against the exact release
candidate and do not mark the release gate complete until the manual result
column has actual operator evidence.

## 7. Release decision

Review all required runbooks and corrective actions. Populate a release evidence manifest using `infrastructure/release/release-evidence.example.json`, keep every unfinished item `pending`, and run `rarity-release-gate` against the full candidate revision. A passing manifest permits a human release decision; it is not itself deployment authorization.
