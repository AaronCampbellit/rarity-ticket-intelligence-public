# High Availability

**Status:** Three-node database source topology implemented; failover acceptance pending

Production HA removes avoidable single points of failure across ingress, application/API, workers, database, cache/queue, object storage, identity dependencies, and secret access.

Services are horizontally replaceable where state permits. Readiness distinguishes “running” from “safe to receive work.” Failure domains, quorum, degraded mode, maintenance, failover, and recovery are tested—not assumed.

Rarity has two explicit deployment tiers: a single Ubuntu VM Docker Compose profile for development, testing, and demos; and a production HA profile across three separate Ubuntu VMs or preferably separate physical machines. The first profile validates product behavior, not availability. The production profile validates quorum, fencing, automatic failover, and failure-domain recovery.

The implemented database profile lives in `infrastructure/ha`. Each failure-domain node runs one TLS-authenticated etcd member, one Patroni-managed PostgreSQL member with required Linux watchdog access, and one HAProxy endpoint. Container images are supplied only as repository plus immutable digest. Host networking is intentional for stable peer identities and must be restricted by host firewalls to the three cluster nodes and authorized application callers.

Patroni uses etcd v3, strict synchronous replication with one synchronous standby, data checksums, `pg_rewind`, TLS 1.2 or newer, and continuous WAL archive invocation. HAProxy checks Patroni’s TLS `/primary` endpoint and routes write traffic only to the current primary over TLS. The profile does not claim HA until a three-failure-domain environment proves quorum loss behavior, watchdog fencing, former-primary rejoin, synchronous-replica loss write blocking, zero acknowledged-write loss, and failover under two minutes.
