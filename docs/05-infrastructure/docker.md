# Docker Deployment

**Status:** Demo and single-node pilot source profiles implemented; constrained demo Compose acceptance complete and supported-profile acceptance pending

The initial supported topology is Ubuntu Server 24.04 LTS x86-64 using Docker Engine and Docker Compose v2. It uses signed, pinned container images and a documented base Compose configuration plus a single-node pilot override. The pilot includes the application/API process with its leased worker loops, PostgreSQL, Valkey, local MinIO, Caddy ingress, and a remote MinIO/S3-compatible backup destination. A separate worker container is not part of the current Compose model.

Containers run without unnecessary privilege, use health checks, bounded resources, read-only filesystems where possible, isolated networks, durable named storage, and external secret injection. Development/demo uses 4 vCPU, 16 GB RAM, and 150 GB SSD; a controlled pilot uses 8 vCPU, 32 GB RAM, and 1 TB NVMe/SSD. Single-node deployment is not represented as production HA.

The demo and pilot overlays apply `unless-stopped` restart policies so the API
worker process recovers after transient state-service outages. The pilot
override additionally requires operator-owned TLS material for every state
service, publishes only HTTPS, persists ingress certificate state, and
configures bounded local logs. The application rejects pilot/production
configuration using plaintext database, cache, or object-storage transport and
rejects session keys shorter than 32 characters. Live image signature/pinning
verification and Ubuntu host qualification remain release gates.

The demo overlay publishes Caddy on every VM network interface by default at
`RARITY_DEMO_HTTP_PORT`. Set `RARITY_DEMO_BIND_ADDRESS` to a specific VM
address when the host has multiple networks, or to `127.0.0.1` for an
operator-tunnel-only deployment. PostgreSQL, Valkey, and MinIO remain
unpublished on the private Compose networks.
