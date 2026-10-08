# ADR-0022: Go, React, and SQL-first application stack

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

Rarity V1 uses a modular-monolith architecture: Go backend services; React and TypeScript frontend built with Vite; PostgreSQL accessed through `pgx` and generated typed `sqlc` queries; Goose versioned SQL migrations; Go unit/integration tests and Playwright browser tests.

## Consequences

The project does not introduce microservices, Kubernetes-first deployment, GraphQL, a heavy ORM, or multiple backend languages in V1. Docker Compose on Ubuntu remains the local, demo, acceptance, and initial single-node deployment path.
