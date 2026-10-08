# V1 Identity and AI Boundaries

**Status:** Accepted V1 direction

## Identity and privileged operations

Local Platform Administrators are the required installation-administration identity and work without Microsoft credentials. They can only hold the Platform Administrator role, are network restricted, and are prominently audited. Entra is an optional workforce identity provider with first-login just-in-time provisioning; SCIM is deferred. V1 does not require step-up MFA or dual approval: it relies on the active authentication policy, RBAC, explicit destructive-action confirmations, and immutable audit records.

Roles are MSP-global in V1. Users may hold multiple roles and receive the combined permitted capabilities. Default roles are Administrator, Service Manager, Billing Manager, Service Desk Technician, Service Desk Manager, Professional Services, and Central Services. Administrators may edit defaults and create custom roles; every role/permission change records before/after history.

## AI

AI is MSP opt-in and limited to technician-controlled summaries, reply drafts, and similar-ticket/knowledge suggestions. It cannot autonomously alter records, send messages, or run automation. Provider data sharing is disclosed before enablement. Rarity never trains its own models on customer data. Attachments, credentials, secrets, and sensitive fields are excluded by default.
