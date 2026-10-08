# Native PSA Opportunities and Projects Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Rarity's native Opportunity-to-Project PSA lifecycle, including configurable sales pipelines, versioned proposals and approvals, atomic Project conversion, Phases, resource capacity, Project financials, and Change Orders.

**Architecture:** Extend the Go modular monolith with separate `sales` and `projects` domain packages that use the shared identity, authorization, client-isolation, audit, outbox, task, attachment, and time-entry services. Expose `/api/v1` REST resources through thin HTTP adapters and consume them from focused React/TypeScript feature modules. Deliver Sales/Proposals first, then Project Delivery/Financials, with the conversion service as the explicit transactional boundary.

**Tech Stack:** Go, PostgreSQL, `pgx`, `sqlc`, Goose SQL migrations, React, TypeScript, Vite, Go unit/integration tests, Vitest, React Testing Library, and Playwright.

## Global Constraints

- Rarity is the PSA system of record; Opportunities and Projects are native first-class domains.
- Preserve MSP and Client isolation on every create, read, update, relationship, search, export, and delete path.
- Use optimistic concurrency and atomically write business state, audit history, and durable outbox events.
- Proposal Versions and the accepted commercial baseline are immutable.
- Project conversion is previewable, atomic, and idempotent.
- Move only user-selected incomplete Opportunity tasks; preserve task identity and history.
- Projects use ordered Phases and subtasks; do not introduce Milestones or task dependencies.
- Change Order override uses normal Project/Change Order update authorization, requires a reason, and does not add a dedicated override permission.
- Native invoicing, payments, and accounting synchronization are out of scope.
- AI and automation cannot autonomously approve, convert, or alter commercial or delivery records.

## Planned file structure

- `backend/internal/sales/`: Prospect, pipeline, Opportunity, Proposal, approval, forecast, and application services.
- `backend/internal/projects/`: Project, Phase, resource plan, financial, Change Order, and conversion application services.
- `backend/internal/tasks/`: shared task re-parenting contract used by Opportunities and Projects.
- `backend/internal/httpapi/`: `/api/v1` request/response adapters only; no business rules.
- `backend/internal/store/sql/`: SQL source files consumed by `sqlc`.
- `backend/internal/store/generated/`: generated typed query code; never hand-edit.
- `backend/migrations/`: Goose migrations with reversible schema changes.
- `frontend/src/features/sales/`: pipeline, Opportunity, Proposal, approval, and forecast UI.
- `frontend/src/features/projects/`: conversion, Project planning, capacity, financials, and Change Order UI.
- `tests/integration/`: PostgreSQL-backed domain, isolation, transaction, and API tests.
- `tests/e2e/`: Playwright user-journey tests.

---

### Task 1: Establish PSA schemas and generated data access

**Files:**
- Create: `backend/migrations/000010_psa_sales.sql`
- Create: `backend/migrations/000011_psa_projects.sql`
- Create: `backend/internal/store/sql/sales.sql`
- Create: `backend/internal/store/sql/projects.sql`
- Test: `tests/integration/psa_schema_test.go`

**Interfaces:**
- Produces: typed persistence for `Prospect`, `Pipeline`, `PipelineStage`, `Opportunity`, `Proposal`, `ProposalVersion`, `ProposalLine`, `Approval`, `Project`, `Phase`, `ResourcePlan`, `ProjectBudget`, `CostActual`, and `ChangeOrder`.
- Produces: uniqueness key `opportunity_conversions.opportunity_id` for idempotent conversion.

- [ ] **Step 1: Write the failing migration integration test**

```go
func TestPSASchemaEnforcesScopeVersionAndConversionUniqueness(t *testing.T) {
    db := testdb.OpenMigrated(t)
    assertTableHasColumns(t, db, "opportunities", "id", "msp_id", "client_id", "version")
    assertTableHasColumns(t, db, "projects", "id", "msp_id", "client_id", "original_proposal_version_id", "version")
    assertUniqueConstraint(t, db, "opportunity_conversions", "opportunity_id")
}
```

- [ ] **Step 2: Run the test and verify the schema is absent**

Run: `go test ./tests/integration -run TestPSASchemaEnforcesScopeVersionAndConversionUniqueness -count=1`
Expected: FAIL because the PSA tables do not exist.

- [ ] **Step 3: Add reversible Goose migrations and typed queries**

```sql
CREATE TABLE opportunities (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid,
  prospect_id uuid,
  pipeline_id uuid NOT NULL,
  stage_id uuid NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CHECK ((client_id IS NULL) <> (prospect_id IS NULL))
);
```

Add foreign keys, scope indexes, immutable Proposal Version tables, Project/Phase financial tables, Change Order versions, and the idempotent conversion record. Define named `sqlc` queries for inserts, version-checked updates, scoped reads, and ordered lists.

- [ ] **Step 4: Generate queries and run migration tests**

Run: `sqlc generate && go test ./tests/integration -run TestPSASchemaEnforcesScopeVersionAndConversionUniqueness -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/migrations backend/internal/store tests/integration/psa_schema_test.go
git commit -m "feat: add native PSA persistence"
```

### Task 2: Implement Prospects, pipelines, Opportunities, and forecasts

**Files:**
- Create: `backend/internal/sales/model.go`
- Create: `backend/internal/sales/repository.go`
- Create: `backend/internal/sales/service.go`
- Create: `backend/internal/sales/forecast.go`
- Test: `backend/internal/sales/service_test.go`
- Test: `tests/integration/sales_isolation_test.go`

**Interfaces:**
- Produces: `sales.Service.CreateProspect(ctx, command) (Prospect, error)`.
- Produces: `sales.Service.CreatePipeline(ctx, command) (Pipeline, error)`.
- Produces: `sales.Service.TransitionOpportunity(ctx, id, expectedVersion, stageID) (Opportunity, error)`.
- Produces: `sales.Forecast.Calculate(opportunities, stages) Forecast`.

- [ ] **Step 1: Write failing stage-transition and forecast tests**

```go
func TestTransitionRequiresConfiguredFieldsAndAllowedEdge(t *testing.T) {
    svc := newSalesService(t)
    _, err := svc.TransitionOpportunity(ctx, opportunityID, 3, proposalStageID)
    require.ErrorIs(t, err, sales.ErrStageRequirements)
}

func TestForecastUsesConfiguredProbabilityAndCommittedCategory(t *testing.T) {
    got := sales.CalculateForecast([]sales.ForecastInput{
        {Amount: money.USD(10000), Probability: 40, Category: sales.Weighted},
        {Amount: money.USD(5000), Probability: 100, Category: sales.Committed},
    })
    require.Equal(t, money.USD(9000), got.WeightedRevenue)
    require.Equal(t, money.USD(5000), got.CommittedRevenue)
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./backend/internal/sales -run 'TestTransition|TestForecast' -count=1`
Expected: FAIL because the sales service does not exist.

- [ ] **Step 3: Implement the sales domain and service**

```go
type PipelineStage struct {
    ID PipelineStageID
    Probability uint8
    RequiredFields []FieldKey
    AllowedNext []PipelineStageID
    RequiresProposal bool
    RequiresApproval bool
}

func (s *Service) TransitionOpportunity(
    ctx context.Context,
    id OpportunityID,
    expectedVersion int64,
    stageID PipelineStageID,
) (Opportunity, error)
```

Validate stage configuration, required fields, legal transitions, quote/approval gates, scope, and expected version before persisting and emitting `opportunity.stage.changed`.

- [ ] **Step 4: Add cross-client denial tests and run the package**

Run: `go test ./backend/internal/sales ./tests/integration -run 'Sales|Opportunity|Prospect|Pipeline|Forecast' -count=1`
Expected: PASS, including denial when an authenticated client scope differs from the Opportunity scope.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/sales tests/integration/sales_isolation_test.go
git commit -m "feat: add opportunities and configurable pipelines"
```

### Task 3: Implement versioned Proposals and approvals

**Files:**
- Create: `backend/internal/sales/proposal.go`
- Create: `backend/internal/sales/approval.go`
- Create: `backend/internal/sales/proposal_service.go`
- Test: `backend/internal/sales/proposal_service_test.go`
- Test: `tests/integration/proposal_immutability_test.go`

**Interfaces:**
- Produces: `sales.ProposalService.IssueVersion(ctx, command) (ProposalVersion, error)`.
- Produces: `sales.ProposalService.AcceptElectronically(ctx, command) (Acceptance, error)`.
- Produces: `sales.ProposalService.RecordOfflineAcceptance(ctx, command) (Acceptance, error)`.
- Produces: `sales.ProposalService.GetAcceptedSnapshot(ctx, proposalID) (PDFSnapshot, error)`.

- [ ] **Step 1: Write failing immutability, line-type, and acceptance tests**

```go
func TestIssuedProposalVersionCannotBeMutated(t *testing.T) {
    svc := newProposalService(t)
    issued := issueProposal(t, svc)
    err := svc.UpdateIssuedVersion(ctx, issued.ID, issued.Version, validLines())
    require.ErrorIs(t, err, sales.ErrProposalVersionImmutable)
}

func TestOfflineAcceptanceRecordsExactVersionAndRecorder(t *testing.T) {
    accepted, err := svc.RecordOfflineAcceptance(ctx, sales.OfflineAcceptanceCommand{
        ProposalVersionID: versionID, SignerName: "Alex Client",
        RecordedBy: technicianID, AcceptedAt: acceptedAt,
    })
    require.NoError(t, err)
    require.Equal(t, versionID, accepted.ProposalVersionID)
    require.NotEmpty(t, accepted.PDFSnapshotID)
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./backend/internal/sales -run 'Proposal|Acceptance|Approval' -count=1`
Expected: FAIL because Proposal services are undefined.

- [ ] **Step 3: Implement versioning, four line types, and both acceptance paths**

```go
type ProposalLineType string
const (
    FixedFee ProposalLineType = "fixed_fee"
    TimeAndMaterials ProposalLineType = "time_and_materials"
    ProductLicense ProposalLineType = "product_license"
    RecurringService ProposalLineType = "recurring_service"
)
```

Require money currency consistency, non-negative quantities, explicit tax treatment, calculated margin, rule-based internal approval, signer evidence, and immutable PDF snapshot storage.

- [ ] **Step 4: Run unit and PostgreSQL immutability tests**

Run: `go test ./backend/internal/sales ./tests/integration -run 'Proposal|Acceptance|Approval' -count=1`
Expected: PASS; direct attempts to update issued-version rows also fail.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/sales tests/integration/proposal_immutability_test.go
git commit -m "feat: add versioned proposals and acceptance"
```

### Task 4: Extend shared Tasks for Opportunity-to-Project movement

**Files:**
- Create: `backend/internal/tasks/reparent.go`
- Modify: `backend/internal/tasks/service.go`
- Test: `backend/internal/tasks/reparent_test.go`
- Test: `tests/integration/task_history_test.go`

**Interfaces:**
- Produces: `tasks.Service.MoveIncomplete(ctx, command) ([]Task, error)`.
- Consumes: Opportunity and Project IDs under the same MSP/Client scope.

- [ ] **Step 1: Write failing selective-movement tests**

```go
func TestMoveIncompletePreservesIdentityAndLeavesCompletedTasks(t *testing.T) {
    result, err := svc.MoveIncomplete(ctx, tasks.MoveCommand{
        From: opportunityRef, To: projectRef,
        Selected: []tasks.ID{incompleteID, completedID},
    })
    require.NoError(t, err)
    require.Equal(t, projectRef, result[0].Parent)
    require.Equal(t, incompleteID, result[0].ID)
    require.Equal(t, opportunityRef, loadTask(t, completedID).Parent)
}
```

- [ ] **Step 2: Run the tests and verify failure**

Run: `go test ./backend/internal/tasks ./tests/integration -run 'MoveIncomplete|TaskHistory' -count=1`
Expected: FAIL because task movement is not implemented.

- [ ] **Step 3: Implement guarded re-parenting**

```go
type MoveCommand struct {
    From object.Ref
    To object.Ref
    Selected []ID
    Actor identity.Principal
}
```

Reject completed, cross-MSP, cross-Client, stale, or unauthorized moves. Update only the parent reference while preserving task identity, subtasks, ownership, estimates, attachments, comments, and history; append a movement history record.

- [ ] **Step 4: Run focused and isolation tests**

Run: `go test ./backend/internal/tasks ./tests/integration -run 'Task|Move|Isolation' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/tasks tests/integration/task_history_test.go
git commit -m "feat: support selective opportunity task conversion"
```

### Task 5: Implement Projects, Phases, subtasks, and resource plans

**Files:**
- Create: `backend/internal/projects/model.go`
- Create: `backend/internal/projects/repository.go`
- Create: `backend/internal/projects/service.go`
- Create: `backend/internal/projects/capacity.go`
- Test: `backend/internal/projects/service_test.go`
- Test: `tests/integration/project_isolation_test.go`

**Interfaces:**
- Produces: `projects.Service.Create(ctx, command) (Project, error)`.
- Produces: `projects.Service.UpdatePhase(ctx, command) (Phase, error)`.
- Produces: `projects.CapacityService.Calculate(ctx, window, resources) (CapacityView, error)`.
- Consumes: shared Task service for Phase tasks and subtasks.

- [ ] **Step 1: Write failing Phase and capacity tests**

```go
func TestProjectUsesOrderedPhasesWithoutMilestonesOrDependencies(t *testing.T) {
    project := createProject(t, svc, []projects.PhaseInput{{Name: "Discover"}, {Name: "Deliver"}})
    require.Equal(t, []int{1, 2}, phasePositions(project))
    require.False(t, project.SupportsMilestones)
    require.False(t, project.SupportsTaskDependencies)
}

func TestCapacityShowsOverbooking(t *testing.T) {
    got := capacity.Calculate(40*time.Hour, 48*time.Hour, 12*time.Hour)
    require.Equal(t, 20*time.Hour, got.Overbooked)
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./backend/internal/projects -run 'Project|Phase|Capacity' -count=1`
Expected: FAIL because the Projects package is absent.

- [ ] **Step 3: Implement Project, Phase, resource plan, and capacity services**

```go
type Phase struct {
    ID PhaseID
    Position int
    OwnerID identity.UserID
    PlannedStart time.Time
    PlannedEnd time.Time
    PlannedHours duration.Hours
    Budget money.Amount
    Deliverables []Deliverable
    CompletionCriteria []Criterion
}
```

Support Phase-level role/team hours and task-level named assignments. Calculate availability, scheduled work, actual time, and overbooking without automatically modifying plans.

- [ ] **Step 4: Run package and isolation tests**

Run: `go test ./backend/internal/projects ./tests/integration -run 'Project|Phase|Capacity|Resource|Isolation' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects tests/integration/project_isolation_test.go
git commit -m "feat: add project phases and resource capacity"
```

### Task 6: Implement conversion preview and atomic conversion

**Files:**
- Create: `backend/internal/projects/conversion.go`
- Create: `backend/internal/projects/conversion_service.go`
- Test: `backend/internal/projects/conversion_service_test.go`
- Test: `tests/integration/opportunity_conversion_test.go`

**Interfaces:**
- Produces: `projects.ConversionService.Preview(ctx, command) (ConversionPreview, error)`.
- Produces: `projects.ConversionService.Convert(ctx, command) (ConversionResult, error)`.
- Consumes: accepted Proposal Version, Prospect/Client matching, Project/Phase repository, and `tasks.Service.MoveIncomplete`.

- [ ] **Step 1: Write failing preview, rollback, and retry tests**

```go
func TestConvertIsAtomicAndIdempotent(t *testing.T) {
    command := validConversionCommand()
    first, err := svc.Convert(ctx, command)
    require.NoError(t, err)
    second, err := svc.Convert(ctx, command)
    require.NoError(t, err)
    require.Equal(t, first.ProjectID, second.ProjectID)
}

func TestConvertRollsBackEveryWriteWhenTaskMovementFails(t *testing.T) {
    taskMover.FailWith(tasks.ErrCrossClient)
    _, err := svc.Convert(ctx, validConversionCommand())
    require.Error(t, err)
    require.Zero(t, countProjects(t, db))
    require.Equal(t, sales.Open, loadOpportunity(t).State)
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./backend/internal/projects ./tests/integration -run 'Conversion|Convert' -count=1`
Expected: FAIL because the conversion service is absent.

- [ ] **Step 3: Implement preview validation and one-transaction conversion**

```go
type ConversionCommand struct {
    OpportunityID sales.OpportunityID
    AcceptedProposalVersionID sales.ProposalVersionID
    ExpectedOpportunityVersion int64
    ClientMatch *clients.ID
    Phases []PhaseMapping
    SelectedTaskIDs []tasks.ID
    IdempotencyKey string
}
```

Validate acceptance, Client/Prospect data, ownership, Proposal Line mappings, money, planned hours, and selected incomplete tasks. Within one PostgreSQL transaction, create or match Client, create Project and Phases, lock the original baseline, move tasks, mark the Opportunity won, store the conversion result, and write audit/outbox rows.

- [ ] **Step 4: Run transaction, retry, duplicate-Client, and isolation tests**

Run: `go test ./backend/internal/projects ./tests/integration -run 'Conversion|Convert|Prospect|DuplicateClient|Isolation' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects tests/integration/opportunity_conversion_test.go
git commit -m "feat: convert opportunities into projects atomically"
```

### Task 7: Implement Project financials and Change Orders

**Files:**
- Create: `backend/internal/projects/financials.go`
- Create: `backend/internal/projects/change_order.go`
- Create: `backend/internal/projects/change_order_service.go`
- Test: `backend/internal/projects/financials_test.go`
- Test: `tests/integration/change_order_test.go`

**Interfaces:**
- Produces: `projects.FinancialService.Calculate(ctx, projectID) (FinancialSummary, error)`.
- Produces: `projects.ChangeOrderService.IssueVersion(ctx, command) (ChangeOrderVersion, error)`.
- Produces: `projects.ChangeOrderService.OverrideApproval(ctx, command) (ChangeOrderVersion, error)`.
- Produces: `projects.ChangeOrderService.Apply(ctx, command) (Project, error)`.

- [ ] **Step 1: Write failing baseline, profitability, and override tests**

```go
func TestAppliedChangeOrderPreservesOriginalBaseline(t *testing.T) {
    before := loadProject(t)
    applied := approveAndApplyChangeOrder(t, svc, money.USD(2000))
    require.Equal(t, before.OriginalBudget, applied.OriginalBudget)
    require.Equal(t, before.CurrentBudget.Add(money.USD(2000)), applied.CurrentBudget)
}

func TestOverrideUsesNormalUpdateAccessAndRequiresReason(t *testing.T) {
    _, err := svc.OverrideApproval(ctx, projects.OverrideCommand{
        VersionID: changeVersionID, Reason: "",
    })
    require.ErrorIs(t, err, projects.ErrOverrideReasonRequired)
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./backend/internal/projects ./tests/integration -run 'Financial|Profit|ChangeOrder|Override' -count=1`
Expected: FAIL because financial and Change Order services are absent.

- [ ] **Step 3: Implement financial calculations and versioned Change Orders**

```go
type FinancialSummary struct {
    OriginalBudget money.Amount
    CurrentBudget money.Amount
    PlannedLabor money.Amount
    ActualLabor money.Amount
    CostActuals money.Amount
    CommittedCost money.Amount
    BillableWork money.Amount
    Profit money.Amount
    MarginPercent decimal.Decimal
}
```

Use normal Change Order update authorization for override, require a non-empty reason, and record actor, timestamp, exact version, previous state, and audit event. Apply an approved version once, updating current scope/budget/hours without changing the original baseline.

- [ ] **Step 4: Run financial, authorization, concurrency, and audit tests**

Run: `go test ./backend/internal/projects ./tests/integration -run 'Financial|Profit|ChangeOrder|Override|Concurrency|Audit' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/projects tests/integration/change_order_test.go
git commit -m "feat: add project financials and change orders"
```

### Task 8: Expose PSA REST APIs and event contracts

**Files:**
- Create: `backend/internal/httpapi/sales_routes.go`
- Create: `backend/internal/httpapi/project_routes.go`
- Create: `backend/internal/httpapi/psa_dto.go`
- Modify: `backend/internal/httpapi/router.go`
- Test: `backend/internal/httpapi/psa_routes_test.go`
- Test: `tests/integration/psa_events_test.go`

**Interfaces:**
- Produces: `/api/v1/prospects`, `/api/v1/pipelines`, `/api/v1/opportunities`, `/api/v1/proposals`, `/api/v1/projects`, and `/api/v1/change-orders`.
- Produces: explicit actions `/proposal-versions/{id}/accept`, `/opportunities/{id}/conversion-preview`, `/opportunities/{id}/convert`, and `/change-order-versions/{id}/override-approval`.

- [ ] **Step 1: Write failing HTTP contract tests**

```go
func TestConvertRequiresExpectedVersionAndIdempotencyKey(t *testing.T) {
    res := request(t, "POST", "/api/v1/opportunities/"+id+"/convert", `{}`)
    require.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
    requireErrorCode(t, res, "validation_failed")
}
```

- [ ] **Step 2: Run HTTP tests and verify routes are missing**

Run: `go test ./backend/internal/httpapi -run PSA -count=1`
Expected: FAIL with route-not-found assertions.

- [ ] **Step 3: Implement thin handlers and stable DTOs**

```go
type ConvertOpportunityRequest struct {
    ExpectedVersion int64 `json:"expected_version"`
    IdempotencyKey string `json:"idempotency_key"`
    AcceptedProposalVersionID string `json:"accepted_proposal_version_id"`
    Phases []PhaseMappingDTO `json:"phases"`
    SelectedTaskIDs []string `json:"selected_task_ids"`
}
```

Map domain validation, authorization, not-found, and concurrency errors to canonical API errors without revealing inaccessible object existence. Return `ETag` for versioned resources.

- [ ] **Step 4: Run API and event-envelope tests**

Run: `go test ./backend/internal/httpapi ./tests/integration -run 'PSA|Event|Outbox' -count=1`
Expected: PASS with `opportunity.*`, `proposal.*`, `project.*`, `phase.*`, and `change_order.*` events using the canonical envelope.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi tests/integration/psa_events_test.go
git commit -m "feat: expose native PSA APIs"
```

### Task 9: Build Sales and Proposal user experiences

**Files:**
- Create: `frontend/src/features/sales/api.ts`
- Create: `frontend/src/features/sales/types.ts`
- Create: `frontend/src/features/sales/PipelinePage.tsx`
- Create: `frontend/src/features/sales/OpportunityPage.tsx`
- Create: `frontend/src/features/sales/ProposalEditor.tsx`
- Create: `frontend/src/features/sales/ForecastView.tsx`
- Test: `frontend/src/features/sales/OpportunityPage.test.tsx`
- Test: `frontend/src/features/sales/ProposalEditor.test.tsx`

**Interfaces:**
- Consumes: PSA Sales REST endpoints and ETag concurrency.
- Produces: accessible pipeline, Opportunity, Proposal version, approval, and forecast interfaces.

- [ ] **Step 1: Write failing UI tests for stage gates and immutable versions**

```tsx
it("blocks a gated transition and focuses the first missing field", async () => {
  render(<OpportunityPage opportunity={opportunityMissingProposal} />);
  await user.click(screen.getByRole("button", { name: "Move to Proposal" }));
  expect(await screen.findByText("A proposal is required for this stage.")).toBeVisible();
  expect(screen.getByLabelText("Proposal")).toHaveFocus();
});
```

- [ ] **Step 2: Run focused frontend tests and verify failure**

Run: `npm --prefix frontend test -- --run src/features/sales`
Expected: FAIL because Sales components do not exist.

- [ ] **Step 3: Implement API hooks and focused Sales pages**

```ts
export async function transitionOpportunity(
  id: string,
  expectedVersion: number,
  stageId: string,
): Promise<Opportunity>
```

Show pipeline aging, probability, required-field errors, activities, tasks, all Proposal Versions, four line types, margin/discount approval state, acceptance evidence, and weighted/committed forecast totals. Preserve entered values on concurrency conflict.

- [ ] **Step 4: Run accessibility-focused component tests and build**

Run: `npm --prefix frontend test -- --run src/features/sales && npm --prefix frontend run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/features/sales
git commit -m "feat: add sales and proposal workspace"
```

### Task 10: Build Project conversion and delivery user experiences

**Files:**
- Create: `frontend/src/features/projects/api.ts`
- Create: `frontend/src/features/projects/types.ts`
- Create: `frontend/src/features/projects/ConversionPreview.tsx`
- Create: `frontend/src/features/projects/ProjectPage.tsx`
- Create: `frontend/src/features/projects/CapacityView.tsx`
- Create: `frontend/src/features/projects/FinancialSummary.tsx`
- Create: `frontend/src/features/projects/ChangeOrderEditor.tsx`
- Test: `frontend/src/features/projects/ConversionPreview.test.tsx`
- Test: `frontend/src/features/projects/ChangeOrderEditor.test.tsx`

**Interfaces:**
- Consumes: conversion preview, Project, Phase, capacity, financial, and Change Order APIs.
- Produces: editable conversion mapping and the Project delivery workspace.

- [ ] **Step 1: Write failing conversion-selection and override tests**

```tsx
it("selects only incomplete opportunity tasks for conversion", async () => {
  render(<ConversionPreview preview={previewWithCompletedTask} />);
  expect(screen.getByRole("checkbox", { name: "Schedule kickoff" })).toBeEnabled();
  expect(screen.getByRole("checkbox", { name: "Qualify prospect" })).toBeDisabled();
});

it("requires an override reason without asking for a special permission", async () => {
  render(<ChangeOrderEditor changeOrder={pendingChangeOrder} />);
  await user.click(screen.getByRole("button", { name: "Override approval" }));
  expect(await screen.findByText("Enter a reason for the override.")).toBeVisible();
});
```

- [ ] **Step 2: Run focused frontend tests and verify failure**

Run: `npm --prefix frontend test -- --run src/features/projects`
Expected: FAIL because Project components do not exist.

- [ ] **Step 3: Implement conversion and Project delivery pages**

```ts
export async function convertOpportunity(
  opportunityId: string,
  request: ConvertOpportunityRequest,
): Promise<ConversionResult>
```

Provide Phase mapping, task selection, Client match review, ownership, planned hours, and budget review before conversion. Show ordered Phases, subtasks, resource capacity, original/current financial baselines, profitability, Change Order versions, normal approvals, and audited override reason.

- [ ] **Step 4: Run component tests and production build**

Run: `npm --prefix frontend test -- --run src/features/projects && npm --prefix frontend run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/features/projects
git commit -m "feat: add project delivery workspace"
```

### Task 11: Verify end-to-end PSA journeys and operational qualities

**Files:**
- Create: `tests/e2e/opportunity_to_project.spec.ts`
- Create: `tests/e2e/change_order.spec.ts`
- Create: `tests/e2e/project_capacity.spec.ts`
- Create: `docs/06-development/psa-acceptance.md`

**Interfaces:**
- Consumes: complete Sales, Proposal, conversion, Project, financial, and Change Order stack.
- Produces: executable acceptance evidence for the initial native PSA release.

- [ ] **Step 1: Write failing end-to-end journeys**

```ts
test("prospect opportunity becomes a budgeted project", async ({ page }) => {
  await createProspectOpportunity(page);
  await issueAndAcceptProposal(page);
  await previewAndConfirmConversion(page);
  await expect(page.getByText("Original commercial baseline")).toBeVisible();
  await expect(page.getByText("Qualify prospect")).not.toBeVisible();
  await expect(page.getByText("Schedule kickoff")).toBeVisible();
});
```

- [ ] **Step 2: Run journeys and record initial failures**

Run: `npx playwright test tests/e2e/opportunity_to_project.spec.ts tests/e2e/change_order.spec.ts tests/e2e/project_capacity.spec.ts`
Expected: FAIL until route wiring, fixtures, and any discovered integration gaps are completed.

- [ ] **Step 3: Fix only observed integration gaps and document acceptance commands**

```markdown
## Native PSA acceptance

1. Create a Prospect Opportunity in a configurable pipeline.
2. Issue and accept an immutable Proposal Version.
3. Convert with reviewed Client, Phase, budget, hours, and task mappings.
4. Verify original/current financial baselines and resource capacity.
5. Override and apply a Change Order with a recorded reason.
```

- [ ] **Step 4: Run the full quality gate**

Run: `go test ./... && npm --prefix frontend test -- --run && npm --prefix frontend run build && npx playwright test tests/e2e/opportunity_to_project.spec.ts tests/e2e/change_order.spec.ts tests/e2e/project_capacity.spec.ts`
Expected: all commands PASS with no skipped PSA acceptance journeys.

- [ ] **Step 5: Commit**

```bash
git add tests/e2e docs/06-development/psa-acceptance.md
git commit -m "test: verify native PSA lifecycle"
```

### Task 12: Synchronize API documentation and release evidence

**Files:**
- Create: `docs/04-api/psa-opportunities-projects.md`
- Create: `scripts/validate-docs.mjs`
- Modify: `docs/02-platform/opportunities.md`
- Modify: `docs/02-platform/projects.md`
- Modify: `docs/09-roadmap/implementation-plan.md`
- Modify: `docs/09-roadmap/build-backlog.md`
- Modify: `docs-site/app.js`

**Interfaces:**
- Consumes: implemented routes, event names, error codes, and acceptance evidence.
- Produces: canonical operator/developer documentation that matches the released behavior.

- [ ] **Step 1: Add a documentation-contract check**

```js
const requiredPSARoutes = [
  "/api/v1/opportunities",
  "/api/v1/proposals",
  "/api/v1/projects",
  "/api/v1/change-orders",
];
```

Assert each implemented route and event family appears in `docs/04-api/psa-opportunities-projects.md`, and every new Markdown document appears in the tracker manifest.

- [ ] **Step 2: Run documentation validation and verify missing coverage**

Run: `node scripts/validate-docs.mjs`
Expected: FAIL with the exact undocumented routes or untracked Markdown paths.

- [ ] **Step 3: Document final contracts and acceptance evidence**

```markdown
| Action | Endpoint | Concurrency |
|---|---|---|
| Preview conversion | `POST /api/v1/opportunities/{id}/conversion-preview` | Opportunity ETag |
| Convert | `POST /api/v1/opportunities/{id}/convert` | ETag plus idempotency key |
| Override Change Order | `POST /api/v1/change-order-versions/{id}/override-approval` | Change Order ETag |
```

Update statuses and roadmap completion only where test evidence exists; do not mark native invoicing, payments, or accounting synchronization complete.

- [ ] **Step 4: Run documentation and full release gates**

Run: `node scripts/validate-docs.mjs && git diff --check && go test ./... && npm --prefix frontend test -- --run && npm --prefix frontend run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add docs docs-site/app.js scripts/validate-docs.mjs
git commit -m "docs: publish native PSA implementation contracts"
```
