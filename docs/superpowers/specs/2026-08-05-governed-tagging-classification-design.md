# Governed Tagging and Classification Design

**Status:** Approved in design review on 2026-08-05; awaiting written-spec review

## Goal

Create one governed classification capability for every first-class Rarity
object. The primary outcome is faster, more accurate technician
classification. The same model must also support reporting, automation,
inheritance, migration, and controlled AI application in the initial release.

The experience must keep three concepts distinct:

- **Type** answers “What is it?” Examples include Incident, Request, Problem,
  and Change.
- **Status** answers “Where is it?” Examples include New, In Progress, Waiting
  for Customer, and Waiting for Vendor.
- **Tags** answer “What is it about?” Examples include VPN, M365, Cisco,
  Executive, and Billing.

Tags replace Category completely. Type and Status remain first-class fields and
are never inferred from, stored as, or presented as tags.

## Approved Product Decisions

- The catalog is global to the MSP. There are no client-specific, team-specific,
  or technician-created tags.
- Administrators create and maintain tags. Technicians select existing tags.
- Every tag is available on every supported object type.
- Tickets, tasks, projects, assets, documentation, and time entries support
  tags through the same platform capability.
- Every object must have at least one effective tag.
- Automatically created objects fall back to the system-managed `Unclassified`
  tag rather than failing intake.
- An object cannot reach a terminal state when its only classification is
  `Unclassified` or archived.
- Tags have globally unique visible labels, immutable identities, and hidden
  internal namespaces.
- Administrators organize tags into groups. Each tag belongs to exactly one
  group, but groups do not restrict where a tag can be used.
- Tasks dynamically inherit project tags and may also have direct tags.
- AI always provides suggestions and administrators may explicitly enable
  automatic application above a configured high-confidence threshold.
- AI does not silently remove human-selected tags.
- Administrators clean up the catalog by renaming, merging, and archiving
  rather than permanently deleting tags.
- Existing Category values receive a complete migration to tags. No temporary
  Category compatibility layer remains after cutover.
- Reporting, automation, and AI are complete parts of the release rather than
  deferred phases.

## Scope and Non-Goals

The initial capability includes:

- global tag and group administration
- direct and inherited tag assignment
- interactive, bulk, automated, integration, and AI classification
- minimum-classification and terminal-transition enforcement
- tag-driven search, reporting, trends, and recurring-issue analysis
- tag conditions, triggers, and actions in automation
- Category preflight, migration, verification, and removal
- audit, authorization, client isolation, accessibility, and operational health

The following are intentionally out of scope:

- client-local or team-local tag catalogs
- free-form technician tag creation
- different tag applicability rules for different object types
- hierarchical parent/child tag semantics
- tags that replace Type, Status, Queue, Priority, or ownership
- a post-cutover Category field or Category translation API

Groups organize the picker and reporting interface; they are not mandatory
classification slots. The system requires one meaningful tag, not one tag from
each group.

## Domain Model

### Tag groups

A tag group has:

- an immutable ID
- a unique display name
- a description
- an administrator-controlled display order
- active or archived state
- audit metadata

Archiving a group does not archive or detach its tags. An administrator must
move active tags to another active group before the group can be archived.

### Tags

A tag has:

- an immutable ID used by associations, reports, automations, and integrations
- an immutable hidden namespaced key used by internal contracts
- a globally unique, case-insensitive display label
- exactly one current group
- optional administrator-managed search synonyms
- active, merged, or archived lifecycle state
- creation, rename, group-move, merge, and archive audit history

The internal namespace is independent of the mutable display group. For
example, a stable key such as `taxonomy.core.cisco` may display as `Cisco` in
the Technology group. Moving the tag to another display group does not change
its key or break consumers.

Visible labels are unique after whitespace and case normalization. Search
synonyms must not create an exact-match ambiguity between active tags. Renaming
a tag changes its presentation without changing its identity or historical
reporting.

Merged tags retain a redirect to the surviving tag. A merge moves active
associations and future references to the survivor while preserving the
retired identity in audit and historical evidence.

Archived tags remain attached to historical records and remain resolvable in
reports. They cannot be selected for new assignments. Permanent tag deletion
is not supported.

### Associations and effective tags

Each direct tag association records:

- target object type and immutable object ID
- tag ID
- assignment source
- actor or system process
- timestamp
- correlation and causation IDs
- optional AI suggestion or automation evidence

Assignment sources are:

- human
- AI confirmed by a human
- AI automatically applied
- automation
- integration
- migration
- system fallback

Inheritance is not stored as copied direct associations. A task's effective tag
set is the union of:

- active direct task tags
- active tags currently assigned to its project

The resolver retains the source of each effective tag so the UI and reports can
distinguish direct from inherited classification. Duplicate direct and
inherited identities appear once with both sources.

An active, non-merged tag other than `Unclassified` is meaningful.
`Unclassified` and archived associations remain visible but do not satisfy the
terminal-state requirement. Archived associations are excluded from the
effective set. If a catalog lifecycle change would leave an active object with
no effective tag, the operation attaches `Unclassified` before the archive
becomes active. When a meaningful tag is added to an object, `Unclassified` is
removed atomically.

## Governance and Administration

Administrators use a dedicated Classification workspace with five areas:

1. **Catalog** — search, usage, lifecycle, rename, group move, merge, and
   archive.
2. **Groups** — names, descriptions, ordering, and lifecycle.
3. **AI policy** — automatic-application enablement, confidence threshold, and
   recent outcomes.
4. **Classification health** — `Unclassified`, archive-caused fallback, stale,
   and low-confidence populations.
5. **Migration history** — Category preflight, decisions, execution evidence,
   and verification.

The catalog shows label, group, state, total authorized usage, recent trend,
and last administrative change. Administrators may add search synonyms without
changing the visible label.

Potentially disruptive actions require an impact preview:

- Rename lists affected saved views, reports, and automation presentations.
  Identity-based consumers continue working.
- Merge lists association counts, saved views, reports, and automations that
  will resolve to the survivor.
- Archive lists active objects that will receive `Unclassified` because their
  last meaningful tag is being retired.
- Group move shows picker and reporting organization changes without implying
  applicability changes.

Bulk replacement is available before archive. If an administrator proceeds
without replacement, the controlled archive operation attaches `Unclassified`
to each affected active object before retiring the tag. Rejected or incomplete
administrative changes leave the tag active and retain the preview as evidence
for correction.

## Technician Experience

All supported object workspaces use one shared tag picker. It provides:

- visible label search
- synonym search
- group filters and grouped results
- recent and frequently used tags
- AI suggestions above the full catalog
- keyboard-first multi-selection
- direct versus inherited source indicators
- bulk add and bulk remove
- inline minimum-classification validation

Technicians may add or remove direct tags when they can edit the target object.
They cannot create, rename, merge, archive, or move tags. An inherited tag
cannot be removed from a task; its source indicator links to the project where
the classification can be changed by an authorized user.

Interactive creation requires at least one active tag before submission.
Automated intake, integrations, APIs, and automation use `Unclassified` when
they cannot produce a meaningful valid tag. This preserves intake while making
the classification debt visible.

Terminal actions such as resolve, complete, approve, or close validate the
effective tag set. When only `Unclassified` or archived associations remain,
the action is rejected and focus moves to the tag picker with a plain-language
explanation. User input for the attempted action is preserved.

Every tag mutation appears in the activity timeline with its source, actor,
time, and concise before/after meaning. Inheritance changes identify the source
project rather than appearing as unexplained task edits.

## AI Classification

The classification service evaluates only content and context available to its
authorized initiating user or configured system process. It returns:

- existing tag IDs
- confidence scores
- short human-readable rationales
- model and policy evidence required for audit

Suggestions never create catalog entries. Technicians may accept or dismiss
suggestions individually or in bulk. Those outcomes become feedback evidence
for evaluation and future tuning.

Automatic application is disabled until an administrator explicitly enables
it and chooses a high-confidence threshold. Above that threshold, AI may add
one or more active tags and remove `Unclassified` after a meaningful assignment
commits. It cannot automatically remove a human-selected tag.

Below the threshold, suggestions require confirmation. AI timeouts, provider
failures, invalid tag identities, or low confidence never block object
creation. The object remains manually classifiable and receives
`Unclassified` when no meaningful classification exists.

Classification health distinguishes:

- suggestion acceptance and dismissal rates
- automatic tags retained or later changed by technicians
- time spent in `Unclassified`
- confidence calibration by tag and group
- model or provider failures

These measures describe operational accuracy without treating AI confidence as
ground truth.

## Reporting and Trend Discovery

Tags are durable reporting dimensions. Authorized users can analyze:

- one tag or group
- any, all, or none of a tag set
- tag combinations and co-occurrence
- object type, client, technician, team, priority, status, and time range
- direct, inherited, human, AI, automation, integration, migration, or all
  effective assignments
- adoption, replacement, and classification-health trends
- `Unclassified` volume, age, and responsible queue
- recurring issues by client, technology, period, and tag combination

Operational reports use effective tags by default. A source filter separates
direct and inherited classification. Archived and merged identities resolve
through their immutable history so rename, group move, or cleanup does not
create artificial trend breaks.

Every aggregate and drill-down enforces object visibility. A global tag's usage
count never reveals the existence or volume of records outside the requesting
user's authorized scope.

Recurring-issue analysis reports the evidence behind a pattern: matching
objects, interval, clients, and tag combination. It does not silently create a
Problem or change another record. Users may explicitly launch an authorized
workflow from the evidence.

## Automation

Automation conditions and triggers include:

- tag added, removed, or replaced
- object has any, all, or none of a tag set
- object has an active tag from a group
- `Unclassified` persists beyond a configured duration
- a tag or combination recurs within a client and time window
- assignment source is human, AI, inheritance, integration, migration, or
  automation

Automation actions may:

- add or remove direct tags
- replace `Unclassified`
- assign work
- change priority
- notify authorized recipients
- launch a workflow
- request AI analysis

An action cannot remove an inherited tag, assign an archived tag, or leave the
object without an effective tag. Automation uses immutable tag IDs, while its
builder shows current labels and merge state.

Every event and action carries idempotency, correlation, and causation
identifiers. The engine records tags changed within the correlation chain,
prevents a rule from repeatedly applying the same mutation, caps chained rule
depth, and surfaces circular or stopped executions in automation health.

## Architecture and Data Flow

The capability is divided into bounded platform units:

- **Catalog service** owns groups, tag identity, global uniqueness, synonyms,
  lifecycle, merge redirects, and AI policy.
- **Association service** owns direct assignments, validation, authorization,
  source attribution, audit, and mutation events.
- **Effective-tag resolver** combines canonical direct associations with
  project-derived task tags.
- **Classification service** owns AI suggestions, confidence policy, automatic
  application, and feedback evidence.
- **Reporting projection** owns optimized usage, combination, trend, recurrence,
  and classification-health reads.
- **Automation integration** consumes durable tag events and submits guarded
  mutations through the association service.
- **Migration service** owns Category preflight, conversion, verification, and
  cutover evidence.

A mutation follows one authoritative path:

1. Resolve and authorize the target object.
2. Resolve merged identities and validate that requested tags are active.
3. Apply additions and removals to a proposed direct-tag set.
4. Resolve the resulting effective set and enforce classification invariants.
5. Persist associations, object version, audit evidence, and outbox events
   atomically.
6. Return the canonical direct and effective tags to the caller.
7. Let reporting, search, notification, and automation projections consume the
   durable events.

Operational reads resolve canonical direct and inherited tags immediately.
Analytics projections may update asynchronously and disclose their freshness;
projection lag never changes operational validation.

Shared UI components include `TagPicker`, `TagChip`, `TagGroupFilter`,
`AISuggestion`, and `ClassificationStatus`. Feature modules provide an object
reference, current tags, version, and permissions. They do not reimplement tag
invariants.

## Authorization and Isolation

Catalog mutation requires an administrative classification capability. Object
tag mutation requires edit permission for the exact target object. Project-tag
changes affect task inheritance only where the underlying project relationship
exists; they grant no new access.

Search, reports, usage counts, automation, and AI enforce the same MSP, client,
object, field, and content-visibility rules as the underlying object. The
global catalog is discoverable to authenticated users who may classify work,
but tag discovery does not disclose tagged records.

System processes use explicit scoped identities. Automatic AI application and
automation cannot use broader target access than their configured execution
scope. Every accepted or rejected administrative and object-level mutation
produces audit evidence without logging restricted object content.

## Category Migration

Migration is a deliberate full cutover:

1. Inventory every Category definition, stored value, reference, filter,
   report, automation, import, export, API field, UI control, and document.
2. Normalize casing and whitespace and propose globally unique destination
   tags and groups.
3. Identify collisions, blank values, invalid values, and semantically
   ambiguous mappings.
4. Require an administrator to resolve every ambiguity in preflight.
5. Create or reuse destination tags and attach them with `migration` source
   evidence.
6. Assign `Unclassified` where no meaningful Category existed.
7. Verify association counts, object coverage, report continuity, automation
   references, and client isolation.
8. Remove Category from schemas, storage, APIs, SDKs, imports, exports,
   automations, reports, UI, tests, and documentation.
9. Block release if a runtime Category contract or unmigrated object remains.

The migration preserves the original Category-to-tag decision map and execution
evidence for audit. It does not preserve a callable or user-facing Category
compatibility mechanism.

## Failure and Conflict Handling

- A stale object version returns a readable comparison of submitted and current
  direct tags. The user may reload and reapply permitted changes.
- A requested merged tag resolves to the active survivor and reports that
  resolution. A requested archived tag is rejected with an active replacement
  search.
- Permission loss preserves the user's attempted selection in the interface
  but commits nothing.
- AI failure falls back to manual selection or `Unclassified` and records
  provider health without exposing sensitive context.
- Reporting projection lag displays an as-of timestamp and does not affect
  picker, transition, or inheritance correctness.
- Automation cycles stop with rule, correlation, and recovery evidence.
- A tag archive or merge conflict leaves the catalog unchanged and refreshes
  its impact preview.
- Migration ambiguity or incomplete verification blocks cutover with a
  resolvable report. It never guesses a destination.

## Accessibility

- The picker has a persistent label, searchable listbox semantics, group
  announcements, and complete keyboard operation.
- Tag chips expose label, source, and permitted removal actions to assistive
  technology.
- Inherited tags explain their project source without relying on color.
- AI suggestions announce confidence and rationale without making confidence
  the only acceptance cue.
- Minimum-classification and terminal-transition failures move focus to the
  recovery control and use an announced validation message.
- Bulk selection, merge previews, and automation builders have non-drag,
  keyboard-accessible interactions.
- Status, source, archived state, and `Unclassified` never rely on color alone.

## Verification

### Domain and service tests

- global, case-insensitive label uniqueness
- exactly one active group per active tag
- synonym ambiguity prevention
- rename identity stability
- merge redirects and preserved historical evidence
- archive fallback and terminal blocking
- at least one effective tag on every supported object
- automatic `Unclassified` assignment and meaningful-tag replacement
- direct and dynamically inherited task tags
- duplicate direct/inherited source resolution
- optimistic concurrency and atomic audit/outbox behavior

### Authorization and integration tests

- MSP and client isolation for associations and aggregates
- object-level edit enforcement
- global catalog visibility without record disclosure
- scoped AI context and automatic application
- automation identity, loop prevention, and idempotency
- immediate operational inheritance with eventually consistent reporting
- archived and merged tag behavior across search, reports, and automations

### AI and reporting tests

- suggestion ordering, rationale, acceptance, and dismissal
- administrator enablement and high-confidence threshold enforcement
- no automatic removal of human-selected tags
- manual fallback and `Unclassified` on AI failure
- any/all/none filters, combinations, trends, recurrence, and drill-down
- direct versus inherited and assignment-source filters
- rename, merge, and archive continuity in historical reports

### Migration tests

- normalization and collision preflight
- administrator resolution of ambiguous mappings
- complete Category-to-tag association counts
- `Unclassified` fallback for missing values
- rollback before cutover and no partial committed cutover
- source and runtime scans proving complete Category removal

### User-experience tests

- keyboard-only create, search, select, and remove
- bulk classification and minimum-tag validation
- automated intake triage
- AI suggestion confirmation and automatic application
- project-to-task propagation and source navigation
- terminal-transition recovery
- report drill-down with preserved filters
- administrator rename, merge, archive, and impact previews
- screen-reader announcements, focus recovery, zoom, forced colors, and reduced
  motion

## Acceptance Criteria

The feature is complete when:

- every supported object uses the shared platform tagging capability
- technicians cannot create tags and administrators can govern the full global
  catalog
- every active object has a valid classification state
- `Unclassified` preserves automated intake but cannot pass a terminal
  transition
- project tag changes appear immediately on tasks without copied inherited
  associations
- controlled AI suggestion and automatic high-confidence application work with
  complete evidence
- tag combinations, trends, recurring issues, and source-aware reporting are
  available under authorization
- tag-driven automation cannot violate classification or create mutation loops
- Category data is migrated and every Category runtime contract is removed
- all authorization, audit, migration, accessibility, and verification
  requirements pass
