# Guided Setup Center Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the completed-installation JSON editor with guided intake, object-storage, and backup/PITR workflows whose readiness comes from authoritative configuration and live evidence.

**Architecture:** Extend the setup domain with typed, secret-free readiness contracts and a small verification-evidence repository. Intake readiness is composed from existing connection records, object storage is verified through a bounded probe against the already configured runtime adapter, and backup/PITR is verified from short-lived HMAC-signed CLI evidence. The React page becomes an overview plus three focused panels and never serializes arbitrary maps.

**Tech Stack:** Go 1.24, PostgreSQL/pgx, MinIO S3 adapter, React 19, TypeScript, Vitest/Testing Library, Docker Compose.

## Global Constraints

- No raw JSON editors or secret-reference URI syntax appear in the completed-installation Setup Center.
- The web process must not rewrite host environment files, Compose definitions, PostgreSQL configuration, or pgBackRest configuration.
- Provider secrets are write-only and are never returned, logged, audited, or stored in verification evidence.
- A saved value alone never produces the `verified` state.
- Every mutation requires `organization.manage`, CSRF protection, an audit reason, optimistic concurrency, an audit record, and an outbox event.
- Storage probes use one temporary object, always attempt cleanup, and return only safe step-specific error codes.
- Backup evidence is installation-bound, HMAC-signed, expires after 15 minutes, and rejects replay.
- Preserve compatibility for existing bootstrap rows while removing arbitrary configuration maps from the browser workflow.

---

### Task 1: Typed readiness and verification evidence

**Files:**
- Create: `backend/migrations/000075_setup_verification_evidence.sql`
- Create: `backend/migrations/setup_verification_evidence_contract_test.go`
- Modify: `backend/internal/setup/service.go`
- Modify: `backend/internal/setup/postgres.go`
- Modify: `backend/internal/setup/service_test.go`
- Modify: `backend/internal/setup/postgres_integration_test.go`

**Interfaces:**
- Produces:
  - `type ReadinessState string` with `not_started`, `action_required`, `ready_to_verify`, `verified`, and `attention`.
  - `type VerificationResult struct { Section, State, SafeCode string; CheckedAt, ValidUntil time.Time; Details map[string]string }`.
  - `Repository.Verification(ctx, mspID, section string) (VerificationResult, error)`.
  - `Repository.RecordVerification(ctx, VerificationMutation) error`.
  - Typed `IntakeStatus`, `ObjectStorageStatus`, and `BackupStatus` fields on `CenterStatus`.

- [ ] **Step 1: Add the migration contract test**

Create a migration test that requires a table named
`setup_verification_evidence` with `(msp_id, section)` uniqueness, a constrained
section (`object_storage` or `backups`), safe result fields, evidence hash,
expiry, consumed nonce, version, audit identity, and no credential or raw-output
columns.

- [ ] **Step 2: Run the migration contract test and confirm failure**

Run:

```bash
go test ./backend/migrations -run TestSetupVerificationEvidenceContract -count=1
```

Expected: failure because migration `000075` does not exist.

- [ ] **Step 3: Add migration 75**

Create `setup_verification_evidence` with:

```sql
CREATE TABLE setup_verification_evidence (
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  section text NOT NULL CHECK (section IN ('object_storage','backups')),
  state text NOT NULL CHECK (state IN ('ready_to_verify','verified','attention')),
  safe_code text NOT NULL,
  checked_at timestamptz NOT NULL,
  valid_until timestamptz NOT NULL,
  details jsonb NOT NULL DEFAULT '{}'::jsonb,
  evidence_hash bytea,
  consumed_nonce text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  updated_by uuid NOT NULL,
  PRIMARY KEY (msp_id, section),
  CHECK (jsonb_typeof(details) = 'object')
);
CREATE UNIQUE INDEX setup_verification_evidence_nonce_idx
  ON setup_verification_evidence (msp_id, consumed_nonce)
  WHERE consumed_nonce IS NOT NULL;
```

Include a reversible Goose down migration.

- [ ] **Step 4: Add typed setup-domain tests**

Tests must assert that `CenterStatus` contains typed, secret-free fields, expired
evidence maps to `attention`, missing evidence maps to `ready_to_verify` when
effective configuration exists, and no existing non-empty bootstrap map can
produce `verified`.

- [ ] **Step 5: Implement the typed contracts and repository methods**

Add:

```go
type IntakeStatus struct {
    GraphConfigured      bool `json:"graph_configured"`
    APIKeyConfigured     bool `json:"api_key_configured"`
    ForwardingConfigured bool `json:"forwarding_configured"`
}

type ObjectStorageStatus struct {
    Provider             string              `json:"provider"`
    Endpoint             string              `json:"endpoint"`
    Bucket               string              `json:"bucket"`
    Region               string              `json:"region"`
    AccessKeyConfigured  bool                `json:"access_key_configured"`
    CredentialConfigured bool                `json:"credential_configured"`
    Verification         *VerificationResult `json:"verification,omitempty"`
}

type BackupStatus struct {
    EvidenceKeyConfigured bool                `json:"evidence_key_configured"`
    Verification          *VerificationResult `json:"verification,omitempty"`
}
```

Repository reads must project only allowlisted detail keys. Recording uses an
expected version and updates the audit ledger and outbox in the same
transaction.

- [ ] **Step 6: Run domain and integration tests**

Run:

```bash
go test ./backend/internal/setup ./backend/migrations -count=1
```

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add backend/migrations/000075_setup_verification_evidence.sql \
  backend/migrations/setup_verification_evidence_contract_test.go \
  backend/internal/setup
git commit -m "feat: add typed setup readiness evidence"
```

### Task 2: Authoritative intake and effective runtime status

**Files:**
- Modify: `backend/internal/setup/postgres.go`
- Modify: `backend/internal/setup/postgres_integration_test.go`
- Modify: `backend/internal/setup/service.go`
- Modify: `backend/internal/setup/service_test.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/internal/config/config.go`

**Interfaces:**
- Consumes: typed `CenterStatus` from Task 1.
- Produces:
  - `type RuntimeConfiguration struct { PublicURL, S3Endpoint, S3Bucket, S3Region string; S3AccessKeyConfigured, S3CredentialConfigured, BackupEvidenceKeyConfigured bool }`.
  - `WithRuntimeConfiguration(RuntimeConfiguration) Option`.
  - `Repository.IntakeStatus(ctx, mspID string) (IntakeStatus, error)`.

- [ ] **Step 1: Write failing intake-composition tests**

Cover these cases:

```text
enabled Graph connection with configured credential -> graph_configured=true
unrevoked, unexpired service key -> api_key_configured=true
enabled forwarding connection with healthy/pending state -> forwarding_configured=true
disabled/revoked/expired rows -> false
rows for a different MSP -> ignored
```

Assert the section is `verified` when at least one intake path is configured and
healthy, `action_required` when none exists, and `attention` when a configured
path has a failed health state.

- [ ] **Step 2: Run focused tests and confirm failure**

Run:

```bash
go test ./backend/internal/setup -run 'TestCenterStatus.*(Intake|Runtime)' -count=1
```

Expected: failure because authoritative composition is not implemented.

- [ ] **Step 3: Implement scoped intake queries**

Query `graph_mailbox_connections`, `service_api_keys`, and
`forwarding_intake_connections` by the requested MSP. Use active timestamps and
existing health columns; do not infer readiness from `installation_setup.intake_config`.

- [ ] **Step 4: Inject effective runtime configuration**

Pass only non-secret values and credential-presence booleans from
`config.Config` into the setup service. Derive provider as `minio` for a MinIO
endpoint hostname, `amazon_s3` for AWS S3 endpoints, otherwise
`s3_compatible`. Never pass an access key or secret value into status.

- [ ] **Step 5: Run setup and config tests**

Run:

```bash
go test ./backend/internal/setup ./backend/internal/config -count=1
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/setup backend/internal/config/config.go \
  backend/cmd/rarity-api/main.go
git commit -m "feat: compose setup readiness from live configuration"
```

### Task 3: Bounded object-storage verification

**Files:**
- Create: `backend/internal/setup/storage_probe.go`
- Create: `backend/internal/setup/storage_probe_test.go`
- Modify: `backend/internal/setup/service.go`
- Modify: `backend/internal/setup/service_test.go`
- Modify: `backend/internal/httpapi/setup_routes.go`
- Modify: `backend/internal/httpapi/setup_routes_test.go`
- Modify: `backend/cmd/rarity-api/main.go`

**Interfaces:**
- Consumes: `Repository.RecordVerification` from Task 1 and the configured
  `objectstorage.Store`.
- Produces:
  - `type StorageProbe interface { Put(context.Context,string,[]byte,string) error; Delete(context.Context,string) error }`.
  - `Service.VerifyObjectStorage(ctx, VerifyObjectStorageCommand) (CenterStatus, error)`.
  - `POST /api/v1/setup/center/object-storage/verify`.

- [ ] **Step 1: Write failing probe tests**

Use a recording fake and assert:

```go
key := "setup-probes/" + mspID + "/" + generatedID
body := []byte("rarity-storage-probe")
contentType := "application/octet-stream"
```

Test success, put failure, delete failure, panic-free cleanup after every
successful put, safe codes `storage_write_failed` and
`storage_cleanup_failed`, no provider response body retention, and 30-minute
verification validity.

- [ ] **Step 2: Run focused tests and confirm failure**

Run:

```bash
go test ./backend/internal/setup -run TestStorageProbe -count=1
```

Expected: failure because `storage_probe.go` does not exist.

- [ ] **Step 3: Implement the storage probe**

Write the fixed probe body, delete it immediately, and translate adapter errors
to the fixed safe codes. The audit safe diff contains only section, result, and
timestamp. A cleanup failure is `attention`, never `verified`.

- [ ] **Step 4: Add the protected HTTP route**

Decode:

```go
type verifySetupRequest struct {
    ExpectedVersion int64  `json:"expected_version"`
    Reason          string `json:"reason"`
}
```

Require session authentication, CSRF, `organization.manage`, non-empty reason,
and optimistic version. Return the refreshed typed `CenterStatus`.

- [ ] **Step 5: Run setup and HTTP tests**

Run:

```bash
go test ./backend/internal/setup ./backend/internal/httpapi -count=1
```

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/setup backend/internal/httpapi \
  backend/cmd/rarity-api/main.go
git commit -m "feat: verify setup object storage"
```

### Task 4: Signed backup and PITR evidence

**Files:**
- Create: `backend/internal/setup/backup_evidence.go`
- Create: `backend/internal/setup/backup_evidence_test.go`
- Create: `backend/cmd/rarity-backup-evidence/main.go`
- Create: `backend/cmd/rarity-backup-evidence/main_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `.env.example`
- Modify: `backend/internal/httpapi/setup_routes.go`
- Modify: `backend/internal/httpapi/setup_routes_test.go`
- Modify: `backend/cmd/rarity-api/main.go`

**Interfaces:**
- Consumes: `Repository.RecordVerification` from Task 1.
- Produces:
  - `BackupEvidencePayload { MSPID, Nonce, IssuedAt, Stanza, Repository, LatestBackupAt, LatestWALAt, RestoreVerifiedAt }`.
  - `SignBackupEvidence(payload, key []byte) (string, error)`.
  - `Service.AcceptBackupEvidence(ctx, AcceptBackupEvidenceCommand) (CenterStatus, error)`.
  - `POST /api/v1/setup/center/backups/evidence`.
  - CLI flags `--pgbackrest-json`, `--restore-evidence`, `--msp-id`,
    `--api-url`, and `--reason`.

- [ ] **Step 1: Write failing signature and policy tests**

Test canonical JSON signing with HMAC-SHA-256, constant-time verification,
installation MSP binding, 15-minute issue-time expiry, unique nonce replay
rejection, backup freshness of 24 hours, WAL freshness of 15 minutes, and
restore-proof freshness of 31 days. Safe results are:

```text
backup_verified
backup_stale
wal_stale
restore_proof_stale
evidence_invalid
evidence_replayed
```

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./backend/internal/setup -run TestBackupEvidence -count=1
```

Expected: failure because the evidence contract does not exist.

- [ ] **Step 3: Implement evidence verification**

Load `RARITY_BACKUP_EVIDENCE_KEY` as a minimum 32-byte secret in API and CLI
environments. Store only the SHA-256 evidence hash, consumed nonce, allowlisted
stanza/repository labels, safe result code, and timestamps. Never store the
signature, command output, paths, credentials, or pgBackRest payload.

- [ ] **Step 4: Implement the read-only CLI**

The CLI reads an operator-supplied pgBackRest JSON file and restore-evidence
JSON file, normalizes only the required timestamps and labels, creates a random
nonce, signs the canonical payload, and submits it over HTTPS with:

```http
Authorization: BackupEvidence <base64url-signature>
Content-Type: application/json
```

Reject non-HTTPS API URLs except loopback development. Print only the final
section state and safe code.

- [ ] **Step 5: Add the ingestion route and tests**

The route is not session-authenticated; the evidence signature is its narrow
authority. It accepts one evidence submission, enforces size limits through the
existing request decoder, verifies before mutation, and returns no internal
details. Replay returns `409`; invalid or expired evidence returns `401`.

- [ ] **Step 6: Run CLI, setup, HTTP, and config tests**

Run:

```bash
go test ./backend/cmd/rarity-backup-evidence ./backend/internal/setup \
  ./backend/internal/httpapi ./backend/internal/config -count=1
```

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add .env.example backend/cmd/rarity-backup-evidence \
  backend/internal/setup backend/internal/httpapi backend/internal/config \
  backend/cmd/rarity-api/main.go
git commit -m "feat: ingest signed backup readiness evidence"
```

### Task 5: Guided plain-English Setup Center UI

**Files:**
- Create: `frontend/src/features/setup/setupTypes.ts`
- Create: `frontend/src/features/setup/SetupCenterOverview.tsx`
- Create: `frontend/src/features/setup/IntakeSetupPanel.tsx`
- Create: `frontend/src/features/setup/ObjectStorageSetupPanel.tsx`
- Create: `frontend/src/features/setup/BackupSetupPanel.tsx`
- Create: `frontend/src/features/setup/SetupGuidance.tsx`
- Modify: `frontend/src/features/setup/SetupPage.tsx`
- Modify: `frontend/src/features/setup/SetupPage.test.tsx`
- Modify: `frontend/src/features/setup/setup.css`

**Interfaces:**
- Consumes: typed Setup Center payload and verification endpoints from Tasks
  1–4.
- Produces:
  - `SetupCenterOverview({center,onOpen})`.
  - `IntakeSetupPanel({status,onClose})`.
  - `ObjectStorageSetupPanel({center,onUpdated,onClose})`.
  - `BackupSetupPanel({status,onClose})`.
  - No `JSON.parse`, `JSON.stringify`, or completed-installation textarea.

- [ ] **Step 1: Replace JSON-editor tests with failing guided-flow tests**

Assert:

```text
no textbox label contains "JSON"
no rendered text contains "env://", "secret://", or "vault://"
each card opens only its own named region
helper text is associated with fields and actions
object-storage effective values are read-only
missing runtime values show exact environment setting names
test storage submits expected_version and reason with CSRF
backup panel shows the copyable CLI command and freshness requirements
intake panel links to Graph mailboxes, Service API keys, and Forwarding intake
focus moves to the opened panel heading and returns to the card on close
```

- [ ] **Step 2: Run SetupPage tests and confirm failure**

Run:

```bash
npm --prefix frontend test -- --run src/features/setup/SetupPage.test.tsx
```

Expected: failure because the raw JSON form is still rendered.

- [ ] **Step 3: Add typed UI contracts and overview**

Use exact state labels:

```ts
const setupStateLabel = {
  not_started: "Not started",
  action_required: "Action required",
  ready_to_verify: "Ready to verify",
  verified: "Verified",
  attention: "Attention",
} as const;
```

Cards show one sentence of status, last verification time when present, and
`Start setup`, `Continue setup`, `Test again`, or `Review issue` based on state.

- [ ] **Step 4: Implement the intake panel**

Explain both intake paths in plain language. Reuse ordinary links to
`#/graph-settings`, `#/service-keys`, and `#/forwarding-settings`; show current
configured/health state from the typed payload. Do not duplicate credential
forms already owned by those protected pages.

- [ ] **Step 5: Implement the object-storage panel**

Show provider, endpoint, bucket, region, and credential-presence status as
named definition rows. For missing values, show exact deployment names
`S3_ENDPOINT`, `S3_BUCKET`, `S3_REGION`, `S3_ACCESS_KEY_ID`, and
`S3_SECRET_ACCESS_KEY`, with the restart explanation. Submit the bounded test
only after a required reason is entered.

- [ ] **Step 6: Implement the backup panel**

Show the separate-failure-domain explanation, named pgBackRest checklist,
24-hour backup, 15-minute WAL, and 31-day restore-proof freshness requirements.
Render a copyable command using the current public URL and MSP ID, but never
embed the evidence key. Show backup and restore proof as separate status rows.

- [ ] **Step 7: Add responsive and accessible styling**

Use the existing setup card styles. At desktop, instructions and status may
form two columns; below 760px, use one column. Associate helper/error text with
`aria-describedby`, put verification updates in `role="status"`, and focus the
panel heading with `tabIndex={-1}`.

- [ ] **Step 8: Run Setup Center tests**

Run:

```bash
npm --prefix frontend test -- --run src/features/setup/SetupPage.test.tsx
npm --prefix frontend exec prettier -- --check \
  src/features/setup src/features/setup/SetupPage.test.tsx
```

Expected: pass.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/features/setup
git commit -m "feat: add guided setup center workflows"
```

### Task 6: Retire the browser reference editor and document operations

**Files:**
- Modify: `backend/internal/httpapi/setup_routes.go`
- Modify: `backend/internal/httpapi/setup_routes_test.go`
- Modify: `backend/internal/setup/service.go`
- Modify: `backend/internal/setup/service_test.go`
- Modify: `docs/07-ui-ux/first-run-and-help.md`
- Modify: `docs/05-infrastructure/deployment.md`
- Modify: `docs/05-infrastructure/storage.md`
- Modify: `docs/05-infrastructure/backups.md`
- Modify: `docs/04-api/rest-api.md`

**Interfaces:**
- Consumes: all guided workflows and verification endpoints.
- Produces: no authenticated browser route that accepts arbitrary intake,
  storage, or backup maps.

- [ ] **Step 1: Write failing retirement tests**

Assert `PUT /api/v1/setup/center/configuration` returns `410 Gone` with
`setup_reference_editor_retired`, while bootstrap compatibility remains intact.
Assert the completed-installation frontend never calls that route.

- [ ] **Step 2: Run focused tests and confirm failure**

Run:

```bash
go test ./backend/internal/httpapi -run TestSetupReferenceEditorRetired -count=1
npm --prefix frontend test -- --run src/features/setup/SetupPage.test.tsx
```

Expected: Go test fails because the route still updates arbitrary maps.

- [ ] **Step 3: Retire the mutation boundary**

Keep decoding legacy bootstrap maps for existing first-run compatibility, but
remove their influence on completed-installation readiness. Return the fixed
`410` error from the old authenticated update route for one release so stale
clients fail explicitly.

- [ ] **Step 4: Update administrator and API documentation**

Document:

```text
what Rarity configures directly
what remains deployment-managed
where each value comes from
how the storage probe works and cleans up
how to generate and submit backup evidence
freshness windows and safe failure codes
secret-handling and restart boundaries
```

- [ ] **Step 5: Run repository verification**

Run:

```bash
go test -race ./backend/...
go test ./tests/... -count=1
go test ./infrastructure/... -count=1
go vet ./backend/... ./tests/...
npm --prefix frontend test -- --run
npm --prefix frontend run build
npm --prefix frontend exec prettier -- --check .
```

Expected: all pass. The existing Vite chunk-size advisory may remain a warning.

- [ ] **Step 6: Validate migration 75 on a disposable database**

Apply all migrations to a new temporary PostgreSQL database, verify the schema
at version 75, run the integration tests, and remove only that explicitly named
temporary database.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/httpapi backend/internal/setup docs
git commit -m "docs: publish guided setup operations"
```

### Task 7: Deploy and complete rendered acceptance

**Files:**
- Modify only if acceptance exposes a defect: files owned by Tasks 1–6.

**Interfaces:**
- Consumes: exact committed candidate and the RTI demo deployment workflow.
- Produces: exact-revision live proof and screenshots at desktop and narrow
  widths.

- [ ] **Step 1: Confirm the candidate**

Run:

```bash
git status --short --branch
git log -1 --oneline
```

Expected: clean feature branch with the planned commits.

- [ ] **Step 2: Push the exact candidate**

Push `codex/entra-setup-center-fix` without changing the demo server's remote
configuration. Confirm the remote branch resolves to the local SHA.

- [ ] **Step 3: Deploy through the RTI demo workflow**

Transfer/fetch the exact commit, deploy it with the existing scoped Compose
files, apply migration 75, and verify `/readyz` plus `/v1/system/build` report
the exact SHA.

- [ ] **Step 4: Run authenticated full-page acceptance**

At 1440x1000, verify:

```text
all five readiness cards render
no JSON editor or reference URI syntax is visible
each guided panel opens, focuses, and closes
intake links reach the correct admin surfaces
object storage shows effective values without credentials
storage test returns a safe result and removes its probe object
backup instructions and freshness states are understandable
no 404, runtime overlay, relevant console error, or blank content
```

Do not submit synthetic mailbox credentials or backup evidence.

- [ ] **Step 5: Run narrow acceptance**

Below 760px, verify one-column layout, no horizontal overflow, keyboard access,
visible field help, and readable verification results.

- [ ] **Step 6: Preserve the deliverable tab and report**

Leave the authenticated Setup Center open at desktop width. Report the deployed
SHA, live checks, any intentionally incomplete real-provider steps, and the next
operator action.
