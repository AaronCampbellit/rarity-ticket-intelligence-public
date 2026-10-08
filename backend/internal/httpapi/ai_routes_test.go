package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/object"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

type aiManagementActions struct {
	created        aiassist.CreateConnectionCommand
	credentialText string
	updated        aiassist.UpdateConnectionCommand
	enabled        aiassist.SetConnectionEnabledCommand
	credential     aiassist.ReplaceCredentialCommand
	models         aiassist.UpdateModelsCommand
	policy         aiassist.UpdatePolicyCommand
	err            error
}

func (a *aiManagementActions) CreateConnection(_ context.Context, command aiassist.CreateConnectionCommand) (aiassist.ProviderConnection, error) {
	a.credentialText = string(command.PlaintextCredential)
	a.created = command
	return aiProvider("provider-1", 1), a.err
}
func (*aiManagementActions) ListConnections(context.Context, aiassist.ListConnectionsCommand) ([]aiassist.ProviderConnection, error) {
	return []aiassist.ProviderConnection{aiProvider("provider-1", 1)}, nil
}
func (a *aiManagementActions) UpdateConnection(_ context.Context, command aiassist.UpdateConnectionCommand) (aiassist.ProviderConnection, error) {
	a.updated = command
	return aiProvider(command.Connection.ID, command.ExpectedVersion+1), a.err
}
func (a *aiManagementActions) SetConnectionEnabled(_ context.Context, command aiassist.SetConnectionEnabledCommand) (aiassist.ProviderConnection, error) {
	a.enabled = command
	p := aiProvider(command.ID, command.ExpectedVersion+1)
	p.Enabled = command.Enabled
	return p, a.err
}
func (a *aiManagementActions) ReplaceCredential(_ context.Context, command aiassist.ReplaceCredentialCommand) (aiassist.ProviderConnection, error) {
	a.credential = command
	return aiProvider(command.ID, command.ExpectedVersion+1), a.err
}
func (*aiManagementActions) TestConnection(context.Context, aiassist.TestConnectionCommand) (aiassist.ProviderConnection, error) {
	return aiProvider("provider-1", 2), nil
}
func (*aiManagementActions) DiscoverModels(context.Context, aiassist.DiscoverModelsCommand) ([]aiassist.ModelProfile, error) {
	return []aiassist.ModelProfile{aiModel("model-1", 1)}, nil
}
func (*aiManagementActions) ListModels(context.Context, aiassist.ListModelsCommand) ([]aiassist.ModelProfile, error) {
	return []aiassist.ModelProfile{aiModel("model-1", 1)}, nil
}
func (a *aiManagementActions) UpdateModels(_ context.Context, command aiassist.UpdateModelsCommand) ([]aiassist.ModelProfile, error) {
	a.models = command
	if len(command.Updates) == 1 {
		model := command.Updates[0].Model
		model.Version = command.Updates[0].ExpectedVersion + 1
		return []aiassist.ModelProfile{model}, a.err
	}
	return []aiassist.ModelProfile{aiModel("model-1", 2)}, a.err
}
func (*aiManagementActions) GetPolicy(context.Context, aiassist.GetPolicyCommand) (aiassist.Policy, error) {
	return aiPolicy(1), nil
}
func (a *aiManagementActions) UpdatePolicy(_ context.Context, command aiassist.UpdatePolicyCommand) (aiassist.Policy, error) {
	a.policy = command
	policy := command.Policy
	policy.Version = command.ExpectedVersion + 1
	return policy, a.err
}

type aiJobActions struct {
	submitted aiassist.SubmitCommand
	cancelled aiassist.CancelCommand
	retried   aiassist.RetryCommand
	getErr    error
	calls     int
	err       error
}

type aiRecommendationActions struct {
	gotID string
	err   error
}

type calendarRecommendationActions struct {
	command aiassist.RecommendCalendarCommand
	calls   int
}

func (a *calendarRecommendationActions) Recommend(_ context.Context, command aiassist.RecommendCalendarCommand) (aiassist.CalendarRecommendation, error) {
	a.calls++
	a.command = command
	return aiassist.CalendarRecommendation{Candidates: []aiassist.RecommendationCandidate{{TechnicianID: "tech-1", Explanation: "Balanced", PreviewRequest: aiassist.CalendarPreviewRequest{ProjectionID: command.ProjectionID}}}}, nil
}

func (a *aiRecommendationActions) Get(
	_ context.Context,
	_ authorization.Principal,
	id string,
) (aiassist.RecommendationView, error) {
	a.gotID = id
	return aiassist.RecommendationView{
		ID: "recommendation-1", JobID: "job-1", WorkRecordID: "work-1",
		Feature: aiassist.FeatureSummary, Text: "A plain-text draft.",
		CandidateIDs: []string{"work-2"}, RelevantInputs: []string{"title", "description"},
		State:       aiassist.RecommendationPendingHuman,
		GeneratedAt: time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC), Version: 1,
	}, a.err
}

func (a *aiJobActions) Submit(_ context.Context, command aiassist.SubmitCommand) (aiassist.GenerationJob, error) {
	a.calls++
	a.submitted = command
	return aiJob("job-1", 1), a.err
}
func (a *aiJobActions) Get(context.Context, authorization.Principal, string) (aiassist.GenerationJob, error) {
	a.calls++
	return aiJob("job-1", 1), a.getErr
}
func (a *aiJobActions) Cancel(_ context.Context, command aiassist.CancelCommand) (aiassist.GenerationJob, error) {
	a.calls++
	a.cancelled = command
	return aiJob(command.JobID, command.ExpectedVersion+1), a.err
}
func (a *aiJobActions) Retry(_ context.Context, command aiassist.RetryCommand) (aiassist.GenerationJob, error) {
	a.calls++
	a.retried = command
	return aiJob(command.JobID, command.ExpectedVersion+1), a.err
}

func aiJSONRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func aiManagerPrincipal(*http.Request) (authorization.Principal, error) {
	return authorization.Principal{ID: "manager", Scope: scope.Principal{MSPID: "msp-1"}, Capabilities: authorization.NewCapabilitySet("ai.manage")}, nil
}
func aiTechnicianPrincipal(*http.Request) (authorization.Principal, error) {
	return authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp-1", ClientID: "client-1"}, Capabilities: authorization.NewCapabilitySet("ai.assist")}, nil
}
func aiProvider(id string, version int64) aiassist.ProviderConnection {
	return aiassist.ProviderConnection{ID: id, Name: "Local Ollama", BaseURL: "http://127.0.0.1:11434", Adapter: aiassist.AdapterOllama, Network: aiassist.NetworkLocal, CredentialConfigured: true, Timeout: 15 * time.Minute, RequestLimitBytes: 1 << 20, ResponseLimitBytes: 5 << 20, Enabled: false, Health: aiassist.HealthPending, Version: version}
}
func aiModel(id string, version int64) aiassist.ModelProfile {
	return aiassist.ModelProfile{ID: id, ConnectionID: "provider-1", ProviderModelID: "llama", DisplayName: "Llama", SupportedFeatures: []aiassist.Feature{aiassist.FeatureSummary}, ContextLimit: 4096, OutputLimit: 1024, Version: version}
}
func aiPolicy(version int64) aiassist.Policy {
	return aiassist.Policy{MSPID: "msp-1", Version: version}
}
func aiJob(id string, version int64) aiassist.GenerationJob {
	return aiassist.GenerationJob{ID: id, MSPID: "msp-1", ClientID: "client-1", WorkRecordID: "work-1", RequestedBy: "tech", Feature: aiassist.FeatureSummary, State: aiassist.JobQueued, Version: version}
}

func TestCreateAIProviderAcceptsWriteOnlyCredential(t *testing.T) {
	actions := &aiManagementActions{}
	handler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: actions})
	request := aiJSONRequest(http.MethodPost, "/api/v1/ai/providers", `{"name":"Local Ollama","adapter":"ollama","network_mode":"local","base_url":"http://127.0.0.1:11434","credential":"synthetic","local_network_acknowledged":true,"reason":"initial configuration"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if actions.credentialText != "synthetic" || !actions.created.AcknowledgeLocalNetwork {
		t.Fatalf("create command=%+v", actions.created)
	}
	if strings.Contains(response.Body.String(), "synthetic") || strings.Contains(response.Body.String(), "credential_ciphertext") {
		t.Fatal("provider response exposed credential material")
	}
	if response.Header().Get("ETag") != `"1"` {
		t.Fatalf("ETag=%q", response.Header().Get("ETag"))
	}
}

func TestCalendarRecommendationRouteRequiresCapabilityAndMapsTrustedPrincipal(t *testing.T) {
	const projectionID = "00000000-0000-4000-8000-000000000301"
	actions := &calendarRecommendationActions{}
	principal := func(*http.Request) (authorization.Principal, error) {
		return authorization.Principal{ID: "planner", Scope: scope.Principal{MSPID: "msp-1"}, Capabilities: authorization.NewCapabilitySet("calendar.ai.recommend")}, nil
	}
	handler := NewRouter(Dependencies{Principal: principal, CalendarAIRecommendations: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, aiJSONRequest(http.MethodPost, "/api/v1/ai/calendar/recommendations", `{"projection_id":"`+projectionID+`"}`))
	if response.Code != http.StatusOK || actions.calls != 1 || actions.command.Principal.ID != "planner" || actions.command.ProjectionID != projectionID {
		t.Fatalf("status=%d calls=%d command=%+v body=%s", response.Code, actions.calls, actions.command, response.Body.String())
	}

	denied := NewRouter(Dependencies{Principal: aiManagerPrincipal, CalendarAIRecommendations: actions})
	response = httptest.NewRecorder()
	denied.ServeHTTP(response, aiJSONRequest(http.MethodPost, "/api/v1/ai/calendar/recommendations", `{"projection_id":"`+projectionID+`"}`))
	if response.Code != http.StatusForbidden || actions.calls != 1 {
		t.Fatalf("denied status=%d calls=%d", response.Code, actions.calls)
	}
}

func TestCalendarRecommendationRouteRejectsUnknownFields(t *testing.T) {
	actions := &calendarRecommendationActions{}
	principal := func(*http.Request) (authorization.Principal, error) {
		return authorization.Principal{ID: "planner", Scope: scope.Principal{MSPID: "msp-1"}, Capabilities: authorization.NewCapabilitySet("calendar.ai.recommend")}, nil
	}
	handler := NewRouter(Dependencies{Principal: principal, CalendarAIRecommendations: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, aiJSONRequest(http.MethodPost, "/api/v1/ai/calendar/recommendations", `{"projection_id":"event-1","apply":true}`))
	if response.Code != http.StatusBadRequest || actions.calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, actions.calls, response.Body.String())
	}
}

func TestCalendarRecommendationRouteRejectsMalformedProjectionIDBeforeAction(t *testing.T) {
	principal := func(*http.Request) (authorization.Principal, error) {
		return authorization.Principal{ID: "planner", Scope: scope.Principal{MSPID: "msp-1"}, Capabilities: authorization.NewCapabilitySet("calendar.ai.recommend")}, nil
	}
	for _, projectionID := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000", "00000000-0000-4000-8000-00000000000A", "urn:uuid:00000000-0000-4000-8000-000000000001"} {
		actions := &calendarRecommendationActions{}
		response := httptest.NewRecorder()
		NewRouter(Dependencies{Principal: principal, CalendarAIRecommendations: actions}).ServeHTTP(response, aiJSONRequest(http.MethodPost, "/api/v1/ai/calendar/recommendations", `{"projection_id":"`+projectionID+`"}`))
		if response.Code != http.StatusBadRequest || actions.calls != 0 {
			t.Fatalf("id=%q status=%d calls=%d body=%s", projectionID, response.Code, actions.calls, response.Body.String())
		}
	}
}

func TestAIRoutesRejectUnknownTrailingAndOversizeJSON(t *testing.T) {
	handler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: &aiManagementActions{}})
	for _, body := range []string{
		`{"name":"x","unknown":true}`,
		`{"name":"x"}{}`,
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, aiJSONRequest(http.MethodPost, "/api/v1/ai/providers", body))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%q status=%d want 400", body, response.Code)
		}
	}
	response := httptest.NewRecorder()
	payload := append([]byte(`{"name":"`), bytes.Repeat([]byte("x"), 1<<20)...)
	payload = append(payload, []byte(`"}`)...)
	overSize := httptest.NewRequest(http.MethodPost, "/api/v1/ai/providers", bytes.NewReader(payload))
	overSize.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, overSize)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status=%d body=%s", response.Code, response.Body.String())
	}
	wrongContentType := httptest.NewRequest(http.MethodPost, "/api/v1/ai/providers", strings.NewReader(`{}`))
	wrongContentType.Header.Set("Content-Type", "text/plain")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, wrongContentType)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAIProviderPatchUsesDedicatedEnableActionAndETagVersion(t *testing.T) {
	actions := &aiManagementActions{}
	handler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: actions})
	request := aiJSONRequest(http.MethodPatch, "/api/v1/ai/providers/provider-1", `{"enabled":true,"reason":"approved"}`)
	request.Header.Set("If-Match", `"4"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || actions.enabled.ExpectedVersion != 4 || !actions.enabled.Enabled || actions.enabled.Reason != "approved" {
		t.Fatalf("status=%d command=%+v", response.Code, actions.enabled)
	}
	if response.Header().Get("ETag") != `"5"` {
		t.Fatalf("ETag=%q", response.Header().Get("ETag"))
	}
}

func TestAIProviderPatchMapsReferencedProviderConflict(t *testing.T) {
	actions := &aiManagementActions{err: aiassist.ErrProviderInUse}
	handler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: actions})
	request := aiJSONRequest(http.MethodPatch, "/api/v1/ai/providers/provider-1", `{"name":"Renamed","adapter":"ollama","network_mode":"local","base_url":"http://127.0.0.1:11434","timeout_seconds":900,"request_limit_bytes":1048576,"response_limit_bytes":5242880,"local_network_acknowledged":true,"reason":"move endpoint","expected_version":1}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body ErrorResponse
	_ = json.NewDecoder(response.Body).Decode(&body)
	if body.Error.Code != "provider_in_use" {
		t.Fatalf("error=%+v", body.Error)
	}
}

func TestAISubmissionUsesServerScopeAndIdempotencyHeader(t *testing.T) {
	actions := &aiJobActions{}
	handler := NewRouter(Dependencies{Principal: aiTechnicianPrincipal, AIJobs: actions})
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, aiJSONRequest(http.MethodPost, "/api/v1/work-records/work-1/ai/jobs", `{"feature":"summary"}`))
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency status=%d", missing.Code)
	}
	request := aiJSONRequest(http.MethodPost, "/api/v1/work-records/work-1/ai/jobs", `{"feature":"summary"}`)
	request.Header.Set("Idempotency-Key", "request-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || actions.submitted.WorkRecordID != "work-1" || actions.submitted.IdempotencyKey != "request-1" || actions.submitted.Principal.Scope.ClientID != "client-1" {
		t.Fatalf("status=%d command=%+v", response.Code, actions.submitted)
	}
	if response.Header().Get("Location") != "/api/v1/ai/jobs/job-1" || response.Header().Get("Retry-After") == "" {
		t.Fatalf("headers=%v", response.Header())
	}
}

func TestAIJobMutationRequiresReasonVersionAndMapsErrors(t *testing.T) {
	actions := &aiJobActions{err: aiassist.ErrJobNotRetryable}
	handler := NewRouter(Dependencies{Principal: aiTechnicianPrincipal, AIJobs: actions})
	request := aiJSONRequest(http.MethodPost, "/api/v1/ai/jobs/job-1/retry", `{"reason":"try again"}`)
	request.Header.Set("If-Match", `"2"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body ErrorResponse
	_ = json.NewDecoder(response.Body).Decode(&body)
	if body.Error.Code != "job_not_retryable" {
		t.Fatalf("error=%+v", body.Error)
	}
	actions.err = object.ErrVersionConflict
	request = aiJSONRequest(http.MethodPost, "/api/v1/ai/jobs/job-1/cancel", `{"reason":"stop","expected_version":2}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("version conflict status=%d", response.Code)
	}
}

func TestGetAIRecommendationReturnsOnlyTechnicianSafeReviewFields(t *testing.T) {
	actions := &aiRecommendationActions{}
	handler := NewRouter(Dependencies{Principal: aiTechnicianPrincipal, AIRecommendations: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/ai/recommendations/recommendation-1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if actions.gotID != "recommendation-1" || response.Header().Get("ETag") != `"1"` {
		t.Fatalf("lookup=%q ETag=%q", actions.gotID, response.Header().Get("ETag"))
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["job_id"] != "job-1" || body["text"] != "A plain-text draft." || body["state"] != "pending_human" {
		t.Fatalf("response=%+v", body)
	}
	for _, forbidden := range []string{"provider", "model", "prompt_version", "context", "credential", "raw_response"} {
		if _, found := body[forbidden]; found {
			t.Fatalf("response exposed %s: %+v", forbidden, body)
		}
	}
}

func TestAIProviderErrorsAreSafe(t *testing.T) {
	actions := &aiManagementActions{err: errors.New("provider said credential synthetic invalid")}
	handler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, aiJSONRequest(http.MethodPost, "/api/v1/ai/providers", `{"name":"Remote","adapter":"openai_compatible","network_mode":"remote","base_url":"https://models.example.test","reason":"initial configuration"}`))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "synthetic") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAIManagementAndJobRoutesEnforceCapabilitiesBeforeActions(t *testing.T) {
	managerActions := &aiManagementActions{}
	managerHandler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{ID: "reader", Scope: scope.Principal{MSPID: "msp-1"}}, nil
		},
		AIManagement: managerActions,
	})
	managerResponse := httptest.NewRecorder()
	managerHandler.ServeHTTP(managerResponse, httptest.NewRequest(http.MethodGet, "/api/v1/ai/providers", nil))
	if managerResponse.Code != http.StatusForbidden {
		t.Fatalf("management status=%d", managerResponse.Code)
	}
	jobActions := &aiJobActions{}
	jobHandler := NewRouter(Dependencies{
		Principal: func(*http.Request) (authorization.Principal, error) {
			return authorization.Principal{ID: "reader", Scope: scope.Principal{MSPID: "msp-1", ClientID: "client-1"}}, nil
		},
		AIJobs: jobActions,
	})
	jobResponse := httptest.NewRecorder()
	jobHandler.ServeHTTP(jobResponse, httptest.NewRequest(http.MethodGet, "/api/v1/ai/jobs/job-1", nil))
	if jobResponse.Code != http.StatusForbidden {
		t.Fatalf("job status=%d", jobResponse.Code)
	}
}

func TestAIRequestBodiesRequireJSONObjectContentAndRequiredProviderFields(t *testing.T) {
	handler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: &aiManagementActions{}})
	missingType := httptest.NewRequest(http.MethodPost, "/api/v1/ai/providers", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, missingType)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing Content-Type status=%d body=%s", response.Code, response.Body.String())
	}
	for _, contentType := range []string{"invalid", "text/plain"} {
		request := aiJSONRequest(http.MethodPost, "/api/v1/ai/providers", `{}`)
		request.Header.Set("Content-Type", contentType)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("Content-Type %q status=%d", contentType, response.Code)
		}
	}
	caseInsensitive := aiJSONRequest(http.MethodPost, "/api/v1/ai/providers", `{}`)
	caseInsensitive.Header.Set("Content-Type", "APPLICATION/JSON; charset=utf-8")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, caseInsensitive)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("case-insensitive JSON status=%d body=%s", response.Code, response.Body.String())
	}
	for _, body := range []string{"", "null", "[]", `"x"`, "1", `{}`} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, aiJSONRequest(http.MethodPost, "/api/v1/ai/providers", body))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q status=%d body=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestAIProviderCreateRequiresReasonAndRejectsMixedEnablePatch(t *testing.T) {
	actions := &aiManagementActions{}
	handler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, aiJSONRequest(http.MethodPost, "/api/v1/ai/providers", `{"name":"Remote","adapter":"openai_compatible","network_mode":"remote","base_url":"https://models.example.test"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing create reason status=%d", response.Code)
	}
	request := aiJSONRequest(http.MethodPatch, "/api/v1/ai/providers/provider-1", `{"enabled":true,"name":"renamed","reason":"approved","expected_version":1}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || actions.enabled.ID != "" || actions.updated.Connection.ID != "" {
		t.Fatalf("mixed enable status=%d enabled=%+v update=%+v", response.Code, actions.enabled, actions.updated)
	}
	emptyConfig := aiJSONRequest(http.MethodPatch, "/api/v1/ai/providers/provider-1", `{"enabled":true,"name":"","reason":"approved","expected_version":1}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, emptyConfig)
	if response.Code != http.StatusBadRequest || actions.enabled.ID != "" || actions.updated.Connection.ID != "" {
		t.Fatalf("mixed empty config status=%d enabled=%+v update=%+v", response.Code, actions.enabled, actions.updated)
	}
}

func TestAIRejectsMalformedVersionETags(t *testing.T) {
	for _, value := range []string{"4", `W/"4"`, `"4","5"`, `"nope"`, `"0"`, `"-1"`} {
		t.Run(value, func(t *testing.T) {
			actions := &aiManagementActions{}
			handler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: actions})
			request := aiJSONRequest(http.MethodPatch, "/api/v1/ai/providers/provider-1", `{"enabled":true,"reason":"approved"}`)
			request.Header.Set("If-Match", value)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || actions.enabled.ID != "" {
				t.Fatalf("If-Match=%q status=%d command=%+v", value, response.Code, actions.enabled)
			}
		})
	}
	policyActions := &aiManagementActions{}
	policyHandler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: policyActions})
	policyRequest := aiJSONRequest(http.MethodPatch, "/api/v1/ai/policy", `{"enabled":false,"reason":"disable"}`)
	policyRequest.Header.Set("If-Match", `W/"1"`)
	policyResponse := httptest.NewRecorder()
	policyHandler.ServeHTTP(policyResponse, policyRequest)
	if policyResponse.Code != http.StatusBadRequest || policyActions.policy.Reason != "" {
		t.Fatalf("malformed policy If-Match status=%d command=%+v", policyResponse.Code, policyActions.policy)
	}
	modelRequest := aiJSONRequest(http.MethodPatch, "/api/v1/ai/providers/provider-1/models", `{"reason":"price update","updates":[{"id":"model-1","expected_version":1,"display_name":"Llama","supported_features":["summary"],"context_limit":4096,"output_limit":1024}]}`)
	modelRequest.Header.Set("If-Match", `W/"1"`)
	modelResponse := httptest.NewRecorder()
	policyHandler.ServeHTTP(modelResponse, modelRequest)
	if modelResponse.Code != http.StatusBadRequest || len(policyActions.models.Updates) != 0 {
		t.Fatalf("malformed model If-Match status=%d command=%+v", modelResponse.Code, policyActions.models)
	}
	jobActions := &aiJobActions{}
	jobHandler := NewRouter(Dependencies{Principal: aiTechnicianPrincipal, AIJobs: jobActions})
	jobRequest := aiJSONRequest(http.MethodPost, "/api/v1/ai/jobs/job-1/cancel", `{"reason":"stop"}`)
	jobRequest.Header.Set("If-Match", `W/"1"`)
	jobResponse := httptest.NewRecorder()
	jobHandler.ServeHTTP(jobResponse, jobRequest)
	if jobResponse.Code != http.StatusBadRequest || jobActions.calls != 0 {
		t.Fatalf("malformed job If-Match status=%d calls=%d", jobResponse.Code, jobActions.calls)
	}
}

func TestAIJobsRequireActiveClientScopeBeforeRepositoryActions(t *testing.T) {
	principal := func(*http.Request) (authorization.Principal, error) {
		return authorization.Principal{ID: "tech", Scope: scope.Principal{MSPID: "msp-1"}, Capabilities: authorization.NewCapabilitySet("ai.assist")}, nil
	}
	for _, test := range []struct{ method, target, body string }{
		{http.MethodPost, "/api/v1/work-records/work-1/ai/jobs", `{"feature":"summary"}`},
		{http.MethodGet, "/api/v1/ai/jobs/job-1", ""},
		{http.MethodPost, "/api/v1/ai/jobs/job-1/cancel", `{"reason":"stop","expected_version":1}`},
		{http.MethodPost, "/api/v1/ai/jobs/job-1/retry", `{"reason":"retry","expected_version":1}`},
	} {
		t.Run(test.method+test.target, func(t *testing.T) {
			actions := &aiJobActions{}
			handler := NewRouter(Dependencies{Principal: principal, AIJobs: actions})
			request := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			if strings.Contains(test.target, "/work-records/") {
				request.Header.Set("Idempotency-Key", "request-1")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden || actions.calls != 0 {
				t.Fatalf("status=%d calls=%d", response.Code, actions.calls)
			}
		})
	}
}

func TestAIJobLookupAndMethodsAreEnumerationSafe(t *testing.T) {
	actions := &aiJobActions{getErr: scope.ErrNotFound}
	handler := NewRouter(Dependencies{Principal: aiTechnicianPrincipal, AIJobs: actions})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/ai/jobs/other-msp-job", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("enumeration status=%d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/ai/jobs/job-1", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status=%d", response.Code)
	}
}

func TestAIModelPricingAndPolicyCapDTOsRoundTrip(t *testing.T) {
	inputPrice, outputPrice := int64(15), int64(30)
	actions := &aiManagementActions{}
	handler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: actions})
	modelRequest := aiJSONRequest(http.MethodPatch, "/api/v1/ai/providers/provider-1/models", `{"reason":"price update","updates":[{"id":"model-1","expected_version":1,"display_name":"Llama","supported_features":["summary"],"context_limit":4096,"output_limit":1024,"input_cost_per_million_minor":15,"output_cost_per_million_minor":30,"enabled":true}]}`)
	modelResponse := httptest.NewRecorder()
	handler.ServeHTTP(modelResponse, modelRequest)
	if modelResponse.Code != http.StatusOK || len(actions.models.Updates) != 1 || actions.models.Updates[0].Model.InputCostPerMillionMinor == nil || *actions.models.Updates[0].Model.InputCostPerMillionMinor != inputPrice || *actions.models.Updates[0].Model.OutputCostPerMillionMinor != outputPrice {
		t.Fatalf("model status=%d command=%+v", modelResponse.Code, actions.models)
	}
	var returnedModels []AIModelResponse
	if err := json.NewDecoder(modelResponse.Body).Decode(&returnedModels); err != nil || len(returnedModels) != 1 || returnedModels[0].InputCostPerMillionMinor == nil || *returnedModels[0].InputCostPerMillionMinor != inputPrice || returnedModels[0].OutputCostPerMillionMinor == nil || *returnedModels[0].OutputCostPerMillionMinor != outputPrice {
		t.Fatalf("model response=%+v error=%v", returnedModels, err)
	}
	policyRequest := aiJSONRequest(http.MethodPatch, "/api/v1/ai/policy", `{"enabled":false,"cost_limit_enabled":true,"allow_unmetered_unknown":true,"monthly_cost_limit_minor":12500,"reason":"set monthly cap"}`)
	policyResponse := httptest.NewRecorder()
	handler.ServeHTTP(policyResponse, policyRequest)
	if policyResponse.Code != http.StatusOK || actions.policy.Policy.MonthlyCostLimitMinor != 12500 || !actions.policy.Policy.CostLimitEnabled || !actions.policy.Policy.AllowUnmeteredUnknown {
		t.Fatalf("policy status=%d command=%+v", policyResponse.Code, actions.policy)
	}
	var returnedPolicy AIPolicyResponse
	if err := json.NewDecoder(policyResponse.Body).Decode(&returnedPolicy); err != nil || returnedPolicy.MonthlyCostLimitMinor != 12500 || !returnedPolicy.CostLimitEnabled || !returnedPolicy.AllowUnmeteredUnknown {
		t.Fatalf("policy response=%+v error=%v", returnedPolicy, err)
	}
}

func TestAIPolicyResponseEncodesEmptyAllowedFeaturesAsArray(t *testing.T) {
	body, err := json.Marshal(policyResponse(aiassist.Policy{}))
	if err != nil {
		t.Fatalf("marshal policy response: %v", err)
	}
	if !strings.Contains(string(body), `"allowed_features":[]`) {
		t.Fatalf("empty allowed features must be an array: %s", body)
	}
}

func TestAIPolicyCalendarRecommendationModelMapping(t *testing.T) {
	request := AIPolicyRequest{
		Enabled: true, ProviderDisclosureAccepted: true, PromptVersion: "calendar-v1",
		AllowedFeatures:                      []aiassist.Feature{aiassist.FeatureCalendarRecommendation},
		CalendarRecommendationModelProfileID: "calendar-model",
	}
	policy := policyFromRequest(request)
	if policy.CalendarRecommendationModelProfileID != "calendar-model" || len(policy.AllowedFeatures) != 1 {
		t.Fatalf("policyFromRequest()=%+v", policy)
	}
	response := policyResponse(policy)
	if response.CalendarRecommendationModelProfileID != "calendar-model" {
		t.Fatalf("policyResponse()=%+v", response)
	}
}

func TestAIJobAndModelMutationsRejectEmptyRequiredFields(t *testing.T) {
	jobActions := &aiJobActions{}
	jobHandler := NewRouter(Dependencies{Principal: aiTechnicianPrincipal, AIJobs: jobActions})
	jobRequest := aiJSONRequest(http.MethodPost, "/api/v1/work-records/work-1/ai/jobs", `{}`)
	jobRequest.Header.Set("Idempotency-Key", "request-1")
	jobResponse := httptest.NewRecorder()
	jobHandler.ServeHTTP(jobResponse, jobRequest)
	if jobResponse.Code != http.StatusBadRequest || jobActions.calls != 0 {
		t.Fatalf("empty feature status=%d calls=%d", jobResponse.Code, jobActions.calls)
	}
	managementActions := &aiManagementActions{}
	managementHandler := NewRouter(Dependencies{Principal: aiManagerPrincipal, AIManagement: managementActions})
	modelResponse := httptest.NewRecorder()
	managementHandler.ServeHTTP(modelResponse, aiJSONRequest(http.MethodPatch, "/api/v1/ai/providers/provider-1/models", `{"reason":"update","updates":[]}`))
	if modelResponse.Code != http.StatusBadRequest || len(managementActions.models.Updates) != 0 {
		t.Fatalf("empty model updates status=%d updates=%+v", modelResponse.Code, managementActions.models.Updates)
	}
}
