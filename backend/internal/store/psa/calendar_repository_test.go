package psa

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

func TestCalendarDependencyProjectionDecodesFilterDimensionsForSourceAuthorization(t *testing.T) {
	const projectionID = "00000000-0000-4000-8000-000000000001"
	start := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(v ...any) {
		*(v[0].(*string)) = projectionID
		*(v[1].(*string)) = "00000000-0000-4000-8000-000000000002"
		*(v[2].(*string)) = "00000000-0000-4000-8000-000000000003"
		*(v[3].(*string)) = "task"
		*(v[4].(*string)) = "00000000-0000-4000-8000-000000000004"
		*(v[5].(*string)) = "scheduled_work"
		*(v[6].(*string)) = ""
		*(v[7].(*int64)) = 1
		*(v[8].(*string)) = "Project task"
		*(v[9].(*bool)) = false
		*(v[10].(**time.Time)) = nil
		*(v[11].(**time.Time)) = nil
		*(v[12].(**time.Time)) = &start
		*(v[13].(**time.Time)) = &end
		*(v[14].(*string)) = "UTC"
		*(v[15].(*calendar.SchedulingMode)) = calendar.FixedBlock
		*(v[16].(*bool)) = false
		*(v[17].(*string)) = ""
		*(v[18].(*string)) = ""
		*(v[19].(*int64)) = 60
		*(v[20].(*calendar.TerminalState)) = calendar.Active
		*(v[21].(*[]byte)) = []byte(`{"project_ids":["project-1"]}`)
	}}}
	found, err := NewCalendarRepository(db).LoadDependencyProjection(context.Background(), projectionID)
	if err != nil || !reflect.DeepEqual(found.Dimensions.ProjectIDs, []string{"project-1"}) {
		t.Fatalf("projection=%+v err=%v", found, err)
	}
	if !strings.Contains(db.query, "filter_dimensions") {
		t.Fatalf("query does not load authorization dimensions: %s", db.query)
	}
}

func TestCalendarQueryRepositoryUsesAuthorizedClientsAndBoundedIndexedWindow(t *testing.T) {
	rows := &fakeRows{}
	db := &fakeSalesDB{queryRows: rows}
	repository := NewCalendarRepository(db)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	principal := authorization.Principal{ID: "00000000-0000-4000-8000-000000000003", Scope: scope.Principal{MSPID: "00000000-0000-4000-8000-000000000001"}}
	_, err := repository.ListCalendarProjections(context.Background(), principal, []string{"00000000-0000-4000-8000-000000000002"}, calendar.QueryWindow{Start: start, End: start.Add(24 * time.Hour)}, calendar.Filter{TagIDs: []string{"tag-narrow"}}, calendar.QueryVisibilityFull, 101)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"calendar_event_projections", "client_id = ANY", "starts_at <", "starts_on <", "filter_dimensions->'tag_ids' ?| $15", "$9::text[] IS NULL", "$23='full'", "source_assignment", "LIMIT $5"} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("query lacks %q: %s", fragment, db.query)
		}
	}
}

func TestCalendarQueryRepositoryPreservesExplicitEmptyClientFilter(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	principal := authorization.Principal{ID: "00000000-0000-4000-8000-000000000003", Scope: scope.Principal{MSPID: "00000000-0000-4000-8000-000000000001"}}
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	_, err := NewCalendarRepository(db).ListCalendarProjections(context.Background(), principal, []string{"00000000-0000-4000-8000-000000000002"}, calendar.QueryWindow{Start: start, End: start.Add(24 * time.Hour)}, calendar.Filter{ClientIDs: []string{}}, calendar.QueryVisibilityFull, 10)
	if err != nil {
		t.Fatal(err)
	}
	clients, ok := db.args[8].([]string)
	if !ok || clients == nil || len(clients) != 0 {
		t.Fatalf("explicit-empty client argument lost: %#v", db.args[8])
	}
}

func TestCalendarAuthorizedClientResolutionUsesActiveMembershipCapability(t *testing.T) {
	rows := &fakeRows{scans: []func(...any){func(v ...any) { *(v[0].(*string)) = "00000000-0000-4000-8000-000000000003" }}}
	db := &fakeSalesDB{queryRows: rows}
	principal := authorization.Principal{ID: "00000000-0000-4000-8000-000000000002", Scope: scope.Principal{MSPID: "00000000-0000-4000-8000-000000000001"}}
	found, err := NewCalendarRepository(db).AuthorizedCalendarClientIDs(context.Background(), principal)
	if err != nil || len(found) != 1 {
		t.Fatalf("clients=%v err=%v", found, err)
	}
	for _, fragment := range []string{"client.lifecycle_state='active'", "role_assignments", "assignment.client_id IS NULL OR assignment.client_id=client.id", "assignment.expires_at", "capability.capability=$3"} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("membership query lacks %q: %s", fragment, db.query)
		}
	}
}

func TestCalendarSourceDetailAuthorizationUsesSourceSpecificClientRole(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(v ...any) { *(v[0].(*bool)) = true }}}
	principal := authorization.Principal{ID: "00000000-0000-4000-8000-000000000002", Scope: scope.Principal{MSPID: "00000000-0000-4000-8000-000000000001"}}
	allowed, err := NewCalendarRepository(db).CanReadCalendarSource(context.Background(), principal, calendar.SourceRef{MSPID: principal.Scope.MSPID, ClientID: "00000000-0000-4000-8000-000000000003", Type: "project", ID: "00000000-0000-4000-8000-000000000004"})
	if err != nil || !allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	for _, fragment := range []string{"role_assignments", "assignment.client_id IS NULL OR assignment.client_id IS NOT DISTINCT FROM", "capability.capability=ANY"} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("source query lacks %q: %s", fragment, db.query)
		}
	}
}

func TestCalendarTaskSourceAuthorizationUsesActualParentCapability(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(v ...any) { *(v[0].(*bool)) = true }}}
	principal := authorization.Principal{ID: "00000000-0000-4000-8000-000000000002", Scope: scope.Principal{MSPID: "00000000-0000-4000-8000-000000000001"}}
	allowed, err := NewCalendarRepository(db).CanReadCalendarSource(context.Background(), principal, calendar.SourceRef{MSPID: principal.Scope.MSPID, ClientID: "00000000-0000-4000-8000-000000000003", Type: "task", ID: "00000000-0000-4000-8000-000000000004"})
	if err != nil || !allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	for _, fragment := range []string{"FROM tasks task", "task.parent_type", "work_record.read", "project.read", "opportunity.read"} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("task authorization lacks %q: %s", fragment, db.query)
		}
	}
}

func TestCalendarCustomDateAuthorizationUsesUnderlyingTypedObject(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(v ...any) { *(v[0].(*bool)) = true }}}
	principal := authorization.Principal{ID: "00000000-0000-4000-8000-000000000002", Scope: scope.Principal{MSPID: "00000000-0000-4000-8000-000000000001"}}
	allowed, err := NewCalendarRepository(db).CanReadCalendarSource(context.Background(), principal, calendar.SourceRef{MSPID: principal.Scope.MSPID, ClientID: "00000000-0000-4000-8000-000000000003", Type: "custom_date", ID: "00000000-0000-4000-8000-000000000004"})
	if err != nil || !allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	for _, fragment := range []string{"object_custom_date_values", "value.object_type", "LEFT JOIN tasks", "knowledge.read", "time_entry.read_scoped", "search.read"} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("custom authorization lacks %q: %s", fragment, db.query)
		}
	}
	if strings.Contains(db.query, "capability.capability='calendar.read'") {
		t.Fatalf("custom date used calendar.read as source authorization: %s", db.query)
	}
}

func TestCalendarLiveRepositoryScopesCursorBeforeReadingChanges(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(v ...any) { *(v[0].(*uint64)) = 10; *(v[1].(*uint64)) = 20 }}, queryRows: &fakeRows{}}
	page, err := NewCalendarRepository(db).ListCalendarChangesAfter(context.Background(), "00000000-0000-4000-8000-000000000001", []string{"00000000-0000-4000-8000-000000000002"}, 10, 100)
	if err != nil || page.LatestCursor != 20 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	for _, fragment := range []string{"cursor>$2", "client_id=ANY($3::uuid[])", "ORDER BY cursor", "LIMIT $4"} {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("live query lacks %q: %s", fragment, db.query)
		}
	}
}

func TestCalendarConfigurationPersistenceIsOptimisticAtomicAndDoesNotBackfill(t *testing.T) {
	const (
		mspID   = "00000000-0000-4000-8000-000000000801"
		actorID = "00000000-0000-4000-8000-000000000802"
	)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRows: []row{fakeRow{scan: func(v ...any) { *(v[0].(*int64)) = 0 }}}}
	repository := NewCalendarRepository(&fakeSalesDB{tx: tx})
	accepted := calendar.ConflictPolicyMutation{
		ID: "00000000-0000-4000-8000-000000000803", MSPID: mspID,
		Scope: calendar.ConflictPolicyScope{Type: calendar.ConflictScopeMSP}, Version: 1, EffectiveFrom: now,
		Rules: []calendar.ConflictPolicyRule{{Kind: calendar.ConflictApprovedPTO, Severity: calendar.ConflictHard}},
		Audit: mutation.AuditRecord{ID: "00000000-0000-4000-8000-000000000804", OccurredAt: now, MSPID: mspID, ActorType: "technician", ActorID: actorID, Action: "calendar.conflict_policies.replaced", SubjectType: "calendar_conflict_policy", SubjectID: "00000000-0000-4000-8000-000000000803", SubjectVersion: 1, Source: "test", CorrelationID: "00000000-0000-4000-8000-000000000806"},
		Event: mutation.EventRecord{EventID: "00000000-0000-4000-8000-000000000805", EventType: "calendar.conflict_policies.replaced", SchemaVersion: 1, OccurredAt: now, MSPID: mspID, ActorType: "technician", ActorID: actorID, SubjectType: "calendar_conflict_policy", SubjectID: "00000000-0000-4000-8000-000000000803", SubjectVersion: 1, Source: "test", CorrelationID: "00000000-0000-4000-8000-000000000806"},
	}
	if err := repository.ReplaceConflictPoliciesAtomic(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(tx.queries, "\n") + "\n" + tx.query
	for _, fragment := range []string{"pg_advisory_xact_lock", "FOR UPDATE", "calendar_conflict_policies", "audit_ledger", "event_outbox"} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("policy transaction missing %q: %s", fragment, joined)
		}
	}
	if strings.Contains(strings.ToLower(joined), "calendar_event_projections") || strings.Contains(strings.ToLower(joined), "backfill") {
		t.Fatalf("configuration must not backfill projections: %s", joined)
	}
}

func TestRepositoryAllDayPTOUsesTechnicianScheduleTimezone(t *testing.T) {
	date := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	request := workforce.PTORequest{AllDay: true, StartsOn: &date, State: workforce.Approved}
	proposed := calendar.TimeInterval{Start: time.Date(2026, 8, 11, 4, 30, 0, 0, time.UTC), End: time.Date(2026, 8, 11, 4, 45, 0, 0, time.UTC)}
	interval, ok := repositoryPTOInterval(request, proposed, "America/Chicago")
	if !ok || !interval.Start.Equal(time.Date(2026, 8, 10, 5, 0, 0, 0, time.UTC)) || !interval.End.Equal(time.Date(2026, 8, 11, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("interval=%+v ok=%v", interval, ok)
	}
	if _, ok = repositoryPTOInterval(request, proposed, ""); ok {
		t.Fatal("all-day PTO invented UTC without an authoritative schedule timezone")
	}
}

func TestConflictResolverAllowsScopedUnassignedProjection(t *testing.T) {
	const (
		mspID        = "00000000-0000-4000-8000-000000000901"
		clientID     = "00000000-0000-4000-8000-000000000902"
		projectionID = "00000000-0000-4000-8000-000000000903"
		sourceID     = "00000000-0000-4000-8000-000000000904"
	)
	db := &fakeSalesDB{
		queryRow: fakeRow{scan: func(v ...any) {
			*(v[0].(*string)) = clientID
			*(v[1].(*string)) = "project"
			*(v[2].(*string)) = sourceID
			*(v[3].(*string)) = ""
			*(v[4].(*[]byte)) = []byte(`{"client_ids":["` + clientID + `"]}`)
		}},
		queryRows: &fakeRows{},
	}
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	input, err := NewCalendarRepository(db).LoadConflictInput(context.Background(), authorization.Principal{Scope: scope.Principal{MSPID: mspID}}, calendar.ProposedSchedule{ProjectionID: projectionID, ClientID: clientID, Interval: calendar.TimeInterval{Start: start, End: start.Add(24 * time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	if input.Proposed.ClientID != clientID || input.Proposed.TechnicianID != "" || input.Proposed.Source.Type != "project" {
		t.Fatalf("resolved=%+v", input.Proposed)
	}
}

func TestSchedulingWorkforceAuthorityRejectsTechnicianOutsideActorTeamScope(t *testing.T) {
	const (
		mspID    = "00000000-0000-4000-8000-000000000911"
		actorID  = "00000000-0000-4000-8000-000000000912"
		clientID = "00000000-0000-4000-8000-000000000913"
		techID   = "00000000-0000-4000-8000-000000000914"
	)
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(v ...any) { *(v[0].(*bool)) = false }}}
	principal := authorization.Principal{ID: actorID, Scope: scope.Principal{MSPID: mspID}, Capabilities: authorization.NewCapabilitySet("calendar.schedule")}
	now := time.Now()
	err := NewCalendarRepository(db).AuthorizeScheduling(context.Background(), principal, calendar.ProposedSchedule{ClientID: clientID, TechnicianID: techID, Interval: calendar.TimeInterval{Start: now, End: now.Add(time.Hour)}}, now)
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("error=%v", err)
	}
	for _, required := range []string{"team_memberships", "workforce_manager_id", "calendar.workforce.manage"} {
		if !strings.Contains(db.query, required) {
			t.Fatalf("workforce authority query lacks %q: %s", required, db.query)
		}
	}
}

func TestCalendarRepositoryRejectsInvalidCustomDateFieldBeforeTransaction(t *testing.T) {
	accepted := calendar.CustomDateFieldMutation{Field: calendar.CustomDateField{ID: "00000000-0000-4000-8000-000000000811", MSPID: "00000000-0000-4000-8000-000000000812", ObjectType: "task", FieldID: "scheduled", Label: "Scheduled", Category: "operations", FieldType: calendar.FieldDateTime, SchedulingMode: calendar.EffortAllocation, CapacityBearing: true, TimezoneSource: "client", Version: 1}, ExpectedVersion: 0,
		Audit: mutation.AuditRecord{ID: "00000000-0000-4000-8000-000000000813", OccurredAt: time.Now(), MSPID: "00000000-0000-4000-8000-000000000812", ActorID: "00000000-0000-4000-8000-000000000814", SubjectID: "00000000-0000-4000-8000-000000000811", SubjectVersion: 1, CorrelationID: "00000000-0000-4000-8000-000000000815"},
		Event: mutation.EventRecord{EventID: "00000000-0000-4000-8000-000000000816", OccurredAt: time.Now(), MSPID: "00000000-0000-4000-8000-000000000812", ActorID: "00000000-0000-4000-8000-000000000814", SubjectID: "00000000-0000-4000-8000-000000000811", SubjectVersion: 1, CorrelationID: "00000000-0000-4000-8000-000000000815"}}
	err := NewCalendarRepository(&fakeSalesDB{}).UpsertCustomDateFieldAtomic(context.Background(), accepted)
	if !errors.Is(err, calendar.ErrInvalidCustomDateConfiguration) {
		t.Fatalf("error=%v", err)
	}
}

func TestCancelledDependencyPredecessorSatisfiesTerminalCondition(t *testing.T) {
	successor := calendar.TimeInterval{Start: time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)}
	rows := &fakeRows{scans: []func(...any){func(v ...any) {
		*(v[0].(*string)) = "00000000-0000-4000-8000-000000000899"
		*(v[1].(*calendar.DependencyType)) = calendar.FinishToStart
		*(v[2].(*int)) = 0
		*(v[3].(*time.Time)) = time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
		*(v[4].(*time.Time)) = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
		*(v[5].(*calendar.TerminalState)) = calendar.Cancelled
	}}}
	found, err := scanTimedDependencyConstraints(rows, successor)
	if err != nil || len(found) != 0 {
		t.Fatalf("constraints=%+v err=%v", found, err)
	}
}

func TestCalendarDependencyPersistenceUsesScopedGraphLockAndStableTraversal(t *testing.T) {
	const (
		mspID         = "00000000-0000-4000-8000-000000000101"
		clientID      = "00000000-0000-4000-8000-000000000102"
		predecessorID = "00000000-0000-4000-8000-000000000103"
		successorID   = "00000000-0000-4000-8000-000000000104"
		dependencyID  = "00000000-0000-4000-8000-000000000105"
		actorID       = "00000000-0000-4000-8000-000000000106"
		eventID       = "00000000-0000-4000-8000-000000000108"
	)
	rows := &fakeRows{scans: []func(...any){
		func(v ...any) {
			*(v[0].(*string)) = "00000000-0000-4000-8000-000000000107"
			*(v[1].(*string)) = mspID
			*(v[2].(*string)) = clientID
			*(v[3].(*string)) = predecessorID
			*(v[4].(*string)) = successorID
			*(v[5].(*calendar.DependencyType)) = calendar.StartToStart
			*(v[6].(*int)) = -15
			*(v[7].(*int64)) = 1
			*(v[8].(*time.Time)) = time.Date(2026, 8, 7, 1, 0, 0, 0, time.UTC)
			*(v[9].(*string)) = actorID
			*(v[10].(*time.Time)) = time.Date(2026, 8, 7, 1, 0, 0, 0, time.UTC)
			*(v[11].(*string)) = actorID
		},
	}}
	db := &fakeSalesDB{queryRows: rows}
	repository := NewCalendarRepository(db)
	found, err := repository.ListDependencies(context.Background(), mspID, clientID)
	if err != nil || len(found) != 1 || found[0].Type != calendar.StartToStart {
		t.Fatalf("dependencies=%+v err=%v", found, err)
	}
	if !strings.Contains(db.query, "ORDER BY predecessor_projection_id,successor_projection_id,relationship_type,id") {
		t.Fatalf("dependency traversal is not stable: %s", db.query)
	}

	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(v ...any) { *(v[0].(*bool)) = false; *(v[1].(*bool)) = false }},
		fakeRow{scan: func(v ...any) { *(v[0].(*bool)) = false }},
	}}
	repository = NewCalendarRepository(&fakeSalesDB{tx: tx})
	dependency := calendar.Dependency{ID: dependencyID, MSPID: mspID, ClientID: clientID, PredecessorID: predecessorID, SuccessorID: successorID, Type: calendar.FinishToStart, LeadLagMinutes: 30, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(), CreatedBy: actorID, UpdatedBy: actorID}
	created, err := repository.InsertDependency(context.Background(), dependency, calendar.DependencyChangeFact{EventID: eventID, ActorID: actorID, OccurredAt: dependency.CreatedAt})
	if err != nil || created != dependency || !tx.committed {
		t.Fatalf("created=%+v commit=%v err=%v", created, tx.committed, err)
	}
	joined := strings.Join(tx.queries, "\n") + "\n" + tx.query
	for _, fragment := range []string{"pg_advisory_xact_lock", "WITH RECURSIVE reachable", "INSERT INTO calendar_dependencies"} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("dependency transaction missing %q: %s", fragment, joined)
		}
	}
}

func TestCalendarDependencyDeletionAtomicallyPublishesHealthEndpoints(t *testing.T) {
	const (
		mspID         = "00000000-0000-4000-8000-000000000601"
		clientID      = "00000000-0000-4000-8000-000000000602"
		predecessorID = "00000000-0000-4000-8000-000000000603"
		successorID   = "00000000-0000-4000-8000-000000000604"
		dependencyID  = "00000000-0000-4000-8000-000000000605"
		actorID       = "00000000-0000-4000-8000-000000000606"
		eventID       = "00000000-0000-4000-8000-000000000607"
	)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	dependency := calendar.Dependency{ID: dependencyID, MSPID: mspID, ClientID: clientID, PredecessorID: predecessorID, SuccessorID: successorID, Type: calendar.FinishToStart, Version: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), CreatedBy: actorID, UpdatedBy: actorID}
	fact := calendar.DependencyDeletionFact{EventID: eventID, ActorID: actorID, OccurredAt: now}
	tx := &fakeSalesTx{}
	if err := NewCalendarRepository(&fakeSalesDB{tx: tx}).DeleteDependency(context.Background(), dependency, fact); err != nil {
		t.Fatal(err)
	}
	if !tx.committed {
		t.Fatal("dependency deletion transaction not committed")
	}
	joined := strings.Join(tx.queries, "\n")
	for _, fragment := range []string{"DELETE FROM calendar_dependencies", "INSERT INTO event_outbox", "calendar.dependency.deleted", "predecessor_projection_id", "successor_projection_id", "projection_ids"} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("deletion transaction missing %q: %s", fragment, joined)
		}
	}
}

func TestCalendarDependencyCreationAtomicallyPublishesHealthEndpoints(t *testing.T) {
	const (
		mspID         = "00000000-0000-4000-8000-000000000701"
		clientID      = "00000000-0000-4000-8000-000000000702"
		predecessorID = "00000000-0000-4000-8000-000000000703"
		successorID   = "00000000-0000-4000-8000-000000000704"
		dependencyID  = "00000000-0000-4000-8000-000000000705"
		actorID       = "00000000-0000-4000-8000-000000000706"
		eventID       = "00000000-0000-4000-8000-000000000707"
	)
	now := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
	dependency := calendar.Dependency{ID: dependencyID, MSPID: mspID, ClientID: clientID, PredecessorID: predecessorID, SuccessorID: successorID, Type: calendar.FinishToStart, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: actorID, UpdatedBy: actorID}
	fact := calendar.DependencyChangeFact{EventID: eventID, ActorID: actorID, OccurredAt: now}
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(v ...any) { *(v[0].(*bool)) = false; *(v[1].(*bool)) = false }},
		fakeRow{scan: func(v ...any) { *(v[0].(*bool)) = false }},
	}}
	created, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).InsertDependency(context.Background(), dependency, fact)
	if err != nil || created != dependency || !tx.committed {
		t.Fatalf("created=%+v committed=%v err=%v", created, tx.committed, err)
	}
	joined := strings.Join(tx.queries, "\n")
	for _, fragment := range []string{"INSERT INTO calendar_dependencies", "INSERT INTO event_outbox", "calendar.dependency.created", "predecessor_projection_id", "successor_projection_id", "projection_ids"} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("creation transaction missing %q: %s", fragment, joined)
		}
	}
	if strings.Index(joined, "INSERT INTO calendar_dependencies") > strings.Index(joined, "INSERT INTO event_outbox") {
		t.Fatalf("creation fact written before dependency: %s", joined)
	}
}

func TestCalendarDependencyCreationRollsBackWhenFactWriteFails(t *testing.T) {
	const (
		mspID         = "00000000-0000-4000-8000-000000000711"
		clientID      = "00000000-0000-4000-8000-000000000712"
		predecessorID = "00000000-0000-4000-8000-000000000713"
		successorID   = "00000000-0000-4000-8000-000000000714"
		dependencyID  = "00000000-0000-4000-8000-000000000715"
		actorID       = "00000000-0000-4000-8000-000000000716"
		eventID       = "00000000-0000-4000-8000-000000000717"
	)
	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	dependency := calendar.Dependency{ID: dependencyID, MSPID: mspID, ClientID: clientID, PredecessorID: predecessorID, SuccessorID: successorID, Type: calendar.FinishToStart, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: actorID, UpdatedBy: actorID}
	fact := calendar.DependencyChangeFact{EventID: eventID, ActorID: actorID, OccurredAt: now}
	want := errors.New("outbox unavailable")
	tx := &fakeSalesTx{queryRows: []row{
		fakeRow{scan: func(v ...any) { *(v[0].(*bool)) = false; *(v[1].(*bool)) = false }},
		fakeRow{scan: func(v ...any) { *(v[0].(*bool)) = false }},
	}, failAt: 3, failError: want}
	_, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).InsertDependency(context.Background(), dependency, fact)
	if !errors.Is(err, want) || !tx.rolledBack || tx.committed {
		t.Fatalf("error=%v rolled_back=%v committed=%v", err, tx.rolledBack, tx.committed)
	}
}

func TestCalendarHealthPersistenceClaimsVersionAndPublishesOnlyChanges(t *testing.T) {
	const (
		eventID      = "00000000-0000-4000-8000-000000000201"
		mspID        = "00000000-0000-4000-8000-000000000202"
		clientID     = "00000000-0000-4000-8000-000000000203"
		projectionID = "00000000-0000-4000-8000-000000000204"
		sourceID     = "00000000-0000-4000-8000-000000000205"
	)
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	event := calendar.HealthRecomputeEvent{SourceEventID: eventID, MSPID: mspID, Kind: calendar.HealthFactCapacity, SubjectID: projectionID, OccurredAt: now}
	update := calendar.HealthUpdate{ProjectionID: projectionID, MSPID: mspID, ClientID: clientID, Source: calendar.SourceRef{MSPID: mspID, ClientID: clientID, Type: "task", ID: sourceID}, SourceRevision: 4, Result: calendar.HealthResult{State: calendar.HealthAtRisk, Reasons: []calendar.HealthReason{{Code: "capacity_shortage"}}, RuleVersion: calendar.CurrentHealthRuleVersion, EvaluatedAt: now}}
	tx := &fakeSalesTx{queryRows: []row{healthProjectionRow("task", sourceID, clientID, "scheduled_work", 4, calendar.Active, "00000000-0000-4000-8000-000000000206", calendar.HealthOnTrack)}}
	repository := NewCalendarRepository(&fakeSalesDB{tx: tx})
	applied, err := repository.ApplyHealthResultsAtomic(context.Background(), event, []calendar.HealthUpdate{update})
	if err != nil || !applied || !tx.committed {
		t.Fatalf("applied=%v commit=%v err=%v", applied, tx.committed, err)
	}
	joined := strings.Join(tx.calls, "\n")
	for _, fragment := range []string{"INSERT INTO calendar_health_event_claims", "ON CONFLICT DO NOTHING", "FOR UPDATE", "health_state", "IS DISTINCT FROM", "source_revision=$8", "'health_changed'", "calendar_live_changes", "calendar.schedule_changed"} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("health transaction missing %q: %s", fragment, joined)
		}
	}
	payload := canonicalEventPayload(t, tx)
	assertCanonicalCalendarPayload(t, payload, "00000000-0000-4000-8000-000000000206", "conflict", "important", "task", sourceID, clientID, "scheduled_work", 4)

	duplicateTx := &fakeSalesTx{zeroRowsAt: 1}
	applied, err = NewCalendarRepository(&fakeSalesDB{tx: duplicateTx}).ApplyHealthResultsAtomic(context.Background(), event, []calendar.HealthUpdate{update})
	if err != nil || applied || !duplicateTx.committed || len(duplicateTx.queries) != 1 {
		t.Fatalf("duplicate applied=%v queries=%d commit=%v err=%v", applied, len(duplicateTx.queries), duplicateTx.committed, err)
	}
}

func TestCalendarHealthConflictFactOnlyEmitsOnNonActionableToActionableTransition(t *testing.T) {
	const (
		eventID      = "00000000-0000-4000-8000-000000000211"
		mspID        = "00000000-0000-4000-8000-000000000212"
		clientID     = "00000000-0000-4000-8000-000000000213"
		projectionID = "00000000-0000-4000-8000-000000000214"
		sourceID     = "00000000-0000-4000-8000-000000000215"
		techID       = "00000000-0000-4000-8000-000000000216"
	)
	now := time.Date(2026, 8, 15, 11, 0, 0, 0, time.UTC)
	event := calendar.HealthRecomputeEvent{SourceEventID: eventID, MSPID: mspID, Kind: calendar.HealthFactProjection, SubjectID: projectionID, OccurredAt: now}
	base := calendar.HealthUpdate{ProjectionID: projectionID, MSPID: mspID, ClientID: clientID, Source: calendar.SourceRef{MSPID: mspID, ClientID: clientID, Type: "task", ID: sourceID}, SourceRevision: 7, Result: calendar.HealthResult{State: calendar.HealthBlocked, Reasons: []calendar.HealthReason{{Code: "dependency_blocked"}}, RuleVersion: calendar.CurrentHealthRuleVersion, EvaluatedAt: now}}

	tests := []struct {
		name       string
		prior      calendar.HealthState
		terminal   calendar.TerminalState
		wantEvents int
	}{
		{name: "on track to blocked", prior: calendar.HealthOnTrack, terminal: calendar.Active, wantEvents: 1},
		{name: "at risk to blocked", prior: calendar.HealthAtRisk, terminal: calendar.Active, wantEvents: 0},
		{name: "terminal projection", prior: calendar.HealthOnTrack, terminal: calendar.Cancelled, wantEvents: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := &fakeSalesTx{queryRows: []row{healthProjectionRow("task", sourceID, clientID, "scheduled_work", 7, test.terminal, techID, test.prior)}}
			applied, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplyHealthResultsAtomic(context.Background(), event, []calendar.HealthUpdate{base})
			if err != nil || !applied {
				t.Fatalf("applied=%v err=%v", applied, err)
			}
			if got := canonicalEventCountByClass(t, tx, "conflict"); got != test.wantEvents {
				t.Fatalf("canonical conflict events=%d want=%d", got, test.wantEvents)
			}
			if test.wantEvents == 1 {
				payload := canonicalEventPayload(t, tx)
				assertCanonicalCalendarPayload(t, payload, techID, "conflict", "urgent", "task", sourceID, clientID, "scheduled_work", 7)
			}
		})
	}
}

func TestCalendarHealthConflictFactFailureRollsBackHealthAndClaim(t *testing.T) {
	const (
		eventID      = "00000000-0000-4000-8000-000000000221"
		mspID        = "00000000-0000-4000-8000-000000000222"
		clientID     = "00000000-0000-4000-8000-000000000223"
		projectionID = "00000000-0000-4000-8000-000000000224"
		sourceID     = "00000000-0000-4000-8000-000000000225"
		techID       = "00000000-0000-4000-8000-000000000226"
	)
	want := errors.New("canonical event unavailable")
	now := time.Date(2026, 8, 15, 11, 0, 0, 0, time.UTC)
	tx := &fakeSalesTx{queryRows: []row{healthProjectionRow("task", sourceID, clientID, "scheduled_work", 3, calendar.Active, techID, calendar.HealthOnTrack)}, failAt: 4, failError: want}
	event := calendar.HealthRecomputeEvent{SourceEventID: eventID, MSPID: mspID, Kind: calendar.HealthFactCapacity, SubjectID: projectionID, OccurredAt: now}
	update := calendar.HealthUpdate{ProjectionID: projectionID, MSPID: mspID, ClientID: clientID, Source: calendar.SourceRef{MSPID: mspID, ClientID: clientID, Type: "task", ID: sourceID}, SourceRevision: 3, Result: calendar.HealthResult{State: calendar.HealthAtRisk, Reasons: []calendar.HealthReason{{Code: "capacity_shortage"}}, RuleVersion: calendar.CurrentHealthRuleVersion, EvaluatedAt: now}}
	applied, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplyHealthResultsAtomic(context.Background(), event, []calendar.HealthUpdate{update})
	if !errors.Is(err, want) || applied || !tx.rolledBack || tx.committed {
		t.Fatalf("applied=%v error=%v committed=%v rolled_back=%v queries=%d", applied, err, tx.committed, tx.rolledBack, len(tx.queries))
	}
}

func healthProjectionRow(sourceType, sourceID, clientID, eventRole string, revision int64, terminal calendar.TerminalState, assigneeID string, health calendar.HealthState) fakeRow {
	return fakeRow{scan: func(v ...any) {
		*(v[0].(*string)) = sourceType
		*(v[1].(*string)) = sourceID
		*(v[2].(*string)) = clientID
		*(v[3].(*string)) = eventRole
		*(v[4].(*int64)) = revision
		*(v[5].(*calendar.TerminalState)) = terminal
		*(v[6].(*string)) = assigneeID
		*(v[7].(*calendar.HealthState)) = health
	}}
}

func TestCalendarHealthResolverReloadsAuthoritativeProjectionInputsForEveryFact(t *testing.T) {
	const (
		mspID        = "00000000-0000-4000-8000-000000000302"
		clientID     = "00000000-0000-4000-8000-000000000303"
		projectionID = "00000000-0000-4000-8000-000000000304"
		sourceID     = "00000000-0000-4000-8000-000000000305"
	)
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(-time.Minute)
	for _, kind := range []calendar.HealthFactKind{calendar.HealthFactProjection, calendar.HealthFactSourceStatus, calendar.HealthFactDependency, calendar.HealthFactSchedule, calendar.HealthFactAvailability, calendar.HealthFactCapacity, calendar.HealthFactSLA} {
		rows := &fakeRows{scans: []func(...any){func(v ...any) {
			*(v[0].(*string)) = projectionID
			*(v[1].(*string)) = mspID
			*(v[2].(*string)) = clientID
			*(v[3].(*string)) = "task"
			*(v[4].(*string)) = sourceID
			*(v[5].(*int64)) = 2
			*(v[6].(*calendar.TerminalState)) = calendar.Active
			*(v[7].(**time.Time)) = nil
			*(v[8].(**time.Time)) = &deadline
			*(v[9].(*[]byte)) = []byte(`{"blocked":true,"hard_constraint":true,"capacity_shortage":true,"dependency_delay":true,"schedule_variance":true,"sla_at_risk":true}`)
			*(v[10].(*bool)) = true
			*(v[11].(*bool)) = false
		}}}
		db := &fakeSalesDB{queryRows: rows}
		found, err := NewCalendarRepository(db).ResolveAffectedHealth(context.Background(), calendar.HealthRecomputeEvent{SourceEventID: "00000000-0000-4000-8000-000000000301", MSPID: mspID, Kind: kind, SubjectID: projectionID, OccurredAt: now})
		if err != nil || len(found) != 1 || found[0].Context.EndsAt == nil || !found[0].Context.EndsAt.Equal(deadline) || !found[0].Context.SourceBlocked || !found[0].Context.HardConstraint || !found[0].Context.CapacityShortage || !found[0].Context.DependencyDelay || !found[0].Context.ScheduleVariance || !found[0].Context.SLAAtRisk {
			t.Fatalf("kind=%q contexts=%+v err=%v", kind, found, err)
		}
		for _, fragment := range []string{"WITH RECURSIVE", "calendar_dependencies", "health_inputs", "work_records", "project_milestones", "ORDER BY projection.id"} {
			if !strings.Contains(db.query, fragment) {
				t.Errorf("kind=%q resolver missing %q", kind, fragment)
			}
		}
	}
}

func TestCalendarHealthResolverCarriesDeletedDependencyEndpoints(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	event := calendar.HealthRecomputeEvent{SourceEventID: "00000000-0000-4000-8000-000000000401", MSPID: "00000000-0000-4000-8000-000000000402", Kind: calendar.HealthFactDependency, SubjectID: "00000000-0000-4000-8000-000000000403", ProjectionIDs: []string{"00000000-0000-4000-8000-000000000404", "00000000-0000-4000-8000-000000000405"}, OccurredAt: time.Now()}
	if _, err := NewCalendarRepository(db).ResolveAffectedHealth(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(db.query, "projection.id=ANY($4::uuid[])") || !reflect.DeepEqual(db.args[3], event.ProjectionIDs) {
		t.Fatalf("query=%s args=%v", db.query, db.args)
	}
}

func TestCalendarHealthResolverPreservesAllDayDeadlineAndUsesDateConstraints(t *testing.T) {
	deadline := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	rows := &fakeRows{scans: []func(...any){func(v ...any) {
		*(v[0].(*string)) = "00000000-0000-4000-8000-000000000504"
		*(v[1].(*string)) = "00000000-0000-4000-8000-000000000502"
		*(v[2].(*string)) = "00000000-0000-4000-8000-000000000503"
		*(v[3].(*string)) = "milestone"
		*(v[4].(*string)) = "00000000-0000-4000-8000-000000000505"
		*(v[5].(*int64)) = 1
		*(v[6].(*calendar.TerminalState)) = calendar.Active
		*(v[7].(**time.Time)) = &deadline
		*(v[8].(**time.Time)) = nil
		*(v[9].(*[]byte)) = []byte(`{}`)
		*(v[10].(*bool)) = false
		*(v[11].(*bool)) = true
	}}}
	db := &fakeSalesDB{queryRows: rows}
	found, err := NewCalendarRepository(db).ResolveAffectedHealth(context.Background(), calendar.HealthRecomputeEvent{SourceEventID: "00000000-0000-4000-8000-000000000501", MSPID: "00000000-0000-4000-8000-000000000502", Kind: calendar.HealthFactDependency, SubjectID: "00000000-0000-4000-8000-000000000506", OccurredAt: time.Now()})
	if err != nil || len(found) != 1 || found[0].Context.EndsOn == nil || !found[0].Context.EndsOn.Equal(deadline) || found[0].Context.EndsAt != nil || !found[0].Context.UnmetDependency {
		t.Fatalf("contexts=%+v err=%v", found, err)
	}
	for _, fragment := range []string{"predecessor.all_day AND projection.all_day", "predecessor.ends_on", "projection.starts_on", "lead_lag_minutes / 1440"} {
		if !strings.Contains(db.query, fragment) {
			t.Errorf("date-aware dependency query missing %q: %s", fragment, db.query)
		}
	}
	if strings.Contains(db.query, "AT TIME ZONE 'UTC'") {
		t.Fatalf("all-day deadline coerced to UTC timestamp: %s", db.query)
	}
}

func TestCalendarHealthReasonsPersistInDeterministicOrder(t *testing.T) {
	result := calendar.EvaluateHealth(calendar.HealthContext{Now: time.Now(), RiskReasons: []calendar.HealthReason{{Code: "z"}, {Code: "a"}}})
	if !reflect.DeepEqual(result.Reasons, []calendar.HealthReason{{Code: "a"}, {Code: "z"}}) {
		t.Fatalf("reasons=%v", result.Reasons)
	}
}

func TestCalendarProjectionBatchUsesSourceLockRevisionUpsertDeleteLiveAndCursor(t *testing.T) {
	tx := &fakeSalesTx{queryRows: []row{fakeRow{scan: func(v ...any) { *(v[0].(*int64)) = 3; *(v[1].(*string)) = "00000000-0000-0000-0000-000000000902" }}}, queryResult: &fakeRows{scans: []func(...any){func(v ...any) {
		*(v[0].(*string)) = "due"
		*(v[1].(*string)) = ""
		*(v[2].(*string)) = "00000000-0000-0000-0000-000000000991"
		*(v[3].(*int64)) = 3
		*(v[4].(*string)) = "00000000-0000-0000-0000-000000000902"
	}}}}
	r := NewCalendarRepository(&fakeSalesDB{tx: tx})
	date := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	batch := calendar.ProjectionBatch{Source: calendar.SourceRef{MSPID: "00000000-0000-0000-0000-000000000901", ClientID: "00000000-0000-0000-0000-000000000902", Type: "task", ID: "00000000-0000-0000-0000-000000000903"}, SourceRevision: 4, Projections: []calendar.Projection{{ID: "00000000-0000-0000-0000-000000000904", Source: calendar.SourceRef{MSPID: "00000000-0000-0000-0000-000000000901", ClientID: "00000000-0000-0000-0000-000000000902", Type: "task", ID: "00000000-0000-0000-0000-000000000903"}, EventRole: "scheduled_work", SourceRevision: 4, Title: "Safe title", AllDay: true, StartsOn: &date, SchedulingMode: calendar.Informational, TerminalState: calendar.Active}}, Cursor: calendar.ProjectionCursor{ConsumerKey: "calendar", OccurredAt: time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC), EventID: "00000000-0000-0000-0000-000000000905"}}
	applied, err := r.ApplyProjectionBatchAtomic(context.Background(), batch)
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("new revision ignored")
	}
	if !tx.committed {
		t.Fatal("transaction not committed")
	}
	joined := strings.Join(tx.queries, "\n")
	for _, fragment := range []string{"pg_advisory_xact_lock", "FOR UPDATE", "INSERT INTO calendar_event_projections", "DELETE FROM calendar_event_projections", "INSERT INTO calendar_live_changes", "INSERT INTO calendar_projection_cursors"} {
		if !strings.Contains(joined, fragment) {
			t.Errorf("missing SQL contract %q", fragment)
		}
	}
	if strings.Contains(joined, "description") || strings.Contains(joined, "comment") || strings.Contains(joined, "decision_reason") {
		t.Fatal("projection SQL contains private content columns")
	}
}

func TestCalendarSourceResolverUsesTypedSubjectIdentityNotPayload(t *testing.T) {
	repository := NewCalendarRepository(&fakeSalesDB{})
	resolved, err := repository.ResolveProjectionSource(context.Background(), mutation.EventRecord{EventType: "task.updated", MSPID: "msp", ClientID: "client", SubjectType: "task", SubjectID: "task", Data: map[string]any{"source_id": "attacker", "description": "private"}})
	if err != nil {
		t.Fatal(err)
	}
	want := calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "task", ID: "task"}
	if !resolved.Relevant || resolved.Source != want {
		t.Fatalf("resolved=%+v", resolved)
	}
}

func TestCalendarSourceResolverMapsSLAChangesToParentWorkRecord(t *testing.T) {
	db := &fakeSalesDB{queryRow: fakeRow{scan: func(v ...any) {
		*(v[0].(*string)) = "msp"
		*(v[1].(*string)) = "client"
		*(v[2].(*string)) = "work"
	}}}
	resolved, err := NewCalendarRepository(db).ResolveProjectionSource(context.Background(), mutation.EventRecord{MSPID: "msp", ClientID: "client", SubjectType: "work_record_sla", SubjectID: "sla"})
	if err != nil {
		t.Fatal(err)
	}
	want := calendar.SourceRef{MSPID: "msp", ClientID: "client", Type: "work_record", ID: "work"}
	if !resolved.Relevant || resolved.Source != want {
		t.Fatalf("resolved=%+v", resolved)
	}
}

func TestCalendarReconciliationCatalogUsesCompositeClientJoin(t *testing.T) {
	db := &fakeSalesDB{queryRows: &fakeRows{}}
	repository := NewCalendarRepository(db)
	values, err := repository.ScanProjectionSources(context.Background(), calendar.ReconcileRequest{MSPID: "00000000-0000-0000-0000-000000000901", SourceTypes: []string{"task"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("values=%v", values)
	}
	for _, fragment := range []string{"FULL OUTER JOIN", "client_id IS NOT DISTINCT FROM", "source_type=$2"} {
		if !strings.Contains(db.query, fragment) {
			t.Errorf("missing reconcile SQL %q", fragment)
		}
	}
}

func TestCalendarProjectionBatchIgnoresStaleAndEqualRevision(t *testing.T) {
	for _, revision := range []int64{6, 7} {
		tx := &fakeSalesTx{queryRows: []row{fakeRow{scan: func(v ...any) { *(v[0].(*int64)) = 7; *(v[1].(*string)) = "" }}}}
		r := NewCalendarRepository(&fakeSalesDB{tx: tx})
		applied, err := r.ApplyProjectionBatchAtomic(context.Background(), calendar.ProjectionBatch{Source: calendar.SourceRef{MSPID: "00000000-0000-0000-0000-000000000911", Type: "pto", ID: "00000000-0000-0000-0000-000000000912"}, SourceRevision: revision})
		if err != nil {
			t.Fatal(err)
		}
		if applied {
			t.Fatalf("revision %d applied over 7", revision)
		}
		joined := strings.Join(tx.queries, "\n")
		if strings.Contains(joined, "INSERT INTO calendar_event_projections") {
			t.Fatal("stale/equal revision upserted")
		}
		if !tx.committed {
			t.Fatal("stale transaction not committed")
		}
	}
}

func TestCalendarProjectionBatchRollsBackEveryWriteOnFailure(t *testing.T) {
	want := errors.New("database unavailable")
	tx := &fakeSalesTx{queryRows: []row{fakeRow{err: pgx.ErrNoRows}}, queryResult: &fakeRows{}, failAt: 3, failError: want}
	r := NewCalendarRepository(&fakeSalesDB{tx: tx})
	_, err := r.ApplyProjectionBatchAtomic(context.Background(), calendar.ProjectionBatch{Source: calendar.SourceRef{MSPID: "00000000-0000-0000-0000-000000000921", Type: "pto", ID: "00000000-0000-0000-0000-000000000922"}, SourceRevision: 1})
	if !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
	if !tx.rolledBack || tx.committed {
		t.Fatalf("rollback=%v commit=%v", tx.rolledBack, tx.committed)
	}
}

func TestCanonicalCalendarInitialPTORequestProjectionDoesNotEmit(t *testing.T) {
	const (
		mspID        = "00000000-0000-4000-8000-000000000931"
		techID       = "00000000-0000-4000-8000-000000000932"
		sourceID     = "00000000-0000-4000-8000-000000000933"
		projectionID = "00000000-0000-4000-8000-000000000934"
		eventID      = "00000000-0000-4000-8000-000000000935"
	)
	tx := &fakeSalesTx{queryRows: []row{fakeRow{err: pgx.ErrNoRows}}, queryResult: &fakeRows{}}
	date := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	projection := calendar.Projection{ID: projectionID, Source: calendar.SourceRef{MSPID: mspID, Type: "pto", ID: sourceID}, EventRole: "unavailability", SourceRevision: 2, Title: "must not enter the event", AllDay: true, StartsOn: &date, SchedulingMode: calendar.Informational, OwnerID: techID, TerminalState: calendar.Active}
	applied, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplyProjectionBatchAtomic(context.Background(), calendar.ProjectionBatch{Source: projection.Source, SourceRevision: 2, Projections: []calendar.Projection{projection}, Cursor: calendar.ProjectionCursor{ConsumerKey: "calendar", OccurredAt: date, EventID: eventID}})
	if err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if got := canonicalEventCountByClass(t, tx, "pto"); got != 0 {
		t.Fatalf("initial PTO request projection emitted %d canonical facts", got)
	}
}

func TestCanonicalCalendarPTOFactDoesNotEmitForNonCalendarRevision(t *testing.T) {
	const (
		mspID        = "00000000-0000-4000-8000-000000000936"
		techID       = "00000000-0000-4000-8000-000000000937"
		sourceID     = "00000000-0000-4000-8000-000000000938"
		projectionID = "00000000-0000-4000-8000-000000000939"
	)
	date := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	tx := projectionCanonicalFactTx("unavailability", projectionID, 1, "", techID, calendar.Active, calendar.HealthInputs{}, calendar.Projection{AllDay: true, StartsOn: &date})
	projection := calendar.Projection{ID: projectionID, Source: calendar.SourceRef{MSPID: mspID, Type: "pto", ID: sourceID}, EventRole: "unavailability", SourceRevision: 2, Title: "title-only source edit", AllDay: true, StartsOn: &date, SchedulingMode: calendar.Informational, AssigneeID: techID, TerminalState: calendar.Active}
	if applied, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplyProjectionBatchAtomic(context.Background(), calendar.ProjectionBatch{Source: projection.Source, SourceRevision: 2, Projections: []calendar.Projection{projection}, Cursor: calendar.ProjectionCursor{ConsumerKey: "calendar", OccurredAt: date, EventID: "00000000-0000-4000-8000-000000000940"}}); err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if canonicalEventCount(tx) != 0 {
		t.Fatal("non-calendar PTO revision emitted a user-facing fact")
	}
}

func TestCanonicalCalendarPTOProjectionRemovalDoesNotEmit(t *testing.T) {
	const (
		mspID        = "00000000-0000-4000-8000-000000000921"
		techID       = "00000000-0000-4000-8000-000000000922"
		sourceID     = "00000000-0000-4000-8000-000000000923"
		projectionID = "00000000-0000-4000-8000-000000000924"
	)
	date := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	prior := calendar.Projection{AllDay: true, StartsOn: &date, OwnerID: techID}
	tx := projectionCanonicalFactTx("unavailability", projectionID, 1, "", "", calendar.Active, calendar.HealthInputs{}, prior)
	batch := calendar.ProjectionBatch{Source: calendar.SourceRef{MSPID: mspID, Type: "pto", ID: sourceID}, SourceRevision: 2, Cursor: calendar.ProjectionCursor{ConsumerKey: "calendar", OccurredAt: date, EventID: "00000000-0000-4000-8000-000000000925"}}
	if applied, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplyProjectionBatchAtomic(context.Background(), batch); err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if got := canonicalEventCountByClass(t, tx, "pto"); got != 0 {
		t.Fatalf("PTO projection removal emitted %d canonical facts", got)
	}
}

func TestCanonicalCalendarPTOComparisonUsesSemanticTimesAndRecurrenceJSON(t *testing.T) {
	const (
		mspID        = "00000000-0000-4000-8000-000000000926"
		techID       = "00000000-0000-4000-8000-000000000927"
		sourceID     = "00000000-0000-4000-8000-000000000928"
		projectionID = "00000000-0000-4000-8000-000000000929"
	)
	priorStart := time.Date(2026, 8, 17, 14, 0, 0, 0, time.UTC)
	newStart := priorStart.In(time.FixedZone("equivalent-offset", -4*60*60))
	rule := &calendar.RecurrenceRule{Frequency: calendar.Daily, Interval: 1}
	prior := calendar.Projection{StartsAt: &priorStart, Timezone: "America/New_York", OwnerID: techID, AssigneeID: techID, Recurrence: rule}
	tx := projectionCanonicalFactTxWithRecurrence("unavailability", projectionID, 1, "", techID, calendar.Active, calendar.HealthInputs{}, prior, []byte(`{"interval":1,"frequency":"daily"}`))
	projection := calendar.Projection{ID: projectionID, Source: calendar.SourceRef{MSPID: mspID, Type: "pto", ID: sourceID}, EventRole: "unavailability", SourceRevision: 2, Title: "same calendar state", StartsAt: &newStart, Timezone: "America/New_York", SchedulingMode: calendar.Informational, OwnerID: techID, AssigneeID: techID, TerminalState: calendar.Active, Recurrence: rule}
	if applied, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplyProjectionBatchAtomic(context.Background(), calendar.ProjectionBatch{Source: projection.Source, SourceRevision: 2, Projections: []calendar.Projection{projection}, Cursor: calendar.ProjectionCursor{ConsumerKey: "calendar", OccurredAt: priorStart, EventID: "00000000-0000-4000-8000-000000000930"}}); err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if canonicalEventCount(tx) != 0 {
		t.Fatal("semantically equivalent PTO state emitted a user-facing fact")
	}
}

func TestCanonicalCalendarProjectionHealthInputsDoNotEmitConflict(t *testing.T) {
	const (
		mspID        = "00000000-0000-4000-8000-000000000941"
		techID       = "00000000-0000-4000-8000-000000000942"
		sourceID     = "00000000-0000-4000-8000-000000000943"
		projectionID = "00000000-0000-4000-8000-000000000944"
		eventID      = "00000000-0000-4000-8000-000000000945"
	)
	start, end := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC), time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	tx := projectionCanonicalFactTx("scheduled_work", projectionID, 3, "", techID, calendar.Active, calendar.HealthInputs{})
	projection := calendar.Projection{ID: projectionID, Source: calendar.SourceRef{MSPID: mspID, Type: "task", ID: sourceID}, EventRole: "scheduled_work", SourceRevision: 4, Title: "private", StartsAt: &start, EndsAt: &end, Timezone: "UTC", SchedulingMode: calendar.FixedBlock, AssigneeID: techID, TerminalState: calendar.Active, HealthInputs: calendar.HealthInputs{HardConstraint: true}}
	if applied, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplyProjectionBatchAtomic(context.Background(), calendar.ProjectionBatch{Source: projection.Source, SourceRevision: 4, Projections: []calendar.Projection{projection}, Cursor: calendar.ProjectionCursor{ConsumerKey: "calendar", OccurredAt: start, EventID: eventID}}); err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if got := canonicalEventCountByClass(t, tx, "conflict"); got != 0 {
		t.Fatalf("projection health inputs emitted %d conflict facts", got)
	}

	alreadyActionable := projectionCanonicalFactTx("scheduled_work", projectionID, 4, "", techID, calendar.Active, calendar.HealthInputs{HardConstraint: true})
	projection.SourceRevision = 5
	if applied, err := NewCalendarRepository(&fakeSalesDB{tx: alreadyActionable}).ApplyProjectionBatchAtomic(context.Background(), calendar.ProjectionBatch{Source: projection.Source, SourceRevision: 5, Projections: []calendar.Projection{projection}, Cursor: calendar.ProjectionCursor{ConsumerKey: "calendar", OccurredAt: start.Add(time.Minute), EventID: "00000000-0000-4000-8000-000000000946"}}); err != nil || !applied {
		t.Fatalf("repeat applied=%v err=%v", applied, err)
	}
	if canonicalEventCount(alreadyActionable) != 0 {
		t.Fatal("already-actionable projection emitted another conflict fact")
	}
}

func TestCanonicalCalendarTerminalProjectionDoesNotEmitConflict(t *testing.T) {
	const (
		mspID        = "00000000-0000-4000-8000-000000000947"
		techID       = "00000000-0000-4000-8000-000000000948"
		sourceID     = "00000000-0000-4000-8000-000000000949"
		projectionID = "00000000-0000-4000-8000-000000000950"
	)
	start := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	tx := projectionCanonicalFactTx("scheduled_work", projectionID, 1, "", techID, calendar.Active, calendar.HealthInputs{})
	projection := calendar.Projection{ID: projectionID, Source: calendar.SourceRef{MSPID: mspID, Type: "task", ID: sourceID}, EventRole: "scheduled_work", SourceRevision: 2, Title: "terminal", StartsAt: &start, Timezone: "UTC", SchedulingMode: calendar.FixedBlock, AssigneeID: techID, TerminalState: calendar.Cancelled, HealthInputs: calendar.HealthInputs{HardConstraint: true}}
	if applied, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplyProjectionBatchAtomic(context.Background(), calendar.ProjectionBatch{Source: projection.Source, SourceRevision: 2, Projections: []calendar.Projection{projection}, Cursor: calendar.ProjectionCursor{ConsumerKey: "calendar", OccurredAt: start, EventID: "00000000-0000-4000-8000-000000000951"}}); err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if got := canonicalEventCountByClass(t, tx, "conflict"); got != 0 {
		t.Fatalf("terminal projection emitted %d conflict facts", got)
	}
	if got := canonicalEventCountByClass(t, tx, "cancellation"); got != 1 {
		t.Fatalf("terminal transition emitted %d cancellation facts", got)
	}
}

func TestCanonicalCalendarProjectionReactivationDoesNotEmitConflictFromInputs(t *testing.T) {
	const (
		mspID        = "00000000-0000-4000-8000-000000000952"
		techID       = "00000000-0000-4000-8000-000000000953"
		sourceID     = "00000000-0000-4000-8000-000000000954"
		projectionID = "00000000-0000-4000-8000-000000000955"
	)
	start := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	tx := projectionCanonicalFactTx("scheduled_work", projectionID, 1, "", techID, calendar.Cancelled, calendar.HealthInputs{HardConstraint: true})
	projection := calendar.Projection{ID: projectionID, Source: calendar.SourceRef{MSPID: mspID, Type: "task", ID: sourceID}, EventRole: "scheduled_work", SourceRevision: 2, Title: "reactivated", StartsAt: &start, Timezone: "UTC", SchedulingMode: calendar.FixedBlock, AssigneeID: techID, TerminalState: calendar.Active, HealthInputs: calendar.HealthInputs{HardConstraint: true}}
	if applied, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplyProjectionBatchAtomic(context.Background(), calendar.ProjectionBatch{Source: projection.Source, SourceRevision: 2, Projections: []calendar.Projection{projection}, Cursor: calendar.ProjectionCursor{ConsumerKey: "calendar", OccurredAt: start, EventID: "00000000-0000-4000-8000-000000000956"}}); err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if got := canonicalEventCountByClass(t, tx, "conflict"); got != 0 {
		t.Fatalf("projection reactivation emitted %d conflict facts from inputs", got)
	}
}

func TestCanonicalCalendarCancellationFactEmitsWhenActiveAssignmentIsRemoved(t *testing.T) {
	const (
		mspID        = "00000000-0000-4000-8000-000000000951"
		clientID     = "00000000-0000-4000-8000-000000000952"
		techID       = "00000000-0000-4000-8000-000000000953"
		sourceID     = "00000000-0000-4000-8000-000000000954"
		projectionID = "00000000-0000-4000-8000-000000000955"
	)
	tx := projectionCanonicalFactTx("scheduled_work", projectionID, 7, clientID, techID, calendar.Active, calendar.HealthInputs{})
	batch := calendar.ProjectionBatch{Source: calendar.SourceRef{MSPID: mspID, ClientID: clientID, Type: "task", ID: sourceID}, SourceRevision: 8, Cursor: calendar.ProjectionCursor{ConsumerKey: "calendar", OccurredAt: time.Date(2026, 8, 18, 11, 0, 0, 0, time.UTC), EventID: "00000000-0000-4000-8000-000000000956"}}
	if applied, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplyProjectionBatchAtomic(context.Background(), batch); err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	payload := canonicalEventPayload(t, tx)
	assertCanonicalCalendarPayload(t, payload, techID, "cancellation", "important", "task", sourceID, clientID, "scheduled_work", 8)
}

func TestCanonicalCalendarReconciliationDoesNotInventUserFacingFacts(t *testing.T) {
	const (
		mspID        = "00000000-0000-4000-8000-000000000961"
		techID       = "00000000-0000-4000-8000-000000000962"
		sourceID     = "00000000-0000-4000-8000-000000000963"
		projectionID = "00000000-0000-4000-8000-000000000964"
	)
	tx := projectionCanonicalFactTx("unavailability", projectionID, 1, "", techID, calendar.Active, calendar.HealthInputs{})
	date := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	projection := calendar.Projection{ID: projectionID, Source: calendar.SourceRef{MSPID: mspID, Type: "pto", ID: sourceID}, EventRole: "unavailability", SourceRevision: 2, Title: "private", AllDay: true, StartsOn: &date, SchedulingMode: calendar.Informational, AssigneeID: techID, TerminalState: calendar.Active}
	if applied, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplyProjectionBatchAtomic(context.Background(), calendar.ProjectionBatch{Source: projection.Source, SourceRevision: 2, Projections: []calendar.Projection{projection}, TrustedSourceReload: true}); err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if canonicalEventCount(tx) != 0 {
		t.Fatal("cursorless reconciliation emitted a user-facing fact")
	}
}

func projectionCanonicalFactTx(role, projectionID string, revision int64, clientID, assigneeID string, state calendar.TerminalState, health calendar.HealthInputs, stateProjection ...calendar.Projection) *fakeSalesTx {
	projectedState := calendar.Projection{}
	if len(stateProjection) > 0 {
		projectedState = stateProjection[0]
	}
	recurrenceJSON, _ := json.Marshal(projectedState.Recurrence)
	if projectedState.Recurrence == nil {
		recurrenceJSON = nil
	}
	return projectionCanonicalFactTxWithRecurrence(role, projectionID, revision, clientID, assigneeID, state, health, projectedState, recurrenceJSON)
}

func projectionCanonicalFactTxWithRecurrence(role, projectionID string, revision int64, clientID, assigneeID string, state calendar.TerminalState, health calendar.HealthInputs, projectedState calendar.Projection, recurrenceJSON []byte) *fakeSalesTx {
	healthJSON, _ := json.Marshal(health)
	return &fakeSalesTx{
		queryRows: []row{fakeRow{scan: func(v ...any) { *(v[0].(*int64)) = revision; *(v[1].(*string)) = clientID }}},
		queryResult: &fakeRows{scans: []func(...any){func(v ...any) {
			*(v[0].(*string)) = role
			*(v[1].(*string)) = ""
			*(v[2].(*string)) = projectionID
			*(v[3].(*int64)) = revision
			*(v[4].(*string)) = clientID
			if len(v) > 5 {
				*(v[5].(*string)) = assigneeID
				*(v[6].(*calendar.TerminalState)) = state
				*(v[7].(*[]byte)) = healthJSON
			}
			if len(v) > 8 {
				*(v[8].(*bool)) = projectedState.AllDay
				*(v[9].(**time.Time)) = projectedState.StartsOn
				*(v[10].(**time.Time)) = projectedState.EndsOn
				*(v[11].(**time.Time)) = projectedState.StartsAt
				*(v[12].(**time.Time)) = projectedState.EndsAt
				*(v[13].(*string)) = projectedState.Timezone
				*(v[14].(*[]byte)) = recurrenceJSON
			}
			if len(v) > 15 {
				*(v[15].(*string)) = projectedState.OwnerID
			}
		}}},
	}
}

func canonicalEventCount(tx *fakeSalesTx) int {
	count := 0
	for _, query := range tx.queries {
		if strings.Contains(query, "event_outbox") && strings.Contains(query, "calendar.schedule_changed") {
			count++
		}
	}
	return count
}

func canonicalEventCountByClass(t *testing.T, tx *fakeSalesTx, changeClass string) int {
	t.Helper()
	count := 0
	for index, query := range tx.queries {
		if !strings.Contains(query, "event_outbox") || !strings.Contains(query, "calendar.schedule_changed") {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(tx.args[index][len(tx.args[index])-1].([]byte), &payload); err != nil {
			t.Fatalf("decode canonical payload: %v", err)
		}
		if payload["change_class"] == changeClass {
			count++
		}
	}
	return count
}

func canonicalEventPayload(t *testing.T, tx *fakeSalesTx) map[string]any {
	t.Helper()
	for index, query := range tx.queries {
		if !strings.Contains(query, "event_outbox") || !strings.Contains(query, "calendar.schedule_changed") {
			continue
		}
		var payload map[string]any
		raw := tx.args[index][len(tx.args[index])-1]
		if err := json.Unmarshal(raw.([]byte), &payload); err != nil {
			t.Fatalf("decode canonical payload: %v", err)
		}
		return payload
	}
	t.Fatal("canonical calendar event not found")
	return nil
}

func assertCanonicalCalendarPayload(t *testing.T, payload map[string]any, recipientID, changeClass, urgency, sourceType, sourceID, clientID, eventRole string, revision int64) {
	t.Helper()
	wantKeys := []string{"action_path", "change_class", "recipient_id", "source_refs", "urgency"}
	if len(payload) != len(wantKeys) {
		t.Fatalf("payload contains non-canonical fields: %#v", payload)
	}
	if payload["recipient_id"] != recipientID || payload["change_class"] != changeClass || payload["urgency"] != urgency || payload["action_path"] != "/calendar" {
		t.Fatalf("payload=%#v", payload)
	}
	refs, ok := payload["source_refs"].([]any)
	if !ok || len(refs) != 1 {
		t.Fatalf("source_refs=%#v", payload["source_refs"])
	}
	ref := refs[0].(map[string]any)
	if len(ref) != 5 || ref["type"] != sourceType || ref["id"] != sourceID || ref["client_id"] != clientID || ref["event_role"] != eventRole || ref["source_revision"] != float64(revision) {
		t.Fatalf("source_ref=%#v", ref)
	}
}

func TestCalendarSchedulingProposalPersistsBindingsAndTypedChangesAtomically(t *testing.T) {
	tx := &fakeSalesTx{}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	start, end := now.Add(time.Hour), now.Add(2*time.Hour)
	proposal := calendar.SchedulingProposal{
		ID: "00000000-0000-4000-8000-000000000951", ActorID: "00000000-0000-4000-8000-000000000952", MSPID: "00000000-0000-4000-8000-000000000953", ClientID: "00000000-0000-4000-8000-000000000954", AuthorizationHash: strings.Repeat("ab", 32), CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute), State: "previewed",
		Bindings: []calendar.RevisionBinding{{Kind: calendar.RevisionSource, ID: "source", Version: 2}, {Kind: calendar.RevisionPolicy, ID: "policy", Version: 4}}, RequiresReason: true,
		Changes: []calendar.ProposedChange{{ID: "00000000-0000-4000-8000-000000000955", Required: true, Requested: calendar.RequestedChange{ProjectionID: "00000000-0000-4000-8000-000000000956", Source: calendar.SourceRef{MSPID: "00000000-0000-4000-8000-000000000953", ClientID: "00000000-0000-4000-8000-000000000954", Type: "task", ID: "00000000-0000-4000-8000-000000000957"}, EventRole: "scheduled_work", SourceRevision: 2, StartsAt: &start, EndsAt: &end, Timezone: "UTC"}}},
	}
	if err := NewCalendarRepository(&fakeSalesDB{tx: tx}).SaveSchedulingProposal(context.Background(), proposal); err != nil {
		t.Fatal(err)
	}
	assertQueryOrder(t, tx.queries, "INSERT INTO calendar_scheduling_proposals", "INSERT INTO calendar_proposal_changes")
	if !tx.committed || tx.rolledBack {
		t.Fatalf("commit=%v rollback=%v", tx.committed, tx.rolledBack)
	}
	joined := strings.Join(tx.queries, "\n")
	if strings.Contains(joined, "calendar_event_projections SET") {
		t.Fatalf("proposal preview wrote projection: %s", joined)
	}
}

func TestCalendarSchedulingAtomicCallbackFailureRollsBack(t *testing.T) {
	proposalID, mspID, actorID := "00000000-0000-4000-8000-000000000961", "00000000-0000-4000-8000-000000000962", "00000000-0000-4000-8000-000000000963"
	principal := authorization.Principal{ID: actorID, Scope: scope.Principal{MSPID: mspID}, Capabilities: authorization.NewCapabilitySet("calendar.schedule")}
	hash := calendar.AuthorizationFingerprint(principal)
	tx := &fakeSalesTx{queryRow: fakeRow{scan: func(v ...any) {
		*v[0].(*string) = actorID
		*v[1].(*string) = "previewed"
		*v[2].(*string) = hash
		*v[3].(*time.Time) = time.Now().Add(time.Hour)
		*v[4].(*[]byte) = []byte(`{"bindings":null}`)
	}}}
	want := errors.New("injected callback failure")
	_, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplySchedulingProposalAtomic(context.Background(), calendar.ScheduleApplyRequest{Principal: principal, Proposal: calendar.SchedulingProposal{ID: proposalID, ActorID: actorID, MSPID: mspID}, EvaluatedAt: time.Now()}, func(ctx context.Context, scheduleTx calendar.ScheduleTx) error {
		raw := scheduleTx.(*calendarScheduleTx)
		_, _ = raw.tx.Exec(ctx, "UPDATE tasks SET title=title")
		return want
	})
	if !errors.Is(err, want) || !tx.rolledBack || tx.committed {
		t.Fatalf("err=%v rollback=%v commit=%v", err, tx.rolledBack, tx.committed)
	}
}

func TestCanonicalCalendarScheduleFactGroupsAcceptedChangesAfterTypedMutations(t *testing.T) {
	const (
		proposalID    = "00000000-0000-4000-8000-000000000971"
		mspID         = "00000000-0000-4000-8000-000000000972"
		clientID      = "00000000-0000-4000-8000-000000000973"
		actorID       = "00000000-0000-4000-8000-000000000974"
		techID        = "00000000-0000-4000-8000-000000000975"
		changeID      = "00000000-0000-4000-8000-000000000976"
		projectionID  = "00000000-0000-4000-8000-000000000977"
		sourceID      = "00000000-0000-4000-8000-000000000978"
		correlationID = "00000000-0000-4000-8000-000000000979"
	)
	now := time.Date(2026, 8, 19, 13, 0, 0, 0, time.UTC)
	start, end := now.Add(time.Hour), now.Add(2*time.Hour)
	source := calendar.SourceRef{MSPID: mspID, ClientID: clientID, Type: "task", ID: sourceID}
	requested := calendar.RequestedChange{ProjectionID: projectionID, Source: source, EventRole: "scheduled_work", SourceRevision: 3, StartsAt: &start, EndsAt: &end, Timezone: "UTC"}
	change := calendar.ProposedChange{ID: changeID, Requested: requested, Prepared: calendar.PreparedChange{ID: changeID, Request: requested, Source: source, EventRole: "scheduled_work", ExpectedSourceRevision: 3}, Schedule: calendar.ProposedSchedule{ProjectionID: projectionID, ClientID: clientID, TechnicianID: techID}, Conflicts: []calendar.Conflict{{Severity: calendar.ConflictWarning}}}
	stored, err := json.Marshal(map[string]any{"requested": requested, "required": false, "schedule": change.Schedule})
	if err != nil {
		t.Fatal(err)
	}
	conflicts, _ := json.Marshal(change.Conflicts)
	principal := authorization.Principal{ID: actorID, Scope: scope.Principal{MSPID: mspID}, Capabilities: authorization.NewCapabilitySet("calendar.schedule", "task.edit")}
	tx := &fakeSalesTx{
		queryRows: []row{
			fakeRow{scan: func(v ...any) {
				*v[0].(*string) = actorID
				*v[1].(*string) = "previewed"
				*v[2].(*string) = calendar.AuthorizationFingerprint(principal)
				*v[3].(*time.Time) = now.Add(time.Hour)
				*v[4].(*[]byte) = []byte(`{"bindings":null}`)
			}},
			fakeRow{scan: func(v ...any) { *v[0].(*bool) = true }},
			fakeRow{scan: func(v ...any) { *v[0].(*bool) = true }},
		},
		queryResult: &fakeRows{scans: []func(...any){func(v ...any) {
			*v[0].(*string) = changeID
			*v[1].(*string) = "task"
			*v[2].(*string) = sourceID
			*v[3].(*string) = "scheduled_work"
			*v[4].(*int64) = 3
			*v[5].(*[]byte) = stored
			*v[6].(*[]byte) = conflicts
			*v[7].(*[]byte) = []byte(`[]`)
		}}},
	}
	result, err := NewCalendarRepository(&fakeSalesDB{tx: tx}).ApplySchedulingProposalAtomic(context.Background(), calendar.ScheduleApplyRequest{Principal: principal, Proposal: calendar.SchedulingProposal{ID: proposalID, ActorID: actorID, MSPID: mspID}, Changes: []calendar.ProposedChange{change}, EvaluatedAt: now}, func(ctx context.Context, scheduleTx calendar.ScheduleTx) error {
		raw := scheduleTx.(*calendarScheduleTx)
		raw.correlationID, raw.appliedAt = correlationID, now
		_, writeErr := raw.tx.Exec(ctx, "UPDATE tasks SET version=version+1")
		return writeErr
	})
	if err != nil || result.CorrelationID != correlationID || !tx.committed {
		t.Fatalf("result=%+v err=%v committed=%v", result, err, tx.committed)
	}
	payload := canonicalEventPayload(t, tx)
	assertCanonicalCalendarPayload(t, payload, techID, "schedule", "important", "task", sourceID, clientID, "scheduled_work", 4)
	joined := strings.Join(tx.queries, "\n")
	if strings.Index(joined, "UPDATE tasks SET version=version+1") > strings.Index(joined, "calendar.schedule_changed") || strings.Index(joined, "calendar.schedule_changed") > strings.Index(joined, "UPDATE calendar_scheduling_proposals") {
		t.Fatalf("canonical event was not between typed mutation and proposal commit: %s", joined)
	}
}
