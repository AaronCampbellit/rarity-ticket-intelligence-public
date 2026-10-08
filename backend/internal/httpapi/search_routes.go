package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/search"
)

func (r *Router) registerSearchRoutes() {
	r.mux.HandleFunc("GET /api/v1/search", r.search)
}

func (r *Router) search(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Search == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "search service is unavailable")
		return
	}
	limit := 25
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(request.Context(), writer, http.StatusUnprocessableEntity, "validation_failed", "limit is invalid")
			return
		}
		limit = parsed
	}
	results, err := r.dependencies.Search.Search(
		request.Context(),
		search.Query{
			Principal: principal, Target: targetFor(principal),
			Text: request.URL.Query().Get("q"), Limit: limit,
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, results)
}
