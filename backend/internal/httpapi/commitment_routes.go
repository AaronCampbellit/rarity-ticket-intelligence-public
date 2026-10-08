package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/commitments"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/projects"
)

type milestoneRequest struct {
	PhaseID         projects.PhaseID         `json:"phase_id"`
	Name            string                   `json:"name"`
	Description     string                   `json:"description"`
	Priority        string                   `json:"priority"`
	OwnerID         string                   `json:"owner_id"`
	DueOn           string                   `json:"due_on"`
	AllDay          bool                     `json:"all_day"`
	StartsOn        string                   `json:"starts_on"`
	EndsOn          string                   `json:"ends_on"`
	StartsAt        string                   `json:"starts_at"`
	EndsAt          string                   `json:"ends_at"`
	Timezone        string                   `json:"timezone"`
	Recurrence      *calendar.RecurrenceRule `json:"recurrence"`
	ExpectedVersion int64                    `json:"expected_version"`
	ToStatus        string                   `json:"to_status"`
	IdempotencyKey  string                   `json:"idempotency_key"`
}
type maintenanceRequest struct {
	Title           string                     `json:"title"`
	Description     string                     `json:"description"`
	OwnerID         string                     `json:"owner_id"`
	AllDay          bool                       `json:"all_day"`
	StartsOn        string                     `json:"starts_on"`
	EndsOn          string                     `json:"ends_on"`
	StartsAt        string                     `json:"starts_at"`
	EndsAt          string                     `json:"ends_at"`
	Timezone        string                     `json:"timezone"`
	Recurrence      *calendar.RecurrenceRule   `json:"recurrence"`
	Protected       bool                       `json:"protected"`
	ConflictPolicy  commitments.ConflictPolicy `json:"conflict_policy"`
	Scopes          []commitmentScopeRequest   `json:"scopes"`
	ExpectedVersion int64                      `json:"expected_version"`
	ToStatus        string                     `json:"to_status"`
	IdempotencyKey  string                     `json:"idempotency_key"`
}
type commitmentScopeRequest struct {
	Type commitments.ScopeType `json:"type"`
	ID   string                `json:"id"`
}
type commercialRequest struct {
	ClientID          string                     `json:"client_id"`
	Type              commitments.CommercialType `json:"type"`
	Title             string                     `json:"title"`
	Description       string                     `json:"description"`
	Vendor            string                     `json:"vendor"`
	OwnerID           string                     `json:"owner_id"`
	ExternalReference string                     `json:"external_reference"`
	Currency          string                     `json:"currency"`
	EffectiveOn       string                     `json:"effective_on"`
	NoticeOn          string                     `json:"notice_on"`
	RenewalOn         string                     `json:"renewal_on"`
	ExpirationOn      string                     `json:"expiration_on"`
	QuantityUnits     int64                      `json:"quantity_units"`
	CostMinor         int64                      `json:"cost_minor"`
	HasCost           bool                       `json:"has_cost"`
	Recurrence        *calendar.RecurrenceRule   `json:"recurrence"`
	ServiceID         string                     `json:"service_id"`
	AssetID           string                     `json:"asset_id"`
	ContractID        string                     `json:"contract_id"`
	ExpectedVersion   int64                      `json:"expected_version"`
	ToStatus          string                     `json:"to_status"`
	IdempotencyKey    string                     `json:"idempotency_key"`
}

func (r *Router) registerCommitmentRoutes() {
	r.mux.HandleFunc("GET /api/v1/projects/{id}/milestones", r.listProjectMilestones)
	r.mux.HandleFunc("POST /api/v1/projects/{id}/milestones", r.createProjectMilestone)
	r.mux.HandleFunc("PATCH /api/v1/project-milestones/{id}", r.patchProjectMilestone)
	r.mux.HandleFunc("GET /api/v1/maintenance-windows", r.listMaintenanceWindows)
	r.mux.HandleFunc("POST /api/v1/maintenance-windows", r.createMaintenanceWindow)
	r.mux.HandleFunc("PATCH /api/v1/maintenance-windows/{id}", r.patchMaintenanceWindow)
	r.mux.HandleFunc("GET /api/v1/commercial-commitments", r.listCommercialCommitments)
	r.mux.HandleFunc("POST /api/v1/commercial-commitments", r.createCommercialCommitment)
	r.mux.HandleFunc("PATCH /api/v1/commercial-commitments/{id}", r.patchCommercialCommitment)
}

func (r *Router) listProjectMilestones(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.ProjectMilestoneQueries == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "milestone queries are unavailable")
		return
	}
	result, err := r.dependencies.ProjectMilestoneQueries.ListMilestones(request.Context(), principal, projects.ProjectID(request.PathValue("id")))
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, milestoneResponses(result))
}
func (r *Router) createProjectMilestone(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.ProjectMilestones == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "milestone service is unavailable")
		return
	}
	var body milestoneRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_project_milestone") {
		return
	}
	command, err := milestoneCreateCommand(principal, projects.ProjectID(request.PathValue("id")), body)
	if err != nil {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_project_milestone", "milestone dates are invalid")
		return
	}
	result, err := r.dependencies.ProjectMilestones.Create(request.Context(), command)
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeETag(w, result.Version)
	writeJSON(w, http.StatusCreated, milestoneResponse(result))
}
func (r *Router) patchProjectMilestone(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.ProjectMilestones == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "milestone service is unavailable")
		return
	}
	var body milestoneRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_project_milestone") {
		return
	}
	if body.ExpectedVersion < 1 {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_project_milestone", "expected version is required")
		return
	}
	var result projects.Milestone
	var err error
	if body.ToStatus != "" {
		result, err = r.dependencies.ProjectMilestones.Transition(request.Context(), projects.TransitionMilestoneCommand{Principal: principal, MilestoneID: request.PathValue("id"), ExpectedVersion: body.ExpectedVersion, ToStatus: body.ToStatus, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
	} else {
		command, parseErr := milestoneCreateCommand(principal, "", body)
		if parseErr != nil {
			writeError(request.Context(), w, http.StatusBadRequest, "invalid_project_milestone", "milestone dates are invalid")
			return
		}
		command.ProjectID = ""
		result, err = r.dependencies.ProjectMilestones.Update(request.Context(), projects.UpdateMilestoneCommand{Principal: principal, Milestone: projects.Milestone{ID: request.PathValue("id"), MSPID: principal.Scope.MSPID, ClientID: principal.Scope.ClientID, PhaseID: command.PhaseID, Name: command.Name, Description: command.Description, Priority: command.Priority, OwnerID: command.OwnerID, DueOn: command.DueOn, AllDay: command.AllDay, StartsOn: command.StartsOn, EndsOn: command.EndsOn, StartsAt: command.StartsAt, EndsAt: command.EndsAt, Timezone: command.Timezone, Recurrence: command.Recurrence}, ExpectedVersion: body.ExpectedVersion, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
	}
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeETag(w, result.Version)
	writeJSON(w, http.StatusOK, milestoneResponse(result))
}
func milestoneCreateCommand(principal authorization.Principal, projectID projects.ProjectID, body milestoneRequest) (projects.CreateMilestoneCommand, error) {
	due, err := parseISODatePointer(body.DueOn)
	if err != nil {
		return projects.CreateMilestoneCommand{}, err
	}
	startsOn, endsOn, startsAt, endsAt, err := parseRequestInterval(body.StartsOn, body.EndsOn, body.StartsAt, body.EndsAt)
	if body.StartsOn == "" && body.StartsAt == "" {
		startsOn, endsOn, startsAt, endsAt, err = nil, nil, nil, nil, nil
	}
	if err != nil || body.AllDay && startsAt != nil || !body.AllDay && startsOn != nil {
		return projects.CreateMilestoneCommand{}, errors.New("mixed interval")
	}
	return projects.CreateMilestoneCommand{Principal: principal, ProjectID: projectID, PhaseID: body.PhaseID, Name: body.Name, Description: body.Description, Priority: body.Priority, OwnerID: body.OwnerID, DueOn: *due, AllDay: body.AllDay, StartsOn: startsOn, EndsOn: endsOn, StartsAt: startsAt, EndsAt: endsAt, Timezone: body.Timezone, Recurrence: body.Recurrence, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey}, nil
}

func (r *Router) listMaintenanceWindows(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CommitmentQueries == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "maintenance queries are unavailable")
		return
	}
	window, err := optionalCalendarWindow(request)
	if err != nil {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_maintenance_query", "maintenance window is invalid")
		return
	}
	result, err := r.dependencies.CommitmentQueries.ListMaintenanceWindows(request.Context(), principal, window)
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, maintenanceResponses(result))
}
func (r *Router) createMaintenanceWindow(w http.ResponseWriter, request *http.Request) {
	r.mutateMaintenanceWindow(w, request, false)
}
func (r *Router) patchMaintenanceWindow(w http.ResponseWriter, request *http.Request) {
	r.mutateMaintenanceWindow(w, request, true)
}
func (r *Router) mutateMaintenanceWindow(w http.ResponseWriter, request *http.Request, update bool) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.MaintenanceWindows == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "maintenance service is unavailable")
		return
	}
	var body maintenanceRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_maintenance_window") {
		return
	}
	if update && body.ExpectedVersion < 1 {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_maintenance_window", "expected version is required")
		return
	}
	if update && body.ToStatus != "" {
		result, err := r.dependencies.MaintenanceWindows.Transition(request.Context(), commitments.TransitionMaintenanceCommand{Principal: principal, WindowID: request.PathValue("id"), ExpectedVersion: body.ExpectedVersion, ToStatus: body.ToStatus, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
		if err != nil {
			writeDomainError(w, request, err)
			return
		}
		writeETag(w, result.Version)
		writeJSON(w, http.StatusOK, maintenanceResponse(result))
		return
	}
	startsOn, endsOn, startsAt, endsAt, err := parseRequestInterval(body.StartsOn, body.EndsOn, body.StartsAt, body.EndsAt)
	if err != nil || body.AllDay != (startsOn != nil) {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_maintenance_window", "maintenance interval is invalid")
		return
	}
	scopes := make([]commitments.ScopeRef, len(body.Scopes))
	for i, value := range body.Scopes {
		scopes[i] = commitments.ScopeRef{Type: value.Type, ID: value.ID}
	}
	if update {
		result, updateErr := r.dependencies.MaintenanceWindows.Update(request.Context(), commitments.UpdateMaintenanceCommand{Principal: principal, WindowID: request.PathValue("id"), ExpectedVersion: body.ExpectedVersion, Title: body.Title, Description: body.Description, Timezone: body.Timezone, OwnerID: body.OwnerID, StartsAt: dereferenceTime(startsAt), EndsAt: dereferenceTime(endsAt), AllDay: body.AllDay, StartsOn: startsOn, EndsOn: endsOn, Recurrence: body.Recurrence, Protected: body.Protected, ConflictPolicy: body.ConflictPolicy, Scopes: scopes, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
		if updateErr != nil {
			writeDomainError(w, request, updateErr)
			return
		}
		writeETag(w, result.Version)
		writeJSON(w, http.StatusOK, maintenanceResponse(result))
		return
	}
	result, createErr := r.dependencies.MaintenanceWindows.Create(request.Context(), commitments.CreateMaintenanceCommand{Principal: principal, Title: body.Title, Description: body.Description, Timezone: body.Timezone, OwnerID: body.OwnerID, StartsAt: dereferenceTime(startsAt), EndsAt: dereferenceTime(endsAt), AllDay: body.AllDay, StartsOn: startsOn, EndsOn: endsOn, Recurrence: body.Recurrence, Protected: body.Protected, ConflictPolicy: body.ConflictPolicy, Scopes: scopes, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
	if createErr != nil {
		writeDomainError(w, request, createErr)
		return
	}
	writeETag(w, result.Version)
	writeJSON(w, http.StatusCreated, maintenanceResponse(result))
}

func (r *Router) listCommercialCommitments(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CommitmentQueries == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "commercial commitment queries are unavailable")
		return
	}
	result, err := r.dependencies.CommitmentQueries.ListCommercialCommitments(request.Context(), principal, calendarQueryValues(request.URL.Query()["client_ids"]))
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, commercialResponses(result))
}
func (r *Router) createCommercialCommitment(w http.ResponseWriter, request *http.Request) {
	r.mutateCommercialCommitment(w, request, false)
}
func (r *Router) patchCommercialCommitment(w http.ResponseWriter, request *http.Request) {
	r.mutateCommercialCommitment(w, request, true)
}
func (r *Router) mutateCommercialCommitment(w http.ResponseWriter, request *http.Request, update bool) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	if r.dependencies.CommercialCommitments == nil {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "commercial commitment service is unavailable")
		return
	}
	var body commercialRequest
	if !decodeCalendarRequest(w, request, &body, "invalid_commercial_commitment") {
		return
	}
	if update && body.ExpectedVersion < 1 {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_commercial_commitment", "expected version is required")
		return
	}
	if update && body.ToStatus != "" {
		result, err := r.dependencies.CommercialCommitments.Transition(request.Context(), commitments.TransitionCommercialCommand{Principal: principal, CommitmentID: request.PathValue("id"), ClientID: body.ClientID, ToStatus: body.ToStatus, ExpectedVersion: body.ExpectedVersion, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
		if err != nil {
			writeDomainError(w, request, err)
			return
		}
		writeETag(w, result.Version)
		writeJSON(w, http.StatusOK, commercialResponse(result))
		return
	}
	effective, notice, renewal, expiration, err := parseCommercialDates(body)
	if err != nil {
		writeError(request.Context(), w, http.StatusBadRequest, "invalid_commercial_commitment", "commercial dates must be ISO dates")
		return
	}
	if update {
		result, updateErr := r.dependencies.CommercialCommitments.Update(request.Context(), commitments.UpdateCommercialCommand{Principal: principal, CommitmentID: request.PathValue("id"), ClientID: body.ClientID, Type: body.Type, Title: body.Title, Description: body.Description, Vendor: body.Vendor, OwnerID: body.OwnerID, ExternalReference: body.ExternalReference, Currency: body.Currency, EffectiveOn: effective, NoticeOn: notice, RenewalOn: renewal, ExpirationOn: expiration, QuantityUnits: body.QuantityUnits, CostMinor: body.CostMinor, HasCost: body.HasCost, Recurrence: body.Recurrence, ServiceID: body.ServiceID, AssetID: body.AssetID, ContractID: body.ContractID, ExpectedVersion: body.ExpectedVersion, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
		if updateErr != nil {
			writeDomainError(w, request, updateErr)
			return
		}
		writeETag(w, result.Version)
		writeJSON(w, http.StatusOK, commercialResponse(result))
		return
	}
	result, createErr := r.dependencies.CommercialCommitments.Create(request.Context(), commitments.CreateCommercialCommand{Principal: principal, ClientID: body.ClientID, Type: body.Type, Title: body.Title, Description: body.Description, Vendor: body.Vendor, OwnerID: body.OwnerID, ExternalReference: body.ExternalReference, Currency: body.Currency, EffectiveOn: effective, NoticeOn: notice, RenewalOn: renewal, ExpirationOn: expiration, QuantityUnits: body.QuantityUnits, CostMinor: body.CostMinor, HasCost: body.HasCost, Recurrence: body.Recurrence, ServiceID: body.ServiceID, AssetID: body.AssetID, ContractID: body.ContractID, ActorID: principal.ID, Source: "http", IdempotencyKey: body.IdempotencyKey})
	if createErr != nil {
		writeDomainError(w, request, createErr)
		return
	}
	writeETag(w, result.Version)
	writeJSON(w, http.StatusCreated, commercialResponse(result))
}
func parseCommercialDates(body commercialRequest) (time.Time, time.Time, time.Time, time.Time, error) {
	effective, err := parseISODatePointer(body.EffectiveOn)
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, time.Time{}, err
	}
	expiration, err := parseISODatePointer(body.ExpirationOn)
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, time.Time{}, err
	}
	var notice, renewal time.Time
	if body.NoticeOn != "" {
		parsed, e := parseISODatePointer(body.NoticeOn)
		if e != nil {
			return time.Time{}, time.Time{}, time.Time{}, time.Time{}, e
		}
		notice = *parsed
	}
	if body.RenewalOn != "" {
		parsed, e := parseISODatePointer(body.RenewalOn)
		if e != nil {
			return time.Time{}, time.Time{}, time.Time{}, time.Time{}, e
		}
		renewal = *parsed
	}
	return *effective, notice, renewal, *expiration, nil
}
func dereferenceTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
