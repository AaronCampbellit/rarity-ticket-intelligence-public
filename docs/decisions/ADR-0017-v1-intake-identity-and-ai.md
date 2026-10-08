# ADR-0017: V1 intake, identity, and AI boundaries

**Status:** Accepted\
**Date:** 2026-07-25

## Decision

V1 intake supports Microsoft Graph, email forwarding, direct API, and inbound webhooks. APIs start at `/v1` with scoped service keys and expiring technician PATs. Entra uses JIT first-login provisioning and local audited break-glass accounts. AI is MSP opt-in and limited to technician-controlled summaries, reply drafts, and similar-record/knowledge suggestions.

## Consequences

OAuth/SCIM, autonomous AI, AI messages/automation, attachment analysis, and provider training on customer data are out of scope. No step-up MFA or dual approval is required in V1; RBAC, confirmation, standard Entra sign-in, encryption, and immutable audit remain mandatory.
