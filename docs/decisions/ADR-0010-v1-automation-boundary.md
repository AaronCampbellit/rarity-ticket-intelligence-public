# ADR-0010: V1 automation boundary

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

V1 automation may mutate Rarity through supported application services and call secured external HTTP APIs. It may not run arbitrary host or endpoint code, access databases directly, or read local files.

## Consequences

Connections hold encrypted credentials. Typed actions, permissions, retries, idempotency, loop prevention, run history, and dead-letter handling are required. Endpoint execution would be a separately secured future subsystem.
