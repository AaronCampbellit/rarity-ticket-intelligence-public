# ADR-0019: V1 decision worksheet defaults

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

The project owner approved the recommended defaults for all V1 Technician Platform worksheet items except INF-05 (object-storage support matrix) and API-02 (technician personal access tokens).

## Consequences

The approved defaults become the Phase 0 implementation baseline for infrastructure, security, intake/API, Datto RMM, technician experience, and delivery governance. INF-05 remains open because Docker on Ubuntu selects the application host, not the attachment/backup object-storage provider. API-02 remains open pending a decision on whether technician scripts need personal API credentials at all.

The interactive worksheet records these defaults as project-approved and exports them accordingly. Durable choices made while resolving the two exceptions will receive follow-up ADRs.
