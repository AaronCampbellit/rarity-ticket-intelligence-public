# Production Operating Model

**Status:** Accepted V1 direction

## Supported deployment

Rarity is Docker-first and may run wherever Docker is supported. Docker Compose is the initial supported deployment. Kubernetes is deferred until the deployment and operating contracts are proven.

## High availability

HA uses three PostgreSQL nodes: one primary, one synchronous failover replica, and one asynchronous replica. Automatic failover targets recovery in under two minutes with zero confirmed-data loss for acknowledged writes. Former primaries are fenced before rejoining. Connection routing, quorum, replication lag, and failover health are visible to operators.

## Disaster recovery and backups

Full cluster disaster recovery targets restoration within one hour with up to five minutes of data loss. Encrypted snapshots plus continuous WAL archiving are stored in a separate failure domain for point-in-time recovery. Automated restore verification runs monthly; a documented full DR exercise runs quarterly. Restore evidence, exceptions, and corrective work are retained.

## State and storage

PostgreSQL is the durable system of record for tickets, objects, events, audit, and automation history. Queue/cache infrastructure is temporary only: it may dispatch, rate-limit, lock, or cache but cannot be the sole holder of business state. MinIO is the first supported self-hosted S3-compatible object-storage option for attachments, inbound payloads, and selected backup artifacts. Single-server MinIO may share the Ubuntu host; HA object storage must be independently durable or external to that application host.

## Upgrade policy

HA deployments support rolling minimal- or zero-downtime upgrades where component/database compatibility permits. Single-node deployments use short guided maintenance windows. Every upgrade requires a pre-upgrade backup, compatibility check, release notes, health verification, and rollback plan.

## Encryption and observability

Encryption in transit and at rest is mandatory for databases, object storage, backups, attachments, and secrets. V1 exposes a health dashboard, structured logs, metrics, tracing, and health for backups, queues, integrations, sync, and security events.
