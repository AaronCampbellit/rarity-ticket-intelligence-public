# Backups and Snapshots

**Status:** Backup and clean-room restore source contracts implemented; live acceptance pending

Backups use layered protection: pgBackRest encrypted physical backups, continuous WAL archiving to separate storage for point-in-time recovery, configuration and secret-recovery material, off-platform copies, and policy-controlled retention. PostgreSQL archives WAL with `archive_timeout=60s`; a warning begins at two minutes of archival delay, critical alerting at four, and five minutes is an RPO-at-risk condition.

The default cadence is an incremental backup every six hours, a differential backup nightly, and a full backup weekly. WAL and backup objects reside in a separate failure domain using the installation's configured remote MinIO or S3-compatible object storage. Restore points retain for 90 days by default and may be extended by configuration.

Jobs are monitored for freshness, completeness, duration, integrity, retention, object-storage capacity, and restore-verification outcome. Monthly automated restore verification and a documented quarterly DR exercise are required. A backup is not considered successful until restoration is routinely tested. Encryption and object-store recovery material is separately protected, has authorized custodians, and is included in recovery drills. Documentation records dependencies and recovery order.

## Implemented operating assets

`infrastructure/recovery/pgbackrest.conf.tmpl` configures a TLS-verified remote S3-compatible repository, AES-256 repository encryption, asynchronous WAL spooling, 13 retained weekly full backup sets, and archive retention tied to full backups. Repository credentials and the independent cipher passphrase are rendered only into operator-owned restricted configuration.

`backup-schedule.yaml` records the accepted UTC cadence: full weekly, differential on the other six nights, incremental every six hours, and restore verification monthly. `backup.sh` allows only those three backup types, takes an exclusive lock, checks the repository, and writes an owner-only JSON evidence record containing pgBackRest repository state.

`restore-verify.sh` accepts a past UTC recovery target and restores only into a new directory beneath an approved clean-room root. It refuses existing targets and never uses delta restore. Verification checks the repository, control data, page checksums, isolated database startup, and a SQL probe before emitting owner-only evidence. Actual schedules, remote credentials, backups, and restores remain environment acceptance gates.

## Setup Center verification evidence

Set `RARITY_BACKUP_EVIDENCE_KEY` to an installation-unique value of at least 32
characters in both the API and operator environments. After a pgBackRest check
and clean restore test, run:

```bash
go run ./backend/cmd/rarity-backup-evidence \
  --pgbackrest-evidence ./pgbackrest-evidence \
  --restore-evidence ./restore-evidence \
  --msp-id <installation-msp-uuid> \
  --api-url https://rarity.example \
  --expected-version <setup-center-version>
```

The normalized pgBackRest file contains `stanza`, `repository`,
`latest_backup_at`, and `latest_wal_at`; the restore file contains
`restore_verified_at`. Signed evidence expires after 15 minutes and its nonce is
single-use. Rarity retains only an evidence hash, safe labels, timestamps, and
the result code. Verification requires a backup newer than 24 hours, WAL
evidence newer than 15 minutes, and restore proof newer than 31 days.
