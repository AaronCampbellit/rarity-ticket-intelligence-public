package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/identity"
)

type BreakGlassManagementActions interface {
	Create(context.Context, identity.CreateBreakGlassAccountCommand) (identity.ManagedBreakGlassAccount, error)
	List(context.Context, authorization.Principal) ([]identity.ManagedBreakGlassAccount, error)
	ResetPassword(context.Context, identity.ResetBreakGlassPasswordCommand) (identity.ManagedBreakGlassAccount, error)
	UpdateNetworks(context.Context, identity.UpdateBreakGlassNetworksCommand) (identity.ManagedBreakGlassAccount, error)
	Disable(context.Context, identity.DisableBreakGlassAccountCommand) (identity.ManagedBreakGlassAccount, error)
}

type createBreakGlassAccountRequest struct {
	Email        string   `json:"email"`
	DisplayName  string   `json:"display_name"`
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	AllowedCIDRs []string `json:"allowed_cidrs"`
	Reason       string   `json:"reason"`
}

type disableBreakGlassAccountRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

type resetBreakGlassPasswordRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Password        string `json:"password"`
	Reason          string `json:"reason"`
}

type updateBreakGlassNetworksRequest struct {
	ExpectedVersion int64    `json:"expected_version"`
	AllowedCIDRs    []string `json:"allowed_cidrs"`
	Reason          string   `json:"reason"`
}

func (r *Router) registerBreakGlassManagementRoutes() {
	r.mux.HandleFunc("GET /api/v1/admin/break-glass-accounts", r.listBreakGlassAccounts)
	r.mux.HandleFunc("POST /api/v1/admin/break-glass-accounts", r.createBreakGlassAccount)
	r.mux.HandleFunc("POST /api/v1/admin/break-glass-accounts/{id}/password", r.resetBreakGlassPassword)
	r.mux.HandleFunc("PATCH /api/v1/admin/break-glass-accounts/{id}/networks", r.updateBreakGlassNetworks)
	r.mux.HandleFunc("POST /api/v1/admin/break-glass-accounts/{id}/disable", r.disableBreakGlassAccount)
	r.mux.HandleFunc("GET /api/v1/admin/local-administrators", r.listBreakGlassAccounts)
	r.mux.HandleFunc("POST /api/v1/admin/local-administrators", r.createBreakGlassAccount)
	r.mux.HandleFunc("POST /api/v1/admin/local-administrators/{id}/password", r.resetBreakGlassPassword)
	r.mux.HandleFunc("PATCH /api/v1/admin/local-administrators/{id}/networks", r.updateBreakGlassNetworks)
	r.mux.HandleFunc("POST /api/v1/admin/local-administrators/{id}/disable", r.disableBreakGlassAccount)
}

func (r *Router) listBreakGlassAccounts(writer http.ResponseWriter, request *http.Request) {
	localAdminDeprecationHeaders(writer, request)
	principal, ok := r.breakGlassManagementPrincipal(writer, request)
	if !ok {
		return
	}
	accounts, err := r.dependencies.BreakGlassManagement.List(request.Context(), principal)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, accounts)
}

func (r *Router) createBreakGlassAccount(writer http.ResponseWriter, request *http.Request) {
	localAdminDeprecationHeaders(writer, request)
	principal, ok := r.breakGlassManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body createBreakGlassAccountRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	account, err := r.dependencies.BreakGlassManagement.Create(request.Context(), identity.CreateBreakGlassAccountCommand{
		Principal: principal, Email: body.Email, DisplayName: body.DisplayName,
		Username: body.Username, Password: body.Password,
		AllowedCIDRs: body.AllowedCIDRs, Reason: body.Reason, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, account.Version)
	writeJSON(writer, http.StatusCreated, account)
}

func (r *Router) disableBreakGlassAccount(writer http.ResponseWriter, request *http.Request) {
	localAdminDeprecationHeaders(writer, request)
	principal, ok := r.breakGlassManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body disableBreakGlassAccountRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	account, err := r.dependencies.BreakGlassManagement.Disable(request.Context(), identity.DisableBreakGlassAccountCommand{
		Principal: principal, AccountID: strings.TrimSpace(request.PathValue("id")),
		ExpectedVersion: body.ExpectedVersion, Reason: body.Reason, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, account.Version)
	writeJSON(writer, http.StatusOK, account)
}

func (r *Router) resetBreakGlassPassword(writer http.ResponseWriter, request *http.Request) {
	localAdminDeprecationHeaders(writer, request)
	principal, ok := r.breakGlassManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body resetBreakGlassPasswordRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	account, err := r.dependencies.BreakGlassManagement.ResetPassword(request.Context(), identity.ResetBreakGlassPasswordCommand{
		Principal: principal, AccountID: strings.TrimSpace(request.PathValue("id")),
		ExpectedVersion: body.ExpectedVersion, Password: body.Password,
		Reason: body.Reason, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, account.Version)
	writeJSON(writer, http.StatusOK, account)
}

func (r *Router) updateBreakGlassNetworks(writer http.ResponseWriter, request *http.Request) {
	localAdminDeprecationHeaders(writer, request)
	principal, ok := r.breakGlassManagementPrincipal(writer, request)
	if !ok {
		return
	}
	var body updateBreakGlassNetworksRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	account, err := r.dependencies.BreakGlassManagement.UpdateNetworks(request.Context(), identity.UpdateBreakGlassNetworksCommand{
		Principal: principal, AccountID: strings.TrimSpace(request.PathValue("id")),
		ExpectedVersion: body.ExpectedVersion, AllowedCIDRs: body.AllowedCIDRs,
		Reason: body.Reason, Source: source(request),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, account.Version)
	writeJSON(writer, http.StatusOK, account)
}

func localAdminDeprecationHeaders(writer http.ResponseWriter, request *http.Request) {
	if !strings.Contains(request.URL.Path, "/break-glass-accounts") {
		return
	}
	writer.Header().Set("Deprecation", "true")
	writer.Header().Set("Sunset", "Mon, 02 Nov 2026 00:00:00 GMT")
	writer.Header().Set(
		"Link",
		`</api/v1/admin/local-administrators>; rel="successor-version"`,
	)
}

func (r *Router) breakGlassManagementPrincipal(writer http.ResponseWriter, request *http.Request) (authorization.Principal, bool) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	if r.dependencies.BreakGlassManagement == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "recovery access management is unavailable")
		return authorization.Principal{}, false
	}
	return principal, true
}
