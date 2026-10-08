# ADR-0015: Docker-first production operating model

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

Rarity is Docker-first, with Docker Compose as the initial supported deployment. HA uses a primary PostgreSQL node, a synchronous failover replica, and an asynchronous replica; normal failover targets under two minutes with zero confirmed-data loss, and full-cluster DR targets under one hour with up to five minutes data loss.

## Consequences

PostgreSQL holds durable business records, events, and automation history; cache/queue layers are temporary. Encrypted snapshots and continuous WAL archiving enable PITR. Monthly restore verification, quarterly DR exercises, rolling HA upgrades, and guided single-node maintenance are release/operations requirements.
