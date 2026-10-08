# Authenticated Application Boundary Design

**Status:** Implemented and verified on 2026-08-03

## Goal

Rarity must never expose its application shell, navigation, preview records, or
product routes to an unauthenticated browser. After installation completes,
every signed-out visit resolves to a dedicated login experience appropriate to
the installation's active authentication mode.

This design replaces the current signed-out product preview and supersedes the
signed-out GUI behavior in the Optional Entra and Local Platform Administrator
design. It does not change the requirement that every installation retain an
enabled local Platform Administrator.

## Public and protected routes

The frontend has one application-level authentication boundary. Before it
renders any protected route, it resolves setup status and the current
principal. While either request is unresolved, the browser displays only a
neutral full-page loading state containing the Rarity brand. It does not mount
the application shell or protected page components behind the loading state.

Only these signed-out experiences are public:

- first-run setup while bootstrap remains available;
- the normal login page;
- the local administrator recovery login page; and
- the authentication callback and signed-out completion flows required by the
  existing session handlers.

Every product, organization, integration, and administration route is
protected. Unknown routes follow the same authentication boundary and must not
fall through to a preview page.

## Routing state machine

The frontend resolves navigation in this order:

1. Load public setup status.
2. If setup is incomplete, allow the setup route and redirect every other
   application route to setup.
3. If setup is complete, resolve the current principal.
4. If a principal exists, allow protected routes and redirect login or recovery
   routes to the authenticated default page.
5. If no principal exists, allow only the applicable login and recovery routes
   and redirect all protected routes to the normal login page.

An unavailable setup-status or principal request produces a dedicated
full-page error with Retry. It must not optimistically render authenticated or
preview content.

## Authentication-mode login experience

The normal login page uses the public setup-status authentication capability:

- When Entra is active and available, the page shows **Sign in with Microsoft**
  as its only authentication action.
- When Entra is not active, the page shows the local administrator username and
  password form.

An Entra-configured login page does not link to, advertise, or reveal the local
administrator recovery path. The local recovery login remains reachable only
through its separate, documented URL. This keeps Microsoft as the ordinary
workforce entry point while preserving durable operator recovery when Entra is
unavailable.

The login and recovery pages use the shared Rarity brand, typography, controls,
validation, and responsive design tokens, but not the application sidebar,
topbar, navigation, product titles, or product data.

## Redirect and session behavior

When a signed-out user requests a protected hash route, Rarity records that
route as an internal return destination and sends the browser to login. After a
successful Microsoft or local login, the browser returns to that route if the
principal is authorized; otherwise it opens the first permitted authenticated
route.

Return destinations are restricted to recognized internal application routes.
External URLs, protocol-relative values, malformed hashes, login routes, setup,
and recovery routes are rejected to prevent open redirects and authentication
loops.

Logout clears authenticated frontend state and immediately replaces the
current route with login. A session that expires during use follows the same
path after the first authenticated API rejection. Concurrent failures collapse
into one transition so the browser does not flash multiple errors or repeatedly
redirect.

Authentication failures remain on the relevant login page with the existing
generic error contract. Microsoft callback failures return to the Microsoft
login page with a safe, user-readable retry state. Local recovery failures do
not disclose whether the username, password, enabled state, or allowed network
caused rejection.

## Authorization behavior

Authentication controls whether the application may render. Existing
permission filtering continues to control which authenticated routes and
navigation entries a principal may use.

Deep-link authorization is evaluated after authentication. A signed-in user
without permission for the requested route sees the existing authenticated
access-denied behavior or is sent to the first permitted route; protected page
content must not mount before that decision.

The backend remains authoritative. Existing API and mutation authentication,
authorization, CSRF, session, and network restrictions are unchanged. The
frontend boundary is not treated as a security control for API access.

## Setup and recovery invariants

First-run setup must remain reachable without a session until bootstrap commits.
Once setup completes, setup no longer acts as a signed-out entry point.
Authenticated setup administration continues through the existing protected
Setup Center.

The recovery URL remains stable across Entra activation, Entra outage, and
normal deployments. It uses the same local login endpoint, secure session
cookie, throttling, auditing, generic errors, and allowed-network enforcement as
today. A successful recovery login enters the normal authenticated application
with the local Platform Administrator principal.

## Accessibility and responsive behavior

Login, recovery, loading, and error states must work at 320 CSS pixels, browser
zoom and reflow, keyboard-only navigation, touch input, visible focus,
screen-reader labeling, reduced motion, high contrast, and light/dark system
preferences supported by the application.

The full Rarity wordmark is preferred when space permits. The compact approved
logo remains visible at narrow widths, so reducing browser width never removes
application identity.

## Verification

Automated coverage must prove:

- protected components and the application shell never mount while setup or
  principal state is loading, failed, or unauthenticated;
- completed Entra installations show only Microsoft sign-in on the normal login
  page;
- completed Entra-free installations show the local login form;
- the separate recovery URL remains usable and unlinked from the Entra login
  page;
- incomplete installations route to setup and completed installations cannot
  use setup as a signed-out bypass;
- valid protected deep links survive login, while unsafe or unauthorized return
  destinations do not;
- logout, session expiry, concurrent 401 responses, callback failure, request
  failure, and Retry all settle into one deterministic public state;
- unknown hashes do not render the Sales demo or another protected fallback;
- permission-restricted principals cannot mount unauthorized protected pages;
  and
- the public screens preserve accessible focus, labeling, error announcement,
  logo visibility, and responsive layout.

Live browser acceptance covers Entra and local-only installations, Microsoft
login, local recovery login, logout, an expired session, authorized and
unauthorized deep links, setup before and after bootstrap, network-rejected
local recovery, loading/error states, narrow mobile width, and browser console
and network health.
