# Contributing

Rarity is in active implementation and release qualification. Start with the [current execution backlog](docs/09-roadmap/current-execution.md) and [local development guide](docs/06-development/local-development.md). Preserve accepted architecture decisions and make changes explicit.

## Before proposing a change

1. Read the [Architecture Manifesto](docs/00-foundation/02-architecture-manifesto.md) and [Things We Will Never Do](docs/00-foundation/05-things-we-will-never-do.md).
2. Identify the owning document and existing ADRs.
3. Describe the problem, affected tenants and objects, security boundary, API/event impact, recovery behavior, observability, and compatibility.
4. Add or update an ADR for a durable architectural decision.

## Definition of done

A feature is not complete unless it is secure, permission-aware, auditable, recoverable, observable, testable, accessible through supported APIs, and documented. UI-only business behavior is not accepted.

## Changes

Keep changes focused. Add tests proportionate to risk. Never commit credentials, customer data, production payloads, or secrets. Breaking API or event changes require explicit versioning and migration guidance.
