# Workflow Engine

**Status:** Draft

Multiple workflows may govern the same work-record type. Selection evaluates record type, client, contract, location, department, team, queue, catalog item, priority, impact, source, fields, tags, and business hours.

Eligible workflows have priority, conditions, effective dates, enabled state, immutable published version, and fallback designation. The highest-priority match wins; ties are rejected or resolved by an explicit stable order. Every record shows the selected workflow, matched rules, rejected higher-priority rules, version, and evaluation time.

Workflows define states, transitions, permissions, required fields, approvals, SLA behavior, assignment, entry/exit actions, notifications, client-visible mapping, reopen behavior, and closure requirements. Existing records normally remain on their starting version. Explicit migration requires preview, validation, audit, and rollback where feasible.

Inheritance is one clear parent chain with constrained overrides, not unrestricted mixins.

## Classification guards

Published transitions may require one or more effective tags or require at
least one meaningful effective tag. The engine evaluates direct and derived
Task inheritance against the current catalog before committing the transition.
Terminal transitions cannot bypass the global rule that every active supported
object has an effective tag, and they fail with the stable
`classification_required` error before state, audit, or outbox facts change.
Workflow requirements supplement the MSP-global taxonomy; they never create a
workflow-local or Client-local tag namespace.
