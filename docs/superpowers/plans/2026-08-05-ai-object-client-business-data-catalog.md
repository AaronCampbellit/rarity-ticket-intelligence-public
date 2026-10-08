# AI Object Catalog and Client Business Data Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the MSP-wide AI workspace with safe Client-resource
read/create/update/lifecycle actions and the next service-backed operational
object catalog.

**Architecture:** Three isolated workstreams implement one approved design.
The Client-resource foundation owns exact resolution, active-target
transactions, typed conflicts, and prepared identity/correlation. Lifecycle
builds on that boundary with optimistic versioning and typed state
transitions. The broader catalog adds only closed tools backed by existing
ordinary services. Each write retains exact preview, confirmation-time
reauthorization/re-preview, audit, outbox, and no-invented-data behavior.

**Tech Stack:** Go 1.25, PostgreSQL 16/Goose/pgx, React 19, TypeScript, Vitest,
Docker Compose demo deployment, Hank Notes Kanban.

**Progress (2026-08-06):** Tasks 1–13 are complete. The Task 12 portable
gates and original isolated PostgreSQL suite passed, then final whole-branch
review placed the branch on hold for six material resolver, summary,
transaction, error-mapping, and proposal-identity findings. One source fix
wave addresses all six. Refreshed Go, vet, build, documentation, diff, focused
frontend, and isolated PostgreSQL evidence now passes; exact re-review found no
remaining Critical or Important issues. Task 13 publication, exact-revision
deployment, and 1440×900 live acceptance completed. The live pass also found
and permanently repaired the all-Clients routing/calendar/SLA setup path and
routing capability exposure before accepting exact Ticket routing. Deferred
second-wave and high-impact financial, credential, integration, and
contractual actions remain outside this first-wave catalog.

## Global Constraints

- New AI workspace conversations are MSP-global; page Client is a hint only.
- Every Client-targeted action explicitly resolves one authorized active
  Client.
- User/model input never supplies trusted IDs, scope, actor, correlation,
  version, lifecycle, or authority.
- The user supplies every business value; omitted optional values remain
  unset.
- Unknown JSON fields fail validation.
- Names/display IDs resolve server-side; zero/ambiguous results create no
  proposal.
- Every write expires, previews exact changes, and requires confirmation.
- Confirmation reloads principal, target, references, state, and version.
- Execution uses ordinary services with `Source: "ai_workspace"` and proposal
  correlation.
- Rejection and expiry never invoke a writer.
- No generic CRUD, SQL, HTTP, shell, route-selection, or high-impact financial,
  credential, integration, or contractual tool.
- Use an isolated demo scratch database for `TEST_DATABASE_URL`; never run
  destructive integration cleanup against the live demo database.
- Preserve unrelated dirty work and merge the stream branches in dependency
  order.

---

## Stream ownership and merge order

- **Stream A — Client resources create/read:** Tasks 1–4.
- **Stream B — Client resources lifecycle:** Tasks 5–7.
- **Stream C — Broader operational catalog:** Tasks 8–10.
- **Integration owner:** Tasks 11–13.

Streams use isolated worktrees. A and C may merge independently. B may develop
ordinary domain/repository work in parallel, but it must rebase onto A before
its AI/API/UI composition is reviewed and merged.

### Task 1: Add exact Client-resource query and resolution contracts

**Files:**
- Create: `backend/internal/clientresources/query.go`
- Create: `backend/internal/clientresources/query_test.go`
- Modify: `backend/internal/clientresources/catalog.go`
- Modify: `backend/internal/store/psa/client_resource_catalog_repository.go`
- Modify: `backend/internal/store/psa/client_resource_catalog_repository_test.go`

**Interfaces:**
- Produces:

```go
type Kind string

const (
    LocationKind Kind = "location"
    ContactKind  Kind = "contact"
    AssetKind    Kind = "asset"
    ServiceKind  Kind = "service"
    ContractKind Kind = "contract"
)

type Query struct {
    Kind             Kind
    Literal          string
    Limit            int
    IncludeInactive  bool
}

type ResourceDetail struct {
    Summary
    LifecycleState string     `json:"lifecycle_state"`
    Email          string     `json:"email,omitempty"`
    Phone          string     `json:"phone,omitempty"`
    AssetType      string     `json:"asset_type,omitempty"`
    SourceSystem   string     `json:"source_system,omitempty"`
    ExternalID     string     `json:"external_id,omitempty"`
    Authority      Authority  `json:"authority,omitempty"`
    Criticality    string     `json:"criticality,omitempty"`
    StartsOn       *time.Time `json:"starts_on,omitempty"`
    EndsOn         *time.Time `json:"ends_on,omitempty"`
}

type QueryRepository interface {
    QueryResources(context.Context, scope.Target, Query) ([]ResourceDetail, error)
    GetResource(context.Context, scope.Target, Kind, string, bool) (ResourceDetail, error)
}

func (s *CatalogService) Query(context.Context, QueryCommand) ([]ResourceDetail, error)
func (s *CatalogService) Resolve(context.Context, ResolveCommand) (ResourceDetail, error)
func (s *CatalogService) Get(context.Context, GetCommand) (ResourceDetail, error)
```

`Resolve` checks exact display ID first, then normalized exact name. More than
one name match returns `ErrAmbiguousResource`. Creation/reference resolution is
active-only. Exact lifecycle Get may include inactive.

- [x] **Step 1: Write failing query and resolver tests**

Add table tests for kind/query/limit validation, active-only default,
include-inactive exact Get, exact display-ID precedence, normalized exact
name, missing/ambiguous, and cross-Client isolation.

```go
func TestCatalogResolveRequiresUniqueExactReference(t *testing.T) {
    repository := &queryRepositoryStub{results: []ResourceDetail{
        {Summary: Summary{ID: "a", DisplayID: "LOC-A", Name: "Head Office"}},
        {Summary: Summary{ID: "b", DisplayID: "LOC-B", Name: "Head Office"}},
    }}
    _, err := NewCatalogService(repository).Resolve(
        context.Background(),
        ResolveCommand{Principal: activePrincipal(), Target: northwind(), Kind: LocationKind, Reference: "Head Office"},
    )
    if !errors.Is(err, ErrAmbiguousResource) {
        t.Fatalf("error = %v, want ErrAmbiguousResource", err)
    }
}
```

- [x] **Step 2: Run tests and capture RED**

Run:

```bash
go test ./backend/internal/clientresources ./backend/internal/store/psa \
  -run 'Catalog|ResourceQuery|ResolveResource' -count=1
```

Expected: FAIL because query types/repository methods are absent.

- [x] **Step 3: Implement typed query service and PostgreSQL query**

Use a fixed `switch Kind` to select reviewed SQL fragments. Bind all values.
Apply exact MSP/Client and lifecycle predicates. Never interpolate caller text
as identifiers or SQL.

```go
func (s *CatalogService) Resolve(ctx context.Context, command ResolveCommand) (ResourceDetail, error) {
    results, err := s.Query(ctx, QueryCommand{
        Principal: command.Principal,
        Target: command.Target,
        Query: Query{Kind: command.Kind, Literal: strings.TrimSpace(command.Reference), Limit: 50},
    })
    if err != nil { return ResourceDetail{}, err }
    if byDisplayID := exactDisplayID(results, command.Reference); len(byDisplayID) == 1 {
        return byDisplayID[0], nil
    }
    byName := normalizedExactName(results, command.Reference)
    if len(byName) == 1 { return byName[0], nil }
    if len(byName) > 1 { return ResourceDetail{}, ErrAmbiguousResource }
    return ResourceDetail{}, scope.ErrNotFound
}
```

- [x] **Step 4: Run focused tests and vet**

Run:

```bash
go test ./backend/internal/clientresources ./backend/internal/store/psa \
  -run 'Catalog|ResourceQuery|ResolveResource' -count=1
go vet ./backend/internal/clientresources ./backend/internal/store/psa
```

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add backend/internal/clientresources/query.go \
  backend/internal/clientresources/query_test.go \
  backend/internal/clientresources/catalog.go \
  backend/internal/store/psa/client_resource_catalog_repository.go \
  backend/internal/store/psa/client_resource_catalog_repository_test.go
git commit -m "feat: add exact Client resource queries"
```

### Task 2: Harden ordinary Client-resource creation

**Files:**
- Modify: `backend/internal/clientresources/service.go`
- Modify: `backend/internal/clientresources/service_test.go`
- Modify: `backend/internal/store/psa/client_resource_repository.go`
- Modify: `backend/internal/store/psa/contact_repository.go`
- Modify: `backend/internal/store/psa/asset_repository.go`
- Modify: `backend/internal/store/psa/service_resource_repository.go`
- Modify: `backend/internal/store/psa/contract_repository.go`
- Modify: matching repository test files
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/internal/httpapi/psa_routes_test.go`

**Interfaces:**
- Consumes: `clientresources.Kind`, exact query contracts from Task 1,
  `psa.lockActiveClient`.
- Produces:

```go
var ErrResourceIdentityConflict = errors.New("resource identity conflict")

type PreparedIdentity struct {
    ResourceID    string
    CorrelationID string
}
```

Add `Prepared PreparedIdentity` to all five create commands. Empty values cause
ordinary services to generate IDs; non-empty values are validated and used
verbatim by the trusted AI adapter.

- [x] **Step 1: Write failing service and repository transaction tests**

For every kind assert:

1. active-Client lock is the first query;
2. inactive Client returns `scope.ErrNotFound` before insert/audit/outbox;
3. Contact/Asset require an active same-Client Location;
4. prepared resource/correlation IDs reach resource, audit, and event;
5. PostgreSQL unique violations map to `ErrResourceIdentityConflict`;
6. rollback occurs on every partial failure.

```go
func TestCreateContactLocksClientAndActiveLocationBeforeInsert(t *testing.T) {
    tx := newOrderedResourceTx()
    tx.ExpectQuery(activeClientLockSQL)
    tx.ExpectQuery(activeLocationSQL)
    tx.ExpectExec(contactInsertSQL)
    tx.ExpectExec(auditInsertSQL)
    tx.ExpectExec(outboxInsertSQL)
    require.NoError(t, repository.CreateAtomic(context.Background(), mutation))
    tx.AssertComplete(t)
}
```

- [x] **Step 2: Run tests and capture RED**

Run:

```bash
go test ./backend/internal/clientresources ./backend/internal/store/psa \
  ./backend/internal/httpapi -run 'Create(Location|Contact|Asset|Service|Contract)|ResourceIdentity' -count=1
```

Expected: FAIL on missing lock, active Location, prepared identity, and safe
conflict contracts.

- [x] **Step 3: Implement shared hardening**

Call `lockActiveClient` immediately after `Begin`. Add
`lifecycle_state = 'active'` to Location validation SQL. Map constraint names:

```go
func resourceWriteError(err error) error {
    var pgErr *pgconn.PgError
    if errors.As(err, &pgErr) && pgErr.Code == "23505" {
        switch pgErr.ConstraintName {
        case "locations_msp_id_client_id_display_id_key",
             "contacts_msp_id_client_id_display_id_key",
             "assets_msp_id_client_id_display_id_key",
             "services_msp_id_client_id_display_id_key",
             "contracts_msp_id_client_id_display_id_key",
             "assets_external_identity_unique":
            return clientresources.ErrResourceIdentityConflict
        }
    }
    return err
}
```

Add safe HTTP mapping:

```go
case errors.Is(err, clientresources.ErrResourceIdentityConflict):
    writeError(ctx, writer, http.StatusConflict, "resource_identity_conflict", "resource identity conflicts with an existing record")
```

- [x] **Step 4: Run focused and full affected tests**

Run:

```bash
go test ./backend/internal/clientresources ./backend/internal/store/psa \
  ./backend/internal/httpapi -count=1
go vet ./backend/internal/clientresources ./backend/internal/store/psa \
  ./backend/internal/httpapi
```

Expected: PASS.

- [x] **Step 5: Commit**

Stage only Task 2 files and commit:

```bash
git commit -m "fix: harden Client resource creation"
```

### Task 3: Add Client-resource AI read/create tools

**Files:**
- Create: `backend/internal/aiassist/rtitools/client_resource_tools.go`
- Create: `backend/internal/aiassist/rtitools/client_resource_tools_test.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/cmd/rarity-api/main_test.go`
- Modify: `backend/internal/aiassist/workspace_planner.go`
- Modify: `backend/internal/aiassist/workspace_planner_test.go`
- Modify: `backend/internal/aiassist/rtitools/composed_services_test.go`

**Interfaces:**
- Consumes: Task 1 resolver/query and Task 2 prepared identity.
- Produces tools:
  `client_resource.list`, `location.create`, `contact.create`, `asset.create`,
  `service.create`, `contract.create`.

- [x] **Step 1: Write strict-schema and tool-contract tests**

Each write test covers valid preview, unknown field, missing business data,
capability denial, cross-Client target, ambiguous Location, inactive Location,
prepared ID, exact preview, confirmation drift, execution correlation, and
rejection no-write.

```go
func TestAssetCreateToolDoesNotInventOptionalBusinessData(t *testing.T) {
    proposal := prepare(t, `{"client":"Northwind Legal","display_id":"AST-1","name":"Firewall","asset_type":"firewall"}`)
    assertChangeAbsent(t, proposal.Preview, "location")
    assertChangeAbsent(t, proposal.Preview, "source system")
    assertChangeAbsent(t, proposal.Preview, "external id")
    assertChange(t, proposal.Preview, "authority", "", "technician_confirmed")
}
```

- [x] **Step 2: Run tests and capture RED**

Run:

```bash
go test ./backend/internal/aiassist ./backend/internal/aiassist/rtitools \
  ./backend/cmd/rarity-api -run 'ClientResource|LocationCreate|ContactCreate|AssetCreate|ServiceCreate|ContractCreate' -count=1
```

Expected: FAIL because tools and composition are absent.

- [x] **Step 3: Implement closed constructors and planner grammars**

Use reviewed typed inputs and `json.Decoder.DisallowUnknownFields`. Preparation
resolves Client/Location and adds `resource_id`. The planner supports exact
commands and returns clarification when required clauses are missing.

```go
type resourceCreatePrepared struct {
    ClientID  string `json:"client_id"`
    ResourceID string `json:"resource_id"`
    DisplayID string `json:"display_id"`
    Name      string `json:"name"`
}
```

Do not create a generic model-controlled resource tool. Shared Go constructors
may reduce duplication while each registered tool retains one fixed name,
capability, validator, preview, and executor.

- [x] **Step 4: Run focused and composition tests**

Run:

```bash
go test ./backend/internal/aiassist ./backend/internal/aiassist/rtitools \
  ./backend/cmd/rarity-api -count=1
go vet ./backend/internal/aiassist ./backend/internal/aiassist/rtitools \
  ./backend/cmd/rarity-api
```

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git commit -m "feat: add AI Client resource creation tools"
```

### Task 4: Add Client-resource drawer modes

**Files:**
- Modify: `frontend/src/features/ai/types.ts`
- Modify: `frontend/src/features/ai/workspaceApi.ts`
- Modify: `frontend/src/features/ai/api.test.ts`
- Modify: `frontend/src/features/ai/AIWorkspace.tsx`
- Modify: `frontend/src/features/ai/AIWorkspace.test.tsx`
- Modify: `frontend/src/features/ai/AIProposal.tsx`
- Modify: `frontend/src/features/ai/workspace.css`

**Interfaces:**
- Consumes Task 3 tool names and backend-shaped proposal/result payloads.
- Produces structured `Add client resource` and `Look up client resources`
  modes.

- [x] **Step 1: Write failing UI tests**

Cover capability-filtered kinds, Target Client changes, Location loading,
dynamic required fields, optional values omitted, list results, all five exact
proposals, canonical Client/Location display, rejection, confirmation,
stale/unresolved fail-closed behavior, and accessibility.

```tsx
expect(screen.getByRole("option", { name: "Add client resource" })).toBeVisible()
await user.selectOptions(screen.getByLabelText("Resource kind"), "contact")
expect(screen.getByLabelText("Location")).toHaveDisplayValue("No location")
```

- [x] **Step 2: Run tests and capture RED**

Run:

```bash
npm --prefix frontend test -- --run AIWorkspace api
```

Expected: FAIL because modes/fields are absent.

- [x] **Step 3: Implement compact dynamic forms**

Add discriminated form state per kind. Serialize only non-empty optional
fields. Reuse canonical proposal rendering and the existing authorized Client
directory. Location options load from `client_resource.list` after Client/kind
selection.

- [x] **Step 4: Run frontend tests/build**

Run:

```bash
npm --prefix frontend test -- --run
npm --prefix frontend run build
```

Expected: PASS; only the existing non-failing chunk-size advisory is allowed.

- [x] **Step 5: Commit**

```bash
git commit -m "feat: add AI Client resource workspace"
```

### Task 5: Add versioned Client-resource lifecycle domain contracts

**Files:**
- Create: `backend/migrations/000097_client_resource_lifecycle.sql`
- Modify: `backend/migrations/client_resource_metadata_contract_test.go`
- Create: `backend/internal/clientresources/lifecycle.go`
- Create: `backend/internal/clientresources/lifecycle_test.go`
- Modify: `backend/internal/clientresources/service.go`
- Modify: `backend/internal/clientresources/service_test.go`
- Create or modify per-kind lifecycle repository files/tests under
  `backend/internal/store/psa/`
- Modify: `backend/internal/mutation/audit.go` and tests if required for safe diff

**Interfaces:**
- Consumes Task 1 exact Get/query and Task 2 active-Client lock.
- Produces:

```go
type UpdateCommand struct {
    Principal authorization.Principal
    Target scope.Target
    Kind Kind
    ResourceID string
    ExpectedVersion int64
    Patch UpdatePatch
    Reason string
    ActorID string
    Source string
    CorrelationID string
}

type LifecycleCommand struct {
    Principal authorization.Principal
    Target scope.Target
    Kind Kind
    ResourceID string
    ExpectedVersion int64
    Reason string
    ActorID string
    Source string
    CorrelationID string
}

func (s *Service) Update(context.Context, UpdateCommand) (ResourceDetail, error)
func (s *Service) Deactivate(context.Context, LifecycleCommand) (ResourceDetail, error)
func (s *Service) Reactivate(context.Context, LifecycleCommand) (ResourceDetail, error)
```

Typed errors: `ErrLifecycleConflict`, `ErrResourceInUse`,
`ErrResourceAuthorityConflict`.

- [x] **Step 1: Write migration and domain RED tests**

Cover lifecycle CHECKs, active/inactive transitions, expected version, reason,
no-op patch, immutable display ID/scope/provenance, Contact phone/email
validation, Contract dates, criticality, explicit clears, discovered Asset
authority, and Location dependencies.

- [x] **Step 2: Run RED**

Run:

```bash
go test ./backend/migrations ./backend/internal/clientresources \
  ./backend/internal/store/psa -run 'ResourceLifecycle|ResourceUpdate|SafeDiff' -count=1
```

Expected: FAIL because migration/services/repositories are absent.

- [x] **Step 3: Implement typed state machines and repositories**

Each transaction locks active Client, loads exact resource/dependencies, then
updates with:

```sql
WHERE id = $1
  AND msp_id = $2
  AND client_id = $3
  AND version = $4
  AND lifecycle_state = $5
```

Require one affected row, increment version, append redacted audit and
minimized outbox records, and commit.

- [x] **Step 4: Run focused tests/vet**

Run:

```bash
go test ./backend/migrations ./backend/internal/clientresources \
  ./backend/internal/store/psa -count=1
go vet ./backend/internal/clientresources ./backend/internal/store/psa
```

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git commit -m "feat: add Client resource lifecycle services"
```

### Task 6: Expose lifecycle HTTP and AI contracts

**Files:**
- Modify: `backend/internal/httpapi/client_resource_routes.go`
- Modify: `backend/internal/httpapi/psa_dto.go`
- Modify: `backend/internal/httpapi/psa_routes_test.go`
- Modify: `backend/internal/httpapi/principal_routes.go`
- Modify: `backend/internal/httpapi/principal_routes_test.go`
- Create: `backend/internal/aiassist/rtitools/client_resource_lifecycle_tools.go`
- Create: `backend/internal/aiassist/rtitools/client_resource_lifecycle_tools_test.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/cmd/rarity-api/main_test.go`
- Modify: `backend/internal/aiassist/workspace_planner.go`
- Modify: `backend/internal/aiassist/workspace_planner_test.go`

**Interfaces:**
- Produces ordinary GET/PATCH/deactivate/reactivate routes, capabilities
  `<kind>.update`/`<kind>.lifecycle`, and 15 closed AI tool names.

- [x] **Step 1: Write failing route/tool tests**

Cover quoted `If-Match`, body/header version agreement, safe error mapping,
exact preview, explicit clear, missing reason, stale version, state drift,
Location dependency, discovered Asset, rejection, and audit correlation.

- [x] **Step 2: Run RED**

```bash
go test ./backend/internal/httpapi ./backend/internal/aiassist \
  ./backend/internal/aiassist/rtitools ./backend/cmd/rarity-api \
  -run 'Resource(Update|Deactivate|Reactivate|Lifecycle)' -count=1
```

Expected: FAIL because routes/tools/capabilities are absent.

- [x] **Step 3: Implement thin adapters**

Use shared constructors internally, but register fixed names such as
`location.update`, `location.deactivate`, and `location.reactivate`. Prepared
inputs contain trusted resource ID/current version. Public inputs contain
reference, exact patch, and reason.

- [x] **Step 4: Run focused tests/vet**

```bash
go test ./backend/internal/httpapi ./backend/internal/aiassist \
  ./backend/internal/aiassist/rtitools ./backend/cmd/rarity-api -count=1
go vet ./backend/internal/httpapi ./backend/internal/aiassist/... \
  ./backend/cmd/rarity-api
```

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git commit -m "feat: expose Client resource lifecycle actions"
```

### Task 7: Replace Client Resources cards with lifecycle table

**Files:**
- Modify: `frontend/src/features/organization/ClientResourcesPage.tsx`
- Modify: `frontend/src/features/organization/ClientResourcesPage.test.tsx`
- Modify: `frontend/src/features/ai/AIWorkspace.tsx`
- Modify: `frontend/src/features/ai/AIWorkspace.test.tsx`
- Modify: relevant operations/workspace CSS

**Interfaces:**
- Consumes Task 6 routes/tool contracts.
- Produces dense resource table, active/inactive filter, edit/lifecycle
  controls, and structured AI lifecycle modes.

- [x] **Step 1: Write failing UI/accessibility tests**

Cover kind/display/name/status/version columns, inactive filter, capability
gates, edit dialog, reason, exact version, deactivate/reactivate, conflicts,
discovered Asset lockout, responsive 1440×900 layout, and keyboard/axe checks.

- [x] **Step 2: Run RED**

```bash
npm --prefix frontend test -- --run ClientResourcesPage AIWorkspace
```

Expected: FAIL because lifecycle UI is absent.

- [x] **Step 3: Implement dense table and compact action surfaces**

Retain creation in a compact form/action. Do not render raw email/phone in
general audit notices. Refresh the catalog after successful ordinary or AI
mutation with last-started-request-wins sequencing.

- [x] **Step 4: Run frontend tests/build**

```bash
npm --prefix frontend test -- --run
npm --prefix frontend run build
```

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git commit -m "feat: add Client resource lifecycle workspace"
```

### Task 8: Add safe operational read tools

**Files:**
- Create: `backend/internal/aiassist/rtitools/operational_read_tools.go`
- Create: `backend/internal/aiassist/rtitools/operational_read_tools_test.go`
- Modify: Project/Knowledge/Sales query services only where explicit targets or
  minimized outputs are missing
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/cmd/rarity-api/main_test.go`

**Interfaces:**
- Produces `project.search`, `project.get`, `knowledge.search`,
  `knowledge.get`, and `prospect.list`.

- [x] **Step 1: Write failing read-tool tests**

Cover explicit authorized Client, bounded limit, minimized output,
cross-Client/inactive denial, exact/ambiguous reference behavior, financial
field capability filtering, and MSP-global Prospect listing.

- [x] **Step 2: Run RED**

```bash
go test ./backend/internal/aiassist/rtitools ./backend/internal/projects \
  ./backend/internal/knowledge ./backend/internal/sales ./backend/cmd/rarity-api \
  -run 'OperationalRead|ProjectSearch|KnowledgeSearch|ProspectList' -count=1
```

- [x] **Step 3: Implement service-backed read tools**

Return explicit minimized DTOs; never marshal full domain records by default.
Use exact target-aware services and caps of 50 results.

- [x] **Step 4: Run focused tests/vet**

```bash
go test ./backend/internal/aiassist/rtitools ./backend/internal/projects \
  ./backend/internal/knowledge ./backend/internal/sales ./backend/cmd/rarity-api -count=1
go vet ./backend/internal/aiassist/rtitools ./backend/internal/projects \
  ./backend/internal/knowledge ./backend/internal/sales ./backend/cmd/rarity-api
```

- [x] **Step 5: Commit**

```bash
git commit -m "feat: add AI operational read catalog"
```

### Task 9: Add Knowledge, Prospect, and Ticket routing writes

**Files:**
- Create: `backend/internal/aiassist/rtitools/operational_write_tools.go`
- Create: `backend/internal/aiassist/rtitools/operational_write_tools_test.go`
- Modify: `backend/internal/knowledge/service.go` and tests
- Modify: `backend/internal/sales/service.go` and tests
- Modify: `backend/internal/workrecords/queue.go` and tests
- Modify: `backend/internal/aiassist/workspace_planner.go` and tests
- Modify: `backend/cmd/rarity-api/main.go` and tests

**Interfaces:**
- Produces `knowledge.draft.create`, `knowledge.draft.revise`,
  `prospect.create`, and `ticket.route`.

- [x] **Step 1: Write failing tool/composition tests**

Cover exact user fields, trusted IDs/correlation, expected version, canonical
Article/Ticket/Queue, stale preview, capability, target, no inferred Prospect
contact data, rejection, and atomic facts.

- [x] **Step 2: Run RED**

```bash
go test ./backend/internal/aiassist ./backend/internal/aiassist/rtitools \
  ./backend/internal/knowledge ./backend/internal/sales \
  ./backend/internal/workrecords ./backend/cmd/rarity-api \
  -run 'KnowledgeDraft|ProspectCreate|TicketRoute' -count=1
```

- [x] **Step 3: Implement prepared identity/correlation and closed tools**

Knowledge create requires display ID/title/body. Revise requires exact Article,
expected version, title/body. Prospect requires display ID/name; email/phone
remain optional and absent unless supplied. Ticket route requires exact
Client/Ticket/Queue, expected version, and reason.

- [x] **Step 4: Run focused tests/vet**

Run affected full package tests and `go vet` for the same packages.

- [x] **Step 5: Commit**

```bash
git commit -m "feat: add AI operational write catalog"
```

### Task 10: Unify chat and structured action catalog

**Files:**
- Modify: `frontend/src/features/ai/AIWorkspace.tsx`
- Modify: `frontend/src/features/ai/AIWorkspace.test.tsx`
- Modify: `frontend/src/features/ai/types.ts`
- Modify: `frontend/src/features/ai/workspaceApi.ts`
- Modify: `backend/internal/aiassist/workspace_planner.go`
- Modify: `backend/internal/aiassist/workspace_planner_test.go`

**Interfaces:**
- Consumes Tasks 8–9 and existing ticket/project/task/client tools.
- Produces coherent structured/chat access to approved first-wave tools.

- [x] **Step 1: Write failing catalog tests**

Prove existing ticket get/search/transition/priority/note/reply and
project/task/client tools remain available, new read/write actions appear only
with capabilities, ambiguous references clarify, and UI copy says
`client-visible reply`, never email.

- [x] **Step 2: Run RED**

```bash
go test ./backend/internal/aiassist -run 'WorkspacePlanner|Operational' -count=1
npm --prefix frontend test -- --run AIWorkspace
```

- [x] **Step 3: Implement catalog grouping and deterministic grammars**

Group actions as Read, Work, Client resources, Knowledge, and Sales. Preserve
the exact Target Client selector for Client-bound actions and omit it only for
MSP-global Prospect actions.

- [x] **Step 4: Run backend/frontend gates**

```bash
go test ./backend/internal/aiassist -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run build
```

- [x] **Step 5: Commit**

```bash
git commit -m "feat: expose broader AI action catalog"
```

### Task 11: Run isolated PostgreSQL concurrency acceptance

**Files:**
- Create or modify integration tests under:
  `backend/internal/store/psa/` and `backend/internal/clientresources/`
- Modify test-only scripts only if needed for exact scratch DB lifecycle

**Interfaces:**
- Consumes merged Tasks 1–10.
- Produces real PostgreSQL evidence for resource/Client and lifecycle races.

- [x] **Step 1: Add environment-gated real database tests**

Prove:

- resource create versus Client deactivation serializes;
- two same-version updates produce one success and one conflict;
- Location deactivation versus dependent Contact/Asset mutation serializes;
- resource/audit/outbox rollback is atomic;
- existing ordinary-create versus Prospect-conversion Client identity test
  runs against the scratch database.

- [x] **Step 2: Run portable compile/skip**

```bash
go test ./backend/internal/clientresources ./backend/internal/store/psa \
  ./backend/internal/organizations -run 'Postgres|Concurrency|Serialize' -count=1 -v
```

Expected locally: PASS with explicit skip if `TEST_DATABASE_URL` is absent.

- [x] **Step 3: Provision exact demo scratch database**

On the demo host, create a separately named scratch database such as
`rti_ai_catalog_test_20260805`. Resolve the name exactly before create/drop.
Use the configured PostgreSQL role without printing credentials. Apply all
migrations, run tests from the exact committed source with
`TEST_DATABASE_URL`, and never point it at the live application database.

- [x] **Step 4: Run real tests and clean scratch database**

Expected: all concurrency assertions PASS. Drop only the exact scratch database
after terminating only its sessions. Recheck live RTI health/readiness and
Compose projects.

- [x] **Step 5: Record evidence commit**

Commit test/evidence documentation without claiming production HA.

### Task 12: Complete whole-branch review and portable verification

**Files:**
- Modify implementation only for reviewed findings
- Modify canonical docs and rendered tracker

- [x] **Step 1: Run per-stream specification and code-quality reviews**

Use fresh read-only reviewers. Resolve every Critical/Important finding and
re-review exact fix ranges.

- [x] **Step 2: Run final whole-branch review**

Review the complete diff against the approved design and this plan, including
authorization, target integrity, races, UI state, migration safety, safe
errors, and test realism.

- [x] **Step 3: Run full portable gates**

```bash
go test ./...
go vet ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
node scripts/validate-markdown-links.mjs
node scripts/validate-docs.mjs
git diff --check
```

Expected: PASS.

- [x] **Step 4: Commit integration/docs**

Stage only intended files and commit. Keep publication/deployment separate.

### Task 13: Publish, deploy, and complete live acceptance

**Files:**
- Modify acceptance/roadmap/docs-site evidence after live proof
- Update active Hank RTI Kanban work log

- [x] **Step 1: Merge dependency-ordered stream branches**

Merge A, rebase/merge B onto A, merge C, and verify the exact merged tree.

- [x] **Step 2: Push main and verify remote parity**

Push `main`; require local HEAD, `origin/main`, and remote `refs/heads/main` to
match exactly.

- [x] **Step 3: Deploy exact revision**

Use the RTI demo-server runbook and scoped Compose project. Verify exact source
revision, migrations through 81, health, readiness, build metadata, fresh
assets, clean startup, and unaffected Hank/RTM projects.

- [x] **Step 4: Run 1440×900 live acceptance**

Execute the approved scenarios from the design:

1. list all five resource kinds;
2. prepare/reject exact creates for all five;
3. confirm selected synthetic create/update/lifecycle actions;
4. prove stale/inactive/ambiguous/in-use/authority failures;
5. search/read Projects and Knowledge across Clients;
6. prepare/reject Prospect;
7. create/revise synthetic Knowledge draft;
8. route a synthetic Ticket with exact preview;
9. verify audit/outbox/version and empty console errors.

- [x] **Step 5: Publish acceptance evidence and align final demo SHA**

Commit and push evidence, redeploy the final documentation SHA, and reverify
exact build identity. Append the outcome to the active RTI Kanban card. Keep
the whole-system card active only for intentionally deferred second-wave and
high-impact actions.
