# Task 11 report: isolated PostgreSQL concurrency acceptance

## Status

Complete. Environment-gated real PostgreSQL coverage now proves:

- resource creation waits for a concurrent Client deactivation and rejects the
  now-inactive Client without resource, audit, or outbox facts;
- two same-version Location updates produce exactly one success and one typed
  version conflict, with one committed audit/outbox pair;
- Location deactivation first blocks behind an intentionally held dependent
  Contact/Asset row while retaining its Location lock, then the real dependent
  mutation blocks behind that Location lock; after release, the Location stays
  active and only the dependent update commits;
- a resource insert and its audit fact roll back when the outbox insert fails;
- the existing ordinary-create versus Prospect-conversion Client identity race
  runs in the same filtered database-backed suite.

Every generated Task 11 fixture registers a bounded, MSP-ID-scoped cleanup.
The cleanup validates the UUID scope, removes event/audit facts before the
five Client resource tables, then removes the Client and MSP. It temporarily
disables only the append-only audit trigger inside the cleanup transaction and
restores it before commit. A post-suite SQL check found zero matching MSP,
Client, resource, audit, or outbox rows.

No production code changed. The acceptance tests name observable failures in
the existing production lock and transaction boundaries. A test-only mutation
that removed the Location deactivation row lock failed at the exact second
blocker-edge assertion: PostgreSQL still showed deactivation waiting on the
Contact row, but the Contact update was no longer blocked by the deactivation
PID's Location lock. The mutation was never committed or applied to the demo
checkout.

Source test commit:

```text
51aa77b test: add PostgreSQL client resource concurrency acceptance
74fa546 test: prove client resource lock serialization
```

## Portable verification

With `TEST_DATABASE_URL` absent:

```text
go test ./backend/internal/clientresources ./backend/internal/store/psa \
  ./backend/internal/organizations -run 'Postgres|Concurrency|Serialize' \
  -count=1 -v

PASS
TestPostgresClientResourceConcurrencyAcceptance:
  SKIP TEST_DATABASE_URL is required for PostgreSQL concurrency acceptance
TestPostgresRepositoryRollsBackAllRecordsWhenOutboxInsertFails:
  SKIP TEST_DATABASE_URL is required for PostgreSQL transaction verification
TestOrdinaryCreationAndProspectConversionSerializeNormalizedClientIdentity:
  SKIP TEST_DATABASE_URL is required for cross-path PostgreSQL concurrency verification
```

The full `backend/internal/store/psa` package test and `go vet` for
`store/psa` plus `organizations` also passed locally.

## Isolated demo PostgreSQL proof

- Live RTI checkout and running containers were not changed or deployed.
- Commit `74fa546` was transferred into a temporary remote checkout. The
  committed integration-test file SHA-256 matched locally and
  remotely:
  `ea6e90c8d882147c65fbd49af7a7023f51fda6fade77882163bc48c7c7b182f4`.
- The exact scratch database was resolved absent, created as
  `rti_ai_catalog_test_20260805`, and was the only test database used.
- Connection parameters came from the running PostgreSQL container without
  printing credentials. The test process connected only to the scratch
  database.
- The focused committed test applied every migration through version 81
  before the multi-package filtered command. An earlier attempt that launched
  package migrations concurrently against a completely empty database hit a
  Goose metadata bootstrap race; its cleanup trap dropped the scratch
  database, and the ordered migration-then-test rerun passed.
- The required filtered command passed against real PostgreSQL. Observed
  database-backed passes included all five new resource subtests, the existing
  organization transaction rollback test, and
  `TestOrdinaryCreationAndProspectConversionSerializeNormalizedClientIdentity`.
  The Client resource test observed the resource writer blocked on the
  uncommitted Client deactivation. For both Contact and Asset, it observed the
  Location deactivation blocked by the held dependent row and then the
  dependent mutation blocked by the deactivation's Location lock. The identity
  test observed both distinct repository transactions waiting on the shared
  Client identity advisory lock.
- The reusable scratch schema contained zero Task 11 MSP, Client, Location,
  Contact, Asset, Service, Contract, audit, or outbox fixture rows after the
  tests.
- `dropdb --force` targeted only
  `rti_ai_catalog_test_20260805`; the exact database was verified absent
  afterward. The isolated checkout and task-local Go caches were removed.

The first remote attempt stopped before migrations because
`GOTOOLCHAIN=local` exposed the host's Go 1.24.4 launcher. Its exit trap
dropped the exact scratch database. The clean retry used automatic Go 1.26.5
toolchain resolution in the task-local cache and passed.

## Post-run safety check

```text
https://rarity.campbellservers.com/healthz
{"status":"ok"}

https://rarity.campbellservers.com/readyz
{"revision":"f29d1204bdefdd2be01681e81c3b129e4cc0a0a4","status":"ready"}

Compose projects:
hankserverside running(6)
rti running(6)
rtm running(4)

Scratch database:
rti_ai_catalog_test_20260805 exists = 0
```

## Self-review and limits

The tests use only fixed repository entry points and allowlisted test table
helpers; no generic SQL surface was added. The blocking assertion observes
PostgreSQL's actual `pg_blocking_pids` graph instead of relying on a sleep or
simultaneous release. Every writer and cleanup has a bounded context. Cleanup
is scoped to freshly generated MSP UUIDs and ordered around foreign keys.

This is isolated single-node demo PostgreSQL acceptance. It is not deployment,
production, HA, failover, backup, or disaster-recovery proof.
