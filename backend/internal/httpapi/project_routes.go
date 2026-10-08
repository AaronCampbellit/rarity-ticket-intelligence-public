package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tagging"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/tasks"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/timeentries"
)

func (r *Router) registerProjectRoutes() {
	r.mux.HandleFunc("POST /api/v1/projects", r.createProject)
	r.mux.HandleFunc("GET /api/v1/projects", r.listProjects)
	r.mux.HandleFunc("GET /api/v1/projects/{id}", r.getProject)
	r.mux.HandleFunc("POST /api/v1/projects/{id}/tasks", r.createProjectTask)
	r.mux.HandleFunc("POST /api/v1/projects/{id}/resource-plans", r.createResourcePlan)
	r.mux.HandleFunc("POST /api/v1/projects/{id}/cost-actuals", r.createCostActual)
	r.mux.HandleFunc(
		"POST /api/v1/admin/technicians/{id}/availability",
		r.createTechnicianAvailability,
	)
	r.mux.HandleFunc(
		"POST /api/v1/admin/technicians/{id}/labor-cost-rates",
		r.createTechnicianLaborCostRate,
	)
	r.mux.HandleFunc(
		"POST /api/v1/projects/{id}/billable-work",
		r.recognizeProjectBillableWork,
	)
	r.mux.HandleFunc("POST /api/v1/projects/{id}/change-orders", r.createChangeOrder)
	r.mux.HandleFunc("PATCH /api/v1/phases/{id}", r.updatePhase)
	r.mux.HandleFunc("POST /api/v1/phases/{id}/tasks", r.createPhaseTask)
	r.mux.HandleFunc("POST /api/v1/tasks/{id}/time-entries", r.createProjectTaskTimeEntry)
	r.mux.HandleFunc("GET /api/v1/tasks/{id}", r.getTask)
	r.mux.HandleFunc("POST /api/v1/change-orders/{id}/versions", r.issueChangeOrderVersion)
	r.mux.HandleFunc("POST /api/v1/change-order-versions/{id}/approve", r.approveChangeOrderVersion)
	r.mux.HandleFunc("POST /api/v1/change-order-versions/{id}/override-approval", r.overrideChangeOrderApproval)
	r.mux.HandleFunc("POST /api/v1/change-order-versions/{id}/apply", r.applyChangeOrderVersion)
}

func (r *Router) getTask(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Tasks == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "task service is unavailable")
		return
	}
	result, err := r.dependencies.Tasks.Get(request.Context(), principal, request.PathValue("id"))
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

type createTechnicianLaborCostRateRequest struct {
	HourlyRate  projects.Money `json:"hourly_rate"`
	EffectiveAt time.Time      `json:"effective_at"`
}

type recognizeProjectBillableWorkRequest struct {
	PhaseID      string         `json:"phase_id"`
	Description  string         `json:"description"`
	Amount       projects.Money `json:"amount"`
	RecognizedAt time.Time      `json:"recognized_at"`
}

func (r *Router) createTechnicianLaborCostRate(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.FinancialInputs == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "Project financial inputs are unavailable")
		return
	}
	var body createTechnicianLaborCostRateRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	found, err := r.dependencies.FinancialInputs.CreateLaborCostRate(
		request.Context(),
		projects.CreateLaborCostRateCommand{
			Principal: principal, TechnicianID: request.PathValue("id"),
			HourlyRate: body.HourlyRate, EffectiveAt: body.EffectiveAt,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, found.Version)
	writeJSON(writer, http.StatusCreated, found)
}

func (r *Router) recognizeProjectBillableWork(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.FinancialInputs == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "Project financial inputs are unavailable")
		return
	}
	var body recognizeProjectBillableWorkRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	found, err := r.dependencies.FinancialInputs.RecognizeBillableWork(
		request.Context(),
		projects.RecognizeBillableWorkCommand{
			Principal: principal, Target: targetFor(principal),
			ProjectID:   projects.ProjectID(request.PathValue("id")),
			PhaseID:     projects.PhaseID(body.PhaseID),
			Description: body.Description, Amount: body.Amount,
			RecognizedAt: body.RecognizedAt,
			ActorID:      principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, found.Version)
	writeJSON(writer, http.StatusCreated, found)
}

type createTechnicianAvailabilityRequest struct {
	StartsAt         time.Time `json:"starts_at"`
	EndsAt           time.Time `json:"ends_at"`
	AvailableMinutes int64     `json:"available_minutes"`
}

func (r *Router) createTechnicianAvailability(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Availability == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "technician availability is unavailable")
		return
	}
	var body createTechnicianAvailabilityRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	found, err := r.dependencies.Availability.Create(
		request.Context(),
		projects.CreateAvailabilityCommand{
			Principal: principal, TechnicianID: request.PathValue("id"),
			StartsAt: body.StartsAt, EndsAt: body.EndsAt,
			AvailableMinutes: body.AvailableMinutes,
			ActorID:          principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, found.Version)
	writeJSON(writer, http.StatusCreated, found)
}

func (r *Router) createCostActual(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.CostActuals == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "Project cost actuals are unavailable")
		return
	}
	var body CreateCostActualRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.CostActuals.Create(
		request.Context(),
		projects.CreateCostActualCommand{
			Principal: principal, Target: targetFor(principal),
			ProjectID: projects.ProjectID(request.PathValue("id")),
			PhaseID:   projects.PhaseID(body.PhaseID), CostType: body.CostType,
			Description: body.Description, Amount: body.Amount,
			Committed: body.Committed, IncurredAt: body.IncurredAt,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) createProjectTaskTimeEntry(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.TimeEntries == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "time-entry service is unavailable")
		return
	}
	var body CreateTimeEntryRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.TimeEntries.Create(
		request.Context(),
		timeentries.CreateCommand{
			Principal: principal, Target: targetFor(principal),
			TaskID: request.PathValue("id"), TechnicianID: body.TechnicianID,
			StartedAt: body.StartedAt, EndedAt: body.EndedAt,
			Billable: body.Billable, Note: body.Note,
			ActorID: principal.ID, Source: source(request), TagIDs: body.TagIDs, ClassificationPolicy: tagging.CreationRequireMeaningful,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) createPhaseTask(writer http.ResponseWriter, request *http.Request) {
	r.createDeliveryTask(writer, request, tasks.ParentPhase)
}

func (r *Router) createProjectTask(writer http.ResponseWriter, request *http.Request) {
	r.createDeliveryTask(writer, request, tasks.ParentProject)
}

func (r *Router) createDeliveryTask(
	writer http.ResponseWriter,
	request *http.Request,
	parentType tasks.ParentType,
) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Tasks == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "task service is unavailable")
		return
	}
	var body CreateTaskRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.Tasks.Create(
		request.Context(),
		tasks.CreateCommand{
			Principal: principal,
			Parent: tasks.Ref{
				Type: parentType, ID: request.PathValue("id"),
			},
			ParentTaskID: body.ParentTaskID, Title: body.Title,
			OwnerID: body.OwnerID, EstimateMinutes: body.EstimateMinutes,
			ActorID: principal.ID, Source: source(request), TagIDs: body.TagIDs, ClassificationPolicy: tagging.CreationRequireMeaningful,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) createChangeOrder(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.ChangeOrders == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "Change Order service is unavailable")
		return
	}
	var body CreateChangeOrderRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.ChangeOrders.Create(
		request.Context(),
		projects.CreateChangeOrderCommand{
			Principal: principal, Target: targetFor(principal),
			ProjectID: projects.ProjectID(request.PathValue("id")),
			DisplayID: body.DisplayID, ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) updatePhase(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Projects == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "project service is unavailable")
		return
	}
	var body UpdatePhaseRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	version, present, err := parseExpectedVersionETag(request)
	if err != nil {
		writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "If-Match is invalid")
		return
	}
	if present {
		if body.ExpectedVersion > 0 && body.ExpectedVersion != version {
			writeError(request.Context(), writer, http.StatusBadRequest, "validation_failed", "expected version does not match If-Match")
			return
		}
		body.ExpectedVersion = version
	}
	result, err := r.dependencies.Projects.UpdatePhase(
		request.Context(),
		projects.UpdatePhaseCommand{
			Principal: principal, Target: targetFor(principal),
			ID:              projects.PhaseID(request.PathValue("id")),
			ExpectedVersion: body.ExpectedVersion,
			Name:            body.Name, OwnerID: body.OwnerID,
			ParticipatingTeams: append([]string(nil), body.ParticipatingTeams...),
			PlannedStart:       body.PlannedStart, PlannedEnd: body.PlannedEnd,
			PlannedMinutes: body.PlannedMinutes, Budget: body.Budget,
			Deliverables:       append([]string(nil), body.Deliverables...),
			CompletionCriteria: append([]string(nil), body.CompletionCriteria...),
			ActorID:            principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) createResourcePlan(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.ResourcePlans == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "resource planning is unavailable")
		return
	}
	var body CreateResourcePlanRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.ResourcePlans.Plan(
		request.Context(),
		projects.PlanResourceCommand{
			Principal: principal, Target: targetFor(principal),
			ProjectID: projects.ProjectID(request.PathValue("id")),
			PhaseID:   projects.PhaseID(body.PhaseID),
			RoleID:    body.RoleID, TeamID: body.TeamID,
			StartsOn: body.StartsOn, EndsOn: body.EndsOn,
			PlannedMinutes: body.PlannedMinutes,
			ActorID:        principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) listProjects(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.ProjectQueries == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "project queries are unavailable")
		return
	}
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	found, err := r.dependencies.ProjectQueries.List(
		request.Context(), principal, targetFor(principal), limit,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) getProject(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.ProjectQueries == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "project queries are unavailable")
		return
	}
	found, err := r.dependencies.ProjectQueries.Get(
		request.Context(), principal, targetFor(principal),
		projects.ProjectID(strings.TrimSpace(request.PathValue("id"))),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, found.Version)
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) createProject(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Projects == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "project service is unavailable")
		return
	}
	var body CreateProjectRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	phases := make([]projects.PhaseInput, 0, len(body.Phases))
	for _, phase := range body.Phases {
		phases = append(phases, projects.PhaseInput{
			Name: phase.Name, OwnerID: phase.OwnerID,
			ParticipatingTeams: append([]string(nil), phase.ParticipatingTeams...),
			PlannedStart:       phase.PlannedStart, PlannedEnd: phase.PlannedEnd,
			PlannedMinutes: phase.PlannedMinutes, Budget: phase.Budget,
			Deliverables:       append([]string(nil), phase.Deliverables...),
			CompletionCriteria: append([]string(nil), phase.CompletionCriteria...),
		})
	}
	result, err := r.dependencies.Projects.Create(request.Context(), projects.CreateCommand{
		Principal: principal, Target: targetFor(principal),
		DisplayID: body.DisplayID, Name: body.Name,
		OriginalProposalVersionID: body.OriginalProposalVersionID,
		PlannedStart:              body.PlannedStart, PlannedEnd: body.PlannedEnd,
		Phases: phases, ActorID: principal.ID, Source: source(request), TagIDs: body.TagIDs, ClassificationPolicy: tagging.CreationRequireMeaningful,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) issueChangeOrderVersion(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.ChangeOrders == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "Change Order service is unavailable")
		return
	}
	var body IssueChangeOrderVersionRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	result, err := r.dependencies.ChangeOrders.IssueVersion(request.Context(), projects.IssueChangeOrderCommand{
		Principal: principal, Target: targetFor(principal),
		ChangeOrderID: request.PathValue("id"), ExpectedOrderVersion: body.ExpectedVersion,
		Description: body.Description, Currency: body.Currency,
		RevenueDeltaMinor: body.RevenueDeltaMinor, CostDeltaMinor: body.CostDeltaMinor,
		LaborDeltaMinutes: body.LaborDeltaMinutes,
		ActorID:           principal.ID, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusCreated, result)
}

func (r *Router) approveChangeOrderVersion(writer http.ResponseWriter, request *http.Request) {
	principal, body, ok := r.changeOrderDecisionRequest(writer, request)
	if !ok {
		return
	}
	result, err := r.dependencies.ChangeOrders.Approve(request.Context(), projects.ApprovalCommand{
		Principal: principal, Target: targetFor(principal), VersionID: request.PathValue("id"),
		ExpectedOrderVersion: body.ExpectedVersion, ActorID: principal.ID, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) overrideChangeOrderApproval(writer http.ResponseWriter, request *http.Request) {
	principal, body, ok := r.changeOrderDecisionRequest(writer, request)
	if !ok {
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "override reason is required")
		return
	}
	result, err := r.dependencies.ChangeOrders.OverrideApproval(request.Context(), projects.OverrideCommand{
		Principal: principal, Target: targetFor(principal), VersionID: request.PathValue("id"),
		ExpectedOrderVersion: body.ExpectedVersion, Reason: body.Reason,
		ActorID: principal.ID, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) applyChangeOrderVersion(writer http.ResponseWriter, request *http.Request) {
	principal, body, ok := r.changeOrderDecisionRequest(writer, request)
	if !ok {
		return
	}
	result, err := r.dependencies.ChangeOrders.Apply(request.Context(), projects.ApplyChangeOrderCommand{
		Principal: principal, Target: targetFor(principal), VersionID: request.PathValue("id"),
		ExpectedOrderVersion: body.ExpectedVersion,
		ActorID:              principal.ID, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, result.Version)
	writeJSON(writer, http.StatusOK, result)
}

func (r *Router) changeOrderDecisionRequest(
	writer http.ResponseWriter,
	request *http.Request,
) (authorization.Principal, ChangeOrderDecisionRequest, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, ChangeOrderDecisionRequest{}, false
	}
	if r.dependencies.ChangeOrders == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "Change Order service is unavailable")
		return authorization.Principal{}, ChangeOrderDecisionRequest{}, false
	}
	var body ChangeOrderDecisionRequest
	if !decodeRequest(writer, request, &body) {
		return authorization.Principal{}, ChangeOrderDecisionRequest{}, false
	}
	if body.ExpectedVersion < 1 {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "expected version is required")
		return authorization.Principal{}, ChangeOrderDecisionRequest{}, false
	}
	return principal, body, true
}
