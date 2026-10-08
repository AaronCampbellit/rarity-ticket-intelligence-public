# ADR-0003: Event-driven architecture

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

Meaningful platform actions emit versioned events through a durable outbox/event pipeline.

## Consequences

Automation, email, webhooks, search, reporting, metrics, and AI can evolve independently. Delivery is at least once, so consumers are idempotent. Schemas, replay, causation, dead letters, retention, and sensitive-data minimization are foundational work.
