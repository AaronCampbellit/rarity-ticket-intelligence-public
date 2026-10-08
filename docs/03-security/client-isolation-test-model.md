# Client-Isolation Test Model

**Status:** Detailed draft\
**Version:** 0.1\
**Last updated:** 2026-07-25

## Goal

Prove that no user, integration, automation, background job, query, cache, export, search index, notification, or AI operation can leak one client’s data into another client’s context.

## Standard fixture

Every isolation suite creates:

- one MSP with Platform Administrator and constrained technician identities;
- Client Alpha and Client Bravo with similar names and deliberately colliding human-readable values;
- locations, contacts, work records, comments, internal notes, attachments, assets, services, graph edges, contracts, and custom fields for each client;
- one MSP-global queue and one client-scoped queue per client;
- scoped API and automation principals for Alpha only;
- deleted, archived, and recently transferred/permission-changed objects.

UUIDs are distinct, while ticket subjects, filenames, IP addresses, external IDs, and search terms intentionally overlap.

## Mandatory attack matrix

For every client-bound entity and operation, tests attempt:

1. direct lookup by Bravo UUID using an Alpha-scoped principal;
2. list/filter/sort/pagination queries designed to cross scope;
3. create/update payloads that reference Bravo parents or relationships;
4. guessed display IDs and external IDs;
5. batch and bulk actions containing mixed-client objects;
6. search, autocomplete, counts, facets, reports, dashboards, and exports;
7. attachment upload, download, thumbnail, preview, and short-lived URL reuse;
8. event subscriptions, webhook payloads, retries, and dead letters;
9. automation triggers, waits, retries, connection scope, and replay;
10. cache hits after membership removal or object transfer;
11. background jobs, reindexing, retention, restore, and migration paths;
12. AI retrieval, prompt context, embeddings, recommendations, logs, and evaluation datasets.

The expected result is denial or omission without confirming inaccessible object existence.

## Database tests

- Every client-bound table has required scope and validated parent consistency.
- Unique constraints include scope where human identifiers may repeat.
- Repository/query helpers require trusted scope rather than accepting an optional filter.
- Raw administrative queries are isolated to migration/operations tooling and cannot serve application traffic.
- Transaction tests attempt cross-client relationship creation and mixed-scope updates.

Row-level security may provide an additional layer if adopted, but application authorization and schema invariants remain mandatory.

## Derived-system tests

Search indexes, caches, metrics labels, report stores, event payloads, and AI stores are tested independently because transactional isolation does not automatically secure derived systems. Permission revocation and client deletion tests include maximum acceptable propagation delay and deny access during uncertainty.

## Browser and API tests

The suite tests URL changes, hidden fields, stale tabs, copied deep links, optimistic updates, browser history, downloads, GraphQL if introduced, API pagination cursors, error timing, and enumeration-resistant errors.

## Operational tests

Backup restore, point-in-time recovery, support bundles, diagnostics, logging, tracing, and audit export are inspected for client content and access policy. Support tooling requires explicit audited scope.

## Release gate

Any cross-client disclosure is a release blocker and security incident. Isolation tests run in CI and against production-like deployments before a release. New first-class entity types cannot merge without joining the fixture and attack matrix.

## Mention isolation extension

The fixture creates identical internal details, comments, notes, display IDs,
technician names, and team names under Alpha and Bravo. It then verifies that:

- candidate search and exact team confirmation cannot enumerate another MSP or
  an unauthorized Client;
- forged Client, parent, target, team snapshot, item, occurrence, cursor, and
  optimistic-version values fail without object disclosure;
- public and client-visible mutation paths reject structured tokens;
- widget counts, previews, and exact-source links are recipient-owned and
  reauthorize against current Client/project/object visibility;
- role removal, Client access removal, project visibility loss, technician
  disablement, transfer, deletion, and redaction suppress affected items and
  pending delivery;
- team membership changes enqueue recomputation but never grant access;
- revoke-before-mention and mention-before-revoke commit order both create a
  durable decision for the exact latest occurrence; access restoration before
  worker execution does not resurrect it, while a newer valid mention produces
  a new unread version;
- occurrence storage retains `token_id` but no token JSON, label, or offsets;
  preview and deep-link offsets come from the current authorized source; and
- occurrence events, invalidation records, logs, metrics, email, and Teams
  payloads contain identifiers and safe outcomes only—never body, preview,
  label, or token-adjacent text.

## Calendar isolation

Calendar PostgreSQL tests exercise full events, Busy workforce visibility, opaque
pagination, live changes and saved lens scope. Dependency query tests require both
endpoints to be readable; hidden source membership must not appear through edges.
Custom date definition reads check the source's current persisted grants, including
client restrictions, before returning active definitions. Busy event tests reject
projection IDs, occurrence keys, revisions, recurrence, health reasons and tags.
Browser tests inject malicious full-source fields into Busy payloads and verify
that the UI exposes no source details or scheduling action.

See the [calendar contract](../02-platform/unified-calendar.md) and
[current verification record](../09-roadmap/current-execution.md). This coverage
does not substitute for retained provider, deployment, or release evidence.
