# Object Model

**Status:** Draft

Every first-class object has an immutable UUID, type, scope, lifecycle state, created/updated metadata, version, relationships, authorization surface, audit trail, API representation, events, and search policy.

Human-readable identifiers are separate from UUIDs and may use type-specific sequences. Updates use optimistic concurrency. Deletion is normally a soft-delete/tombstone followed by policy-controlled retention. Restoration preserves identity and history.

Custom fields are typed, validated, schema-versioned, permission-aware, searchable by policy, and cannot replace core invariants. Relationships are first-class records with type, direction, source, confidence, effective dates, verification state, and history.
