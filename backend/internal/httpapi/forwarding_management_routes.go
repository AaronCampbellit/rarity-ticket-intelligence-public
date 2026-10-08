package httpapi

import (
	"net/http"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/intake"
)

type forwardingConnectionRequest struct {
	IntakeAddress        string   `json:"intake_address"`
	AllowedSenderDomains []string `json:"allowed_sender_domains"`
	MaxMessageBytes      int64    `json:"max_message_bytes"`
	RateLimitPerMinute   int      `json:"rate_limit_per_minute"`
	Reason               string   `json:"reason"`
}

type forwardingConnectionPatchRequest struct {
	ExpectedVersion      int64    `json:"expected_version"`
	IntakeAddress        string   `json:"intake_address"`
	AllowedSenderDomains []string `json:"allowed_sender_domains"`
	MaxMessageBytes      int64    `json:"max_message_bytes"`
	RateLimitPerMinute   int      `json:"rate_limit_per_minute"`
	Enabled              *bool    `json:"enabled,omitempty"`
	Reason               string   `json:"reason"`
}

func (r *Router) registerForwardingManagementRoutes() {
	r.mux.HandleFunc("GET /api/v1/integrations/forwarding/connections", r.listForwardingConnections)
	r.mux.HandleFunc("POST /api/v1/integrations/forwarding/connections", r.createForwardingConnection)
	r.mux.HandleFunc("PATCH /api/v1/integrations/forwarding/connections/{id}", r.patchForwardingConnection)
}

func (r *Router) listForwardingConnections(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.forwardingManagementPrincipal(writer, request)
	if !ok {
		return
	}
	connections, err := r.dependencies.ForwardingManagement.List(request.Context(), principal)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, connections)
}

func (r *Router) createForwardingConnection(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.forwardingManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body forwardingConnectionRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	connection, err := r.dependencies.ForwardingManagement.Create(request.Context(), intake.CreateForwardingConnectionCommand{
		Principal: principal, IntakeAddress: body.IntakeAddress,
		AllowedSenderDomains: body.AllowedSenderDomains,
		MaxMessageBytes:      body.MaxMessageBytes,
		RateLimitPerMinute:   body.RateLimitPerMinute, Reason: body.Reason,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, connection.Version)
	writeJSON(writer, http.StatusCreated, connection)
}

func (r *Router) patchForwardingConnection(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.forwardingManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body forwardingConnectionPatchRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	connection, err := r.dependencies.ForwardingManagement.Update(request.Context(), intake.UpdateForwardingConnectionCommand{
		Principal: principal, ID: request.PathValue("id"),
		ExpectedVersion: body.ExpectedVersion, IntakeAddress: body.IntakeAddress,
		AllowedSenderDomains: body.AllowedSenderDomains,
		MaxMessageBytes:      body.MaxMessageBytes,
		RateLimitPerMinute:   body.RateLimitPerMinute,
		Enabled:              body.Enabled, Reason: body.Reason,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, connection.Version)
	writeJSON(writer, http.StatusOK, connection)
}

func (r *Router) forwardingManagementPrincipal(
	writer http.ResponseWriter,
	request *http.Request,
) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	if r.dependencies.ForwardingManagement == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "forwarding management is unavailable")
		return authorization.Principal{}, false
	}
	return principal, true
}
