# ADR-0033: Temporary shared demo-host exception

**Status:** Accepted\
**Date:** 2026-07-28

## Context

ADR-0031 defines the first supported Rarity development/demo profile as Ubuntu Server 24.04 LTS, 4 vCPU, 16 GB RAM, and 150 GB SSD. The available owner-managed host is `Hankdemoserver`: Ubuntu 25.10, 4 vCPU, approximately 7.3 GiB RAM, and 146 GB root storage. It is also running the independent `hankserverside` and `rtm` Docker Compose projects plus their shared Cloudflare tunnel.

The project owner explicitly accepts this host as a temporary constrained exception in order to unblock the Phase 0 runnable-foundation work. CPU and RAM expansion is planned later.

## Decision

Use the shared host only for RTI's early, non-production Compose validation and demonstrations, subject to all of the following:

- Run a separately named RTI Compose project and use an isolated RTI directory, networks, volumes, and ports. Do not restart, remove, reconfigure, or rely on HankServerside, RTM, or the shared tunnel.
- Use synthetic fixture data and non-production secrets only. Do not connect Entra, Graph, Datto, Teams, customer mailboxes, or customer storage.
- Apply explicit CPU, memory, and storage limits to RTI services; stop the RTI deployment if it affects the existing projects or host health.
- Keep RTI internal to the host or LAN until a distinct ingress and public-URL review is completed. Do not modify the shared Cloudflare tunnel.
- Treat every result as constrained demo evidence only. This host provides no HA, backup/restore, production-capacity, or supported-profile acceptance evidence.

## Consequences

This is an exception to the supported demo profile, not a change to ADR-0031. RTI must be requalified on Ubuntu 24.04 LTS with at least 16 GB RAM and 150 GB available storage before it can be described as the approved development/demo profile. Production and HA requirements remain unchanged.
