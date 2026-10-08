# Global AI Workspace Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver global permission-aware AI chat, safe RTI knowledge retrieval, and explicitly confirmed typed actions.

**Architecture:** Extend the durable `aiassist` runtime with conversations, curated product documents, and a typed tool registry. `AppShell` hosts a persistent drawer; every tool resolves server scope and invokes existing application services, with writes represented as expiring previews that are reauthorized on confirmation.

**Tech Stack:** Go 1.24, PostgreSQL/pgx, existing provider adapters and job worker, React 19, TypeScript, Vite, Vitest/Testing Library.

## Global Constraints

- Read-only tools may execute immediately.
- Every write requires an exact preview and explicit confirmation.
- The server reauthorizes the logged-in principal immediately before execution.
- Tools invoke registered application services only; arbitrary SQL, HTTP, shell, or routes are prohibited.
- Product knowledge excludes secrets, credentials, tokens, and recovery material.
- Conversation and operational context remain MSP- and Client-isolated.
- PostgreSQL full-text search is the initial retrieval mechanism.

---

## File Structure

- `backend/internal/aiassist/conversations.go`: conversation and message lifecycle.
- `backend/internal/aiassist/tools.go`: typed tool registry, proposals, confirmation, and execution.
- `backend/internal/aiassist/product_knowledge.go`: curated corpus and full-text retrieval.
- `backend/internal/store/psa/ai_workspace_repository.go`: workspace persistence and leases.
- `backend/internal/httpapi/ai_workspace_routes.go`: conversation and proposal APIs.
- `frontend/src/features/ai/AIWorkspace.tsx`: global drawer and conversation UI.
- `frontend/src/features/ai/AIProposal.tsx`: exact write preview and confirmation.

### Task 1: Add conversation, corpus, and proposal schema

**Files:**
- Create: `backend/migrations/000073_global_ai_workspace.sql`
- Modify: `backend/migrations/ai_contract_test.go`

**Interfaces:**
- Produces `ai_conversations`, `ai_messages`, `ai_product_documents`, and `ai_action_proposals`.

- [ ] **Step 1: Write the failing migration contract**

```go
func TestGlobalAIWorkspaceMigrationDefinesScopedConversationsAndExpiringProposals(t *testing.T) {
	body, err := FS.ReadFile("000073_global_ai_workspace.sql")
	if err != nil { t.Fatal(err) }
	sql := string(body)
	for _, fragment := range []string{
		"CREATE TABLE ai_conversations",
		"CREATE TABLE ai_messages",
		"CREATE TABLE ai_product_documents",
		"search_vector tsvector",
		"CREATE TABLE ai_action_proposals",
		"expires_at timestamptz NOT NULL",
		"state IN ('pending', 'confirmed', 'rejected', 'expired', 'failed')",
	} {
		if !strings.Contains(sql, fragment) { t.Fatalf("missing %q", fragment) }
	}
}
```

- [ ] **Step 2: Verify red**

Run: `go test ./backend/migrations -run TestGlobalAIWorkspace -count=1`  
Expected: FAIL because migration 73 is absent.

- [ ] **Step 3: Create scoped tables**

```sql
CREATE TABLE ai_conversations (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid,
  principal_id uuid NOT NULL,
  title text NOT NULL,
  archived_at timestamptz,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE ai_product_documents (
  id uuid PRIMARY KEY,
  source_key text NOT NULL,
  source_version text NOT NULL,
  section text NOT NULL,
  audience text NOT NULL,
  body text NOT NULL,
  search_vector tsvector GENERATED ALWAYS AS
    (to_tsvector('english', coalesce(section, '') || ' ' || coalesce(body, ''))) STORED,
  UNIQUE (source_key, source_version, section)
);
```

Add scoped messages and proposals containing tool name/version, normalized input JSON, exact preview JSON, target identity/version, required capability, expiry, state, actor, and correlation ID.

- [ ] **Step 4: Verify migration contracts**

Run: `go test ./backend/migrations -count=1`  
Expected: PASS.

- [ ] **Step 5: Commit schema**

```bash
git add backend/migrations/000073_global_ai_workspace.sql backend/migrations/ai_contract_test.go
git commit -m "feat: define global AI workspace schema"
```

### Task 2: Implement curated RTI knowledge and conversations

**Files:**
- Create: `backend/internal/aiassist/product_knowledge.go`
- Create: `backend/internal/aiassist/product_knowledge_test.go`
- Create: `backend/internal/aiassist/conversations.go`
- Create: `backend/internal/aiassist/conversations_test.go`
- Create: `backend/internal/store/psa/ai_workspace_repository.go`
- Create: `backend/internal/store/psa/ai_workspace_repository_test.go`

**Interfaces:**
- Produces `SearchProductKnowledge(ctx, query string, limit int) ([]Citation, error)`.
- Produces conversation create/list/get/archive and message append commands.

- [ ] **Step 1: Write failing safety and scope tests**

```go
func TestProductCorpusRejectsProtectedSourcesAndConversationCannotCrossClient(t *testing.T) {
	if err := ValidateProductDocument(ProductDocument{SourceKey: "docs/recovery-access.md", Body: "token"}); !errors.Is(err, ErrUnsafeKnowledgeSource) {
		t.Fatalf("%v", err)
	}
	_, err := service.Get(ctx, principalFor("client-2"), "conversation-client-1")
	if !errors.Is(err, scope.ErrNotFound) { t.Fatalf("%v", err) }
}
```

- [ ] **Step 2: Verify red**

Run: `go test ./backend/internal/aiassist -run 'ProductKnowledge|Conversation' -count=1`  
Expected: FAIL because services are absent.

- [ ] **Step 3: Implement safe corpus and scoped conversations**

```go
type Citation struct { SourceKey, SourceVersion, Section, Excerpt string }
type Conversation struct {
	ID, MSPID, ClientID, PrincipalID, Title string
	Version int64
	CreatedAt, UpdatedAt time.Time
}
```

Use an allowlist of approved documentation roots and an explicit denylist for recovery, secret, credential, token, and environment material. Retrieve excerpts with bounded PostgreSQL full-text queries. Require matching principal and active Client for conversation access.

- [ ] **Step 4: Verify domain and repository**

Run: `go test ./backend/internal/aiassist ./backend/internal/store/psa -run 'ProductKnowledge|Conversation' -count=1`  
Expected: PASS.

- [ ] **Step 5: Commit knowledge and conversations**

```bash
git add backend/internal/aiassist backend/internal/store/psa/ai_workspace_repository.go backend/internal/store/psa/ai_workspace_repository_test.go
git commit -m "feat: add safe AI conversations and product knowledge"
```

### Task 3: Implement the typed tool registry and confirmed writes

**Files:**
- Create: `backend/internal/aiassist/tools.go`
- Create: `backend/internal/aiassist/tools_test.go`
- Modify: `backend/internal/store/psa/ai_workspace_repository.go`

**Interfaces:**
- Produces `Tool`, `Registry`, `Propose`, `Confirm`, and `Reject`.

- [ ] **Step 1: Write failing authorization and confirmation tests**

```go
func TestWriteToolRequiresPreviewAndLiveReauthorization(t *testing.T) {
	registry := MustNewRegistry(ticketUpdateTool)
	proposal, err := registry.Propose(ctx, authorizedPrincipal, ToolRequest{Name: "ticket.update", Input: input})
	if err != nil || proposal.Preview["status"] != "Resolved" { t.Fatalf("%+v %v", proposal, err) }
	permissions.Revoke("work_record.update")
	_, err = registry.Confirm(ctx, authorizedPrincipal, proposal.ID, proposal.Version)
	if !errors.Is(err, authorization.ErrForbidden) { t.Fatalf("%v", err) }
	if executor.Calls != 0 { t.Fatalf("executor called %d times", executor.Calls) }
}
```

- [ ] **Step 2: Verify red**

Run: `go test ./backend/internal/aiassist -run Tool -count=1`  
Expected: FAIL because the registry is absent.

- [ ] **Step 3: Implement closed registry contracts**

```go
type Tool interface {
	Name() string
	Version() int
	Kind() ToolKind
	Validate(json.RawMessage) error
	ResolveScope(context.Context, authorization.Principal, json.RawMessage) (scope.Target, error)
	RequiredCapability() string
	Preview(context.Context, authorization.Principal, json.RawMessage) (Preview, error)
	Execute(context.Context, authorization.Principal, json.RawMessage, string) (ToolResult, error)
}
```

Reject duplicate names, unknown tools, malformed inputs, missing capabilities, stale targets, and expired proposals. Read tools execute after authorization. Write tools persist normalized input and preview, then reload principal and target before confirmation.

- [ ] **Step 4: Verify registry**

Run: `go test ./backend/internal/aiassist -run 'Tool|Proposal' -count=1`  
Expected: PASS.

- [ ] **Step 5: Commit the execution boundary**

```bash
git add backend/internal/aiassist/tools.go backend/internal/aiassist/tools_test.go backend/internal/store/psa/ai_workspace_repository.go
git commit -m "feat: add confirmed AI tool actions"
```

### Task 4: Add read tools and low-risk ticket write tools

**Files:**
- Create: `backend/internal/aiassist/rtitools/read_tools.go`
- Create: `backend/internal/aiassist/rtitools/read_tools_test.go`
- Create: `backend/internal/aiassist/rtitools/ticket_tools.go`
- Create: `backend/internal/aiassist/rtitools/ticket_tools_test.go`
- Modify: `backend/cmd/rarity-api/main.go`

**Interfaces:**
- Registers product help, navigation, health, ticket read/search, ticket create/update, note, and email tools.

- [ ] **Step 1: Write failing tool contract tests**

```go
func TestTicketUpdateToolUsesExistingServiceAndExactPreview(t *testing.T) {
	tool := NewTicketUpdateTool(workRecordService)
	preview, err := tool.Preview(ctx, principal, json.RawMessage(`{"id":"ticket-1","expected_version":3,"status":"resolved"}`))
	if err != nil { t.Fatal(err) }
	if preview.TargetVersion != 3 || preview.Changes["status"].After != "resolved" { t.Fatalf("%+v", preview) }
}
```

- [ ] **Step 2: Verify red**

Run: `go test ./backend/internal/aiassist/rtitools -count=1`  
Expected: FAIL because the package is absent.

- [ ] **Step 3: Implement tools as application-service adapters**

Each adapter validates a fixed input struct, declares the same capability used by the normal route, loads server-owned context, and invokes the existing domain service. Health output contains safe status codes only. Product-help citations retain source and version.

- [ ] **Step 4: Verify tool contracts and composition**

Run: `go test ./backend/internal/aiassist/... ./backend/cmd/rarity-api -run 'Tool|AIComposition' -count=1`  
Expected: PASS.

- [ ] **Step 5: Commit initial tools**

```bash
git add backend/internal/aiassist/rtitools backend/cmd/rarity-api/main.go
git commit -m "feat: add permission-aware RTI AI tools"
```

### Task 5: Expose workspace APIs and global drawer

**Files:**
- Create: `backend/internal/httpapi/ai_workspace_routes.go`
- Create: `backend/internal/httpapi/ai_workspace_routes_test.go`
- Modify: `backend/internal/httpapi/router.go`
- Create: `frontend/src/features/ai/AIWorkspace.tsx`
- Create: `frontend/src/features/ai/AIWorkspace.test.tsx`
- Create: `frontend/src/features/ai/AIProposal.tsx`
- Modify: `frontend/src/design-system/components/navigation/AppShell.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/navigation.ts`

**Interfaces:**
- Adds conversation/message/proposal routes and a persistent `AIWorkspace` top-bar drawer.

- [ ] **Step 1: Write failing route and shell tests**

```tsx
it("keeps AI available while navigating and requires confirmation for writes", async () => {
	render(<TestApplication initialPath="/work" />);
	await user.click(screen.getByRole("button", { name: "Open AI workspace" }));
	await user.click(screen.getByRole("link", { name: "Organizations" }));
	expect(screen.getByRole("dialog", { name: "AI workspace" })).toBeVisible();
	expect(screen.getByRole("button", { name: "Confirm ticket update" })).toBeVisible();
});
```

- [ ] **Step 2: Verify red**

Run: `go test ./backend/internal/httpapi -run AIWorkspace -count=1 && npm --prefix frontend test -- --run AIWorkspace`  
Expected: Go or Vitest FAIL because routes/components are absent.

- [ ] **Step 3: Implement strict APIs and persistent drawer**

Use bounded request decoding, authenticated principals, active Client scope, durable job state, and safe errors. The drawer renders citations and proposal changes, does not close on route changes, and clears Client-bound context on confirmed Client switching.

- [ ] **Step 4: Run portable acceptance**

Run: `go test ./backend/... -count=1 && npm --prefix frontend test -- --run && npm --prefix frontend run build`  
Expected: PASS.

- [ ] **Step 5: Commit and update roadmap/docs tracker**

```bash
git add backend frontend docs/09-roadmap/implementation-plan.md docs-site
git commit -m "feat: deliver global permission-aware AI workspace"
```

