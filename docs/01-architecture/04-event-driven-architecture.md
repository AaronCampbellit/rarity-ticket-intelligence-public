# Event-Driven Architecture

**Status:** Draft

Every accepted mutation writes state and an outbox record atomically. Publishers deliver versioned events to consumers with at-least-once semantics; consumers must be idempotent.

Events include event ID, type, schema version, occurred time, actor, MSP/client scope, object reference, correlation ID, causation ID, source, and a minimal permission-safe payload. Sensitive data is minimized and never used as a substitute for fetching authorized current state.

Consumers include automation, notifications, email, webhooks, search indexing, reporting, metrics, audit projection, and AI services. Replay is controlled, observable, scoped, and never duplicates side effects. Dead-letter handling supports inspection, retry, and dismissal.
