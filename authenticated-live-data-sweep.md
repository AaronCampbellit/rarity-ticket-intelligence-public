# Authenticated live-data sweep

Date: 2026-08-03
Live URL: `https://rarity.campbellservers.com`
Fresh-install candidate: `10c9add3cd38efd81c316917f92dc98f5dcc059c`

## Verdict

The fresh-install migration is functionally complete for the configured demo
surface. The design-system shell, long names, primary sales-to-project flow,
empty collections, administrative pages, and mobile navigation were exercised
against a newly bootstrapped database.

The sweep created synthetic Northwind data through the UI: client, department,
team, queue, prospect, two pipelines, opportunities, accepted proposals, and a
converted project with phases, resource plan, budget, labor, and projected
profit. No real customer data was used.

## Defects found and fixed

1. Global Administrator navigation exposed pages without the action
   capabilities their live controls require.
2. Pipelines could be created without a `closed_won` stage even though
   conversion requires one.
3. Empty conversion phase arrays serialized as `null` and violated the
   PostgreSQL JSON contract.
4. Project financials queried a table name that does not exist in the migrated
   schema.
5. Empty phase teams and delivery collections could blank the entire project
   route.
6. Fresh Service Desk collections could be `null`, causing a React `.find`
   crash.
7. Datto reconciliation used an ambiguously typed PostgreSQL parameter and
   failed before returning an empty candidate list.
8. Empty business calendars serialized as `null` instead of `[]`.
9. Local administrator cookies were selected only by environment name rather
   than the configured HTTPS public URL.

Each code defect has a focused regression test.

## Route and state coverage

- Authenticated desktop UI: Work, Sales, Proposals, Conversion, Projects,
  Prospects, Knowledge, Clients and directory, Client resources, Sessions,
  Local administrators, Roles, and Audit.
- Fresh-install desktop recovery: Service Desk crash reproduced, fixed,
  rebuilt, deployed, and reloaded without a console error.
- Authenticated API contracts: principal, audit, local administrators, roles,
  service keys, automation, billing approvals, calendars, client resources,
  directory, integration configuration and health, Teams, Webhooks, Knowledge,
  opportunities, forecasting, pipelines, projects, proposals, prospects,
  sessions, setup center, SLA, work, and workflows.
- Responsive UI: all 29 routes at a 390 × 844 viewport, including the mobile
  drawer, compact application symbol, long sales/project names, cards, loading
  states, empty states, and permission/error panels. No route emitted a new
  console warning or error.

The compact header retains the Rarity symbol. Opening the drawer reveals the
full Rarity wordmark, correctly sized navigation, larger section headings, and
Admin-contained administrative routes.

## Live-state results

- Knowledge, Teams, and Webhooks return healthy empty collections on a fresh
  install.
- Billing approvals return a healthy empty collection.
- Datto reconciliation was the only remaining route-specific 500; its SQL
  parameter contract is fixed in this candidate.
- The fresh directory contains one client, department, team, and queue.
- The sales and delivery data contains one prospect, two pipelines, three
  opportunities, two proposals, and one converted project.
- Provider-backed Datto, Graph, forwarding, Teams delivery, Webhooks delivery,
  AI, and Entra scenarios remain configuration-dependent rather than
  fresh-install failures.

## Acceptance boundary

The in-app browser's secure credential-handoff capability was unavailable
after the final HTTPS-cookie deployment. Per the authentication safety
boundary, no additional browser login workaround was attempted. The final
candidate therefore still needs one short authenticated browser confirmation
that the secure local-administrator cookie persists across a reload.

Lower-role visual acceptance also remains pending an appropriately permissioned
interactive account. API permission behavior and the Global Administrator
capability set were exercised earlier in this sweep.

## Verification

- Focused Go tests for API composition, Datto management/repository behavior,
  SLA empty collections, project conversion/query behavior, and principal
  capabilities.
- Focused frontend tests for Service Desk fresh-install collections and project
  null collection handling.
- TypeScript check and production Vite build.
- Live HTTPS deployment at the exact candidate revision.
- Authenticated live API and PostgreSQL checks against the fresh synthetic
  dataset.

This is shared-demo evidence, not production, HA, backup/restore, capacity, or
configured-provider acceptance.
