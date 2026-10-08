# MSP-Wide AI Workspace and Client Actions Design

**Date:** 2026-08-04

**Status:** Approved design

**Scope:** Make the global AI drawer operate across the MSP's authorized
Clients, resolve named Clients for typed actions, and add confirmed Client
creation.

## Goal

The AI drawer is an MSP-wide operating surface. A page's selected Client may
filter that page, but it does not constrain the AI conversation. The drawer can
find and act on any Client available to the authenticated MSP principal, while
every read and write still uses an ordinary application service, exact target
scope, live authorization, and explicit confirmation.

## Product Decisions

- AI conversations and proposals created by the global drawer are MSP- and
  principal-scoped, not bound to the page's selected Client.
- The page-level Client selector remains a navigation and page-data filter. It
  may provide a default hint in an action form, but it is not an AI
  authorization boundary.
- Every Client-targeted AI action identifies its Client explicitly. Chat
  commands use a user-supplied Client name or display ID; structured action
  forms use an explicit Client selector.
- The server resolves Client references against the authorized MSP directory.
  The browser or model never supplies a trusted internal Client ID.
- Client creation is MSP-global and requires both a user-supplied Client name
  and display ID.
- The AI may generate only system identifiers, proposal metadata, audit/event
  identifiers, timestamps, and correlation identity.
- Client contacts, domains, locations, contracts, billing settings, service
  configuration, and other optional business data are not inferred or created.
- Every write remains an expiring exact preview requiring explicit
  confirmation and live reauthorization.

This specification intentionally supersedes the earlier assumption that a
drawer conversation is bound to one active Client. Cross-Client conversation
references are allowed only for records the MSP-global authenticated principal
can access, and every referenced record or proposal must display its Client.

## Architecture

### MSP-wide conversation scope

The frontend workspace API omits `X-Rarity-Client-ID` for conversation,
message, proposal, confirmation, and rejection requests. The session resolver
therefore loads the authenticated MSP-global principal. Existing backend
conversation and proposal storage already supports an empty `client_id`; new
drawer conversations use that scope.

Existing Client-scoped conversation records remain stored and isolated. This
slice does not rewrite or merge their history into MSP-wide conversations.

`AIWorkspace` receives the authorized Client directory plus the page's
currently selected Client as a presentation hint. Its header says
“All authorized clients.” Structured write forms require an explicit target
Client and may initially select the page-filtered Client.

### Client resolution

A single server helper resolves a user-supplied Client reference against a
global `Directory.List` result:

1. normalize whitespace and case;
2. match exact normalized Client name or display ID;
3. deduplicate identical Client IDs;
4. return exactly one authorized Client;
5. return a safe clarification for zero or multiple matches.

Project and standalone Task chat commands reuse this resolver. Their current
“for CLIENT” values no longer need to match a page-selected Client. The
resolved internal Client ID is added server-side to the typed tool input.

Client creation uses the organization service's normalized identity-conflict
check to prevent an AI proposal when the name or display ID already belongs to
any Client across all lifecycle states, including inactive, archived, and
deleted. The typed tool repeats this check while rebuilding the preview at
confirmation, so a Client created after proposal time makes the proposal stale
or invalid instead of creating a duplicate.

One shared internal identity boundary owns the deterministic Go normalization,
MSP-scoped PostgreSQL advisory transaction lock, all-lifecycle conflict query,
cross-field matching, and typed conflict used at commit time. Ordinary and AI
Client creation call it inside the organization repository transaction.
Prospect conversion calls the same boundary inside its larger conversion
transaction before inserting a new Client, contact, Project, or any other
conversion mutation. A conversion that selects an existing Client does not
acquire the Client-identity lock.

### Typed Client creation

Add the closed write tool `client.create`, requiring capability
`client.create` and resolving only the MSP-global target.

The accepted chat form is:

> Create a client named NAME with display ID DISPLAY_ID.

`NAME` and `DISPLAY_ID` are the only business values. Missing values return a
clarification and create no proposal. Unknown JSON fields and caller-supplied
Client, actor, audit, event, or correlation IDs are rejected.

During trusted input preparation the tool generates a stable future Client ID.
Its preview shows:

- Client name;
- display ID;
- lifecycle state `active`.

The preview target is the prepared Client ID at version zero. Confirmation
reloads the MSP-global principal, rechecks `client.create`, repeats duplicate
resolution, rebuilds the exact preview, and calls
`organizations.Service.CreateClient`.

`organizations.CreateClientCommand` gains optional trusted `ClientID` and
`CorrelationID` fields. The service generates them when omitted, preserving
the ordinary HTTP contract. When supplied by the typed tool, the service uses
them for the new Client and its atomic audit/outbox facts. Neither field is
accepted from the public Client request body.

### Existing typed actions

The closed registry remains the only execution surface.

- Project chat creation resolves the named Client globally, then submits the
  existing `project.create` input.
- Standalone Task chat creation resolves the named Client globally, then
  resolves the named Project only inside that Client before submitting
  `task.create`.
- Structured ticket and Project proposal forms require an explicit Client
  selection and submit that server-known ID to their existing typed tools.
- Client creation is available through the exact chat command in this slice.
  A separate generic database, HTTP, shell, or model-generated mutation route
  remains prohibited.

## Data Flow

1. The user opens the top-bar AI drawer from any authenticated page.
2. The drawer loads an MSP-scoped conversation without a Client header.
3. A chat command includes the target Client name/display ID, or a structured
   action form includes an explicit Client selection.
4. The server loads the global authorized directory and resolves the target.
5. The registry validates the closed tool schema and authorizes the exact
   Client or MSP target.
6. A pending proposal stores normalized input, stable target identity, exact
   preview, required capability, expiry, and correlation identity.
7. Confirmation reloads the same MSP-global principal, resolves and authorizes
   the target again, rebuilds the preview, and invokes the ordinary service.
8. The result and audit/outbox facts retain the human actor and AI proposal
   correlation evidence.

## Failure Handling

- A principal without MSP-global `ai.assist` cannot open or use the MSP-wide
  drawer.
- A principal without the action capability may converse but cannot prepare
  that action.
- Missing Client names/display IDs produce plain-English clarification.
- Unknown or ambiguous Client references produce no proposal and expose no
  inaccessible record details.
- Duplicate Client name or display ID in any lifecycle state, including
  inactive, archived, and deleted, produces no creation proposal.
- Lost permissions, expired proposals, changed directory resolution, or stale
  previews block confirmation.
- Client-filter changes on the underlying page do not change or erase the
  MSP-wide conversation.
- Historical Client-scoped conversations are not merged into the global
  conversation.

## GUI

The drawer header displays “All authorized clients.” Each cited operational
record and every write preview displays its target Client. The existing page
header continues to show the page's active Client filter.

Structured action mode adds a required “Target client” selector populated from
the authorized directory. The page-filtered Client may be the initial option,
but the user can choose another Client without navigating away. Client
creation remains chat-driven in this slice.

After a confirmed `client.create`, the shell refreshes and reconciles the
authorized Client directory so the new Client is available to the page filter,
structured selector, and later proposal previews without a full reload.
Target-bearing proposals display the canonical directory name and display ID.
If that identity cannot be resolved, the card identifies the unavailable
target and confirmation fails closed. The `client.create` card continues to
label its prepared identity as “New client.”

No new visual language is introduced; the existing RTM-authoritative dark,
dense drawer and proposal components remain the presentation authority.

## Verification

- Conversation/API tests prove workspace requests omit the Client header and
  remain stable when the page filter changes.
- Authorization tests prove the drawer uses an MSP-global principal and every
  typed action still authorizes its exact Client or MSP target.
- Client resolver tests cover exact name, exact display ID, missing, duplicate,
  and inaccessible matches.
- Planner tests cover complete and incomplete Client creation commands without
  regressing Project, Task, or product-help planning.
- Tool tests cover trusted Client/correlation IDs, exact preview fields,
  duplicate recheck, strict JSON, and ordinary service execution.
- Organization service tests prove trusted optional IDs preserve existing HTTP
  behavior and atomic audit/outbox correlation.
- Conversion transaction tests prove the shared lock/recheck precedes every
  write, conflicts roll back without mutation, and existing-Client conversion
  skips the lock. An environment-gated PostgreSQL regression races ordinary
  Client creation against Prospect conversion and requires exactly one winner.
- Frontend tests cover the “All authorized clients” label, explicit action
  Client selection, no Client header, page-filter independence, post-create
  directory refresh, canonical target rendering, unresolved-target
  confirmation blocking, and generic proposal confirmation/rejection.
- Full Go tests, vet, frontend tests/build, documentation validators, and
  formatting/diff checks must pass.
- Live acceptance at 1440 by 900 submits a unique synthetic Client command,
  verifies the exact preview, rejects it, and proves the global directory is
  unchanged. A Project or Task command also verifies that Northwind Legal is
  resolved by name from the MSP-wide drawer.

## Explicit Exclusions

- Silent Client creation or automatic confirmation.
- Automatic page-filter switching after a Client reference or creation.
- Model-invented display IDs or other Client business data.
- Client contacts, domains, locations, contracts, billing, service setup, or
  onboarding records.
- Client updates, merges, disablement, or deletion.
- Rewriting historical Client-scoped AI conversations.
- Generic SQL, HTTP, shell, or arbitrary automation access.
