package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/aiassist"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	internalid "github.com/rarity-ticket-intelligence/rarity/backend/internal/id"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/scope"
)

func (r *Router) registerAIRoutes() {
	r.mux.HandleFunc("POST /api/v1/ai/calendar/recommendations", r.recommendCalendarSchedule)
	r.mux.HandleFunc("GET /api/v1/ai/providers", r.listAIProviders)
	r.mux.HandleFunc("POST /api/v1/ai/providers", r.createAIProvider)
	r.mux.HandleFunc("PATCH /api/v1/ai/providers/{id}", r.patchAIProvider)
	r.mux.HandleFunc("POST /api/v1/ai/providers/{id}/credential", r.replaceAIProviderCredential)
	r.mux.HandleFunc("POST /api/v1/ai/providers/{id}/test", r.testAIProvider)
	r.mux.HandleFunc("POST /api/v1/ai/providers/{id}/discover-models", r.discoverAIProviderModels)
	r.mux.HandleFunc("GET /api/v1/ai/providers/{id}/models", r.listAIProviderModels)
	r.mux.HandleFunc("PATCH /api/v1/ai/providers/{id}/models", r.patchAIProviderModels)
	r.mux.HandleFunc("GET /api/v1/ai/policy", r.getAIPolicy)
	r.mux.HandleFunc("PATCH /api/v1/ai/policy", r.patchAIPolicy)
	r.mux.HandleFunc("POST /api/v1/work-records/{id}/ai/jobs", r.submitAIJob)
	r.mux.HandleFunc("GET /api/v1/ai/jobs/{id}", r.getAIJob)
	r.mux.HandleFunc("POST /api/v1/ai/jobs/{id}/cancel", r.cancelAIJob)
	r.mux.HandleFunc("POST /api/v1/ai/jobs/{id}/retry", r.retryAIJob)
	r.mux.HandleFunc("GET /api/v1/ai/recommendations/{id}", r.getAIRecommendation)
	r.mux.HandleFunc("POST /api/v1/ai/recommendations/{id}/decide", r.decideAIRecommendation)
}

type calendarRecommendationRequest struct {
	ProjectionID string `json:"projection_id"`
}

func (r *Router) recommendCalendarSchedule(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if err := authorization.Authorize(principal, "calendar.ai.recommend", scope.Target{MSPID: principal.Scope.MSPID}); err != nil {
		writeDomainError(writer, request, err)
		return
	}
	if r.dependencies.CalendarAIRecommendations == nil {
		aiUnavailable(writer, request, "calendar recommendation service")
		return
	}
	var body calendarRecommendationRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	if !internalid.ValidCanonical(body.ProjectionID) {
		aiValidation(writer, request, "projection_id must be a canonical UUID")
		return
	}
	result, err := r.dependencies.CalendarAIRecommendations.Recommend(request.Context(), aiassist.RecommendCalendarCommand{Principal: principal, ProjectionID: body.ProjectionID})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) listAIProviders(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIManagement(writer, request, principal) {
		return
	}
	if r.dependencies.AIManagement == nil {
		aiUnavailable(writer, request, "AI management service")
		return
	}
	connections, err := r.dependencies.AIManagement.ListConnections(request.Context(), aiassist.ListConnectionsCommand{Principal: principal})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	result := make([]AIProviderResponse, 0, len(connections))
	for _, connection := range connections {
		result = append(result, providerResponse(connection))
	}
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) createAIProvider(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIManagement(writer, request, principal) {
		return
	}
	if r.dependencies.AIManagement == nil {
		aiUnavailable(writer, request, "AI management service")
		return
	}
	var body AIProviderCreateRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	if strings.TrimSpace(body.Name) == "" || body.Adapter == "" || body.NetworkMode == "" || strings.TrimSpace(body.BaseURL) == "" || strings.TrimSpace(body.Reason) == "" {
		aiValidation(writer, request, "provider name, adapter, network mode, base URL, and reason are required")
		return
	}
	credential := []byte(body.Credential)
	body.Credential = ""
	defer wipeBytes(credential)
	provider, err := r.dependencies.AIManagement.CreateConnection(request.Context(), aiassist.CreateConnectionCommand{
		Principal: principal, Connection: providerFromCreate(body), PlaintextCredential: credential,
		AcknowledgeLocalNetwork: body.LocalNetworkAcknowledged, Reason: body.Reason,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, provider.Version)
	writeJSON(writer, http.StatusCreated, providerResponse(provider))
}

func (r *Router) patchAIProvider(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIManagement(writer, request, principal) {
		return
	}
	if r.dependencies.AIManagement == nil {
		aiUnavailable(writer, request, "AI management service")
		return
	}
	var body AIProviderPatchRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	expected, ok := aiExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok || strings.TrimSpace(body.Reason) == "" {
		if ok {
			aiValidation(writer, request, "reason is required")
		}
		return
	}
	if body.Enabled != nil && providerPatchHasConfiguration(body) {
		aiValidation(writer, request, "enabled must be changed separately from provider configuration")
		return
	}
	var provider aiassist.ProviderConnection
	var err error
	if body.Enabled != nil {
		provider, err = r.dependencies.AIManagement.SetConnectionEnabled(request.Context(), aiassist.SetConnectionEnabledCommand{Principal: principal, ID: request.PathValue("id"), ExpectedVersion: expected, Enabled: *body.Enabled, Reason: body.Reason})
	} else {
		provider, err = r.dependencies.AIManagement.UpdateConnection(request.Context(), aiassist.UpdateConnectionCommand{Principal: principal, Connection: providerFromPatch(request.PathValue("id"), body), ExpectedVersion: expected, Reason: body.Reason, AcknowledgeLocalNetwork: body.LocalNetworkAcknowledged != nil && *body.LocalNetworkAcknowledged})
	}
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, provider.Version)
	writeJSON(writer, http.StatusOK, providerResponse(provider))
}

func (r *Router) replaceAIProviderCredential(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIManagement(writer, request, principal) {
		return
	}
	if r.dependencies.AIManagement == nil {
		aiUnavailable(writer, request, "AI management service")
		return
	}
	var body AICredentialReplaceRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	expected, ok := aiExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok || strings.TrimSpace(body.Reason) == "" {
		if ok {
			aiValidation(writer, request, "reason is required")
		}
		return
	}
	credential := []byte(body.Credential)
	body.Credential = ""
	defer wipeBytes(credential)
	provider, err := r.dependencies.AIManagement.ReplaceCredential(request.Context(), aiassist.ReplaceCredentialCommand{Principal: principal, ID: request.PathValue("id"), ExpectedVersion: expected, PlaintextCredential: credential, Reason: body.Reason})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, provider.Version)
	writeJSON(writer, http.StatusOK, providerResponse(provider))
}

func (r *Router) testAIProvider(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIManagement(writer, request, principal) {
		return
	}
	if r.dependencies.AIManagement == nil {
		aiUnavailable(writer, request, "AI management service")
		return
	}
	var body AIProviderTestRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	provider, err := r.dependencies.AIManagement.TestConnection(request.Context(), aiassist.TestConnectionCommand{Principal: principal, ID: request.PathValue("id")})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, provider.Version)
	writeJSON(writer, http.StatusOK, providerResponse(provider))
}

func (r *Router) discoverAIProviderModels(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIManagement(writer, request, principal) {
		return
	}
	if r.dependencies.AIManagement == nil {
		aiUnavailable(writer, request, "AI management service")
		return
	}
	var body AIModelDiscoveryRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	expected, ok := aiExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok || strings.TrimSpace(body.Reason) == "" {
		if ok {
			aiValidation(writer, request, "reason is required")
		}
		return
	}
	models, err := r.dependencies.AIManagement.DiscoverModels(request.Context(), aiassist.DiscoverModelsCommand{Principal: principal, ID: request.PathValue("id"), ExpectedVersion: expected, Reason: body.Reason})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, modelResponses(models))
}

func (r *Router) listAIProviderModels(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIManagement(writer, request, principal) {
		return
	}
	if r.dependencies.AIManagement == nil {
		aiUnavailable(writer, request, "AI management service")
		return
	}
	models, err := r.dependencies.AIManagement.ListModels(request.Context(), aiassist.ListModelsCommand{Principal: principal, ConnectionID: request.PathValue("id")})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, modelResponses(models))
}

func (r *Router) patchAIProviderModels(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIManagement(writer, request, principal) {
		return
	}
	if r.dependencies.AIManagement == nil {
		aiUnavailable(writer, request, "AI management service")
		return
	}
	var body AIModelsPatchRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	if version, present, err := parseExpectedVersionETag(request); err != nil || (present && (len(body.Updates) != 1 || body.Updates[0].ExpectedVersion != version)) {
		aiValidation(writer, request, "If-Match must match the single model update version")
		return
	}
	if strings.TrimSpace(body.Reason) == "" || len(body.Updates) == 0 {
		aiValidation(writer, request, "reason and at least one model update are required")
		return
	}
	updates := make([]aiassist.ModelUpdate, 0, len(body.Updates))
	for _, update := range body.Updates {
		updates = append(updates, aiassist.ModelUpdate{ExpectedVersion: update.ExpectedVersion, Model: aiassist.ModelProfile{ID: update.ID, ConnectionID: request.PathValue("id"), DisplayName: update.DisplayName, SupportedFeatures: update.SupportedFeatures, ContextLimit: update.ContextLimit, OutputLimit: update.OutputLimit, ZeroCost: update.ZeroCost, InputCostPerMillionMinor: update.InputCostPerMillionMinor, OutputCostPerMillionMinor: update.OutputCostPerMillionMinor, Enabled: update.Enabled}})
	}
	models, err := r.dependencies.AIManagement.UpdateModels(request.Context(), aiassist.UpdateModelsCommand{Principal: principal, Updates: updates, Reason: body.Reason})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, modelResponses(models))
}

func (r *Router) getAIPolicy(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIManagement(writer, request, principal) {
		return
	}
	if r.dependencies.AIManagement == nil {
		aiUnavailable(writer, request, "AI management service")
		return
	}
	policy, err := r.dependencies.AIManagement.GetPolicy(request.Context(), aiassist.GetPolicyCommand{Principal: principal})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, policy.Version)
	writeJSON(writer, http.StatusOK, policyResponse(policy))
}

func (r *Router) patchAIPolicy(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIManagement(writer, request, principal) {
		return
	}
	if r.dependencies.AIManagement == nil {
		aiUnavailable(writer, request, "AI management service")
		return
	}
	var body AIPolicyRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	expected, ok := aiExpectedVersionAllowZero(writer, request, body.ExpectedVersion)
	if !ok || strings.TrimSpace(body.Reason) == "" {
		if ok {
			aiValidation(writer, request, "reason is required")
		}
		return
	}
	policy, err := r.dependencies.AIManagement.UpdatePolicy(request.Context(), aiassist.UpdatePolicyCommand{Principal: principal, ExpectedVersion: expected, Reason: body.Reason, Policy: policyFromRequest(body)})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, policy.Version)
	writeJSON(writer, http.StatusOK, policyResponse(policy))
}

func (r *Router) submitAIJob(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIAssist(writer, request, principal) {
		return
	}
	if r.dependencies.AIJobs == nil {
		aiUnavailable(writer, request, "AI job service")
		return
	}
	var body AIJobSubmitRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	if body.Feature == "" {
		aiValidation(writer, request, "feature is required")
		return
	}
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if key == "" {
		aiValidation(writer, request, "Idempotency-Key is required")
		return
	}
	job, err := r.dependencies.AIJobs.Submit(request.Context(), aiassist.SubmitCommand{Principal: principal, WorkRecordID: request.PathValue("id"), Feature: body.Feature, IdempotencyKey: key})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writer.Header().Set("Location", "/api/v1/ai/jobs/"+job.ID)
	writer.Header().Set("Retry-After", "3")
	writeETag(writer, job.Version)
	writeJSON(writer, http.StatusAccepted, jobResponse(job))
}

func (r *Router) getAIJob(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIAssist(writer, request, principal) {
		return
	}
	if r.dependencies.AIJobs == nil {
		aiUnavailable(writer, request, "AI job service")
		return
	}
	job, err := r.dependencies.AIJobs.Get(request.Context(), principal, request.PathValue("id"))
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, job.Version)
	writeJSON(writer, http.StatusOK, jobResponse(job))
}

func (r *Router) cancelAIJob(writer http.ResponseWriter, request *http.Request) {
	r.mutateAIJob(writer, request, false)
}
func (r *Router) retryAIJob(writer http.ResponseWriter, request *http.Request) {
	r.mutateAIJob(writer, request, true)
}

func (r *Router) getAIRecommendation(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIAssist(writer, request, principal) {
		return
	}
	if r.dependencies.AIRecommendations == nil {
		aiUnavailable(writer, request, "AI recommendation service")
		return
	}
	recommendation, err := r.dependencies.AIRecommendations.Get(request.Context(), principal, request.PathValue("id"))
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, recommendation.Version)
	writeJSON(writer, http.StatusOK, AIRecommendationResponse{
		ID: recommendation.ID, JobID: recommendation.JobID, ClientID: recommendation.ClientID,
		WorkRecordID: recommendation.WorkRecordID, Feature: recommendation.Feature,
		Text: recommendation.Text, CandidateIDs: append([]string(nil), recommendation.CandidateIDs...),
		Confidence: recommendation.Confidence, RelevantInputs: append([]string(nil), recommendation.RelevantInputs...),
		State: recommendation.State, GeneratedAt: recommendation.GeneratedAt, Version: recommendation.Version,
	})
}

func (r *Router) mutateAIJob(writer http.ResponseWriter, request *http.Request, retry bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIAssist(writer, request, principal) {
		return
	}
	if r.dependencies.AIJobs == nil {
		aiUnavailable(writer, request, "AI job service")
		return
	}
	var body AIJobMutationRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	expected, ok := aiExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok || strings.TrimSpace(body.Reason) == "" {
		if ok {
			aiValidation(writer, request, "reason is required")
		}
		return
	}
	var job aiassist.GenerationJob
	var err error
	if retry {
		job, err = r.dependencies.AIJobs.Retry(request.Context(), aiassist.RetryCommand{Principal: principal, JobID: request.PathValue("id"), ExpectedVersion: expected, Reason: body.Reason})
	} else {
		job, err = r.dependencies.AIJobs.Cancel(request.Context(), aiassist.CancelCommand{Principal: principal, JobID: request.PathValue("id"), ExpectedVersion: expected, Reason: body.Reason})
	}
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, job.Version)
	writeJSON(writer, http.StatusOK, jobResponse(job))
}

func (r *Router) decideAIRecommendation(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !authorizeAIAssist(writer, request, principal) {
		return
	}
	if r.dependencies.AIDecisions == nil {
		aiUnavailable(writer, request, "AI decision service")
		return
	}
	var body AIDecisionRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.AIDecisions.Decide(request.Context(), aiassist.DecisionCommand{Principal: principal, ID: request.PathValue("id"), Decision: body.Decision, Reason: body.Reason})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, AIDecisionResponse{ID: result.ID, State: result.State, Applied: false, Sent: false})
}

func providerFromCreate(body AIProviderCreateRequest) aiassist.ProviderConnection {
	return aiassist.ProviderConnection{Name: body.Name, Adapter: body.Adapter, Network: body.NetworkMode, BaseURL: body.BaseURL, Timeout: time.Duration(body.TimeoutSeconds) * time.Second, RequestLimitBytes: body.RequestLimitBytes, ResponseLimitBytes: body.ResponseLimitBytes}
}
func providerFromPatch(id string, body AIProviderPatchRequest) aiassist.ProviderConnection {
	provider := aiassist.ProviderConnection{ID: id}
	if body.Name != nil {
		provider.Name = *body.Name
	}
	if body.Adapter != nil {
		provider.Adapter = *body.Adapter
	}
	if body.NetworkMode != nil {
		provider.Network = *body.NetworkMode
	}
	if body.BaseURL != nil {
		provider.BaseURL = *body.BaseURL
	}
	if body.TimeoutSeconds != nil {
		provider.Timeout = time.Duration(*body.TimeoutSeconds) * time.Second
	}
	if body.RequestLimitBytes != nil {
		provider.RequestLimitBytes = *body.RequestLimitBytes
	}
	if body.ResponseLimitBytes != nil {
		provider.ResponseLimitBytes = *body.ResponseLimitBytes
	}
	return provider
}
func providerResponse(provider aiassist.ProviderConnection) AIProviderResponse {
	return AIProviderResponse{ID: provider.ID, Name: provider.Name, Adapter: provider.Adapter, NetworkMode: provider.Network, BaseURL: provider.BaseURL, CredentialConfigured: provider.CredentialConfigured, Enabled: provider.Enabled, TimeoutSeconds: int64(provider.Timeout / time.Second), RequestLimitBytes: provider.RequestLimitBytes, ResponseLimitBytes: provider.ResponseLimitBytes, LocalNetworkAcknowledgedAt: provider.LocalNetworkAcknowledgedAt, Health: provider.Health, LastTestedAt: provider.LastTestedAt, LastSucceededAt: provider.LastSucceededAt, LastErrorCode: provider.LastErrorCode, Version: provider.Version}
}
func modelResponses(models []aiassist.ModelProfile) []AIModelResponse {
	result := make([]AIModelResponse, 0, len(models))
	for _, model := range models {
		result = append(result, AIModelResponse{ID: model.ID, ConnectionID: model.ConnectionID, ProviderModelID: model.ProviderModelID, DisplayName: model.DisplayName, SupportedFeatures: append([]aiassist.Feature(nil), model.SupportedFeatures...), ContextLimit: model.ContextLimit, OutputLimit: model.OutputLimit, ZeroCost: model.ZeroCost, InputCostPerMillionMinor: model.InputCostPerMillionMinor, OutputCostPerMillionMinor: model.OutputCostPerMillionMinor, Enabled: model.Enabled, Version: model.Version})
	}
	return result
}
func policyFromRequest(body AIPolicyRequest) aiassist.Policy {
	return aiassist.Policy{Enabled: body.Enabled, ProviderDisclosureAccepted: body.ProviderDisclosureAccepted, PromptVersion: body.PromptVersion, AllowedFeatures: append([]aiassist.Feature(nil), body.AllowedFeatures...), SummaryModelProfileID: body.SummaryModelProfileID, ReplyDraftModelProfileID: body.ReplyDraftModelProfileID, SimilarSuggestionsModelProfileID: body.SimilarSuggestionsModelProfileID, CalendarRecommendationModelProfileID: body.CalendarRecommendationModelProfileID, CostLimitEnabled: body.CostLimitEnabled, AllowUnmeteredUnknown: body.AllowUnmeteredUnknown, MonthlyCostLimitMinor: body.MonthlyCostLimitMinor}
}
func policyResponse(policy aiassist.Policy) AIPolicyResponse {
	return AIPolicyResponse{Enabled: policy.Enabled, ProviderDisclosureAccepted: policy.ProviderDisclosureAccepted, PromptVersion: policy.PromptVersion, AllowedFeatures: append([]aiassist.Feature{}, policy.AllowedFeatures...), SummaryModelProfileID: policy.SummaryModelProfileID, ReplyDraftModelProfileID: policy.ReplyDraftModelProfileID, SimilarSuggestionsModelProfileID: policy.SimilarSuggestionsModelProfileID, ClassificationModelProfileID: policy.ClassificationModelProfileID, CalendarRecommendationModelProfileID: policy.CalendarRecommendationModelProfileID, CostLimitEnabled: policy.CostLimitEnabled, AllowUnmeteredUnknown: policy.AllowUnmeteredUnknown, MonthlyCostLimitMinor: policy.MonthlyCostLimitMinor, CurrentMonthlyCostMinor: policy.CurrentMonthlyCostMinor, Version: policy.Version}
}
func jobResponse(job aiassist.GenerationJob) AIJobResponse {
	return AIJobResponse{ID: job.ID, WorkRecordID: job.WorkRecordID, Feature: job.Feature, ModelProfileID: job.ModelProfileID, State: job.State, Attempt: job.Attempt, MaxAttempts: job.MaxAttempts, CancellationRequestedAt: job.CancellationRequestedAt, SafeErrorCode: job.SafeErrorCode, RecommendationID: job.RecommendationID, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, CompletedAt: job.CompletedAt, Version: job.Version}
}
func wipeBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
func aiUnavailable(writer http.ResponseWriter, request *http.Request, service string) {
	writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", service+" is unavailable")
}
func aiValidation(writer http.ResponseWriter, request *http.Request, message string) {
	writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", message)
}
func aiExpectedVersion(writer http.ResponseWriter, request *http.Request, bodyVersion int64) (int64, bool) {
	return aiExpectedVersionValue(writer, request, bodyVersion, false)
}
func aiExpectedVersionAllowZero(writer http.ResponseWriter, request *http.Request, bodyVersion int64) (int64, bool) {
	return aiExpectedVersionValue(writer, request, bodyVersion, true)
}
func aiExpectedVersionValue(writer http.ResponseWriter, request *http.Request, bodyVersion int64, allowZero bool) (int64, bool) {
	header, present, err := parseExpectedVersionETag(request)
	if err != nil {
		aiValidation(writer, request, "If-Match is invalid")
		return 0, false
	}
	if present && bodyVersion > 0 && header != bodyVersion {
		aiValidation(writer, request, "expected version does not match If-Match")
		return 0, false
	}
	if bodyVersion == 0 && present {
		bodyVersion = header
	}
	if bodyVersion < 0 || (!allowZero && bodyVersion < 1) {
		aiValidation(writer, request, "expected version is required")
		return 0, false
	}
	return bodyVersion, true
}

func providerPatchHasConfiguration(body AIProviderPatchRequest) bool {
	return body.Name != nil || body.Adapter != nil || body.NetworkMode != nil || body.BaseURL != nil ||
		body.TimeoutSeconds != nil || body.RequestLimitBytes != nil || body.ResponseLimitBytes != nil ||
		body.LocalNetworkAcknowledged != nil
}

func authorizeAIManagement(writer http.ResponseWriter, request *http.Request, principal authorization.Principal) bool {
	if err := authorization.Authorize(principal, "ai.manage", scope.Target{MSPID: principal.Scope.MSPID}); err != nil {
		writeDomainError(writer, request, err)
		return false
	}
	return true
}

func authorizeAIAssist(writer http.ResponseWriter, request *http.Request, principal authorization.Principal) bool {
	if strings.TrimSpace(principal.Scope.ClientID) == "" {
		writeError(request.Context(), writer, http.StatusForbidden, "forbidden", "action is not permitted")
		return false
	}
	if err := authorization.Authorize(principal, "ai.assist", targetFor(principal)); err != nil {
		writeDomainError(writer, request, err)
		return false
	}
	return true
}
