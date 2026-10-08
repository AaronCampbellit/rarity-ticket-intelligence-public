# Core Domain Model

**Status:** Draft

## Organizational model

Installation → MSP organization → client organizations → locations, contacts, assets, services, contracts, and work.

Technicians may belong independently to multiple departments, teams, and queues. Membership may carry role, effective dates, on-call schedule, skill, capacity, or manager metadata.

## Work model

A work record has a type, client context, workflow version, status, priority, impact, urgency, queue/team/department routing, one optional primary owner, zero or more collaborators and watchers, relationships, comments, attachments, time entries, SLAs, approvals, tags, custom fields, and history.

Type, Status, and Tags are independent dimensions. Type describes what an
object is, Status describes its lifecycle, and Tags provide governed
classification. A tag change therefore never changes Type or Status and a
workflow transition never manufactures a tag.

The classification catalog is one MSP-global taxonomy, organized into
separate groups for presentation. It is not partitioned by Client, team,
queue, or object type, and technicians cannot create free-form or
Client-local tags. Direct classification is supported on Work Records, Tasks,
Projects, Assets, Knowledge Articles, and Time Entries. Tasks dynamically
inherit the current effective tags of their parent Project or Phase; inherited
tags are derived, de-duplicated with direct tags, and cannot be removed from
the Task.

Every active supported object must have at least one effective tag. Interactive
human creation requires a meaningful tag. Trusted automation and integration
paths may use the protected `Unclassified` system fallback so ingestion can
complete without pretending that classification happened. Later writes may
not remove the final meaningful effective tag. Archived tags cannot be newly
assigned, and merged tags resolve to one active survivor while immutable
assignment history retains the original identity.

Classification is governed by four separate capabilities:
`classification.apply`, `classification.manage`, `classification.report`, and
`classification.ai.manage`. Object access is always evaluated in the active
Client scope even though catalog management is MSP-global.

## Platform objects

Organization, Prospect, Client, Location, Contact, Technician, Department, Team, Queue, Work Record, Asset, Service, Knowledge Article, Pipeline, Pipeline Stage, Opportunity, Proposal, Proposal Version, Proposal Line, Project, Phase, Task, Resource Plan, Project Budget, Cost Actual, Change Order, Contract, Workflow, Automation, Integration, Connection, Approval, Attachment, Comment, Time Entry, SLA, API Principal, Webhook, and AI Agent.

Rarity is the native PSA system of record. Sales, service delivery, and Project delivery use separate first-class domains joined by explicit relationships and audited transitions. An Opportunity remains the historical sales record after it creates a Project; the Project retains an immutable reference to the accepted Proposal Version that established its original commercial baseline.

All client-bound objects carry explicit client scope. Cross-client relationships are denied unless a future, narrowly defined MSP-global object contract permits them.

## Detailed specifications

- [Entity Contracts](11-entity-contracts.md)
- [Relationship Registry](12-relationship-registry.md)
- [Event Catalog and Envelope](13-event-catalog.md)
