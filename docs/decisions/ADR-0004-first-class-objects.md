# ADR-0004: First-class objects

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

Important entities share an object contract: immutable identity, scope, permissions, relationships, versions, audit, APIs, events, search policy, metadata, and recoverable lifecycle.

## Consequences

Modules remain coherent and extensible. Exceptions require architectural justification. Custom fields supplement but cannot replace core invariants.
