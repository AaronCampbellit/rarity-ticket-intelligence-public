# Cache and Queue

**Status:** Accepted V1 direction

Valkey is the V1 cache/queue technology. It supports ephemeral caching, rate limits, locks, presence, notification fan-out, and worker signaling. It is never the sole durable record of a business event, ticket, or automation run; durable business state remains in PostgreSQL.

Development/testing uses one Valkey container. Production uses a primary, replica, and three Sentinel voters across production failure domains; V1 does not use sharded Valkey Cluster. The operating contract defines HA, eviction safety, client isolation, encryption, authentication, backlog visibility, and PostgreSQL fallback/rebuild behavior.
