# ADR-0025: pgBackRest continuous point-in-time recovery

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

Rarity V1 uses pgBackRest for encrypted PostgreSQL physical backups and continuous WAL archiving. Backup and WAL objects must be stored in a separate failure domain from the live PostgreSQL cluster, using the installation's configured remote MinIO or S3-compatible object storage.

PostgreSQL archives WAL continuously with `archive_timeout` set to 60 seconds. The standard backup cadence is an incremental backup every six hours, a differential backup nightly, and a full backup weekly. The minimum restore-point retention is 90 days.

## Verification and recovery

Automated restore verification runs monthly, and a documented disaster-recovery exercise runs quarterly. Restore verification proves that a clean PostgreSQL instance can recover from a selected backup and its archived WAL. The quarterly exercise also validates the published one-hour recovery-time objective and five-minute recovery-point objective.

Backup-encryption and object-store recovery material is held separately from the live cluster and backup destination. A recovery procedure identifies authorized custodians, access prerequisites, rotation, and a tested recovery path; neither a database node nor an ordinary application administrator is the sole holder of the ability to recover backups.

## Alerting contract

The platform warns when WAL archival is delayed by two minutes, raises critical alerting at four minutes, and declares the five-minute recovery-point objective at risk at five minutes. It also alerts on failed backups, missed cadence, checksum/integrity failure, unsuccessful restore verification, retention shortfall, object-storage capacity risk, and backup duration materially exceeding its baseline.

## Consequences

The production deployment has a measurable, testable recovery posture rather than an unverified snapshot policy. Backup storage and recovery keys require their own monitoring, access controls, and exercises. This decision does not make the object store itself a database or change PostgreSQL's role as the durable system of record.
