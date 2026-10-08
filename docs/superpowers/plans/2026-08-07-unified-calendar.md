# Unified Calendar Implementation Plan

**Execution status (2026-09-04):** Backend and browser tasks 1–14 have source implementations. Task 15 verification and external gates are recorded in the [current execution backlog](../../09-roadmap/current-execution.md). Original checkboxes and example code are historical and must be checked against the deployed contracts before use.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver one Rarity-native, cross-client calendar engine and workspace for typed dated objects, scheduling, recurrence, dependencies, capacity, deterministic health, notifications, and human-approved AI recommendations.

**Architecture:** Authoritative domain objects publish versioned event roles through transactional outbox records into an idempotent normalized calendar projection. All calendar lenses query that permission-filtered projection; scheduling uses revision-bound preview/apply proposals that delegate writes to source adapters and commit correlated source changes, audit evidence, and outbox events atomically. Recurrence, dependency, availability, capacity, health, privacy, and notifications are independent calendar services over the shared model.

**Tech Stack:** Go 1.25 with the Go 1.26.5 toolchain, PostgreSQL, Goose, pgx/v5, `net/http`, React 19, TypeScript 7, Vite 8, Vitest, Testing Library, Playwright, axe-core, and Node.js 22.

## Global Constraints

- The source domain object owns every authoritative date, status, permission,
  and business rule; calendar projections are rebuildable read models.
- Every event resolves to a typed source object and exact event role. Generic
  free-form calendar events are forbidden.
- All built-in typed date fields project automatically. Configured custom date
  fields preserve date versus date-time semantics.
- Calendar reads default to all and only clients the principal is authorized to
  access. Do not inherit the active client selector as a hidden restriction.
- Built-in lenses are exactly Day, Week, Month, Timeline, Capacity, and Agenda.
- Date-only values remain all-day. Never invent a default time.
- Timed and recurring values retain an IANA timezone and preserve intended
  local wall-clock time across daylight-saving changes.
- Capacity is consumed only by assigned, explicitly scheduled, capacity-bearing
  work with positive planned effort.
- Scheduling modes are `fixed_block`, `effort_allocation`, and
  `informational`.
- Dependency types are `finish_to_start`, `start_to_start`, and
  `finish_to_finish`, with lead or lag. Reject `start_to_finish`.
- Direct dependencies remain inside one client. MSP-wide operations are shared
  constraints, not cross-client dependency edges.
- Cascades, recurrence edits, conflict overrides, and AI recommendations require
  an authorized human preview and approval.
- Ordinary overbooking is overrideable only with a reason. Administrator
  policies may make approved PTO, non-working time, and protected maintenance
  hard blocks.
- Health is deterministic, explainable, and ordered `blocked`, `overdue`,
  `at_risk`, then `on_track`. AI never assigns health.
- Scheduling authorization requires source edit permission and workforce
  authority over every affected assignee.
- Shared lenses never grant source access. Counts, capacity, dependencies,
  notifications, and live updates must not leak inaccessible source details.
- External calendar synchronization and legacy event backfill are excluded.
- Do not add a frontend or backend runtime dependency without explicit user
  permission.
- Node.js 22 is required for frontend and documentation commands.

## Execution Prerequisites and Migration Reservation

Execute the three approved plans in this order:

1. `2026-08-05-governed-tagging-classification.md`, implemented in migrations
   `000080` through `000087`.
2. `2026-08-06-internal-mentions-collaboration.md`, implemented in migrations
   `000088` through `000090`.
3. This plan, reserving migrations `000091`, `000092`, and `000093`.

The classification plan provides the tag filter and tag projection dimensions.
The mentions plan provides durable organizational-team memberships reused by
workforce scope and shared-lens audiences. If execution order changes, renumber
all unexecuted migration references together before writing SQL.

---

## File and Responsibility Map

### Calendar core

- `backend/internal/calendar/model.go` — stable event, source, occurrence,
  filter, privacy, and scheduling types.
- `backend/internal/calendar/registry.go` — event-role definitions and source
  adapter registry.
- `backend/internal/calendar/recurrence.go` — structured recurrence validation,
  expansion, occurrence identity, and exceptions.
- `backend/internal/calendar/projection.go` — revision-aware projection
  application and deletion.
- `backend/internal/calendar/reconciliation.go` — missing, stale, and orphaned
  projection detection and repair.
- `backend/internal/calendar/dependencies.go` — dependency validation, graph
  traversal, constraint calculation, and cascade impact.
- `backend/internal/calendar/availability.go` — recurring workforce schedule
  resolution and dated exceptions.
- `backend/internal/calendar/capacity.go` — fixed-block and effort-allocation
  capacity calculation.
- `backend/internal/calendar/conflicts.go` — warning, overrideable, and hard
  constraint evaluation.
- `backend/internal/calendar/health.go` — deterministic health rules and reason
  codes.
- `backend/internal/calendar/proposals.go` — preview, revision binding,
  authorization, approval, and atomic cascade orchestration.
- `backend/internal/calendar/query.go` — windowed events, occurrence expansion,
  filters, summaries, and Agenda pagination.
- `backend/internal/calendar/privacy.go` — source visibility and privacy-safe
  busy blocks.
- `backend/internal/calendar/live.go` — cursor-based calendar update feed.

### Typed source domains

- `backend/internal/projects/milestones.go` — first-class project milestone
  commands and rules.
- `backend/internal/workforce/schedules.go` — recurring technician schedules
  and dated exceptions.
- `backend/internal/workforce/pto.go` — PTO workflow and manager/admin approval.
- `backend/internal/commitments/maintenance.go` — maintenance windows and
  affected-resource scopes.
- `backend/internal/commitments/commercial.go` — renewal and license records.
- `backend/internal/calendar/adapters/*.go` — projection and scheduling
  adapters for work records, tasks, projects, milestones, SLA commitments,
  workforce records, maintenance, commercial commitments, and custom dates.

### Persistence and migrations

- `backend/migrations/000091_unified_calendar_core.sql` — projections,
  recurrence exceptions, dependencies, proposals, custom date configuration,
  live cursors, and capabilities.
- `backend/migrations/000092_workforce_scheduling.sql` — recurring schedules,
  schedule exceptions, PTO, approvals, and conflict policies.
- `backend/migrations/000093_calendar_commitments.sql` — milestones,
  maintenance scopes, renewals, and licenses.
- `backend/internal/store/psa/calendar_repository.go` — projections, queries,
  proposal unit of work, dependencies, live updates, and reconciliation.
- `backend/internal/store/psa/workforce_repository.go` — schedules, exceptions,
  PTO, and workforce scope.
- `backend/internal/store/psa/commitment_repository.go` — milestone,
  maintenance, renewal, and license persistence.

### HTTP, composition, notifications, and AI

- `backend/internal/httpapi/calendar_routes.go` — event, capacity, dependency,
  proposal, and live-feed endpoints.
- `backend/internal/httpapi/workforce_schedule_routes.go` — schedules,
  exceptions, PTO, and approval endpoints.
- `backend/internal/httpapi/commitment_routes.go` — milestone, maintenance,
  renewal, and license endpoints.
- `backend/internal/httpapi/router.go` — action interfaces, dependencies, and
  route registration.
- `backend/internal/notifications/calendar.go` — schedule and commitment
  notification planning.
- `backend/internal/aiassist/calendar_recommendations.go` — structured,
  recommendation-only scheduling proposals.
- `backend/cmd/rarity-api/main.go` — service, worker, planner, and route wiring.
- `backend/cmd/rarity-admin/main.go` — projection reconciliation and demo-seed
  commands.

### Frontend

- `frontend/src/features/calendar/types.ts` — browser event, filter, capacity,
  dependency, proposal, and typed-record contracts.
- `frontend/src/features/calendar/api.ts` — calendar, proposal, workforce,
  commitment, and live-update requests.
- `frontend/src/features/calendar/useCalendarWorkspace.ts` — shared lens,
  filter, query, selection, and update state.
- `frontend/src/features/calendar/CalendarPage.tsx` — workspace shell and
  common controls.
- `frontend/src/features/calendar/CalendarFilters.tsx` — authorized filters and
  saved-lens controls.
- `frontend/src/features/calendar/DayWeekGrid.tsx` — Day and Week resource grid.
- `frontend/src/features/calendar/MonthGrid.tsx` — Month density and event view.
- `frontend/src/features/calendar/AgendaView.tsx` — chronological paginated
  view.
- `frontend/src/features/calendar/TimelineView.tsx` — range and dependency view.
- `frontend/src/features/calendar/CapacityView.tsx` — availability and
  utilization view.
- `frontend/src/features/calendar/EventDetailsPanel.tsx` — source context,
  health, timezone, and navigation.
- `frontend/src/features/calendar/ScheduleProposalPanel.tsx` — impact,
  conflict, override, cascade, and approval UI.
- `frontend/src/features/calendar/TypedCreateMenu.tsx` — routes creation into
  milestone, PTO, maintenance, renewal, and license forms.
- `frontend/src/features/calendar/calendar.css` — approved dense operations
  workspace presentation.
- `frontend/src/design-system/components/data/CalendarLensSwitcher.tsx` —
  accessible six-lens switcher.
- `frontend/src/design-system/components/data/CalendarEventCard.tsx` — typed,
  health-aware event presentation.
- `frontend/src/app/routes.ts`, `frontend/src/navigation.ts`,
  `frontend/src/App.tsx` — protected Calendar route.
- `frontend/src/features/home/HomePage.tsx` — authoritative upcoming-schedule
  summary replacing the current disconnected-feed empty state.

---

### Task 1: Persist Calendar, Workforce, and Typed Commitment Foundations

**Files:**
- Create: `backend/migrations/000091_unified_calendar_core.sql`
- Create: `backend/migrations/unified_calendar_core_contract_test.go`
- Create: `backend/migrations/000092_workforce_scheduling.sql`
- Create: `backend/migrations/workforce_scheduling_contract_test.go`
- Create: `backend/migrations/000093_calendar_commitments.sql`
- Create: `backend/migrations/calendar_commitments_contract_test.go`
- Modify: `backend/internal/setup/postgres.go`

**Interfaces:**
- Consumes: MSP, Client, technician, team-membership, Work Record, task,
  project, SLA, tag, audit-ledger, event-outbox, notification, and saved-view
  tables produced by the current repository and the first two plans.
- Produces: `calendar_event_projections`, `calendar_recurrence_exceptions`,
  `calendar_dependencies`, `calendar_scheduling_proposals`,
  `calendar_proposal_changes`, `calendar_custom_date_fields`,
  `object_custom_date_values`,
  `calendar_projection_cursors`, `calendar_live_changes`,
  `calendar_reminder_facts`,
  `technician_schedule_versions`, `technician_schedule_windows`,
  `technician_schedule_exceptions`, `pto_requests`, `calendar_conflict_policies`,
  `project_milestones`, `maintenance_windows`, `maintenance_window_scopes`,
  `commercial_commitments`, and calendar capability grants.

- [ ] **Step 1: Write failing migration contracts**

```go
func calendarMigrationBody(t *testing.T, name string) string {
	t.Helper()
	body, err := FS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func TestUnifiedCalendarCoreMigrationContract(t *testing.T) {
	body := calendarMigrationBody(t, "000091_unified_calendar_core.sql")
	for _, fragment := range []string{
		"CREATE TABLE calendar_event_projections",
		"UNIQUE (msp_id, source_type, source_id, event_role, source_role_key)",
		"CREATE TABLE calendar_recurrence_exceptions",
		"CREATE TABLE calendar_dependencies",
		"CHECK (relationship_type IN ('finish_to_start', 'start_to_start', 'finish_to_finish'))",
		"CREATE TABLE calendar_scheduling_proposals",
		"CREATE TABLE calendar_proposal_changes",
		"CREATE TABLE calendar_custom_date_fields",
		"CREATE TABLE object_custom_date_values",
		"CREATE TABLE calendar_live_changes",
		"CREATE TABLE calendar_reminder_facts",
		"ALTER TABLE work_records",
		"ADD COLUMN scheduled_starts_at",
		"ALTER TABLE tasks",
		"'calendar.read'",
		"'calendar.schedule'",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("calendar core migration missing %q", fragment)
		}
	}
}

func TestWorkforceSchedulingMigrationContract(t *testing.T) {
	body := calendarMigrationBody(t, "000092_workforce_scheduling.sql")
	for _, fragment := range []string{
		"CREATE TABLE technician_schedule_versions",
		"CREATE TABLE technician_schedule_windows",
		"CREATE TABLE technician_schedule_exceptions",
		"CREATE TABLE pto_requests",
		"CHECK (state IN ('requested', 'approved', 'rejected', 'cancelled'))",
		"CREATE TABLE calendar_conflict_policies",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("workforce migration missing %q", fragment)
		}
	}
}

func TestCalendarCommitmentsMigrationContract(t *testing.T) {
	body := calendarMigrationBody(t, "000093_calendar_commitments.sql")
	for _, fragment := range []string{
		"CREATE TABLE project_milestones",
		"CREATE TABLE maintenance_windows",
		"CREATE TABLE maintenance_window_scopes",
		"CREATE TABLE commercial_commitments",
		"CHECK (commitment_type IN ('renewal', 'license'))",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("commitment migration missing %q", fragment)
		}
	}
}
```

- [ ] **Step 2: Run the contracts and verify failure**

Run:

```bash
go test ./backend/migrations -run 'Test(UnifiedCalendarCore|WorkforceScheduling|CalendarCommitments)MigrationContract' -count=1
```

Expected: FAIL because migrations `000091`, `000092`, and `000093` do not
exist.

- [ ] **Step 3: Add the normalized calendar core schema**

Define strict enum checks, MSP/client composite foreign keys, revision columns,
and bounded indexes. Store recurrence as validated JSON plus timezone; do not
store expanded infinite series.

```sql
CREATE TABLE calendar_event_projections (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid,
  source_type text NOT NULL,
  source_id uuid NOT NULL,
  event_role text NOT NULL,
  source_role_key text NOT NULL DEFAULT '',
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  title text NOT NULL,
  starts_on date,
  ends_on date,
  starts_at timestamptz,
  ends_at timestamptz,
  timezone text,
  all_day boolean NOT NULL,
  scheduling_mode text NOT NULL
    CHECK (scheduling_mode IN ('fixed_block', 'effort_allocation', 'informational')),
  owner_id uuid,
  assignee_id uuid,
  planned_minutes bigint NOT NULL DEFAULT 0 CHECK (planned_minutes >= 0),
  filter_dimensions jsonb NOT NULL DEFAULT '{}'::jsonb,
  recurrence_rule jsonb,
  health_inputs jsonb NOT NULL DEFAULT '{}'::jsonb,
  terminal_state text NOT NULL DEFAULT 'active'
    CHECK (terminal_state IN ('active', 'completed', 'cancelled')),
  projection_revision bigint NOT NULL DEFAULT 1 CHECK (projection_revision > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  UNIQUE (msp_id, source_type, source_id, event_role, source_role_key),
  CHECK (
    (all_day AND starts_on IS NOT NULL AND starts_at IS NULL AND timezone IS NULL)
    OR
    (NOT all_day AND starts_at IS NOT NULL AND timezone IS NOT NULL)
  )
);
CREATE INDEX calendar_event_window_timed
  ON calendar_event_projections (msp_id, starts_at, ends_at)
  WHERE NOT all_day;
CREATE INDEX calendar_event_window_all_day
  ON calendar_event_projections (msp_id, starts_on, ends_on)
  WHERE all_day;
CREATE INDEX calendar_event_filters
  ON calendar_event_projections USING gin (filter_dimensions);
```

Add proposal expiry, actor, authorization-context hash, source-revision
bindings, conflict-policy versions, required-reason flags, and applied audit
correlation. Add dependency client keys and reject a database-level relationship
type outside the approved three.

`calendar_custom_date_fields` defines administrator policy.
`object_custom_date_values` stores source-owned typed values for
`work_record`, `task`, `project`, `asset`, `knowledge_article`, and
`time_entry`. Require exactly one `date_value` or `timestamp_value`, and require
an IANA timezone with a timestamp. The source object's normal authorization and
version remain authoritative.

Extend Work Records and tasks with source-owned scheduling fields:

```sql
ALTER TABLE work_records
  ADD COLUMN scheduled_starts_at timestamptz,
  ADD COLUMN scheduled_ends_at timestamptz,
  ADD COLUMN schedule_timezone text,
  ADD COLUMN scheduling_mode text NOT NULL DEFAULT 'informational',
  ADD COLUMN planned_effort_minutes bigint NOT NULL DEFAULT 0,
  ADD COLUMN due_on date,
  ADD COLUMN follow_up_on date,
  ADD COLUMN schedule_recurrence jsonb;

ALTER TABLE tasks
  ADD COLUMN scheduled_starts_at timestamptz,
  ADD COLUMN scheduled_ends_at timestamptz,
  ADD COLUMN schedule_timezone text,
  ADD COLUMN scheduling_mode text NOT NULL DEFAULT 'informational',
  ADD COLUMN due_on date,
  ADD COLUMN schedule_recurrence jsonb;
```

Add checks requiring start/timezone for schedulable timed modes, end after
start when present, positive planned effort for a capacity-bearing Work Record,
and approved scheduling-mode values. Task planned effort uses its existing
`estimate_minutes`.

- [ ] **Step 4: Add workforce and typed commitment schemas**

Use normalized local-time weekly windows and IANA timezones:

```sql
CREATE TABLE technician_schedule_versions (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  timezone text NOT NULL,
  effective_from date NOT NULL,
  effective_through date,
  version bigint NOT NULL CHECK (version > 0),
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  UNIQUE (msp_id, technician_id, version),
  CHECK (effective_through IS NULL OR effective_through >= effective_from)
);

CREATE TABLE pto_requests (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  starts_on date,
  ends_on date,
  starts_at timestamptz,
  ends_at timestamptz,
  timezone text,
  all_day boolean NOT NULL,
  pto_type text NOT NULL,
  state text NOT NULL
    CHECK (state IN ('requested', 'approved', 'rejected', 'cancelled')),
  manager_id uuid,
  decided_by uuid,
  decision_reason text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);
```

Define first-class milestone, maintenance, and commercial commitment tables
with source-owned dates, recurrence, status, owner, version, audit actors, and
the relationships from the spec. Use `maintenance_window_scopes` rows for
`client`, `service`, and `asset`; do not encode scope as comma-separated text.

- [ ] **Step 5: Grant calendar capabilities and update setup verification**

Add these canonical capabilities with `ON CONFLICT DO NOTHING` and include
them in `globalAdminCapabilities`:

```text
calendar.read
calendar.schedule
calendar.workforce.manage
calendar.commitment.manage
calendar.policy.manage
calendar.ai.recommend
```

Update setup schema verification to require the three calendar migration
anchors and the new capability rows.

- [ ] **Step 6: Run migration and setup tests**

Run:

```bash
go test ./backend/migrations ./backend/internal/setup -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/migrations/000091_unified_calendar_core.sql backend/migrations/unified_calendar_core_contract_test.go backend/migrations/000092_workforce_scheduling.sql backend/migrations/workforce_scheduling_contract_test.go backend/migrations/000093_calendar_commitments.sql backend/migrations/calendar_commitments_contract_test.go backend/internal/setup/postgres.go
git commit -m "feat: add unified calendar persistence"
```

---

### Task 2: Define Calendar Types, Event Roles, and Recurrence

**Files:**
- Create: `backend/internal/calendar/model.go`
- Create: `backend/internal/calendar/model_test.go`
- Create: `backend/internal/calendar/registry.go`
- Create: `backend/internal/calendar/registry_test.go`
- Create: `backend/internal/calendar/recurrence.go`
- Create: `backend/internal/calendar/recurrence_test.go`

**Interfaces:**
- Consumes: Go `time`, `authorization.Principal`, `scope.Target`, and no new
  recurrence library.
- Produces:
  `SourceRef`, `EventRoleDefinition`, `Projection`, `RecurrenceRule`,
  `RecurrenceException`, `Occurrence`, `Filter`, `QueryWindow`,
  `RoleRegistry.Register(EventRoleDefinition) error`, and
  `ExpandOccurrences(Projection, QueryWindow, []RecurrenceException) ([]Occurrence, error)`.

- [ ] **Step 1: Write failing model and registry tests**

```go
func TestProjectionValidatePreservesDateSemantics(t *testing.T) {
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	allDay := Projection{
		Source: SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task-1"},
		EventRole: "due", SourceRevision: 1, Title: "Due",
		AllDay: true, StartsOn: &date, SchedulingMode: Informational,
	}
	if err := allDay.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	allDay.StartsAt = &date
	if !errors.Is(allDay.Validate(), ErrInvalidProjection) {
		t.Fatal("all-day projection accepted an invented timestamp")
	}
}

func TestRoleRegistryRejectsDuplicateAndGenericRoles(t *testing.T) {
	registry := NewRoleRegistry()
	definition := EventRoleDefinition{
		SourceType: "task", Role: "scheduled_work",
		SchedulingMode: FixedBlock, CapacityBearing: true,
		DependencyEligible: true,
	}
	if err := registry.Register(definition); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(registry.Register(definition), ErrDuplicateRole) {
		t.Fatal("duplicate role was accepted")
	}
	if err := registry.Register(EventRoleDefinition{
		SourceType: "generic_event", Role: "event",
	}); !errors.Is(err, ErrInvalidRole) {
		t.Fatal("generic event role was accepted")
	}
}
```

- [ ] **Step 2: Run the model tests and verify failure**

Run:

```bash
go test ./backend/internal/calendar -run 'Test(Projection|RoleRegistry)' -count=1
```

Expected: FAIL because the calendar package does not exist.

- [ ] **Step 3: Implement stable domain types**

Use explicit enums and separate date/timestamp fields:

```go
type SchedulingMode string

const (
	FixedBlock       SchedulingMode = "fixed_block"
	EffortAllocation SchedulingMode = "effort_allocation"
	Informational    SchedulingMode = "informational"
)

type SourceRef struct {
	MSPID, ClientID string
	Type, ID        string
}

type Projection struct {
	ID, EventRole, SourceRoleKey string
	Source                       SourceRef
	SourceRevision               int64
	Title                        string
	AllDay                       bool
	StartsOn, EndsOn             *time.Time
	StartsAt, EndsAt             *time.Time
	Timezone                     string
	SchedulingMode               SchedulingMode
	OwnerID, AssigneeID          string
	PlannedMinutes               int64
	Recurrence                   *RecurrenceRule
	Dimensions                   FilterDimensions
	HealthInputs                 HealthInputs
	TerminalState                TerminalState
}

type EventRoleDefinition struct {
	SourceType, Role  string
	SchedulingMode   SchedulingMode
	CapacityBearing  bool
	DependencyEligible bool
	ReadOnly         bool
}
```

`Validate` must reject mixed date/timestamp shapes, missing IANA locations,
negative effort, capacity-bearing informational roles, and untyped source
references.

- [ ] **Step 4: Write recurrence tests**

```go
func TestExpandOccurrencesPreservesLocalTimeAcrossDST(t *testing.T) {
	start := mustTime(t, "2026-03-01T09:00:00-05:00")
	projection := recurringProjection(start, "America/New_York", RecurrenceRule{
		Frequency: Weekly, Interval: 1, Weekdays: []time.Weekday{time.Sunday},
		Count: 3,
	})
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-03-01T00:00:00Z"),
		End: mustTime(t, "2026-03-20T00:00:00Z"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, occurrence := range found {
		local := occurrence.StartsAt.In(mustLocation(t, "America/New_York"))
		if local.Hour() != 9 {
			t.Fatalf("local hour = %d, want 9", local.Hour())
		}
	}
}

func TestExpandOccurrencesAppliesCancelAndMoveExceptions(t *testing.T) {
	start := mustTime(t, "2026-08-10T09:00:00Z")
	projection := recurringProjection(start, "UTC", RecurrenceRule{
		Frequency: Daily, Interval: 1, Count: 3,
	})
	secondKey := "2026-08-11T09:00:00"
	thirdKey := "2026-08-12T09:00:00"
	moved := mustTime(t, "2026-08-13T11:00:00Z")
	found, err := ExpandOccurrences(projection, QueryWindow{
		Start: mustTime(t, "2026-08-10T00:00:00Z"),
		End: mustTime(t, "2026-08-15T00:00:00Z"),
	}, []RecurrenceException{
		{OriginalLocalKey: secondKey, State: ExceptionCancelled},
		{OriginalLocalKey: thirdKey, State: ExceptionRescheduled, StartsAt: &moved},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("occurrences = %d, want 2", len(found))
	}
	if found[1].OriginalLocalKey != thirdKey || !found[1].StartsAt.Equal(moved) {
		t.Fatalf("moved occurrence lost identity: %+v", found[1])
	}
}
```

- [ ] **Step 5: Implement recurrence validation and bounded expansion**

Support only:

```go
type Frequency string
const (
	Daily Frequency = "daily"
	Weekly Frequency = "weekly"
	Monthly Frequency = "monthly"
	Yearly Frequency = "yearly"
)

type RecurrenceRule struct {
	Frequency Frequency
	Interval int
	Weekdays []time.Weekday
	Count int
	Until *time.Time
}

type RecurrenceExceptionState string
const (
	ExceptionCancelled RecurrenceExceptionState = "cancelled"
	ExceptionRescheduled RecurrenceExceptionState = "rescheduled"
	ExceptionOverridden RecurrenceExceptionState = "overridden"
)

type RecurrenceException struct {
	OriginalLocalKey string
	State RecurrenceExceptionState
	StartsOn, EndsOn *time.Time
	StartsAt, EndsAt *time.Time
}

type Occurrence struct {
	ID, ProjectionID, OriginalLocalKey string
	AllDay bool
	StartsOn, EndsOn *time.Time
	StartsAt, EndsAt time.Time
	Timezone string
}
```

Reject rules with both `Count` and `Until`, zero intervals, invalid weekday
combinations, or no bounded query window. Derive occurrence IDs from projection
ID plus the original local occurrence key so rescheduling does not change
identity. Implement monthly end-of-month behavior explicitly: a series started
on day 29–31 skips months without that date rather than silently clamping.

- [ ] **Step 6: Run calendar core tests**

Run:

```bash
go test ./backend/internal/calendar -run 'Test(Projection|RoleRegistry|Expand|Recurrence)' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/calendar/model.go backend/internal/calendar/model_test.go backend/internal/calendar/registry.go backend/internal/calendar/registry_test.go backend/internal/calendar/recurrence.go backend/internal/calendar/recurrence_test.go
git commit -m "feat: define calendar event and recurrence model"
```

---

### Task 3: Add First-Class Milestone, Workforce, Maintenance, Renewal, and License Services

**Files:**
- Create: `backend/internal/projects/milestones.go`
- Create: `backend/internal/projects/milestones_test.go`
- Create: `backend/internal/workforce/schedules.go`
- Create: `backend/internal/workforce/schedules_test.go`
- Create: `backend/internal/workforce/pto.go`
- Create: `backend/internal/workforce/pto_test.go`
- Create: `backend/internal/commitments/maintenance.go`
- Create: `backend/internal/commitments/maintenance_test.go`
- Create: `backend/internal/commitments/commercial.go`
- Create: `backend/internal/commitments/commercial_test.go`
- Create: `backend/internal/customfields/dates.go`
- Create: `backend/internal/customfields/dates_test.go`
- Create: `backend/internal/store/psa/workforce_repository.go`
- Create: `backend/internal/store/psa/commitment_repository.go`
- Create: `backend/internal/store/psa/custom_date_repository.go`
- Modify: `backend/internal/store/psa/project_repository.go`

**Interfaces:**
- Consumes: existing project, technician, team-membership, client-resource,
  audit, event-outbox, `authorization`, `mutation`, and `scope` contracts.
- Produces: `projects.MilestoneService`, `workforce.ScheduleService`,
  `workforce.PTOService`, `commitments.MaintenanceService`,
  `commitments.CommercialService`, `customfields.DateService`, and atomic
  PostgreSQL repositories.

- [ ] **Step 1: Write failing milestone and maintenance tests**

```go
func TestCreateMilestoneRequiresProjectClientAndWritesMutation(t *testing.T) {
	repository := &milestoneRepositoryStub{
		project: Project{ID: "project-1", MSPID: "msp", ClientID: "client"},
	}
	service := NewMilestoneService(repository, fixedNow, sequentialIDs())
	found, err := service.Create(context.Background(), CreateMilestoneCommand{
		Principal: principal("project.edit", "msp", "client"),
		ProjectID: "project-1", Name: "Cutover",
		DueOn: date(2026, 9, 15), OwnerID: "tech-1",
		ActorID: "actor", Source: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if found.ClientID != "client" || repository.mutation.Event.EventType != "project.milestone.created" {
		t.Fatalf("unexpected milestone mutation: %+v", repository.mutation)
	}
}

func TestMaintenanceRejectsCrossMSPScopes(t *testing.T) {
	service := NewMaintenanceService(&maintenanceRepositoryStub{
		resourcesBelongToMSP: false,
	}, fixedNow, sequentialIDs())
	_, err := service.Create(context.Background(), CreateMaintenanceCommand{
		Principal: principal("calendar.commitment.manage", "msp", ""),
		Title: "Core upgrade", StartsAt: instant(2026, 9, 1, 1),
		EndsAt: instant(2026, 9, 1, 3), Timezone: "UTC",
		Scopes: []ScopeRef{{Type: ScopeAsset, ID: "foreign-asset"}},
		ActorID: "actor", Source: "api",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("error = %v, want scoped not found", err)
	}
}

func TestCustomDateValueRequiresExactSourceAuthorization(t *testing.T) {
	service := customfields.NewDateService(&customDateRepositoryStub{
		sourceVisible: false,
	}, fixedNow, sequentialIDs())
	_, err := service.Set(context.Background(), customfields.SetDateCommand{
		Principal: principal("work_record.edit", "msp", "client-a"),
		ObjectType: "work_record", ObjectID: "work-in-client-b",
		FieldID: "follow-up", DateValue: ptr(date(2026, 9, 1)),
		ActorID: "actor", Source: "api",
	})
	if !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("error = %v, want scoped not found", err)
	}
}
```

- [ ] **Step 2: Write failing schedule and PTO tests**

```go
func TestScheduleRejectsOverlappingWeeklyWindows(t *testing.T) {
	service := NewScheduleService(&scheduleRepositoryStub{}, fixedNow, sequentialIDs())
	_, err := service.Publish(context.Background(), PublishScheduleCommand{
		Principal: principal("calendar.workforce.manage", "msp", ""),
		TechnicianID: "tech-1", Timezone: "America/Chicago",
		EffectiveFrom: date(2026, 8, 1),
		Windows: []WeeklyWindow{
			{Weekday: time.Monday, StartsMinute: 540, EndsMinute: 1020},
			{Weekday: time.Monday, StartsMinute: 600, EndsMinute: 660},
		},
		ActorID: "actor", Source: "api",
	})
	if !errors.Is(err, ErrScheduleOverlap) {
		t.Fatalf("error = %v, want overlap", err)
	}
}

func TestPTOApprovalRequiresConfiguredManagerOrAdmin(t *testing.T) {
	repository := &ptoRepositoryStub{managerID: "manager-1"}
	service := NewPTOService(repository, fixedNow, sequentialIDs())
	_, err := service.Decide(context.Background(), DecidePTOCommand{
		Principal: principal("calendar.schedule", "msp", ""),
		RequestID: "pto-1", Decision: Approved,
		ActorID: "other-tech", ExpectedVersion: 1,
	})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("error = %v, want forbidden", err)
	}
}
```

- [ ] **Step 3: Run the new domain tests and verify failure**

Run:

```bash
go test ./backend/internal/projects ./backend/internal/workforce ./backend/internal/commitments ./backend/internal/customfields -run 'Test(CreateMilestone|Maintenance|CustomDate|Schedule|PTO|Commercial)' -count=1
```

Expected: FAIL because the services do not exist.

- [ ] **Step 4: Implement typed domain services**

Each accepted command must construct source state, one audit record, and one
outbox event with a shared correlation ID. Use optimistic expected versions for
updates and transitions.

Define PTO decisions exactly:

```go
type PTOState string
const (
	Requested PTOState = "requested"
	Approved  PTOState = "approved"
	Rejected  PTOState = "rejected"
	Cancelled PTOState = "cancelled"
)
```

Requested PTO is capacity-tentative. Only `Approved` reduces capacity.
`Decide` permits the configured active team manager or a principal with
`calendar.workforce.manage`. Cancellation permits the requester before or
after approval and records a new version.

Define commercial commitment types exactly:

```go
type CommercialType string
const (
	Renewal CommercialType = "renewal"
	License CommercialType = "license"
)
```

Require vendor, owner, effective/expiration dates, nonnegative quantity/cost,
currency when cost is present, and same-client service/asset/contract links.

`customfields.DateService` permits only the six Task 1 object types, loads the
administrator definition, requires the source object's edit capability, and
persists either a date or timestamp/timezone value with source version,
audit, and outbox evidence. It cannot create a generic calendar source.

- [ ] **Step 5: Implement atomic PostgreSQL repositories**

Use the existing `mutation.AuditRecord` and `mutation.EventRecord` insert
patterns. Each repository transaction must:

1. validate source and relationship rows under the trusted MSP/client scope
2. insert or update the typed record with expected version
3. write the audit row
4. write the event-outbox row
5. commit or roll back everything

Return `scope.ErrNotFound` for cross-scope IDs and a domain conflict for stale
versions.

- [ ] **Step 6: Run service and repository tests**

Run:

```bash
go test ./backend/internal/projects ./backend/internal/workforce ./backend/internal/commitments ./backend/internal/customfields ./backend/internal/store/psa -run 'Milestone|Schedule|PTO|Maintenance|Commercial|CustomDate' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/projects/milestones.go backend/internal/projects/milestones_test.go backend/internal/workforce backend/internal/commitments backend/internal/customfields backend/internal/store/psa/workforce_repository.go backend/internal/store/psa/commitment_repository.go backend/internal/store/psa/custom_date_repository.go backend/internal/store/psa/project_repository.go
git commit -m "feat: add typed calendar source records"
```

---

### Task 4: Project Source Objects into the Calendar Read Model

**Files:**
- Create: `backend/internal/calendar/adapter.go`
- Create: `backend/internal/calendar/adapter_test.go`
- Create: `backend/internal/calendar/adapters/work.go`
- Create: `backend/internal/calendar/adapters/projects.go`
- Create: `backend/internal/calendar/adapters/workforce.go`
- Create: `backend/internal/calendar/adapters/commitments.go`
- Create: `backend/internal/calendar/adapters/custom_dates.go`
- Create: `backend/internal/calendar/projection.go`
- Create: `backend/internal/calendar/projection_test.go`
- Create: `backend/internal/calendar/reconciliation.go`
- Create: `backend/internal/calendar/reconciliation_test.go`
- Create: `backend/internal/store/psa/calendar_repository.go`
- Create: `backend/internal/store/psa/calendar_repository_test.go`
- Modify: `backend/internal/outbox/dispatcher.go`

**Interfaces:**
- Consumes: Task 2 `Projection`, `EventRoleDefinition`, and recurrence types;
  Task 3 typed records; existing Work Record SLA bindings, project/phase/task
  records, tag resolver, and event outbox.
- Produces:
  `SourceAdapter.Project(context.Context, SourceRef) ([]Projection, error)`,
  `ProjectionService.Apply(context.Context, ProjectionBatch) error`,
  `ProjectionWorker.Handle(context.Context, mutation.EventRecord) error`, and
  `ReconciliationService.Scan(context.Context, ReconcileRequest) (ReconcileReport, error)`.

- [ ] **Step 1: Write failing adapter contract tests**

```go
func TestEveryRegisteredRoleProducesTypedProjection(t *testing.T) {
	registry := productionRoleRegistry()
	adapters := productionAdapterRegistry()
	for _, definition := range registry.Definitions() {
		adapter, ok := adapters.ForSource(definition.SourceType)
		if !ok {
			t.Errorf("source %q has no adapter", definition.SourceType)
			continue
		}
		if adapter.SourceType() == "generic_event" {
			t.Fatal("generic event adapter registered")
		}
	}
}

func TestWorkRecordAdapterProjectsScheduleAndSLASeparately(t *testing.T) {
	adapter := NewWorkRecordAdapter(&workSourceStub{record: scheduledRecord()})
	found, err := adapter.Project(context.Background(), SourceRef{
		MSPID: "msp", ClientID: "client", Type: "work_record", ID: "work-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRoles(t, found, "scheduled_work", "sla_response_deadline", "sla_resolution_deadline")
	assertMode(t, found, "scheduled_work", FixedBlock)
	assertMode(t, found, "sla_resolution_deadline", Informational)
}
```

- [ ] **Step 2: Write failing projection ordering tests**

```go
func TestProjectionServiceIgnoresOlderSourceRevision(t *testing.T) {
	repository := &projectionRepositoryStub{currentRevision: 7}
	service := NewProjectionService(repository, fixedNow)
	err := service.Apply(context.Background(), ProjectionBatch{
		Source: SourceRef{MSPID: "msp", Type: "task", ID: "task-1"},
		SourceRevision: 6,
		Projections: []Projection{projectionAtRevision(6)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.upserted {
		t.Fatal("older source revision overwrote projection")
	}
}

func TestProjectionServiceRemovesRolesMissingFromNewRevision(t *testing.T) {
	repository := &projectionRepositoryStub{
		existingRoles: []string{"scheduled_work", "due"},
	}
	service := NewProjectionService(repository, fixedNow)
	err := service.Apply(context.Background(), ProjectionBatch{
		Source: SourceRef{MSPID: "msp", Type: "task", ID: "task-1"},
		SourceRevision: 4,
		Projections: []Projection{projectionWithRole("due", 4)},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, repository.deletedRoles, []string{"scheduled_work"})
}
```

- [ ] **Step 3: Run projection tests and verify failure**

Run:

```bash
go test ./backend/internal/calendar ./backend/internal/calendar/adapters ./backend/internal/store/psa -run 'Test(EveryRegistered|WorkRecordAdapter|ProjectionService|CalendarProjection)' -count=1
```

Expected: FAIL because adapters and projection services do not exist.

- [ ] **Step 4: Implement the adapter registry and built-in adapters**

Register explicit source/event roles:

```text
work_record: scheduled_work, due, follow_up, sla_response_deadline, sla_resolution_deadline
task: scheduled_work, due
project: planned_start, planned_end
phase: planned_start, planned_end
milestone: milestone
technician_schedule: availability
pto: unavailability
maintenance_window: maintenance
commercial_commitment: effective, notice, renewal, expiration
custom_date: configured role key
```

Adapters must project all-day fields as dates and timed fields with the
authoritative timezone. SLA and commercial derived roles are informational.
Scheduled Work Record/task roles are capacity-bearing only when assigned and
planned minutes are positive.

- [ ] **Step 5: Implement revision-aware projection and PostgreSQL persistence**

`ApplyProjectionBatchAtomic` must lock current rows for the source, ignore a
batch older than the highest current source revision, upsert current roles,
delete roles removed by the authoritative revision, append
`calendar_live_changes`, and advance the projection cursor in one transaction.

Do not copy ticket descriptions, comments, notes, or private PTO details into
projection rows.

- [ ] **Step 6: Implement outbox worker and reconciliation**

Map relevant source event types to one adapter source key. The worker loads the
current source rather than trusting event payload content.

```go
type ReconcileRequest struct {
	MSPID string
	SourceTypes []string
	Limit int
	Repair bool
}

type ReconcileReport struct {
	Scanned, Missing, Stale, Orphaned, Repaired int
}
```

When `Repair` is true, regenerate projection batches only. Never mutate source
dates. Add `calendar-reconcile --msp-id --source-type --limit --repair` to
`rarity-admin` in Task 15.

- [ ] **Step 7: Run projection, adapter, and outbox tests**

Run:

```bash
go test ./backend/internal/calendar/... ./backend/internal/outbox ./backend/internal/store/psa -run 'Calendar|Projection|Adapter|Reconcile' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/calendar backend/internal/store/psa/calendar_repository.go backend/internal/store/psa/calendar_repository_test.go backend/internal/outbox/dispatcher.go
git commit -m "feat: project typed sources into calendar events"
```

---

### Task 5: Implement Dependencies and Deterministic Event Health

**Files:**
- Create: `backend/internal/calendar/dependencies.go`
- Create: `backend/internal/calendar/dependencies_test.go`
- Create: `backend/internal/calendar/health.go`
- Create: `backend/internal/calendar/health_test.go`
- Create: `backend/internal/calendar/health_worker.go`
- Create: `backend/internal/calendar/health_worker_test.go`
- Modify: `backend/internal/store/psa/calendar_repository.go`
- Modify: `backend/internal/store/psa/calendar_repository_test.go`

**Interfaces:**
- Consumes: projected event roles, authoritative client IDs, scheduling
  intervals, capacity and source-status inputs.
- Produces:
  `DependencyService.Preview`, `DependencyService.Create`,
  `DependencyService.Delete`, `BuildCascadeImpact`,
  `EvaluateHealth(HealthContext) HealthResult`,
  `HealthWorker.Handle(HealthRecomputeEvent)`, and persisted reason codes.

- [ ] **Step 1: Write failing dependency tests**

```go
func TestDependencyServiceRejectsCrossClientAndCycles(t *testing.T) {
	repository := dependencyRepositoryStub{
		events: map[string]Projection{
			"a": projectionForClient("a", "client-1"),
			"b": projectionForClient("b", "client-2"),
		},
	}
	service := NewDependencyService(&repository, fixedNow, sequentialIDs())
	_, err := service.Preview(context.Background(), CreateDependencyCommand{
		Principal: principal("calendar.schedule", "msp", ""),
		PredecessorID: "a", SuccessorID: "b",
		Type: FinishToStart,
	})
	if !errors.Is(err, ErrCrossClientDependency) {
		t.Fatalf("error = %v, want cross-client", err)
	}

	repository.events["b"] = projectionForClient("b", "client-1")
	repository.edges = []Dependency{{PredecessorID: "b", SuccessorID: "a"}}
	_, err = service.Preview(context.Background(), CreateDependencyCommand{
		Principal: principal("calendar.schedule", "msp", ""),
		PredecessorID: "a", SuccessorID: "b", Type: FinishToStart,
	})
	if !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("error = %v, want cycle", err)
	}
}

func TestDependencyTypesExcludeStartToFinish(t *testing.T) {
	if validDependencyType(DependencyType("start_to_finish")) {
		t.Fatal("start-to-finish dependency accepted")
	}
}
```

- [ ] **Step 2: Write failing health tests**

```go
func TestHealthPrecedenceAndReasonCodes(t *testing.T) {
	now := instant(2026, 8, 7, 12)
	found := EvaluateHealth(HealthContext{
		Now: now, TerminalState: Active,
		BlockedReasons: []HealthReason{{Code: "dependency_blocked"}},
		DueAt: ptr(now.Add(-time.Hour)),
		RiskReasons: []HealthReason{{Code: "capacity_shortage"}},
	})
	if found.State != HealthBlocked {
		t.Fatalf("state = %q, want blocked", found.State)
	}
	assertReasonCodes(t, found, "dependency_blocked", "overdue", "capacity_shortage")
}

func TestCompletedEventHasNoActiveRisk(t *testing.T) {
	found := EvaluateHealth(HealthContext{
		Now: instant(2026, 8, 7, 12), TerminalState: Completed,
		DueAt: ptr(instant(2026, 8, 1, 12)),
	})
	if found.State != HealthTerminal || len(found.Reasons) != 0 {
		t.Fatalf("unexpected completed health: %+v", found)
	}
}
```

- [ ] **Step 3: Run dependency and health tests and verify failure**

Run:

```bash
go test ./backend/internal/calendar -run 'Test(Dependency|Health|CompletedEvent)' -count=1
```

Expected: FAIL because dependency and health services do not exist.

- [ ] **Step 4: Implement graph validation and cascade impact**

Use deterministic adjacency traversal sorted by projection ID. Validate:

- same non-empty client
- dependency-eligible event roles
- no self edge or duplicate
- no cycle
- relationship type in the approved set
- lead/lag inside an administrator-defined absolute maximum
- view/edit authorization on both source contexts

Compute finish-to-start, start-to-start, and finish-to-finish constraints using
the predecessor interval and signed lead/lag. `BuildCascadeImpact` returns
required moves, optional moves, blocked sources, affected health, and capacity
recalculation keys without mutating state.

- [ ] **Step 5: Implement deterministic health**

```go
type HealthState string
const (
	HealthBlocked HealthState = "blocked"
	HealthOverdue HealthState = "overdue"
	HealthAtRisk HealthState = "at_risk"
	HealthOnTrack HealthState = "on_track"
	HealthTerminal HealthState = "terminal"
)
```

Record all supporting reasons while choosing visible state by precedence.
Inputs are source-blocked status, unmet dependencies, hard constraints,
authoritative due/end time, capacity shortage, dependency delay, schedule
variance, and SLA risk margin. Store rule version and evaluated time.

- [ ] **Step 6: Recompute health from every relevant authoritative change**

`HealthWorker` consumes projection, source-status, dependency, schedule,
availability, capacity, and SLA facts. It resolves affected projections,
evaluates with the same rule version, persists state/reasons only when the
result changes, and appends a calendar live-change row. The worker is
idempotent by source event ID plus health rule version.

- [ ] **Step 7: Run dependency, health, and persistence tests**

Run:

```bash
go test ./backend/internal/calendar ./backend/internal/store/psa -run 'Dependency|Health|Calendar' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/calendar/dependencies.go backend/internal/calendar/dependencies_test.go backend/internal/calendar/health.go backend/internal/calendar/health_test.go backend/internal/calendar/health_worker.go backend/internal/calendar/health_worker_test.go backend/internal/store/psa/calendar_repository.go backend/internal/store/psa/calendar_repository_test.go
git commit -m "feat: add calendar dependencies and health"
```

---

### Task 6: Calculate Availability, Capacity, and Conflict Policies

**Files:**
- Create: `backend/internal/calendar/availability.go`
- Create: `backend/internal/calendar/availability_test.go`
- Create: `backend/internal/calendar/capacity.go`
- Create: `backend/internal/calendar/capacity_test.go`
- Create: `backend/internal/calendar/conflicts.go`
- Create: `backend/internal/calendar/conflicts_test.go`
- Create: `backend/internal/calendar/configuration.go`
- Create: `backend/internal/calendar/configuration_test.go`
- Modify: `backend/internal/projects/capacity.go`
- Modify: `backend/internal/projects/query.go`
- Modify: `backend/internal/store/psa/project_repository.go`
- Modify: `backend/internal/store/psa/calendar_repository.go`

**Interfaces:**
- Consumes: recurring schedule versions, dated exceptions, PTO state,
  maintenance constraints, projected assigned work, planned effort, dependency
  constraints, and administrator conflict policies.
- Produces:
  `AvailabilityService.Resolve`, `CapacityService.Calculate`,
  `ConflictService.Evaluate`, `ConfigurationService.ReplaceConflictPolicies`,
  `ConfigurationService.UpsertCustomDateField`, `CapacitySummary`,
  `AllocationSegment`, `Conflict`, and a compatibility bridge for existing
  project capacity views.

- [ ] **Step 1: Write failing availability tests**

```go
func TestAvailabilityAppliesApprovedButNotRequestedPTO(t *testing.T) {
	service := NewAvailabilityService(&availabilityRepositoryStub{
		schedule: weekdaySchedule("America/Chicago", 9*60, 17*60),
		pto: []PTOInterval{
			{State: workforce.Requested, StartsAt: localInstant("2026-08-10T09:00", "America/Chicago"), EndsAt: localInstant("2026-08-10T11:00", "America/Chicago")},
			{State: workforce.Approved, StartsAt: localInstant("2026-08-10T13:00", "America/Chicago"), EndsAt: localInstant("2026-08-10T15:00", "America/Chicago")},
		},
	})
	found, err := service.Resolve(context.Background(), "msp", "tech-1", dayWindow(2026, 8, 10))
	if err != nil {
		t.Fatal(err)
	}
	if found.AvailableMinutes != 360 || found.TentativeMinutes != 120 {
		t.Fatalf("availability = %+v", found)
	}
}
```

- [ ] **Step 2: Write failing capacity and conflict tests**

```go
func TestCapacityCountsOnlyAssignedScheduledEffort(t *testing.T) {
	found := CalculateCapacity(CapacityInput{
		Available: intervals(minutes(480)),
		Events: []CapacityEvent{
			{Assigned: true, Mode: FixedBlock, PlannedMinutes: 120, Interval: intervalMinutes(120)},
			{Assigned: true, Mode: Informational, PlannedMinutes: 60},
			{Assigned: false, Mode: EffortAllocation, PlannedMinutes: 180},
		},
	})
	if found.CommittedMinutes != 120 || found.RemainingMinutes != 360 {
		t.Fatalf("capacity = %+v", found)
	}
}

func TestProtectedPTOIsHardConflict(t *testing.T) {
	found := EvaluateConflicts(ConflictInput{
		Policies: []ConflictPolicy{{Kind: "approved_pto", Severity: ConflictHard}},
		Proposed: fixedBlockDuringApprovedPTO(),
	})
	if len(found) != 1 || found[0].Severity != ConflictHard {
		t.Fatalf("conflicts = %+v", found)
	}
}

func TestCustomDateConfigurationCannotInventTimeForDateField(t *testing.T) {
	service := NewConfigurationService(&configurationRepositoryStub{}, fixedNow, sequentialIDs())
	_, err := service.UpsertCustomDateField(context.Background(), UpsertCustomDateCommand{
		Principal: principal("calendar.policy.manage", "msp", ""),
		ObjectType: "task", FieldID: "follow_up_on", FieldType: FieldDate,
		Label: "Follow up", SchedulingMode: FixedBlock,
		TimezoneSource: "America/Chicago",
	})
	if !errors.Is(err, ErrInvalidCustomDateConfiguration) {
		t.Fatalf("error = %v, want invalid custom date configuration", err)
	}
}
```

- [ ] **Step 3: Run capacity tests and verify failure**

Run:

```bash
go test ./backend/internal/calendar ./backend/internal/projects -run 'Test(Availability|Capacity|ProtectedPTO)' -count=1
```

Expected: FAIL because the new availability, capacity, and configuration
services do not exist.

- [ ] **Step 4: Implement timezone-aware availability resolution**

Expand weekly schedule windows in their IANA timezone, then subtract or adjust:

1. approved PTO
2. holidays
3. training
4. manual overrides
5. protected maintenance constraints

Return requested PTO as tentative metadata without subtracting it. Resolve DST
using the schedule's intended local wall-clock window.

- [ ] **Step 5: Implement fixed and allocated consumption**

Fixed blocks consume their exact intersection with the resolved planning
window. Effort allocations fill available segments in chronological order
inside the authoritative range after fixed blocks and hard unavailability,
returning allocated and unallocated minutes.

```go
type CapacitySummary struct {
	AvailableMinutes, FixedMinutes, AllocatedMinutes int64
	RemainingMinutes, OverbookedMinutes              int64
	TentativeUnavailableMinutes, UnscheduledMinutes  int64
	Segments []AllocationSegment
}
```

Unassigned or unscheduled effort contributes only to `UnscheduledMinutes`.
Aggregate team results across all authorized client work for each technician.

- [ ] **Step 6: Implement policy-driven conflict evaluation**

Use severities:

```go
const (
	ConflictInfo         ConflictSeverity = "informational"
	ConflictWarning      ConflictSeverity = "warning"
	ConflictOverrideable ConflictSeverity = "overrideable_block"
	ConflictHard         ConflictSeverity = "hard_block"
)
```

Default ordinary overbooking to `overrideable_block` with `ReasonRequired=true`.
Permit administrators with `calendar.policy.manage` to configure approved PTO,
non-working time, and protected maintenance as hard blocks. Every conflict
includes policy ID/version, reason code, affected interval, and safe related
source reference.

- [ ] **Step 7: Implement calendar policy and custom-date configuration**

Require `calendar.policy.manage` and optimistic expected versions. Conflict
policy replacement accepts only known conflict kinds and the four approved
severities.

Custom date configuration requires:

```go
type CustomDateField struct {
	ID, MSPID, ObjectType, FieldID, Label, Category string
	FieldType FieldType
	SchedulingMode SchedulingMode
	CapacityBearing bool
	TimezoneSource string
	Version int64
}
```

`FieldDate` must remain all-day and must reject a timezone source. It may be
informational or movable as an all-day fixed block, but it cannot consume
capacity. `FieldDateTime` requires an authoritative timezone source.
Capacity-bearing configuration requires a schedulable mode and positive
planned-effort source. Accepted changes write audit/outbox evidence so affected
records project when they are next created or updated; do not backfill.

- [ ] **Step 8: Bridge existing project capacity**

Keep the existing project query contract stable but delegate schedule and
capacity math to the calendar capacity service. Remove duplicate calculation
rules only after characterization tests prove existing project results remain
compatible.

- [ ] **Step 9: Run availability, capacity, conflict, configuration, and project tests**

Run:

```bash
go test ./backend/internal/calendar ./backend/internal/projects ./backend/internal/store/psa -run 'Availability|Capacity|Conflict|Configuration|Project' -count=1
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/calendar/availability.go backend/internal/calendar/availability_test.go backend/internal/calendar/capacity.go backend/internal/calendar/capacity_test.go backend/internal/calendar/conflicts.go backend/internal/calendar/conflicts_test.go backend/internal/calendar/configuration.go backend/internal/calendar/configuration_test.go backend/internal/projects/capacity.go backend/internal/projects/query.go backend/internal/store/psa/project_repository.go backend/internal/store/psa/calendar_repository.go
git commit -m "feat: add unified scheduling capacity"
```

---

### Task 7: Preview and Atomically Apply Scheduling Proposals

**Files:**
- Create: `backend/internal/calendar/write_adapter.go`
- Create: `backend/internal/calendar/write_adapter_test.go`
- Create: `backend/internal/calendar/proposals.go`
- Create: `backend/internal/calendar/proposals_test.go`
- Create: `backend/internal/calendar/scheduling.go`
- Create: `backend/internal/calendar/scheduling_test.go`
- Modify: `backend/internal/calendar/adapters/work.go`
- Modify: `backend/internal/calendar/adapters/projects.go`
- Modify: `backend/internal/calendar/adapters/workforce.go`
- Modify: `backend/internal/calendar/adapters/commitments.go`
- Modify: `backend/internal/workrecords/service.go`
- Modify: `backend/internal/tasks/service.go`
- Modify: `backend/internal/projects/model.go`
- Modify: `backend/internal/store/psa/work_record_repository.go`
- Modify: `backend/internal/store/psa/task_repository.go`
- Modify: `backend/internal/store/psa/project_repository.go`
- Modify: `backend/internal/store/psa/calendar_repository.go`
- Modify: `backend/internal/store/psa/calendar_repository_test.go`

**Interfaces:**
- Consumes: dependency impact, availability, capacity, conflict policies,
  source role definitions, source revisions, workforce scope, and optimistic
  domain versions.
- Produces:
  `ProposalService.Preview(context.Context, PreviewCommand) (SchedulingProposal, error)`,
  `ProposalService.Apply(context.Context, ApplyCommand) (AppliedProposal, error)`,
  `WriteAdapter.Prepare`, `WriteAdapter.Apply`, and
  `ScheduleUnitOfWork.ApplyAtomic`.

- [ ] **Step 1: Write failing proposal tests**

```go
func TestPreviewReturnsCascadeCapacityAndOverrideRequirements(t *testing.T) {
	service := proposalFixture(t)
	found, err := service.Preview(context.Background(), PreviewCommand{
		Principal: schedulingPrincipal(),
		PrimaryChange: RequestedChange{
			ProjectionID: "event-a", StartsAt: ptr(instant(2026, 8, 10, 10)),
			EndsAt: ptr(instant(2026, 8, 10, 12)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Changes) < 2 || len(found.Conflicts) == 0 ||
		!found.RequiresReason {
		t.Fatalf("proposal lacks impact: %+v", found)
	}
}

func TestApplyRejectsStaleProposalWithoutPartialChanges(t *testing.T) {
	fixture := proposalFixture(t)
	proposal := fixture.savedProposal()
	fixture.sources["event-a"].Revision++
	_, err := fixture.service.Apply(context.Background(), ApplyCommand{
		Principal: schedulingPrincipal(), ProposalID: proposal.ID,
		AcceptedOptionalChangeIDs: proposal.OptionalChangeIDs(),
		OverrideReason: "Dispatch-approved overlap",
	})
	if !errors.Is(err, ErrStaleProposal) {
		t.Fatalf("error = %v, want stale proposal", err)
	}
	if fixture.unitOfWork.applyCalls != 0 {
		t.Fatal("stale proposal applied partial changes")
	}
}
```

- [ ] **Step 2: Write failing authorization and recurrence-scope tests**

```go
func TestPreviewRequiresSourceAndWorkforceAuthorityForEveryChange(t *testing.T) {
	fixture := proposalFixture(t)
	fixture.workforce.allowedTechnicians = map[string]bool{"tech-a": true}
	_, err := fixture.service.Preview(context.Background(), PreviewCommand{
		Principal: schedulingPrincipal(),
		PrimaryChange: moveWithCascade("event-a", "event-b-for-tech-b"),
	})
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("error = %v, want forbidden", err)
	}
}

func TestRecurringChangeRequiresExplicitScope(t *testing.T) {
	fixture := proposalFixture(t)
	_, err := fixture.service.Preview(context.Background(), PreviewCommand{
		Principal: schedulingPrincipal(),
		PrimaryChange: recurringMoveWithoutScope(),
	})
	if !errors.Is(err, ErrOccurrenceScopeRequired) {
		t.Fatalf("error = %v, want occurrence scope", err)
	}
}
```

- [ ] **Step 3: Run proposal tests and verify failure**

Run:

```bash
go test ./backend/internal/calendar -run 'Test(Preview|Apply|RecurringChange)' -count=1
```

Expected: FAIL because proposal and write-back services do not exist.

- [ ] **Step 4: Implement preview and revision bindings**

```go
type OccurrenceScope string
const (
	ThisOccurrence OccurrenceScope = "this_occurrence"
	ThisAndFuture OccurrenceScope = "this_and_future"
	EntireSeries OccurrenceScope = "entire_series"
)

type SchedulingProposal struct {
	ID string
	ActorID string
	ExpiresAt time.Time
	Changes []ProposedChange
	Conflicts []Conflict
	Capacity []CapacityImpact
	Health []HealthImpact
	Bindings []RevisionBinding
	RequiresReason bool
}
```

Preview must:

1. resolve the exact occurrence and source role
2. verify source edit capability and workforce scope
3. prepare the source-domain change through its write adapter
4. compute recurrence, dependency, capacity, health, and notification impacts
5. distinguish required from optional cascade changes
6. persist a short-lived proposal with source, schedule, dependency, and policy
   revision bindings

- [ ] **Step 5: Implement source adapters and atomic apply**

`WriteAdapter.Prepare` returns a typed domain mutation but does not persist it.
`ScheduleUnitOfWork.ApplyAtomic` opens one PostgreSQL transaction and calls each
adapter's `Apply` against the same transaction.

```go
type WriteAdapter interface {
	SourceType() string
	Prepare(context.Context, authorization.Principal, RequestedChange) (PreparedChange, error)
	Apply(context.Context, ScheduleTx, PreparedChange, mutation.Evidence) error
}
```

The unit of work revalidates every revision and permission, writes all required
and accepted optional source mutations, records correlated audit rows and
outbox events, marks the proposal applied, and commits once. Any failure rolls
back the whole cascade.

Add scheduling mutation models to Work Records, tasks, project phases/resource
plans, and milestones using the Task 1 source-owned fields. Their write
adapters call domain validation shared with normal source APIs and update the
source version; they do not write projection rows directly.

Derived SLA, renewal, license, and informational custom-date roles return
`ErrReadOnlyEventRole`.

- [ ] **Step 6: Implement conflict override and proposal expiry rules**

Reject:

- expired proposal
- actor mismatch
- changed authorization
- changed source, schedule, dependency, or policy revision
- missing reason for an overrideable conflict
- any hard conflict
- accepted optional change not present in the proposal

Store the trimmed override reason and every overridden policy ID in audit
metadata.

- [ ] **Step 7: Run proposal and repository tests**

Run:

```bash
go test ./backend/internal/calendar ./backend/internal/store/psa -run 'Proposal|Schedule|WriteAdapter|Calendar' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/calendar/write_adapter.go backend/internal/calendar/write_adapter_test.go backend/internal/calendar/proposals.go backend/internal/calendar/proposals_test.go backend/internal/calendar/scheduling.go backend/internal/calendar/scheduling_test.go backend/internal/calendar/adapters backend/internal/workrecords/service.go backend/internal/tasks/service.go backend/internal/projects/model.go backend/internal/store/psa/work_record_repository.go backend/internal/store/psa/task_repository.go backend/internal/store/psa/project_repository.go backend/internal/store/psa/calendar_repository.go backend/internal/store/psa/calendar_repository_test.go
git commit -m "feat: add calendar scheduling proposals"
```

---

### Task 8: Query All Authorized Clients Without Leaking Source Details

**Files:**
- Create: `backend/internal/calendar/query.go`
- Create: `backend/internal/calendar/query_test.go`
- Create: `backend/internal/calendar/privacy.go`
- Create: `backend/internal/calendar/privacy_test.go`
- Create: `backend/internal/calendar/live.go`
- Create: `backend/internal/calendar/live_test.go`
- Modify: `backend/internal/views/service.go`
- Modify: `backend/internal/views/service_test.go`
- Modify: `backend/internal/store/psa/calendar_repository.go`
- Modify: `backend/internal/store/psa/calendar_repository_test.go`
- Modify: `backend/internal/store/psa/view_repository.go`

**Interfaces:**
- Consumes: authorized Client membership resolution, projection and occurrence
  storage, source-detail authorizer, workforce authority, tags, dependency and
  capacity services, and saved-view audiences.
- Produces:
  `QueryService.List`, `QueryService.Capacity`,
  `QueryService.FilterOptions`, `LiveService.ListAfter`,
  privacy-safe `EventView`, and `views.KindCalendarLens`.

- [ ] **Step 1: Write failing all-client query tests**

```go
func TestCalendarQueryDefaultsToAllAuthorizedClients(t *testing.T) {
	repository := &queryRepositoryStub{
		authorizedClients: []string{"client-a", "client-c"},
		events: []Projection{
			projectionForClient("a", "client-a"),
			projectionForClient("b", "client-b"),
			projectionForClient("c", "client-c"),
		},
	}
	service := NewQueryService(repository, allowSourceDetails{}, recurrenceService{})
	found, err := service.List(context.Background(), QueryRequest{
		Principal: mspPrincipal("calendar.read"),
		Window: weekWindow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEventIDs(t, found.Events, "a", "c")
}

func TestCalendarQueryDoesNotUseActiveClientAsHiddenDefault(t *testing.T) {
	principal := mspPrincipal("calendar.read")
	principal.Scope.ClientID = "client-a"
	service := allClientQueryFixture()
	found, err := service.List(context.Background(), QueryRequest{
		Principal: principal, Window: weekWindow(),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEventClients(t, found.Events, "client-a", "client-c")
}
```

- [ ] **Step 2: Write failing privacy and saved-lens tests**

```go
func TestHiddenSourceReturnsBusyBlockWithoutAggregateLeak(t *testing.T) {
	service := privacyQueryFixture()
	found, err := service.List(context.Background(), hiddenSourceRequest())
	if err != nil {
		t.Fatal(err)
	}
	event := found.Events[0]
	if event.Privacy != PrivacyBusy || event.Title != "Busy" ||
		event.ClientID != "" || len(event.Tags) != 0 || event.Source.ID != "" {
		t.Fatalf("unsafe busy block: %+v", event)
	}
}

func TestSharedCalendarLensDoesNotElevateEventAccess(t *testing.T) {
	viewService := calendarLensFixture()
	view := sharedMSPCalendarLens()
	found, err := viewService.Resolve(context.Background(), restrictedPrincipal(), view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Query["client_ids"] == "unauthorized-client" {
		t.Fatal("shared lens retained unauthorized client scope")
	}
}
```

- [ ] **Step 3: Run query tests and verify failure**

Run:

```bash
go test ./backend/internal/calendar ./backend/internal/views ./backend/internal/store/psa -run 'Test(CalendarQuery|HiddenSource|SharedCalendarLens)' -count=1
```

Expected: FAIL because calendar queries and lens kind do not exist.

- [ ] **Step 4: Implement bounded, filterable event queries**

```go
type Filter struct {
	TechnicianIDs, OwnerIDs, TeamIDs, ClientIDs []string
	TechnologyIDs, ProjectIDs, PhaseIDs, SLAIDs []string
	TicketTypes, TagIDs, Priorities, EventRoles []string
	HealthStates []HealthState
	ConflictsOnly bool
}

type QueryRequest struct {
	Principal authorization.Principal
	Window QueryWindow
	Filter Filter
	Cursor string
	Limit int
}
```

Resolve the principal's authorized Client IDs first and intersect any explicit
client filter. Query projection rows using that set before grouping or
aggregation. Expand recurrence only inside the bounded window. Agenda uses a
stable occurrence-time/ID cursor. Cap result and recurrence expansion counts
with an explicit `window_too_large` or `result_too_large` error.

- [ ] **Step 5: Implement privacy-safe busy blocks**

Full source detail requires source-read authorization. If the principal has
workforce authority to schedule the technician but cannot read the source,
return only:

```go
type PrivacyMode string
const (
	PrivacyFull PrivacyMode = "full"
	PrivacyBusy PrivacyMode = "busy"
)

EventView{
	ID: privacyStableID,
	Title: "Busy",
	Privacy: PrivacyBusy,
	AllDay: occurrence.AllDay,
	StartsOn: occurrence.StartsOn,
	EndsOn: occurrence.EndsOn,
	StartsAt: occurrence.StartsAt,
	EndsAt: occurrence.EndsAt,
	Timezone: occurrence.Timezone,
	AssigneeID: occurrence.AssigneeID,
}
```

Do not return client, source, tags, project, technology, priority, description,
or dependency identity. Apply the same redaction before capacity aggregation,
filter-option counting, notification planning, and live updates.

- [ ] **Step 6: Extend saved views and live updates**

Add `views.KindCalendarLens = "calendar_lens"` using existing Private, Team,
Department, Queue, and MSP audiences. Validate lens query keys and strip
unauthorized Client IDs at resolve time.

`LiveService.ListAfter` accepts a cursor and returns permission-filtered
upsert/remove hints. Cursors are opaque and reconnect-safe. A missing/expired
cursor returns `RefetchRequired=true`; live updates are never the only source of
truth.

- [ ] **Step 7: Run query, privacy, saved-view, and live tests**

Run:

```bash
go test ./backend/internal/calendar ./backend/internal/views ./backend/internal/store/psa -run 'Calendar|Privacy|Lens|Live' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/calendar/query.go backend/internal/calendar/query_test.go backend/internal/calendar/privacy.go backend/internal/calendar/privacy_test.go backend/internal/calendar/live.go backend/internal/calendar/live_test.go backend/internal/views/service.go backend/internal/views/service_test.go backend/internal/store/psa/calendar_repository.go backend/internal/store/psa/calendar_repository_test.go backend/internal/store/psa/view_repository.go
git commit -m "feat: add authorized calendar queries"
```

---

### Task 9: Expose Calendar, Workforce, and Commitment HTTP APIs

**Files:**
- Create: `backend/internal/httpapi/calendar_routes.go`
- Create: `backend/internal/httpapi/calendar_routes_test.go`
- Create: `backend/internal/httpapi/workforce_schedule_routes.go`
- Create: `backend/internal/httpapi/workforce_schedule_routes_test.go`
- Create: `backend/internal/httpapi/commitment_routes.go`
- Create: `backend/internal/httpapi/commitment_routes_test.go`
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/internal/httpapi/view_routes.go`
- Modify: `backend/cmd/rarity-api/main.go`

**Interfaces:**
- Consumes: Tasks 3–8 services and the existing trusted principal resolver,
  CSRF middleware, strict JSON helpers, error envelope, and route composition.
- Produces:
  calendar read/command endpoints, typed source endpoints, stable DTOs, SSE
  live feed, and complete runtime wiring.

- [ ] **Step 1: Write failing route tests**

```go
func TestCalendarRoutesUsePrincipalAuthorizedClients(t *testing.T) {
	actions := &calendarActionsStub{}
	router := testRouterWithCalendar(actions, mspPrincipal("calendar.read"))
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/calendar/events?start=2026-08-01T00:00:00Z&end=2026-08-08T00:00:00Z",
		nil)
	request.Header.Set("X-Client-ID", "client-a")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if actions.request.Filter.ClientIDs != nil {
		t.Fatal("active client header became a hidden calendar filter")
	}
}

func TestApplyProposalRequiresExpectedProposalVersionAndReason(t *testing.T) {
	router := testRouterWithCalendar(&calendarActionsStub{}, schedulingPrincipal())
	response := performJSON(router, http.MethodPost,
		"/api/v1/calendar/proposals/proposal-1/apply",
		`{"accepted_optional_change_ids":[]}`)
	assertAPIError(t, response, http.StatusBadRequest, "invalid_calendar_proposal")
}
```

- [ ] **Step 2: Write failing typed-source and SSE tests**

```go
func TestPTODecisionRejectsUnknownFields(t *testing.T) {
	router := testRouterWithWorkforce(&ptoActionsStub{}, managerPrincipal())
	response := performJSON(router, http.MethodPost,
		"/api/v1/workforce/pto/pto-1/decision",
		`{"decision":"approved","expected_version":1,"surprise":true}`)
	assertAPIError(t, response, http.StatusBadRequest, "invalid_pto_decision")
}

func TestCalendarLiveRouteReturnsRefetchEventForExpiredCursor(t *testing.T) {
	actions := &calendarActionsStub{live: calendar.LivePage{RefetchRequired: true}}
	router := testRouterWithCalendar(actions, mspPrincipal("calendar.read"))
	response := perform(router, http.MethodGet, "/api/v1/calendar/live?cursor=expired")
	if !strings.Contains(response.Body.String(), "event: refetch") {
		t.Fatalf("body = %q", response.Body.String())
	}
}
```

- [ ] **Step 3: Run route tests and verify failure**

Run:

```bash
go test ./backend/internal/httpapi -run 'Test(CalendarRoutes|ApplyProposal|PTODecision|CalendarLive)' -count=1
```

Expected: FAIL because routes are not registered.

- [ ] **Step 4: Add strict DTOs and calendar routes**

Register:

```text
GET    /api/v1/calendar/events
GET    /api/v1/calendar/capacity
GET    /api/v1/calendar/filter-options
GET    /api/v1/calendar/live
POST   /api/v1/calendar/proposals
POST   /api/v1/calendar/proposals/{id}/apply
POST   /api/v1/calendar/dependencies/preview
POST   /api/v1/calendar/dependencies
DELETE /api/v1/calendar/dependencies/{id}
GET/PUT /api/v1/calendar/conflict-policies
GET/PUT /api/v1/calendar/custom-date-fields
GET/PUT /api/v1/calendar/notification-preferences
GET/PUT /api/v1/objects/{type}/{id}/custom-date-values
```

Require RFC3339 bounded windows for timed queries and ISO dates for all-day
parameters. Do not infer timezone from server locale. Return stable error codes
for invalid window, too-large window, stale proposal, hard conflict, missing
reason, cross-client dependency, cycle, read-only role, and forbidden
workforce scope.

- [ ] **Step 5: Add typed source routes**

Register:

```text
GET/POST /api/v1/workforce/schedules
POST     /api/v1/workforce/schedules/{id}/exceptions
GET/POST /api/v1/workforce/pto
POST     /api/v1/workforce/pto/{id}/decision
POST     /api/v1/workforce/pto/{id}/cancel
GET/POST /api/v1/projects/{id}/milestones
PATCH    /api/v1/project-milestones/{id}
GET/POST /api/v1/maintenance-windows
PATCH    /api/v1/maintenance-windows/{id}
GET/POST /api/v1/commercial-commitments
PATCH    /api/v1/commercial-commitments/{id}
```

All mutations use strict JSON decoding, trusted principal scope, derived actor
ID, expected version for updates, and source-specific capabilities. Conflict
and custom-date policy routes require `calendar.policy.manage`.

- [ ] **Step 6: Wire services and projection handlers**

Construct one role registry, adapter registry, projection worker, query service,
dependency service, capacity service, proposal service, notification planner,
and AI recommender. Register relevant outbox handlers and bounded worker
intervals. Do not create a second database pool or authorization stack.

- [ ] **Step 7: Run HTTP and composition tests**

Run:

```bash
go test ./backend/internal/httpapi ./backend/cmd/rarity-api -run 'Calendar|Workforce|PTO|Milestone|Maintenance|Commitment' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/httpapi/calendar_routes.go backend/internal/httpapi/calendar_routes_test.go backend/internal/httpapi/workforce_schedule_routes.go backend/internal/httpapi/workforce_schedule_routes_test.go backend/internal/httpapi/commitment_routes.go backend/internal/httpapi/commitment_routes_test.go backend/internal/httpapi/router.go backend/internal/httpapi/view_routes.go backend/cmd/rarity-api/main.go
git commit -m "feat: expose unified calendar APIs"
```

---

### Task 10: Plan Calendar Notifications and AI Recommendations

**Files:**
- Create: `backend/internal/notifications/calendar.go`
- Create: `backend/internal/notifications/calendar_test.go`
- Create: `backend/internal/calendar/reminders.go`
- Create: `backend/internal/calendar/reminders_test.go`
- Modify: `backend/internal/notifications/planner.go`
- Modify: `backend/internal/notifications/planner_test.go`
- Modify: `backend/internal/notifications/engine.go`
- Create: `backend/internal/aiassist/calendar_recommendations.go`
- Create: `backend/internal/aiassist/calendar_recommendations_test.go`
- Modify: `backend/internal/aiassist/tools.go`
- Modify: `backend/internal/httpapi/ai_routes.go`
- Modify: `backend/internal/httpapi/ai_routes_test.go`

**Interfaces:**
- Consumes: applied proposal facts, PTO decisions, approaching-commitment
  evaluator facts, notification policy/preferences, permission-safe source
  summaries, AI provider runtime, and deterministic proposal preview.
- Produces: deduplicated calendar delivery plans and
  `CalendarPreferenceService.Get/Replace`, `ReminderService.EvaluateDue`,
  `CalendarRecommendationService.Recommend`.

- [ ] **Step 1: Write failing notification tests**

```go
func TestCalendarCascadeProducesOneDigestPerRecipient(t *testing.T) {
	planner := NewCalendarPlanner(permissionStub{allowed: true}, preferenceStub{})
	found, err := planner.Plan(context.Background(), AppliedScheduleEvent{
		CorrelationID: "cascade-1",
		Changes: []ScheduleChange{
			{AssigneeID: "tech-1", SourceTitle: "Task A"},
			{AssigneeID: "tech-1", SourceTitle: "Task B"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].RecipientID != "tech-1" {
		t.Fatalf("plans = %+v", found)
	}
}

func TestCalendarNotificationOmitsUnauthorizedSourceDetails(t *testing.T) {
	planner := NewCalendarPlanner(permissionStub{allowed: false}, preferenceStub{})
	found, err := planner.Plan(context.Background(), hiddenSourceScheduleEvent())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(found[0].Body, "Client A") || strings.Contains(found[0].Body, "VPN") {
		t.Fatalf("unsafe notification: %+v", found[0])
	}
}

func TestReminderEvaluatorEmitsEachThresholdOnce(t *testing.T) {
	repository := &reminderRepositoryStub{due: []ReminderCandidate{{
		ProjectionID: "event-1", Threshold: "24h", DueAt: fixedNow().Add(24 * time.Hour),
	}}}
	service := NewReminderService(repository, fixedNow)
	first, err := service.EvaluateDue(context.Background(), "msp", 100)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.EvaluateDue(context.Background(), "msp", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("first=%d second=%d, want 1 then 0", len(first), len(second))
	}
}
```

- [ ] **Step 2: Write failing AI recommendation tests**

```go
func TestCalendarAIProducesProposalInputButCannotApply(t *testing.T) {
	service := recommendationFixture(t)
	found, err := service.Recommend(context.Background(), RecommendCommand{
		Principal: principal("calendar.ai.recommend", "msp", ""),
		ProjectionID: "event-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Candidates) == 0 || found.Candidates[0].PreviewRequest.ProjectionID == "" {
		t.Fatalf("recommendation = %+v", found)
	}
	if service.unitOfWork.applyCalls != 0 {
		t.Fatal("AI recommendation applied a schedule")
	}
}

func TestCalendarAIExcludesUnauthorizedCandidateData(t *testing.T) {
	service := recommendationFixture(t)
	service.authorizer.hideClient("client-secret")
	found, err := service.Recommend(context.Background(), recommendCommand())
	if err != nil {
		t.Fatal(err)
	}
	assertNoClient(t, found, "client-secret")
}
```

- [ ] **Step 3: Run notification and AI tests and verify failure**

Run:

```bash
go test ./backend/internal/calendar ./backend/internal/notifications ./backend/internal/aiassist ./backend/internal/httpapi -run 'Reminder|Calendar|Schedule|Recommendation' -count=1
```

Expected: FAIL because calendar planners and recommenders do not exist.

- [ ] **Step 4: Implement notification planning**

Plan preference-controlled notifications for assignment, meaningful
rescheduling, cancellation, new conflict, approaching commitment, PTO decision,
and approved cascade. Deduplicate by correlation ID, recipient, and change
class. Aggregate cascade changes into one bounded message.

Load safe presentation at planning time and recheck access. Notification
failure must remain asynchronous and must not roll back scheduling.

Add recipient-owned calendar preferences keyed by event class, change class,
urgency, and channel. `Replace` derives the recipient from the trusted
principal, validates known values, versions the preference set, and writes
audit/outbox evidence. Organization policy and quiet periods remain the outer
delivery constraints.

- [ ] **Step 5: Implement approaching-commitment evaluation**

`ReminderService.EvaluateDue` loads a bounded page of active commitments whose
configured notification thresholds have been crossed. It atomically records a
deduplication key of projection ID, occurrence ID, threshold, and source
revision before emitting an outbox fact. A reschedule to a new source revision
may produce a new threshold fact; repeated worker runs at the same revision do
not.

Evaluate in the event's authoritative timezone and skip terminal, cancelled,
inaccessible, or preference-disabled recipients. Wire the evaluator at a
bounded interval through the existing runtime pattern.

- [ ] **Step 6: Implement recommendation-only AI**

Expose a structured provider feature that accepts permission-filtered:

- candidate technicians and skills
- available intervals and workload
- source priority and required technology
- dependency constraints
- deterministic health and conflicts

Validate provider output into:

```go
type CalendarRecommendation struct {
	Candidates []RecommendationCandidate
}

type RecommendationCandidate struct {
	TechnicianID string
	StartsAt, EndsAt *time.Time
	Allocation *AllocationRequest
	Explanation string
	Tradeoffs []string
	PreviewRequest PreviewCommand
}
```

Every candidate must pass the deterministic preview service before returning.
Do not expose an AI tool that calls `Apply`.

- [ ] **Step 7: Add AI HTTP route**

Register:

```text
POST /api/v1/ai/calendar/recommendations
```

Require `calendar.ai.recommend`, strict JSON, and a typed source projection ID.
Return explanations, tradeoffs, safe conflicts, and a preview request token,
not a mutation token.

- [ ] **Step 8: Run reminder, notification, and AI suites**

Run:

```bash
go test ./backend/internal/calendar ./backend/internal/notifications ./backend/internal/aiassist ./backend/internal/httpapi -run 'Reminder|Calendar|Schedule|Recommendation' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/calendar/reminders.go backend/internal/calendar/reminders_test.go backend/internal/notifications/calendar.go backend/internal/notifications/calendar_test.go backend/internal/notifications/planner.go backend/internal/notifications/planner_test.go backend/internal/notifications/engine.go backend/internal/aiassist/calendar_recommendations.go backend/internal/aiassist/calendar_recommendations_test.go backend/internal/aiassist/tools.go backend/internal/httpapi/ai_routes.go backend/internal/httpapi/ai_routes_test.go
git commit -m "feat: add calendar notifications and recommendations"
```

---

### Task 11: Build the Shared Calendar Browser Contract and Primitives

**Files:**
- Create: `frontend/src/features/calendar/types.ts`
- Create: `frontend/src/features/calendar/api.ts`
- Create: `frontend/src/features/calendar/api.test.ts`
- Create: `frontend/src/features/calendar/preferences.ts`
- Create: `frontend/src/features/calendar/preferences.test.ts`
- Create: `frontend/src/features/calendar/useCalendarWorkspace.ts`
- Create: `frontend/src/features/calendar/useCalendarWorkspace.test.tsx`
- Create: `frontend/src/design-system/components/data/CalendarLensSwitcher.tsx`
- Create: `frontend/src/design-system/components/data/CalendarLensSwitcher.test.tsx`
- Create: `frontend/src/design-system/components/data/CalendarEventCard.tsx`
- Create: `frontend/src/design-system/components/data/CalendarEventCard.test.tsx`
- Modify: `frontend/src/design-system/components/data/data.css`

**Interfaces:**
- Consumes: Task 9 JSON contracts, existing `FilterBar`, field components,
  saved-view endpoints, browser session/CSRF helpers, and no new UI dependency.
- Produces:
  `CalendarEvent`, `CalendarFilter`, `CalendarLens`,
  `SchedulingProposal`, API request functions, validated per-principal viewer
  timezone preferences, `useCalendarWorkspace`, `CalendarLensSwitcher`, and
  `CalendarEventCard`.

- [ ] **Step 1: Write failing API mapping tests**

```tsx
it("maps typed all-day and timed events without inventing dates", async () => {
  fetcher.mockResolvedValueOnce(jsonResponse({
    events: [
      { id: "all-day", all_day: true, starts_on: "2026-08-10", event_role: "due", title: "Due" },
      { id: "timed", all_day: false, starts_at: "2026-08-10T14:00:00Z", ends_at: "2026-08-10T15:00:00Z", timezone: "America/Chicago", event_role: "scheduled_work", title: "Work" },
    ],
    next_cursor: "",
  }));
  const found = await listCalendarEvents({
    start: "2026-08-10T00:00:00Z",
    end: "2026-08-11T00:00:00Z",
  });
  expect(found.events[0]).toMatchObject({ allDay: true, startsOn: "2026-08-10", startsAt: undefined });
  expect(found.events[1]).toMatchObject({ allDay: false, timezone: "America/Chicago" });
});
```

- [ ] **Step 2: Write failing primitive and workspace-hook tests**

```tsx
it("offers exactly the six approved lenses", async () => {
  render(<CalendarLensSwitcher value="week" onChange={vi.fn()} />);
  expect(screen.getAllByRole("button")).toHaveLength(6);
  for (const name of ["Day", "Week", "Month", "Timeline", "Capacity", "Agenda"]) {
    expect(screen.getByRole("button", { name: `${name} view` })).toBeInTheDocument();
  }
});

it("keeps client scope empty until the user applies a visible filter", async () => {
  const { result } = renderHook(() => useCalendarWorkspace());
  expect(result.current.filter.clientIDs).toEqual([]);
  expect(result.current.request.client_ids).toBeUndefined();
});

it("persists only a valid IANA viewer timezone for the current principal", () => {
  const storage = new MapStorage();
  writeCalendarPreferences(storage, "tech-1", { timezone: "America/Chicago" });
  expect(readCalendarPreferences(storage, "tech-1")).toEqual({
    timezone: "America/Chicago",
  });
  writeCalendarPreferences(storage, "tech-1", { timezone: "not/a-zone" });
  expect(readCalendarPreferences(storage, "tech-1").timezone).toBe("UTC");
});
```

- [ ] **Step 3: Run frontend contract tests and verify failure**

Run:

```bash
npm --prefix frontend test -- --run src/features/calendar/api.test.ts src/features/calendar/preferences.test.ts src/features/calendar/useCalendarWorkspace.test.tsx src/design-system/components/data/CalendarLensSwitcher.test.tsx src/design-system/components/data/CalendarEventCard.test.tsx
```

Expected: FAIL because the calendar browser modules do not exist.

- [ ] **Step 4: Implement typed API mappings**

Define exact browser types:

```ts
export type CalendarLens =
  | "day" | "week" | "month" | "timeline" | "capacity" | "agenda";

export type CalendarEvent = {
  id: string;
  occurrenceID: string;
  eventRole: string;
  title: string;
  allDay: boolean;
  startsOn?: string;
  endsOn?: string;
  startsAt?: string;
  endsAt?: string;
  timezone?: string;
  originalTimezone?: string;
  health: "blocked" | "overdue" | "at_risk" | "on_track" | "terminal";
  privacy: "full" | "busy";
  schedulingMode: "fixed_block" | "effort_allocation" | "informational";
  capabilities: { viewSource: boolean; schedule: boolean };
};
```

Add abortable reads, strict response guards, CSRF mutations, stable API errors,
Agenda cursors, capacity summaries, dependency overlays, proposal preview/apply,
typed record commands, and live cursor polling/SSE helpers.

- [ ] **Step 5: Implement shared workspace state**

`useCalendarWorkspace` owns lens, visible window, viewer timezone, filters,
saved lens, selection, proposal, loading/error state, and live cursor. It must
cancel stale requests and ignore responses from an older window/filter
revision.

Persist the viewer timezone per principal in local storage using
`rarity:calendar-preferences:<principal-id>`. Validate with
`Intl.DateTimeFormat(undefined, { timeZone })`; fall back to UTC for missing or
invalid values. Changing viewer timezone changes presentation and query window
conversion but never rewrites source timezone.

- [ ] **Step 6: Implement accessible primitives**

`CalendarLensSwitcher` exposes exactly six pressed-state buttons.
`CalendarEventCard` renders typed role, time, source-safe title, health text,
and conflict icon without color-only meaning. Busy events never render source
actions. Use existing buttons, badges, tooltip, and focus styles.

- [ ] **Step 7: Run tests and frontend build**

Run:

```bash
npm --prefix frontend test -- --run src/features/calendar src/design-system/components/data/CalendarLensSwitcher.test.tsx src/design-system/components/data/CalendarEventCard.test.tsx
npm --prefix frontend run build
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/features/calendar/types.ts frontend/src/features/calendar/api.ts frontend/src/features/calendar/api.test.ts frontend/src/features/calendar/preferences.ts frontend/src/features/calendar/preferences.test.ts frontend/src/features/calendar/useCalendarWorkspace.ts frontend/src/features/calendar/useCalendarWorkspace.test.tsx frontend/src/design-system/components/data/CalendarLensSwitcher.tsx frontend/src/design-system/components/data/CalendarLensSwitcher.test.tsx frontend/src/design-system/components/data/CalendarEventCard.tsx frontend/src/design-system/components/data/CalendarEventCard.test.tsx frontend/src/design-system/components/data/data.css
git commit -m "feat: add calendar frontend foundation"
```

---

### Task 12: Build Day, Week, Month, Agenda, Filters, and Route Integration

**Files:**
- Create: `frontend/src/features/calendar/CalendarPage.tsx`
- Create: `frontend/src/features/calendar/CalendarPage.test.tsx`
- Create: `frontend/src/features/calendar/CalendarFilters.tsx`
- Create: `frontend/src/features/calendar/CalendarFilters.test.tsx`
- Create: `frontend/src/features/calendar/DayWeekGrid.tsx`
- Create: `frontend/src/features/calendar/DayWeekGrid.test.tsx`
- Create: `frontend/src/features/calendar/MonthGrid.tsx`
- Create: `frontend/src/features/calendar/MonthGrid.test.tsx`
- Create: `frontend/src/features/calendar/AgendaView.tsx`
- Create: `frontend/src/features/calendar/AgendaView.test.tsx`
- Create: `frontend/src/features/calendar/EventDetailsPanel.tsx`
- Create: `frontend/src/features/calendar/EventDetailsPanel.test.tsx`
- Create: `frontend/src/features/calendar/calendar.css`
- Modify: `frontend/src/app/routes.ts`
- Modify: `frontend/src/app/routes.test.ts`
- Modify: `frontend/src/navigation.ts`
- Modify: `frontend/src/navigation.test.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/features/home/HomePage.tsx`
- Modify: `frontend/src/features/home/HomePage.test.tsx`

**Interfaces:**
- Consumes: Task 11 browser contracts, existing route manifest, App shell,
  PageHeader, FilterBar, date fields, saved views, and source navigation.
- Produces: protected Calendar route, four operational lenses, authorized
  filters, details panel, saved lenses, and Home upcoming-schedule summary.

- [ ] **Step 1: Write failing Calendar route and workspace tests**

```tsx
it("registers Calendar as an all-client desktop workspace", () => {
  const route = routeManifest.find(({ id }) => id === "calendar");
  expect(route).toMatchObject({
    hash: "#/calendar",
    requiresClient: false,
    phone: "handoff",
  });
  expect(route?.capabilities).toContain("calendar.read");
});

it("loads events without the selected client header becoming a filter", async () => {
  render(<CalendarPage viewerTimezone="America/Chicago" />);
  await waitFor(() => expect(fetcher).toHaveBeenCalled());
  const url = String(fetcher.mock.calls[0][0]);
  expect(url).not.toContain("client_ids=");
});
```

- [ ] **Step 2: Write failing lens and accessibility tests**

```tsx
it("renders all-day dates separately from timed events", () => {
  render(<DayWeekGrid events={[allDayEvent(), timedEvent()]} lens="week" />);
  expect(screen.getByLabelText("All-day events")).toHaveTextContent("Due");
  expect(screen.getByLabelText("Timed schedule")).toHaveTextContent("Scheduled work");
});

it("Agenda loads the next stable cursor page", async () => {
  const onLoadMore = vi.fn();
  render(<AgendaView events={[timedEvent()]} nextCursor="cursor-2" onLoadMore={onLoadMore} />);
  await userEvent.click(screen.getByRole("button", { name: "Load more" }));
  expect(onLoadMore).toHaveBeenCalledWith("cursor-2");
});
```

- [ ] **Step 3: Run workspace tests and verify failure**

Run:

```bash
npm --prefix frontend test -- --run src/features/calendar/CalendarPage.test.tsx src/features/calendar/CalendarFilters.test.tsx src/features/calendar/DayWeekGrid.test.tsx src/features/calendar/MonthGrid.test.tsx src/features/calendar/AgendaView.test.tsx src/features/calendar/EventDetailsPanel.test.tsx src/app/routes.test.ts src/navigation.test.ts src/App.test.tsx src/features/home/HomePage.test.tsx
```

Expected: FAIL because the workspace and route do not exist.

- [ ] **Step 4: Implement route, shell, controls, and saved lenses**

Add `calendar` to `RouteID` and `routeManifest` with no required Client. Render
`CalendarPage` from `App` using the principal's selected presentation timezone,
not the browser server timezone.

The header contains date navigation, Today, lens switcher, viewer-timezone
selector, typed Create, filter summary, and saved-lens controls. Changing the
viewer timezone persists the Task 11 principal preference and never changes an
event's original timezone. `CalendarFilters` covers the approved filter set and
displays Client explicitly.

- [ ] **Step 5: Implement Day, Week, Month, and Agenda**

Day and Week group by technician/team when requested and provide all-day and
timed regions. Month groups by date, shows health/density, and opens a bounded
day overflow panel. Agenda uses semantic headings, chronological rows, and
cursor pagination.

Provide keyboard actions for selecting events and initiating schedule changes;
do not make pointer dragging the only scheduling path.

- [ ] **Step 6: Implement details and source navigation**

`EventDetailsPanel` shows safe source metadata, viewer and original timezone,
health reasons, conflicts, recurrence, capacity meaning, and source action.
Privacy-busy events show only interval and availability impact.

Use existing record routes for deep links. Derived read-only roles show “Edit
on source record,” not drag handles.

- [ ] **Step 7: Replace the Home disconnected-feed empty state**

Load a small authoritative upcoming-event query from the same calendar API.
Show the next authorized events and a Calendar link. Preserve the existing
honest empty state when the API returns no commitments; remove the claim that
no authoritative feed is connected.

- [ ] **Step 8: Run workspace tests and build**

Run:

```bash
npm --prefix frontend test -- --run src/features/calendar src/app/routes.test.ts src/navigation.test.ts src/App.test.tsx src/features/home/HomePage.test.tsx
npm --prefix frontend run build
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/features/calendar frontend/src/app/routes.ts frontend/src/app/routes.test.ts frontend/src/navigation.ts frontend/src/navigation.test.ts frontend/src/App.tsx frontend/src/App.test.tsx frontend/src/features/home/HomePage.tsx frontend/src/features/home/HomePage.test.tsx
git commit -m "feat: add unified calendar workspace"
```

---

### Task 13: Add Timeline, Capacity, Drag Scheduling, and Impact Confirmation

**Files:**
- Create: `frontend/src/features/calendar/TimelineView.tsx`
- Create: `frontend/src/features/calendar/TimelineView.test.tsx`
- Create: `frontend/src/features/calendar/CapacityView.tsx`
- Create: `frontend/src/features/calendar/CapacityView.test.tsx`
- Create: `frontend/src/features/calendar/ScheduleProposalPanel.tsx`
- Create: `frontend/src/features/calendar/ScheduleProposalPanel.test.tsx`
- Create: `frontend/src/features/calendar/DependencyEditor.tsx`
- Create: `frontend/src/features/calendar/DependencyEditor.test.tsx`
- Create: `frontend/src/features/calendar/AISchedulingRecommendations.tsx`
- Create: `frontend/src/features/calendar/AISchedulingRecommendations.test.tsx`
- Create: `frontend/src/features/calendar/useCalendarScheduling.ts`
- Create: `frontend/src/features/calendar/useCalendarScheduling.test.tsx`
- Modify: `frontend/src/features/calendar/CalendarPage.tsx`
- Modify: `frontend/src/features/calendar/DayWeekGrid.tsx`
- Modify: `frontend/src/features/calendar/MonthGrid.tsx`
- Modify: `frontend/src/features/calendar/calendar.css`

**Interfaces:**
- Consumes: proposal, dependency, capacity, and apply APIs from Task 11;
  Task 12 selection and navigation.
- Produces: dependency Timeline, technician/team Capacity lens, pointer and
  keyboard scheduling, explicit recurrence scope, impact preview, reasoned
  overrides, dependency editing, AI candidate review, and apply/refetch flow.

- [ ] **Step 1: Write failing Timeline and Capacity tests**

```tsx
it("renders dependency connectors only for authorized dependency identities", () => {
  render(<TimelineView events={[eventA(), eventB(), busyEvent()]} dependencies={[edgeAB(), hiddenEdge()]} />);
  expect(screen.getByTestId("dependency-a-b")).toBeInTheDocument();
  expect(screen.queryByTestId("dependency-hidden")).not.toBeInTheDocument();
});

it("separates committed utilization from unscheduled effort", () => {
  render(<CapacityView rows={[{
    technicianID: "tech-1",
    availableMinutes: 480,
    committedMinutes: 360,
    remainingMinutes: 120,
    unscheduledMinutes: 240,
  }]} />);
  expect(screen.getByText("75% committed")).toBeInTheDocument();
  expect(screen.getByText("4h unscheduled")).toBeInTheDocument();
});
```

- [ ] **Step 2: Write failing proposal interaction tests**

```tsx
it("does not apply a cascade until the user confirms the impact", async () => {
  const preview = vi.fn().mockResolvedValue(cascadeProposal());
  const apply = vi.fn();
  render(<ScheduleHarness preview={preview} apply={apply} />);
  await moveEventWithKeyboard("Task A", "Next day");
  expect(await screen.findByRole("heading", { name: "Review schedule impact" })).toBeInTheDocument();
  expect(apply).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "Apply 3 changes" }));
  expect(apply).toHaveBeenCalledTimes(1);
});

it("requires a reason before overriding overbooking", async () => {
  render(<ScheduleProposalPanel proposal={overbookedProposal()} onApply={vi.fn()} />);
  expect(screen.getByRole("button", { name: "Apply schedule" })).toBeDisabled();
  await userEvent.type(screen.getByLabelText("Override reason"), "Dispatcher approved");
  expect(screen.getByRole("button", { name: "Apply schedule" })).toBeEnabled();
});

it("previews a dependency before creating it", async () => {
  const onPreview = vi.fn().mockResolvedValue(validDependencyPreview());
  const onCreate = vi.fn();
  render(<DependencyEditor event={eventA()} onPreview={onPreview} onCreate={onCreate} />);
  await selectDependencyTarget("Task B");
  await selectDependencyType("Finish to start");
  await userEvent.click(screen.getByRole("button", { name: "Review dependency" }));
  expect(onPreview).toHaveBeenCalled();
  expect(onCreate).not.toHaveBeenCalled();
});

it("routes an AI candidate through deterministic preview", async () => {
  const onPreview = vi.fn();
  const result = aiCandidates();
  render(<AISchedulingRecommendations result={result} onPreview={onPreview} />);
  await userEvent.click(screen.getByRole("button", { name: "Review recommendation 1" }));
  expect(onPreview).toHaveBeenCalledWith(result.candidates[0].previewRequest);
});
```

- [ ] **Step 3: Run scheduling UI tests and verify failure**

Run:

```bash
npm --prefix frontend test -- --run src/features/calendar/TimelineView.test.tsx src/features/calendar/CapacityView.test.tsx src/features/calendar/ScheduleProposalPanel.test.tsx src/features/calendar/DependencyEditor.test.tsx src/features/calendar/AISchedulingRecommendations.test.tsx src/features/calendar/useCalendarScheduling.test.tsx
```

Expected: FAIL because the components do not exist.

- [ ] **Step 4: Implement Timeline and Capacity lenses**

Timeline renders projects, tasks, milestones, and ranges with accessible
dependency connectors, violation labels, and a critical downstream highlight
based on server impact data. Do not infer hidden edges in the browser.

Capacity shows available, fixed, allocated, remaining, overbooked, tentative,
and unscheduled values by technician and team. Include text equivalents for
every bar and heat state.

- [ ] **Step 5: Implement scheduling interactions**

Use native pointer events and keyboard commands; add no drag library.
`useCalendarScheduling` converts a move/resize/allocation into a preview request
and never mutates displayed authoritative state before the server returns.

Recurring events require one of:

```ts
type OccurrenceScope =
  | "this_occurrence"
  | "this_and_future"
  | "entire_series";
```

Hard conflicts disable apply. Overrideable conflicts require a non-empty
reason. Optional cascade changes have explicit checked controls.

- [ ] **Step 6: Implement dependency editing and AI candidate review**

`DependencyEditor` searches only authorized, same-client, dependency-eligible
source roles. It supports Finish to start, Start to start, and Finish to finish,
plus signed lead/lag. It always previews cycle, constraint, health, and
capacity impact before enabling Create.

`AISchedulingRecommendations` displays explanation, technician/time or
allocation, conflicts, and tradeoffs. Its only action sends the candidate's
typed request to deterministic proposal preview. It has no direct Apply
callback.

- [ ] **Step 7: Implement stale and success behavior**

On `stale_calendar_proposal`, preserve the user's intended primary change,
request a fresh preview, and announce that the impact changed. On success,
close the panel, announce the applied count, and refetch the affected window.
Do not optimistically pretend a source mutation committed.

- [ ] **Step 8: Run calendar UI tests, accessibility checks, and build**

Run:

```bash
npm --prefix frontend test -- --run src/features/calendar
npm --prefix frontend run build
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/features/calendar/TimelineView.tsx frontend/src/features/calendar/TimelineView.test.tsx frontend/src/features/calendar/CapacityView.tsx frontend/src/features/calendar/CapacityView.test.tsx frontend/src/features/calendar/ScheduleProposalPanel.tsx frontend/src/features/calendar/ScheduleProposalPanel.test.tsx frontend/src/features/calendar/DependencyEditor.tsx frontend/src/features/calendar/DependencyEditor.test.tsx frontend/src/features/calendar/AISchedulingRecommendations.tsx frontend/src/features/calendar/AISchedulingRecommendations.test.tsx frontend/src/features/calendar/useCalendarScheduling.ts frontend/src/features/calendar/useCalendarScheduling.test.tsx frontend/src/features/calendar/CalendarPage.tsx frontend/src/features/calendar/DayWeekGrid.tsx frontend/src/features/calendar/MonthGrid.tsx frontend/src/features/calendar/calendar.css
git commit -m "feat: add calendar planning interactions"
```

---

### Task 14: Add Typed Creation, Workforce Administration, and Custom Date Configuration

**Files:**
- Create: `frontend/src/features/calendar/TypedCreateMenu.tsx`
- Create: `frontend/src/features/calendar/TypedCreateMenu.test.tsx`
- Create: `frontend/src/features/calendar/MilestoneForm.tsx`
- Create: `frontend/src/features/calendar/MilestoneForm.test.tsx`
- Create: `frontend/src/features/calendar/PTOForm.tsx`
- Create: `frontend/src/features/calendar/PTOForm.test.tsx`
- Create: `frontend/src/features/calendar/MaintenanceForm.tsx`
- Create: `frontend/src/features/calendar/MaintenanceForm.test.tsx`
- Create: `frontend/src/features/calendar/CommercialCommitmentForm.tsx`
- Create: `frontend/src/features/calendar/CommercialCommitmentForm.test.tsx`
- Create: `frontend/src/features/calendar/WorkforceScheduleSettings.tsx`
- Create: `frontend/src/features/calendar/WorkforceScheduleSettings.test.tsx`
- Create: `frontend/src/features/calendar/CalendarPolicySettings.tsx`
- Create: `frontend/src/features/calendar/CalendarPolicySettings.test.tsx`
- Create: `frontend/src/features/calendar/RecurrenceEditor.tsx`
- Create: `frontend/src/features/calendar/RecurrenceEditor.test.tsx`
- Create: `frontend/src/features/calendar/CalendarNotificationPreferences.tsx`
- Create: `frontend/src/features/calendar/CalendarNotificationPreferences.test.tsx`
- Create: `frontend/src/features/calendar/CustomDateEditor.tsx`
- Create: `frontend/src/features/calendar/CustomDateEditor.test.tsx`
- Modify: `frontend/src/features/calendar/CalendarPage.tsx`
- Modify: `frontend/src/features/organization/DirectorySettingsPage.tsx`
- Modify: `frontend/src/features/organization/DirectorySettingsPage.test.tsx`
- Modify: `frontend/src/features/projects/ProjectWorklist.tsx`
- Modify: `frontend/src/features/projects/ProjectWorklist.test.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.test.tsx`
- Modify: `frontend/src/features/organization/ClientResourcesPage.tsx`
- Modify: `frontend/src/features/organization/ClientResourcesPage.test.tsx`
- Modify: `frontend/src/features/knowledge/KnowledgePage.tsx`
- Modify: `frontend/src/features/knowledge/KnowledgePage.test.tsx`
- Modify: `frontend/src/features/timesheets/TimesheetPage.tsx`
- Modify: `frontend/src/features/timesheets/TimesheetPage.test.tsx`

**Interfaces:**
- Consumes: Task 11 typed APIs, Task 9 endpoints, existing forms/dialogs,
  project and directory workspaces, and capability-aware route controls.
- Produces: typed creation only, schedule/PTO administration, manager approval,
  maintenance scopes, commercial relationships, milestone editing, conflict
  policy settings, custom date-field configuration, structured recurrence, and
  recipient notification preferences.

- [ ] **Step 1: Write failing typed-create tests**

```tsx
it("offers typed records and no generic event option", async () => {
  render(<TypedCreateMenu capabilities={allCalendarCapabilities()} />);
  await userEvent.click(screen.getByRole("button", { name: "Create" }));
  for (const name of ["Milestone", "PTO request", "Maintenance window", "Renewal", "License"]) {
    expect(screen.getByRole("menuitem", { name })).toBeInTheDocument();
  }
  expect(screen.queryByRole("menuitem", { name: /generic event/i })).not.toBeInTheDocument();
});

it("preserves a date-only milestone without adding a time", async () => {
  const onSubmit = vi.fn();
  render(<MilestoneForm projectID="project-1" onSubmit={onSubmit} />);
  await userEvent.type(screen.getByLabelText("Name"), "Cutover");
  await userEvent.type(screen.getByLabelText("Date"), "2026-09-15");
  await userEvent.click(screen.getByRole("button", { name: "Create milestone" }));
  expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
    due_on: "2026-09-15",
    starts_at: undefined,
  }));
});
```

- [ ] **Step 2: Write failing workforce and policy tests**

```tsx
it("shows requested PTO as tentative until manager approval", () => {
  render(<WorkforceScheduleSettings model={scheduleWithRequestedPTO()} />);
  expect(screen.getByText("Tentative")).toBeInTheDocument();
  expect(screen.getByText("Does not reduce committed capacity")).toBeInTheDocument();
});

it("requires an IANA timezone for a timed custom field", async () => {
  render(<CalendarPolicySettings capabilities={new Set(["calendar.policy.manage"])} />);
  await enableCustomDateField("Maintenance start", "date_time");
  await userEvent.click(screen.getByRole("button", { name: "Save calendar policy" }));
  expect(screen.getByText("Choose an IANA timezone source")).toBeInTheDocument();
});

it("creates a bounded selected-weekday recurrence rule", async () => {
  const onChange = vi.fn();
  render(<RecurrenceEditor value={undefined} onChange={onChange} />);
  await chooseFrequency("Weekly");
  await userEvent.click(screen.getByRole("checkbox", { name: "Monday" }));
  await userEvent.click(screen.getByRole("checkbox", { name: "Wednesday" }));
  await chooseRecurrenceEnd("After occurrences");
  await userEvent.type(screen.getByLabelText("Occurrence count"), "12");
  expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({
    frequency: "weekly", weekdays: [1, 3], count: 12,
  }));
});

it("saves recipient calendar notification preferences", async () => {
  const onSave = vi.fn();
  render(<CalendarNotificationPreferences value={defaultCalendarPreferences()} onSave={onSave} />);
  await userEvent.click(screen.getByRole("checkbox", { name: "Rescheduling email" }));
  await userEvent.click(screen.getByRole("button", { name: "Save notification preferences" }));
  expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ version: 1 }));
});

it("submits a custom date value with no invented timestamp", async () => {
  const onSave = vi.fn();
  render(<CustomDateEditor definitions={[dateDefinition()]} values={[]} onSave={onSave} />);
  await userEvent.type(screen.getByLabelText("Follow-up date"), "2026-09-01");
  await userEvent.click(screen.getByRole("button", { name: "Save custom dates" }));
  expect(onSave).toHaveBeenCalledWith([{
    field_id: dateDefinition().id,
    date_value: "2026-09-01",
  }]);
});
```

- [ ] **Step 3: Run creation and settings tests and verify failure**

Run:

```bash
npm --prefix frontend test -- --run src/features/calendar/TypedCreateMenu.test.tsx src/features/calendar/MilestoneForm.test.tsx src/features/calendar/PTOForm.test.tsx src/features/calendar/MaintenanceForm.test.tsx src/features/calendar/CommercialCommitmentForm.test.tsx src/features/calendar/WorkforceScheduleSettings.test.tsx src/features/calendar/CalendarPolicySettings.test.tsx src/features/calendar/RecurrenceEditor.test.tsx src/features/calendar/CalendarNotificationPreferences.test.tsx src/features/calendar/CustomDateEditor.test.tsx
```

Expected: FAIL because typed forms and settings do not exist.

- [ ] **Step 4: Implement typed creation forms**

Use existing DatePicker, TimeInput, Select, MultiSelect, Dialog, validation
summary, and form actions. Date-only forms submit only ISO dates. Timed forms
require timezone and submit RFC3339 instants plus the original IANA timezone.

Maintenance scopes use typed client/service/asset selectors. Commercial forms
require vendor, owner, effective/expiration dates, and conditionally validate
quantity, cost, currency, and related client resources.

`RecurrenceEditor` supports daily, weekly, monthly, yearly, selected weekdays,
end count or date, and no raw recurrence-rule text. Maintenance and commercial
forms use it directly.

- [ ] **Step 5: Implement workforce schedule and PTO administration**

Provide recurring weekly windows, effective dates, timezone, dated exceptions,
PTO request, manager decision, and workforce-admin override. Prevent overlapping
weekly windows client-side while retaining server validation.

Requested PTO uses a tentative label. Approved PTO shows capacity impact.
Private details remain hidden when the response is privacy-safe.

- [ ] **Step 6: Implement conflict and custom-date policy settings**

Administrators configure severity for approved PTO, non-working time, and
protected maintenance. The UI explains hard versus overrideable behavior.

Custom date configuration requires object type, field ID, label, category,
read-only/schedulable mode, and optional capacity behavior. It never permits a
date-only field to choose a default clock time.

- [ ] **Step 7: Implement recipient notification preferences**

Render known calendar event classes, change classes, urgency, and available
channels from the server contract. Save with the current expected version.
Explain that organization policy and quiet periods still apply. Do not expose
source details in preference labels or counts.

- [ ] **Step 8: Integrate project milestones and directory schedules**

Add milestones to the project workspace and workforce schedules/PTO to
Directory Settings using the same typed components. Calendar Create routes to
these domain forms with context preselected instead of duplicating record logic.

Embed `CustomDateEditor` in authorized Work Record/task, project, asset,
knowledge-article, and time-entry detail surfaces. It renders only active
definitions returned for that source type and preserves date versus
date-time/timezone values.

- [ ] **Step 9: Run focused and full frontend tests**

Run:

```bash
npm --prefix frontend test -- --run src/features/calendar src/features/organization/DirectorySettingsPage.test.tsx src/features/projects/ProjectWorklist.test.tsx src/features/work/TechnicianWorklist.test.tsx src/features/organization/ClientResourcesPage.test.tsx src/features/knowledge/KnowledgePage.test.tsx src/features/timesheets/TimesheetPage.test.tsx
npm --prefix frontend run build
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add frontend/src/features/calendar frontend/src/features/organization/DirectorySettingsPage.tsx frontend/src/features/organization/DirectorySettingsPage.test.tsx frontend/src/features/projects/ProjectWorklist.tsx frontend/src/features/projects/ProjectWorklist.test.tsx frontend/src/features/work/TechnicianWorklist.tsx frontend/src/features/work/TechnicianWorklist.test.tsx frontend/src/features/organization/ClientResourcesPage.tsx frontend/src/features/organization/ClientResourcesPage.test.tsx frontend/src/features/knowledge/KnowledgePage.tsx frontend/src/features/knowledge/KnowledgePage.test.tsx frontend/src/features/timesheets/TimesheetPage.tsx frontend/src/features/timesheets/TimesheetPage.test.tsx
git commit -m "feat: add typed calendar administration"
```

---

### Task 15: Add Demo Data, Operations, Documentation, Security, and Release Verification

**Files:**
- Create: `backend/internal/calendar/demo_seed.go`
- Create: `backend/internal/calendar/demo_seed_test.go`
- Modify: `backend/cmd/rarity-admin/main.go`
- Create: `backend/internal/calendar/performance_test.go`
- Create: `backend/internal/calendar/security_integration_test.go`
- Modify: `docs/02-platform/technicians.md`
- Modify: `docs/02-platform/projects.md`
- Modify: `docs/02-platform/tickets.md`
- Create: `docs/02-platform/unified-calendar.md`
- Modify: `docs/04-api/rest-api.md`
- Modify: `docs/03-security/permission-matrix.md`
- Modify: `docs/03-security/client-isolation-test-model.md`
- Modify: `docs/01-architecture/13-event-catalog.md`
- Modify: `docs/09-roadmap/build-backlog.md`
- Modify: `scripts/validate-docs.mjs`

**Interfaces:**
- Consumes: the complete backend/frontend feature and repository verification
  commands.
- Produces: representative opt-in demo seed, reconciliation CLI, performance
  evidence, cross-client privacy proof, canonical documentation, and release
  gate.

- [ ] **Step 1: Write failing demo and security tests**

```go
func TestCalendarDemoSeedCoversEveryEventAndHealthClass(t *testing.T) {
	found := BuildDemoSeed(DemoSeedInput{
		MSPID: "msp", ClientIDs: []string{"client-a", "client-b"},
		TechnicianIDs: []string{"tech-a", "tech-b"}, Now: fixedNow(),
	})
	assertContainsSourceTypes(t, found,
		"work_record", "task", "project", "milestone", "pto",
		"maintenance_window", "commercial_commitment")
	assertContainsHealth(t, found, "blocked", "overdue", "at_risk", "on_track")
	assertContainsSchedulingModes(t, found, "fixed_block", "effort_allocation", "informational")
}

func TestCalendarCrossClientSecurityMatrix(t *testing.T) {
	fixture := calendarSecurityFixture(t)
	restricted := fixture.queryAs(clientRestrictedPrincipal("client-a"))
	assertOnlyClients(t, restricted.FullEvents, "client-a")
	assertNoSourceDetails(t, restricted.BusyEvents)
	assertNoUnauthorizedDimensions(t, restricted.FilterOptions, restricted.Capacity)
}
```

- [ ] **Step 2: Run demo and security tests and verify failure**

Run:

```bash
go test ./backend/internal/calendar -run 'Test(CalendarDemoSeed|CalendarCrossClientSecurityMatrix)' -count=1
```

Expected: FAIL because demo seeding and the integrated security fixture do not
exist.

- [ ] **Step 3: Implement opt-in demo seed and admin commands**

`BuildDemoSeed` creates deterministic command inputs for two clients, two
technicians, every typed record, date-only/timed events, two timezones,
recurrence exceptions, dependencies, capacity shortage, PTO states, conflict
severities, and all health states.

Add explicit admin commands:

```text
rarity-admin calendar-demo-seed --msp-id <uuid>
rarity-admin calendar-reconcile --msp-id <uuid> --limit 500
rarity-admin calendar-reconcile --msp-id <uuid> --limit 500 --repair
```

Never run the demo seed automatically at startup or migration time.

- [ ] **Step 4: Add performance and security coverage**

Build a deterministic fixture with at least:

- 1,000 authorized clients
- 50 concurrent technicians
- 90-day window
- 100,000 projections
- recurring series and exceptions
- a 100-node dependency graph

Measure broad event queries, dense Week/Month windows, Timeline dependencies,
team Capacity, recurrence expansion, and cascade preview. Assert the existing
ADR-0030 interactive-read p95 target of 500 ms for in-process repository
fixtures and record database integration measurements without weakening the
published release gate.

Security tests cover event rows, counts, filters, capacity, dependencies,
saved lenses, notifications, AI inputs, live updates, and deep links.

- [ ] **Step 5: Document the complete contract**

Document:

- normalized projection and source ownership
- typed source/event-role catalog
- all-client authorization behavior
- date/time/recurrence semantics
- scheduling modes and proposal flow
- dependency and capacity rules
- deterministic health
- privacy-safe busy blocks
- typed routes and stable error codes
- capabilities and notification policy
- reconciliation and metrics
- Rarity-native-only integration boundary

Update the event catalog with all new outbox event types and the permission
matrix with every calendar capability. Mark the calendar backlog item complete
only after the full verification step passes.

- [ ] **Step 6: Extend docs validation**

Require `docs/02-platform/unified-calendar.md`, its navigation entry, the
calendar route catalog, and all capability names. Keep existing validator
requirements intact.

Run:

```bash
node scripts/validate-docs.mjs
```

Expected: PASS.

- [ ] **Step 7: Run focused backend verification**

Run:

```bash
go test ./backend/internal/calendar/... ./backend/internal/workforce ./backend/internal/commitments ./backend/internal/projects ./backend/internal/views ./backend/internal/notifications ./backend/internal/aiassist ./backend/internal/httpapi ./backend/internal/store/psa ./backend/migrations -count=1
```

Expected: PASS.

- [ ] **Step 8: Run focused frontend verification**

Run:

```bash
npm --prefix frontend test -- --run src/features/calendar src/features/home/HomePage.test.tsx src/features/organization/DirectorySettingsPage.test.tsx src/features/projects/ProjectWorklist.test.tsx src/app/routes.test.ts src/navigation.test.ts src/App.test.tsx
npm --prefix frontend run build
```

Expected: PASS.

- [ ] **Step 9: Run repository verification**

Run:

```bash
go test ./backend/...
npm --prefix frontend test -- --run
npm --prefix frontend run build
node scripts/validate-docs.mjs
docker compose --env-file .env -f infrastructure/compose/compose.yaml config --quiet
git diff --check
```

Expected: every command exits 0. If `.env` is absent, run
`make local-env` once and rerun the Compose validation; do not commit generated
secrets.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/calendar/demo_seed.go backend/internal/calendar/demo_seed_test.go backend/internal/calendar/performance_test.go backend/internal/calendar/security_integration_test.go backend/cmd/rarity-admin/main.go docs/02-platform/technicians.md docs/02-platform/projects.md docs/02-platform/tickets.md docs/02-platform/unified-calendar.md docs/04-api/rest-api.md docs/03-security/permission-matrix.md docs/03-security/client-isolation-test-model.md docs/01-architecture/13-event-catalog.md docs/09-roadmap/build-backlog.md scripts/validate-docs.mjs
git commit -m "docs: complete unified calendar release"
```

---

## Plan Completion Gate

Before claiming implementation completion:

- [ ] Every calendar event resolves to a typed source and exact event role.
- [ ] Built-in and configured date fields preserve date/date-time semantics.
- [ ] Day, Week, Month, Timeline, Capacity, and Agenda share one read model.
- [ ] Default queries span all and only authorized clients.
- [ ] Recurrence, occurrence scopes, exceptions, timezones, and DST pass.
- [ ] Fixed blocks and effort allocations produce correct capacity results.
- [ ] Requested PTO remains tentative; approved PTO reduces capacity.
- [ ] Dependency types, same-client validation, cycles, lead/lag, and cascade
  previews pass.
- [ ] No cascade, override, recurrence edit, or AI recommendation applies
  without human approval.
- [ ] Conflict warnings, reason-required overrides, and hard blocks pass.
- [ ] Health state and reason codes are deterministic.
- [ ] Full details, aggregates, filters, saved lenses, notifications, AI inputs,
  live updates, and deep links pass isolation tests.
- [ ] Projection retries and reconciliation never mutate source dates.
- [ ] No generic event UI/API, external calendar sync, or legacy backfill was
  introduced.
- [ ] Backend, frontend, migration, documentation, security, performance, and
  Compose verification all pass.
