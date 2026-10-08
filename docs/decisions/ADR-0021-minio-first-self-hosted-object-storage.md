# ADR-0021: MinIO is the first self-hosted object-storage option

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

MinIO is the first supported self-hosted S3-compatible object-storage option for Rarity V1. It stores authorized attachments, raw inbound email payloads, large integration payloads, and selected backup artifacts; PostgreSQL remains the durable system of record for structured data and object metadata.

## Deployment model

The initial single-server profile may run MinIO as a Docker Compose service on the Ubuntu host with dedicated persistent storage. Production HA must use object storage outside the single Rarity application host or an independently durable/HA MinIO deployment. Encrypted PostgreSQL snapshots and WAL archives must reach a separate failure domain.

## Consequences

Rarity stores object IDs, checksums, lifecycle, scope, permissions, and relationships in PostgreSQL, while file bytes live in MinIO. Database replication, backup, restore, and failover are not burdened by large attachment binaries. Managed cloud object storage remains a future support-matrix extension, not a required V1 installation path.
