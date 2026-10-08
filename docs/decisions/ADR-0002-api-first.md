# ADR-0002: API-first platform

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

Every supported browser capability is exposed through documented application APIs, and the UI uses those same domain services.

## Consequences

Mobile, CLI, automation, integrations, and future agents can share behavior. UI-only business logic and privileged hidden browser endpoints are prohibited. Compatibility, idempotency, authorization, and documentation become release gates.
