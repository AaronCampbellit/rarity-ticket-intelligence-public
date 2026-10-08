# API Standards

**Status:** Draft

REST/JSON is the primary public contract, starting at `/v1`. APIs use stable resource nouns, explicit versions, consistent representations, ISO 8601 UTC timestamps, UUIDs, human-readable identifiers, deterministic errors, and machine-readable schemas.

The UI uses supported application APIs. All mutations pass through authorization, validation, domain invariants, audit, and event production. OpenAPI is generated and checked for compatibility. GraphQL is optional and deferred.

See [Canonical API Contract Examples](canonical-contract-examples.md) and the [Event Catalog and Envelope](../01-architecture/13-event-catalog.md) for the proposed review baseline.
