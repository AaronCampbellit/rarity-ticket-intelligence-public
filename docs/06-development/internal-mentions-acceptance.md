# Internal Mentions Acceptance Contract

**Status:** Required release gate
**Version:** 1.0
**Last updated:** 2026-08-08

## Supported matrix

The complete parent/source cross-product is supported for internal content:

| Parent | Details | Comment | Note |
|---|:---:|:---:|:---:|
| `work_record` | required | required | required |
| `task` | required | required | required |
| `project` | required | required | required |

Every mutation persists body and structured tokens atomically. Public and
customer-visible endpoints have no token DTO and reject forged tokens. Plain
historical `@name` text remains inert.

## Stable release assertions

- Targets are `staff` and existing organizational `team` only. AI targets,
  `@AI` commands, mention intents, mention-specific groups, and public tokens
  are unsupported.
- Direct recipients must be eligible. Teams snapshot current active eligible
  members; partial teams warn and require exact current confirmation.
- One item exists per MSP/recipient/parent. Re-mention replaces its latest
  occurrence, clears suppression, and returns read or archived state to unread.
- The Dashboard widget is the only in-app mention delivery. There is no full
  inbox and no generic notification duplicate.
- The authenticated resolve endpoint returns the authorized Client plus an
  exact parent/source/token hash link and marks read with optimistic versioning.
  Edited, removed, redacted, publicized, and inaccessible sources fail safely.
- Mentions never alter assignment, participant, watcher, task, approval, or
  permission state.
- Current authorization is checked at widget, preview, state, deep-link,
  planning, and delivery boundaries. Authorization mutations and mention
  creation serialize on one MSP revision row. A loss event writes one compact,
  content-free marker at that revision; it never scans mention items in the
  event transaction. Reads fail closed while an applicable marker is pending.
  The worker pages both markers and items, evaluates role access from temporal
  assignment/capability history at the exact boundary, and persists only
  confirmed occurrence-specific losses. A later regrant cannot erase a loss;
  only a newer authorized mention can supersede it.
- Expired role assignments are not evaluated from the worker's wall clock
  alone. The worker first opens a new MSP authorization revision, then removes
  a bounded set of expired assignments and emits immutable `role.unassigned`
  evidence in that transaction. The revision's recorded timestamp is the
  boundary used for temporal access checks.
- Every producer that can change mention authorization locks the MSP revision
  before role, capability, parent, or source rows. This common lock order also
  applies to work-record merges and prevents authorization/mention deadlocks.
- Email/Teams planning honors policy, recipient preferences, availability,
  quiet periods, idempotency, retries, and delivery-time authorization.
- Occurrence rows store only opaque identifiers, including `token_id`; full
  token JSON, rendered labels, and offsets remain in the current source and
  are resolved there at read time.
- Mention mutation metrics are published only after the source transaction
  commits. Concurrent notification planning has one atomic owner, so replay
  and losing planners do not double-count outcomes.
- Occurrences, resolutions, items, outbox events, invalidations, logs, metrics,
  and external payloads contain no source body, preview, rendered label, token
  offsets, or token-adjacent text.

## Required automated gates

Run all mention/collaboration/organization/notification/HTTP/store/migration
tests, the complete Go suite, frontend tests, the production frontend build,
this documentation validator under Node 22, and `git diff --check`. PostgreSQL
migration and repository tests run against an isolated schema on PostgreSQL 16.
