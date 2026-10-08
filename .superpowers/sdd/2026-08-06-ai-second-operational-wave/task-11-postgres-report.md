# Task 11 PostgreSQL concurrency and atomicity report

Date: 2026-08-07

## Scope and environment boundary

- The proof started from commit `08db32d`.
- PostgreSQL ran in one-off containers on an isolated internal Docker network
  with no published ports and temporary storage. The test source bundles,
  containers, networks, and databases used Task 11-specific names.
- `TEST_DATABASE_URL` was constructed only inside each isolated test process.
  No credential or connection URL is recorded here.
- No RTI, HankServerside, or RTM Compose service, database, named volume, or
  live source checkout was modified or restarted.
- Scratch databases:
  - `rti_ai_second_wave_20260807_b7c4e2`
  - `rti_ai_second_wave_20260807_c8d5f3`
  - `rti_ai_second_wave_20260807_d3a91f`
- All 77 project migration files in the `000001` through `000081` range were
  applied. Goose reported schema version 81.

## Environment gate

Command:

```text
env -u TEST_DATABASE_URL go test ./backend/internal/store/psa \
  -run TestAISecondWavePostgres -count=1 -v
```

Outcome: PASS with the explicit skip:

```text
TEST_DATABASE_URL is required for AI second-wave PostgreSQL concurrency and rollback proof
```

The test has no fallback database.

## Deterministic RED evidence

The final integration test was overlaid on the unmodified `08db32d` source and
run against the isolated PostgreSQL database. Each race test waits for a
specific PostgreSQL blocking edge through `pg_stat_activity` and
`pg_blocking_pids`; elapsed time alone is not accepted as proof.

The baseline exposed these transaction gaps:

- Ticket create finished before the expected Service and Contract row-lock
  barriers.
- Ticket create finished before the expected routing, workflow, and SLA
  configuration row-lock barriers.
- Same-version ticket assignment never produced the required target
  `work_records` blocking edge; it blocked only at the later update.
- Opportunity transition and activity never produced the required
  target-first `opportunities` blocking edge.
- Proposal create finished before the expected Opportunity row-lock barrier.

Representative baseline failures were:

```text
mutation finished before PostgreSQL barrier "FROM services" was observed: <nil>
mutation finished before PostgreSQL barrier "FROM contracts" was observed: <nil>
mutation finished before PostgreSQL barrier "FROM routing_rule_sets" was observed: <nil>
mutation finished before PostgreSQL barrier "FROM workflows" was observed: <nil>
mutation finished before PostgreSQL barrier "FROM sla_policies" was observed: <nil>
observe PostgreSQL blocking edge: timeout: context deadline exceeded
```

## Transaction corrections

- Ticket create now locks the active Client first, then its Service, Contract,
  and queue dependencies, followed by the accepted routing, workflow, SLA, and
  calendar version rows before inserting the domain row.
- Ticket assignment now locks the active Client and exact ticket version before
  locking the technician and role-assignment scope. Multiple eligible role
  assignments use a deterministic row order.
- Opportunity transition and activity now lock the active Client and exact
  Opportunity version before pipeline and stage configuration.
- Proposal create now locks the active Client and exact Opportunity scope and
  version before inserting.
- Knowledge publication required no production change; the PostgreSQL proof
  confirmed its existing Client/revision lock order.
- Each operation remains one transaction containing the domain mutation,
  immutable audit record, and outbox event.

## GREEN and rollback evidence

Isolated PostgreSQL command:

```text
TEST_DATABASE_URL=<isolated-scratch-url> go test \
  ./backend/internal/store/psa -run TestAISecondWavePostgres -count=1 -v
```

Outcome on the final source:

- PASS on all scratch databases.
- 29 leaf cases passed: 17 deterministic concurrency cases and 12 failure
  rollback cases.
- Concurrency coverage includes every dependency listed in the Task 11 brief.
- Audit failure and outbox failure each rolled back the domain row for ticket
  create, ticket assignment, Opportunity transition, Opportunity activity,
  proposal create, and knowledge publish.

Additional verification:

```text
TEST_DATABASE_URL=<isolated-scratch-url> go test \
  ./backend/internal/store/psa -count=1
go test ./backend/internal/store/psa -count=1
go test ./backend/... -count=1
```

Outcomes: the isolated full PSA package, local PSA package, and full local
backend suite all passed.

## Review-fix evidence

A focused review identified two proof-contract gaps after the initial Task 11
commit.

First, the Proposal lock combined active scope and expected version in one
`WHERE` clause, so Opportunity version drift returned `scope.ErrNotFound`.
The focused unit RED was:

```text
CreateProposalAtomic() error=object not found
```

The lock now reads the active, exactly scoped Opportunity version under
`FOR SHARE`, returns `scope.ErrNotFound` only when that row is absent, and
compares the returned version explicitly with `object.RequireVersion`.
The focused unit tests and the real PostgreSQL competing-version case now
return `object.ErrVersionConflict`.

Second, each rollback case now requires a `*pgconn.PgError` with SQLSTATE
`23514`, the exact table, and the exact intended constraint before checking
domain/audit/outbox residue. The fresh isolated run on
`rti_ai_second_wave_20260807_d3a91f` observed all 12 expected errors:

```text
audit_ledger|audit_ledger_subject_version_check|CHECK ((subject_version > 0))
event_outbox|event_outbox_subject_version_check|CHECK ((subject_version > 0))
```

The names and definitions above were independently read from `pg_constraint`
after migrations. The complete 29-leaf PostgreSQL suite passed in 4.19
seconds, with `fixture_residue=0` and `other_connections=0`.

## Residue and cleanup

- The fixture cleanup query returned
  `fixture_residue=0` after the final PostgreSQL run.
- The final connection query returned `other_connections=0`.
- Each scratch database was dropped by exact name and verified absent before
  its temporary PostgreSQL container was removed.
- All Task 11 source bundles, PostgreSQL containers, and internal networks
  were verified absent.
- Shared-host inventory before and after the proof matched exactly:
  - RTI: 6 containers total, 6 running, 2 volumes.
  - HankServerside: 7 containers total, 6 running, 14 volumes.
  - RTM: 4 containers total, 4 running, 1 volume.
  - Host total: 20 containers, 17 running, 94 volumes.

This is source and isolated-database verification only. It is not a deployment
or authenticated live acceptance claim.
