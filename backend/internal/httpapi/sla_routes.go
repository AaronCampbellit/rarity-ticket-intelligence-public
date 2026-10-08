package httpapi

import (
	"net/http"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/sla"
)

type publishBusinessCalendarRequest struct {
	ExpectedVersion int64                  `json:"expected_version,omitempty"`
	Key             string                 `json:"key"`
	Name            string                 `json:"name"`
	Definition      sla.CalendarDefinition `json:"definition"`
}

type publishSLAPolicyRequest struct {
	ExpectedVersion         int64                `json:"expected_version,omitempty"`
	Key                     string               `json:"key"`
	Name                    string               `json:"name"`
	CalendarID              string               `json:"calendar_id"`
	Conditions              sla.PolicyConditions `json:"conditions,omitempty"`
	ResponseTargetSeconds   int                  `json:"response_target_seconds"`
	ResolutionTargetSeconds int                  `json:"resolution_target_seconds"`
	WarningPercent          int                  `json:"warning_percent"`
	PauseStates             []string             `json:"pause_states,omitempty"`
	Enabled                 bool                 `json:"enabled"`
	Priority                int                  `json:"priority"`
	StableOrder             int                  `json:"stable_order"`
	Fallback                bool                 `json:"fallback"`
}

func (r *Router) registerSLARoutes() {
	r.mux.HandleFunc("GET /api/v1/business-calendars", r.listBusinessCalendars)
	r.mux.HandleFunc("POST /api/v1/business-calendars", r.publishBusinessCalendar)
	r.mux.HandleFunc("POST /api/v1/business-calendars/{id}/versions", r.publishBusinessCalendar)
	r.mux.HandleFunc("POST /api/v1/sla-policies", r.publishSLAPolicy)
	r.mux.HandleFunc("GET /api/v1/sla-policies", r.listSLAPolicies)
	r.mux.HandleFunc("POST /api/v1/sla-policies/{id}/versions", r.publishSLAPolicy)
}

func (r *Router) listBusinessCalendars(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.SLACalendars == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "SLA calendar management is unavailable")
		return
	}
	found, err := r.dependencies.SLACalendars.ListCalendars(
		request.Context(), principal, targetFor(principal),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) listSLAPolicies(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.SLAPolicies == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "SLA policy management is unavailable")
		return
	}
	found, err := r.dependencies.SLAPolicies.ListPolicies(
		request.Context(), principal, targetFor(principal),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) publishSLAPolicy(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.SLAPolicies == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "SLA policy management is unavailable")
		return
	}
	var body publishSLAPolicyRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	published, err := r.dependencies.SLAPolicies.PublishPolicy(
		request.Context(),
		sla.PublishPolicyCommand{
			Principal: principal, Target: targetFor(principal),
			PolicyID: request.PathValue("id"), ExpectedVersion: body.ExpectedVersion,
			Key: body.Key, Name: body.Name, CalendarID: body.CalendarID,
			Conditions:              body.Conditions,
			ResponseTargetSeconds:   body.ResponseTargetSeconds,
			ResolutionTargetSeconds: body.ResolutionTargetSeconds,
			WarningPercent:          body.WarningPercent, PauseStates: body.PauseStates,
			Enabled: body.Enabled, Priority: body.Priority,
			StableOrder: body.StableOrder, Fallback: body.Fallback,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, published.Version)
	writeJSON(writer, http.StatusCreated, published)
}

func (r *Router) publishBusinessCalendar(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.SLACalendars == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "SLA calendar management is unavailable")
		return
	}
	var body publishBusinessCalendarRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	published, err := r.dependencies.SLACalendars.PublishCalendar(
		request.Context(),
		sla.PublishCalendarCommand{
			Principal: principal, Target: targetFor(principal),
			CalendarID: request.PathValue("id"), ExpectedVersion: body.ExpectedVersion,
			Key: body.Key, Name: body.Name, Definition: body.Definition,
			ActorID: principal.ID, Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, published.Version)
	writeJSON(writer, http.StatusCreated, published)
}
