package psa_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workforce"
)

func TestTypedCalendarRepositoriesAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for typed calendar verification")
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
	msp, client, otherClient, actor, owner := id.New(), id.New(), id.New(), id.New(), id.New()
	work, foreignWork, serviceID, assetID, contractID := id.New(), id.New(), id.New(), id.New(), id.New()
	pipeline, stage, opportunity := id.New(), id.New(), id.New()
	proposal, proposalVersion, project, phase := id.New(), id.New(), id.New(), id.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatalf("fixture: %v", e)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'calendar',$3,$3)`, msp, "M-"+msp, actor)
	for _, c := range []string{client, otherClient} {
		exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'client',$4,$4)`, c, msp, "C-"+c, actor)
	}
	for _, tech := range []string{actor, owner} {
		exec(`INSERT INTO technicians(id,msp_id,email,display_name,lifecycle_state) VALUES($1,$2,$3,'tech','active')`, tech, msp, tech+"@example.test")
	}
	team := id.New()
	exec(`INSERT INTO teams(id,msp_id,key,name,workforce_manager_id) VALUES($1,$2,$3,'Calendar team',$4)`, team, msp, "calendar-"+team, actor)
	for _, tech := range []string{actor, owner} {
		exec(`INSERT INTO team_memberships(team_id,technician_id,msp_id,lifecycle_state,created_at,created_by,updated_at,updated_by) VALUES($1,$2,$3,'active',CURRENT_TIMESTAMP,$4,CURRENT_TIMESTAMP,$4)`, team, tech, msp, actor)
	}
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES($1,$2,$3,$4,'incident','calendar','new','normal',$5,$5)`, work, msp, client, "W-"+work, actor)
	exec(`INSERT INTO work_records(id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES($1,$2,$3,$4,'incident','foreign','new','normal',$5,$5)`, foreignWork, msp, otherClient, "W-"+foreignWork, actor)
	exec(`INSERT INTO services(id,msp_id,client_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,$4,'service',$5,$5)`, serviceID, msp, client, "S-"+serviceID, actor)
	exec(`INSERT INTO assets(id,msp_id,client_id,display_id,name,asset_type,created_by,updated_by) VALUES($1,$2,$3,$4,'asset','server',$5,$5)`, assetID, msp, client, "A-"+assetID, actor)
	exec(`INSERT INTO contracts(id,msp_id,client_id,display_id,name,starts_on,created_by,updated_by) VALUES($1,$2,$3,$4,'contract',CURRENT_DATE,$5,$5)`, contractID, msp, client, "K-"+contractID, actor)
	exec(`INSERT INTO pipelines(id,msp_id,key,name,created_by,updated_by) VALUES($1,$2,$3,'calendar',$4,$4)`, pipeline, msp, "calendar-"+pipeline, actor)
	exec(`INSERT INTO pipeline_stages(id,pipeline_id,msp_id,key,name,position,probability,forecast_category) VALUES($1,$2,$3,'qualified','Qualified',1,50,'weighted')`, stage, pipeline, msp)
	exec(`INSERT INTO opportunities(id,msp_id,client_id,pipeline_id,stage_id,display_id,name,currency,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$6,'calendar','USD',$7,$7)`, opportunity, msp, client, pipeline, stage, "O-"+opportunity, actor)
	exec(`INSERT INTO proposals(id,msp_id,client_id,opportunity_id,display_id,current_version,state,created_by,updated_by) VALUES($1,$2,$3,$4,$5,1,'draft',$6,$6)`, proposal, msp, client, opportunity, "P-"+proposal, actor)
	exec(`INSERT INTO proposal_versions(id,proposal_id,msp_id,version,currency,subtotal_minor,tax_minor,total_minor,cost_minor,margin_minor,issued_by,pdf_snapshot_id) VALUES($1,$2,$3,1,'USD',0,0,0,0,0,$4,$5)`, proposalVersion, proposal, msp, actor, id.New())
	exec(`INSERT INTO projects(id,msp_id,client_id,display_id,name,original_proposal_version_id,created_by,updated_by) VALUES($1,$2,$3,$4,'project',$5,$6,$6)`, project, msp, client, "PRJ-"+project, proposalVersion, actor)
	exec(`INSERT INTO phases(id,project_id,msp_id,client_id,name,position) VALUES($1,$2,$3,$4,'phase',1)`, phase, project, msp, client)
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	admin := authorization.Principal{ID: actor, Scope: scope.Principal{MSPID: msp}, Capabilities: authorization.NewCapabilitySet("calendar.workforce.manage", "calendar.commitment.manage", "project.edit")}
	clientEditor := authorization.Principal{ID: actor, Scope: scope.Principal{MSPID: msp, ClientID: client}, Capabilities: authorization.NewCapabilitySet("work_record.edit", "task.edit", "project.edit", "asset.edit", "knowledge.edit", "time_entry.edit")}
	milestones := projects.NewMilestoneService(psa.NewProjectRepositoryFromPool(pool), func() time.Time { return now }, id.New)
	milestoneStart, milestoneEnd := now, now.Add(time.Hour)
	milestone, err := milestones.Create(ctx, projects.CreateMilestoneCommand{Principal: clientEditor, ProjectID: projects.ProjectID(project), PhaseID: projects.PhaseID(phase), Name: "Cutover", DueOn: now, StartsAt: &milestoneStart, EndsAt: &milestoneEnd, Timezone: "UTC", Recurrence: &calendar.RecurrenceRule{Frequency: calendar.Daily, Interval: 1, Count: 2}, OwnerID: owner, ActorID: actor, Source: "integration", IdempotencyKey: "milestone-create"})
	if err != nil {
		t.Fatalf("milestone: %v", err)
	}
	milestone.Name = "Cutover updated"
	milestone, err = milestones.Update(ctx, projects.UpdateMilestoneCommand{Principal: clientEditor, Milestone: milestone, ExpectedVersion: 1, ActorID: actor, Source: "integration", IdempotencyKey: "milestone-update"})
	if err != nil {
		t.Fatalf("milestone update: %v", err)
	}
	if _, err = milestones.Transition(ctx, projects.TransitionMilestoneCommand{Principal: clientEditor, MilestoneID: milestone.ID, ExpectedVersion: 2, ToStatus: "in_progress", ActorID: actor, Source: "integration", IdempotencyKey: "milestone-transition"}); err != nil {
		t.Fatalf("milestone transition: %v", err)
	}
	schedules := workforce.NewScheduleService(psa.NewWorkforceRepositoryFromPool(pool), func() time.Time { return now }, id.New)
	if _, err = schedules.Publish(ctx, workforce.PublishScheduleCommand{Principal: admin, TechnicianID: owner, Timezone: "America/Chicago", EffectiveFrom: now, Windows: []workforce.WeeklyWindow{{Weekday: time.Monday, StartsMinute: 540, EndsMinute: 1440}}, Exceptions: []workforce.ScheduleException{{ExceptionOn: now.AddDate(0, 0, 1), State: workforce.Available, StartsMinute: 600, EndsMinute: 1440}}, ActorID: actor, Source: "integration", IdempotencyKey: "schedule-publish"}); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if _, err = schedules.Publish(ctx, workforce.PublishScheduleCommand{Principal: admin, TechnicianID: owner, Timezone: "America/Chicago", EffectiveFrom: now.AddDate(0, 0, 2), ExpectedVersion: 1, Windows: []workforce.WeeklyWindow{{Weekday: time.Tuesday, StartsMinute: 480, EndsMinute: 1440}}, ActorID: actor, Source: "integration", IdempotencyKey: "schedule-update"}); err != nil {
		t.Fatalf("schedule update: %v", err)
	}
	ptoPrincipal := authorization.Principal{ID: owner, Scope: scope.Principal{MSPID: msp}}
	pto := workforce.NewPTOService(psa.NewWorkforceRepositoryFromPool(pool), func() time.Time { return now }, id.New)
	start, end := now, now.Add(8*time.Hour)
	requested, err := pto.Request(ctx, workforce.RequestPTOCommand{Principal: ptoPrincipal, TechnicianID: owner, PTOType: "vacation", StartsAt: &start, EndsAt: &end, Timezone: "UTC", ActorID: owner, Source: "integration", IdempotencyKey: "pto-request"})
	if err != nil {
		t.Fatalf("pto request: %v", err)
	}
	var initialPTOFacts int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE event_type='calendar.schedule_changed' AND msp_id=$1 AND subject_id=$2 AND data->>'change_class'='pto'`, msp, requested.ID).Scan(&initialPTOFacts); err != nil || initialPTOFacts != 0 {
		t.Fatalf("initial PTO request facts=%d err=%v", initialPTOFacts, err)
	}
	approved, err := pto.Decide(ctx, workforce.DecidePTOCommand{Principal: admin, RequestID: requested.ID, Decision: workforce.Approved, ActorID: actor, ExpectedVersion: 1, Source: "integration", IdempotencyKey: "pto-decision"})
	if err != nil {
		t.Fatalf("pto approval: %v", err)
	}
	assertCanonicalPTO := func(requestID, forbidden string) {
		t.Helper()
		var facts int
		var canonical, leaked bool
		queryErr := pool.QueryRow(ctx, `SELECT count(*),COALESCE(bool_and(data=jsonb_build_object('recipient_id',$3::text,'change_class','pto','urgency','routine','source_refs',jsonb_build_array(jsonb_build_object('type','pto','id',$2::text,'client_id','','event_role','unavailability','source_revision',2)),'action_path','/calendar')),false),COALESCE(bool_or(data ? 'decision_reason' OR data ? 'pto_type' OR data::text LIKE '%' || $4 || '%'),false) FROM event_outbox WHERE event_type='calendar.schedule_changed' AND msp_id=$1 AND subject_id=$2::uuid AND data->>'change_class'='pto'`, msp, requestID, owner, forbidden).Scan(&facts, &canonical, &leaked)
		if queryErr != nil || facts != 1 || !canonical || leaked {
			t.Fatalf("canonical PTO request=%s facts=%d canonical=%v leaked=%v err=%v", requestID, facts, canonical, leaked, queryErr)
		}
	}
	assertCanonicalPTO(requested.ID, "vacation")
	rejectedRequest, err := pto.Request(ctx, workforce.RequestPTOCommand{Principal: ptoPrincipal, TechnicianID: owner, PTOType: "sick", StartsAt: &start, EndsAt: &end, Timezone: "UTC", ActorID: owner, Source: "integration", IdempotencyKey: "pto-reject-request"})
	if err != nil {
		t.Fatalf("rejected PTO request: %v", err)
	}
	if _, err = pto.Decide(ctx, workforce.DecidePTOCommand{Principal: admin, RequestID: rejectedRequest.ID, Decision: workforce.Rejected, Reason: "private medical detail", ActorID: actor, ExpectedVersion: 1, Source: "integration", IdempotencyKey: "pto-reject-decision"}); err != nil {
		t.Fatalf("pto rejection: %v", err)
	}
	assertCanonicalPTO(rejectedRequest.ID, "private medical detail")
	if _, err = pto.Cancel(ctx, workforce.CancelPTOCommand{Principal: ptoPrincipal, RequestID: requested.ID, ExpectedVersion: 2, ActorID: owner, Source: "integration", IdempotencyKey: "pto-cancel"}); err != nil {
		t.Fatalf("pto cancel: %v", err)
	}
	staleCorrelation, staleEvent, staleAudit := id.New(), id.New(), id.New()
	approved.Version = 3
	err = psa.NewWorkforceRepositoryFromPool(pool).UpdatePTOAtomic(ctx, workforce.PTOMutation{Request: approved, Audit: mutation.AuditRecord{ID: staleAudit, OccurredAt: now, MSPID: msp, ActorType: "technician", ActorID: actor, Action: "pto.approved", SubjectType: "pto_request", SubjectID: approved.ID, SubjectVersion: 3, Source: "integration", CorrelationID: staleCorrelation}, Event: mutation.EventRecord{EventID: staleEvent, EventType: "pto.approved", SchemaVersion: 1, OccurredAt: now, MSPID: msp, ActorType: "technician", ActorID: actor, SubjectType: "pto_request", SubjectID: approved.ID, SubjectVersion: 3, Source: "integration", CorrelationID: staleCorrelation}, Authority: "decision", IdempotencyKey: "pto-stale", RequestFingerprint: "stale", RequestID: id.New()}, 1)
	if !errors.Is(err, workforce.ErrWorkforceVersionConflict) {
		t.Fatalf("stale PTO repository update=%v", err)
	}
	var staleFacts int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM audit_ledger WHERE id=$1)+(SELECT count(*) FROM event_outbox WHERE event_id=$2)`, staleAudit, staleEvent).Scan(&staleFacts); err != nil || staleFacts != 0 {
		t.Fatalf("stale facts=%d err=%v", staleFacts, err)
	}
	revokedPTO, revokedAudit, revokedEvent := id.New(), id.New(), id.New()
	exec(`INSERT INTO pto_requests(id,msp_id,technician_id,starts_at,ends_at,timezone,all_day,pto_type,state,manager_id,version,created_at,created_by,updated_at,updated_by) VALUES($1,$2,$3,$4,$5,'UTC',false,'vacation','requested',$6,1,$4,$3,$4,$3)`, revokedPTO, msp, owner, now, now.Add(time.Hour), actor)
	exec(`UPDATE team_memberships SET lifecycle_state='inactive',version=version+1 WHERE team_id=$1 AND technician_id=$2`, team, actor)
	revokedRequest := workforce.PTORequest{ID: revokedPTO, MSPID: msp, TechnicianID: owner, PTOType: "vacation", ManagerID: actor, StartsAt: &start, EndsAt: &end, Timezone: "UTC", State: workforce.Approved, Version: 2, CreatedAt: now, UpdatedAt: now, CreatedBy: owner, UpdatedBy: actor, DecidedBy: actor}
	revokedCorrelation := id.New()
	err = psa.NewWorkforceRepositoryFromPool(pool).UpdatePTOAtomic(ctx, workforce.PTOMutation{Request: revokedRequest, Audit: mutation.AuditRecord{ID: revokedAudit, OccurredAt: now, MSPID: msp, ActorType: "technician", ActorID: actor, Action: "pto.approved", SubjectType: "pto_request", SubjectID: revokedPTO, SubjectVersion: 2, Source: "integration", CorrelationID: revokedCorrelation}, Event: mutation.EventRecord{EventID: revokedEvent, EventType: "pto.approved", SchemaVersion: 1, OccurredAt: now, MSPID: msp, ActorType: "technician", ActorID: actor, SubjectType: "pto_request", SubjectID: revokedPTO, SubjectVersion: 2, Source: "integration", CorrelationID: revokedCorrelation}, Authority: "decision", IdempotencyKey: "pto-revoked", RequestFingerprint: "revoked", RequestID: id.New()}, 1)
	if !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("revoked manager repository decision=%v", err)
	}
	var revokedFacts int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM audit_ledger WHERE id=$1)+(SELECT count(*) FROM event_outbox WHERE event_id=$2)`, revokedAudit, revokedEvent).Scan(&revokedFacts); err != nil || revokedFacts != 0 {
		t.Fatalf("revoked manager facts=%d err=%v", revokedFacts, err)
	}
	exec(`UPDATE team_memberships SET lifecycle_state='active',version=version+1 WHERE team_id=$1 AND technician_id=$2`, team, actor)
	maintenance := commitments.NewMaintenanceService(psa.NewCommitmentRepositoryFromPool(pool), func() time.Time { return now }, id.New)
	window, err := maintenance.Create(ctx, commitments.CreateMaintenanceCommand{Principal: admin, Title: "Core upgrade", StartsAt: now, EndsAt: now.Add(2 * time.Hour), Timezone: "UTC", Recurrence: &calendar.RecurrenceRule{Frequency: calendar.Daily, Interval: 1, Count: 2}, Protected: true, ConflictPolicy: commitments.PolicyHardBlock, Scopes: []commitments.ScopeRef{{Type: commitments.ScopeService, ID: serviceID}, {Type: commitments.ScopeAsset, ID: assetID}}, ActorID: actor, Source: "integration", IdempotencyKey: "maintenance-create"})
	if err != nil {
		t.Fatalf("maintenance: %v", err)
	}
	window, err = maintenance.Update(ctx, commitments.UpdateMaintenanceCommand{Principal: admin, WindowID: window.ID, ExpectedVersion: 1, Title: "Core upgrade updated", StartsAt: now, EndsAt: now.Add(3 * time.Hour), Timezone: "UTC", Recurrence: &calendar.RecurrenceRule{Frequency: calendar.Weekly, Interval: 1, Weekdays: []time.Weekday{time.Monday}, Count: 2}, Protected: true, ConflictPolicy: commitments.PolicyHardBlock, Scopes: []commitments.ScopeRef{{Type: commitments.ScopeService, ID: serviceID}, {Type: commitments.ScopeAsset, ID: assetID}}, ActorID: actor, Source: "integration", IdempotencyKey: "maintenance-update"})
	if err != nil {
		t.Fatalf("maintenance update: %v", err)
	}
	scopeSnapshot := func(windowID string) string {
		t.Helper()
		var snapshot string
		if e := pool.QueryRow(ctx, `SELECT jsonb_build_object('policy',mw.conflict_policy,'protected',mw.protected,'scopes',COALESCE(jsonb_agg(to_jsonb(scope) ORDER BY scope.id),'[]'::jsonb))::text FROM maintenance_windows mw LEFT JOIN maintenance_window_scopes scope ON scope.maintenance_window_id=mw.id AND scope.msp_id=mw.msp_id WHERE mw.id=$1 GROUP BY mw.id`, windowID).Scan(&snapshot); e != nil {
			t.Fatal(e)
		}
		return snapshot
	}
	originalScopes := scopeSnapshot(window.ID)
	window, err = maintenance.Transition(ctx, commitments.TransitionMaintenanceCommand{Principal: admin, WindowID: window.ID, ExpectedVersion: 2, ToStatus: "active", ActorID: actor, Source: "integration", IdempotencyKey: "maintenance-transition"})
	if err != nil {
		t.Fatalf("maintenance transition: %v", err)
	}
	if got := scopeSnapshot(window.ID); got != originalScopes {
		t.Fatalf("maintenance scopes changed on activate\nbefore=%s\nafter=%s", originalScopes, got)
	}
	if _, err = maintenance.Transition(ctx, commitments.TransitionMaintenanceCommand{Principal: admin, WindowID: window.ID, ExpectedVersion: 3, ToStatus: "completed", ActorID: actor, Source: "integration", IdempotencyKey: "maintenance-complete"}); err != nil {
		t.Fatalf("maintenance complete: %v", err)
	}
	if got := scopeSnapshot(window.ID); got != originalScopes {
		t.Fatalf("maintenance scopes changed on complete\nbefore=%s\nafter=%s", originalScopes, got)
	}
	cancelWindow, err := maintenance.Create(ctx, commitments.CreateMaintenanceCommand{Principal: admin, Title: "Cancelled window", StartsAt: now, EndsAt: now.Add(time.Hour), Timezone: "UTC", Protected: true, ConflictPolicy: commitments.PolicyHardBlock, Scopes: []commitments.ScopeRef{{Type: commitments.ScopeClient, ID: client}}, ActorID: actor, Source: "integration", IdempotencyKey: "maintenance-cancel-create"})
	if err != nil {
		t.Fatalf("cancel window create: %v", err)
	}
	cancelScopes := scopeSnapshot(cancelWindow.ID)
	if _, err = maintenance.Transition(ctx, commitments.TransitionMaintenanceCommand{Principal: admin, WindowID: cancelWindow.ID, ExpectedVersion: 1, ToStatus: "cancelled", ActorID: actor, Source: "integration", IdempotencyKey: "maintenance-cancel"}); err != nil {
		t.Fatalf("maintenance cancel: %v", err)
	}
	if got := scopeSnapshot(cancelWindow.ID); got != cancelScopes {
		t.Fatalf("maintenance scopes changed on cancel\nbefore=%s\nafter=%s", cancelScopes, got)
	}
	commercial := commitments.NewCommercialService(psa.NewCommitmentRepositoryFromPool(pool), func() time.Time { return now }, id.New)
	commercialCommand := commitments.CreateCommercialCommand{Principal: admin, ClientID: client, Type: commitments.License, Title: "M365", Vendor: "Microsoft", OwnerID: owner, EffectiveOn: now, ExpirationOn: now.AddDate(1, 0, 0), QuantityUnits: 9_000_000_000_000_001, HasCost: true, CostMinor: 120000, Currency: "USD", Recurrence: &calendar.RecurrenceRule{Frequency: calendar.Monthly, Interval: 1, Count: 2}, ServiceID: serviceID, ContractID: contractID, ActorID: actor, Source: "integration", IdempotencyKey: "commercial-create"}
	commercialRecord, err := commercial.Create(ctx, commercialCommand)
	if err != nil {
		t.Fatalf("commercial: %v", err)
	}
	replayedCommercial, err := commercial.Create(ctx, commercialCommand)
	if err != nil || replayedCommercial.ID != commercialRecord.ID {
		t.Fatalf("commercial replay=%+v original=%+v err=%v", replayedCommercial, commercialRecord, err)
	}
	mismatch := commercialCommand
	mismatch.Title = "Different payload"
	if _, err = commercial.Create(ctx, mismatch); !errors.Is(err, mutation.ErrIdempotencyConflict) {
		t.Fatalf("commercial idempotency mismatch=%v", err)
	}
	commercialUpdate := commitments.UpdateCommercialCommand{Principal: admin, CommitmentID: commercialRecord.ID, ClientID: client, ExpectedVersion: 1, Type: commitments.License, Title: "M365 updated", Vendor: "Microsoft", OwnerID: owner, EffectiveOn: now, ExpirationOn: now.AddDate(1, 0, 0), QuantityUnits: 9_000_000_000_000_002, HasCost: true, CostMinor: 120001, Currency: "USD", Recurrence: &calendar.RecurrenceRule{Frequency: calendar.Yearly, Interval: 1, Count: 2}, ServiceID: serviceID, ContractID: contractID, ActorID: actor, Source: "integration", IdempotencyKey: "commercial-update"}
	commercialRecord, err = commercial.Update(ctx, commercialUpdate)
	if err != nil {
		t.Fatalf("commercial update: %v", err)
	}
	commercialReplay, err := commercial.Update(ctx, commercialUpdate)
	if err != nil || commercialReplay.ID != commercialRecord.ID || commercialReplay.Version != commercialRecord.Version {
		t.Fatalf("commercial update replay=%+v original=%+v err=%v", commercialReplay, commercialRecord, err)
	}
	commercialUpdate.Title = "mismatched update"
	if _, err = commercial.Update(ctx, commercialUpdate); !errors.Is(err, mutation.ErrIdempotencyConflict) {
		t.Fatalf("commercial update mismatch=%v", err)
	}
	if _, err = commercial.Transition(ctx, commitments.TransitionCommercialCommand{Principal: admin, CommitmentID: commercialRecord.ID, ClientID: client, ExpectedVersion: 2, ToStatus: "renewed", ActorID: actor, Source: "integration", IdempotencyKey: "commercial-transition"}); err != nil {
		t.Fatalf("commercial transition: %v", err)
	}
	badOwner := id.New()
	if _, err = commercial.Create(ctx, commitments.CreateCommercialCommand{Principal: admin, ClientID: client, Type: commitments.Renewal, Title: "Invalid owner", Vendor: "Vendor", OwnerID: badOwner, EffectiveOn: now, ExpirationOn: now.AddDate(1, 0, 0), ActorID: actor, Source: "integration", IdempotencyKey: "commercial-bad-owner"}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("bad-owner commercial=%v", err)
	}
	var badRows int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM commercial_commitments WHERE owner_id=$1`, badOwner).Scan(&badRows); err != nil || badRows != 0 {
		t.Fatalf("rolled-back commercial rows=%d err=%v", badRows, err)
	}
	taskID, articleID, timeEntryID := id.New(), id.New(), id.New()
	exec(`INSERT INTO tasks(id,msp_id,client_id,parent_type,parent_id,work_record_id,title,status,position,owner_id,created_by,updated_by) VALUES($1,$2,$3,'work_record',$4,$4,'calendar task','new',1,$5,$5,$5)`, taskID, msp, client, work, actor)
	exec(`INSERT INTO knowledge_articles(id,msp_id,client_id,display_id,title,state,created_by,updated_by) VALUES($1,$2,$3,$4,'calendar article','draft',$5,$5)`, articleID, msp, client, "KA-"+articleID, actor)
	exec(`INSERT INTO time_entries(id,msp_id,client_id,work_record_id,task_id,technician_id,started_at,ended_at,duration_seconds,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,3600,$6)`, timeEntryID, msp, client, work, taskID, actor, now, now.Add(time.Hour))
	field := id.New()
	exec(`INSERT INTO calendar_custom_date_fields(id,msp_id,object_type,internal_key,label,value_kind,event_role,category,color_category,created_by,updated_by) VALUES($1,$2,'work_record','follow_up','Follow up','date','follow_up','operations','blue',$3,$3)`, field, msp, actor)
	readRole := id.New()
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,'calendar-source-reader','Source reader')`, readRole, msp)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,'work_record.read')`, readRole, msp)
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$5,$4)`, id.New(), msp, client, actor, readRole)
	calendarQueries := psa.NewCalendarRepositoryFromPool(pool)
	definitions, readErr := calendarQueries.ListObjectCustomDateFields(ctx, clientEditor, customfields.ObjectWorkRecord, work)
	if readErr != nil || len(definitions) != 1 || definitions[0].ID != field || definitions[0].TimezoneSource != "" {
		t.Fatalf("source definitions=%+v err=%v", definitions, readErr)
	}
	crossClientReader := clientEditor
	crossClientReader.Scope.ClientID = ""
	if _, readErr = calendarQueries.ListObjectCustomDateFields(ctx, crossClientReader, customfields.ObjectWorkRecord, foreignWork); !errors.Is(readErr, scope.ErrNotFound) {
		t.Fatalf("cross-client definition read=%v", readErr)
	}
	dates := customfields.NewDateService(psa.NewCustomDateRepositoryFromPool(pool), func() time.Time { return now }, id.New)
	due := now.AddDate(0, 0, 2)
	createdDate, err := dates.Set(ctx, customfields.SetDateCommand{Principal: clientEditor, ObjectType: customfields.ObjectWorkRecord, ObjectID: work, FieldID: field, DateValue: &due, ActorID: actor, Source: "integration", IdempotencyKey: "custom-date-create"})
	if err != nil {
		t.Fatalf("custom date: %v", err)
	}
	later := due.AddDate(0, 0, 1)
	updatedDate, err := dates.Set(ctx, customfields.SetDateCommand{Principal: clientEditor, ObjectType: customfields.ObjectWorkRecord, ObjectID: work, FieldID: field, DateValue: &later, ExpectedVersion: 1, ActorID: actor, Source: "integration", IdempotencyKey: "custom-date-update"})
	if err != nil || updatedDate.ID != createdDate.ID || updatedDate.Version != 2 {
		t.Fatalf("custom date update=%+v err=%v", updatedDate, err)
	}
	replayedDate, err := dates.Set(ctx, customfields.SetDateCommand{Principal: clientEditor, ObjectType: customfields.ObjectWorkRecord, ObjectID: work, FieldID: field, DateValue: &later, ExpectedVersion: 1, ActorID: actor, Source: "integration", IdempotencyKey: "custom-date-update"})
	if err != nil || replayedDate.ID != updatedDate.ID || replayedDate.Version != updatedDate.Version {
		t.Fatalf("custom date replay=%+v original=%+v err=%v", replayedDate, updatedDate, err)
	}
	mismatchedDate := later.AddDate(0, 0, 1)
	if _, err = dates.Set(ctx, customfields.SetDateCommand{Principal: clientEditor, ObjectType: customfields.ObjectWorkRecord, ObjectID: work, FieldID: field, DateValue: &mismatchedDate, ExpectedVersion: 1, ActorID: actor, Source: "integration", IdempotencyKey: "custom-date-update"}); !errors.Is(err, mutation.ErrIdempotencyConflict) {
		t.Fatalf("custom date mismatch=%v", err)
	}
	if _, err = dates.Set(ctx, customfields.SetDateCommand{Principal: clientEditor, ObjectType: customfields.ObjectWorkRecord, ObjectID: work, FieldID: field, DateValue: &later, ActorID: actor, Source: "integration", IdempotencyKey: "custom-date-competing-create"}); !errors.Is(err, customfields.ErrCustomDateVersionConflict) {
		t.Fatalf("custom date competing create=%v", err)
	}
	for index, source := range []struct {
		objectType customfields.ObjectType
		objectID   string
	}{
		{customfields.ObjectTask, taskID},
		{customfields.ObjectProject, project},
		{customfields.ObjectAsset, assetID},
		{customfields.ObjectKnowledgeArticle, articleID},
		{customfields.ObjectTimeEntry, timeEntryID},
	} {
		typedField := id.New()
		exec(`INSERT INTO calendar_custom_date_fields(id,msp_id,object_type,internal_key,label,value_kind,event_role,category,color_category,created_by,updated_by) VALUES($1,$2,$3,$4,'Typed date','date','follow_up','operations','blue',$5,$5)`, typedField, msp, source.objectType, fmt.Sprintf("typed_%d", index), actor)
		if _, err = dates.Set(ctx, customfields.SetDateCommand{Principal: clientEditor, ObjectType: source.objectType, ObjectID: source.objectID, FieldID: typedField, DateValue: &due, ActorID: actor, Source: "integration", IdempotencyKey: fmt.Sprintf("custom-date-type-%d", index)}); err != nil {
			t.Fatalf("custom date source %s: %v", source.objectType, err)
		}
	}
	if _, err = dates.Set(ctx, customfields.SetDateCommand{Principal: clientEditor, ObjectType: customfields.ObjectWorkRecord, ObjectID: foreignWork, FieldID: field, DateValue: &due, ActorID: actor, Source: "integration", IdempotencyKey: "custom-date-foreign"}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client custom date=%v", err)
	}
	var audits, events int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_ledger WHERE msp_id=$1 AND source='integration'`, msp).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE msp_id=$1 AND source='integration'`, msp).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if audits != 26 || events != 26 {
		t.Fatalf("correlated facts audits=%d events=%d", audits, events)
	}
}
