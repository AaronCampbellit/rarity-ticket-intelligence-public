# Optional Entra and Local Platform Administrator Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make RTI fully installable and administrable with local Platform Administrator accounts while keeping Microsoft Entra optional and connectable later.

**Architecture:** Preserve the existing technician, role, session, and credential records while promoting local credentials from break-glass-only terminology to a normal Platform Administrator authentication path. Make Entra columns nullable, load local authentication independently, add versioned Entra settings and local-administrator APIs, and update setup and signed-out GUI behavior to reflect actual availability.

**Tech Stack:** Go 1.25, PostgreSQL 16 migrations/pgx, bcrypt, encrypted secret provider, React 19, TypeScript, Vitest, Testing Library, axe-core, Docker Compose.

## Global Constraints

- Bootstrap must work without any Entra field.
- Every local account is linked to its own technician with the system Platform Administrator role.
- At least one enabled local Platform Administrator must remain.
- Existing password hashes, account enablement, CIDRs, technician links, and sessions must survive migration.
- Entra activation must never disable local authentication.
- Passwords, bootstrap tokens, Entra secrets, and provider secrets must remain write-only and absent from logs, events, audits, and responses.
- The old break-glass routes remain compatibility aliases for one release and emit deprecation headers.
- No subagents are used; execution is inline.

---

### Task 1: Nullable Entra Setup Persistence

**Files:**
- Create: `backend/migrations/000061_optional_entra_local_administrators.sql`
- Modify: `backend/internal/setup/service.go`
- Modify: `backend/internal/setup/postgres.go`
- Test: `backend/internal/setup/service_test.go`
- Test: `backend/internal/setup/postgres_integration_test.go`
- Test: `backend/migrations/kernel_contract_test.go`

**Interfaces:**
- Produces: `Configuration.EntraConfigured() bool`
- Produces: `RuntimeConfiguration.EntraConfigured bool`
- Produces: nullable installation Entra columns plus `entra_state text` and `entra_version bigint`

- [ ] **Step 1: Add failing service tests**

Add tests proving an Entra-free `Configuration` is valid, a partial Entra group
is rejected, and the secret provider is not called for an omitted Entra group:

```go
func TestBootstrapAcceptsNoEntraConfiguration(t *testing.T) {
    value := validConfiguration()
    value.EntraTenantID, value.EntraClientID = "", ""
    value.EntraClientSecret, value.EntraRedirectURL = "", ""
    value.AdminEntraSubject = ""
    // Bootstrap must succeed and EntraSecret must remain zero-valued.
}

func TestBootstrapRejectsPartialEntraConfiguration(t *testing.T) {
    value := validConfiguration()
    value.EntraClientSecret = ""
    // Bootstrap must return ErrInvalidBootstrap.
}
```

- [ ] **Step 2: Run the focused tests and confirm failure**

Run:

```bash
go test ./backend/internal/setup -run 'TestBootstrap(AcceptsNoEntra|RejectsPartialEntra)' -count=1
```

Expected: the no-Entra case fails validation.

- [ ] **Step 3: Add the forward migration**

Create migration `000061` that:

```sql
ALTER TABLE installation_setup
  ALTER COLUMN entra_tenant_id DROP NOT NULL,
  ALTER COLUMN entra_client_id DROP NOT NULL,
  ALTER COLUMN entra_secret_version DROP NOT NULL,
  ALTER COLUMN entra_secret_nonce DROP NOT NULL,
  ALTER COLUMN entra_secret_ciphertext DROP NOT NULL,
  ALTER COLUMN entra_redirect_url DROP NOT NULL;
ALTER TABLE installation_setup
  ADD COLUMN entra_state text NOT NULL DEFAULT 'not_connected'
    CHECK (entra_state IN ('not_connected','verification_required','connected','action_required')),
  ADD COLUMN entra_version bigint NOT NULL DEFAULT 1 CHECK (entra_version > 0);
UPDATE installation_setup
SET entra_state = 'connected'
WHERE entra_tenant_id IS NOT NULL;
```

Add a constraint requiring the Entra columns to be either all null or all
present. The down migration removes state/version, fills no values, and restores
`NOT NULL` only after refusing rows with absent Entra configuration.

- [ ] **Step 4: Implement grouped Entra validation and optional sealing**

Add:

```go
func (value Configuration) EntraConfigured() bool
```

Return false when all five Entra inputs are empty. Validate all five when any is
present. In `Bootstrap`, seal and clear the client secret only for a complete
Entra group.

- [ ] **Step 5: Make PostgreSQL bootstrap and runtime reads nullable**

Build the external identity insert and Entra secret arguments only for a
configured Entra group. Store `entra_state='connected'` for bootstrap with Entra
and `not_connected` otherwise. Scan nullable Entra fields and open a sealed
secret only when configured. Return `RuntimeConfiguration{MSPID: ..., EntraConfigured:false}`
for completed local-only installations.

- [ ] **Step 6: Add and run PostgreSQL coverage**

Extend the integration test to bootstrap without Entra and assert:

```go
runtime.EntraConfigured == false
runtime.MSPID == mspID
runtime.EntraClientSecret == ""
```

Run:

```bash
go test ./backend/internal/setup ./backend/migrations -count=1
```

- [ ] **Step 7: Commit**

```bash
git add backend/migrations/000061_optional_entra_local_administrators.sql backend/internal/setup
git commit -m "feat: make Entra optional during bootstrap"
```

### Task 2: Local Platform Administrator Domain and Compatibility API

**Files:**
- Create: `backend/internal/identity/local_admin.go`
- Create: `backend/internal/identity/local_admin_management.go`
- Create: `backend/internal/store/authn/local_admin.go`
- Create: `backend/internal/httpapi/local_admin_routes.go`
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Test: corresponding `*_test.go` files

**Interfaces:**
- Produces: `LocalAdminAuthenticator.Authenticate(context.Context, username, password, sourceIP string) (technicianID string, error)`
- Produces: `LocalAdminManagementService` list/create/reset-password/update-networks/disable operations
- Produces: `/auth/local/login`
- Produces: `/api/v1/admin/local-administrators`
- Consumes: the existing `break_glass_accounts` table during the compatibility release

- [ ] **Step 1: Write failing local-authentication tests**

Copy the behavioral coverage, not the names, from `breakglass_test.go`. Assert
generic denial for missing, disabled, wrong-password, disallowed-network, and
audit failure. Assert successful use records `security.local_admin.authenticated`.

- [ ] **Step 2: Run and confirm the new package API is absent**

```bash
go test ./backend/internal/identity -run LocalAdmin -count=1
```

- [ ] **Step 3: Implement local authentication**

Use the same bcrypt cost floor, canonical usernames, constant-cost dummy hash,
CIDR enforcement, and atomic last-use/audit/outbox mutation as the current
authenticator. Rename emitted subject type to `local_administrator` and reason
to `local administrator authentication`.

- [ ] **Step 4: Write failing management tests**

Cover:

```go
CreateLocalAdminCommand{
    Principal: platformAdmin,
    Email: "second-admin@example.test",
    DisplayName: "Second Admin",
    Username: "second-admin",
    Password: "correct horse battery staple",
    AllowedCIDRs: []string{"192.168.86.0/24"},
    Reason: "Add installation administrator",
    Source: "browser",
}
```

Assert that creation makes a new technician, assigns the existing
`global-admin` role, hashes the password, and emits one audit and one event in a
single transaction. Add reset-password, CIDR update, optimistic-version
conflict, disable, and final-enabled-admin tests.

- [ ] **Step 5: Implement the repository transactions and service**

Keep account credentials in the existing table for migration safety. Creation
must insert a new technician, resolve the installation's system global-admin
role, assign it, and insert the credential atomically. Reset and CIDR changes
increment versions and revoke existing sessions for that technician.

- [ ] **Step 6: Add new HTTP routes and old aliases**

Register:

```text
GET    /api/v1/admin/local-administrators
POST   /api/v1/admin/local-administrators
POST   /api/v1/admin/local-administrators/{id}/password
PATCH  /api/v1/admin/local-administrators/{id}/networks
POST   /api/v1/admin/local-administrators/{id}/disable
POST   /auth/local/login
```

Keep old routes wired to equivalent behavior and set:

```http
Deprecation: true
Sunset: <one release date>
Link: </api/v1/admin/local-administrators>; rel="successor-version"
```

- [ ] **Step 7: Run focused backend tests**

```bash
go test ./backend/internal/identity ./backend/internal/store/authn ./backend/internal/browserauth ./backend/internal/httpapi ./backend/cmd/rarity-api -count=1
```

- [ ] **Step 8: Commit**

```bash
git add backend/internal/identity backend/internal/store/authn backend/internal/httpapi backend/internal/browserauth backend/cmd/rarity-api
git commit -m "feat: add local Platform Administrator authentication"
```

### Task 3: Entra-Free Runtime and Entra Settings Boundary

**Files:**
- Create: `backend/internal/identity/entra_settings.go`
- Create: `backend/internal/store/authn/entra_settings.go`
- Create: `backend/internal/httpapi/entra_settings_routes.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/internal/setup/postgres.go`
- Test: corresponding backend tests

**Interfaces:**
- Produces: `GET /api/v1/admin/identity/entra`
- Produces: `PUT /api/v1/admin/identity/entra`
- Produces: `POST /api/v1/admin/identity/entra/verify`
- Produces: `POST /api/v1/admin/identity/entra/disable`
- Produces: state metadata without returning a client secret

- [ ] **Step 1: Write failing runtime tests**

Add cases proving `applyRuntimeSetup` always loads `MSPID`, installs local auth
without Entra, and installs the Entra handler only when
`RuntimeConfiguration.EntraConfigured` is true.

- [ ] **Step 2: Run focused tests and confirm failure**

```bash
go test ./backend/cmd/rarity-api -run 'RuntimeSetup|BrowserAuth' -count=1
```

- [ ] **Step 3: Decouple local and Entra handler construction**

Apply runtime MSP values unconditionally after setup. Build local authentication
whenever `MSPID` is present. Return without wrapping the Entra handler when the
tenant/client/secret/redirect group is inactive.

- [ ] **Step 4: Add Entra settings service and persistence tests**

Test authorization, reason/version requirements, write-only secret behavior,
grouped validation, inactive draft state, failed verification preserving the
previous active generation, and disable behavior.

- [ ] **Step 5: Implement settings API**

Persist a candidate sealed secret and metadata with state
`verification_required`. The verification endpoint performs tenant-specific
OIDC discovery, validates exact issuer and authorization/token/JWKS HTTPS
origins, then marks the generation `connected`. Because a full authorization
code requires a human Microsoft sign-in, runtime discovery verification is
automatic while controlled sign-in remains a visible acceptance item.

- [ ] **Step 6: Run backend tests**

```bash
go test ./backend/internal/identity ./backend/internal/store/authn ./backend/internal/httpapi ./backend/cmd/rarity-api -count=1
```

- [ ] **Step 7: Commit**

```bash
git add backend/internal/identity backend/internal/store/authn backend/internal/httpapi backend/cmd/rarity-api backend/internal/setup
git commit -m "feat: support later Entra activation"
```

### Task 4: Setup and Identity GUI

**Files:**
- Modify: `frontend/src/features/setup/SetupPage.tsx`
- Modify: `frontend/src/features/setup/SetupPage.test.tsx`
- Create: `frontend/src/features/auth/LocalAdministratorsPage.tsx`
- Create: `frontend/src/features/auth/LocalAdministratorsPage.test.tsx`
- Create: `frontend/src/features/auth/EntraSettingsPanel.tsx`
- Create: `frontend/src/features/auth/EntraSettingsPanel.test.tsx`
- Modify: `frontend/src/api/browserSession.ts`
- Modify: `frontend/src/api/browserSession.test.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/navigation.ts`
- Modify: `frontend/src/app.css`

**Interfaces:**
- Consumes: Task 2 and Task 3 HTTP APIs
- Produces: optional Entra setup section, local sign-in route, and authenticated Identity settings

- [ ] **Step 1: Write failing setup GUI tests**

Assert that Entra defaults to **Skip for now**, its inputs are absent/disabled,
an Entra-free submit omits Entra keys, selecting **Configure now** reveals all
fields, and partial configuration cannot submit.

- [ ] **Step 2: Run the focused Vitest file**

```bash
npm --prefix frontend test -- --run src/features/setup/SetupPage.test.tsx
```

- [ ] **Step 3: Implement progressive setup**

Rename recovery fields to local-administrator fields in copy and payload.
Render radio controls for Entra choice and serialize either the complete group
or no Entra keys. Success copy must say local sign-in is ready and Entra can be
connected later.

- [ ] **Step 4: Write failing signed-out shell tests**

Assert local sign-in is primary when Entra is unavailable, Microsoft is absent
in that state, both options render when connected, and the local page contains
no outage-only warning.

- [ ] **Step 5: Implement local sign-in availability state**

Extend public setup status with a non-sensitive `entra_available` boolean after
completion. Use it only to decide whether the Microsoft link is actionable.
Post local credentials to `/auth/local/login`.

- [ ] **Step 6: Add Identity management tests and UI**

Test list/create/reset/network/disable flows, explicit reason capture,
write-only password controls, final-admin error, optimistic conflict recovery,
and no secret rendering. Add the Entra status/configure/verify/disable panel.

- [ ] **Step 7: Run frontend verification**

```bash
npm --prefix frontend test -- --run
npm --prefix frontend run build
```

- [ ] **Step 8: Commit**

```bash
git add frontend/src
git commit -m "feat: add Entra-optional setup and local admin UI"
```

### Task 5: Documentation and Full Verification

**Files:**
- Modify: `docs/02-platform/identity-and-access.md`
- Modify: `docs/03-security/authentication.md`
- Modify: `docs/03-security/v1-identity-and-ai-boundaries.md`
- Modify: `docs/03-security/entra-sso.md`
- Modify: `docs/07-ui-ux/first-run-and-help.md`
- Modify: `docs/07-ui-ux/technician-workspace-wireframes.md`
- Modify: `docs/09-roadmap/build-backlog.md`
- Modify: `docs/09-roadmap/implementation-plan.md`

**Interfaces:**
- Produces: aligned product, security, UX, and phase documentation

- [ ] **Step 1: Replace obsolete identity requirements**

Document Entra as optional workforce SSO, local Platform Administrators as the
required installation administration path, compatibility aliases, later Entra
activation, and the separation between identity Entra and Graph credentials.

- [ ] **Step 2: Run stale-copy and formatting scans**

```bash
rg -n "Entra-linked global administrator|Local recovery sign-in|solely to expose the bootstrap|Restart the Rarity API to activate" docs frontend
git diff --check
```

Expected: no obsolete product claims remain in active docs or UI.

- [ ] **Step 3: Run full source verification**

```bash
go list ./backend/... | grep -v '/infrastructure/compose$' | xargs go test
go vet ./backend/...
npm --prefix frontend test -- --run
npm --prefix frontend run build
```

- [ ] **Step 4: Commit**

```bash
git add docs
git commit -m "docs: align identity guidance with optional Entra"
```

### Task 6: Demo Deployment and Entra-Free Acceptance

**Files:**
- Modify only if required by verified defects: `infrastructure/compose/*`
- Record evidence in: `docs/09-roadmap/implementation-plan.md`

**Interfaces:**
- Consumes: RTI demo-server skill and current VM connection
- Produces: deployed migration, healthy API, local-authenticated browser session

- [x] **Step 1: Read the RTI demo-server skill and inspect current state**

Use the pinned SSH connection, strict known-host verification, `/home/campbellservers/rti-demo`,
and the Compose project/network discovered from the live host.

- [x] **Step 2: Deploy the exact committed source**

Use the skill's deployment workflow. Verify migration `000068`, API revision,
container health, `/healthz`, `/readyz`, and LAN GUI asset revision.

- [x] **Step 3: Complete Entra-free bootstrap**

Issue a fresh 15-minute token without printing it to logs, submit organization
and local-admin fields with Entra omitted, and verify bootstrap closes.

- [x] **Step 4: Verify local administration**

Sign in through `/auth/local/login`, confirm `/api/v1/me`, authenticated
navigation, setup-center state, local-administrator list, and creation of a
second local administrator. Do not print passwords or session cookies.

- [x] **Step 5: Record acceptance and commit**

Update the implementation plan with source, database, deployment, and browser
evidence kept distinct, then commit:

```bash
git commit -am "docs: record Entra-free demo acceptance"
```

### Task 7: Ollama Live Provider Acceptance

**Files:**
- Modify only if defects are found: `backend/internal/ai/*`, `frontend/src/features/ai/*`
- Record evidence in: `docs/09-roadmap/implementation-plan.md`

**Interfaces:**
- Consumes: Ollama endpoint `http://192.168.86.158:11434`
- Produces: provider connection, discovered models, selected model, and completed test request

- [x] **Step 1: Verify network reachability without mutation**

From the RTI API container/network, call Ollama `/api/version` and `/api/tags`
with bounded timeouts. Record version, model identifiers, response size, and
latency without copying model blobs.

- [x] **Step 2: Configure through the authenticated provider API**

Create an `ollama` provider using the GUI/API contract, select the discovered
model, and confirm the secret-free provider response and persisted selection.

- [x] **Step 3: Run a bounded test request**

Submit a short synthetic prompt through RTI's provider test/runtime path. Verify
queued/running/completed transitions, raw-response ceiling behavior, timing,
output persistence, audit, and absence of secret leakage.

- [x] **Step 4: Diagnose and fix verified defects with TDD**

For each failure, first add the smallest regression test, confirm it fails,
implement the fix, rerun focused and full relevant tests, redeploy, and repeat
the live acceptance.

- [x] **Step 5: Record facts and commit**

Record actual Ollama version, discovered model, selected model, request state,
duration, and any remaining data-dependent AI work. Commit intentional fixes
and evidence.

### Task 8: Accessibility and Remaining Environment Phases

**Files:**
- Modify: `docs/05-infrastructure/pilot-acceptance-runbook.md`
- Modify: `docs/09-roadmap/implementation-plan.md`
- Modify: `docs-site/app.js`
- Modify only on verified defects: authenticated frontend/design-system files

**Interfaces:**
- Produces: objective browser evidence and explicit human-only acceptance list
- Produces: refreshed HTML implementation guide manifest/status

- [x] **Step 1: Automate zoom/reflow and touch checks**

Use browser automation at 200% and 400% effective zoom/viewports and touch-sized
mobile viewports. Check no two-dimensional page overflow, reachable actions,
visible focus, and minimum target sizing. Capture screenshots/evidence and fix
verified defects test-first.

- [x] **Step 2: Automate high-contrast checks**

Emulate forced colors and increased contrast where supported. Verify status,
focus, selected state, errors, and disabled controls retain non-color meaning.
Record browser limitations separately.

- [ ] **Step 3: Complete available VoiceOver checks**

On macOS, inspect landmark order, accessible names, status/error announcements,
setup choice semantics, local login, provider creation, model selection, and
test-request results using VoiceOver where the connected environment permits.
Record any interaction requiring physical human confirmation.

- [x] **Step 4: Record NVDA as external-device acceptance if unavailable**

Do not claim NVDA completion without a connected Windows environment and actual
NVDA run. Preserve an exact checklist, browser/version fields, expected
announcements, and result slots while continuing all non-human work.

- [ ] **Step 5: Resume broader real-environment phases**

Read the canonical implementation plan from the first incomplete phase onward.
Complete every safe item supported by available demo infrastructure, configured
services, synthetic data, and automation. Leave HA failover, destructive DR,
external credentials, and physical assistive-technology checks pending when
they require new authority or human touch.

- [x] **Step 6: Refresh the implementation HTML**

Update `docs-site/app.js` so every Markdown source remains represented and phase
states/evidence match the canonical plan. Run manifest coverage and browser
filtering checks, then open the implementation guide for review.

- [ ] **Step 7: Final full verification, commit, and push**

Run source tests/build, live health/revision checks, `git diff --check`, guide
coverage, and `git status`. Commit intentional changes. If no remote exists,
report the exact push blocker without inventing a destination.
