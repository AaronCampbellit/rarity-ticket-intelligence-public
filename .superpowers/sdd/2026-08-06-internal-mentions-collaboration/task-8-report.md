# Task 8 Report: Recipient-Specific Mention Alerts

## Outcome

Implemented recipient-specific email and Teams notification delivery for internal mentions, self-service recipient preferences, the Service Desk Mentions policy preset, delivery-time reauthorization, and recipient-aware delivery deduplication.

The dashboard widget remains the only in-app mention surface. Mention policies and planner logic reject or ignore `in_app` and `webhook`; external alerts contain no internal source body, preview, target label, or note text.

## Backend implementation

- Generalized planning and delivery records with typed subject, occurrence, and exact recipient IDs.
- Loaded `mention.occurred` directly through collaboration source, occurrence, eligible recipient resolution, current mention item, and access joins.
- Grouped recipient rows by outbox event so all recipient/channel deliveries and the event-plan marker commit atomically.
- Deduplicated direct/team overlap and multiple policy destinations to one delivery per event, recipient, and channel.
- Applied organization policy before recipient preference; unsupported channels cannot be enabled by a recipient.
- Added validated IANA time zones and paired local `HH:MM` quiet hours, including deterministic overnight and DST evaluation.
- Preserved critical quiet-hour bypass while always enforcing current access and explicit recipient disablement.
- Added bounded email retry and the existing bounded Teams retry path.
- Added a fixed, bounded, CR/LF-safe email template containing only author label, object display/subject, and an authenticated HTTPS deep link.
- Added all-or-none SMTP environment configuration and a STARTTLS-only SMTP transport with TLS hostname verification and no credential logging.
- Reauthorized immediately before email/Teams transport: active internal technician, `mention.read`, current parent access, active/current source token, current unsuppressed item/latest occurrence, current policy destination, current preference, current email, and enabled named Teams connection.
- Access or policy loss is terminally suppressed with safe reason codes; transport failures remain retryable within the bounded window.
- Added self-only `GET`/`PATCH /api/v1/notification-preferences/mentions` with strict JSON, optimistic versioning, time-zone, and paired-time validation.
- Added server rejection for `in_app` and `webhook` destinations on `mention.occurred` policies.

## Migration

Added `000090_mention_notification_delivery_dedupe.sql` without rewriting applied migration 89. It replaces the old delivery uniqueness constraint with disjoint partial unique indexes:

- legacy deliveries with a NULL technician retain the original policy/event/channel/recipient uniqueness;
- mention deliveries include `recipient_technician_id`, allowing distinct exact recipients while rejecting duplicate delivery for the same recipient.

Upgrade, down, reapply, fresh migration, legacy dedupe, and recipient dedupe behavior are covered against PostgreSQL 16.

## Frontend implementation

- Added an accessible compact Mention notification preferences disclosure.
- Disabled organization-unavailable channels instead of allowing the technician to enable them.
- Added time zone and local quiet-hour controls with versioned self-service save.
- Added a Mentions policy preset fixed to `mention.occurred`.
- Restricted the preset destination editor to email and Microsoft Teams on the client, with the matching server guard.

## Verification evidence

- TDD RED captured before implementation for missing mention planning, preferences, email, and delivery authorization types.
- `go test ./backend/...` — PASS.
- `go test ./...` with synthetic Compose verification variables — PASS.
- `go test -race ./backend/internal/notifications ./backend/internal/store/psa ./backend/internal/httpapi ./backend/internal/config` — PASS.
- `go vet ./backend/...` — PASS.
- PostgreSQL 16 fresh 1→90 migration and internal mention delivery FK test — PASS.
- PostgreSQL 16 89→90 upgrade, down, restored legacy uniqueness, and reapply — PASS.
- PostgreSQL 16 live mention planner, preferences, claim, reauthorization, and access-revocation flow on a uniquely named fresh database — PASS; the disposable database was removed afterward.
- Node 22.23.2 full frontend suite — 160/160 suites and 488/488 tests PASS.
- Node 22.23.2 `npm --prefix frontend run build` — PASS; Vite reports the existing bundle-size warning only.
- SMTP security regression confirms a server without STARTTLS is rejected.
- Mention policy regressions confirm `in_app` and `webhook` are rejected.
- `git diff --check` — PASS.

## Notes

- No new runtime dependency was added.
- SMTP is optional; all five variables (`RARITY_SMTP_HOST`, `RARITY_SMTP_PORT`, `RARITY_SMTP_USERNAME`, `RARITY_SMTP_PASSWORD`, `RARITY_SMTP_FROM`) must be supplied together.
- The temporary PostgreSQL database and named Node test container used for verification were removed.

## Review fix round 1

Resolved all four Important findings and the Minor finding from `task-8-review.md`:

- Mention delivery uniqueness is now exactly `(event_id, recipient_technician_id, channel)` for rows with a recipient technician. It is independent of policy and destination reference, while NULL-recipient legacy deliveries retain their original key.
- `PlanAtomic` now claims `notification_event_plans` first. Only the transaction that inserts that marker may insert deliveries; losing concurrent or late planners commit no delivery rows.
- Migration 90 deterministically reconciles pre-existing semantic mention duplicates on Up and forward-valid legacy-key conflicts on Down before installing each unique key. The Down is intentionally lossy and keeps the earliest `planned_at`, then lowest `id`.
- Delivery reauthorization now requires the current enabled policy to remain `mention.occurred`, contain the exact current channel destination, and have a currently operational email or named Teams channel. Invalid current mailboxes and disabled/missing named Teams connections terminally suppress as `channel_unavailable` before transport.
- Preference PATCH now preserves JSON presence. Every scalar is explicitly required and non-null; both quiet fields are required and must be either explicit NULL together or valid `HH:MM` strings together. Invalid requests make zero preference-service calls.
- The preferences UI now maintains separate canonical server and editable draft states. A 409 reloads current state, preserves the draft, announces an accessible current-versus-draft conflict, blocks stale resubmission, and requires an explicit “Load server settings” or “Reapply my draft” choice.

### TDD and live evidence

- RED was observed for the semantic migration key/Down reconciliation, marker-first ownership, lost-claim no-write behavior, current policy type, strict PATCH field presence, required time zone, and both 409 resolution paths before implementation.
- PostgreSQL 16 89→90/down/reapply: PASS, including distinct recipients, policy/ref-independent mention dedupe, deterministic legacy collapse, restored legacy dedupe, and clean reapply.
- PostgreSQL 16 fresh 1→90 on a disposable database: PASS.
- Live simultaneous `PlanAtomic` calls: PASS with exactly one event-plan marker and one recipient/channel delivery.
- Live reauthorization: PASS for current valid email, invalid current email suppression, changed event-type suppression, enabled named Teams authorization, disabled named Teams suppression, and revoked-access suppression.
- Focused backend packages: PASS.
- Full backend `go test ./...`: PASS.
- Full backend `go test -race ./...`: PASS.
- Full backend `go vet ./...`: PASS.
- Node 22.23.2 focused preference tests: 3/3 PASS.
- Node 22.23.2 full frontend suite: 84/84 files and 490/490 tests PASS.
- Node 22.23.2 production build: PASS; only the existing Vite bundle-size advisory was emitted.
- `git diff --check`: PASS.

### Verification note

Running every Go package concurrently against one shared fresh `TEST_DATABASE_URL` is unsupported by the current integration harness because separate packages race to initialize Goose metadata. That diagnostic run was stopped and its disposable database removed. The fresh migration/live tests were rerun serially on a uniquely named empty database, while the complete unit and race suites ran separately and passed.
