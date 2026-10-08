# Provider-Agnostic AI Runtime Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

**Status:** Source implementation, portable acceptance, and constrained demo
runtime acceptance complete; configured provider evaluation remains pending.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build GUI-managed Ollama and OpenAI-compatible provider connections, model profiles, MSP policy, durable generation jobs, and human-only AI recommendations.

**Architecture:** Extend `aiassist` with provider-neutral management, adapter, transport, and durable-job contracts. PostgreSQL stores encrypted provider credentials, model profiles, feature mappings, jobs, usage, audit, and outbox evidence; the API process composes leased workers and exposes strict `/api/v1` management and technician routes. React settings and technician components consume those routes without ever reading credentials.

**Tech Stack:** Go 1.24, `net/http`, PostgreSQL/pgx, goose migrations, AES-GCM `secrets.Provider`, React 19, TypeScript, Vite, Vitest/Testing Library.

## Global Constraints

- Initial adapters are exactly `ollama` and `openai_compatible`; arbitrary administrator-defined HTTP templates are excluded.
- Remote mode requires public HTTPS, rejects private/loopback/link-local destinations, refuses redirects, and pins public DNS results at dial time.
- Local mode permits HTTP/HTTPS and private/loopback/local DNS only after an administrator acknowledgment; redirects remain disabled and adapter paths remain fixed.
- Remote timeout defaults to 5 minutes; local timeout defaults to 15 minutes; the hard maximum is 60 minutes.
- Serialized request default ceiling is 1 MiB with a 5 MiB hard maximum.
- Raw structured response default ceiling is 5 MiB with a 10 MiB hard maximum.
- Provider credentials are encrypted, write-only, never returned, and never logged.
- Generation context and authorized candidates are loaded server-side within the active Client; the browser cannot supply trusted fields or candidate authorization.
- Recommendations remain `pending_human`; accepting or rejecting always records `applied: false` and `sent: false`.
- Preserve unrelated changes in `docs-site/app.js`, `docs/09-roadmap/implementation-plan.md`, `docs/07-ui-ux/technician-platform-page-specifications.md`, `docs/superpowers/plans/2026-07-28-runnable-foundation.md`, and `prototypes/`.

---

## File Structure

- `backend/internal/aiassist/provider.go`: provider/model/policy value objects and validation.
- `backend/internal/aiassist/management.go`: authorized provider, model, and policy commands.
- `backend/internal/aiassist/adapter.go`: provider-neutral discovery/generation interfaces and registry.
- `backend/internal/aiassist/transport.go`: bounded remote/local HTTP transports.
- `backend/internal/aiassist/ollama.go`: native Ollama adapter.
- `backend/internal/aiassist/openai_compatible.go`: OpenAI-compatible adapter.
- `backend/internal/aiassist/jobs.go`: submission, cancellation, retry, lease, and worker orchestration.
- `backend/internal/aiassist/service.go`: retain core recommendation validation and generation-record construction, adapted for worker use.
- `backend/internal/store/psa/ai_repository.go`: existing human decision persistence plus generation completion.
- `backend/internal/store/psa/ai_management_repository.go`: encrypted connection/model/policy persistence.
- `backend/internal/store/psa/ai_job_repository.go`: context loading, durable job leases, retries, cancellation, and atomic completion.
- `backend/internal/httpapi/ai_routes.go`: management, generation-job, and existing decision routes.
- `frontend/src/features/ai/`: API types/client, provider settings, policy settings, job status, and tests.

---

### Task 1: Add Provider, Model, Policy, and Job Schema

**Files:**
- Create: `backend/migrations/000049_provider_agnostic_ai_runtime.sql`
- Modify: `backend/migrations/ai_contract_test.go`

**Interfaces:**
- Produces PostgreSQL tables `ai_provider_connections`, `ai_model_profiles`, and `ai_generation_jobs`.
- Extends `ai_policies` with feature model mappings and explicit cost-governance fields.
- Makes unknown usage/cost nullable instead of storing misleading zeroes.

- [ ] **Step 1: Write the failing migration contract test**

```go
func TestProviderAgnosticAIRuntimeMigrationDefinesEncryptedDurableControlPlane(t *testing.T) {
	body, err := FS.ReadFile("000049_provider_agnostic_ai_runtime.sql")
	if err != nil {
		t.Fatalf("read provider AI migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"CREATE TABLE ai_provider_connections",
		"adapter_type text NOT NULL",
		"network_mode text NOT NULL",
		"credential_ciphertext bytea",
		"local_network_acknowledged_at timestamptz",
		"response_limit_bytes bigint NOT NULL DEFAULT 5242880",
		"CREATE TABLE ai_model_profiles",
		"CREATE TABLE ai_generation_jobs",
		"lease_until timestamptz",
		"UNIQUE (idempotency_key)",
		"ALTER TABLE ai_usage_records ALTER COLUMN cost_minor DROP NOT NULL",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("provider AI migration missing %q", fragment)
		}
	}
}
```

- [ ] **Step 2: Run the migration test and verify RED**

Run: `go test ./backend/migrations -run TestProviderAgnosticAIRuntimeMigration -count=1`

Expected: FAIL because migration 49 does not exist.

- [ ] **Step 3: Create migration 49 with explicit constraints**

```sql
CREATE TABLE ai_provider_connections (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  name text NOT NULL,
  adapter_type text NOT NULL CHECK (adapter_type IN ('ollama', 'openai_compatible')),
  network_mode text NOT NULL CHECK (network_mode IN ('local', 'remote')),
  base_url text NOT NULL,
  credential_version integer,
  credential_nonce bytea,
  credential_ciphertext bytea,
  enabled boolean NOT NULL DEFAULT false,
  timeout_seconds integer NOT NULL CHECK (timeout_seconds BETWEEN 1 AND 3600),
  request_limit_bytes bigint NOT NULL DEFAULT 1048576
    CHECK (request_limit_bytes BETWEEN 1024 AND 5242880),
  response_limit_bytes bigint NOT NULL DEFAULT 5242880
    CHECK (response_limit_bytes BETWEEN 1024 AND 10485760),
  disclosure_accepted_at timestamptz,
  disclosure_accepted_by uuid,
  local_network_acknowledged_at timestamptz,
  local_network_acknowledged_by uuid,
  health_state text NOT NULL DEFAULT 'pending'
    CHECK (health_state IN ('pending', 'healthy', 'degraded', 'failed', 'disabled')),
  last_tested_at timestamptz,
  last_succeeded_at timestamptz,
  last_error_code text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL,
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, name),
  CHECK (
    (credential_version IS NULL AND credential_nonce IS NULL AND credential_ciphertext IS NULL)
    OR
    (credential_version IS NOT NULL AND credential_nonce IS NOT NULL AND credential_ciphertext IS NOT NULL)
  ),
  CHECK (
    network_mode <> 'local'
    OR (local_network_acknowledged_at IS NOT NULL AND local_network_acknowledged_by IS NOT NULL)
  )
);
```

Add `ai_model_profiles` with connection/MSP composite foreign keys, supported feature array, context/output limits, enabled/version/discovery fields, and unique provider model IDs per connection. Add `ai_generation_jobs` with requester, Client, Work Record, feature, model profile, immutable relevant-input names, queued/running/completed/failed/cancelled state, attempt/max-attempt, lease, cancellation, safe-error, recommendation ID, idempotency key, and timestamps. Replace the original `ai_policies` enabled check and single `provider_connection_id` assumption with per-feature model profile IDs, `cost_limit_enabled`, and `allow_unmetered_unknown`; retain disabled-by-default behavior and validate that every configured default belongs to the policy MSP.

- [ ] **Step 4: Run migration contracts**

Run: `go test ./backend/migrations -count=1`

Expected: PASS.

- [ ] **Step 5: Commit schema**

```bash
git add backend/migrations/000049_provider_agnostic_ai_runtime.sql backend/migrations/ai_contract_test.go
git commit -m "feat: define provider-agnostic AI runtime"
```

---

### Task 2: Define Provider Configuration and Management Domain

**Files:**
- Create: `backend/internal/aiassist/provider.go`
- Create: `backend/internal/aiassist/provider_test.go`
- Create: `backend/internal/aiassist/management.go`
- Create: `backend/internal/aiassist/management_test.go`

**Interfaces:**
- Produces:

```go
type AdapterType string
const (
	AdapterOllama AdapterType = "ollama"
	AdapterOpenAICompatible AdapterType = "openai_compatible"
)

type NetworkMode string
const (
	NetworkLocal NetworkMode = "local"
	NetworkRemote NetworkMode = "remote"
)

type ProviderConnection struct {
	ID, MSPID, Name, BaseURL string
	Adapter AdapterType
	Network NetworkMode
	CredentialConfigured bool
	Enabled bool
	Timeout time.Duration
	RequestLimitBytes, ResponseLimitBytes int64
	LocalNetworkAcknowledgedAt *time.Time
	Health HealthState
	LastTestedAt, LastSucceededAt *time.Time
	LastErrorCode string
	Version int64
}

type ModelProfile struct {
	ID, MSPID, ConnectionID, ProviderModelID, DisplayName string
	SupportedFeatures []Feature
	ContextLimit, OutputLimit int64
	ZeroCost, Enabled bool
	Version int64
}

type DiscoveredModel struct {
	ProviderModelID, DisplayName string
	ContextLimit int64
}
```

- Produces `ManagementRepository` and `ManagementService` methods for create/list/update/replace credential, enable/disable, discovered-model reconciliation, model update, policy get/update, and connection health.
- `ManagementService` consumes a `ProviderOperations` boundary for connection test and model discovery; Task 5 supplies its adapter-registry implementation so provider I/O never enters the repository.
- Requires `ai.manage` at MSP scope.

- [ ] **Step 1: Write failing validation tests**

```go
func TestValidateConnectionSeparatesRemoteAndAcknowledgedLocalModes(t *testing.T) {
	remote := ProviderConnection{
		ID: "id", MSPID: "msp", Name: "Remote", Adapter: AdapterOpenAICompatible,
		Network: NetworkRemote, BaseURL: "https://models.example.test",
		Timeout: 5*time.Minute, RequestLimitBytes: 1<<20, ResponseLimitBytes: 5<<20,
	}
	if err := ValidateConnection(remote); err != nil {
		t.Fatalf("remote connection rejected: %v", err)
	}
	local := remote
	local.Adapter, local.Network, local.BaseURL = AdapterOllama, NetworkLocal, "http://127.0.0.1:11434"
	if err := ValidateConnection(local); !errors.Is(err, ErrLocalAcknowledgementRequired) {
		t.Fatalf("unacknowledged local error=%v", err)
	}
}
```

Also test the exact timeout/request/response hard maxima, remote HTTPS requirement, adapter enum, optional credential handling for self-hosted providers, credential-configured metadata, and secret-free JSON serialization. Connection testing—not adapter name alone—determines whether a particular endpoint requires authentication.

- [ ] **Step 2: Verify RED**

Run: `go test ./backend/internal/aiassist -run 'TestValidateConnection|TestManagementService' -count=1`

Expected: FAIL because provider management types do not exist.

- [ ] **Step 3: Implement value validation and authorized commands**

```go
func ValidateConnection(connection ProviderConnection) error {
	if strings.TrimSpace(connection.ID) == "" ||
		strings.TrimSpace(connection.MSPID) == "" ||
		strings.TrimSpace(connection.Name) == "" ||
		connection.Timeout < time.Second || connection.Timeout > time.Hour ||
		connection.RequestLimitBytes < 1024 || connection.RequestLimitBytes > 5<<20 ||
		connection.ResponseLimitBytes < 1024 || connection.ResponseLimitBytes > 10<<20 {
		return ErrInvalidProviderConfiguration
	}
	// Adapter/network-specific URL and acknowledgment checks follow.
	return nil
}
```

Management commands accept plaintext credentials only in create/replace calls, seal through a repository mutation boundary, and return `ProviderConnection` without credential bytes. Every mutation contains optimistic version, reason where required, audit, and outbox records.

```go
type ProviderOperations interface {
	Test(context.Context, ProviderConnection) error
	Discover(context.Context, ProviderConnection) ([]DiscoveredModel, error)
}
```

- [ ] **Step 4: Run domain tests**

Run: `go test ./backend/internal/aiassist -count=1`

Expected: PASS.

- [ ] **Step 5: Commit domain**

```bash
git add backend/internal/aiassist/provider.go backend/internal/aiassist/provider_test.go backend/internal/aiassist/management.go backend/internal/aiassist/management_test.go
git commit -m "feat: govern AI provider settings"
```

---

### Task 3: Persist Encrypted Connections, Models, and Policy

**Files:**
- Create: `backend/internal/store/psa/ai_management_repository.go`
- Create: `backend/internal/store/psa/ai_management_repository_test.go`
- Modify: `backend/internal/store/psa/pgx.go`

**Interfaces:**
- Consumes `aiassist.ManagementRepository`, `secrets.Provider`, `mutation.AuditRecord`, and `mutation.EventRecord`.
- Produces `NewAIManagementRepository(database, secrets.Provider, func() string)`.
- Uses encryption purpose `ai.provider.<connection-id>` so ciphertext cannot be opened for another connection.

- [ ] **Step 1: Write failing repository tests**

```go
func TestAIManagementRepositorySealsCredentialAndWritesFactsAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	sealer := &recordingSecretProvider{}
	repository := NewAIManagementRepository(&fakeSalesDB{tx: tx}, sealer, func() string { return "id" })
	err := repository.CreateConnection(context.Background(), aiassist.ConnectionMutation{
		Connection: validProviderConnection(),
		PlaintextCredential: []byte("synthetic-api-key"),
		Audit: validAudit(), Event: validEvent(),
	})
	if err != nil {
		t.Fatalf("CreateConnection() error=%v", err)
	}
	if sealer.purpose != "ai.provider.connection-id" {
		t.Fatalf("purpose=%q", sealer.purpose)
	}
	assertQueryOrder(t, tx.queries,
		"INSERT INTO ai_provider_connections",
		"INSERT INTO audit_ledger", "INSERT INTO event_outbox",
	)
	for _, args := range tx.args {
		if fmt.Sprint(args) == "synthetic-api-key" {
			t.Fatal("plaintext credential reached SQL")
		}
	}
}
```

Add tests for MSP-scoped enumeration-safe lookup, version conflicts, replacement encryption, credential clearing rules, local acknowledgment persistence, model reconciliation without automatic enablement, policy model ownership, and audit/outbox atomicity.

- [ ] **Step 2: Verify RED**

Run: `go test ./backend/internal/store/psa -run TestAIManagementRepository -count=1`

Expected: FAIL because repository does not exist.

- [ ] **Step 3: Implement repository transactions**

```go
sealed, err := r.secrets.Seal(
	ctx,
	"ai.provider."+accepted.Connection.ID,
	accepted.PlaintextCredential,
)
if err != nil {
	return err
}
```

Store only `sealed.Version`, `sealed.Nonce`, and `sealed.Ciphertext`. Reads return `credential_configured` derived from non-null sealed columns. Updates require `WHERE id=$1 AND msp_id=$2 AND version=$3`. Policy writes validate all selected models through joins to enabled profiles and enabled connections in the same MSP before updating.

- [ ] **Step 4: Add pool constructor and run tests**

```go
func NewAIManagementRepositoryFromPool(
	pool *pgxpool.Pool,
	provider secrets.Provider,
	newID func() string,
) *AIManagementRepository {
	return NewAIManagementRepository(&poolDatabase{pool: pool}, provider, newID)
}
```

Run: `go test ./backend/internal/store/psa ./backend/internal/aiassist -count=1`

Expected: PASS.

- [ ] **Step 5: Commit persistence**

```bash
git add backend/internal/store/psa/ai_management_repository.go backend/internal/store/psa/ai_management_repository_test.go backend/internal/store/psa/pgx.go
git commit -m "feat: persist protected AI settings"
```

---

### Task 4: Build Bounded Local and Remote Provider Transports

**Files:**
- Create: `backend/internal/aiassist/transport.go`
- Create: `backend/internal/aiassist/transport_test.go`

**Interfaces:**
- Produces:

```go
type HTTPRequest struct {
	Method, Path string
	Headers http.Header
	Body []byte
}

type HTTPResponse struct {
	StatusCode int
	Header http.Header
	Body []byte
}

type Transport interface {
	Do(context.Context, ProviderConnection, HTTPRequest) (HTTPResponse, error)
}

func NewRemoteTransport(resolver webhooks.Resolver) Transport
func NewLocalTransport() Transport
func NewModeTransport(remote Transport, local Transport) Transport
```

- [ ] **Step 1: Write failing transport security tests**

```go
func TestRemoteTransportRejectsRedirectAndPrivateDialAddress(t *testing.T) {
	transport := newTestRemoteTransport(
		resolverReturning(net.ParseIP("127.0.0.1")),
	)
	_, err := transport.Do(context.Background(), remoteConnection(), HTTPRequest{
		Method: http.MethodPost, Path: "/v1/chat/completions", Body: []byte(`{}`),
	})
	if !errors.Is(err, ErrUnsafeProviderEndpoint) {
		t.Fatalf("private destination error=%v", err)
	}
}
```

Add tests for remote HTTP rejection, redirect refusal, off-origin path protection, DNS rebinding/pinned dial, local private/loopback acceptance, local redirect refusal, cancellation, timeout, request ceiling before send, response streaming cutoff at 5 MiB default, and safe non-2xx errors without body leakage.

- [ ] **Step 2: Verify RED**

Run: `go test ./backend/internal/aiassist -run TestRemoteTransport -count=1`

Expected: FAIL because transports do not exist.

- [ ] **Step 3: Implement transports**

Use `webhooks.ValidateDestination` and the existing public-address resolver/dialer pattern for remote mode. Configure both clients with:

```go
CheckRedirect: func(*http.Request, []*http.Request) error {
	return ErrProviderRedirectRefused
}
```

Wrap request bodies with exact configured-size validation. Read responses through `io.LimitReader(response.Body, limit+1)` and return `ErrProviderResponseTooLarge` when the extra byte exists. `NewModeTransport` selects only from the validated stored `NetworkMode`, allowing either adapter to use local or remote mode. Do not include response bodies or credentials in errors.

- [ ] **Step 4: Run transport and existing webhook security tests**

Run: `go test ./backend/internal/aiassist ./backend/internal/webhooks -count=1`

Expected: PASS.

- [ ] **Step 5: Commit transports**

```bash
git add backend/internal/aiassist/transport.go backend/internal/aiassist/transport_test.go
git commit -m "feat: bound AI provider transport"
```

---

### Task 5: Implement Adapter Registry, Ollama, and OpenAI-Compatible Adapters

**Files:**
- Create: `backend/internal/aiassist/adapter.go`
- Create: `backend/internal/aiassist/adapter_contract_test.go`
- Create: `backend/internal/aiassist/ollama.go`
- Create: `backend/internal/aiassist/ollama_test.go`
- Create: `backend/internal/aiassist/openai_compatible.go`
- Create: `backend/internal/aiassist/openai_compatible_test.go`

**Interfaces:**
- Consumes `ProviderConnection`, `ModelProfile`, and `DiscoveredModel` from Task 2.
- Produces:

```go
type Usage struct {
	InputUnits, OutputUnits *int64
	CostMinor *int64
}

type GenerationResult struct {
	Text string
	Confidence *float64
	CandidateIDs []string
	Usage Usage
}

type Adapter interface {
	Type() AdapterType
	Discover(context.Context, ProviderConnection, []byte) ([]DiscoveredModel, error)
	Generate(context.Context, ProviderConnection, ModelProfile, ProviderRequest, []byte) (GenerationResult, error)
}

type AdapterRegistry struct { /* immutable adapter map */ }
```

- [ ] **Step 1: Write a shared failing adapter contract**

```go
func runAdapterContract(
	t *testing.T,
	adapter Adapter,
	connection ProviderConnection,
	model ModelProfile,
	fixture *providerFixture,
) {
	t.Helper()
	models, err := adapter.Discover(context.Background(), connection, fixture.credential)
	if err != nil || len(models) == 0 || models[0].ProviderModelID == "" {
		t.Fatalf("Discover() models=%+v error=%v", models, err)
	}
	result, err := adapter.Generate(
		context.Background(), connection, model, providerRequestFixture(),
		fixture.credential,
	)
	if err != nil || strings.TrimSpace(result.Text) == "" {
		t.Fatalf("Generate() result=%+v error=%v", result, err)
	}
}
```

Run it against native Ollama fixtures (`GET /api/tags`, `POST /api/chat`) and OpenAI-compatible fixtures (`GET /v1/models`, `POST /v1/chat/completions`). Assert credentials are sent only by the adapter that requires them, `stream:false` is fixed, usage is optional, candidates are structured, malformed JSON fails, and provider error bodies never escape.

- [ ] **Step 2: Verify RED**

Run: `go test ./backend/internal/aiassist -run 'Test(Ollama|OpenAICompatible|Adapter)' -count=1`

Expected: FAIL because adapters do not exist.

- [ ] **Step 3: Implement registry and adapters**

```go
func NewAdapterRegistry(adapters ...Adapter) (*AdapterRegistry, error) {
	values := make(map[AdapterType]Adapter, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil || values[adapter.Type()] != nil {
			return nil, ErrInvalidProviderConfiguration
		}
		values[adapter.Type()] = adapter
	}
	return &AdapterRegistry{values: values}, nil
}
```

Adapters build prompts from `ProviderRequest.Fields`, ask for one bounded structured recommendation, and parse only documented adapter-owned fields. They do not accept administrator-defined paths, headers, templates, or response selectors.

- [ ] **Step 4: Run adapter and race tests**

Run: `go test -race ./backend/internal/aiassist -count=1`

Expected: PASS.

- [ ] **Step 5: Commit adapters**

```bash
git add backend/internal/aiassist/adapter.go backend/internal/aiassist/adapter_contract_test.go backend/internal/aiassist/ollama.go backend/internal/aiassist/ollama_test.go backend/internal/aiassist/openai_compatible.go backend/internal/aiassist/openai_compatible_test.go
git commit -m "feat: adapt Ollama and compatible models"
```

---

### Task 6: Implement Durable Submission, Context Loading, and Worker Runtime

**Files:**
- Create: `backend/internal/aiassist/jobs.go`
- Create: `backend/internal/aiassist/jobs_test.go`
- Modify: `backend/internal/aiassist/service.go`
- Modify: `backend/internal/aiassist/service_test.go`
- Create: `backend/internal/store/psa/ai_job_repository.go`
- Create: `backend/internal/store/psa/ai_job_repository_test.go`
- Modify: `backend/internal/store/psa/ai_repository.go`
- Modify: `backend/internal/store/psa/ai_repository_test.go`

**Interfaces:**
- Produces:

```go
type SubmitCommand struct {
	Principal authorization.Principal
	WorkRecordID string
	Feature Feature
	IdempotencyKey string
}

type JobService struct {
	store JobStore
	now func() time.Time
	newID func() string
}

type JobWorker struct {
	queue JobQueue
	registry *AdapterRegistry
	now func() time.Time
}

func (s *JobService) Submit(context.Context, SubmitCommand) (GenerationJob, error)
func (s *JobService) Get(context.Context, authorization.Principal, string) (GenerationJob, error)
func (s *JobService) Cancel(context.Context, CancelCommand) (GenerationJob, error)
func (s *JobService) Retry(context.Context, RetryCommand) (GenerationJob, error)
func (w *JobWorker) RunOnce(context.Context, int) (WorkerResult, error)
```

- [ ] **Step 1: Write failing job service and worker tests**

```go
func TestJobSubmissionLoadsServerOwnedContextAndReturnsDurableJob(t *testing.T) {
	store := &jobStoreStub{
		policy: enabledPolicyFixture(),
		context: GenerationContext{
			Fields: []ContextField{{Name: "title", Value: "VPN down", Classification: ContextStandard}},
			AuthorizedCandidateIDs: []string{"candidate-in-client"},
		},
	}
	service := NewJobService(store, fixedNow, sequenceIDs("job-id", "audit-id", "event-id", "correlation-id"))
	job, err := service.Submit(context.Background(), SubmitCommand{
		Principal: aiPrincipal(), WorkRecordID: "work-id",
		Feature: FeatureSummary, IdempotencyKey: "request-1",
	})
	if err != nil || job.State != JobQueued ||
		!reflect.DeepEqual(job.RelevantInputs, []string{"title"}) {
		t.Fatalf("Submit() job=%+v error=%v", job, err)
	}
}
```

Add tests for cross-Client denial, disabled policy/model/connection, browser inability to supply context, idempotent replay, queued/running cancellation, eligible transient retry, non-retryable unsafe/auth/invalid/oversize failures, lease recovery, context reauthorization at execution, cancellation propagation, and one recommendation per job.

- [ ] **Step 2: Verify RED**

Run: `go test ./backend/internal/aiassist ./backend/internal/store/psa -run 'Test(Job|AIJobRepository)' -count=1`

Expected: FAIL because durable jobs do not exist.

- [ ] **Step 3: Implement job domain and PostgreSQL queue**

Submission reads the Work Record and policy in the transaction that inserts the job, storing only field names in the job row. Worker claim uses `FOR UPDATE SKIP LOCKED`, changes state to running, increments attempt, and assigns a lease. Credential opening uses purpose `ai.provider.<connection-id>` immediately before the adapter call.

```sql
SELECT id
FROM ai_generation_jobs
WHERE state = 'queued'
   OR (state = 'running' AND lease_until < $1)
ORDER BY created_at, id
FOR UPDATE SKIP LOCKED
LIMIT $2
```

Completion transaction inserts recommendation and nullable usage, updates the job with `recommendation_id`, writes audit/outbox, and guards `state='running' AND lease_token=$token`. Cancellation checks before the call and again before completion so cancelled jobs cannot publish recommendations.

- [ ] **Step 4: Adapt recommendation generation validation**

Keep `Service.Generate` usable in unit tests but factor response validation and `RecommendationRecord` construction into a provider-neutral function consumed by the worker:

```go
func BuildRecommendationRecord(
	command RecommendationCommand,
	result GenerationResult,
	now time.Time,
	ids RecommendationIDs,
) (RecommendationRecord, error)
```

Require returned candidates to be a subset of repository-loaded authorized IDs. Store unknown confidence, units, or cost as nullable values.

- [ ] **Step 5: Run job, repository, and race tests**

Run: `go test -race ./backend/internal/aiassist ./backend/internal/store/psa -count=1`

Expected: PASS.

- [ ] **Step 6: Commit runtime**

```bash
git add backend/internal/aiassist/jobs.go backend/internal/aiassist/jobs_test.go backend/internal/aiassist/service.go backend/internal/aiassist/service_test.go backend/internal/store/psa/ai_job_repository.go backend/internal/store/psa/ai_job_repository_test.go backend/internal/store/psa/ai_repository.go backend/internal/store/psa/ai_repository_test.go
git commit -m "feat: run durable AI generation jobs"
```

---

### Task 7: Expose Strict Management and Generation APIs

**Files:**
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/internal/httpapi/psa_dto.go`
- Modify: `backend/internal/httpapi/ai_routes.go`
- Create: `backend/internal/httpapi/ai_routes_test.go`

**Interfaces:**
- Consumes `aiassist.ManagementService` and `aiassist.JobService`.
- Produces routes:
  - `GET|POST /api/v1/ai/providers`
  - `PATCH /api/v1/ai/providers/{id}`
  - `POST /api/v1/ai/providers/{id}/credential`
  - `POST /api/v1/ai/providers/{id}/test`
  - `POST /api/v1/ai/providers/{id}/discover-models`
  - `GET|PATCH /api/v1/ai/providers/{id}/models`
  - `GET|PATCH /api/v1/ai/policy`
  - `POST /api/v1/work-records/{id}/ai/jobs`
  - `GET /api/v1/ai/jobs/{id}`
  - `POST /api/v1/ai/jobs/{id}/cancel`
  - `POST /api/v1/ai/jobs/{id}/retry`
  - existing `POST /api/v1/ai/recommendations/{id}/decide`

- [ ] **Step 1: Write failing HTTP mapping and secret-response tests**

```go
func TestCreateAIProviderAcceptsWriteOnlyCredential(t *testing.T) {
	actions := &aiManagementActions{}
	handler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: actions})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ai/providers",
		strings.NewReader(`{
			"name":"Local Ollama","adapter":"ollama","network_mode":"local",
			"base_url":"http://127.0.0.1:11434","credential":"synthetic",
			"local_network_acknowledged":true
		}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "synthetic") ||
		strings.Contains(response.Body.String(), "credential_ciphertext") {
		t.Fatal("provider response exposed credential material")
	}
}
```

Add tests for strict unknown-field rejection, 1 MiB request cap, auth, MSP/Client mapping, optimistic versions/ETags, reason requirements, `202` generation response, idempotency key requirement, job enumeration safety, cancellation/retry mapping, and human-only decision response.

- [ ] **Step 2: Verify RED**

Run: `go test ./backend/internal/httpapi -run 'Test.*AI' -count=1`

Expected: FAIL because dependencies and routes do not exist.

- [ ] **Step 3: Implement DTOs, interfaces, routes, and error mapping**

```go
type AIManagementActions interface {
	CreateConnection(context.Context, aiassist.CreateConnectionCommand) (aiassist.ProviderConnection, error)
	ListConnections(context.Context, aiassist.ListConnectionsCommand) ([]aiassist.ProviderConnection, error)
	UpdateConnection(context.Context, aiassist.UpdateConnectionCommand) (aiassist.ProviderConnection, error)
	ReplaceCredential(context.Context, aiassist.ReplaceCredentialCommand) (aiassist.ProviderConnection, error)
	TestConnection(context.Context, aiassist.TestConnectionCommand) (aiassist.ProviderConnection, error)
	DiscoverModels(context.Context, aiassist.DiscoverModelsCommand) ([]aiassist.ModelProfile, error)
	UpdateModels(context.Context, aiassist.UpdateModelsCommand) ([]aiassist.ModelProfile, error)
	GetPolicy(context.Context, aiassist.GetPolicyCommand) (aiassist.Policy, error)
	UpdatePolicy(context.Context, aiassist.UpdatePolicyCommand) (aiassist.Policy, error)
}
```

Never encode plaintext or sealed credentials. Return stable `validation_failed`, `version_conflict`, `forbidden`, `not_found`, `provider_unavailable`, and `job_not_retryable` errors without raw provider details.

- [ ] **Step 4: Run HTTP tests**

Run: `go test ./backend/internal/httpapi -count=1`

Expected: PASS.

- [ ] **Step 5: Commit APIs**

```bash
git add backend/internal/httpapi/router.go backend/internal/httpapi/psa_dto.go backend/internal/httpapi/ai_routes.go backend/internal/httpapi/ai_routes_test.go
git commit -m "feat: expose AI provider and job APIs"
```

---

### Task 8: Compose Protected AI Services and Worker

**Files:**
- Modify: `backend/internal/store/psa/pgx.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/cmd/rarity-api/main_test.go`

**Interfaces:**
- Consumes the runtime-wide `secrets.Provider`, PostgreSQL pool, adapters, transports, and HTTP dependencies.
- Produces `buildAIRuntime(pool, secretProvider) (*aiassist.JobWorker, *aiassist.ManagementService, *aiassist.JobService, error)`.

- [ ] **Step 1: Write failing composition tests**

```go
func TestBuildAIRuntimeComposesProviderNeutralProtectedRuntime(t *testing.T) {
	provider, err := secrets.NewLocalProvider(bytes.Repeat([]byte{1}, 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	worker, management, jobs, err := buildAIRuntime(nil, provider)
	if err != nil || worker == nil || management == nil || jobs == nil {
		t.Fatalf("runtime worker=%v management=%v jobs=%v error=%v",
			worker, management, jobs, err)
	}
}
```

Extend `TestRuntimeDependenciesComposePSAPersistence` to require AI management/jobs and retain AI decisions.

- [ ] **Step 2: Verify RED**

Run: `go test ./backend/cmd/rarity-api -run 'TestBuildAIRuntime|TestRuntimeDependencies' -count=1`

Expected: FAIL because AI runtime composition does not exist.

- [ ] **Step 3: Compose runtime and worker loop**

```go
transport := aiassist.NewModeTransport(
	aiassist.NewRemoteTransport(nil),
	aiassist.NewLocalTransport(),
)
registry, err := aiassist.NewAdapterRegistry(
	aiassist.NewOllamaAdapter(transport),
	aiassist.NewOpenAICompatibleAdapter(transport),
)
```

Select local or remote transport per connection inside the adapter request boundary; do not let a stored adapter bypass its configured network mode. Start a five-second worker loop with a bounded batch and safe aggregate logs:

```go
go runAIJobWorker(workerContext, logger, aiWorker)
```

Change `buildDependencies` to receive the already-built AI management and job services alongside the existing Graph notification override. Do not construct an AI repository without the runtime `secrets.Provider`; both HTTP management and the worker must share the same protected-secret boundary.

Logs may include claimed/completed/failed/cancelled counts, never job context or provider bodies.

- [ ] **Step 4: Run main and backend tests**

Run: `go test ./backend/cmd/rarity-api ./backend/... -count=1`

Expected: PASS.

- [ ] **Step 5: Commit composition**

```bash
git add backend/internal/store/psa/pgx.go backend/cmd/rarity-api/main.go backend/cmd/rarity-api/main_test.go
git commit -m "feat: compose AI provider runtime"
```

---

### Task 9: Build AI Provider and Policy Settings GUI

**Files:**
- Create: `frontend/src/features/ai/types.ts`
- Create: `frontend/src/features/ai/api.ts`
- Create: `frontend/src/features/ai/AISettingsPage.tsx`
- Create: `frontend/src/features/ai/AISettingsPage.test.tsx`
- Create: `frontend/src/features/ai/ai.css`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`

**Interfaces:**
- Produces `AISettingsPage` consuming an injectable `AISettingsAPI`.
- Adds `#/ai-settings` navigation.
- Uses write-only `credential?: string` only in create/replace request types; response types expose `credentialConfigured: boolean`.

- [ ] **Step 1: Write failing accessible workflow tests**

```tsx
it("creates a local Ollama connection without revealing its credential", async () => {
	const api = aiSettingsAPIStub();
	render(<AISettingsPage api={api} />);
	await user.click(screen.getByRole("button", { name: "Add provider" }));
	await user.selectOptions(screen.getByLabelText("Provider adapter"), "ollama");
	await user.selectOptions(screen.getByLabelText("Network mode"), "local");
	await user.type(screen.getByLabelText("Base URL"), "http://127.0.0.1:11434");
	await user.click(screen.getByLabelText(/allow private-network access/i));
	await user.click(screen.getByRole("button", { name: "Save provider" }));
	expect(api.createConnection).toHaveBeenCalledWith(
		expect.objectContaining({ adapter: "ollama", networkMode: "local" }),
	);
	expect(screen.queryByDisplayValue(/api.key/i)).not.toBeInTheDocument();
});
```

Add tests for remote/local explanatory copy, defaults (5/15 minutes and 5 MiB), maximum validation, write-only credential replacement, test/discover flow, explicit model enablement, per-feature policy mapping, optimistic-conflict value preservation, disabled-model exclusion, keyboard operation, labels, and status announcements.

- [ ] **Step 2: Verify RED**

Run: `npm --prefix frontend test -- --run AISettingsPage`

Expected: FAIL because the feature does not exist.

- [ ] **Step 3: Implement types and API client**

```ts
export type ProviderConnection = {
  id: string;
  name: string;
  adapter: "ollama" | "openai_compatible";
  networkMode: "local" | "remote";
  baseUrl: string;
  credentialConfigured: boolean;
  enabled: boolean;
  timeoutSeconds: number;
  requestLimitBytes: number;
  responseLimitBytes: number;
  healthState: "pending" | "healthy" | "degraded" | "failed" | "disabled";
  version: number;
};
```

Use strict response decoding helpers and `If-Match`/expected-version conventions already used by feature API modules. Do not cache submitted credentials in component state after a successful request.

- [ ] **Step 4: Implement settings page and navigation**

Use a connection list plus one focused edit form, a model table with checkboxes, and a policy mapping section. Show “Local network does not necessarily mean this machine.” Present credential status as configured/not configured. Announce save/test/discovery results through `role="status"` and focus validation errors.

- [ ] **Step 5: Run frontend tests and production build**

Run: `npm --prefix frontend test -- --run && npm --prefix frontend run build`

Expected: PASS.

- [ ] **Step 6: Commit GUI settings**

```bash
git add frontend/src/features/ai frontend/src/App.tsx frontend/src/App.test.tsx
git commit -m "feat: configure AI providers in GUI"
```

---

### Task 10: Add Technician Generation Progress and Review UI

**Files:**
- Create: `frontend/src/features/ai/AIAssistPanel.tsx`
- Create: `frontend/src/features/ai/AIAssistPanel.test.tsx`
- Modify: `frontend/src/features/ai/api.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`

**Interfaces:**
- Consumes job submit/get/cancel/retry and recommendation decide routes.
- Produces a reusable `AIAssistPanel` receiving `workRecordID`, supported features, and `AIAssistAPI`.
- Adds a demonstrable technician `#/ai-assist` workspace without coupling Work Record AI to Sales or Project components; the future Work Record page consumes the same panel unchanged.

- [ ] **Step 1: Write failing durable-progress tests**

```tsx
it("keeps a slow local generation visible and cancellable", async () => {
	const api = aiAssistAPIStub({
		submitResult: { id: "job", state: "queued" },
		getResults: [
			{ id: "job", state: "running" },
			{ id: "job", state: "running" },
		],
	});
	render(<AIAssistPanel workRecordID="work" api={api} />);
	await user.click(screen.getByRole("button", { name: "Generate summary" }));
	expect(await screen.findByText("Generating summary…")).toBeVisible();
	await user.click(screen.getByRole("button", { name: "Cancel generation" }));
	expect(api.cancel).toHaveBeenCalledWith("job");
});
```

Add tests for refresh-safe job reload, safe failure/retry eligibility, non-retryable errors, field-name disclosure, pending-human label, accept/reject reason, and absence of autonomous apply/send actions.

- [ ] **Step 2: Verify RED**

Run: `npm --prefix frontend test -- --run AIAssistPanel`

Expected: FAIL because component does not exist.

- [ ] **Step 3: Implement progress and review**

Poll running jobs at a bounded interval while mounted, abort polls on unmount, and expose a manual refresh fallback. Store only job identity in browser session state. Render recommendation text as plain text, candidate links only from returned authorized identities, and accept/reject forms with required reasons.

- [ ] **Step 4: Run accessibility-oriented frontend tests**

Run: `npm --prefix frontend test -- --run && npm --prefix frontend run build`

Expected: PASS.

- [ ] **Step 5: Commit technician UI**

```bash
git add frontend/src/features/ai/AIAssistPanel.tsx frontend/src/features/ai/AIAssistPanel.test.tsx frontend/src/features/ai/api.ts frontend/src/App.tsx frontend/src/App.test.tsx
git commit -m "feat: review durable AI recommendations"
```

---

### Task 11: Document Contracts and Run Full Acceptance

**Files:**
- Modify: `docs/01-architecture/08-ai-platform.md`
- Modify: `docs/04-api/integration-automation-ai-contracts.md`
- Modify: `docs/04-api/rest-api.md`
- Modify: `docs/06-development/integration-automation-ai-acceptance.md`
- Modify: `scripts/validate-integration-automation-docs.mjs`

**Interfaces:**
- Documents exact management/job routes, capability boundaries, local/remote protections, encrypted write-only credentials, adapter list, durable states, limits, and environment gates.
- Updates the documentation validator to require those contracts.

- [ ] **Step 1: Extend validator first**

```js
const required = [
  "ollama",
  "openai_compatible",
  "5 MiB",
  "60 minutes",
  "POST /api/v1/ai/providers",
  "POST /api/v1/work-records/{id}/ai/jobs",
  "pending_human",
  "applied: false",
  "sent: false",
];
```

- [ ] **Step 2: Verify validator RED**

Run: `node scripts/validate-integration-automation-docs.mjs`

Expected: FAIL with missing provider runtime contract fragments.

- [ ] **Step 3: Update source-of-truth documents**

State that provider-neutral source composition is complete while live provider and PostgreSQL acceptance remains pending. Document that GUI credentials are encrypted, not environment-key configuration; Ollama may use acknowledged local mode; remote endpoints remain public HTTPS only; and 5 MiB is the raw response default rather than a token limit.

- [ ] **Step 4: Run the full portable gate**

```bash
go mod verify
go test -race ./backend/internal/aiassist ./backend/internal/httpapi ./backend/internal/store/psa
go test ./backend/... ./tests/...
go vet ./backend/... ./tests/...
npm --prefix frontend test -- --run
npm --prefix frontend run build
node scripts/validate-integration-automation-docs.mjs
git diff --check
```

Expected: all portable checks PASS.

- [ ] **Step 5: Check environment gates without exposing secrets**

```bash
if [[ -n "${TEST_DATABASE_URL:-}" ]]; then
  go test ./tests/integration -count=1
else
  echo "TEST_DATABASE_URL unavailable; PostgreSQL acceptance remains pending"
fi
```

If a synthetic Ollama or hosted provider is configured through the GUI, prove discovery, slow generation, cancellation, usage evidence, and Client isolation. Otherwise record provider acceptance as pending; never substitute fixture tests for live acceptance.

- [ ] **Step 6: Commit contracts**

```bash
git add docs/01-architecture/08-ai-platform.md docs/04-api/integration-automation-ai-contracts.md docs/04-api/rest-api.md docs/06-development/integration-automation-ai-acceptance.md scripts/validate-integration-automation-docs.mjs
git commit -m "docs: complete provider AI runtime contract"
```

---

## Completion Audit

- [x] Every design requirement maps to one or more tasks above.
- [x] Provider adapters remain code-owned and provider-neutral orchestration has no vendor branch.
- [x] Remote and local network modes have distinct tested security boundaries.
- [x] Credential plaintext appears only in request memory and protected-secret sealing/opening calls.
- [x] Request, response, timeout, cancellation, and lease limits are enforced in code and tests.
- [x] Context and candidates are loaded server-side within the active Client.
- [x] Job completion, recommendation, nullable usage/cost, audit, and outbox commit atomically.
- [x] GUI covers provider, credential, discovery, model, policy, job, cancellation, retry, and human decision workflows.
- [x] No AI action applies data, sends a message, or starts automation.
- [x] Portable checks pass and environment-dependent PostgreSQL/provider evidence is reported honestly.
