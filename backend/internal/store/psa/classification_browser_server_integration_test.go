package psa_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/httpapi"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/mutation"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workrecords"
)

// TestClassificationBrowserAcceptanceServer is started by Playwright. It is a
// shared live acceptance boundary: classification HTTP handlers, services,
// workers and persistence are production implementations. The small shell
// endpoints only provide browser identity, directory and task display data.
func TestClassificationBrowserAcceptanceServer(t *testing.T) {
	if os.Getenv("CLASSIFICATION_E2E_SERVE") != "1" {
		t.Skip("started only by the classification Playwright project")
	}
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	fixture := seedClassificationBrowserFixture(t, ctx, pool)
	repository := psa.NewTaggingRepositoryFromPool(pool)
	associations := tagging.NewAssociationService(repository)
	projectionRepository := psa.NewTaggingProjectionRepositoryFromPool(pool)
	registry, err := aiassist.NewAdapterRegistry(aiassist.NewOllamaAdapter(acceptanceClassificationTransport{tagID: fixture.SecurityID}))
	if err != nil {
		t.Fatal(err)
	}
	jobWorker := aiassist.NewJobWorker(psa.NewAIJobRepositoryFromPool(pool, nil), registry, time.Now, id.New)
	applicationWorker := tagging.NewClassificationApplicationWorker(repository, associations, time.Now, id.New)
	projectionWorker := tagging.NewProjectionWorker(projectionRepository)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				_, _ = jobWorker.RunOnce(workerCtx, 100)
				_ = applicationWorker.RunOnce(workerCtx, 100)
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				_, _ = projectionWorker.RunOnce(workerCtx, 500)
			}
		}
	}()

	capabilityNames := []string{"classification.manage", "classification.apply", "classification.report", "classification.ai.manage"}
	calendarMode := os.Getenv("CALENDAR_E2E_SERVE") == "1"
	if calendarMode {
		capabilityNames = append(capabilityNames, browserCalendarCapabilities...)
	}
	capabilities := authorization.NewCapabilitySet(capabilityNames...)
	resolver := func(request *http.Request) (authorization.Principal, error) {
		clientID := request.Header.Get("X-Rarity-Client-ID")
		return authorization.Principal{ID: fixture.ActorID, Scope: scope.Principal{MSPID: fixture.MSPID, ClientID: clientID}, Capabilities: capabilities}, nil
	}
	dependencies := httpapi.Dependencies{
		Principal: resolver, TagCatalog: tagging.NewCatalogService(repository, time.Now, id.New),
		TagAssociations:              associations,
		TagClassification:            tagging.NewClassificationService(repository, associations, id.New),
		TagClassificationSuggestions: tagging.NewClassificationSuggestionService(repository, associations, id.New),
		TagReports:                   tagging.NewReportService(projectionRepository),
	}
	if calendarMode {
		attachCalendarBrowserServices(t, workerCtx, pool, fixture, &dependencies)
	}
	router := httpapi.NewRouter(dependencies)

	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/e2e/health":
			_, _ = writer.Write([]byte(`{"ok":true}`))
		case "/e2e/fixture":
			_ = json.NewEncoder(writer).Encode(fixture)
		case "/v1/system/build":
			_, _ = writer.Write([]byte(`{"revision":"classification-live"}`))
		case "/api/v1/setup/status":
			_, _ = writer.Write([]byte(`{"completed":true,"bootstrap_available":false,"entra_available":true}`))
		case "/api/v1/me":
			if calendarMode {
				_ = json.NewEncoder(writer).Encode(map[string]any{"id": fixture.ActorID, "navigation": []string{"calendar", "project", "work", "classification-settings", "classification-insights"}, "capabilities": capabilityNames})
				return
			}
			_, _ = writer.Write([]byte(`{"id":"classification-e2e","navigation":["project","classification-settings","classification-insights"],"capabilities":["*"]}`))
		case "/api/v1/directory":
			_ = json.NewEncoder(writer).Encode(map[string]any{"clients": []map[string]string{{"ID": fixture.ClientA, "Name": "Alpha Client"}, {"ID": fixture.ClientB, "Name": "Beta Client"}}, "departments": []any{}, "teams": []any{}, "queues": []any{}, "technicians": []map[string]string{{"id": fixture.ActorID, "display_name": "Browser technician"}}})
		case "/api/v1/views":
			if calendarMode {
				router.ServeHTTP(writer, request)
				return
			}
			_, _ = writer.Write([]byte(`[]`))
		case "/api/v1/projects":
			if calendarMode {
				_ = json.NewEncoder(writer).Encode([]map[string]any{{"id": fixture.ProjectID, "name": "VPN rollout", "display_id": "BROWSER-PROJECT", "lifecycle_state": "active", "version": 1}})
				return
			}
			_, _ = writer.Write([]byte(`[]`))
		case "/api/v1/projects/" + fixture.ProjectID:
			baseline := map[string]any{"currency": "USD", "revenue_minor": 0, "cost_minor": 0, "planned_minutes": 60}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"id": fixture.ProjectID, "display_id": "BROWSER-PROJECT", "name": "VPN rollout",
				"client_name": "Alpha Client", "lifecycle_state": "active", "original_proposal_version": 1,
				"original_baseline": baseline, "current_baseline": baseline, "phases": []any{},
				"project_tasks": []map[string]any{
					{"id": fixture.TaskID, "title": "Inherited security task", "status": "open", "subtasks": 0, "estimate_minutes": 30, "actual_minutes": 0, "version": 1},
					{"id": fixture.RecurringTaskID, "title": "Recurring security task", "status": "open", "subtasks": 0, "estimate_minutes": 30, "actual_minutes": 0, "version": 1},
				},
				"resource_plans": []any{}, "cost_actuals": []any{}, "capacity": []any{}, "change_orders": []any{}, "financials_visible": false, "version": 1,
			})
		case "/api/v1/tasks/" + fixture.TaskID:
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": fixture.TaskID, "title": "Inherited security task", "status": "open", "estimate_minutes": 30, "parent": map[string]string{"type": "project", "id": fixture.ProjectID}})
		case "/api/v1/tasks/" + fixture.RecurringTaskID:
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": fixture.RecurringTaskID, "title": "Recurring security task", "status": "open", "estimate_minutes": 30, "parent": map[string]string{"type": "project", "id": fixture.ProjectID}})
		case "/api/v1/tasks/" + fixture.FallbackTaskID:
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": fixture.FallbackTaskID, "title": "Unclassified intake task", "status": "open", "estimate_minutes": 30, "parent": map[string]string{"type": "work_record", "id": fixture.FallbackWorkRecordID}})
		case "/e2e/evidence":
			var archived, assignmentEvents, projectionEffects int
			var currentUnclassified, currentMeaningful int
			var fallbackRemovedEvents, meaningfulAddedEvents, recurringReady int
			_ = pool.QueryRow(request.Context(), `SELECT count(*) FROM tags WHERE id=$1 AND lifecycle_state='archived'`, fixture.NetworkID).Scan(&archived)
			_ = pool.QueryRow(request.Context(), `SELECT count(*) FROM tag_assignment_events WHERE object_id=$1 AND operation IN ('added','removed')`, fixture.FallbackTaskID).Scan(&assignmentEvents)
			_ = pool.QueryRow(request.Context(), `SELECT count(*) FROM tag_projection_effects effect JOIN tag_assignment_events event ON event.id=effect.event_id WHERE event.object_id=$1`, fixture.FallbackTaskID).Scan(&projectionEffects)
			_ = pool.QueryRow(request.Context(), `
SELECT count(*) FROM object_tag_assignments assignment
JOIN tags tag ON tag.id=assignment.tag_id AND tag.msp_id=assignment.msp_id
WHERE assignment.msp_id=$1 AND assignment.client_id=$2
	  AND assignment.object_type='task' AND assignment.object_id=$3
	  AND tag.internal_key='taxonomy.system.unclassified'`, fixture.MSPID, fixture.ClientA, fixture.FallbackTaskID).Scan(&currentUnclassified)
			_ = pool.QueryRow(request.Context(), `
SELECT count(*) FROM object_tag_assignments assignment
JOIN tags tag ON tag.id=assignment.tag_id AND tag.msp_id=assignment.msp_id
WHERE assignment.msp_id=$1 AND assignment.client_id=$2
	  AND assignment.object_type='task' AND assignment.object_id=$3
	  AND tag.internal_key IS DISTINCT FROM 'taxonomy.system.unclassified'`, fixture.MSPID, fixture.ClientA, fixture.FallbackTaskID).Scan(&currentMeaningful)
			_ = pool.QueryRow(request.Context(), `
SELECT count(*) FROM tag_assignment_events
WHERE msp_id=$1 AND client_id=$2 AND object_type='task' AND object_id=$3
  AND tag_id=$4 AND operation='removed'`, fixture.MSPID, fixture.ClientA, fixture.FallbackTaskID, fixture.UnclassifiedID).Scan(&fallbackRemovedEvents)
			_ = pool.QueryRow(request.Context(), `
SELECT count(*) FROM tag_assignment_events
WHERE msp_id=$1 AND client_id=$2 AND object_type='task' AND object_id=$3
  AND tag_id<>$4 AND operation='added'`, fixture.MSPID, fixture.ClientA, fixture.FallbackTaskID, fixture.UnclassifiedID).Scan(&meaningfulAddedEvents)
			_ = pool.QueryRow(request.Context(), `
SELECT count(DISTINCT object_id) FROM tag_projection_effects
WHERE msp_id=$1 AND client_id=$2 AND object_type='task' AND tag_id=$3
  AND operation='added' AND object_id=ANY($4::uuid[])`, fixture.MSPID, fixture.ClientA, fixture.SecurityID, []string{fixture.TaskID, fixture.RecurringTaskID}).Scan(&recurringReady)
			_ = json.NewEncoder(writer).Encode(map[string]int{
				"archived": archived, "assignment_events": assignmentEvents, "projection_effects": projectionEffects,
				"current_unclassified": currentUnclassified, "current_meaningful": currentMeaningful,
				"fallback_removed_events": fallbackRemovedEvents, "meaningful_added_events": meaningfulAddedEvents,
				"recurring_ready": recurringReady,
			})
		default:
			router.ServeHTTP(writer, request)
		}
	})
	server := &http.Server{Addr: "127.0.0.1:18082", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}

type classificationBrowserFixture struct {
	MSPID                string `json:"-"`
	ClientA              string `json:"client_a"`
	ClientB              string `json:"client_b"`
	ActorID              string `json:"-"`
	TaskID               string `json:"task_id"`
	FallbackTaskID       string `json:"fallback_task_id"`
	FallbackWorkRecordID string `json:"-"`
	RecurringTaskID      string `json:"recurring_task_id"`
	ProjectID            string `json:"project_id"`
	UnclassifiedID       string `json:"unclassified_id"`
	NetworkID            string `json:"network_id"`
	SecurityID           string `json:"security_id"`
}

func seedClassificationBrowserFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) classificationBrowserFixture {
	t.Helper()
	f := classificationBrowserFixture{MSPID: id.New(), ClientA: id.New(), ClientB: id.New(), ActorID: id.New()}
	exec := func(query string, args ...any) {
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("classification browser fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations(id,display_id,name,created_by,updated_by) VALUES($1,$2,'Browser acceptance',$3,$3)`, f.MSPID, "BROWSER-"+f.MSPID, f.ActorID)
	exec(`INSERT INTO client_organizations(id,msp_id,display_id,name,created_by,updated_by) VALUES($1,$2,$3,'Alpha Client',$4,$4),($5,$2,$6,'Beta Client',$4,$4)`, f.ClientA, f.MSPID, "A-"+f.ClientA, f.ActorID, f.ClientB, "B-"+f.ClientB)
	exec(`INSERT INTO technicians(id,msp_id,email,display_name) VALUES($1,$2,$3,'Browser technician')`, f.ActorID, f.MSPID, f.ActorID+"@example.test")
	roleID := id.New()
	exec(`INSERT INTO roles(id,msp_id,key,name) VALUES($1,$2,$3,'Browser classification')`, roleID, f.MSPID, "browser-"+roleID)
	exec(`INSERT INTO role_capabilities(role_id,msp_id,capability) VALUES($1,$2,'classification.apply')`, roleID, f.MSPID)
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES($1,$2,$3,$4,$5,$4)`, id.New(), f.MSPID, f.ClientA, f.ActorID, roleID)
	repository := psa.NewTaggingRepositoryFromPool(pool)
	capabilities := authorization.NewCapabilitySet("classification.manage", "classification.apply", "project.create", "task.create", "work_record.create")
	catalogPrincipal := authorization.Principal{ID: f.ActorID, Scope: scope.Principal{MSPID: f.MSPID}, Capabilities: capabilities}
	clientPrincipal := authorization.Principal{ID: f.ActorID, Scope: scope.Principal{MSPID: f.MSPID, ClientID: f.ClientA}, Capabilities: capabilities}
	catalog := tagging.NewCatalogService(repository, time.Now, id.New)
	unclassified, err := catalog.EnsureSystemCatalog(ctx, f.MSPID, f.ActorID)
	if err != nil {
		t.Fatal(err)
	}
	f.UnclassifiedID = unclassified.ID
	group, err := catalog.CreateGroup(ctx, tagging.CreateGroupCommand{Principal: catalogPrincipal, Label: "Technology", Position: 2})
	if err != nil {
		t.Fatal(err)
	}
	network, err := catalog.CreateTag(ctx, tagging.CreateTagCommand{Principal: catalogPrincipal, GroupID: group.ID, Label: "Network", Synonyms: []string{"VPN"}})
	if err != nil {
		t.Fatal(err)
	}
	security, err := catalog.CreateTag(ctx, tagging.CreateTagCommand{Principal: catalogPrincipal, GroupID: group.ID, Label: "Security"})
	if err != nil {
		t.Fatal(err)
	}
	f.NetworkID, f.SecurityID = network.ID, security.ID
	proposal := insertAcceptanceProposal(t, ctx, pool, f.MSPID, f.ClientA, f.ActorID, "browser")
	creation := tagging.NewCreationPreparer(repository)
	project, err := projects.NewService(psa.NewProjectRepositoryFromPool(pool), time.Now, id.New, creation).Create(ctx, projects.CreateCommand{Principal: clientPrincipal, Target: scope.Target{MSPID: f.MSPID, ClientID: f.ClientA}, DisplayID: "BROWSER-PROJECT-" + f.MSPID[:8], Name: "VPN rollout", OriginalProposalVersionID: proposal, Phases: []projects.PhaseInput{{Name: "Delivery"}}, ActorID: f.ActorID, Source: "e2e", TagIDs: []string{security.ID}, ClassificationPolicy: tagging.CreationRequireMeaningful})
	if err != nil {
		t.Fatal(err)
	}
	f.ProjectID = string(project.ID)
	taskService := tasks.NewService(psa.NewTaskRepositoryFromPool(pool), time.Now, id.New, creation)
	task, err := taskService.Create(ctx, tasks.CreateCommand{Principal: clientPrincipal, Target: scope.Target{MSPID: f.MSPID, ClientID: f.ClientA}, Parent: tasks.Ref{Type: tasks.ParentProject, ID: f.ProjectID}, Title: "Inherited security task", ActorID: f.ActorID, Source: "e2e", ClassificationPolicy: tagging.CreationRequireMeaningful})
	if err != nil {
		t.Fatal(err)
	}
	f.TaskID = task.ID
	recurringTask, err := taskService.Create(ctx, tasks.CreateCommand{Principal: clientPrincipal, Target: scope.Target{MSPID: f.MSPID, ClientID: f.ClientA}, Parent: tasks.Ref{Type: tasks.ParentProject, ID: f.ProjectID}, Title: "Recurring security task", ActorID: f.ActorID, Source: "e2e", ClassificationPolicy: tagging.CreationRequireMeaningful})
	if err != nil {
		t.Fatal(err)
	}
	f.RecurringTaskID = recurringTask.ID

	insertAcceptanceWorkRecordConfiguration(t, ctx, pool, f.MSPID, f.ActorID)
	workRecordService := workrecords.NewService(
		psa.NewWorkRecordRepositoryFromPool(pool), psa.NewRoutingRepositoryFromPool(pool),
		psa.NewWorkflowRepositoryFromPool(pool), psa.NewSLARepositoryFromPool(pool),
		time.Now, id.New, creation,
	)
	fallbackRecord, err := workRecordService.CreateDattoIncident(ctx, workrecords.CreateCommand{
		Principal: clientPrincipal, Target: scope.Target{MSPID: f.MSPID, ClientID: f.ClientA},
		Actor: workrecords.Actor{ID: f.ActorID}, DisplayID: "BROWSER-ALERT-" + f.MSPID[:8],
		Type: workrecords.Incident, Title: "Trusted automated alert", Status: "new", Priority: "normal",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.FallbackWorkRecordID = fallbackRecord.ID
	initial, err := creation.Prepare(ctx, tagging.PrepareCreationCommand{
		MSPID: f.MSPID, ClientID: f.ClientA, ObjectType: tagging.ObjectTask,
		Source: tagging.SourceAutomation, Policy: tagging.CreationAllowFallback,
	})
	if err != nil {
		t.Fatal(err)
	}
	now, correlationID := time.Now().UTC(), id.New()
	fallbackTask := tasks.Task{
		ID: id.New(), MSPID: f.MSPID, ClientID: f.ClientA,
		Parent:       tasks.Ref{Type: tasks.ParentWorkRecord, ID: fallbackRecord.ID, MSPID: f.MSPID, ClientID: f.ClientA},
		WorkRecordID: fallbackRecord.ID, Title: "Unclassified intake task", Status: "open",
		Position: 1, Version: 1, CreatedBy: f.ActorID, EstimateMinutes: 30,
	}
	if err := psa.NewTaskRepositoryFromPool(pool).CreateAtomic(ctx, tasks.CreateMutation{
		Task:        fallbackTask,
		InitialTags: initial.WithProvenance(tagging.InitialAssignmentProvenance{ActorType: "integration", ActorID: f.ActorID, OccurredAt: now, CorrelationID: correlationID}),
		Audit:       mutation.AuditRecord{ID: id.New(), OccurredAt: now, MSPID: f.MSPID, ClientID: f.ClientA, ActorType: "integration", ActorID: f.ActorID, Action: "task.created", SubjectType: "task", SubjectID: fallbackTask.ID, SubjectVersion: 1, Source: "e2e", CorrelationID: correlationID},
		Event:       mutation.EventRecord{EventID: id.New(), EventType: "task.created", SchemaVersion: 1, OccurredAt: now, MSPID: f.MSPID, ClientID: f.ClientA, ActorType: "integration", ActorID: f.ActorID, SubjectType: "task", SubjectID: fallbackTask.ID, SubjectVersion: 1, Source: "e2e", CorrelationID: correlationID},
	}); err != nil {
		t.Fatal(err)
	}
	f.FallbackTaskID = fallbackTask.ID
	connectionID, modelID := id.New(), id.New()
	exec(`INSERT INTO ai_provider_connections(id,msp_id,name,adapter_type,network_mode,base_url,enabled,timeout_seconds,disclosure_accepted_at,disclosure_accepted_by,local_network_acknowledged_at,local_network_acknowledged_by,health_state,created_at,created_by,updated_at,updated_by) VALUES($1,$2,'browser','ollama','local','http://127.0.0.1:11434',true,30,now(),$3,now(),$3,'healthy',now(),$3,now(),$3)`, connectionID, f.MSPID, f.ActorID)
	exec(`INSERT INTO ai_model_profiles(id,msp_id,connection_id,provider_model_id,display_name,supported_features,context_limit,output_limit,zero_cost,enabled,discovered_at,last_discovered_at,created_at,updated_at) VALUES($1,$2,$3,'classifier','Classifier',ARRAY['classification'],100000,1000,true,true,now(),now(),now(),now())`, modelID, f.MSPID, connectionID)
	exec(`INSERT INTO ai_policies(id,msp_id,enabled,allowed_features,monthly_cost_limit_minor,classification_model_profile_id,created_by,updated_by) VALUES($1,$2,true,ARRAY['classification'],0,$3,$4,$4)`, id.New(), f.MSPID, modelID, f.ActorID)
	policy := tagging.NewClassificationService(repository, tagging.NewAssociationService(repository), id.New)
	if _, err := policy.UpdatePolicy(ctx, tagging.UpdateClassificationPolicyCommand{Principal: authorization.Principal{ID: f.ActorID, Scope: scope.Principal{MSPID: f.MSPID}, Capabilities: authorization.NewCapabilitySet("classification.ai.manage")}, Enabled: false, Threshold: .95, ModelProfileID: modelID, ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	return f
}
