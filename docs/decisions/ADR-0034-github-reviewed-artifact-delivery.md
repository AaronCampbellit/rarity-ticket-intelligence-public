# ADR-0034: GitHub reviewed-artifact delivery

**Status:** Accepted\
**Date:** 2026-07-30\
**Supersedes:** [ADR-0026](ADR-0026-local-git-first-delivery-baseline.md) for remote delivery

## Decision

Rarity uses a private GitHub repository, GitHub Actions, and GitHub Container Registry for the first remote delivery baseline. Pull requests run with read-only repository permission. Only a successful `CI` run for a push to protected `main` may start image publication.

The published API and frontend images:

- are built from the exact successful `main` revision;
- use GHCR names scoped to the repository and component;
- carry immutable full-revision tags plus a movable `main` convenience tag;
- include BuildKit SBOM and maximum-mode provenance attestations;
- are scanned by digest before release qualification;
- are keyless-signed with Cosign using GitHub OIDC.

Publication first pushes a run-specific candidate tag. The exact digest is scanned and keyless-signed; its BuildKit SBOM/provenance and signature are verified before the workflow applies the full-revision and `main` tags to that same digest. A failed candidate can remain quarantined in registry retention, but it is never promoted. Deployment and promotion consume only digest-pinned images. A tag is never release evidence.

## Repository policy

Protect `main` with a repository ruleset that:

- requires pull requests and at least one approval;
- dismisses stale approvals when the reviewed diff changes;
- requires the `Source, unit, and security gates`, `PostgreSQL migration and transaction gates`, and `Browser journeys` checks;
- requires the branch to be current or uses a merge queue;
- blocks force pushes and deletion;
- restricts direct pushes to an explicit break-glass role; and
- requires signed commits if the organization can operate that policy without blocking automated merge commits.

GitHub Actions defaults to read-only `GITHUB_TOKEN` permissions. Fork pull requests never receive write tokens or repository secrets and do not use `pull_request_target`. Repository or organization policy requires every external action, including GitHub-authored actions, to use a full commit SHA.

## CI and publication boundary

Pull-request CI verifies formatting, module integrity, race-enabled Go tests, static analysis, frontend unit/accessibility tests, browser journeys, documentation contracts, disposable-PostgreSQL migration/transaction behavior, dependency audit, secret/configuration/license scanning, and both container builds.

Publication receives only `contents: read`, `packages: write`, and `id-token: write`. It runs after successful trusted-branch CI and does not deploy. Pilot or production promotion remains a separate environment-authorized operation using the existing revision-bound release evidence gate.

## Consequences

Remote source availability and repeatable reviewed artifacts are now available without changing Rarity's self-hosted Docker Compose operating model. GitHub and GHCR are control-plane dependencies for source collaboration and artifact production, not runtime dependencies.

Keyless signing depends on GitHub OIDC, Sigstore Fulcio/Rekor availability, and public transparency-log disclosure of the signing identity. Offline or air-gapped environments retain and verify the signature plus BuildKit SBOM/provenance before importing the same digest.
