# Deployment

**Status:** Single-node pilot source profile implemented; constrained demo host accepted and supported-profile host acceptance pending

Rarity supports MSP-controlled hardware, private cloud, and public cloud. The first supported profile is Ubuntu Server 24.04 LTS on x86-64 with Docker Engine and Docker Compose v2. Docker Compose is the first deployment experience; Kubernetes support follows after service boundaries and operating requirements are proven.

Deployment is declarative, repeatable, versioned, preflight-validated, and reversible. Configuration separates non-secret settings from secret references. Upgrades include compatibility checks, database migration gates, health verification, and rollback guidance.

Development/demo uses 4 vCPU, 16 GB RAM, and 150 GB SSD. The controlled single-node pilot uses 8 vCPU, 32 GB RAM, and 1 TB NVMe/SSD. It runs the application/API process with its leased worker loops, PostgreSQL, Valkey, local MinIO, and Caddy ingress; remote object storage remains mandatory for backups/WAL. A separate worker container is not currently deployed. Only HTTPS is public, and internal state services remain on private Docker networks.

See the accepted [Production Operating Model](production-operating-model.md) for HA, recovery, storage, encryption, and upgrade commitments.

Emergency credential recovery uses the packaged, one-shot operator container described in [Local Administrator Recovery](local-administrator-recovery.md). It is excluded from normal startup by the `operator` profile and has no published port.

## Single-node pilot profile

The pilot merges `infrastructure/compose/compose.yaml` with `infrastructure/compose/compose.pilot.yaml`. It publishes only TCP 443 through Caddy, persists Caddy certificate state, keeps PostgreSQL/Valkey/MinIO private, enables TLS for each state service, bounds local container logs, and applies restart policies. This profile is not production HA.

Set `RARITY_ENV=pilot`, `RARITY_PUBLIC_HOST`, and an absolute `RARITY_CONFIG_DIR`. The configuration directory is operator-controlled, excluded from source control, and contains:

```text
ca/ca.crt
postgres/server.crt
postgres/server.key
valkey/server.crt
valkey/server.key
minio/certs/public.crt
minio/certs/private.key
minio/certs/CAs/ca.crt
```

The PostgreSQL and Valkey server certificates must cover their Compose DNS names; the MinIO certificate must cover `minio` and `localhost` for its health probe. Private keys use owner-only permissions and are readable only by the matching container identity. `DATABASE_URL` uses `sslmode=verify-full` and the mounted `/run/rarity-ca/ca.crt`; `VALKEY_URL` uses `rediss://`; `S3_ENDPOINT` uses `https://`. `S3_BUCKET`, `S3_REGION`, `S3_ACCESS_KEY_ID`, and `S3_SECRET_ACCESS_KEY` identify the pre-provisioned attachment bucket and its least-privilege application credential.

After operator configuration, render and inspect the merged model before starting it:

```bash
docker compose --env-file .env \
  -f infrastructure/compose/compose.yaml \
  -f infrastructure/compose/compose.pilot.yaml \
  config --quiet
```

Docker rendering and live TLS/host acceptance remain environment gates.

## Upgrade gate

`infrastructure/recovery/upgrade-gate.sh` is a non-mutating evidence gate. Before an operator starts an upgrade, it requires a passed pre-upgrade backup record, compatible current/target schema evidence, the exact intended revision, a rollback plan covering trigger/procedure/data compatibility, and passed health evidence for that revision. It does not perform an upgrade or infer rollback safety; those remain release-specific operator actions.
