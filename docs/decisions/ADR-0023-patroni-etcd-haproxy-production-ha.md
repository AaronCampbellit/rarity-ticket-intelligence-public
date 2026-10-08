# ADR-0023: Patroni, etcd, and HAProxy production HA

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

Production PostgreSQL HA uses Patroni for database orchestration, a three-member etcd quorum for leader coordination, and HAProxy for the stable application write endpoint. Patroni uses strict synchronous replication and required Linux watchdog fencing for the primary.

## Deployment tiers

Development, testing, and demos may run all services on one Ubuntu VM using Docker Compose; this is explicitly non-HA. Production HA uses three separate Ubuntu VMs or, preferably, three separate physical machines/failure domains. Each production node runs PostgreSQL, Patroni, and one etcd member.

## Consequences

Normal V1 application reads and writes use the HAProxy primary endpoint. The synchronous replica protects acknowledged writes; the asynchronous replica provides the third copy but is not used for normal application reads. If the synchronous replica is unavailable, strict mode may block new writes rather than acknowledge data that could be lost.
