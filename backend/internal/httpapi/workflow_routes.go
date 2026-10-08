package httpapi

import (
	"net/http"

	"github.com/rarity-ticket-intelligence/rarity/backend/internal/workflow"
)

func (r *Router) registerWorkflowRoutes() {
	r.mux.HandleFunc("GET /api/v1/workflows", r.listWorkflows)
	r.mux.HandleFunc("POST /api/v1/workflows", r.createWorkflowVersion)
	r.mux.HandleFunc("POST /api/v1/workflows/{id}/versions", r.createWorkflowVersion)
}

func (r *Router) listWorkflows(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Workflows == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "workflow management is unavailable")
		return
	}
	found, err := r.dependencies.Workflows.List(
		request.Context(), principal, targetFor(principal),
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (r *Router) createWorkflowVersion(writer http.ResponseWriter, request *http.Request) {
	principal, ok := r.principal(request)
	if !ok {
		writeError(request.Context(), writer, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if r.dependencies.Workflows == nil {
		writeError(request.Context(), writer, http.StatusNotImplemented, "not_implemented", "workflow management is unavailable")
		return
	}
	var body PublishWorkflowRequest
	if !decodeRequest(writer, request, &body) {
		return
	}
	published, err := r.dependencies.Workflows.Publish(
		request.Context(),
		workflow.PublishCommand{
			Principal: principal, Target: targetFor(principal),
			WorkflowID:      request.PathValue("id"),
			ExpectedVersion: body.ExpectedVersion,
			Key:             body.Key, Name: body.Name, Enabled: body.Enabled,
			Priority: body.Priority, StableOrder: body.StableOrder,
			Fallback: body.Fallback, EffectiveFrom: body.EffectiveFrom,
			EffectiveTo: body.EffectiveTo, Conditions: body.Conditions,
			Definition: body.Definition, ActorID: principal.ID,
			Source: source(request),
		},
	)
	if err != nil {
		writeDomainError(writer, request, err)
		return
	}
	writeETag(writer, published.Version)
	writeJSON(writer, http.StatusCreated, published)
}
