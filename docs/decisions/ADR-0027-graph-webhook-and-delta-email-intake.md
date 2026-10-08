# ADR-0027: Microsoft Graph webhook and delta email intake

**Status:** Accepted\
**Date:** 2026-07-26

## Decision

Rarity V1 uses a dedicated Microsoft 365 shared support mailbox as its primary email-intake source. A dedicated Entra application uses least-privileged application permissions restricted to that mailbox. It receives basic Microsoft Graph change notifications, validates every notification, and securely retrieves the actual message from Graph; ticket content is not carried in the webhook.

## Reliability model

Graph subscription renewal occurs before expiry and lifecycle notifications are handled for reauthorization, removal, and missed notifications. Rarity retains a per-folder delta cursor and reconciles mail changes at least every five minutes. Graph retrieval failures, subscription health, renewal status, delta lag, and intake-queue delay are visible in the operational health dashboard.

## Retention and threading

Rarity stores the original MIME message, normalized content, and authorized attachments in encrypted object storage. The retention default is seven years. Threading uses Graph identifiers, internet message headers, and Rarity ticket references; unmatched messages enter normal intake rather than being silently attached to a ticket.

## Forwarding fallback

Standard forwarding remains a secondary intake route for legacy or non-Microsoft mail. It uses a dedicated Rarity intake address with sender validation, rate limits, quarantine, and health visibility. The mailbox protection remains responsible for email-borne malware screening in V1; Rarity does not add an attachment malware scanner in this release.

## Consequences

Email intake is near-real-time but does not treat webhook delivery as durable proof of receipt. Delta reconciliation makes the path recoverable from notification gaps. The support mailbox and Entra application require explicit onboarding, credential rotation, and least-privilege review.
