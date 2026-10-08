# MSP-Wide AI Workspace and Client Actions Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the top-bar AI workspace operate across every Client authorized
to the MSP principal, resolve Client-targeted actions independently of the
page filter, and add explicitly confirmed Client creation using only the name
and display ID supplied by the user.

**Architecture:** New drawer conversations and proposals use the MSP-global
principal. Proposal storage separates the caller's access scope from the
tool's execution target so an MSP-scoped conversation can safely confirm an
action against one Client. A shared exact Client-reference resolver supplies
trusted IDs to the closed typed-tool registry. The new `client.create` tool
uses prepared system identifiers and the ordinary organization service. The
frontend omits the Client header for all workspace requests while retaining the
page-selected Client only as the initial structured-form selection.

**Tech Stack:** Go 1.24, PostgreSQL migrations and repositories, existing
`aiassist.Registry`, organization/project/task application services, React 19,
TypeScript, Vitest, Testing Library, Docker Compose demo deployment.

**Status:** Tasks 1-8 are implemented, reviewed, published, deployed, and
accepted on the constrained demo. Every in-product Client writer, including
Prospect conversion, shares the same atomic identity boundary. The
environment-gated cross-path PostgreSQL regression still requires an isolated
`TEST_DATABASE_URL`; its deterministic transaction coverage and test
composition pass portably. The broader whole-system AI action catalog remains
outside this slice.

## Global Constraints

- The drawer is MSP-wide; `X-Rarity-Client-ID` must be absent from every new
  workspace conversation, message, proposal, confirmation, and rejection.
- The page Client remains a page-data filter and optional form default, never
  an AI authorization boundary.
- Every Client-targeted action must name or select its Client explicitly.
- Server code resolves names/display IDs to internal Client IDs from the
  authorized directory. Model/browser-supplied IDs are never trusted for chat.
- Client creation accepts only a user-supplied name and display ID. Optional
  Client business data remains unset.
- A Client name or display ID that already belongs to any lifecycle state,
  including inactive, archived, and deleted, blocks creation.
- All writes retain exact preview, expiry, live authorization, stale-preview
  detection, explicit confirmation, audit, outbox, and correlation evidence.
- Existing Client-scoped conversations remain isolated and are not migrated.
- No generic SQL, shell, HTTP, arbitrary mutation, or silent confirmation
  surface is added.
- Work proceeds test-first and preserves unrelated working-tree changes.

---

### Task 1: Separate proposal access scope from execution target

**Files:**
- Create: `backend/migrations/000079_ai_workspace_proposal_target_scope.sql`
- Modify: `backend/migrations/ai_contract_test.go`
- Modify: `backend/internal/aiassist/tools.go`
- Modify: `backend/internal/aiassist/tools_test.go`
- Modify: `backend/internal/store/psa/ai_workspace_repository.go`
- Modify: `backend/internal/store/psa/ai_workspace_repository_test.go`

**Contract:**
- `ActionProposal.ClientID` continues to identify the conversation/caller
  access scope and may be empty for an MSP-wide proposal.
- Add `TargetClientID string` for the tool execution target; an empty value
  means an MSP-global action.
- `Propose` stores caller access scope separately from `ResolveScope`.
- `Confirm` retrieves by caller access scope, reloads the same principal,
  resolves the tool target again, compares it with the stored execution
  target, authorizes that target, then rebuilds and executes the preview.
- Reject, expire, complete, and fail continue to address the proposal through
  caller access scope.

- [x] **Step 1: Write failing registry tests**

Add tests in `tools_test.go` proving an MSP-global caller can create, retrieve,
confirm, and reject a proposal whose tool target is one Client. Assert the
stored proposal has empty `ClientID` and the resolved Client in
`TargetClientID`. Add a confirmation failure when the newly resolved target
differs from the stored target, plus a regression test where a Client-scoped
caller has matching access and execution scopes.

- [x] **Step 2: Write failing repository and migration tests**

Extend the proposal repository tests to require round-trip persistence of
`target_client_id` independently of nullable access `client_id`. Extend the
migration contract to require:

```sql
ALTER TABLE ai_action_proposals
  ADD COLUMN target_client_id uuid;

UPDATE ai_action_proposals
SET target_client_id = client_id
WHERE client_id IS NOT NULL;
```

The migration must add the appropriate same-MSP Client foreign-key protection
without changing historical global proposal access.

- [x] **Step 3: Run the focused tests and verify red**

Run:

```sh
go test ./backend/internal/aiassist ./backend/internal/store/psa ./backend/migrations \
  -run 'Proposal|AIContract' -count=1
```

Expected: build/assertion failure because target scope is still conflated.

- [x] **Step 4: Implement the minimal scope split**

Add `TargetClientID` to `ActionProposal`, persistence scans/inserts, and the
migration. In `Registry.Propose`, derive:

```go
accessTarget := scope.Target{
    MSPID: principal.Scope.MSPID,
    ClientID: principal.Scope.ClientID,
}
executionTarget := resolvedTarget
```

Store access fields from `accessTarget` and target fields from
`executionTarget`. In `Confirm`, use `accessTarget` for proposal repository
operations and compare the freshly resolved target to:

```go
scope.Target{
    MSPID: proposal.MSPID,
    ClientID: proposal.TargetClientID,
}
```

Authorize and execute only against that exact target.

- [x] **Step 5: Run focused tests and verify green**

Run the Step 3 command and expect PASS.

### Task 2: Add exact Client-reference resolution

**Files:**
- Create: `backend/internal/organizations/client_reference.go`
- Create: `backend/internal/organizations/client_reference_test.go`

**Contract:**
- Match an authorized directory result by exact normalized `Name` or
  `DisplayID`.
- Normalization trims outer whitespace, collapses internal whitespace, and
  compares case-insensitively.
- Deduplicate matches by internal Client ID.
- Return distinct safe errors for missing and ambiguous references.

- [x] **Step 1: Write failing resolver tests**

Cover exact name, exact display ID, case/whitespace normalization, one Client
matching both fields, no match, and two Clients sharing a normalized name.
Inputs are already authorization-filtered directory rows; the resolver must
not enumerate any wider store.

- [x] **Step 2: Run the focused test and verify red**

Run:

```sh
go test ./backend/internal/organizations -run ClientReference -count=1
```

Expected: build failure because `ResolveClientReference` is absent.

- [x] **Step 3: Implement the pure resolver**

Add `ErrClientReferenceNotFound` and `ErrClientReferenceAmbiguous`. Return the
single canonical `organizations.Client` only when exactly one unique ID
matches.

- [x] **Step 4: Run the focused test and verify green**

Run the Step 2 command and expect PASS.

### Task 3: Support trusted prepared Client identity in the organization service

**Files:**
- Modify: `backend/internal/organizations/service.go`
- Modify: `backend/internal/organizations/service_test.go`
- Modify: `backend/internal/httpapi/organization_routes_test.go`

**Contract:**
- `CreateClientCommand` gains optional trusted `ClientID` and
  `CorrelationID`.
- The service generates either value when omitted.
- A supplied value becomes the canonical Client/audit/event identity.
- The public HTTP request body remains name/display-ID only and cannot set
  trusted identity fields.

- [x] **Step 1: Write failing service tests**

Prove supplied IDs are used consistently for the Client envelope, audit row,
event row, and shared correlation. Prove omitted IDs preserve ordinary
generation and atomic behavior. Add invalid-value coverage if IDs fail the
service's existing identifier rules.

- [x] **Step 2: Add an HTTP regression test**

Submit unknown `client_id` and `correlation_id` JSON fields to the ordinary
Client-create endpoint and assert strict decoding rejects them, while the
existing valid body still generates its own identities.

- [x] **Step 3: Run focused tests and verify red**

Run:

```sh
go test ./backend/internal/organizations ./backend/internal/httpapi \
  -run 'CreateClient|Organization' -count=1
```

Expected: compile/assertion failure because trusted command fields are absent.

- [x] **Step 4: Implement minimal trusted-ID handling**

Trim and select the optional IDs before building the mutation. Generate only
the omitted values. Do not add either field to the route DTO.

- [x] **Step 5: Run focused tests and verify green**

Run the Step 3 command and expect PASS.

### Task 4: Add the closed `client.create` typed tool

**Files:**
- Create: `backend/internal/aiassist/rtitools/client_tools.go`
- Create: `backend/internal/aiassist/rtitools/client_tools_test.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/cmd/rarity-api/main_test.go`

**Interfaces:**
- Identity-conflict dependency:
  `HasClientIdentityConflict(context.Context, authorization.Principal, string,
  string) (bool, error)`.
- Creator dependency:
  `CreateClient(context.Context, organizations.CreateClientCommand)
  (organizations.Client, error)`.
- Produces:
  `NewClientCreateTool(conflicts, creator, newID) aiassist.Tool`.

- [x] **Step 1: Write failing tool tests**

Require strict user input:

```go
type clientCreateRequest struct {
    DisplayID string `json:"display_id"`
    Name      string `json:"name"`
}
```

The input preparer must strictly decode that public shape, reject any
caller-supplied Client ID or unknown field, and marshal an internal prepared
shape containing one generated stable future Client ID. Tests must prove
MSP-global target resolution,
`client.create` capability, exact preview fields (`name`, `display_id`,
`lifecycle_state: active`), version zero, and execute mapping to
`organizations.CreateClientCommand` with the prepared Client ID, proposal
correlation ID, human actor, and `ai_workspace` source.

Add validation failures for unknown fields, empty name/display ID,
Client-scoped caller, duplicate normalized name, and duplicate display ID
across all lifecycle states, including inactive, archived, and deleted. Repeat
the identity-conflict check during `Preview` so confirmation becomes stale or
invalid if a duplicate appears after proposal creation.

- [x] **Step 2: Run focused tests and verify red**

Run:

```sh
go test ./backend/internal/aiassist/rtitools ./backend/cmd/rarity-api \
  -run 'ClientCreate|AIComposition' -count=1
```

Expected: build failure because the tool and composition are absent.

- [x] **Step 3: Implement and register the tool**

Register `client.create` in the closed catalog with the existing organization
identity-conflict check and creation service. Update catalog count/capability
assertions. The tool must call the ordinary creation service only from
`Execute`.

- [x] **Step 4: Run focused tests and verify green**

Run the Step 2 command and expect PASS.

### Task 5: Make chat planning and routes MSP-wide

**Files:**
- Modify: `backend/internal/aiassist/workspace_planner.go`
- Modify: `backend/internal/aiassist/workspace_planner_test.go`
- Modify: `backend/internal/httpapi/ai_workspace_routes.go`
- Modify: `backend/internal/httpapi/ai_workspace_routes_test.go`

**Contract:**
- Recognize:
  `Create a client named NAME with display ID DISPLAY_ID.`
- Extend `WorkspaceMessagePlan` with `ClientDisplayID`.
- Project and Task commands resolve their supplied Client globally.
- Client creation preflights name/display-ID conflicts across every lifecycle
  state, then proposes `client.create`.
- Zero/ambiguous/missing references produce plain-English clarification and no
  proposal.

- [x] **Step 1: Write failing planner tests**

Prove exact extraction of name/display ID, helpful clarification when either
is missing, and no regression to Project, Task, or product-help intents.

- [x] **Step 2: Write failing route tests**

Use an MSP-global principal and a directory containing at least two Clients.
Prove a Project command can target the non-page Client by name, a Task command
resolves its Project only inside the named Client, and Client creation
proposes only the two user-supplied business fields. Cover name/display-ID
resolution, missing, ambiguous, duplicate creation, unauthorized directory,
and Client-scoped request rejection for new MSP-wide conversations.

- [x] **Step 3: Run focused tests and verify red**

Run:

```sh
go test ./backend/internal/aiassist ./backend/internal/httpapi \
  -run 'WorkspacePlanner|AIWorkspace|ClientResolution' -count=1
```

Expected: planner/route failures under the active-Client implementation.

- [x] **Step 4: Implement global route resolution**

Replace `requireAIWorkspaceActiveClient` with a helper that calls the ordinary
global directory and `organizations.ResolveClientReference`. Put the resolved
internal ID in typed Project/Task inputs. Resolve Task Projects only after the
Client target is known. Add the `client.create` route branch and safe
clarification responses without exposing inaccessible Client details.

- [x] **Step 5: Run focused tests and verify green**

Run the Step 3 command and expect PASS.

### Task 6: Make the React drawer MSP-wide

**Files:**
- Modify: `frontend/src/features/ai/types.ts`
- Modify: `frontend/src/features/ai/workspaceApi.ts`
- Modify: `frontend/src/features/ai/api.test.ts`
- Modify: `frontend/src/features/ai/AIWorkspace.tsx`
- Modify: `frontend/src/features/ai/AIWorkspace.test.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/features/ai/workspace.css`

**Contract:**
- `AIWorkspaceAPI` methods no longer accept a Client ID.
- `workspaceApi` sends CSRF/session headers but never
  `X-Rarity-Client-ID`.
- `AIWorkspace` receives `clients` and `pageClientID`; `pageClientID` only
  seeds the required Target Client selector.
- Header scope text is `All authorized clients.`
- Structured Project and ticket proposals submit the explicitly selected
  Client ID.
- Changing the page filter does not clear or reload an open global
  conversation.
- Confirmed Client creation refreshes the authorized Client directory without
  reloading the application.
- Target-bearing proposals require a canonical directory identity before
  confirmation; unresolved targets are visible and fail closed.
- The Client creation preview retains its exact `New client` identity.

- [x] **Step 1: Write failing API tests**

Update every workspace method test to assert the Client-context header is
absent and request paths/bodies remain correct.

- [x] **Step 2: Write failing component/application tests**

Prove the global scope label, required Target Client selector, page-filter
default, selecting another Client, correct typed-tool input, empty-directory
guidance, and conversation continuity when `pageClientID` changes. Prove App
passes the authorized directory and current page filter separately, refreshes
it after confirmed Client creation, and makes the new Client available to
subsequent selectors and proposal previews. Prove unresolved proposal targets
disable confirmation while ordinary resolved targets remain actionable.

- [x] **Step 3: Run frontend tests and verify red**

Run:

```sh
npm --prefix frontend test -- --run \
  AIWorkspace api App
```

Expected: type/assertion failures under the Client-bound API.

- [x] **Step 4: Implement the API and component changes**

Remove Client ID parameters from the workspace API. In `AIWorkspace`, maintain
an explicit `targetClientID` for structured forms and initialize it from a
valid `pageClientID`, otherwise the first authorized Client. Do not include
`pageClientID` in the conversation-loading effect dependencies.

Render a labeled Client selector before action-specific inputs:

```tsx
<label>
  Target client
  <select value={targetClientID} required>
    {clients.map((client) => (
      <option key={client.id} value={client.id}>
        {client.name} ({client.display_id})
      </option>
    ))}
  </select>
</label>
```

Keep the existing dense RTM-authoritative drawer styling and responsive
behavior.

- [x] **Step 5: Run frontend tests and verify green**

Run the Step 3 command and expect PASS.

### Task 7: Integrate, document, and verify the complete slice

**Files:**
- Modify: `docs/09-roadmap/implementation-plan.md`
- Modify: `docs-site/index.html`
- Modify: `docs-site/app.js`
- Modify: implementation files only where integration failures expose a
  requirement already covered above

- [x] **Step 1: Run focused cross-layer verification**

```sh
go test ./backend/internal/aiassist ./backend/internal/aiassist/rtitools \
  ./backend/internal/httpapi ./backend/internal/organizations \
  ./backend/internal/store/psa -run 'Workspace|Client|Proposal' -count=1
npm --prefix frontend test -- --run \
  AIWorkspace DirectorySettingsPage browserSession
```

Expected: PASS.

- [x] **Step 2: Update canonical status and rendered tracker**

Record the completed MSP-wide AI Client slice, its confirmation boundary, and
the still-excluded Client business-data automation. Add this plan to the
docs-site source manifest.

- [x] **Step 3: Run full local verification**

```sh
go test ./...
go vet ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
node scripts/validate-markdown-links.mjs
node scripts/validate-docs.mjs
git diff --check
```

Expected: all commands PASS. If the repository validator names differ, use the
documented equivalents and record the exact command/output.

- [x] **Step 4: Complete reviewer gates**

For each implementation task, run a specification-compliance review before a
code-quality review. Resolve every material finding and rerun the task's
focused tests. Finish with a whole-diff review against the approved design.

- [x] **Step 5: Commit and publish**

Review the exact staged diff, commit only this slice, push `main`, and verify
the remote branch resolves to the local commit.

- [x] **Step 6: Deploy the exact revision to the RTI demo**

Use the RTI demo-server runbook and its scoped Compose project. Verify the
deployed source revision, migration 79, ready/health endpoints, and fresh
frontend assets. Do not treat push success as deployment acceptance.

- [x] **Step 7: Complete full-page live acceptance**

At a 1440 by 900 viewport on
`https://rarity.campbellservers.com`, verify:

1. the drawer says `All authorized clients`;
2. structured forms allow any authorized Client without changing page filter;
3. a Project or Task command resolves `Northwind Legal` by name;
4. submit
   `Create a client named Cedar Grove AI Rejection Test with display ID CLIENT-AI-REJECT-20260804`;
5. verify the preview contains only name, display ID, and active lifecycle;
6. reject the proposal;
7. reload the Client directory and prove its count/names are unchanged and the
   synthetic Client is absent.

Capture visible evidence for the proposal, rejection, and unchanged directory.

Accepted at 1440 by 900 on demo revision
`690b1e9dc1338a1731ef7be0d4a69c3400514fec`. Migration 79, public health,
readiness, build identity, and fresh assets passed. The drawer displayed
`All authorized clients`; the structured selector displayed
`Northwind Legal (NORTHWIND)`; the Project command resolved the same canonical
Client and tasks A, B, C; the synthetic Client preview contained only the
supplied name/display ID and active lifecycle. Rejection reported
`Action rejected.`, and a fresh directory reload contained only Northwind with
the synthetic Client absent. Browser console warnings/errors were empty.

- [ ] **Step 8: Close the work item**

Update the active RTI Kanban card with verification, commit, deployment, and
acceptance evidence, then move it through the configured review/completion
state only when its acceptance criteria are satisfied.

### Task 8: Unify Prospect conversion with atomic Client identity enforcement

**Files:**

- Create: `backend/internal/clientidentity/identity.go`
- Create: `backend/internal/clientidentity/identity_test.go`
- Create:
  `backend/internal/organizations/client_identity_cross_path_integration_test.go`
- Modify: organization and PSA conversion PostgreSQL repositories/tests
- Modify: conversion HTTP regression, RTI CSS tokens, approved design, roadmap,
  and rendered tracker

**Contract:**

- Ordinary, AI, and Prospect-conversion Client creation use one shared
  deterministic normalization, MSP advisory transaction lock, all-lifecycle
  conflict query, cross-field match, and typed conflict.
- Prospect conversion acquires that boundary inside its existing larger
  transaction before any Client/contact/Project/conversion/fact write.
- Conversion that selects an existing Client does not acquire the identity
  lock.
- The public conversion boundary returns safe
  `409 client_identity_conflict` without revealing the conflicting Client.

- [x] **Step 1: Capture focused RED**

Conversion transaction tests observed the missing lock/recheck ordering and
three Unicode/cross-field lifecycle conflicts returning success. The shared
boundary test observed the absent API, and the conversion HTTP regression
observed a safe shared conflict returning `500` instead of `409`.

- [x] **Step 2: Implement the shared transaction boundary**

Extract the lock SQL, all-lifecycle conflict SQL, normalization, cross-field
matching, and conflict sentinel into the smallest import-cycle-free internal
package. Adapt both repositories while retaining the conversion transaction.

- [x] **Step 3: Add cross-path and minor regressions**

Replace the two-ordinary-writer PostgreSQL check with ordinary
`CreateClientAtomic` versus real Prospect conversion. Cleanup explicitly
removes conversion, Project, contact, audit/outbox, Client, sales seed, and MSP
records, transactionally disabling and restoring immutable test triggers.
Replace the undefined `--rti-surface-raised` references with the existing
`--rti-surface-inset` token.

- [x] **Step 4: Verify the final local tree**

The focused affected packages and all required portable full gates pass. The
real PostgreSQL cross-path test compiles and skips locally because an isolated
`TEST_DATABASE_URL` is unavailable. Exact-revision deployment and live
acceptance completed under Task 7.
