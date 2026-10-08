# Data Lifecycle and Retention

**Status:** Accepted V1 policy

## Default retention

Retention defaults are editable through the GUI by authorized administrators:

| Data class | Default |
|---|---:|
| Ticket and immutable audit history | Permanent |
| Email and attachments | Seven years |
| Restore points | 90 days; configurable longer |

Retention changes are versioned, auditable, prospective by default, and cannot silently destroy data subject to legal hold or explicit preservation policy.

## Lifecycle

Tickets and audit records are not purged by ordinary retention jobs. Deleted business objects are tombstoned and recoverable under policy. Email and attachment expiration removes payloads and derived previews according to policy while preserving the appropriate audit record. Backups and WAL archives follow the restore-point policy and are securely expired from every configured destination.

## Administration

The GUI shows current policy, scope, exceptions, job freshness, upcoming expiration, legal-hold state, and failed lifecycle jobs. Any permanent erasure remains a deliberate RBAC-protected action with confirmation and immutable audit.
