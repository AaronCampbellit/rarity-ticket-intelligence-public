# ADR-0020: No technician personal access tokens in V1

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

V1 will not issue personal access tokens to technicians. Named integrations use scoped service API keys.

## Rationale

Normal technician work occurs through the Rarity web application. No concrete technician-script use case requires personal API credentials in V1, and omitting them reduces credential lifecycle, scope, revocation, and support risk.

## Consequences

Technician scripts cannot call the public API using personal credentials in V1. A future verified use case may introduce expiring, scoped PATs through a new ADR without changing the service-key integration model.
