# Guided Setup Center Design

**Status:** Approved for implementation on 2026-08-04

## Goal

Replace the Setup Center's generic JSON reference editor with an administrator-
friendly workflow that explains what is being configured, collects only
meaningful named values, performs the setup Rarity can safely own, and verifies
host-managed prerequisites before calling them complete.

An administrator must not need to understand Rarity's internal JSON shape,
secret-reference syntax, or database representation.

## Approaches considered

### Guided hybrid setup — selected

Each readiness card opens a dedicated plain-English workflow. Rarity directly
configures application-owned integrations through their existing protected APIs.
Host-owned services show exact deployment steps and values, then use live checks
to confirm the running installation sees them.

This gives administrators a useful setup experience without granting the web
process authority to rewrite host environment files, Compose definitions, or
pgBackRest configuration.

### Typed fields that still save reference records

The frontend could translate friendly fields into the existing arbitrary maps.
This is smaller, but it would still mark services configured because metadata
was saved rather than because the service works. That repeats the current
problem behind a nicer form and is not acceptable.

### Full infrastructure control plane

Rarity could store every credential and rewrite or restart its own deployment.
This would make more steps automatic, but it materially expands web-process
privileges and creates a second deployment manager. It is outside the current
security and operating model.

## Admin workflow

The Setup Center retains its readiness-card overview. Card states become:

- **Not started:** required values or resources are absent.
- **Action required:** an administrator must complete a described step.
- **Ready to verify:** configuration is present but has not passed a live check.
- **Verified:** the latest live check passed.
- **Attention:** configuration exists but its health check failed or is stale.

Opening a card shows only that card's workflow. Every workflow includes:

1. A short explanation of what the service does and why Rarity needs it.
2. A prerequisites checklist with links to the relevant provider or Rarity
   administration surface.
3. Plain-English labeled fields with examples and field-level helper text.
4. A clear warning beside any write-only credential field.
5. A **Test configuration** action and a result that names the failing step.
6. A reason field only when an audited mutation is actually performed.
7. A final next step; saving metadata alone never produces **Verified**.

Raw JSON and secret-reference URI syntax are removed from the browser UI.

## Mailbox and API intake

The card offers two setup paths.

### Microsoft 365 mailbox

The workflow explains the required Entra application permissions and links to
the existing Graph mailbox administration surface. It collects:

- A recognizable connection name.
- Mailbox email address.
- Microsoft tenant ID.
- Application/client ID.
- Client secret in a write-only field.
- Folder to monitor, defaulting to Inbox.

The notification URL is generated from Rarity's public URL and displayed with a
copy action and instructions for the Entra application. Saving uses the existing
purpose-encrypted Graph mailbox APIs. The test verifies credentials, mailbox
access, folder access, and notification readiness without exposing the secret.

### API intake

The workflow explains when a service API key or forwarding intake should be
used and links to those existing administration surfaces. Completion is derived
from an active scoped key or a healthy forwarding configuration, not from a
manually entered reference.

The card is **Verified** when at least one chosen intake path has a successful,
current health result. Individual paths remain visible so an administrator can
add both.

## Object storage

Object storage remains deployment-managed because the running API receives its
S3-compatible storage configuration at process startup.

The workflow asks whether the administrator uses Amazon S3, MinIO, or another
S3-compatible provider, then explains how to create a dedicated bucket and a
least-privilege application credential. Named fields show the effective,
non-secret runtime values:

- Endpoint.
- Bucket name.
- Region.
- Access-key identifier.
- Credential source status, shown only as configured or missing.

Helper text shows the corresponding deployment setting names without asking the
administrator to type `env://` references. Secret values are never returned.
If a value is absent, the UI shows the exact Compose or environment setting to
add and explains that the Rarity API must be restarted.

**Test configuration** asks the running storage adapter to perform a bounded
bucket-access check using the effective runtime configuration. It verifies TLS,
bucket existence, and the minimum read/write/delete permissions using a
temporary probe object. Cleanup is mandatory, and failures return safe,
step-specific messages.

The card becomes **Verified** only after that probe succeeds.

## Backup and PITR

Backup configuration remains host-managed because pgBackRest, PostgreSQL WAL
archiving, repository encryption, and restore operations must stay outside the
web process's authority.

The workflow explains the separate-failure-domain requirement and presents the
supported pgBackRest settings as named checklist items:

- Repository endpoint, bucket, and region.
- Dedicated repository credential configured.
- Independent repository cipher passphrase configured.
- Rarity stanza name.
- WAL archiving enabled.
- Backup schedule installed.

The UI provides copyable commands from the documented deployment profile, but
never executes privileged host commands. A deployment operator runs a
read-only CLI verification command that submits signed, non-secret evidence to
Rarity or uploads the generated evidence file through the authenticated UI.

Verification must prove:

- pgBackRest reports the expected stanza and repository.
- The latest backup is within the documented freshness window.
- WAL archiving is healthy.
- A restore/PITR verification has been recorded within the required interval.

The card distinguishes **backup current** from **restore proof current**. It
cannot become **Verified** from an administrator checkbox or saved references.

## API and persistence

The arbitrary-map update endpoint is retired from the browser workflow.
Existing stored maps remain readable during migration but no longer determine
readiness by their non-empty state.

Setup Center status is composed from the authoritative application surfaces:

- Graph mailboxes, service keys, and forwarding health for intake.
- Effective runtime storage configuration plus the latest storage probe.
- Signed backup/restore evidence plus freshness policy for backup and PITR.

New verification mutations are reason-required where they change durable state,
optimistically versioned, authorized with `organization.manage`, audited, and
published through the outbox. Probe results persist safe status, timestamps,
and error codes only. Credentials, tokens, command output containing paths, and
provider response bodies are not retained or returned.

Concurrent changes return a conflict message that tells the administrator to
refresh. Validation errors appear beside the relevant field; operational
failures preserve entered non-secret values and provide a safe remediation
step.

## Components

The frontend separates the overview from three focused workflows:

- `SetupCenterOverview` owns cards, status, and refresh.
- `IntakeSetupPanel` orchestrates existing integration surfaces.
- `ObjectStorageSetupPanel` renders effective settings and the live probe.
- `BackupSetupPanel` renders operator steps and verification evidence.

Shared `SetupStep`, `SetupFieldHelp`, `SecretFieldNotice`, and
`VerificationResult` components keep language, accessibility, and state
presentation consistent. Each unit receives a typed contract; no component
serializes arbitrary configuration objects.

## Accessibility and responsive behavior

Each card action opens an inline region headed by the card name and moves focus
to that heading. Steps use ordered lists and native form controls. Helper and
error text is programmatically associated with its field. Status is conveyed by
text as well as color, and verification updates use a polite live region.

At desktop width, the workflow may use a two-column instructions-and-form
layout. Below the existing narrow breakpoint it becomes one column with actions
after the fields. No horizontally scrolling code or JSON editor is present.

## Verification

Automated coverage includes:

- No JSON textareas or secret-reference syntax in the completed-installation UI.
- Correct helper text, labels, focus behavior, and field-level errors.
- Mailbox/API flows call only the established protected integration APIs.
- Storage probes reject missing configuration, clean up probe objects, redact
  provider details, and persist only safe evidence.
- Backup evidence rejects unsigned, mismatched, expired, or replayed results.
- Readiness states derive from live/evidence freshness rather than saved maps.
- Permission, CSRF, audit, optimistic-concurrency, and outbox contracts.

Rendered acceptance covers all three workflows at full desktop width and below
the narrow breakpoint. It verifies useful instructions, keyboard operation,
non-secret error messages, successful and failing test states, no runtime
overlays or console errors, and screenshots. Live acceptance uses the demo
deployment and does not alter production infrastructure.
