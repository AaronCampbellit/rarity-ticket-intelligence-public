package psa_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/automation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/httpapi"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

// TestGovernedClassificationAcceptanceAcrossTwoClients composes the same
// services, workers, HTTP handler and PostgreSQL repositories used in
// production. SQL below is deliberately limited to immutable tenant,
// authorization, sales-reference and AI-provider setup plus final evidence.
func TestGovernedClassificationAcceptanceAcrossTwoClients(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for governed classification acceptance")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)

	mspID, clientA, clientB, actorID := id.New(), id.New(), id.New(), id.New()
	displayID := "TAG-ACCEPTANCE-12-" + mspID[:8]
	execFixture := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("immutable acceptance fixture: %v", err)
		}
	}
	execFixture(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'classification acceptance',$3,$3)`, mspID, displayID, actorID)
	for _, clientID := range []string{clientA, clientB} {
		execFixture(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,$3,$4,$4)`, clientID, mspID, "CLIENT-"+clientID, actorID)
	}
	execFixture(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,$3,'Acceptance technician')`, actorID, mspID, actorID+"@example.test")
	roleID := id.New()
	execFixture(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,$3,'Acceptance role')`, roleID, mspID, "acceptance-"+roleID)
	for _, capability := range []string{"ai.assist", "classification.apply"} {
		execFixture(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,$3)`, roleID, mspID, capability)
	}
	for _, clientID := range []string{clientA, clientB} {
		execFixture(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$5,$4)`, id.New(), mspID, clientID, actorID, roleID)
	}
	proposalA := insertAcceptanceProposal(t, ctx, pool, mspID, clientA, actorID, "A")
	proposalB := insertAcceptanceProposal(t, ctx, pool, mspID, clientB, actorID, "B")

	capabilities := authorization.NewCapabilitySet(
		"classification.manage", "classification.apply", "classification.report",
		"classification.ai.manage", "project.create", "project.read", "task.create",
		"automation.manage", "work_record.create",
	)
	principalA := authorization.Principal{ID: actorID, Scope: scope.Principal{MSPID: mspID, ClientID: clientA}, Capabilities: capabilities}
	principalB := authorization.Principal{ID: actorID, Scope: scope.Principal{MSPID: mspID, ClientID: clientB}, Capabilities: capabilities}
	catalogPrincipal := authorization.Principal{ID: actorID, Scope: scope.Principal{MSPID: mspID}, Capabilities: capabilities}

	tagRepository := psa.NewTaggingRepositoryFromPool(pool)
	catalog := tagging.NewCatalogService(tagRepository, time.Now, id.New)
	unclassified, err := catalog.EnsureSystemCatalog(ctx, mspID, actorID)
	if err != nil {
		t.Fatalf("ensure system catalog: %v", err)
	}
	group, err := catalog.CreateGroup(ctx, tagging.CreateGroupCommand{Principal: catalogPrincipal, Label: "Technology", Position: 2})
	if err != nil {
		t.Fatalf("create catalog group: %v", err)
	}
	createTag := func(label string) tagging.Tag {
		t.Helper()
		tag, createErr := catalog.CreateTag(ctx, tagging.CreateTagCommand{Principal: catalogPrincipal, GroupID: group.ID, Label: label})
		if createErr != nil {
			t.Fatalf("create %s tag: %v", label, createErr)
		}
		return tag
	}
	network, security, automationTag := createTag("Network"), createTag("Security"), createTag("Automated")
	mergeCandidate, archiveCandidate := createTag("Network legacy"), createTag("Archive me")
	listed, err := catalog.List(ctx, tagging.ListCatalogCommand{Principal: principalB})
	if err != nil || len(listed.Groups) < 2 {
		t.Fatalf("catalog through second client principal: groups=%d error=%v", len(listed.Groups), err)
	}

	creation := tagging.NewCreationPreparer(tagRepository)
	projectService := projects.NewService(psa.NewProjectRepositoryFromPool(pool), time.Now, id.New, creation)
	projectA, err := projectService.Create(ctx, projects.CreateCommand{
		Principal: principalA, Target: scope.Target{MSPID: mspID, ClientID: clientA},
		DisplayID: "PRJ-A-" + mspID[:8], Name: "VPN rollout", OriginalProposalVersionID: proposalA,
		Phases: []projects.PhaseInput{{Name: "Delivery"}}, ActorID: actorID, Source: "api",
		TagIDs: []string{network.ID, mergeCandidate.ID}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("interactive project create: %v", err)
	}
	projectB, err := projectService.Create(ctx, projects.CreateCommand{
		Principal: principalB, Target: scope.Target{MSPID: mspID, ClientID: clientB},
		DisplayID: "PRJ-B-" + mspID[:8], Name: "Firewall rollout", OriginalProposalVersionID: proposalB,
		Phases: []projects.PhaseInput{{Name: "Delivery"}}, ActorID: actorID, Source: "api",
		TagIDs: []string{security.ID, archiveCandidate.ID}, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("second-client project create: %v", err)
	}
	taskService := tasks.NewService(psa.NewTaskRepositoryFromPool(pool), time.Now, id.New, creation)
	taskA, err := taskService.Create(ctx, tasks.CreateCommand{
		Principal: principalA, Target: scope.Target{MSPID: mspID, ClientID: clientA},
		Parent: tasks.Ref{Type: tasks.ParentProject, ID: string(projectA.ID)}, Title: "Inherited network task",
		ActorID: actorID, Source: "api", ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		t.Fatalf("task create with project inheritance: %v", err)
	}
	associations := tagging.NewAssociationService(tagRepository)
	deniedPrincipal := authorization.Principal{ID: actorID, Scope: principalA.Scope, Capabilities: authorization.NewCapabilitySet()}
	projectATarget := tagging.TargetRef{MSPID: mspID, ClientID: clientA, ObjectType: tagging.ObjectProject, ObjectID: string(projectA.ID)}
	if _, err := associations.Get(ctx, tagging.GetCommand{Principal: deniedPrincipal, Target: projectATarget}); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("same-client association without classification.apply error=%v, want forbidden", err)
	}
	projectBTarget := tagging.TargetRef{MSPID: mspID, ClientID: clientB, ObjectType: tagging.ObjectProject, ObjectID: string(projectB.ID)}
	if _, err := associations.Get(ctx, tagging.GetCommand{Principal: principalA, Target: projectBTarget}); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("cross-client association read error=%v, want scope-hidden not found", err)
	}
	taskTags, err := associations.Get(ctx, tagging.GetCommand{Principal: principalA, Target: tagging.TargetRef{MSPID: mspID, ClientID: clientA, ObjectType: tagging.ObjectTask, ObjectID: taskA.ID}})
	if err != nil || len(taskTags.Direct) != 0 || !containsAssignment(taskTags.Inherited, network.ID) {
		t.Fatalf("derived task inheritance direct=%d inherited=%+v error=%v", len(taskTags.Direct), taskTags.Inherited, err)
	}

	insertAcceptanceWorkRecordConfiguration(t, ctx, pool, mspID, actorID)
	workRecordService := workrecords.NewService(
		psa.NewWorkRecordRepositoryFromPool(pool), psa.NewRoutingRepositoryFromPool(pool),
		psa.NewWorkflowRepositoryFromPool(pool), psa.NewSLARepositoryFromPool(pool),
		time.Now, id.New, creation,
	)
	fallbackRecord, err := workRecordService.CreateDattoIncident(ctx, workrecords.CreateCommand{
		Principal: principalA, Target: scope.Target{MSPID: mspID, ClientID: clientA},
		Actor: workrecords.Actor{ID: actorID}, DisplayID: "DATTO-" + mspID[:8],
		Type: workrecords.Incident, Title: "Trusted automated alert", Status: "new", Priority: "normal",
	})
	if err != nil {
		t.Fatalf("trusted Datto fallback creation: %v", err)
	}
	fallbackTarget := tagging.TargetRef{MSPID: mspID, ClientID: clientA, ObjectType: tagging.ObjectWorkRecord, ObjectID: fallbackRecord.ID}
	fallbackTags, err := associations.Get(ctx, tagging.GetCommand{Principal: principalA, Target: fallbackTarget})
	if err != nil || len(fallbackTags.Direct) != 1 || !containsAssignmentWithSource(fallbackTags.Direct, unclassified.ID, tagging.SourceSystemFallback) {
		t.Fatalf("trusted fallback direct assignments=%+v error=%v", fallbackTags.Direct, err)
	}
	var fallbackSource, fallbackActorType string
	var fallbackAssignedBy, fallbackEventActorID *string
	var initialEvidence bool
	if err := pool.QueryRow(ctx, `
SELECT assignment.assignment_source, assignment.assigned_by::text,
       event.actor_type, event.actor_id::text,
       COALESCE((event.evidence->>'initial_assignment')::boolean, false)
FROM object_tag_assignments assignment
JOIN tag_assignment_events event ON event.assignment_id=assignment.id AND event.operation='added'
WHERE assignment.msp_id=$1 AND assignment.object_type='work_record'
  AND assignment.object_id=$2 AND assignment.tag_id=$3`,
		mspID, fallbackRecord.ID, unclassified.ID,
	).Scan(&fallbackSource, &fallbackAssignedBy, &fallbackActorType, &fallbackEventActorID, &initialEvidence); err != nil ||
		fallbackSource != tagging.SourceSystemFallback.String() || fallbackAssignedBy != nil ||
		fallbackActorType != "system" || fallbackEventActorID != nil || !initialEvidence {
		t.Fatalf("fallback provenance source=%q assigned_by=%v actor=%q event_actor=%v initial=%t error=%v", fallbackSource, fallbackAssignedBy, fallbackActorType, fallbackEventActorID, initialEvidence, err)
	}

	configureAndApplyAI(t, ctx, pool, mspID, clientA, actorID, principalA, projectA, security.ID, tagRepository, associations)
	runAutomation(t, ctx, pool, mspID, clientB, projectB, automationTag.ID, principalB, associations)

	projectionRepository := psa.NewTaggingProjectionRepositoryFromPool(pool)
	if err := projectAcceptanceEvents(t, ctx, pool, mspID, projectionRepository); err != nil {
		t.Fatalf("project classification events: %v", err)
	}
	reports := tagging.NewReportService(projectionRepository)
	filter := tagging.ReportFilter{From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 200}
	reportA, err := reports.Query(ctx, principalA, tagging.ReportUsage, filter)
	if err != nil || len(reportA.Rows) == 0 {
		t.Fatalf("client A report rows=%d error=%v", len(reportA.Rows), err)
	}
	reportB, err := reports.Query(ctx, principalB, tagging.ReportUsage, filter)
	if err != nil || len(reportB.Rows) == 0 {
		t.Fatalf("client B report rows=%d error=%v", len(reportB.Rows), err)
	}
	for _, row := range reportB.Rows {
		if row.TagID == network.ID || row.TagID == mergeCandidate.ID {
			t.Fatalf("client B report leaked client A tag row: %+v", row)
		}
	}

	if _, err := catalog.PreviewImpact(ctx, tagging.ImpactCommand{Principal: catalogPrincipal, TagID: mergeCandidate.ID, Operation: tagging.ImpactMerge, ReplacementTagID: network.ID}); err != nil {
		t.Fatalf("merge preview: %v", err)
	}
	if _, err := catalog.Merge(ctx, tagging.MergeCommand{Principal: catalogPrincipal, TagID: mergeCandidate.ID, SurvivorTagID: network.ID, ExpectedVersion: mergeCandidate.Version, Reason: "canonicalize taxonomy"}); err != nil {
		t.Fatalf("catalog merge: %v", err)
	}
	if _, err := catalog.PreviewImpact(ctx, tagging.ImpactCommand{Principal: catalogPrincipal, TagID: archiveCandidate.ID, Operation: tagging.ImpactArchive, ReplacementTagID: security.ID}); err != nil {
		t.Fatalf("archive preview: %v", err)
	}
	if _, err := catalog.Archive(ctx, tagging.ArchiveCommand{Principal: catalogPrincipal, TagID: archiveCandidate.ID, ReplacementTagID: security.ID, ExpectedVersion: archiveCandidate.Version, Reason: "retire obsolete taxonomy"}); err != nil {
		t.Fatalf("catalog archive: %v", err)
	}
	projectATags, err := associations.Get(ctx, tagging.GetCommand{Principal: principalA, Target: tagging.TargetRef{MSPID: mspID, ClientID: clientA, ObjectType: tagging.ObjectProject, ObjectID: string(projectA.ID)}})
	if err != nil || !containsAssignment(projectATags.Direct, network.ID) || containsAssignment(projectATags.Direct, mergeCandidate.ID) {
		t.Fatalf("post-merge current assignments=%+v error=%v", projectATags.Direct, err)
	}
	projectBTags, err := associations.Get(ctx, tagging.GetCommand{Principal: principalB, Target: tagging.TargetRef{MSPID: mspID, ClientID: clientB, ObjectType: tagging.ObjectProject, ObjectID: string(projectB.ID)}})
	if err != nil || !containsAssignment(projectBTags.Effective, security.ID) || containsAssignment(projectBTags.Effective, archiveCandidate.ID) {
		t.Fatalf("post-archive effective assignments=%+v direct_history=%+v error=%v", projectBTags.Effective, projectBTags.Direct, err)
	}
	var historical int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_assignment_events WHERE msp_id=$1 AND tag_id=$2`, mspID, mergeCandidate.ID).Scan(&historical); err != nil || historical == 0 {
		t.Fatalf("merge retained immutable history count=%d error=%v", historical, err)
	}
	var archiveRemoved int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_assignment_events WHERE msp_id=$1 AND tag_id=$2 AND operation='removed' AND evidence->>'archived_tag_id'=$2::uuid::text`, mspID, archiveCandidate.ID).Scan(&archiveRemoved); err != nil || archiveRemoved == 0 {
		t.Fatalf("archive removed-event history count=%d error=%v", archiveRemoved, err)
	}
	beforeRetirementProjection := reportB.ProjectionAsOf
	if err := projectAcceptanceEvents(t, ctx, pool, mspID, projectionRepository); err != nil {
		t.Fatalf("project retirement assignment history: %v", err)
	}
	postRetirementReport, err := reports.Query(ctx, principalB, tagging.ReportUsage, filter)
	if err != nil || len(postRetirementReport.Rows) == 0 || postRetirementReport.ProjectionAsOf.Before(beforeRetirementProjection) {
		t.Fatalf("post-retirement report continuity rows=%d before=%s after=%s error=%v", len(postRetirementReport.Rows), beforeRetirementProjection, postRetirementReport.ProjectionAsOf, err)
	}

	execFixture(`INSERT INTO classification_migration_runs(id,msp_id,started_at,completed_at,status,category_source_present,evidence) VALUES($1,$2,now(),now(),'completed',false,'{"migration":"000080_tagging_classification","strategy":"full_cutover"}')`, id.New(), mspID)
	repositoryRoot, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "go", "run", "./backend/cmd/rarity-admin", "classification-preflight", "--msp", displayID)
	command.Dir = repositoryRoot
	command.Env = append(os.Environ(), "DATABASE_URL="+databaseURL)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("classification-preflight: %v output=%s", err, output)
	}
	var preflight struct {
		Passed         bool                    `json:"passed"`
		MSPDisplayID   string                  `json:"msp_display_id"`
		Totals         struct{ Invalid int64 } `json:"totals"`
		VerifiedNoOpAt time.Time               `json:"verified_no_op_at"`
	}
	if err := json.Unmarshal(output, &preflight); err != nil || !preflight.Passed || preflight.MSPDisplayID != displayID || preflight.Totals.Invalid != 0 || preflight.VerifiedNoOpAt.IsZero() {
		t.Fatalf("preflight output=%s decoded=%+v error=%v", output, preflight, err)
	}
	if unclassified.ID == "" {
		t.Fatal("system catalog did not return the governed fallback tag")
	}
}

func projectAcceptanceEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mspID string, repository *psa.TaggingProjectionRepository) error {
	t.Helper()
	worker := tagging.NewProjectionWorker(repository)
	for attempt := 0; attempt < 100; attempt++ {
		if _, err := worker.RunOnce(ctx, 500); err != nil {
			return err
		}
		var pending int
		if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM tag_assignment_events event
JOIN tag_projection_cursors cursor
  ON cursor.msp_id=event.msp_id AND cursor.projection_name='tagging-daily-v1'
WHERE event.msp_id=$1
  AND (cursor.last_occurred_at IS NULL OR event.occurred_at > cursor.last_occurred_at
       OR (event.occurred_at = cursor.last_occurred_at AND event.id > cursor.last_event_id))`, mspID).Scan(&pending); err != nil {
			return err
		}
		if pending == 0 {
			return nil
		}
	}
	return fmt.Errorf("acceptance MSP projection did not catch up")
}

func insertAcceptanceProposal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mspID, clientID, actorID, suffix string) string {
	t.Helper()
	pipelineID, stageID, opportunityID, proposalID, versionID := id.New(), id.New(), id.New(), id.New(), id.New()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO pipelines(id,msp_id,key,name,created_by,updated_by) VALUES($1,$2,$3,$3,$4,$4)`, []any{pipelineID, mspID, "acceptance-" + suffix + "-" + pipelineID, actorID}},
		{`INSERT INTO pipeline_stages(id,pipeline_id,msp_id,key,name,position,probability,forecast_category) VALUES($1,$2,$3,'won','Won',1,100,'closed_won')`, []any{stageID, pipelineID, mspID}},
		{`INSERT INTO opportunities(id,msp_id,client_id,pipeline_id,stage_id,display_id,name,currency,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$6,'Acceptance','USD',$7,$7)`, []any{opportunityID, mspID, clientID, pipelineID, stageID, "O-" + suffix + "-" + opportunityID, actorID}},
		{`INSERT INTO proposals(id,msp_id,client_id,opportunity_id,display_id,current_version,state,created_by,updated_by) VALUES($1,$2,$3,$4,$5,1,'draft',$6,$6)`, []any{proposalID, mspID, clientID, opportunityID, "P-" + suffix + "-" + proposalID, actorID}},
		{`INSERT INTO proposal_versions(id,proposal_id,msp_id,version,currency,subtotal_minor,tax_minor,total_minor,cost_minor,margin_minor,issued_by,pdf_snapshot_id) VALUES($1,$2,$3,1,'USD',0,0,0,0,0,$4,$5)`, []any{versionID, proposalID, mspID, actorID, id.New()}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("sales reference fixture: %v", err)
		}
	}
	return versionID
}

func insertAcceptanceWorkRecordConfiguration(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mspID, actorID string) {
	t.Helper()
	routingSetID, routingRuleID, queueID := id.New(), id.New(), id.New()
	workflowID, calendarID, policyID := id.New(), id.New(), id.New()
	now := time.Now().UTC()
	weekly := `{"sunday":[{"start_minute":0,"end_minute":1440}],"monday":[{"start_minute":0,"end_minute":1440}],"tuesday":[{"start_minute":0,"end_minute":1440}],"wednesday":[{"start_minute":0,"end_minute":1440}],"thursday":[{"start_minute":0,"end_minute":1440}],"friday":[{"start_minute":0,"end_minute":1440}],"saturday":[{"start_minute":0,"end_minute":1440}]}`
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO queues(id,msp_id,key,name) VALUES($1,$2,$3,'Acceptance intake')`, []any{queueID, mspID, "acceptance-" + queueID}},
		{`INSERT INTO routing_rule_sets(id,msp_id,current_version,created_at,created_by,updated_at,updated_by) VALUES($1,$2,1,$3,$4,$3,$4)`, []any{routingSetID, mspID, now, actorID}},
		{`INSERT INTO routing_rule_set_versions(rule_set_id,msp_id,version,published_at,published_by) VALUES($1,$2,1,$3,$4)`, []any{routingSetID, mspID, now, actorID}},
		{`INSERT INTO routing_rule_versions(rule_set_id,msp_id,version,rule_id,position,queue_id) VALUES($1,$2,1,$3,1,$4)`, []any{routingSetID, mspID, routingRuleID, queueID}},
		{`INSERT INTO workflows(id,msp_id,key,name,enabled,priority,stable_order,fallback,current_version,created_at,created_by,updated_at,updated_by) VALUES($1,$2,$3,'Acceptance intake',true,0,0,true,1,$4,$5,$4,$5)`, []any{workflowID, mspID, "acceptance-" + workflowID, now, actorID}},
		{`INSERT INTO workflow_versions(workflow_id,msp_id,version,definition,published_at,published_by) VALUES($1,$2,1,'{"states":[{"key":"new"}],"transitions":[]}',$3,$4)`, []any{workflowID, mspID, now, actorID}},
		{`INSERT INTO business_calendars(id,msp_id,key,name,timezone,weekly_schedule) VALUES($1,$2,$3,'Acceptance 24x7','UTC',$4::jsonb)`, []any{calendarID, mspID, "acceptance-" + calendarID, weekly}},
		{`INSERT INTO business_calendar_versions(calendar_id,msp_id,version,timezone,weekly_schedule,holidays,published_at,published_by) VALUES($1,$2,1,'UTC',$3::jsonb,'[]',$4,$5)`, []any{calendarID, mspID, weekly, now, actorID}},
		{`INSERT INTO sla_policies(id,msp_id,key,name,calendar_id,response_target_seconds,resolution_target_seconds) VALUES($1,$2,$3,'Acceptance SLA',$4,60,120)`, []any{policyID, mspID, "acceptance-" + policyID, calendarID}},
		{`INSERT INTO sla_policy_versions(policy_id,msp_id,version,calendar_id,calendar_version,conditions,response_target_seconds,resolution_target_seconds,warning_percent,pause_states,enabled,priority,stable_order,fallback,published_at,published_by) VALUES($1,$2,1,$3,1,'{}',60,120,80,'[]',true,0,0,true,$4,$5)`, []any{policyID, mspID, calendarID, now, actorID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("work-record acceptance configuration: %v", err)
		}
	}
}

type acceptanceClassificationTransport struct{ tagID string }

func (transport acceptanceClassificationTransport) Do(_ context.Context, _ aiassist.ProviderConnection, request aiassist.HTTPRequest) (aiassist.HTTPResponse, error) {
	if request.Method != http.MethodPost {
		return aiassist.HTTPResponse{}, fmt.Errorf("unexpected provider method %s", request.Method)
	}
	content := fmt.Sprintf(`{"suggestions":[{"tag_id":%q,"confidence":0.990,"rationale":"security classification"}]}`, transport.tagID)
	body := fmt.Sprintf(`{"message":{"content":%q},"prompt_eval_count":10,"eval_count":5}`, content)
	return aiassist.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(body)}, nil
}

func configureAndApplyAI(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mspID, clientID, actorID string, principal authorization.Principal, project projects.Project, aiTagID string, repository *psa.TaggingRepository, associations *tagging.AssociationService) {
	t.Helper()
	connectionID, modelID := id.New(), id.New()
	exec := func(query string, args ...any) {
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("AI boundary fixture: %v", err)
		}
	}
	exec(`INSERT INTO ai_provider_connections(id,msp_id,name,adapter_type,network_mode,base_url,enabled,timeout_seconds,disclosure_accepted_at,disclosure_accepted_by,local_network_acknowledged_at,local_network_acknowledged_by,health_state,created_at,created_by,updated_at,updated_by) VALUES($1,$2,'local','ollama','local','http://127.0.0.1:11434',true,30,now(),$3,now(),$3,'healthy',now(),$3,now(),$3)`, connectionID, mspID, actorID)
	exec(`INSERT INTO ai_model_profiles(id,msp_id,connection_id,provider_model_id,display_name,supported_features,context_limit,output_limit,zero_cost,enabled,discovered_at,last_discovered_at,created_at,updated_at) VALUES($1,$2,$3,'classifier','Classifier',ARRAY['classification'],100000,1000,true,true,now(),now(),now(),now())`, modelID, mspID, connectionID)
	exec(`INSERT INTO ai_policies(id,msp_id,enabled,allowed_features,monthly_cost_limit_minor,classification_model_profile_id,created_by,updated_by) VALUES($1,$2,false,ARRAY[]::text[],0,NULL,$3,$3)`, id.New(), mspID, actorID)
	policyPrincipal := principal
	policyPrincipal.Scope.ClientID = ""
	policy := tagging.NewClassificationService(repository, associations, id.New)
	router := httpapi.NewRouter(httpapi.Dependencies{Principal: func(*http.Request) (authorization.Principal, error) { return policyPrincipal, nil }, TagClassification: policy})
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/classification/ai-policy", strings.NewReader(fmt.Sprintf(`{"automatic_apply_enabled":true,"automatic_apply_threshold":0.95,"model_profile_id":%q,"expected_version":1}`, modelID)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"1"`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("configure AI policy over HTTP: status=%d body=%s", response.Code, response.Body.String())
	}
	target := tagging.TargetRef{MSPID: mspID, ClientID: clientID, ObjectType: tagging.ObjectProject, ObjectID: string(project.ID)}
	suggestion, err := tagging.NewClassificationSuggestionService(repository, associations, id.New).Request(ctx, principal, target)
	if err != nil {
		t.Fatalf("request classification suggestion: %v", err)
	}
	registry, err := aiassist.NewAdapterRegistry(aiassist.NewOllamaAdapter(acceptanceClassificationTransport{tagID: aiTagID}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := aiassist.NewJobWorker(psa.NewAIJobRepositoryFromPool(pool, nil), registry, time.Now, id.New).RunOnce(ctx, 100)
	if err != nil || result.Completed < 1 {
		t.Fatalf("AI generation result=%+v error=%v", result, err)
	}
	if err := tagging.NewClassificationApplicationWorker(repository, associations, time.Now, id.New).RunOnce(ctx, 100); err != nil {
		t.Fatalf("AI automatic application: %v", err)
	}
	loaded, err := associations.Get(ctx, tagging.GetCommand{Principal: principal, Target: target})
	if err != nil || !containsAssignmentWithSource(loaded.Direct, aiTagID, tagging.SourceAIAutomatic) {
		t.Fatalf("AI suggestion %s not automatically applied: %+v error=%v", suggestion.ID, loaded.Direct, err)
	}
}

func runAutomation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, mspID, clientID string, project projects.Project, tagID string, principal authorization.Principal, associations *tagging.AssociationService) {
	t.Helper()
	management := automation.NewManagementService(psa.NewAutomationRepositoryFromPool(pool), time.Now, id.New)
	definition, err := management.CreateDefinition(ctx, automation.CreateDefinitionCommand{
		Principal: principal, Name: "Governed classification action", Trigger: automation.Trigger{EventType: "tag.added"},
		Capabilities: []string{"classification.apply"}, Steps: []automation.Step{{ID: id.New(), Kind: automation.StepAction, Action: &automation.Action{Kind: automation.ActionAddTags, Parameters: map[string]string{"tag_ids": fmt.Sprintf(`[%q]`, tagID)}}}},
	})
	if err != nil {
		t.Fatalf("create automation definition: %v", err)
	}
	definition, err = management.PublishDefinition(ctx, automation.PublishDefinitionCommand{Principal: principal, ID: definition.ID, Version: definition.Version, ExpectedRecordVersion: 1})
	if err != nil {
		t.Fatalf("publish automation definition: %v", err)
	}
	executor := automation.NewRuntimeActionExecutor(acceptanceNoopActions{}, acceptanceNoopActions{}, acceptanceNoopActions{}, acceptanceNoopActions{}, acceptanceNoopActions{}, associations)
	engine := automation.NewEngine(psa.NewAutomationRuntimeRepositoryFromPool(pool, id.New), executor, acceptanceNoopActions{}, time.Now, id.New)
	command := automation.RunCommand{Definition: definition, Event: automation.TriggerEvent{ID: id.New(), Type: "tag.added", MSPID: mspID, ClientID: clientID, InputSnapshot: map[string]string{"_subject_type": "project", "_subject_id": string(project.ID), "_subject_version": "1"}}, IdempotencyKey: "acceptance-automation:" + string(project.ID), Attempt: 1, MaxAttempts: 1}
	first, err := engine.Run(ctx, command)
	if err != nil || first.Replayed {
		t.Fatalf("first automation run=%+v error=%v", first, err)
	}
	second, err := engine.Run(ctx, command)
	if err != nil || !second.Replayed || second.RunID != first.RunID {
		t.Fatalf("idempotent replay=%+v first=%+v error=%v", second, first, err)
	}
	command.IdempotencyKey += ":loop"
	command.Event.ID = id.New()
	command.Event.Depth = automation.MaxRunDepth
	if _, err := engine.Run(ctx, command); !errors.Is(err, automation.ErrAutomationLoop) {
		t.Fatalf("loop guard error=%v", err)
	}
}

type acceptanceNoopActions struct{}

func (acceptanceNoopActions) Change(context.Context, workrecords.PriorityCommand) (workrecords.Record, error) {
	return workrecords.Record{}, automation.ErrActionFailed
}
func (acceptanceNoopActions) Assign(context.Context, workrecords.AssignCommand) (workrecords.Record, error) {
	return workrecords.Record{}, automation.ErrActionFailed
}
func (acceptanceNoopActions) Transition(context.Context, workrecords.TransitionCommand) (workrecords.Record, error) {
	return workrecords.Record{}, automation.ErrActionFailed
}
func (acceptanceNoopActions) Create(context.Context, automation.CommentCommand) (string, error) {
	return "", automation.ErrActionFailed
}
func (acceptanceNoopActions) Call(context.Context, authorization.Principal, string, map[string]string, map[string]string) (automation.ActionResult, error) {
	return automation.ActionResult{}, automation.ErrActionFailed
}
func (acceptanceNoopActions) Authorize(context.Context, string, string, string, automation.ActionKind) error {
	return nil
}

func containsAssignment(assignments []tagging.Assignment, tagID string) bool {
	for _, assignment := range assignments {
		if assignment.Tag.ID == tagID {
			return true
		}
	}
	return false
}

func containsAssignmentWithSource(assignments []tagging.Assignment, tagID string, source tagging.Source) bool {
	for _, assignment := range assignments {
		if assignment.Tag.ID == tagID && assignment.Source == source {
			return true
		}
	}
	return false
}
