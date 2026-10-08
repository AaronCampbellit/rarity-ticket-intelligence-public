# Task 13 full-page live acceptance report

Date: 2026-08-06
Environment: constrained non-production RTI demo
Viewport: 1440 by 900 pixels
Initial deployed revision: `ce75dcc7b4857f96c269147458db1ae5c95e2192`
Accepted service-desk revision: `347addfdf5780d6a4507e822f0c352625a9c6027`

## Accepted journeys

- The signed-in MSP-wide AI workspace exposed the complete closed catalog for
  Tickets, Projects, Tasks, Clients, five Client-resource kinds, Knowledge,
  and Prospects.
- Client-resource reads returned authoritative empty or populated results for
  Locations, Contacts, Assets, Services, and Contracts.
- Exact create proposals were prepared and explicitly rejected for all five
  resource kinds. No rejected proposal produced a row.
- A synthetic Location was created, updated, deactivated, reactivated, and
  finally deactivated. A dependent synthetic Contact proved that Location
  deactivation fails while the Location is in use. The Contact and Location
  were then deactivated.
- Two active Locations with the same name proved typed ambiguity handling:
  the name-only update was rejected with “More than one exact object matches.
  Use its display ID and try again.” Exact display IDs remained usable, and
  both synthetic Locations were deactivated after the check.
- Repeating deactivation against an inactive Location was rejected before a
  proposal could be prepared.
- Project listing resolved the existing Northwind Project and rendered its
  nested values as plain-English definition lists rather than raw JSON.
- Knowledge search returned an authoritative empty result. A synthetic
  Knowledge draft was created at version 1 and revised at exact version 1 to
  version 2. A second revision with stale version 1 was rejected.
- A complete Prospect proposal containing only the supplied display ID, name,
  email, and phone was explicitly rejected; the Prospect table remained
  unchanged.
- Proposal cards displayed canonical Client and referenced-resource identity.
  Nested Client, Project, Knowledge, and resource values rendered as readable
  labels and values with no raw JSON object output.
- The repaired all-Clients Service Desk setup path created the global
  `Global Triage` queue, published routing rule set version 1, published
  `Global business hours` calendar version 1, and published
  `Global default SLA` policy version 1. Client-scoped configuration remained
  explicitly labeled and gated.
- A synthetic incident `INC-AI-ACCEPT-20260806` was created through the
  ordinary Work path and initially routed to `Global Triage`. The AI drawer
  then prepared an exact route preview from `Global Triage` to
  `Northwind Escalations`, showing ticket version 1 to version 2 and only the
  supplied reason. Confirmation succeeded.

## Durable evidence

The first synthetic Location lifecycle produced correlated audit and outbox
facts for versions 1 through 4: create, update, deactivate, and reactivate.
The Knowledge draft produced correlated audit and outbox facts for versions 1
and 2: create draft and revise draft. Every inspected outbox fact carried the
matching subject version with no delivery error.

The synthetic incident is durable at version 2 in queue
`northwind-escalations`. Its create mutation used correlation
`34dedd17-ed42-46fc-9c4c-0757019b01f9`; its queue change used correlation
`f192bef1-f6f9-48f9-89e6-2b10457e5ece`. Both matching outbox facts have zero
delivery attempts and no last-error code.

## Environment-bounded exclusions

- Only one authorized active Client exists in the demo, so a cross-Client read
  cannot be demonstrated without adding a durable tenancy fixture solely for
  acceptance.
- True authority denial remains environment-bounded because the signed-in
  actor is the MSP-global administrator. Source and automated tests cover the
  capability-denial contract.

## Runtime observations

- The exact build identity was visible in the authenticated shell.
- No browser console warning or error was present after the accepted journeys.
- The demo remained healthy while acceptance data was created and cleaned to
  inactive synthetic rows.
- Live acceptance exposed two deployment-only gaps: the setup center could not
  publish the ordinary routing/calendar/SLA prerequisites from all-Clients
  scope, and `/api/v1/me` omitted the already-authorized `routing.manage`
  capability. Both were fixed permanently, reviewed, tested, pushed, and
  deployed before the Ticket route was accepted.
