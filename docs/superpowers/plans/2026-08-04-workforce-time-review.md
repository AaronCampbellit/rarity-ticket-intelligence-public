# Workforce Time Review Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver ticket-specific timers, labor-role rate snapshots, weekly timesheets, and scoped Time Reviewer workflows.

**Architecture:** Extend the existing `timeentries`, authorization, billing-export, ticket-comment, and ticket-email application services. PostgreSQL owns timer timestamps, capture consumption, effective-dated labor roles, and immutable snapshots; React renders server state and never calculates trusted duration or rates.

**Tech Stack:** Go 1.24, PostgreSQL/pgx, goose migrations, React 19, TypeScript, Vite, Vitest/Testing Library.

## Global Constraints

- A timer belongs to one ticket; there is no technician-wide single-timer lock.
- Stopped timer captures are server-owned and may be consumed exactly once.
- Labor roles are separate from authorization roles.
- Time entries snapshot effective labor-role version, internal cost, bill rate, and currency.
- The MSP timezone and Monday-start boundary define weekly timesheets.
- Pending entries may be reasonedly amended; approved entries require reversal and replacement.
- Every trusted mutation writes audit and outbox evidence atomically.

---

## File Structure

- `backend/internal/timeentries/labor_roles.go`: labor-role validation and effective-rate commands.
- `backend/internal/timeentries/timers.go`: ticket timer lifecycle and capture consumption.
- `backend/internal/timeentries/timesheets.go`: weekly queries, amendments, and reversals.
- `backend/internal/store/psa/time_workforce_repository.go`: PostgreSQL labor-role, timer, and timesheet persistence.
- `backend/internal/httpapi/time_workforce_routes.go`: versioned workforce endpoints.
- `frontend/src/features/work/TicketTimer.tsx`: ticket timer and stopped-capture UI.
- `frontend/src/features/timesheets/`: technician and reviewer pages.
- `frontend/src/features/directory/LaborRoleSettings.tsx`: labor-role settings.

### Task 1: Add workforce schema and capability contract

**Files:**
- Create: `backend/migrations/000072_workforce_time_review.sql`
- Modify: `backend/migrations/contract_test.go`
- Modify: `backend/internal/setup/postgres.go`

**Interfaces:**
- Produces `labor_roles`, `labor_role_versions`, `ticket_timer_sessions`, and `time_entry_amendments`.
- Adds snapshot columns to `time_entries`.
- Adds capabilities `timesheet.read_own`, `timesheet.review`, `time_entry.read_scoped`, `time_entry.update_own`, and `time_entry.amend`.

- [x] **Step 1: Write a failing migration contract**

```go
func TestWorkforceTimeReviewMigrationDefinesTrustedCaptureAndSnapshots(t *testing.T) {
	body, err := FS.ReadFile("000072_workforce_time_review.sql")
	if err != nil { t.Fatal(err) }
	sql := string(body)
	for _, fragment := range []string{
		"CREATE TABLE labor_roles",
		"CREATE TABLE labor_role_versions",
		"CREATE TABLE ticket_timer_sessions",
		"state IN ('running', 'stopped', 'consumed', 'discarded')",
		"CREATE UNIQUE INDEX ticket_timer_one_running_per_ticket_technician",
		"ADD COLUMN labor_role_version_id",
		"ADD COLUMN internal_cost_minor",
		"ADD COLUMN bill_rate_minor",
		"CREATE TABLE time_entry_amendments",
	} {
		if !strings.Contains(sql, fragment) { t.Fatalf("missing %q", fragment) }
	}
}
```

- [x] **Step 2: Verify the contract is red**

Run: `go test ./backend/migrations -run TestWorkforceTimeReviewMigration -count=1`  
Expected: FAIL because migration 72 is absent.

- [x] **Step 3: Add the constrained schema**

```sql
CREATE TABLE labor_roles (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  key text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  UNIQUE (msp_id, key),
  UNIQUE (id, msp_id)
);

CREATE TABLE labor_role_versions (
  id uuid PRIMARY KEY,
  labor_role_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  name text NOT NULL,
  internal_cost_minor bigint NOT NULL CHECK (internal_cost_minor >= 0),
  bill_rate_minor bigint NOT NULL CHECK (bill_rate_minor >= 0),
  currency char(3) NOT NULL,
  effective_from timestamptz NOT NULL,
  effective_until timestamptz,
  enabled boolean NOT NULL,
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  FOREIGN KEY (labor_role_id, msp_id) REFERENCES labor_roles(id, msp_id),
  CHECK (effective_until IS NULL OR effective_until > effective_from)
);

CREATE TABLE ticket_timer_sessions (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  work_record_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  state text NOT NULL CHECK (state IN ('running', 'stopped', 'consumed', 'discarded')),
  started_at timestamptz NOT NULL,
  stopped_at timestamptz,
  duration_seconds bigint,
  consumed_time_entry_id uuid,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CHECK ((state = 'running' AND stopped_at IS NULL AND duration_seconds IS NULL)
    OR (state <> 'running' AND stopped_at IS NOT NULL AND duration_seconds > 0))
);

CREATE UNIQUE INDEX ticket_timer_one_running_per_ticket_technician
  ON ticket_timer_sessions(msp_id, client_id, work_record_id, technician_id)
  WHERE state = 'running';
```

Add immutable snapshot columns to `time_entries`, an amendment evidence table with before/after JSON, and the five capabilities to the setup catalog and Global Administrator baseline.

- [x] **Step 4: Verify migrations and setup**

Run: `go test ./backend/migrations ./backend/internal/setup -count=1`  
Expected: PASS.

- [x] **Step 5: Commit the schema**

```bash
git add backend/migrations/000072_workforce_time_review.sql backend/migrations/contract_test.go backend/internal/setup/postgres.go
git commit -m "feat: define workforce time review schema"
```

### Task 2: Implement labor roles and immutable rate resolution

**Files:**
- Create: `backend/internal/timeentries/labor_roles.go`
- Create: `backend/internal/timeentries/labor_roles_test.go`
- Create: `backend/internal/store/psa/time_workforce_repository.go`
- Create: `backend/internal/store/psa/time_workforce_repository_test.go`

**Interfaces:**
- Produces `LaborRole`, `LaborRoleVersion`, `CreateLaborRole`, `VersionLaborRole`, and `ResolveLaborRate`.
- `ResolveLaborRate(ctx, mspID, laborRoleID, technicianID string, at time.Time) (RateSnapshot, error)`.

- [x] **Step 1: Write failing domain tests**

```go
func TestResolveLaborRateUsesEffectiveRoleAndTechnicianOverride(t *testing.T) {
	repo := &laborRepositoryStub{snapshot: RateSnapshot{
		LaborRoleVersionID: "role-v2", InternalCostMinor: 4500,
		BillRateMinor: 15000, Currency: "USD",
	}}
	service := NewLaborRoleService(repo, fixedNow, nextID)
	got, err := service.ResolveLaborRate(context.Background(), principalWith("time_entry.create"),
		"role", "tech", time.Date(2026, 8, 4, 14, 0, 0, 0, time.UTC))
	if err != nil { t.Fatal(err) }
	if got.LaborRoleVersionID != "role-v2" || got.InternalCostMinor != 4500 { t.Fatalf("%+v", got) }
}
```

- [x] **Step 2: Verify red**

Run: `go test ./backend/internal/timeentries -run LaborRole -count=1`  
Expected: FAIL because the labor-role service is absent.

- [x] **Step 3: Implement domain types and service**

```go
type RateSnapshot struct {
	LaborRoleVersionID string
	InternalCostMinor  int64
	BillRateMinor      int64
	Currency           string
}

type LaborRoleRepository interface {
	CreateLaborRoleAtomic(context.Context, LaborRoleMutation) error
	VersionLaborRoleAtomic(context.Context, LaborRoleVersionMutation) error
	ResolveLaborRate(context.Context, string, string, string, time.Time) (RateSnapshot, error)
}
```

Require `organization.manage` for role management and `time_entry.create` for capture-time resolution. Normalize currency to uppercase ISO-style three-letter codes and reject overlapping effective ranges.

- [x] **Step 4: Add PostgreSQL persistence and verify**

Run: `go test ./backend/internal/timeentries ./backend/internal/store/psa -run 'LaborRole|LaborRate' -count=1`  
Expected: PASS.

- [x] **Step 5: Commit labor roles**

```bash
git add backend/internal/timeentries/labor_roles.go backend/internal/timeentries/labor_roles_test.go backend/internal/store/psa/time_workforce_repository.go backend/internal/store/psa/time_workforce_repository_test.go
git commit -m "feat: add effective labor roles"
```

### Task 3: Implement ticket-specific timer lifecycle

**Files:**
- Create: `backend/internal/timeentries/timers.go`
- Create: `backend/internal/timeentries/timers_test.go`
- Modify: `backend/internal/store/psa/time_workforce_repository.go`
- Modify: `backend/internal/store/psa/time_workforce_repository_test.go`

**Interfaces:**
- Produces `StartTimer`, `StopTimer`, `DiscardTimer`, and `ConsumeCapture`.
- `ConsumeCapture` returns authoritative start, end, duration, and resolved rates for `Service.Create`.

- [x] **Step 1: Write failing lifecycle tests**

```go
func TestTechnicianCanRunTimersOnDifferentTicketsAndConsumeEachOnce(t *testing.T) {
	repo := newTimerRepositoryStub()
	service := NewTimerService(repo, fixedNow, nextID)
	first, err := service.Start(ctx, StartTimerCommand{Principal: tech, WorkRecordID: "ticket-1", IdempotencyKey: "a"})
	if err != nil { t.Fatal(err) }
	second, err := service.Start(ctx, StartTimerCommand{Principal: tech, WorkRecordID: "ticket-2", IdempotencyKey: "b"})
	if err != nil { t.Fatal(err) }
	if first.ID == second.ID { t.Fatal("ticket timers must be independent") }
	stopped, err := service.Stop(ctx, StopTimerCommand{Principal: tech, ID: first.ID, ExpectedVersion: 1})
	if err != nil { t.Fatal(err) }
	if stopped.DurationSeconds != 900 { t.Fatalf("%+v", stopped) }
	if _, err := service.Consume(ctx, ConsumeCaptureCommand{Principal: tech, ID: stopped.ID, ExpectedVersion: 2}); err != nil { t.Fatal(err) }
	if _, err := service.Consume(ctx, ConsumeCaptureCommand{Principal: tech, ID: stopped.ID, ExpectedVersion: 2}); !errors.Is(err, ErrCaptureConsumed) { t.Fatalf("%v", err) }
}
```

- [x] **Step 2: Verify red**

Run: `go test ./backend/internal/timeentries -run Timer -count=1`  
Expected: FAIL because the timer service is absent.

- [x] **Step 3: Implement server-owned state transitions**

```go
type TimerSession struct {
	ID, MSPID, ClientID, WorkRecordID, TechnicianID string
	State TimerState
	StartedAt time.Time
	StoppedAt *time.Time
	DurationSeconds int64
	ConsumedTimeEntryID string
	Version int64
}
```

Authorize every command against the ticket Client, compute duration from repository-locked server timestamps, and require optimistic versions. Start uses an idempotency key; consume records the resulting time-entry ID in the same transaction.

- [x] **Step 4: Verify domain and repository concurrency**

Run: `go test ./backend/internal/timeentries ./backend/internal/store/psa -run 'Timer|Capture' -count=1`  
Expected: PASS.

- [x] **Step 5: Commit timer lifecycle**

```bash
git add backend/internal/timeentries/timers.go backend/internal/timeentries/timers_test.go backend/internal/store/psa/time_workforce_repository.go backend/internal/store/psa/time_workforce_repository_test.go
git commit -m "feat: add ticket-specific timers"
```

### Task 4: Expose timer routes and ticket composer objects

**Status:** Durable per-ticket timer controls, one-time capture consumption into
a labor-role-backed Time Entry, and atomic composition with an internal note or
public client reply are implemented. Outbound email composition remains pending
because the product does not yet provide an outbound email delivery service
with the required transaction boundary.

**Files:**
- Create: `backend/internal/httpapi/time_workforce_routes.go`
- Create: `backend/internal/httpapi/time_workforce_routes_test.go`
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/internal/httpapi/work_record_routes.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Create: `frontend/src/features/work/TicketTimer.tsx`
- Create: `frontend/src/features/work/TicketTimer.test.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.tsx`
- Modify: `frontend/src/features/work/api.ts`

**Interfaces:**
- Adds `/api/v1/work-records/{id}/timers`, `/api/v1/timers/{id}:stop`, and `/api/v1/timers/{id}:discard`.
- Ticket note/email requests accept `{time_capture:{id,expected_version,labor_role_id,billable,idempotency_key}}`.

- [ ] **Step 1: Write failing HTTP and component tests**

```tsx
it("stops a ticket timer and attaches its server duration to the composer", async () => {
	render(<TicketTimer workRecordID="ticket-1" api={api} onCapture={onCapture} />);
	await user.click(screen.getByRole("button", { name: "Start timer" }));
	await user.click(screen.getByRole("button", { name: "Stop timer" }));
	expect(onCapture).toHaveBeenCalledWith(expect.objectContaining({ id: "capture-1", duration_seconds: 900 }));
});
```

- [ ] **Step 2: Verify red**

Run: `go test ./backend/internal/httpapi -run TimeWorkforce -count=1 && npm --prefix frontend test -- --run TicketTimer`  
Expected: Go or Vitest FAIL because routes/components are absent.

- [ ] **Step 3: Implement routes and atomic composition**

Decode strict bounded bodies, use the authenticated technician as actor, and reject browser duration/rate fields. Coordinate note/email creation with capture consumption and time-entry creation through a repository transaction boundary.

- [ ] **Step 4: Implement ticket UI and verify**

Run: `go test ./backend/internal/httpapi -run 'TimeWorkforce|WorkRecord' -count=1 && npm --prefix frontend test -- --run TicketTimer TechnicianWorklist`  
Expected: PASS.

- [ ] **Step 5: Commit ticket capture**

```bash
git add backend/internal/httpapi backend/cmd/rarity-api/main.go frontend/src/features/work
git commit -m "feat: compose ticket time with notes and email"
```

### Task 5: Add weekly timesheets and reviewer amendments

**Files:**
- Create: `backend/internal/timeentries/timesheets.go`
- Create: `backend/internal/timeentries/timesheets_test.go`
- Modify: `backend/internal/store/psa/time_workforce_repository.go`
- Modify: `backend/internal/httpapi/time_workforce_routes.go`
- Create: `frontend/src/features/timesheets/TimesheetPage.tsx`
- Create: `frontend/src/features/timesheets/TimesheetPage.test.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/navigation.ts`

**Interfaces:**
- Adds own and reviewer weekly queries, pending amendment, rejection, approval, and approved reversal/replacement.

- [x] **Step 1: Write failing week, scope, and immutability tests**

```go
func TestApprovedEntryRequiresReversalAndReplacement(t *testing.T) {
	service := NewTimesheetService(repoWithApprovedEntry(), fixedNow, nextID)
	_, err := service.Amend(ctx, AmendCommand{Principal: reviewer, EntryID: "approved", ExpectedVersion: 2, Reason: "correct duration"})
	if !errors.Is(err, ErrApprovedEntryImmutable) { t.Fatalf("%v", err) }
	result, err := service.ReverseAndReplace(ctx, ReverseCommand{Principal: reviewer, EntryID: "approved", ExpectedVersion: 2, Reason: "correct duration", Replacement: replacement})
	if err != nil || result.Original.ReversedAt == nil { t.Fatalf("%+v %v", result, err) }
}
```

- [x] **Step 2: Verify red**

Run: `go test ./backend/internal/timeentries -run Timesheet -count=1`  
Expected: FAIL because the service is absent.

- [x] **Step 3: Implement server-side MSP week and scoped commands**

```go
type Week struct { StartsAt, EndsAt time.Time; Timezone string }
type TimesheetRow struct {
	Entry Entry
	ClientName, WorkItemTitle, LaborRoleName, ApprovalState string
}
```

Load the MSP IANA timezone, derive Monday 00:00 boundaries server-side, and authorize own versus reviewer queries separately. Persist amendment before/after evidence and reason.

- [x] **Step 4: Implement pages and verify**

Run: `go test ./backend/internal/timeentries ./backend/internal/httpapi ./backend/internal/store/psa -run Timesheet -count=1 && npm --prefix frontend test -- --run TimesheetPage`  
Expected: PASS.

- [x] **Step 5: Commit timesheets**

```bash
git add backend/internal/timeentries backend/internal/store/psa/time_workforce_repository.go backend/internal/httpapi/time_workforce_routes.go frontend/src/features/timesheets frontend/src/App.tsx frontend/src/navigation.ts
git commit -m "feat: add weekly time review"
```

### Task 6: Add Time Reviewer role assignment and labor-role settings

**Files:**
- Modify: `backend/internal/authorization/role_management.go`
- Modify: `backend/internal/authorization/role_management_test.go`
- Modify: `backend/internal/store/authn/roles.go`
- Modify: `backend/internal/httpapi/role_management_routes.go`
- Modify: `frontend/src/features/settings/RoleManagementPage.tsx`
- Create: `frontend/src/features/directory/LaborRoleSettings.tsx`
- Create: `frontend/src/features/directory/LaborRoleSettings.test.tsx`

**Interfaces:**
- Adds reasoned role creation and principal assignment commands.
- Seeds immutable-key system role `time_reviewer`.

- [x] **Step 1: Write failing assignment and settings tests**

```go
func TestAssignTimeReviewerPreservesClientScope(t *testing.T) {
	assignment, err := service.Assign(ctx, manager, AssignRoleCommand{
		PrincipalID: "reviewer", RoleKey: "time_reviewer", ClientID: "client-1",
		Reason: "review client time",
	})
	if err != nil { t.Fatal(err) }
	if assignment.ClientID != "client-1" { t.Fatalf("%+v", assignment) }
}
```

- [x] **Step 2: Verify red**

Run: `go test ./backend/internal/authorization ./backend/internal/httpapi -run 'AssignRole|TimeReviewer' -count=1`  
Expected: FAIL because assignment commands are absent.

- [x] **Step 3: Implement reasoned create/assign and settings UI**

System-role keys remain immutable; assignment requires `role.manage`, a current principal, valid scope, reason, audit, and outbox. Labor-role settings call Task 2 routes and show effective versions rather than editing historical rows.

- [x] **Step 4: Run portable acceptance**

Run: `go test ./backend/... -count=1 && npm --prefix frontend test -- --run && npm --prefix frontend run build`  
Expected: PASS.

- [x] **Step 5: Commit and update roadmap/docs tracker**

```bash
git add backend frontend docs/09-roadmap/implementation-plan.md docs-site
git commit -m "feat: complete workforce time review"
```
