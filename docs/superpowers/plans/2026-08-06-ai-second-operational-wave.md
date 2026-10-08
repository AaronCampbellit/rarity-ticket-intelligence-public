# AI Second Operational Wave Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add safe Ticket creation/assignment, Opportunity and Proposal
operations, and internal Knowledge publication to the MSP-wide AI workspace.

**Architecture:** Extend ordinary domain services with shared read-only
preflight, exact-reference queries, selection fences, and reasoned audit
contracts before registering any AI tool. Each closed tool prepares canonical
identities, previews exact safe effects, reauthorizes and re-previews on
confirmation, then delegates to the ordinary writer. High-impact financial,
contractual, credential, integration, automation, and configuration actions
remain absent.

**Tech Stack:** Go 1.25, PostgreSQL 16/pgx/Goose, React 19, TypeScript, Vitest,
Docker Compose demo deployment, Hank Notes Kanban.

## Global Constraints

- The AI workspace remains MSP-wide; page Client is a hint, never action scope.
- Every Client-bound action resolves one explicit authorized active Client.
- The user supplies every business value; the system generates only trusted
  IDs, timestamps, actor, correlation, and causation metadata.
- Unknown JSON fields and invalid enums fail closed.
- Exact resolvers return at most two candidates and preserve typed zero-match
  and ambiguity errors.
- Confirmation reloads principal, Client, target, references, versions, and
  configuration before any writer runs.
- Rejection, expiry, ambiguity, invalid input, authorization loss, and drift
  write no domain, audit, or outbox row.
- AI tools call ordinary application services with source `ai_workspace`;
  they never write repositories directly.
- Proposal issue/approval/acceptance/conversion, pricing, Project financials,
  Change Orders, time/billing, configuration, automation, integrations,
  credentials, secrets, and external calls remain excluded.
- Preserve unrelated work. Stage only files owned by each task.
- PostgreSQL destructive fixture cleanup runs only against an isolated scratch
  database supplied through `TEST_DATABASE_URL`.

---

## File and responsibility map

- `backend/internal/workrecords/service.go`: shared Ticket create selection,
  preflight result, and execution fence.
- `backend/internal/workrecords/assignment.go`: reasoned assignment command and
  no-op/version enforcement.
- `backend/internal/workrecords/assignment_directory.go`: safe technician
  candidate contract.
- `backend/internal/store/psa/technician_directory_repository.go`: bounded
  effective-scope technician resolution.
- `backend/internal/aiassist/rtitools/ticket_create_tool.go`: closed
  `ticket.create` preparation, preview, and execution.
- `backend/internal/aiassist/rtitools/ticket_assignment_tool.go`: closed
  `ticket.assign` preparation, preview, and execution.
- `backend/internal/sales/service.go`: explicit-target Opportunity reads,
  exact resolvers, and reasoned stage transition.
- `backend/internal/sales/proposal_service.go`: exact Proposal/Opportunity
  resolution for safe reads and draft creation.
- `backend/internal/store/psa/sales_repository.go` and
  `backend/internal/store/psa/proposal_repository.go`: bounded normalized SQL
  resolution.
- `backend/internal/aiassist/rtitools/operational_sales_read_tools.go`: closed
  Opportunity and Proposal reads.
- `backend/internal/aiassist/rtitools/operational_sales_write_tools.go`: closed
  Opportunity transition/activity and draft Proposal creation.
- `backend/internal/knowledge/service.go`: reasoned internal publication.
- `backend/internal/aiassist/rtitools/knowledge_publish_tool.go`: closed
  publication proposal.
- `backend/internal/aiassist/workspace_planner.go`: natural-language plans that
  extract supplied values only.
- `backend/cmd/rarity-api/main.go`: tool composition from shared services.
- `frontend/src/features/ai/*`: typed catalog, serializer, form, and readable
  preview behavior.
- `backend/internal/httpapi/*` and ordinary frontend feature files: reason
  evidence for existing assignment, Opportunity transition, and Knowledge
  publication callers.

No schema migration is planned. Audit reasons use the existing nullable
`audit_ledger.reason` column.

---

### Task 1: Shared Ticket creation preflight and selection fence

**Files:**
- Modify: `backend/internal/workrecords/service.go`
- Modify: `backend/internal/workrecords/service_test.go`

**Interfaces:**
- Produces:

```go
type CreateSelectionFence struct {
    RuleSetID, QueueID, WorkflowID, SLAPolicyID, CalendarID string
    RuleSetVersion, WorkflowVersion, SLAPolicyVersion, CalendarVersion int64
}

type CreatePreflight struct {
    Target scope.Target
    Routing RoutingSelection
    Workflow workflow.Selection
    SLA AppliedSLA
    ServiceID, ContractID string
    Fence CreateSelectionFence
}

func (s *Service) PreflightCreate(
    context.Context, CreateCommand,
) (CreatePreflight, error)
```

- Extends `CreateCommand` with
  `ExpectedSelection *CreateSelectionFence`.
- Keeps `Create(context.Context, CreateCommand) (Record, error)` as the only
  Ticket creation writer.

- [ ] **Step 1: Write failing shared-selection tests**

Add table tests proving `PreflightCreate` and `Create` select the same Queue,
Workflow, initial state, SLA policy/calendar, Service, and Contract. Include
no-write repository assertions:

```go
preflight, err := service.PreflightCreate(ctx, command)
if err != nil {
    t.Fatal(err)
}
if repository.createCalls != 0 {
    t.Fatal("preflight wrote a work record")
}
command.ExpectedSelection = &preflight.Fence
record, err := service.Create(ctx, command)
if err != nil || record.QueueID != preflight.Routing.Decision.QueueID {
    t.Fatalf("create=%+v preflight=%+v err=%v", record, preflight, err)
}
```

- [ ] **Step 2: Run the focused test and verify failure**

Run:

```bash
go test ./backend/internal/workrecords -run 'TestCreatePreflight|TestCreateSelectionFence' -count=1
```

Expected: compile failure because the preflight types and method do not exist.

- [ ] **Step 3: Extract one pure selection path**

Move reference validation plus routing, workflow, and SLA selection into:

```go
func (s *Service) prepareCreate(
    ctx context.Context, command CreateCommand, evaluatedAt time.Time,
) (CreatePreflight, error)
```

`PreflightCreate` authorizes and calls `prepareCreate` without writing.
`Create` calls the same function, compares `ExpectedSelection` when supplied,
then constructs the record and atomic mutation.

- [ ] **Step 4: Add fence-drift and setup-failure tests**

Cover changed rule-set version, Queue, Workflow version, initial state, SLA
policy/calendar, Service/Contract validity, inactive Client, and missing setup.
Every drift case must return `object.ErrVersionConflict` or the existing typed
setup error before `CreateAtomic`.

- [ ] **Step 5: Run the Work Record package**

```bash
go test ./backend/internal/workrecords -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/workrecords/service.go backend/internal/workrecords/service_test.go
git commit -m "feat: add ticket creation preflight"
```

---

### Task 2: Closed `ticket.create` AI tool

**Files:**
- Create: `backend/internal/aiassist/rtitools/ticket_create_tool.go`
- Create: `backend/internal/aiassist/rtitools/ticket_create_tool_test.go`
- Modify: `backend/internal/aiassist/tools_test.go`

**Interfaces:**
- Consumes `WorkRecordCreateActions`:

```go
type WorkRecordCreateActions interface {
    PreflightCreate(context.Context, workrecords.CreateCommand) (
        workrecords.CreatePreflight, error,
    )
    Create(context.Context, workrecords.CreateCommand) (
        workrecords.Record, error,
    )
}
```

- Produces `NewTicketCreateTool(ActiveClientResolver,
  ClientResourceCatalog, WorkRecordCreateActions, func() string) aiassist.Tool`.

- [ ] **Step 1: Write failing strict-input tests**

Use this public request and reject missing/unknown fields:

```json
{
  "client": "Northwind Legal",
  "display_id": "INC-2042",
  "type": "incident",
  "title": "VPN unavailable",
  "description": "Remote staff cannot connect",
  "status": "new",
  "priority": "normal",
  "service": "Managed Network",
  "contract": "Support Agreement"
}
```

Assert that `id`, `client_id`, Queue, Workflow, SLA, actor, correlation, and
causation are rejected as user input.

- [ ] **Step 2: Run the focused test and verify failure**

```bash
go test ./backend/internal/aiassist/rtitools -run TestTicketCreateTool -count=1
```

Expected: compile failure because `NewTicketCreateTool` does not exist.

- [ ] **Step 3: Implement preparation and canonical input**

Resolve the active Client and optional active Service/Contract by exact
reference, generate the Ticket ID, call `PreflightCreate`, and persist a
prepared payload containing canonical labels and `CreateSelectionFence`.

- [ ] **Step 4: Implement readable preview and fenced execution**

The preview changes must include display ID, type, title, status, priority,
Client, Queue, Workflow, SLA policy/calendar, and supplied optional references.
On confirmation, rerun preflight, compare canonical identities/fence, and pass
the stored fence to ordinary `Create`.

- [ ] **Step 5: Add drift, rejection, expiry, and result tests**

Prove changed configuration, inactive Client/reference, duplicate display ID,
and mismatched preflight create no row. Prove success returns version 1 and
the ordinary service receives source `ai_workspace`.

- [ ] **Step 6: Run tool and registry tests**

```bash
go test ./backend/internal/aiassist/rtitools ./backend/internal/aiassist -run 'TicketCreate|Registry' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/aiassist/rtitools/ticket_create_tool.go \
  backend/internal/aiassist/rtitools/ticket_create_tool_test.go \
  backend/internal/aiassist/tools_test.go
git commit -m "feat: add AI ticket creation tool"
```

---

### Task 3: Technician resolver and reasoned ordinary assignment

**Files:**
- Create: `backend/internal/workrecords/assignment_directory.go`
- Create: `backend/internal/workrecords/assignment_directory_test.go`
- Create: `backend/internal/store/psa/technician_directory_repository.go`
- Create: `backend/internal/store/psa/technician_directory_repository_test.go`
- Modify: `backend/internal/workrecords/assignment.go`
- Modify: `backend/internal/workrecords/assignment_test.go`
- Modify: `backend/internal/store/psa/work_record_repository.go`
- Modify: `backend/internal/store/psa/work_record_repository_test.go`
- Modify: `backend/internal/httpapi/psa_dto.go`
- Modify: `backend/internal/httpapi/work_record_routes.go`
- Modify: `backend/internal/httpapi/psa_routes_test.go`
- Modify: `frontend/src/features/work/api.ts`
- Modify: `frontend/src/features/work/TechnicianWorklist.test.tsx`

**Interfaces:**
- Produces:

```go
type AssignmentCandidate struct {
    ID, MSPID, DisplayName, Email string
    Version int64
}

type AssignmentDirectory interface {
    ResolveAssignmentCandidates(
        context.Context, scope.Target, string, int,
    ) ([]AssignmentCandidate, error)
}
```

- Extends `AssignCommand` with `Reason string`.

- [ ] **Step 1: Write failing resolver SQL tests**

Require normalized exact display-name or email matching, `LIMIT 2`, active
technician lifecycle, and one unexpired global or matching-Client role
assignment:

```sql
AND t.lifecycle_state = 'active'
AND (
  lower(regexp_replace(btrim(t.display_name), '\s+', ' ', 'g')) = $3
  OR lower(regexp_replace(btrim(t.email), '\s+', ' ', 'g')) = $3
)
AND EXISTS (
  SELECT 1 FROM role_assignments ra
  WHERE ra.technician_id = t.id AND ra.msp_id = t.msp_id
    AND (ra.client_id IS NULL OR ra.client_id = $2)
    AND (ra.expires_at IS NULL OR ra.expires_at > $4)
)
LIMIT $5
```

- [ ] **Step 2: Write failing assignment-reason tests**

Assert blank reason and same-owner assignment fail before persistence, and the
accepted audit fact stores the trimmed reason.

- [ ] **Step 3: Run the focused tests and verify failure**

```bash
go test ./backend/internal/workrecords ./backend/internal/store/psa \
  -run 'AssignmentCandidate|Assign.*Reason|Assign.*SameOwner' -count=1
```

- [ ] **Step 4: Implement the resolver and ordinary contract**

Use shared normalization semantics, enumeration-safe zero results, deterministic
ordering by technician ID, and `scope.ErrNotFound` for an invalid candidate at
commit. Add `Reason` to `AssignWorkRecordRequest` and propagate it into
`AssignCommand` and `Audit.Reason`.

- [ ] **Step 5: Keep ordinary Claim usable**

Update the Worklist API to send the explicit UI reason:

```ts
body: JSON.stringify({
  expected_version: record.version,
  owner_id: technicianID,
  reason: "Claimed from technician worklist",
})
```

- [ ] **Step 6: Run assignment, HTTP, and Worklist tests**

```bash
go test ./backend/internal/workrecords ./backend/internal/store/psa \
  ./backend/internal/httpapi -run 'Assign|AssignmentCandidate' -count=1
npm --prefix frontend test -- --run frontend/src/features/work/TechnicianWorklist.test.tsx
```

- [ ] **Step 7: Commit**

Stage only the files listed in this task and commit:

```bash
git commit -m "feat: resolve and audit ticket assignees"
```

---

### Task 4: Closed `ticket.assign` AI tool

**Files:**
- Create: `backend/internal/aiassist/rtitools/ticket_assignment_tool.go`
- Create: `backend/internal/aiassist/rtitools/ticket_assignment_tool_test.go`
- Modify: `backend/internal/store/psa/work_record_repository.go`
- Modify: `backend/internal/store/psa/work_record_repository_test.go`

**Interfaces:**
- Consumes `AssignmentDirectory` and:

```go
type WorkRecordAssignmentResolver interface {
    ResolveWorkRecordForAssignment(
        context.Context, scope.Target, string, int,
    ) ([]workrecords.Record, error)
}
type WorkRecordAssigner interface {
    Assign(context.Context, workrecords.AssignCommand) (
        workrecords.Record, error,
    )
}
```

- Produces `NewTicketAssignTool(ActiveClientResolver,
  WorkRecordAssignmentResolver, AssignmentDirectory,
  WorkRecordAssigner) aiassist.Tool`.

- [ ] **Step 1: Write failing preparation tests**

Use:

```json
{
  "client": "Northwind Legal",
  "ticket": "INC-2042",
  "technician": "alex@example.test",
  "expected_version": 4,
  "reason": "Assign to network escalation owner"
}
```

Cover exact display ID/name Ticket resolution, exact technician email/name,
zero matches, two matches, inactive/expired scope, same owner, and stale Ticket.

- [ ] **Step 2: Run the focused test and verify failure**

```bash
go test ./backend/internal/aiassist/rtitools -run TestTicketAssignTool -count=1
```

- [ ] **Step 3: Implement canonical preparation and preview**

Add the bounded display-ID-first Work Record resolver to the ordinary
repository/service boundary, then store Client/Ticket/technician canonical
identities and versions. Preview current owner → proposed owner, Ticket
version 4 → 5, and supplied reason.

- [ ] **Step 4: Implement confirmation rechecks and execution**

Reload active Client, Ticket, and technician candidate; require exact identity,
version, and capability; call ordinary assignment with proposal correlation as
causation and source `ai_workspace`.

- [ ] **Step 5: Run focused and registry tests**

```bash
go test ./backend/internal/aiassist/rtitools ./backend/internal/aiassist \
  -run 'TicketAssign|ProposalStale|Registry' -count=1
```

- [ ] **Step 6: Commit**

```bash
git add backend/internal/aiassist/rtitools/ticket_assignment_tool.go \
  backend/internal/aiassist/rtitools/ticket_assignment_tool_test.go \
  backend/internal/store/psa/work_record_repository.go \
  backend/internal/store/psa/work_record_repository_test.go
git commit -m "feat: add AI ticket assignment tool"
```

---

### Task 5: Explicit-Client Sales query and resolver foundation

**Files:**
- Modify: `backend/internal/sales/repository.go`
- Modify: `backend/internal/sales/service.go`
- Modify: `backend/internal/sales/service_test.go`
- Modify: `backend/internal/sales/proposal_service.go`
- Modify: `backend/internal/sales/proposal_service_test.go`
- Modify: `backend/internal/store/psa/sales_repository.go`
- Modify: `backend/internal/store/psa/sales_repository_test.go`
- Modify: `backend/internal/store/psa/proposal_repository.go`
- Modify: `backend/internal/store/psa/proposal_repository_test.go`
- Create: `backend/internal/aiassist/rtitools/operational_sales_read_tools.go`
- Create: `backend/internal/aiassist/rtitools/operational_sales_read_tools_test.go`

**Interfaces:**
- Produces explicit-target methods while retaining HTTP wrappers:

```go
func (s *Service) GetOpportunityInTarget(
    context.Context, authorization.Principal, scope.Target, OpportunityID,
) (Opportunity, error)
func (s *Service) ListOpportunitiesInTarget(
    context.Context, authorization.Principal, scope.Target,
    OpportunityListFilter,
) ([]Opportunity, error)
func (s *Service) ResolveOpportunityReference(
    context.Context, authorization.Principal, scope.Target, string, int,
) ([]Opportunity, error)
func (s *Service) ResolveStageReference(
    context.Context, authorization.Principal, scope.Target,
    OpportunityID, string, int,
) ([]PipelineStage, error)
func (s *ProposalService) ResolveProposalReference(
    context.Context, authorization.Principal, scope.Target, string, int,
) ([]Proposal, error)
```

- Produces tools `opportunity.list`, `opportunity.get`, `proposal.list`, and
  `proposal.get`.

- [ ] **Step 1: Write failing service-target tests**

Prove an MSP-global principal can query an explicit authorized Client without
mutating `principal.Scope.ClientID`, while a Client-scoped principal cannot
escape its scope.

- [ ] **Step 2: Write failing bounded SQL resolver tests**

Require display-ID-first, normalized exact name matching for Opportunities,
exact display ID for Proposals, composite MSP/Client predicates, deterministic
ordering, and `LIMIT 2`.

- [ ] **Step 3: Run focused tests and verify failure**

```bash
go test ./backend/internal/sales ./backend/internal/store/psa \
  -run 'InTarget|ResolveOpportunity|ResolveProposal|ResolveStage' -count=1
```

- [ ] **Step 4: Implement target-aware services and repository queries**

Keep existing `GetOpportunity`/`ListOpportunities` as wrappers that construct
the current principal target. Do not change ordinary HTTP behavior.

- [ ] **Step 5: Write and implement read-tool tests**

Assert minimized data:

```go
map[string]any{
    "display_id": opportunity.DisplayID,
    "name": opportunity.Name,
    "stage": canonicalStageName,
    "version": opportunity.Version,
}
```

Exclude amount, custom fields, contacts, acceptance evidence, attachments, and
Proposal version financials from AI read results.

- [ ] **Step 6: Run Sales and read-tool packages**

```bash
go test ./backend/internal/sales ./backend/internal/store/psa \
  ./backend/internal/aiassist/rtitools -run 'Opportunity|Proposal' -count=1
```

- [ ] **Step 7: Commit**

Stage only Task 5 files and commit:

```bash
git commit -m "feat: add scoped AI sales queries"
```

---

### Task 6: Opportunity transition and activity tools

**Files:**
- Modify: `backend/internal/sales/service.go`
- Modify: `backend/internal/sales/service_test.go`
- Modify: `backend/internal/httpapi/psa_dto.go`
- Modify: `backend/internal/httpapi/sales_routes.go`
- Modify: `backend/internal/httpapi/psa_routes_test.go`
- Modify: `frontend/src/features/sales/api.ts`
- Modify: `frontend/src/features/sales/SalesWorklists.tsx`
- Modify: `frontend/src/features/sales/SalesWorklists.test.tsx`
- Create: `backend/internal/aiassist/rtitools/operational_sales_write_tools.go`
- Create: `backend/internal/aiassist/rtitools/operational_sales_write_tools_test.go`

**Interfaces:**
- Extends `sales.TransitionCommand` and `TransitionOpportunityRequest` with
  `Reason string`.
- Extends `sales.CreateOpportunityActivityCommand` with
  `Target scope.Target` so the MSP-wide drawer never depends on page scope.
- Produces `NewOpportunityTransitionTool(...)` and
  `NewOpportunityActivityCreateTool(...)`.

- [ ] **Step 1: Write failing ordinary transition-reason tests**

Assert blank reason fails, trimmed reason reaches `Audit.Reason`, and the
ordinary Sales UI submits a required reason.

- [ ] **Step 2: Write failing AI tool tests**

Transition input:

```json
{
  "client": "Northwind Legal",
  "opportunity": "OPP-2042",
  "stage": "Qualified",
  "expected_version": 3,
  "reason": "Discovery completed"
}
```

Activity input:

```json
{
  "client": "Northwind Legal",
  "opportunity": "OPP-2042",
  "kind": "call",
  "summary": "Reviewed onboarding scope",
  "details": "Customer confirmed the supplied milestones"
}
```

- [ ] **Step 3: Run focused tests and verify failure**

```bash
go test ./backend/internal/sales ./backend/internal/httpapi \
  ./backend/internal/aiassist/rtitools -run 'Opportunity.*Reason|OpportunityTransitionTool|OpportunityActivity' -count=1
```

- [ ] **Step 4: Implement the ordinary reason contract**

Persist `Reason` on `opportunity.stage.changed` audit facts. Add a compact
reason field to the existing Sales transition surface without changing stage
authorization or requirements.

- [ ] **Step 5: Implement transition and activity tools**

Preparation resolves canonical Client, Opportunity, and destination stage.
Preview shows stage before/after and exact activity contents. Confirmation
rechecks pipeline/stage eligibility, versions, and capability before ordinary
service execution.

- [ ] **Step 6: Add no-invention and prohibited-action tests**

Reject omitted kind/summary/stage/reason, arbitrary stage IDs supplied as
trusted identity, amount/probability/custom-field inputs, and any conversion
language.

- [ ] **Step 7: Run affected tests**

```bash
go test ./backend/internal/sales ./backend/internal/httpapi \
  ./backend/internal/aiassist/rtitools -count=1
npm --prefix frontend test -- --run frontend/src/features/sales/SalesWorklists.test.tsx
```

- [ ] **Step 8: Commit**

Stage only Task 6 files and commit:

```bash
git commit -m "feat: add AI opportunity actions"
```

---

### Task 7: Draft Proposal creation tool

**Files:**
- Create: `backend/internal/aiassist/rtitools/proposal_write_tool.go`
- Create: `backend/internal/aiassist/rtitools/proposal_write_tool_test.go`
- Modify: `backend/internal/sales/proposal_service.go`
- Modify: `backend/internal/sales/proposal_service_test.go`
- Modify: `backend/internal/store/psa/proposal_repository.go`
- Modify: `backend/internal/store/psa/proposal_repository_test.go`

**Interfaces:**
- Produces a read-only identity preflight:

```go
type ProposalDraftPreflight struct {
    Opportunity Opportunity
    DisplayID string
}
func (s *ProposalService) PreflightCreateProposal(
    context.Context, CreateProposalCommand,
) (ProposalDraftPreflight, error)
```

- Produces `NewProposalCreateTool(ActiveClientResolver,
  ProposalOperationalActions) aiassist.Tool`.

- [ ] **Step 1: Write failing Proposal preflight tests**

Prove preflight checks Client, source Opportunity, and display-ID availability
without inserting Proposal, audit, or outbox rows.

- [ ] **Step 2: Write failing closed-tool tests**

Input is exactly:

```json
{
  "client": "Northwind Legal",
  "opportunity": "OPP-2042",
  "display_id": "PROP-2042"
}
```

Reject lines, prices, currency, approval, issue, signer, acceptance,
conversion, and user-supplied internal IDs.

- [ ] **Step 3: Run focused tests and verify failure**

```bash
go test ./backend/internal/sales ./backend/internal/store/psa \
  ./backend/internal/aiassist/rtitools -run 'Proposal.*Preflight|ProposalCreateTool' -count=1
```

- [ ] **Step 4: Implement preflight and tool**

Prepare canonical Client and Opportunity identity, preview a version-1 draft
linkage keyed by the supplied display ID, recheck preflight on confirmation,
and call ordinary `CreateProposal`. The ordinary service generates the trusted
Proposal ID with the same ID source used by non-AI callers.

- [ ] **Step 5: Add drift and atomicity tests**

Cover Opportunity change/ineligibility, Client deactivation, duplicate display
ID, authorization loss, and repository failure. No case may leave a partial
Proposal/audit/outbox set.

- [ ] **Step 6: Run affected packages**

```bash
go test ./backend/internal/sales ./backend/internal/store/psa \
  ./backend/internal/aiassist/rtitools -run Proposal -count=1
```

- [ ] **Step 7: Commit**

```bash
git add backend/internal/aiassist/rtitools/proposal_write_tool.go \
  backend/internal/aiassist/rtitools/proposal_write_tool_test.go \
  backend/internal/sales/proposal_service.go \
  backend/internal/sales/proposal_service_test.go \
  backend/internal/store/psa/proposal_repository.go \
  backend/internal/store/psa/proposal_repository_test.go
git commit -m "feat: add AI draft proposal creation"
```

---

### Task 8: Internal Knowledge publication tool and reason contract

**Files:**
- Modify: `backend/internal/knowledge/service.go`
- Modify: `backend/internal/knowledge/service_test.go`
- Modify: `backend/internal/store/psa/knowledge_repository_test.go`
- Modify: `backend/internal/httpapi/knowledge_routes.go`
- Modify: `backend/internal/httpapi/psa_routes_test.go`
- Modify: `frontend/src/features/knowledge/KnowledgePage.tsx`
- Modify: `frontend/src/features/knowledge/KnowledgePage.test.tsx`
- Create: `backend/internal/aiassist/rtitools/knowledge_publish_tool.go`
- Create: `backend/internal/aiassist/rtitools/knowledge_publish_tool_test.go`

**Interfaces:**
- Extends `knowledge.PublishCommand` and `publishKnowledgeRequest` with
  `Reason string`.
- Produces `NewKnowledgePublishTool(ActiveClientResolver,
  KnowledgePublishActions) aiassist.Tool`.

- [ ] **Step 1: Write failing ordinary publication-reason tests**

Assert blank reason fails before repository access and the trimmed reason is
stored on `knowledge.published`.

- [ ] **Step 2: Write failing tool tests**

Use:

```json
{
  "client": "Northwind Legal",
  "article": "KB-2042",
  "expected_version": 2,
  "reason": "Reviewed and approved for internal use"
}
```

Reject client-visible flags, recipient/delivery fields, empty drafts, already
published Articles, ambiguity, and stale versions.

- [ ] **Step 3: Run focused tests and verify failure**

```bash
go test ./backend/internal/knowledge ./backend/internal/httpapi \
  ./backend/internal/aiassist/rtitools -run 'Knowledge.*Publish|Publication.*Reason' -count=1
```

- [ ] **Step 4: Implement ordinary UI reason capture**

Replace the one-click publish button with an inline required reason input and
submit `{expected_version, reason}`. Preserve the existing capability and
draft-state gates.

- [ ] **Step 5: Implement the AI publication tool**

Resolve exact internal draft, preview `draft` → `published` with version and
the explicit “internal only; no external delivery” impact, recheck on
confirmation, and call ordinary `Publish`.

- [ ] **Step 6: Run Knowledge, HTTP, and frontend tests**

```bash
go test ./backend/internal/knowledge ./backend/internal/store/psa \
  ./backend/internal/httpapi ./backend/internal/aiassist/rtitools \
  -run Knowledge -count=1
npm --prefix frontend test -- --run frontend/src/features/knowledge/KnowledgePage.test.tsx
```

- [ ] **Step 7: Commit**

Stage only Task 8 files and commit:

```bash
git commit -m "feat: add reasoned AI knowledge publication"
```

---

### Task 9: Compose the complete backend catalog and planner

**Files:**
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/cmd/rarity-api/main_test.go`
- Modify: `backend/internal/aiassist/workspace_planner.go`
- Modify: `backend/internal/aiassist/workspace_planner_test.go`
- Modify: `backend/internal/httpapi/ai_workspace_routes.go`
- Modify: `backend/internal/httpapi/ai_workspace_routes_test.go`

**Interfaces:**
- Registers the ten approved tools and no high-impact tools.
- Extends `WorkspaceMessagePlan` only with fields required by the approved
  request schemas.

- [ ] **Step 1: Update the failing catalog contract**

Replace the `ticket.create must remain deferred` assertion and require:

```go
"ticket.create":               "work_record.create",
"ticket.assign":               "work_record.assign",
"opportunity.list":            "opportunity.read",
"opportunity.get":             "opportunity.read",
"opportunity.transition":      "opportunity.transition",
"opportunity.activity.create": "opportunity.activity.create",
"proposal.list":               "proposal.read",
"proposal.get":                "proposal.read",
"proposal.create":             "proposal.create",
"knowledge.publish":           "knowledge.publish",
```

Assert issue/approve/accept/convert/financial/configuration/integration tool
names remain absent.

- [ ] **Step 2: Run the composition test and verify failure**

```bash
go test ./backend/cmd/rarity-api -run TestBuildAIWorkspaceTools -count=1
```

- [ ] **Step 3: Compose shared services once**

Instantiate one Work Record service, Assignment service, technician directory,
Sales service, Proposal service, and Knowledge service, then pass those same
instances to HTTP dependencies and AI tools. Do not create divergent business
logic.

- [ ] **Step 4: Add planner tests before planner code**

Cover one complete natural-language request and one clarification per tool.
For example:

```go
plan := PlanWorkspaceMessage(
    "Assign ticket INC-2042 for Northwind Legal to alex@example.test " +
        "at version 4 because network escalation owns it",
)
if plan.ToolName != "ticket.assign" ||
    plan.ExpectedVersion != 4 ||
    plan.Reason != "network escalation owns it" {
    t.Fatalf("plan=%+v", plan)
}
```

Also prove omitted values are clarified and high-impact language returns help,
not an action proposal.

- [ ] **Step 5: Implement narrow planner branches**

Add specific parsers for the approved sentence shapes. Do not add a generic
verb/object executor or infer omitted values.

- [ ] **Step 6: Preserve typed HTTP errors**

Map Sales/Proposal ambiguity, unavailable technician, setup missing, and
selection drift to the existing safe `ambiguous_reference`,
`not_found`/`inactive_target`, `configuration_required`, and
`version_conflict` families without leaking internal IDs.

- [ ] **Step 7: Run backend catalog, planner, and HTTP tests**

```bash
go test ./backend/cmd/rarity-api ./backend/internal/aiassist \
  ./backend/internal/httpapi -run 'AIWorkspace|WorkspacePlan|BuildAIWorkspaceTools' -count=1
```

- [ ] **Step 8: Commit**

Stage only Task 9 files and commit:

```bash
git commit -m "feat: compose AI second-wave catalog"
```

---

### Task 10: Structured workspace forms and readable previews

**Files:**
- Modify: `frontend/src/features/ai/types.ts`
- Modify: `frontend/src/features/ai/workspaceCatalog.ts`
- Modify: `frontend/src/features/ai/workspaceCatalog.test.ts`
- Modify: `frontend/src/features/ai/structuredAction.ts`
- Modify: `frontend/src/features/ai/structuredAction.test.ts`
- Modify: `frontend/src/features/ai/useStructuredActionController.ts`
- Modify: `frontend/src/features/ai/StructuredActionForm.tsx`
- Modify: `frontend/src/features/ai/AIWorkspace.test.tsx`
- Modify: `frontend/src/features/ai/AIProposal.tsx`

**Interfaces:**
- Adds typed read tools for Opportunity/Proposal list/get.
- Adds typed write tools for Ticket create/assign, Opportunity
  transition/activity, Proposal create, and Knowledge publish.

- [ ] **Step 1: Write failing catalog and serializer tests**

Require capability filtering, explicit Client selection, exact serialized field
names, and absence of high-impact actions. Example:

```ts
expect(
  serializeStructuredAction("proposal_create", values),
).toEqual({
  mode: "write",
  tool: "proposal.create",
  input: {
    client: "Northwind Legal",
    opportunity: "OPP-2042",
    display_id: "PROP-2042",
  },
});
```

- [ ] **Step 2: Run focused tests and verify failure**

```bash
npm --prefix frontend test -- --run \
  frontend/src/features/ai/workspaceCatalog.test.ts \
  frontend/src/features/ai/structuredAction.test.ts \
  frontend/src/features/ai/AIWorkspace.test.tsx
```

- [ ] **Step 3: Extend types, catalog, and serializer**

Use distinct `WorkspaceAction` values and only show actions supported by the
principal capability set. Every Client-bound serializer sends the canonical
selected Client display ID/name expected by the preparation contract.

- [ ] **Step 4: Add action-specific form regions**

Reuse compact grids but provide explicit labels for Ticket type/status/priority,
technician, stage, activity kind/summary/details/time, Proposal display ID, and
publication reason. Do not use one generic “New value” field for these actions.

- [ ] **Step 5: Verify plain-English previews**

Ensure nested routing/workflow/SLA, Opportunity stage, owner, Proposal linkage,
and publication impact use definition-list rendering and wrap safely. No raw
JSON object string may appear.

- [ ] **Step 6: Add stale-request and accessibility tests**

Cover action/client switching during reads/proposals, required labels,
keyboard submission/cancel, disabled unavailable actions, 620px responsive
stacking, and 1440px full-width layout.

- [ ] **Step 7: Run frontend tests and build**

```bash
npm --prefix frontend test
npm --prefix frontend run build
```

Expected: PASS; record the existing chunk-size advisory separately if it
remains non-fatal.

- [ ] **Step 8: Commit**

Stage only Task 10 files and commit:

```bash
git commit -m "feat: expose AI second-wave workspace actions"
```

---

### Task 11: PostgreSQL concurrency and atomicity proof

**Files:**
- Create: `backend/internal/store/psa/ai_second_wave_postgres_integration_test.go`
- Modify when required by failing proof:
  `backend/internal/store/psa/work_record_repository.go`
- Modify when required by failing proof:
  `backend/internal/store/psa/sales_repository.go`
- Modify when required by failing proof:
  `backend/internal/store/psa/proposal_repository.go`
- Modify when required by failing proof:
  `backend/internal/store/psa/knowledge_repository.go`
- Create: `.superpowers/sdd/2026-08-06-ai-second-operational-wave/task-11-postgres-report.md`

**Interfaces:**
- Uses only `TEST_DATABASE_URL` pointing to an isolated scratch database.

- [ ] **Step 1: Write environment-gated race tests**

Implement deterministic barriers proving:

- Ticket create versus Client/Service/Contract deactivation;
- Ticket create versus routing/workflow/SLA version replacement;
- Ticket assignment versus same-version assignment and technician
  deactivation/scope expiry;
- Opportunity transition/activity versus Opportunity version or Client change;
- Proposal create versus Opportunity/Client change;
- Knowledge publish versus revision and Client deactivation; and
- rollback of each domain row with audit/outbox failure.

- [ ] **Step 2: Run without `TEST_DATABASE_URL`**

```bash
go test ./backend/internal/store/psa -run TestAISecondWavePostgres -count=1
```

Expected: explicit skip naming `TEST_DATABASE_URL`; never fall back to the live
demo database.

- [ ] **Step 3: Run against an isolated PostgreSQL database**

Create a scratch database with a unique `rti_ai_second_wave_YYYYMMDD` name,
export its URL only for the test process, apply migrations, and run:

```bash
go test ./backend/internal/store/psa -run TestAISecondWavePostgres -count=1 -v
```

- [ ] **Step 4: Fix only proven transaction gaps**

Use deterministic row-lock order: active Client, target, dependent references,
then configuration/version rows. Preserve one domain mutation + audit + outbox
transaction.

- [ ] **Step 5: Re-run, inspect residue, and remove the scratch database**

Require all tests PASS, zero named fixture residue, and verified scratch
database absence after cleanup. Do not alter RTI/Hank/RTM live volumes.

- [ ] **Step 6: Record exact evidence and commit**

Document database name, migration range, commands, outcomes, cleanup, and
environment boundaries without credentials:

```bash
git add backend/internal/store/psa/ai_second_wave_postgres_integration_test.go \
  backend/internal/store/psa/work_record_repository.go \
  backend/internal/store/psa/sales_repository.go \
  backend/internal/store/psa/proposal_repository.go \
  backend/internal/store/psa/knowledge_repository.go \
  .superpowers/sdd/2026-08-06-ai-second-operational-wave/task-11-postgres-report.md
git commit -m "test: prove AI second-wave concurrency"
```

---

### Task 12: Whole-branch verification and review

**Files:**
- Modify: `docs/04-api/integration-automation-ai-contracts.md`
- Modify: `docs/06-development/integration-automation-ai-acceptance.md`
- Modify: `docs/09-roadmap/implementation-plan.md`
- Modify: `docs-site/index.html`
- Modify: `docs-site/app.js` only if a new evidence document requires manifest
  registration.
- Create:
  `.superpowers/sdd/2026-08-06-ai-second-operational-wave/task-12-review-report.md`

- [ ] **Step 1: Update contracts and pending/complete language**

Document exact tool names, capabilities, supplied-value rule, preflight/fence,
reason evidence, exclusions, and PostgreSQL proof. Do not claim publication or
live acceptance yet.

- [ ] **Step 2: Run portable backend gates**

```bash
gofmt -l backend
go test ./...
go vet ./...
go build ./...
```

Expected: `gofmt -l backend` prints no changed Go file.

- [ ] **Step 3: Run portable frontend and documentation gates**

```bash
npm --prefix frontend test
npm --prefix frontend run build
node scripts/validate-markdown-links.mjs
node scripts/validate-docs.mjs
git diff --check
```

- [ ] **Step 4: Review the exact whole-branch diff**

Review from the design commit through current HEAD for authorization,
enumeration, preflight/execute drift, transaction ordering, audit/outbox,
no-invention, high-impact catalog leakage, and frontend stale-state defects.
Resolve every Critical or Important finding and rerun affected gates.

- [ ] **Step 5: Record review evidence and commit**

```bash
git add docs/04-api/integration-automation-ai-contracts.md \
  docs/06-development/integration-automation-ai-acceptance.md \
  docs/09-roadmap/implementation-plan.md \
  docs-site/index.html docs-site/app.js \
  .superpowers/sdd/2026-08-06-ai-second-operational-wave/task-12-review-report.md
git commit -m "docs: prepare AI second-wave acceptance"
```

---

### Task 13: Publish, deploy, and complete live acceptance

**Files:**
- Create:
  `.superpowers/sdd/2026-08-06-ai-second-operational-wave/task-13-live-acceptance-report.md`
- Modify: `docs/06-development/integration-automation-ai-acceptance.md`
- Modify: `docs/09-roadmap/implementation-plan.md`
- Modify: `docs-site/index.html`
- Update: active Hank RTI Kanban card work log.

- [ ] **Step 1: Push `main` and verify parity**

Require local HEAD, `origin/main`, and remote `refs/heads/main` to match
exactly.

- [ ] **Step 2: Deploy the exact pushed revision**

Use the `rti-demo-server` runbook and scoped RTI Compose project. Verify the
remote checkout, migration version, health, readiness, `/v1/system/build`,
fresh frontend assets, zero API/web restarts, clean startup logs, and unchanged
Hank/RTM container counts.

- [ ] **Step 3: Run 1440×900 authenticated acceptance**

Use full-page—not narrow—browser presentation:

1. prepare/reject a Ticket create and inspect routing/workflow/SLA effects;
2. confirm a disposable Ticket create and exact technician assignment;
3. prove stale assignment plus ambiguous/inactive technician failure;
4. list/get an Opportunity and prepare/reject transition/activity actions;
5. list/get Proposals and prepare/reject a draft Proposal;
6. prepare/reject Knowledge publication and publish only a disposable internal
   draft when safe;
7. verify versions plus matching audit/outbox correlation and no external or
   client-visible effect; and
8. verify an empty browser console.

Record the one-Client/global-admin environment limitation instead of fabricating
cross-Client or restricted-principal proof.

- [ ] **Step 4: Publish final acceptance evidence**

Update the report, contracts, roadmap, and docs site with exact accepted
journeys and environment boundaries. Validate docs, commit, and push.

- [ ] **Step 5: Align the demo to the final evidence SHA**

Deploy the final documentation commit so local `main`, `origin/main`, remote
main, demo checkout, public readiness/build metadata, and rendered shell show
one exact SHA.

- [ ] **Step 6: Update the RTI Kanban card**

Append tests, review, PostgreSQL proof, exact SHA, demo acceptance, and remaining
high-impact exclusions. Keep the whole-system card active because the separate
high-impact design remains intentionally deferred.

- [ ] **Step 7: Final verification**

Require clean worktree, exact Git parity, migration health, RTI 6/6, Hank 6/6,
RTM 4/4, empty browser console, and removal of exact temporary deployment
bundles.
