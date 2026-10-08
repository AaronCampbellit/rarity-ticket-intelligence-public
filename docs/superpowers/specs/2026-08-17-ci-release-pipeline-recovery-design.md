# CI and Release Pipeline Recovery Design

Publication note: historical commits, CI runs, pull requests, signing identities,
and image digests in this document belong to the original private development
repository. They are retained as provenance for their recorded revisions; this
public source snapshot begins with fresh history and does not publish those
old artifacts or attest a new release.

**Status:** Approved for implementation on 2026-08-17.

## Outcome

Restore trustworthy reviewed-`main` continuous integration and unblock exact-commit artifact publication for the notification-center release. The repair keeps every existing quality and security gate intact, makes the browser job capable of starting its database-backed acceptance server, fixes PostgreSQL fixture teardown, and resolves the one high-severity frontend audit finding with the smallest compatible lockfile change.

The failed CI run for reviewed commit `8df28cb3b0adacc3d68acb6c85c3b7d8139d060e` is the baseline: GitHub Actions run `32077527124` (private development history). Because publication is chained to successful CI, no publish run should be expected until a repaired `main` commit passes all CI jobs.

## Failure Evidence

Three independent failures must be repaired:

- The browser-journey job cannot start Playwright's Go acceptance server. `frontend/playwright.config.ts` now launches `TestClassificationBrowserAcceptanceServer`, which connects to PostgreSQL, but the job provisions neither Go nor PostgreSQL.
- The PostgreSQL job reaches `TestAISecondWavePostgres` teardown, then fails to delete the MSP because `mention_access_revisions` still references it.
- The source/security job rejects `nanoid@3.3.16` under `npm audit --audit-level=high`. The vulnerable package is transitive through `vite -> postcss`, and the existing dependency range permits a patched `nanoid` release.

These are pipeline and test-fixture defects. There is no evidence that the notification-center feature itself failed its assertions.

## Approaches Considered

### Repair each root cause in place — selected

Give the browser job the services and toolchain its configured server requires, correct fixture cleanup ordering, and update only the vulnerable transitive lockfile resolution. This preserves the intended gates and keeps the change narrowly attributable.

### Loosen or bypass failing gates

Increasing the Playwright timeout, skipping the PostgreSQL suite, lowering the audit threshold, or allowing publication after failed CI would make the pipeline green without fixing its guarantees. This approach is rejected.

### Broad CI and dependency modernization

Consolidating jobs, redesigning the browser harness, or broadly upgrading frontend packages could address the symptoms, but would expand the blast radius and make release recovery harder to verify. Those changes are outside this slice.

## Browser-Job Environment

Update the browser-journey job in `.github/workflows/ci.yml` to match the runtime contract established by `frontend/playwright.config.ts`:

- provision the same pinned PostgreSQL service image and health check used by the repository's PostgreSQL integration job;
- expose PostgreSQL on the runner's port `5432` and set the browser job's `TEST_DATABASE_URL` explicitly to `postgres://postgres:postgres@127.0.0.1:5432/rarity_test?sslmode=disable`;
- install Go with the repository's pinned setup action, derive the version from `go.mod`, and enable its normal module cache;
- retain the current Node installation, Playwright setup, web-server command, test command, and timeout behavior.

The local Playwright fallback on port `55432` remains unchanged. CI supplies its explicit URL, so local Compose-oriented development and hosted CI do not need to share a port convention.

Extend `infrastructure/release/ci_contract_test.go` to assert that the browser gate contains its PostgreSQL service, Go setup, and database URL. This turns the missing runtime prerequisites into a durable workflow contract rather than relying on another live-run discovery.

## PostgreSQL Fixture Cleanup

Update `backend/internal/store/psa/ai_second_wave_postgres_integration_test.go` so its teardown explicitly deletes `mention_access_revisions` for the test MSP before deleting the parent `msp_organizations` row.

The change belongs in test cleanup only. It must not add cascading foreign keys, weaken referential integrity, or alter production deletion behavior. A focused PostgreSQL test should reproduce the teardown failure before the fix and pass afterward.

## Frontend Dependency Repair

Refresh the existing package-lock resolution so `nanoid` resolves to at least `3.3.18`, within the semver range already accepted by `postcss`. Prefer the package manager's targeted lockfile update and preserve declared direct-dependency versions.

Do not use a broad `npm audit fix`, add a dependency override, or upgrade Vite/PostCSS unless the compatible lockfile update proves impossible. The resulting clean install must pass `npm audit --audit-level=high` with no high- or critical-severity findings.

## Verification and Release Flow

Implementation follows focused red/green verification before broad checks:

1. Add a failing workflow-contract assertion for the browser prerequisites, then update CI until it passes.
2. Reproduce the PostgreSQL teardown failure, add the explicit child-row cleanup, and rerun the focused integration test.
3. Apply the targeted lockfile refresh and verify both a clean install and the audit gate.
4. Run repository-wide Go tests, the PostgreSQL-backed suite, frontend tests, the production frontend build, and Playwright browser journeys against local PostgreSQL.
5. Commit and push the repair to `main`, then monitor CI for that exact commit. A retry of an unchanged failure is not evidence of repair.
6. After CI succeeds, monitor the chained publish workflow and verify that its source revision is the same repaired commit.
7. Verify the immutable API and frontend image digests, signatures, SBOMs, and provenance with `scripts/verify-release-artifacts.sh`, storing downloaded evidence outside the repository working tree.

Artifact verification fails closed: a missing digest, mismatched revision, invalid signature, missing SBOM, or invalid provenance blocks completion and promotion claims.

## Non-Goals

- No quality, security, or publication gate is weakened or removed.
- No production database schema or runtime deletion semantics change.
- No unrelated dependency, workflow, or action upgrade is included.
- No notification-center product behavior changes.
- No release is claimed from artifacts that cannot be tied to the exact successful `main` commit.

## Acceptance Criteria

- The source/security, PostgreSQL, and browser-journey CI jobs all pass for the same repaired `main` commit.
- The browser job's database and Go prerequisites are protected by a repository contract test.
- PostgreSQL integration teardown completes without foreign-key violations and without schema changes.
- A clean frontend install reports no high- or critical-severity audit findings.
- The chained publish workflow succeeds for the exact reviewed commit, not a branch-relative or stale revision.
- Both API and frontend immutable image digests have valid signatures, SBOMs, and provenance identifying that commit.
- The repository is clean after verification, with release evidence retained outside the working tree.
