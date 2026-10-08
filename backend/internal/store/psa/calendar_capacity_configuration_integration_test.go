package psa_test

import (
	"context"
	"errors"
	"os"
	"sync"
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

func TestCalendarCapacityAndConfigurationAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for calendar capacity/configuration verification")
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
	mspID, clientA, clientB, clientC, actorID := id.New(), id.New(), id.New(), id.New(), id.New()
	scheduleID, windowID := id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, query, args...); execErr != nil {
			t.Fatalf("fixture: %v", execErr)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Task 6 MSP',$3,$3)`, mspID, "M-"+mspID, actorID)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name,lifecycle_state) VALUES($1,$2,$3,'Task 6 tech','active')`, actorID, mspID, actorID+"@example.test")
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$4,$5,'Task 6 A',$7,$7),($2,$4,$6,'Task 6 B',$7,$7),($3,$4,$8,'Task 6 C',$7,$7)`, clientA, clientB, clientC, mspID, "A-"+clientA, "B-"+clientB, actorID, "C-"+clientC)
	exec(`INSERT INTO technician_schedule_versions(id,msp_id,technician_id,timezone,effective_from,version,lifecycle_state,created_by) VALUES($1,$2,$3,'America/Chicago','2026-01-01',1,'active',$3)`, scheduleID, mspID, actorID)
	exec(`INSERT INTO technician_schedule_windows(id,schedule_version_id,msp_id,technician_id,weekday,starts_local,ends_local,capacity_percent) VALUES($1,$2,$3,$4,1,'09:00','17:00',100)`, windowID, scheduleID, mspID, actorID)
	exec(`INSERT INTO pto_requests(id,msp_id,technician_id,starts_at,ends_at,timezone,all_day,pto_type,state,version,created_by,updated_by) VALUES($1,$2,$3,'2026-08-10 14:00Z','2026-08-10 16:00Z','America/Chicago',false,'vacation','requested',1,$3,$3)`, id.New(), mspID, actorID)
	exec(`INSERT INTO pto_requests(id,msp_id,technician_id,starts_at,ends_at,timezone,all_day,pto_type,state,decided_by,decided_at,version,created_by,updated_by) VALUES($1,$2,$3,'2026-08-10 18:00Z','2026-08-10 20:00Z','America/Chicago',false,'vacation','approved',$3,now(),1,$3,$3)`, id.New(), mspID, actorID)
	projectionByClient := map[string]string{}
	for index, clientID := range []string{clientA, clientB} {
		start := time.Date(2026, 8, 10, 14+index, 0, 0, 0, time.UTC)
		projectionID := id.New()
		projectionByClient[clientID] = projectionID
		exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,capacity_bearing,assignee_id,planned_minutes,terminal_state) VALUES($1,$2,$3,'task',$4,'scheduled_work',1,'Task 6 work',$5,$6,'America/Chicago',false,'fixed_block',true,$7,60,'active')`, projectionID, mspID, clientID, id.New(), start, start.Add(time.Hour), actorID)
	}
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,capacity_bearing,assignee_id,planned_minutes,terminal_state) VALUES($1,$2,$3,'task',$4,'scheduled_work',1,'Effort range','2026-08-10 14:00Z','2026-08-10 22:00Z','America/Chicago',false,'effort_allocation',true,$5,60,'active')`, id.New(), mspID, clientA, id.New(), actorID)
	recurringProjectionID := id.New()
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,capacity_bearing,assignee_id,planned_minutes,recurrence_rule,terminal_state) VALUES($1,$2,$3,'task',$4,'scheduled_work',1,'Cancelled recurring work','2026-08-10 21:00Z','2026-08-10 22:00Z','America/Chicago',false,'fixed_block',true,$5,60,'{"frequency":"daily","interval":1,"count":2}'::jsonb,'active')`, recurringProjectionID, mspID, clientA, id.New(), actorID)
	exec(`INSERT INTO calendar_recurrence_exceptions(id,msp_id,client_id,client_scope_key,projection_id,original_local_key,state,source_revision,created_by,updated_by) VALUES($1::uuid,$2::uuid,$3::uuid,'client:'||$3::uuid::text,$4::uuid,'2026-08-10T16:00:00','cancelled',1,$5::uuid,$5::uuid)`, id.New(), mspID, clientA, recurringProjectionID, actorID)
	type maintenanceCase struct {
		id         string
		start, end time.Time
		severity   calendar.ConflictSeverity
	}
	maintenanceCases := []maintenanceCase{
		{id: id.New(), start: time.Date(2026, 8, 10, 16, 0, 0, 0, time.UTC), end: time.Date(2026, 8, 10, 16, 15, 0, 0, time.UTC), severity: calendar.ConflictInfo},
		{id: id.New(), start: time.Date(2026, 8, 10, 16, 15, 0, 0, time.UTC), end: time.Date(2026, 8, 10, 16, 30, 0, 0, time.UTC), severity: calendar.ConflictWarning},
		{id: id.New(), start: time.Date(2026, 8, 10, 16, 30, 0, 0, time.UTC), end: time.Date(2026, 8, 10, 16, 45, 0, 0, time.UTC), severity: calendar.ConflictOverrideable},
		{id: id.New(), start: time.Date(2026, 8, 10, 16, 45, 0, 0, time.UTC), end: time.Date(2026, 8, 10, 17, 0, 0, 0, time.UTC), severity: calendar.ConflictHard},
	}
	for _, maintenance := range maintenanceCases {
		exec(`INSERT INTO maintenance_windows(id,msp_id,title,starts_at,ends_at,timezone,all_day,protected,conflict_policy,status,created_by,updated_by) VALUES($1,$2,'Client A maintenance',$3,$4,'America/Chicago',false,true,$5,'planned',$6,$6)`, maintenance.id, mspID, maintenance.start, maintenance.end, maintenance.severity, actorID)
		exec(`INSERT INTO maintenance_window_scopes(id,maintenance_window_id,msp_id,client_id,scope_type,created_by) VALUES($1,$2,$3,$4,'client',$5)`, id.New(), maintenance.id, mspID, clientA, actorID)
	}
	window := calendar.QueryWindow{Start: time.Date(2026, 8, 10, 5, 0, 0, 0, time.UTC), End: time.Date(2026, 8, 11, 5, 0, 0, 0, time.UTC)}
	available, err := calendar.NewAvailabilityService(repository).Resolve(ctx, mspID, actorID, window)
	if err != nil {
		t.Fatal(err)
	}
	if available.AvailableMinutes != 360 || available.TentativeMinutes != 120 {
		t.Fatalf("availability=%+v", available)
	}
	principal := authorization.Principal{ID: actorID, Scope: scope.Principal{MSPID: mspID}, Capabilities: authorization.NewCapabilitySet("calendar.policy.manage")}
	for _, maintenance := range maintenanceCases {
		windowConflicts, windowErr := calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: projectionByClient[clientA], TechnicianID: actorID, ClientID: clientA, Interval: calendar.TimeInterval{Start: maintenance.start, End: maintenance.end}})
		if windowErr != nil {
			t.Fatal(windowErr)
		}
		found := false
		for _, conflict := range windowConflicts {
			if conflict.ReasonCode == string(calendar.ConflictProtectedMaintenance) && conflict.Related.ID == maintenance.id {
				found = conflict.Severity == maintenance.severity && conflict.PolicyID == "maintenance_window:"+maintenance.id && conflict.PolicyVersion == 1 && conflict.ReasonRequired == (maintenance.severity == calendar.ConflictOverrideable)
			}
		}
		if !found {
			t.Fatalf("maintenance policy evidence not preserved for %s: %+v", maintenance.severity, windowConflicts)
		}
	}
	bConflicts, err := calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: projectionByClient[clientB], TechnicianID: actorID, ClientID: clientB, Interval: calendar.TimeInterval{Start: maintenanceCases[0].start, End: maintenanceCases[len(maintenanceCases)-1].end}})
	if err != nil {
		t.Fatal(err)
	}
	for _, conflict := range bConflicts {
		if conflict.ReasonCode == string(calendar.ConflictProtectedMaintenance) {
			t.Fatalf("client A maintenance leaked into client B proposal: %+v", bConflicts)
		}
	}
	omittedScopeConflicts, err := calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: projectionByClient[clientA], Interval: calendar.TimeInterval{Start: maintenanceCases[0].start, End: maintenanceCases[0].end}})
	if err != nil {
		t.Fatal(err)
	}
	foundOmittedScopeMaintenance := false
	for _, conflict := range omittedScopeConflicts {
		foundOmittedScopeMaintenance = foundOmittedScopeMaintenance || conflict.ReasonCode == string(calendar.ConflictProtectedMaintenance)
	}
	if !foundOmittedScopeMaintenance {
		t.Fatalf("omitted caller scope bypassed authoritative maintenance: %+v", omittedScopeConflicts)
	}
	_, err = calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: projectionByClient[clientB], ClientID: clientA, TechnicianID: actorID, Interval: calendar.TimeInterval{Start: maintenanceCases[0].start, End: maintenanceCases[0].end}})
	if !errors.Is(err, calendar.ErrInvalidConflictInput) {
		t.Fatalf("spoofed client scope error=%v", err)
	}
	_, err = calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: projectionByClient[clientA], ClientID: clientA, TechnicianID: id.New(), TeamID: id.New(), ServiceID: id.New(), AssetID: id.New(), Interval: calendar.TimeInterval{Start: maintenanceCases[0].start, End: maintenanceCases[0].end}})
	if !errors.Is(err, calendar.ErrInvalidConflictInput) {
		t.Fatalf("spoofed technician/team/service/asset scope error=%v", err)
	}
	capacity, err := calendar.NewCapacityService(repository).Calculate(ctx, principal, window, []string{actorID})
	if err != nil {
		t.Fatal(err)
	}
	if capacity[actorID].CommittedMinutes != 180 || capacity[actorID].RemainingMinutes != 180 {
		t.Fatalf("capacity=%+v", capacity[actorID])
	}
	lateConflicts, err := calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: projectionByClient[clientA], TechnicianID: actorID, ClientID: clientA, Interval: calendar.TimeInterval{Start: time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC), End: time.Date(2026, 8, 10, 22, 0, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, conflict := range lateConflicts {
		if conflict.ReasonCode == string(calendar.ConflictOrdinaryOverbooking) {
			t.Fatalf("effort range or cancelled recurrence was treated as occupied time: %+v", lateConflicts)
		}
	}
	configuration := calendar.NewConfigurationService(repository, func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) }, id.New)
	policies, err := configuration.ReplaceConflictPolicies(ctx, calendar.ReplaceConflictPoliciesCommand{Principal: principal, Scope: calendar.ConflictPolicyScope{Type: calendar.ConflictScopeMSP}, Rules: []calendar.ConflictPolicyRule{{Kind: calendar.ConflictApprovedPTO, Severity: calendar.ConflictInfo}, {Kind: calendar.ConflictNonWorkingTime, Severity: calendar.ConflictWarning}, {Kind: calendar.ConflictProtectedMaintenance, Severity: calendar.ConflictWarning}, {Kind: calendar.ConflictOrdinaryOverbooking, Severity: calendar.ConflictOverrideable}}, ExpectedVersion: 0, Source: "integration"})
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 4 {
		t.Fatalf("policies=%+v", policies)
	}
	for _, testCase := range []struct {
		maintenance maintenanceCase
		severity    calendar.ConflictSeverity
		policyID    string
	}{
		{maintenance: maintenanceCases[0], severity: calendar.ConflictWarning, policyID: policies[0].ID},
		{maintenance: maintenanceCases[3], severity: calendar.ConflictHard, policyID: "maintenance_window:" + maintenanceCases[3].id},
	} {
		scopedConflicts, scopedErr := calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: projectionByClient[clientA], TechnicianID: actorID, ClientID: clientA, Interval: calendar.TimeInterval{Start: testCase.maintenance.start, End: testCase.maintenance.end}})
		if scopedErr != nil {
			t.Fatal(scopedErr)
		}
		found := false
		for _, conflict := range scopedConflicts {
			if conflict.ReasonCode == string(calendar.ConflictProtectedMaintenance) && conflict.Related.ID == testCase.maintenance.id {
				found = conflict.Severity == testCase.severity && conflict.PolicyID == testCase.policyID
			}
		}
		if !found {
			t.Fatalf("strongest maintenance policy not selected: %+v", scopedConflicts)
		}
	}
	predecessorID, successorID, dependencyID := id.New(), id.New(), id.New()
	exec(`INSERT INTO calendar_event_projections(id,msp_id,client_id,source_type,source_id,event_role,source_revision,title,starts_at,ends_at,timezone,all_day,scheduling_mode,capacity_bearing,assignee_id,planned_minutes,terminal_state) VALUES($1,$2,$3,'task',$4,'scheduled_work',1,'Dependency predecessor','2026-08-10 20:00Z','2026-08-10 21:00Z','America/Chicago',false,'fixed_block',false,$7,0,'active'),($5,$2,$3,'task',$6,'scheduled_work',1,'Dependency successor','2026-08-10 21:00Z','2026-08-10 22:00Z','America/Chicago',false,'fixed_block',false,$7,0,'active')`, predecessorID, mspID, clientA, id.New(), successorID, id.New(), actorID)
	exec(`INSERT INTO calendar_dependencies(id,msp_id,client_id,predecessor_projection_id,successor_projection_id,relationship_type,lead_lag_minutes,created_by,updated_by) VALUES($1,$2,$3,$4,$5,'finish_to_start',0,$6,$6)`, dependencyID, mspID, clientA, predecessorID, successorID, actorID)
	dependencyConflicts, err := calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: successorID, TechnicianID: actorID, ClientID: clientA, Interval: calendar.TimeInterval{Start: time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC), End: time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatal(err)
	}
	foundDependency := false
	for _, conflict := range dependencyConflicts {
		if conflict.ReasonCode == "dependency_constraint" {
			foundDependency = conflict.Severity == calendar.ConflictHard && conflict.Related == (calendar.SafeSourceRef{Type: "calendar_dependency", ID: dependencyID})
		}
	}
	if !foundDependency {
		t.Fatalf("unmet dependency did not surface as a safe hard conflict: %+v", dependencyConflicts)
	}
	exec(`UPDATE calendar_event_projections SET terminal_state='cancelled' WHERE id=$1`, predecessorID)
	cancelledDependencyConflicts, err := calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: successorID, TechnicianID: actorID, ClientID: clientA, Interval: calendar.TimeInterval{Start: time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC), End: time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, conflict := range cancelledDependencyConflicts {
		if conflict.ReasonCode == "dependency_constraint" {
			t.Fatalf("cancelled predecessor remained an unmet dependency: %+v", cancelledDependencyConflicts)
		}
	}
	conflicts, err := calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: projectionByClient[clientA], TechnicianID: actorID, ClientID: clientA, Interval: calendar.TimeInterval{Start: time.Date(2026, 8, 10, 18, 30, 0, 0, time.UTC), End: time.Date(2026, 8, 10, 19, 30, 0, 0, time.UTC)}})
	if err != nil || len(conflicts) == 0 || conflicts[0].Severity != calendar.ConflictInfo || conflicts[0].PolicyVersion != 1 {
		t.Fatalf("conflicts=%+v err=%v", conflicts, err)
	}
	for _, conflict := range conflicts {
		if conflict.ReasonCode == string(calendar.ConflictProtectedMaintenance) {
			t.Fatalf("unrelated client maintenance leaked into conflict input: %+v", conflicts)
		}
	}
	gapConflicts, err := calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: projectionByClient[clientA], TechnicianID: actorID, ClientID: clientA, Interval: calendar.TimeInterval{Start: time.Date(2026, 8, 10, 13, 30, 0, 0, time.UTC), End: time.Date(2026, 8, 10, 14, 30, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatal(err)
	}
	foundExactGap := false
	for _, conflict := range gapConflicts {
		if conflict.ReasonCode == string(calendar.ConflictNonWorkingTime) {
			foundExactGap = conflict.Interval.Start.Equal(time.Date(2026, 8, 10, 13, 30, 0, 0, time.UTC)) && conflict.Interval.End.Equal(time.Date(2026, 8, 10, 14, 0, 0, 0, time.UTC))
		}
	}
	if !foundExactGap {
		t.Fatalf("non-working conflict did not report the exact uncovered gap: %+v", gapConflicts)
	}
	allDayPTOID := id.New()
	exec(`INSERT INTO pto_requests(id,msp_id,technician_id,starts_on,all_day,pto_type,state,decided_by,decided_at,version,created_by,updated_by) VALUES($1,$2,$3,'2026-08-10',true,'vacation','approved',$3,now(),1,$3,$3)`, allDayPTOID, mspID, actorID)
	boundaryConflicts, err := calendar.NewConflictService(repository).Evaluate(ctx, principal, calendar.ProposedSchedule{ProjectionID: projectionByClient[clientA], TechnicianID: actorID, ClientID: clientA, Interval: calendar.TimeInterval{Start: time.Date(2026, 8, 11, 4, 30, 0, 0, time.UTC), End: time.Date(2026, 8, 11, 4, 45, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatal(err)
	}
	foundBoundaryPTO := false
	for _, conflict := range boundaryConflicts {
		if conflict.ReasonCode == string(calendar.ConflictApprovedPTO) && conflict.Related.ID == allDayPTOID {
			foundBoundaryPTO = true
		}
	}
	if !foundBoundaryPTO {
		t.Fatalf("timezone-less all-day PTO was not interpreted in the technician schedule timezone: %+v", boundaryConflicts)
	}
	var projectionsBefore int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM calendar_event_projections WHERE msp_id=$1`, mspID).Scan(&projectionsBefore); err != nil {
		t.Fatal(err)
	}
	field, err := configuration.UpsertCustomDateField(ctx, calendar.UpsertCustomDateCommand{Principal: principal, ObjectType: "task", FieldID: "scheduled_follow_up", Label: "Scheduled follow up", Category: "operations", FieldType: calendar.FieldDateTime, SchedulingMode: calendar.EffortAllocation, CapacityBearing: true, TimezoneSource: "client", PlannedEffortSource: "task.estimate_minutes", ExpectedVersion: 0, Source: "integration"})
	if err != nil {
		t.Fatal(err)
	}
	fields, err := repository.ListCustomDateFields(ctx, principal)
	if err != nil || len(fields) != 1 || fields[0].FieldType != calendar.FieldDateTime || fields[0].TimezoneSource != "client" {
		t.Fatalf("typed date definition round trip=%+v err=%v", fields, err)
	}
	policies, readErr := repository.ListConflictPolicies(ctx, principal, calendar.ConflictPolicyScope{Type: calendar.ConflictScopeMSP})
	if readErr != nil || len(policies) != 4 {
		t.Fatalf("policy HTTP read=%+v err=%v", policies, readErr)
	}
	for _, policy := range policies {
		if policy.ID == "" || policy.Version != 1 || policy.Kind == "" || policy.Severity == "" {
			t.Fatalf("incomplete policy rule=%+v", policy)
		}
	}
	var plannedSource string
	var projectionsAfter, facts int
	if err = pool.QueryRow(ctx, `SELECT planned_effort_source FROM calendar_custom_date_fields WHERE id=$1`, field.ID).Scan(&plannedSource); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM calendar_event_projections WHERE msp_id=$1`, mspID).Scan(&projectionsAfter); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE msp_id=$1 AND event_type IN('calendar.conflict_policies.replaced','calendar.custom_date_field.upserted')`, mspID).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if plannedSource != "task.estimate_minutes" || projectionsAfter != projectionsBefore || facts != 2 {
		t.Fatalf("planned_source=%q projections=%d/%d facts=%d", plannedSource, projectionsBefore, projectionsAfter, facts)
	}
	if _, err = pool.Exec(ctx, `UPDATE calendar_custom_date_fields SET planned_effort_source=NULL WHERE id=$1`, field.ID); err == nil {
		t.Fatal("database accepted a capacity-bearing custom date without planned_effort_source")
	}

	commands := []calendar.ReplaceConflictPoliciesCommand{
		{Principal: principal, Scope: calendar.ConflictPolicyScope{Type: calendar.ConflictScopeTechnician, TechnicianID: actorID}, Rules: []calendar.ConflictPolicyRule{{Kind: calendar.ConflictApprovedPTO, Severity: calendar.ConflictHard}}, ExpectedVersion: 0, Source: "integration-concurrent-a"},
		{Principal: principal, Scope: calendar.ConflictPolicyScope{Type: calendar.ConflictScopeTechnician, TechnicianID: actorID}, Rules: []calendar.ConflictPolicyRule{{Kind: calendar.ConflictApprovedPTO, Severity: calendar.ConflictWarning}}, ExpectedVersion: 0, Source: "integration-concurrent-b"},
	}
	startConcurrent := make(chan struct{})
	errorsFound := make([]error, len(commands))
	var wait sync.WaitGroup
	for index := range commands {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-startConcurrent
			_, errorsFound[index] = configuration.ReplaceConflictPolicies(ctx, commands[index])
		}(index)
	}
	close(startConcurrent)
	wait.Wait()
	succeeded, conflicted := 0, 0
	for _, foundErr := range errorsFound {
		if foundErr == nil {
			succeeded++
		} else if errors.Is(foundErr, calendar.ErrConflictPolicyVersionConflict) {
			conflicted++
		} else {
			t.Fatalf("concurrent policy replace returned raw/unexpected error: %v", foundErr)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent policy replace results=%v", errorsFound)
	}
}
