package httpapi

import (
	"net/http"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/webhooks"
)

func (r *Router) registerWebhookManagementRoutes() {
	r.mux.HandleFunc("GET /api/v1/integrations/webhooks/connections", r.listWebhookConnections)
	r.mux.HandleFunc("GET /api/v1/integrations/webhooks/deliveries", r.listWebhookDeliveries)
	r.mux.HandleFunc("POST /api/v1/integrations/webhooks/connections", r.createWebhookConnection)
	r.mux.HandleFunc("PATCH /api/v1/integrations/webhooks/connections/{id}", r.patchWebhookConnection)
	r.mux.HandleFunc("POST /api/v1/integrations/webhooks/connections/{id}/credential", r.replaceWebhookCredential)
	r.mux.HandleFunc("POST /api/v1/integrations/webhooks/connections/{id}/deliveries/{event_id}/retry", r.retryWebhookDelivery)
}

type webhookConnectionRequest struct {
	Name               string             `json:"name"`
	Direction          webhooks.Direction `json:"direction"`
	EndpointURL        string             `json:"endpoint_url"`
	EventTypes         []string           `json:"event_types"`
	RetryWindowSeconds int64              `json:"retry_window_seconds"`
	SigningSecret      string             `json:"signing_secret"`
	Reason             string             `json:"reason"`
}

type webhookPatchRequest struct {
	ExpectedVersion    int64    `json:"expected_version"`
	Name               string   `json:"name"`
	EndpointURL        string   `json:"endpoint_url"`
	EventTypes         []string `json:"event_types"`
	RetryWindowSeconds int64    `json:"retry_window_seconds"`
	Enabled            *bool    `json:"enabled,omitempty"`
	Reason             string   `json:"reason"`
}

type webhookCredentialRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	SigningSecret   string `json:"signing_secret"`
	Reason          string `json:"reason"`
}

type webhookRetryRequest struct {
	Reason string `json:"reason"`
}

func (r *Router) createWebhookConnection(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.webhookPrincipal(writer, request)
	if !ok {
		return
	}
	var body webhookConnectionRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	secret := []byte(body.SigningSecret)
	body.SigningSecret = ""
	defer wipeBytes(secret)
	connection, err := r.dependencies.WebhookManagement.Create(request.Context(), webhooks.CreateConnectionCommand{
		Principal: principal, Target: targetFor(principal), Name: body.Name,
		Direction: body.Direction, Endpoint: body.EndpointURL,
		EventTypes:      body.EventTypes,
		RetryWindow:     time.Duration(body.RetryWindowSeconds) * time.Second,
		PlaintextSecret: secret, Reason: body.Reason,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, connection.Version)
	writeJSON(writer, http.StatusCreated, connection)
}

func (r *Router) patchWebhookConnection(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.webhookPrincipal(writer, request)
	if !ok {
		return
	}
	var body webhookPatchRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	connection, err := r.dependencies.WebhookManagement.Update(request.Context(), webhooks.UpdateConnectionCommand{
		Principal: principal, Target: targetFor(principal), ID: request.PathValue("id"),
		ExpectedVersion: body.ExpectedVersion, Name: body.Name,
		Endpoint: body.EndpointURL, EventTypes: body.EventTypes,
		RetryWindow: time.Duration(body.RetryWindowSeconds) * time.Second,
		Enabled:     body.Enabled, Reason: body.Reason,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, connection.Version)
	writeJSON(writer, http.StatusOK, connection)
}

func (r *Router) replaceWebhookCredential(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.webhookPrincipal(writer, request)
	if !ok {
		return
	}
	var body webhookCredentialRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	secret := []byte(body.SigningSecret)
	body.SigningSecret = ""
	defer wipeBytes(secret)
	connection, err := r.dependencies.WebhookManagement.ReplaceCredential(request.Context(), webhooks.ReplaceCredentialCommand{
		Principal: principal, Target: targetFor(principal), ID: request.PathValue("id"),
		ExpectedVersion: body.ExpectedVersion, PlaintextSecret: secret, Reason: body.Reason,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, connection.Version)
	writeJSON(writer, http.StatusOK, connection)
}

func (r *Router) retryWebhookDelivery(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.webhookPrincipal(writer, request)
	if !ok {
		return
	}
	var body webhookRetryRequest
	if !decodeAIRequest(writer, request, &body) {
		return
	}
	err := r.dependencies.WebhookManagement.RetryDelivery(request.Context(), webhooks.RetryDeliveryCommand{
		Principal: principal, Target: targetFor(principal),
		ConnectionID: request.PathValue("id"), EventID: request.PathValue("event_id"),
		Reason: body.Reason,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (r *Router) listWebhookConnections(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.webhookPrincipal(writer, request)
	if !ok {
		return
	}
	connections, err := r.dependencies.WebhookManagement.ListConnections(
		request.Context(), principal, targetFor(principal),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, connections)
}

func (r *Router) listWebhookDeliveries(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.webhookPrincipal(writer, request)
	if !ok {
		return
	}
	deliveries, err := r.dependencies.WebhookManagement.ListDeliveries(
		request.Context(), principal, targetFor(principal),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, deliveries)
}

func (r *Router) webhookPrincipal(
	writer http.ResponseWriter,
	request *http.Request,
) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	if r.dependencies.WebhookManagement == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "webhook management is unavailable")
		return authorization.Principal{}, false
	}
	return principal, true
}
