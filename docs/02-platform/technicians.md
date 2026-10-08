# Technicians

**Status:** Draft

Technicians are internal MSP users with identity links, permissions, skills,
schedules, availability, workload, and memberships. They may participate in
multiple departments, teams, and queues simultaneously.

Authorized MSP-wide organization administrators define exact dated
availability windows with an available-minute total. Overlapping windows for
the same technician are rejected. Project capacity uses these settings without
exposing the technician's underlying assignments or Client records across
tenant boundaries.

The same MSP-wide administration surface appends effective-dated internal
hourly labor-cost rates. Rates are immutable once written. Project financial
reporting selects the latest rate effective when each delivery Time Entry
started; if captured time lacks a rate, actual labor and recognized profit are
explicitly marked incomplete rather than displayed as zero.

Ticket participation is separate from platform permission. An owner is accountable; collaborators work; reviewers approve; escalation engineers participate temporarily; watchers observe. All assignment and ownership changes are audited.

Technician participation is persisted as versioned, effective history rather than a mutable ID list. Adding or removing a participant versions the Work Record, checks that the technician is active in the same MSP, and writes audit/outbox evidence atomically; removing participation requires a reason.

## Calendar workspace

See the [unified calendar contract](unified-calendar.md) for shared scheduling, typed source forms, date semantics, capacity, and current implementation evidence.
