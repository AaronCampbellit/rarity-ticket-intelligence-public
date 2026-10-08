# Clients

**Status:** Client resource read/create/update/lifecycle source complete;
publication and live acceptance pending

Clients represent MSP customers and are isolation, reporting, workflow, contract, and visibility boundaries. A client has lifecycle status, identifiers, locations, contacts, defaults, contracts, supported services, assets, and relationships.

Location, Location-scoped Contact and Asset, operational Service, and
effective-dated Contract reads and mutations use the validated active-Client
principal, capability authorization, exact scoped resolution, optimistic
versions, PostgreSQL persistence, and atomic audit/outbox evidence. Updates
preserve immutable identifiers and scope. Deactivate/reactivate follows the
typed lifecycle state machine, including active dependency checks for
Locations and system-owned reconciliation for discovered Assets. Asset
discovery provenance and technician-confirmed authority remain durable.

The MSP-wide AI workspace exposes a closed Client-resource catalog for all five
kinds. It lists minimized active summaries and prepares exact create, update,
deactivate, and reactivate proposals only from user-supplied business values.
Every write requires an explicit Target Client, exact preview, live
reauthorization/re-preview, and confirmation. The final whole-branch fix wave
moves Client-resource exact resolution into a Client-scoped,
display-ID-first SQL boundary limited to two rows. Typed Client and resource
ambiguity, missing-target, lifecycle, authority, and version errors remain
intact through proposal preparation and safe HTTP mapping. The same wave
preserves lifecycle and Asset authority in summaries and makes canonical
Location identity part of the proposal rather than a separate `search.read`
dependency.
Refreshed isolated PostgreSQL acceptance now covers Client deactivation races
against resource creation, Knowledge drafting, and Ticket routing. Exact
whole-branch re-review found no remaining Critical or Important issues;
publication, exact-revision deployment, and live browser acceptance remain
pending.

Disabling or deleting a client is a governed lifecycle action with impact preview, retention, and recovery. Client merges or transfers require a future explicit design because they affect nearly every isolation guarantee.
