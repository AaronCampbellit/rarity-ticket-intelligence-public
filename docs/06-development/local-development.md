# Local development

**Status:** Current developer workflow
**Updated:** 2026-09-05

Use synthetic data. Run commands from the repository root unless stated otherwise.
Go versions come from `go.mod`; frontend dependencies and the supported Node minimum
come from `frontend/package.json` (CI uses Node 22). Docker Engine with Compose v2
is required for Compose checks and disposable database acceptance. Install missing
tools through your normal approved workstation setup.

## Source checks

```bash
npm --prefix frontend ci
go test ./backend/... ./tests/...
npm --prefix frontend test
npm --prefix frontend run build
node scripts/validate-markdown-links.mjs
node scripts/validate-docs.mjs
node scripts/validate-integration-automation-docs.mjs
node --test scripts/validate-release-acceptance-docs.test.mjs
```

Without `TEST_DATABASE_URL`, database-dependent Go tests skip. A successful portable
run is not PostgreSQL acceptance. `make format` rewrites source; use the check-only
commands in [CI](../../.github/workflows/ci.yml) for a read-only review.

## Disposable PostgreSQL acceptance

Use a dedicated container, never a live application database. The runner creates
and drops `rarity_test_acceptance_*` databases on the supplied server, so do not run
two acceptance runners against the same server at once.

```bash
docker run --detach --name rarity-dev-postgres \
  --publish 127.0.0.1:55432:5432 \
  --env POSTGRES_HOST_AUTH_METHOD=trust --env POSTGRES_DB=rarity_test \
  postgres:16.11-alpine3.22@sha256:4ddcf75a487ff3793dcb8bd5e085304c4b03b99cd7f5fc93f7961ba19e25af08
docker exec rarity-dev-postgres pg_isready -U postgres -d rarity_test
TEST_DATABASE_URL='postgres://postgres@127.0.0.1:55432/rarity_test?sslmode=disable' \
  scripts/run-postgres-acceptance.sh
```

Wait for `pg_isready` to report ready before starting tests. Trust authentication
is only for this disposable, loopback-bound synthetic fixture; never use it for
application deployment. Stop and remove this specific test container when finished:

```bash
docker stop rarity-dev-postgres
docker rm rarity-dev-postgres
```

CI also qualifies PostgreSQL 17. Local PostgreSQL 16 results do not claim that
separate CI job passed for the working tree.

## Browser journeys

Use a fresh dedicated PostgreSQL database as above. Playwright starts its Go
acceptance server on port 18082 and Vite on 18081. Both ports must be free;
stop only servers you own or adjust the local configuration to avoid a conflict.
A separately running application on 18081 is not the acceptance server.

```bash
(cd frontend && TEST_DATABASE_URL='postgres://postgres@127.0.0.1:55432/rarity_test?sslmode=disable' npx playwright test)
```

Alternatively run `npx playwright test` from `frontend/` with the same environment.
Use an already installed compatible Chromium, or install Playwright's browser
through your approved tool setup. Some journeys use HTTP fixtures; the
classification and calendar journeys use the real Go/PostgreSQL acceptance server. Neither
substitutes for full deployed pilot acceptance.

## Local application and first ticket

```bash
make local-env
docker compose --env-file .env -f infrastructure/compose/compose.yaml config --quiet
docker compose --env-file .env -f infrastructure/compose/compose.yaml up --build -d
```

The generator refuses to overwrite `.env`. It creates local credentials and binds
Caddy to `http://127.0.0.1:18080`. The API applies migrations and creates the local
attachment bucket at startup. View API startup output privately to open its
single-use setup URL; do not paste that token-bearing output into reports.
Complete the organization and local administrator setup, then sign in. Entra is optional.

In the authenticated Setup Center, resolve configuration requirements. Create a
synthetic Client and configure service-desk routing, a published workflow, and an
SLA/business calendar before creating a Work Record. Open Work for that Client,
create an Incident, assign it, add an internal note and time, and transition it
using the configured workflow. These are application steps, not fixture seeding.

Stop the local application with `docker compose --env-file .env -f
infrastructure/compose/compose.yaml stop`. Preserve its volumes unless you intend
to discard all local data. Pilot deployment uses a separate TLS profile and the
[pilot runbook](../05-infrastructure/pilot-acceptance-runbook.md).
