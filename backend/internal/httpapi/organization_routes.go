package httpapi

import (
	"net/http"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/organizations"
)

type createClientRequest struct {
	DisplayID string `json:"display_id"`
	Name      string `json:"name"`
}

type createDepartmentRequest struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type createTeamRequest struct {
	DepartmentID string `json:"department_id"`
	Key          string `json:"key"`
	Name         string `json:"name"`
	Reason       string `json:"reason"`
}

type createQueueRequest struct {
	ClientID     string `json:"client_id"`
	DepartmentID string `json:"department_id"`
	TeamID       string `json:"team_id"`
	Key          string `json:"key"`
	Name         string `json:"name"`
	Reason       string `json:"reason"`
}

type replaceTeamMembersRequest struct {
	TechnicianIDs   *[]string `json:"technician_ids"`
	ExpectedVersion int64     `json:"expected_version"`
	Reason          string    `json:"reason"`
}

func (r *Router) registerOrganizationRoutes() {
	r.mux.HandleFunc("POST /api/v1/admin/clients", r.createClient)
	r.mux.HandleFunc("POST /api/v1/admin/departments", r.createDepartment)
	r.mux.HandleFunc("POST /api/v1/admin/teams", r.createTeam)
	r.mux.HandleFunc("PUT /api/v1/admin/teams/{id}/members", r.replaceTeamMembers)
	r.mux.HandleFunc("POST /api/v1/admin/queues", r.createQueue)
	r.mux.HandleFunc("GET /api/v1/directory", r.listDirectory)
}

func (r *Router) replaceTeamMembers(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Directory == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "directory management is unavailable")
		return
	}
	var body replaceTeamMembersRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	if body.TechnicianIDs == nil || !validRequestUUID(request.PathValue("id")) {
		writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body is invalid")
		return
	}
	for _, technicianID := range *body.TechnicianIDs {
		if !validRequestUUID(technicianID) {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "request body is invalid")
			return
		}
	}
	team, err := r.dependencies.Directory.ReplaceTeamMembers(
		request.Context(),
		organizations.ReplaceTeamMembersCommand{
			Principal: principal, TeamID: request.PathValue("id"),
			TechnicianIDs: *body.TechnicianIDs, ExpectedVersion: body.ExpectedVersion,
			Reason: body.Reason, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, team.Version)
	writeJSON(writer, http.StatusOK, team)
}

func (r *Router) createClient(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Organizations == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "organization management is unavailable")
		return
	}
	var body createClientRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	client, err := r.dependencies.Organizations.CreateClient(
		request.Context(),
		organizations.CreateClientCommand{
			Principal: principal,
			Actor: organizations.Actor{
				Type: "technician", ID: principal.ID, Source: source(request),
			},
			DisplayID: body.DisplayID, Name: body.Name,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, client.Version)
	writeJSON(writer, http.StatusCreated, client)
}

func (r *Router) createDepartment(writer http.ResponseWriter, request *http.Request) {
	principal, body, ok := r.directoryRequest(writer, request)
	if !ok {
		return
	}
	value, err := r.dependencies.Directory.CreateDepartment(
		request.Context(),
		organizations.CreateDepartmentCommand{
			Principal: principal, Key: body.Key, Name: body.Name,
			Reason: body.Reason, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, value.Version)
	writeJSON(writer, http.StatusCreated, value)
}

func (r *Router) createTeam(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Directory == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "directory management is unavailable")
		return
	}
	var body createTeamRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	value, err := r.dependencies.Directory.CreateTeam(
		request.Context(),
		organizations.CreateTeamCommand{
			Principal: principal, DepartmentID: body.DepartmentID,
			Key: body.Key, Name: body.Name, Reason: body.Reason,
			Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, value.Version)
	writeJSON(writer, http.StatusCreated, value)
}

func (r *Router) createQueue(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Directory == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "directory management is unavailable")
		return
	}
	var body createQueueRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	value, err := r.dependencies.Directory.CreateQueue(
		request.Context(),
		organizations.CreateQueueCommand{
			Principal: principal, ClientID: body.ClientID,
			DepartmentID: body.DepartmentID, TeamID: body.TeamID,
			Key: body.Key, Name: body.Name, Reason: body.Reason,
			Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, value.Version)
	writeJSON(writer, http.StatusCreated, value)
}

func (r *Router) listDirectory(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Directory == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "directory management is unavailable")
		return
	}
	value, err := r.dependencies.Directory.List(
		request.Context(),
		organizations.ListDirectoryCommand{
			Principal: principal, Target: targetFor(principal),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (r *Router) directoryRequest(
	writer http.ResponseWriter,
	request *http.Request,
) (authorization.Principal, createDepartmentRequest, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, createDepartmentRequest{}, false
	}
	if r.dependencies.Directory == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "directory management is unavailable")
		return authorization.Principal{}, createDepartmentRequest{}, false
	}
	var body createDepartmentRequest
	if !decodeRequest(writer, request, &body) {
		return authorization.Principal{}, createDepartmentRequest{}, false
	}
	return principal, body, true
}
