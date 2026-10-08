# Workforce Time Review Design

**Date:** 2026-08-04

**Status:** Approved design

**Scope:** Labor roles, ticket timers, weekly timesheets, reviewer authority, and time-entry amendments

## Goal

Rarity will let technicians capture time against individual tickets, include stopped timer time with a ticket note or email, and manage a weekly timesheet spanning authorized tickets and project tasks. Time Reviewers can inspect, reasonedly amend, approve, or reject pending time without weakening audit or billing history.

## Decisions

- A timer belongs to one ticket. Entering a ticket exposes its start/stop control and accumulated stopped sessions.
- A stopped timer produces a reusable server-owned time-capture object. Submitting that object with a ticket note, time entry, or outbound email creates one time entry from the captured interval without asking the browser to recalculate duration.
- Timer state is ticket-specific. Rarity does not impose a technician-wide single-timer lock.
- Labor roles are MSP-scoped work categories, separate from authorization roles.
- A labor role carries effective-dated internal cost and bill rates. A technician-specific internal cost rate remains an optional override.
- Every time entry snapshots the labor-role version, internal cost rate, and bill rate used when the entry is created.
- Technicians may edit their own pending entries. Reviewers may make reasoned amendments to pending or rejected entries.
- Approved entries are immutable. Corrections use an auditable reversal and replacement.
- Weekly timesheets use the MSP timezone and begin Monday.
- The Time Reviewer authorization role can be assigned MSP-wide or within Client scope.

## Domain boundaries

### Authorization roles

Authorization roles answer what a principal may do. Role management gains create-role and assignment operations so administrators can assign a `time_reviewer` system role. The role receives time review capabilities but does not imply a labor category or billing rate.

### Labor roles and rates

Labor roles answer what kind of work was performed. Each role has a stable identity and effective-dated versions containing a name, active state, internal-cost rate, bill rate, and currency. Versions are append-only after use.

When a time entry is created, the server resolves the effective labor-role version and optional technician cost override at the entry start time. The entry stores the resolved role version and rate snapshots. Later rate changes cannot rewrite project actuals, approval decisions, or billing exports.

### Ticket timers

A timer session has `running`, `stopped`, `consumed`, or `discarded` state and belongs to one MSP, Client, ticket, and technician. Starting a timer is an idempotent, audited mutation. A ticket cannot have more than one running session for the same technician, but a technician may have running sessions on different tickets.

Stopping a timer records the server stop time and exact duration. The browser may display an advancing clock, but server timestamps are authoritative. A stopped capture may be consumed once. Concurrent submissions use optimistic version and idempotency checks so one interval cannot create duplicate time entries.

The ticket note and email commands accept an optional stopped capture identity and labor role. Their existing application services coordinate with the time-entry service in the same trusted mutation boundary. Failure rolls back the note/email and time-entry mutation together when a shared transaction is available; otherwise the API rejects combined submission until the repository can guarantee atomicity.

### Weekly timesheets

The weekly view is calculated for one technician and MSP week, then filtered to Client records the requesting principal may read. It groups entries by day, Client, ticket or project task, and labor role, with billable and nonbillable totals.

Pending entries can be amended in place with optimistic versioning and complete before/after audit evidence. Rejected entries can be replaced. Approved entries are immutable; reversal records preserve the original entry and approval evidence before a replacement is created.

### Review and approval

The baseline `time_reviewer` role contains:

- `time_entry.read_scoped`
- `time_entry.amend`
- `time_entry.approve`
- `timesheet.review`

Technician time capture continues to use `time_entry.create`; own-week visibility uses `timesheet.read_own`; pending self-edit uses `time_entry.update_own`. Existing export permission remains separate.

Every review mutation requires a non-empty reason, current object version, live authorization, audit record, and outbox event.

## API shape

Versioned `/api/v1` routes cover:

- labor-role list, create, version, enable, and disable;
- ticket timer get, start, stop, discard, and consume;
- weekly own-timesheet read;
- scoped reviewer-timesheet read;
- pending time-entry amendment;
- approved time-entry reversal and replacement;
- authorization-role creation and assignment.

Ticket note and email request bodies accept a `time_capture` object containing capture ID, expected version, labor-role ID, billable state, and idempotency key. Raw browser-provided start, end, duration, or rates are not trusted when a capture ID is present.

## GUI

The ticket workspace places the timer beside the existing ticket actions. Starting and stopping does not navigate away. A stopped duration appears as a composable object in the note/email editor, where the technician selects labor role and billable state before submission.

The Timesheet page shows the current MSP week by default, daily totals, weekly totals, ticket/task links, labor role, approval state, and validation errors. Technicians can correct pending entries. Reviewers get a scoped queue with before/after amendment evidence and approve/reject actions.

Directory settings gains labor-role and effective-rate management. Role settings gains role creation and assignment without mixing labor roles into access control.

## Failure handling

- A stopped or consumed capture cannot be stopped again.
- A consumed capture cannot be reused.
- A stale capture, time entry, role, or rate version returns a version conflict.
- A disabled labor role remains readable for historical entries but cannot be newly selected.
- A timer whose ticket or Client access is lost remains inaccessible until an authorized reviewer resolves it.
- Duration must be positive and within configured maximum bounds.
- Approved or exported time cannot be edited in place.
- Timezone and week boundaries are computed server-side from MSP settings.

## Verification

- Domain tests cover ticket-specific concurrent timers, idempotent start/stop, single consumption, server-owned duration, role/rate resolution, snapshot immutability, self-edit scope, reviewer scope, and approved-entry reversal.
- Repository tests cover uniqueness, optimistic versions, effective dating, and atomic time/audit/outbox persistence.
- HTTP tests prove trusted-principal scope and reject browser-supplied duration/rates for stopped captures.
- Frontend tests cover ticket start/stop, stopped-time objects in note/email composition, weekly totals, pending edits, reviewer amendments, and approval state.
- PostgreSQL acceptance proves concurrent stop/consume behavior and immutable historical rates.

## Explicit exclusions

- Payroll calculation or payroll-provider export.
- Automatic billing rounding during timer capture.
- Treating labor roles as authorization roles.
- Rewriting approved or exported time in place.
- Browser-authoritative duration or rates.

