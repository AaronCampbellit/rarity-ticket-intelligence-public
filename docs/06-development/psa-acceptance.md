# Native PSA Acceptance

**Status:** Source, browser, and PostgreSQL integration acceptance complete on the constrained demo profile.

## Accepted lifecycle

The initial native PSA release is accepted when the synthetic Northwind workflow can:

1. Move an Opportunity from Discovery into the Proposal workspace.
2. Retain issued and accepted Proposal Versions as immutable commercial records.
3. Preview conversion with an existing Client match, complete Proposal Line-to-Phase mapping, planned hours, budget, and selective movement of incomplete Opportunity Tasks.
4. Create a Project that exposes ordered Phases, task lineage, resource and team capacity, the original commercial baseline, current financials, and profitability.
5. Record a reasoned Change Order approval override as immutable evidence and apply the approved version once to the current Project baseline.

## Verification

Run from the repository root:

```bash
go test ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
npm --prefix frontend exec -- playwright test --config=frontend/playwright.config.ts
```

The browser suite covers the shared shell, Opportunity-to-Project lifecycle, selective task movement, original financial baseline, capacity and overbooking, and reasoned Change Order approval/application.

The local Vite shell uses the explicit `local-vite` build identity only on `localhost` and `127.0.0.1`. Browser tests intercept the build endpoint with a deterministic synthetic revision. Production remains fail-closed when build metadata is unavailable or invalid.

## Environment boundary

Portable unit, contract, HTTP, component, production-build, and browser checks
run without infrastructure. PostgreSQL transaction and migration tests skip
unless `TEST_DATABASE_URL` points to an isolated PostgreSQL database. On
2026-07-31, revision `3338d3c3430025c5766ac21cf9b7842bd7889b6f` passed the
complete migration chain and PSA PostgreSQL integration suite on the
constrained ADR-0033 demo host. The deployed revision also passed health,
readiness, build identity, rendered-shell revision, and a pinned Playwright
browser smoke check. This is demo evidence only; supported-profile, HA,
recovery, and capacity acceptance remain separate.

The PostgreSQL suite includes direct rejection proofs for Opportunity custom
field shape, cross-Client Contact participation, and cross-Client attachment
metadata in addition to conversion, Change Order, task-history, proposal
immutability, and Project-isolation contracts.
