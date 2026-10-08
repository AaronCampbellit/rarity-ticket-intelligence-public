package httpapi

import (
	"net/http"
	"time"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/datto"
)

type dattoConnectionCreateRequest struct {
	Name                string `json:"name"`
	APIURL              string `json:"api_url"`
	APIKey              string `json:"api_key"`
	APISecret           string `json:"api_secret"`
	SyncIntervalSeconds int64  `json:"sync_interval_seconds"`
	Reason              string `json:"reason"`
}

type dattoConnectionPatchRequest struct {
	ExpectedVersion     int64  `json:"expected_version"`
	Name                string `json:"name"`
	SyncIntervalSeconds int64  `json:"sync_interval_seconds"`
	Enabled             *bool  `json:"enabled,omitempty"`
	Reason              string `json:"reason"`
}

type dattoCredentialRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	APIURL          string `json:"api_url"`
	APIKey          string `json:"api_key"`
	APISecret       string `json:"api_secret"`
	Reason          string `json:"reason"`
}

func (r *Router) registerDattoManagementRoutes() {
	r.mux.HandleFunc(
		"GET /api/v1/integrations/datto/connections",
		r.listDattoConnections,
	)
	r.mux.HandleFunc(
		"POST /api/v1/integrations/datto/connections",
		r.createDattoConnection,
	)
	r.mux.HandleFunc(
		"PATCH /api/v1/integrations/datto/connections/{id}",
		r.patchDattoConnection,
	)
	r.mux.HandleFunc(
		"POST /api/v1/integrations/datto/connections/{id}/credential",
		r.replaceDattoCredential,
	)
}

func (r *Router) listDattoConnections(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.dattoManagementPrincipal(writer, request)
	if !ok {
		return
	}
	items, err := r.dependencies.DattoManagement.List(
		request.Context(), principal,
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, items)
}

func (r *Router) createDattoConnection(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.dattoManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body dattoConnectionCreateRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	key := []byte(body.APIKey)
	secret := []byte(body.APISecret)
	body.APIKey = ""
	body.APISecret = ""
	defer wipeBytes(key)
	defer wipeBytes(secret)
	item, err := r.dependencies.DattoManagement.Create(
		request.Context(),
		datto.CreateConnectionCommand{
			Principal: principal, Name: body.Name, APIURL: body.APIURL,
			APIKey: key, APISecret: secret,
			SyncInterval: time.Duration(body.SyncIntervalSeconds) * time.Second,
			Reason:       body.Reason,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, item.Version)
	writeJSON(writer, http.StatusCreated, item)
}

func (r *Router) patchDattoConnection(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.dattoManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body dattoConnectionPatchRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	item, err := r.dependencies.DattoManagement.Update(
		request.Context(),
		datto.UpdateConnectionCommand{
			Principal: principal, ID: request.PathValue("id"),
			ExpectedVersion: body.ExpectedVersion, Name: body.Name,
			SyncInterval: time.Duration(body.SyncIntervalSeconds) * time.Second,
			Enabled:      body.Enabled, Reason: body.Reason,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, item.Version)
	writeJSON(writer, http.StatusOK, item)
}

func (r *Router) replaceDattoCredential(
	writer http.ResponseWriter,
	request *http.Request,
) {
	principal, ok := r.dattoManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body dattoCredentialRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	key := []byte(body.APIKey)
	secret := []byte(body.APISecret)
	body.APIKey = ""
	body.APISecret = ""
	defer wipeBytes(key)
	defer wipeBytes(secret)
	item, err := r.dependencies.DattoManagement.ReplaceCredential(
		request.Context(),
		datto.ReplaceCredentialCommand{
			Principal: principal, ID: request.PathValue("id"),
			ExpectedVersion: body.ExpectedVersion, APIURL: body.APIURL,
			APIKey: key, APISecret: secret, Reason: body.Reason,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, item.Version)
	writeJSON(writer, http.StatusOK, item)
}

func (r *Router) dattoManagementPrincipal(
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
	if r.dependencies.DattoManagement == nil {
		writeError(
			request.Context(), writer, http.StatusNotImplemented,
			"not_implemented", "Datto connection management is unavailable",
		)
		return authorization.Principal{}, false
	}
	return principal, true
}
