# Unified Calendar Task 10: AI Runtime Slice

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

**Goal:** Compose calendar recommendations through the governed AI control plane while preserving a preview-only, permission-filtered boundary.

## Safety invariants

- Calendar recommendation is a closed AI feature with its own configured model profile.
- Provider input contains only repository-produced, permission-filtered context.
- Provider output is strict, bounded JSON and may reference only authorized technician IDs.
- Credentials are opened through the existing snapshot-bound credential provider.
- The provider cannot call calendar Apply; every returned candidate must pass deterministic Preview.
- Missing, disabled, unhealthy, or mismatched policy/model/connection state fails closed.
- Synchronous recommendations require a zero-cost model until this feature uses
  the usage reservation and completion-accounting runtime.
- The response contains a non-applyable preview summary; proposal identity,
  version, authorization binding, and expiry remain server-side.
- The current deterministic write contract supports time recommendations for
  the existing assignee only. Reassignment and allocation candidates fail
  closed until typed schedule mutations can preview and apply those fields.

## Implementation

1. Add `calendar_recommendation` to the feature, model, and policy contracts, including a migration for the selected model profile and database constraints.
2. Add strict provider request instructions and output parsing for bounded calendar candidates.
3. Add a synchronous governed calendar provider that loads and validates the configured policy/model/connection before provider I/O.
4. Add a PostgreSQL calendar recommendation context loader that enforces source visibility and returns bounded technician availability/workload facts.
5. Share the adapter registry/control-plane repository at runtime and compose the real recommendation service with deterministic proposal preview.
6. Run focused tests, all backend tests, frontend tests/build, docs validation, Compose rendering, and diff checks sequentially.
