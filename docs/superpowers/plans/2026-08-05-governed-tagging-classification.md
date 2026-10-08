# Governed Tagging and Classification Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver one global, administrator-governed tagging fabric across tickets, tasks, projects, assets, knowledge articles, and time entries, including required classification, inheritance, AI assistance, automation, reporting, and complete Category cutover.

**Architecture:** A new `tagging` application boundary owns the global catalog, direct associations, effective-tag resolution, classification invariants, AI policy, reporting queries, and migration evidence. PostgreSQL keeps canonical tag identities and append-only assignment history; existing object repositories attach initial tags in their current atomic create transactions, while tag mutations version the target object and write audit/outbox evidence atomically. Shared React classification components consume one typed API and are embedded in every supported feature surface.

**Tech Stack:** Go 1.25 with the Go 1.26.5 toolchain, PostgreSQL, Goose, pgx/v5, React 19, TypeScript 7, Vite 8, Vitest, Testing Library, Playwright, and axe-core.

## Global Constraints

- The tag catalog is MSP-global; client-, team-, object-type-, and technician-local catalogs are forbidden.
- Only administrators with `classification.manage` may create or maintain groups and tags.
- Technicians need `classification.apply` and authorized access to the exact client-bound object to change direct tags.
- Every active supported object has at least one effective tag. Interactive creation requires a meaningful tag; trusted automated creation may use the system-managed `Unclassified` fallback.
- `Unclassified` and archived tags never satisfy a terminal-state guard.
- Every tag is usable on every supported object type and belongs to exactly one display group.
- Visible labels and exact synonyms are case-insensitively unambiguous within the MSP.
- Tags use immutable IDs and immutable hidden namespaced keys; rename, regroup, merge, and archive never rewrite historical identity.
- Task inheritance is derived from the current project association and is never copied into direct task assignments.
- AI automatic application is disabled by default, requires an administrator-set high-confidence threshold, and never removes a human-selected tag.
- All accepted mutations persist the state change, target version, audit evidence, assignment history, and outbox facts atomically.
- Reporting and catalog usage counts never disclose records outside the active authorized Client.
- The deprecated generic Category concept receives no compatibility API or post-cutover storage.
- Preserve `forecast_category`, `content_classification`, and billing classification because they are distinct domain concepts, not the removed generic Category field.
- Add no runtime dependency without explicit user permission.

---

## Planned File Map

### Persistence and backend

- `backend/migrations/000080_tagging_classification.sql` — catalog, assignment, AI-policy, suggestion, projection, and migration-evidence schema.
- `backend/migrations/tagging_classification_contract_test.go` — schema and cutover contract.
- `backend/internal/tagging/model.go` — stable public domain types and enums.
- `backend/internal/tagging/catalog.go` — group/tag lifecycle and impact-preview service.
- `backend/internal/tagging/associations.go` — direct mutations, effective resolution, bulk actions, and classification guards.
- `backend/internal/tagging/creation.go` — initial-tag preparation shared by object creation services.
- `backend/internal/tagging/classification.go` — AI policy, suggestion decisions, and automatic-application rules.
- `backend/internal/tagging/reporting.go` — authorized report filters and result models.
- `backend/internal/tagging/projection.go` — append-only history projection worker.
- `backend/internal/tagging/migration.go` — Category cutover and coverage preflight.
- `backend/internal/store/psa/tagging_repository.go` — scoped PostgreSQL implementation.
- `backend/internal/store/psa/tagging_projection_repository.go` — projection cursor and aggregates.
- `backend/internal/httpapi/tagging_routes.go` — catalog, association, AI-policy, and report HTTP surface.
- `backend/cmd/rarity-api/main.go` — service and worker composition.
- `backend/cmd/rarity-admin/main.go` — classification preflight command.

### Existing object integration

- `backend/internal/workrecords/service.go`, `transition.go`, and PostgreSQL repository — initial tags and terminal guard.
- `backend/internal/tasks/service.go` and PostgreSQL repository — initial direct tags and project inheritance.
- `backend/internal/projects/service.go`, `conversion_service.go`, and PostgreSQL repository — initial tags.
- `backend/internal/clientresources/service.go`, Datto ingestion, and asset repositories — interactive or fallback asset tags.
- `backend/internal/knowledge/service.go` and PostgreSQL repository — initial tags and publish guard.
- `backend/internal/timeentries/service.go`, billing approval/export, and PostgreSQL repositories — initial tags and terminal guard.
- `backend/internal/automation/definition.go`, `actions.go`, and snapshot/runtime repositories — tag triggers, conditions, actions, and loop protection.
- `backend/internal/aiassist/*` — structured classification feature, generic subject reference, and model-policy mapping.

### Frontend

- `frontend/src/features/classification/types.ts` and `api.ts` — shared browser contract.
- `frontend/src/features/classification/ClassificationSettingsPage.tsx` — catalog, groups, impact previews, AI policy, and migration health.
- `frontend/src/features/classification/ClassificationInsightsPage.tsx` — trends, combinations, recurring issues, and health.
- `frontend/src/features/classification/ObjectTagEditor.tsx` — direct/inherited editor used by feature pages.
- `frontend/src/design-system/components/fields/TagPicker.tsx` — accessible grouped multi-select.
- `frontend/src/design-system/components/data/TagChip.tsx` — direct/inherited/source presentation.
- Existing Work, Project, Knowledge, Client Resources, and Billing pages — creation and editing integration.

---

### Task 1: Tagging schema, invariants, and system fallback

**Files:**
- Create: `backend/migrations/000080_tagging_classification.sql`
- Create: `backend/migrations/tagging_classification_contract_test.go`
- Modify: `backend/migrations/kernel_contract_test.go`

**Interfaces:**
- Produces: `tag_groups`, `tags`, `tag_synonyms`, `object_tag_assignments`, `tag_assignment_events`, `tag_ai_policies`, `tag_ai_suggestions`, `tag_ai_suggestion_items`, `tag_projection_cursors`, `tag_usage_daily`, `tag_cooccurrence_daily`, `classification_migration_runs`, and `classification_migration_mappings`.
- Consumes: existing MSP, Client, technician, audit, outbox, AI model-profile, and supported-object tables.

- [ ] **Step 1: Write the failing migration contract**

```go
func TestTaggingClassificationMigrationDefinesGovernedScopedFabric(t *testing.T) {
	body := migrationBody(t, "000080_tagging_classification.sql")
	for _, fragment := range []string{
		"CREATE TABLE tag_groups",
		"CREATE TABLE tags",
		"CREATE UNIQUE INDEX tags_label_unique",
		"CREATE TABLE object_tag_assignments",
		"CHECK (object_type IN ('work_record', 'task', 'project', 'asset', 'knowledge_article', 'time_entry'))",
		"CREATE TABLE tag_assignment_events",
		"CREATE TRIGGER tag_assignment_events_append_only",
		"CREATE TABLE tag_ai_policies",
		"CREATE TABLE tag_ai_suggestion_items",
		"CREATE TABLE tag_projection_cursors",
		"CREATE TABLE classification_migration_runs",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("tagging migration missing %q", fragment)
		}
	}
	if strings.Contains(body, "category text") {
		t.Fatal("generic Category storage must not be introduced")
	}
}
```

- [ ] **Step 2: Run the contract and verify failure**

Run: `go test ./backend/migrations -run TaggingClassification -count=1`

Expected: FAIL because `000080_tagging_classification.sql` does not exist.

- [ ] **Step 3: Create the catalog and association schema**

Use MSP-scoped composite keys and normalized expression indexes:

```sql
CREATE TABLE tag_groups (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL REFERENCES msp_organizations(id),
  name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
  description text NOT NULL DEFAULT '',
  display_order integer NOT NULL CHECK (display_order > 0),
  state text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'archived')),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL,
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id)
);
CREATE UNIQUE INDEX tag_groups_name_unique
  ON tag_groups (msp_id, lower(btrim(name)));

CREATE TABLE tags (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  group_id uuid NOT NULL,
  internal_key text NOT NULL,
  label text NOT NULL CHECK (length(btrim(label)) BETWEEN 1 AND 80),
  state text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'merged', 'archived')),
  system_managed boolean NOT NULL DEFAULT false,
  merged_into_tag_id uuid,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL,
  updated_by uuid NOT NULL,
  UNIQUE (id, msp_id),
  UNIQUE (msp_id, internal_key),
  FOREIGN KEY (group_id, msp_id) REFERENCES tag_groups(id, msp_id),
  FOREIGN KEY (merged_into_tag_id, msp_id) REFERENCES tags(id, msp_id),
  CHECK ((state = 'merged') = (merged_into_tag_id IS NOT NULL))
);
CREATE UNIQUE INDEX tags_label_unique ON tags (msp_id, lower(btrim(label)));
```

Add `tag_synonyms` with a unique `(msp_id, lower(btrim(value)))` index.
Create `object_tag_assignments` with client scope, direct assignment source,
optional AI/automation evidence, and uniqueness on
`(msp_id, client_id, object_type, object_id, tag_id)`.

- [ ] **Step 4: Add append-only history, AI, projection, and migration tables**

`tag_assignment_events` records `operation IN ('added','removed')`, source,
actor, target version, correlation/causation IDs, and a globally unique
`idempotency_key`. Protect it with a trigger that rejects update/delete.

`tag_ai_policies` is one versioned row per MSP with:

```sql
automatic_apply_enabled boolean NOT NULL DEFAULT false,
automatic_apply_threshold numeric(4,3) NOT NULL DEFAULT 0.950
  CHECK (automatic_apply_threshold BETWEEN 0.500 AND 1.000),
model_profile_id uuid,
version bigint NOT NULL DEFAULT 1
```

Add suggestion headers/items, daily usage/co-occurrence aggregates, projection
cursors, and migration run/mapping tables exactly as listed in the produced
interface. Keep all suggestion candidate IDs foreign-keyed to `tags`.

- [ ] **Step 5: Backfill existing installations**

In the migration, create one system group and `Unclassified` tag for each
existing MSP, then insert `system_fallback` assignments for every existing
work record, task, project, asset, knowledge article, and time entry. Insert a
completed `classification_migration_runs` row per MSP with counts and
`category_source_present = false`. New installations are handled by
`EnsureSystemCatalog` in Task 2 because migrations run before first-run MSP
creation.

- [ ] **Step 6: Run migration contracts**

Run: `go test ./backend/migrations -run 'TaggingClassification|SecureKernel|WorkManagement' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/migrations/000080_tagging_classification.sql backend/migrations/tagging_classification_contract_test.go backend/migrations/kernel_contract_test.go
git commit -m "Add governed tagging persistence"
```

### Task 2: Domain model and governed catalog service

**Files:**
- Create: `backend/internal/tagging/model.go`
- Create: `backend/internal/tagging/catalog.go`
- Create: `backend/internal/tagging/catalog_test.go`
- Create: `backend/internal/store/psa/tagging_repository.go`
- Create: `backend/internal/store/psa/tagging_repository_test.go`

**Interfaces:**
- Produces: `ObjectType`, `Source`, `Group`, `Tag`, `Catalog`, `Impact`, `CatalogService`, and `CatalogRepository`.
- Consumes: `authorization.Principal`, `scope.Target`, `mutation.AuditRecord`, `mutation.EventRecord`, and the Task 1 schema.

- [ ] **Step 1: Define stable tagging types**

```go
type ObjectType string
const (
	ObjectWorkRecord ObjectType = "work_record"
	ObjectTask ObjectType = "task"
	ObjectProject ObjectType = "project"
	ObjectAsset ObjectType = "asset"
	ObjectKnowledgeArticle ObjectType = "knowledge_article"
	ObjectTimeEntry ObjectType = "time_entry"
)

type Source string
const (
	SourceHuman Source = "human"
	SourceAIConfirmed Source = "ai_confirmed"
	SourceAIAutomatic Source = "ai_automatic"
	SourceAutomation Source = "automation"
	SourceIntegration Source = "integration"
	SourceMigration Source = "migration"
	SourceSystemFallback Source = "system_fallback"
)
```

Define JSON-tagged `Group`, `Tag`, `Catalog`, and `Impact` structs. `Tag` carries
`id`, `internal_key`, `label`, `group_id`, `state`, `system_managed`,
`merged_into_tag_id`, `synonyms`, and `version`. Also define `Health` with
meaningful, `Unclassified`, and archive-fallback counts by object type, plus
`MigrationRun` with preflight/cutover state and verified counts.

- [ ] **Step 2: Write catalog-service tests**

Cover:

```go
func TestCatalogRejectsDuplicateNormalizedLabelAndSynonym(t *testing.T)
func TestCatalogRenamePreservesIDAndInternalKey(t *testing.T)
func TestCatalogMergeRejectsSystemTagAndUsesActiveSurvivor(t *testing.T)
func TestCatalogArchiveAppliesUnclassifiedToImpactedObjects(t *testing.T)
func TestCatalogRequiresClassificationManage(t *testing.T)
```

Assert audit/outbox action names:
`classification.group.created`, `classification.tag.created`,
`classification.tag.renamed`, `classification.tag.merged`, and
`classification.tag.archived`.

- [ ] **Step 3: Run the tests and verify failure**

Run: `go test ./backend/internal/tagging -run Catalog -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 4: Implement catalog commands and lifecycle rules**

Expose exact service methods:

```go
func (s *CatalogService) EnsureSystemCatalog(context.Context, string, string) (Tag, error)
func (s *CatalogService) List(context.Context, ListCatalogCommand) (Catalog, error)
func (s *CatalogService) Health(context.Context, HealthCommand) (Health, error)
func (s *CatalogService) MigrationHistory(context.Context, MigrationHistoryCommand) ([]MigrationRun, error)
func (s *CatalogService) CreateGroup(context.Context, CreateGroupCommand) (Group, error)
func (s *CatalogService) UpdateGroup(context.Context, UpdateGroupCommand) (Group, error)
func (s *CatalogService) CreateTag(context.Context, CreateTagCommand) (Tag, error)
func (s *CatalogService) UpdateTag(context.Context, UpdateTagCommand) (Tag, error)
func (s *CatalogService) PreviewImpact(context.Context, ImpactCommand) (Impact, error)
func (s *CatalogService) Merge(context.Context, MergeCommand) (Tag, error)
func (s *CatalogService) Archive(context.Context, ArchiveCommand) (Tag, error)
```

Normalize whitespace, compare labels/synonyms case-insensitively, prohibit
changes to the system group/tag, require reasons for merge/archive, and require
optimistic versions for every update.

- [ ] **Step 5: Implement the PostgreSQL repository**

Use transactions for lifecycle mutation plus audit/outbox facts. Merge moves
assignments with `ON CONFLICT DO NOTHING`, appends removal/addition history,
records the survivor redirect, and marks the retired tag `merged`. Published
automation versions and historical report evidence remain immutable and
resolve the redirect at read/evaluation time. Archive previews affected active
objects and adds `Unclassified` before setting the tag state to `archived`.

- [ ] **Step 6: Run focused backend tests**

Run: `go test ./backend/internal/tagging ./backend/internal/store/psa -run 'Catalog|TaggingRepository' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/tagging backend/internal/store/psa/tagging_repository.go backend/internal/store/psa/tagging_repository_test.go
git commit -m "Add governed tag catalog"
```

### Task 3: Direct associations, effective tags, and object authorization

**Files:**
- Create: `backend/internal/tagging/associations.go`
- Create: `backend/internal/tagging/associations_test.go`
- Create: `backend/internal/tagging/creation.go`
- Create: `backend/internal/tagging/creation_test.go`
- Modify: `backend/internal/store/psa/tagging_repository.go`
- Modify: `backend/internal/store/psa/tagging_repository_test.go`
- Create: `tests/integration/tagging_isolation_test.go`

**Interfaces:**
- Produces: `TargetRef`, `TaggedObject`, `Assignment`, `HistoryEntry`, `ReplaceCommand`, `BulkCommand`, `CreationPolicy`, `InitialAssignmentSet`, `AssociationService`, `CreationPreparer`, and `TerminalGuard`.
- Consumes: catalog types and repository from Task 2.

- [ ] **Step 1: Write association invariants**

```go
type TargetRef struct {
	ObjectType ObjectType `json:"object_type"`
	ObjectID string `json:"object_id"`
	MSPID string `json:"msp_id"`
	ClientID string `json:"client_id"`
}

type TaggedObject struct {
	Target TargetRef `json:"target"`
	ObjectVersion int64 `json:"object_version"`
	Direct []Assignment `json:"direct"`
	Inherited []Assignment `json:"inherited"`
	Effective []Assignment `json:"effective"`
	ClassificationState string `json:"classification_state"`
}
```

Tests must prove:

- cross-Client targets return `scope.ErrNotFound`
- `classification.apply` is required
- archived input is rejected and merged input resolves to its survivor
- replacing the last meaningful tag with nothing is rejected
- adding a meaningful tag removes `Unclassified`
- project tags appear immediately as inherited task tags
- inherited tags cannot be removed through a task mutation
- repeated idempotency keys return the original accepted result
- bulk mutation is per-object atomic and reports individual conflicts

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./backend/internal/tagging -run 'Association|Creation|Effective' -count=1`

Expected: FAIL on missing association service.

- [ ] **Step 3: Implement the authoritative association API**

```go
func (s *AssociationService) Get(context.Context, GetCommand) (TaggedObject, error)
func (s *AssociationService) History(context.Context, HistoryCommand) ([]HistoryEntry, error)
func (s *AssociationService) ReplaceDirect(context.Context, ReplaceCommand) (TaggedObject, error)
func (s *AssociationService) Bulk(context.Context, BulkCommand) ([]BulkResult, error)
func (s *AssociationService) RequireMeaningful(context.Context, GuardCommand) error
func (s *CreationPreparer) Prepare(context.Context, PrepareCreationCommand) (InitialAssignmentSet, error)
```

`ReplaceDirect` accepts exact desired direct tag IDs, expected object version,
source, actor, reason, correlation/causation IDs, and idempotency key. It
authorizes `classification.apply`, loads the exact target by MSP and Client,
resolves active/merged tags, computes the post-mutation effective set, and
builds one accepted repository mutation.

`History` returns direct assignment events plus Project assignment events that
changed the caller's derived Task classification. Each inherited entry keeps
the source Project ID and is presented as inherited evidence; it does not
create a copied Task event row.

- [ ] **Step 4: Implement supported-object resolution and versioning**

The PostgreSQL repository uses an explicit `switch ObjectType` with fixed SQL;
never interpolate a caller-provided table name. Each branch loads and versions
one of `work_records`, `tasks`, `projects`, `assets`, `knowledge_articles`, or
`time_entries` under `(id, msp_id, client_id, version)`.

For task effective tags, join project tags when:

```sql
task.parent_type = 'project'
AND task.parent_id = project.id
```

or when `parent_type = 'phase'` by joining `phases.project_id`. Subtasks inherit
through their canonical project/phase parent reference, not through copied
assignment rows.

- [ ] **Step 5: Add initial-classification preparation**

`CreationRequireMeaningful` rejects a post-create effective set that would be
empty or `Unclassified`-only. For a Task under a Project or Phase, the
preparer accepts a parent reference and includes derived Project tags in that
check, so inherited meaningful classification can satisfy the invariant
without creating copied direct rows. `CreationAllowFallback` resolves
requested active tags and otherwise returns the MSP's system `Unclassified`
tag. `InitialAssignmentSet` contains canonical direct tag IDs and source
evidence for an existing object repository to insert in its current
transaction.

- [ ] **Step 6: Add PostgreSQL isolation coverage**

Create two clients under one MSP, reuse an object UUID attempt across scopes,
and assert that reads, mutations, effective inheritance, catalog usage counts,
and bulk results never cross the active Client.

Run: `TEST_DATABASE_URL="$TEST_DATABASE_URL" go test ./tests/integration -run TaggingIsolation -count=1`

Expected: PASS when PostgreSQL is configured; otherwise SKIP with the existing
test-suite convention.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/tagging backend/internal/store/psa/tagging_repository.go backend/internal/store/psa/tagging_repository_test.go tests/integration/tagging_isolation_test.go
git commit -m "Add scoped tag associations and inheritance"
```

### Task 4: HTTP routes, capabilities, and application composition

**Files:**
- Create: `backend/internal/httpapi/tagging_routes.go`
- Create: `backend/internal/httpapi/tagging_routes_test.go`
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/internal/httpapi/principal_routes.go`
- Modify: `backend/internal/httpapi/principal_routes_test.go`
- Modify: `backend/internal/setup/postgres.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/cmd/rarity-api/main_test.go`
- Modify: `docs/04-api/rest-api.md`

**Interfaces:**
- Produces: `/api/v1/tag-groups`, `/api/v1/tags`, tag impact/lifecycle routes, `/api/v1/objects/{object_type}/{id}/tags`, and `/api/v1/tag-bulk-actions`.
- Consumes: Task 2 catalog and Task 3 association services.

- [ ] **Step 1: Write strict HTTP contract tests**

Test JSON unknown-field rejection, 1 MiB body limits, `If-Match`, ETags,
reason requirements, capability failures, cross-Client 404 behavior, merged
resolution, and stable error codes:

```text
classification_required
tag_archived
tag_ambiguous
version_conflict
not_found
forbidden
```

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./backend/internal/httpapi -run Tagging -count=1`

Expected: FAIL because routes and dependencies are absent.

- [ ] **Step 3: Add narrow HTTP-facing interfaces and handlers**

Add `TagCatalogActions` and `TagAssociationActions` to `router.go`, register
`registerTaggingRoutes()`, and implement:

```text
GET    /api/v1/tag-groups
POST   /api/v1/tag-groups
PATCH  /api/v1/tag-groups/{id}
GET    /api/v1/tags
POST   /api/v1/tags
PATCH  /api/v1/tags/{id}
GET    /api/v1/tags/{id}/impact
POST   /api/v1/tags/{id}/merge
POST   /api/v1/tags/{id}/archive
GET    /api/v1/classification/health
GET    /api/v1/classification/migration-runs
GET    /api/v1/objects/{object_type}/{id}/tags
GET    /api/v1/objects/{object_type}/{id}/tag-history
PUT    /api/v1/objects/{object_type}/{id}/tags
POST   /api/v1/tag-bulk-actions
```

The server derives actor, Client, source, correlation, and object scope. It
never trusts those values from the request body.

- [ ] **Step 4: Add capabilities and first-run system catalog**

Add `classification.manage`, `classification.apply`,
`classification.report`, and `classification.ai.manage` to
`globalAdminCapabilities`; expose appropriate UI capabilities. Add
`classification-settings` and `classification-insights` to
`navigationCapabilities`. During first-run bootstrap, create the system group
and `Unclassified` tag in the same bootstrap transaction or invoke the
idempotent `EnsureSystemCatalog` before setup completion.

- [ ] **Step 5: Compose and test the services**

Construct one `TaggingRepository`, `CatalogService`, `AssociationService`, and
`CreationPreparer` in `buildDependencies`. Assert every new router dependency
is non-nil in `main_test.go`.

- [ ] **Step 6: Document the API**

Document exact routes, version headers, object types, assignment sources,
effective-tag semantics, capability rules, and error codes in
`docs/04-api/rest-api.md`.

- [ ] **Step 7: Run focused tests**

Run: `go test ./backend/internal/httpapi ./backend/internal/setup ./backend/cmd/rarity-api -run 'Tagging|Principal|SystemCatalog|Dependencies' -count=1`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/httpapi backend/internal/setup/postgres.go backend/cmd/rarity-api docs/04-api/rest-api.md
git commit -m "Expose governed tagging APIs"
```

### Task 5: Atomic initial tags and terminal guards on every supported object

**Files:**
- Modify: `backend/internal/workrecords/service.go`
- Modify: `backend/internal/workrecords/transition.go`
- Modify: `backend/internal/tasks/service.go`
- Modify: `backend/internal/projects/service.go`
- Modify: `backend/internal/projects/conversion_service.go`
- Modify: `backend/internal/clientresources/service.go`
- Modify: `backend/internal/knowledge/service.go`
- Modify: `backend/internal/timeentries/service.go`
- Modify: `backend/internal/billingexport/service.go`
- Modify: `backend/internal/datto/alert_incident.go`
- Modify: `backend/internal/store/psa/work_record_repository.go`
- Modify: `backend/internal/store/psa/task_repository.go`
- Modify: `backend/internal/store/psa/project_repository.go`
- Modify: `backend/internal/store/psa/asset_repository.go`
- Modify: `backend/internal/store/psa/datto_repository.go`
- Modify: `backend/internal/store/psa/knowledge_repository.go`
- Modify: `backend/internal/store/psa/time_entry_repository.go`
- Modify: `backend/internal/store/psa/billing_export_repository.go`
- Modify: `backend/internal/workrecords/service_test.go`
- Modify: `backend/internal/workrecords/transition_test.go`
- Modify: `backend/internal/tasks/service_test.go`
- Modify: `backend/internal/projects/service_test.go`
- Modify: `backend/internal/projects/conversion_service_test.go`
- Modify: `backend/internal/clientresources/service_test.go`
- Modify: `backend/internal/knowledge/service_test.go`
- Modify: `backend/internal/timeentries/service_test.go`
- Modify: `backend/internal/billingexport/service_test.go`
- Modify: `backend/internal/datto/alert_incident_test.go`
- Modify: `backend/internal/store/psa/work_record_repository_test.go`
- Modify: `backend/internal/store/psa/task_repository_test.go`
- Modify: `backend/internal/store/psa/project_repository_test.go`
- Modify: `backend/internal/store/psa/asset_repository_test.go`
- Modify: `backend/internal/store/psa/datto_repository_test.go`
- Modify: `backend/internal/store/psa/knowledge_repository_test.go`
- Modify: `backend/internal/store/psa/time_entry_repository_test.go`
- Modify: `backend/internal/store/psa/billing_export_repository_test.go`

**Interfaces:**
- Consumes: `CreationPreparer.Prepare`, `InitialAssignmentSet`, and `TerminalGuard.RequireMeaningful`.
- Produces: atomic initial associations for all six supported types and terminal-state enforcement at existing lifecycle boundaries.

- [ ] **Step 1: Add failing creation-policy tests**

For each supported object, add one interactive test that rejects missing tags
and one trusted-system test that persists `Unclassified`. Assert the object,
assignments, object audit/outbox, and tag assignment event are one transaction.

The command additions are exact:

```go
TagIDs []string
ClassificationPolicy tagging.CreationPolicy
```

HTTP technician routes always set `CreationRequireMeaningful`. Opportunity
conversion, Datto-discovered assets, and Datto alert incidents set
`CreationAllowFallback`. Existing direct, Graph, and forwarding intake paths
persist inbound events rather than supported taggable objects; their future
object-materialization step must use `CreationAllowFallback`.

- [ ] **Step 2: Run the creation tests and verify failure**

Run: `go test ./backend/internal/{workrecords,tasks,projects,clientresources,knowledge,timeentries} ./backend/internal/store/psa -run 'InitialTags|ClassificationPolicy' -count=1`

Expected: FAIL because commands and mutations lack initial tags.

- [ ] **Step 3: Extend mutations and repositories atomically**

Add `InitialTags tagging.InitialAssignmentSet` to each create mutation. Add one
package-local PostgreSQL helper:

```go
func insertInitialTagAssignments(
	ctx context.Context,
	tx transaction,
	target tagging.TargetRef,
	version int64,
	initial tagging.InitialAssignmentSet,
) error
```

Call it before audit/outbox insertion inside the existing object transaction.
The helper inserts direct assignments and append-only assignment events; a
failure rolls back the object.

- [ ] **Step 4: Wire interactive and automated callers**

Add `tagging.CreationPreparer` to the affected service constructors. Update
work-record, task, project, asset, knowledge, and time-entry HTTP DTOs to accept
`tag_ids`. Update Datto alert incident creation and opportunity conversion to
request fallback classification. In `DattoRepository.Apply`, call a fixed-SQL
`insertSystemFallbackAssignment` helper for newly discovered assets in the
same transaction as the asset insert.

- [ ] **Step 5: Add terminal guards**

Call `RequireMeaningful` before:

- a Work Record transition whose workflow destination resolves or cancels work
- knowledge publication
- time-entry approval and export

Preserve the attempted user action on HTTP failure by returning
`classification_required` with the tagged-object recovery link. Tasks and
Projects have no general terminal command today; document the guard as a
required dependency for any future terminal mutation rather than inventing a
new lifecycle API.

- [ ] **Step 6: Run cross-module tests**

Run: `go test ./backend/internal/{workrecords,tasks,projects,clientresources,knowledge,timeentries,billingexport,intake,datto} ./backend/internal/store/psa -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/workrecords backend/internal/tasks backend/internal/projects backend/internal/clientresources backend/internal/knowledge backend/internal/timeentries backend/internal/billingexport backend/internal/intake backend/internal/datto backend/internal/store/psa backend/internal/httpapi
git commit -m "Require classification on supported objects"
```

### Task 6: Shared frontend tag picker and object editor

**Files:**
- Create: `frontend/src/features/classification/types.ts`
- Create: `frontend/src/features/classification/api.ts`
- Create: `frontend/src/features/classification/api.test.ts`
- Create: `frontend/src/features/classification/ObjectTagEditor.tsx`
- Create: `frontend/src/features/classification/ObjectTagEditor.test.tsx`
- Create: `frontend/src/design-system/components/fields/TagPicker.tsx`
- Create: `frontend/src/design-system/components/fields/TagPicker.test.tsx`
- Create: `frontend/src/design-system/components/data/TagChip.tsx`
- Modify: `frontend/src/design-system/components/fields/fields.css`
- Modify: `frontend/src/design-system/components/data/data.css`
- Modify: `frontend/src/design-system/index.ts`
- Modify: `frontend/src/design-system/catalog/DesignSystemCatalog.tsx`

**Interfaces:**
- Produces: `ClassificationAPI`, `TagPicker`, `TagChip`, and `ObjectTagEditor`.
- Consumes: Task 4 HTTP contracts.

- [ ] **Step 1: Define the browser types and API tests**

```ts
export type Tag = {
  id: string;
  label: string;
  groupId: string;
  state: "active" | "merged" | "archived";
  synonyms: string[];
  version: number;
};

export type TaggedObject = {
  target: { objectType: TagObjectType; objectId: string };
  objectVersion: number;
  direct: TagAssignment[];
  inherited: TagAssignment[];
  effective: TagAssignment[];
  classificationState: "meaningful" | "unclassified" | "required";
};
```

API tests must assert snake_case mapping, `If-Match`, AbortSignal propagation,
stable error-code parsing, and no client/actor/source values in request bodies.

- [ ] **Step 2: Run frontend tests and verify failure**

Run: `npm --prefix frontend test -- src/features/classification src/design-system/components/fields/TagPicker.test.tsx`

Expected: FAIL because the feature and components do not exist.

- [ ] **Step 3: Implement the accessible picker**

`TagPicker` props:

```ts
type TagPickerProps = {
  label: string;
  groups: TagGroup[];
  tags: Tag[];
  selectedIds: string[];
  inheritedIds?: string[];
  suggestions?: TagSuggestion[];
  required?: boolean;
  disabled?: boolean;
  error?: string;
  onChange(ids: string[]): void;
  onSuggestionDecision?(id: string, decision: "accept" | "dismiss"): void;
};
```

Implement searchable grouped listbox behavior, exact-label and synonym search,
recent/frequent sections, keyboard selection/removal, inherited non-removable
chips, visible labels, and announced validation. Do not expose internal keys.

- [ ] **Step 4: Implement optimistic object editing**

`ObjectTagEditor` loads catalog plus tagged object, separates direct and
inherited chips, PUTs the complete desired direct set with the current ETag,
handles 409 by reloading and showing submitted/current comparison, and moves
focus to the error on `classification_required`. It also loads tag history and
renders source, actor, timestamp, before/after operation, and source-Project
navigation in plain language.

- [ ] **Step 5: Add catalog examples and accessibility checks**

Render default, loading, suggested, inherited, required-error, archived, and
disabled states in the design-system catalog. Use axe in tests and verify every
chip exposes label, source, and permitted removal. Add screen-reader
announcements for result counts, selection changes, inherited sources, AI
suggestion decisions, save completion, and validation failure.

- [ ] **Step 6: Run tests and build**

Run: `npm --prefix frontend test -- src/features/classification src/design-system && npm --prefix frontend run build`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/features/classification frontend/src/design-system
git commit -m "Add shared classification controls"
```

### Task 7: Classification administration workspace

**Files:**
- Create: `frontend/src/features/classification/ClassificationSettingsPage.tsx`
- Create: `frontend/src/features/classification/ClassificationSettingsPage.test.tsx`
- Create: `frontend/src/features/classification/classification.css`
- Modify: `frontend/src/navigation.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/api/browserSession.ts`

**Interfaces:**
- Produces: `#/classification-settings`.
- Consumes: catalog, impact, lifecycle, classification-health, and migration-history endpoints.

- [ ] **Step 1: Write route and administrator-flow tests**

Test permission-filtered navigation, catalog search, group ordering, create,
rename, regroup, synonyms, merge preview/confirmation, archive fallback
preview, stale-version recovery, and system-tag protection.

- [ ] **Step 2: Run tests and verify failure**

Run: `npm --prefix frontend test -- src/features/classification/ClassificationSettingsPage.test.tsx src/App.test.tsx`

Expected: FAIL because the route and page are absent.

- [ ] **Step 3: Implement the page**

Create local tabs for Catalog, Groups, Classification health, and Migration
history. Task 9 adds AI policy after its backend contract exists. Use
structured forms only. Impact actions must display authorized object counts,
saved-view/report/automation references, the exact fallback consequence, a
required reason, and the expected tag version.

- [ ] **Step 4: Register the protected route**

Add `"classification-settings"` to `Page`, navigation, grouping, renderer, and
tests. Show it only when the principal navigation payload includes it.

- [ ] **Step 5: Run tests and responsive checks**

Run: `npm --prefix frontend test -- src/features/classification src/App.test.tsx && npm --prefix frontend run build`

Expected: PASS at 390, 834, 1280, and 1440 CSS-pixel widths without horizontal
overflow.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/features/classification frontend/src/navigation.ts frontend/src/App.tsx frontend/src/App.test.tsx frontend/src/api/browserSession.ts
git commit -m "Add classification administration workspace"
```

### Task 8: Integrate tags into every supported frontend object flow

**Files:**
- Modify: `frontend/src/features/work/GlobalWorkActions.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.tsx`
- Modify: `frontend/src/features/work/GlobalWorkActions.test.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.test.tsx`
- Modify: `frontend/src/features/work/api.ts`
- Modify: `frontend/src/features/work/api.test.ts`
- Modify: `frontend/src/features/work/work.css`
- Modify: `frontend/src/features/projects/ProjectPage.tsx`
- Modify: `frontend/src/features/projects/ProjectWorklist.tsx`
- Modify: `frontend/src/features/projects/ProjectWorklist.test.tsx`
- Modify: `frontend/src/features/projects/types.ts`
- Modify: `frontend/src/features/projects/api.ts`
- Modify: `frontend/src/features/projects/projects.css`
- Modify: `frontend/src/features/knowledge/KnowledgePage.tsx`
- Modify: `frontend/src/features/knowledge/KnowledgePage.test.tsx`
- Modify: `frontend/src/features/knowledge/knowledge.css`
- Modify: `frontend/src/features/organization/ClientResourcesPage.tsx`
- Modify: `frontend/src/features/organization/ClientResourcesPage.test.tsx`
- Modify: `frontend/src/features/billing/BillingPage.tsx`
- Modify: `frontend/src/features/billing/BillingPage.test.tsx`
- Modify: `frontend/src/features/sales/OpportunityPage.tsx`
- Modify: `frontend/src/features/sales/OpportunityPage.test.tsx`

**Interfaces:**
- Consumes: `TagPicker` and `ObjectTagEditor`.
- Produces: required create-time classification and editable direct/effective tags across all six object types.

- [ ] **Step 1: Add failing feature integration tests**

Across the supported surfaces, assert:

- each existing interactive ticket, task, asset, knowledge, and time-entry
  create form is disabled until one meaningful effective tag is selected
- request payload contains canonical `tag_ids`
- `Unclassified` is visibly flagged on automatically converted Projects and
  other loaded automated objects
- inherited Project tags on Tasks are labeled and non-removable
- selecting the inheritance source navigates to the Project
- a server classification error focuses the shared picker
- Type, Status/Queue, Priority, billing classification, and Tags remain separate

- [ ] **Step 2: Run tests and verify failure**

Run: `npm --prefix frontend test -- src/features/{work,projects,knowledge,organization,billing}`

Expected: FAIL on absent tagging controls and payload fields.

- [ ] **Step 3: Add required tags to create forms**

Load the catalog once per active Client session, use one controlled
`TagPicker`, and send selected IDs for interactive tickets, tasks, assets,
knowledge drafts, and time entries. Project creation currently occurs through
automatic opportunity conversion and therefore uses the backend fallback from
Task 5. Never accept free-form text as a tag.

- [ ] **Step 4: Add effective-tag reading and editing**

Render compact `TagChip` collections on worklists. Mount `ObjectTagEditor` in
ticket actions, Project detail, Task detail rows, Asset detail, Knowledge
detail, and Billing time-entry detail. Preserve active filters after edits.

- [ ] **Step 5: Verify all supported surfaces**

Run: `npm --prefix frontend test -- src/features && npm --prefix frontend run build`

Expected: PASS with no generic Category control or label introduced.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/features
git commit -m "Integrate classification across product objects"
```

### Task 9: Permission-aware AI classification and automatic application

**Files:**
- Create: `backend/migrations/000081_ai_classification_subjects.sql`
- Create: `backend/migrations/ai_classification_subjects_contract_test.go`
- Modify: `backend/internal/aiassist/service.go`
- Modify: `backend/internal/aiassist/adapter.go`
- Modify: `backend/internal/aiassist/jobs.go`
- Modify: `backend/internal/aiassist/management.go`
- Modify: `backend/internal/store/psa/ai_job_repository.go`
- Modify: `backend/internal/store/psa/ai_management_repository.go`
- Create: `backend/internal/tagging/classification.go`
- Create: `backend/internal/tagging/classification_test.go`
- Create: `backend/internal/tagging/classification_worker.go`
- Create: `backend/internal/tagging/classification_worker_test.go`
- Modify: `backend/internal/httpapi/tagging_routes.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `frontend/src/features/classification/ObjectTagEditor.tsx`
- Modify: `frontend/src/features/classification/ClassificationSettingsPage.tsx`
- Modify: `backend/internal/aiassist/service_test.go`
- Modify: `backend/internal/aiassist/adapter_contract_test.go`
- Modify: `backend/internal/aiassist/adapter_review_test.go`
- Modify: `backend/internal/aiassist/jobs_test.go`
- Modify: `backend/internal/aiassist/management_test.go`
- Modify: `backend/internal/store/psa/ai_job_repository_test.go`
- Modify: `backend/internal/store/psa/ai_management_repository_test.go`
- Modify: `backend/internal/httpapi/tagging_routes_test.go`
- Modify: `backend/cmd/rarity-api/main_test.go`
- Modify: `frontend/src/features/classification/ObjectTagEditor.test.tsx`
- Modify: `frontend/src/features/classification/ClassificationSettingsPage.test.tsx`

**Interfaces:**
- Produces: AI feature `classification`, structured per-tag suggestions, `/api/v1/classification/ai-policy`, suggestion decisions, and an application worker.
- Consumes: existing provider connections/models/cost governance and Task 3 association service.

- [ ] **Step 1: Write structured-output and authority tests**

Assert:

- provider output must be exactly `{suggestions:[{tag_id,confidence,rationale}]}`
- every tag ID is a subset of the authorized active candidate set
- confidence is within `[0,1]` and rationale is bounded
- restricted/secret/attachment fields are excluded
- automatic application is off until an admin enables it
- only suggestions at or above the stored threshold auto-apply
- human-selected tags are never removed
- provider failure leaves manual classification or `Unclassified`
- accept/dismiss decisions are append-only and auditable

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./backend/internal/aiassist ./backend/internal/tagging -run Classification -count=1`

Expected: FAIL on missing feature and structured output.

- [ ] **Step 3: Add the classification provider contract**

Migration `000081_ai_classification_subjects.sql` adds `subject_type` and
`subject_id`, backfills them from `work_record_id`, makes `work_record_id`
nullable, adds supported-subject checks, and expands AI feature checks with
`classification`. Add `FeatureClassification`. Generalize AI job identity to a
typed subject without breaking existing work-record endpoints:

```go
type SubjectRef struct {
	Type string
	ID string
	MSPID string
	ClientID string
}

type ClassificationCandidate struct {
	TagID string `json:"tag_id"`
	Confidence float64 `json:"confidence"`
	Rationale string `json:"rationale"`
}
```

Keep existing recommendation JSON for summary/reply/similar features. For
classification, use a separate fixed system instruction and strict parser.
Persist per-tag rows in `tag_ai_suggestion_items`.

- [ ] **Step 4: Build authorized context and automatic application**

The tagging classification service loads the subject under exact MSP/Client
scope, emits only approved standard fields, and supplies only active catalog
tag IDs. The application worker rechecks policy version, tag state, subject
visibility, current object version, threshold, and provider evidence before
calling `ReplaceDirect` with `SourceAIAutomatic`.

- [ ] **Step 5: Add policy and decision routes**

```text
GET   /api/v1/classification/ai-policy
PATCH /api/v1/classification/ai-policy
POST  /api/v1/objects/{object_type}/{id}/classification-suggestions
GET   /api/v1/classification-suggestions/{id}
POST  /api/v1/classification-suggestions/{id}/decide
```

Policy mutation requires `classification.ai.manage`; manual suggestion request
and decision require `classification.apply` plus object access.

- [ ] **Step 6: Wire the frontend**

Display suggestions with label, confidence, rationale, Accept, and Dismiss.
Add the administrator enablement switch, threshold control constrained to
0.500–1.000, model selection, retained/change rates, and provider failure
health.

- [ ] **Step 7: Run AI and classification suites**

Run: `go test ./backend/internal/{aiassist,tagging,httpapi} ./backend/internal/store/psa -run 'AI|Classification' -count=1 && npm --prefix frontend test -- src/features/classification`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/migrations/000081_ai_classification_subjects.sql backend/migrations/ai_classification_subjects_contract_test.go backend/internal/aiassist backend/internal/tagging backend/internal/store/psa backend/internal/httpapi backend/cmd/rarity-api/main.go frontend/src/features/classification
git commit -m "Add controlled AI classification"
```

### Task 10: Tag-driven automation with loop protection

**Files:**
- Modify: `backend/internal/automation/definition.go`
- Modify: `backend/internal/automation/definition_test.go`
- Modify: `backend/internal/automation/actions.go`
- Modify: `backend/internal/automation/actions_test.go`
- Modify: `backend/internal/store/psa/snapshot_store.go`
- Modify: `backend/internal/store/psa/snapshot_store_test.go`
- Modify: `backend/internal/automation/execution.go`
- Modify: automation repositories and tests under `backend/internal/store/psa`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `frontend/src/features/automation/AutomationPage.tsx`
- Modify: `frontend/src/features/automation/AutomationPage.test.tsx`

**Interfaces:**
- Produces: `add_tags`, `remove_tags`, tag any/all/none/group conditions, tag-event triggers, and mutation-fingerprint loop prevention.
- Consumes: association service and `classification.apply`.

- [ ] **Step 1: Write definition and runtime tests**

Add constants:

```go
ActionAddTags ActionKind = "add_tags"
ActionRemoveTags ActionKind = "remove_tags"
OperatorHasAnyTag ConditionOperator = "has_any_tag"
OperatorHasAllTags ConditionOperator = "has_all_tags"
OperatorHasNoTags ConditionOperator = "has_no_tags"
OperatorHasTagInGroup ConditionOperator = "has_tag_in_group"
```

Test immutable tag IDs, active-catalog validation at publish time, source
conditions, `tag.added`/`tag.removed` triggers, inherited-removal rejection,
minimum-tag preservation, same-mutation idempotency, correlation-chain depth,
and circular rule diagnostics.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./backend/internal/automation ./backend/internal/store/psa -run 'Tag|Loop' -count=1`

Expected: FAIL because tag actions/operators are unsupported.

- [ ] **Step 3: Extend validation and snapshots**

Store tag IDs as a JSON array parameter decoded into a bounded maximum of 50
unique UUIDs. Snapshot tag events with subject identity/version, direct and
effective tag IDs, group IDs, assignment source, and causation ID. Never place
labels or object content in the authoritative condition snapshot.

- [ ] **Step 4: Execute through the association service**

Extend `RuntimeActionExecutor` with `TagActions`. Build the desired direct set,
call `ReplaceDirect` using the automation run/step mutation fingerprint as the
idempotency key, and pass the run ID as causation. A repeated fingerprint
returns the prior result; a contradictory cycle stops at the existing maximum
depth and records a safe diagnostic.

- [ ] **Step 5: Replace raw tag parameters in the UI**

Use `TagPicker` for tag conditions/actions and a group select for
`has_tag_in_group`. Show inherited-removal and minimum-classification
constraints before publishing.

- [ ] **Step 6: Run automation tests**

Run: `go test ./backend/internal/automation ./backend/internal/store/psa -run 'Automation|Tag|Loop' -count=1 && npm --prefix frontend test -- src/features/automation`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/automation backend/internal/store/psa backend/cmd/rarity-api/main.go frontend/src/features/automation
git commit -m "Add tag driven automation"
```

### Task 11: Reporting projections, recurring issues, and insights UI

**Files:**
- Create: `backend/internal/tagging/reporting.go`
- Create: `backend/internal/tagging/reporting_test.go`
- Create: `backend/internal/tagging/projection.go`
- Create: `backend/internal/tagging/projection_test.go`
- Create: `backend/internal/store/psa/tagging_projection_repository.go`
- Create: `backend/internal/store/psa/tagging_projection_repository_test.go`
- Modify: `backend/internal/httpapi/tagging_routes.go`
- Modify: `backend/internal/httpapi/tagging_routes_test.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Create: `frontend/src/features/classification/ClassificationInsightsPage.tsx`
- Create: `frontend/src/features/classification/ClassificationInsightsPage.test.tsx`
- Modify: `frontend/src/features/classification/api.ts`
- Modify: `frontend/src/navigation.ts`
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- Produces: authorized usage, combinations, trends, classification health, recurring issues, projection freshness, and `#/classification-insights`.
- Consumes: append-only assignment events and canonical effective-tag resolver.

- [ ] **Step 1: Write projection and report tests**

Cover additions/removals by day, source, type, tag, and Client; ordered
co-occurrence pairs; rename/merge continuity; direct versus inherited filters;
any/all/none filtering; `Unclassified` age; recurring-window evidence;
idempotent cursor replay; and no cross-Client aggregate leakage.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./backend/internal/tagging ./backend/internal/store/psa -run 'Projection|Report|Recurring' -count=1`

Expected: FAIL because reporting units do not exist.

- [ ] **Step 3: Implement the projection worker**

Read `tag_assignment_events` after the `(occurred_at, id)` cursor, update daily
usage and canonical ordered tag-pair rows in one transaction, then advance the
cursor. For a Project event, resolve its current Tasks and update inherited
usage/co-occurrence projections for the affected Task IDs without creating
direct assignment rows. Replay must be idempotent. Expose `RunOnce(ctx, 500)`
and run it on the existing bounded worker loop with structured counts and lag
logging.

- [ ] **Step 4: Implement authorized report queries**

Expose:

```text
GET /api/v1/tag-reports/usage
GET /api/v1/tag-reports/combinations
GET /api/v1/tag-reports/trends
GET /api/v1/tag-reports/classification-health
GET /api/v1/tag-reports/recurring-issues
```

Require `classification.report`, an active Client, bounded date ranges, at most
50 tag IDs, and stable pagination for evidence drill-down. Return
`projection_as_of` on every aggregate response. Recurring results include the
matching authorized object IDs and never create a Problem automatically.

- [ ] **Step 5: Build the insights page**

Add filters for Client, object type, group, tags, any/all/none, source,
technician, team, priority, status, and date range. Render classification
health, trend tables/charts, co-occurrence, and recurring issues with preserved
filters when opening underlying records.

- [ ] **Step 6: Run reporting tests and build**

Run: `go test ./backend/internal/tagging ./backend/internal/store/psa ./backend/internal/httpapi -run 'Projection|Report|Recurring' -count=1 && npm --prefix frontend test -- src/features/classification/ClassificationInsightsPage.test.tsx && npm --prefix frontend run build`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/tagging backend/internal/store/psa backend/internal/httpapi backend/cmd/rarity-api/main.go frontend/src/features/classification frontend/src/navigation.ts frontend/src/App.tsx
git commit -m "Add classification reporting and trends"
```

### Task 12: Category cutover, documentation, and release verification

**Files:**
- Create: `backend/internal/tagging/migration.go`
- Create: `backend/internal/tagging/migration_test.go`
- Modify: `backend/cmd/rarity-admin/main.go`
- Modify: `backend/cmd/rarity-admin/main_test.go`
- Create: `scripts/validate-classification-cutover.mjs`
- Modify: `scripts/validate-docs.mjs`
- Modify: `docs/01-architecture/02-core-domain-model.md`
- Modify: `docs/01-architecture/06-workflow-engine.md`
- Modify: `docs/01-architecture/07-automation-engine.md`
- Modify: `docs/01-architecture/08-ai-platform.md`
- Modify: `docs/04-api/rest-api.md`
- Modify: `docs/08-modules/ticket-intelligence.md`
- Create: `tests/integration/tagging_acceptance_test.go`
- Create: `frontend/tests/e2e/classification.spec.ts`

**Interfaces:**
- Produces: `rarity-admin classification-preflight`, a source/runtime cutover validator, complete documentation, and end-to-end release evidence.
- Consumes: every preceding task.

- [ ] **Step 1: Write cutover and acceptance tests**

The preflight must report:

- supported-object totals
- meaningful, `Unclassified`, and invalid classification totals
- unresolved merged/archived references
- projection lag
- AI policy state
- Category source detection

The source validator fails on a generic `category` property attached to any of
the six supported object contracts, but explicitly permits
`forecast_category`, `content_classification`, and billing classification.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./backend/internal/tagging ./backend/cmd/rarity-admin -run 'Migration|ClassificationPreflight' -count=1 && node scripts/validate-classification-cutover.mjs`

Expected: FAIL because the preflight and validator do not exist.

- [ ] **Step 3: Implement the preflight and verified no-op Category migration**

The current repository has no generic Category column or API. Record that fact
in `classification_migration_runs`, validate the Task 1 `Unclassified`
backfill, and refuse a successful preflight if any supported object lacks an
effective tag. Do not rename or remove opportunity `forecast_category`,
notification `content_classification`, AI context classification, or time-entry
billing classification.

- [ ] **Step 4: Update architecture and API documentation**

Document Type/Status/Tags separation, global governance, inheritance, AI
authority, automation events, report freshness, capabilities, terminal guards,
and Category removal. Update the AI architecture statement that currently
defers classification.

- [ ] **Step 5: Add integrated acceptance coverage**

The Go acceptance test creates two Clients and exercises catalog governance,
interactive and automated creation, dynamic inheritance, AI automatic
thresholding, automation loop protection, reports, merge/archive continuity,
and scope isolation.

The Playwright test uses keyboard-only tagging, fixes an `Unclassified` record,
verifies inherited tags, accepts an AI suggestion, previews an archive, and
drills from a recurring-issue report to authorized records. Run axe on settings,
picker, and insights states.

- [ ] **Step 6: Run the complete verification matrix**

Run:

```bash
go test ./backend/... ./tests/integration
npm --prefix frontend test
npm --prefix frontend run build
npm --prefix frontend exec -- playwright test classification.spec.ts
node scripts/validate-classification-cutover.mjs
node scripts/validate-docs.mjs
git diff --check
```

Expected: PASS. PostgreSQL integration tests may SKIP only when
`TEST_DATABASE_URL` is absent; release evidence requires rerunning them with a
real PostgreSQL database.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/tagging backend/cmd/rarity-admin scripts docs tests/integration frontend/tests/e2e
git commit -m "Complete classification cutover and acceptance"
```

---

## Final Review Gate

Before merging:

- [ ] Confirm every supported object creation path chooses
  `CreationRequireMeaningful` or `CreationAllowFallback` explicitly.
- [ ] Confirm no supported object can exist without an effective tag.
- [ ] Confirm terminal transitions reject `Unclassified` and archived-only
  evidence.
- [ ] Confirm inherited tags are derived, source-labeled, and non-removable on
  Tasks.
- [ ] Confirm catalog counts, reports, AI, and automation enforce active Client
  authorization.
- [ ] Confirm AI automatic application is opt-in, thresholded, and additive.
- [ ] Confirm tag automation is idempotent and bounded against loops.
- [ ] Confirm merged and archived identities preserve historical reporting.
- [ ] Confirm generic Category is absent while legitimate forecast, content,
  AI-context, and billing classifications remain intact.
- [ ] Confirm all focused, full, integration, browser, accessibility, build,
  documentation, and cutover checks pass.
