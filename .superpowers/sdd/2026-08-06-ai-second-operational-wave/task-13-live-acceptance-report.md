# Task 13 report — Exact-revision demo acceptance

## Scope and revision

- Public surface: `https://rarity.campbellservers.com`
- Accepted application revision:
  `963f4759effe28e34eed6033230f0439ea7e890f`
- Viewport: 1440 by 900 pixels
- Principal: authenticated Entra global administrator
- Workspace scope shown by the drawer: `All authorized clients`
- Demo data boundary: one authorized active Client, Northwind Legal

The pushed `main`, remote `main`, detached demo checkout, `/readyz`,
`/v1/system/build`, and rendered shell all identified the same revision. The
deployment applied schema version 81, served a fresh frontend asset, kept all
six RTI containers healthy with zero restarts, and left the existing Hank and
RTM projects running.

## Authenticated browser evidence

- `ticket.create` appeared in the capability-filtered catalog. A synthetic
  Incident preview showed the selected Queue, Workflow version, SLA policy,
  SLA calendar, and initial state. The first proposal was rejected; the second
  created `INC-AI-WAVE2-20260807-01`.
- `ticket.assign` rejected expected version 99 and a nonexistent technician
  with safe, non-enumerating feedback. The exact active technician
  `Acampbell@campbellservers.com` resolved to Aaron Campbell; confirmation
  moved the Ticket from version 1 to version 2 with the supplied reason.
- Opportunity list/get returned `OPP-NW-1042` at version 1. A transition from
  Discovery to Proposal and an internal note activity both rendered readable,
  exact previews and were rejected without mutation.
- Proposal list/get returned the existing Northwind proposals. A new empty
  draft `PROP-AI-WAVE2-20260807-01` for `OPP-NW-1042` rendered an exact
  version-1 preview and was rejected without mutation.
- Knowledge publication first rendered and rejected the exact draft-to-
  published preview for the disposable internal fixture
  `KB-AI-ACCEPT-20260806`. A fresh proposal was then confirmed. The result
  remained version 2, internal-only, and explicitly stated that no external
  delivery would occur.
- Browser warnings and errors after the journeys: zero.

The demo has only one active Client and two active technicians, both governed
by MSP-wide global-administrator assignments. It therefore cannot honestly
demonstrate cross-Client, restricted-principal, inactive-technician, or
ambiguous-technician browser denials. Deterministic service/tool tests cover
those cases; the live run proved stale-version and enumeration-safe no-match
behavior without manufacturing misleading production-like identities.

## PostgreSQL and side-effect evidence

The accepted Ticket is an Incident in `new`/`normal`, has the preflight-selected
Queue and Aaron Campbell owner, and is version 2. Its create and owner-change
audit facts match the corresponding outbox events by subject, version, and
correlation ID. The assignment audit fact retained
`Disposable AI second-wave assignment acceptance.`

The disposable Knowledge Article is `published`, version 2, and
`client_visible = false`. Its `knowledge.published` audit and outbox facts
match at version 2 and the audit retained
`Disposable internal AI acceptance publication.`

For the accepted Ticket and Knowledge events, the database contained zero
outbound webhook deliveries, zero non-in-app notification deliveries, and zero
automation execution jobs. Rejected Proposal, Opportunity transition,
Opportunity activity, and first Knowledge proposals made no domain mutation.

## Remaining boundary

This accepts only the closed second-wave catalog. Financial, billing, pricing,
Proposal issue/approval/acceptance/conversion, Opportunity conversion,
Project financial/Change Order, configuration, automation administration,
integration, credential, network, and client-visible/external Knowledge
actions remain absent.
