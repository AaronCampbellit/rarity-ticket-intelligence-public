# Internal Mentions and Collaboration Design

**Status:** Approved in design review on 2026-08-06; awaiting written-spec review

## Goal

Create a lightweight internal attention mechanism that lets staff mention an
individual or organizational team from authorized ticket, task, and project
content. A mention must take the recipient directly to the latest source,
remain distinct from assignment and subscription concepts, and provide a
compact dashboard workflow for unread, read, and archived attention.

The feature must preserve Rarity's client and project isolation. A mention
never grants access, never makes someone an assignee or watcher, and never
copies protected internal content into events or notification records.

## Approved Product Decisions

- Mentions are separate from owners, assignees, collaborators, watchers, and
  tasks.
- Only internal staff may create or receive mentions.
- Mentions are allowed only in internal content. Customer-visible descriptions,
  public comments, portal content, and outbound messages cannot contain active
  mention tokens.
- Supported parent objects are tickets, tasks, and projects.
- Supported sources are their internal details, internal comments, and notes.
- Individuals and existing organizational teams are supported in the initial
  release.
- A team mention snapshots its current active, eligible members when the source
  is saved.
- Ineligible team members receive nothing, and the author is warned about the
  exclusions.
- Each recipient has one Mentions widget item per parent object.
- A later mention on the same object updates that item to the latest occurrence
  and returns it to active and unread.
- Opening a mention's deep link marks it read. Manual read, unread, and archive
  controls are also available.
- Re-mentioning an archived item restores it to the active unread view.
- The dashboard widget is the complete in-app mentions surface; there is no
  separate Mentions inbox.
- The widget may show a short, permission-checked preview loaded from the source
  at view time. Mention records never persist that preview.
- The widget is the sole in-app destination for mentions. The generic
  notification inbox does not receive duplicate mention rows.
- Email and Teams delivery follow organization policies, recipient preferences,
  quiet periods, and the existing notification runtime.
- Mention intents are excluded. Tasks own action, assignment, approval, and
  completion tracking.
- AI mention targets and commands are excluded. This does not redefine AI
  capabilities elsewhere in the platform.
- Existing plain `@name` text is not interpreted or backfilled.

## Scope and Non-Goals

The initial capability includes:

- structured mentions in authorized internal ticket, task, and project content
- permission-filtered individual and organizational-team selection
- immutable occurrence history and snapshot team expansion
- one consolidated recipient/object widget projection
- unread, read, unread-again, archive, and re-mention behavior
- exact-source deep links and unavailable-source fallback
- permission-safe, on-demand previews
- policy-driven email and Teams delivery
- tenant isolation, audit, accessibility, observability, and complete coverage
  across all supported internal source mutations

The following are intentionally out of scope:

- client-contact or portal-user mentions
- mention tokens in public or customer-visible content
- mention-specific groups separate from the organization directory
- mention intents such as FYI, Action Required, or Approval
- task creation, assignments, approvals, or completion state derived from a
  mention
- AI commands such as `@AI summarize` or `@AI investigate`
- a standalone Mentions page or second in-app notification entry
- interpreting plain text as a mention without a selected structured token
- backfilling alerts from historical content
- using a mention to grant or imply access

## Architecture

The feature uses a dedicated mention projection rather than treating mentions
as ordinary notification rows or folding all collaboration concepts into a
general relationship graph.

Source content owns structured mention tokens. The mention service converts
new tokens into immutable occurrences, snapshots their eligible recipients,
and updates one recipient/object projection for the dashboard widget. External
notification delivery consumes safe outbox events asynchronously.

This separation provides:

- stable target identity independent of display names
- exact evidence for direct and team targeting
- deterministic re-mention behavior
- one widget item instead of one row per occurrence
- no coupling to assignment, watcher, or task lifecycle
- external delivery without duplicating the in-app experience

## Domain Model

### Structured mention token

A structured token belongs to a particular revision of internal source content
and contains:

- an immutable token ID
- target type: `staff` or `team`
- target ID
- presentation label
- source anchor metadata required to render the token

The target type and ID are authoritative. The label is presentation data and
may be refreshed after a user or team rename. Plain typed or pasted text such
as `@Jane` is not a token and has no mention behavior.

### Mention occurrence

A mention occurrence is immutable evidence that a saved source revision added
a structured mention target. It records:

- immutable occurrence ID
- MSP and client scope
- parent object type and ID
- exact source type and source ID
- source revision or version
- author ID
- token ID
- target type and target ID
- audit-safe target display snapshot
- creation timestamp
- correlation and causation IDs

The occurrence does not copy the comment, note, description, or surrounding
preview text. Removing a token later does not remove its occurrence. Deleting
or redacting the source leaves the safe occurrence metadata intact.

### Mention recipient resolution

A recipient-resolution record snapshots one staff member evaluated for an
occurrence. It records:

- occurrence ID and candidate staff ID
- resolution path: direct, team, or both
- team IDs that contributed to the expansion
- outcome: eligible or excluded
- eligibility decision time and safe exclusion reason when applicable

Team expansion uses the membership and authorization state inside the source
save transaction. Future membership changes do not rewrite historical
resolutions. Only eligible resolutions produce recipient widget updates or
external notification plans.

When direct and team targets in one source mutation reach the same staff
member, the system retains both resolution paths but produces only one widget
update and one external notification for that mutation.

### Mention item

A mention item is the recipient's current dashboard projection. It is uniquely
keyed by:

- MSP
- recipient staff ID
- parent object type
- parent object ID

It stores:

- latest occurrence ID
- latest mention time
- state: unread, read, or archived
- read and archive timestamps where applicable
- last state actor
- optimistic version
- access-suppression metadata separate from user-selected state

The item does not store preview or source body text.

### State precedence

The following rules are authoritative:

1. A first mention creates an unread item.
2. Opening the authorized deep link marks the item read.
3. The recipient may explicitly mark it read, unread, or archived.
4. A later committed occurrence on the same parent object updates the latest
   occurrence and returns the item to unread, including from archive.
5. A state change using a stale expected version fails rather than overwriting
   a later occurrence.
6. A re-mention committed after a read or archive action wins because it is the
   newer version.

## Internal Authoring Experience

The shared internal editor exposes mention behavior in:

- ticket internal details, internal comments, and notes
- task internal details, internal comments, and notes
- project internal details, internal comments, and notes

Typing `@` opens an accessible picker with People and Teams sections. Search is
keyboard operable and returns only active targets that are eligible in the
current parent-object context.

The picker omits:

- the author
- inactive or disabled staff
- direct targets without current access
- teams with no eligible active members

Team results show eligible membership counts. When only part of a team is
eligible, the interface identifies the excluded members or count and requires
the author to confirm that partial delivery before submission.

The editor submits internal content and structured tokens together. The server
validates token integrity, source visibility, target scope, membership, and
authorization inside the source mutation transaction.

The mention service compares the previous and submitted token sets:

- unchanged token IDs produce no occurrence or notification
- newly inserted tokens produce occurrences
- removing a token preserves prior occurrences
- re-adding a removed target with a new token is a genuine re-mention
- repeating the same target in one save is deduplicated for recipient updates
  and external delivery

Source content and mention records commit atomically. A mention validation or
write failure prevents the source update from committing.

### Strict internal boundary

Public editors do not expose the mention picker. Public and outbound APIs reject
structured mention tokens even when a caller forges a valid internal target
ID.

Internal content containing active mention tokens cannot be converted to public
visibility until the tokens are removed. Rendering an internal token as plain
text and publishing it implicitly is not allowed.

## Permission and Team Resolution

A mention communicates attention but confers no authorization.

Creating one requires:

- permission to edit the internal source
- the internal mention creation capability
- an active internal staff principal

Candidate lookup and save-time validation enforce:

- exact MSP scope
- parent client scope
- parent ticket, task, or project visibility
- project visibility where the parent is project-bound
- active staff status
- current organizational-team membership
- target authorization to view the parent and exact internal source class

A direct ineligible target rejects the mutation with a target-specific
explanation. A partially eligible team mention succeeds for eligible members
after author confirmation and records the exclusions. A team with no eligible
members is rejected.

The existing organization directory is the source of mentionable teams.
Mention-specific groups are forbidden. If team membership persistence or
administration is incomplete, it is completed as an organization-directory
capability and reused by mentions and other team-aware platform features.

Every widget query, preview request, and deep-link resolution rechecks current
authorization. If a recipient later loses access:

- the widget item is immediately suppressed
- pending external delivery is canceled
- the deep link reveals no object or source content
- immutable audit history remains

Membership, object-visibility, and staff-status changes emit invalidation work
for affected projections. Read-time and pre-delivery authorization remain the
security backstops if that asynchronous invalidation has not completed.

Regaining access does not restore the old item. A new valid mention is required.
Disabling a staff account similarly makes its widget state inaccessible and
cancels pending delivery.

Mentioning a user never adds a work-record participant, watcher, collaborator,
reviewer, escalation recipient, owner, or assignee.

## Dashboard Widget

The Mentions widget is the complete in-app workflow. It provides:

- Unread, Read, and Archived filters
- counts for each filter
- newest-mention-first ordering
- bounded cursor pagination or incremental loading
- mark read
- mark unread
- archive
- open source

Each row presents:

- parent object type, stable display ID, and subject
- latest author
- latest mention timestamp
- direct or team origin
- a short sanitized preview when currently authorized and available

The preview is loaded from the exact source at render time, trimmed to a
bounded context around the token, sanitized for display, and never persisted
in the mention item or event. A preview failure falls back to safe parent
metadata without making the widget unavailable.

Archived items remain available under the Archived filter until ordinary
object and audit-retention rules remove the underlying evidence. There is no
separate search or inbox page.

## Deep Links

A mention link identifies the immutable occurrence, not only the parent object.
Resolution performs authorization before returning a canonical parent route
and exact source anchor.

For an available source, the destination:

1. opens the ticket, task, or project;
2. expands the relevant internal detail, comment, or note region;
3. scrolls the exact source into view;
4. gives the mention a brief accessible visual focus; and
5. marks the item read for that recipient.

If the token was removed but the source remains, the destination focuses the
source and explains that the mention was edited. If the source was deleted or
redacted, the destination opens the authorized parent object with a
source-unavailable notice and does not recover deleted text.

If a former internal source is later made public after all active mention
tokens are removed, the historical mention treats its exact source as
unavailable and provides no preview.

If current authorization fails, resolution returns the normal non-disclosing
authorization response, reveals no parent or source metadata, and suppresses
the widget item.

## Services and API Boundaries

### Mention service

One backend service owns:

- structured-token validation and diffing
- parent and source authorization
- candidate lookup
- direct-recipient validation
- team membership expansion
- exclusion reporting and confirmation
- occurrence and recipient persistence
- recipient/mutation deduplication
- mention-item projection
- access suppression
- audit and outbox events

Ticket, task, project, comment, and note services call it inside their existing
mutation transactions. There is no endpoint for creating an unanchored
occurrence.

### Source adapter

Each supported internal content family implements a common adapter for:

- resolving the parent object and tenant scope
- authorizing source reads and writes
- determining internal versus public visibility
- loading source revisions and structured tokens
- producing an authorized preview
- producing a canonical parent route and exact-source anchor

This contract prevents object-specific code from creating inconsistent mention
semantics.

### API surface

The HTTP surface provides:

- permission-filtered candidate lookup by parent and internal source type
- eligible and excluded team-member previews
- mention-aware fields on existing internal source mutations
- widget counts and cursor-paginated items by state
- versioned read, unread, and archive mutations
- occurrence-based deep-link resolution

State-changing requests require expected versions. Caller-supplied recipient,
eligibility, preview, and deep-link data are never trusted.

## Events and External Notification Delivery

A successful source transaction updates mention items and appends a safe
`mention.occurred` event to the transactional outbox. The event includes:

- tenant and parent-object identifiers
- occurrence identifier
- recipient identifiers
- author identifier
- source type
- correlation and causation identifiers

It excludes source bodies, preview text, attachments, and other internal-note
content.

The Mentions widget is the only in-app destination. The existing notification
planner handles configured email and Teams channels without creating a generic
in-app inbox row.

Each genuinely new mention or re-mention may create a new external delivery.
Delivery follows:

- organization mention-notification policies
- recipient channel preferences
- quiet periods
- named destination health
- existing idempotency, retry, lease, and bounded failure-history behavior

The dispatcher rechecks recipient status and authorization immediately before
sending. Revoked or disabled recipients are canceled. External delivery
failure never rolls back committed source content or widget state.

External payloads contain only minimal safe object metadata and an
authenticated occurrence link. They never contain internal source text or
widget previews.

## Errors and Concurrency

Expected domain failures include:

- direct recipient is inactive or unauthorized
- team has no eligible members
- partial-team confirmation is absent or stale
- candidate result became stale before save
- source visibility is not internal
- token is malformed or does not match the submitted source
- source or parent version is stale
- widget expected version is stale
- deep-link source is unavailable
- current access has been revoked

The API returns stable error codes and field-level details suitable for keeping
the author's unsaved content intact. Authorization failures remain
non-disclosing.

Source writes, occurrences, recipients, mention-item updates, audit records,
and outbox facts commit atomically. External delivery is deliberately outside
that transaction.

Optimistic locking protects source edits and widget state. Mutation-scoped
deduplication prevents retrying the same source request from producing another
occurrence, widget bump, or external delivery.

## Migration and Release

The feature ships as one complete release across all supported internal
ticket, task, and project surfaces.

Database changes are additive. Existing plain-text content remains readable and
unchanged. Historical strings resembling mentions are not parsed because they
do not contain stable target identities and could create false alerts.

New or edited internal sources use the structured-token contract. Public
surfaces remain plain and reject tokens.

Release readiness requires:

- completed organizational-team membership persistence and administration
- every supported source adapter
- the shared editor and picker on every supported internal surface
- the complete widget state model
- exact-source deep links
- external email and Teams delivery
- authorization, audit, telemetry, and documentation

A partially connected source surface is a release blocker because it would
create inconsistent mention behavior.

## Observability and Audit

Metrics and structured logs cover:

- direct and team occurrences created
- eligible expansions and exclusions
- direct-plus-team deduplication
- widget creation and state transitions
- re-mentions and archive restoration
- candidate and save-time authorization failures
- access suppression
- preview availability and authorization failures
- deep-link outcomes
- planned, suppressed, delivered, retried, and failed external notifications

Metrics, logs, traces, and events contain identifiers and safe enums only. They
never contain source text, preview text, or token-adjacent content.

Audited actions include:

- occurrence creation
- team membership snapshot and exclusions
- recipient resolution paths
- widget state mutation
- access suppression
- external notification planning and delivery outcome

## Verification Strategy

### Domain and persistence

Tests verify:

- structured-token validation and prior/new diffing
- stable identity across user and team renames
- immutable occurrences after token removal or source deletion
- one mention item per recipient and parent object
- first-mention and re-mention state transitions
- optimistic concurrency and stale state rejection
- mutation idempotency
- direct, team, and combined resolution history

### Authorization and isolation

Tests cover:

- internal-staff-only creation and receipt
- public and outbound token rejection
- direct-recipient authorization
- partial and empty team expansion
- MSP, client, project, object, and internal-source isolation
- forged and cross-tenant identifiers
- access revocation before widget read, preview, deep-link resolution, and
  external delivery
- disabled authors and recipients
- prevention of participant, watcher, owner, and assignee side effects

### Service integration

Contract tests prove that every supported ticket, task, project, internal
comment, and note mutation invokes the shared mention service and commits
source and mention state atomically.

Deep-link tests cover internal details, comments, notes, removed tokens, deleted
sources, redaction, unauthorized parents, and non-disclosing failures.

Notification tests cover:

- no duplicate generic in-app notification
- one delivery for direct-plus-team overlap in one mutation
- a new delivery for a genuine re-mention
- quiet periods and recipient preferences
- pre-delivery authorization cancellation
- retry idempotency and destination failure
- absence of source content in events and external payloads

### Frontend and accessibility

Tests verify:

- keyboard and screen-reader operation of the People and Teams picker
- plain `@name` text remaining inert
- author, inactive staff, and ineligible target filtering
- partial-team warning and confirmation
- strict internal/public editor boundaries
- preservation of unsaved content after validation errors
- unread, read, and archived counts and filters
- manual state controls and stale-version recovery
- re-mention restoration
- bounded loading
- authorized preview rendering
- exact-source focus and accessible highlight
- unavailable-source and revoked-access behavior

## Documentation Impact

Implementation updates:

- the permission matrix and client-isolation test model
- REST API contracts
- canonical event schemas
- notification policy and delivery documentation
- organization directory and team-membership contracts
- ticket, task, project, comment, and note module documentation
- navigation and stable deep-link guidance
- AI documentation to record that AI mention targets and commands remain
  excluded from this capability
- operational metrics, dashboards, and support diagnostics

## Acceptance Criteria

The design is satisfied when:

- an authorized internal author can mention an eligible staff member or
  organizational team from every supported internal content surface;
- public and customer-visible content cannot create or carry mention tokens;
- team expansion snapshots current eligible members and reports exclusions;
- each recipient sees one widget item per parent object;
- re-mentions update the latest source and restore unread state from read or
  archive;
- the widget provides the complete unread/read/archive workflow without a
  separate inbox or duplicate in-app notification;
- deep links authorize and focus the exact source or safely fall back when the
  source is unavailable;
- current access loss immediately suppresses the item and cancels pending
  delivery without erasing audit history;
- email and Teams alerts use the existing reliable notification runtime and
  carry no internal source content;
- mentions do not change assignments, participants, watchers, tasks, or
  permissions; and
- all supported source mutations share one tested, tenant-safe mention
  capability.
