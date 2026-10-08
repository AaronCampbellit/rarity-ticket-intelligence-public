# Authenticated Application Boundary Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Implemented and verified on 2026-08-03. Frontend unit, production
build, backend, and Playwright acceptance passed. The aggregate verifier reached
Compose configuration after passing its earlier checks, but local Compose
verification remains environment-pending because Docker is not installed.

**Goal:** Replace the signed-out product preview with a fail-closed, authentication-mode-aware login boundary that never mounts protected Rarity UI before authentication.

**Architecture:** Resolve public setup status before attempting principal resolution, represent the result as an explicit access state, and render public access screens separately from the authenticated `AppShell`. Keep safe deep-link state in session storage across Microsoft redirects, monitor protected API responses for session expiry, and make Entra callback failures return to the branded login screen.

**Tech Stack:** React 19, TypeScript 7, Vitest, Testing Library, Playwright, Go 1.26, `net/http`, existing Rarity design-system tokens and components.

## Global Constraints

- Never mount the application shell or protected feature components while setup or principal state is loading, failed, or unauthenticated.
- A completed Entra installation shows Microsoft sign-in as the only normal login action.
- A completed Entra-free installation shows the local administrator form on the normal login page.
- Local recovery remains available at `#/break-glass` but is not linked or advertised from the Entra login page.
- First-run setup remains public only until bootstrap commits.
- Only recognized internal protected hashes may be retained as post-login return destinations.
- Existing backend authentication, authorization, CSRF, session, throttling, audit, and allowed-network enforcement remains authoritative.
- The approved compact Rarity logo remains visible at 320 CSS pixels.

---

### Task 1: Access resolution and safe return routing

**Files:**
- Create: `frontend/src/features/auth/applicationAccess.ts`
- Create: `frontend/src/features/auth/applicationAccess.test.ts`
- Modify: `frontend/src/api/browserSession.ts`
- Modify: `frontend/src/api/browserSession.test.ts`
- Modify: `frontend/src/navigation.ts`
- Modify: `frontend/src/navigation.test.ts`

**Interfaces:**
- Consumes: `loadPrincipalNavigation(signal?: AbortSignal)` and the public `/api/v1/setup/status` response.
- Produces: `loadApplicationAccess(signal?: AbortSignal): Promise<ApplicationAccess>`, `isAuthenticationError(error): boolean`, `rememberReturnHash(hash, catalogEnabled): void`, `takeReturnHash(catalogEnabled): string | undefined`, and a recognized `login` page.

- [x] **Step 1: Write failing setup-status and typed HTTP-error tests**

Add browser-session tests proving that `loadSetupStatus()` returns all public
capability fields and that a 401 from principal resolution can be distinguished
from a 500:

```ts
expect(await loadSetupStatus()).toEqual({
  completed: true,
  bootstrap_available: false,
  entra_available: true,
});
await expect(loadPrincipalNavigation()).rejects.toMatchObject({ status: 401 });
```

- [x] **Step 2: Run the focused API tests and verify RED**

Run: `npm --prefix frontend test -- src/api/browserSession.test.ts`

Expected: FAIL because `loadSetupStatus` and a status-bearing request error do
not exist.

- [x] **Step 3: Implement the public setup client and request error**

Add:

```ts
export type SetupStatus = {
  completed: boolean;
  bootstrap_available: boolean;
  entra_available: boolean;
};

export class BrowserRequestError extends Error {
  constructor(
    readonly resource: string,
    readonly status: number,
  ) {
    super(`${resource}_${status}`);
  }
}

export function isAuthenticationError(error: unknown): boolean {
  return error instanceof BrowserRequestError && error.status === 401;
}
```

Use `BrowserRequestError` for failed principal requests and implement
`loadSetupStatus(signal)` with same-origin credentials.

- [x] **Step 4: Run the focused API tests and verify GREEN**

Run: `npm --prefix frontend test -- src/api/browserSession.test.ts`

Expected: PASS.

- [x] **Step 5: Write failing access-state and return-target tests**

Cover these literal outcomes in `applicationAccess.test.ts` and
`navigation.test.ts`:

```ts
await expect(resolve()).resolves.toEqual({
  kind: "setup",
  entraAvailable: false,
});
await expect(resolve()).resolves.toMatchObject({
  kind: "unauthenticated",
  entraAvailable: true,
});
expect(safeReturnHash("#/sales?workRecordID=abc", false)).toBe(
  "#/sales?workRecordID=abc",
);
expect(safeReturnHash("#/login", false)).toBeUndefined();
expect(safeReturnHash("#/break-glass", false)).toBeUndefined();
expect(safeReturnHash("#/setup", false)).toBeUndefined();
expect(safeReturnHash("#/unknown", false)).toBeUndefined();
expect(safeReturnHash("https://evil.example", false)).toBeUndefined();
```

Also prove a principal 500 rejects access resolution while a principal 401
produces the unauthenticated state.

- [x] **Step 6: Run access and navigation tests and verify RED**

Run: `npm --prefix frontend test -- src/features/auth/applicationAccess.test.ts src/navigation.test.ts`

Expected: FAIL because the access resolver, login page, and safe hash helpers do
not exist.

- [x] **Step 7: Implement access resolution and safe return storage**

Define:

```ts
export type ApplicationAccess =
  | { kind: "setup"; entraAvailable: boolean }
  | { kind: "unauthenticated"; entraAvailable: boolean }
  | {
      kind: "authenticated";
      entraAvailable: boolean;
      principal: PrincipalNavigation;
    };
```

`loadApplicationAccess` must load setup first, skip `/api/v1/me` when setup is
incomplete, translate only principal 401 into `unauthenticated`, and propagate
network and server failures. Add `login` to `Page`, recognize it in
`pageFromHash`, and store return hashes under
`rarity.post-authentication-return`.

- [x] **Step 8: Run Task 1 tests and verify GREEN**

Run: `npm --prefix frontend test -- src/api/browserSession.test.ts src/features/auth/applicationAccess.test.ts src/navigation.test.ts`

Expected: PASS.

- [x] **Step 9: Commit Task 1**

```bash
git add frontend/src/api/browserSession.ts frontend/src/api/browserSession.test.ts frontend/src/features/auth/applicationAccess.ts frontend/src/features/auth/applicationAccess.test.ts frontend/src/navigation.ts frontend/src/navigation.test.ts
git commit -m "Add application access resolution"
```

### Task 2: Dedicated public authentication screens

**Files:**
- Create: `frontend/src/features/auth/PublicAccessPage.tsx`
- Create: `frontend/src/features/auth/PublicAccessPage.test.tsx`
- Modify: `frontend/src/app.css`

**Interfaces:**
- Consumes: `RarityBrand`, `loginLocalAdministrator(username, password)`, `entraAvailable`, public state, and `onLocalAuthenticated`.
- Produces: `PublicAccessPage` for `loading`, `error`, `login`, `recovery`, and `setup` presentation without `AppShell`.

- [x] **Step 1: Write failing public-screen behavior tests**

Test the real component and assert:

- loading renders the Rarity image and `Loading Rarity` status without
  navigation;
- request failure renders `Rarity is unavailable` and Retry;
- Entra login renders one `Sign in with Microsoft` link to
  `/auth/entra/login`, no username field, and no local-login link;
- local-only login renders username/password fields and no Microsoft link;
- recovery renders the local form regardless of Entra availability;
- rejected local login announces `Authentication failed.` without disclosing a
  reason; and
- successful local login calls `onLocalAuthenticated`.

- [x] **Step 2: Run the component test and verify RED**

Run: `npm --prefix frontend test -- src/features/auth/PublicAccessPage.test.tsx`

Expected: FAIL because `PublicAccessPage` does not exist.

- [x] **Step 3: Implement the public access component**

Use one full-page `<main id="main-content" className="public-access">`, place
`<RarityBrand variant="full" />` above the content card, and keep the local form
state inside a focused `LocalAdministratorLogin` child. Use:

```tsx
<a className="public-access__primary" href="/auth/entra/login">
  Sign in with Microsoft
</a>
```

Do not render an `AppShell`, primary navigation, topbar, build preview, local
recovery URL, or product copy on the Entra login branch.

- [x] **Step 4: Add responsive public-screen styling**

Add token-based styles for `.public-access`, `.public-access__brand`,
`.public-access__card`, form fields, status/error copy, and primary actions.
At `max-width: 520px`, use the compact symbol asset through CSS/component
composition while preserving an accessible Rarity name. Keep the card within
`calc(100vw - 32px)` and all controls at least 44 CSS pixels high.

- [x] **Step 5: Run the component test and verify GREEN**

Run: `npm --prefix frontend test -- src/features/auth/PublicAccessPage.test.tsx`

Expected: PASS with no React warnings.

- [x] **Step 6: Commit Task 2**

```bash
git add frontend/src/features/auth/PublicAccessPage.tsx frontend/src/features/auth/PublicAccessPage.test.tsx frontend/src/app.css
git commit -m "Add dedicated Rarity login screens"
```

### Task 3: Fail-closed application composition

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`

**Interfaces:**
- Consumes: `loadApplicationAccess`, `PublicAccessPage`, safe return helpers,
  authenticated principal navigation/capabilities, and the existing protected
  feature components.
- Produces: a top-level public boundary plus `AuthenticatedWorkspace`, which is
  the only component allowed to mount `AppShell` and protected features.

- [x] **Step 1: Replace preview expectations with failing boundary tests**

Build complete public response fixtures for `/api/v1/setup/status` and
`/api/v1/me`. Prove:

- unresolved access displays only loading and no `Primary` navigation or demo
  opportunity;
- signed-out Entra requests for `#/sales` become `#/login`, retain
  `#/sales`, and show Microsoft only;
- signed-out local-only requests show the local form;
- `#/break-glass` remains recovery-only;
- incomplete setup renders Setup without the product shell;
- setup/principal network errors render Retry without the product shell;
- unknown signed-out hashes become login without storing a return hash;
- authenticated principals mount the shell and land on a retained authorized
  route;
- authenticated principals requesting a disallowed retained route land on the
  first permitted route; and
- authenticated visits to login or recovery redirect to the first permitted
  route.

- [x] **Step 2: Run App tests and verify RED**

Run: `npm --prefix frontend test -- src/App.test.tsx`

Expected: FAIL because the current app renders the shell and demo records before
and after authentication failure.

- [x] **Step 3: Split public and authenticated composition**

Make `App` own only access resolution, Retry, hash classification, return-target
storage, and selection between public setup/login/error/loading and
`AuthenticatedWorkspace`. Move existing shell state and protected page
composition into `AuthenticatedWorkspace`, initialized from the resolved
principal.

Delete all unauthenticated demo fallbacks and remove signed-out props/branches
that existed solely for preview rendering. No protected feature component may
appear in the public return paths.

- [x] **Step 4: Implement deterministic redirects**

Use `window.location.replace("#/login")` semantics for public redirects so
Back does not reveal a protected intermediate state. After access resolves as
authenticated, consume the stored safe return hash once; allow it only when the
principal navigation contains the page, otherwise navigate to the first
permitted page.

On local-login success, rerun access resolution in place instead of relying on
an unverified full-page reload.

- [x] **Step 5: Run App tests and verify GREEN**

Run: `npm --prefix frontend test -- src/App.test.tsx`

Expected: PASS with the shell absent from every public state.

- [x] **Step 6: Run the complete frontend unit suite**

Run: `npm --prefix frontend test`

Expected: PASS with zero failed tests.

- [x] **Step 7: Commit Task 3**

```bash
git add frontend/src/App.tsx frontend/src/App.test.tsx
git commit -m "Gate Rarity behind authentication"
```

### Task 4: Logout and first-401 session expiry

**Files:**
- Create: `frontend/src/api/sessionExpiry.ts`
- Create: `frontend/src/api/sessionExpiry.test.ts`
- Modify: `frontend/src/main.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`

**Interfaces:**
- Consumes: the browser's original `fetch`, protected `/api/v1/*` responses,
  and the top-level application access transition.
- Produces: `createSessionAwareFetch(originalFetch)` and the
  `rarity:session-expired` browser event.

- [x] **Step 1: Write failing fetch-monitor tests**

Use a real `Response` and prove:

```ts
const monitored = createSessionAwareFetch(fakeFetch);
await monitored("/api/v1/work-records");
expect(expiredEvents).toBe(1);
```

Assert one event for a protected 401, no event for 403 or 500, and no event for
401 from `/api/v1/me` or `/api/v1/setup/status`.

- [x] **Step 2: Run the monitor test and verify RED**

Run: `npm --prefix frontend test -- src/api/sessionExpiry.test.ts`

Expected: FAIL because the monitored fetch does not exist.

- [x] **Step 3: Implement and install the monitored fetch**

Capture the original browser fetch once in `main.tsx`, wrap it with
`createSessionAwareFetch`, preserve native `RequestInfo | URL` and `RequestInit`
semantics, and dispatch one `CustomEvent("rarity:session-expired")` per protected
401 response. The App transition is idempotent, so concurrent responses settle
on the same login state.

- [x] **Step 4: Write failing App session-transition tests**

Prove that:

- the first `rarity:session-expired` event stores the current protected hash,
  unmounts the shell, and routes to login;
- repeated expiry events do not change the stored return destination;
- successful logout routes to login without retaining the signed-out route; and
- failed logout still clears local authenticated presentation.

- [x] **Step 5: Run App tests and verify RED**

Run: `npm --prefix frontend test -- src/App.test.tsx`

Expected: FAIL because logout and expiry currently leave the signed-out shell
visible.

- [x] **Step 6: Implement logout and expiry transitions**

Listen for `rarity:session-expired` only while authenticated. Preserve the
current safe protected hash for expiry, clear authenticated client/capability
state, and enter the public login state once. For explicit logout, clear any
stored return destination before entering login in the `finally` branch.

- [x] **Step 7: Run Task 4 tests and verify GREEN**

Run: `npm --prefix frontend test -- src/api/sessionExpiry.test.ts src/App.test.tsx`

Expected: PASS.

- [x] **Step 8: Commit Task 4**

```bash
git add frontend/src/api/sessionExpiry.ts frontend/src/api/sessionExpiry.test.ts frontend/src/main.tsx frontend/src/App.tsx frontend/src/App.test.tsx
git commit -m "Handle browser session expiry at the app boundary"
```

### Task 5: Entra callback recovery

**Files:**
- Modify: `backend/internal/browserauth/entra.go`
- Modify: `backend/internal/browserauth/entra_test.go`
- Modify: `frontend/src/features/auth/PublicAccessPage.test.tsx`

**Interfaces:**
- Consumes: Entra callback error branches and the public `#/login` screen.
- Produces: same-origin redirects to `/#/login?error=entra` or
  `/#/login?error=unavailable`, plus safe retry copy on the login page.

- [x] **Step 1: Write failing callback redirect tests**

Add table-driven Go tests for missing OIDC cookie, provider `error`, invalid
state, failed token exchange, identity rejection, session issuance failure, and
CSRF generation failure. Authentication failures must return:

```go
if response.Code != http.StatusFound ||
  response.Header().Get("Location") != "/#/login?error=entra" {
  t.Fatalf("status=%d location=%q", response.Code, response.Header().Get("Location"))
}
```

Operational issuance/random failures must use
`/#/login?error=unavailable`. Every response remains `Cache-Control: no-store`
and contains no provider error detail.

- [x] **Step 2: Run backend browser-auth tests and verify RED**

Run: `go test ./backend/internal/browserauth -run 'TestEntraCallback' -count=1`

Expected: FAIL because callback errors currently return raw 401/503 bodies.

- [x] **Step 3: Implement safe callback redirects**

Add one private helper that expires the transient OIDC cookie and redirects to
the fixed same-origin login hash. Do not reflect any request query or provider
message into the destination.

- [x] **Step 4: Add and run the failing login-error UI test**

Render the Entra login state at `#/login?error=entra` and expect an alert saying
`Microsoft sign-in was not completed. Try again.` Render
`error=unavailable` and expect `Microsoft sign-in is temporarily unavailable.`

Run: `npm --prefix frontend test -- src/features/auth/PublicAccessPage.test.tsx`

Expected: FAIL because callback error query handling does not exist.

- [x] **Step 5: Implement safe login-error copy**

Parse only the literal `entra` and `unavailable` values from the login hash.
Ignore all other query values and never render arbitrary provider text.

- [x] **Step 6: Run Task 5 tests and verify GREEN**

Run:

```bash
go test ./backend/internal/browserauth -count=1
npm --prefix frontend test -- src/features/auth/PublicAccessPage.test.tsx
```

Expected: PASS.

- [x] **Step 7: Commit Task 5**

```bash
git add backend/internal/browserauth/entra.go backend/internal/browserauth/entra_test.go frontend/src/features/auth/PublicAccessPage.test.tsx frontend/src/features/auth/PublicAccessPage.tsx
git commit -m "Return Entra failures to the login page"
```

### Task 6: Browser acceptance and full verification

**Files:**
- Modify: `frontend/tests/e2e/runnable_foundation.spec.ts`
- Modify: `docs/superpowers/specs/2026-08-03-authenticated-application-boundary-design.md`
- Modify: `docs/superpowers/plans/2026-08-03-authenticated-application-boundary.md`

**Interfaces:**
- Consumes: the completed public boundary, mocked setup/principal endpoints, and
  the existing Vite Playwright server.
- Produces: executable browser proof for signed-out, setup, authenticated, and
  narrow-width behavior.

- [x] **Step 1: Write failing Playwright boundary cases**

Route `/api/v1/setup/status`, `/api/v1/me`, `/api/v1/directory`, and
`/v1/system/build` explicitly. Add cases proving:

- signed-out Entra mode at `#/sales` shows Microsoft login and no primary nav,
  local form, build label, or Northwind preview;
- signed-out local mode shows the local form and no Microsoft action;
- incomplete setup shows the setup experience and no primary navigation;
- an authenticated principal shows the permitted shell route; and
- at a 320-by-700 viewport, a visible Rarity logo and fully contained login card
  remain present with no horizontal scroll.

- [x] **Step 2: Run Playwright and verify RED**

Run: `npm --prefix frontend exec playwright test frontend/tests/e2e/runnable_foundation.spec.ts`

Expected: at least the signed-out shell assertion fails against the old
behavior.

- [x] **Step 3: Adjust only integration defects exposed by Playwright**

For each failure, first add or tighten the corresponding unit regression test,
verify it fails, then make the smallest production adjustment and rerun the
focused test before rerunning Playwright.

- [x] **Step 4: Run formatting, frontend tests, backend tests, and build**

Run:

```bash
npm --prefix frontend run format
npm --prefix frontend test
npm --prefix frontend run build
go test ./backend/...
```

Expected: all commands exit 0 with zero failed tests.

- [x] **Step 5: Run browser acceptance**

Run: `npm --prefix frontend exec playwright test`

Expected: all Playwright cases pass.

- [x] **Step 6: Run repository verification**

Run: `make verify`

Expected: formatting, Go tests, frontend tests, and Compose configuration all
exit 0. If Docker is unavailable, record Compose verification as pending
instead of treating unit/build proof as deployment acceptance.

- [x] **Step 7: Update design and plan status**

Set the design status to `Implemented and verified on 2026-08-03`. Check every
completed plan checkbox. Record any environment-dependent verification boundary
in the plan without weakening the acceptance criteria.

- [x] **Step 8: Review the final diff and commit**

Run:

```bash
git diff --check
git status --short
git diff --stat
```

Then commit only intentional changes:

```bash
git add frontend backend/internal/browserauth docs/superpowers
git commit -m "Complete authenticated application boundary"
```

- [x] **Step 9: Finish the branch**

Use `superpowers:finishing-a-development-branch`, verify the full suite again,
merge the implementation into the approved target branch, and push only after
the finishing workflow confirms the exact branch and clean state.
