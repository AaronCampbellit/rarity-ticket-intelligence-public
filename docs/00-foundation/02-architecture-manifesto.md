# Architecture Manifesto

**Status:** Foundational draft\
**Version:** 1.0\
**Last updated:** 2026-07-25

> Build a platform that outlives today’s technology decisions.

## Constitution

1. **Platform before product.** Ticketing is the first module on a shared MSP operations platform.
2. **API first.** Every supported UI capability is available through documented application APIs; business logic never exists only in the browser.
3. **Event driven.** Meaningful actions produce versioned events. Subsystems use durable event contracts to reduce coupling and support replay.
4. **First-class objects.** Important entities have immutable identity, permissions, relationships, history, APIs, events, search, metadata, and recoverable deletion.
5. **Security is foundational.** Zero implicit trust, least privilege, secure defaults, strong identity, encrypted secrets, scoped APIs, and review are part of design.
6. **Client isolation is mandatory.** API, search, reporting, jobs, automation, AI, caching, and storage must preserve client boundaries.
7. **Workflows are configuration.** Business processes are versioned rules and state machines, not client-specific code.
8. **Automation is a platform capability.** New features expose safe triggers, conditions, and typed actions.
9. **Decisions are explainable.** Routing, workflow selection, automation, correlation, and AI recommendations show their inputs and reasons.
10. **AI assists; humans retain authority.** AI may summarize, classify, draft, search, and recommend. Material changes require policy-controlled approval and are never silent.
11. **Reliability outranks feature count.** Degraded behavior is bounded, visible, and recoverable.
12. **Every material change is auditable.** Actor, time, source, before/after state, reason where available, correlation, and causation are retained.
13. **Recovery is a product feature.** Object recovery, backups, snapshots, and restoration are designed and routinely tested.
14. **Design for scale.** Millions of records and assets, thousands of technicians, large attachments, high API throughput, and continuous integration traffic must not require a fundamental redesign.
15. **Performance is visible.** Latency budgets and capacity limits are defined and measured.
16. **Observability everywhere.** Services, workers, queues, integrations, backups, and data stores expose health, metrics, logs, traces, and actionable events.
17. **Consistency wins.** Terminology, navigation, permissions, APIs, auditing, and interaction patterns remain coherent across modules.
18. **Extensible by design.** Providers and integrations use explicit contracts and can evolve without rewriting the platform.
19. **Documentation is part of the product.** Architecture, schemas, APIs, events, operations, and decisions are incomplete until documented.
20. **Do not choose known throwaway architecture.** Experiments are labeled and isolated; foundations are built for their stated lifecycle.
21. **Enterprise by default.** HA, security, audit, backups, API governance, dependency awareness, and operability are core capabilities, not premium add-ons.
22. **Progressive complexity.** New technicians see a focused workspace; capabilities appear according to role and responsibility without limiting administrators.

## Engineering test

Every feature must answer: Is it secure, isolated, permission-aware, API-accessible, event-producing, auditable, observable, recoverable, testable, accessible, and consistent? A “no” requires redesign or a recorded exception.

## Final principle

We are building the platform we would choose to run an MSP on for the next twenty years.
