package httpapi

import (
	"context"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/authorization"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/calendar"
	"github.com/rarity-ticket-intelligence/rarity/backend/internal/customfields"
	"net/http"
)

func (r *Router) getObjectCustomDateFields(w http.ResponseWriter, request *http.Request) {
	principal, ok := r.calendarPrincipal(w, request)
	if !ok {
		return
	}
	query, ok := r.dependencies.CalendarCustomDateQueries.(interface {
		ListObjectCustomDateFields(context.Context, authorization.Principal, customfields.ObjectType, string) ([]calendar.CustomDateField, error)
	})
	if !ok {
		writeError(request.Context(), w, http.StatusNotImplemented, "not_implemented", "custom date definitions are unavailable")
		return
	}
	result, err := query.ListObjectCustomDateFields(request.Context(), principal, customfields.ObjectType(request.PathValue("type")), request.PathValue("id"))
	if err != nil {
		writeDomainError(w, request, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
