# Phase 0 Delivery Baseline

**Status:** Local and reviewed-remote baselines implemented; constrained demo and published-artifact acceptance complete; supported-profile acceptance pending

## Purpose

Phase 0 creates the repeatable, non-production delivery path required before product feature code begins. Local Git and Docker Compose remain the developer baseline; [ADR-0034](../decisions/ADR-0034-github-reviewed-artifact-delivery.md) adds reviewed remote source and artifact production.

## Repository conventions

- `backend/`, `frontend/`, `infrastructure/`, `integrations/`, `sdk/`, and `tests/` remain the product surfaces.
- `docs/` is the approved architecture source of truth; decisions change through ADRs.
- Generated artifacts are reproducible and never hand-edited.
- Secrets, customer data, production exports, and private keys never enter source control.
- Product changes are small, reviewed, tested, documented, and traceable to a backlog item.
- GitHub Actions provides read-only pull-request gates, and reviewed `main` produces digest-pinned, scanned, SBOM/provenance-attested, keyless-signed GHCR images.

## Delivery checks

The delivery baseline checks formatting, linting, unit/contract tests, authorization/isolation tests, API/event compatibility, dependency/license/security scanning, secret scanning, migration validation, documentation links, browser journeys, and both container builds. Remote images require BuildKit SBOM/maximum-mode provenance, digest scanning, and Cosign signing.

Before a demo deployment, `scripts/demo-preflight.sh` requires a clean,
identified Git revision, every non-empty runtime/Compose configuration key, an
exactly matching `RARITY_BUILD_REVISION`, and the configured RTI demo-server
record. This rejects stale deployment secret files before Compose can rebuild
or recreate services. After deployment,
`scripts/demo-verify.sh <base-url> <revision>` proves liveness, readiness, the
build endpoint, and the rendered revision. The initial Alpha/Bravo collision
fixture is the seed for later client-isolation checks.

## Environments

Local development uses synthetic data and non-production credentials only. The acceptance environment is isolated, reproducible, and exercises PostgreSQL, temporary queue/cache, object storage, email/webhook simulators, and failure conditions. Production-like HA/DR validation is separate from developer convenience environments.

## Exit criteria

Before product implementation begins, the team can create a clean environment, run required checks, prove client isolation against fixtures, produce a traceable artifact, and destroy/recreate non-production state safely.
