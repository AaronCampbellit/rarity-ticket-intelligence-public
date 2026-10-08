package httpapi

import (
	"net/http"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/notifications"
)

type teamsConnectionCreateRequest struct {
	Name       string `json:"name"`
	WebhookURL string `json:"webhook_url"`
	Reason     string `json:"reason"`
}

type teamsConnectionPatchRequest struct {
	ExpectedVersion int64   `json:"expected_version"`
	Name            *string `json:"name,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	Reason          string  `json:"reason"`
}

type teamsCredentialRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	WebhookURL      string `json:"webhook_url"`
	Reason          string `json:"reason"`
}

type teamsTestRequest struct {
	Reason string `json:"reason"`
}

func (r *Router) registerTeamsConnectionRoutes() {
	r.mux.HandleFunc("GET /api/v1/integrations/teams/connections", r.listTeamsConnections)
	r.mux.HandleFunc("POST /api/v1/integrations/teams/connections", r.createTeamsConnection)
	r.mux.HandleFunc("PATCH /api/v1/integrations/teams/connections/{id}", r.patchTeamsConnection)
	r.mux.HandleFunc("POST /api/v1/integrations/teams/connections/{id}/credential", r.replaceTeamsCredential)
	r.mux.HandleFunc("POST /api/v1/integrations/teams/connections/{id}/test", r.testTeamsConnection)
}

func (r *Router) listTeamsConnections(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.authorizeTeamsManagement(writer, request)
	if !ok {
		return
	}
	if r.dependencies.TeamsConnections == nil {
		teamsUnavailable(writer, request)
		return
	}
	connections, err := r.dependencies.TeamsConnections.List(request.Context(), notifications.ListTeamsConnectionsCommand{
		Principal: principal, Target: targetFor(principal),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	if connections == nil {
		connections = []notifications.ManagedTeamsConnection{}
	}
	writeJSON(writer, http.StatusOK, connections)
}

func (r *Router) createTeamsConnection(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.authorizeTeamsManagement(writer, request)
	if !ok {
		return
	}
	if r.dependencies.TeamsConnections == nil {
		teamsUnavailable(writer, request)
		return
	}
	var body teamsConnectionCreateRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	webhook := []byte(body.WebhookURL)
	body.WebhookURL = ""
	defer wipeBytes(webhook)
	connection, err := r.dependencies.TeamsConnections.Create(request.Context(), notifications.CreateTeamsConnectionCommand{
		Principal: principal, Target: targetFor(principal), ClientID: principal.Scope.ClientID,
		Name: body.Name, PlaintextWebhookURL: webhook, Reason: body.Reason,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, connection.Version)
	writeJSON(writer, http.StatusCreated, connection)
}

func (r *Router) patchTeamsConnection(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.authorizeTeamsManagement(writer, request)
	if !ok {
		return
	}
	if r.dependencies.TeamsConnections == nil {
		teamsUnavailable(writer, request)
		return
	}
	var body teamsConnectionPatchRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	expected, ok := aiExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok {
		return
	}
	if strings.TrimSpace(body.Reason) == "" || (body.Name == nil) == (body.Enabled == nil) {
		aiValidation(writer, request, "exactly one of name or enabled and a reason are required")
		return
	}
	var connection notifications.ManagedTeamsConnection
	var err error
	if body.Name != nil {
		connection, err = r.dependencies.TeamsConnections.UpdateMetadata(request.Context(), notifications.UpdateTeamsConnectionMetadataCommand{
			Principal: principal, Target: targetFor(principal), ID: request.PathValue("id"),
			Name: *body.Name, ExpectedVersion: expected, Reason: body.Reason,
		})
	} else {
		connection, err = r.dependencies.TeamsConnections.SetEnabled(request.Context(), notifications.SetTeamsConnectionEnabledCommand{
			Principal: principal, Target: targetFor(principal), ID: request.PathValue("id"),
			ExpectedVersion: expected, Enabled: *body.Enabled, Reason: body.Reason,
		})
	}
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, connection.Version)
	writeJSON(writer, http.StatusOK, connection)
}

func (r *Router) replaceTeamsCredential(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.authorizeTeamsManagement(writer, request)
	if !ok {
		return
	}
	if r.dependencies.TeamsConnections == nil {
		teamsUnavailable(writer, request)
		return
	}
	var body teamsCredentialRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	expected, ok := aiExpectedVersion(writer, request, body.ExpectedVersion)
	if !ok {
		return
	}
	webhook := []byte(body.WebhookURL)
	body.WebhookURL = ""
	defer wipeBytes(webhook)
	connection, err := r.dependencies.TeamsConnections.ReplaceCredential(request.Context(), notifications.ReplaceTeamsConnectionCredentialCommand{
		Principal: principal, Target: targetFor(principal), ID: request.PathValue("id"),
		ExpectedVersion: expected, PlaintextWebhookURL: webhook, Reason: body.Reason,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, connection.Version)
	writeJSON(writer, http.StatusOK, connection)
}

func (r *Router) testTeamsConnection(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.authorizeTeamsManagement(writer, request)
	if !ok {
		return
	}
	if r.dependencies.TeamsConnections == nil {
		teamsUnavailable(writer, request)
		return
	}
	var body teamsTestRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	connection, err := r.dependencies.TeamsConnections.Test(request.Context(), notifications.TestTeamsConnectionCommand{
		Principal: principal, Target: targetFor(principal), ID: request.PathValue("id"), Reason: body.Reason,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, connection.Version)
	writeJSON(writer, http.StatusOK, connection)
}

func (r *Router) authorizeTeamsManagement(writer http.ResponseWriter, request *http.Request) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	if err := authorization.Authorize(principal, "integration.manage", targetFor(principal)); err != nil {
		writeDomainError(writer, request, err)
		return authorization.Principal{}, false
	}
	return principal, true
}

func teamsUnavailable(writer http.ResponseWriter, request *http.Request) {
	writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "Teams connection management is unavailable")
}
