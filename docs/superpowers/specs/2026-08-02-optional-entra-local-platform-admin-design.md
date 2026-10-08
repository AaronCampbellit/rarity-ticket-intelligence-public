# Optional Entra and Local Platform Administrator Design

**Status:** Approved design

## Purpose

Rarity must be installable, evaluable, and administrable without Microsoft
Entra credentials. Entra is an optional workforce identity integration that can
be configured later; it is not bootstrap authority and it does not replace the
installation's local administrative access.

## Accepted authentication model

- Bootstrap creates the organization and its first local Platform
  Administrator.
- Additional local accounts may be created, but every local account is a
  Platform Administrator. Local accounts cannot receive ordinary workforce
  roles.
- Local authentication remains available after Entra is activated. Entra
  activation never silently disables or deletes local accounts.
- Entra-backed users continue to receive normal workforce roles through the
  existing Entra identity and authorization model.
- At least one enabled local Platform Administrator must always remain. This is
  both the normal Entra-free administration path and the durable recovery path.
- Local credentials remain password-hashed, network-restricted, session-bound,
  throttled by the existing authentication boundary, and fully audited.

The product uses the terms **Local administrator**, **Local administrator
sign-in**, and **Local Platform Administrators**. Existing break-glass records
are migrated without changing passwords, network policies, technician links,
enabled state, or session behavior.

## First-run setup

The bootstrap request requires:

- the deployment operator's single-use token;
- the installation MSP UUID and organization name/display ID;
- the first administrator's email, display name, local username, password, and
  allowed network CIDRs; and
- syntactically valid operational configuration objects, which may initially be
  empty.

Entra is a separate optional section. The GUI presents **Configure now** and
**Skip for now**. If skipped, the request omits Entra fields and setup completes
normally. If configured, tenant ID, client ID, client secret, HTTPS redirect
URL, and the initial administrator's Entra object ID are accepted as one
all-or-nothing configuration. Partial Entra input is rejected without consuming
the bootstrap token.

The serializable bootstrap transaction creates the organization, technician,
global Platform Administrator role and assignment, local credential, setup
record, audit record, and outbox event. It creates an external Entra identity
and sealed Entra secret only when a complete Entra configuration was supplied.
Bootstrap closes permanently after the transaction commits.

## Runtime and later Entra activation

An installation setup record no longer implies that Entra is configured.
Runtime setup always loads the MSP identity and starts local authentication.
The Entra browser handler is installed only when an active, complete Entra
configuration can be decrypted.

The authenticated Identity settings surface exposes Entra as one of:

- **Not connected** — local administration is fully operational;
- **Configured, verification required** — values are recorded but not active;
- **Connected** — metadata, discovery, and a controlled sign-in validation
  passed; or
- **Action required** — credentials, discovery, callback, or secret access
  failed.

Adding or replacing Entra configuration requires `organization.manage`, a
reason, an expected version, and write-only secret input. Rarity validates the
tenant-specific discovery document, exact issuer, client metadata, redirect
URL, and a controlled sign-in before activation. Failed validation leaves the
previous active configuration unchanged. Removing or disabling Entra ends new
Entra sign-ins but does not affect local administrators.

Microsoft Graph mailbox intake has its own application credentials and health
state. The UI explains when a Microsoft integration needs Entra, but neither
Graph nor Entra is required to finish installation or use unrelated product
features.

## Local administrator management

Identity settings list local administrators with username, linked technician,
allowed networks, enabled state, last use, and version. Authorized local or
Entra Platform Administrators may:

- create another local Platform Administrator with a new technician identity;
- reset a local password through a write-only, audited mutation;
- replace allowed CIDRs with optimistic concurrency;
- disable an account when another enabled local administrator remains; and
- revoke its active sessions.

Creating a local administrator never attaches the creator's technician record
to the new credential. Usernames are normalized and unique per installation.
Passwords remain 16–72 bytes and use the existing bcrypt cost floor. Mutations
require an explicit reason and emit immutable audit and outbox facts without
credential material.

## Compatibility and migration

A forward-only migration makes Entra columns nullable and records explicit
Entra configuration state/version. Existing complete Entra installations remain
active.

Existing `break_glass_accounts` data is preserved and exposed through the local
administrator service. Code, API, audit, and UI names move to local
administrator terminology. The old `/auth/break-glass/login` and
`/api/v1/admin/break-glass-accounts` routes remain temporary compatibility
aliases for one release and carry deprecation headers; new clients use
`/auth/local/login` and `/api/v1/admin/local-administrators`.

No migration rewrites password hashes or invalidates enabled accounts. Existing
Entra-linked bootstrap administrators keep their Entra identity and gain no
additional permissions because the local credential already points to the same
Platform Administrator technician.

## Failure and security behavior

- Public setup status reveals only whether bootstrap is available/completed.
- Authentication returns one generic invalid-credentials response for unknown,
  disabled, wrong-password, and disallowed-network cases.
- Entra being absent or unhealthy never blocks local sign-in.
- A partial Entra payload, invalid redirect, secret sealing failure, expired
  bootstrap token, or MSP mismatch leaves setup uncommitted.
- The final enabled local administrator cannot be disabled.
- Passwords, bootstrap tokens, Entra secrets, and provider secrets never appear
  in responses, audit diffs, events, or logs.
- Local sessions use the same secure cookie, CSRF, rotation, expiry, revocation,
  and authenticated principal resolution as Entra sessions.

## GUI behavior

The signed-out shell shows **Local administrator sign-in** as the primary action
when Entra is not connected and shows both Microsoft and local choices when it
is connected. It never offers a Microsoft sign-in link that cannot work.

First-run setup uses progressive sections with clear completion state. Skipping
Entra marks it **Optional — not connected**, not an error. Setup Center remains
available after bootstrap and routes identity remediation to Identity settings.
The former Recovery access page becomes Local administrators and retains
keyboard, screen-reader, non-pointer, high-contrast, zoom/reflow, and touch
semantics from the authenticated design system.

## Verification

Automated coverage must prove:

- bootstrap succeeds with no Entra fields and rejects partial Entra input;
- the bootstrap token is consumed only by a committed setup;
- local sign-in provides the Platform Administrator principal and full
  authenticated navigation without Entra;
- multiple local administrators have distinct technicians and credentials;
- password, network, disable, final-admin, version-conflict, secret-redaction,
  audit, event, session, and authorization invariants;
- existing bootstrap and break-glass data migrates without credential changes;
- complete existing Entra configuration continues to load;
- later Entra validation/activation does not disrupt local sign-in; and
- the GUI exposes accurate setup, sign-in, Identity, error, and optional-state
  behavior.

Live demo acceptance must complete an Entra-free bootstrap, local administrator
sign-in, authenticated product navigation, creation of a second local
administrator, and later provider/integration configuration without any
Microsoft credential.
