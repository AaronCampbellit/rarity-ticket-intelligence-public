# First Run and Help Center

**Status:** Accepted V1 scope

The first-run GUI wizard guides an authorized administrator through organization identity, creation of the first local Platform Administrator, optional Entra configuration, mailbox/API intake, object storage, and backup/PITR configuration. The wizard validates prerequisites, explains security impact, avoids exposing secrets after entry, and records every accepted configuration action.

The same setup surface remains available after onboarding as an always-available setup/help center. It shows configuration state, health, missing requirements, backup status, integration status, safe remediation links, and role-sensitive documentation. It is not a hidden one-time installer.

## Bootstrap authority

The deployment operator configures `RARITY_PUBLIC_URL` as the absolute address
that administrators use from their own browsers. It must use HTTPS outside
loopback development. On every startup before setup is complete, the API
automatically issues a high-entropy token and prints one absolute setup URL and
its expiry to deployment output. The URL uses `RARITY_PUBLIC_URL`, not the API
bind address or `localhost`, so an operator can copy it from an SSH or remote
console and open it on another workstation.

The token is carried after the URL fragment marker (`#`), so it is not sent to
the web server or reverse-proxy access log. The setup GUI copies it into
component memory and immediately removes it from the browser address and
history. It is never stored in browser storage or cookies.

Issuing a token invalidates any prior unused token. An incomplete-installation
restart therefore rotates the setup URL. Only a SHA-256 hash is stored, and the
token expires after 15 minutes. Startup output is secret-bearing until that
token is consumed, rotated, or expired. Completed installations never issue or
print bootstrap authority.

For explicit recovery or rotation, the deployment operator may run
`go run ./backend/cmd/rarity-bootstrap-token` with `DATABASE_URL` and
`RARITY_PUBLIC_URL` set. The helper prints the same absolute setup URL contract;
it is not required during normal installation.

The first-run GUI sends the in-memory token in the
`Authorization: Bootstrap ...` header over HTTPS. A successful serializable
transaction consumes it and creates the
single MSP organization, local Platform Administrator, setup configuration
evidence, audit record, and outbox event. The MSP
UUID must match the deployment's `RARITY_MSP_ID`. After the transaction commits,
bootstrap is permanently unavailable. Entra may be skipped entirely. When a
complete Entra group is supplied, its client secret is encrypted with the
installation secret provider and is never returned. Runtime setup always starts
local authentication and starts Entra authentication only for an active,
complete configuration. The route then becomes the authenticated Setup Center.

Other provider credentials remain in the deployment secret provider. The
authenticated Setup Center does not expose JSON or secret-reference syntax. It
derives intake readiness from active Graph mailboxes, service keys, and
forwarding connections; shows effective non-secret storage values; and explains
host-managed pgBackRest steps in plain language. Public setup status
reveals only whether bootstrap is complete and whether an Entra sign-in action
is available. The authenticated Setup Center
distinguishes configuration from live verification, reports exact missing
requirements, and links to the administration surface that owns each change.

Object storage is verified with one temporary probe object that is immediately
deleted. Backup/PITR readiness comes from installation-bound, short-lived,
single-use signed CLI evidence. Neither saved metadata nor an administrator
checkbox can produce a verified state.
