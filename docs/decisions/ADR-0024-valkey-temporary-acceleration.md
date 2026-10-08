# ADR-0024: Valkey for temporary acceleration

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

Rarity V1 uses Valkey for temporary cache, distributed locks, rate limits, presence, notification fan-out, and worker signaling. PostgreSQL remains the durable record for tickets, events, automation runs, and all business work.

## Deployment tiers

Development, testing, and demos use one Valkey container. Production uses one Valkey primary, one replica, and three Sentinel voters distributed across the production failure domains. Sharding/Valkey Cluster is not required in V1.

## Consequences

If Valkey is lost, caches rebuild, locks expire, rate limits reset conservatively, and workers fall back to PostgreSQL polling. No customer work is lost. Valkey is not a durable queue of record.
