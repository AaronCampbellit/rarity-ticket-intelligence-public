# Initial Permission Matrix

**Status:** Detailed draft\
**Version:** 0.1\
**Last updated:** 2026-07-25

## Authorization model

Roles are named collections of capabilities. Authorization combines:

```text
principal + capability + MSP scope + client scope
+ object relationship + workflow state + contextual policy
```

Assignment and ownership do not automatically bypass capability checks. The UI may hide unavailable actions for clarity, but the application service remains authoritative.

## Baseline roles

These are starter roles, not hardcoded product identities:

- **Platform Administrator:** installation, identity, security, infrastructure, and recovery administration.
- **Service Desk Manager:** cross-queue work oversight, workflow operations, escalations, and reporting.
- **Technician:** assigned/authorized client work within membership and queue policy.
- **Read-only Auditor:** immutable read access to authorized records, history, and logs.
- **Integration Principal:** only explicitly granted API scopes and clients.
- **Automation Principal:** union of published automation policy and connection scope; never the publisher’s unlimited rights.
- **Break-glass Administrator:** emergency platform administration with heightened monitoring and restrictions.

## Capability matrix

`A` = allowed by baseline role, subject to scope/context. `—` = denied unless a custom role explicitly grants it.

| Capability | Platform Admin | Manager | Technician | Auditor | Integration | Automation |
|---|:---:|:---:|:---:|:---:|:---:|:---:|
| `Client.Read` | A | A | scoped | scoped | scoped | scoped |
| `Client.Create` | A | A | — | — | scoped | — |
| `Client.Edit` | A | A | — | — | scoped | scoped |
| `Client.Delete` | A | — | — | — | — | — |
| `organization.read` | A | A | scoped | scoped | — | — |
| `organization.manage` | A | A | — | — | — | — |
| `WorkRecord.Read` | A | A | scoped | scoped | scoped | scoped |
| `WorkRecord.Create` | A | A | A | — | scoped | scoped |
| `WorkRecord.Edit` | A | A | scoped | — | scoped | scoped |
| `WorkRecord.Assign` | A | A | scoped | — | scoped | scoped |
| `WorkRecord.TakeOwnership` | A | A | scoped | — | — | scoped |
| `WorkRecord.TransferOwnership` | A | A | scoped | — | scoped | scoped |
| `WorkRecord.Transition` | A | A | scoped | — | scoped | scoped |
| `WorkRecord.Close` | A | A | scoped | — | scoped | scoped |
| `WorkRecord.Delete` | A | — | — | — | — | — |
| `InternalNote.Read` | A | A | scoped | scoped | scoped | scoped |
| `InternalNote.Create` | A | A | scoped | — | scoped | scoped |
| `mention.create` | A | A | scoped | — | — | — |
| `mention.read` | A | A | scoped | scoped | — | — |
| `ClientReply.Send` | A | A | scoped | — | scoped | scoped |
| `Attachment.Upload` | A | A | scoped | — | scoped | scoped |
| `Attachment.Download` | A | A | scoped | scoped | scoped | scoped |
| `TimeEntry.Create` | A | A | scoped | — | scoped | scoped |
| `Opportunity.Read` | A | A | scoped | scoped | scoped | scoped |
| `Opportunity.Create` | A | A | scoped | — | scoped | scoped |
| `Opportunity.Edit` | A | A | scoped | — | scoped | scoped |
| `Proposal.Issue` | A | A | scoped | — | scoped | — |
| `Proposal.RecordAcceptance` | A | A | scoped | — | scoped | — |
| `Project.Read` | A | A | scoped | scoped | scoped | scoped |
| `Project.Edit` | A | A | scoped | — | scoped | scoped |
| `ChangeOrder.Edit` | A | A | scoped | — | scoped | — |
| `Relationship.Manage` | A | A | scoped | — | scoped | scoped |
| `Workflow.Read` | A | A | scoped | scoped | — | — |
| `Workflow.EditDraft` | A | A | — | — | — | — |
| `Workflow.Publish` | A | scoped | — | — | — | — |
| `Automation.Read` | A | A | scoped | scoped | — | — |
| `Automation.EditDraft` | A | A | — | — | — | — |
| `Automation.Publish` | A | scoped | — | — | — | — |
| `Automation.Run.Retry` | A | A | scoped | — | — | — |
| `Connection.Secret.Rotate` | A | — | — | — | — | — |
| `service_key.manage` | A | — | — | — | — | — |
| `Audit.Read` | A | scoped | own/scoped | A | — | — |
| `Audit.Export` | A | scoped | — | scoped | — | — |
| `Security.Policy.Manage` | A | — | — | — | — | — |
| `Backup.ReadStatus` | A | scoped | — | scoped | — | — |
| `Restore.Execute` | step-up | — | — | — | — | — |

## Scope rules

- `scoped` means the role grant must intersect an authorized client, queue, department, team, object relationship, or explicit resource set.
- MSP-global read does not imply client-bound read.
- Contact-facing visibility never includes internal notes, restricted attachments, audit details, or security metadata.
- Integration and automation scopes are allowlists. An omitted client or action is denied.
- Background jobs receive a service identity and minimum capabilities; they do not run as an unrestricted system user.
- Change Order approval override uses `ChangeOrder.Edit` in the authorized Project scope. It requires an audited reason but deliberately has no separate override capability.

## Internal mentions

Mentions are communication facts, not access grants. `mention.create` is
required in addition to edit permission for the exact internal details,
comment, or note. A recipient needs `mention.read`, active internal-technician
status, Client access, and current read permission for the ticket, task, or
project. Team membership selects recipients but never supplies read access.
Internal collaboration history follows the parent content permission and is
not gated by `mention.read`, which is specific to recipient widget access. UI
mutation controls require both `mention.create` and effective parent edit;
read-only and create-without-parent-edit users see history without composers,
edit actions, or redaction actions.

The recipient alone owns unread, read, and archived widget state. Authors,
administrators, teams, assignments, participants, watchers, notification
policies, and mention creation cannot change that state. Mentions never create
an assignment, task, approval, participant, watcher, collaborator, or
permission. Public/customer-visible content and public tokens cannot contain
structured mention tokens; notes and mention sources are strictly internal.

When authorization may have been lost, the changing transaction increments a
locked MSP authorization revision and writes one compact scoped marker. Mention
occurrences record the revision under which they were authorized. Applicable
pending markers deny reads immediately. A bounded worker pages marker/item
pairs, evaluates temporal role-assignment and relevant-capability history at
the marker boundary, and persists only confirmed snapshot losses. It then
suppresses the snapshot and cancels pending delivery with `access_revoked`,
even if access was restored before the worker ran. Only a newer authorized
mention supersedes the decision and creates a new unread projection version.
Assignment expiry follows the same rule: a bounded worker transaction first
increments the MSP revision, then deletes expired assignments and emits
`role.unassigned` audit/outbox evidence. Temporal authorization uses the
recorded revision timestamp, so wall-clock expiry cannot silently restore an
older mention after a later grant. Authorization, parent, and source mutation
producers acquire the revision lock before their domain rows.

## V1 privileged-action policy

V1 does not require step-up MFA or dual approval. It relies on normal Entra sign-in, RBAC, explicit destructive-action confirmations, and immutable audit records. The following remain prominent confirmation/audit actions and candidates for future strengthening:

- executing a restore or permanent erasure;
- changing identity-provider trust or break-glass credentials;
- exporting broad client or audit datasets;
- rotating root encryption/recovery keys;
- disabling immutable audit controls;
- publishing automations with broad client scope or external data transfer;
- granting Platform Administrator or equivalent permissions.

## Explainability

Denied and allowed decisions produce a structured internal reason containing policy version, evaluated capability, role grants, scope intersection, relationship rule, workflow condition, and correlation ID. Public errors avoid revealing inaccessible object existence.

## Required tests

The full matrix is generated into authorization contract tests. Tests cover direct API calls, UI calls, automation, integrations, background jobs, stale membership, disabled users, deleted clients, record transitions, and ownership changes.

## Unified calendar

| Capability | Boundary |
| --- | --- |
| `calendar.read` | Authorized cross-client events and safe workforce Busy time |
| `calendar.schedule` | Preview/apply; source edit and workforce authority are rechecked |
| `calendar.commitment.manage` | Typed maintenance, renewal and license records |
| `calendar.policy.manage` | Conflict policy and custom-date definition administration |
| `calendar.workforce.manage` | Working schedules and authorized PTO administration |
| `view.save`, `view.share` | Private/shared calendar lenses; query scope is revalidated |

Dependency reads require both source endpoints to remain readable. Source-level
custom date definitions use persisted source read grants, not calendar-policy
administration. See the [calendar contract](../02-platform/unified-calendar.md).
