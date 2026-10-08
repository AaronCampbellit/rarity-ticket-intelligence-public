# AI Workspace Standalone Project Task Actions Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an authenticated user create one standalone task on an existing
active-Client Project from an explicit AI chat command, using only the Project,
Client, and task title supplied by the user.

**Architecture:** Extend the bounded workspace-message planner with one
`task.create` intent. The HTTP route verifies the supplied Client against the
active Client and resolves the supplied Project name or display ID through the
ordinary Project query service. A new typed write tool loads the Project for
the exact preview and version check, then invokes the existing Task application
service only after explicit confirmation.

**Tech Stack:** Go 1.24, existing `aiassist.Registry`, Projects and Tasks
application services, PostgreSQL-backed repositories, React 19 proposal UI.

## Global Constraints

- Only project-level task creation is included; Phase tasks, subtasks, task
  updates, assignment, estimates, and status changes are excluded.
- The user must supply the task title, active Client name, and Project name or
  display ID.
- Internal Client and Project IDs are resolved server-side inside the
  authenticated active-Client scope.
- Omitted owner, estimate, description, due date, and other business fields
  remain unset or at the ordinary Task service default.
- Every write remains an expiring exact preview requiring explicit
  confirmation and live reauthorization.
- The AI never receives a generic Task mutation route or arbitrary database
  access.

---

### Task 1: Add the standalone Task typed tool

**Files:**
- Create: `backend/internal/aiassist/rtitools/task_tools.go`
- Create: `backend/internal/aiassist/rtitools/task_tools_test.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/cmd/rarity-api/main_test.go`

**Interfaces:**
- Consumes:
  `ProjectWorkspaceGetter.Get(context.Context, authorization.Principal,
  scope.Target, projects.ProjectID) (projects.ProjectWorkspace, error)` and
  `TaskCreator.Create(context.Context, tasks.CreateCommand) (tasks.Task, error)`.
- Produces:
  `NewProjectTaskCreateTool(ProjectWorkspaceGetter, TaskCreator) aiassist.Tool`
  registered as `task.create`.

- [x] **Step 1: Write the failing tool tests**

Add tests proving the preview targets the resolved Project and includes only
the supplied title plus ordinary `open` status; confirmation maps the trusted
principal, Project parent, `ai_workspace` source, and correlation ID into
`tasks.CreateCommand`. Add rejection cases for unknown JSON fields, empty
title, missing Project identity, and a Project name/ID mismatch.

- [x] **Step 2: Run the focused test and verify red**

Run:
`go test ./backend/internal/aiassist/rtitools -run ProjectTaskCreate -count=1`

Expected: build failure because `NewProjectTaskCreateTool` is absent.

- [x] **Step 3: Implement the minimal typed tool**

Decode exactly:

```go
type projectTaskCreateInput struct {
    ClientID   string `json:"client_id"`
    ProjectID  string `json:"project_id"`
    ProjectRef string `json:"project_ref"`
    Title      string `json:"title"`
}
```

Load the scoped Project during preview, require the canonical name or display
ID to match `ProjectRef`, emit the Project version as `TargetVersion`, and call
`tasks.Service.Create` with `ParentProject`, the supplied title, trusted actor,
and AI source during execution.

- [x] **Step 4: Register and verify the closed catalog**

Pass the composed Project query and Task services into
`buildAIWorkspaceTools`, register `task.create`, and update the catalog test to
expect 12 tools and capability `task.create`.

- [x] **Step 5: Run focused tests and verify green**

Run:
`go test ./backend/internal/aiassist/rtitools ./backend/cmd/rarity-api -run 'ProjectTaskCreate|AIComposition' -count=1`

Expected: PASS.

### Task 2: Plan and resolve Task creation from chat

**Files:**
- Modify: `backend/internal/aiassist/workspace_planner.go`
- Modify: `backend/internal/aiassist/workspace_planner_test.go`
- Modify: `backend/internal/httpapi/ai_workspace_routes.go`
- Modify: `backend/internal/httpapi/ai_workspace_routes_test.go`

**Interfaces:**
- Extends `WorkspaceMessagePlan` with `TaskTitle` and `ProjectRef`.
- Recognizes:
  “Add a task titled TITLE to project PROJECT for CLIENT.”
- Returns the existing message response with optional `task.create` proposal.

- [x] **Step 1: Write failing planner tests**

Prove exact extraction from the supported sentence, clarification when title,
Project, or Client is absent, and no regression to `project.create` or
`product.help`.

- [x] **Step 2: Run the planner test and verify red**

Run:
`go test ./backend/internal/aiassist -run PlanWorkspaceMessage -count=1`

Expected: FAIL because task intent fields and parsing are absent.

- [x] **Step 3: Implement the minimal task intent parser**

Recognize only explicit `add/create a task` commands. Split the task title from
`to project` or `in project`, use the final `for` clause as the Client name,
trim punctuation, and return clarification instead of guessing missing data.

- [x] **Step 4: Write failing HTTP route tests**

Prove that an exact active-Client Project name produces `task.create` input
with the server-resolved Project ID; missing Projects and duplicate names
produce clarification and no proposal; a different Client remains refused.

- [x] **Step 5: Run route tests and verify red**

Run:
`go test ./backend/internal/httpapi -run AIWorkspaceMessage -count=1`

Expected: FAIL because task intents are not routed or resolved.

- [x] **Step 6: Implement scoped Project resolution and proposal creation**

Reuse active-Client verification, list at most 100 authorized Projects, match
the normalized supplied Project name or display ID, require one result, and
submit the resolved input to the typed registry with the user message ID.

- [x] **Step 7: Run focused route and planner tests**

Run:
`go test ./backend/internal/aiassist ./backend/internal/httpapi -run 'PlanWorkspaceMessage|AIWorkspaceMessage' -count=1`

Expected: PASS.

### Task 3: Document, verify, publish, and accept

**Files:**
- Modify: `docs/04-api/psa-opportunities-projects.md`
- Modify: `docs/09-roadmap/implementation-plan.md`
- Modify: `docs/06-development/integration-automation-ai-acceptance.md`
- Modify: `docs-site/index.html`
- Modify: `docs-site/app.js`

- [x] **Step 1: Document the Task command and boundaries**

Document the exact supported form, active-Client/Project resolution, omitted
field behavior, confirmation requirement, and explicit exclusion of task
updates until an ordinary application-service contract exists.

- [x] **Step 2: Run full verification**

Run the full Go suite, `go vet ./...`, all frontend tests, frontend production
build, both documentation validators, formatting, and diff checks.

- [x] **Step 3: Publish and deploy**

Commit only this scope, push `main`, deploy the exact revision through the
configured RTI Compose profile, and verify public health, readiness, build
identity, fresh assets, and clean API/web logs.

- [x] **Step 4: Perform full-width live acceptance**

At 1440 by 900 with Northwind Legal active, submit a synthetic Task command
against the existing modernization Project. Verify the exact preview, reject
it, and confirm the Project task list is unchanged.

- [x] **Step 5: Record the outcome**

Update the acceptance evidence and active RTI Kanban work log while leaving the
broad whole-system AI card In progress for the remaining action catalog.
