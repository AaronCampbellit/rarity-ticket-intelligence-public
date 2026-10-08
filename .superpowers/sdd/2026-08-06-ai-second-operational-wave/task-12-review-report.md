# Task 12 whole-branch verification and review report

Date: 2026-08-07

## Scope and proof boundary

- The review started from clean Task 11 commit
  `51f187c46e5fdc3357d1ca5d02894be9f5844d59`.
- The design/branch base was `42ef4dd`. The exact committed source range
  reviewed was `42ef4dd..51f187c`; the final re-review included the Task 12
  working-tree fix and documentation through `git diff 42ef4dd`.
- The review covered authorization, bounded enumeration, preflight/execution
  drift, transaction order, audit/outbox atomicity, user-supplied-value
  enforcement, high-impact catalog leakage, and frontend stale-state handling.
- This report claims source and isolated-database verification only. Task 12
  did not publish Git state, push a branch, deploy any revision, inspect a live
  build identity, or perform authenticated browser acceptance.

## Contract and documentation result

The API contract, acceptance record, roadmap, and local documentation tracker
now identify the exact ten-tool wave and capabilities:

- `ticket.create` — `work_record.create`
- `ticket.assign` — `work_record.assign`
- `opportunity.list` and `opportunity.get` — `opportunity.read`
- `opportunity.transition` — `opportunity.transition`
- `opportunity.activity.create` — `opportunity.activity.create`
- `proposal.list` and `proposal.get` — `proposal.read`
- `proposal.create` — `proposal.create`
- `knowledge.publish` — `knowledge.publish`

They also record the explicit active-Client requirement, bounded exact
resolution, user-supplied business-value/no-invention rule, Ticket
preflight/selection/reference fences, Proposal display-ID and Opportunity
reference contract, assignment/transition/publication reason evidence,
high-impact exclusions, isolated PostgreSQL proof, and the pending
publication/deployment/authenticated-acceptance boundary. No docs-site manifest
change was needed because the permanent report is internal SDD evidence rather
than a public documentation-library document.

## Whole-branch finding and resolution

### Important — Ticket create omitted exact reference versions at commit

Evidence:

- `ticket.create` stored Service and Contract versions but did not store the
  active Client version.
- Confirmation reloaded the named Client/Service/Contract, but the ordinary
  Work Record create command carried only their identities.
- `CreateAtomic` locked the active Client, Service, and Contract without
  comparing the versions confirmed in the preview.
- A same-identity version change could therefore win between the final preview
  and row locks without invalidating the create, contrary to the approved
  confirmation and concurrency contract.

Focused RED evidence:

```text
go test ./backend/internal/aiassist/rtitools \
  -run TestTicketCreateToolRejectsClientVersionDriftBeforeCreate -count=1
```

Failed as intended:

```text
Confirm() error=<nil>, want ErrProposalStale
```

The service-propagation RED then failed to compile because
`ExpectedClientVersion`, `ExpectedServiceVersion`, and
`ExpectedContractVersion` did not exist. The repository RED failed because the
Client lock received no version argument and its query had no version fence.

Resolution:

- the prepared proposal now stores and compares the active Client version;
- the AI adapter passes confirmed Client, Service, and Contract versions to
  the ordinary create command;
- the application service preserves those versions in `CreateMutation`; and
- the repository locks the active Client, Service, and Contract at those exact
  versions before configuration locks and inserts. Zero retains the ordinary
  non-AI compatibility contract.

Focused GREEN evidence:

```text
go test ./backend/internal/aiassist/rtitools \
  -run 'TestTicketCreateTool(PreviewsCanonicalSelectionAndCreatesWithFence|RejectsClientVersionDriftBeforeCreate)' \
  -count=1
go test ./backend/internal/workrecords ./backend/internal/store/psa \
  -run 'TestCreate(PreflightAndCreateUseTheSameSelection|WorkRecordWritesRecordAndFactsAtomically)' \
  -count=1
```

Both commands passed.

## Exact review verdict

- **Authorization:** the Registry authorizes the prepared exact target, reloads
  the principal and execution target on confirmation, and every writer
  reauthorizes through the ordinary service. Explicit active Client resolution
  remains independent of the page Client hint.
- **Enumeration:** Ticket, technician, Opportunity, stage, Proposal, and
  Knowledge exact resolvers remain Client-scoped and bounded to two rows.
  Sales/Knowledge ambiguity retains typed safe mapping; technician and Ticket
  assignment deliberately collapse zero/multiple matches to enumeration-safe
  not found.
- **Preflight and execution drift:** Ticket creation shares one ordinary
  selection path and now fences Client/Service/Contract plus routing,
  Queue, Workflow, SLA policy, and calendar state. Proposal create preflights
  exact Opportunity eligibility and display-ID availability. Every write
  re-previews before the ordinary writer.
- **Transaction ordering:** the final repository order is active Client,
  target/reference rows, dependent configuration/version rows, mutation, audit,
  then outbox. The Task 11 deterministic PostgreSQL barriers cover each
  approved writer.
- **Audit/outbox:** Ticket assignment, Opportunity transition, and Knowledge
  publication store trimmed reasons. Each domain write, audit row, and outbox
  event remains in one transaction; Task 11 forced both audit and outbox
  failures for every writer.
- **No invention:** strict DTOs reject unknown fields and invalid enums. The
  planner proposes only complete approved sentence shapes and does not infer
  Ticket defaults, technician, stage, activity content, Proposal content or
  pricing, or Knowledge publication authority.
- **High-impact leakage:** composition and frontend catalog tests require the
  ten approved tools and exclude Proposal issue/approval/acceptance/conversion,
  pricing/lines, Opportunity conversion, Project financial/Change Order,
  time/billing, configuration, automation, integration, credential, secret,
  network, and external/client-visible Knowledge actions.
- **Frontend stale state:** read and proposal responses are fenced by request
  identity, action, Client, and resource kind. Action/Client changes invalidate
  pending results, the form prevents a second submission while busy, and the
  focused stale-target/stale-proposal tests pass.

Final verdict: one Important finding was fixed and re-reviewed. No Critical or
other Important finding remains. No safety- or behavior-relevant minor finding
was deferred.

## Task 11 isolated PostgreSQL evidence

Task 11 applied all 77 migration files through schema version 81 in isolated
PostgreSQL containers and scratch databases supplied only through
`TEST_DATABASE_URL`. It passed 29 leaf cases: 17 deterministic concurrency
cases and 12 forced audit/outbox rollback cases across Ticket
create/assignment, Opportunity transition/activity, Proposal draft creation,
and Knowledge publication.

The final isolated run reported `fixture_residue=0` and
`other_connections=0`. Exact scratch databases and temporary containers,
networks, and source bundles were removed. RTI, HankServerside, and RTM
inventory was unchanged. Task 12 did not recreate or rerun that isolated
database environment; the portable full Go gate executed the environment test
only under its explicit no-`TEST_DATABASE_URL` skip boundary.

## Required Task 12 gate

All commands were run from the isolated worktree after the fix and
documentation updates.

```text
gofmt -l backend
```

PASS, exit 0, no output.

```text
go test ./...
```

PASS, exit 0, all repository Go packages passed.

```text
go vet ./...
go build ./...
```

PASS, both exit 0 with no output.

```text
npm --prefix frontend test
```

PASS, exit 0: 78 test files and 475 tests passed. The run printed non-fatal
jsdom `HTMLCanvasElement.getContext()` notices; there was no failed test.

```text
npm --prefix frontend run build
```

PASS, exit 0: TypeScript and Vite production build completed. Output included
`index-DgerjL53.js` at 687.58 kB (169.87 kB gzip) and the existing non-fatal
Vite advisory for chunks larger than 500 kB. Bundle splitting is deferred as a
general performance improvement; it is not a second-wave correctness or safety
finding.

```text
node scripts/validate-markdown-links.mjs
```

PASS: `Local Markdown links valid.`

```text
node scripts/validate-docs.mjs
```

PASS: `Documentation contract valid: 12 PSA routes, 12 PSA events, 194 site documents.`

```text
git diff --check
```

PASS, exit 0, no output. The final whole-range `git diff --check 42ef4dd` also
returned exit 0 with no output.

## Remaining acceptance work

Task 13 still owns Git publication/parity, exact-revision demo deployment,
health/readiness/build/fresh-asset verification, authenticated 1440×900
journeys, audit/outbox inspection, browser-console evidence, final
documentation alignment, and the Hank RTI Kanban update. None is claimed here.

## Final review fix

Final review found that legacy assignment automations could validate and remain
published without a `reason`, but runtime forwarded the blank value to the
ordinary assignment service, which correctly rejects blank reason evidence.
The executor now preserves explicitly supplied reasons after trimming and, only
for a legacy blank assignment reason, supplies the deterministic evidence
`Automation assignment by definition <definition-id> (run <run-id>)`. Direct
and AI assignment paths retain the ordinary service's nonblank-reason
requirement.

The focused regression failed before the fix with the legacy run returning
`automation action failed`; it now covers published-definition validation,
Engine execution through the real assignment service, owner mutation, and the
exact stored audit reason and automation actor/source evidence. Focused
automation and work-record package tests pass.

## Task 13 live-acceptance hotfix source evidence

Authenticated live review of deployed revision `ed00bf8` found that the
principal's role grants and source catalog included `work_record.create` and
`work_record.assign`, while the `/api/v1/me`-driven AI action dropdown omitted
Create ticket and Assign ticket. Source tracing showed that
`principalUICapabilities` intentionally projects only known navigation and UI
action capabilities; those two approved action capabilities were missing from
`uiActionCapabilities`.

The focused route regression added both authorized capabilities while retaining
the existing assertion that `secret.capability` is excluded. Before the source
fix, this command failed for exactly the two missing response capabilities:

```text
go test ./backend/internal/httpapi \
  -run TestCurrentPrincipalReturnsSafeUICapabilitiesForAuthorizedActions \
  -count=1
```

The minimal source fix added only `work_record.assign` and
`work_record.create` to `uiActionCapabilities`. The focused regression then
passed, as did:

```text
go test ./backend/internal/httpapi -count=1
go test ./backend/...
gofmt -l backend
go vet ./backend/...
go build ./backend/...
git diff --check
```

This is local source and test evidence only. It does not claim Git publication,
deployment, live build identity, fresh assets, or authenticated browser
acceptance of the hotfix revision.
