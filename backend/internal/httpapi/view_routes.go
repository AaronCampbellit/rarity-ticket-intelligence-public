package httpapi

import (
	"net/http"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/views"
)

type saveViewRequest struct {
	Kind     views.Kind     `json:"kind"`
	Name     string         `json:"name"`
	Query    map[string]any `json:"query"`
	Audience views.Audience `json:"audience"`
}

func (r *Router) registerViewRoutes() {
	r.mux.HandleFunc("POST /api/v1/views", r.saveView)
	r.mux.HandleFunc("GET /api/v1/views", r.listViews)
	r.mux.HandleFunc("GET /api/v1/views/{id}/resolve", r.resolveView)
}

func (r *Router) listViews(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Views == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "view service is unavailable")
		return
	}
	kind := views.Kind(request.URL.Query().Get("kind"))
	if kind == views.KindCalendarLens {
		principal.Scope.ClientID = ""
	}
	found, err := r.dependencies.Views.List(request.Context(), views.ListCommand{
		Principal: principal, Target: targetFor(principal), Kind: kind,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) saveView(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Views == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "view service is unavailable")
		return
	}
	var body saveViewRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	if body.Kind == views.KindCalendarLens {
		principal.Scope.ClientID = ""
	}
	view, err := r.dependencies.Views.Save(request.Context(), views.SaveCommand{
		Principal: principal, OwnerID: principal.ID, Kind: body.Kind,
		Name: body.Name, Query: body.Query, Audience: body.Audience,
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, view.Version)
	writeJSON(writer, http.StatusCreated, view)
}

func (r *Router) resolveView(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Views == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "view service is unavailable")
		return
	}
	resolved, err := r.dependencies.Views.Resolve(request.Context(), views.ResolveCommand{
		Principal: principal, Target: targetFor(principal), ViewID: request.PathValue("id"),
	})
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, resolved)
}
