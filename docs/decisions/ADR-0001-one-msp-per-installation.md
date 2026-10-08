# ADR-0001: One MSP per installation

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

One Rarity installation serves exactly one MSP and many client organizations beneath it.

## Rationale and consequences

This simplifies self-hosted isolation, ownership, licensing, branding, identity, backup, and administration while supporting large client estates. It does not remove client isolation requirements. Hosting multiple independent MSPs in one control plane would require a new architecture and ADR.
