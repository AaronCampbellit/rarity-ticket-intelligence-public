package httpapi

import (
	"context"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"net/http"
)

func (r *Router) listCalendarDependencies(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	query, ok := r.dependencies.CalendarDependencies.(interface {
		List(context.Context, authorization.Principal, string) ([]calendar.Dependency, error)
	})
	if !ok {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "calendar dependencies are unavailable")
		return
	}
	result, err := query.List(request.Context(), principal, request.URL.Query().Get("projection_id"))
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
