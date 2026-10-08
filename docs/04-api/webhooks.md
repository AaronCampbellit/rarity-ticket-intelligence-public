# Webhooks

**Status:** Draft

Outbound webhooks deliver versioned events signed with the per-integration secret, plus timestamp/replay protection, destination allowlisting, bounded payloads, automatic retry/backoff, idempotent event IDs, delivery logs, disablement policy, and dead-letter handling.

Inbound webhooks terminate at authenticated integration endpoints, retain source provenance, enforce size/rate/schema limits, and enter the normalized intake pipeline. Secrets and sensitive fields are excluded by event contract.
