# Data Architecture

**Status:** Draft

PostgreSQL is the transactional system of record for objects, relationships, workflows, audit metadata, and the event outbox. Strong constraints, explicit client scope, transactional boundaries, online migration discipline, and point-in-time recovery are required.

Large attachments belong in object storage; search indexes and caches are derived and rebuildable. Graph relationships begin in the relational store with indexed edges and traversal services; a specialized graph store is adopted only with measured need and an authoritative synchronization design.
