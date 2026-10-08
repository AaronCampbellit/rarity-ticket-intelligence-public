package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/auditlog"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
)

type AuditActions interface {
	List(context.Context, authorization.Principal, int) ([]auditlog.Entry, error)
}

func (r *Router) registerAuditRoutes() {
	r.mux.HandleFunc("GET /api/v1/admin/audit", r.listAuditEntries)
}

func (r *Router) listAuditEntries(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Audit == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "audit query is unavailable")
		return
	}
	limit := 50
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeDomainError(writer, request, auditlog.ErrInvalidQuery)
			return
		}
		limit = parsed
	}
	entries, err := r.dependencies.Audit.List(request.Context(), principal, limit)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, entries)
}
