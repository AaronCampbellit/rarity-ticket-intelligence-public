package httpapi

import (
	"context"
	"net/http"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
)

type EntraSettingsActions interface {
	Get(context.Context, authorization.Principal) (identity.EntraSettings, error)
	Configure(context.Context, identity.ConfigureEntraCommand) (identity.EntraSettings, error)
	Verify(context.Context, identity.VerifyEntraCommand) (identity.EntraSettings, error)
	Disable(context.Context, identity.DisableEntraCommand) (identity.EntraSettings, error)
}

type configureEntraRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	TenantID        string `json:"tenant_id"`
	ClientID        string `json:"client_id"`
	ClientSecret    string `json:"client_secret"`
	RedirectURL     string `json:"redirect_url"`
	Reason          string `json:"reason"`
}

type mutateEntraRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

func (r *Router) registerEntraSettingsRoutes() {
	r.mux.HandleFunc("GET /api/v1/admin/identity/entra", r.getEntraSettings)
	r.mux.HandleFunc("PUT /api/v1/admin/identity/entra", r.configureEntra)
	r.mux.HandleFunc("POST /api/v1/admin/identity/entra/verify", r.verifyEntra)
	r.mux.HandleFunc("POST /api/v1/admin/identity/entra/disable", r.disableEntra)
}

func (r *Router) getEntraSettings(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.entraSettingsPrincipal(writer, request)
	if !ok {
		return
	}
	settings, err := r.dependencies.EntraSettings.Get(request.Context(), principal)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, settings.Version)
	writeJSON(writer, http.StatusOK, settings)
}

func (r *Router) configureEntra(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.entraSettingsPrincipal(writer, request)
	if !ok {
		return
	}
	var body configureEntraRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	settings, err := r.dependencies.EntraSettings.Configure(request.Context(), identity.ConfigureEntraCommand{
		Principal: principal, ExpectedVersion: body.ExpectedVersion,
		TenantID: body.TenantID, ClientID: body.ClientID,
		ClientSecret: body.ClientSecret, RedirectURL: body.RedirectURL,
		Reason: body.Reason, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, settings.Version)
	writeJSON(writer, http.StatusOK, settings)
}

func (r *Router) verifyEntra(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.entraSettingsPrincipal(writer, request)
	if !ok {
		return
	}
	var body mutateEntraRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	settings, err := r.dependencies.EntraSettings.Verify(request.Context(), identity.VerifyEntraCommand{
		Principal: principal, ExpectedVersion: body.ExpectedVersion,
		Reason: body.Reason, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, settings.Version)
	writeJSON(writer, http.StatusOK, settings)
}

func (r *Router) disableEntra(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.entraSettingsPrincipal(writer, request)
	if !ok {
		return
	}
	var body mutateEntraRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	settings, err := r.dependencies.EntraSettings.Disable(request.Context(), identity.DisableEntraCommand{
		Principal: principal, ExpectedVersion: body.ExpectedVersion,
		Reason: body.Reason, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, settings.Version)
	writeJSON(writer, http.StatusOK, settings)
}

func (r *Router) entraSettingsPrincipal(writer http.ResponseWriter, request *http.Request) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	if r.dependencies.EntraSettings == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "Entra settings are unavailable")
		return authorization.Principal{}, false
	}
	return principal, true
}
