# Automatic Remote Bootstrap URL Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically print a secure, copyable first-run setup URL that can be opened from a different device and consumed without leaving the bootstrap token in browser history.

**Architecture:** Add a validated `RARITY_PUBLIC_URL`, a focused setup-link builder, and a testable API-startup issuance boundary around the existing single-use token service. The React setup page consumes the token from the hash-router fragment into component memory, scrubs the fragment immediately, and submits through the existing bootstrap authorization header.

**Tech Stack:** Go 1.24, pgx/PostgreSQL, React 19, TypeScript, Vitest/Testing Library, Playwright, Docker Compose.

## Global Constraints

- `pilot` and `production` public URLs require HTTPS.
- HTTP is allowed only for loopback hosts in `local`, `test`, and `demo`.
- Tokens remain 32-byte, single-use, SHA-256-hashed, and valid for 15 minutes.
- Incomplete-installation restarts rotate unused authority; completed installations never issue authority.
- The token exists in plaintext only in the single intentional deployment-output URL, browser component memory, and the `Authorization: Bootstrap` request header.
- Existing uncommitted UI changes in the main checkout must remain unstaged and uncommitted unless directly required by this feature.

---

### Task 1: Public URL and setup-link contract

**Files:**
- Create: `backend/internal/setup/bootstrap_url.go`
- Create: `backend/internal/setup/bootstrap_url_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`

**Interfaces:**
- Produces: `setup.BuildBootstrapURL(publicURL, token string) (string, error)`.
- Produces: `config.Config.PublicURL string`.
- Consumes: `RARITY_PUBLIC_URL` and the existing environment classification.

- [ ] **Step 1: Write failing setup-link tests**

Cover a normal origin, a path-prefixed origin, missing token, user information,
query, fragment, and non-absolute input:

```go
func TestBuildBootstrapURLPreservesPublicPathPrefix(t *testing.T) {
    got, err := BuildBootstrapURL("https://rarity.example/support/rti", "secret-token")
    if err != nil {
        t.Fatal(err)
    }
    want := "https://rarity.example/support/rti/#/setup?bootstrap_token=secret-token"
    if got != want {
        t.Fatalf("URL=%q want=%q", got, want)
    }
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `GOTOOLCHAIN=local go test ./backend/internal/setup ./backend/internal/config`

Expected: FAIL because `BuildBootstrapURL` and `Config.PublicURL` do not exist.

- [ ] **Step 3: Implement minimal URL construction and configuration validation**

`BuildBootstrapURL` parses the public URL, rejects credentials/query/fragment,
preserves a cleaned path prefix, and sets the hash to:

```go
values := url.Values{"bootstrap_token": []string{token}}
parsed.Fragment = "/setup?" + values.Encode()
return parsed.String(), nil
```

`config.Load` reads `RARITY_PUBLIC_URL`. When non-empty it validates an absolute
HTTP(S) URL with no user information, query, or fragment. `pilot` and
`production` accept only HTTPS. Other environments accept HTTP only for
`localhost`, `127.0.0.1`, or `::1`.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run: `GOTOOLCHAIN=local go test ./backend/internal/setup ./backend/internal/config`

Expected: PASS.

- [ ] **Step 5: Commit the contract**

Stage only the four Task 1 files and commit:

```text
feat(setup): validate public bootstrap URL
```

---

### Task 2: Automatic startup issuance and recovery helper

**Files:**
- Create: `backend/cmd/rarity-api/bootstrap.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/cmd/rarity-api/main_test.go`
- Modify: `backend/cmd/rarity-bootstrap-token/main.go`
- Create: `backend/cmd/rarity-bootstrap-token/main_test.go`

**Interfaces:**
- Consumes: `setup.BuildBootstrapURL`, `setup.Service.IssueToken`, installation completion state, `Config.PublicURL`.
- Produces: `issueFirstRunSetup(context.Context, bool, string, bootstrapTokenIssuer, io.Writer) error`.
- Produces: one deployment-output notice containing URL and RFC 3339 expiry.

- [ ] **Step 1: Write failing startup tests**

Use an issuer stub with call counting and deterministic token/expiry. Prove:

```go
func TestIssueFirstRunSetupPrintsRemoteURLOnce(t *testing.T) {
    output := new(bytes.Buffer)
    issuer := &bootstrapIssuerStub{
        token: "bootstrap-secret",
        expiresAt: time.Date(2026, 8, 3, 16, 15, 0, 0, time.UTC),
    }
    err := issueFirstRunSetup(
        context.Background(), false, "https://rarity.example",
        issuer, output,
    )
    if err != nil {
        t.Fatal(err)
    }
    if issuer.calls != 1 ||
        !strings.Contains(output.String(), "https://rarity.example/#/setup?bootstrap_token=bootstrap-secret") ||
        !strings.Contains(output.String(), "2026-08-03T16:15:00Z") {
        t.Fatalf("calls=%d output=%q", issuer.calls, output.String())
    }
}
```

Additional tests prove completed setup makes zero calls and prints nothing,
missing public URL fails before issuance, and issuer failure returns an error
without output.

For the helper command, move environment-driven execution into
`run(lookup func(string) string, stdout, stderr io.Writer) error` and prove it
prints the same absolute URL rather than a bare token.

- [ ] **Step 2: Run focused command tests and verify RED**

Run: `GOTOOLCHAIN=local go test ./backend/cmd/rarity-api ./backend/cmd/rarity-bootstrap-token`

Expected: FAIL because the startup helper and testable command runner do not
exist.

- [ ] **Step 3: Implement startup issuance**

Define:

```go
type bootstrapTokenIssuer interface {
    IssueToken(context.Context) (string, time.Time, error)
}
```

`issueFirstRunSetup` returns immediately when complete, rejects a blank public
URL before calling the issuer, issues once, builds the URL, and writes one
warning block directly to the supplied writer. Do not pass the URL through the
normal structured logger because `_url` attributes are intentionally redacted.

In `main`, reuse one `setup.PostgresRepository` after migrations. After runtime
setup lookup and before starting HTTP/workers, call `issueFirstRunSetup` with
`os.Stdout`. Any error logs a non-secret `first_run_setup_unavailable` code and
exits.

Update `rarity-bootstrap-token` to require `RARITY_PUBLIC_URL`, call the shared
URL builder, and preserve expiry on stderr.

- [ ] **Step 4: Run focused command tests and verify GREEN**

Run: `GOTOOLCHAIN=local go test ./backend/cmd/rarity-api ./backend/cmd/rarity-bootstrap-token`

Expected: PASS.

- [ ] **Step 5: Commit startup issuance**

Stage only Task 2 files and commit:

```text
feat(setup): print first-run setup link
```

---

### Task 3: Consume and scrub the token in the GUI

**Files:**
- Modify: `frontend/src/features/setup/SetupPage.tsx`
- Modify: `frontend/src/features/setup/SetupPage.test.tsx`

**Interfaces:**
- Consumes: `#/setup?bootstrap_token=<token>`.
- Produces: in-memory bootstrap authority and a scrubbed `#/setup` browser URL.
- Preserves: `Authorization: Bootstrap <token>` with no token in request body.

- [ ] **Step 1: Write failing fragment-consumption tests**

Set the URL before render and assert immediate scrubbing plus header-only
submission:

```tsx
window.history.replaceState(
  null,
  "",
  "/#/setup?bootstrap_token=bootstrap-secret",
);
render(<SetupPage />);
expect(window.location.hash).toBe("#/setup");
expect(screen.getByText("Deployment authority accepted")).toBeVisible();
expect(document.body.textContent).not.toContain("bootstrap-secret");
```

Update the setup-completion test to omit token-field entry and continue
asserting that the request body excludes the secret. Add a missing-token test
that shows deployment-output guidance and disables completion.

- [ ] **Step 2: Run the focused frontend test and verify RED**

Run: `npm --prefix frontend test -- --run src/features/setup/SetupPage.test.tsx`

Expected: FAIL because the page still renders a manual token input and does not
scrub the fragment.

- [ ] **Step 3: Implement in-memory fragment handling**

Add a focused parser:

```ts
function consumeBootstrapToken(): string {
  const [route, query = ""] = window.location.hash.split("?", 2);
  if (route !== "#/setup") return "";
  const token = new URLSearchParams(query).get("bootstrap_token")?.trim() ?? "";
  if (token) {
    window.history.replaceState(
      null,
      "",
      `${window.location.pathname}${window.location.search}#/setup`,
    );
  }
  return token;
}
```

Initialize component state from the parser, remove the password input, show a
non-secret accepted notice when present, and show deployment-output guidance
when absent. Use the state value in the authorization header. Disable
`Complete secure setup` while authority is missing.

- [ ] **Step 4: Run focused frontend tests and verify GREEN**

Run: `npm --prefix frontend test -- --run src/features/setup/SetupPage.test.tsx`

Expected: PASS with no token rendered.

- [ ] **Step 5: Commit GUI consumption**

Stage only the two Task 3 files and commit:

```text
feat(setup): consume bootstrap link in memory
```

---

### Task 4: Deployment defaults and operator documentation

**Files:**
- Modify: `.env.example`
- Modify: `scripts/generate-local-env.sh`
- Modify: `scripts/demo-preflight.sh`
- Modify: `scripts/demo_preflight_test.go`
- Modify: `infrastructure/compose/compose.pilot.yaml`
- Modify: `docs/07-ui-ux/first-run-and-help.md`
- Modify: `infrastructure/README.md`

**Interfaces:**
- Produces: local default `http://127.0.0.1:18080`.
- Produces: pilot `https://${RARITY_PUBLIC_HOST}`.
- Requires: explicit browser-reachable `RARITY_PUBLIC_URL` in demo preflight.

- [ ] **Step 1: Write failing deployment contract tests**

Extend `scripts/demo_preflight_test.go` to require
`RARITY_PUBLIC_URL=https://rarity.example` and reject its absence. Extend the
pilot Compose contract test to assert:

```go
"RARITY_PUBLIC_URL: https://${RARITY_PUBLIC_HOST}"
```

- [ ] **Step 2: Run deployment tests and verify RED**

Run: `GOTOOLCHAIN=local go test ./scripts ./infrastructure/compose`

Expected: FAIL because the environment contracts do not expose the public URL.

- [ ] **Step 3: Implement deployment configuration and docs**

Add `RARITY_PUBLIC_URL` to `.env.example`; generate the loopback local value;
require it in demo preflight; derive the pilot HTTPS value from the required
public host. Replace the documented manual-first-run workflow with automatic
startup output, remote copy/paste instructions, 15-minute rotation behavior,
and the warning that incomplete-installation logs temporarily contain setup
authority. Retain the helper command as explicit recovery.

- [ ] **Step 4: Run deployment and docs checks**

Run: `GOTOOLCHAIN=local go test ./scripts ./infrastructure/compose`

Run: `node scripts/validate-docs.mjs`

Expected: both PASS.

- [ ] **Step 5: Commit deployment contracts**

Stage only Task 4 files and commit:

```text
docs(setup): wire remote first-run URL
```

---

### Task 5: Full verification and live browser proof

**Files:**
- Modify only if a regression is found in an in-scope file.

**Interfaces:**
- Verifies all prior task contracts as one integrated feature.

- [ ] **Step 1: Run backend and static checks**

Run: `GOTOOLCHAIN=local go test ./backend/... ./scripts ./infrastructure/compose`

Run: `GOTOOLCHAIN=local go vet ./backend/...`

Run: `git diff --check`

Expected: PASS.

- [ ] **Step 2: Run the frontend gate**

Run: `npm --prefix frontend test -- --run`

Run: `npm --prefix frontend run build`

Run from `frontend`: `npx playwright test`

Expected: all PASS.

- [ ] **Step 3: Run documentation validation**

Run: `node scripts/validate-docs.mjs`

Expected: documentation contract PASS.

- [ ] **Step 4: Perform local browser QA**

Open a synthetic setup link containing a test token. Confirm the visible URL is
immediately scrubbed to `#/setup`, the page shows deployment authority accepted,
the secret is absent from the rendered DOM, missing-token guidance is clear,
and narrow/desktop layouts remain usable. Check browser logs for secret
material.

- [ ] **Step 5: Record the acceptance boundary**

Report local automated/browser evidence separately from deployed HTTPS proof.
Do not claim cross-device live acceptance until a real HTTPS installation is
started with a browser-reachable `RARITY_PUBLIC_URL` and completed from another
device.
