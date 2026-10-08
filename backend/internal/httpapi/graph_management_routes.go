package httpapi

import (
	"net/http"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/graphintake"
)

type graphMailboxCreateRequest struct {
	MailboxAddress string `json:"mailbox_address"`
	TenantID       string `json:"tenant_id"`
	ClientID       string `json:"client_id"`
	ClientSecret   string `json:"client_secret"`
	Reason         string `json:"reason"`
}
type graphMailboxPatchRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	MailboxAddress  string `json:"mailbox_address"`
	Enabled         *bool  `json:"enabled,omitempty"`
	Reason          string `json:"reason"`
}
type graphCredentialRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	TenantID        string `json:"tenant_id"`
	ClientID        string `json:"client_id"`
	ClientSecret    string `json:"client_secret"`
	Reason          string `json:"reason"`
}

func (r *Router) registerGraphManagementRoutes() {
	r.mux.HandleFunc("GET /api/v1/integrations/graph/mailboxes", r.listGraphMailboxes)
	r.mux.HandleFunc("POST /api/v1/integrations/graph/mailboxes", r.createGraphMailbox)
	r.mux.HandleFunc("PATCH /api/v1/integrations/graph/mailboxes/{id}", r.patchGraphMailbox)
	r.mux.HandleFunc("POST /api/v1/integrations/graph/mailboxes/{id}/credential", r.replaceGraphCredential)
}
func (r *Router) listGraphMailboxes(w http.ResponseWriter, req *http.Request) {
	p, ok := r.graphManagementPrincipal(w, req)
	if !ok {
		return
	}
	items, err := r.dependencies.GraphManagement.List(req.Context(), p)
	if err != nil {
		writeDomainError(w, req, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (r *Router) createGraphMailbox(w http.ResponseWriter, req *http.Request) {
	p, ok := r.graphManagementPrincipal(w, req)
	if !ok {
		return
	}
	var body graphMailboxCreateRequest
	if !decodeAIRequest(w, req, &body) {
		return
	}
	secret := []byte(body.ClientSecret)
	body.ClientSecret = ""
	defer wipeBytes(secret)
	item, err := r.dependencies.GraphManagement.Create(req.Context(), graphintake.CreateMailboxCommand{Principal: p, Mailbox: body.MailboxAddress, TenantID: body.TenantID, ClientID: body.ClientID, ClientSecret: secret, Reason: body.Reason})
	if err != nil {
		writeDomainError(w, req, err)
		return
	}
	writeETag(w, item.Version)
	writeJSON(w, http.StatusCreated, item)
}
func (r *Router) patchGraphMailbox(w http.ResponseWriter, req *http.Request) {
	p, ok := r.graphManagementPrincipal(w, req)
	if !ok {
		return
	}
	var body graphMailboxPatchRequest
	if !decodeAIRequest(w, req, &body) {
		return
	}
	item, err := r.dependencies.GraphManagement.Update(req.Context(), graphintake.UpdateMailboxCommand{Principal: p, ID: req.PathValue("id"), ExpectedVersion: body.ExpectedVersion, Mailbox: body.MailboxAddress, Enabled: body.Enabled, Reason: body.Reason})
	if err != nil {
		writeDomainError(w, req, err)
		return
	}
	writeETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}
func (r *Router) replaceGraphCredential(w http.ResponseWriter, req *http.Request) {
	p, ok := r.graphManagementPrincipal(w, req)
	if !ok {
		return
	}
	var body graphCredentialRequest
	if !decodeAIRequest(w, req, &body) {
		return
	}
	secret := []byte(body.ClientSecret)
	body.ClientSecret = ""
	defer wipeBytes(secret)
	item, err := r.dependencies.GraphManagement.ReplaceCredential(req.Context(), graphintake.ReplaceMailboxCredentialCommand{Principal: p, ID: req.PathValue("id"), ExpectedVersion: body.ExpectedVersion, TenantID: body.TenantID, ClientID: body.ClientID, ClientSecret: secret, Reason: body.Reason})
	if err != nil {
		writeDomainError(w, req, err)
		return
	}
	writeETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}
func (r *Router) graphManagementPrincipal(w http.ResponseWriter, req *http.Request) (authorization.Principal, bool) {
	p, ok := r.principal(req)
	if !ok {
		writeError(req.Context(), w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return authorization.Principal{}, false
	}
	if r.dependencies.GraphManagement == nil {
		writeError(req.Context(), w, http.StatusNotImplemented, "not_implemented", "Graph management is unavailable")
		return authorization.Principal{}, false
	}
	return p, true
}
