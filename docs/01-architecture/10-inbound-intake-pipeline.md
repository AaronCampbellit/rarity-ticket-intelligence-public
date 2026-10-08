# Inbound Intake Pipeline

**Status:** Draft

Email, API calls, monitoring alerts, and technician actions are normalized into inbound events before they create or update work.

```text
Receive → authenticate/validate → normalize → scan/classify
→ correlate/deduplicate → select workflow → run policy/automation
→ create or update work → emit platform events
```

Inbound events retain source, external ID, received time, authentication result, normalized payload, raw-payload retention reference, correlation fingerprint, processing state, and errors. Processing is idempotent. Quarantine and replay are authorized and audited.

V1 email intake supports Microsoft Graph notifications followed by secure retrieval, and standard forwarding into the Rarity intake address. The configured MSP support address creates or updates threaded tickets. APIs/webhooks are system intake. Email threading must prefer reliable message identifiers and ticket tokens over subject matching. Monitoring alerts use external IDs and deduplication fingerprints. Internal notes are never emitted as client email.
