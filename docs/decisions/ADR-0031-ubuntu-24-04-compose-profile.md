# ADR-0031: Ubuntu 24.04 Docker Compose deployment profile

**Status:** Accepted\
**Date:** 2026-07-27

## Decision

The first supported Rarity deployment profile is Ubuntu Server 24.04 LTS on x86-64 using Docker Engine and Docker Compose v2. Ubuntu 26.04 LTS will be evaluated after the Rarity stack has completed compatibility qualification.

## Deployment tiers

| Tier | Suggested resources | Intended use |
| --- | --- | --- |
| Development/demo | 4 vCPU, 16 GB RAM, 150 GB SSD | Development, test, demonstrations |
| Single-node pilot | 8 vCPU, 32 GB RAM, 1 TB NVMe/SSD | Controlled pilot; explicitly non-HA |

The single-node pilot runs the Rarity web/API and worker services, one PostgreSQL instance, one Valkey instance, one local MinIO instance for attachments and inbound payloads, and Caddy as the initial HTTPS ingress. Remote MinIO/S3-compatible object storage remains mandatory for backups and WAL archives.

## Security and operations

Only HTTPS is public. PostgreSQL, Valkey, and MinIO stay on private Docker networks. Images are signed/pinned, containers run with least privilege, durable data uses documented host paths, and no development code mounts or default credentials appear in the pilot profile. Configuration and secret references stay outside source control with restricted filesystem permissions.

Every upgrade takes a pre-upgrade backup, runs compatibility checks, uses a guided maintenance window, verifies health, and has a rollback plan. The first-run wizard configures the organization, Entra, intake, object storage, backups, and initial administrator.

## Consequences

The initial install is repeatable on familiar Ubuntu/Docker infrastructure but does not provide HA. It is suitable for development, demos, and controlled pilots. Production HA uses the separate approved PostgreSQL/Patroni/etcd nodes and independently durable object storage.
