package httpapi

import (
	"context"
	"net/http"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

type RoleManagementActions interface {
	List(context.Context, authorization.Principal) ([]authorization.Role, error)
	Create(
		context.Context,
		authorization.Principal,
		authorization.CreateRoleCommand,
	) (authorization.Role, error)
	Assign(
		context.Context,
		authorization.Principal,
		authorization.AssignRoleCommand,
	) (authorization.RoleAssignment, error)
	ReplaceCapabilities(
		context.Context,
		authorization.Principal,
		string,
		int64,
		[]string,
		string,
	) (authorization.Role, error)
}

type createRoleRequest struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Reason       string   `json:"reason"`
}

type assignRoleRequest struct {
	PrincipalID string `json:"principal_id"`
	RoleKey     string `json:"role_key"`
	ClientID    string `json:"client_id,omitempty"`
	Reason      string `json:"reason"`
}

type replaceRoleCapabilitiesRequest struct {
	ExpectedVersion int64    `json:"expected_version"`
	Capabilities    []string `json:"capabilities"`
	Reason          string   `json:"reason"`
}

func (r *Router) registerRoleManagementRoutes() {
	r.mux.HandleFunc("GET /api/v1/admin/roles", r.listRoles)
	r.mux.HandleFunc("POST /api/v1/admin/roles", r.createRole)
	r.mux.HandleFunc(
		"POST /api/v1/admin/role-assignments",
		r.assignRole,
	)
	r.mux.HandleFunc(
		"PUT /api/v1/admin/roles/{id}/capabilities",
		r.replaceRoleCapabilities,
	)
}

func (r *Router) createRole(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.roleManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body createRoleRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	role, err := r.dependencies.RoleManagement.Create(
		request.Context(),
		principal,
		authorization.CreateRoleCommand{
			Key: body.Key, Name: body.Name, Capabilities: body.Capabilities,
			Reason: body.Reason, ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, role.Version)
	writeJSON(writer, http.StatusCreated, role)
}

func (r *Router) assignRole(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.roleManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body assignRoleRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	assignment, err := r.dependencies.RoleManagement.Assign(
		request.Context(),
		principal,
		authorization.AssignRoleCommand{
			PrincipalID: body.PrincipalID, RoleKey: body.RoleKey,
			ClientID: body.ClientID, Reason: body.Reason,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, assignment)
}

func (r *Router) listRoles(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.roleManagementPrincipal(writer, request)
	if !ok {
		return
	}
	roles, err := r.dependencies.RoleManagement.List(request.Context(), principal)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, roles)
}

func (r *Router) replaceRoleCapabilities(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.roleManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body replaceRoleCapabilitiesRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	role, err := r.dependencies.RoleManagement.ReplaceCapabilities(
		request.Context(), principal, request.PathValue("id"),
		body.ExpectedVersion, body.Capabilities, body.Reason,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, role.Version)
	writeJSON(writer, http.StatusOK, role)
}

func (r *Router) roleManagementPrincipal(
	writer http.ResponseWriter,
	request *http.Request,
) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(
			request.Context(), writer, http.StatusUnauthorized,
			"unauthenticated", "authentication required",
		)
		return authorization.Principal{}, false
	}
	if r.dependencies.RoleManagement == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "role management is unavailable",
		)
		return authorization.Principal{}, false
	}
	return principal, true
}
