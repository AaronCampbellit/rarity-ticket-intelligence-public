# ADR-0028: Datto read/ingest reconciliation and alert deduplication

**Status:** Accepted\
**Date:** 2026-07-27

## Decision

Rarity V1 uses Datto RMM as a read/ingest-only integration. It does not resolve Datto alerts, change devices, run jobs, or write any data back to Datto. Each connection uses a dedicated, named credential stored as an encrypted connection object and supports rotation without editing automations.

## Synchronization and reconciliation

An initial full inventory sync establishes the Datto snapshot. Incremental synchronization then runs every 15 minutes by default, with MSP-configurable scheduling, manual runs, progress, rate-limit visibility, and error history. Stable Datto identifiers are the primary asset match keys; serial number, hostname, and MAC address are candidate evidence only.

Datto sites map to Rarity clients through an explicit mapping surface. Routine, pre-approved mapping and field-reconciliation rules may run automatically. Ambiguous matches and conflicts require an authorized user to choose Rarity, choose Datto, link records, or keep them separate. Every result carries source provenance and is auditable. Assets that are missing or retired in Datto become stale/inactive for review and are never automatically deleted.

## Alert behavior

One Datto alert identity maps to one open Rarity incident. Repeated observations update that incident. Similar recurring alerts may update an existing open incident when they match an MSP-configurable fingerprint within a default 24-hour window. A cleared Datto alert appends a recovery event but never automatically resolves the Rarity incident.

## Consequences

Datto remains the authoritative source for its inventory and alert state, while Rarity remains the technician's authoritative work record. The connector must preserve external IDs, source payload references, reconciliation outcomes, and failures so every imported state can be explained.
