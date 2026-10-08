# V1 Threat Model

**Status:** Phase 0 baseline

## Assets to protect

Client-isolated business data, credentials and secrets, Entra identity trust, break-glass access, attachments/email, audit evidence, backups/WAL, API keys/PATs, webhook secrets, Datto tokens, AI context, and service availability.

## Primary threats and baseline controls

| Threat | Baseline control | Required evidence |
|---|---|---|
| Cross-client access | Explicit scope, centralized authorization, isolation test suite | CI isolation results |
| Account/session theft | Entra validation, secure sessions, revocation, audit | Session/auth tests |
| Break-glass misuse | Restricted local accounts, prominent audit, exercise | Quarterly account exercise |
| API key theft/abuse | Scoped/expiring credentials, rotation, rate limits, audit | Key lifecycle tests |
| Webhook forgery/replay | Per-integration signing, timestamp/replay checks | Contract tests |
| Malicious intake/attachment | Bounded intake, safe rendering, download-only active types | Upload/render tests |
| Automation escalation/loop | Typed actions, authorization, causation/idempotency, dead letters | Automation abuse tests |
| Backup compromise/ransomware | Separate encrypted storage, least privilege, restore verification | Restore/DR evidence |
| HA split-brain/data loss | Quorum, fencing, synchronous acknowledgement, failover tests | HA exercise evidence |
| AI data disclosure | MSP opt-in, provider disclosure, context exclusions, isolation | AI context tests |
| Supply-chain compromise | Immutable-SHA actions, digest-pinned bases/promotion, read-only PR tokens, scanned dependencies/images, keyless-signed artifacts, SBOM/provenance | CI, signature, attestation, and digest evidence |

## Security gates

Every Phase 1+ implementation item must identify its threat-model entries, authorization behavior, audit event, data classification, failure mode, test coverage, and operational signal. A new external trust boundary requires a threat-model update before implementation.
