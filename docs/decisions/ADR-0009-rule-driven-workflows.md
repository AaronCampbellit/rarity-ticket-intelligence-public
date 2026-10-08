# ADR-0009: Rule-driven workflow selection

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

Multiple workflows can apply to a record type. Deterministic priority and matching rules select one workflow, with a required default fallback.

## Consequences

Selection is explainable. Published versions are immutable; active records normally retain their starting version. Inheritance is limited to a clear parent chain, and migrations require preview, validation, audit, and rollback planning.
