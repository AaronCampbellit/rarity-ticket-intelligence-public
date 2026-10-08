package psa_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/httpapi"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sessions"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store"
	storeauthn "github.com/rarity-ticket-intelligence/rarity/backend/internal/store/authn"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/store/psa"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/migrations"
)

type classificationTransport struct {
	tagID           string
	additionalTagID string
	invalid         bool
	calls           *int
}

func (transport classificationTransport) Do(_ context.Context, _ aiassist.ProviderConnection, request aiassist.HTTPRequest) (aiassist.HTTPResponse, error) {
	if transport.calls != nil {
		(*transport.calls)++
	}
	if request.Method != http.MethodPost || request.Path != "/api/chat" {
		return aiassist.HTTPResponse{}, fmt.Errorf("unexpected provider request %s %s", request.Method, request.Path)
	}
	if !strings.Contains(string(request.Body), transport.tagID) || (transport.additionalTagID != "" && !strings.Contains(string(request.Body), transport.additionalTagID)) || (!strings.Contains(string(request.Body), "Classify VPN access") && !strings.Contains(string(request.Body), "Global VPN runbook")) {
		return aiassist.HTTPResponse{}, fmt.Errorf("provider request omitted authorized context or candidate")
	}
	content := fmt.Sprintf(`{"suggestions":[{"tag_id":%q,"confidence":0.990,"rationale":"VPN access request"}]}`, transport.tagID)
	if transport.additionalTagID != "" {
		content = fmt.Sprintf(`{"suggestions":[{"tag_id":%q,"confidence":0.990,"rationale":"VPN access request"},{"tag_id":%q,"confidence":0.980,"rationale":"Network access request"}]}`, transport.tagID, transport.additionalTagID)
	}
	if transport.invalid {
		content = `{"suggestions":[{"tag_id":"not-authorized","confidence":0.990,"rationale":"invalid candidate"}]}`
	}
	body := fmt.Sprintf(`{"message":{"content":%q},"prompt_eval_count":10,"eval_count":5}`, content)
	return aiassist.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(body)}, nil
}

func TestClassificationPipelineAgainstPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for classification pipeline verification")
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	mspID, clientID, secondClientID, technicianID, roleID := id.New(), id.New(), id.New(), id.New(), id.New()
	connectionID, modelID, aiPolicyID, tagPolicyID := id.New(), id.New(), id.New(), id.New()
	groupID, humanTagID, aiTagID, aiTagBID := id.New(), id.New(), id.New(), id.New()
	workID, taskID := id.New(), id.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO msp_organizations (id,display_id,name,created_by,updated_by) VALUES ($1,$2,'classification pipeline',$3,$3)`, mspID, "AI-"+mspID, technicianID)
	exec(`INSERT INTO client_organizations (id,msp_id,display_id,name,created_by,updated_by) VALUES ($1,$2,$3,'client',$4,$4)`, clientID, mspID, "CLIENT-"+clientID, technicianID)
	exec(`INSERT INTO client_organizations (id,msp_id,display_id,name,created_by,updated_by) VALUES ($1,$2,$3,'second client',$4,$4)`, secondClientID, mspID, "CLIENT-"+secondClientID, technicianID)
	exec(`INSERT INTO technicians (id,msp_id,email,display_name) VALUES ($1,$2,$3,'Classifier')`, technicianID, mspID, technicianID+"@example.test")
	exec(`INSERT INTO roles (id,msp_id,key,name) VALUES ($1,$2,$3,'AI classifier')`, roleID, mspID, "classifier-"+roleID)
	exec(`INSERT INTO role_capabilities (role_id,msp_id,capability) VALUES ($1,$2,'ai.assist'),($1,$2,'classification.apply'),($1,$2,'classification.ai.manage')`, roleID, mspID)
	exec(`INSERT INTO role_assignments (id,msp_id,client_id,technician_id,role_id,granted_by) VALUES ($1,$2,$3,$4,$5,$4)`, id.New(), mspID, clientID, technicianID, roleID)
	exec(`INSERT INTO work_records (id,msp_id,client_id,display_id,record_type,title,status,priority,created_by,updated_by) VALUES ($1,$2,$3,$4,'request','Parent','new','normal',$5,$5)`, workID, mspID, clientID, "W-"+workID, technicianID)
	exec(`INSERT INTO tasks (id,msp_id,client_id,parent_type,parent_id,work_record_id,title,status,position,created_by,updated_by) VALUES ($1,$2,$3,'work_record',$4,$4,'Classify VPN access','new',1,$5,$5)`, taskID, mspID, clientID, workID, technicianID)
	exec(`INSERT INTO tag_groups (id,msp_id,internal_key,label,position,created_by,updated_by) VALUES ($1,$2,$3,'Technology',1,$4,$4)`, groupID, mspID, "taxonomy."+groupID, technicianID)
	exec(`INSERT INTO tags (id,msp_id,group_id,internal_key,label,created_by,updated_by) VALUES ($1,$2,$3,$4,'Human',$5,$5),($6,$2,$3,$7,'VPN dismissed',$5,$5),($8,$2,$3,$9,'Network',$5,$5)`, humanTagID, mspID, groupID, "taxonomy."+humanTagID, technicianID, aiTagID, "taxonomy."+aiTagID, aiTagBID, "taxonomy."+aiTagBID)
	exec(`INSERT INTO ai_provider_connections (id,msp_id,name,adapter_type,network_mode,base_url,enabled,timeout_seconds,disclosure_accepted_at,disclosure_accepted_by,local_network_acknowledged_at,local_network_acknowledged_by,health_state,created_at,created_by,updated_at,updated_by) VALUES ($1,$2,'local','ollama','local','http://127.0.0.1:11434',true,30,now(),$3,now(),$3,'healthy',now(),$3,now(),$3)`, connectionID, mspID, technicianID)
	exec(`INSERT INTO ai_model_profiles (id,msp_id,connection_id,provider_model_id,display_name,supported_features,context_limit,output_limit,zero_cost,enabled,discovered_at,last_discovered_at,created_at,updated_at) VALUES ($1,$2,$3,'classifier','Classifier',ARRAY['classification'],100000,1000,true,true,now(),now(),now(),now())`, modelID, mspID, connectionID)
	exec(`INSERT INTO ai_policies (id,msp_id,enabled,allowed_features,monthly_cost_limit_minor,classification_model_profile_id,created_by,updated_by) VALUES ($1,$2,false,ARRAY[]::text[],0,NULL,$3,$3)`, aiPolicyID, mspID, technicianID)
	exec(`INSERT INTO tag_ai_policies (id,msp_id,automatic_apply_enabled,automatic_apply_threshold,model_profile_id,created_by,updated_by) VALUES ($1,$2,false,0.950,NULL,$3,$3)`, tagPolicyID, mspID, technicianID)

	tagRepository := psa.NewTaggingRepositoryFromPool(pool)
	associationService := tagging.NewAssociationService(tagRepository)
	principal := authorization.Principal{ID: technicianID, Scope: scope.Principal{MSPID: mspID, ClientID: clientID}, Capabilities: authorization.NewCapabilitySet("classification.apply")}
	policyService := tagging.NewClassificationService(tagRepository, associationService, id.New)
	principalStore := storeauthn.NewPrincipalStore(pool, time.Now)
	clientPolicyPrincipal, err := principalStore.LoadPrincipal(ctx, sessions.Authenticated{TechnicianID: technicianID, MSPID: mspID}, clientID)
	if err != nil || !clientPolicyPrincipal.Capabilities.Has("classification.ai.manage") {
		t.Fatalf("load client-scoped AI manager: principal=%+v error=%v", clientPolicyPrincipal, err)
	}
	clientPolicyHandler := httpapi.NewRouter(httpapi.Dependencies{
		Principal:         func(*http.Request) (authorization.Principal, error) { return clientPolicyPrincipal, nil },
		TagClassification: policyService,
	})
	clientPolicyRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/classification/ai-policy", strings.NewReader(fmt.Sprintf(`{"automatic_apply_enabled":true,"automatic_apply_threshold":0.95,"model_profile_id":%q,"expected_version":1}`, modelID)))
	clientPolicyRequest.Header.Set("Content-Type", "application/json")
	clientPolicyRequest.Header.Set("If-Match", `"1"`)
	clientPolicyResponse := httptest.NewRecorder()
	clientPolicyHandler.ServeHTTP(clientPolicyResponse, clientPolicyRequest)
	if clientPolicyResponse.Code != http.StatusNotFound {
		t.Fatalf("client-scoped global policy mutation status=%d body=%s", clientPolicyResponse.Code, clientPolicyResponse.Body.String())
	}
	var unchangedAutomatic, unchangedAIEnabled bool
	if err := pool.QueryRow(ctx, `SELECT tag_policy.automatic_apply_enabled,ai_policy.enabled FROM tag_ai_policies tag_policy JOIN ai_policies ai_policy USING(msp_id) WHERE tag_policy.msp_id=$1`, mspID).Scan(&unchangedAutomatic, &unchangedAIEnabled); err != nil || unchangedAutomatic || unchangedAIEnabled {
		t.Fatalf("client-scoped grant mutated MSP policy automatic=%t enabled=%t error=%v", unchangedAutomatic, unchangedAIEnabled, err)
	}
	exec(`INSERT INTO role_assignments (id,msp_id,client_id,technician_id,role_id,granted_by) VALUES ($1,$2,NULL,$3,$4,$3)`, id.New(), mspID, technicianID, roleID)
	policyPrincipal, err := principalStore.LoadPrincipal(ctx, sessions.Authenticated{TechnicianID: technicianID, MSPID: mspID}, "")
	if err != nil || !policyPrincipal.Capabilities.Has("classification.ai.manage") || policyPrincipal.Scope.ClientID != "" {
		t.Fatalf("load MSP-global AI manager: principal=%+v error=%v", policyPrincipal, err)
	}
	policyHandler := httpapi.NewRouter(httpapi.Dependencies{
		Principal:         func(*http.Request) (authorization.Principal, error) { return policyPrincipal, nil },
		TagClassification: policyService,
	})
	policyRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/classification/ai-policy", strings.NewReader(fmt.Sprintf(`{"automatic_apply_enabled":true,"automatic_apply_threshold":0.95,"model_profile_id":%q,"expected_version":1}`, modelID)))
	policyRequest.Header.Set("Content-Type", "application/json")
	policyRequest.Header.Set("If-Match", `"1"`)
	policyResponse := httptest.NewRecorder()
	policyHandler.ServeHTTP(policyResponse, policyRequest)
	if policyResponse.Code != http.StatusOK {
		t.Fatalf("configure classification policy over public HTTP: status=%d body=%s", policyResponse.Code, policyResponse.Body.String())
	}
	var configuredAutomatic bool
	var configuredTagModel, configuredJobModel string
	if err := pool.QueryRow(ctx, `SELECT tag_policy.automatic_apply_enabled,tag_policy.model_profile_id::text,ai_policy.classification_model_profile_id::text FROM tag_ai_policies tag_policy JOIN ai_policies ai_policy USING(msp_id) WHERE tag_policy.msp_id=$1`, mspID).Scan(&configuredAutomatic, &configuredTagModel, &configuredJobModel); err != nil {
		t.Fatalf("load HTTP-configured policy: %v", err)
	}
	if !configuredAutomatic || configuredTagModel != modelID || configuredJobModel != modelID {
		t.Fatalf("HTTP-configured policy automatic=%t tag_model=%s job_model=%s", configuredAutomatic, configuredTagModel, configuredJobModel)
	}
	target := tagging.TargetRef{MSPID: mspID, ClientID: clientID, ObjectType: tagging.ObjectTask, ObjectID: taskID}
	classified, err := associationService.ReplaceDirect(ctx, tagging.ReplaceCommand{Principal: principal, Target: target, ExpectedObjectVersion: 1, TagIDs: []string{humanTagID}, Source: tagging.SourceHuman, Reason: "initial human classification", IdempotencyKey: "human:" + taskID, CorrelationID: id.New()})
	if err != nil {
		t.Fatalf("initial human classification: %v", err)
	}
	suggestionService := tagging.NewClassificationSuggestionService(tagRepository, associationService, id.New)
	suggestion, err := suggestionService.Request(ctx, principal, target)
	if err != nil {
		t.Fatalf("request suggestion: %v", err)
	}
	var subjectType, subjectID, jobState string
	if err := pool.QueryRow(ctx, `SELECT subject_type,subject_id::text,state FROM ai_generation_jobs WHERE id=$1`, suggestion.JobID).Scan(&subjectType, &subjectID, &jobState); err != nil {
		t.Fatalf("load queued job: %v", err)
	}
	if subjectType != "task" || subjectID != taskID || jobState != "queued" {
		t.Fatalf("queued job subject=%s/%s state=%s", subjectType, subjectID, jobState)
	}

	registry, err := aiassist.NewAdapterRegistry(aiassist.NewOllamaAdapter(classificationTransport{tagID: aiTagID, additionalTagID: aiTagBID}))
	if err != nil {
		t.Fatalf("adapter registry: %v", err)
	}
	jobWorker := aiassist.NewJobWorker(psa.NewAIJobRepositoryFromPool(pool, nil), registry, time.Now, id.New)
	result, err := jobWorker.RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("classification generation: %v", err)
	}
	if result.Completed != 1 {
		t.Fatalf("classification generation result=%+v", result)
	}
	var suggestionState, applicationState string
	var itemCount, completionEvents int
	if err := pool.QueryRow(ctx, `SELECT status,application_state FROM tag_ai_suggestions WHERE id=$1`, suggestion.ID).Scan(&suggestionState, &applicationState); err != nil {
		t.Fatalf("load completed suggestion: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_ai_suggestion_items WHERE suggestion_id=$1 AND tag_id IN($2,$3) AND confidence>=0.980`, suggestion.ID, aiTagID, aiTagBID).Scan(&itemCount); err != nil {
		t.Fatalf("load suggestion items: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE msp_id=$1 AND subject_type='tag_ai_suggestion' AND subject_id=$2`, mspID, suggestion.ID).Scan(&completionEvents); err != nil {
		t.Fatalf("load completion outbox: %v", err)
	}
	if suggestionState != "completed" || applicationState != "queued" || itemCount != 2 || completionEvents != 1 {
		t.Fatalf("completed suggestion status=%s application=%s items=%d outbox=%d", suggestionState, applicationState, itemCount, completionEvents)
	}
	// Paid classification jobs settle actual usage atomically and subsequent
	// reservations observe every earlier completion in the same month.
	exec(`UPDATE ai_model_profiles SET zero_cost=false,input_cost_per_million_minor=100000,output_cost_per_million_minor=100000,version=version+1 WHERE id=$1`, modelID)
	exec(`UPDATE ai_policies SET cost_limit_enabled=true,allow_unmetered_unknown=false,monthly_cost_limit_minor=1000000000,version=version+1 WHERE id=$1`, aiPolicyID)
	paidCalls := 0
	paidRegistry, err := aiassist.NewAdapterRegistry(aiassist.NewOllamaAdapter(classificationTransport{tagID: aiTagID, calls: &paidCalls}))
	if err != nil {
		t.Fatal(err)
	}
	paidWorker := aiassist.NewJobWorker(psa.NewAIJobRepositoryFromPool(pool, nil), paidRegistry, time.Now, id.New)
	paidJobIDs := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		paidSuggestion, requestErr := tagging.NewClassificationSuggestionService(tagRepository, associationService, id.New).Request(ctx, principal, target)
		if requestErr != nil {
			t.Fatalf("request paid classification %d: %v", i, requestErr)
		}
		paidJobIDs = append(paidJobIDs, paidSuggestion.JobID)
		paidResult, runErr := paidWorker.RunOnce(ctx, 1)
		if runErr != nil || paidResult.Completed != 1 {
			t.Fatalf("paid classification %d result=%+v error=%v", i, paidResult, runErr)
		}
		exec(`UPDATE tag_ai_suggestions SET application_state='skipped' WHERE id=$1`, paidSuggestion.ID)
	}
	var paidUsageCount int
	var paidActualCost, paidReservations int64
	if err := pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum(cost_minor),0) FROM ai_usage_records WHERE generation_job_id=ANY($1::uuid[])`, paidJobIDs).Scan(&paidUsageCount, &paidActualCost); err != nil {
		t.Fatalf("paid classification usage: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(reserved_cost_minor),0) FROM ai_generation_jobs WHERE id=ANY($1::uuid[])`, paidJobIDs).Scan(&paidReservations); err != nil {
		t.Fatal(err)
	}
	if paidUsageCount != 2 || paidActualCost <= 0 || paidReservations != 0 || paidCalls != 2 {
		t.Fatalf("paid usage rows=%d cost=%d reservations=%d provider_calls=%d", paidUsageCount, paidActualCost, paidReservations, paidCalls)
	}
	exec(`UPDATE ai_policies SET monthly_cost_limit_minor=$2,version=version+1 WHERE id=$1`, aiPolicyID, paidActualCost)
	blockedSuggestion, err := tagging.NewClassificationSuggestionService(tagRepository, associationService, id.New).Request(ctx, principal, target)
	if err != nil {
		t.Fatal(err)
	}
	blockedResult, err := paidWorker.RunOnce(ctx, 1)
	if err != nil || blockedResult.Failed != 1 || paidCalls != 2 {
		t.Fatalf("monthly cap result=%+v error=%v provider_calls=%d", blockedResult, err, paidCalls)
	}
	var blockedState string
	var blockedReservation int64
	if err := pool.QueryRow(ctx, `SELECT state,reserved_cost_minor FROM ai_generation_jobs WHERE id=$1`, blockedSuggestion.JobID).Scan(&blockedState, &blockedReservation); err != nil {
		t.Fatal(err)
	}
	if blockedState != "failed" || blockedReservation != 0 {
		t.Fatalf("blocked classification state=%s reservation=%d", blockedState, blockedReservation)
	}
	exec(`UPDATE ai_policies SET monthly_cost_limit_minor=1000000000,version=version+1 WHERE id=$1`, aiPolicyID)
	exec(`UPDATE ai_model_profiles SET input_cost_per_million_minor=NULL,output_cost_per_million_minor=NULL,version=version+1 WHERE id=$1`, modelID)
	unknownSuggestion, err := tagging.NewClassificationSuggestionService(tagRepository, associationService, id.New).Request(ctx, principal, target)
	if err != nil {
		t.Fatal(err)
	}
	unknownResult, err := paidWorker.RunOnce(ctx, 1)
	if err != nil || unknownResult.Failed != 1 || paidCalls != 2 {
		t.Fatalf("unknown paid cost result=%+v error=%v provider_calls=%d", unknownResult, err, paidCalls)
	}
	if err := pool.QueryRow(ctx, `SELECT state,reserved_cost_minor FROM ai_generation_jobs WHERE id=$1`, unknownSuggestion.JobID).Scan(&blockedState, &blockedReservation); err != nil || blockedState != "failed" || blockedReservation != 0 {
		t.Fatalf("unknown-cost failure state=%s reservation=%d error=%v", blockedState, blockedReservation, err)
	}
	exec(`UPDATE ai_model_profiles SET zero_cost=true,input_cost_per_million_minor=NULL,output_cost_per_million_minor=NULL,version=version+1 WHERE id=$1`, modelID)
	exec(`UPDATE ai_policies SET cost_limit_enabled=false,version=version+1 WHERE id=$1`, aiPolicyID)
	dismissed, err := suggestionService.Decide(ctx, principal, target, suggestion.ID, aiTagID, "dismissed")
	if err != nil || dismissed.Status != tagging.SuggestionCompleted {
		t.Fatalf("dismiss one item before automatic application: suggestion=%+v error=%v", dismissed, err)
	}
	leasedApplications, err := tagRepository.ClaimClassificationApplications(ctx, 1, time.Minute)
	if err != nil || len(leasedApplications) != 1 || leasedApplications[0].LeaseToken == "" {
		t.Fatalf("claim application lease=%+v error=%v", leasedApplications, err)
	}
	exec(`UPDATE tag_ai_suggestions SET application_lease_until=now()-interval '1 second' WHERE id=$1`, suggestion.ID)
	if err := tagRepository.FinishClassificationApplication(ctx, leasedApplications[0], "skipped"); !errors.Is(err, scope.ErrNotFound) {
		t.Fatalf("stale application lease error=%v, want scoped not found", err)
	}

	applicationWorker := tagging.NewClassificationApplicationWorker(tagRepository, associationService, time.Now, id.New)
	// Hold the automatic assignment at its final write after it has acquired
	// every authority lock. Both an administrator disabling automatic apply and
	// a simultaneous role revocation must wait for that atomic decision.
	exec(`DROP TRIGGER IF EXISTS classification_application_test_gate ON object_tag_assignments`)
	exec(`DROP FUNCTION IF EXISTS classification_application_test_gate()`)
	exec(`CREATE FUNCTION classification_application_test_gate() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.assignment_source='ai_automatic' THEN PERFORM pg_advisory_xact_lock(7461829301::bigint); END IF; RETURN NEW; END $$`)
	exec(`CREATE TRIGGER classification_application_test_gate BEFORE INSERT ON object_tag_assignments FOR EACH ROW EXECUTE FUNCTION classification_application_test_gate()`)
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gate.Rollback(ctx) }()
	if _, err := gate.Exec(ctx, `SELECT pg_advisory_xact_lock(7461829301::bigint)`); err != nil {
		t.Fatal(err)
	}
	applicationDone := make(chan error, 1)
	go func() { applicationDone <- applicationWorker.RunOnce(ctx, 1) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%INSERT INTO object_tag_assignments%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-applicationDone:
			t.Fatalf("automatic apply finished before test gate: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic apply did not reach the gated association write")
		}
		time.Sleep(10 * time.Millisecond)
	}
	disableDone := make(chan error, 1)
	go func() {
		_, updateErr := policyService.UpdatePolicy(ctx, tagging.UpdateClassificationPolicyCommand{Principal: policyPrincipal, Enabled: false, Threshold: .95, ModelProfileID: modelID, ExpectedVersion: 2})
		disableDone <- updateErr
	}()
	revokeDone := make(chan error, 1)
	go func() {
		_, revokeErr := pool.Exec(ctx, `DELETE FROM role_assignments WHERE msp_id=$1 AND client_id=$2 AND technician_id=$3 AND role_id=$4`, mspID, clientID, technicianID, roleID)
		revokeDone <- revokeErr
	}()
	for name, done := range map[string]<-chan error{"policy disable": disableDone, "role revoke": revokeDone} {
		select {
		case err := <-done:
			t.Fatalf("%s escaped automatic-apply authority locks: %v", name, err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err := gate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for name, done := range map[string]<-chan error{"automatic apply": applicationDone, "policy disable": disableDone, "role revoke": revokeDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s after releasing authority lock: %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s remained blocked after releasing authority lock", name)
		}
	}
	exec(`DROP TRIGGER classification_application_test_gate ON object_tag_assignments`)
	exec(`DROP FUNCTION classification_application_test_gate()`)
	if _, err := policyService.UpdatePolicy(ctx, tagging.UpdateClassificationPolicyCommand{Principal: policyPrincipal, Enabled: true, Threshold: .95, ModelProfileID: modelID, ExpectedVersion: 3}); err != nil {
		t.Fatalf("restore automatic policy after concurrency fence: %v", err)
	}
	exec(`INSERT INTO role_assignments (id,msp_id,client_id,technician_id,role_id,granted_by) VALUES ($1,$2,$3,$4,$5,$4)`, id.New(), mspID, clientID, technicianID, roleID)
	loaded, err := associationService.Get(ctx, tagging.GetCommand{Principal: principal, Target: target})
	if err != nil {
		t.Fatalf("load applied classification: %v", err)
	}
	if loaded.ObjectVersion != classified.ObjectVersion+1 || len(loaded.Direct) != 2 {
		t.Fatalf("applied object=%+v", loaded)
	}
	sources := map[string]tagging.Source{}
	for _, assignment := range loaded.Direct {
		sources[assignment.Tag.ID] = assignment.Source
	}
	if sources[humanTagID] != tagging.SourceHuman || sources[aiTagBID] != tagging.SourceAIAutomatic || sources[aiTagID] != "" {
		t.Fatalf("assignment sources=%v", sources)
	}
	var finalState string
	var automaticDecision, associationHistory, associationEvents, dismissedDecision, contradictoryDecision int
	var dismissedDisposition, appliedDisposition string
	if err := pool.QueryRow(ctx, `SELECT application_state FROM tag_ai_suggestions WHERE id=$1`, suggestion.ID).Scan(&finalState); err != nil {
		t.Fatalf("load application state: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_ai_suggestion_decisions WHERE suggestion_id=$1 AND tag_id=$2 AND decision='automatically_applied'`, suggestion.ID, aiTagBID).Scan(&automaticDecision); err != nil {
		t.Fatalf("load automatic decision: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_assignment_events WHERE msp_id=$1 AND object_type='task' AND object_id=$2 AND tag_id=$3 AND assignment_source='ai_automatic'`, mspID, taskID, aiTagBID).Scan(&associationHistory); err != nil {
		t.Fatalf("load assignment history: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE decision='dismissed'),count(*) FILTER(WHERE decision='automatically_applied') FROM tag_ai_suggestion_decisions WHERE suggestion_id=$1 AND tag_id=$2`, suggestion.ID, aiTagID).Scan(&dismissedDecision, &contradictoryDecision); err != nil {
		t.Fatalf("load dismissed decision facts: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT dismissed.disposition,applied.disposition FROM tag_ai_suggestion_items dismissed JOIN tag_ai_suggestion_items applied ON applied.suggestion_id=dismissed.suggestion_id AND applied.msp_id=dismissed.msp_id WHERE dismissed.suggestion_id=$1 AND dismissed.tag_id=$2 AND applied.tag_id=$3`, suggestion.ID, aiTagID, aiTagBID).Scan(&dismissedDisposition, &appliedDisposition); err != nil {
		t.Fatalf("load mixed item projections: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox WHERE msp_id=$1 AND subject_type='task' AND subject_id=$2 AND event_type='classification.tags.replaced'`, mspID, taskID).Scan(&associationEvents); err != nil {
		t.Fatalf("load association outbox: %v", err)
	}
	if finalState != "applied" || automaticDecision != 1 || associationHistory != 1 || associationEvents < 1 || dismissedDecision != 1 || contradictoryDecision != 0 || dismissedDisposition != "rejected" || appliedDisposition != "automatically_applied" {
		t.Fatalf("application state=%s automatic_decision=%d history=%d outbox=%d dismissed_decision=%d contradictory=%d dispositions=%s/%s", finalState, automaticDecision, associationHistory, associationEvents, dismissedDecision, contradictoryDecision, dismissedDisposition, appliedDisposition)
	}

	// Re-running both workers is an exact no-op: no second provider completion,
	// application, history row, or outbox fact is emitted.
	if result, err := jobWorker.RunOnce(ctx, 1); err != nil || result.Claimed != 0 {
		t.Fatalf("generation replay result=%+v error=%v", result, err)
	}
	if err := applicationWorker.RunOnce(ctx, 1); err != nil {
		t.Fatalf("application replay: %v", err)
	}
	var repeatedHistory, repeatedDecision int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_assignment_events WHERE msp_id=$1 AND object_type='task' AND object_id=$2 AND tag_id=$3 AND assignment_source='ai_automatic'`, mspID, taskID, aiTagBID).Scan(&repeatedHistory); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_ai_suggestion_decisions WHERE suggestion_id=$1 AND decision='automatically_applied'`, suggestion.ID).Scan(&repeatedDecision); err != nil {
		t.Fatal(err)
	}
	if repeatedHistory != associationHistory || repeatedDecision != automaticDecision {
		t.Fatalf("replay history=%d decision=%d, want %d/%d", repeatedHistory, repeatedDecision, associationHistory, automaticDecision)
	}

	// In the inverse ordering, automatic application owns the suggestion lock
	// first. A concurrent human dismissal must wait, then observe the immutable
	// automatic decision and fail consistently without creating a second fact.
	raceSuggestion, err := suggestionService.Request(ctx, principal, target)
	if err != nil {
		t.Fatalf("request decision/application race suggestion: %v", err)
	}
	if raceResult, err := jobWorker.RunOnce(ctx, 1); err != nil || raceResult.Completed != 1 {
		t.Fatalf("generate decision/application race suggestion result=%+v error=%v", raceResult, err)
	}
	exec(`CREATE FUNCTION classification_application_test_gate() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.assignment_source='ai_automatic' THEN PERFORM pg_advisory_xact_lock(7461829301::bigint); END IF; RETURN NEW; END $$`)
	exec(`CREATE TRIGGER classification_application_test_gate BEFORE INSERT ON object_tag_assignments FOR EACH ROW EXECUTE FUNCTION classification_application_test_gate()`)
	raceGate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raceGate.Rollback(ctx) }()
	if _, err := raceGate.Exec(ctx, `SELECT pg_advisory_xact_lock(7461829301::bigint)`); err != nil {
		t.Fatal(err)
	}
	raceApplicationDone := make(chan error, 1)
	go func() { raceApplicationDone <- applicationWorker.RunOnce(ctx, 1) }()
	deadline = time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%INSERT INTO object_tag_assignments%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-raceApplicationDone:
			t.Fatalf("race application finished before test gate: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("race application did not reach the gated association write")
		}
		time.Sleep(10 * time.Millisecond)
	}
	raceDecisionDone := make(chan error, 1)
	go func() {
		_, decisionErr := suggestionService.Decide(ctx, principal, target, raceSuggestion.ID, aiTagID, "dismissed")
		raceDecisionDone <- decisionErr
	}()
	select {
	case err := <-raceDecisionDone:
		t.Fatalf("human decision escaped the application suggestion lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := raceGate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-raceApplicationDone:
		if err != nil {
			t.Fatalf("race automatic application: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("race automatic application remained blocked after gate release")
	}
	select {
	case err := <-raceDecisionDone:
		if !errors.Is(err, object.ErrVersionConflict) {
			t.Fatalf("decision ordered after automatic application error=%v, want version conflict", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("human decision remained blocked after automatic application committed")
	}
	exec(`DROP TRIGGER classification_application_test_gate ON object_tag_assignments`)
	exec(`DROP FUNCTION classification_application_test_gate()`)
	if _, err := suggestionService.Decide(ctx, principal, target, raceSuggestion.ID, aiTagID, "dismissed"); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("post-application conflicting decision replay error=%v", err)
	}
	var raceAssignments, raceAutomaticDecisions, raceDismissedDecisions int
	var raceDisposition, raceApplicationState string
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM object_tag_assignments WHERE msp_id=$1 AND client_id=$2 AND object_type='task' AND object_id=$3 AND tag_id=$4 AND assignment_source='ai_automatic'`, mspID, clientID, taskID, aiTagID).Scan(&raceAssignments); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE decision='automatically_applied'),count(*) FILTER(WHERE decision='dismissed') FROM tag_ai_suggestion_decisions WHERE suggestion_id=$1 AND tag_id=$2`, raceSuggestion.ID, aiTagID).Scan(&raceAutomaticDecisions, &raceDismissedDecisions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT item.disposition,suggestion.application_state FROM tag_ai_suggestion_items item JOIN tag_ai_suggestions suggestion ON suggestion.id=item.suggestion_id AND suggestion.msp_id=item.msp_id WHERE item.suggestion_id=$1 AND item.tag_id=$2`, raceSuggestion.ID, aiTagID).Scan(&raceDisposition, &raceApplicationState); err != nil {
		t.Fatal(err)
	}
	if raceAssignments != 1 || raceAutomaticDecisions != 1 || raceDismissedDecisions != 0 || raceDisposition != "automatically_applied" || raceApplicationState != "applied" {
		t.Fatalf("serialized decision/application assignment=%d automatic=%d dismissed=%d disposition=%s state=%s", raceAssignments, raceAutomaticDecisions, raceDismissedDecisions, raceDisposition, raceApplicationState)
	}
	if err := applicationWorker.RunOnce(ctx, 1); err != nil {
		t.Fatalf("race application replay: %v", err)
	}
	var replayRaceAutomaticDecisions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tag_ai_suggestion_decisions WHERE suggestion_id=$1 AND tag_id=$2 AND decision='automatically_applied'`, raceSuggestion.ID, aiTagID).Scan(&replayRaceAutomaticDecisions); err != nil || replayRaceAutomaticDecisions != 1 {
		t.Fatalf("race application decision replay count=%d error=%v", replayRaceAutomaticDecisions, err)
	}

	// Global knowledge is a first-class null-Client classification subject.
	knowledgeID := id.New()
	exec(`INSERT INTO knowledge_articles(id,msp_id,client_id,display_id,title,state,current_version,created_by,updated_by) VALUES($1,$2,NULL,$3,'Global VPN runbook','published',1,$4,$4)`, knowledgeID, mspID, "K-"+knowledgeID, technicianID)
	exec(`INSERT INTO knowledge_article_versions(article_id,msp_id,version,body,created_by,published_at,published_by) VALUES($1,$2,1,'Global VPN runbook troubleshooting steps',$3,now(),$3)`, knowledgeID, mspID, technicianID)
	exec(`INSERT INTO role_assignments(id,msp_id,client_id,technician_id,role_id,granted_by) VALUES($1,$2,NULL,$3,$4,$3)`, id.New(), mspID, technicianID, roleID)
	globalPrincipal := authorization.Principal{ID: technicianID, Scope: scope.Principal{MSPID: mspID}, Capabilities: authorization.NewCapabilitySet("classification.apply")}
	globalTarget := tagging.TargetRef{MSPID: mspID, ObjectType: tagging.ObjectKnowledgeArticle, ObjectID: knowledgeID}
	globalSuggestion, err := tagging.NewClassificationSuggestionService(tagRepository, associationService, id.New).Request(ctx, globalPrincipal, globalTarget)
	if err != nil {
		t.Fatalf("request global knowledge classification: %v", err)
	}
	globalResult, err := jobWorker.RunOnce(ctx, 1)
	if err != nil || globalResult.Completed != 1 {
		t.Fatalf("global knowledge generation result=%+v error=%v", globalResult, err)
	}
	var globalClient, globalSubject, globalStatus string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(job.client_id::text,''),job.subject_type,suggestion.status FROM ai_generation_jobs job JOIN tag_ai_suggestions suggestion ON suggestion.ai_generation_job_id=job.id WHERE suggestion.id=$1`, globalSuggestion.ID).Scan(&globalClient, &globalSubject, &globalStatus); err != nil {
		t.Fatal(err)
	}
	if globalClient != "" || globalSubject != "knowledge_article" || globalStatus != "completed" {
		t.Fatalf("global knowledge client=%q subject=%s status=%s", globalClient, globalSubject, globalStatus)
	}

	// Human decisions are per item: the first accept keeps the second item
	// decidable, and two sequential accepts apply both tags exactly once.
	manualTagA, manualTagB := id.New(), id.New()
	exec(`INSERT INTO tags(id,msp_id,group_id,internal_key,label,created_by,updated_by) VALUES($1,$2,$3,$4,'Manual A',$5,$5),($6,$2,$3,$7,'Manual B',$5,$5)`, manualTagA, mspID, groupID, "taxonomy."+manualTagA, technicianID, manualTagB, "taxonomy."+manualTagB)
	manualSuggestion, err := tagging.NewClassificationSuggestionService(tagRepository, associationService, id.New).Request(ctx, principal, target)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE ai_generation_jobs SET state='failed',completed_at=now(),safe_error_code='manual_test' WHERE id=$1`, manualSuggestion.JobID)
	exec(`UPDATE tag_ai_suggestions SET status='completed',completed_at=now(),application_state='skipped' WHERE id=$1`, manualSuggestion.ID)
	exec(`INSERT INTO tag_ai_suggestion_items(suggestion_id,msp_id,tag_id,rank,confidence,rationale) VALUES($1,$2,$3,1,.9,'first'),($1,$2,$4,2,.8,'second')`, manualSuggestion.ID, mspID, manualTagA, manualTagB)
	firstDecision, err := suggestionService.Decide(ctx, principal, target, manualSuggestion.ID, manualTagA, "accepted")
	if err != nil || firstDecision.Status != tagging.SuggestionCompleted {
		t.Fatalf("first per-item decision=%+v error=%v", firstDecision, err)
	}
	finalDecision, err := suggestionService.Decide(ctx, principal, target, manualSuggestion.ID, manualTagB, "accepted")
	if err != nil || finalDecision.Status != tagging.SuggestionAccepted {
		t.Fatalf("second accepted decision=%+v error=%v", finalDecision, err)
	}
	acceptedObject, err := associationService.Get(ctx, tagging.GetCommand{Principal: principal, Target: target})
	if err != nil || len(acceptedObject.Direct) != 5 {
		t.Fatalf("two sequential accepts object=%+v error=%v", acceptedObject, err)
	}
	if _, err := suggestionService.Decide(ctx, principal, target, manualSuggestion.ID, manualTagA, "accepted"); err != nil {
		t.Fatalf("exact decision replay: %v", err)
	}
	if _, err := suggestionService.Decide(ctx, principal, target, manualSuggestion.ID, manualTagA, "dismissed"); !errors.Is(err, object.ErrVersionConflict) {
		t.Fatalf("conflicting decision replay error=%v", err)
	}

	// A strict provider-contract failure terminalizes both the job and its
	// suggestion without disturbing the existing manual/automatic tags.
	failedSuggestion, err := tagging.NewClassificationSuggestionService(tagRepository, associationService, id.New).Request(ctx, principal, target)
	if err != nil {
		t.Fatalf("request failing suggestion: %v", err)
	}
	invalidRegistry, err := aiassist.NewAdapterRegistry(aiassist.NewOllamaAdapter(classificationTransport{tagID: aiTagID, invalid: true}))
	if err != nil {
		t.Fatal(err)
	}
	failedResult, err := aiassist.NewJobWorker(psa.NewAIJobRepositoryFromPool(pool, nil), invalidRegistry, time.Now, id.New).RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("failed provider run: %v", err)
	}
	var failedJobState, failedSuggestionState, safeCode string
	if err := pool.QueryRow(ctx, `SELECT state,safe_error_code FROM ai_generation_jobs WHERE id=$1`, failedSuggestion.JobID).Scan(&failedJobState, &safeCode); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM tag_ai_suggestions WHERE id=$1`, failedSuggestion.ID).Scan(&failedSuggestionState); err != nil {
		t.Fatal(err)
	}
	afterFailure, err := associationService.Get(ctx, tagging.GetCommand{Principal: principal, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if failedResult.Failed != 1 || failedJobState != "failed" || failedSuggestionState != "failed" || safeCode == "" || len(afterFailure.Direct) != 5 {
		t.Fatalf("failure result=%+v job=%s suggestion=%s code=%s object=%+v", failedResult, failedJobState, failedSuggestionState, safeCode, afterFailure)
	}

	jobRepository := psa.NewAIJobRepositoryFromPool(pool, nil)
	claimOne := func(suggestion tagging.ClassificationSuggestionRecord) aiassist.ClaimedGenerationJob {
		t.Helper()
		claimed, err := jobRepository.Claim(ctx, 1, time.Minute, []aiassist.JobTransitionIDs{{AuditID: id.New(), EventID: id.New(), CorrelationID: id.New()}})
		if err != nil || len(claimed) != 1 || claimed[0].Job.ID != suggestion.JobID {
			t.Fatalf("claim typed job=%+v error=%v want=%s", claimed, err, suggestion.JobID)
		}
		return claimed[0]
	}
	providerBody := func(execution aiassist.JobExecution) ([]byte, string) {
		t.Helper()
		fields := make([]aiassist.ContextField, 0, len(execution.Context.Fields))
		for _, field := range execution.Context.Fields {
			if strings.TrimSpace(field.Value) != "" {
				fields = append(fields, field)
			}
		}
		request := aiassist.ProviderRequest{Feature: execution.Job.Feature, Provider: string(execution.Connection.Adapter), Model: execution.Model.ProviderModelID, PromptVersion: execution.Policy.PromptVersion, MSPID: execution.Job.MSPID, ClientID: execution.Job.ClientID, WorkRecordID: execution.Job.WorkRecordID, Fields: fields, AuthorizedCandidateIDs: execution.Context.AuthorizedCandidateIDs, MaxOutputUnits: execution.Model.OutputLimit}
		body, err := aiassist.PrepareProviderRequest(execution.Connection.Adapter, execution.Model, request)
		if err != nil {
			t.Fatalf("prepare fenced request: %v", err)
		}
		return body, aiassist.ExecutionFingerprint(body, execution)
	}

	staleBeforeReserve, err := tagging.NewClassificationSuggestionService(tagRepository, associationService, id.New).Request(ctx, principal, target)
	if err != nil {
		t.Fatal(err)
	}
	claimed := claimOne(staleBeforeReserve)
	execution, err := jobRepository.LoadExecution(ctx, claimed.Job.ID, claimed.LeaseToken)
	if err != nil {
		t.Fatalf("load typed execution: %v", err)
	}
	body, fingerprint := providerBody(execution)
	exec(`UPDATE tasks SET updated_at=updated_at+interval '1 second' WHERE id=$1`, taskID)
	reservedAfterChange, err := jobRepository.AuthorizeAndReserve(ctx, claimed.Job.ID, claimed.LeaseToken, fingerprint, int64(len(body)), execution.Model.OutputLimit)
	if err != nil || aiassist.ExecutionFingerprint(body, reservedAfterChange) == fingerprint {
		t.Fatalf("stale typed reserve error=%v retained_fingerprint=%t", err, aiassist.ExecutionFingerprint(body, reservedAfterChange) == fingerprint)
	}
	if err := jobRepository.Fail(ctx, aiassist.JobFailure{JobID: claimed.Job.ID, LeaseToken: claimed.LeaseToken, SafeErrorCode: "execution_changed", FailedAt: time.Now().UTC(), IDs: aiassist.JobTransitionIDs{AuditID: id.New(), EventID: id.New(), CorrelationID: id.New()}}); err != nil {
		t.Fatalf("terminalize stale reserve: %v", err)
	}

	staleBeforeCredential, err := tagging.NewClassificationSuggestionService(tagRepository, associationService, id.New).Request(ctx, principal, target)
	if err != nil {
		t.Fatal(err)
	}
	claimed = claimOne(staleBeforeCredential)
	execution, err = jobRepository.LoadExecution(ctx, claimed.Job.ID, claimed.LeaseToken)
	if err != nil {
		t.Fatal(err)
	}
	body, fingerprint = providerBody(execution)
	reserved, err := jobRepository.AuthorizeAndReserve(ctx, claimed.Job.ID, claimed.LeaseToken, fingerprint, int64(len(body)), execution.Model.OutputLimit)
	if err != nil {
		t.Fatalf("reserve typed execution: %v", err)
	}
	exec(`UPDATE tasks SET updated_at=updated_at+interval '1 second' WHERE id=$1`, taskID)
	called := false
	err = jobRepository.ExecuteWithCredential(ctx, aiassist.CredentialReference{ConnectionID: reserved.Connection.ID, MSPID: reserved.Connection.MSPID, ConnectionVersion: reserved.Connection.Version, ConnectionBaseURL: reserved.Connection.BaseURL, ConnectionNetwork: reserved.Connection.Network, JobID: claimed.Job.ID, LeaseToken: claimed.LeaseToken, ExecutionFingerprint: fingerprint}, func([]byte) error {
		called = true
		return nil
	})
	if !errors.Is(err, aiassist.ErrExecutionFenceLost) || called {
		t.Fatalf("typed credential fence error=%v callback=%t", err, called)
	}

	// The destructive release rollback must handle populated client and global
	// classification jobs/suggestions, including the null-Client knowledge row.
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	goose.SetBaseFS(migrations.FS)
	defer goose.SetBaseFS(nil)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownToContext(ctx, database, ".", 80); err != nil {
		t.Fatalf("populated classification Down: %v", err)
	}
}
