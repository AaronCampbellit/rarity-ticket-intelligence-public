# Current execution backlog

Publication note: historical commits, CI runs, pull requests, signing identities,
and image digests in this document belong to the original private development
repository. They are retained as provenance for their recorded revisions; this
public source snapshot begins with fresh history and does not publish those
old artifacts or attest a new release.

**Updated:** 2026-09-05
**Status:** Local qualification and constrained authenticated demo acceptance complete

This is the ordered execution queue. It supersedes the scheduling and checkbox
state of older build plans, while preserving their accepted domain constraints.
The implementation baseline is `bb8c7ad`; changes are committed on
`codex/calendar-demo-qualification` and proposed in
private development pull request `#1`.
Local and constrained-demo evidence below is not a signed release candidate
attestation.

## Current assessment

Core ticketing, sales, project delivery, integrations, AI, classification, internal
mentions, notifications, and operational tooling have source implementations.
The unified calendar now has its browser workspace, typed source forms, source
editors, scheduling/dependency controls, and operational tools. The ticket resolver
and 100-record worklist issues found during review are repaired; protected frontend
pages load on demand. Local evidence applies to this working tree. Supported
provider, deployment, capacity and manual accessibility acceptance remain open.

## Ordered local work

| ID | Work | Completion criterion | State |
| --- | --- | --- | --- |
| D1 | Reconcile entry points, roadmap, plan archive, and tracker | Current status agrees across entry points; local instructions and documentation validators pass | Complete: documentation validators pass |
| T1 | Repair ticket queue/assignment lookup row mapping | Both resolvers return real PostgreSQL rows including scheduling fields; regression tests pass | Complete: real PostgreSQL lookup regression passed |
| T2 | Make technician worklists complete beyond 100 records | Server filters and bounded pagination expose older matches; scope, stale requests, empty/error states covered | Complete: 106-ticket PostgreSQL fixture and component/API regressions passed |
| F1 | Split frontend feature loading | Major protected pages load on demand; existing navigation/tests/build pass with measured bundle output | Complete: lazy navigation tests and production build pass |
| C1 | Calendar browser contracts and state (task 11) | Strict runtime mappings, timezone preferences, abortable query state, six-lens primitives and tests | Complete: runtime contracts, scope/date/DST and stale-request tests pass |
| C2 | Calendar workspace (task 12) | Day/Week/Month/Agenda, visible authorized filters, saved lenses, protected route and source navigation | Complete: six lenses, filters, saved views, source links, route and Home feed |
| C3 | Calendar scheduling and capacity (task 13) | Timeline/Capacity, dependency and conflict presentation, preview/apply with explicit confirmation and keyboard equivalent | Complete: dependency privacy, capacity failure states and confirmed preview/apply |
| C4 | Calendar typed administration (task 14) | Existing milestone/PTO/maintenance/renewal/license, workforce, and custom-date contracts reachable under ordinary permissions | Complete: typed administration, source date editors and real-service browser round trips pass |
| C5 | Calendar acceptance and documentation (task 15) | Real API/browser verification, stale/scope/timezone cases, operational documentation and all relevant source checks | Local work complete: contract, reconciliation CLI, fixture export and performance harness verified; external qualification remains below |
| Q1 | Run available qualification checks and repair failures | Go/race/vet, disposable PostgreSQL, frontend, browser, infrastructure and documentation checks recorded | Complete for the local environment; results below |

Calendar implementation follows the [unified calendar specification](../superpowers/specs/2026-08-07-unified-calendar-design.md)
and [tasks 11–15](../superpowers/plans/2026-08-07-unified-calendar.md#task-11-build-the-shared-calendar-browser-contract-and-primitives).
The deployed API and accepted domain invariants take precedence over illustrative
code or stale migration numbers in the original plan. Recheck existing code before
each task; do not recreate implemented tasks 1–10.

## External acceptance and human decisions

Continue all independent local work before stopping for these inputs.

The existing demo on `hankdemoserver` at
[rarity.campbellservers.com](https://rarity.campbellservers.com) is the current
live verification target; a new host or domain is not needed for that work.
Its API and frontend have been updated using locally built, revision-labelled
images pinned by image ID. Only those two services were recreated. Existing
PostgreSQL, storage, proxy and unrelated workloads were preserved. No migrations
were added by this change. Candidate and rollback metadata are retained at
`/home/campbellservers/rti-deployments/20260905-calendar/` on the demo host.

Host-local and public HTTPS health, readiness, build identity and rendered-shell
checks passed. The deployed reconciliation CLI repaired one missing source's
derived projection state; a subsequent read-only scan found no missing, stale or
orphaned state across all four current sources. The public browser rendered the
Microsoft sign-in page and local administrator form on desktop and phone, with
no page exceptions or horizontal overflow. The expected unauthenticated
`/api/v1/me` response is 401. Browser acceptance found a 4.26:1 sign-in button
contrast ratio; the follow-up uses the existing white action-text token.
The owner authorized a local administrator password reset through the supported
operator CLI. The reset advanced the account version, revoked both existing
sessions, and persisted its audit and outbox records. Authenticated sign-in then
succeeded with secure session cookies. Credentials remain in a private local
handoff file, outside the repository and deployment evidence.

Authenticated live browser checks pass for all six calendar lenses, timezone
changes, a private saved view surviving reload, keyboard event selection,
project source navigation, all administration tabs and client/project/technician
choices. Ticket checks pass for matching and empty searches, record selection,
List/Kanban switching and keyboard selection. Calendar, form and ticket axe
checks report no violations; phone calendar handoff has no horizontal overflow.
The live directory exposed mixed uppercase client envelopes and lowercase roster
fields. Calendar now reuses the shared directory normalizer; its regression test
and the corrected real-service browser fixture pass. Screenshot review also found
ticket rows stretching to the detail panel's height. Aligning the grid items to
the start reduces those rows from 503–519 px to 126–142 px at 1440 px width.

This acceptance created one private calendar view. Source schedules and outbound
provider communications were not changed. The demo's single-client, two-ticket
fixture does not establish live cross-client, high-volume, scheduling mutation or
provider acceptance; those source mutations were exercised in the isolated
real-service browser harness.

The first PR run passed PostgreSQL 17, browser journeys and source tests, but its
dependency scan rejected `golang.org/x/crypto` v0.53.0 for
[CVE-2026-56854](https://pkg.go.dev/vuln/GO-2026-6303). The dependency is updated to
the fixed v0.55.0 with the companion modules required by Go's module selection.
Rarity uses the module's password-hashing package; `go mod why` reports no
dependency on the affected `golang.org/x/crypto/ssh` package. This is dependency
maintenance and does not establish that Rarity exposed the vulnerable SSH path.

| Gate | Required input or environment | What local work can establish |
| --- | --- | --- |
| Reviewed delivery | Reviewer approval of pull request 1 under ADR-0034 | Reviewable source, CI and constrained demo evidence; no automatic main merge |
| Configured Graph, Datto, Teams and additional AI providers | Authorized non-production accounts, credentials and provider choices | Simulated provider contracts, retries, scope and redaction |
| Supported pilot | Named Ubuntu 24.04 host, TLS/secret configuration and candidate digests | Compose validation, build and migration contracts |
| HA, restore/DR, upgrades, telemetry and capacity | Separate three-node HA/restore targets, remote backup storage, collector and supported capacity host | Tooling and synthetic tests; no claim of operational RPO/RTO or load qualification |
| Manual accessibility | Human operators with VoiceOver/NVDA, actual browser zoom and physical touch devices | Browser/axe/keyboard/emulation evidence |
| Release | One immutable candidate with all required retained evidence and a release owner decision | Fail-closed validation of the manifest; historical signatures are not current-candidate proof |
| License and third-party distribution | Owner selected rights reserved for original material on 2026-10-08; third-party terms still apply | Preserve the original-only [rights reservation](../../LICENSE), [third-party notices](../../THIRD_PARTY_NOTICES.md), and outstanding distribution obligations |

## Verification record

- Full PostgreSQL acceptance runner passed using a disposable PostgreSQL 16 server.
  Subsequent calendar/source changes passed a focused PostgreSQL rerun, followed
  by the new conflict-policy read regression against the real schema.
- Complete backend race suite passed; changed calendar, HTTP, repository and CLI
  packages also passed a subsequent race run.
- Portable integration and infrastructure/Compose checks passed. Generated local
  environment files were removed after their checks.
- Go module verification/static analysis and frontend formatting pass. npm audit
  reports zero vulnerabilities.
- Full frontend baseline suite: **708 tests in 98 files passed**. A subsequent
  focused project workspace run passed all nine tests. The deployed-directory
  regression and calendar/API suite passed **34 tests in seven files**; the
  production frontend build also passed.
- Browser evidence includes real milestone, PTO, maintenance, renewal, license,
  workforce schedule, conflict-policy, custom-date and notification persistence;
  projected refresh; adversarial Busy payloads; proposal confirmation; axe checks;
  desktop screenshots and phone handoff. **All 18 Playwright tests passed** in the
  final run, including the existing classification and project journeys. All 18
  passed again after correcting the directory fixture to match the deployed API.
- Documentation contracts, local Markdown links, integration/automation checks,
  release-document validator tests and whitespace checks pass.
- Main JavaScript entry chunk: **829.85 → 309.29 kB**; gzip **208.14 → 88.33 kB**.
  Shared chunks and the requested feature load separately; this is not total page weight.
- Indexed in-memory performance fixture: 100,000 projections, 1,000 clients, 50
  concurrent technician queries over 90 days, **194 ms p95**; 100-node cascade
  **0.17 ms**. An earlier unindexed fixture took 628 ms p95; adding a technician
  index made the test repository reflect indexed lookup behavior. This measurement
  is not PostgreSQL or deployment load qualification.

## Implementation choices replacing illustrative plan steps

- Shared typed forms are grouped by domain and reused from the calendar, project
  and directory surfaces; the old plan's exact file list is not an acceptance API.
- Dependencies and source custom-date definitions needed new read endpoints with
  ordinary persisted source checks. Busy payloads retain their redaction boundary.
- Custom date definition reads now normalize nullable fields and database timestamp
  kinds. Projection adapters load current definitions without application restart.
- Conflict-policy reads expand the stored policy's four severity columns into the
  API's typed rule rows. A PostgreSQL regression verifies the real schema and
  versioned results; the browser round trip verifies policy creation and reread.
- `calendar-demo-seed` exports deterministic fixture descriptions without database
  writes. The isolated browser harness materializes real typed records through
  ordinary services. Arbitrary sample rows are never injected into production.
- The bounded `calendar-reconcile` command defaults to read-only; `--repair` is explicit.
- Historical release attestations and unchecked dated-plan steps remain historical.
  The scoped demo deployment and acceptance are recorded above. Provider messages,
  signed publication, third-party distribution compliance and release approval remain separate gates.
