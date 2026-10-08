# Disaster Recovery

**Status:** Recovery verification source tooling implemented; live DR exercises pending

Disaster recovery defines service tiers, RPO/RTO, declared disaster authority, communication, clean-room restore, DNS/ingress recovery, identity and key recovery, database and object-store consistency, event replay, and validation. The V1 targets are recovery in under one hour with up to five minutes of data loss after full-cluster disaster; normal database failover targets under two minutes with zero confirmed-data loss for acknowledged writes.

The recovery implementation uses pgBackRest physical backups and continuous encrypted WAL archive in a separate failure domain. A full backup runs weekly, a differential nightly, and incrementals every six hours; WAL is forced at least every 60 seconds. The database recovery key path is protected independently of both the running cluster and backup destination.

Monthly automated restores prove a clean database can recover selected backup/WAL sets. Quarterly exercises include node loss, site loss, accidental deletion, corrupted migration, ransomware, credential loss, and compromised backup. Results become tracked remediation work.

The implemented recovery tool restores a selected point in time only beneath `/var/lib/rarity/restore-verification` or `/srv/rarity/restore-verification`, refuses an existing destination, validates physical control/checksum state, starts PostgreSQL on an isolated loopback port, runs a SQL probe, and records structured evidence. It deliberately leaves the restored target for authorized review instead of deleting evidence.

Quarterly acceptance must additionally time the full declared-disaster procedure; recover independently held TLS, database, object-store, and backup-encryption material; restore attachment/object state consistently; validate application readiness and immutable audit/outbox state; prove required event replay behavior; record the observed RPO/RTO; and assign every exception corrective work.
