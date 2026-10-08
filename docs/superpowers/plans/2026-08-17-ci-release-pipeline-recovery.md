# CI and Release Pipeline Recovery Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore all reviewed-`main` CI gates and publish cryptographically verifiable API and frontend images for the exact repaired commit.

**Architecture:** Repair each evidenced failure at its owning boundary: encode the browser job's Go/PostgreSQL runtime contract in both a Go test and the workflow, correct PostgreSQL fixtures and stale assertions, qualify the Datto update, refresh only the compatible vulnerable transitive lockfile entry, and register the approved documents in the repository's required docs manifest. Run a portable pass without a database, then run each database-backed test-file group serially against a fresh sibling database so migration concurrency and residue across unrelated integration fixtures are impossible. After local parity checks, push the reviewed commits, require all CI jobs to pass for that SHA, then verify the chained publish run and immutable artifact evidence for the same SHA.

**Tech Stack:** GitHub Actions, Go 1.25 from `go.mod`, PostgreSQL 17, Node.js 22, npm, Vite 8, Playwright, Docker Buildx, Cosign, GHCR.

## Global Constraints

- Do not weaken or remove any quality, security, or publication gate.
- Keep `frontend/playwright.config.ts`'s local PostgreSQL fallback on port `55432`; CI must override it with port `5432`.
- Use PostgreSQL image `postgres:17-alpine@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193` in CI.
- Keep `TEST_DATABASE_URL` equal to `postgres://postgres:postgres@127.0.0.1:5432/rarity_test?sslmode=disable` in the browser job.
- Do not change production schema, referential integrity, or deletion semantics.
- Resolve `nanoid` to at least `3.3.18` without changing declared direct-dependency versions or adding an override.
- Do not include unrelated workflow, action, dependency, or notification-center changes.
- Run portable packages without `TEST_DATABASE_URL`, then PostgreSQL-backed test-file groups serially against a fresh database per group; every package and test remains enabled.
- Treat missing or mismatched signatures, SBOMs, provenance, digests, or revisions as release-blocking failures.

---

## File Structure

- `.github/workflows/ci.yml`: supplies the browser job's pinned PostgreSQL service, explicit database URL, and Go toolchain.
- `infrastructure/release/ci_contract_test.go`: permanently asserts the browser job's database-backed Go-server prerequisites.
- `backend/internal/store/psa/ai_second_wave_postgres_integration_test.go`: deletes mention-access revision fixtures before their MSP parent.
- `frontend/package-lock.json`: records the compatible patched `nanoid` tarball and integrity.
- `docs-site/app.js`: registers the notification and CI-recovery design/plan documents required by the documentation gate.
- `scripts/run-postgres-acceptance.sh`: invokes the package-isolated PostgreSQL acceptance command.
- `infrastructure/cmd/postgres-acceptance/main.go`: runs a portable package pass and every database-backed test-file group serially against a fresh sibling database.
- `infrastructure/cmd/postgres-acceptance/main_test.go`: protects URL safety, package/test discovery, exact grouping, database lifecycle, cleanup, and fail-fast behavior.
- `infrastructure/release/postgres_acceptance_test.go`: proves the shell entry point routes the full backend/integration package set through the package-isolated command.

---

### Task 1: Make the browser CI runtime contract executable

**Files:**
- Modify: `infrastructure/release/ci_contract_test.go:20-60`
- Modify: `.github/workflows/ci.yml:139-164`

**Interfaces:**
- Consumes: `frontend/playwright.config.ts`'s `TestClassificationBrowserAcceptanceServer` command and `TEST_DATABASE_URL` override.
- Produces: a browser job with healthy PostgreSQL and Go available before Playwright starts; `TestBrowserCIRunsDatabaseBackedGoAcceptanceServer` protects that contract.

- [ ] **Step 1: Add the failing browser-job contract test**

Append this test after `TestPullRequestCIIsReadOnlyAndCoversPortableAndDatabaseGates`:

```go
func TestBrowserCIRunsDatabaseBackedGoAcceptanceServer(t *testing.T) {
	body := readWorkflow(t, "ci.yml")
	browserStart := strings.Index(body, "\n  browser:\n")
	if browserStart < 0 {
		t.Fatal("CI workflow is missing the browser job")
	}
	browser := body[browserStart:]
	requireFragments(t, browser,
		"services:",
		"image: postgres:17-alpine@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193",
		"POSTGRES_DB: rarity_test",
		"- 5432:5432",
		"TEST_DATABASE_URL: postgres://postgres:postgres@127.0.0.1:5432/rarity_test?sslmode=disable",
		"name: Set up Go",
		"uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16",
		"go-version-file: go.mod",
	)
}
```

- [ ] **Step 2: Run the contract test and witness RED**

Run: `go test ./infrastructure/release -run '^TestBrowserCIRunsDatabaseBackedGoAcceptanceServer$' -count=1 -v`

Expected: FAIL with missing fragments for `services:`, `TEST_DATABASE_URL`, and Go setup.

- [ ] **Step 3: Add the browser service, environment, and Go setup**

Insert this job configuration between `timeout-minutes` and `steps`:

```yaml
    services:
      postgres:
        image: postgres:17-alpine@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193
        env:
          POSTGRES_DB: rarity_test
          POSTGRES_USER: postgres
          POSTGRES_PASSWORD: postgres
        ports:
          - 5432:5432
        options: >-
          --health-cmd "pg_isready -U postgres -d rarity_test"
          --health-interval 5s
          --health-timeout 5s
          --health-retries 10
    env:
      TEST_DATABASE_URL: postgres://postgres:postgres@127.0.0.1:5432/rarity_test?sslmode=disable
```

Insert this step after checkout and before Node setup:

```yaml
      - name: Set up Go
        uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6
        with:
          go-version-file: go.mod
          cache: true
```

- [ ] **Step 4: Run workflow contracts and witness GREEN**

Run: `go test ./infrastructure/release -count=1 -v`

Expected: PASS, including the new browser-specific contract and full-SHA action pinning.

- [ ] **Step 5: Commit the browser CI repair**

```bash
git add infrastructure/release/ci_contract_test.go .github/workflows/ci.yml
git commit -m "fix: provision browser CI acceptance runtime"
```

---

### Task 2: Correct PostgreSQL fixture teardown ordering

**Files:**
- Modify: `backend/internal/store/psa/ai_second_wave_postgres_integration_test.go:415-445`

**Interfaces:**
- Consumes: `mention_access_revisions.msp_id`'s existing foreign key to `msp_organizations.id`.
- Produces: `cleanupAISecondWaveFixture` removes all test-owned mention access revisions before deleting its MSP.

- [ ] **Step 1: Start an isolated PostgreSQL 17 test service**

Run:

```bash
docker run --detach --rm \
  --name rarity-ci-recovery-postgres \
  --env POSTGRES_DB=rarity_test \
  --env POSTGRES_USER=postgres \
  --env POSTGRES_PASSWORD=postgres \
  --publish 127.0.0.1:55432:5432 \
  postgres:17-alpine@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193
```

Wait until `docker exec rarity-ci-recovery-postgres pg_isready -U postgres -d rarity_test` succeeds.

- [ ] **Step 2: Reproduce the existing teardown failure**

Run:

```bash
TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:55432/rarity_test?sslmode=disable' \
  go test ./backend/internal/store/psa -run '^TestAISecondWavePostgres$' -count=1 -v
```

Expected: FAIL during cleanup because `mention_access_revisions_msp_id_fkey` prevents deletion from `msp_organizations`.

- [ ] **Step 3: Add the minimal child-row cleanup**

Add this statement immediately before the `client_organizations` delete in `cleanupAISecondWaveFixture`'s `deletes` slice:

```go
		"DELETE FROM mention_access_revisions WHERE msp_id = $1",
```

- [ ] **Step 4: Rerun the focused PostgreSQL test and witness GREEN**

Run the command from Step 2 again.

Expected: PASS, including the final zero-residue assertion.

- [ ] **Step 5: Commit the fixture repair**

```bash
git add backend/internal/store/psa/ai_second_wave_postgres_integration_test.go
git commit -m "test: clean mention revisions before MSP fixtures"
```

Keep the isolated PostgreSQL service running for Task 5.

---

### Task 3: Refresh the vulnerable transitive dependency

**Files:**
- Modify: `frontend/package-lock.json:1992-2010`

**Interfaces:**
- Consumes: `postcss@8.5.24`'s existing `nanoid` range `^3.3.16`.
- Produces: a reproducible clean install whose `nanoid` resolution is at least `3.3.18` and passes the high-severity audit gate.

- [ ] **Step 1: Reproduce the audit failure**

Run: `npm audit --audit-level=high` from `frontend/`.

Expected: nonzero exit identifying `nanoid <3.3.18` and advisory `GHSA-2v37-7h3g-55p8`.

- [ ] **Step 2: Perform only the compatible lockfile refresh**

Run: `npm update nanoid --package-lock-only` from `frontend/`.

Then run: `git diff --exit-code -- frontend/package.json`

Expected: PASS; `package.json` remains byte-for-byte unchanged.

- [ ] **Step 3: Verify the resolved package and clean install**

Run from `frontend/`:

```bash
npm ci
npm ls nanoid
```

Expected: PASS; the resolved `nanoid` version is `3.3.18` or newer within major version 3.

- [ ] **Step 4: Witness the audit gate GREEN**

Run: `npm audit --audit-level=high` from `frontend/`.

Expected: exit 0 with no high- or critical-severity findings.

- [ ] **Step 5: Commit the dependency repair**

```bash
git add frontend/package-lock.json
git commit -m "fix: update patched nanoid lock resolution"
```

---

### Task 4: Restore documentation manifest completeness

**Files:**
- Modify: `docs-site/app.js:163-166`

**Interfaces:**
- Consumes: the repository-wide Markdown inventory enforced by `scripts/validate-docs.mjs`.
- Produces: a complete `documentPaths` manifest containing the notification-center and CI-recovery design/plan pairs.

- [ ] **Step 1: Witness the documentation gate RED**

Run: `node scripts/validate-docs.mjs`

Expected: FAIL listing exactly these four missing documents:

```text
docs/superpowers/plans/2026-08-16-frontend-notification-center.md
docs/superpowers/plans/2026-08-17-ci-release-pipeline-recovery.md
docs/superpowers/specs/2026-08-16-frontend-notification-center-design.md
docs/superpowers/specs/2026-08-17-ci-release-pipeline-recovery-design.md
```

- [ ] **Step 2: Register the four existing documents**

Add these exact paths to `documentPaths` in `docs-site/app.js`, after the 2026-08-15 notification routing entries and before the older AI second-wave entries:

```text
docs/superpowers/specs/2026-08-16-frontend-notification-center-design.md
docs/superpowers/plans/2026-08-16-frontend-notification-center.md
docs/superpowers/specs/2026-08-17-ci-release-pipeline-recovery-design.md
docs/superpowers/plans/2026-08-17-ci-release-pipeline-recovery.md
```

- [ ] **Step 3: Rerun documentation contracts and witness GREEN**

Run:

```bash
node --check docs-site/app.js
node scripts/validate-markdown-links.mjs
node scripts/validate-docs.mjs
node scripts/validate-integration-automation-docs.mjs
```

Expected: every command exits 0; the docs manifest has neither missing nor stale entries.

- [ ] **Step 4: Commit the manifest repair**

```bash
git add docs-site/app.js
git commit -m "docs: register notification and recovery plans"
```

---

### Task 5: Serialize shared-schema PostgreSQL acceptance

**Files:**
- Create: `scripts/run-postgres-acceptance.sh`
- Create: `infrastructure/release/postgres_acceptance_test.go`
- Modify: `infrastructure/release/ci_contract_test.go:20-60`
- Modify: `.github/workflows/ci.yml:136-137`

**Interfaces:**
- Consumes: one `TEST_DATABASE_URL` pointing to the CI job's isolated `rarity_test` schema.
- Produces: `scripts/run-postgres-acceptance.sh`, which runs all `./backend/...` and `./tests/...` packages with Go package parallelism fixed at one; the CI PostgreSQL job calls that runner.

- [ ] **Step 1: Write the failing runner behavior test**

Create `infrastructure/release/postgres_acceptance_test.go`. Run the real shell artifact with a temporary fake `go` executable at the external process boundary. The fake records its working directory and each argument; assert the runner changes to the repository root and invokes exactly:

```go
package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostgresAcceptanceSerializesPackagesAgainstSharedSchema(t *testing.T) {
	temporaryDirectory := t.TempDir()
	capturePath := filepath.Join(temporaryDirectory, "invocation")
	fakeGoPath := filepath.Join(temporaryDirectory, "go")
	fakeGo := `#!/usr/bin/env bash
set -euo pipefail
{
  pwd
  printf '%s\n' "$@"
} >"$POSTGRES_ACCEPTANCE_CAPTURE"
`
	if err := os.WriteFile(fakeGoPath, []byte(fakeGo), 0o700); err != nil {
		t.Fatalf("write fake go: %v", err)
	}

	command := exec.Command("bash", "../../scripts/run-postgres-acceptance.sh")
	for _, variable := range os.Environ() {
		if strings.HasPrefix(variable, "PATH=") ||
			strings.HasPrefix(variable, "POSTGRES_ACCEPTANCE_CAPTURE=") {
			continue
		}
		command.Env = append(command.Env, variable)
	}
	command.Env = append(command.Env,
		"PATH="+temporaryDirectory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"POSTGRES_ACCEPTANCE_CAPTURE="+capturePath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run PostgreSQL acceptance wrapper: %v\n%s", err, output)
	}
	capture, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured go invocation: %v", err)
	}
	repositoryRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	want := strings.Join([]string{
		repositoryRoot,
		"test",
		"-p",
		"1",
		"./backend/...",
		"./tests/...",
		"-count=1",
		"",
	}, "\n")
	if string(capture) != want {
		t.Fatalf("captured invocation:\n%s\nwant:\n%s", capture, want)
	}
}
```

Name the test `TestPostgresAcceptanceSerializesPackagesAgainstSharedSchema`. The production break it catches is removing or changing `-p 1`, which would restore deterministic shared-schema migration races.

- [ ] **Step 2: Run the behavior test and witness RED**

Run: `go test ./infrastructure/release -run '^TestPostgresAcceptanceSerializesPackagesAgainstSharedSchema$' -count=1 -v`

Expected: FAIL because `scripts/run-postgres-acceptance.sh` does not exist.

- [ ] **Step 3: Implement the minimal runner and witness GREEN**

Create executable `scripts/run-postgres-acceptance.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"
exec go test -p 1 ./backend/... ./tests/... -count=1
```

Run the focused command from Step 2 again.

Expected: PASS with pristine output.

- [ ] **Step 4: Write the failing workflow contract and route CI through the runner**

In `TestPullRequestCIIsReadOnlyAndCoversPortableAndDatabaseGates`, replace the old parallel command fragment with `scripts/run-postgres-acceptance.sh`, then run:

```bash
go test ./infrastructure/release -run '^TestPullRequestCIIsReadOnlyAndCoversPortableAndDatabaseGates$' -count=1 -v
```

Expected RED: FAIL because `.github/workflows/ci.yml` still contains the raw parallel `go test` command.

Change only the PostgreSQL job step to:

```yaml
      - name: Run all backend and integration contracts against PostgreSQL
        run: scripts/run-postgres-acceptance.sh
```

Rerun `go test ./infrastructure/release -count=1 -v`.

Expected GREEN: PASS, including action pinning and the browser job contract.

- [ ] **Step 5: Prove serialized execution against fresh PostgreSQL**

Start a fresh disposable PostgreSQL 17 service using the pinned image and a free loopback host port. After `pg_isready`, run:

```bash
TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:55433/rarity_test?sslmode=disable' \
  scripts/run-postgres-acceptance.sh
```

Expected: PASS for all backend and integration packages, with no concurrent migration, schema, or fixture-residue failures. Stop and remove the disposable database afterward.

- [ ] **Step 6: Commit the deterministic PostgreSQL gate**

```bash
git add scripts/run-postgres-acceptance.sh infrastructure/release/postgres_acceptance_test.go \
  infrastructure/release/ci_contract_test.go .github/workflows/ci.yml
git commit -m "fix: serialize PostgreSQL acceptance packages"
```

---

### Task 6: Qualify the Datto asset version update

**Files:**
- Modify: `backend/internal/store/psa/datto_repository_test.go`
- Modify: `backend/internal/store/psa/datto_repository.go`

**Interfaces:**
- Consumes: an `UPDATE assets ... FROM datto_asset_snapshots ... JOIN datto_connections` statement where both target and joined tables expose `version`.
- Produces: an update whose increment unambiguously reads the locked target asset version.

- [ ] **Step 1: Tighten the SQL contract and witness RED**

Change the existing repository assertion from `version = version + 1` to `version = asset.version + 1`, then run:

```bash
go test ./backend/internal/store/psa -run '^TestDattoRepository' -count=1
```

Expected: FAIL because the production statement is still ambiguous.

- [ ] **Step 2: Qualify the production update and witness GREEN**

Change only the right-hand side to `version = asset.version + 1`. Rerun the focused unit tests, then run `TestDattoNewAssetFallbackAgainstPostgres` against fresh PostgreSQL 17.

Expected: both pass and PostgreSQL no longer reports SQLSTATE 42702.

- [ ] **Step 3: Commit the Datto repair**

```bash
git add backend/internal/store/psa/datto_repository.go backend/internal/store/psa/datto_repository_test.go
git commit -m "fix: qualify Datto asset version update"
```

---

### Task 7: Make the mention intake fixture represent a real access loss

**Files:**
- Modify: `backend/internal/store/psa/mention_invalidation_intake_integration_test.go`

**Interfaces:**
- Consumes: the marker capture trigger's invariant that only authorization mutations with a demonstrable loss create `mention_access_loss_markers`.
- Produces: a historical-intake paging test whose relevant event deterministically creates a marker while the queue trigger is disabled.

- [ ] **Step 1: Preserve the PostgreSQL 17 failure evidence**

Run `TestMentionInvalidationHistoricalIntakePagesNonMatchingEventsAgainstPostgres` on a fresh PostgreSQL 17 database.

Expected: FAIL with zero claims because the synthetic `role.capabilities_replaced` event has no capability history and correctly creates no loss marker.

- [ ] **Step 2: Use a self-contained definitive-loss event fixture**

Replace only the synthetic relevant event type/subject with `technician.disabled`, which the production capture trigger classifies as definitive recipient loss without unrelated role-history setup. Keep the three nonmatching events, disabled queue trigger, two page claims, cursor assertions, and exact one-claim assertion unchanged.

- [ ] **Step 3: Witness GREEN and commit**

Run the focused test twice against fresh PostgreSQL 17. Expected: both pass, proving the first raw page advances past nonmatching events and the second enqueues the real marker exactly once.

```bash
git add backend/internal/store/psa/mention_invalidation_intake_integration_test.go
git commit -m "test: model definitive mention access loss"
```

---

### Task 8: Assert both scheduling facts emitted by atomic apply

**Files:**
- Modify: `backend/internal/store/psa/calendar_repository_integration_test.go`

**Interfaces:**
- Consumes: one domain `task.scheduled` event plus one canonical `calendar.schedule_changed` event intentionally emitted for the affected technician under the same correlation.
- Produces: an atomic-apply assertion that distinguishes the two required facts and still catches duplicates or missing events.

- [ ] **Step 1: Witness the stale assertion RED**

Run `TestCalendarSchedulingProposalAtomicRollbackAgainstPostgres` on fresh PostgreSQL 17.

Expected: FAIL with `events=2` while the legacy assertion expects one.

- [ ] **Step 2: Assert the exact event pair**

Extend the correlation query to count total events, `task.scheduled`, and `calendar.schedule_changed`. Require total two and exactly one of each, preserving all rollback, task version, audit, reason, and policy assertions.

- [ ] **Step 3: Witness GREEN and commit**

Run the focused PostgreSQL 17 test twice. Expected: both pass.

```bash
git add backend/internal/store/psa/calendar_repository_integration_test.go
git commit -m "test: expect canonical calendar scheduling event"
```

---

### Task 9: Isolate PostgreSQL acceptance packages

**Files:**
- Create: `infrastructure/cmd/postgres-acceptance/main.go`
- Create: `infrastructure/cmd/postgres-acceptance/main_test.go`
- Modify: `scripts/run-postgres-acceptance.sh`
- Modify: `infrastructure/release/postgres_acceptance_test.go`

**Interfaces:**
- Consumes: a loopback `TEST_DATABASE_URL` for the disposable `rarity_test` database and package patterns `./backend/... ./tests/...`.
- Produces: deterministic package enumeration followed by one fresh `rarity_test_acceptance_NNN` sibling database per sequential `go test <package> -count=1` invocation, with forced cleanup on success and failure.

- [ ] **Step 1: Add failing command behavior tests**

Cover URL validation (required PostgreSQL URL, loopback host, `rarity_test` database), stable generated database names, maintenance/test URL derivation, exact `go list` patterns, sequential package execution, cleanup after failure, and propagation of the first nonzero test exit. Use injected command/database boundaries; do not require a live database in unit tests.

Run:

```bash
go test ./infrastructure/cmd/postgres-acceptance -count=1
```

Expected: RED before the command implementation exists.

- [ ] **Step 2: Implement the package-isolated command**

Use `pgx` to connect to the `postgres` maintenance database. Enumerate packages with `go list ./backend/... ./tests/...`; for each package, drop any stale generated name, create a fresh database, run `go test <package> -count=1` with only `TEST_DATABASE_URL` replaced, then drop the database with `WITH (FORCE)`. Reject non-loopback or non-`rarity_test` inputs before any database mutation. Stream child output and stop at the first failure while still cleaning its database.

- [ ] **Step 3: Route the shell entry point through the command**

Change the executable shell wrapper to:

```bash
exec go run ./infrastructure/cmd/postgres-acceptance ./backend/... ./tests/...
```

Update the release contract to fake `go` and assert the repository working directory plus exact `run` arguments. Run:

```bash
go test ./infrastructure/cmd/postgres-acceptance ./infrastructure/release -count=1
```

Expected: GREEN.

- [ ] **Step 4: Prove full isolation against pinned PostgreSQL 17**

Run `scripts/run-postgres-acceptance.sh` twice against a fresh pinned PostgreSQL 17 service. Expected: every package passes twice; no migration or fixture residue crosses package boundaries; generated databases are absent afterward.

- [ ] **Step 5: Commit the isolated gate**

```bash
git add infrastructure/cmd/postgres-acceptance scripts/run-postgres-acceptance.sh infrastructure/release/postgres_acceptance_test.go
git commit -m "fix: isolate PostgreSQL acceptance packages"
```

---

### Task 10: Make local-admin recovery self-migrating

**Files:**
- Modify: `backend/internal/localadminrecovery/postgres_integration_test.go`

**Interfaces:**
- Consumes: a fresh empty PostgreSQL database supplied to the package through `TEST_DATABASE_URL`.
- Produces: an atomic password-reset integration test that owns its schema prerequisite instead of relying on another package's prior migration.

- [ ] **Step 1: Witness RED on an empty PostgreSQL 17 database**

Run `TestPostgresResetPasswordIsAtomicAndAudited` against a fresh database from the pinned PostgreSQL 17 image.

Expected: FAIL at the first `msp_organizations` fixture insert with SQLSTATE 42P01.

- [ ] **Step 2: Migrate before opening the fixture pool**

Import `backend/internal/store` and call `store.Migrate(ctx, databaseURL)` immediately after creating the context and before `pgxpool.New`. Fail the test with a migration-specific message. Do not change recovery behavior or assertions.

- [ ] **Step 3: Witness GREEN and commit**

Run the focused test twice against separate fresh PostgreSQL 17 databases. Expected: both pass.

```bash
git add backend/internal/localadminrecovery/postgres_integration_test.go
git commit -m "test: migrate local admin recovery database"
```

---

### Task 11: Diagnose package-internal PostgreSQL residue

**Files:**
- Verify only: the package-isolated acceptance runner and external evidence.

**Interfaces:**
- Consumes: Task 9's fresh database per package.
- Produces: a deterministic predecessor/failure pair proving whether unrelated tests within one package still leak fixtures.

- [x] **Step 1: Run the package-isolated wrapper on pinned PostgreSQL 17**

The wrapper failed in `backend/internal/store/psa` when the notification planner observed five prior eligible mention events.

- [x] **Step 2: Minimize the predecessor sequence**

The notification planner passes alone and after the invalidation intake test, but fails immediately after `TestMentionPersistenceWidgetIdempotencyAndRevocationAgainstPostgres`. Migration tests likewise retain calendar-delivery fixtures within their package. Evidence is recorded in `/tmp/rarity-task11-attempt2.9L7pUa`.

- [x] **Step 3: Select the isolation boundary**

Use one portable package pass with `TEST_DATABASE_URL` removed, then one fresh database for each source file containing database-backed tests. Execute the exact top-level tests from that file as one group. This isolates unrelated fixtures without paying the cost of a fresh migration for every unit test.

---

### Task 12: Isolate database-backed test-file groups

**Files:**
- Modify: `infrastructure/cmd/postgres-acceptance/main.go`
- Modify: `infrastructure/cmd/postgres-acceptance/main_test.go`

**Interfaces:**
- Consumes: `go list -json` metadata for `./backend/... ./tests/...`, source files that explicitly consume `TEST_DATABASE_URL`, and the safe database lifecycle from Task 9.
- Produces: one database-disabled portable pass per package plus deterministic, exact `Test`/`Example`/`Fuzz` selections per database-backed source file, each against a fresh database.

- [ ] **Step 1: Add failing discovery and orchestration tests**

Using temporary Go test files and injected command/database boundaries, require deterministic package/file ordering; removal of every inherited `TEST_DATABASE_URL` during the portable pass; recognition of files containing the exact environment-variable name; extraction and regexp quoting of top-level `Test`, `Example`, and `Fuzz` names; one exact `-run` group per database-backed file; and fresh-database cleanup/fail-fast behavior for every group.

Expected: RED because the current command runs the whole package once with a database.

- [ ] **Step 2: Implement metadata and test-file discovery**

Decode concatenated `go list -json` package objects containing directory, import path, `TestGoFiles`, and `XTestGoFiles`. Sort packages and files. Run each package once with all `TEST_DATABASE_URL` entries removed so portable tests and compilation remain covered. Read each listed test file; files containing `TEST_DATABASE_URL` are database-backed. Parse those files with Go's parser, collect top-level `Test`, `Example`, and `Fuzz` functions, sort and regexp-quote their names, and fail closed if a marked file has no runnable top-level function.

- [ ] **Step 3: Execute each database-backed file group in isolation**

For every discovered group, reuse Task 9's validated maintenance connection, pre-armed cleanup, bounded detached cleanup, process-group cancellation, and exit-code propagation. Run exactly:

```text
go test <import-path> -run ^(<quoted names>)$ -count=1
```

with a fresh generated database URL. Label failures with package and source filename. Do not run database-backed tests together across files.

- [ ] **Step 4: Prove full coverage and timing on PostgreSQL 17**

Run command/release tests, race tests, vet, shell syntax, formatting, and diff checks. Then run the full wrapper twice against the pinned PostgreSQL 17 image. Expected: every portable package and all 45 currently discovered database-backed file groups pass twice; generated database count is zero afterward; each run finishes below the CI job's 20-minute timeout.

- [ ] **Step 5: Commit the file-group isolation**

```bash
git add infrastructure/cmd/postgres-acceptance/main.go infrastructure/cmd/postgres-acceptance/main_test.go
git commit -m "fix: isolate PostgreSQL test file groups"
```

---

### Task 13: Give initial-tag knowledge fixtures distinct identities

**Files:**
- Modify: `backend/internal/store/psa/initial_tag_assignments_integration_test.go`

**Interfaces:**
- Consumes: the knowledge repository's enforced normalized-title identity within one MSP/client.
- Produces: direct-tag and fallback-tag knowledge fixtures that exercise separate articles instead of colliding on the same normalized identity.

- [ ] **Step 1: Witness RED on fresh PostgreSQL 17**

Run `TestInitialTagAssignmentsAgainstPostgres` alone against an empty database from the pinned PostgreSQL 17 image.

Expected: FAIL creating the fallback knowledge article with `knowledge identity conflict` because both article mutations use title `Task 5` for the same client.

- [ ] **Step 2: Make the article identity fixture-specific**

In `articleMutation`, derive the title from the stable unique `objectID` (for example, `"Task 5 "+objectID`). Keep the article IDs, display IDs, body, tag assignments, production repository behavior, and every assertion unchanged.

- [ ] **Step 3: Witness GREEN and commit**

Run the focused test twice against separate fresh PostgreSQL 17 databases. Expected: both pass through direct, fallback, and rollback assertions.

```bash
git add backend/internal/store/psa/initial_tag_assignments_integration_test.go
git commit -m "test: distinguish initial tag knowledge fixtures"
```

---

### Task 14: Diagnose frontend formatting drift

**Files:**
- Verify only: `frontend/src/**/*.{ts,tsx,css}` and external evidence.

**Interfaces:**
- Consumes: the exact pinned Prettier 3.9.6 dependency installed by `npm ci`.
- Produces: the exact set of tracked source files failing the CI formatting gate.

- [x] **Step 1: Run the exact formatting gate**

`npx prettier --check "src/**/*.{ts,tsx,css}"` failed on 29 tracked files after clean install.

- [x] **Step 2: Confirm formatter/runtime parity and minimize**

Node 24.19.0, npm 11.17.0, and declared/locked/runtime Prettier 3.9.6 agree. A focused in-memory format diff for `src/features/ai/api.ts` proves ordinary indentation drift rather than a formatter-version mismatch. Evidence is recorded in `/tmp/rarity-task14-attempt2.3kgIV7`.

---

### Task 15: Normalize tracked frontend formatting

**Files:**
- Modify: exactly the files reported by `npx prettier --check "src/**/*.{ts,tsx,css}"`

**Interfaces:**
- Consumes: the repository's pinned Prettier 3.9.6 configuration/defaults.
- Produces: formatting-clean TypeScript, TSX, and CSS with no behavioral change.

- [ ] **Step 1: Preserve the exact RED file inventory**

Run the formatting check after `npm ci` and save its 29-file warning list. Refuse scope expansion if the write command changes a file outside that inventory.

- [ ] **Step 2: Apply only the pinned formatter**

Run from `frontend/`:

```bash
npx prettier --write "src/**/*.{ts,tsx,css}"
```

Compare the changed-file list to the RED inventory and inspect the complete diff for behavior changes. Do not hand-edit or change formatter configuration/dependencies.

- [ ] **Step 3: Verify formatting and frontend behavior**

Run:

```bash
npx prettier --check "src/**/*.{ts,tsx,css}"
npm test -- --run
npm run build
```

Expected: formatting passes, all component/accessibility tests pass, and the production build succeeds.

- [ ] **Step 4: Commit the mechanical normalization**

```bash
git add frontend/src
git commit -m "style: normalize frontend formatting"
```

---

### Task 16: Diagnose stale browser contracts

**Files:**
- Verify only: browser fixtures and external Playwright evidence.

**Interfaces:**
- Consumes: the current authorized-client selection contract and route manifest's `mobile` capability.
- Produces: exact stale fixture/test assumptions for the five deterministic browser failures.

- [x] **Step 1: Reproduce on an isolated browser harness**

With a stable bridge to pinned PostgreSQL 17, Playwright passed 11/16 and failed the same five tests twice. Evidence is recorded in `/tmp/rarity-task16-local.hQH23j`.

- [x] **Step 2: Identify the current contracts**

`authenticatedPage.ts` supplies an empty authorized-client directory, so sales/project routes correctly render `Client scope required`. The design-system route is explicitly `mobile: false`, so widths below `47.99rem` correctly render `Desktop workspace`; the failing tests still expected catalog controls at 720, 390, and 360 CSS pixels.

---

### Task 17: Align E2E fixtures with client scope and mobile routing

**Files:**
- Modify: `frontend/tests/e2e/authenticatedPage.ts`
- Modify: `frontend/tests/e2e/design_system.spec.ts`

**Interfaces:**
- Consumes: the authenticated directory response shape and route manifest's desktop-only design-system declaration.
- Produces: operational E2E tests with one authorized client plus explicit accessible handoff coverage at narrow/effective-zoom/touch widths.

- [ ] **Step 1: Preserve the five-test RED evidence**

Run the three operational specs and the two failing design-system cases against the isolated pinned PostgreSQL 17 browser harness. Expected: operational routes render `Client scope required`; narrow catalog cases render `Desktop workspace` instead of their stale controls.

- [ ] **Step 2: Supply one canonical authorized client**

In `authenticatePage`, replace only the empty `clients` collection with one directory client using the deployed response fields (`id`, `display_id`, `name`). Keep departments, teams, queues, principal, capabilities, and navigation unchanged. This makes the operational test routes select a real authorized client instead of bypassing client-scope behavior.

- [ ] **Step 3: Assert the desktop-only mobile handoff**

Keep desktop catalog interaction coverage at supported widths. At 720/390/360 widths, assert the `Desktop workspace` eyebrow, `Design system` heading, no horizontal overflow, and reachable return action instead of catalog-only controls. In the touch project, require a 44-by-44-pixel return target, tap it, and verify navigation to mobile Home. Update misleading test names accordingly; do not change the route manifest or production components.

- [ ] **Step 4: Run targeted and full browser gates**

Run the five formerly failing cases, then all 16 Playwright tests using the exact pinned PostgreSQL 17 image in an isolated harness that does not disturb user-owned ports/containers. Expected: 5/5 targeted and 16/16 full pass with no relevant browser console errors.

Also run from `frontend/`:

```bash
npx prettier --check "tests/e2e/**/*.{ts,tsx}"
npm test -- --run
npm run build
```

- [ ] **Step 5: Commit the E2E contract repair**

```bash
git add frontend/tests/e2e/authenticatedPage.ts frontend/tests/e2e/design_system.spec.ts
git commit -m "test: align browser scope and mobile contracts"
```

---

### Task 18: Back current browser journeys with scoped acceptance fixtures

**Files:**
- Create: `frontend/tests/e2e/operationalFixtures.ts`
- Modify: `frontend/tests/e2e/change_order.spec.ts`
- Modify: `frontend/tests/e2e/opportunity_to_project.spec.ts`
- Modify: `frontend/tests/e2e/project_capacity.spec.ts`
- Modify: `frontend/src/app.css`

**Interfaces:**
- Consumes: current client-scoped opportunity, proposal, conversion, project, change-order, and mobile-handoff UI contracts.
- Produces: deterministic browser fixtures that exercise those current contracts without reintroducing application demo data, plus a WCAG-sized handoff action.

- [ ] **Step 1: Preserve the four remaining RED cases**

Run the three operational specs and 390px touch-handoff case against the isolated browser harness. Expected: operational pages receive empty/unsupported acceptance endpoints and the handoff return action measures 25px high.

- [ ] **Step 2: Add narrowly scoped operational network fixtures**

Create helpers that register only the API routes each operational journey needs. Use the same `client-id` exposed by `authenticatePage`, assert the `X-Rarity-Client-ID` header on client-scoped requests, return production response shapes, and keep mutable state only where a successful mutation must be observed by a subsequent reload. Unexpected methods or malformed version/reason/conversion bodies must fail the route rather than receive a generic success.

Cover:

- one current opportunity plus pipeline/forecast/activity/task/attachment data;
- one accepted proposal and current version;
- conversion preview and conversion results with explicit selected-task state;
- one project workspace with capacity, current baselines, and one issued change order;
- override/apply mutations that require the expected version/reason and expose approved/applied state on the following project reload.

Do not change the shared classification server or intercept the classification journey's production-backed routes.

- [ ] **Step 3: Update the three journeys to the current UI flow**

Each spec registers only its fixture before navigation. Preserve the business outcomes while following the current live pages: scoped opportunity visibility, accepted-proposal selection and explicit conversion preview, selected kickoff task, project creation/baseline evidence, capacity over/free values, and reasoned one-time change-order override/apply. Assert the relevant scoped requests, not only visible text.

- [ ] **Step 4: Make the handoff action touch-sized**

Add a narrowly scoped `.rti-mobile-handoff button` rule with at least 44px minimum inline and block dimensions. Do not change the breakpoint, route manifest, or other controls. The Task 17 touch test is the RED/GREEN behavioral proof.

- [ ] **Step 5: Run browser and frontend gates**

Run the four formerly failing cases, then all 16 Playwright tests using the isolated exact PostgreSQL 17 harness. Expected: 4/4 targeted and 16/16 full pass, including restored mobile navigation geometry/focus and the production-backed classification journey.

Run Prettier check, all 93/683 frontend unit tests, and the production build.

- [ ] **Step 6: Commit the acceptance repair**

```bash
git add frontend/tests/e2e/operationalFixtures.ts \
  frontend/tests/e2e/change_order.spec.ts \
  frontend/tests/e2e/opportunity_to_project.spec.ts \
  frontend/tests/e2e/project_capacity.spec.ts frontend/src/app.css
git commit -m "test: restore scoped browser journeys"
```

---

### Task 19: Keep conversion task selection inside the preview hash

**Files:**
- Modify: `frontend/src/features/projects/LiveConversionPage.tsx`
- Modify: `frontend/src/features/projects/LiveConversionPage.test.tsx`
- Modify: `frontend/src/features/projects/ConversionPreview.tsx`
- Modify: `frontend/src/features/projects/ConversionPreview.test.tsx`
- Modify: `frontend/tests/e2e/operationalFixtures.ts`
- Modify: `frontend/tests/e2e/opportunity_to_project.spec.ts`

**Interfaces:**
- Consumes: backend conversion preview semantics, where selected task IDs and versions are loaded, validated, serialized, and SHA-256 hashed into the preview.
- Produces: a launcher that selects tasks before preview, a review surface whose selected set cannot drift, and an E2E fixture that enforces identical preview/convert state with a production-shaped hash.

- [ ] **Step 1: Add failing unit contracts for pre-preview selection**

Extend `LiveConversionPage.test.tsx` with current opportunity-task responses. Require open tasks to be selectable before `Preview conversion`, completed tasks to remain disabled, and the preview body to include the exact selected task IDs and versions. Expected RED: the launcher does not load/render tasks and sends empty task state.

Update `ConversionPreview.test.tsx` to require every task returned by the preview to be shown as already selected and immutable, and `Create project` to submit exactly the preview's task IDs/versions. Expected RED: the current component starts empty and permits post-hash selection changes.

- [ ] **Step 2: Load and select opportunity tasks before preview**

Use the existing client-scoped `listOpportunityTasks` API for the selected opportunity. Reset/abort task state when the selected proposal/opportunity changes. Render a launcher fieldset with incomplete tasks selectable and completed history disabled. Include only selected incomplete IDs and their current versions in both the preview request stored for conversion and the request body. Preserve titles by joining the returned preview IDs to the loaded task records.

- [ ] **Step 3: Freeze the reviewed task set through conversion**

Make `ConversionPreview` present the preview's tasks as checked, disabled reviewed evidence. Remove post-preview task toggling. `Create project` must return the exact task IDs/versions already in the preview alongside its hash. Do not allow a task set different from the hashed preview to reach `convertOpportunity`.

- [ ] **Step 4: Make the operational fixture reject the old impossible flow**

Require the kickoff ID/version in the preview body and the identical set in the convert body. Construct a complete production-shaped preview payload and compute or supply its real lowercase 64-hex SHA-256 hash; reject a changed set/hash. Extend the journey assertion to prove the associated `$100,000` original commercial baseline and `40h` planned work, not only static labels.

- [ ] **Step 5: Verify unit, browser, and build gates**

Run focused conversion component tests, all 93 frontend unit files, Prettier, and build. Run the opportunity journey, the four Task 18 targeted browser cases, and all 16 Playwright tests in the isolated pinned PostgreSQL 17 harness. Expected: every gate passes and the strict fixture records identical preview/convert task state.

- [ ] **Step 6: Commit the conversion integrity repair**

```bash
git add frontend/src/features/projects/LiveConversionPage.tsx \
  frontend/src/features/projects/LiveConversionPage.test.tsx \
  frontend/src/features/projects/ConversionPreview.tsx \
  frontend/src/features/projects/ConversionPreview.test.tsx \
  frontend/tests/e2e/operationalFixtures.ts \
  frontend/tests/e2e/opportunity_to_project.spec.ts
git commit -m "fix: bind conversion tasks to preview"
```

---

### Task 20: Verify locally, publish the exact SHA, and verify artifacts

**Files:**
- Verify only: repository and external release evidence.

**Interfaces:**
- Consumes: the reviewed repair commits, GitHub `CI`, the chained `Publish reviewed images` workflow, GHCR, and `scripts/verify-release-artifacts.sh`.
- Produces: one exact successful `main` revision plus API/frontend signature, SBOM, provenance, and manifest evidence outside the repository.

- [ ] **Step 1: Run Go formatting, module, static, and portable tests**

Run:

```bash
test -z "$(gofmt -l backend infrastructure tests)"
go mod verify
git diff --check
go test -race ./backend/...
go test ./tests/... -count=1
scripts/generate-local-env.sh
go test ./infrastructure/... -count=1
rm -f .env
go vet ./backend/... ./tests/...
```

Run the generated `.env` steps under an exit trap that removes `.env` even if the infrastructure test fails. Expected: every command exits 0 and `.env` is absent afterward, matching CI's transient environment setup.

- [ ] **Step 2: Run the full PostgreSQL-backed backend and integration suite**

Run:

```bash
TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:55432/rarity_test?sslmode=disable' \
  scripts/run-postgres-acceptance.sh
```

Expected: PASS with no fixture residue or foreign-key teardown errors.

- [ ] **Step 3: Run frontend, documentation, and browser gates**

Run from `frontend/`:

```bash
npm ci
npm audit --audit-level=high
npx prettier --check "src/**/*.{ts,tsx,css}"
npm test -- --run
npm run build
TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:55432/rarity_test?sslmode=disable' npx playwright test
```

Run from the repository root:

```bash
node scripts/validate-markdown-links.mjs
node scripts/validate-docs.mjs
node scripts/validate-integration-automation-docs.mjs
```

Expected: every command exits 0, Playwright starts the Go server, and there are no relevant browser console errors.

- [ ] **Step 4: Stop the disposable database and confirm a clean tree**

Run:

```bash
docker stop rarity-ci-recovery-postgres
git status --short
```

Expected: the container is removed by `--rm`; Git emits no working-tree entries.

- [ ] **Step 5: Push the repaired `main` and monitor exact-SHA CI**

Run:

```bash
git push origin main
repair_sha="$(git rev-parse HEAD)"
ci_run_id="$(
  gh run list --workflow CI --commit "$repair_sha" --event push --limit 1 \
    --json databaseId,headSha \
    --jq '.[0] | select(.headSha == "'"$repair_sha"'") | .databaseId'
)"
test -n "$ci_run_id"
gh run view "$ci_run_id" --json headSha,url \
  --jq 'select(.headSha == "'"$repair_sha"'") | .url'
gh run watch "$ci_run_id" --exit-status
```

Expected: the SHA equality check succeeds and source/security, PostgreSQL, and browser journeys all pass for `$repair_sha`.

- [ ] **Step 6: Monitor the chained exact-SHA publish workflow**

Run:

```bash
publish_run_id=""
for attempt in $(seq 1 30); do
  publish_run_id="$(
    gh run list --workflow 'Publish reviewed images' --branch main --limit 10 \
      --json databaseId,headSha,event \
    | jq -r --arg sha "$repair_sha" \
        '.[] | select(.headSha == $sha and .event == "workflow_run") | .databaseId' \
    | head -1
  )"
  if [[ -n "$publish_run_id" ]]; then
    break
  fi
  sleep 10
done
test -n "$publish_run_id"
gh run view "$publish_run_id" --json headSha,url \
  --jq 'select(.headSha == "'"$repair_sha"'") | .url'
gh run watch "$publish_run_id" --exit-status
```

Expected: both `Publish api` and `Publish frontend` succeed, including digest scan, keyless signing, signature verification, and promotion.

- [ ] **Step 7: Resolve immutable references and verify release evidence**

Resolve each promoted revision tag and require the returned `Name:` value to be an immutable `@sha256:` reference:

```bash
api_tag="ghcr.io/aaroncampbellit/rarity-ticket-intelligence-api:${repair_sha}"
frontend_tag="ghcr.io/aaroncampbellit/rarity-ticket-intelligence-frontend:${repair_sha}"
api_ref="$(docker buildx imagetools inspect "$api_tag" | sed -n 's/^Name:[[:space:]]*//p' | head -1)"
frontend_ref="$(docker buildx imagetools inspect "$frontend_tag" | sed -n 's/^Name:[[:space:]]*//p' | head -1)"
[[ "$api_ref" =~ ^ghcr.io/aaroncampbellit/rarity-ticket-intelligence-api@sha256:[0-9a-f]{64}$ ]]
[[ "$frontend_ref" =~ ^ghcr.io/aaroncampbellit/rarity-ticket-intelligence-frontend@sha256:[0-9a-f]{64}$ ]]
```

Create the evidence directory outside the repository and run the verifier against those exact references:

```bash
evidence_directory="$(mktemp -d /tmp/rarity-release-evidence.XXXXXX)"
scripts/verify-release-artifacts.sh \
  aaroncampbellit/rarity-ticket-intelligence \
  "$repair_sha" \
  "$api_ref" \
  "$frontend_ref" \
  "$evidence_directory"
find "$evidence_directory" -maxdepth 1 -type f -printf '%f\n' | sort
```

Expected: `Verified release artifacts for aaroncampbellit/rarity-ticket-intelligence`; the directory contains both signature bundles, both SBOMs, both provenance files, and `artifact-provenance.json`, all tied to `$repair_sha`.

- [ ] **Step 8: Record final verification state**

Run:

```bash
git status --short --branch
git log -8 --oneline
```

Expected: clean `main`, synchronized with `origin/main`, with the design, plan, workflow, fixture, and dependency commits visible. Report the exact CI URL, publish URL, `$repair_sha`, both immutable digest references, and `$evidence_directory`.

---

### Task 21: Upgrade the vulnerable Go network module

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `infrastructure/compose/api.Dockerfile`
- Modify: `infrastructure/compose/frontend.Dockerfile`

**Interfaces:**
- Consumes: exact-SHA CI run `32100999493`, which reports HIGH `CVE-2026-46600` in selected `golang.org/x/net v0.55.0` with fixed version `v0.56.0`.
- Produces: the same Go module graph and application behavior with `golang.org/x/net v0.56.0` selected, the Go toolchain pinned to patched `go1.26.6`, and no HIGH/CRITICAL Trivy finding in source or built artifacts.

- [ ] **Step 1: Preserve the failing security evidence and dependency path**

Record the Trivy finding from run `32100999493`. Confirm `go mod why -m golang.org/x/net` reaches the module through the object-storage/MinIO dependency path and that the current selected version is `v0.55.0`.

- [ ] **Step 2: Apply the narrow dependency repair**

Upgrade `golang.org/x/net` to `v0.56.0` using the Go module toolchain, pin the `toolchain` directive to `go1.26.6`, then run `go mod tidy`. Pin both release builder stages to `golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83`; the current `golang:1.25-alpine` builders set `GOTOOLCHAIN=local` and otherwise ignore the module toolchain directive. The official Go vulnerability record identifies affected standard-library paths in both Go 1.25.12 and Go 1.26.5, so the module, repository toolchain, and release-builder boundaries must all be patched. Expected tracked changes are limited to `go.mod`, `go.sum`, and the two release Dockerfiles; do not suppress or ignore the vulnerability.

- [ ] **Step 3: Verify the repaired module graph and legitimate behavior**

Run:

```bash
go list -m golang.org/x/net
go version
go mod verify
git diff --check
go test -race ./backend/...
go test ./tests/... -count=1
go vet ./backend/... ./tests/...
```

Expected: the selected module is exactly `golang.org/x/net v0.56.0`, the active repository toolchain is `go1.26.6`, the vulnerable `v0.55.0` checksums are absent, and all behavior-preservation gates pass.

- [ ] **Step 4: Re-run the security trigger**

Run a current Trivy filesystem scan with the same CI scanners, severity threshold, and `ignore-unfixed` behavior. Build both Go-containing release artifacts with the pinned toolchain and scan them with the publish workflow's HIGH/CRITICAL policy. Expected: every scan exits 0, including absence of the standard-library `CVE-2026-46600`; there are no HIGH/CRITICAL vulnerabilities, secrets, misconfigurations, or disallowed licenses.

- [ ] **Step 5: Commit the reviewed security repair**

```bash
git add go.mod go.sum infrastructure/compose/api.Dockerfile infrastructure/compose/frontend.Dockerfile
git commit -m "fix: update vulnerable network module"
```

---

### Task 22: Re-run exact-SHA release verification and artifact publication

**Files:**
- Verify only: repository and external release evidence.

Repeat every local gate and safety constraint from Task 20 against the reviewed Task 21 head. Push only the exact locally verified SHA, monitor its exact-SHA `CI` run to success, then monitor the chained exact-SHA `Publish reviewed images` run to success. Resolve both promoted image tags to immutable digests and run `scripts/verify-release-artifacts.sh` with a task-owned temporary Cosign installation and evidence directory outside the repository. Report the final SHA, CI and publish URLs, both immutable image references, evidence directory contents, clean synchronized branch state, and proof that all task-owned containers/processes were removed while the user-owned services were preserved.
