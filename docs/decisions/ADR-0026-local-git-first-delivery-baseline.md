# ADR-0026: Local Git-first delivery baseline

**Status:** Accepted\
**Date:** 2026-07-26

## Decision

Rarity begins product development with a local Git repository and local Docker Compose acceptance environment. GitHub, GitHub Actions, GitHub Container Registry (GHCR), and any other remote source-control, CI, or artifact service are intentionally deferred until the first runnable product milestone.

## V1 development baseline

- Local Git records focused, reviewable commits at stable milestones.
- Docker Compose recreates the non-production acceptance environment using only synthetic data and non-production credentials.
- Required local checks include formatting, tests, migration validation, API/event contract validation, security/secret/dependency scanning, and documentation-link validation as tooling is introduced.
- No source code, images, customer data, credentials, or artifacts are published remotely under this decision.

## Deferred remote-delivery baseline

When the first runnable product milestone needs off-machine source backup, collaboration, or repeatable server deployment, the project will create a private remote Git repository. A subsequent ADR will select the remote provider and, if required, a minimal CI build/test workflow and container registry. That decision will not change Rarity's self-hosted Docker deployment model.

## Consequences

The team can begin with familiar Git and Docker workflows without adding CI or registry operations before they offer practical value. Until a remote is adopted, the availability of source history depends on the backup protection of the local project storage.
