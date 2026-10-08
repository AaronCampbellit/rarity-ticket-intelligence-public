# Task 6 Report: Durable AI Generation Jobs

Status: complete

Evidence:

- Added server-scoped durable submission, idempotency replay, lease claiming,
  context reload, cancellation fencing, provider credential callbacks, and
  atomic recommendation, usage, audit, and outbox completion.
- Recommendation validation is provider-neutral and rejects invalid output or
  unauthorized candidates while retaining unknown metering as SQL NULL.
- `go test -race ./backend/internal/aiassist ./backend/internal/store/psa -count=1` passed.
- `go test ./backend/... -run '^$' -count=1` passed.
- `git diff --check` passed.

Commit: `4d13c4e712466a2ecace909223129a917c566a23` (`feat: run durable AI generation jobs`)

## Review fix round 1

Status: complete

- Added lease ownership/cancellation maintenance with a bounded sequential worker loop and child provider context cancellation.
- Rechecks active requester role capability, policy/model/connection mapping, context bounds, and cost policy before disclosure.
- Added lease heartbeat/version and retry scheduling fields to migration 49; retriable failures requeue with a bounded delay.
- Re-ran race tests and backend compile sweep successfully.

Commit: `4eece060a9386e81849fe42d3154be6275fe6cbc` (`fix: harden AI job leases and governance`)

## Review fix round 2

Status: complete

- Wired policy cost configuration and current-month known spend into execution authorization, with a final completion-time cost guard.
- Enforced conservative request-context budgeting plus provider-owned output ceilings for Ollama and OpenAI-compatible adapters.
- Verified migration contracts, focused race tests, and the backend compile sweep.

Commit: `d577ef57353659859b708c262503cb7dc17ed05b` (`fix: enforce AI job cost and output limits`)

## Review fix round 3

Status: partial

- Corrected monthly spend SQL to use UTC-safe timestamptz boundaries.
- Required active work-record and client lifecycle state during execution reauthorization.
- Removed the redundant cancellation CASE and duplicate completion lease predicate.
- Focused aiassist/PSA tests and diff check passed; full reservation, stranded sweep, and transition-evidence contracts remain outstanding.

Commit: `2e93aa14a66938f9ac093e9b7723b96a3fc13c55` (`fix: tighten AI job authorization SQL`)

## Continuation: governance and execution-boundary hardening

Status: partial — core enforcement is committed and all requested local gates
pass. No live PostgreSQL/Docker validation was run. The remaining transition
fact coverage noted below is product work, not an environment blocker.

- A — implemented: migration 49 has nullable non-negative per-million model
  prices, zero-cost coherence, and durable job reservations. Final execution
  authorization takes an MSP-scoped PostgreSQL advisory transaction lock,
  reloads current authorization/lifecycle/policy/model/connection state,
  computes UTC-month known spend plus active reservations, and persists the
  conservative ceiling reservation before credential access. Completion holds
  the same lock and clears the reservation with the one usage insert; failures
  clear reservations. Covered by domain, SQL-shape, and focused race tests.
- B — implemented: `SweepStranded` runs before every claim and terminalizes
  expired cancellation/max-attempt jobs, clears lease and reservation, bumps
  version, and emits system audit/outbox facts in its transaction.
- C — implemented: worker re-enters `AuthorizeAndReserve` immediately before
  credentials/provider I/O; a rejection prevents credential callback use.
- D — implemented: body-safe HTTP status errors make 401/403/other 4xx
  terminal, 429/5xx retryable, and only temporary transport failures retryable.
- E — partial: cancel/retry now require reason and expected version and write
  transactional facts. Completion and sweep are evidenced. Claim and generic
  fail/requeue transitions still need their own durable fact IDs/writes.
- F — implemented: worker uses the exact shared marshalled fixed-adapter body
  for encoded preflight; both adapters send those same bytes. Escaped control
  and quote expansion is covered before transport.
- G — implemented: blocking adapter test proves periodic maintenance cancels
  the provider context and prevents completion; heartbeat maintenance remains
  audit-free by design because it does not represent a business transition.
- H — implemented for the currently redundant cancellation/lease SQL paths;
  query predicates are consolidated and checked by the focused repository
  tests.

Commits: `7d26b7d`, `fc6aff8`, `c1793a1`, `cf1314d`, `3d287e7`.

Verification:

- `go test ./backend/migrations -count=1` — pass
- `go test -race ./backend/internal/aiassist ./backend/internal/store/psa -count=1` — pass
- `go test ./backend/... -run '^$' -count=1` — pass
- `git diff --check` — pass

## Continuation: transition evidence closure

Status: complete

- E — complete: worker-supplied unique audit/event/correlation IDs now make
  claim and generic fail transitions transactional with their audit and outbox
  facts. Claim records `ai.generation_job.claimed`; retryable failures record
  `ai.generation_job.requeued`; terminal failures and cancellations record
  their terminal action. Facts retain only safe reason codes and attribute the
  worker action to the job's submitting technician rather than inventing a
  system principal.
- Lease maintenance remains audit-free and no longer increments the business
  version because it renews lease metadata only. Stranded-sweep evidence was
  checked to retain one atomic audit/outbox pair per transition.
- Focused rollback, ordering, ID propagation, action/reason, and version
  assertions passed, together with the requested race and backend compile
  gates. No schema changed.

Implementation commit: `15214ad2ba03a8b3b3d4f5ec9d0d3955db7cab60`

## Final review fixes

Status: complete (local validation only; no live PostgreSQL required)

- Submission now requires all generated fact IDs and persists the queued job,
  audit record, and outbox event atomically; idempotent replay emits no second
  fact pair. Focused tests cover ordering, supplied IDs, and rollback.
- Every queue-owned execution boundary is lease-expiry fenced. Heartbeats cannot
  revive expired/cancelled work; credential opening is additionally bound to
  current job ownership, MSP, connection ID, and connection version.
- The final authorization and completion SQL require the currently allowed
  feature and its feature-selected model profile. Completion classifies
  cancellation and stale ownership separately from an actual cost rejection.
- Provider I/O rebuilds its request from the final reserved execution snapshot;
  changed request size/context is requeued without provider I/O. Credential
  rotation or connection-version changes fail closed before transport.
- Policy repository reads/writes `monthly_cost_limit_minor`; retry clears
  `completed_at`; monthly spend uses UTC lower and next-month upper bounds;
  unknown actual cost under a finite cap is a cost-limit rejection.

Verification:

- `go test -race ./internal/aiassist ./internal/store/psa -count=1` — pass
- `go test ./migrations -count=1` — pass
- `go test ./... -run '^$' -count=1` — pass
- `git diff --check` — pass

## Concurrency and execution-fence closure

Status: complete locally; live PostgreSQL execution remains opt-in because
`TEST_DATABASE_URL` is not configured.

- Concurrent idempotent submission now uses one `ON CONFLICT ... DO UPDATE`
  statement that returns the canonical job with an inserted flag. Submission
  audit/outbox facts are written only for the new row; replay does not alter
  logical version or timestamps and emits no duplicate facts. An opt-in
  PostgreSQL concurrent-insert contract proves one canonical row and one
  inserter.
- Claims, stranded sweeping, and heartbeats use PostgreSQL `now()` and accept
  only a lease duration from the worker. Leases cannot be reclaimed, revived,
  or shortened by a worker clock that is ahead or behind database time.
- A SHA-256 execution fingerprint binds the exact marshalled request bytes to
  trusted policy/model/connection/work/candidate snapshot data. Reservation
  persists that fingerprint and snapshot versions, rechecks them transactionally,
  and a final repository fence rechecks them immediately before provider I/O.
  Same-length context and post-reservation model mutations fail closed without
  provider I/O.
- Every call, including credentialless Ollama, now uses the fenced execution
  callback. The callback validates current MSP/connection/version/enabled/base
  URL/mode and reserved job snapshot; a rotated endpoint cannot reach transport.

Verification:

- `go test ./internal/aiassist ./internal/store/psa ./migrations -count=1` — pass
- `go test -race ./internal/aiassist ./internal/store/psa -count=1` — pass
- `go test ./... -run '^$' -count=1` — pass
- `go test ./migrations -run TestProviderAgnosticAIRuntimeMigrationUpDownContract -v` — skipped only because `TEST_DATABASE_URL` is absent
- `git diff --check` — pass

## Final follow-up: lease precision and provider mutation serialization

Status: complete locally; the PostgreSQL contention contract remains opt-in
because `TEST_DATABASE_URL` is not configured.

- All PSA lease writes now pass ceiling-rounded microseconds to PostgreSQL,
  rather than truncating fractional seconds. Claim and heartbeat coverage
  includes 1500ms and sub-second leases; positive sub-microsecond values round
  up, never down.
- Discovered model reconciliation advances the model version only when trusted
  executable discovery fields (`display_name` or `context_limit`) change.
  Repeated identical discovery remains a logical no-op; the existing final
  model-version fence therefore rejects a changed context limit before I/O.
- Execution credentials now acquire a pool-pinned session shared advisory lock
  keyed by MSP and connection ID. The final fence, credential opening, callback,
  secret wipe, and cleanup remain within that lock without holding a database
  transaction across provider I/O. Connection create/update/enable/credential
  replacement and discovery acquire the matching exclusive transaction lock.
  Panic cleanup, credentialless execution ordering, and optional PostgreSQL
  blocking behavior are covered.

Verification:

- `go test ./internal/store/psa -count=1` — pass
- `go test -race ./internal/aiassist ./internal/store/psa -count=1` — pass
- `go test ./... -run '^$'` — pass
- `git diff --check` — pass
