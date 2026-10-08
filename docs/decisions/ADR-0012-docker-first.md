# ADR-0012: Docker Compose before Kubernetes

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

Docker Compose is the first supported deployment experience. Kubernetes support follows after operating contracts are proven.

## Consequences

V1 remains accessible to self-hosted MSPs while keeping services container-portable. Compose production guidance must clearly distinguish HA from single-node development. No application design may depend on one container host.
