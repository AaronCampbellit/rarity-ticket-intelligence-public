# Contracts and SLAs

**Status:** SLA configuration and timer runtime implemented; environment acceptance active

Contracts connect clients to covered services, assets, hours, billing references, response/resolution objectives, escalation policy, and effective dates. V1 applies contract policy automatically, calculates included/billable time, and exports approved billing data as CSV only; invoicing, payments, and direct accounting connectors are deferred.

SLA selection is explainable and versioned. The runnable API publishes immutable IANA-timezone business calendars and ordered SLA policy versions, validates one global fallback, and binds each new Work Record to its exact policy/calendar versions with the full selection trace and calculated response/resolution warning and deadline timestamps. Service and Contract context is validated inside the authenticated Client before selection. Historical records retain the versions actually applied.

The first client-visible reply records response attainment atomically; internal notes are SLA-neutral. Governed workflow transitions pause/resume timers using business time from the bound calendar version and record resolved, cancelled, or reopened outcomes from validated workflow-state SLA behavior. A bounded 30-second process evaluator versions unpaused timers at response/resolution warning and breach thresholds and emits dedicated audit/outbox facts with optimistic conflict handling. Late response or resolution remains breached rather than being relabeled met.

V1 priority changes deliberately retain the bound SLA and write explicit correlated retention evidence; they never silently reset a target. Separately authorized administrators can override unfinished response or resolution deadlines only with dual optimistic versions and a reason. Warning timestamps shift by the same delta, completed targets remain immutable, and every override preserves an immutable before/after evidence row.

Live PostgreSQL migration and evaluator-worker behavior remains an environment acceptance gate.
