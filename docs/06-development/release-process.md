# Release Process

**Status:** Evidence gate and runbooks implemented; live acceptance pending

Releases declare version, compatibility, migrations, security changes, operational impact, known issues, upgrade path, rollback limits, and documentation updates.

Database and event changes are expand/migrate/contract where possible. Release readiness includes restore confidence, capacity evidence, API compatibility, and operator runbooks.

Reviewed `main` images are produced under [ADR-0034](../decisions/ADR-0034-github-reviewed-artifact-delivery.md). Release candidates identify API and frontend images by OCI digest, verify their keyless signatures and BuildKit attestations, and retain the associated SBOM/provenance evidence. The movable `main` tag is never promoted.

The executable evidence contract and promotion/rollback rules are defined in [Release Readiness and Evidence Gate](release-readiness.md). Pilot qualification follows the [Pilot Acceptance Runbook](../05-infrastructure/pilot-acceptance-runbook.md).
