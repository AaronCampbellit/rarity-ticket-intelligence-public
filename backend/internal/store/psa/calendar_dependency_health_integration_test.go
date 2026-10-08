package psa_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
)

func TestCalendarDependencyAndHealthPersistenceAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for calendar dependency and health verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	repository := psa.NewCalendarRepositoryFromPool(pool)

	mspID, clientID, actorID := id.New(), id.New(), id.New()
	predecessorID, successorID, projectID := id.New(), id.New(), id.New()
	predecessorSourceID, successorSourceID := id.New(), id.New()
	dependencyID, reverseID := id.New(), id.New()
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatalf("fixture: %v", execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Task 5 calendar',$3,$3)`, mspID, "M-"+mspID, actorID)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'Task 5 client',$4,$4)`, clientID, mspID, "C-"+clientID, actorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name,lifecycle_state) VALUES($1,$2,$3,'Task 5 actor','active')`, actorID, mspID, actorID+"@example.test")
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,terminal_state,filter_dimensions,assignee_id,created_at,updated_at) VALUES
($1,$3,$4,'task',$5,'scheduled_work',1,'Predecessor',$7,$8,'UTC',false,'fixed_block','active',jsonb_build_object('project_ids',jsonb_build_array($11::text)),$12,now(),now()),
($2,$3,$4,'task',$6,'scheduled_work',1,'Successor',$9,$10,'UTC',false,'fixed_block','active',jsonb_build_object('project_ids',jsonb_build_array($11::text)),$12,now(),now())`, predecessorID, successorID, mspID, clientID, predecessorSourceID, successorSourceID, now.Add(-3*time.Hour), now.Add(-2*time.Hour), now.Add(-time.Hour), now, projectID, actorID)
	var freshRuleVersions int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM calendar_event_projections WHERE id IN($1,$2) AND health_rule_version=$3`, predecessorID, successorID, calendar.CurrentHealthRuleVersion).Scan(&freshRuleVersions); err != nil || freshRuleVersions != 2 {
		t.Fatalf("fresh projection health rule versions=%d err=%v", freshRuleVersions, err)
	}

	projectScheduler := authorization.Principal{ID: actorID, Scope: scope.Principal{MSPID: mspID, ClientID: clientID}, Capabilities: authorization.NewCapabilitySet("calendar.read", "calendar.schedule", "project.read", "project.edit")}
	if _, err = calendar.NewDependencyService(repository, func() time.Time { return now }, id.New).Preview(ctx, calendar.CreateDependencyCommand{Principal: projectScheduler, PredecessorID: predecessorID, SuccessorID: successorID, Type: calendar.FinishToStart}); err != nil {
		t.Fatalf("project task dependency authorization: %v", err)
	}

	dependency := calendar.Dependency{ID: dependencyID, MSPID: mspID, ClientID: clientID, PredecessorID: predecessorID, SuccessorID: successorID, Type: calendar.FinishToStart, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: actorID, UpdatedBy: actorID}
	createdFact := calendar.DependencyChangeFact{EventID: id.New(), ActorID: actorID, OccurredAt: now}
	if _, err = repository.InsertDependency(ctx, dependency, createdFact); err != nil {
		t.Fatalf("insert dependency: %v", err)
	}
	if _, err = repository.InsertDependency(ctx, dependency, createdFact); !errors.Is(err, calendar.ErrDuplicateDependency) {
		t.Fatalf("duplicate dependency error=%v", err)
	}
	var createdFacts int
	var createdPredecessor, createdSuccessor string
	if err = pool.QueryRow(ctx, `SELECT count(*),max(data->>'predecessor_projection_id'),max(data->>'successor_projection_id') FROM event_outbox WHERE event_id=$1 AND event_type='calendar.dependency.created'`, createdFact.EventID).Scan(&createdFacts, &createdPredecessor, &createdSuccessor); err != nil || createdFacts != 1 || createdPredecessor != predecessorID || createdSuccessor != successorID {
		t.Fatalf("created facts=%d predecessor=%s successor=%s err=%v", createdFacts, createdPredecessor, createdSuccessor, err)
	}
	reverse := dependency
	reverse.ID, reverse.PredecessorID, reverse.SuccessorID = reverseID, successorID, predecessorID
	if _, err = repository.InsertDependency(ctx, reverse, calendar.DependencyChangeFact{EventID: id.New(), ActorID: actorID, OccurredAt: now}); !errors.Is(err, calendar.ErrDependencyCycle) {
		t.Fatalf("reverse dependency error=%v", err)
	}
	dependencies, err := repository.ListDependencies(ctx, mspID, clientID)
	if err != nil || len(dependencies) != 1 || dependencies[0].ID != dependencyID {
		t.Fatalf("dependencies=%+v err=%v", dependencies, err)
	}

	resolveEvent := calendar.HealthRecomputeEvent{SourceEventID: id.New(), MSPID: mspID, Kind: calendar.HealthFactDependency, SubjectID: dependencyID, OccurredAt: now}
	contexts, err := repository.ResolveAffectedHealth(ctx, resolveEvent)
	if err != nil || len(contexts) != 2 {
		t.Fatalf("contexts=%+v err=%v", contexts, err)
	}
	var successor calendar.HealthProjectionContext
	for _, value := range contexts {
		if value.ProjectionID == successorID {
			successor = value
		}
	}
	if !successor.Context.UnmetDependency {
		t.Fatalf("successor health context=%+v", successor.Context)
	}
	successor.Context.Now = now
	result := calendar.EvaluateHealth(successor.Context)
	update := calendar.HealthUpdate{ProjectionID: successor.ProjectionID, MSPID: successor.MSPID, ClientID: successor.ClientID, Source: successor.Source, SourceRevision: successor.SourceRevision, Result: result}
	applied, err := repository.ApplyHealthResultsAtomic(ctx, resolveEvent, []calendar.HealthUpdate{update})
	if err != nil || !applied {
		t.Fatalf("apply health=%v err=%v", applied, err)
	}
	var state calendar.HealthState
	var reasonCode string
	var liveChanges int
	if err = pool.QueryRow(ctx, `SELECT health_state,health_reasons->0->>'code',(SELECT count(*) FROM calendar_live_changes WHERE projection_id=$1 AND change_type='health_changed') FROM calendar_event_projections WHERE id=$1`, successorID).Scan(&state, &reasonCode, &liveChanges); err != nil {
		t.Fatal(err)
	}
	if state != calendar.HealthBlocked || reasonCode != "dependency_blocked" || liveChanges != 1 {
		t.Fatalf("state=%q reason=%q live=%d", state, reasonCode, liveChanges)
	}
	var conflictFacts int
	var canonicalConflict, leakedConflict, correlated bool
	if err = pool.QueryRow(ctx, `SELECT count(*),COALESCE(bool_and(data=jsonb_build_object('recipient_id',$3::text,'change_class','conflict','urgency','urgent','source_refs',jsonb_build_array(jsonb_build_object('type','task','id',$2::text,'client_id',$4::text,'event_role','scheduled_work','source_revision',1)),'action_path','/calendar')),false),COALESCE(bool_or(data::text LIKE '%dependency_blocked%' OR data::text LIKE '%Successor%'),false),COALESCE(bool_and(correlation_id=$5),false) FROM event_outbox WHERE event_type='calendar.schedule_changed' AND msp_id=$1 AND subject_id=$2::uuid AND data->>'change_class'='conflict'`, mspID, successorSourceID, actorID, clientID, resolveEvent.SourceEventID).Scan(&conflictFacts, &canonicalConflict, &leakedConflict, &correlated); err != nil || conflictFacts != 1 || !canonicalConflict || leakedConflict || !correlated {
		t.Fatalf("conflict facts=%d canonical=%v leaked=%v correlated=%v err=%v", conflictFacts, canonicalConflict, leakedConflict, correlated, err)
	}
	if applied, err = repository.ApplyHealthResultsAtomic(ctx, resolveEvent, []calendar.HealthUpdate{update}); err != nil || applied {
		t.Fatalf("replay applied=%v err=%v", applied, err)
	}
	unchangedEvent := resolveEvent
	unchangedEvent.SourceEventID = id.New()
	if applied, err = repository.ApplyHealthResultsAtomic(ctx, unchangedEvent, []calendar.HealthUpdate{update}); err != nil || !applied {
		t.Fatalf("new unchanged fact applied=%v err=%v", applied, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM calendar_live_changes WHERE projection_id=$1 AND change_type='health_changed'`, successorID).Scan(&liveChanges); err != nil || liveChanges != 1 {
		t.Fatalf("unchanged live=%d err=%v", liveChanges, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE event_type='calendar.schedule_changed' AND msp_id=$1 AND subject_id=$2 AND data->>'change_class'='conflict'`, mspID, successorSourceID).Scan(&conflictFacts); err != nil || conflictFacts != 1 {
		t.Fatalf("actionable replay conflict facts=%d err=%v", conflictFacts, err)
	}

	deletionEventID := id.New()
	if err = repository.DeleteDependency(ctx, dependency, calendar.DependencyDeletionFact{EventID: deletionEventID, ActorID: actorID, OccurredAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("delete dependency: %v", err)
	}
	var deletedPredecessor, deletedSuccessor string
	var deletedEndpoints []string
	if err = pool.QueryRow(ctx, `SELECT data->>'predecessor_projection_id',data->>'successor_projection_id',ARRAY(SELECT jsonb_array_elements_text(data->'projection_ids')) FROM event_outbox WHERE event_id=$1 AND event_type='calendar.dependency.deleted'`, deletionEventID).Scan(&deletedPredecessor, &deletedSuccessor, &deletedEndpoints); err != nil || deletedPredecessor != predecessorID || deletedSuccessor != successorID || len(deletedEndpoints) != 2 {
		t.Fatalf("deletion fact predecessor=%s successor=%s endpoints=%v err=%v", deletedPredecessor, deletedSuccessor, deletedEndpoints, err)
	}
	deletedEvent := calendar.HealthRecomputeEvent{SourceEventID: deletionEventID, MSPID: mspID, Kind: calendar.HealthFactDependency, SubjectID: dependencyID, ProjectionIDs: []string{successorID}, OccurredAt: now.Add(time.Minute)}
	deletedContexts, err := repository.ResolveAffectedHealth(ctx, deletedEvent)
	if err != nil || len(deletedContexts) != 1 || deletedContexts[0].ProjectionID != successorID || deletedContexts[0].Context.UnmetDependency {
		t.Fatalf("deleted dependency contexts=%+v err=%v", deletedContexts, err)
	}
	deletedContexts[0].Context.Now = now.Add(time.Minute)
	staleResult := calendar.EvaluateHealth(deletedContexts[0].Context)
	if _, err = pool.Exec(ctx, `UPDATE calendar_event_projections SET source_revision=2 WHERE id=$1`, successorID); err != nil {
		t.Fatal(err)
	}
	staleUpdate := calendar.HealthUpdate{ProjectionID: successorID, MSPID: mspID, ClientID: clientID, Source: deletedContexts[0].Source, SourceRevision: 1, Result: staleResult}
	if applied, err = repository.ApplyHealthResultsAtomic(ctx, deletedEvent, []calendar.HealthUpdate{staleUpdate}); err != nil || !applied {
		t.Fatalf("stale fact claim applied=%v err=%v", applied, err)
	}
	if err = pool.QueryRow(ctx, `SELECT health_state,(SELECT count(*) FROM calendar_live_changes WHERE projection_id=$1 AND change_type='health_changed') FROM calendar_event_projections WHERE id=$1`, successorID).Scan(&state, &liveChanges); err != nil || state != calendar.HealthBlocked || liveChanges != 1 {
		t.Fatalf("stale write state=%q live=%d err=%v", state, liveChanges, err)
	}

	allDayID, instantID := id.New(), id.New()
	allDaySourceID, instantSourceID := id.New(), id.New()
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_on,all_day,scheduling_mode,terminal_state,created_at,updated_at) VALUES($1,$2,$3,'task',$4,'due',1,'All-day deadline',DATE '2026-08-06',true,'informational','active',now(),now())`, allDayID, mspID, clientID, allDaySourceID)
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_at,timezone,all_day,scheduling_mode,terminal_state,created_at,updated_at) VALUES($1,$2,$3,'work_record',$4,'sla_resolution_deadline',1,'Instant deadline',$5,'UTC',false,'informational','active',now(),now())`, instantID, mspID, clientID, instantSourceID, now.Add(-time.Minute))
	for _, projectionID := range []string{allDayID, instantID} {
		deadlineEvent := calendar.HealthRecomputeEvent{SourceEventID: id.New(), MSPID: mspID, Kind: calendar.HealthFactProjection, SubjectID: projectionID, OccurredAt: now}
		deadlineContexts, resolveErr := repository.ResolveAffectedHealth(ctx, deadlineEvent)
		if resolveErr != nil || len(deadlineContexts) != 1 {
			t.Fatalf("deadline projection=%s contexts=%+v err=%v", projectionID, deadlineContexts, resolveErr)
		}
		if projectionID == allDayID && (deadlineContexts[0].Context.EndsOn == nil || deadlineContexts[0].Context.EndsAt != nil) {
			t.Fatalf("all-day deadline lost date precision: %+v", deadlineContexts[0].Context)
		}
		deadlineContexts[0].Context.Now = now
		if deadlineHealth := calendar.EvaluateHealth(deadlineContexts[0].Context); deadlineHealth.State != calendar.HealthOverdue {
			t.Fatalf("deadline projection=%s health=%+v context=%+v", projectionID, deadlineHealth, deadlineContexts[0].Context)
		}
	}

	milestonePredecessorID, milestoneSuccessorID := id.New(), id.New()
	milestonePredecessorSourceID, milestoneSuccessorSourceID := id.New(), id.New()
	milestoneDependencyID := id.New()
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_on,all_day,scheduling_mode,terminal_state,created_at,updated_at) VALUES
($1,$3,$4,'milestone',$5,'milestone',1,'Completed predecessor',DATE '2026-08-10',true,'informational','completed',now(),now()),
($2,$3,$4,'milestone',$6,'milestone',1,'Early successor',DATE '2026-08-10',true,'informational','active',now(),now())`, milestonePredecessorID, milestoneSuccessorID, mspID, clientID, milestonePredecessorSourceID, milestoneSuccessorSourceID)
	milestoneDependency := calendar.Dependency{ID: milestoneDependencyID, MSPID: mspID, ClientID: clientID, PredecessorID: milestonePredecessorID, SuccessorID: milestoneSuccessorID, Type: calendar.FinishToStart, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: actorID, UpdatedBy: actorID}
	if _, err = repository.InsertDependency(ctx, milestoneDependency, calendar.DependencyChangeFact{EventID: id.New(), ActorID: actorID, OccurredAt: now}); err != nil {
		t.Fatalf("insert milestone dependency: %v", err)
	}
	invalidMinuteDependency := milestoneDependency
	invalidMinuteDependency.ID, invalidMinuteDependency.Type, invalidMinuteDependency.LeadLagMinutes = id.New(), calendar.StartToStart, 30
	if _, err = repository.InsertDependency(ctx, invalidMinuteDependency, calendar.DependencyChangeFact{EventID: id.New(), ActorID: actorID, OccurredAt: now}); !errors.Is(err, calendar.ErrLeadLagPrecision) {
		t.Fatalf("persist all-day minute dependency error=%v", err)
	}
	milestoneContexts, err := repository.ResolveAffectedHealth(ctx, calendar.HealthRecomputeEvent{SourceEventID: id.New(), MSPID: mspID, Kind: calendar.HealthFactDependency, SubjectID: milestoneDependencyID, OccurredAt: now})
	if err != nil {
		t.Fatalf("resolve milestone dependency: %v", err)
	}
	var milestoneBlocked bool
	for _, value := range milestoneContexts {
		if value.ProjectionID == milestoneSuccessorID {
			milestoneBlocked = value.Context.UnmetDependency
		}
	}
	if !milestoneBlocked {
		t.Fatalf("date-only milestone constraint was not evaluated: %+v", milestoneContexts)
	}
}
