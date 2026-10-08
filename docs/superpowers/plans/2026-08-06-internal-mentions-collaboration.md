# Internal Mentions and Collaboration Implementation Plan

**Execution record:** Dated plan retained for design and delivery history. Original checkboxes are not current completion status. Consult the [plan index](../README.md) and [current execution backlog](../../09-roadmap/current-execution.md) before executing any remaining step; do not replay implemented migrations or deployment commands.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver internal individual and organizational-team mentions across tickets, tasks, and projects, with one permission-safe dashboard widget item per recipient/object, exact-source deep links, and policy-driven external notifications.

**Architecture:** Store internal collaboration content as structured text plus stable mention tokens. A dedicated mention service validates new tokens, snapshots direct/team recipient resolutions, and atomically maintains immutable occurrences plus one recipient/object widget projection. The dashboard reads that projection; email and Teams delivery consume content-free outbox events through the existing notification runtime.

**Tech Stack:** Go 1.25 with the Go 1.26.5 toolchain, PostgreSQL/pgx, goose migrations, `net/http`, React 19, TypeScript, Vite, Vitest, Testing Library, Node.js 22.

## Global Constraints

- Only active internal staff may create or receive mentions.
- Creating mentions requires `mention.create`; receiving and reading the widget
  requires `mention.read`.
- Mentions are allowed only in internal ticket, task, and project details, comments, and notes.
- Public/customer-visible content and outbound messages must reject structured mention tokens.
- Mention targets are active staff and existing organizational teams; do not create mention-specific groups.
- Team expansion snapshots current active eligible members at source-save time and records exclusions.
- A mention never grants access or changes ownership, assignment, participation, watcher, or task state.
- Keep one `mention_item` per recipient and parent object; every later occurrence restores it to unread, including from archive.
- The Mentions dashboard widget is the sole in-app surface; do not create generic in-app notification rows.
- Events, delivery records, logs, metrics, and mention rows must never contain source bodies or preview text.
- Previews are authorized, sanitized, bounded, and loaded from the source at read time.
- AI mention targets, mention intents, and historical plain-`@name` backfill are forbidden.
- Preserve existing plain-text content; only newly selected structured tokens generate occurrences.
- All mutating APIs use trusted principal scope, strict JSON decoding, stable error codes, and optimistic versions.
- Node.js 22 is required for frontend and documentation commands.

---

## File and Responsibility Map

### Backend domain

- `backend/internal/organizations/memberships.go` — organizational-team membership replacement and audit contract.
- `backend/internal/collaboration/model.go` — shared internal details/comment/note source model.
- `backend/internal/collaboration/service.go` — internal-only content commands and mention transaction boundary.
- `backend/internal/mentions/model.go` — token, occurrence, resolution, item, widget, and deep-link types.
- `backend/internal/mentions/tokens.go` — structured-token validation and prior/new diffing.
- `backend/internal/mentions/resolver.go` — direct/team candidate and recipient resolution rules.
- `backend/internal/mentions/service.go` — occurrence creation, deduplication, item state transitions, and mutation facts.
- `backend/internal/mentions/query.go` — candidates, widget pages/counts, previews, and deep-link resolution.
- `backend/internal/mentions/invalidation.go` — access-loss suppression and pending-delivery cancellation.

### PostgreSQL and composition

- `backend/migrations/000082_team_memberships.sql` — durable organizational-team memberships and mention capability grants.
- `backend/migrations/000083_internal_mentions.sql` — internal sources, tokens, occurrences, resolutions, items, preferences, invalidation, and delivery metadata.
- `backend/internal/store/psa/directory_repository.go` — membership replacement and directory projection.
- `backend/internal/store/psa/collaboration_repository.go` — source/mention atomic writes and source reads.
- `backend/internal/store/psa/mention_repository.go` — candidates, widget, state, deep-link, suppression, and notification queries.
- `backend/internal/store/psa/pgx.go` — repository constructors.
- `backend/cmd/rarity-api/main.go` — service, router, planner, and worker composition.

### HTTP

- `backend/internal/httpapi/collaboration_routes.go` — shared internal-content routes for work records, tasks, and projects.
- `backend/internal/httpapi/mention_routes.go` — candidates, widget, state, and deep-link routes.
- `backend/internal/httpapi/organization_routes.go` — team-membership administration route.
- `backend/internal/httpapi/router.go` — action interfaces, dependencies, and route registration.
- `backend/internal/httpapi/work_record_routes.go` — preserve public comments and delegate internal comments to collaboration.

### Notification runtime

- `backend/internal/notifications/planner.go` — recipient-specific mention planning with no in-app destination.
- `backend/internal/notifications/delivery.go` — delivery-time authorization cancellation and email/Teams dispatch.
- `backend/internal/notifications/email.go` — bounded safe email payload and TLS SMTP transport.
- `backend/internal/notifications/engine.go` — mention-safe delivery summary types.
- `backend/internal/config/config.go` — optional complete SMTP configuration.

### Frontend

- `frontend/src/features/mentions/types.ts` — API and editor types.
- `frontend/src/features/mentions/api.ts` — candidate, widget, state, deep-link, and collaboration requests.
- `frontend/src/features/mentions/MentionEditor.tsx` — structured internal editor.
- `frontend/src/features/mentions/MentionPicker.tsx` — accessible People/Teams selection.
- `frontend/src/features/mentions/MentionsWidget.tsx` — unread/read/archive dashboard workflow.
- `frontend/src/features/mentions/mentions.css` — picker, token, widget, and source-focus presentation.
- `frontend/src/features/collaboration/InternalCollaborationPanel.tsx` — shared details/comments/notes panel.
- `frontend/src/features/home/HomePage.tsx` — existing operational-home host for the widget.
- `frontend/src/features/work/TechnicianWorklist.tsx` — ticket and task collaboration surfaces.
- `frontend/src/features/projects/ProjectWorklist.tsx` — project and project-task collaboration surfaces.
- `frontend/src/app/routes.ts`, `frontend/src/navigation.ts`, and `frontend/src/App.tsx` — occurrence deep-link routing and record focus.

---

### Task 1: Persist Team Memberships and Mention Foundations

**Files:**
- Create: `backend/migrations/000082_team_memberships.sql`
- Create: `backend/migrations/team_memberships_contract_test.go`
- Create: `backend/migrations/000083_internal_mentions.sql`
- Create: `backend/migrations/internal_mentions_contract_test.go`
- Modify: `backend/internal/setup/postgres.go`

**Interfaces:**
- Consumes: existing `teams`, `technicians`, `role_capabilities`, `audit_ledger`, `event_outbox`, and notification runtime tables.
- Produces: `team_memberships`, `internal_collaboration_sources`, `mention_occurrences`, `mention_recipient_resolutions`, `mention_items`, `notification_recipient_preferences`, `mention_access_invalidations`, and recipient-aware notification columns.

- [ ] **Step 1: Write the migration contract tests**

```go
func migrationBody(t *testing.T, name string) string {
	t.Helper()
	body, err := FS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func TestTeamMembershipMigrationContract(t *testing.T) {
	body := migrationBody(t, "000082_team_memberships.sql")
	for _, fragment := range []string{
		"CREATE TABLE team_memberships",
		"FOREIGN KEY (team_id, msp_id) REFERENCES teams",
		"FOREIGN KEY (technician_id, msp_id) REFERENCES technicians",
		"CHECK (lifecycle_state IN ('active', 'inactive'))",
		"'mention.create'",
		"'mention.read'",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("team membership migration missing %q", fragment)
		}
	}
}

func TestInternalMentionsMigrationContract(t *testing.T) {
	body := migrationBody(t, "000083_internal_mentions.sql")
	for _, fragment := range []string{
		"CREATE TABLE internal_collaboration_sources",
		"CHECK (parent_type IN ('work_record', 'task', 'project'))",
		"CHECK (source_kind IN ('details', 'comment', 'note'))",
		"CREATE TABLE mention_occurrences",
		"CREATE TABLE mention_recipient_resolutions",
		"CREATE TABLE mention_items",
		"UNIQUE (msp_id, recipient_id, parent_type, parent_id)",
		"CHECK (state IN ('unread', 'read', 'archived'))",
		"CREATE TABLE notification_recipient_preferences",
		"CREATE TABLE mention_access_invalidations",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("mention migration missing %q", fragment)
		}
	}
}
```

- [ ] **Step 2: Run the tests and verify the migrations do not exist**

Run: `go test ./backend/migrations -run 'Test(TeamMembership|InternalMentions)MigrationContract'`

Expected: FAIL because migrations `000082` and `000083` are missing.

- [ ] **Step 3: Add the team-membership migration**

Define `team_memberships` with the composite MSP foreign keys, active/inactive
lifecycle, version, creation/update actors, timestamps, and an index on active
membership by technician. Insert `mention.create` and `mention.read` for the
`global-admin` role with `ON CONFLICT DO NOTHING`, and add both capabilities to
`globalAdminCapabilities`.

```sql
CREATE TABLE team_memberships (
  team_id uuid NOT NULL,
  technician_id uuid NOT NULL,
  msp_id uuid NOT NULL,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  created_by uuid NOT NULL,
  updated_at timestamptz NOT NULL,
  updated_by uuid NOT NULL,
  PRIMARY KEY (team_id, technician_id),
  FOREIGN KEY (team_id, msp_id) REFERENCES teams(id, msp_id),
  FOREIGN KEY (technician_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (lifecycle_state IN ('active', 'inactive'))
);
```

- [ ] **Step 4: Add the mention schema**

Store the source body as text and tokens as validated JSONB. Use partial unique
indexes to allow only one live `details` source per parent. Keep occurrence and
resolution tables append-only through the existing
`reject_immutable_version_mutation()` trigger.

```sql
CREATE TABLE internal_collaboration_sources (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  parent_type text NOT NULL,
  parent_id uuid NOT NULL,
  source_kind text NOT NULL,
  body text NOT NULL,
  mention_tokens jsonb NOT NULL DEFAULT '[]'::jsonb,
  author_id uuid NOT NULL,
  lifecycle_state text NOT NULL DEFAULT 'active',
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  redacted_at timestamptz,
  FOREIGN KEY (client_id, msp_id)
    REFERENCES client_organizations(id, msp_id),
  FOREIGN KEY (author_id, msp_id) REFERENCES technicians(id, msp_id),
  CHECK (parent_type IN ('work_record', 'task', 'project')),
  CHECK (source_kind IN ('details', 'comment', 'note')),
  CHECK (lifecycle_state IN ('active', 'redacted'))
);

CREATE TABLE mention_items (
  id uuid PRIMARY KEY,
  msp_id uuid NOT NULL,
  client_id uuid NOT NULL,
  recipient_id uuid NOT NULL,
  parent_type text NOT NULL,
  parent_id uuid NOT NULL,
  latest_occurrence_id uuid NOT NULL,
  state text NOT NULL DEFAULT 'unread',
  read_at timestamptz,
  archived_at timestamptz,
  last_mentioned_at timestamptz NOT NULL,
  suppressed_at timestamptz,
  suppression_reason text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (msp_id, recipient_id, parent_type, parent_id),
  FOREIGN KEY (recipient_id, msp_id) REFERENCES technicians(id, msp_id),
  FOREIGN KEY (client_id, msp_id) REFERENCES client_organizations(id, msp_id),
  CHECK (state IN ('unread', 'read', 'archived'))
);
```

`mention_occurrences` references a source ID/revision and stores token,
author, target type/ID, safe label snapshot, time, correlation, and causation.
`mention_recipient_resolutions` references an occurrence and candidate staff
member and stores `eligible|excluded`, `direct|team|both`, contributing team
IDs, decision time, and a safe reason code. The occurrence and resolution
tables contain no source-body columns.

Add `mention_occurrence_id` and `recipient_technician_id` to
`notification_deliveries`, with foreign keys and a recipient-aware index.
Add `notification_recipient_preferences` keyed by technician and event type,
with email/Teams enablement, IANA time zone, optional local quiet-hour start
and end, and optimistic version.
Do not add source body or preview columns anywhere in the mention or delivery
tables.

- [ ] **Step 5: Run the migration tests**

Run: `go test ./backend/migrations -run 'Test(TeamMembership|InternalMentions)MigrationContract'`

Expected: PASS.

- [ ] **Step 6: Run the migration package**

Run: `go test ./backend/migrations`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/migrations/000082_team_memberships.sql backend/migrations/000083_internal_mentions.sql backend/migrations/team_memberships_contract_test.go backend/migrations/internal_mentions_contract_test.go backend/internal/setup/postgres.go
git commit -m "feat: add internal mention persistence"
```

### Task 2: Complete Organizational-Team Membership Administration

**Files:**
- Create: `backend/internal/organizations/memberships.go`
- Create: `backend/internal/organizations/memberships_test.go`
- Modify: `backend/internal/organizations/directory.go`
- Modify: `backend/internal/store/psa/directory_repository.go`
- Modify: `backend/internal/store/psa/directory_repository_test.go`
- Modify: `backend/internal/httpapi/organization_routes.go`
- Modify: `backend/internal/httpapi/organization_routes_test.go`
- Modify: `backend/internal/httpapi/router.go`
- Modify: `frontend/src/features/organization/DirectorySettingsPage.tsx`
- Modify: `frontend/src/features/organization/DirectorySettingsPage.test.tsx`

**Interfaces:**
- Consumes: `team_memberships` and existing MSP-global `organization.manage`.
- Produces: `ReplaceTeamMembers(context.Context, ReplaceTeamMembersCommand) (Team, error)` and `Team.MemberIDs []string`.

- [ ] **Step 1: Write failing service tests**

```go
func TestReplaceTeamMembersRequiresMSPGlobalOrganizationManage(t *testing.T) {
	repository := &membershipRepository{}
	service := NewDirectoryService(repository, fixedNow, sequentialIDs())
	_, err := service.ReplaceTeamMembers(context.Background(), ReplaceTeamMembersCommand{
		Principal: scopedPrincipal("client-1", "organization.manage"),
		TeamID: "team-1", TechnicianIDs: []string{"tech-1"},
		ExpectedVersion: 1, Reason: "Staffing change", Source: "organization.settings",
	})
	if !errors.Is(err, ErrForbidden) || repository.called {
		t.Fatalf("error=%v called=%v", err, repository.called)
	}
}

func TestReplaceTeamMembersNormalizesAndAuditsSnapshot(t *testing.T) {
	// Submit tech-2 twice and tech-1 once; expect sorted unique IDs,
	// team version 2, team.members.replaced audit, and no role changes.
}
```

- [ ] **Step 2: Run the tests and verify failure**

Run: `go test ./backend/internal/organizations -run ReplaceTeamMembers`

Expected: FAIL because the command and method do not exist.

- [ ] **Step 3: Implement the membership domain contract**

```go
type ReplaceTeamMembersCommand struct {
	Principal       authorization.Principal
	TeamID          string
	TechnicianIDs   []string
	ExpectedVersion int64
	Reason          string
	Source          string
}

type TeamMembershipMutation struct {
	Team          Team
	TechnicianIDs []string
	ChangedAt     time.Time
	ChangedBy     string
	Audit         mutation.AuditRecord
	Event         mutation.EventRecord
}
```

Require MSP-global scope, `organization.manage`, a non-empty reason/source,
positive expected version, unique normalized IDs, and a maximum of 500 members.
The event data contains only member IDs and counts.

- [ ] **Step 4: Extend the repository atomically**

Add:

```go
ReplaceTeamMembersAtomic(context.Context, TeamMembershipMutation) error
```

The transaction must lock the team at `expected_version`, reject inactive
technicians or cross-MSP IDs, mark removed memberships inactive, reactivate or
insert selected memberships, increment the team version once, and write audit
and outbox facts. Extend `ListDirectory` to return active `member_ids`.

- [ ] **Step 5: Add and test the HTTP route**

Register:

```text
PUT /api/v1/admin/teams/{id}/members
```

Strict body:

```json
{
  "technician_ids": ["uuid"],
  "expected_version": 1,
  "reason": "Staffing change"
}
```

Test success, unknown fields, stale version, client-scoped denial, inactive
member rejection, and non-disclosing team lookup.

- [ ] **Step 6: Add the directory membership UI**

Render each team with its current members and an administrator-only searchable
design-system `MultiSelect`. Submit the complete replacement set with the team
version and reason. Preserve the form after a version conflict and reload the
current membership before retry.

- [ ] **Step 7: Run focused tests**

Run: `go test ./backend/internal/organizations ./backend/internal/store/psa ./backend/internal/httpapi -run 'TeamMember|ReplaceTeam'`

Run: `npm --prefix frontend test -- --run src/features/organization/DirectorySettingsPage.test.tsx`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/organizations backend/internal/store/psa/directory_repository.go backend/internal/store/psa/directory_repository_test.go backend/internal/httpapi/organization_routes.go backend/internal/httpapi/organization_routes_test.go backend/internal/httpapi/router.go frontend/src/features/organization/DirectorySettingsPage.tsx frontend/src/features/organization/DirectorySettingsPage.test.tsx
git commit -m "feat: manage organizational team memberships"
```

### Task 3: Define Structured Mention Tokens and State Rules

**Files:**
- Create: `backend/internal/mentions/model.go`
- Create: `backend/internal/mentions/tokens.go`
- Create: `backend/internal/mentions/tokens_test.go`
- Create: `backend/internal/mentions/state.go`
- Create: `backend/internal/mentions/state_test.go`

**Interfaces:**
- Consumes: stable user/team IDs and versioned internal source content.
- Produces: `Token`, `DiffTokens`, `Item.ApplyOccurrence`, and `Item.ApplyState`.

- [ ] **Step 1: Write failing token and state tests**

```go
func TestDiffTokensReturnsOnlyNewStableTokenIDs(t *testing.T) {
	before := []Token{{ID: "keep", TargetType: TargetStaff, TargetID: "tech-1"}}
	after := []Token{
		{ID: "keep", TargetType: TargetStaff, TargetID: "tech-1"},
		{ID: "new", TargetType: TargetTeam, TargetID: "team-1"},
	}
	diff, err := DiffTokens(before, after)
	if err != nil || !reflect.DeepEqual(diff.Added, after[1:]) {
		t.Fatalf("diff=%+v error=%v", diff, err)
	}
}

func TestOccurrenceRestoresArchivedItemToUnread(t *testing.T) {
	item := Item{State: Archived, Version: 4}
	item.ApplyOccurrence("occurrence-2", fixedNow())
	if item.State != Unread || item.Version != 5 || item.ArchivedAt != nil {
		t.Fatalf("item=%+v", item)
	}
}
```

Add cases for malformed IDs, duplicate token IDs, target mutation under an
existing token ID, raw text without tokens, read/unread/archive transitions,
and stale expected versions.

- [ ] **Step 2: Run the tests and verify failure**

Run: `go test ./backend/internal/mentions -run 'DiffTokens|Occurrence|ItemState'`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement the types**

```go
type TargetType string
const (
	TargetStaff TargetType = "staff"
	TargetTeam  TargetType = "team"
)

type ParentType string
const (
	ParentWorkRecord ParentType = "work_record"
	ParentTask       ParentType = "task"
	ParentProject    ParentType = "project"
)

type SourceKind string
const (
	SourceDetails SourceKind = "details"
	SourceComment SourceKind = "comment"
	SourceNote    SourceKind = "note"
)

type Token struct {
	ID         string     `json:"id"`
	TargetType TargetType `json:"target_type"`
	TargetID   string     `json:"target_id"`
	Label      string     `json:"label"`
	Start      int        `json:"start"`
	End        int        `json:"end"`
}

type ItemState string
const (
	Unread   ItemState = "unread"
	Read     ItemState = "read"
	Archived ItemState = "archived"
)
```

Validate a maximum of 100 tokens per source, non-empty bounded stable IDs, ordered
non-overlapping spans within the submitted body, exact label/span agreement,
unique token IDs, and immutable target identity for retained token IDs.

- [ ] **Step 4: Implement deterministic state transitions**

`ApplyState(next, expectedVersion, actorID, at)` accepts only the three states,
clears incompatible timestamps, and returns `object.ErrVersionConflict` for a
stale version. `ApplyOccurrence` always sets unread, clears read/archive and
access-suppression fields, replaces the latest occurrence, and increments the
version.

- [ ] **Step 5: Run package tests**

Run: `go test ./backend/internal/mentions`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/mentions/model.go backend/internal/mentions/tokens.go backend/internal/mentions/tokens_test.go backend/internal/mentions/state.go backend/internal/mentions/state_test.go
git commit -m "feat: define structured mention semantics"
```

### Task 4: Resolve Permission-Filtered People and Teams

**Files:**
- Create: `backend/internal/mentions/resolver.go`
- Create: `backend/internal/mentions/resolver_test.go`
- Create: `backend/internal/mentions/candidates.go`
- Create: `backend/internal/mentions/candidates_test.go`

**Interfaces:**
- Consumes: `Token`, `SourceRef`, organizational memberships, staff lifecycle, and effective object-read grants.
- Produces: `CandidateService.List`, `Resolver.Resolve`, `Resolution`, and `TeamConfirmation`.

- [ ] **Step 1: Write failing candidate and resolution tests**

```go
func TestResolveTeamSnapshotsOnlyEligibleActiveMembers(t *testing.T) {
	directory := &candidateRepository{
		members: []MemberAccess{
			{StaffID: "tech-1", Active: true, CanRead: true},
			{StaffID: "tech-2", Active: true, CanRead: false},
			{StaffID: "tech-3", Active: false, CanRead: true},
		},
	}
	result, err := NewResolver(directory).Resolve(ctx, ResolveCommand{
		AuthorID: "author", Source: workSource(), Tokens: []Token{teamToken("team-1")},
		ConfirmedTeamSnapshots: map[string]TeamConfirmation{
			"team-1": {
				TeamVersion: 3, EligibleMemberIDs: []string{"tech-1"},
			},
		},
	})
	if err != nil || ids(result.Eligible) != "tech-1" ||
		len(result.Excluded) != 2 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
```

Test author exclusion, cross-MSP targets, direct ineligible rejection, empty
team rejection, missing/stale partial-team confirmation, direct-plus-team
deduplication, and two mentions of one target in a mutation.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./backend/internal/mentions -run 'Candidate|ResolveTeam|ResolveDirect'`

Expected: FAIL because candidate/resolver services are missing.

- [ ] **Step 3: Define the permission repository**

```go
type AccessRepository interface {
	ListCandidates(context.Context, CandidateQuery) ([]Candidate, error)
	LoadDirectAccess(context.Context, SourceRef, []string) ([]MemberAccess, error)
	LoadTeamAccess(context.Context, SourceRef, []string) ([]TeamAccess, error)
}

type SourceRef struct {
	MSPID, ClientID string
	ParentType      ParentType
	ParentID        string
	SourceKind      SourceKind
}

type TeamConfirmation struct {
	TeamVersion       int64    `json:"team_version"`
	EligibleMemberIDs []string `json:"eligible_member_ids"`
}
```

`CanRead` must be computed from current role assignments and object/project
visibility; team membership alone never grants it. Task access inherits from
its work-record or project/phase parent. A recipient must also hold
`mention.read`; an author must hold `mention.create`.

- [ ] **Step 4: Implement picker candidates**

Require `mention.create` and source-edit authorization. Return:

```go
type Candidate struct {
	TargetType    TargetType `json:"target_type"`
	ID            string     `json:"id"`
	Label         string     `json:"label"`
	EligibleCount int        `json:"eligible_count,omitempty"`
	ExcludedCount int        `json:"excluded_count,omitempty"`
	Version       int64      `json:"version"`
}
```

Omit author, inactive staff, direct ineligible staff, and teams with zero
eligible members. Sort exact prefix matches before label matches and cap at 50.

- [ ] **Step 5: Implement transactional resolution**

Re-resolve every submitted target in the write transaction. A partial team
requires confirmation of the exact sorted eligible member snapshot and team
version returned by the picker. Return eligible and excluded resolution rows;
deduplicate widget/delivery recipients while retaining direct/team paths.

- [ ] **Step 6: Run mention package tests**

Run: `go test ./backend/internal/mentions`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/mentions/resolver.go backend/internal/mentions/resolver_test.go backend/internal/mentions/candidates.go backend/internal/mentions/candidates_test.go
git commit -m "feat: resolve authorized mention recipients"
```

### Task 5: Build the Atomic Internal Collaboration and Mention Service

**Files:**
- Create: `backend/internal/collaboration/model.go`
- Create: `backend/internal/collaboration/service.go`
- Create: `backend/internal/collaboration/service_test.go`
- Create: `backend/internal/mentions/service.go`
- Create: `backend/internal/mentions/service_test.go`

**Interfaces:**
- Consumes: `mentions.DiffTokens`, `mentions.Resolver`, source-edit authorization, and an atomic collaboration repository.
- Produces: `collaboration.Service.PutDetails`, `CreateComment`, `CreateNote`, `Edit`, `Redact`, and `mentions.Service.PrepareMutation`.

- [ ] **Step 1: Write failing atomic-service tests**

```go
func TestCreateInternalCommentCommitsSourceAndMentionProjectionTogether(t *testing.T) {
	repository := &atomicRepository{}
	service := newTestService(repository)
	source, err := service.CreateComment(ctx, CreateCommand{
		Principal: internalAuthor(),
		Parent: ParentRef{Type: "work_record", ID: "work-1"},
		Body: "@Mira please review",
		Tokens: []mentions.Token{staffToken("token-1", "tech-2", 0, 5)},
		IdempotencyKey: "request-1", Source: "work.comment",
	})
	if err != nil || source.ID == "" || len(repository.accepted.Occurrences) != 1 ||
		repository.accepted.Items[0].State != mentions.Unread {
		t.Fatalf("source=%+v mutation=%+v error=%v", source, repository.accepted, err)
	}
}
```

Add tests proving public visibility is rejected before repository access,
unchanged tokens emit nothing, re-added tokens re-mention, same-mutation
duplicates collapse, source failure writes nothing, and no event data contains
body text.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./backend/internal/collaboration ./backend/internal/mentions -run 'Internal|Atomic|Mutation'`

Expected: FAIL because collaboration and mention services are missing.

- [ ] **Step 3: Define internal source commands**

```go
type UpsertCommand struct {
	Principal              authorization.Principal
	Parent                 ParentRef
	SourceID               string
	Kind                   SourceKind
	Body                   string
	Tokens                 []mentions.Token
	ConfirmedTeamSnapshots map[string]mentions.TeamConfirmation
	ExpectedVersion        int64
	IdempotencyKey         string
	Source                 string
}

type ParentRef struct {
	Type mentions.ParentType
	ID   string
}

type SourceKind = mentions.SourceKind
```

Enforce internal visibility by construction: the collaboration package exposes
no public visibility enum. Bound body size at 256 KiB and require non-empty
details/comments/notes after trimming.

- [ ] **Step 4: Build immutable occurrence mutations**

For each added unique target, create one occurrence with source revision and
safe target snapshot. Append eligible/excluded resolution rows and one item
upsert per eligible recipient. Write one audit fact per occurrence and one
content-free `mention.occurred` outbox event per source mutation containing
unique recipient IDs plus each recipient's deterministic latest occurrence.
This prevents direct/team overlap in one save from producing two delivery
events.

- Define the mention preparer contract:

```go
type PrepareCommand struct {
	Author                 authorization.Principal
	Source                 SourceRef
	PriorTokens            []Token
	SubmittedBody          string
	SubmittedTokens        []Token
	ConfirmedTeams         map[string]TeamConfirmation
	SourceRevision         int64
	CorrelationID          string
}

func (s *Service) PrepareMutation(
	ctx context.Context,
	access AccessRepository,
	command PrepareCommand,
) (PreparedMutation, error)
```

- [ ] **Step 5: Implement atomic service methods**

Use one repository unit of work:

```go
type Repository interface {
	WithTransaction(
		context.Context,
		mentions.SourceRef,
		func(Transaction) error,
	) error
}

type Transaction interface {
	mentions.AccessRepository
	LoadSourceForUpdate(context.Context, mentions.SourceRef) (Source, error)
	Save(context.Context, Mutation) error
}
```

Inside the callback, load/lock the source, call
`mentions.Service.PrepareMutation` with the transaction as its access
repository, then save the returned source/mention mutation. The transaction
owns source, token, occurrence, resolution, item, audit, and outbox
persistence. Idempotency is keyed by MSP, parent, and request key.

- [ ] **Step 6: Run focused tests**

Run: `go test ./backend/internal/collaboration ./backend/internal/mentions`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/collaboration backend/internal/mentions/service.go backend/internal/mentions/service_test.go
git commit -m "feat: add atomic internal collaboration service"
```

### Task 6: Persist Sources, Mentions, Widget Queries, and Deep Links

**Files:**
- Create: `backend/internal/store/psa/collaboration_repository.go`
- Create: `backend/internal/store/psa/collaboration_repository_test.go`
- Create: `backend/internal/store/psa/mention_repository.go`
- Create: `backend/internal/store/psa/mention_repository_test.go`
- Modify: `backend/internal/store/psa/pgx.go`
- Create: `backend/internal/mentions/query.go`
- Create: `backend/internal/mentions/query_test.go`

**Interfaces:**
- Consumes: mutations from Task 5 and existing `database`/`transaction` helpers.
- Produces: `mentions.QueryService.ListCandidates`, `ListWidget`, `ChangeState`, `ResolveDeepLink`, and `LoadPreview`.

- [ ] **Step 1: Write failing repository and query tests**

Repository tests must assert SQL contains:

```go
for _, fragment := range []string{
	"INSERT INTO internal_collaboration_sources",
	"INSERT INTO mention_occurrences",
	"INSERT INTO mention_recipient_resolutions",
	"ON CONFLICT (msp_id, recipient_id, parent_type, parent_id)",
	"state = 'unread'",
	"INSERT INTO audit_ledger",
	"INSERT INTO event_outbox",
} {
	if !strings.Contains(strings.Join(tx.queries, "\n"), fragment) {
		t.Errorf("atomic mention query missing %q", fragment)
	}
}
```

Query tests cover state counts, cursor bounds, recipient ownership, current
authorization, sanitized preview truncation, removed token, deleted/redacted
source, former-internal-now-public source, and read-on-resolve behavior.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./backend/internal/store/psa ./backend/internal/mentions -run 'Mention|Collaboration|DeepLink|Widget'`

Expected: FAIL because repositories and query service do not exist.

- [ ] **Step 3: Implement the atomic repository unit of work**

`WithTransaction` begins one pgx transaction and passes a transaction-scoped
adapter implementing `mentions.AccessRepository`, `LoadSourceForUpdate`, and
`Save`. Lock parent/source/team membership rows in deterministic ID order.
Insert the source revision, occurrences, resolution outcomes, and audit/outbox
facts. Upsert each item with:

```sql
ON CONFLICT (msp_id, recipient_id, parent_type, parent_id)
DO UPDATE SET
  latest_occurrence_id = EXCLUDED.latest_occurrence_id,
  last_mentioned_at = EXCLUDED.last_mentioned_at,
  state = 'unread',
  read_at = NULL,
  archived_at = NULL,
  suppressed_at = NULL,
  suppression_reason = NULL,
  version = mention_items.version + 1
```

Use an idempotency claim so retrying the same source request does not add
another occurrence.

The work-record internal-content list also returns existing rows from
`comments` where `visibility = 'internal'` as read-only plain-text legacy
sources. Do not parse them for tokens or copy them into the new table. Newly
created internal comments use `internal_collaboration_sources`.

- [ ] **Step 4: Implement permission-aware access SQL**

Resolve a recipient's effective read capability from active role assignments
and role capabilities in the source client. For projects, require
`project.read`; for work records, `work_record.read`; for tasks, resolve the
parent and require the parent's read capability. Apply project-specific
visibility relationships where present. Never use team membership as a read
grant.

- [ ] **Step 5: Implement widget queries**

```go
type WidgetQuery struct {
	Principal authorization.Principal
	State     ItemState
	Cursor    string
	Limit     int
}

type StateCounts struct {
	Unread   int `json:"unread"`
	Read     int `json:"read"`
	Archived int `json:"archived"`
}

type WidgetItem struct {
	ID                 string    `json:"id"`
	ParentType         ParentType `json:"parent_type"`
	ParentID           string    `json:"parent_id"`
	ParentDisplayID    string    `json:"parent_display_id"`
	ParentSubject      string    `json:"parent_subject"`
	LatestOccurrenceID string    `json:"latest_occurrence_id"`
	AuthorLabel        string    `json:"author_label"`
	Origin             string    `json:"origin"`
	Preview            string    `json:"preview,omitempty"`
	State              ItemState `json:"state"`
	LastMentionedAt    time.Time `json:"last_mentioned_at"`
	Version            int64     `json:"version"`
}

type WidgetPage struct {
	Counts     StateCounts `json:"counts"`
	Items      []WidgetItem `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}
```

Enforce recipient ID equals authenticated principal ID, limit 1–50, stable
`last_mentioned_at,id` cursor order, and read-time access. Fetch safe object
metadata, then load/sanitize at most 240 Unicode characters around the current
token. Do not write the preview back.

- [ ] **Step 6: Implement versioned state and deep-link resolution**

`ChangeState` updates only the authenticated recipient's unsuppressed item at
the expected version. `ResolveDeepLink` authorizes the parent/source, marks the
item read in the same transaction, and returns:

```go
type DeepLink struct {
	Href            string `json:"href"`
	ParentType      string `json:"parent_type"`
	ParentID        string `json:"parent_id"`
	SourceID        string `json:"source_id,omitempty"`
	SourceAvailable bool   `json:"source_available"`
	ItemVersion     int64  `json:"item_version"`
}
```

- [ ] **Step 7: Run focused tests**

Run: `go test ./backend/internal/store/psa ./backend/internal/mentions`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/store/psa/collaboration_repository.go backend/internal/store/psa/collaboration_repository_test.go backend/internal/store/psa/mention_repository.go backend/internal/store/psa/mention_repository_test.go backend/internal/store/psa/pgx.go backend/internal/mentions/query.go backend/internal/mentions/query_test.go
git commit -m "feat: persist and query internal mentions"
```

### Task 7: Expose Collaboration and Mention HTTP Contracts

**Files:**
- Create: `backend/internal/httpapi/collaboration_routes.go`
- Create: `backend/internal/httpapi/collaboration_routes_test.go`
- Create: `backend/internal/httpapi/mention_routes.go`
- Create: `backend/internal/httpapi/mention_routes_test.go`
- Modify: `backend/internal/httpapi/work_record_routes.go`
- Modify: `backend/internal/httpapi/psa_routes_test.go`
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/cmd/rarity-api/main.go`

**Interfaces:**
- Consumes: collaboration and mention services from Tasks 5–6.
- Produces: authenticated internal-content, candidate, widget, state, and deep-link APIs.

- [ ] **Step 1: Write failing route tests**

Cover these routes:

```text
GET  /api/v1/mentions/candidates?parent_type=work_record&parent_id={id}&source_kind=comment&q=mir
GET  /api/v1/mentions/widget?state=unread&limit=20&cursor={cursor}
PATCH /api/v1/mentions/items/{id}
POST /api/v1/mentions/occurrences/{id}/resolve

GET  /api/v1/{work-records|tasks|projects}/{id}/internal-content
PUT  /api/v1/{work-records|tasks|projects}/{id}/internal-details
POST /api/v1/{work-records|tasks|projects}/{id}/internal-comments
POST /api/v1/{work-records|tasks|projects}/{id}/notes
PATCH /api/v1/internal-content/{source_id}
POST /api/v1/internal-content/{source_id}/redact
```

Assert trusted principal scope replaces all caller scope fields, unknown JSON
is rejected, public visibility is rejected, source body is preserved after a
partial-team validation error, and deep-link authorization is non-disclosing.

- [ ] **Step 2: Run route tests and verify failure**

Run: `go test ./backend/internal/httpapi -run 'Mention|InternalContent|InternalCommentDelegates'`

Expected: FAIL because the routes are not registered.

- [ ] **Step 3: Add strict DTOs and route action interfaces**

```go
type mentionTokenDTO struct {
	ID, TargetType, TargetID, Label string
	Start, End                      int
}

type internalContentRequest struct {
	Body                   string                       `json:"body"`
	Tokens                 []mentionTokenDTO            `json:"tokens"`
	ConfirmedTeamSnapshots map[string]teamConfirmation  `json:"confirmed_team_snapshots"`
	ExpectedVersion        int64                        `json:"expected_version"`
	IdempotencyKey         string                       `json:"idempotency_key"`
}

type teamConfirmation struct {
	TeamVersion       int64    `json:"team_version"`
	EligibleMemberIDs []string `json:"eligible_member_ids"`
}

type mentionItemStateRequest struct {
	State           string `json:"state"`
	ExpectedVersion int64  `json:"expected_version"`
}
```

Map domain errors to stable codes:
`mention_target_ineligible`, `mention_team_empty`,
`mention_team_confirmation_stale`, `mention_token_invalid`,
`source_not_internal`, `version_conflict`, and non-disclosing `not_found`.

- [ ] **Step 4: Preserve the public-comment boundary**

Keep `POST /api/v1/work-records/{id}/comments` for current clients. When
visibility is `client`, use `comments.Service` and reject tokens. When
visibility is `internal`, delegate to `collaboration.Service.CreateComment`.
Automation-created internal plain comments pass an empty token list and cannot
forge mention delivery.

- [ ] **Step 5: Compose production services**

Construct the directory membership, collaboration, mention query, and mention
invalidation services from one pgx pool. Add them to `httpapi.Dependencies`.
Wire the existing `id.New`, `time.Now`, authenticated principal resolver, and
public URL helper.

- [ ] **Step 6: Run HTTP and main-package tests**

Run: `go test ./backend/internal/httpapi ./backend/cmd/rarity-api`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/httpapi/collaboration_routes.go backend/internal/httpapi/collaboration_routes_test.go backend/internal/httpapi/mention_routes.go backend/internal/httpapi/mention_routes_test.go backend/internal/httpapi/work_record_routes.go backend/internal/httpapi/psa_routes_test.go backend/internal/httpapi/router.go backend/cmd/rarity-api/main.go
git commit -m "feat: expose internal mention APIs"
```

### Task 8: Deliver Recipient-Specific Email and Teams Alerts

**Files:**
- Modify: `backend/internal/notifications/planner.go`
- Modify: `backend/internal/notifications/planner_test.go`
- Modify: `backend/internal/notifications/delivery.go`
- Modify: `backend/internal/notifications/delivery_test.go`
- Modify: `backend/internal/notifications/engine.go`
- Modify: `backend/internal/notifications/engine_test.go`
- Create: `backend/internal/notifications/email.go`
- Create: `backend/internal/notifications/email_test.go`
- Modify: `backend/internal/store/psa/notification_repository.go`
- Modify: `backend/internal/store/psa/notification_repository_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Create: `backend/internal/notifications/preferences.go`
- Create: `backend/internal/notifications/preferences_test.go`
- Modify: `backend/internal/httpapi/notification_routes.go`
- Create: `backend/internal/httpapi/notification_routes_test.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Create: `frontend/src/features/mentions/MentionNotificationPreferences.tsx`
- Create: `frontend/src/features/mentions/MentionNotificationPreferences.test.tsx`
- Modify: `frontend/src/features/work/ServiceDeskSettingsPage.tsx`
- Modify: `frontend/src/features/work/ServiceDeskSettingsPage.test.tsx`

**Interfaces:**
- Consumes: `mention.occurred`, recipient resolution rows, notification policies/preferences, and exact authenticated deep links.
- Produces: one external delivery per recipient/channel/source mutation, recipient preference APIs/UI, and delivery-time authorization cancellation.

- [ ] **Step 1: Write failing mention-planning tests**

```go
func TestPlannerSkipsInAppAndPlansMentionForExactRecipient(t *testing.T) {
	repository := mentionPlannerRepository()
	result, err := NewPlanner(repository, fixedNow, sequentialIDs()).RunOnce(ctx, 10)
	if err != nil || result.Pending != 2 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	for _, delivery := range repository.decision.Deliveries {
		if delivery.Channel == InApp || delivery.RecipientTechnicianID != "tech-2" {
			t.Fatalf("delivery=%+v", delivery)
		}
	}
}
```

Add cases for direct/team overlap, quiet periods, preference-disabled email,
no authorized channels, revoked access before planning, no source text in
payloads, and idempotent retry.

- [ ] **Step 2: Write failing delivery tests**

Test that authorization is checked immediately before send, revoked delivery is
marked suppressed with `access_revoked`, email uses the technician's current
address, Teams uses only an enabled named connection, and neither payload
contains internal body/preview text.

- [ ] **Step 3: Run notification tests and verify failure**

Run: `go test ./backend/internal/notifications ./backend/internal/store/psa -run 'Mention|Email|RecipientAuthorization'`

Expected: FAIL because mention planning and email transport are missing.

- [ ] **Step 4: Generalize safe planning events**

Add `SubjectType`, `SubjectID`, `MentionOccurrenceID`, and
`RecipientTechnicianID` to `PlanningEvent`/`PlannedDelivery`. Load
`mention.occurred` through occurrence/resolution joins instead of forcing it
through a work-record join. For mention events:

- ignore `in_app` destinations;
- resolve policy destinations to the exact recipient;
- apply recipient preferences and quiet periods;
- deduplicate by event, recipient, and channel;
- persist no source text.

- [ ] **Step 5: Add bounded email delivery**

```go
type EmailRequest struct {
	EventID, Recipient, Subject, AuthenticatedURL string
}

type EmailTransport interface {
	Send(context.Context, EmailRequest) error
}
```

Build a fixed template containing author, safe object display ID/subject, and
authenticated link only. Add optional all-or-none SMTP configuration:
`RARITY_SMTP_HOST`, `RARITY_SMTP_PORT`, `RARITY_SMTP_USERNAME`,
`RARITY_SMTP_PASSWORD`, and `RARITY_SMTP_FROM`. Require TLS and validate the
server name in pilot/production; never log credentials.

- [ ] **Step 6: Implement recipient preferences**

Expose:

```text
GET   /api/v1/notification-preferences/mentions
PATCH /api/v1/notification-preferences/mentions
```

Use:

```go
type RecipientPreference struct {
	TechnicianID string
	EventType    string
	EmailEnabled bool
	TeamsEnabled bool
	TimeZone     string
	QuietStart   string
	QuietEnd     string
	Version      int64
}
```

The authenticated staff member may update only their own email/Teams
enablement, IANA time zone, local quiet start/end, and expected version.
Render these controls in a compact settings disclosure beside the dashboard
widget. Organization policy remains authoritative: a recipient can disable a
permitted channel but cannot enable a channel the organization has not
configured.

Add a Mentions policy preset to Service Desk settings. It fixes the event type
to `mention.occurred`, offers only email and Teams destinations, and rejects
`in_app` or webhook destinations on both client and server.

- [ ] **Step 7: Reauthorize before delivery**

Before email or Teams send, verify recipient active status, current source
access, current item not suppressed, and enabled destination. Mark failures as
`suppressed/access_revoked`, not retryable. Preserve existing bounded retry for
transport failures.

- [ ] **Step 8: Run notification, preference, and configuration tests**

Run: `go test ./backend/internal/notifications ./backend/internal/store/psa ./backend/internal/config ./backend/internal/httpapi ./backend/cmd/rarity-api`

Run: `npm --prefix frontend test -- --run src/features/mentions/MentionNotificationPreferences.test.tsx`

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/notifications backend/internal/store/psa/notification_repository.go backend/internal/store/psa/notification_repository_test.go backend/internal/config/config.go backend/internal/config/config_test.go backend/internal/httpapi/notification_routes.go backend/internal/httpapi/notification_routes_test.go backend/cmd/rarity-api/main.go frontend/src/features/mentions/MentionNotificationPreferences.tsx frontend/src/features/mentions/MentionNotificationPreferences.test.tsx frontend/src/features/work/ServiceDeskSettingsPage.tsx frontend/src/features/work/ServiceDeskSettingsPage.test.tsx
git commit -m "feat: deliver external mention notifications"
```

### Task 9: Build the Accessible Mention Editor and Picker

**Files:**
- Create: `frontend/src/features/mentions/types.ts`
- Create: `frontend/src/features/mentions/api.ts`
- Create: `frontend/src/features/mentions/api.test.ts`
- Create: `frontend/src/features/mentions/MentionEditor.tsx`
- Create: `frontend/src/features/mentions/MentionEditor.test.tsx`
- Create: `frontend/src/features/mentions/MentionPicker.tsx`
- Create: `frontend/src/features/mentions/MentionPicker.test.tsx`
- Create: `frontend/src/features/mentions/mentions.css`

**Interfaces:**
- Consumes: candidate and internal-content APIs from Task 7.
- Consumes: shared design-system `Combobox`, `Popover`, `Notice`, and focus conventions.
- Produces: `MentionDocument`, `MentionEditor`, and `MentionPicker`.

- [ ] **Step 1: Write failing picker/editor tests**

```tsx
it("inserts a structured person token from keyboard selection", async () => {
  const onChange = vi.fn();
  render(<MentionEditor value={emptyDocument} context={workContext} onChange={onChange} />);
  await user.type(screen.getByRole("textbox"), "@mi");
  await user.keyboard("{ArrowDown}{Enter}");
  expect(onChange).toHaveBeenLastCalledWith(
    expect.objectContaining({
      tokens: [expect.objectContaining({ targetType: "staff", targetId: "tech-2" })],
    }),
  );
});
```

Test People/Teams grouping, author omission from server results, search loading
and failure, plain pasted `@Jane` remaining text, token deletion, renamed labels,
partial-team confirmation, keyboard escape, focus return, screen-reader
announcements, and token/body span serialization.

- [ ] **Step 2: Run tests and verify failure**

Run: `npm --prefix frontend test -- --run src/features/mentions`

Expected: FAIL because the feature files do not exist.

- [ ] **Step 3: Define exact frontend types**

```ts
export type MentionToken = {
  id: string;
  targetType: "staff" | "team";
  targetId: string;
  label: string;
  start: number;
  end: number;
};

export type TeamConfirmation = {
  teamVersion: number;
  eligibleMemberIds: string[];
};

export type MentionDocument = {
  body: string;
  tokens: MentionToken[];
  confirmedTeamSnapshots: Record<string, TeamConfirmation>;
};
```

Use `crypto.randomUUID()` for client token IDs; the server remains
authoritative.

- [ ] **Step 4: Implement candidate API and accessible picker**

Debounce search by 150 ms, abort superseded requests, cap rendered options at
50, use combobox/listbox semantics, and keep People/Teams section labels
available to assistive technology. A partial team opens a confirmation summary
with eligible/excluded counts before token insertion.

- [ ] **Step 5: Implement the structured editor**

Use a controlled contenteditable surface with token elements
`contentEditable={false}` and a hidden plain-text fallback value. Maintain
ordered spans after edits, render labels from the latest candidate/source
response, and treat arbitrary pasted markup as plain text. Expose validation
messages without clearing the document.

- [ ] **Step 6: Run frontend tests and typecheck**

Run: `npm --prefix frontend test -- --run src/features/mentions`

Run: `npm --prefix frontend run build`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/features/mentions
git commit -m "feat: add accessible mention editor"
```

### Task 10: Add Internal Collaboration to Tickets, Tasks, and Projects

**Files:**
- Create: `frontend/src/features/collaboration/InternalCollaborationPanel.tsx`
- Create: `frontend/src/features/collaboration/InternalCollaborationPanel.test.tsx`
- Create: `frontend/src/features/collaboration/collaboration.css`
- Modify: `frontend/src/features/work/api.ts`
- Modify: `frontend/src/features/work/api.test.ts`
- Modify: `frontend/src/features/work/TechnicianWorklist.tsx`
- Modify: `frontend/src/features/work/TechnicianWorklist.test.tsx`
- Modify: `frontend/src/features/projects/api.ts`
- Modify: `frontend/src/features/projects/ProjectWorklist.tsx`
- Modify: `frontend/src/features/projects/ProjectWorklist.test.tsx`

**Interfaces:**
- Consumes: `MentionEditor` and collaboration APIs.
- Produces: one reusable internal details/comments/notes surface for `work_record`, `task`, and `project`.

- [ ] **Step 1: Write failing shared-panel tests**

Test loading details/comments/notes, saving each source kind, preserving text
and tokens after stale confirmation or version conflict, rendering old
plain-text content, indicating edited/deleted source state, and never offering
a public visibility control.

- [ ] **Step 2: Write failing host-surface tests**

For `TechnicianWorklist`, select a work record and a child task and assert each
gets the correct parent context. For `ProjectWorklist`, select a project and
project task and assert the same. Verify the existing public reply composer
does not render `MentionEditor`.

- [ ] **Step 3: Run tests and verify failure**

Run: `npm --prefix frontend test -- --run src/features/collaboration src/features/work/TechnicianWorklist.test.tsx src/features/projects/ProjectWorklist.test.tsx`

Expected: FAIL because the panel is missing.

- [ ] **Step 4: Implement the shared panel**

Render three explicit regions:

- Internal details: singleton, versioned save.
- Internal comments: append-only create, edited history display.
- Notes: append-only create, internal-only label.

Pass `parentType`, `parentId`, `sourceKind`, and author context to the editor.
On partial-team validation response, show the updated eligible/excluded
snapshot and require resubmission. On version conflict, retain the local draft
and offer reload/compare.

- [ ] **Step 5: Integrate ticket and task surfaces**

Place the panel next to the selected work-record detail and task detail. Keep
the existing public comment flow on `comments.Service`; route new internal
comments through collaboration. Do not change owner, participants, or watcher
UI after a mention save. Reuse the existing record-only workspace tabs and
preview/direct-open contracts; do not introduce parallel ticket or task routes.

- [ ] **Step 6: Integrate project and project-task surfaces**

Add the same panel to selected project and task contexts. Task source access
inherits from its project/phase/work-record parent on the server; the client
passes only the task ID. Reuse `RecordWorkspace` and the current project/task
workspace items.

- [ ] **Step 7: Run focused tests and build**

Run: `npm --prefix frontend test -- --run src/features/collaboration src/features/work src/features/projects`

Run: `npm --prefix frontend run build`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/features/collaboration frontend/src/features/work frontend/src/features/projects
git commit -m "feat: add internal collaboration surfaces"
```

### Task 11: Add the Mentions Widget to Home and Exact-Source Navigation

**Files:**
- Create: `frontend/src/features/mentions/MentionsWidget.tsx`
- Create: `frontend/src/features/mentions/MentionsWidget.test.tsx`
- Modify: `frontend/src/features/home/HomePage.tsx`
- Modify: `frontend/src/features/home/HomePage.test.tsx`
- Modify: `frontend/src/features/home/home.css`
- Modify: `frontend/src/app/routes.ts`
- Modify: `frontend/src/app/routes.test.ts`
- Modify: `frontend/src/navigation.ts`
- Modify: `frontend/src/navigation.test.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/features/collaboration/InternalCollaborationPanel.tsx`
- Modify: `frontend/src/features/collaboration/InternalCollaborationPanel.test.tsx`

**Interfaces:**
- Consumes: widget/state/deep-link API and source IDs rendered by collaboration panels.
- Produces: an operational-home Mentions widget and occurrence-aware record-workspace focus behavior.

- [ ] **Step 1: Write failing widget tests**

```tsx
it("restores an archived item when a re-mention response arrives", async () => {
  const api = apiWithArchivedThenRementionedItem();
  render(<MentionsWidget api={api} />);
  await user.click(screen.getByRole("tab", { name: "Archived" }));
  expect(await screen.findByText("RTY-1042")).toBeVisible();
  api.emitRefresh();
  await user.click(screen.getByRole("tab", { name: "Unread" }));
  expect(await screen.findByText("RTY-1042")).toBeVisible();
});
```

Test unread/read/archive counts, newest-first order, mark state, stale-version
reload, bounded load-more, safe preview absence, direct/team origin, loading,
empty/error states, and no full-inbox link.

- [ ] **Step 2: Write failing navigation/focus tests**

Test `#/home`, authenticated occurrence resolution, work/project route
selection, task parent routing, record-tab opening, exact source
expansion/focus, removed-token notice, unavailable-source notice, read-on-open
refresh, and revoked-access non-disclosure.

- [ ] **Step 3: Run tests and verify failure**

Run: `npm --prefix frontend test -- --run src/features/mentions/MentionsWidget.test.tsx src/features/home/HomePage.test.tsx src/app/routes.test.ts src/navigation.test.ts src/App.test.tsx src/features/collaboration/InternalCollaborationPanel.test.tsx`

Expected: FAIL because the Home widget and focus behavior are missing.

- [ ] **Step 4: Implement the widget**

Use tabs with visible counts and one paginated list. Each item shows safe parent
metadata, author/time, origin, optional preview, and Open/Read/Unread/Archive
controls. Apply optimistic state locally, but reload on `version_conflict`.
Refresh counts after every mutation and when the window regains focus.

- [ ] **Step 5: Host the widget in the existing operational Home**

Add `MentionsWidget` to the existing `HomePage` side column and preserve the
live-work, health, approvals, and audit modules already owned there. Do not add
a second Dashboard route: `home` remains the canonical operational dashboard.
Keep the widget permission-aware: reading its own items requires
`mention.read`; composing tokens requires `mention.create`.

- [ ] **Step 6: Implement occurrence navigation**

Opening calls `POST /mentions/occurrences/{id}/resolve`, then navigates to the
returned authenticated hash:

```text
#/work?parentID={id}&mentionOccurrenceID={id}&sourceID={id}
#/project?parentID={id}&mentionOccurrenceID={id}&sourceID={id}
```

Resolve the returned route through `routeManifest`, then open or activate the
record with the existing workspace `openRecord` contract. The collaboration
panel expands the matching source, sets programmatic focus without trapping
it, scrolls with reduced-motion support, and applies a temporary
`.mention-source-focus` outline. Source-unavailable focuses the parent notice
instead.

- [ ] **Step 7: Run frontend tests and build**

Run: `npm --prefix frontend test -- --run`

Run: `npm --prefix frontend run build`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/features/mentions/MentionsWidget.tsx frontend/src/features/mentions/MentionsWidget.test.tsx frontend/src/features/home frontend/src/app/routes.ts frontend/src/app/routes.test.ts frontend/src/navigation.ts frontend/src/navigation.test.ts frontend/src/App.tsx frontend/src/App.test.tsx frontend/src/features/collaboration/InternalCollaborationPanel.tsx frontend/src/features/collaboration/InternalCollaborationPanel.test.tsx
git commit -m "feat: add mentions home widget"
```

### Task 12: Enforce Access Invalidation, Telemetry, Documentation, and Release Gates

**Files:**
- Create: `backend/internal/mentions/invalidation.go`
- Create: `backend/internal/mentions/invalidation_test.go`
- Modify: `backend/internal/store/psa/mention_repository.go`
- Modify: `backend/internal/store/psa/mention_repository_test.go`
- Modify: `backend/cmd/rarity-api/main.go`
- Modify: `backend/internal/observability/logger.go`
- Modify: `docs/03-security/permission-matrix.md`
- Modify: `docs/03-security/client-isolation-test-model.md`
- Modify: `docs/04-api/rest-api.md`
- Modify: `docs/01-architecture/13-event-catalog.md`
- Modify: `docs/01-architecture/08-ai-platform.md`
- Modify: `docs/08-modules/ticket-intelligence.md`
- Modify: `docs/07-ui-ux/information-architecture.md`
- Create: `docs/06-development/internal-mentions-acceptance.md`
- Modify: `docs-site/app.js`
- Modify: `scripts/validate-docs.mjs`

**Interfaces:**
- Consumes: authorization/membership/status change events, mention projection, delivery records, and all prior task acceptance tests.
- Produces: bounded invalidation worker, content-free operational evidence, updated contracts, and complete release verification.

- [ ] **Step 1: Write failing invalidation tests**

```go
func TestInvalidationSuppressesItemsAndCancelsPendingDelivery(t *testing.T) {
	repository := &invalidationRepository{affected: []AffectedItem{{ID: "item-1"}}}
	result, err := NewInvalidationWorker(repository, fixedNow).RunOnce(ctx, 50)
	if err != nil || result.Suppressed != 1 || result.DeliveriesCanceled != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
```

Cover role removal, client access removal, project visibility removal, team
membership change without access change, technician disablement, idempotent
replay, read-time security backstop, no auto-restore after access returns, and
new mention clearing suppression.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./backend/internal/mentions ./backend/internal/store/psa -run 'Invalidation|Suppress|AccessRevoked'`

Expected: FAIL because the worker is missing.

- [ ] **Step 3: Implement bounded invalidation**

Claim `mention_access_invalidations` with a visibility lease. For each affected
recipient/object pair, recompute current access. When access is gone, set
`suppressed_at`, retain user state/audit history, and suppress pending delivery
with `access_revoked`. Mark the invalidation complete atomically. Do not
unsuppress on access return.

Enqueue invalidations from team-membership, role-assignment, technician-status,
client/object transfer, project-visibility, and object-deletion/redaction
events. Keep widget-read and delivery-time checks as synchronous backstops.

- [ ] **Step 4: Add content-free telemetry**

Emit counters for occurrence target type, eligible/excluded counts,
deduplication, item state transitions, re-mention, suppression, preview outcome,
deep-link outcome, and notification outcome. Structured logs may include MSP,
client, object type/ID, occurrence ID, and safe error code; assert in tests that
body, preview, and token-adjacent text are absent.

- [ ] **Step 5: Update authoritative documentation**

Document:

- `mention.create` and recipient-owned widget state;
- internal/public boundaries and non-granting semantics;
- team membership APIs;
- collaboration/mention REST routes and exact DTOs;
- `mention.occurred` safe event schema;
- the dashboard route and deep-link contract;
- exclusion of AI mention targets/commands without changing unrelated AI
  capabilities;
- email/Teams delivery and content-classification limits;
- operational metrics and invalidation behavior.

Add the Tagging design/plan, Mentions design/plan, and any other currently
missing Markdown documents to `docs-site/app.js` so the documentation validator
returns to a green baseline.

- [ ] **Step 6: Add acceptance contract checks**

Extend `scripts/validate-docs.mjs` and create
`docs/06-development/internal-mentions-acceptance.md` with stable checks for
all supported parent/source kinds, widget-only in-app delivery, no AI/intents,
no public tokens, exact-source links, and content-free events.

- [ ] **Step 7: Run focused security and contract tests**

Run: `go test ./backend/internal/mentions ./backend/internal/collaboration ./backend/internal/organizations ./backend/internal/notifications ./backend/internal/httpapi ./backend/internal/store/psa ./backend/migrations`

Expected: PASS.

- [ ] **Step 8: Run complete backend verification**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 9: Run complete frontend verification**

Run: `npm --prefix frontend test -- --run`

Run: `npm --prefix frontend run build`

Expected: PASS.

- [ ] **Step 10: Run documentation and diff verification**

Run: `npx --yes node@22 scripts/validate-docs.mjs`

Run: `git diff --check`

Expected: both commands exit 0.

- [ ] **Step 11: Commit**

```bash
git add backend/internal/mentions/invalidation.go backend/internal/mentions/invalidation_test.go backend/internal/store/psa/mention_repository.go backend/internal/store/psa/mention_repository_test.go backend/cmd/rarity-api/main.go backend/internal/observability/logger.go docs docs-site/app.js scripts/validate-docs.mjs
git commit -m "docs: complete internal mentions acceptance"
```

## Final Acceptance Checklist

- [ ] Individual mentions work in internal ticket, task, and project details,
      comments, and notes.
- [ ] Existing organizational teams are mentionable and snapshot only current
      eligible active members.
- [ ] Direct ineligible targets fail; partial teams warn and require an exact
      current confirmation.
- [ ] Public/customer-visible sources reject tokens at UI and API boundaries.
- [ ] One recipient/object item supports unread, read, unread-again, archive,
      and re-mention restoration.
- [ ] The Dashboard widget is the sole in-app surface and has no full-inbox or
      generic-notification duplicate.
- [ ] Deep links reauthorize, mark read, focus the exact source, and safely
      handle edited, removed, redacted, publicized, or inaccessible sources.
- [ ] Access loss suppresses items and cancels pending delivery without erasing
      immutable history or auto-restoring later.
- [ ] Email and Teams delivery follows policies, preferences, quiet periods,
      idempotency, retries, and delivery-time authorization.
- [ ] Occurrences, resolutions, items, events, logs, metrics, and external
      payloads contain no source body or preview.
- [ ] Mentions do not alter assignments, participants, watchers, permissions,
      tasks, approvals, or AI workflows.
- [ ] Historical plain `@name` text remains inert and readable.
- [ ] Backend, frontend, migration, security, documentation, and production
      build gates pass.
