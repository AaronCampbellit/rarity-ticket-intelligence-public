package psa_test

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar/adapters"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

type bindingOnlyImpactPreviewer struct{ repository *psa.CalendarRepository }

func (p bindingOnlyImpactPreviewer) PreviewSchedulingImpact(ctx context.Context, principal authorization.Principal, change calendar.ProposedChange) (calendar.SchedulingImpact, error) {
	bindings, err := p.repository.SchedulingRevisionBindings(ctx, principal, change.Schedule)
	return calendar.SchedulingImpact{Bindings: bindings}, err
}

func TestCalendarSchedulingProposalAtomicRollbackAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	msp, client, actor, role, assignment := id.New(), id.New(), id.New(), id.New(), id.New()
	taskA, taskB, parent := id.New(), id.New(), id.New()
	proposalID, changeA, changeB, projectionA, projectionB := id.New(), id.New(), id.New(), id.New(), id.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by)VALUES($1,$2,'schedule atomic',$3,$3)`, msp, "M-"+msp, actor)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by)VALUES($1,$2,$3,'client',$4,$4)`, client, msp, "C-"+client, actor)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name)VALUES($1,$2,$3,'Actor')`, actor, msp, actor+"@example.test")
	exec(`INSERT INTO roles(id,msp_id,key,name)VALUES($1,$2,$3,'Scheduler')`, role, msp, "scheduler_"+role)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability)VALUES($1,$2,'calendar.schedule'),($1,$2,'task.edit')`, role, msp)
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by)VALUES($1,$2,$3,$4,$5,$4)`, assignment, msp, client, actor, role)
	baseStart := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	baseEnd := baseStart.Add(time.Hour)
	for _, taskID := range []string{taskA, taskB} {
		exec(`INSERT INTO tasks(id,msp_id,client_id,parent_type,parent_id,title,status,position,owner_id,estimate_minutes,version,created_by,updated_by,scheduled_starts_at,scheduled_ends_at,schedule_timezone,scheduling_mode)VALUES($1,$2,$3,'project',$4,'Scheduled','open',1,$5,60,1,$5,$5,$6,$7,'UTC','fixed_block')`, taskID, msp, client, parent, actor, baseStart, baseEnd)
	}
	principal := authorization.Principal{ID: actor, Scope: scope.Principal{MSPID: msp}, Capabilities: authorization.NewCapabilitySet("calendar.schedule", "task.edit")}
	movedStart, movedEnd := baseStart.Add(2*time.Hour), baseEnd.Add(2*time.Hour)
	makeChange := func(changeID, projectionID, taskID string, expected int64) calendar.ProposedChange {
		request := calendar.RequestedChange{ID: changeID, ProjectionID: projectionID, Source: calendar.SourceRef{MSPID: msp, ClientID: client, Type: "task", ID: taskID}, EventRole: "scheduled_work", SourceRevision: expected, StartsAt: &movedStart, EndsAt: &movedEnd, Timezone: "UTC", Required: true}
		mutation := tasks.ScheduleMutation{TaskID: taskID, MSPID: msp, ClientID: client, ExpectedVersion: expected, Interval: calendar.TypedInterval{StartsAt: &movedStart, EndsAt: &movedEnd, Timezone: "UTC"}, Mode: calendar.FixedBlock, EstimateMinutes: 60, ProjectionID: projectionID}
		return calendar.ProposedChange{ID: changeID, Required: true, Requested: request, Prepared: calendar.PreparedChange{ID: changeID, Request: request, Source: request.Source, EventRole: request.EventRole, ExpectedSourceRevision: expected, Mutation: mutation}, Schedule: calendar.ProposedSchedule{ProjectionID: projectionID, ClientID: client, TechnicianID: actor, Interval: calendar.TimeInterval{Start: movedStart, End: movedEnd}}}
	}
	changes := []calendar.ProposedChange{makeChange(changeA, projectionA, taskA, 1), makeChange(changeB, projectionB, taskB, 99)}
	proposal := calendar.SchedulingProposal{ID: proposalID, ActorID: actor, MSPID: msp, ClientID: client, AuthorizationHash: calendar.AuthorizationFingerprint(principal), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute), State: "previewed", Bindings: []calendar.RevisionBinding{{Kind: calendar.RevisionSource, ID: msp + "/" + client + "/task/" + taskA, Version: 1}}, Changes: changes}
	repository := psa.NewCalendarRepositoryFromPool(pool)
	if err = repository.SaveSchedulingProposal(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	registry := calendar.NewWriteAdapterRegistry()
	if err = registry.Register(adapters.NewTaskAdapter(repository)); err != nil {
		t.Fatal(err)
	}
	uow := calendar.NewScheduleUnitOfWork(repository, registry, time.Now, id.New)
	_, err = uow.ApplyAtomic(ctx, calendar.ScheduleApplyRequest{Principal: principal, Proposal: proposal, Changes: changes})
	if !errors.Is(err, calendar.ErrStaleProposal) {
		t.Fatalf("error=%v", err)
	}
	var version int64
	var start time.Time
	if err = pool.QueryRow(ctx, `SELECT version,scheduled_starts_at FROM tasks WHERE id=$1`, taskA).Scan(&version, &start); err != nil {
		t.Fatal(err)
	}
	if version != 1 || !start.Equal(baseStart) {
		t.Fatalf("partial mutation escaped rollback: version=%d start=%v", version, start)
	}
	successProposalID, successChangeID := id.New(), id.New()
	successChange := makeChange(successChangeID, projectionA, taskA, 1)
	overriddenPolicyID := id.New()
	successChange.Conflicts = []calendar.Conflict{{Severity: calendar.ConflictOverrideable, ReasonRequired: true, PolicyID: overriddenPolicyID}}
	successProposal := calendar.SchedulingProposal{ID: successProposalID, ActorID: actor, MSPID: msp, ClientID: client, AuthorizationHash: calendar.AuthorizationFingerprint(principal), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute), State: "previewed", Bindings: []calendar.RevisionBinding{{Kind: calendar.RevisionSource, ID: msp + "/" + client + "/task/" + taskA, Version: 1}}, Changes: []calendar.ProposedChange{successChange}}
	if err = repository.SaveSchedulingProposal(ctx, successProposal); err != nil {
		t.Fatal(err)
	}
	applied, err := uow.ApplyAtomic(ctx, calendar.ScheduleApplyRequest{Principal: principal, Proposal: successProposal, Changes: []calendar.ProposedChange{successChange}, OverrideReason: "dispatch approved overlap"})
	if err != nil {
		t.Fatal(err)
	}
	if applied.CorrelationID == "" {
		t.Fatal("apply omitted audit correlation")
	}
	var state, auditReason, auditPolicies string
	var audits, events, taskScheduledEvents, calendarScheduleChangedEvents int
	if err = pool.QueryRow(ctx, `SELECT task.version,task.scheduled_starts_at,proposal.state,(SELECT count(*) FROM audit_ledger WHERE correlation_id=$3),(SELECT count(*) FROM event_outbox WHERE correlation_id=$3),(SELECT count(*) FROM event_outbox WHERE correlation_id=$3 AND event_type='task.scheduled'),(SELECT count(*) FROM event_outbox WHERE correlation_id=$3 AND event_type='calendar.schedule_changed'),(SELECT safe_diff->>'override_reason' FROM audit_ledger WHERE correlation_id=$3 LIMIT 1),(SELECT authorization_context->'overridden_policy_ids'->>0 FROM audit_ledger WHERE correlation_id=$3 LIMIT 1) FROM tasks task CROSS JOIN calendar_scheduling_proposals proposal WHERE task.id=$1 AND proposal.id=$2`, taskA, successProposalID, applied.CorrelationID).Scan(&version, &start, &state, &audits, &events, &taskScheduledEvents, &calendarScheduleChangedEvents, &auditReason, &auditPolicies); err != nil {
		t.Fatal(err)
	}
	if version != 2 || !start.Equal(movedStart) || state != "applied" || audits != 1 || events != 2 || taskScheduledEvents != 1 || calendarScheduleChangedEvents != 1 || auditReason != "dispatch approved overlap" || auditPolicies != overriddenPolicyID {
		t.Fatalf("version=%d start=%v state=%s audits=%d events=%d task_scheduled_events=%d calendar_schedule_changed_events=%d reason=%q policy=%q", version, start, state, audits, events, taskScheduledEvents, calendarScheduleChangedEvents, auditReason, auditPolicies)
	}

	// A fresh accepted-set evaluation can discover an override after an
	// optional cascade move is declined. The stored change tuple remains free
	// of that conflict, while the explicit fresh evidence must reach the audit.
	freshProposalID, freshChangeID, freshPolicyID := id.New(), id.New(), id.New()
	freshChange := makeChange(freshChangeID, projectionA, taskA, 2)
	freshProposal := calendar.SchedulingProposal{ID: freshProposalID, ActorID: actor, MSPID: msp, ClientID: client, AuthorizationHash: calendar.AuthorizationFingerprint(principal), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute), State: "previewed", Bindings: []calendar.RevisionBinding{{Kind: calendar.RevisionSource, ID: msp + "/" + client + "/task/" + taskA, Version: 2}}, Changes: []calendar.ProposedChange{freshChange}}
	if err = repository.SaveSchedulingProposal(ctx, freshProposal); err != nil {
		t.Fatal(err)
	}
	freshApplied, err := uow.ApplyAtomic(ctx, calendar.ScheduleApplyRequest{Principal: principal, Proposal: freshProposal, Changes: []calendar.ProposedChange{freshChange}, OverrideReason: "declined optional move", OverridePolicyIDs: []string{freshPolicyID}})
	if err != nil {
		t.Fatal(err)
	}
	var freshAuditPolicy string
	var freshRequiresOverride, freshRequiredReason bool
	if err = pool.QueryRow(ctx, `SELECT
		(SELECT authorization_context->'overridden_policy_ids'->>0 FROM audit_ledger WHERE correlation_id=$2 LIMIT 1),
		proposal.requires_override,proposal.required_reason
		FROM calendar_scheduling_proposals proposal WHERE proposal.id=$1`, freshProposalID, freshApplied.CorrelationID).Scan(&freshAuditPolicy, &freshRequiresOverride, &freshRequiredReason); err != nil {
		t.Fatal(err)
	}
	if freshAuditPolicy != freshPolicyID || !freshRequiresOverride || !freshRequiredReason {
		t.Fatalf("fresh override audit policy=%q requires_override=%v required_reason=%v", freshAuditPolicy, freshRequiresOverride, freshRequiredReason)
	}

	// Exercise the public preview -> persist -> load -> re-prepare -> atomic apply
	// lifecycle and prove that a dependency inserted after preview invalidates the
	// graph-scope snapshot even though no prior dependency row was bound.
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,capacity_bearing,assignee_id,planned_minutes,terminal_state) VALUES($1,$2,$3,'task',$4,'scheduled_work',3,'Task A projection',$5,$6,'UTC',false,'fixed_block',true,$7,60,'active'),($8,$2,$3,'task',$9,'scheduled_work',1,'Task B projection',$10,$11,'UTC',false,'fixed_block',true,$7,60,'active')`, projectionA, msp, client, taskA, movedStart, movedEnd, actor, projectionB, taskB, baseStart, baseEnd)
	roles, roleErr := calendar.NewProductionRoleRegistry(nil)
	if roleErr != nil {
		t.Fatal(roleErr)
	}
	service := calendar.NewProposalService(calendar.ProposalServiceDependencies{Projections: repository, Workforce: repository, Impacts: bindingOnlyImpactPreviewer{repository}, Store: repository, UnitOfWork: uow, RevisionValidator: repository, Adapters: registry, Roles: roles, Now: time.Now, NewID: id.New, TTL: 5 * time.Minute})
	serviceStart, serviceEnd := baseStart.Add(4*time.Hour), baseEnd.Add(4*time.Hour)
	stalePreview, err := service.Preview(ctx, calendar.PreviewCommand{Principal: principal, PrimaryChange: calendar.RequestedChange{ProjectionID: projectionB, StartsAt: &serviceStart, EndsAt: &serviceEnd}})
	if err != nil {
		t.Fatal(err)
	}
	dependencyID := id.New()
	exec(`INSERT INTO calendar_dependencies(id,msp_id,client_id,predecessor_projection_id,successor_projection_id,relationship_type,lead_lag_minutes,version,created_by,updated_by) VALUES($1,$2,$3,$4,$5,'finish_to_start',0,1,$6,$6)`, dependencyID, msp, client, projectionA, projectionB, actor)
	if _, err = service.Apply(ctx, calendar.ApplyCommand{Principal: principal, ProposalID: stalePreview.ID}); !errors.Is(err, calendar.ErrStaleProposal) {
		t.Fatalf("new dependency did not stale preview: %v", err)
	}
	exec(`DELETE FROM calendar_dependencies WHERE id=$1`, dependencyID)
	lifecyclePreview, err := service.Preview(ctx, calendar.PreviewCommand{Principal: principal, PrimaryChange: calendar.RequestedChange{ProjectionID: projectionB, StartsAt: &serviceStart, EndsAt: &serviceEnd}})
	if err != nil {
		t.Fatal(err)
	}
	lifecycleApplied, err := service.Apply(ctx, calendar.ApplyCommand{Principal: principal, ProposalID: lifecyclePreview.ID})
	if err != nil {
		t.Fatal(err)
	}
	if lifecycleApplied.ProposalID != lifecyclePreview.ID {
		t.Fatalf("applied=%+v preview=%+v", lifecycleApplied, lifecyclePreview)
	}
	if err = pool.QueryRow(ctx, `SELECT version,scheduled_starts_at FROM tasks WHERE id=$1`, taskB).Scan(&version, &start); err != nil {
		t.Fatal(err)
	}
	if version != 2 || !start.Equal(serviceStart) {
		t.Fatalf("service lifecycle did not apply: version=%d start=%v", version, start)
	}

	tamperProposalID, tamperChangeID := id.New(), id.New()
	tamperChange := makeChange(tamperChangeID, projectionB, taskB, 2)
	tamperProposal := calendar.SchedulingProposal{ID: tamperProposalID, ActorID: actor, MSPID: msp, ClientID: client, AuthorizationHash: calendar.AuthorizationFingerprint(principal), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute), State: "previewed", Bindings: []calendar.RevisionBinding{{Kind: calendar.RevisionSource, ID: msp + "/" + client + "/task/" + taskB, Version: 2}}, Changes: []calendar.ProposedChange{tamperChange}}
	if err = repository.SaveSchedulingProposal(ctx, tamperProposal); err != nil {
		t.Fatal(err)
	}
	tamperChange.Required = false
	if _, err = uow.ApplyAtomic(ctx, calendar.ScheduleApplyRequest{Principal: principal, Proposal: tamperProposal, Changes: []calendar.ProposedChange{tamperChange}}); !errors.Is(err, calendar.ErrStaleProposal) {
		t.Fatalf("caller-tampered required tuple was trusted: %v", err)
	}

	blockedProposalID, blockedChangeID := id.New(), id.New()
	blockedChange := makeChange(blockedChangeID, projectionB, taskB, 2)
	blockedChange.BlockedSources = []calendar.CascadeBlockedSource{{ProjectionID: projectionA, Source: calendar.SourceRef{MSPID: msp, ClientID: client, Type: "task", ID: taskA}, ReasonCode: "dependency_source_not_movable"}}
	blockedProposal := calendar.SchedulingProposal{ID: blockedProposalID, ActorID: actor, MSPID: msp, ClientID: client, AuthorizationHash: calendar.AuthorizationFingerprint(principal), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute), State: "previewed", Changes: []calendar.ProposedChange{blockedChange}}
	if err = repository.SaveSchedulingProposal(ctx, blockedProposal); err != nil {
		t.Fatal(err)
	}
	loadedBlocked, err := repository.LoadSchedulingProposal(ctx, msp, blockedProposalID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedBlocked.Changes) != 1 || len(loadedBlocked.Changes[0].BlockedSources) != 1 || loadedBlocked.Changes[0].BlockedSources[0].ProjectionID != projectionA {
		t.Fatalf("blocked dependency impact did not round-trip: %+v", loadedBlocked)
	}

	resourceSourceID, resourceProjectionID := id.New(), id.New()
	resourceProposalID, resourceChangeID := id.New(), id.New()
	resourceStart := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	resourceEnd := resourceStart.AddDate(0, 0, 5)
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_on,ends_on,all_day,scheduling_mode,planned_minutes,terminal_state) VALUES($1,$2,$3,'resource_plan',$4,'allocation',1,'Resource allocation',$5,$6,true,'effort_allocation',1200,'active')`, resourceProjectionID, msp, client, resourceSourceID, resourceStart, resourceEnd)
	resourceRequest := calendar.RequestedChange{ID: resourceChangeID, ProjectionID: resourceProjectionID, Source: calendar.SourceRef{MSPID: msp, ClientID: client, Type: "resource_plan", ID: resourceSourceID}, EventRole: "allocation", SourceRevision: 1, AllDay: true, StartsOn: &resourceStart, EndsOn: &resourceEnd, Required: true}
	resourceChange := calendar.ProposedChange{ID: resourceChangeID, Required: true, Requested: resourceRequest, Schedule: calendar.ProposedSchedule{ProjectionID: resourceProjectionID, ClientID: client, Interval: calendar.TimeInterval{Start: resourceStart, End: resourceEnd.AddDate(0, 0, 1)}}}
	resourceProposal := calendar.SchedulingProposal{ID: resourceProposalID, ActorID: actor, MSPID: msp, ClientID: client, AuthorizationHash: calendar.AuthorizationFingerprint(principal), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute), State: "previewed", Changes: []calendar.ProposedChange{resourceChange}}
	if err = repository.SaveSchedulingProposal(ctx, resourceProposal); err != nil {
		t.Fatal(err)
	}
	loadedResource, err := repository.LoadSchedulingProposal(ctx, msp, resourceProposalID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedResource.Changes) != 1 || loadedResource.Changes[0].Requested.Source.Type != "resource_plan" || loadedResource.Changes[0].Requested.EventRole != "allocation" {
		t.Fatalf("resource-plan proposal did not round-trip: %+v", loadedResource)
	}
}

func TestCalendarProjectionRepositoryAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	msp, clientA, clientB, actor := id.New(), id.New(), id.New(), id.New()
	source, projectionDue, projectionSchedule := id.New(), id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by)VALUES($1,$2,'calendar projection',$3,$3)`, msp, "M-"+msp, actor)
	for _, client := range []string{clientA, clientB} {
		exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by)VALUES($1,$2,$3,'client',$4,$4)`, client, msp, "C-"+client, actor)
	}
	exec(`INSERT INTO technicians(id,msp_id,email,display_name)VALUES($1,$2,$3,'Actor')`, actor, msp, actor+"@example.test")
	repository := psa.NewCalendarRepositoryFromPool(pool)
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	eventID := id.New()
	ref := calendar.SourceRef{MSPID: msp, ClientID: clientA, Type: "task", ID: source}
	projection := func(idValue, role string, revision int64) calendar.Projection {
		p := calendar.Projection{ID: idValue, Source: ref, EventRole: role, SourceRevision: revision, Title: "Privacy-safe title", SchedulingMode: calendar.Informational, TerminalState: calendar.Active}
		if role == "due" {
			p.AllDay = true
			p.StartsOn = &date
		} else {
			p.StartsAt = &start
			p.Timezone = "UTC"
		}
		return p
	}
	applied, err := repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: ref, SourceRevision: 2, Projections: []calendar.Projection{projection(projectionDue, "due", 2), projection(projectionSchedule, "scheduled_work", 2)}, Cursor: calendar.ProjectionCursor{ConsumerKey: "calendar", OccurredAt: start, EventID: eventID}})
	if err != nil || !applied {
		t.Fatalf("initial apply=%v err=%v", applied, err)
	}
	for _, revision := range []int64{1, 2} {
		applied, err = repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: ref, SourceRevision: revision, Projections: []calendar.Projection{projection(projectionDue, "due", revision)}})
		if err != nil || applied {
			t.Fatalf("revision %d apply=%v err=%v", revision, applied, err)
		}
	}
	applied, err = repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: ref, SourceRevision: 3, Projections: []calendar.Projection{projection(projectionDue, "due", 3)}})
	if err != nil || !applied {
		t.Fatalf("role deletion apply=%v err=%v", applied, err)
	}
	foreign := ref
	foreign.ClientID = clientB
	foreignProjection := projection(projectionDue, "due", 4)
	foreignProjection.Source = foreign
	foreignProjection.SourceRevision = 4
	if _, err = repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: foreign, SourceRevision: 4, Projections: []calendar.Projection{foreignProjection}}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client error=%v", err)
	}
	transferSource, transferProjection := id.New(), id.New()
	transferA := calendar.SourceRef{MSPID: msp, ClientID: clientA, Type: "task", ID: transferSource}
	transferB := calendar.SourceRef{MSPID: msp, ClientID: clientB, Type: "task", ID: transferSource}
	makeTransferProjection := func(ref calendar.SourceRef, revision int64) calendar.Projection {
		return calendar.Projection{ID: transferProjection, Source: ref, EventRole: "due", SourceRevision: revision, Title: "Transferred", AllDay: true, StartsOn: &date, SchedulingMode: calendar.Informational, TerminalState: calendar.Active}
	}
	if applied, err = repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: transferA, SourceRevision: 1, Projections: []calendar.Projection{makeTransferProjection(transferA, 1)}}); err != nil || !applied {
		t.Fatalf("transfer seed apply=%v err=%v", applied, err)
	}
	if _, err = repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: transferB, SourceRevision: 2, Projections: []calendar.Projection{makeTransferProjection(transferB, 2)}}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("untrusted transfer error=%v", err)
	}
	if applied, err = repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: transferB, SourceRevision: 2, Projections: []calendar.Projection{makeTransferProjection(transferB, 2)}, TrustedSourceReload: true}); err != nil || !applied {
		t.Fatalf("trusted transfer apply=%v err=%v", applied, err)
	}
	var oldScopeCount, newScopeCount, removedOld int
	if err = pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE client_id=$2),count(*) FILTER(WHERE client_id=$3) FROM calendar_event_projections WHERE msp_id=$1 AND source_id=$4`, msp, clientA, clientB, transferSource).Scan(&oldScopeCount, &newScopeCount); err != nil || oldScopeCount != 0 || newScopeCount != 1 {
		t.Fatalf("transfer scopes old=%d new=%d err=%v", oldScopeCount, newScopeCount, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM calendar_live_changes WHERE msp_id=$1 AND client_id=$2 AND source_id=$3 AND change_type='removed' AND source_revision=2`, msp, clientA, transferSource).Scan(&removedOld); err != nil || removedOld != 1 {
		t.Fatalf("transfer removal=%d err=%v", removedOld, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, revision := range []int64{4, 5} {
		wg.Add(1)
		go func(rev int64) {
			defer wg.Done()
			p := projection(projectionDue, "due", rev)
			_, e := repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: ref, SourceRevision: rev, Projections: []calendar.Projection{p}})
			errs <- e
		}(revision)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var revision int64
	var count, live int
	var cursorEvent string
	if err = pool.QueryRow(ctx, `SELECT max(source_revision),count(*) FROM calendar_event_projections WHERE msp_id=$1 AND client_id=$2 AND source_id=$3`, msp, clientA, source).Scan(&revision, &count); err != nil || revision != 5 || count != 1 {
		t.Fatalf("revision=%d count=%d err=%v", revision, count, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM calendar_live_changes WHERE msp_id=$1 AND client_id=$2 AND source_id=$3`, msp, clientA, source).Scan(&live); err != nil || live < 5 {
		t.Fatalf("live=%d err=%v", live, err)
	}
	if err = pool.QueryRow(ctx, `SELECT last_event_id::text FROM calendar_projection_cursors WHERE msp_id=$1 AND consumer_key='calendar'`, msp).Scan(&cursorEvent); err != nil || cursorEvent != eventID {
		t.Fatalf("cursor=%s err=%v", cursorEvent, err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SET LOCAL enable_seqscan=off`); err != nil {
		t.Fatal(err)
	}
	explainRows, err := tx.Query(ctx, `EXPLAIN SELECT source_revision,COALESCE(client_id::text,'') FROM calendar_live_changes WHERE msp_id=$1 AND source_type=$2 AND source_id=$3 ORDER BY source_revision DESC,cursor DESC LIMIT 1`, msp, ref.Type, source)
	if err != nil {
		t.Fatal(err)
	}
	plan := []string{}
	for explainRows.Next() {
		var line string
		if err = explainRows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	explainRows.Close()
	_ = tx.Rollback(ctx)
	if !strings.Contains(strings.Join(plan, "\n"), "calendar_live_changes_source_revision_idx") {
		t.Fatalf("historical lookup plan=%s", strings.Join(plan, "\n"))
	}
	exec(`CREATE FUNCTION calendar_projection_test_reject_live() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'reject live';END$$`)
	exec(`CREATE TRIGGER calendar_projection_test_reject BEFORE INSERT ON calendar_live_changes FOR EACH ROW EXECUTE FUNCTION calendar_projection_test_reject_live()`)
	p6 := projection(projectionDue, "due", 6)
	if _, err = repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: ref, SourceRevision: 6, Projections: []calendar.Projection{p6}}); err == nil {
		t.Fatal("trigger did not reject live write")
	}
	exec(`DROP TRIGGER calendar_projection_test_reject ON calendar_live_changes`)
	exec(`DROP FUNCTION calendar_projection_test_reject_live()`)
	if err = pool.QueryRow(ctx, `SELECT source_revision FROM calendar_event_projections WHERE id=$1`, projectionDue).Scan(&revision); err != nil || revision != 5 {
		t.Fatalf("rollback revision=%d err=%v", revision, err)
	}
	scheduleID, windowA, windowB := id.New(), id.New(), id.New()
	exec(`INSERT INTO technician_schedule_versions(id,msp_id,technician_id,timezone,effective_from,version,lifecycle_state,created_by)VALUES($1,$2,$3,'America/New_York',DATE '2026-08-10',1,'active',$3)`, scheduleID, msp, actor)
	exec(`INSERT INTO technician_schedule_windows(id,schedule_version_id,msp_id,technician_id,weekday,starts_local,ends_local,capacity_percent)VALUES($1,$3,$4,$5,1,TIME '09:00',TIME '17:00',100),($2,$3,$4,$5,2,TIME '10:00',TIME '14:00',80)`, windowA, windowB, scheduleID, msp, actor)
	scheduleRef := calendar.SourceRef{MSPID: msp, Type: "technician_schedule", ID: scheduleID}
	scheduleAdapter := adapters.NewScheduleAdapter(repository)
	scheduled, err := scheduleAdapter.Project(ctx, scheduleRef)
	if err != nil {
		t.Fatal(err)
	}
	roles, _ := calendar.NewProductionRoleRegistry(nil)
	if _, err = calendar.NewProjectionService(repository, roles).Apply(ctx, calendar.ProjectionBatch{Source: scheduleRef, SourceRevision: 1, Projections: scheduled}); err != nil {
		t.Fatal(err)
	}
	var scheduleCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM calendar_event_projections WHERE msp_id=$1 AND source_type='technician_schedule' AND source_id=$2 AND timezone='America/New_York'`, msp, scheduleID).Scan(&scheduleCount); err != nil || scheduleCount != 2 {
		t.Fatalf("schedule projections=%d err=%v", scheduleCount, err)
	}
}

func TestCalendarReconciliationRepairsTypedSourceAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	msp, client, actor, work := id.New(), id.New(), id.New(), id.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by)VALUES($1,$2,'reconcile',$3,$3)`, msp, "M-"+msp, actor)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by)VALUES($1,$2,$3,'client',$4,$4)`, client, msp, "C-"+client, actor)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name)VALUES($1,$2,$3,'Actor')`, actor, msp, actor+"@example.test")
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,due_on,created_by,updated_by)VALUES($1,$2,$3,$4,'incident','Reconcile me','new','normal',DATE '2026-08-20',$5,$5)`, work, msp, client, "W-"+work, actor)
	repository := psa.NewCalendarRepositoryFromPool(pool)
	adapterRegistry, err := adapters.NewProductionAdapterRegistry(adapters.Dependencies{Work: repository, Projects: repository, Workforce: repository, Commitments: repository, CustomDates: repository})
	if err != nil {
		t.Fatal(err)
	}
	roles, err := calendar.NewProductionRoleRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	service := calendar.NewProjectionService(repository, roles)
	reconciler := calendar.NewReconciliationService(repository, adapterRegistry, service)
	report, err := reconciler.Scan(ctx, calendar.ReconcileRequest{MSPID: msp, SourceTypes: []string{"work_record"}, Limit: 10, Repair: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Missing != 1 || report.Repaired != 1 {
		t.Fatalf("report=%+v", report)
	}
	var title, role string
	if err = pool.QueryRow(ctx, `SELECT title,event_role FROM calendar_event_projections WHERE msp_id=$1 AND source_id=$2`, msp, work).Scan(&title, &role); err != nil || title != "Reconcile me" || role != "due" {
		t.Fatalf("title=%q role=%q err=%v", title, role, err)
	}
}

func TestCalendarReconciliationScansGlobalSourcesAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	msp, actor, schedule, window, pto, maintenance := id.New(), id.New(), id.New(), id.New(), id.New(), id.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by)VALUES($1,$2,'global reconcile',$3,$3)`, msp, "M-"+msp, actor)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name)VALUES($1,$2,$3,'Actor')`, actor, msp, actor+"@example.test")
	exec(`INSERT INTO technician_schedule_versions(id,msp_id,technician_id,timezone,effective_from,version,lifecycle_state,created_by)VALUES($1,$2,$3,'UTC',DATE '2026-08-10',1,'active',$3)`, schedule, msp, actor)
	exec(`INSERT INTO technician_schedule_windows(id,schedule_version_id,msp_id,technician_id,weekday,starts_local,ends_local,capacity_percent)VALUES($1,$2,$3,$4,1,TIME '09:00',TIME '17:00',100)`, window, schedule, msp, actor)
	exec(`INSERT INTO pto_requests(id,msp_id,technician_id,starts_on,ends_on,all_day,pto_type,state,version,created_by,updated_by)VALUES($1,$2,$3,DATE '2026-08-12',DATE '2026-08-12',true,'vacation','requested',1,$3,$3)`, pto, msp, actor)
	exec(`INSERT INTO maintenance_windows(id,msp_id,title,starts_on,ends_on,all_day,protected,conflict_policy,status,version,created_by,updated_by)VALUES($1,$2,'Maintenance',DATE '2026-08-13',DATE '2026-08-13',true,false,'warning','planned',1,$3,$3)`, maintenance, msp, actor)
	repository := psa.NewCalendarRepositoryFromPool(pool)
	values, err := repository.ScanProjectionSources(ctx, calendar.ReconcileRequest{MSPID: msp, SourceTypes: []string{"technician_schedule", "pto", "maintenance_window"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 3 {
		t.Fatalf("values=%+v", values)
	}
	for _, value := range values {
		if value.Source.ClientID != "" {
			t.Fatalf("global source had client: %+v", value.Source)
		}
	}
}

func TestCalendarReconciliationRemembersAppliedZeroRoleRevisionAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	msp, client, actor, work := id.New(), id.New(), id.New(), id.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by)VALUES($1,$2,'zero role',$3,$3)`, msp, "M-"+msp, actor)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by)VALUES($1,$2,$3,'client',$4,$4)`, client, msp, "C-"+client, actor)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name)VALUES($1,$2,$3,'Actor')`, actor, msp, actor+"@example.test")
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by)VALUES($1,$2,$3,$4,'incident','No dates','new','normal',$5,$5)`, work, msp, client, "W-"+work, actor)
	repository := psa.NewCalendarRepositoryFromPool(pool)
	adapterRegistry, err := adapters.NewProductionAdapterRegistry(adapters.Dependencies{Work: repository, Projects: repository, Workforce: repository, Commitments: repository, CustomDates: repository})
	if err != nil {
		t.Fatal(err)
	}
	roles, _ := calendar.NewProductionRoleRegistry(nil)
	reconciler := calendar.NewReconciliationService(repository, adapterRegistry, calendar.NewProjectionService(repository, roles))
	request := calendar.ReconcileRequest{MSPID: msp, SourceTypes: []string{"work_record"}, Limit: 10, Repair: true}
	first, err := reconciler.Scan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Missing != 1 || first.Repaired != 1 {
		t.Fatalf("first=%+v", first)
	}
	second, err := reconciler.Scan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Missing != 0 || second.Stale != 0 || second.Repaired != 0 {
		t.Fatalf("second=%+v", second)
	}
}

func TestCalendarProjectionTracksIndependentTagsAndSoftDeletesAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	msp, client, actor, work, group, tag := id.New(), id.New(), id.New(), id.New(), id.New(), id.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by)VALUES($1,$2,'calendar tag revision',$3,$3)`, msp, "M-"+msp, actor)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by)VALUES($1,$2,$3,'client',$4,$4)`, client, msp, "C-"+client, actor)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name)VALUES($1,$2,$3,'Actor')`, actor, msp, actor+"@example.test")
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,due_on,created_by,updated_by)VALUES($1,$2,$3,$4,'incident','Tag revision','new','normal',DATE '2026-08-20',$5,$5)`, work, msp, client, "W-"+work, actor)
	exec(`INSERT INTO tag_groups(id,msp_id,internal_key,label,created_by,updated_by)VALUES($1,$2,'technology','Technology',$3,$3)`, group, msp, actor)
	exec(`INSERT INTO tags(id,msp_id,group_id,internal_key,label,created_by,updated_by)VALUES($1,$2,$3,'vpn','VPN',$4,$4)`, tag, msp, group, actor)
	exec(`INSERT INTO classification_object_versions(msp_id,client_id,object_type,object_id,version)VALUES($1,$2,'work_record',$3,1)`, msp, client, work)
	repository := psa.NewCalendarRepositoryFromPool(pool)
	adapter := adapters.NewWorkRecordAdapter(repository)
	ref := calendar.SourceRef{MSPID: msp, ClientID: client, Type: "work_record", ID: work}
	projected, err := adapter.Project(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 1 || projected[0].SourceRevision != 2 {
		t.Fatalf("initial projection=%+v", projected)
	}
	roles, _ := calendar.NewProductionRoleRegistry(nil)
	service := calendar.NewProjectionService(repository, roles)
	if applied, applyErr := service.Apply(ctx, calendar.ProjectionBatch{Source: ref, SourceRevision: 2, Projections: projected, TrustedSourceReload: true}); applyErr != nil || !applied {
		t.Fatalf("initial applied=%v err=%v", applied, applyErr)
	}
	exec(`INSERT INTO object_tag_assignments(id,msp_id,client_id,object_type,object_id,object_version,tag_id,assignment_source,assigned_by)VALUES($1,$2,$3,'work_record',$4,2,$5,'human',$6)`, id.New(), msp, client, work, tag, actor)
	exec(`UPDATE classification_object_versions SET version=2 WHERE msp_id=$1 AND client_id=$2 AND object_type='work_record' AND object_id=$3`, msp, client, work)
	projected, err = adapter.Project(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 1 || projected[0].SourceRevision != 3 || len(projected[0].Dimensions.TagIDs) != 1 || projected[0].Dimensions.TagIDs[0] != tag {
		t.Fatalf("tag-only projection=%+v", projected)
	}
	if applied, applyErr := service.Apply(ctx, calendar.ProjectionBatch{Source: ref, SourceRevision: 3, Projections: projected, TrustedSourceReload: true}); applyErr != nil || !applied {
		t.Fatalf("tag-only applied=%v err=%v", applied, applyErr)
	}
	var storedTag string
	if err = pool.QueryRow(ctx, `SELECT filter_dimensions->'tag_ids'->>0 FROM calendar_event_projections WHERE msp_id=$1 AND source_id=$2`, msp, work).Scan(&storedTag); err != nil || storedTag != tag {
		t.Fatalf("stored tag=%q err=%v", storedTag, err)
	}
	exec(`UPDATE work_records SET deleted_at=now(),deleted_by=$4,version=2 WHERE id=$1 AND msp_id=$2 AND client_id=$3`, work, msp, client, actor)
	registry := calendar.NewAdapterRegistry()
	if err = registry.Register(adapter); err != nil {
		t.Fatal(err)
	}
	worker := calendar.NewProjectionWorker(repository, registry, service, "")
	if err = worker.Handle(ctx, mutation.EventRecord{MSPID: msp, ClientID: client, SubjectType: "work_record", SubjectID: work, SubjectVersion: 2}); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM calendar_event_projections WHERE msp_id=$1 AND source_id=$2`, msp, work).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("soft-delete projections=%d err=%v", remaining, err)
	}
	values, err := repository.ScanProjectionSources(ctx, calendar.ReconcileRequest{MSPID: msp, SourceTypes: []string{"work_record"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if value.Source.ID == work && value.SourceExists {
			t.Fatalf("soft-deleted source remained authoritative: %+v", value)
		}
	}
}

func TestCalendarProjectionPollerAdvancesPastDeletedRelevantRowsAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	mspID, actorID := id.New(), id.New()
	firstEventID, secondEventID := id.New(), id.New()
	firstSubjectID, secondSubjectID := id.New(), id.New()
	base := time.Now().UTC().Truncate(time.Second)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatal(execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'projection poller',$3,$3)`, mspID, "M-"+mspID, actorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,$3,'Actor')`, actorID, mspID, actorID+"@example.test")
	exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES($1,'work_record.sla.deleted',1,$2,$3,'technician',$4,'work_record_sla',$5,1,$1,'integration')`, firstEventID, base, mspID, actorID, firstSubjectID)
	exec(`INSERT INTO event_outbox(event_id,event_type,schema_version,occurred_at,msp_id,actor_type,actor_id,subject_type,subject_id,subject_version,correlation_id,source) VALUES($1,'work_record.sla.deleted',1,$2,$3,'technician',$4,'work_record_sla',$5,1,$1,'integration')`, secondEventID, base.Add(time.Second), mspID, actorID, secondSubjectID)

	repository := psa.NewCalendarRepositoryFromPool(pool)
	roles, _ := calendar.NewProductionRoleRegistry(nil)
	worker := calendar.NewProjectionWorker(repository, calendar.NewAdapterRegistry(), calendar.NewProjectionService(repository, roles), "calendar")
	first, err := repository.ListCalendarProjectionEvents(ctx, mspID, "calendar", 1)
	if err != nil || len(first) != 1 || first[0].EventID != firstEventID {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if err = worker.Handle(ctx, first[0]); err != nil {
		t.Fatal(err)
	}
	second, err := repository.ListCalendarProjectionEvents(ctx, mspID, "calendar", 1)
	if err != nil || len(second) != 1 || second[0].EventID != secondEventID {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if err = worker.Handle(ctx, second[0]); err != nil {
		t.Fatal(err)
	}
	remaining, err := repository.ListCalendarProjectionEvents(ctx, mspID, "calendar", 1)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining=%+v err=%v", remaining, err)
	}
}

func TestScheduleExceptionRevisionReprojectsAndPublishesLiveChangeAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	mspID, actorID := id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatal(execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'schedule exception revision',$3,$3)`, mspID, "M-"+mspID, actorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,$3,'Scheduler')`, actorID, mspID, actorID+"@example.test")
	principal := authorization.Principal{ID: actorID, Scope: scope.Principal{MSPID: mspID}, Capabilities: authorization.NewCapabilitySet("calendar.workforce.manage")}
	clock := time.Now().UTC().Truncate(time.Second)
	workforceRepository := psa.NewWorkforceRepositoryFromPool(pool)
	schedules := workforce.NewScheduleService(workforceRepository, func() time.Time { return clock }, id.New)
	published, err := schedules.Publish(ctx, workforce.PublishScheduleCommand{Principal: principal, TechnicianID: actorID, Timezone: "UTC", EffectiveFrom: clock, Windows: []workforce.WeeklyWindow{{Weekday: time.Monday, StartsMinute: 540, EndsMinute: 1020}}, Source: "integration", IdempotencyKey: "publish-schedule"})
	if err != nil || published.Version != 1 {
		t.Fatalf("published=%+v err=%v", published, err)
	}
	calendarRepository := psa.NewCalendarRepositoryFromPool(pool)
	registry := calendar.NewAdapterRegistry()
	if err = registry.Register(adapters.NewScheduleAdapter(calendarRepository)); err != nil {
		t.Fatal(err)
	}
	roles, _ := calendar.NewProductionRoleRegistry(nil)
	worker := calendar.NewProjectionWorker(calendarRepository, registry, calendar.NewProjectionService(calendarRepository, roles), "calendar")
	process := func() {
		t.Helper()
		events, listErr := calendarRepository.ListCalendarProjectionEvents(ctx, mspID, "calendar", 100)
		if listErr != nil {
			t.Fatal(listErr)
		}
		for _, event := range events {
			if handleErr := worker.Handle(ctx, event); handleErr != nil {
				t.Fatal(handleErr)
			}
		}
	}
	process()
	clock = clock.Add(time.Second)
	updated, err := schedules.AddException(ctx, workforce.AddScheduleExceptionCommand{Principal: principal, ScheduleID: published.ID, ExpectedVersion: 1, Exception: workforce.ScheduleException{ExceptionOn: clock.AddDate(0, 0, 1), State: workforce.Unavailable, AllDay: true, Reason: "training"}, Source: "integration", IdempotencyKey: "add-exception"})
	if err != nil || updated.Version != 2 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	process()
	var storedVersion, projectionRevision int64
	if err = pool.QueryRow(ctx, `SELECT version FROM technician_schedule_versions WHERE id=$1`, published.ID).Scan(&storedVersion); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT source_revision FROM calendar_event_projections WHERE msp_id=$1 AND source_type='technician_schedule' AND source_id=$2 LIMIT 1`, mspID, published.ID).Scan(&projectionRevision); err != nil {
		t.Fatal(err)
	}
	var liveRevisionCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM calendar_live_changes WHERE msp_id=$1 AND source_type='technician_schedule' AND source_id=$2 AND source_revision=2 AND change_type='upserted'`, mspID, published.ID).Scan(&liveRevisionCount); err != nil {
		t.Fatal(err)
	}
	if storedVersion != 2 || projectionRevision != 2 || liveRevisionCount == 0 {
		t.Fatalf("stored=%d projection=%d live=%d", storedVersion, projectionRevision, liveRevisionCount)
	}
	_, err = schedules.AddException(ctx, workforce.AddScheduleExceptionCommand{Principal: principal, ScheduleID: published.ID, ExpectedVersion: 1, Exception: workforce.ScheduleException{ExceptionOn: clock.AddDate(0, 0, 2), State: workforce.Unavailable, AllDay: true}, Source: "integration", IdempotencyKey: "reuse-stale-version"})
	if !errors.Is(err, workforce.ErrWorkforceVersionConflict) {
		t.Fatalf("stale reuse error=%v", err)
	}
}

func TestCalendarReconciliationBoundedPrefixSkipsTransferredEmptyScopeAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	msp, clientA, clientB, actor, parent := id.New(), id.New(), id.New(), id.New(), id.New()
	ordered := []string{id.New(), id.New()}
	sort.Strings(ordered)
	transferred, actionable := ordered[0], ordered[1]
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by)VALUES($1,$2,'reconcile prefix',$3,$3)`, msp, "M-"+msp, actor)
	for _, client := range []string{clientA, clientB} {
		exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by)VALUES($1,$2,$3,'client',$4,$4)`, client, msp, "C-"+client, actor)
	}
	exec(`INSERT INTO technicians(id,msp_id,email,display_name)VALUES($1,$2,$3,'Actor')`, actor, msp, actor+"@example.test")
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by)VALUES($1,$2,$3,$4,'incident','Parent','new','normal',$5,$5)`, parent, msp, clientB, "W-"+parent, actor)
	exec(`INSERT INTO tasks(id,msp_id,client_id,parent_type,parent_id,work_record_id,title,status,position,due_on,created_by,updated_by)VALUES($1,$2,$3,'work_record',$4,$4,'Actionable task','new',1,DATE '2026-08-25',$5,$5)`, actionable, msp, clientB, parent, actor)
	repository := psa.NewCalendarRepositoryFromPool(pool)
	date := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	oldRef := calendar.SourceRef{MSPID: msp, ClientID: clientA, Type: "task", ID: transferred}
	oldProjection := calendar.Projection{ID: id.New(), Source: oldRef, EventRole: "due", SourceRevision: 1, Title: "Old scope", AllDay: true, StartsOn: &date, SchedulingMode: calendar.Informational, TerminalState: calendar.Active}
	if applied, applyErr := repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: oldRef, SourceRevision: 1, Projections: []calendar.Projection{oldProjection}}); applyErr != nil || !applied {
		t.Fatalf("seed old scope applied=%v err=%v", applied, applyErr)
	}
	newRef := calendar.SourceRef{MSPID: msp, ClientID: clientB, Type: "task", ID: transferred}
	if applied, applyErr := repository.ApplyProjectionBatchAtomic(ctx, calendar.ProjectionBatch{Source: newRef, SourceRevision: 2, TrustedSourceReload: true}); applyErr != nil || !applied {
		t.Fatalf("empty transfer applied=%v err=%v", applied, applyErr)
	}
	values, err := repository.ScanProjectionSources(ctx, calendar.ReconcileRequest{MSPID: msp, SourceTypes: []string{"task"}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Source.ID != actionable || !values[0].SourceExists {
		t.Fatalf("bounded prefix=%+v; transferred empty scopes starved actionable task %s", values, actionable)
	}
}
