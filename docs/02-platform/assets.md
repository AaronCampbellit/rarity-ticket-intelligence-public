# Assets

**Status:** Draft

Assets represent supported things: devices, servers, VMs, firewalls, switches, subscriptions, tenants, licenses, certificates, domains, clusters, and other managed resources.

Asset types use versioned schemas. Assets relate to clients, locations, services, contracts, work, documentation, integrations, and graph edges. V1 supports manual/API asset creation and update plus read/ingest-only Datto RMM sync. Datto performs one initial full sync then an MSP-configurable incremental sync with a 15-minute default. Stable external IDs are primary matches; serial number, hostname, and MAC address are candidate evidence. Discovery sources never overwrite technician-confirmed values without provenance and conflict policy; ambiguous Datto conflicts require reconciliation review, and missing assets become stale/inactive rather than being deleted.
