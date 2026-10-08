# Relationship Registry

**Status:** Detailed draft\
**Version:** 0.1\
**Last updated:** 2026-07-25

## Relationship object

Relationships are first-class records rather than untracked foreign-key conventions. Each relationship contains:

- immutable relationship UUID;
- canonical relationship type and schema version;
- source and target object IDs and types;
- MSP and client scope;
- direction and inverse display label;
- source: human, API, integration, migration, automation, or inference;
- confidence and confirmation state where applicable;
- effective start/end, last verified time, and verification actor;
- client visibility and sensitivity classification;
- notes/metadata, version, audit, and lifecycle state.

## Initial registry

| Relationship | Source → target | Inverse | Cross-client |
|---|---|---|---|
| `belongs_to` | Contact/Location/Asset/Service → Client | contains | Never |
| `located_at` | Contact/Asset/Service → Location | contains | Never |
| `assigned_owner` | Work Record → Technician | owns | MSP technician may span clients |
| `assigned_collaborator` | Work Record → Technician | collaborates_on | MSP technician may span clients |
| `watching` | User/Contact → Work Record | watched_by | Same authorized scope only |
| `routed_to_department` | Work Record → Department | receives_work | MSP-global department allowed |
| `routed_to_team` | Work Record → Team | receives_work | MSP-global team allowed |
| `routed_to_queue` | Work Record → Queue | contains_work | Queue scope must authorize client |
| `requested_by` | Work Record → Contact | requested | Never |
| `affected_asset` | Work Record → Asset | affected_by | Never |
| `affected_service` | Work Record → Service | affected_by | Never |
| `parent_of` | Work Record → Work Record | child_of | Never |
| `related_to` | Work Record → Work Record | related_to | Never |
| `caused_by_problem` | Incident → Problem | causes_incident | Never |
| `implemented_by_change` | Problem/Work Record → Change | implements | Never |
| `fulfilled_by_task` | Request/Project → Task | fulfills | Never |
| `pursued_for` | Opportunity → Prospect/Client | sales_opportunity | Prospect is MSP-scoped; Client must match after conversion |
| `proposed_by` | Proposal → Opportunity | has_proposal | Never |
| `supersedes_version` | Proposal Version/Change Order Version → prior version | superseded_by | Never |
| `converted_to` | Opportunity → Project | originated_from | Never |
| `baseline_from` | Project → accepted Proposal Version | establishes_baseline | Never |
| `contains_phase` | Project → Phase | belongs_to_project | Never |
| `changed_by` | Project → Change Order | changes_project | Never |
| `covered_by_contract` | Work/Asset/Service → Contract | covers | Never |
| `documented_by` | Object → Knowledge Article | documents | Same client or authorized MSP-global article |
| `depends_on` | Asset/Service → Asset/Service | supports | Never |
| `runs_on` | Service/Asset → Asset | hosts | Never |
| `connects_through` | Asset/Service → Asset/Service | carries | Never |
| `authenticates_through` | Asset/Service → Service/Asset | authenticates | Never |
| `backed_up_by` | Asset/Service → Asset/Service | protects | Never |
| `monitored_by` | Asset/Service → Integration/Asset | monitors | Connection must authorize client |

## Invariants

- A relationship cannot cross MSP boundaries.
- Client-bound source and target must share a client unless the registry explicitly permits an MSP-global target.
- Deleted, inaccessible, or expired targets cannot receive new relationships.
- Relationship authorization evaluates both endpoints and the relationship type.
- Symmetric relationships store one canonical ordered edge.
- Directed dependency edges undergo cycle policy validation. Not every cycle is invalid, but detected cycles are explicit and explainable.
- Inferred graph edges remain predicted until an authorized actor or trusted policy confirms them.

## Query behavior

Relationship traversal is bounded by depth, edge count, permitted relationship types, and authorization. APIs return the path and truncation reason. Impact queries distinguish direct, transitive, confirmed, and predicted results.

## Registry governance

Adding a relationship type requires an owner, source/target types, direction, inverse label, cardinality, scope rule, deletion behavior, history policy, API/event representation, traversal policy, and migration plan.
