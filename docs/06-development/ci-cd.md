# CI/CD

**Status:** GitHub CI, publication, signing, provenance, and exact-artifact verification accepted

GitHub Actions validates pull requests and pushes to protected `main`; GHCR stores API and frontend images. [ADR-0034](../decisions/ADR-0034-github-reviewed-artifact-delivery.md) defines the trust boundary, required repository ruleset, immutable-action policy, provenance, signing, and promotion contract.

`.github/workflows/ci.yml` is read-only and fork-safe. It validates formatting, modules, race-enabled backend tests, static analysis, frontend tests/build, browser journeys, API/event documentation contracts, disposable-PostgreSQL migrations and transaction contracts, dependencies, images, licenses, secrets, and configuration. It never receives package, OIDC, attestation, or content-write permission.

`.github/workflows/publish.yml` runs only after a successful `CI` push run on `main`. It checks out that run's exact revision and pushes a run-specific candidate image. Only after that exact digest is scanned, BuildKit SBOM/provenance-attested, keyless-signed through Sigstore, and verified does the workflow promote the same digest to its full-revision and `main` tags. It does not deploy.

The repository has completed this GitHub acceptance path:

1. Actions and GHCR run against reviewed `main`.
2. CI retains read-only permissions and fork-safe behavior.
3. Workflow actions use full-length SHA pins.
4. All three CI jobs gate the publication workflow.
5. Both images have passed digest verification, signature validation, SBOM, and provenance extraction with `scripts/verify-release-artifacts.sh`.

Deployment promotion must reuse those exact digests, require environment-specific policy, capture revision-bound evidence, support staged health checks, and never embed production secrets in build output.
