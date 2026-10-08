# PostgreSQL Cluster

**Status:** Source topology implemented; three-node acceptance pending

The supported topology is three PostgreSQL nodes: one primary, one synchronous failover replica, and one asynchronous replica. Patroni orchestrates PostgreSQL; a three-member etcd quorum coordinates leader state; HAProxy exposes the stable application primary endpoint. Patroni strict synchronous replication and required Linux watchdog fencing protect acknowledged writes and prevent unsafe leader behavior.

Normal failover targets under two minutes with zero confirmed-data loss for acknowledged writes. Full cluster disaster recovery targets under one hour with up to five minutes data loss. Production nodes occupy separate Ubuntu VMs or preferably separate physical machines/failure domains. Development, testing, and demos may co-locate all services on one Ubuntu VM through Docker Compose, but that profile is explicitly non-HA. Three copies alone do not constitute tested HA.

## Configuration and bootstrap

`infrastructure/ha` contains the node Compose model and Patroni, etcd, and HAProxy templates. Render a fresh owner-only configuration directory independently on each node:

```bash
go run ./backend/cmd/rarity-ha-render \
  infrastructure/ha \
  /etc/rarity/ha-rendered
```

The renderer requires the node identity (`rti-db-1` through `rti-db-3`), all three unique addresses within the configured cluster CIDR, an installation-specific cluster name/token, pgBackRest stanza, and separate 32-character-or-longer database credentials. It refuses incomplete templates, unsafe identifiers, node/address mismatches, duplicate addresses, weak credentials, and an existing output directory. Rendered files use mode `0600`; TLS and pgBackRest material remain separate operator-controlled mounts.

Bootstrap etcd with `ETCD_INITIAL_CLUSTER_STATE=new` exactly once for the initial three members. Subsequent node starts use `existing`; cluster membership changes use etcd’s governed member operations rather than rewriting a new bootstrap identity. Before service start, operators must validate immutable image digests, certificate SANs, private-key ownership, host firewall rules, `/dev/watchdog` behavior, remote WAL storage, and independent HAProxy endpoint discovery.

Source contract tests verify strict synchronous mode, one synchronous copy, required watchdog fencing, TLS etcd peer/client authentication, three-member identity, leader-only write routing, digest-only images, read-only container filesystems, and private host-network operation. Live bootstrap and failover evidence remain environment gates.
