package httpapi

import (
	"net/http"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/servicekeys"
)

func (r *Router) registerServiceKeyRoutes() {
	r.mux.HandleFunc("GET /api/v1/admin/service-keys", r.listServiceKeys)
	r.mux.HandleFunc("POST /api/v1/admin/service-keys", r.issueServiceKey)
	r.mux.HandleFunc("POST /api/v1/admin/service-keys/{id}/rotate", r.rotateServiceKey)
	r.mux.HandleFunc("POST /api/v1/admin/service-keys/{id}/revoke", r.revokeServiceKey)
}

func (r *Router) listServiceKeys(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.serviceKeyPrincipal(writer, request)
	if !ok {
		return
	}
	records, err := r.dependencies.ServiceKeys.List(
		request.Context(), principal, targetFor(principal),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	response := make([]ServiceKeyResponse, 0, len(records))
	for _, record := range records {
		response = append(response, serviceKeyResponse(record))
	}
	writeJSON(writer, http.StatusOK, response)
}

func (r *Router) issueServiceKey(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.serviceKeyPrincipal(writer, request)
	if !ok {
		return
	}
	var body IssueServiceKeyRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	issued, err := r.dependencies.ServiceKeys.Issue(
		request.Context(),
		servicekeys.IssueCommand{
			Principal: principal, Target: targetFor(principal),
			Name: body.Name, Capabilities: body.Capabilities,
			DataScopes: body.DataScopes, TTL: seconds(body.TTLSeconds),
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, issuedServiceKeyResponse(issued))
}

func (r *Router) rotateServiceKey(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.serviceKeyPrincipal(writer, request)
	if !ok {
		return
	}
	var body RotateServiceKeyRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	issued, err := r.dependencies.ServiceKeys.Rotate(
		request.Context(),
		servicekeys.RotateCommand{
			Principal: principal, Target: targetFor(principal),
			KeyID: request.PathValue("id"), TTL: seconds(body.TTLSeconds),
			ActorID: principal.ID, Reason: body.Reason, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, issuedServiceKeyResponse(issued))
}

func (r *Router) revokeServiceKey(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.serviceKeyPrincipal(writer, request)
	if !ok {
		return
	}
	var body RevokeServiceKeyRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	err := r.dependencies.ServiceKeys.Revoke(
		request.Context(),
		servicekeys.RevokeCommand{
			Principal: principal, Target: targetFor(principal),
			KeyID: request.PathValue("id"), ActorID: principal.ID,
			Reason: body.Reason, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (r *Router) serviceKeyPrincipal(
	writer http.ResponseWriter,
	request *http.Request,
) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	if r.dependencies.ServiceKeys == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "service-key lifecycle is unavailable")
		return authorization.Principal{}, false
	}
	return principal, true
}

func seconds(value int64) time.Duration {
	if value <= 0 || value > int64((365*24*time.Hour)/time.Second) {
		return 0
	}
	return time.Duration(value) * time.Second
}

func issuedServiceKeyResponse(issued servicekeys.Issued) ServiceKeyIssuedResponse {
	return ServiceKeyIssuedResponse{
		ID: issued.Record.ID, ClientID: issued.Record.ClientID,
		Name: issued.Record.Name, Prefix: issued.Record.Prefix,
		Token: issued.Token, Capabilities: issued.Record.Capabilities,
		DataScopes: issued.Record.DataScopes, ExpiresAt: issued.Record.ExpiresAt,
	}
}

func serviceKeyResponse(record servicekeys.Record) ServiceKeyResponse {
	return ServiceKeyResponse{
		ID: record.ID, ClientID: record.ClientID, Name: record.Name,
		Prefix: record.Prefix, Capabilities: record.Capabilities,
		DataScopes: record.DataScopes, CreatedAt: record.CreatedAt,
		ExpiresAt: record.ExpiresAt, RevokedAt: record.RevokedAt,
	}
}
